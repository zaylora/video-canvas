package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/payment"
	"video-canvas/internal/pkg/ws"
)

func TestPaymentService_HandleNotify_Credit(t *testing.T) {
	t.Run("已付：订单转 credited，写一条 recharge 流水并加余额，商品已售 +1，推送 WebSocket，应答成功", func(t *testing.T) {
		e := newPayEnv(t)
		v := e.order("idem-0001")
		e.paid(v.OrderNo, 10000)

		ack := e.notify(payTestWechat)
		if ack.Status != 200 {
			t.Fatalf("验签入账成功应答成功：%+v", ack)
		}
		o := e.store.orderByNo(v.OrderNo)
		if o.Status != model.PayOrderCredited || o.ProviderTradeNo != "T-"+v.OrderNo || o.PaidAmountFen != 10000 || o.CreditedAt == nil || o.LatePaid {
			t.Fatalf("订单状态不对：%+v", o)
		}
		// 账户不存在时按初始积分 50 惰性创建，再加 10500
		if c := e.store.credits[payTestUser]; c == nil || c.Balance != 50+10500 {
			t.Fatalf("余额应为 50 + 10500：%+v", c)
		}
		led := e.store.ledgerOf(o.ID)
		if len(led) != 1 || led[0].Type != model.LedgerRecharge || led[0].Amount != 10500 || led[0].UserID != payTestUser || led[0].TaskID != nil {
			t.Fatalf("应有且仅有一条 recharge 流水：%+v", led)
		}
		if e.store.products[1].SoldCount != 1 {
			t.Fatalf("已售笔数应 +1：%d", e.store.products[1].SoldCount)
		}
		if e.ws.count(ws.TypePaymentUpdated) != 1 {
			t.Fatalf("应推送一次 payment.updated：%d", e.ws.count(ws.TypePaymentUpdated))
		}
		if len(e.store.notifs) != 1 || e.store.notifs[0].Result != model.PayNotifyCredited || e.store.notifs[0].OrderNo != v.OrderNo {
			t.Fatalf("应记一条 credited 通知：%+v", e.store.notifs)
		}
	})

	t.Run("同一通知重复到达：积分只加一次，应答仍然成功", func(t *testing.T) {
		e := newPayEnv(t)
		v := e.order("idem-0001")
		e.paid(v.OrderNo, 10000)
		e.notify(payTestWechat)
		before := e.store.credits[payTestUser].Balance

		for i := 0; i < 3; i++ {
			if ack := e.notify(payTestWechat); ack.Status != 200 {
				t.Fatalf("重复通知也应答成功，否则渠道会一直重试：%+v", ack)
			}
		}
		o := e.store.orderByNo(v.OrderNo)
		if e.store.credits[payTestUser].Balance != before || len(e.store.ledgerOf(o.ID)) != 1 || e.store.products[1].SoldCount != 1 {
			t.Fatalf("重复通知不能再入账：余额 %d→%d 流水 %d", before, e.store.credits[payTestUser].Balance, len(e.store.ledgerOf(o.ID)))
		}
	})

	t.Run("并发 10 个通知：积分只加一次", func(t *testing.T) {
		e := newPayEnv(t)
		v := e.order("idem-0001")
		e.paid(v.OrderNo, 10000)
		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				e.notify(payTestWechat)
			}()
		}
		wg.Wait()
		if got := e.store.credits[payTestUser].Balance; got != 50+10500 {
			t.Fatalf("并发通知只能入账一次，余额 %d", got)
		}
	})

	t.Run("订单已被关闭后才收到已付通知：照样入账并标记迟到", func(t *testing.T) {
		e := newPayEnv(t)
		v := e.order("idem-0001")
		if _, err := e.svc.CancelOrder(context.Background(), payTestUser, v.OrderNo); err != nil {
			t.Fatal(err)
		}
		if e.store.orderByNo(v.OrderNo).Status != model.PayOrderClosed {
			t.Fatal("前置：订单应已关闭")
		}
		e.paid(v.OrderNo, 10000)
		e.notify(payTestWechat)
		o := e.store.orderByNo(v.OrderNo)
		if o.Status != model.PayOrderCredited || !o.LatePaid || e.store.credits[payTestUser].Balance != 50+10500 {
			t.Fatalf("迟到的付款也要入账（系统不做退款）：%+v", o)
		}
	})

	t.Run("下单请求超时导致 create_failed，但渠道其实建了单：付款后照样入账", func(t *testing.T) {
		e := newPayEnv(t)
		e.wx.createErr = errors.New("timeout")
		_, _ = e.svc.CreateOrder(context.Background(), payTestUser, &model.CreatePaymentOrderReq{ProductID: 1, Method: "wechat", IdempotencyKey: "idem-0001"}, "")
		var no string
		for _, o := range e.store.orders {
			no = o.OrderNo
		}
		e.paid(no, 10000)
		e.notify(payTestWechat)
		if o := e.store.orderByNo(no); o.Status != model.PayOrderCredited {
			t.Fatalf("用户确实付了钱，必须入账：%+v", o)
		}
	})

	t.Run("补发：通知到达时积分账户已存在，直接累加", func(t *testing.T) {
		e := newPayEnv(t)
		e.store.credits[payTestUser] = &model.UserCredit{UserID: payTestUser, Balance: 1000}
		v := e.order("idem-0001")
		e.paid(v.OrderNo, 10000)
		e.notify(payTestWechat)
		if got := e.store.credits[payTestUser].Balance; got != 11500 {
			t.Fatalf("余额应为 1000 + 10500：%d", got)
		}
	})
}

func TestPaymentService_HandleNotify_Rejects(t *testing.T) {
	t.Run("验签失败：应答失败，订单和积分都不变，也不记通知", func(t *testing.T) {
		e := newPayEnv(t)
		v := e.order("idem-0001")
		e.wx.verifyErr = payment.ErrVerify
		if ack := e.notify(payTestWechat); ack.Status == 200 {
			t.Fatalf("验签失败不能应答成功：%+v", ack)
		}
		if e.store.orderByNo(v.OrderNo).Status != model.PayOrderPending || len(e.store.credits) != 0 || len(e.store.notifs) != 0 {
			t.Fatal("验签失败不能改动任何数据")
		}
	})

	t.Run("未知渠道：应答失败", func(t *testing.T) {
		e := newPayEnv(t)
		if ack := e.notify("nope"); ack.Status == 200 {
			t.Fatalf("%+v", ack)
		}
	})

	t.Run("实付金额与应付不符：订单转 abnormal，不入账，写审计", func(t *testing.T) {
		e := newPayEnv(t)
		v := e.order("idem-0001")
		e.paid(v.OrderNo, 9999)
		if ack := e.notify(payTestWechat); ack.Status != 200 {
			t.Fatalf("已妥善处理（转人工），应答成功以免渠道重试：%+v", ack)
		}
		o := e.store.orderByNo(v.OrderNo)
		if o.Status != model.PayOrderAbnormal || o.AbnormalReason != model.PayAbnormalAmount || len(e.store.credits) != 0 || len(e.store.ledgerOf(o.ID)) != 0 {
			t.Fatalf("金额不符不能入账：%+v", o)
		}
		if len(e.store.notifs) != 1 || e.store.notifs[0].Result != model.PayNotifyAmountMismatch {
			t.Fatalf("应记 amount_mismatch：%+v", e.store.notifs)
		}
	})

	t.Run("通知里的商户身份与渠道配置不符：订单转 abnormal，不入账", func(t *testing.T) {
		e := newPayEnv(t)
		v := e.order("idem-0001")
		e.paid(v.OrderNo, 10000)
		e.wx.verifyRes.Identity = "someone-else"
		e.notify(payTestWechat)
		o := e.store.orderByNo(v.OrderNo)
		if o.Status != model.PayOrderAbnormal || o.AbnormalReason != model.PayAbnormalIdentity || len(e.store.credits) != 0 {
			t.Fatalf("%+v", o)
		}
	})

	t.Run("渠道交易号已被别的订单占用：订单转 abnormal，不入账", func(t *testing.T) {
		e := newPayEnv(t)
		a := e.order("idem-0001")
		b := e.order("idem-0002")
		e.paid(a.OrderNo, 10000)
		e.notify(payTestWechat)
		e.paid(b.OrderNo, 10000)
		e.wx.verifyRes.TradeNo = "T-" + a.OrderNo // 同一个渠道交易号想给第二个订单入账
		e.wx.verifyRes.EventKey = "evt-other"
		e.notify(payTestWechat)
		o := e.store.orderByNo(b.OrderNo)
		if o.Status != model.PayOrderAbnormal || o.AbnormalReason != model.PayAbnormalTradeNo || e.store.credits[payTestUser].Balance != 50+10500 {
			t.Fatalf("同一笔渠道交易不能给两个订单入账：%+v 余额 %d", o, e.store.credits[payTestUser].Balance)
		}
	})

	t.Run("订单号不存在：应答成功（避免渠道无限重试），不改任何数据", func(t *testing.T) {
		e := newPayEnv(t)
		e.paid("VC-GHOST", 10000)
		if ack := e.notify(payTestWechat); ack.Status != 200 {
			t.Fatalf("%+v", ack)
		}
		if len(e.store.credits) != 0 || len(e.store.notifs) != 1 || e.store.notifs[0].Result != model.PayNotifyNotFound {
			t.Fatalf("%+v", e.store.notifs)
		}
	})

	t.Run("通知表示未付款：不入账，应答成功", func(t *testing.T) {
		e := newPayEnv(t)
		v := e.order("idem-0001")
		e.wx.verifyRes = payment.TradeResult{OrderNo: v.OrderNo, State: payment.StateClosed, EventKey: "evt-closed"}
		if ack := e.notify(payTestWechat); ack.Status != 200 {
			t.Fatalf("%+v", ack)
		}
		if e.store.orderByNo(v.OrderNo).Status != model.PayOrderPending || len(e.store.credits) != 0 {
			t.Fatal("未付款的通知不能改订单")
		}
	})

	t.Run("abnormal 订单再收到已付通知：不自动入账（等人工处理）", func(t *testing.T) {
		e := newPayEnv(t)
		v := e.order("idem-0001")
		e.paid(v.OrderNo, 1)
		e.notify(payTestWechat)
		e.paid(v.OrderNo, 10000)
		e.wx.verifyRes.EventKey = "evt-again"
		e.notify(payTestWechat)
		if len(e.store.credits) != 0 {
			t.Fatal("异常订单不能被后续通知自动入账")
		}
	})

	t.Run("入账事务提交失败：应答失败让渠道重试，数据整体回滚", func(t *testing.T) {
		e := newPayEnv(t)
		v := e.order("idem-0001")
		e.paid(v.OrderNo, 10000)
		e.store.txErr = errors.New("commit failed")
		if ack := e.notify(payTestWechat); ack.Status == 200 {
			t.Fatalf("入账没成功不能应答成功：%+v", ack)
		}
		if e.store.orderByNo(v.OrderNo).Status != model.PayOrderPending || len(e.store.credits) != 0 || len(e.store.ledger) != 0 {
			t.Fatal("事务失败必须整体回滚")
		}
		e.store.txErr = nil
		if ack := e.notify(payTestWechat); ack.Status != 200 || e.store.credits[payTestUser].Balance != 50+10500 {
			t.Fatal("渠道重试后应成功入账")
		}
	})
}

func TestPaymentService_HandleNotify_Confirm(t *testing.T) {
	// MD5 签名的渠道（ConfirmNotify=true）：验签通过后再向渠道查一次单，确认已付才入账。
	setup := func(t *testing.T) (*payEnv, string) {
		e := newPayEnv(t)
		e.wx.caps.ConfirmNotify = true
		v := e.order("idem-0001")
		e.paid(v.OrderNo, 10000)
		return e, v.OrderNo
	}

	t.Run("查单确认已付：入账", func(t *testing.T) {
		e, no := setup(t)
		e.wx.queryRes = payment.TradeResult{OrderNo: no, TradeNo: "T-" + no, State: payment.StatePaid, PaidFen: 10000}
		if ack := e.notify(payTestWechat); ack.Status != 200 || e.wx.queryCalls != 1 {
			t.Fatalf("应查单一次并入账：%+v 查单 %d 次", ack, e.wx.queryCalls)
		}
		if e.store.orderByNo(no).Status != model.PayOrderCredited {
			t.Fatal("应入账")
		}
	})
	t.Run("查单显示未付（通知可能是伪造或乱序）：不入账，应答失败让渠道稍后重试", func(t *testing.T) {
		e, no := setup(t)
		e.wx.queryRes = payment.TradeResult{OrderNo: no, State: payment.StatePending}
		if ack := e.notify(payTestWechat); ack.Status == 200 {
			t.Fatalf("%+v", ack)
		}
		if e.store.orderByNo(no).Status != model.PayOrderPending || len(e.store.credits) != 0 {
			t.Fatal("查单没确认就不能入账")
		}
	})
	t.Run("查单失败：不入账，应答失败", func(t *testing.T) {
		e, no := setup(t)
		e.wx.queryErr = errors.New("upstream down")
		if ack := e.notify(payTestWechat); ack.Status == 200 || e.store.orderByNo(no).Status != model.PayOrderPending {
			t.Fatalf("%+v", ack)
		}
	})
	t.Run("查单金额与通知金额不一致：以查单结果为准，不符则转 abnormal", func(t *testing.T) {
		e, no := setup(t)
		e.wx.queryRes = payment.TradeResult{OrderNo: no, TradeNo: "T-" + no, State: payment.StatePaid, PaidFen: 100}
		e.notify(payTestWechat)
		if o := e.store.orderByNo(no); o.Status != model.PayOrderAbnormal {
			t.Fatalf("%+v", o)
		}
	})
}
