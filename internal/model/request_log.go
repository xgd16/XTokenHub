package model

import "time"

// RequestLog 网关请求日志：每次上游调用的计量与结果。
type RequestLog struct {
	ID          int64       `gorm:"primaryKey;autoIncrement" json:"id"`
	CreatedAt   time.Time   `gorm:"index" json:"created_at"`
	Protocol    Protocol    `gorm:"size:32;not null;index" json:"protocol"`     // 入站协议
	ForwardMode ForwardMode `gorm:"size:32;not null;index" json:"forward_mode"` // 透传/转换
	ChannelID   int64       `gorm:"index" json:"channel_id"`
	ChannelName string      `gorm:"size:128" json:"channel_name"`
	KeyID       int64       `gorm:"index" json:"key_id"` // 调用方密钥（0 = 匿名/未启用鉴权）
	KeyName     string      `gorm:"size:128" json:"key_name"`
	Model       string      `gorm:"size:128;not null;index" json:"model"`
	Stream      bool        `json:"stream"`

	// Committed 内部标记：响应已开始写往客户端（不落库、不序列化）。
	Committed bool `gorm:"-" json:"-"`

	// ReqID 进程内请求序号：进行中/完成事件配对用（不落库）。
	ReqID int64 `gorm:"-" json:"req_id"`

	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	TotalTokens      int64   `json:"total_tokens"`
	CachedTokens     int64   `json:"cached_tokens"`      // 命中缓存的输入 token
	CacheWriteTokens int64   `json:"cache_write_tokens"` // 缓存写入 token
	CacheHitRate     float64 `json:"cache_hit_rate"`     // cached/prompt，0~1

	// CostUSD 本请求费用（USD）：网关在落库前按价格表与上游 usage 口径计算。
	// 未定价模型与失败请求为 0。
	CostUSD float64 `gorm:"index" json:"cost_usd"`
	// UsageStyle 计价所用的上游 usage 口径（openai | anthropic），供审计与费用重算。
	UsageStyle UsageStyle `gorm:"size:16" json:"usage_style"`
	// PricePeriod 计价命中的时段（peak | off_peak）；模型未配置时段价时为空。
	// 与 UsageStyle 同构：留下「这笔为何更便宜」的审计痕迹。
	PricePeriod PricePeriod `gorm:"size:16" json:"price_period"`

	DurationMS     int64  `json:"duration_ms"`
	UpstreamStatus int    `json:"upstream_status"`
	ClientIP       string `gorm:"size:64" json:"client_ip"`
	UserAgent      string `gorm:"size:256" json:"user_agent"` // 调用方 User-Agent，识别 agent 工具
	SessionID      string `gorm:"size:128;index" json:"session_id"` // 调用方会话标识（X-Session-Id 头），控制台按会话聚合展示
	RequestHeaders string `gorm:"size:4096" json:"request_headers"` // JSON 编码的请求头
	Error          string `gorm:"size:1024" json:"error"`
}

// UsageStyle 上游 usage 的计费口径：决定输入侧 token 如何拆分，直接影响费用。
type UsageStyle string

const (
	// UsageStyleOpenAI OpenAI 系（chat/completions、responses）：prompt_tokens 已含缓存命中，
	// 未命中部分为 prompt - cached；无缓存写计费。
	UsageStyleOpenAI UsageStyle = "openai"
	// UsageStyleAnthropic Anthropic messages：input_tokens 不含缓存读写，
	// cached 与 cache_write 均需单独计费。
	UsageStyleAnthropic UsageStyle = "anthropic"
)

// UsageStyleOf 上游协议对应的计费口径。
func UsageStyleOf(p Protocol) UsageStyle {
	if p == ProtocolMessages {
		return UsageStyleAnthropic
	}
	return UsageStyleOpenAI
}
