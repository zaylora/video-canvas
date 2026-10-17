package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/payment"
	"video-canvas/internal/pkg/errcode"
)

func TestPaymentService_CheckOrder(t *testing.T) {
	ctx := context.Background()

	t.Run("渠道显示已付：入账并返回 credited", func(t *testing.T) {
		e := newPayEnv(t)
		v := e.order("idem-0001")
		e.wx.queryRes = payment.TradeResult{State: payment.StatePaid, TradeNo: "T-1", PaidFen: 10000}
		got, err := e.svc.CheckOrder(ctx, payTestUser, v.OrderNo)
		if err != nil || got.Status != model.PayOrderCredited || e.store.credits[payTestUser].Balance != 50+10500 {
			t.Fatalf("%+v %v", got, err)
		}
	})
	t.Run("渠道显示未付：订单保持 pending，并记下查单时间", func(t *testing.T) {
		e := newPayEnv(t)
		v := e.order("idem-0001")
		got, err := e.svc.CheckOrder(ctx, payTestUser, v.OrderNo)
		if err != nil || got.Status != model.PayOrderPending || e.store.orderByNo(v.OrderNo).LastQueriedAt == nil {
			t.Fatalf("%+v %v", got, err)
		}
	})
	t.Run("5 秒内重复查单返回 65014，不再请求渠道；过了 5 秒可以再查", func(t *testing.T) {
		e := newPayEnv(t)
		v := e.order("idem-0001")
		if _, err := e.svc.CheckOrder(ctx, payTestUser, v.OrderNo); err != nil {
			t.Fatal(err)
		}
		_, err := e.svc.CheckOrder(ctx, payTestUser, v.OrderNo)
		wantPayCode(t, err, errcode.ErrPayTooFrequent.Code)
		if e.wx.queryCalls != 1 {
			t.Fatalf("限频时不能请求渠道：%d", e.wx.queryCalls)
		}
		e.clock.Advance(6 * time.Second)
		if _, err := e.svc.CheckOrder(ctx, payTestUser, v.OrderNo); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("渠道不能查单：不请求渠道，原样返回订单", func(t *testing.T) {
		e := newPayEnv(t)
		e.wx.caps.CanQuery = false
		v := e.order("idem-0001")
		got, err := e.svc.CheckOrder(ctx, payTestUser, v.OrderNo)
		if err != nil || got.Status != model.PayOrderPending || e.wx.queryCalls != 0 {
			t.Fatalf("%+v %v 查单 %d 次", got, err, e.wx.queryCalls)
		}
	})
	t.Run("渠道查单失败：返回订单当前状态，不报错（用户可以稍后重试）", func(t *testing.T) {
		e := newPayEnv(t)
		v := e.order("idem-0001")
		e.wx.queryErr = errors.New("upstream down")
		got, err := e.svc.CheckOrder(ctx, payTestUser, v.OrderNo)
		if err != nil || got.Status != model.PayOrderPending {
			t.Fatalf("%+v %v", got, err)
		}
	})
	t.Run("别人的订单返回 65006", func(t *testing.T) {
		e := newPayEnv(t)
		v := e.order("idem-0001")
		_, err := e.svc.CheckOrder(ctx, payTestUser+1, v.OrderNo)
		wantPayCode(t, err, errcode.ErrPayOrderNotFound.Code)
	})
	t.Run("已入账的订单直接返回，不请求渠道", func(t *testing.T) {
		e := newPayEnv(t)
		v := e.order("idem-0001")
		e.paid(v.OrderNo, 10000)
		e.notify(payTestWechat)
		got, err := e.svc.CheckOrder(ctx, payTestUser, v.OrderNo)
		if err != nil || got.Status != model.PayOrderCredited || e.wx.queryCalls != 0 {
			t.Fatalf("%+v %v 查单 %d 次", got, err, e.wx.queryCalls)
		}
	})
}

func TestPaymentService_CancelOrder(t *testing.T) {
	ctx := context.Background()

	t.Run("先查单：未付 → 调用渠道关单 → closed(user)", func(t *testing.T) {
		e := newPayEnv(t)
		v := e.order("idem-0001")
		got, err := e.svc.CancelOrder(ctx, payTestUser, v.OrderNo)
		if err != nil || got.Status != model.PayOrderClosed || got.CloseReason != model.PayCloseUser {
			t.Fatalf("%+v %v", got, err)
		}
		if e.wx.queryCalls != 1 || e.wx.closeCalls != 1 {
			t.Fatalf("应先查单再关单：查 %d 关 %d", e.wx.queryCalls, e.wx.closeCalls)
		}
	})
	t.Run("查单发现已付：入账而不是关单", func(t *testing.T) {
		e := newPayEnv(t)
		v := e.order("idem-0001")
		e.wx.queryRes = payment.TradeResult{State: payment.StatePaid, TradeNo: "T-1", PaidFen: 10000}
		got, err := e.svc.CancelOrder(ctx, payTestUser, v.OrderNo)
		if err != nil || got.Status != model.PayOrderCredited || e.wx.closeCalls != 0 {
			t.Fatalf("取消时用户其实已付款，应入账：%+v %v 关单 %d 次", got, err, e.wx.closeCalls)
		}
	})
	t.Run("渠道不能在渠道侧关单：只在本地关单，不调用渠道关单", func(t *testing.T) {
		e := newPayEnv(t)
		e.wx.caps.CanCloseUpstream = false
		v := e.order("idem-0001")
		got, err := e.svc.CancelOrder(ctx, payTestUser, v.OrderNo)
		if err != nil || got.Status != model.PayOrderClosed || e.wx.closeCalls != 0 {
			t.Fatalf("%+v %v 关单 %d 次", got, err, e.wx.closeCalls)
		}
	})
	t.Run("渠道不能查单也不能关单：直接本地关单", func(t *testing.T) {
		e := newPayEnv(t)
		e.wx.caps.CanQuery, e.wx.caps.CanCloseUpstream = false, false
		v := e.order("idem-0001")
		got, err := e.svc.CancelOrder(ctx, payTestUser, v.OrderNo)
		if err != nil || got.Status != model.PayOrderClosed || e.wx.queryCalls != 0 || e.wx.closeCalls != 0 {
			t.Fatalf("%+v %v", got, err)
		}
	})
	t.Run("渠道关单失败：再查一次单，防止付款恰好发生在两次请求之间；仍未付则保持 pending 并报错", func(t *testing.T) {
		e := newPayEnv(t)
		v := e.order("idem-0001")
		e.wx.closeErr = errors.New("close failed")
		_, err := e.svc.CancelOrder(ctx, payTestUser, v.OrderNo)
		wantPayCode(t, err, errcode.ErrPayUpstream.Code)
		if e.wx.queryCalls != 2 || e.store.orderByNo(v.OrderNo).Status != model.PayOrderPending {
			t.Fatalf("关单失败后应复查：查单 %d 次，状态 %s", e.wx.queryCalls, e.store.orderByNo(v.OrderNo).Status)
		}
	})
	t.Run("渠道关单失败，但复查发现渠道侧已经关闭：视为关单成功，本地关单", func(t *testing.T) {
		e := newPayEnv(t)
		v := e.order("idem-0001")
		e.wx.closeErr = errors.New("trade status error")
		e.wx.queryMap = map[string]payment.TradeResult{} // 第一次查单：未付；复查时渠道已关闭
		calls := 0
		e.wx.queryHook = func(orderNo string) payment.TradeResult {
			calls++
			if calls == 1 {
				return payment.TradeResult{OrderNo: orderNo, State: payment.StatePending}
			}
			return payment.TradeResult{OrderNo: orderNo, State: payment.StateClosed}
		}
		got, err := e.svc.CancelOrder(ctx, payTestUser, v.OrderNo)
		if err != nil || got.Status != model.PayOrderClosed {
			t.Fatalf("渠道侧已关闭应本地关单：%+v %v", got, err)
		}
	})
	t.Run("非 pending 订单返回 65007", func(t *testing.T) {
		e := newPayEnv(t)
		v := e.order("idem-0001")
		e.paid(v.OrderNo, 10000)
		e.notify(payTestWechat)
		_, err := e.svc.CancelOrder(ctx, payTestUser, v.OrderNo)
		wantPayCode(t, err, errcode.ErrPayOrderState.Code)
	})
	t.Run("别人的订单返回 65006", func(t *testing.T) {
		e := newPayEnv(t)
		v := e.order("idem-0001")
		_, err := e.svc.CancelOrder(ctx, payTestUser+1, v.OrderNo)
		wantPayCode(t, err, errcode.ErrPayOrderNotFound.Code)
	})
}

func TestPaymentService_AdminOrderActions(t *testing.T) {
	ctx := context.Background()

	t.Run("管理员查单：已付则入账，并写审计", func(t *testing.T) {
		e := newPayEnv(t)
		v := e.order("idem-0001")
		e.wx.queryRes = payment.TradeResult{State: payment.StatePaid, TradeNo: "T-1", PaidFen: 10000}
		got, err := e.svc.AdminQueryOrder(ctx, 99, v.OrderNo)
		if err != nil || got.Status != model.PayOrderCredited {
			t.Fatalf("%+v %v", got, err)
		}
		if acts := e.audit.actions(); len(acts) != 1 || acts[0] != model.AdminAuditPayOrderQuery {
			t.Fatalf("审计不对：%v", acts)
		}
	})
	t.Run("管理员查单：渠道不能查单返回 65008", func(t *testing.T) {
		e := newPayEnv(t)
		e.wx.caps.CanQuery = false
		v := e.order("idem-0001")
		_, err := e.svc.AdminQueryOrder(ctx, 99, v.OrderNo)
		wantPayCode(t, err, errcode.ErrPayQueryUnsupported.Code)
	})
	t.Run("管理员查单：渠道请求失败返回 65015（和用户侧不同，管理员需要看到失败）", func(t *testing.T) {
		e := newPayEnv(t)
		v := e.order("idem-0001")
		e.wx.queryErr = errors.New("down")
		_, err := e.svc.AdminQueryOrder(ctx, 99, v.OrderNo)
		wantPayCode(t, err, errcode.ErrPayUpstream.Code)
	})
	t.Run("管理员关单：closed(admin) 并写审计；订单不存在返回 65006", func(t *testing.T) {
		e := newPayEnv(t)
		v := e.order("idem-0001")
		got, err := e.svc.AdminCloseOrder(ctx, 99, v.OrderNo)
		if err != nil || got.Status != model.PayOrderClosed || got.CloseReason != model.PayCloseAdmin {
			t.Fatalf("%+v %v", got, err)
		}
		if acts := e.audit.actions(); len(acts) != 1 || acts[0] != model.AdminAuditPayOrderClose {
			t.Fatalf("审计不对：%v", acts)
		}
		_, err = e.svc.AdminCloseOrder(ctx, 99, "VC-GHOST")
		wantPayCode(t, err, errcode.ErrPayOrderNotFound.Code)
	})
}

func TestPaymentService_RunMaintenance(t *testing.T) {
	ctx := context.Background()

	t.Run("到期未付：先查单，未付则关单(timeout)", func(t *testing.T) {
		e := newPayEnv(t)
		v := e.order("idem-0001")
		e.clock.Advance(16 * time.Minute)
		e.svc.RunMaintenance(ctx)
		o := e.store.orderByNo(v.OrderNo)
		if o.Status != model.PayOrderClosed || o.CloseReason != model.PayCloseTimeout || e.wx.queryCalls != 1 || e.wx.closeCalls != 1 {
			t.Fatalf("%+v 查 %d 关 %d", o, e.wx.queryCalls, e.wx.closeCalls)
		}
	})
	t.Run("到期但渠道显示已付（回调丢了）：入账而不是关单", func(t *testing.T) {
		e := newPayEnv(t)
		v := e.order("idem-0001")
		e.wx.queryRes = payment.TradeResult{State: payment.StatePaid, TradeNo: "T-1", PaidFen: 10000}
		e.clock.Advance(16 * time.Minute)
		e.svc.RunMaintenance(ctx)
		if o := e.store.orderByNo(v.OrderNo); o.Status != model.PayOrderCredited || e.wx.closeCalls != 0 {
			t.Fatalf("%+v", o)
		}
	})
	t.Run("没到期的订单：创建不满 2 分钟不查单", func(t *testing.T) {
		e := newPayEnv(t)
		e.order("idem-0001")
		e.clock.Advance(time.Minute)
		e.svc.RunMaintenance(ctx)
		if e.wx.queryCalls != 0 {
			t.Fatalf("不应查单：%d", e.wx.queryCalls)
		}
	})
	t.Run("没到期但创建超过 2 分钟：补查，已付就入账（兜住丢失的回调）", func(t *testing.T) {
		e := newPayEnv(t)
		v := e.order("idem-0001")
		e.wx.queryRes = payment.TradeResult{State: payment.StatePaid, TradeNo: "T-1", PaidFen: 10000}
		e.clock.Advance(3 * time.Minute)
		e.svc.RunMaintenance(ctx)
		if e.store.orderByNo(v.OrderNo).Status != model.PayOrderCredited {
			t.Fatal("应补单入账")
		}
	})
	t.Run("补查有间隔：2 分钟内不重复查同一笔订单", func(t *testing.T) {
		e := newPayEnv(t)
		e.order("idem-0001")
		e.clock.Advance(3 * time.Minute)
		e.svc.RunMaintenance(ctx)
		e.clock.Advance(30 * time.Second)
		e.svc.RunMaintenance(ctx)
		if e.wx.queryCalls != 1 {
			t.Fatalf("补查应有 2 分钟间隔：%d", e.wx.queryCalls)
		}
	})
	t.Run("不能查单的渠道：补查跳过；到期直接本地关单", func(t *testing.T) {
		e := newPayEnv(t)
		e.wx.caps.CanQuery, e.wx.caps.CanCloseUpstream = false, false
		v := e.order("idem-0001")
		e.clock.Advance(3 * time.Minute)
		e.svc.RunMaintenance(ctx)
		if e.wx.queryCalls != 0 || e.store.orderByNo(v.OrderNo).Status != model.PayOrderPending {
			t.Fatal("不能查单的渠道不补查")
		}
		e.clock.Advance(15 * time.Minute)
		e.svc.RunMaintenance(ctx)
		if e.store.orderByNo(v.OrderNo).Status != model.PayOrderClosed {
			t.Fatal("到期应关单")
		}
	})
	t.Run("下单中途崩溃留下的 creating 订单：渠道已付则入账，否则转 create_failed", func(t *testing.T) {
		e := newPayEnv(t)
		now := e.clock.Now()
		for i, no := range []string{"VC-STUCK-1", "VC-STUCK-2"} {
			e.store.orders[uint64(500+i)] = &model.PaymentOrder{
				ID: uint64(500 + i), OrderNo: no, UserID: payTestUser, ProductID: 1, AmountFen: 10000, Credits: 10500, Method: "wechat",
				Provider: model.ProviderWechatNative, Status: model.PayOrderCreating, CreatedAt: now, ExpiresAt: now.Add(15 * time.Minute),
			}
		}
		e.wx.queryMap = map[string]payment.TradeResult{"VC-STUCK-1": {State: payment.StatePaid, TradeNo: "T-1", PaidFen: 10000}}
		e.clock.Advance(3 * time.Minute)
		e.svc.RunMaintenance(ctx)
		if o := e.store.orderByNo("VC-STUCK-1"); o.Status != model.PayOrderCredited {
			t.Fatalf("渠道已付应入账：%+v", o)
		}
		// 第二笔：渠道没有付款记录（用户从没拿到过付款码），转 create_failed
		if o := e.store.orderByNo("VC-STUCK-2"); o.Status != model.PayOrderCreateFailed {
			t.Fatalf("没查到付款应转 create_failed：%+v", o)
		}
	})
}
