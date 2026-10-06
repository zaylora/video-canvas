package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
)

// SettleCallInput 是一次对话调用结束后的结算内容。
type SettleCallInput struct {
	CallID         uint64 // 调用 id
	UserID         uint64 // 调用所属用户，必须与记录一致
	InputTokens    int    // 输入 Token
	OutputTokens   int    // 输出 Token
	CachedTokens   int    // 输入里命中缓存的 Token
	UsageEstimated bool   // Token 数是估的（上游没给用量）
	Credits        int    // 按用量算出的应收积分，0 表示不收费
	Error          string // 调用失败或中断的原因（已脱敏），成功为空
}

// CreateModelCall 创建一条待结算的调用记录。
func (r *AgentRepository) CreateModelCall(ctx context.Context, c *model.AgentModelCall) error {
	return r.db.WithContext(ctx).Create(c).Error
}

// AvailableCredits 返回用户的可用积分（余额减冻结）；用户没有积分账户返回 ErrNotFound。
// 只用来在调用前做粗略预检，不加锁，真正的扣费以 SettleModelCall 里的行锁为准。
func (r *AgentRepository) AvailableCredits(ctx context.Context, userID uint64) (int, error) {
	var acc model.UserCredit
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).First(&acc).Error; err != nil {
		return 0, translate(err)
	}
	return acc.Balance - acc.Frozen, nil
}

// SettleModelCall 按实际用量结算一次调用：在一个事务里完成「标记已结算」「锁账户」「写流水」「扣余额」。
//
// 扣费规则：
//   - 调用结束后一次性扣，不预先冻结；成对写一条冻结流水和一条结算流水（金额相同、冻结余额不变），
//     这样后台对账的两个等式（余额 = 初始 + 调整 − 结算；冻结 = 冻结 − 结算 − 退款）都继续成立；
//   - 实扣额封顶在可用积分（余额减冻结）以内，不够时 Charged 小于 Credits，差额只记在调用上，
//     不会把余额扣成负数，也不会动进行中生成任务冻结的积分。
//
// 幂等：同一调用只会结算一次，重复调用直接返回第一次的结果。
// 调用不存在或不属于该用户返回 ErrNotFound；用户没有积分账户时不报错，实扣 0。
func (r *AgentRepository) SettleModelCall(ctx context.Context, in SettleCallInput) (*model.AgentModelCall, error) {
	var out model.AgentModelCall
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. 调用必须是该用户的；用 CAS（pending → settled）抢结算权，重复结算的那个拿不到
		if err := tx.Where("id = ? AND user_id = ?", in.CallID, in.UserID).First(&out).Error; err != nil {
			return translate(err)
		}
		now := time.Now()
		res := tx.Model(&model.AgentModelCall{}).Where("id = ? AND status = ?", in.CallID, model.ModelCallPending).
			Updates(map[string]any{"status": model.ModelCallSettled, "settled_at": now})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return tx.First(&out, in.CallID).Error // 已经结算过：原样返回，不再扣费
		}
		// 2. 锁账户并算实扣额；加锁让同一用户的并发结算排队，不丢更新
		charged, err := chargeCredits(tx, in)
		if err != nil {
			return err
		}
		// 3. 把用量、应收、实扣写回调用记录
		fields := map[string]any{
			"input_tokens": in.InputTokens, "output_tokens": in.OutputTokens, "cached_tokens": in.CachedTokens,
			"usage_estimated": in.UsageEstimated, "credits": in.Credits, "charged": charged, "error": in.Error,
		}
		if err := tx.Model(&model.AgentModelCall{}).Where("id = ?", in.CallID).Updates(fields).Error; err != nil {
			return err
		}
		return tx.First(&out, in.CallID).Error
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// chargeCredits 锁住账户，按可用积分封顶扣费并写成对流水，返回实扣额。
func chargeCredits(tx *gorm.DB, in SettleCallInput) (int, error) {
	if in.Credits <= 0 {
		return 0, nil
	}
	var acc model.UserCredit
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", in.UserID).First(&acc).Error
	if errors.Is(translate(err), ErrNotFound) {
		return 0, nil // 没有账户就没有可扣的积分；调用前的预检会挡住这种用户，这里只是兜底
	}
	if err != nil {
		return 0, err
	}
	charged := min(in.Credits, max(acc.Balance-acc.Frozen, 0))
	if charged == 0 {
		return 0, nil
	}
	for _, typ := range []string{model.LedgerFreeze, model.LedgerSettle} {
		row := &model.CreditLedger{UserID: in.UserID, AgentCallID: &in.CallID, Type: typ, Amount: charged}
		res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(row)
		if res.Error != nil {
			return 0, res.Error
		}
		if res.RowsAffected == 0 {
			return 0, nil // 流水已存在说明这次调用已经扣过，不再扣
		}
	}
	err = tx.Model(&model.UserCredit{}).Where("user_id = ?", in.UserID).
		UpdateColumn("balance", gorm.Expr("balance - ?", charged)).Error
	return charged, err
}
