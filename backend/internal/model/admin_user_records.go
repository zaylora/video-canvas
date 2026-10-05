package model

import "time"

// 后台用户详情抽屉里的三类记录（生成记录 / 积分流水 / 登录记录）及其游标分页响应。

// CursorPage 是游标分页的响应：NextCursor 是不透明字符串，原样传回 cursor 参数取下一页；没有更多时为空串。
type CursorPage[T any] struct {
	Items      []T    `json:"items"`       // 本页记录（永远不是 null）
	NextCursor string `json:"next_cursor"` // 下一页游标，空串表示已到末尾
}

// AdminTaskItem 是 GET /admin/users/:id/tasks 的一行（契约 §3）。
type AdminTaskItem struct {
	ID             uint64    `json:"id"`
	Kind           string    `json:"kind"`
	ModelKey       string    `json:"model_key"`
	ModelName      string    `json:"model_name"`      // 任务快照里的模型显示名，没有则回落 model_key
	Status         string    `json:"status"`          // pending / queued / running / finalizing / succeeded / failed / canceled / expired
	Credits        int       `json:"credits"`         // 提交时冻结的额度
	ChargedCredits int       `json:"charged_credits"` // 实际扣费，未结算为 0
	Error          string    `json:"error"`           // 失败原因，成功时为空串
	DurationMs     *int64    `json:"duration_ms"`     // 完成时间 - 开始时间（毫秒），进行中为 null
	CreatedAt      time.Time `json:"created_at"`
}

// AdminLedgerItem 是 GET /admin/users/:id/ledger 的一行（契约 §3）。
// Amount 是带符号的“对用户可用积分的影响”（列表接口在响应层映射，库里的值不变）：
// freeze / settle 为负，refund / initial 为正，admin_adjust 为库里的原值（本身带正负号）。
type AdminLedgerItem struct {
	ID           uint64    `json:"id"`
	Type         string    `json:"type"`
	Amount       int       `json:"amount"`
	TaskID       *uint64   `json:"task_id"`
	Note         string    `json:"note"`
	OperatorID   *uint64   `json:"operator_id"`
	OperatorName string    `json:"operator_name"` // 操作人用户名，系统流水为空串
	CreatedAt    time.Time `json:"created_at"`
}

// AdminLoginItem 是 GET /admin/users/:id/logins 的一行（契约 §3）。
type AdminLoginItem struct {
	ID        uint64    `json:"id"`
	Kind      string    `json:"kind"`
	Result    string    `json:"result"`
	IP        string    `json:"ip"`
	UserAgent string    `json:"user_agent"`
	CreatedAt time.Time `json:"created_at"`
}

// AdminCreditView 是积分调整后的账户快照（POST /admin/users/:id/credits 的响应）。
type AdminCreditView struct {
	Balance   int `json:"balance"`   // 余额（含冻结）
	Frozen    int `json:"frozen"`    // 冻结额
	Available int `json:"available"` // 可用 = 余额 - 冻结
}
