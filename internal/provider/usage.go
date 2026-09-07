package provider

import (
	"encoding/json"
	"strings"

	"xtokenhub/internal/model"
)

// UsageSource usage 数据来源。
type UsageSource string

const (
	UsageFromUpstream UsageSource = "upstream" // 上游响应报告
	UsageFromEstimate UsageSource = "estimate" // 本地估算兜底
)

// Usage 归一化的 token 用量（三种协议统一字段）。
type Usage struct {
	PromptTokens     int64
	CompletionTokens int64
	TotalTokens      int64
	CachedTokens     int64 // 命中缓存的输入 token
	CacheWriteTokens int64 // 缓存写入 token
	Source           UsageSource
}

// Normalize 补全 total（上游缺省时 prompt+completion）。
func (u Usage) Normalize() Usage {
	if u.TotalTokens == 0 {
		u.TotalTokens = u.PromptTokens + u.CompletionTokens
	}
	return u
}

// ---------- 非流式：按上游协议解析响应体 ----------

// upstreamUsage 各协议原始 usage 的宽松载体。
type upstreamUsage struct {
	// openai chat/completions
	PromptTokens     *int64 `json:"prompt_tokens"`
	CompletionTokens *int64 `json:"completion_tokens"`
	TotalTokens      *int64 `json:"total_tokens"`
	PromptDetails    *struct {
		CachedTokens *int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`

	// openai responses / anthropic messages
	InputTokens  *int64 `json:"input_tokens"`
	OutputTokens *int64 `json:"output_tokens"`
	InputDetails *struct {
		CachedTokens *int64 `json:"cached_tokens"`
	} `json:"input_tokens_details"`

	// anthropic
	CacheCreation *int64 `json:"cache_creation_input_tokens"`
	CacheRead     *int64 `json:"cache_read_input_tokens"`
}

func (u *upstreamUsage) toUsage() Usage {
	var usage Usage
	usage.Source = UsageFromUpstream
	switch {
	case u.PromptTokens != nil || u.CompletionTokens != nil: // openai chat
		usage.PromptTokens = deref(u.PromptTokens)
		usage.CompletionTokens = deref(u.CompletionTokens)
		usage.TotalTokens = deref(u.TotalTokens)
		if u.PromptDetails != nil {
			usage.CachedTokens = deref(u.PromptDetails.CachedTokens)
		}
	case u.InputTokens != nil || u.OutputTokens != nil: // responses / messages
		usage.PromptTokens = deref(u.InputTokens)
		usage.CompletionTokens = deref(u.OutputTokens)
		usage.TotalTokens = deref(u.TotalTokens)
		if u.InputDetails != nil {
			usage.CachedTokens = deref(u.InputDetails.CachedTokens)
		}
		if u.CacheRead != nil {
			usage.CachedTokens = deref(u.CacheRead)
		}
		if u.CacheCreation != nil {
			usage.CacheWriteTokens = deref(u.CacheCreation)
		}
	}
	return usage.Normalize()
}

func deref(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// ParseUsage 从上游响应体中提取 usage（非流式）。
func ParseUsage(upstreamProto model.Protocol, body []byte) (Usage, bool) {
	var probe struct {
		Usage *upstreamUsage `json:"usage"`
	}
	if err := json.Unmarshal(body, &probe); err != nil || probe.Usage == nil {
		return Usage{}, false
	}
	u := probe.Usage.toUsage()
	return u, u.PromptTokens > 0 || u.CompletionTokens > 0 || u.CachedTokens > 0 || u.CacheWriteTokens > 0
}

// ExtractErrorDetail 从上游错误响应中提取可读信息。
func ExtractErrorDetail(body []byte) string {
	var e struct {
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(body, &e); err != nil || len(e.Error) == 0 {
		if len(body) > 512 {
			return string(body[:512])
		}
		return string(body)
	}
	var detail struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(e.Error, &detail); err == nil && detail.Message != "" {
		return detail.Message
	}
	return string(e.Error)
}

// ---------- 流式：逐事件累积 ----------

// StreamUsageParser 流式统计旁路解析器：逐个喂入上游 SSE data 载荷，
// 结束后 Result() 给出 usage（若上游报告了）与全部 delta 文本（供估算兜底）。
type StreamUsageParser interface {
	// Feed 输入一条 SSE data 载荷（不含 "data: " 前缀）；"[DONE]" 由调用方过滤。
	Feed(data []byte) error
	// Usage 若上游已报告 usage 则返回 true。
	Usage() (Usage, bool)
	// Text 累积的输出文本（估算兜底用）。
	Text() string
}

// NewStreamParser 按上游协议构造流式解析器。
func NewStreamParser(upstreamProto model.Protocol) StreamUsageParser {
	switch upstreamProto {
	case model.ProtocolMessages:
		return &anthropicStreamParser{}
	default:
		return &openaiStreamParser{}
	}
}

// openaiStreamParser 解析 chat/completions 流式 chunk（含 include_usage 尾块）
// 与 responses 流式事件。
type openaiStreamParser struct {
	usage    Usage
	hasUsage bool
	text     strings.Builder
}

func (p *openaiStreamParser) Feed(data []byte) error {
	var chunk struct {
		Usage   *upstreamUsage `json:"usage"`
		Type    string         `json:"type"` // responses 事件类型
		Delta   string         `json:"delta"`
		Choices []struct {
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
		} `json:"choices"`
		Response *struct {
			Usage *upstreamUsage `json:"usage"`
		} `json:"response"`
	}
	if err := json.Unmarshal(data, &chunk); err != nil {
		return nil // 非法块直接跳过，不影响转发
	}
	// chat/completions: choices[].delta.content
	for _, ch := range chunk.Choices {
		p.text.WriteString(ch.Delta.Content)
	}
	// responses: response.output_text.delta
	if chunk.Type == "response.output_text.delta" {
		p.text.WriteString(chunk.Delta)
	}
	// responses: response.completed 带 response.usage
	if chunk.Response != nil && chunk.Response.Usage != nil {
		p.usage = chunk.Response.Usage.toUsage()
		p.hasUsage = true
	}
	// chat/completions: 尾块 usage（include_usage）
	if chunk.Usage != nil {
		p.usage = chunk.Usage.toUsage()
		p.hasUsage = true
	}
	return nil
}

func (p *openaiStreamParser) Usage() (Usage, bool) { return p.usage, p.hasUsage }
func (p *openaiStreamParser) Text() string         { return p.text.String() }

// anthropicStreamParser 解析 messages 流式事件：
// message_start(input_tokens/cache_read) / content_block_delta(text) / message_delta(output_tokens)。
type anthropicStreamParser struct {
	usage    Usage
	hasUsage bool
	text     strings.Builder
}

func (p *anthropicStreamParser) Feed(data []byte) error {
	var ev struct {
		Type  string `json:"type"`
		Delta struct {
			Text string `json:"text"`
		} `json:"delta"`
		Message *struct {
			Usage *upstreamUsage `json:"usage"`
		} `json:"message"`
		Usage *upstreamUsage `json:"usage"`
	}
	if err := json.Unmarshal(data, &ev); err != nil {
		return nil
	}
	switch ev.Type {
	case "content_block_delta":
		p.text.WriteString(ev.Delta.Text)
	case "message_start":
		if ev.Message != nil && ev.Message.Usage != nil {
			u := ev.Message.Usage.toUsage()
			p.usage.PromptTokens = u.PromptTokens
			p.usage.CachedTokens = u.CachedTokens
			p.usage.CacheWriteTokens = u.CacheWriteTokens
			p.hasUsage = true
		}
	case "message_delta":
		if ev.Usage != nil {
			u := ev.Usage.toUsage()
			p.usage.CompletionTokens = u.CompletionTokens
			p.usage.TotalTokens = p.usage.PromptTokens + u.CompletionTokens
			p.hasUsage = true
		}
	}
	return nil
}

func (p *anthropicStreamParser) Usage() (Usage, bool) {
	if p.hasUsage {
		p.usage.Source = UsageFromUpstream
	}
	u := p.usage.Normalize()
	return u, p.hasUsage
}
func (p *anthropicStreamParser) Text() string { return p.text.String() }
