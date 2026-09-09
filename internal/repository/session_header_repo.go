package repository

import (
	"context"

	"gorm.io/gorm"

	"xtokenhub/internal/model"
)

// SessionHeaderConfigRepo 会话标识配置数据访问实现。
type SessionHeaderConfigRepo struct {
	db *gorm.DB
}

// NewSessionHeaderConfigRepo 构造。
func NewSessionHeaderConfigRepo(db *gorm.DB) *SessionHeaderConfigRepo {
	return &SessionHeaderConfigRepo{db: db}
}

// List 获取所有配置。
func (r *SessionHeaderConfigRepo) List(ctx context.Context) ([]model.SessionHeaderConfig, error) {
	var configs []model.SessionHeaderConfig
	err := r.db.WithContext(ctx).Order("id ASC").Find(&configs).Error
	return configs, err
}

// Create 创建配置。
func (r *SessionHeaderConfigRepo) Create(ctx context.Context, config *model.SessionHeaderConfig) error {
	return r.db.WithContext(ctx).Create(config).Error
}

// Update 更新配置。
func (r *SessionHeaderConfigRepo) Update(ctx context.Context, config *model.SessionHeaderConfig) error {
	return r.db.WithContext(ctx).Save(config).Error
}

// Delete 删除配置。
func (r *SessionHeaderConfigRepo) Delete(ctx context.Context, id int64) error {
	return r.db.WithContext(ctx).Delete(&model.SessionHeaderConfig{}, id).Error
}

// GetByID 按 id 获取配置。
func (r *SessionHeaderConfigRepo) GetByID(ctx context.Context, id int64) (*model.SessionHeaderConfig, error) {
	var config model.SessionHeaderConfig
	if err := r.db.WithContext(ctx).First(&config, id).Error; err != nil {
		return nil, err
	}
	return &config, nil
}

// GetByKey 根据 key 获取配置。
func (r *SessionHeaderConfigRepo) GetByKey(ctx context.Context, key string) (*model.SessionHeaderConfig, error) {
	var config model.SessionHeaderConfig
	if err := r.db.WithContext(ctx).Where("`key` = ?", key).First(&config).Error; err != nil {
		return nil, err
	}
	return &config, nil
}

// ListEnabled 获取所有启用的配置。
func (r *SessionHeaderConfigRepo) ListEnabled(ctx context.Context) ([]model.SessionHeaderConfig, error) {
	var configs []model.SessionHeaderConfig
	err := r.db.WithContext(ctx).Where("enabled = ?", true).Order("id ASC").Find(&configs).Error
	return configs, err
}

// Count 统计配置总数（供种子化判断）。
func (r *SessionHeaderConfigRepo) Count(ctx context.Context, count *int64) error {
	return r.db.WithContext(ctx).Model(&model.SessionHeaderConfig{}).Count(count).Error
}
