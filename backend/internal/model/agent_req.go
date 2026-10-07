package model

import "encoding/json"

// AgentModelView 是面向画布的 Agent 模型清单项。
type AgentModelView struct {
	Key    string `json:"key"`    // 模型 key
	Name   string `json:"name"`   // 显示名
	Vision bool   `json:"vision"` // 是否支持看图
}

// AgentRunInput 是启动运行时交给 runtime 的输入。
type AgentRunInput struct {
	Message   string          `json:"message"`   // 用户的消息，行内 chip 已序列化为 @[名字](类型:id)
	Mode      string          `json:"mode"`      // 任务模式
	Selection []string        `json:"selection"` // 选中的节点 id
	Viewport  json.RawMessage `json:"viewport"`  // 视口，可为空
	ModelKey  string          `json:"model_key"` // 本轮使用的 Agent 模型
}

// CreateAgentSessionReq 是新建会话的请求。
type CreateAgentSessionReq struct {
	Title    string `json:"title" binding:"max=40" label:"标题"`                                        // 标题，不传默认「新对话」
	Mode     string `json:"mode" binding:"omitempty,oneof=all script storyboard prompt" label:"任务模式"` // 任务模式，不传默认 all
	ModelKey string `json:"model_key" binding:"max=128" label:"模型"`                                   // Agent 模型，不传取第一个可用的
}

// RenameAgentSessionReq 是重命名会话的请求。
type RenameAgentSessionReq struct {
	Title string `json:"title" binding:"required,max=40" label:"标题"` // 新标题
}

// StartAgentRunReq 是发起一轮运行的请求。
type StartAgentRunReq struct {
	Message       string          `json:"message" binding:"required,max=20000" label:"消息"`                          // 用户的消息
	Mode          string          `json:"mode" binding:"omitempty,oneof=all script storyboard prompt" label:"任务模式"` // 任务模式，不传沿用会话的
	Selection     []string        `json:"selection" binding:"max=200,dive,max=64" label:"选中节点"`                     // 选中的节点 id
	Viewport      json.RawMessage `json:"viewport" label:"视口"`                                                      // 视口
	BudgetCredits *int            `json:"budget_credits" binding:"omitempty,min=0,max=100000" label:"本轮预算"`         // 本轮积分预算，不传默认 50
	AgentModelKey string          `json:"agent_model_key" binding:"max=128" label:"Agent 模型"`                       // Agent 模型，不传沿用会话的
}

// InterjectAgentReq 是运行中插话的请求。
type InterjectAgentReq struct {
	Message string `json:"message" binding:"required,max=20000" label:"消息"` // 补充的要求
}

// ResumeAgentReq 是继续运行的请求。
type ResumeAgentReq struct {
	AddBudget int `json:"add_budget" binding:"min=0,max=100000" label:"追加预算"` // 追加的积分预算，预算用尽时用
}

// ApprovalItemDecision 是对审批里某一项的决定。
type ApprovalItemDecision struct {
	Index   int  `json:"index" binding:"min=0" label:"序号"`       // 审批条目的下标
	Approve bool `json:"approve"`                                // 是否批准这一项
	Count   int  `json:"count" binding:"min=0,max=4" label:"数量"` // 生成张数，只能不大于申请的数量，0 表示按申请的
}

// DecideAgentApprovalReq 是对审批的决定。
type DecideAgentApprovalReq struct {
	Decision  string                 `json:"decision" binding:"required,oneof=approve reject" label:"决定"` // approve 或 reject
	Items     []ApprovalItemDecision `json:"items" binding:"max=50,dive" label:"条目"`                      // 逐项决定，不传表示整体按 decision 处理
	Answer    string                 `json:"answer" binding:"max=2000" label:"回答"`                        // 提问的回答
	AddBudget int                    `json:"add_budget" binding:"min=0,max=100000" label:"追加预算"`          // 批准时追加的积分预算
}
