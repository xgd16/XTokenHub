// Package cleanup 后台清理任务：按保留期分批删除过期请求日志，并按设置把超期
// 请求头置空瘦身。配置来自数据管理设置（每轮执行前读取，设置页改完即生效），
// 支持两种触发模式：每天固定时刻 / 固定间隔；结果持久化进设置行，重启不丢。
package cleanup

import (
	"context"
	"sync"
	"time"

	"gorm.io/gorm"

	"xtokenhub/internal/pkg/errs"
	"xtokenhub/internal/pkg/logger"
	"xtokenhub/internal/repository"
)

// 清理触发模式。
const (
	ModeDaily    = "daily"    // 每天固定时刻
	ModeInterval = "interval" // 固定间隔
)

const (
	// defaultMaxDays 未配置时的保留天数。
	defaultMaxDays = 90
	// defaultBatchSize 未配置时的单批删除行数。
	defaultBatchSize = 1000
	// defaultRunHour daily 模式默认执行时刻（凌晨低峰）。
	defaultRunHour = 4
	// defaultIntervalHours interval 模式默认周期。
	defaultIntervalHours = 24
	// checkEvery 定时检查周期：daily 模式按小时粒度对点，interval 模式按该粒度判到期。
	checkEvery = 10 * time.Minute
)

// Settings 清理配置（由数据管理设置提供）。
type Settings struct {
	// Enabled 是否启用自动清理（不影响手动触发）。
	Enabled bool
	// Mode 触发模式：daily | interval。
	Mode string
	// RunHour daily 模式的每日执行时刻（0-23，本地时区）。
	RunHour int
	// IntervalHours interval 模式的执行间隔（小时）。
	IntervalHours int
	// MaxDays 保留最近 N 天日志。
	MaxDays int
	// BatchSize 单批删除行数。
	BatchSize int
	// Vacuum 清理生效后执行 VACUUM 回收磁盘空间。
	Vacuum bool
	// TrimHeaders 请求头瘦身开关。
	TrimHeaders bool
	// TrimHeadersAfterDays 请求头保留天数（置空更早的行）。
	TrimHeadersAfterDays int
	// LastCleanupDay 最近一次清理所在的本地日桶号（daily 模式每天至多一次的判定依据）。
	LastCleanupDay int64
	// LastCleanupAt 最近一次清理完成时间（interval 模式的到期判定依据）。
	LastCleanupAt time.Time
}

// SettingsProvider 读取当前清理配置；返回错误时本轮跳过（不静默回退默认值，
// 避免设置行异常时按未预期参数删数据）。
type SettingsProvider func(ctx context.Context) (Settings, error)

// Result 一次清理的结果。
type Result struct {
	DeletedRows int64     `json:"deleted_rows"`
	TrimmedRows int64     `json:"trimmed_rows"` // 置空请求头的行数
	DurationMS  int64     `json:"duration_ms"`
	Cutoff      time.Time `json:"cutoff"`
	StartedAt   time.Time `json:"started_at"`
	DoneAt      time.Time `json:"done_at"`
	Error       string    `json:"error,omitempty"`
}

// StatusView 清理状态快照（线程安全读取）。
type StatusView struct {
	Enabled     bool `json:"enabled"`
	MaxDays     int  `json:"max_days"`
	IntervalHrs int  `json:"interval_hours"` // 兼容字段：interval 模式的周期值
	// Mode 触发模式：daily | interval。
	Mode string `json:"mode"`
	// RunHour daily 模式的每日执行时刻（0-23）。
	RunHour int  `json:"run_hour"`
	Vacuum  bool `json:"vacuum"`
	// TrimHeaders 请求头瘦身开关与其保留天数。
	TrimHeaders          bool `json:"trim_headers"`
	TrimHeadersAfterDays int  `json:"trim_headers_after_days"`

	LastRunAt    *time.Time `json:"last_run_at"`
	LastDeleted  int64      `json:"last_deleted_rows"`
	LastTrimmed  int64      `json:"last_trimmed_rows"`
	LastDuration int64      `json:"last_duration_ms"`
	LastError    string     `json:"last_error,omitempty"`
}

// Retention request_logs 保留期清理任务：按设置定时运行 + 手动触发。
type Retention struct {
	repo     repository.RequestLogRepository
	db       *gorm.DB // 仅用于可选 VACUUM，可为 nil
	settings SettingsProvider
	// onFinish 最近一次执行结果持久化回调（可空），在锁内同步调用。
	onFinish func(ctx context.Context, res *Result)
	runMu    sync.Mutex // 串行化清理执行
	stMu     sync.Mutex // 保护 last / hasRun
	last     Result
	hasRun   bool
}

// NewRetention 构造清理任务。settings 每轮执行前读取（设置页改完即生效）；
// onFinish 用于把结果持久化进数据管理设置（可为 nil）。
func NewRetention(repo repository.RequestLogRepository, db *gorm.DB, settings SettingsProvider, onFinish func(ctx context.Context, res *Result)) *Retention {
	return &Retention{repo: repo, db: db, settings: settings, onFinish: onFinish}
}

// Run 阻塞运行：启动先判一次（当天未跑且已到点则补上，保持「启动即清」精神），
// 之后每 checkEvery 检查是否到期；ctx 取消时退出。
func (r *Retention) Run(ctx context.Context) {
	r.runIfDue(ctx)
	tick := time.NewTicker(checkEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			r.runIfDue(ctx)
		}
	}
}

// runIfDue 到期才执行（锁内复查触发条件，避免与并发手动触发重复执行）。
func (r *Retention) runIfDue(ctx context.Context) {
	r.runMu.Lock()
	defer r.runMu.Unlock()
	st, err := r.settings(ctx)
	if err != nil {
		logger.L("retention").Error("读取清理配置失败，本轮跳过", logger.Err(err))
		return
	}
	if !due(st, time.Now()) {
		return
	}
	r.onceLocked(ctx, st)
}

// CleanupOnce 立即执行一次清理（忽略触发条件与 Enabled，供测试与运维调用）。
func (r *Retention) CleanupOnce(ctx context.Context) (*Result, error) {
	r.runMu.Lock()
	defer r.runMu.Unlock()
	st, err := r.settings(ctx)
	if err != nil {
		return nil, errs.Wrap(errs.CodeInternal, "读取清理配置失败", err)
	}
	return r.onceLocked(ctx, st)
}

// Trigger 手动触发一次清理（供 HTTP 接口，不受 Enabled/触发时刻限制）；
// 已有清理运行中时返回业务错误。
func (r *Retention) Trigger(ctx context.Context) (*Result, error) {
	if !r.runMu.TryLock() {
		return nil, errs.New(errs.CodeInvalidParams, "清理正在进行中，请稍后重试")
	}
	defer r.runMu.Unlock()
	st, err := r.settings(ctx)
	if err != nil {
		return nil, errs.Wrap(errs.CodeInternal, "读取清理配置失败", err)
	}
	return r.onceLocked(ctx, st)
}

// due 判断当前是否该执行：
//   - daily：今天尚未清理且已到执行时刻（错过时刻顺延补跑，停机恢复后也能追上）；
//   - interval：距上次清理已满间隔（从未清理过视为已到期，保持旧「启动即清」行为）。
func due(st Settings, now time.Time) bool {
	if !st.Enabled {
		return false
	}
	switch st.Mode {
	case ModeInterval:
		if st.LastCleanupAt.IsZero() {
			return true
		}
		hours := st.IntervalHours
		if hours <= 0 {
			hours = defaultIntervalHours
		}
		return now.Sub(st.LastCleanupAt) >= time.Duration(hours)*time.Hour
	default: // daily
		if st.LastCleanupDay == repository.DayBucketOf(now) {
			return false
		}
		return now.Hour() >= st.RunHour
	}
}

// onceLocked 假定 runMu 已持有：分批删除过期日志 + 置空超期请求头 + 可选 VACUUM，
// 并把结果交给 onFinish 持久化。st.MaxDays/BatchSize 等非法值按默认兜底。
func (r *Retention) onceLocked(ctx context.Context, st Settings) (*Result, error) {
	maxDays := st.MaxDays
	if maxDays <= 0 {
		maxDays = defaultMaxDays
	}
	batch := st.BatchSize
	if batch <= 0 {
		batch = defaultBatchSize
	}
	start := time.Now()
	cutoff := start.AddDate(0, 0, -maxDays)
	res := &Result{Cutoff: cutoff, StartedAt: start}

	// 1) 分批删除过期日志
	var total int64
	for {
		n, err := r.repo.DeleteBefore(ctx, cutoff, batch)
		if err != nil {
			res.Error = err.Error()
			r.finish(ctx, res, start)
			logger.L("retention").Error("清理日志失败", logger.Err(err), "cutoff", cutoff)
			return res, err
		}
		total += n
		if n < int64(batch) {
			break
		}
	}
	res.DeletedRows = total

	// 2) 请求头瘦身：置空早于瘦身体积线（且仍在保留期内）的行的请求头。
	//    保留期清理只删 MaxDays 之前的天，7d~MaxDays 之间的明细仍在，靠这一步省空间。
	if st.TrimHeaders {
		trimCutoff := start.AddDate(0, 0, -st.TrimHeadersAfterDays)
		if trimCutoff.Before(cutoff) {
			trimCutoff = cutoff // 瘦身线早于保留线时无区间可瘦
		}
		var trimmed int64
		for {
			n, err := r.repo.TrimHeadersBefore(ctx, trimCutoff, batch)
			if err != nil {
				res.Error = "trim_headers: " + err.Error()
				logger.L("retention").Error("请求头瘦身失败", logger.Err(err))
				break // 瘦身失败不回滚已删除的过期日志
			}
			trimmed += n
			if n < int64(batch) {
				break
			}
		}
		res.TrimmedRows = trimmed
	}

	// 3) 可选 VACUUM 回收磁盘（独占锁，量大耗时；由设置页开关控制）
	if st.Vacuum && total > 0 && r.db != nil {
		if err := r.db.Exec("VACUUM").Error; err != nil {
			res.Error = "vacuum: " + err.Error()
		}
	}

	r.finish(ctx, res, start)
	logger.L("retention").Info("清理日志完成",
		"deleted", total, "trimmed", res.TrimmedRows, "cutoff", cutoff,
		"duration_ms", res.DurationMS, "batch_size", batch, "vacuum", st.Vacuum)
	return res, nil
}

// finish 补全结束时间、记录最近一次结果并持久化。
func (r *Retention) finish(ctx context.Context, res *Result, start time.Time) {
	res.DoneAt = time.Now()
	res.DurationMS = res.DoneAt.Sub(start).Milliseconds()
	r.stMu.Lock()
	r.hasRun = true
	r.last = *res
	r.stMu.Unlock()
	if r.onFinish != nil {
		r.onFinish(ctx, res)
	}
}

// Status 返回清理状态快照（配置来自数据管理设置，执行信息来自最近一次结果）。
func (r *Retention) Status(ctx context.Context) (StatusView, error) {
	st, err := r.settings(ctx)
	if err != nil {
		return StatusView{}, err
	}
	sv := StatusView{
		Enabled:              st.Enabled,
		MaxDays:              st.MaxDays,
		IntervalHrs:          st.IntervalHours,
		Mode:                 st.Mode,
		RunHour:              st.RunHour,
		Vacuum:               st.Vacuum,
		TrimHeaders:          st.TrimHeaders,
		TrimHeadersAfterDays: st.TrimHeadersAfterDays,
	}
	r.stMu.Lock()
	defer r.stMu.Unlock()
	if r.hasRun {
		t := r.last.DoneAt
		sv.LastRunAt = &t
		sv.LastDeleted = r.last.DeletedRows
		sv.LastTrimmed = r.last.TrimmedRows
		sv.LastDuration = r.last.DurationMS
		sv.LastError = r.last.Error
	}
	return sv, nil
}
