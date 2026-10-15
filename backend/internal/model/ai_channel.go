package model

import "time"

// AIChannel 渠道：只管“连到哪里、用什么凭证、一次最多发多少”——上游地址、自定义请求头、限流、顺序与启停。
// 协议插件、插件版本与协议设置项在模型的 channels[0].protocol 里（见 docs/design/渠道模型重构/渠道模型重构.md）。
// Key 与 Secret Key 存在 ai_secrets，名字固定为 channel:<key> 与 channel:<key>:secret。
type AIChannel struct {
	Key           string    `gorm:"primaryKey;size:64" json:"key"`             // 渠道 key，如 newapi-main
	Name          string    `gorm:"size:128;not null" json:"name"`             // 显示名
	BaseURL       string    `gorm:"size:512;not null" json:"base_url"`         // 请求只能去这个地址
	HeadersJSON   JSONText  `gorm:"type:json;not null" json:"headers_json"`    // 自定义请求头 {"名称":"值"}，明文存储，只加到发往 base_url 同源的请求
	RateLimitJSON JSONText  `gorm:"type:json;not null" json:"rate_limit_json"` // {"rps":5,"max_concurrency":20,"max_running":2}，缺省不限；max_running 是同时在上游生成的任务数上限
	Sort          int       `gorm:"not null;default:100" json:"sort"`          // 渠道顺序，越小越靠前；用户看到的模型顺序 = 渠道顺序 + 渠道内顺序
	Enabled       bool      `gorm:"not null;default:true" json:"enabled"`      // 停用后不再接新任务，进行中的任务按快照继续
	UpdatedBy     uint64    `gorm:"not null;default:0" json:"updated_by"`      // 最后修改人
	CreatedAt     time.Time `json:"created_at"`                                // 创建时间
	UpdatedAt     time.Time `json:"updated_at"`                                // 更新时间

	// 以下四列是渠道绑协议时代的遗留列，代码不再读写；按“先加后删”的迁移规则本版本只放宽约束保留，下个版本再删。
	// 它们都有默认值，新建渠道时不用写。
	LegacyPluginKey        string   `gorm:"column:plugin_key;size:30;not null;default:'';index:idx_ai_channels_plugin_key" json:"-"`
	LegacyPluginVersionID  uint64   `gorm:"column:plugin_version_id;not null;default:0;index:idx_ai_channels_plugin_version_id" json:"-"`
	LegacyAllowCredentials bool     `gorm:"column:allow_credentials;not null;default:false" json:"-"`
	LegacySettingsJSON     JSONText `gorm:"column:settings_json;type:json;not null;default:'{}'" json:"-"`
}

func (AIChannel) TableName() string { return "ai_channels" }

// ChannelSecretName 返回渠道 API Key 在 ai_secrets 里的名字。
func ChannelSecretName(channelKey string) string { return "channel:" + channelKey }

// ChannelSecretKeyName 返回渠道 Secret Key（可选的第二凭证，供 AK/SK 签名类协议用）在 ai_secrets 里的名字。
// 渠道 key 不含冒号，所以它与 ChannelSecretName 不会冲突。
func ChannelSecretKeyName(channelKey string) string { return "channel:" + channelKey + ":secret" }

// ChannelLoad 是一个渠道当前的任务负载，供管理端判断渠道是不是满了。
type ChannelLoad struct {
	Channel string `json:"channel"` // 渠道 key
	Running int    `json:"running"` // 同时生成数：已在上游（queued / running）+ 正在提交（pending 且租约有效），和 max_running 比较
	Waiting int    `json:"waiting"` // 排队数：还没调用上游、在等名额（或等提交重试）的 pending
}
