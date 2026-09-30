package model

import "time"

// 插件来源。
const (
	PluginSourceBuiltin  = "builtin"  // 随仓库发布，启动时登记，不能被删除或覆盖
	PluginSourceUploaded = "uploaded" // 运维（super_admin）在线上传
)

// AIPlugin 协议插件（一个 key 下有多个不可变版本）。见 docs/design/协议插件设计.md 6.7。
type AIPlugin struct {
	Key       string    `gorm:"primaryKey;size:30" json:"key"`                   // 插件 key，小写字母、数字、连字符
	Name      string    `gorm:"size:128;not null" json:"name"`                   // 显示名（取最新版本的 meta.name）
	Source    string    `gorm:"size:16;not null;default:uploaded" json:"source"` // builtin / uploaded
	Enabled   bool      `gorm:"not null;default:true" json:"enabled"`            // 停用后所有使用它的渠道不再接新任务
	UpdatedAt time.Time `json:"updated_at"`                                      // 更新时间
}

func (AIPlugin) TableName() string { return "ai_plugins" }

// AIPluginVersion 插件的一个版本：代码一旦登记就不可修改，任务快照记住它的 sha256，出了问题能按原代码复现。
type AIPluginVersion struct {
	ID        uint64    `gorm:"primaryKey" json:"id"`                                                              // 版本 ID
	PluginKey string    `gorm:"size:30;not null;uniqueIndex:uk_plugin_version,priority:1;index" json:"plugin_key"` // 所属插件
	Version   string    `gorm:"size:32;not null;uniqueIndex:uk_plugin_version,priority:2" json:"version"`          // semver，同一插件下唯一
	SHA256    string    `gorm:"column:sha256;size:64;not null;index" json:"sha256"`                                // 代码的十六进制 sha256
	Code      string    `gorm:"type:text;not null" json:"-"`                                                       // 插件代码（≤512KB），只在装载进 runner 时读取
	MetaJSON  JSONText  `gorm:"type:json;not null" json:"meta_json"`                                               // 预检时读出的 meta（pluginmeta.Meta）
	CreatedBy uint64    `gorm:"not null;default:0" json:"created_by"`                                              // 上传人（内置插件为 0）
	CreatedAt time.Time `json:"created_at"`                                                                        // 登记时间
}

func (AIPluginVersion) TableName() string { return "ai_plugin_versions" }
