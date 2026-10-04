package model

import "time"

// 登录记录的类型与结果。
const (
	LoginKindLogin    = "login"    // 用户名密码登录
	LoginKindRegister = "register" // 注册即登录

	LoginResultOK      = "ok"      // 成功
	LoginResultBadPw   = "badpw"   // 密码错误（或用户不存在）
	LoginResultBlocked = "blocked" // 账号已停用
)

// UserLoginLog 登录记录，保留 180 天。登录失败且用户不存在时 user_id 为 0。
type UserLoginLog struct {
	ID        uint64    `gorm:"primaryKey" json:"id"`                                                      // 记录 ID
	UserID    uint64    `gorm:"not null;default:0;index:idx_login_user_created,priority:1" json:"user_id"` // 用户 ID，不存在时为 0
	Kind      string    `gorm:"size:16;not null" json:"kind"`                                              // login / register
	Result    string    `gorm:"size:16;not null" json:"result"`                                            // ok / badpw / blocked
	IP        string    `gorm:"size:64;not null;default:''" json:"ip"`                                     // 客户端 IP
	UserAgent string    `gorm:"size:255;not null;default:''" json:"user_agent"`                            // User-Agent（截断）
	CreatedAt time.Time `gorm:"index:idx_login_user_created,priority:2,sort:desc" json:"created_at"`       // 发生时间
}

// TableName 返回表名。
func (UserLoginLog) TableName() string { return "user_login_logs" }
