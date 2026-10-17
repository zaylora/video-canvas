package service

import (
	"context"

	"video-canvas/internal/model"
	"video-canvas/internal/payment"
	"video-canvas/internal/pkg/errcode"
)

// queryAndSettle 向渠道查单：已付就入账。先记下查单时间用于限频（即使这次查询失败也算一次），
// 返回最新订单；渠道请求失败返回 error，订单保持原状。
func (s *PaymentService) queryAndSettle(ctx context.Context, o *model.PaymentOrder, lp *loadedProvider) (*model.PaymentOrder, error) {
	if _, err := s.orders.UpdateOrderIf(ctx, o.ID, []string{model.PayOrderPending},
		map[string]any{"last_queried_at": s.now(), "updated_at": s.now()}); err != nil {
		return nil, err
	}
	res, err := queryUpstream(ctx, lp, o)
	if err != nil {
		return nil, err
	}
	if res.State == payment.StatePaid {
		return s.settlePaid(ctx, o, lp, res)
	}
	return s.reload(ctx, o.OrderNo)
}

// CheckOrder 是用户点「我已完成支付」（或从付款标签页切回）时的主动查单：渠道显示已付就入账。
// 同一笔订单 5 秒内只查一次；渠道不能查单或查询失败时原样返回订单，用户可以稍后再点。
func (s *PaymentService) CheckOrder(ctx context.Context, userID uint64, orderNo string) (*model.PaymentOrderView, error) {
	// 1. 只能查自己的订单；已经不是 pending 的订单不需要再问渠道
	o, err := s.orders.FindUserOrder(ctx, userID, orderNo)
	if isNotFound(err) {
		return nil, errcode.ErrPayOrderNotFound
	}
	if err != nil {
		return nil, err
	}
	if o.Status != model.PayOrderPending {
		return orderView(o), nil
	}
	// 2. 限频：防止用户连点把渠道的查单接口打爆
	if o.LastQueriedAt != nil && s.now().Sub(*o.LastQueriedAt) < payCheckMinInterval {
		return nil, errcode.ErrPayTooFrequent
	}
	lp, err := s.loadProvider(ctx, o.Provider)
	if err != nil {
		return nil, err
	}
	if !lp.provider.Capabilities().CanQuery {
		return orderView(o), nil
	}
	// 3. 查单；失败不报错，返回当前状态，等回调或补单任务兜底
	cur, err := s.queryAndSettle(ctx, o, lp)
	if err != nil {
		warnSwallowed("用户主动查单失败", orderNo, err)
		return orderView(o), nil
	}
	return orderView(cur), nil
}

// CancelOrder 用户取消订单：先查单再关单，查到已付就入账而不是关单。
func (s *PaymentService) CancelOrder(ctx context.Context, userID uint64, orderNo string) (*model.PaymentOrderView, error) {
	o, err := s.orders.FindUserOrder(ctx, userID, orderNo)
	if isNotFound(err) {
		return nil, errcode.ErrPayOrderNotFound
	}
	if err != nil {
		return nil, err
	}
	if o.Status != model.PayOrderPending {
		return nil, errcode.ErrPayOrderState
	}
	cur, err := s.closeOrder(ctx, o, model.PayCloseUser)
	if err != nil {
		return nil, err
	}
	return orderView(cur), nil
}

// closeOrder 是用户取消、管理员关单、超时共用的关单流程：
// 1. 能查单的渠道先查单，已付就入账（不关单）；
// 2. 能在渠道侧关单的渠道调用关单接口，失败时再查一次单：付款可能恰好发生在两次请求之间；
// 3. 最后在本地把订单转 closed。
// 不能在渠道侧关单的渠道只在本地关，之后用户仍可能付款：迟到的付款通知照常入账（见 settleTrade）。
// 渠道请求失败返回 65015，订单保持 pending，由调用方（用户重试或下一轮补单）再处理。
func (s *PaymentService) closeOrder(ctx context.Context, o *model.PaymentOrder, reason string) (*model.PaymentOrder, error) {
	lp, err := s.loadProvider(ctx, o.Provider)
	if err != nil {
		return nil, err
	}
	caps := lp.provider.Capabilities()
	if caps.CanQuery {
		res, err := queryUpstream(ctx, lp, o)
		if err != nil {
			warnSwallowed("关单前查单失败", o.OrderNo, err)
			return nil, errcode.ErrPayUpstream
		}
		if res.State == payment.StatePaid {
			return s.settlePaid(ctx, o, lp, res)
		}
	}
	if caps.CanCloseUpstream {
		if cur, err := s.closeUpstream(ctx, o, lp, caps); err != nil || cur != nil {
			return cur, err
		}
	}
	now := s.now()
	updated, err := s.orders.UpdateOrderIf(ctx, o.ID, []string{model.PayOrderPending}, map[string]any{
		"status": model.PayOrderClosed, "closed_at": now, "close_reason": reason, "updated_at": now,
	})
	if err != nil {
		return nil, err
	}
	cur, err := s.reload(ctx, o.OrderNo)
	if err == nil && updated {
		s.publishOrder(ctx, cur)
	}
	return cur, err
}

// closeUpstream 在渠道侧关单。成功返回 (nil, nil) 让调用方继续本地关单；失败时能查单就再查一次：
// 已付则入账并返回最新订单（调用方不再关单）；渠道侧已经是关闭或不存在，等同关单成功，同样返回 (nil, nil)；
// 其余情况（仍未付、复查也失败）返回 65015，订单保持 pending。
func (s *PaymentService) closeUpstream(ctx context.Context, o *model.PaymentOrder, lp *loadedProvider, caps payment.Capabilities) (*model.PaymentOrder, error) {
	uctx, cancel := context.WithTimeout(ctx, payUpstreamTimeout)
	defer cancel()
	cerr := lp.provider.CloseOrder(uctx, lp.cfg, payment.OrderRef{OrderNo: o.OrderNo, Method: o.Method})
	if cerr == nil {
		return nil, nil
	}
	warnSwallowed("渠道关单失败", o.OrderNo, cerr)
	if caps.CanQuery {
		res, qerr := queryUpstream(ctx, lp, o)
		switch {
		case qerr != nil:
		case res.State == payment.StatePaid:
			return s.settlePaid(ctx, o, lp, res)
		case res.State == payment.StateClosed || res.State == payment.StateNotExist:
			return nil, nil
		}
	}
	return nil, errcode.ErrPayUpstream
}

// AdminQueryOrder 管理员在订单页点「查单」：向渠道确认状态，已付就入账（即手动补单，但以渠道结果为准）。写审计。
func (s *PaymentService) AdminQueryOrder(ctx context.Context, actorID uint64, orderNo string) (*model.PaymentOrderView, error) {
	o, err := s.reload(ctx, orderNo)
	if err != nil {
		return nil, err
	}
	lp, err := s.loadProvider(ctx, o.Provider)
	if err != nil {
		return nil, err
	}
	if !lp.provider.Capabilities().CanQuery {
		return nil, errcode.ErrPayQueryUnsupported
	}
	cur, err := s.queryAndSettle(ctx, o, lp)
	if err != nil {
		warnSwallowed("管理员查单失败", orderNo, err)
		return nil, errcode.ErrPayUpstream
	}
	adminAudit(ctx, s.audit, actorID, model.AdminAuditPayOrderQuery, model.AdminAuditTargetPayment, o.ID,
		map[string]any{"order_no": orderNo, "status": cur.Status})
	return orderView(cur), nil
}

// AdminCloseOrder 管理员关单：只能关 pending 订单；流程与用户取消相同（先查单再关单）。写审计。
func (s *PaymentService) AdminCloseOrder(ctx context.Context, actorID uint64, orderNo string) (*model.PaymentOrderView, error) {
	o, err := s.reload(ctx, orderNo)
	if err != nil {
		return nil, err
	}
	if o.Status != model.PayOrderPending {
		return nil, errcode.ErrPayOrderState
	}
	cur, err := s.closeOrder(ctx, o, model.PayCloseAdmin)
	if err != nil {
		return nil, err
	}
	adminAudit(ctx, s.audit, actorID, model.AdminAuditPayOrderClose, model.AdminAuditTargetPayment, o.ID,
		map[string]any{"order_no": orderNo, "status": cur.Status})
	return orderView(cur), nil
}
