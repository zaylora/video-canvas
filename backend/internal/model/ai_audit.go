package model

import "time"

// 审计动作。
const (
	AuditPluginUpload    = "plugin.upload"         // 上传插件版本（detail 含版本号与 sha256）
	AuditPluginEnable    = "plugin.enable"         // 启用 / 停用插件
	AuditPluginDelete    = "plugin.delete"         // 删除插件版本（target_type=plugin_version）或整个插件（target_type=plugin，detail 含全部版本号）
	AuditChannelCreate   = "channel.create"        // 新建渠道
	AuditChannelUpdate   = "channel.update"        // 修改渠道（含切换插件版本）
	AuditChannelSecret   = "channel.secret"        // 设置渠道 Key（不记值）
	AuditChannelTrusted  = "channel.trusted"       // 历史记录：已取消的“允许访问内网”开关（trusted_internal），不再产生新记录，展示旧审计时要认得
	AuditChannelCred     = "channel.credential"    // 历史记录：已取消的 allow_credentials 开关，不再产生新记录，展示旧审计时要认得
	AuditChannelSecKey   = "channel.secret_key"    // 设置 / 清除渠道 Secret Key（不记值）
	AuditChannelSort     = "channel.sort"          // 调整渠道顺序（detail 含新顺序）
	AuditModelImport     = "model.import"          // 从渠道拉取并导入模型（target 是渠道，detail 含新建的模型 key）
	AuditModelProtocol   = "model.protocol"        // 模型的协议（插件 / 版本）变化（detail 含前后值）
	AuditPluginUpgrade   = "plugin.upgrade_models" // 批量把模型升级到插件的新版本
	AuditChannelEnable   = "channel.enable"        // 启用 / 停用渠道
	AuditChannelAutoOff  = "channel.auto_off"      // 连续插件级失败，自动停用（actor 为 0）
	AuditChannelDelete   = "channel.delete"        // 删除渠道（同时删掉它的 Key，detail 不含 Key）
	AuditStorageCreate   = "storage.create"        // 新建存储
	AuditStorageUpdate   = "storage.update"        // 修改存储配置
	AuditStorageSecret   = "storage.secret"        // 替换存储密钥（不记值）
	AuditStorageDefault  = "storage.default"       // 设置默认存储（detail 含新旧默认）
	AuditStorageDelete   = "storage.delete"        // 删除存储（同时删掉它的密钥）
	AuditTargetStorage   = "storage"
	AuditTargetPlugin    = "plugin"
	AuditTargetChannel   = "channel"
	AuditTargetPluginVer = "plugin_version"
)

// AIAuditLog 审计日志：插件上传、渠道变更、凭证变更、allow_credentials 开关（历史上还有 trusted_internal）。只增不改不删。
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
