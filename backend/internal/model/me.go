package model

import "time"

// 个人中心 /api/v1/me/* 的请求与响应（docs/design/个人中心/个人中心.md §8.2）。

// MeView 是当前用户的资料（GET /me、PATCH /me、头像接口的响应）。
type MeView struct {
	ID              uint64     `json:"id"`
	Username        string     `json:"username"`
	Nickname        string     `json:"nickname"`
	Email           string     `json:"email"`
	Role            string     `json:"role"`
	AvatarURL       string     `json:"avatar_url"` // /files/<avatar_key>，没有头像时为空串
	CreatedAt       time.Time  `json:"created_at"`
	EmailVerifiedAt *time.Time `json:"email_verified_at"`
}

// UpdateMeReq 是 PATCH /me 的请求体。Nickname 用指针区分“没传”和“清空”：没传按参数错误处理，空串表示清空。
type UpdateMeReq struct {
	Nickname *string `json:"nickname" label:"昵称"` // 去掉首尾空白后 0..32 个字符，不能含控制字符 / 换行（service 校验）
}

// ChangePasswordReq 是 PUT /me/password 的请求体。新密码的 8..72 字节等规则由 service 统一校验。
type ChangePasswordReq struct {
	OldPassword string `json:"old_password" binding:"required,max=128" label:"当前密码"`
	NewPassword string `json:"new_password" binding:"required,max=128" label:"新密码"`
}

// MeStatsView 是 GET /me/stats 的响应：口径与后台 TaskStats 一致（排除试跑）。
type MeStatsView struct {
	Total        int   `json:"total"`         // 正式任务总数
	Success      int   `json:"success"`       // 成功数
	Failed       int   `json:"failed"`        // 失败数（failed + expired）
	Last7d       int   `json:"last7d"`        // 近 7 天创建的任务数
	SpentCredits int64 `json:"spent_credits"` // 已结算的积分合计
	CanvasCount  int64 `json:"canvas_count"`  // 未删除的画布数
}

// MeActivityReq 是 GET /me/activity 的查询参数。
type MeActivityReq struct {
	TZ   string `form:"tz" binding:"max=64" label:"时区"` // 浏览器 IANA 时区，非法时回落 Asia/Shanghai
	Year int    `form:"year" label:"年份"`                // 0 表示最近一年
}

// ActivityDay 是热力图某一天的计数（按用户时区的日期分桶）。
type ActivityDay struct {
	Date  string `json:"date"` // YYYY-MM-DD
	Count int    `json:"count"`
	Image int    `json:"image"`
	Video int    `json:"video"`
	Audio int    `json:"audio"`
	Text  int    `json:"text"`
}

// MeActivityView 是 GET /me/activity 的响应。Start / End 为闭区间，Days 只含 count>0 的日子。
type MeActivityView struct {
	TZ    string        `json:"tz"` // 实际使用的时区
	Start string        `json:"start"`
	End   string        `json:"end"`
	Years []int         `json:"years"` // 注册年份到今年
	Total int           `json:"total"`
	Days  []ActivityDay `json:"days"`
}

// MeLedgerReq 是 GET /me/credits/ledger 的查询参数。Page / PageSize 用指针区分“没传”（取默认）和“传了非法值”（10001）。
type MeLedgerReq struct {
	Type     string `form:"type" binding:"omitempty,oneof=all task admin" label:"类型"` // all / task / admin
	Page     *int   `form:"page" label:"页码"`                                          // 从 1 开始，默认 1
	PageSize *int   `form:"page_size" label:"每页条数"`                                   // 只允许 10 / 20 / 50，默认 20
}

// MeLedgerItem 是用户侧积分流水的一行：不含操作人。Amount 与后台流水同口径（带符号的“对可用积分的影响”）。
type MeLedgerItem struct {
	ID          uint64    `json:"id"`
	Type        string    `json:"type"`
	Amount      int       `json:"amount"`
	TaskID      *uint64   `json:"task_id"`
	AgentCallID *uint64   `json:"agent_call_id"`
	Note        string    `json:"note"` // 只有管理员调整时非空，向用户展示
	CreatedAt   time.Time `json:"created_at"`
}

// MeLedgerPage 是 GET /me/credits/ledger 的响应（页码分页）。
type MeLedgerPage struct {
	Items    []MeLedgerItem `json:"items"`
	Total    int64          `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
}
