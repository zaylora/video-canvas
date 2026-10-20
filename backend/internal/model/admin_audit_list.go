package model

import "time"

// 后台「日志管理 › 操作审计」：把 admin_audit_logs 与 ai_audit_logs 合并成一个只读列表。

// 审计来源：对应两张表。
const (
	AuditSourceAdmin = "admin" // admin_audit_logs：用户管理、系统设置、登录页展示、系统升级、支付、Agent 轨迹导出
	AuditSourceAI    = "ai"    // ai_audit_logs：插件、渠道、模型、存储
)

// ListAuditReq 是 GET /admin/logs/audits 的查询参数。
type ListAuditReq struct {
	Actor    string `form:"actor" binding:"max=64" label:"操作人"`                                                                                   // 用户名片段或数字 id；「系统」「system」表示系统自动动作
	Module   string `form:"module" binding:"omitempty,oneof=user settings showcase system payment channel plugin model storage agent" label:"模块"` // 动作名点号前的前缀
	Action   string `form:"action" binding:"max=64" label:"动作"`                                                                                   // 完整动作名
	From     string `form:"from" binding:"max=40" label:"开始时间"`                                                                                   // RFC3339，默认 To 往前 30 天
	To       string `form:"to" binding:"max=40" label:"结束时间"`                                                                                     // RFC3339，默认现在
	Page     int    `form:"page" label:"页码"`                                                                                                      // 页码，从 1 开始
	PageSize int    `form:"page_size" label:"每页条数"`                                                                                               // 每页条数，默认 20，最大 100
}

// AuditRow 是合并查询的一行（仓储扫描用）。target_ref：admin 来源是 target_id 的十进制串，ai 来源是 target_key。
type AuditRow struct {
	Source     string
	ID         uint64
	ActorID    uint64
	Action     string
	TargetType string
	TargetRef  string
	DetailJSON JSONText
	CreatedAt  time.Time
}

// AuditItem 是 GET /admin/logs/audits 的一行。
type AuditItem struct {
	Source     string    `json:"source"`      // admin / ai
	ID         uint64    `json:"id"`          // 在各自来源表里的 id，两个来源之间可能重复
	ActorID    uint64    `json:"actor_id"`    // 操作人，0 是系统自动动作
	ActorName  string    `json:"actor_name"`  // 操作人用户名；系统动作或用户已不存在为空串
	Action     string    `json:"action"`      // 动作名，见 AdminAudit* 与 Audit* 常量（含历史动作）
	TargetType string    `json:"target_type"` // 对象类型
	TargetRef  string    `json:"target_ref"`  // 对象标识：用户是 id 的十进制串，渠道 / 插件等是 key
	TargetName string    `json:"target_name"` // 能解析时的展示名（目标是用户时的用户名），否则空串
	DetailJSON JSONText  `json:"detail_json"` // 补充信息，写入时已保证不含密码 / 密钥；没有为 null
	CreatedAt  time.Time `json:"created_at"`  // 发生时间
}
