package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"xtokenhub/internal/model"
	"xtokenhub/internal/pkg/errs"
	"xtokenhub/internal/pkg/pagination"
)

// APIKeyRepository 网关密钥数据访问接口。
type APIKeyRepository interface {
	Create(ctx context.Context, k *model.APIKey) error
	Update(ctx context.Context, k *model.APIKey) error
	Delete(ctx context.Context, id int64) error
	Get(ctx context.Context, id int64) (*model.APIKey, error)
	GetByName(ctx context.Context, name string) (*model.APIKey, error)
	// GetByKey 按密钥原文查询（鉴权热路径，走唯一索引）。
	GetByKey(ctx context.Context, key string) (*model.APIKey, error)
	List(ctx context.Context, p pagination.Params) ([]model.APIKey, int64, error)
}

// apiKeyRepo APIKeyRepository 的 GORM 实现。
type apiKeyRepo struct {
	db *gorm.DB
}

// NewAPIKeyRepository 构造密钥仓储。
func NewAPIKeyRepository(db *gorm.DB) APIKeyRepository {
	return &apiKeyRepo{db: db}
}

func (r *apiKeyRepo) Create(ctx context.Context, k *model.APIKey) error {
	return r.db.WithContext(ctx).Create(k).Error
}

func (r *apiKeyRepo) Update(ctx context.Context, k *model.APIKey) error {
	res := r.db.WithContext(ctx).Model(k).Select("*").Updates(k)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errs.New(errs.CodeNotFound, "密钥不存在")
	}
	return nil
}

func (r *apiKeyRepo) Delete(ctx context.Context, id int64) error {
	res := r.db.WithContext(ctx).Delete(&model.APIKey{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errs.New(errs.CodeNotFound, "密钥不存在")
	}
	return nil
}

func (r *apiKeyRepo) Get(ctx context.Context, id int64) (*model.APIKey, error) {
	var k model.APIKey
	if err := r.db.WithContext(ctx).First(&k, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.New(errs.CodeNotFound, "密钥不存在")
		}
		return nil, err
	}
	return &k, nil
}

func (r *apiKeyRepo) GetByName(ctx context.Context, name string) (*model.APIKey, error) {
	var k model.APIKey
	if err := r.db.WithContext(ctx).Where("name = ?", name).First(&k).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.New(errs.CodeNotFound, "密钥不存在")
		}
		return nil, err
	}
	return &k, nil
}

func (r *apiKeyRepo) GetByKey(ctx context.Context, key string) (*model.APIKey, error) {
	var k model.APIKey
	if err := r.db.WithContext(ctx).Where("`key` = ?", key).First(&k).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.New(errs.CodeNotFound, "密钥不存在")
		}
		return nil, err
	}
	return &k, nil
}

func (r *apiKeyRepo) List(ctx context.Context, p pagination.Params) ([]model.APIKey, int64, error) {
	var (
		items []model.APIKey
		total int64
	)
	q := r.db.WithContext(ctx).Model(&model.APIKey{})
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("id ASC").Offset(p.Offset).Limit(p.Limit).Find(&items).Error
	if items == nil {
		items = []model.APIKey{}
	}
	return items, total, err
}
