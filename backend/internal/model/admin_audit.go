package model

import "time"

// 后台运营审计动作。
//
//nolint:gosec // G101 误报：这些是审计动作名，不是凭证（名字里带 password 只是“重置密码”这个动作）
const (
	AdminAuditCreditAdjust  = "user.credit_adjust"
	AdminAuditUserBan       = "user.ban"
	AdminAuditUserUnban     = "user.unban"
	AdminAuditUserRole      = "user.role"
	AdminAuditResetPassword = "user.reset_password"
	AdminAuditUserLimit     = "user.limit"
	AdminAuditSettingsReg   = "settings.register"
	AdminAuditSettingsSMTP  = "settings.smtp"

	AdminAuditShowcaseCreate   = "showcase.create"
	AdminAuditShowcaseUpdate   = "showcase.update"
	AdminAuditShowcaseDelete   = "showcase.delete"
	AdminAuditShowcaseOrder    = "showcase.order"
	AdminAuditShowcaseSettings = "showcase.settings"

	AdminAuditTargetUser     = "user"
	AdminAuditTargetSettings = "settings"
	AdminAuditTargetShowcase = "showcase"
)

// AdminAuditLog 后台运营审计日志（用户管理与系统设置）：只增不改不删。detail 不含任何密码、密钥。
type AdminAuditLog struct {
	ID         uint64    `gorm:"primaryKey" json:"id"`                                                        // 日志 ID
	ActorID    uint64    `gorm:"not null;default:0;index" json:"actor_id"`                                    // 操作人
	Action     string    `gorm:"size:64;not null;index" json:"action"`                                        // 见 AdminAudit* 常量
	TargetType string    `gorm:"size:32;not null;index:idx_admin_audit_target,priority:1" json:"target_type"` // user / settings / showcase
	TargetID   uint64    `gorm:"not null;default:0;index:idx_admin_audit_target,priority:2" json:"target_id"` // 目标 id，settings 为 0
	DetailJSON JSONText  `gorm:"type:json" json:"detail_json"`                                                // 补充信息
	CreatedAt  time.Time `gorm:"index" json:"created_at"`                                                     // 发生时间
}

// TableName 返回表名。
func (AdminAuditLog) TableName() string { return "admin_audit_logs" }
