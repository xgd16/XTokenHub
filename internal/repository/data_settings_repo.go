package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"xtokenhub/internal/model"
)

// dataSettingsRepo DataSettingsRepository 的 GORM 实现（单行表，ID 固定为 1）。
type dataSettingsRepo struct {
	db *gorm.DB
}

// NewDataSettingsRepository 构造数据管理设置仓储。
func NewDataSettingsRepository(db *gorm.DB) DataSettingsRepository {
	return &dataSettingsRepo{db: db}
}

// dataSettingsRowID 单行设置固定主键。
const dataSettingsRowID int64 = 1

func (r *dataSettingsRepo) Get(ctx context.Context) (*model.DataSettings, error) {
	var s model.DataSettings
	if err := r.db.WithContext(ctx).First(&s, dataSettingsRowID).Error; err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *dataSettingsRepo) Save(ctx context.Context, s *model.DataSettings) error {
	s.ID = dataSettingsRowID
	return r.db.WithContext(ctx).Save(s).Error
}

// UpdateConfig 只更新清理配置列（设置页入口）：聚合/清理状态由后台任务写回，
// 全量 Save 会用读出来的旧行覆盖它们，因此配置更新走列级更新。
func (r *dataSettingsRepo) UpdateConfig(ctx context.Context, s *model.DataSettings) error {
	s.ID = dataSettingsRowID
	s.UpdatedAt = time.Now()
	return r.db.WithContext(ctx).Model(&model.DataSettings{}).
		Where("id = ?", dataSettingsRowID).
		Select(
			"cleanup_enabled", "cleanup_mode", "cleanup_run_hour", "cleanup_interval_hours",
			"cleanup_max_days", "cleanup_batch_size", "cleanup_vacuum",
			"trim_headers", "trim_headers_after_days", "updated_at",
		).Updates(s).Error
}

// UpdateAggStatus 聚合任务写回水位线与最近执行时间。
func (r *dataSettingsRepo) UpdateAggStatus(ctx context.Context, watermarkDay int64, at time.Time) error {
	return r.db.WithContext(ctx).Model(&model.DataSettings{}).
		Where("id = ?", dataSettingsRowID).
		Updates(map[string]any{
			"agg_watermark_day": watermarkDay,
			"agg_last_run_at":   at,
		}).Error
}

// UpdateCleanupStatus 清理任务写回最近一次执行结果（重启不丢）。
func (r *dataSettingsRepo) UpdateCleanupStatus(ctx context.Context, at time.Time, day int64, deleted, trimmed, durationMS int64, lastErr string) error {
	return r.db.WithContext(ctx).Model(&model.DataSettings{}).
		Where("id = ?", dataSettingsRowID).
		Updates(map[string]any{
			"last_cleanup_at":   at,
			"last_cleanup_day":  day,
			"last_deleted_rows": deleted,
			"last_trimmed_rows": trimmed,
			"last_duration_ms":  durationMS,
			"last_error":        lastErr,
		}).Error
}
