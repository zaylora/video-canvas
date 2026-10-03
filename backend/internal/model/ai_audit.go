package model

import "time"

// 审计动作。
const (
	AuditPluginUpload    = "plugin.upload"      // 上传插件版本（detail 含版本号与 sha256）
	AuditPluginEnable    = "plugin.enable"      // 启用 / 停用插件
	AuditPluginDelete    = "plugin.delete"      // 删除插件版本（target_type=plugin_version）或整个插件（target_type=plugin，detail 含全部版本号）
	AuditChannelCreate   = "channel.create"     // 新建渠道
	AuditChannelUpdate   = "channel.update"     // 修改渠道（含切换插件版本）
	AuditChannelSecret   = "channel.secret"     // 设置渠道 Key（不记值）
	AuditChannelTrusted  = "channel.trusted"    // 开关 trusted_internal
	AuditChannelCred     = "channel.credential" // 开关 allow_credentials
	AuditChannelEnable   = "channel.enable"     // 启用 / 停用渠道
	AuditChannelAutoOff  = "channel.auto_off"   // 连续插件级失败，自动停用（actor 为 0）
	AuditChannelDelete   = "channel.delete"     // 删除渠道（同时删掉它的 Key，detail 不含 Key）
	AuditStorageCreate   = "storage.create"     // 新建存储
	AuditStorageUpdate   = "storage.update"     // 修改存储配置
	AuditStorageSecret   = "storage.secret"     // 替换存储密钥（不记值）
	AuditStorageDefault  = "storage.default"    // 设置默认存储（detail 含新旧默认）
	AuditStorageDelete   = "storage.delete"     // 删除存储（同时删掉它的密钥）
	AuditTargetStorage   = "storage"
	AuditTargetPlugin    = "plugin"
	AuditTargetChannel   = "channel"
	AuditTargetPluginVer = "plugin_version"
)

// AIAuditLog 审计日志：插件上传、渠道变更、凭证变更、trusted_internal 与 allow_credentials 开关。只增不改不删。
type AIAuditLog struct {
	ID         uint64    `gorm:"primaryKey" json:"id"`                      // 日志 ID
	ActorID    uint64    `gorm:"not null;default:0;index" json:"actor_id"`  // 操作人（系统动作为 0）
	Action     string    `gorm:"size:64;not null;index" json:"action"`      // 见 Audit* 常量
	TargetType string    `gorm:"size:32;not null" json:"target_type"`       // plugin / plugin_version / channel
	TargetKey  string    `gorm:"size:128;not null;index" json:"target_key"` // 插件 key / 渠道 key
	DetailJSON JSONText  `gorm:"type:json" json:"detail_json"`              // 补充信息（版本号、sha256、开关前后值）；不含任何凭证
	CreatedAt  time.Time `gorm:"index" json:"created_at"`                   // 发生时间
}

func (AIAuditLog) TableName() string { return "ai_audit_logs" }
