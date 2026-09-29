package model

// CreditView 是 GET /credits 的响应：可用积分 = 余额 - 冻结。
type CreditView struct {
	Balance   int `json:"balance"`   // 已结算后的余额（含冻结部分）
	Frozen    int `json:"frozen"`    // 进行中任务冻结的积分
	Available int `json:"available"` // 当前可用于提交新任务的积分
}
