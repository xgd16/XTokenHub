// Package repository 数据访问层：面向接口定义，便于 service 层 mock 与测试。
package repository

import (
	"context"
	"time"

	"xtokenhub/internal/model"
	"xtokenhub/internal/pkg/pagination"
)

// ChannelRepository 渠道数据访问接口。
type ChannelRepository interface {
	Create(ctx context.Context, ch *model.Channel) error
	Update(ctx context.Context, ch *model.Channel) error
	Delete(ctx context.Context, id int64) error
	Get(ctx context.Context, id int64) (*model.Channel, error)
	GetByName(ctx context.Context, name string) (*model.Channel, error)
	List(ctx context.Context, p pagination.Params) ([]model.Channel, int64, error)
	ListAll(ctx context.Context) ([]model.Channel, error)
	ListEnabled(ctx context.Context) ([]model.Channel, error)
}

// CustomModelRepository 自定义模型组数据访问接口。
type CustomModelRepository interface {
	Create(ctx context.Context, cm *model.CustomModel) error
	Update(ctx context.Context, cm *model.CustomModel) error
	Delete(ctx context.Context, id int64) error
	Get(ctx context.Context, id int64) (*model.CustomModel, error)
	GetByName(ctx context.Context, name string) (*model.CustomModel, error)
	List(ctx context.Context, p pagination.Params) ([]model.CustomModel, int64, error)
	ListEnabled(ctx context.Context) ([]model.CustomModel, error)
}

// RequestLogRepository 请求日志数据访问接口。
type RequestLogRepository interface {
	Create(ctx context.Context, log *model.RequestLog) error
	Get(ctx context.Context, id int64) (*model.RequestLog, error)
	List(ctx context.Context, f LogFilter, p pagination.Params) ([]model.RequestLog, int64, error)
	// Stats 聚合统计接口
	Summary(ctx context.Context, since time.Time) (*Summary, error)
	TrendByDay(ctx context.Context, since time.Time) ([]TrendPoint, error)
	TrendByBucket(ctx context.Context, since time.Time, bucketSec int64) ([]TrendPoint, error)
	GroupByModel(ctx context.Context, since time.Time) ([]GroupStat, error)
	GroupByChannel(ctx context.Context, since time.Time) ([]GroupStat, error)
	GroupByKey(ctx context.Context, since time.Time) ([]GroupStat, error)
	// ModelUsage 按模型聚合多时间窗（1h/24h/7d/30d）使用量，now 为窗口基准时刻。
	ModelUsage(ctx context.Context, now time.Time) ([]ModelUsageStat, error)
	// DailyUsage 按日聚合使用量（含成功请求最大耗时），供全历史累计统计与热力图。
	DailyUsage(ctx context.Context, since time.Time) ([]DayUsage, error)
	// TrendByDayModel 按日 × 模型聚合 token 用量，供多模型趋势线。
	TrendByDayModel(ctx context.Context, since time.Time) ([]ModelDayPoint, error)
	// DeleteBefore 分批删除 created_at 早于 cutoff 的日志，单批最多 limit 行，返回实际删除行数。
	// 供保留期清理任务使用；需反复调用直至返回数小于 limit。
	DeleteBefore(ctx context.Context, cutoff time.Time, limit int) (int64, error)
}

// LogFilter 日志筛选条件，零值表示不过滤。
type LogFilter struct {
	Protocol    model.Protocol
	ForwardMode model.ForwardMode
	ChannelID   int64
	KeyID       int64
	Model       string
	Stream      *bool
	StartTime   *time.Time
	EndTime     *time.Time
	ErrorOnly   bool
}

// Summary 汇总统计。
type Summary struct {
	TotalRequests int64   `json:"total_requests"`
	SuccessReqs   int64   `json:"success_requests"`
	ErrorReqs     int64   `json:"error_requests"`
	PromptTokens  int64   `json:"prompt_tokens"`
	CompletionTok int64   `json:"completion_tokens"`
	TotalTokens   int64   `json:"total_tokens"`
	CachedTokens  int64   `json:"cached_tokens"`
	CacheHitRate  float64 `json:"cache_hit_rate"`
	AvgDurationMS float64 `json:"avg_duration_ms"`
	NativeRatio   float64 `json:"native_ratio"` // 原生透传占比
}

// TrendPoint 趋势点。按日查询时 Date 为日期键；分桶查询时 Ts 为桶起点 Unix 秒。
type TrendPoint struct {
	Date        string `json:"date"`
	Ts          int64  `json:"ts,omitempty"`
	Requests    int64  `json:"requests"`
	TotalTokens int64  `json:"total_tokens"`
	ErrorReqs   int64  `json:"error_requests"`
}

// GroupStat 按维度聚合。
type GroupStat struct {
	Name        string  `json:"name"`
	Requests    int64   `json:"requests"`
	TotalTokens int64   `json:"total_tokens"`
	CachedToken int64   `json:"cached_tokens"`
	CacheRate   float64 `json:"cache_rate"`
	AvgMS       float64 `json:"avg_ms"`
}

// ModelWindow 模型在单个时间窗内的使用量。
type ModelWindow struct {
	Requests    int64 `json:"requests"`
	TotalTokens int64 `json:"total_tokens"`
	CachedToken int64 `json:"cached_tokens"`
}

// ModelUsageStat 单个模型的多时间窗使用量。
type ModelUsageStat struct {
	Model string      `json:"model"`
	W1h   ModelWindow `json:"w_1h"`
	W24h  ModelWindow `json:"w_24h"`
	W7d   ModelWindow `json:"w_7d"`
	W30d  ModelWindow `json:"w_30d"`
}

// DayUsage 单日聚合使用量（含成功请求最大耗时）。
type DayUsage struct {
	Date          string `json:"date"` // YYYY-MM-DD
	Requests      int64  `json:"requests"`
	TotalTokens   int64  `json:"total_tokens"`
	MaxDurationMS int64  `json:"max_duration_ms"` // 当日成功请求最大耗时
}

// ModelDayPoint 单日单模型的 token 用量。
type ModelDayPoint struct {
	Date        string `json:"date"` // YYYY-MM-DD
	Model       string `json:"model"`
	TotalTokens int64  `json:"total_tokens"`
}
