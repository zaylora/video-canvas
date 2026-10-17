package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
)

func wantPayCode(t *testing.T, err error, code int) {
	t.Helper()
	var e *errcode.Error
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("期望错误码 %d，实际：%v", code, err)
	}
}

func TestPaymentService_CreateOrder(t *testing.T) {
	ctx := context.Background()

	t.Run("成功：落库 pending、锁定商品和渠道快照、用商户订单号向渠道下单", func(t *testing.T) {
		e := newPayEnv(t)
		v, err := e.svc.CreateOrder(ctx, payTestUser, &model.CreatePaymentOrderReq{ProductID: 1, Method: model.PayMethodWechat, IdempotencyKey: "idem-0001"}, "1.2.3.4")
		if err != nil {
			t.Fatal(err)
		}
		if v.Status != model.PayOrderPending || v.AmountFen != 10000 || v.Credits != 10500 || v.Method != model.PayMethodWechat {
			t.Fatalf("订单视图不对：%+v", v)
		}
		if v.Checkout == nil || v.Checkout.Mode != model.CheckoutQRCode || v.Checkout.Value != "weixin://pay/wechat_native" {
			t.Fatalf("pending 订单必须带收银信息：%+v", v.Checkout)
		}
		if want := e.clock.Now().Add(15 * time.Minute); !v.ExpiresAt.Equal(want) {
			t.Fatalf("到期时间应为下单 + 渠道超时 15 分钟：%v", v.ExpiresAt)
		}
		if !strings.HasPrefix(v.OrderNo, "VC") || len(v.OrderNo) > 32 {
			t.Fatalf("订单号格式不对：%q", v.OrderNo)
		}
		o := e.store.orderByNo(v.OrderNo)
		if o.ChannelVersion != 3 || o.Provider != model.ProviderWechatNative || o.UserID != payTestUser || o.ProductID != 1 || !strings.Contains(string(o.ProductSnapshot), `"price_fen":10000`) {
			t.Fatalf("订单快照不对：%+v", o)
		}
		req := e.wx.createReq[0]
		if req.OrderNo != v.OrderNo || req.AmountFen != 10000 || req.Method != "wechat" || req.ClientIP != "1.2.3.4" ||
			req.NotifyURL != payTestBase+"/api/v1/payment/notify/wechat_native" || !strings.HasPrefix(req.ReturnURL, payTestBase+"/pay/return?order_no=") {
			t.Fatalf("向渠道下单的参数不对：%+v", req)
		}
	})

	t.Run("同一个幂等键重复提交返回同一笔订单，不再向渠道下单", func(t *testing.T) {
		e := newPayEnv(t)
		a := e.order("idem-0001")
		b := e.order("idem-0001")
		if a.OrderNo != b.OrderNo || len(e.wx.createReq) != 1 || len(e.store.orders) != 1 {
			t.Fatalf("应只有一笔订单：%s %s 下单 %d 次 订单 %d 笔", a.OrderNo, b.OrderNo, len(e.wx.createReq), len(e.store.orders))
		}
	})

	tests := []struct {
		name     string
		setup    func(e *payEnv)
		req      model.CreatePaymentOrderReq
		wantCode int
	}{
		{"支付总开关关闭返回 65001", func(e *payEnv) { e.kv.kv[model.SettingPaymentEnabled] = "false" }, model.CreatePaymentOrderReq{ProductID: 1, Method: "wechat", IdempotencyKey: "idem-0001"}, errcode.ErrPaymentDisabled.Code},
		{"商品不存在返回 65003", nil, model.CreatePaymentOrderReq{ProductID: 99, Method: "wechat", IdempotencyKey: "idem-0001"}, errcode.ErrProductUnavailable.Code},
		{"商品已下架返回 65003", nil, model.CreatePaymentOrderReq{ProductID: 2, Method: "wechat", IdempotencyKey: "idem-0001"}, errcode.ErrProductUnavailable.Code},
		{"支付方式没有指定承接渠道返回 65002", func(e *payEnv) { e.kv.kv[model.SettingPaymentMethodWechat] = "" }, model.CreatePaymentOrderReq{ProductID: 1, Method: "wechat", IdempotencyKey: "idem-0001"}, errcode.ErrPayMethodUnavail.Code},
		{"承接渠道已停用返回 65002", func(e *payEnv) { e.store.channels[model.ProviderWechatNative].Enabled = false }, model.CreatePaymentOrderReq{ProductID: 1, Method: "wechat", IdempotencyKey: "idem-0001"}, errcode.ErrPayMethodUnavail.Code},
		{"承接渠道不支持该支付方式返回 65002", func(e *payEnv) { e.kv.kv[model.SettingPaymentMethodAlipay] = model.ProviderWechatNative }, model.CreatePaymentOrderReq{ProductID: 1, Method: "alipay", IdempotencyKey: "idem-0001"}, errcode.ErrPayMethodUnavail.Code},
		{"没有配置公网地址返回 65013", func(e *payEnv) { e.kv.kv[model.SettingPaymentPublicBaseURL] = "" }, model.CreatePaymentOrderReq{ProductID: 1, Method: "wechat", IdempotencyKey: "idem-0001"}, errcode.ErrPayBaseURLMissing.Code},
		{"未支付订单达到上限返回 65004", func(e *payEnv) {
			for _, k := range []string{"idem-a001", "idem-a002", "idem-a003"} {
				e.order(k)
			}
		}, model.CreatePaymentOrderReq{ProductID: 1, Method: "wechat", IdempotencyKey: "idem-0001"}, errcode.ErrTooManyPendingOrder.Code},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newPayEnv(t)
			if tt.setup != nil {
				tt.setup(e)
			}
			_, err := e.svc.CreateOrder(ctx, payTestUser, &tt.req, "")
			wantPayCode(t, err, tt.wantCode)
		})
	}

	t.Run("渠道下单失败：订单转 create_failed 并记录脱敏后的原因，返回 65005", func(t *testing.T) {
		e := newPayEnv(t)
		e.wx.createErr = errors.New("gateway timeout app_secret=abc123")
		_, err := e.svc.CreateOrder(ctx, payTestUser, &model.CreatePaymentOrderReq{ProductID: 1, Method: "wechat", IdempotencyKey: "idem-0001"}, "")
		wantPayCode(t, err, errcode.ErrPayCreateFailed.Code)
		if len(e.store.orders) != 1 {
			t.Fatalf("失败的订单也要留痕：%d", len(e.store.orders))
		}
		for _, o := range e.store.orders {
			if o.Status != model.PayOrderCreateFailed || o.FailReason == "" {
				t.Fatalf("应转 create_failed 并写原因：%+v", o)
			}
		}
	})

	t.Run("create_failed 的订单不占未支付名额", func(t *testing.T) {
		e := newPayEnv(t)
		e.wx.createErr = errors.New("boom")
		for _, k := range []string{"idem-a001", "idem-a002", "idem-a003", "idem-a004"} {
			_, err := e.svc.CreateOrder(ctx, payTestUser, &model.CreatePaymentOrderReq{ProductID: 1, Method: "wechat", IdempotencyKey: k}, "")
			wantPayCode(t, err, errcode.ErrPayCreateFailed.Code)
		}
	})
}

func TestPaymentService_Store(t *testing.T) {
	ctx := context.Background()

	t.Run("返回上架商品和两种支付方式的可用状态", func(t *testing.T) {
		e := newPayEnv(t)
		v, err := e.svc.Store(ctx)
		if err != nil || !v.Enabled || len(v.Products) != 1 || v.Products[0].ID != 1 || v.Products[0].BonusCredits != 500 {
			t.Fatalf("%+v %v", v, err)
		}
		if len(v.Methods) != 2 || !v.Methods[0].Available || !v.Methods[1].Available {
			t.Fatalf("两种方式都应可用：%+v", v.Methods)
		}
	})
	t.Run("方式不可用时带原因，而不是隐藏", func(t *testing.T) {
		e := newPayEnv(t)
		e.kv.kv[model.SettingPaymentMethodAlipay] = ""
		e.store.channels[model.ProviderWechatNative].Enabled = false
		v, _ := e.svc.Store(ctx)
		for _, m := range v.Methods {
			if m.Available || m.Reason == "" {
				t.Fatalf("应不可用并说明原因：%+v", m)
			}
		}
	})
	t.Run("支付总开关关闭时 enabled=false，不返回商品", func(t *testing.T) {
		e := newPayEnv(t)
		e.kv.kv[model.SettingPaymentEnabled] = "false"
		v, err := e.svc.Store(ctx)
		if err != nil || v.Enabled || len(v.Products) != 0 {
			t.Fatalf("%+v %v", v, err)
		}
	})
}

func TestPaymentService_GetAndPendingOrder(t *testing.T) {
	ctx := context.Background()

	t.Run("只能查自己的订单，别人的订单一律返回 65006", func(t *testing.T) {
		e := newPayEnv(t)
		o := e.order("idem-0001")
		if v, err := e.svc.GetOrder(ctx, payTestUser, o.OrderNo); err != nil || v.OrderNo != o.OrderNo {
			t.Fatalf("%+v %v", v, err)
		}
		_, err := e.svc.GetOrder(ctx, payTestUser+1, o.OrderNo)
		wantPayCode(t, err, errcode.ErrPayOrderNotFound.Code)
		_, err = e.svc.GetOrder(ctx, payTestUser, "VC-NOT-EXIST")
		wantPayCode(t, err, errcode.ErrPayOrderNotFound.Code)
	})
	t.Run("PendingOrder 返回最近一笔进行中的订单，没有返回 nil", func(t *testing.T) {
		e := newPayEnv(t)
		if v, err := e.svc.PendingOrder(ctx, payTestUser); err != nil || v != nil {
			t.Fatalf("没有订单应返回 nil：%+v %v", v, err)
		}
		e.order("idem-0001")
		second := e.order("idem-0002")
		v, err := e.svc.PendingOrder(ctx, payTestUser)
		if err != nil || v == nil || v.OrderNo != second.OrderNo || v.Checkout == nil {
			t.Fatalf("应返回最近一笔且带收银信息：%+v %v", v, err)
		}
	})
	t.Run("非 pending 订单的视图不带收银信息", func(t *testing.T) {
		e := newPayEnv(t)
		o := e.order("idem-0001")
		e.paid(o.OrderNo, 10000)
		e.notify(payTestWechat)
		v, _ := e.svc.GetOrder(ctx, payTestUser, o.OrderNo)
		if v.Status != model.PayOrderCredited || v.Checkout != nil || v.CreditedAt == nil {
			t.Fatalf("%+v", v)
		}
	})
}
