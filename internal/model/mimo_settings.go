package model

import "time"

// MimoSettings MiMo 网页余额凭证（单行表，ID 固定为 1）。
// 官方未开放余额 API，这里存网页控制台的会话 Cookie（最小集为 userId +
// api-platform_serviceToken），由渠道余额查询按 Cookie 鉴权重放平台接口。
// 会话由小米 passport 服务端管理，过期后需回网页重新复制。
type MimoSettings struct {
	ID int64 `gorm:"primaryKey" json:"id"`
	// Cookie 网页控制台的完整 Cookie 串（原样存储、原样转发）。与渠道 api_key
	// 同一安全姿态：单用户自托管，明文存储并在设置页回显。
	Cookie string `gorm:"type:text" json:"cookie"`

	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 指定表名。
func (MimoSettings) TableName() string {
	return "mimo_settings"
}
