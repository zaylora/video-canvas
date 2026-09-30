package model

import "time"

// AIChannel 渠道：选一个固定的插件版本 + 上游地址 + 加密的 Key（Key 存在 ai_secrets，名字固定为 channel:<key>）。
// 见 docs/design/协议插件设计.md 6.5。
type AIChannel struct {
	Key              string    `gorm:"primaryKey;size:64" json:"key"`                   // 渠道 key，如 newapi-main
	Name             string    `gorm:"size:128;not null" json:"name"`                   // 显示名
	PluginKey        string    `gorm:"size:30;not null;index" json:"plugin_key"`        // 使用的插件
	PluginVersionID  uint64    `gorm:"not null;index" json:"plugin_version_id"`         // 固定到一个插件版本；升级插件要显式改这里
	BaseURL          string    `gorm:"size:512;not null" json:"base_url"`               // 插件请求只能去这个地址
	TrustedInternal  bool      `gorm:"not null;default:false" json:"trusted_internal"`  // 允许 base_url 解析到内网地址（自建 New API）；只有 super_admin 能开，记审计日志
	AllowCredentials bool      `gorm:"not null;default:false" json:"allow_credentials"` // 插件声明 auth: custom 时必须开启，才会把 Key 放进 ctx.credentials；记审计日志
	SettingsJSON     JSONText  `gorm:"type:json;not null" json:"settings_json"`         // 插件 channelSettings 的取值（对象）
	RateLimitJSON    JSONText  `gorm:"type:json;not null" json:"rate_limit_json"`       // {"rps":5,"max_concurrency":20}，缺省不限
	Enabled          bool      `gorm:"not null;default:true" json:"enabled"`            // 停用后不再接新任务，进行中的任务按快照继续
	UpdatedBy        uint64    `gorm:"not null;default:0" json:"updated_by"`            // 最后修改人
	CreatedAt        time.Time `json:"created_at"`                                      // 创建时间
	UpdatedAt        time.Time `json:"updated_at"`                                      // 更新时间
}

func (AIChannel) TableName() string { return "ai_channels" }

// ChannelSecretName 返回渠道凭证在 ai_secrets 里的名字。
func ChannelSecretName(channelKey string) string { return "channel:" + channelKey }
