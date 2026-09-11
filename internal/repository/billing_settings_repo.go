package repository

import (
	"context"

	"gorm.io/gorm"

	"xtokenhub/internal/model"
)

// BillingSettingsRepo 计费展示设置数据访问实现（单行表，ID 固定为 1）。
type BillingSettingsRepo struct {
	db *gorm.DB
}

// NewBillingSettingsRepository 构造计费设置仓储。
func NewBillingSettingsRepository(db *gorm.DB) BillingSettingsRepository {
	return &BillingSettingsRepo{db: db}
}

func (r *BillingSettingsRepo) Get(ctx context.Context) (*model.BillingSettings, error) {
	var s model.BillingSettings
	if err := r.db.WithContext(ctx).First(&s, billingSettingsRowID).Error; err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *BillingSettingsRepo) Save(ctx context.Context, s *model.BillingSettings) error {
	s.ID = billingSettingsRowID
	return r.db.WithContext(ctx).Save(s).Error
}

// billingSettingsRowID 单行设置固定主键。
const billingSettingsRowID int64 = 1
