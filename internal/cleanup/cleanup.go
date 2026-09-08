// Package cleanup 后台清理任务：按保留期分批删除过期请求日志。
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

// Options 清理参数。
type Options struct {
	// Enabled 是否启用定时自动清理（影响 Status 展示，不影响手动触发）。
	Enabled bool
	// MaxDays 保留最近 N 天日志。
	MaxDays int
	// IntervalHrs 定时运行周期（小时）。
	IntervalHrs int
	// BatchSize 单批删除行数。
	BatchSize int
	// Vacuum 清理生效后执行 VACUUM 回收磁盘空间。
	Vacuum bool
}

// Result 一次清理的结果。
type Result struct {
	DeletedRows int64     `json:"deleted_rows"`
	DurationMS  int64     `json:"duration_ms"`
	Cutoff      time.Time `json:"cutoff"`
	StartedAt   time.Time `json:"started_at"`
	DoneAt      time.Time `json:"done_at"`
	Error       string    `json:"error,omitempty"`
}

// StatusView 清理状态快照（线程安全读取）。
type StatusView struct {
	Enabled      bool       `json:"enabled"`
	MaxDays      int        `json:"max_days"`
	IntervalHrs  int        `json:"interval_hours"`
	Vacuum       bool       `json:"vacuum"`
	LastRunAt    *time.Time `json:"last_run_at"`
	LastDeleted  int64      `json:"last_deleted_rows"`
	LastDuration int64      `json:"last_duration_ms"`
	LastError    string     `json:"last_error,omitempty"`
}

// Retention request_logs 保留期清理任务：定时运行 + 手动触发。
type Retention struct {
	repo   repository.RequestLogRepository
	db     *gorm.DB
	opts   Options
	runMu  sync.Mutex // 串行化清理执行（TryLock 判空）
	stMu   sync.Mutex // 保护 last / hasRun
	last   Result
	hasRun bool
}

// NewRetention 构造清理任务；db 仅用于可选 VACUUM，可为 nil。
func NewRetention(repo repository.RequestLogRepository, db *gorm.DB, opts Options) *Retention {
	if opts.MaxDays <= 0 {
		opts.MaxDays = 90
	}
	if opts.IntervalHrs <= 0 {
		opts.IntervalHrs = 24
	}
	if opts.BatchSize <= 0 {
		opts.BatchSize = 1000
	}
	return &Retention{repo: repo, db: db, opts: opts}
}

// Run 阻塞运行：启动时立即清理一次，之后每 IntervalHrs 小时清理一次；ctx 取消时退出。
func (r *Retention) Run(ctx context.Context) {
	if _, err := r.CleanupOnce(ctx); err != nil {
		logger.L("retention").Error("启动清理失败", logger.Err(err))
	}
	tick := time.NewTicker(time.Duration(r.opts.IntervalHrs) * time.Hour)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if _, err := r.CleanupOnce(ctx); err != nil {
				logger.L("retention").Error("定时清理失败", logger.Err(err))
			}
		}
	}
}

// CleanupOnce 立即执行一次清理：按 cutoff 分批删除，直到单批删除数小于 BatchSize 或出错。
// 并发的 CleanupOnce/Trigger 通过 runMu 串行化，重入返回「清理正在进行中」业务错误。
func (r *Retention) CleanupOnce(ctx context.Context) (*Result, error) {
	r.runMu.Lock()
	defer r.runMu.Unlock()
	return r.onceLocked(ctx)
}

// Trigger 手动触发一次清理（供 HTTP 接口）；已有清理运行中时返回业务错误。
func (r *Retention) Trigger(ctx context.Context) (*Result, error) {
	if !r.runMu.TryLock() {
		return nil, errs.New(errs.CodeInvalidParams, "清理正在进行中，请稍后重试")
	}
	defer r.runMu.Unlock()
	return r.onceLocked(ctx)
}

// onceLocked 假定 runMu 已持有，执行单轮清理并记录结果。
func (r *Retention) onceLocked(ctx context.Context) (*Result, error) {
	start := time.Now()
	cutoff := start.AddDate(0, 0, -r.opts.MaxDays)
	res := &Result{Cutoff: cutoff, StartedAt: start}

	var total int64
	for {
		n, err := r.repo.DeleteBefore(ctx, cutoff, r.opts.BatchSize)
		if err != nil {
			res.Error = err.Error()
			r.finish(res, start)
			logger.L("retention").Error("清理日志失败", logger.Err(err), "cutoff", cutoff)
			return res, err
		}
		total += n
		if n < int64(r.opts.BatchSize) {
			break
		}
	}
	res.DeletedRows = total

	if r.opts.Vacuum && total > 0 && r.db != nil {
		if err := r.db.Exec("VACUUM").Error; err != nil {
			res.Error = "vacuum: " + err.Error()
		}
	}

	r.finish(res, start)
	logger.L("retention").Info("清理日志完成",
		"deleted", total, "cutoff", cutoff, "duration_ms", res.DurationMS,
		"batch_size", r.opts.BatchSize, "vacuum", r.opts.Vacuum)
	return res, nil
}

// finish 补全结束时间并记录最近一次结果。
func (r *Retention) finish(res *Result, start time.Time) {
	res.DoneAt = time.Now()
	res.DurationMS = res.DoneAt.Sub(start).Milliseconds()
	r.stMu.Lock()
	r.hasRun = true
	r.last = *res
	r.stMu.Unlock()
}

// Status 返回清理状态快照。
func (r *Retention) Status() StatusView {
	sv := StatusView{
		Enabled:     r.opts.Enabled,
		MaxDays:     r.opts.MaxDays,
		IntervalHrs: r.opts.IntervalHrs,
		Vacuum:      r.opts.Vacuum,
	}
	r.stMu.Lock()
	defer r.stMu.Unlock()
	if r.hasRun {
		t := r.last.DoneAt
		sv.LastRunAt = &t
		sv.LastDeleted = r.last.DeletedRows
		sv.LastDuration = r.last.DurationMS
		sv.LastError = r.last.Error
	}
	return sv
}
