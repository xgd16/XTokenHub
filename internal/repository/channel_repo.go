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

// channelRepo ChannelRepository 的 GORM 实现。
type channelRepo struct {
	db *gorm.DB
}

// NewChannelRepository 构造渠道仓储。
func NewChannelRepository(db *gorm.DB) ChannelRepository {
	return &channelRepo{db: db}
}

func (r *channelRepo) Create(ctx context.Context, ch *model.Channel) error {
	return r.db.WithContext(ctx).Create(ch).Error
}

func (r *channelRepo) Update(ctx context.Context, ch *model.Channel) error {
	res := r.db.WithContext(ctx).Model(ch).Select("*").Updates(ch)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errs.New(errs.CodeNotFound, "渠道不存在")
	}
	return nil
}

func (r *channelRepo) Delete(ctx context.Context, id int64) error {
	res := r.db.WithContext(ctx).Delete(&model.Channel{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errs.New(errs.CodeNotFound, "渠道不存在")
	}
	return nil
}

func (r *channelRepo) Get(ctx context.Context, id int64) (*model.Channel, error) {
	var ch model.Channel
	if err := r.db.WithContext(ctx).First(&ch, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.New(errs.CodeNotFound, "渠道不存在")
		}
		return nil, err
	}
	return &ch, nil
}

func (r *channelRepo) GetByName(ctx context.Context, name string) (*model.Channel, error) {
	var ch model.Channel
	if err := r.db.WithContext(ctx).Where("name = ?", name).First(&ch).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errs.New(errs.CodeNotFound, "渠道不存在")
		}
		return nil, err
	}
	return &ch, nil
}

func (r *channelRepo) List(ctx context.Context, p pagination.Params) ([]model.Channel, int64, error) {
	var (
		items []model.Channel
		total int64
	)
	q := r.db.WithContext(ctx).Model(&model.Channel{})
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count channels: %w", err)
	}
	err := q.Order("priority ASC, id ASC").Offset(p.Offset).Limit(p.Limit).Find(&items).Error
	// 空 slice 统一返回 []，避免 JSON 序列化成 null 破坏前端数组约定
	if items == nil {
		items = []model.Channel{}
	}
	return items, total, err
}

// ListAll 全量渠道（不分页，含停用），按 priority、id 排序。
func (r *channelRepo) ListAll(ctx context.Context) ([]model.Channel, error) {
	items := make([]model.Channel, 0, 8)
	err := r.db.WithContext(ctx).Order("priority ASC, id ASC").Find(&items).Error
	return items, err
}

func (r *channelRepo) ListEnabled(ctx context.Context) ([]model.Channel, error) {
	items := make([]model.Channel, 0, 8)
	err := r.db.WithContext(ctx).
		Where("status = ?", model.ChannelEnabled).
		Order("priority ASC, id ASC").
		Find(&items).Error
	return items, err
}
