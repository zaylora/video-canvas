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
)

// CreditLedger 积分流水。UNIQUE(task_id, type) 保证同一任务每种流水最多一条，天然幂等。
type CreditLedger struct {
	ID        uint64    `gorm:"primaryKey" json:"id"`                                                    // 流水 ID
	UserID    uint64    `gorm:"not null;index" json:"user_id"`                                           // 用户 ID
	TaskID    uint64    `gorm:"not null;uniqueIndex:uk_ledger_task_type,priority:1" json:"task_id"`      // 关联的生成任务 ID
	Type      string    `gorm:"size:16;not null;uniqueIndex:uk_ledger_task_type,priority:2" json:"type"` // 流水类型：freeze / settle / refund
	Amount    int       `gorm:"not null" json:"amount"`                                                  // 积分数量
	CreatedAt time.Time `json:"created_at"`                                                              // 创建时间
}

func (CreditLedger) TableName() string { return "credit_ledger" }
