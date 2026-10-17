package service

import (
	"context"

	"video-canvas/internal/model"
	"video-canvas/internal/payment"
)

// RunMaintenance 跑一轮补单与超时处理，由后台定时器每 30 秒调用一次。三件事互相独立，一件失败不影响其它：
//  1. 到期的 pending 订单：先查单再关单（已付就入账）；
//  2. 创建超过 2 分钟还没结果的 pending 订单：向渠道补查，兜住丢失的回调；
//  3. 下单中途进程崩溃留下的 creating 订单：渠道已付就入账，否则转 create_failed。
//
// 多个后端实例同时跑也是安全的：每一步都是行锁或 CAS，最多重复向渠道查几次单。
func (s *PaymentService) RunMaintenance(ctx context.Context) {
	s.closeDueOrders(ctx)
	s.recheckUnconfirmed(ctx)
	s.recoverStuckCreating(ctx)
}

// closeDueOrders 处理到期订单。关单失败（渠道请求失败）的订单保持 pending，下一轮再来。
func (s *PaymentService) closeDueOrders(ctx context.Context) {
	due, err := s.orders.ListDueOrders(ctx, s.now(), payBatchLimit)
	if err != nil {
		warnSwallowed("补单任务：读取到期订单失败", "", err)
		return
	}
	for i := range due {
		if _, err := s.closeOrder(ctx, &due[i], model.PayCloseTimeout); err != nil {
			warnSwallowed("补单任务：到期关单失败", due[i].OrderNo, err)
		}
	}
}

// recheckUnconfirmed 补查还没结果的订单。两次补查之间也隔 payRecheckAfter，不能查单的渠道直接跳过。
func (s *PaymentService) recheckUnconfirmed(ctx context.Context) {
	cutoff := s.now().Add(-payRecheckAfter)
	list, err := s.orders.ListUnconfirmedOrders(ctx, cutoff, cutoff, payBatchLimit)
	if err != nil {
		warnSwallowed("补单任务：读取待补查订单失败", "", err)
		return
	}
	for i := range list {
		o := &list[i]
		lp, err := s.loadProvider(ctx, o.Provider)
		if err != nil {
			warnSwallowed("补单任务：装载渠道失败", o.OrderNo, err)
			continue
		}
		if !lp.provider.Capabilities().CanQuery {
			continue
		}
		if _, err := s.queryAndSettle(ctx, o, lp); err != nil {
			warnSwallowed("补单任务：补查失败", o.OrderNo, err)
		}
	}
}

// recoverStuckCreating 处理停在 creating 的订单：用户从没拿到过付款码，所以没查到付款就直接转 create_failed。
// 但渠道可能已经建了单且付了款（下单请求超时、渠道其实成功了），所以能查单的渠道先查。
func (s *PaymentService) recoverStuckCreating(ctx context.Context) {
	list, err := s.orders.ListStuckCreating(ctx, s.now().Add(-payRecheckAfter), payBatchLimit)
	if err != nil {
		warnSwallowed("补单任务：读取卡住的订单失败", "", err)
		return
	}
	for i := range list {
		s.recoverOne(ctx, &list[i])
	}
}

// recoverOne 处理一笔卡在 creating 的订单。
func (s *PaymentService) recoverOne(ctx context.Context, o *model.PaymentOrder) {
	if lp, err := s.loadProvider(ctx, o.Provider); err == nil && lp.provider.Capabilities().CanQuery {
		res, qerr := queryUpstream(ctx, lp, o)
		if qerr != nil {
			warnSwallowed("补单任务：卡住订单查单失败", o.OrderNo, qerr) // 查不到就不能断言没付款，留到下一轮
			return
		}
		if res.State == payment.StatePaid {
			if _, err := s.settlePaid(ctx, o, lp, res); err != nil {
				warnSwallowed("补单任务：卡住订单入账失败", o.OrderNo, err)
			}
			return
		}
	}
	_, err := s.orders.UpdateOrderIf(ctx, o.ID, []string{model.PayOrderCreating}, map[string]any{
		"status": model.PayOrderCreateFailed, "fail_reason": "下单未完成", "updated_at": s.now(),
	})
	if err != nil {
		warnSwallowed("补单任务：卡住订单转失败出错", o.OrderNo, err)
	}
}
