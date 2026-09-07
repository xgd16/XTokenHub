package model

import "time"

// KeyStatus 密钥状态。
type KeyStatus int

const (
	KeyEnabled  KeyStatus = 1
	KeyDisabled KeyStatus = 0
)

// APIKey 网关调用密钥：标识调用方（agent / 脚本），用于网关鉴权与按调用方统计。
// 自托管场景按明文存储（与渠道 api_key 一致）；key 创建后不可修改。
type APIKey struct {
	ID     int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Name   string    `gorm:"size:128;not null;uniqueIndex" json:"name"`
	Key    string    `gorm:"size:128;not null;uniqueIndex" json:"key"`
	Status KeyStatus `gorm:"not null;default:1;index" json:"status"`
	Remark string    `gorm:"size:512" json:"remark"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// KeyPrefix 网关密钥前缀，便于与上游厂家 key 区分。
const KeyPrefix = "sk-xt-"
