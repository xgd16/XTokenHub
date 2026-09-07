// Package model 定义 GORM 数据模型与共享枚举（协议、状态等）。
package model

import (
	"sort"
	"strings"
	"time"
)

// Protocol AI API 协议标准。
type Protocol string

const (
	ProtocolChatCompletions Protocol = "chat_completions" // OpenAI /v1/chat/completions
	ProtocolResponses       Protocol = "responses"        // OpenAI /v1/responses
	ProtocolMessages        Protocol = "messages"         // Anthropic /v1/messages
)

// AllProtocols 三种协议全集。
var AllProtocols = []Protocol{ProtocolChatCompletions, ProtocolResponses, ProtocolMessages}

// Valid 判断协议是否受支持。
func (p Protocol) Valid() bool {
	for _, v := range AllProtocols {
		if v == p {
			return true
		}
	}
	return false
}

// ProviderType 上游厂家类型（决定探测基线与默认适配器）。
type ProviderType string

const (
	ProviderOpenAICompatible ProviderType = "openai_compatible"
	ProviderAnthropic        ProviderType = "anthropic"
)

// Valid 判断厂家类型是否受支持。
func (t ProviderType) Valid() bool {
	return t == ProviderOpenAICompatible || t == ProviderAnthropic
}

// ForwardMode 转发模式。
type ForwardMode string

const (
	ForwardNativePassthrough ForwardMode = "native_passthrough" // 原生透传
	ForwardConverted         ForwardMode = "converted"          // 协议转换
)

// ChannelStatus 渠道状态。
type ChannelStatus int

const (
	ChannelEnabled  ChannelStatus = 1
	ChannelDisabled ChannelStatus = 0
)

// Channel 上游渠道：一个厂家 API 的接入配置。
type Channel struct {
	ID          int64         `gorm:"primaryKey;autoIncrement" json:"id"`
	Name        string        `gorm:"size:128;not null;uniqueIndex" json:"name"`
	Provider    ProviderType  `gorm:"size:32;not null" json:"provider"`
	BaseURL     string        `gorm:"size:512;not null" json:"base_url"`
	APIKey      string        `gorm:"size:256;not null" json:"api_key"`
	Models      string        `gorm:"size:2048;not null;default:''" json:"models"`          // 逗号分隔
	Protocols   string        `gorm:"size:128;not null;default:''" json:"native_protocols"` // 原生支持协议(逗号分隔)，探测结果可手工修正
	Priority    int           `gorm:"not null;default:100;index" json:"priority"`           // 越小越优先
	Weight      int           `gorm:"not null;default:1" json:"weight"`                     // 加权轮询权重
	Status      ChannelStatus `gorm:"not null;default:1;index" json:"status"`
	Remark      string        `gorm:"size:512" json:"remark"`
	LastProbeAt *time.Time    `json:"last_probe_at"`
	ProbeResult string        `gorm:"size:1024" json:"probe_result"` // 最近一次探测详情

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// 渠道为配置型实体，采用硬删除（唯一索引 name 全局生效）；调用结果历史由 RequestLog 快照承担。
}

// ModelList 解析支持的模型列表。
func (ch *Channel) ModelList() []string {
	return splitCSV(ch.Models)
}

// SupportsModel 渠道是否支持该模型（models 为空视为支持全部）。
func (ch *Channel) SupportsModel(name string) bool {
	list := ch.ModelList()
	if len(list) == 0 {
		return true
	}
	for _, m := range list {
		if m == name {
			return true
		}
	}
	return false
}

// NativeProtocols 解析原生支持协议列表。
func (ch *Channel) NativeProtocols() []Protocol {
	var out []Protocol
	for _, s := range splitCSV(ch.Protocols) {
		p := Protocol(s)
		if p.Valid() {
			out = append(out, p)
		}
	}
	return out
}

// IsNative 判断渠道是否原生支持某协议。
func (ch *Channel) IsNative(p Protocol) bool {
	for _, v := range ch.NativeProtocols() {
		if v == p {
			return true
		}
	}
	return false
}

// SetProtocols 规范化写入协议列表（去重、只保留合法值、稳定排序）。
func (ch *Channel) SetProtocols(ps []Protocol) {
	ch.Protocols = joinProtocols(ps)
}

func splitCSV(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// joinProtocols 去重、过滤非法值并按 AllProtocols 稳定排序后拼接。
func joinProtocols(ps []Protocol) string {
	seen := map[Protocol]bool{}
	var out []Protocol
	for _, p := range ps {
		if p.Valid() && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return protocolRank(out[i]) < protocolRank(out[j])
	})
	parts := make([]string, 0, len(out))
	for _, p := range out {
		parts = append(parts, string(p))
	}
	return strings.Join(parts, ",")
}

func protocolRank(p Protocol) int {
	for i, v := range AllProtocols {
		if v == p {
			return i
		}
	}
	return len(AllProtocols)
}
