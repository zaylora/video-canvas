package model

// 后台用户管控接口（积分调整、并发上限、封禁 / 启用、批量）的请求与响应。

// 积分调整方式。
const (
	CreditModeAdd = "add" // 在可用积分上增加
	CreditModeSub = "sub" // 在可用积分上扣减
	CreditModeSet = "set" // 把可用积分设为指定值
)

// AdjustCreditsReq 是 POST /admin/users/:id/credits 的请求体。Amount 用指针：缺字段（nil）与 0 要区分开，
// 否则漏传 amount 的 set 会被悄悄当成“设为 0”。
type AdjustCreditsReq struct {
	Mode   string `json:"mode" binding:"required,oneof=add sub set" label:"调整方式"`   // add / sub / set
	Amount *int   `json:"amount" binding:"required,min=0,max=100000000" label:"数值"` // 整数 >= 0；add / sub 必须 > 0
	Note   string `json:"note" binding:"required,min=1,max=100" label:"备注"`         // 备注 1..100 字，写入流水与审计
}

// SetUserLimitReq 是 PUT /admin/users/:id/limits 的请求体：max_active_tasks 为 null 表示改回全局默认。
type SetUserLimitReq struct {
	MaxActiveTasks *int `json:"max_active_tasks" binding:"omitempty,min=1,max=64" label:"并发上限"` // 1..64 或 null
}

// SetUserStatusReq 是 PUT /admin/users/:id/status 的请求体。
type SetUserStatusReq struct {
	Status       string `json:"status" binding:"required,oneof=active disabled" label:"状态"` // active / disabled
	CancelActive bool   `json:"cancel_active"`                                              // 仅封禁时有效：取消进行中任务并退还冻结
}

// AdminStatusResult 是封禁 / 启用的响应。取消任务是逐个处理的，允许部分失败：已取消的不回滚，失败的在这里体现。
type AdminStatusResult struct {
	Status        string   `json:"status"`                 // 变更后的状态
	Canceled      int      `json:"canceled"`               // 已取消并退还冻结的任务数
	CancelFailed  int      `json:"cancel_failed"`          // 取消失败的任务数
	FailedTaskIDs []uint64 `json:"failed_task_ids"`        // 取消失败的任务 id（永远不是 null）
	CancelError   string   `json:"cancel_error,omitempty"` // 整体取消流程出错（如查询进行中任务失败）时的提示
}

// SetUserRoleReq 是 PUT /admin/users/:id/role 的请求体（仅 super_admin）。
type SetUserRoleReq struct {
	Role string `json:"role" binding:"required,oneof=user admin super_admin" label:"角色"` // user / admin / super_admin
}

// ResetPasswordReq 是 POST /admin/users/:id/reset-password 的请求体（仅 super_admin）。
type ResetPasswordReq struct {
	NewPassword string `json:"new_password" binding:"omitempty,min=6,max=128" label:"新密码"` // 缺省则由系统生成临时密码
}

// ResetPasswordView 是重置密码的响应。TempPassword 仅在这次响应里出现一次，库里只存 bcrypt 哈希，之后无法再查看。
// 管理员指定了新密码时返回的就是该值（便于前端统一展示“请告知用户”）。
type ResetPasswordView struct {
	TempPassword string `json:"temp_password"`
}

// BatchCreditsReq 是 POST /admin/users/batch/credits 的请求体（只增不减）。
type BatchCreditsReq struct {
	IDs    []uint64 `json:"ids" binding:"required,min=1,max=200,dive,min=1" label:"用户列表"` // 最多 200 个，重复的自动去重
	Amount int      `json:"amount" binding:"required,min=1,max=100000000" label:"数值"`     // 每人增加的积分，> 0
	Note   string   `json:"note" binding:"required,min=1,max=100" label:"备注"`             // 备注 1..100 字
}

// BatchStatusReq 是 PUT /admin/users/batch/status 的请求体（不支持取消任务）。
type BatchStatusReq struct {
	IDs    []uint64 `json:"ids" binding:"required,min=1,max=200,dive,min=1" label:"用户列表"` // 最多 200 个，重复的自动去重
	Status string   `json:"status" binding:"required,oneof=active disabled" label:"状态"`   // active / disabled
}

// BatchItem 是批量接口里一个用户的结果：失败（无权限、不存在、校验不过）只写进对应项，不整体失败。
type BatchItem struct {
	ID    uint64 `json:"id"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"` // 失败原因（可直接展示给用户）
}

// BatchResult 是批量接口的响应，顺序与（去重后的）请求 ids 一致。
type BatchResult struct {
	Results []BatchItem `json:"results"`
}

// 三个记录列表的查询参数（游标分页）。limit 缺省 20，超过 100 按 100。
type (
	// ListUserTasksReq 是 GET /admin/users/:id/tasks 的查询参数。
	ListUserTasksReq struct {
		Status string `form:"status" binding:"omitempty,oneof=all success failed running" label:"状态"` // all / success / failed / running
		Cursor string `form:"cursor" binding:"max=64" label:"游标"`
		Limit  int    `form:"limit" label:"条数"`
	}
	// ListUserLedgerReq 是 GET /admin/users/:id/ledger 的查询参数。
	ListUserLedgerReq struct {
		Type   string `form:"type" binding:"omitempty,oneof=all admin task" label:"类型"` // all / admin / task
		Cursor string `form:"cursor" binding:"max=64" label:"游标"`
		Limit  int    `form:"limit" label:"条数"`
	}
	// ListUserLoginsReq 是 GET /admin/users/:id/logins 的查询参数。
	ListUserLoginsReq struct {
		Result string `form:"result" binding:"omitempty,oneof=all fail" label:"结果"` // all / fail
		Cursor string `form:"cursor" binding:"max=64" label:"游标"`
		Limit  int    `form:"limit" label:"条数"`
	}
)
