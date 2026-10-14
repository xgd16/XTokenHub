package model

import (
	"sync"
	"time"
)

// HolidayKind 节假日日期类型（中国国务院放假安排口径）。
type HolidayKind string

const (
	// HolidayKindNone 普通日期（年历中无特殊标注）。
	HolidayKindNone HolidayKind = ""
	// HolidayKindHoliday 放假（含调休连休的全部放假日，非仅法定节假日当天）。
	HolidayKindHoliday HolidayKind = "holiday"
	// HolidayKindWorkday 调休补班上班日（周末但按工作日上班）。
	HolidayKindWorkday HolidayKind = "workday"
)

// Valid 类型是否合法。
func (k HolidayKind) Valid() bool {
	switch k {
	case HolidayKindNone, HolidayKindHoliday, HolidayKindWorkday:
		return true
	}
	return false
}

// HolidayDate 一条节假日标注。
type HolidayDate struct {
	// Date 北京日期 YYYY-MM-DD。
	Date string `gorm:"size:10;primaryKey" json:"date"`
	// Kind holiday=放假（含调休连休）| workday=调休补班上班。
	Kind HolidayKind `gorm:"size:16;not null" json:"kind"`
	// Name 节日名（如 国庆节、国庆节后补班），仅展示。
	Name string `gorm:"size:64" json:"name"`
	// Year 年份（按年整体替换同步结果用）。
	Year int `gorm:"index" json:"year"`

	SyncedAt time.Time `json:"synced_at"`
}

// TableName 表名。
func (HolidayDate) TableName() string { return "holiday_dates" }

// HolidaySource 按北京日期查询节假日类型。PeakWindow 判定 !H / W 记号时使用。
type HolidaySource interface {
	// HolidayKindOf day 为 YYYY-MM-DD（北京日期）。
	HolidayKindOf(day string) HolidayKind
}

var (
	holidayMu     sync.RWMutex
	holidaySource HolidaySource
)

// SetHolidaySource 注册全局节假日数据源（服务启动时注入同步服务；测试注入假表）。
func SetHolidaySource(s HolidaySource) {
	holidayMu.Lock()
	holidaySource = s
	holidayMu.Unlock()
}

// HolidayKindOf 查询全局数据源；未注册或无标注时返回 HolidayKindNone。
func HolidayKindOf(day string) HolidayKind {
	holidayMu.RLock()
	s := holidaySource
	holidayMu.RUnlock()
	if s == nil {
		return HolidayKindNone
	}
	return s.HolidayKindOf(day)
}
