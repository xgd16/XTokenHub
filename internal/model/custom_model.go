package model

import (
	"encoding/json"
	"strings"
	"time"
)

// ModelMember 自定义模型组的成员：真实上游模型名 + 组内路由优先级（越小越优先）。
type ModelMember struct {
	Model    string `json:"model"`
	Priority int    `json:"priority,omitempty"`
}

// CustomModel 自定义模型 ID：把一组相似的真实模型聚合到同一个对外模型名下，
// 网关按「成员优先级 -> 渠道优先级 -> 原生透传优先」自动路由并逐候选 failover。
type CustomModel struct {
	ID     int64  `gorm:"primaryKey;autoIncrement" json:"id"`
	Name   string `gorm:"size:128;not null;uniqueIndex" json:"name"` // 对外模型 ID，如 free-1M
	Members string `gorm:"size:4096;not null;default:''" json:"-"`   // 成员 JSON 数组（经 MemberList/SetMembers 读写）
	Status ChannelStatus `gorm:"not null;default:1;index" json:"status"`
	Remark string        `gorm:"size:512" json:"remark"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// 分组为配置型实体，硬删除；调用历史由 RequestLog 快照承担（记录实际使用的成员模型）。
}

// MemberList 解析成员列表（空或非法 JSON 返回空切片）。
func (cm *CustomModel) MemberList() []ModelMember {
	var out []ModelMember
	if cm.Members == "" {
		return out
	}
	_ = json.Unmarshal([]byte(cm.Members), &out)
	return out
}

// SetMembers 规范化写入成员：去空白、去空项、按模型名去重（保留先出现者优先级）。
func (cm *CustomModel) SetMembers(ms []ModelMember) {
	seen := map[string]bool{}
	clean := make([]ModelMember, 0, len(ms))
	for _, m := range ms {
		m.Model = strings.TrimSpace(m.Model)
		if m.Model == "" || seen[m.Model] {
			continue
		}
		seen[m.Model] = true
		clean = append(clean, m)
	}
	b, err := json.Marshal(clean)
	if err != nil {
		cm.Members = ""
		return
	}
	cm.Members = string(b)
}

// SupportsModel 判断该分组是否包含某模型名（即自身 Name 与成员模型均不匹配该名时为 false）。
func (cm *CustomModel) SupportsModel(name string) bool {
	if cm.Name == name {
		return true
	}
	for _, m := range cm.MemberList() {
		if m.Model == name {
			return true
		}
	}
	return false
}
