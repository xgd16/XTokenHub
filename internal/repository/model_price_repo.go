package repository

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"xtokenhub/internal/model"
)

// ModelPriceRepo 模型价格数据访问实现。
type ModelPriceRepo struct {
	db *gorm.DB
}

// NewModelPriceRepository 构造模型价格仓储。
func NewModelPriceRepository(db *gorm.DB) ModelPriceRepository {
	return &ModelPriceRepo{db: db}
}

func (r *ModelPriceRepo) Create(ctx context.Context, p *model.ModelPrice) error {
	if p.Source == "" {
		p.Source = model.PriceFromManual
	}
	return r.db.WithContext(ctx).Create(p).Error
}

func (r *ModelPriceRepo) Update(ctx context.Context, p *model.ModelPrice) error {
	return r.db.WithContext(ctx).Save(p).Error
}

func (r *ModelPriceRepo) Delete(ctx context.Context, id int64) error {
	return r.db.WithContext(ctx).Delete(&model.ModelPrice{}, id).Error
}

func (r *ModelPriceRepo) GetByID(ctx context.Context, id int64) (*model.ModelPrice, error) {
	var p model.ModelPrice
	if err := r.db.WithContext(ctx).First(&p, id).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *ModelPriceRepo) GetByModel(ctx context.Context, name string) (*model.ModelPrice, error) {
	var p model.ModelPrice
	if err := r.db.WithContext(ctx).Where("model = ?", name).First(&p).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *ModelPriceRepo) List(ctx context.Context) ([]model.ModelPrice, error) {
	var items []model.ModelPrice
	err := r.db.WithContext(ctx).Order("model ASC").Find(&items).Error
	if items == nil {
		items = []model.ModelPrice{}
	}
	return items, err
}

// ListPaged 分页查询。usedOnly 时只返回 request_logs 里出现过的模型，
// 避免把整张上游价格表（数千条）全量推给前端。
func (r *ModelPriceRepo) ListPaged(ctx context.Context, q string, usedOnly bool, offset, limit int) ([]model.ModelPrice, int64, error) {
	query := r.db.WithContext(ctx).Model(&model.ModelPrice{})
	if s := strings.TrimSpace(q); s != "" {
		query = query.Where(`model LIKE ? ESCAPE '\'`, likePattern(s))
	}
	if usedOnly {
		query = query.Where("model IN (?)",
			r.db.Model(&model.RequestLog{}).Select("DISTINCT model"))
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []model.ModelPrice
	err := query.Order("model ASC").Offset(offset).Limit(limit).Find(&items).Error
	if items == nil {
		items = []model.ModelPrice{}
	}
	return items, total, err
}

// ReplaceSynced 整体替换同步来源的价格：事务内先清掉非手工行，再批量写入。
// 同名手工配置行不会被同步覆盖，返回跳过的行数供调用方记日志。
func (r *ModelPriceRepo) ReplaceSynced(ctx context.Context, prices []model.ModelPrice) (int, int, error) {
	if len(prices) == 0 {
		return 0, 0, nil
	}
	var written, skipped int
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var manuals []string
		if err := tx.Model(&model.ModelPrice{}).
			Where("source = ?", model.PriceFromManual).
			Pluck("model", &manuals).Error; err != nil {
			return err
		}
		manualSet := make(map[string]bool, len(manuals))
		for _, m := range manuals {
			manualSet[m] = true
		}

		// source 为空串（迁移前的历史行）与 synced 一并重建
		if err := tx.Where("source IS NULL OR source <> ?", model.PriceFromManual).
			Delete(&model.ModelPrice{}).Error; err != nil {
			return err
		}

		batch := make([]model.ModelPrice, 0, len(prices))
		for _, p := range prices {
			if manualSet[p.Model] {
				skipped++
				continue
			}
			batch = append(batch, p)
		}
		if len(batch) > 0 {
			// 小批次写入：单条 INSERT 的绑定参数数量需低于 SQLite 变量上限
			if err := tx.CreateInBatches(batch, 50).Error; err != nil {
				return err
			}
		}
		written = len(batch)
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	return written, skipped, nil
}

func (r *ModelPriceRepo) Count(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.ModelPrice{}).Count(&n).Error
	return n, err
}

// likePattern 构造 LIKE 模式并转义通配符，避免用户输入 % / _ 变成通配。
func likePattern(s string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + replacer.Replace(s) + "%"
}
