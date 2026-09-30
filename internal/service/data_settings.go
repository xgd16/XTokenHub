package service

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"xtokenhub/internal/aggregation"
	"xtokenhub/internal/cleanup"
	"xtokenhub/internal/model"
	"xtokenhub/internal/pkg/errs"
	"xtokenhub/internal/repository"
)

// DataSettingsService 数据管理业务：清理配置（自动清理开关/模式/时刻、保留天数、
// 请求头瘦身）与日级聚合状态。配置存单行表 data_settings，首次启动以 yaml retention
// 播种，之后以设置页为准；后台任务每轮执行前重新读取，改完即生效。
type DataSettingsService struct {
	repo      repository.DataSettingsRepository
	agg       *aggregation.Service
	retention *cleanup.Retention
	initial   model.DataSettings // yaml 播种默认值
}

// NewDataSettingsService 构造数据管理业务。
func NewDataSettingsService(repo repository.DataSettingsRepository, agg *aggregation.Service, retention *cleanup.Retention, initial model.DataSettings) *DataSettingsService {
	return &DataSettingsService{repo: repo, agg: agg, retention: retention, initial: initial}
}

// EnsureDefaults 首次启动播种（与计费设置同款）：表为空时以 yaml 值初始化，之后不覆盖。
func (s *DataSettingsService) EnsureDefaults(ctx context.Context) error {
	_, err := s.repo.Get(ctx)
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	seed := s.initial
	seed.ID = 1
	seed.UpdatedAt = time.Now()
	return s.repo.Save(ctx, &seed)
}

// Settings 读取数据管理设置（含聚合与清理运行状态）。
func (s *DataSettingsService) Settings(ctx context.Context) (*model.DataSettings, error) {
	return s.repo.Get(ctx)
}

// UpdateDataSettingsInput 清理配置更新入参（设置页整单提交）。
type UpdateDataSettingsInput struct {
	// Enabled 自动清理开关（关闭后仍可手动触发）。
	Enabled bool `json:"enabled"`
	// Mode 触发模式：daily（每天固定时刻）| interval（固定间隔）。
	Mode string `json:"mode"`
	// RunHour daily 模式执行时刻（0-23，本地时区）。
	RunHour int `json:"run_hour"`
	// IntervalHours interval 模式执行间隔（小时）。
	IntervalHours int `json:"interval_hours"`
	// MaxDays 保留最近 N 天日志（累计统计读聚合表，不受影响）。
	MaxDays int `json:"max_days"`
	// BatchSize 单批删除行数，0 = 默认 1000。
	BatchSize int `json:"batch_size"`
	// Vacuum 清理生效后执行 VACUUM 回收磁盘（独占锁，建议低峰开启）。
	Vacuum bool `json:"vacuum"`
	// TrimHeaders 请求头瘦身开关。
	TrimHeaders bool `json:"trim_headers"`
	// TrimHeadersAfterDays 请求头保留天数（置空更早的行）。
	TrimHeadersAfterDays int `json:"trim_headers_after_days"`
}

// Update 更新清理配置（只写配置列，运行状态字段不动），校验后立即生效。
func (s *DataSettingsService) Update(ctx context.Context, in *UpdateDataSettingsInput) (*model.DataSettings, error) {
	if in.Mode != model.CleanupModeDaily && in.Mode != model.CleanupModeInterval {
		return nil, errs.New(errs.CodeInvalidParams, "mode 只支持 daily 或 interval")
	}
	if in.RunHour < 0 || in.RunHour > 23 {
		return nil, errs.New(errs.CodeInvalidParams, "run_hour 需在 0-23 之间")
	}
	if in.Mode == model.CleanupModeInterval && (in.IntervalHours < 1 || in.IntervalHours > 24*30) {
		return nil, errs.New(errs.CodeInvalidParams, "interval_hours 需在 1-720 之间")
	}
	if in.MaxDays < 1 || in.MaxDays > 3650 {
		return nil, errs.New(errs.CodeInvalidParams, "max_days 需在 1-3650 之间")
	}
	batch := in.BatchSize
	if batch == 0 {
		batch = 1000
	}
	if batch < 100 || batch > 10000 {
		return nil, errs.New(errs.CodeInvalidParams, "batch_size 需在 100-10000 之间")
	}
	trimDays := in.TrimHeadersAfterDays
	if trimDays == 0 {
		trimDays = 7 // 未填时保底默认，便于重新开启
	}
	if in.TrimHeaders && (trimDays < 1 || trimDays >= in.MaxDays) {
		return nil, errs.New(errs.CodeInvalidParams, "请求头保留天数需不小于 1 且小于日志保留天数")
	}
	row := &model.DataSettings{
		CleanupEnabled:       in.Enabled,
		CleanupMode:          in.Mode,
		CleanupRunHour:       in.RunHour,
		CleanupIntervalHours: in.IntervalHours,
		CleanupMaxDays:       in.MaxDays,
		CleanupBatchSize:     batch,
		CleanupVacuum:        in.Vacuum,
		TrimHeaders:          in.TrimHeaders,
		TrimHeadersAfterDays: trimDays,
	}
	if err := s.repo.UpdateConfig(ctx, row); err != nil {
		return nil, errs.Wrap(errs.CodeInternal, "保存清理配置失败", err)
	}
	return s.repo.Get(ctx)
}

// TriggerCleanup 手动触发一次清理（不受启用开关限制，与 /logs/cleanup 同一入口）。
func (s *DataSettingsService) TriggerCleanup(ctx context.Context) (*cleanup.Result, error) {
	return s.retention.Trigger(ctx)
}

// RebuildAggregate 重建聚合：days > 0 重建近 N 天，否则全量重建（以现存明细为准）。
func (s *DataSettingsService) RebuildAggregate(ctx context.Context, days int) error {
	if days <= 0 {
		_, err := s.agg.RebuildAll(ctx)
		return err
	}
	to := time.Now()
	return s.agg.RebuildRange(ctx, to.AddDate(0, 0, -days), to)
}

// AggregateStatus 聚合状态（水位线、覆盖范围、最近执行时间）。
func (s *DataSettingsService) AggregateStatus(ctx context.Context) (*aggregation.Status, error) {
	return s.agg.Status(ctx)
}
