package model

import "time"

// 清理触发模式。
const (
	// CleanupModeDaily 每天固定时刻执行（本地时区）。
	CleanupModeDaily = "daily"
	// CleanupModeInterval 固定间隔执行。
	CleanupModeInterval = "interval"
)

// DataSettings 数据管理设置与运行状态（单行表，ID 固定为 1）。
// 配置部分由设置页维护；聚合/清理状态由后台任务写回，重启不丢。
type DataSettings struct {
	ID int64 `gorm:"primaryKey" json:"id"`

	// ---- 清理配置（设置页可改） ----

	// CleanupEnabled 是否启用自动清理（关闭后仍可手动触发）。
	CleanupEnabled bool `json:"cleanup_enabled"`
	// CleanupMode 触发模式：daily | interval。
	CleanupMode string `gorm:"size:8;not null;default:daily" json:"cleanup_mode"`
	// CleanupRunHour daily 模式的每日执行时刻（0-23，本地时区；错过时刻顺延补跑）。
	CleanupRunHour int `json:"cleanup_run_hour"`
	// CleanupIntervalHours interval 模式的执行间隔（小时）。
	CleanupIntervalHours int `json:"cleanup_interval_hours"`
	// CleanupMaxDays 保留最近 N 天日志，超期行自动删除。累计统计读聚合表，不受影响。
	CleanupMaxDays int `json:"cleanup_max_days"`
	// CleanupBatchSize 单批删除行数，分批执行避免长事务。
	CleanupBatchSize int `json:"cleanup_batch_size"`
	// CleanupVacuum 清理生效后执行 VACUUM 回收磁盘空间（独占锁，建议低峰开启）。
	CleanupVacuum bool `json:"cleanup_vacuum"`
	// TrimHeaders 请求头瘦身：把超过 TrimHeadersAfterDays 天的 request_headers 置空。
	// 请求头单行可达 4KB，是日志表体积大头；只有日志详情页会读它。
	TrimHeaders bool `json:"trim_headers"`
	// TrimHeadersAfterDays 请求头保留天数（置空更早的行），应小于 CleanupMaxDays。
	TrimHeadersAfterDays int `json:"trim_headers_after_days"`

	// ---- 聚合状态（聚合任务维护） ----

	// AggWatermarkDay 已封存到的本地日桶号（含）：该日及更早的聚合行不再变动。
	AggWatermarkDay int64 `json:"agg_watermark_day"`
	// AggLastRunAt 聚合任务最近一次执行时间。
	AggLastRunAt time.Time `json:"agg_last_run_at"`

	// ---- 清理状态（清理任务维护） ----

	// LastCleanupAt 最近一次清理完成时间。
	LastCleanupAt time.Time `json:"last_cleanup_at"`
	// LastCleanupDay 最近一次清理所在的本地日桶号（daily 模式每天至多一次的判定依据）。
	LastCleanupDay int64 `json:"last_cleanup_day"`
	// LastDeletedRows 最近一次清理删除的日志行数。
	LastDeletedRows int64 `json:"last_deleted_rows"`
	// LastTrimmedRows 最近一次清理置空的请求头行数。
	LastTrimmedRows int64 `json:"last_trimmed_rows"`
	// LastDurationMS 最近一次清理耗时（毫秒，含瘦身）。
	LastDurationMS int64 `json:"last_duration_ms"`
	// LastError 最近一次清理的错误信息（空 = 成功）。
	LastError string `gorm:"size:512" json:"last_error"`

	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 指定表名。
func (DataSettings) TableName() string {
	return "data_settings"
}
