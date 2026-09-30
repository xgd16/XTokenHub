package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"xtokenhub/internal/model"
)

// mimoSettingsRepo MimoSettingsRepository 的 GORM 实现（单行表，ID 固定为 1）。
type mimoSettingsRepo struct {
	db *gorm.DB
}

// NewMimoSettingsRepository 构造 MiMo 网页凭证仓储。
func NewMimoSettingsRepository(db *gorm.DB) MimoSettingsRepository {
	return &mimoSettingsRepo{db: db}
}

// mimoSettingsRowID 单行设置固定主键。
const mimoSettingsRowID int64 = 1

func (r *mimoSettingsRepo) Get(ctx context.Context) (*model.MimoSettings, error) {
	var s model.MimoSettings
	if err := r.db.WithContext(ctx).First(&s, mimoSettingsRowID).Error; err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *mimoSettingsRepo) Save(ctx context.Context, s *model.MimoSettings) error {
	s.ID = mimoSettingsRowID
	return r.db.WithContext(ctx).Save(s).Error
}

// UpdateCookie 只更新凭证列（设置页入口）：单行表仅此一列可变，走列级更新
// 与 data_settings 的 UpdateConfig 同款纪律——客户端提交不触碰其他列。
func (r *mimoSettingsRepo) UpdateCookie(ctx context.Context, cookie string) error {
	return r.db.WithContext(ctx).Model(&model.MimoSettings{}).
		Where("id = ?", mimoSettingsRowID).
		Updates(map[string]any{
			"cookie":     cookie,
			"updated_at": time.Now(),
		}).Error
}
