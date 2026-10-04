package model

import "time"

// system_settings 的键。库值优先，缺省时由 config.yaml 提供初始默认。
const (
	SettingRegisterEnabled       = "register_enabled"         // 是否开放注册，"true" / "false"，缺省 true
	SettingRegisterVerifyEmail   = "register_verify_email"    // 注册是否需要验证邮箱，"true" / "false"，缺省 true；只有 SMTP 已启用时才真正生效
	SettingInitialCredits        = "initial_credits"          // 新用户初始积分，缺省取 service.DefaultInitialCredits
	SettingDefaultMaxActiveTasks = "default_max_active_tasks" // 默认并发上限，缺省取 service.DefaultMaxActiveTasks
)

// SystemSetting 系统设置的键值表（值统一存文本）。
type SystemSetting struct {
	Key       string    `gorm:"primaryKey;size:64" json:"key"`        // 设置键
	Value     string    `gorm:"type:text;not null" json:"value"`      // 设置值
	UpdatedBy uint64    `gorm:"not null;default:0" json:"updated_by"` // 最后修改人
	UpdatedAt time.Time `json:"updated_at"`                           // 更新时间
}

// TableName 返回表名。
func (SystemSetting) TableName() string { return "system_settings" }

// RegisterSettingsView 是注册设置的读写结构：GET / PUT /admin/settings/register。
type RegisterSettingsView struct {
	RegisterEnabled       bool `json:"register_enabled"`                                               // 是否开放注册
	VerifyEmail           bool `json:"verify_email"`                                                   // 注册是否需要验证邮箱（SMTP 未启用时不会验证）
	InitialCredits        int  `json:"initial_credits" binding:"min=0,max=100000000" label:"初始积分"`     // 新用户初始积分
	DefaultMaxActiveTasks int  `json:"default_max_active_tasks" binding:"min=1,max=64" label:"默认并发上限"` // 默认并发上限
}
