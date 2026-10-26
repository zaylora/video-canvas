package model

import "time"

// AgentCanvasRead 记录某运行已读详情或自行创建的节点；运行由独立 Agent 服务管理，不依赖 Go 的 agent_runs 表。
type AgentCanvasRead struct {
	UserID    uint64    `gorm:"primaryKey;autoIncrement:false"` // 所属用户
	CanvasID  uint64    `gorm:"primaryKey;autoIncrement:false"` // 所属画布
	SessionID uint64    `gorm:"primaryKey;autoIncrement:false"` // 所属会话
	RunID     uint64    `gorm:"primaryKey;autoIncrement:false"` // 所属运行，续跑不变
	NodeID    string    `gorm:"primaryKey;type:text"`           // 返回过详情或自行创建的节点 id
	CreatedAt time.Time // 首次记录时间
}

// TableName 返回已读记录表名。
func (AgentCanvasRead) TableName() string { return "agent_canvas_reads" }
