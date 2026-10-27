package model

// AgentModelView 是面向画布的 Agent 模型清单项。
type AgentModelView struct {
	Key           string `json:"key"`            // 模型 key
	Name          string `json:"name"`           // 显示名
	Vendor        string `json:"vendor"`         // 厂商 slug，画布据此显示 logo；没配是空串
	Vision        bool   `json:"vision"`         // 是否支持看图
	Video         bool   `json:"video"`          // 是否支持理解视频画面和声音
	ContextWindow int    `json:"context_window"` // 上下文窗口（Token），没配置是 0；画布据此显示上下文用量
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

// IssueAnalysisTokenReq 是签发分析令牌的请求。
type IssueAnalysisTokenReq struct {
	Name string `json:"name" binding:"required,max=200" label:"用途"`         // 用途备注，1–64 字（按字数在 service 里校验）
	Days int    `json:"days" binding:"required,min=1,max=3650" label:"有效期"` // 有效天数，1–90（范围在 service 里校验并给出明确提示）
}
