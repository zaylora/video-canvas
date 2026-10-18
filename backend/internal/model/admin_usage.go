package model

import (
	"encoding/json"
	"time"
)

// 后台「使用记录」（用户 › 使用记录）：全站生成任务的流水、摘要与详情。设计见 docs/design/日志管理/日志管理.md。

// UsageFilterReq 是使用记录列表与摘要共用的筛选参数。
type UsageFilterReq struct {
	Q           string `form:"q" binding:"max=64" label:"关键字"`                                                           // 32 位十六进制按 task_ref 精确查；否则按用户名 / 邮箱模糊匹配
	UserID      uint64 `form:"user_id" label:"用户"`                                                                       // 只看某个用户
	Status      string `form:"status" binding:"omitempty,oneof=all active succeeded failed canceled expired" label:"状态"` // failed 只含 failed，取消与超时各自单独筛
	Kind        string `form:"kind" binding:"omitempty,oneof=video image audio text" label:"类型"`
	Model       string `form:"model" binding:"max=128" label:"模型"` // 模型 key
	From        string `form:"from" binding:"max=40" label:"开始时间"` // RFC3339，默认 To 往前 7 天
	To          string `form:"to" binding:"max=40" label:"结束时间"`   // RFC3339，默认现在
	IncludeTest bool   `form:"include_test" label:"含试跑"`           // 默认不含运营试跑任务
}

// ListUsageReq 是 GET /admin/usage 的查询参数。
type ListUsageReq struct {
	UsageFilterReq
	Page     int `form:"page" label:"页码"`        // 页码，从 1 开始
	PageSize int `form:"page_size" label:"每页条数"` // 每页条数，默认 20，最大 100
}

// UsageRow 是列表查询的一行（仓储扫描用，service 转成 UsageItem）。
type UsageRow struct {
	ID             uint64
	TaskRef        string
	UserID         uint64
	Username       string
	Kind           string
	ModelKey       string
	ModelName      string
	Status         string
	Credits        int
	ChargedCredits *int
	ErrorCode      string
	ErrorMessage   string
	DurationMs     *int64
	IsTest         bool
	CreatedAt      time.Time
}

// UsageUser 是使用记录里的用户。
type UsageUser struct {
	ID       uint64 `json:"id"`
	Username string `json:"username"`
}

// UsageItem 是 GET /admin/usage 的一行。
type UsageItem struct {
	ID             uint64    `json:"id"`
	TaskRef        string    `json:"task_ref"`        // 任务编号：界面展示、日志 task_id 都是它
	User           UsageUser `json:"user"`            // 所属用户
	Kind           string    `json:"kind"`            // video / image / audio / text
	ModelKey       string    `json:"model_key"`       // 模型 key
	ModelName      string    `json:"model_name"`      // 任务快照里的模型展示名，没有则回落 model_key
	Status         string    `json:"status"`          // 任务状态
	Credits        int       `json:"credits"`         // 冻结的积分
	ChargedCredits *int      `json:"charged_credits"` // 实际扣的积分，未结算为 null
	ErrorCode      string    `json:"error_code"`      // 统一错误码
	ErrorMessage   string    `json:"error_message"`   // 给用户看的文案
	DurationMs     *int64    `json:"duration_ms"`     // 完成时间 - 创建时间（毫秒），进行中为 null
	IsTest         bool      `json:"is_test"`         // 运营试跑
	CreatedAt      time.Time `json:"created_at"`      // 创建时间
}

// UsageStatusCount 是摘要查询的一行：某状态的任务数与实扣积分。
type UsageStatusCount struct {
	Status  string
	Count   int
	Charged int64
}

// UsageSummary 是 GET /admin/usage/summary 的响应；与列表使用同一组筛选条件。
type UsageSummary struct {
	Total          int   `json:"total"`
	Succeeded      int   `json:"succeeded"`
	Failed         int   `json:"failed"` // 只含 failed
	Canceled       int   `json:"canceled"`
	Expired        int   `json:"expired"`
	Active         int   `json:"active"`          // pending / queued / running / finalizing
	ChargedCredits int64 `json:"charged_credits"` // 实扣积分合计
}

// UsageTask 是详情里的任务基础信息。
type UsageTask struct {
	ID             uint64     `json:"id"`
	TaskRef        string     `json:"task_ref"`
	Kind           string     `json:"kind"`
	ModelKey       string     `json:"model_key"`
	ModelName      string     `json:"model_name"`
	Channel        string     `json:"channel"`   // 渠道 key（来自创建时的配置快照）
	Plugin         string     `json:"plugin"`    // 插件 key@版本（来自快照）
	CanvasID       string     `json:"canvas_id"` // 所属画布（十六进制串），没有为空串
	NodeID         string     `json:"node_id"`
	Status         string     `json:"status"`
	Progress       *int       `json:"progress"`
	Credits        int        `json:"credits"`
	ChargedCredits *int       `json:"charged_credits"`
	ErrorCode      string     `json:"error_code"`
	ErrorMessage   string     `json:"error_message"`
	IsTest         bool       `json:"is_test"`
	CreatedAt      time.Time  `json:"created_at"`
	SubmittedAt    *time.Time `json:"submitted_at"`
	FinishedAt     *time.Time `json:"finished_at"`
	DeadlineAt     time.Time  `json:"deadline_at"`
}

// UsageUserDetail 是详情里的用户。
type UsageUserDetail struct {
	ID       uint64 `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
}

// UsageError 是详情里的失败信息：用户看到的文案，加上给后台排查的上游错误原文。
type UsageError struct {
	Code     string  `json:"code"`
	Message  string  `json:"message"`  // 给用户看的统一文案
	Upstream *string `json:"upstream"` // 上游错误原文（宿主已遮掉渠道 Key）；没有为 null（老任务、超时、取消、用户参数错误等）
}

// UsageEvent 是详情里状态时间线的一项。
type UsageEvent struct {
	ID         uint64         `json:"id"`
	Event      string         `json:"event"`
	FromStatus string         `json:"from_status"`
	ToStatus   string         `json:"to_status"`
	Detail     map[string]any `json:"detail"` // 没有详情为 null
	CreatedAt  time.Time      `json:"created_at"`
}

// UsageLedger 是详情里的积分流水；Amount 带符号（freeze / settle 为负，refund 为正）。
type UsageLedger struct {
	ID        uint64    `json:"id"`
	Type      string    `json:"type"`
	Amount    int       `json:"amount"`
	CreatedAt time.Time `json:"created_at"`
}

// UsageDetail 是 GET /admin/usage/:ref 的响应。
type UsageDetail struct {
	Task    UsageTask       `json:"task"`
	User    UsageUserDetail `json:"user"`
	Input   map[string]any  `json:"input"`   // 创建时已校验的规范化输入（含提示词）
	Outputs []TaskOutput    `json:"outputs"` // 已转存的产物
	Error   UsageError      `json:"error"`
	Events  []UsageEvent    `json:"events"` // 状态时间线；上线前的老任务为空数组
	Ledger  []UsageLedger   `json:"ledger"`
	Trace   json.RawMessage `json:"trace"` // 仅试跑任务：插件钩子与 HTTP 追踪（已由宿主脱敏），其余为 null
}
