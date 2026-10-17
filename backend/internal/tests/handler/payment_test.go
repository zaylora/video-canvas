package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	. "video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/payment"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/pkg/pagination"
)

// ---------- 用户侧 + 回调 + 后台的 fake service ----------

type fakePayUser struct {
	err      error // 非空时业务方法返回它
	userID   uint64
	orderNo  string
	createIn model.CreatePaymentOrderReq
	ip       string
	notified payment.NotifyRequest
	provider string
	ack      payment.Ack
	pending  *model.PaymentOrderView
}

func (f *fakePayUser) Store(context.Context) (*model.PaymentStoreView, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &model.PaymentStoreView{Enabled: true, Products: []model.PaymentProductView{{ID: 1, Name: "10,000 积分", PriceFen: 10000, BaseCredits: 10000}}, Methods: []model.PaymentMethodView{{Method: "wechat", Available: true}}}, nil
}

func (f *fakePayUser) CreateOrder(_ context.Context, userID uint64, req *model.CreatePaymentOrderReq, ip string) (*model.PaymentOrderView, error) {
	f.userID, f.createIn, f.ip = userID, *req, ip
	if f.err != nil {
		return nil, f.err
	}
	return &model.PaymentOrderView{OrderNo: "VC1", Status: "pending", Checkout: &model.PaymentCheckoutView{Mode: "qrcode", Value: "weixin://x"}}, nil
}

func (f *fakePayUser) order(userID uint64, no string) (*model.PaymentOrderView, error) {
	f.userID, f.orderNo = userID, no
	if f.err != nil {
		return nil, f.err
	}
	return &model.PaymentOrderView{OrderNo: no, Status: "pending"}, nil
}

func (f *fakePayUser) GetOrder(_ context.Context, u uint64, no string) (*model.PaymentOrderView, error) {
	return f.order(u, no)
}
func (f *fakePayUser) CheckOrder(_ context.Context, u uint64, no string) (*model.PaymentOrderView, error) {
	return f.order(u, no)
}
func (f *fakePayUser) CancelOrder(_ context.Context, u uint64, no string) (*model.PaymentOrderView, error) {
	return f.order(u, no)
}
func (f *fakePayUser) PendingOrder(_ context.Context, u uint64) (*model.PaymentOrderView, error) {
	f.userID = u
	if f.err != nil {
		return nil, f.err
	}
	return f.pending, nil
}

func (f *fakePayUser) HandleNotify(_ context.Context, provider string, req payment.NotifyRequest) payment.Ack {
	f.provider, f.notified = provider, req
	return f.ack
}

type payHandlerEnv struct {
	router *gin.Engine
	svc    *fakePayUser
}

func newPayHandlerEnv() *payHandlerEnv {
	gin.SetMode(gin.TestMode)
	svc := &fakePayUser{ack: payment.Ack{Status: 200, ContentType: "text/plain; charset=utf-8", Body: "success"}}
	h := NewPaymentHandler(svc)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(middleware.CtxUserIDKey, uint(7)) })
	r.GET("/api/v1/payment/store", h.Store)
	r.POST("/api/v1/payment/orders", h.CreateOrder)
	r.GET("/api/v1/payment/orders/pending", h.PendingOrder)
	r.GET("/api/v1/payment/orders/:no", h.GetOrder)
	r.POST("/api/v1/payment/orders/:no/check", h.CheckOrder)
	r.POST("/api/v1/payment/orders/:no/close", h.CancelOrder)
	r.Any("/api/v1/payment/notify/:provider", h.Notify)
	return &payHandlerEnv{router: r, svc: svc}
}

func (e *payHandlerEnv) do(method, path, body string, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	return w
}

type payAPIResp struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func payDecode(t *testing.T, w *httptest.ResponseRecorder) payAPIResp {
	t.Helper()
	var r payAPIResp
	if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil {
		t.Fatalf("响应不是 JSON：%v %s", err, w.Body.String())
	}
	return r
}

func TestPaymentHandler_Store(t *testing.T) {
	env := newPayHandlerEnv()
	w := env.do(http.MethodGet, "/api/v1/payment/store", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"enabled":true`) || !strings.Contains(w.Body.String(), "10,000 积分") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

func TestPaymentHandler_CreateOrder(t *testing.T) {
	t.Run("成功：当前用户和客户端 IP 传给 service，返回 201 和收银信息", func(t *testing.T) {
		env := newPayHandlerEnv()
		w := env.do(http.MethodPost, "/api/v1/payment/orders", `{"product_id":1,"method":"wechat","idempotency_key":"idem-12345678"}`)
		if w.Code != http.StatusCreated || !strings.Contains(w.Body.String(), "weixin://x") {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		if env.svc.userID != 7 || env.svc.createIn.ProductID != 1 || env.svc.createIn.Method != "wechat" || env.svc.createIn.IdempotencyKey != "idem-12345678" || env.svc.ip == "" {
			t.Fatalf("参数没传对：%+v ip=%q", env.svc, env.svc.ip)
		}
	})
	bad := map[string]string{
		"缺商品":     `{"method":"wechat","idempotency_key":"idem-12345678"}`,
		"支付方式不合法": `{"product_id":1,"method":"paypal","idempotency_key":"idem-12345678"}`,
		"幂等键太短":   `{"product_id":1,"method":"wechat","idempotency_key":"abc"}`,
		"幂等键太长":   `{"product_id":1,"method":"wechat","idempotency_key":"` + strings.Repeat("x", 65) + `"}`,
		"不是 JSON": `not json`,
	}
	for name, body := range bad {
		t.Run("参数错误："+name+" → 400 + 10001", func(t *testing.T) {
			w := newPayHandlerEnv().do(http.MethodPost, "/api/v1/payment/orders", body)
			if w.Code != http.StatusBadRequest || payDecode(t, w).Code != errcode.ErrInvalidParams.Code {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
		})
	}
	t.Run("业务错误按错误码返回对应的 HTTP 状态", func(t *testing.T) {
		for _, e := range []*errcode.Error{errcode.ErrPaymentDisabled, errcode.ErrProductUnavailable, errcode.ErrTooManyPendingOrder, errcode.ErrPayCreateFailed} {
			env := newPayHandlerEnv()
			env.svc.err = e
			w := env.do(http.MethodPost, "/api/v1/payment/orders", `{"product_id":1,"method":"wechat","idempotency_key":"idem-12345678"}`)
			if w.Code != e.HTTPStatus() || payDecode(t, w).Code != e.Code {
				t.Fatalf("%d：%d %s", e.Code, w.Code, w.Body.String())
			}
		}
	})
}

func TestPaymentHandler_OrderActions(t *testing.T) {
	cases := []struct{ name, method, path string }{
		{"查询", http.MethodGet, "/api/v1/payment/orders/VC123"},
		{"主动查单", http.MethodPost, "/api/v1/payment/orders/VC123/check"},
		{"取消", http.MethodPost, "/api/v1/payment/orders/VC123/close"},
	}
	for _, c := range cases {
		t.Run(c.name+"：订单号和当前用户传给 service", func(t *testing.T) {
			env := newPayHandlerEnv()
			w := env.do(c.method, c.path, "")
			if w.Code != 200 || env.svc.orderNo != "VC123" || env.svc.userID != 7 {
				t.Fatalf("%d %s %+v", w.Code, w.Body.String(), env.svc)
			}
		})
		t.Run(c.name+"：订单不存在 404 + 65006", func(t *testing.T) {
			env := newPayHandlerEnv()
			env.svc.err = errcode.ErrPayOrderNotFound
			w := env.do(c.method, c.path, "")
			if w.Code != http.StatusNotFound || payDecode(t, w).Code != errcode.ErrPayOrderNotFound.Code {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
		})
	}
	t.Run("主动查单太频繁 429 + 65014", func(t *testing.T) {
		env := newPayHandlerEnv()
		env.svc.err = errcode.ErrPayTooFrequent
		w := env.do(http.MethodPost, "/api/v1/payment/orders/VC123/check", "")
		if w.Code != http.StatusTooManyRequests || payDecode(t, w).Code != errcode.ErrPayTooFrequent.Code {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	})
	t.Run("订单号超长 400", func(t *testing.T) {
		w := newPayHandlerEnv().do(http.MethodGet, "/api/v1/payment/orders/"+strings.Repeat("A", 80), "")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%d", w.Code)
		}
	})
}

func TestPaymentHandler_PendingOrder(t *testing.T) {
	t.Run("没有进行中的订单：data 是 null", func(t *testing.T) {
		env := newPayHandlerEnv()
		w := env.do(http.MethodGet, "/api/v1/payment/orders/pending", "")
		r := payDecode(t, w)
		if w.Code != 200 || r.Code != 0 || strings.TrimSpace(string(r.Data)) != "null" {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	})
	t.Run("有：返回订单", func(t *testing.T) {
		env := newPayHandlerEnv()
		env.svc.pending = &model.PaymentOrderView{OrderNo: "VC9", Status: "pending"}
		if w := env.do(http.MethodGet, "/api/v1/payment/orders/pending", ""); !strings.Contains(w.Body.String(), "VC9") {
			t.Fatalf("%s", w.Body.String())
		}
	})
	t.Run("静态路径 pending 优先于 :no", func(t *testing.T) {
		env := newPayHandlerEnv()
		env.do(http.MethodGet, "/api/v1/payment/orders/pending", "")
		if env.svc.orderNo != "" {
			t.Fatalf("pending 被当成订单号了：%q", env.svc.orderNo)
		}
	})
}

func TestPaymentHandler_Notify(t *testing.T) {
	t.Run("POST：渠道、方法、请求头、原始请求体原样交给 service，应答格式由渠道决定", func(t *testing.T) {
		env := newPayHandlerEnv()
		env.svc.ack = payment.Ack{Status: 200, ContentType: "application/json", Body: `{"code":"SUCCESS"}`}
		w := env.do(http.MethodPost, "/api/v1/payment/notify/wechat_native", `{"id":"EV1"}`, "Wechatpay-Signature", "sig-1")
		if w.Code != 200 || w.Body.String() != `{"code":"SUCCESS"}` || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
			t.Fatalf("应答没按渠道格式：%d %q %q", w.Code, w.Body.String(), w.Header().Get("Content-Type"))
		}
		n := env.svc.notified
		if env.svc.provider != "wechat_native" || n.Method != http.MethodPost || string(n.Body) != `{"id":"EV1"}` || n.Header.Get("Wechatpay-Signature") != "sig-1" {
			t.Fatalf("%+v provider=%s", n, env.svc.provider)
		}
	})
	t.Run("GET：查询参数交给 service（易支付用 GET 回调）", func(t *testing.T) {
		env := newPayHandlerEnv()
		w := env.do(http.MethodGet, "/api/v1/payment/notify/epay?out_trade_no=VC1&money=100.00", "")
		if w.Code != 200 || w.Body.String() != "success" || env.svc.notified.Query.Get("out_trade_no") != "VC1" || env.svc.notified.Method != http.MethodGet {
			t.Fatalf("%d %q %+v", w.Code, w.Body.String(), env.svc.notified)
		}
	})
	t.Run("service 说失败：把渠道要的失败应答原样回给渠道，渠道才会重试", func(t *testing.T) {
		env := newPayHandlerEnv()
		env.svc.ack = payment.Ack{Status: 500, ContentType: "application/json", Body: `{"code":"FAIL"}`}
		if w := env.do(http.MethodPost, "/api/v1/payment/notify/wechat_native", `{}`); w.Code != 500 || w.Body.String() != `{"code":"FAIL"}` {
			t.Fatalf("%d %q", w.Code, w.Body.String())
		}
	})
	t.Run("请求体超过 64KB：拒绝，不交给 service（防止被灌大包）", func(t *testing.T) {
		env := newPayHandlerEnv()
		w := env.do(http.MethodPost, "/api/v1/payment/notify/epay", strings.Repeat("a", 65*1024))
		if w.Code == 200 || env.svc.provider != "" {
			t.Fatalf("超大请求不应被处理：%d provider=%q", w.Code, env.svc.provider)
		}
	})
	t.Run("渠道标识格式不对：拒绝", func(t *testing.T) {
		env := newPayHandlerEnv()
		w := env.do(http.MethodPost, "/api/v1/payment/notify/"+strings.Repeat("x", 80), `{}`)
		if w.Code == 200 || env.svc.provider != "" {
			t.Fatalf("%d", w.Code)
		}
	})
	t.Run("回调不需要登录，也不读取登录用户", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		svc := &fakePayUser{ack: payment.Ack{Status: 200, Body: "success"}}
		r := gin.New() // 没有任何登录中间件
		r.POST("/n/:provider", NewPaymentHandler(svc).Notify)
		req := httptest.NewRequest(http.MethodPost, "/n/epay", bytes.NewReader([]byte("a=1")))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 200 || svc.provider != "epay" {
			t.Fatalf("%d", w.Code)
		}
	})
}

var _ = errors.New
var _ = pagination.Query{}
