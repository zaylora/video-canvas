package model

import "time"

// system_settings 的键。库值优先，缺省时由 config.yaml 提供初始默认。
const (
	SettingRegisterEnabled       = "register_enabled"         // 是否开放注册，"true" / "false"，缺省 true
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
	InitialCredits        int  `json:"initial_credits" binding:"min=0,max=100000000" label:"初始积分"`     // 新用户初始积分
	DefaultMaxActiveTasks int  `json:"default_max_active_tasks" binding:"min=1,max=64" label:"默认并发上限"` // 默认并发上限
}

// 登录页展示（showcase）相关的系统设置键，值统一存文本；库值缺省或非法时由 service 回落默认。
const (
	SettingShowcaseClipSeconds      = "showcase_clip_seconds"             // 每段视频播放秒数，合法 4–15，缺省 7
	SettingShowcaseShowOnLogin      = "showcase_show_on_login"            // 登录页是否展示轮播，"true" / "false"，缺省 true
	SettingShowcasePosterOnSaveData = "showcase_poster_only_on_save_data" // 省流量模式下是否只显示封面，"true" / "false"，缺省 true
)
