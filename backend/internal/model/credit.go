package model

import "time"

// UserCredit 用户积分账户。可用余额 = Balance - Frozen。
type UserCredit struct {
	UserID    uint64    `gorm:"primaryKey" json:"user_id"`         // 用户 ID
	Balance   int       `gorm:"not null;default:0" json:"balance"` // 已结算后的余额（含冻结部分）
	Frozen    int       `gorm:"not null;default:0" json:"frozen"`  // 进行中任务冻结的积分
	UpdatedAt time.Time `json:"updated_at"`                        // 更新时间
}

func (UserCredit) TableName() string { return "user_credits" }

// 积分流水类型。
const (
	LedgerFreeze = "freeze" // 提交任务时冻结
	LedgerSettle = "settle" // 任务成功后结算（真正扣减）
	LedgerRefund = "refund" // 失败 / 取消 / 超时后退回冻结

	LedgerAdminAdjust = "admin_adjust" // 管理员手动调整（task_id 为空，带 operator_id 与 note）
	LedgerInitial     = "initial"      // 积分账户创建时的初始积分（注册或老用户惰性创建，task_id 为空）
)

// TaskIDPtr 把任务 id 包成 CreditLedger.TaskID 需要的指针（任务类流水必有 task_id）。
func TaskIDPtr(id uint64) *uint64 { return &id }

// CreditLedger 积分流水。(task_id, type) 在 task_id 非空时唯一（部分唯一索引），保证同一任务每种流水最多一条，天然幂等；
// 管理员调整与初始积分流水没有 task_id，不受该索引约束。
type CreditLedger struct {
	ID         uint64    `gorm:"primaryKey" json:"id"`                                                                              // 流水 ID
	UserID     uint64    `gorm:"not null;index" json:"user_id"`                                                                     // 用户 ID
	TaskID     *uint64   `gorm:"uniqueIndex:uk_ledger_task_type,priority:1,where:task_id IS NOT NULL" json:"task_id"`               // 关联的生成任务 ID；管理员调整 / 初始积分为空
	Type       string    `gorm:"size:16;not null;uniqueIndex:uk_ledger_task_type,priority:2,where:task_id IS NOT NULL" json:"type"` // 流水类型：freeze / settle / refund / admin_adjust / initial
	Amount     int       `gorm:"not null" json:"amount"`                                                                            // 积分数量
	OperatorID *uint64   `json:"operator_id"`                                                                                       // 操作人（仅管理员调整）
	Note       string    `gorm:"type:text;not null;default:''" json:"note"`                                                         // 备注（仅管理员调整）
	CreatedAt  time.Time `json:"created_at"`                                                                                        // 创建时间
}

func (CreditLedger) TableName() string { return "credit_ledger" }
