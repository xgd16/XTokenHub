package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"xtokenhub/internal/model"
)

// HolidayDateRepo 节假日年历数据访问实现。
type HolidayDateRepo struct {
	db *gorm.DB
}

// NewHolidayDateRepository 构造节假日年历仓储。
func NewHolidayDateRepository(db *gorm.DB) HolidayDateRepository {
	return &HolidayDateRepo{db: db}
}

// ReplaceYear 整体替换某年标注（事务内先删该年再写入），行自带 SyncedAt。
func (r *HolidayDateRepo) ReplaceYear(ctx context.Context, year int, rows []model.HolidayDate) (int, error) {
	if year <= 0 {
		return 0, gorm.ErrInvalidValue
	}
	written := 0
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("year = ?", year).Delete(&model.HolidayDate{}).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		for i := range rows {
			rows[i].Year = year
			if rows[i].SyncedAt.IsZero() {
				rows[i].SyncedAt = time.Now()
			}
		}
		if err := tx.Create(&rows).Error; err != nil {
			return err
		}
		written = len(rows)
		return nil
	})
	if err != nil {
		return 0, err
	}
	return written, nil
}

// List 全部标注，按日期升序。
func (r *HolidayDateRepo) List(ctx context.Context) ([]model.HolidayDate, error) {
	var items []model.HolidayDate
	err := r.db.WithContext(ctx).Order("date ASC").Find(&items).Error
	if items == nil {
		items = []model.HolidayDate{}
	}
	return items, err
}

// ListYear 某年标注，按日期升序。
func (r *HolidayDateRepo) ListYear(ctx context.Context, year int) ([]model.HolidayDate, error) {
	var items []model.HolidayDate
	err := r.db.WithContext(ctx).Where("year = ?", year).Order("date ASC").Find(&items).Error
	if items == nil {
		items = []model.HolidayDate{}
	}
	return items, err
}

// Count 标注总行数。
func (r *HolidayDateRepo) Count(ctx context.Context) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.HolidayDate{}).Count(&n).Error
	return n, err
}
