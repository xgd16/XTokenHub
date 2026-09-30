package repository

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"xtokenhub/internal/model"
	"xtokenhub/internal/pkg/errs"
)

// ReasoningRewriteRepo 思维档位改写规则数据访问实现。
type ReasoningRewriteRepo struct {
	db *gorm.DB
}

// NewReasoningRewriteRepository 构造思维档位改写规则仓储。
func NewReasoningRewriteRepository(db *gorm.DB) ReasoningRewriteRepository {
	return &ReasoningRewriteRepo{db: db}
}

func (r *ReasoningRewriteRepo) Create(ctx context.Context, rule *model.ReasoningRewrite) error {
	return r.db.WithContext(ctx).Create(rule).Error
}

func (r *ReasoningRewriteRepo) Update(ctx context.Context, rule *model.ReasoningRewrite) error {
	return r.db.WithContext(ctx).Save(rule).Error
}

func (r *ReasoningRewriteRepo) Delete(ctx context.Context, id int64) error {
	return r.db.WithContext(ctx).Delete(&model.ReasoningRewrite{}, id).Error
}

func (r *ReasoningRewriteRepo) GetByID(ctx context.Context, id int64) (*model.ReasoningRewrite, error) {
	var rule model.ReasoningRewrite
	if err := r.db.WithContext(ctx).First(&rule, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.New(errs.CodeNotFound, "改写规则不存在")
		}
		return nil, err
	}
	return &rule, nil
}

// GetByModel 按上游模型名精确查询，不存在返回 gorm.ErrRecordNotFound。
func (r *ReasoningRewriteRepo) GetByModel(ctx context.Context, name string) (*model.ReasoningRewrite, error) {
	var rule model.ReasoningRewrite
	if err := r.db.WithContext(ctx).Where("model = ?", name).First(&rule).Error; err != nil {
		return nil, err
	}
	return &rule, nil
}

func (r *ReasoningRewriteRepo) List(ctx context.Context) ([]model.ReasoningRewrite, error) {
	var items []model.ReasoningRewrite
	err := r.db.WithContext(ctx).Order("model ASC").Find(&items).Error
	if items == nil {
		items = []model.ReasoningRewrite{}
	}
	return items, err
}

// ListPaged 分页查询，q 为模型名模糊匹配（空 = 不限）。
func (r *ReasoningRewriteRepo) ListPaged(ctx context.Context, q string, offset, limit int) ([]model.ReasoningRewrite, int64, error) {
	query := r.db.WithContext(ctx).Model(&model.ReasoningRewrite{})
	if s := strings.TrimSpace(q); s != "" {
		query = query.Where(`model LIKE ? ESCAPE '\'`, likePattern(s))
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []model.ReasoningRewrite
	err := query.Order("model ASC").Offset(offset).Limit(limit).Find(&items).Error
	if items == nil {
		items = []model.ReasoningRewrite{}
	}
	return items, total, err
}
