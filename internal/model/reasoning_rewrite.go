package model

import (
	"strings"
	"time"
)

// 思维开关目标值：MiMo 类上游只认 thinking: {"type": ...} 的 enabled/disabled。
const (
	ReasoningOn  = "enabled"
	ReasoningOff = "disabled"
)

// ReasoningMapping 单个档位映射：客户端档位 -> thinking 开关。
type ReasoningMapping struct {
	Level  string `json:"level"`  // 客户端档位（存储前归一化为小写）
	Target string `json:"target"` // enabled | disabled
}

// ReasoningMappingList 档位映射列表（GORM 以 JSON 文本序列化存储，与线上库兼容）。
type ReasoningMappingList []ReasoningMapping

// ReasoningRewrite 单个上游模型的思维档位改写规则。
//
// 客户端发来的档位（chat 的 reasoning_effort、responses 的 reasoning.effort、
// messages 的 thinking.type 伪档位）按 Mapping 翻译成 thinking: {"type": ...}；
// 未出现在 Mapping 里的档位保留原样透传；客户端未带档位时按 Missing 注入
// （空 = 不注入）。Status=0 时整条规则停用（出站体不做任何改写）。
type ReasoningRewrite struct {
	ID      int64                `gorm:"primaryKey;autoIncrement" json:"id"`
	Model   string               `gorm:"size:128;uniqueIndex;not null" json:"model"`
	Mapping ReasoningMappingList `gorm:"type:text;serializer:json" json:"mapping"`
	// Missing 客户端未带任何档位时的缺省注入开关（enabled/disabled，空 = 不注入）。
	Missing   string    `gorm:"size:12" json:"missing"`
	Status    int       `gorm:"not null;default:1" json:"status"` // 1 启用 0 停用
	Remark    string    `gorm:"size:512" json:"remark"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 指定表名（与线上已部署库同名同列，AutoMigrate 零改动承接存量数据）。
func (ReasoningRewrite) TableName() string {
	return "reasoning_rewrites"
}

// TargetFor 返回档位的改写目标（未命中返回空串，不改写）。
func (r ReasoningRewrite) TargetFor(tier string) string {
	tier = NormalizeTier(tier)
	if tier == "" {
		return ""
	}
	for _, m := range r.Mapping {
		if NormalizeTier(m.Level) == tier {
			return m.Target
		}
	}
	return ""
}

// reasoningTiers 可配置映射的档位集合（归一化后的合法档位名）。
var reasoningTiers = map[string]bool{
	"none": true, "minimal": true, "low": true, "medium": true,
	"high": true, "xhigh": true, "max": true,
}

// NormalizeTier 档位归一化（小写、去首尾空白）；未知串原样返回供上层保留透传。
func NormalizeTier(tier string) string {
	return strings.ToLower(strings.TrimSpace(tier))
}

// IsReasoningTier 是否为可配置映射的档位名。
func IsReasoningTier(tier string) bool {
	return reasoningTiers[NormalizeTier(tier)]
}

// IsReasoningSwitch 是否为合法的 thinking 开关值。
func IsReasoningSwitch(v string) bool {
	return v == ReasoningOn || v == ReasoningOff
}
