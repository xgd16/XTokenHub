package repository

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"xtokenhub/internal/model"
	"xtokenhub/internal/pkg/errs"
	"xtokenhub/internal/pkg/pagination"
)

// customModelRepo CustomModelRepository 的 GORM 实现。
type customModelRepo struct {
	db *gorm.DB
}

// NewCustomModelRepository 构造自定义模型组仓储。
func NewCustomModelRepository(db *gorm.DB) CustomModelRepository {
	return &customModelRepo{db: db}
}

func (r *customModelRepo) Create(ctx context.Context, cm *model.CustomModel) error {
	return r.db.WithContext(ctx).Create(cm).Error
}

func (r *customModelRepo) Update(ctx context.Context, cm *model.CustomModel) error {
	res := r.db.WithContext(ctx).Model(cm).Select("*").Updates(cm)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errs.New(errs.CodeNotFound, "自定义模型不存在")
	}
	return nil
}

func (r *customModelRepo) Delete(ctx context.Context, id int64) error {
	res := r.db.WithContext(ctx).Delete(&model.CustomModel{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errs.New(errs.CodeNotFound, "自定义模型不存在")
	}
	return nil
}

func (r *customModelRepo) Get(ctx context.Context, id int64) (*model.CustomModel, error) {
	var cm model.CustomModel
	if err := r.db.WithContext(ctx).First(&cm, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.New(errs.CodeNotFound, "自定义模型不存在")
		}
		return nil, err
	}
	return &cm, nil
}

func (r *customModelRepo) GetByName(ctx context.Context, name string) (*model.CustomModel, error) {
	var cm model.CustomModel
	if err := r.db.WithContext(ctx).Where("name = ?", name).First(&cm).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.New(errs.CodeNotFound, "自定义模型不存在")
		}
		return nil, err
	}
	return &cm, nil
}

func (r *customModelRepo) List(ctx context.Context, p pagination.Params) ([]model.CustomModel, int64, error) {
	var (
		items []model.CustomModel
		total int64
	)
	q := r.db.WithContext(ctx).Model(&model.CustomModel{})
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count custom models: %w", err)
	}
	err := q.Order("id ASC").Offset(p.Offset).Limit(p.Limit).Find(&items).Error
	if items == nil {
		items = []model.CustomModel{}
	}
	return items, total, err
}

func (r *customModelRepo) ListEnabled(ctx context.Context) ([]model.CustomModel, error) {
	items := make([]model.CustomModel, 0, 8)
	err := r.db.WithContext(ctx).
		Where("status = ?", model.ChannelEnabled).
		Order("id ASC").
		Find(&items).Error
	return items, err
}
