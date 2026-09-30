// Package aggregation 后台日级聚合任务：把 request_logs 按本地日汇总进
// daily_usage_stats / daily_model_stats 两张小表。仪表盘的累计/趋势/花费预测改为
// 「完整历史日读聚合表 + 当天实时查明细」：查询成本只与天数相关，不再随明细行数
// 增长；聚合行封存后不再变动，保留期清理明细不影响累计统计。
//
// 策略（水位线 + 滚动重建）：
//   - 每 tick 重建「昨天 + 今天」两天的聚合行（REPLACE 语义，幂等），迟到写入与
//     费用重算都在这一步落地；
//   - 水位线（已封存到的日，含）之前的天不再变动；每 tick 把水位线从旧值推进到
//     today-2，跨天时封存 1 天；
//   - 首次启动（水位线为空）先全量回填一次，把现存明细整体汇总；
//   - 费用重算完成后由 CostService 回调 RebuildRange 重建受影响日。
package aggregation

import (
	"context"
	"errors"
	"sync"
	"time"

	"gorm.io/gorm"

	"xtokenhub/internal/pkg/logger"
	"xtokenhub/internal/repository"
)

const (
	// module 日志模块名。
	module = "aggregation"
	// defaultInterval 聚合周期。
	defaultInterval = 5 * time.Minute
	// sealLag 水位线滞后天数：today-sealLag 及更早的本地日视为封存。
	// 最近 sealLag 天（昨天+今天）每轮重建，覆盖迟到写入与费用重算。
	sealLag = 2
	// maxRebuildDays RebuildRange 单次重建的日数上限，区间超过则退化为全量回填
	// （重算接口允许任意 from，防御性兜底）。
	maxRebuildDays = 400
)

// Service 日级聚合任务。
type Service struct {
	daily    repository.DailyStatsRepository
	settings repository.DataSettingsRepository
	interval time.Duration
	runMu    sync.Mutex // 串行化手动重建与定时 tick
}

// New 构造聚合任务。
func New(daily repository.DailyStatsRepository, settings repository.DataSettingsRepository) *Service {
	return &Service{daily: daily, settings: settings, interval: defaultInterval}
}

// Run 阻塞运行：先确保完成过全量回填，之后每 interval 聚合一轮；ctx 取消时退出。
func (s *Service) Run(ctx context.Context) {
	if err := s.ensureBackfill(ctx); err != nil {
		logger.L(module).Error("聚合回填失败", logger.Err(err))
	}
	tick := time.NewTicker(s.interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if err := s.TickOnce(ctx); err != nil {
				logger.L(module).Error("聚合执行失败", logger.Err(err))
			}
		}
	}
}

// ensureBackfill 首次启动（水位线为空）时全量回填一次，把现存明细整体汇总进聚合表。
// 设置行缺失视为尚未播种（main 在启动本任务前保证播种），本轮跳过、不重试回填。
func (s *Service) ensureBackfill(ctx context.Context) error {
	st, err := s.settings.Get(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			logger.L(module).Warn("数据管理设置尚未播种，跳过聚合回填")
			return nil
		}
		return err
	}
	if st.AggWatermarkDay > 0 {
		return nil
	}
	if _, err := s.RebuildAll(ctx); err != nil {
		return err
	}
	logger.L(module).Info("聚合首次回填完成")
	return nil
}

// TickOnce 聚合一轮：重建最近 sealLag 个本地日（含今天），再把水位线推进到 today-sealLag。
// 供 Run 循环与测试调用。
func (s *Service) TickOnce(ctx context.Context) error {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	return s.tickLocked(ctx)
}

func (s *Service) tickLocked(ctx context.Context) error {
	now := time.Now()
	today := repository.DayBucketOf(now)
	// 重建最近 sealLag 个本地日（昨天 + 今天）；更早的天只在跨天时由下方封存循环补最后一轮。
	for i := sealLag - 1; i >= 0; i-- {
		if err := s.daily.RebuildDay(ctx, today-int64(i)); err != nil {
			return err
		}
	}
	st, err := s.settings.Get(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil // 未播种：聚合行已重建，水位线待播种后由下一轮推进
		}
		return err
	}
	watermark := today - sealLag
	if st.AggWatermarkDay >= watermark {
		return s.settings.UpdateAggStatus(ctx, st.AggWatermarkDay, now)
	}
	// 封存 (水位线, today-sealLag] 区间：这些天离开滚动窗口前的最后一轮重建，
	// RebuildDay 幂等且源日无明细时自动跳过（保护已被清理明细的封存日）。
	for d := st.AggWatermarkDay + 1; d <= watermark; d++ {
		if err := s.daily.RebuildDay(ctx, d); err != nil {
			return err
		}
	}
	return s.settings.UpdateAggStatus(ctx, watermark, now)
}

// RebuildAll 全量重建：按现存明细整体重聚合并推进水位线（首次初始化 / 手动全量重建）。
// 注意：已删除明细的历史日不会出现在聚合表中——全量重建以现存明细为准。
func (s *Service) RebuildAll(ctx context.Context) (int64, error) {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	days, err := s.daily.BackfillAll(ctx)
	if err != nil {
		return 0, err
	}
	watermark := repository.DayBucketOf(time.Now()) - sealLag
	if err := s.settings.UpdateAggStatus(ctx, watermark, time.Now()); err != nil {
		return days, err
	}
	return days, nil
}

// RebuildRange 重建 [from, to] 覆盖的本地日（费用重算回调与手动重建近 N 天用）。
// 区间超过 maxRebuildDays 天时退化为全量重建。
func (s *Service) RebuildRange(ctx context.Context, from, to time.Time) error {
	if to.Before(from) {
		to = from
	}
	fromDay := repository.DayBucketOf(from)
	toDay := repository.DayBucketOf(to)
	if toDay-fromDay > maxRebuildDays {
		_, err := s.RebuildAll(ctx)
		return err
	}
	s.runMu.Lock()
	defer s.runMu.Unlock()
	for d := fromDay; d <= toDay; d++ {
		if err := s.daily.RebuildDay(ctx, d); err != nil {
			return err
		}
	}
	return nil
}

// Status 聚合状态快照（设置页展示）。
type Status struct {
	// WatermarkDay 已封存到的本地日桶号（含）。
	WatermarkDay int64 `json:"watermark_day"`
	// WatermarkDate 水位线对应的本地日期。
	WatermarkDate string `json:"watermark_date"`
	// LastRunAt 最近一次聚合时间。
	LastRunAt time.Time `json:"last_run_at"`
	// EarliestDay/LatestDay 已聚合数据的日期范围（空表时为零值）。
	EarliestDay string `json:"earliest_day"`
	LatestDay   string `json:"latest_day"`
}

// Status 读取聚合状态。
func (s *Service) Status(ctx context.Context) (*Status, error) {
	st, err := s.settings.Get(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &Status{}, nil
		}
		return nil, err
	}
	minDay, maxDay, err := s.daily.MinMaxDay(ctx)
	if err != nil {
		return nil, err
	}
	out := &Status{
		WatermarkDay:  st.AggWatermarkDay,
		WatermarkDate: repository.DayBucketDate(st.AggWatermarkDay),
		LastRunAt:     st.AggLastRunAt,
	}
	if minDay > 0 {
		out.EarliestDay = repository.DayBucketDate(minDay)
		out.LatestDay = repository.DayBucketDate(maxDay)
	}
	return out, nil
}
