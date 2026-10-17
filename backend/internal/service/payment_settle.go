package service

import (
	"context"
	"errors"
	"fmt"

	"video-canvas/internal/model"
	"video-canvas/internal/payment"
	"video-canvas/internal/repository"
)

// settleTrade 是「渠道说这笔订单已付」的唯一汇合点：回调、用户主动查单、补单任务、管理员查单都调用它。
// 在一个事务里完成：核对状态、商户身份、金额、渠道交易号 → 写 recharge 流水 → 加余额 → 订单转 credited。
// 核对不通过时订单转 abnormal 并提交（outcome 说明原因，error 为 nil），由人工处理；只有数据库故障才返回 error，
// 此时整个事务回滚，调用方应让渠道重试（回调）或等下一轮（补单）。
//
// expectIdentity 是该订单支付方式对应的商户身份（appid / mchid / pid），通知里带了身份时必须一致。
func (s *PaymentService) settleTrade(ctx context.Context, o *model.PaymentOrder, res payment.TradeResult, expectIdentity string) (string, error) {
	// 事务外先读初始积分：老用户第一次充值时积分账户可能还没建，入账要先惰性创建它
	initial, err := s.credits.InitialCredits(ctx)
	if err != nil {
		return "", err
	}
	var outcome string
	err = s.orders.WithTx(ctx, func(tx PaymentTx) error {
		// 1. 行锁读最新订单：并发的回调、查单、补单在这里串行化，后到的看到 credited 直接当作重复
		cur, err := tx.LockOrder(ctx, o.OrderNo)
		if err != nil {
			return err
		}
		switch cur.Status {
		case model.PayOrderCredited:
			outcome = model.PayNotifyDuplicate
			return nil
		case model.PayOrderAbnormal:
			// 异常订单等人工处理，不被后续通知自动入账：否则「金额不符」的拦截形同虚设
			outcome = model.PayNotifyStateRejected
			return nil
		}
		// 2. 核对：商户身份 → 金额 → 渠道交易号。不通过就转 abnormal，并让事务提交这个状态
		if reason, outcomeOnFail := s.checkTrade(ctx, tx, cur, res, expectIdentity); reason != "" {
			outcome = outcomeOnFail
			return s.markAbnormal(ctx, tx, cur, reason)
		}
		// 3. 记账
		outcome = model.PayNotifyCredited
		return s.creditInTx(ctx, tx, cur, res, initial)
	})
	if err != nil {
		return "", err
	}
	// 4. 订单状态变了（入账或转异常）才推送；重复通知和被拒绝的通知不推送
	if outcome == model.PayNotifyCredited || changedToAbnormal(outcome) {
		if cur, err := s.reload(ctx, o.OrderNo); err == nil {
			s.publishOrder(ctx, cur)
		}
	}
	return outcome, nil
}

// changedToAbnormal 判断核对结果是不是「订单被转成了 abnormal」。
func changedToAbnormal(outcome string) bool {
	switch outcome {
	case model.PayNotifyAmountMismatch, model.PayNotifyIdentityMismatch, model.PayNotifyTradeNoConflict:
		return true
	}
	return false
}

// checkTrade 核对渠道报告的交易。通过返回 ("", "")；不通过返回异常原因和对应的通知结果。
func (s *PaymentService) checkTrade(ctx context.Context, tx PaymentTx, o *model.PaymentOrder, res payment.TradeResult, expectIdentity string) (reason, outcome string) {
	if expectIdentity != "" && res.Identity != "" && res.Identity != expectIdentity {
		return model.PayAbnormalIdentity, model.PayNotifyIdentityMismatch
	}
	if res.PaidFen != o.AmountFen {
		return model.PayAbnormalAmount, model.PayNotifyAmountMismatch
	}
	if res.TradeNo != "" {
		taken, err := tx.TradeNoTaken(ctx, o.Provider, res.TradeNo, o.ID)
		// 查询失败时保守处理：当作冲突转人工，比重复入账安全
		if err != nil || taken {
			return model.PayAbnormalTradeNo, model.PayNotifyTradeNoConflict
		}
	}
	return "", ""
}

// markAbnormal 把订单转为 abnormal 并记下原因；之后不会被自动入账，等人工在商户后台核实处理。
func (s *PaymentService) markAbnormal(ctx context.Context, tx PaymentTx, o *model.PaymentOrder, reason string) error {
	return tx.UpdateOrder(ctx, o.ID, map[string]any{
		"status": model.PayOrderAbnormal, "abnormal_reason": reason, "last_queried_at": s.now(), "updated_at": s.now(),
	})
}

// creditInTx 在事务里入账：惰性建账户 → 写 recharge 流水 → 加余额 → 商品已售 +1 → 订单转 credited。
// 流水用 (payment_order_id, type) 唯一索引防重：已存在说明之前入过账，只补订单状态，不再加余额。
func (s *PaymentService) creditInTx(ctx context.Context, tx PaymentTx, o *model.PaymentOrder, res payment.TradeResult, initial int) error {
	if err := tx.EnsureCredit(ctx, o.UserID, initial); err != nil {
		return err
	}
	inserted, err := tx.InsertLedger(ctx, &model.CreditLedger{
		UserID: o.UserID, Type: model.LedgerRecharge, Amount: o.Credits, PaymentOrderID: &o.ID,
		Note: fmt.Sprintf("充值 ¥%.2f（%s）", float64(o.AmountFen)/100, o.OrderNo), CreatedAt: s.now(),
	})
	if err != nil {
		return err
	}
	if inserted {
		if err := tx.AddCredit(ctx, o.UserID, o.Credits, 0); err != nil {
			return err
		}
		if err := tx.IncProductSold(ctx, o.ProductID); err != nil {
			return err
		}
	}
	now := s.now()
	paidAt := res.PaidAt
	if paidAt.IsZero() {
		paidAt = now
	}
	return tx.UpdateOrder(ctx, o.ID, map[string]any{
		"status": model.PayOrderCredited, "provider_trade_no": res.TradeNo, "paid_amount_fen": res.PaidFen,
		"paid_at": paidAt, "credited_at": now,
		"late_paid":  o.Status == model.PayOrderClosed, // 关单后才收到付款：照常入账，只是标记出来供运营留意
		"updated_at": now,
	})
}

// settlePaid 在渠道报告订单已付时入账，并返回入账后的最新订单；用于主动查单、补单和管理员查单。
func (s *PaymentService) settlePaid(ctx context.Context, o *model.PaymentOrder, lp *loadedProvider, res payment.TradeResult) (*model.PaymentOrder, error) {
	if _, err := s.settleTrade(ctx, o, res, lp.provider.Identity(lp.cfg, o.Method)); err != nil {
		return nil, err
	}
	return s.reload(ctx, o.OrderNo)
}

// queryUpstream 向渠道查单；结果里的订单号必须是被查的订单，否则当作渠道返回了错误的数据而拒绝。
func queryUpstream(ctx context.Context, lp *loadedProvider, o *model.PaymentOrder) (payment.TradeResult, error) {
	uctx, cancel := context.WithTimeout(ctx, payUpstreamTimeout)
	defer cancel()
	res, err := lp.provider.QueryOrder(uctx, lp.cfg, payment.OrderRef{OrderNo: o.OrderNo, Method: o.Method})
	if err != nil {
		return res, err
	}
	if res.OrderNo != "" && res.OrderNo != o.OrderNo {
		return res, errors.New("渠道返回了其它订单的查单结果")
	}
	return res, nil
}

// isNotFound 判断 repository 的未找到错误。
func isNotFound(err error) bool { return errors.Is(err, repository.ErrNotFound) }
