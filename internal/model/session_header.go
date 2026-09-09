package model

import "time"

// SessionHeaderConfig 会话标识配置：可识别的 HTTP Header 中的 session 数据 key。
type SessionHeaderConfig struct {
	ID          int64     `gorm:"primaryKey" json:"id"`
	Key         string    `gorm:"size:128;uniqueIndex" json:"key"`         // 配置键（小写，用于唯一标识）
	HeaderName  string    `gorm:"size:256" json:"header_name"`             // HTTP Header 名称（如 X-Session-Id）
	Description string    `gorm:"size:512" json:"description"`             // 配置描述
	Enabled     bool      `gorm:"default:true" json:"enabled"`             // 是否启用
	CreatedAt   time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 指定表名。
func (SessionHeaderConfig) TableName() string {
	return "session_header_configs"
}
