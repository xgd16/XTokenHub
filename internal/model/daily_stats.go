package model

import "time"

// DailyUsageStat 日级使用量聚合（按本地日一行，后台聚合任务从 request_logs 汇总）。
// 行一旦封存不再变动：保留期清理明细后，累计统计仍以本表为准，不再缩水。
type DailyUsageStat struct {
	ID int64 `gorm:"primaryKey" json:"id"`
	// Day 本地日桶号（与 repository.dayBucketSQL 同口径：(Unix 秒 + 本地偏移) / 86400）。
	Day int64 `gorm:"uniqueIndex;not null" json:"day"`
	// Date 本地日期 'YYYY-MM-DD'，直接用于展示与日期比较。
	Date string `gorm:"size:10;not null" json:"date"`
	// Requests 当日全部请求（含失败）。
	Requests int64 `json:"requests"`
	// ErrorRequests 当日失败请求数。
	ErrorRequests int64 `json:"error_requests"`
	PromptTokens  int64 `json:"prompt_tokens"`
	// CompletionTokens 成功请求的补全 token（与明细口径一致，失败请求计 0）。
	CompletionTokens int64 `json:"completion_tokens"`
	// CachedTokens 缓存命中 token（全量求和，与 TrendByDay 口径一致）。
	CachedTokens int64 `json:"cached_tokens"`
	// CostUSD 当日费用合计（USD，未定价与失败请求为 0）。
	CostUSD float64 `json:"cost_usd"`
	// MaxDurationMS 当日成功请求最大耗时。
	MaxDurationMS int64     `json:"max_duration_ms"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// TableName 指定表名。
func (DailyUsageStat) TableName() string {
	return "daily_usage_stats"
}

// DailyModelStat 日 × 模型使用量聚合（供按模型趋势线与模型的 7d/30d 窗口）。
type DailyModelStat struct {
	ID   int64  `gorm:"primaryKey" json:"id"`
	Day  int64  `gorm:"not null;uniqueIndex:idx_daily_model_day_model,priority:1" json:"day"`
	Date string `gorm:"size:10;not null" json:"date"`
	// Model 发往上游的真实模型名（与 request_logs.model 同口径）。
	Model            string    `gorm:"size:255;not null;uniqueIndex:idx_daily_model_day_model,priority:2" json:"model"`
	Requests         int64     `json:"requests"`
	PromptTokens     int64     `json:"prompt_tokens"`
	CompletionTokens int64     `json:"completion_tokens"`
	CachedTokens     int64     `json:"cached_tokens"`
	CostUSD          float64   `json:"cost_usd"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// TableName 指定表名。
func (DailyModelStat) TableName() string {
	return "daily_model_stats"
}
