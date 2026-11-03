package model

// AgentAdminAttempt 持久化二期工具的运行内计次，不保存草稿、输入或凭证。
type AgentAdminAttempt struct {
	RunID   uint64 `gorm:"primaryKey;autoIncrement:false"`
	Kind    string `gorm:"primaryKey;type:varchar(32)"`
	Target  string `gorm:"primaryKey;type:varchar(128)"`
	Token   string `gorm:"primaryKey;type:varchar(64)"`
	Attempt int    `gorm:"not null"`
}

// TableName 返回后台助手计次表名。
func (AgentAdminAttempt) TableName() string { return "agent_admin_attempts" }
