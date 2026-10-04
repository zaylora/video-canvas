package model

import "time"

// SMTP 加密方式。
const (
	SMTPEncNone     = "none"
	SMTPEncStartTLS = "starttls"
	SMTPEncTLS      = "tls"
)

// SMTPSettingID 是 smtp_settings 单行的固定主键。
const SMTPSettingID = 1

// SMTPSetting 邮件服务配置（单行）。密码用 AES-256-GCM 加密后存 PasswordEnc / PasswordNonce，只写不读。
type SMTPSetting struct {
	ID             uint64     `gorm:"primaryKey" json:"-"`                                   // 固定为 1
	Host           string     `gorm:"size:255;not null;default:''" json:"host"`              // 主机
	Port           int        `gorm:"not null;default:0" json:"port"`                        // 端口
	Encryption     string     `gorm:"size:16;not null;default:starttls" json:"encryption"`   // none / starttls / tls
	Username       string     `gorm:"size:255;not null;default:''" json:"username"`          // 登录用户名，为空表示不认证
	PasswordEnc    []byte     `json:"-"`                                                     // 密码密文
	PasswordNonce  []byte     `json:"-"`                                                     // GCM 随机数
	FromAddress    string     `gorm:"size:255;not null;default:''" json:"from_address"`      // 发件地址
	FromName       string     `gorm:"size:128;not null;default:''" json:"from_name"`         // 发件人名称
	Enabled        bool       `gorm:"not null;default:false" json:"enabled"`                 // 是否启用
	LastCheckAt    *time.Time `json:"last_check_at"`                                         // 最近一次测试发信时间
	LastCheckOK    *bool      `json:"last_check_ok"`                                         // 最近一次测试是否成功
	LastCheckError string     `gorm:"type:text;not null;default:''" json:"last_check_error"` // 最近一次失败原因（已脱敏）
	UpdatedBy      uint64     `gorm:"not null;default:0" json:"updated_by"`                  // 最后修改人
	UpdatedAt      time.Time  `json:"updated_at"`                                            // 更新时间
}

// TableName 返回表名。
func (SMTPSetting) TableName() string { return "smtp_settings" }

// SMTPSettingsView 是 GET /admin/settings/smtp 的响应：密码永不返回，只给 has_password。
type SMTPSettingsView struct {
	Host           string     `json:"host"`
	Port           int        `json:"port"`
	Encryption     string     `json:"encryption"`
	Username       string     `json:"username"`
	HasPassword    bool       `json:"has_password"`
	FromAddress    string     `json:"from_address"`
	FromName       string     `json:"from_name"`
	Enabled        bool       `json:"enabled"`
	LastCheckAt    *time.Time `json:"last_check_at"`
	LastCheckOK    *bool      `json:"last_check_ok"`
	LastCheckError string     `json:"last_check_error"`
}

// UpdateSMTPSettingsReq 是 PUT /admin/settings/smtp 的请求（不含密码）。
type UpdateSMTPSettingsReq struct {
	Host        string `json:"host" binding:"max=255" label:"主机"`
	Port        int    `json:"port" binding:"min=0,max=65535" label:"端口"`
	Encryption  string `json:"encryption" binding:"required,oneof=none starttls tls" label:"加密方式"`
	Username    string `json:"username" binding:"max=255" label:"用户名"`
	FromAddress string `json:"from_address" binding:"max=255" label:"发件地址"`
	FromName    string `json:"from_name" binding:"max=128" label:"发件人名称"`
	Enabled     bool   `json:"enabled"`
}

// SetSMTPPasswordReq 是 PUT /admin/settings/smtp/password 的请求。
type SetSMTPPasswordReq struct {
	Password string `json:"password" binding:"required,max=1024" label:"密码"`
}

// TestSMTPReq 是 POST /admin/settings/smtp/test 的请求。
type TestSMTPReq struct {
	To string `json:"to" binding:"required,email,max=255" label:"收件地址"`
}

// MailConfig 是一次发信用的完整 SMTP 参数（含明文密码，只在内存里短暂存在，不进日志、不进响应）。
type MailConfig struct {
	Host        string
	Port        int
	Encryption  string // none / starttls / tls
	Username    string // 为空表示不认证
	Password    string `json:"-"`
	FromAddress string
	FromName    string
}

// MailMessage 是一封纯文本邮件。
type MailMessage struct {
	To      string
	Subject string
	Body    string
}
