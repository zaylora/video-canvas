package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	. "video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/pkg/pagination"
)

// fakePayAdmin 实现 handler.PaymentAdminService，记录收到的参数，可注入业务错误。
type fakePayAdmin struct {
	err      error
	actor    uint64
	key      string
	id       uint64
	no       string
	settings model.UpdatePaymentSettingsReq
	channel  model.UpdatePaymentChannelReq
	product  model.PaymentProductReq
	sortIDs  []uint64
	listReq  model.ListPaymentOrdersReq
}

func (f *fakePayAdmin) Settings(context.Context) (*model.PaymentSettingsView, error) {
	return &model.PaymentSettingsView{Enabled: true, PublicBaseURL: "https://a.com", MaxPending: 3}, f.err
}
func (f *fakePayAdmin) UpdateSettings(_ context.Context, actor uint64, r *model.UpdatePaymentSettingsReq) (*model.PaymentSettingsView, error) {
	f.actor, f.settings = actor, *r
	if f.err != nil {
		return nil, f.err
	}
	return &model.PaymentSettingsView{Enabled: r.Enabled, MaxPending: r.MaxPending}, nil
}
func (f *fakePayAdmin) ListChannels(context.Context) ([]model.PaymentChannelView, error) {
	return []model.PaymentChannelView{{Provider: "epay", Name: "易支付", Fields: []model.PaymentFieldView{{Key: "key", Secret: true, SecretSet: true}}}}, f.err
}
func (f *fakePayAdmin) UpdateChannel(_ context.Context, actor uint64, key string, r *model.UpdatePaymentChannelReq) (*model.PaymentChannelView, error) {
	f.actor, f.key, f.channel = actor, key, *r
	if f.err != nil {
		return nil, f.err
	}
	return &model.PaymentChannelView{Provider: key, Version: r.Version + 1}, nil
}
func (f *fakePayAdmin) chan3(actor uint64, key string) (*model.PaymentChannelView, error) {
	f.actor, f.key = actor, key
	if f.err != nil {
		return nil, f.err
	}
	return &model.PaymentChannelView{Provider: key}, nil
}
func (f *fakePayAdmin) ValidateChannel(_ context.Context, a uint64, k string) (*model.PaymentChannelView, error) {
	return f.chan3(a, k)
}
func (f *fakePayAdmin) EnableChannel(_ context.Context, a uint64, k string) (*model.PaymentChannelView, error) {
	return f.chan3(a, k)
}
func (f *fakePayAdmin) DisableChannel(_ context.Context, a uint64, k string) (*model.PaymentChannelView, error) {
	return f.chan3(a, k)
}
func (f *fakePayAdmin) ListProducts(context.Context) ([]model.RechargeProduct, error) {
	return []model.RechargeProduct{{ID: 1, Name: "A"}}, f.err
}
func (f *fakePayAdmin) CreateProduct(_ context.Context, actor uint64, r *model.PaymentProductReq) (*model.RechargeProduct, error) {
	f.actor, f.product = actor, *r
	if f.err != nil {
		return nil, f.err
	}
	return &model.RechargeProduct{ID: 5, Name: r.Name}, nil
}
func (f *fakePayAdmin) UpdateProduct(_ context.Context, actor, id uint64, r *model.PaymentProductReq) (*model.RechargeProduct, error) {
	f.actor, f.id, f.product = actor, id, *r
	if f.err != nil {
		return nil, f.err
	}
	return &model.RechargeProduct{ID: id, Name: r.Name}, nil
}
func (f *fakePayAdmin) DeleteProduct(_ context.Context, actor, id uint64) error {
	f.actor, f.id = actor, id
	return f.err
}
func (f *fakePayAdmin) SortProducts(_ context.Context, actor uint64, ids []uint64) error {
	f.actor, f.sortIDs = actor, ids
	return f.err
}
func (f *fakePayAdmin) ListOrders(_ context.Context, r *model.ListPaymentOrdersReq) ([]model.AdminPaymentOrderView, int64, pagination.Query, error) {
	f.listReq = *r
	return []model.AdminPaymentOrderView{{OrderNo: "VC1"}}, 1, pagination.Query{Page: 1, PageSize: 20}, f.err
}
func (f *fakePayAdmin) GetOrder(_ context.Context, no string) (*model.AdminPaymentOrderDetail, error) {
	f.no = no
	if f.err != nil {
		return nil, f.err
	}
	return &model.AdminPaymentOrderDetail{AdminPaymentOrderView: model.AdminPaymentOrderView{OrderNo: no}}, nil
}

type payOrderOps struct{ f *fakePayAdmin }

func (o payOrderOps) AdminQueryOrder(_ context.Context, actor uint64, no string) (*model.PaymentOrderView, error) {
	o.f.actor, o.f.no = actor, no
	if o.f.err != nil {
		return nil, o.f.err
	}
	return &model.PaymentOrderView{OrderNo: no, Status: "credited"}, nil
}
func (o payOrderOps) AdminCloseOrder(ctx context.Context, actor uint64, no string) (*model.PaymentOrderView, error) {
	return o.AdminQueryOrder(ctx, actor, no)
}

type payAdminEnv struct {
	router *gin.Engine
	svc    *fakePayAdmin
}

func newPayAdminEnv() *payAdminEnv {
	gin.SetMode(gin.TestMode)
	svc := &fakePayAdmin{}
	h := NewPaymentAdminHandler(svc, payOrderOps{svc})
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(middleware.CtxUserIDKey, uint(9)) })
	g := r.Group("/api/v1/admin/payment")
	g.GET("/settings", h.GetSettings)
	g.PUT("/settings", h.UpdateSettings)
	g.GET("/channels", h.ListChannels)
	g.PUT("/channels/:provider", h.UpdateChannel)
	g.POST("/channels/:provider/validate", h.ValidateChannel)
	g.POST("/channels/:provider/enable", h.EnableChannel)
	g.POST("/channels/:provider/disable", h.DisableChannel)
	g.GET("/products", h.ListProducts)
	g.POST("/products", h.CreateProduct)
	g.PUT("/products/sort", h.SortProducts)
	g.PUT("/products/:id", h.UpdateProduct)
	g.DELETE("/products/:id", h.DeleteProduct)
	g.GET("/orders", h.ListOrders)
	g.GET("/orders/:no", h.GetOrder)
	g.POST("/orders/:no/query", h.QueryOrder)
	g.POST("/orders/:no/close", h.CloseOrder)
	return &payAdminEnv{router: r, svc: svc}
}

func (e *payAdminEnv) do(method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/v1/admin/payment"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	return w
}

func TestPaymentAdminHandler_Settings(t *testing.T) {
	t.Run("读取", func(t *testing.T) {
		w := newPayAdminEnv().do(http.MethodGet, "/settings", "")
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"public_base_url":"https://a.com"`) {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	})
	t.Run("保存：当前管理员和参数传给 service", func(t *testing.T) {
		env := newPayAdminEnv()
		w := env.do(http.MethodPut, "/settings", `{"enabled":true,"public_base_url":"https://a.com","method_wechat":"wechat_native","method_alipay":"","max_pending_per_user":5}`)
		if w.Code != 200 || env.svc.actor != 9 || !env.svc.settings.Enabled || env.svc.settings.MethodWechat != "wechat_native" || env.svc.settings.MaxPending != 5 {
			t.Fatalf("%d %s %+v", w.Code, w.Body.String(), env.svc.settings)
		}
	})
	for name, body := range map[string]string{
		"上限为 0":   `{"enabled":true,"max_pending_per_user":0}`,
		"上限为 11":  `{"enabled":true,"max_pending_per_user":11}`,
		"不是 JSON": `x`,
	} {
		t.Run("参数错误："+name+" → 400 + 10001", func(t *testing.T) {
			w := newPayAdminEnv().do(http.MethodPut, "/settings", body)
			if w.Code != http.StatusBadRequest || payDecode(t, w).Code != errcode.ErrInvalidParams.Code {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
		})
	}
	t.Run("业务错误 65013 → 400", func(t *testing.T) {
		env := newPayAdminEnv()
		env.svc.err = errcode.ErrPayBaseURLMissing
		w := env.do(http.MethodPut, "/settings", `{"enabled":true,"max_pending_per_user":3}`)
		if w.Code != http.StatusBadRequest || payDecode(t, w).Code != 65013 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	})
}

func TestPaymentAdminHandler_Channels(t *testing.T) {
	t.Run("列表：响应里只有「是否已设置」，没有密钥值", func(t *testing.T) {
		w := newPayAdminEnv().do(http.MethodGet, "/channels", "")
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"secret_set":true`) || strings.Contains(w.Body.String(), `"secrets"`) {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	})
	t.Run("保存：渠道标识、版本、非敏感配置和密钥传给 service", func(t *testing.T) {
		env := newPayAdminEnv()
		w := env.do(http.MethodPut, "/channels/epay", `{"version":2,"config":{"pid":"1001"},"secrets":{"key":"abc"},"order_timeout_minutes":30}`)
		c := env.svc.channel
		if w.Code != 200 || env.svc.key != "epay" || env.svc.actor != 9 || c.Version != 2 || c.Config["pid"] != "1001" || c.Secrets["key"] != "abc" || c.TimeoutMinutes == nil || *c.TimeoutMinutes != 30 {
			t.Fatalf("%d %s %+v", w.Code, w.Body.String(), c)
		}
		if strings.Contains(w.Body.String(), "abc") {
			t.Fatalf("响应不能回显密钥：%s", w.Body.String())
		}
	})
	for name, body := range map[string]string{
		"超时太短":    `{"version":1,"order_timeout_minutes":3}`,
		"超时太长":    `{"version":1,"order_timeout_minutes":500}`,
		"版本为负":    `{"version":-1}`,
		"不是 JSON": `{`,
	} {
		t.Run("参数错误："+name+" → 400 + 10001", func(t *testing.T) {
			w := newPayAdminEnv().do(http.MethodPut, "/channels/epay", body)
			if w.Code != http.StatusBadRequest || payDecode(t, w).Code != errcode.ErrInvalidParams.Code {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
		})
	}
	for _, tt := range []struct {
		err  *errcode.Error
		code int
	}{{errcode.ErrPayVersionConflict, 409}, {errcode.ErrPayChannelLocked, 409}, {errcode.ErrPayChannelConfig, 400}} {
		t.Run("保存：业务错误按错误码返回 HTTP 状态", func(t *testing.T) {
			env := newPayAdminEnv()
			env.svc.err = tt.err
			w := env.do(http.MethodPut, "/channels/epay", `{"version":1}`)
			if w.Code != tt.code || payDecode(t, w).Code != tt.err.Code {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
		})
	}
	for _, action := range []string{"validate", "enable", "disable"} {
		t.Run(action+"：渠道标识传给 service", func(t *testing.T) {
			env := newPayAdminEnv()
			w := env.do(http.MethodPost, "/channels/xunhupay/"+action, "")
			if w.Code != 200 || env.svc.key != "xunhupay" || env.svc.actor != 9 {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
		})
	}
	t.Run("校验未通过 400 + 65009", func(t *testing.T) {
		env := newPayAdminEnv()
		env.svc.err = errcode.ErrPayChannelConfig.WithMsg("签名错误")
		w := env.do(http.MethodPost, "/channels/epay/validate", "")
		r := payDecode(t, w)
		if w.Code != 400 || r.Code != 65009 || !strings.Contains(r.Msg, "签名错误") {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	})
}

func TestPaymentAdminHandler_Products(t *testing.T) {
	body := `{"name":"20,000 积分","price_fen":20000,"base_credits":20000,"bonus_credits":1000,"badge":"热门","version":2}`
	t.Run("列表", func(t *testing.T) {
		if w := newPayAdminEnv().do(http.MethodGet, "/products", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"name":"A"`) {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	})
	t.Run("新建：201", func(t *testing.T) {
		env := newPayAdminEnv()
		w := env.do(http.MethodPost, "/products", body)
		if w.Code != http.StatusCreated || env.svc.actor != 9 || env.svc.product.PriceFen != 20000 || env.svc.product.BonusCredits != 1000 {
			t.Fatalf("%d %s %+v", w.Code, w.Body.String(), env.svc.product)
		}
	})
	t.Run("修改：路径 id 和版本传给 service", func(t *testing.T) {
		env := newPayAdminEnv()
		w := env.do(http.MethodPut, "/products/4", body)
		if w.Code != 200 || env.svc.id != 4 || env.svc.product.Version != 2 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	})
	t.Run("删除：204 风格的成功响应；有订单 409 + 65012", func(t *testing.T) {
		env := newPayAdminEnv()
		if w := env.do(http.MethodDelete, "/products/4", ""); w.Code != 200 || env.svc.id != 4 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		env.svc.err = errcode.ErrProductHasOrders
		if w := env.do(http.MethodDelete, "/products/4", ""); w.Code != 409 || payDecode(t, w).Code != 65012 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	})
	t.Run("排序：ids 传给 service；/sort 不会被当成 :id", func(t *testing.T) {
		env := newPayAdminEnv()
		w := env.do(http.MethodPut, "/products/sort", `{"ids":[3,1,2]}`)
		if w.Code != 200 || len(env.svc.sortIDs) != 3 || env.svc.sortIDs[0] != 3 || env.svc.id != 0 {
			t.Fatalf("%d %s %+v", w.Code, w.Body.String(), env.svc)
		}
	})
	bad := map[string]string{
		"名称为空":    `{"name":"","price_fen":100,"base_credits":100}`,
		"售价为 0":   `{"name":"x","price_fen":0,"base_credits":100}`,
		"基础积分为 0": `{"name":"x","price_fen":100,"base_credits":0}`,
		"赠送为负":    `{"name":"x","price_fen":100,"base_credits":100,"bonus_credits":-1}`,
		"角标过长":    `{"name":"x","price_fen":100,"base_credits":100,"badge":"123456789"}`,
	}
	for name, b := range bad {
		t.Run("参数错误："+name+" → 400 + 10001", func(t *testing.T) {
			w := newPayAdminEnv().do(http.MethodPost, "/products", b)
			if w.Code != http.StatusBadRequest || payDecode(t, w).Code != errcode.ErrInvalidParams.Code {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
		})
	}
	t.Run("排序 ids 为空 400", func(t *testing.T) {
		if w := newPayAdminEnv().do(http.MethodPut, "/products/sort", `{"ids":[]}`); w.Code != 400 {
			t.Fatalf("%d", w.Code)
		}
	})
	t.Run("路径 id 不是数字 400", func(t *testing.T) {
		if w := newPayAdminEnv().do(http.MethodPut, "/products/abc", body); w.Code != 400 {
			t.Fatalf("%d", w.Code)
		}
	})
}

func TestPaymentAdminHandler_Orders(t *testing.T) {
	t.Run("列表：分页结构；筛选参数原样传给 service", func(t *testing.T) {
		env := newPayAdminEnv()
		w := env.do(http.MethodGet, "/orders?q=VC1&status=pending&method=wechat&provider=epay&page=2&page_size=30", "")
		g := env.svc.listReq
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"total":1`) || g.Q != "VC1" || g.Status != "pending" || g.Method != "wechat" || g.Provider != "epay" || g.Page != 2 || g.PageSize != 30 {
			t.Fatalf("%d %s %+v", w.Code, w.Body.String(), g)
		}
	})
	t.Run("列表：状态值不合法 400 + 10001", func(t *testing.T) {
		w := newPayAdminEnv().do(http.MethodGet, "/orders?status=bogus", "")
		if w.Code != http.StatusBadRequest || payDecode(t, w).Code != errcode.ErrInvalidParams.Code {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	})
	t.Run("详情", func(t *testing.T) {
		env := newPayAdminEnv()
		w := env.do(http.MethodGet, "/orders/VC123", "")
		if w.Code != 200 || env.svc.no != "VC123" || !strings.Contains(w.Body.String(), "VC123") {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	})
	t.Run("详情：订单不存在 404 + 65006", func(t *testing.T) {
		env := newPayAdminEnv()
		env.svc.err = errcode.ErrPayOrderNotFound
		if w := env.do(http.MethodGet, "/orders/VC123", ""); w.Code != 404 || payDecode(t, w).Code != 65006 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	})
	for _, action := range []string{"query", "close"} {
		t.Run(action+"：当前管理员和订单号传给 service", func(t *testing.T) {
			env := newPayAdminEnv()
			w := env.do(http.MethodPost, "/orders/VC123/"+action, "")
			if w.Code != 200 || env.svc.actor != 9 || env.svc.no != "VC123" {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
		})
	}
	t.Run("渠道不支持查单 409 + 65008", func(t *testing.T) {
		env := newPayAdminEnv()
		env.svc.err = errcode.ErrPayQueryUnsupported
		if w := env.do(http.MethodPost, "/orders/VC123/query", ""); w.Code != 409 || payDecode(t, w).Code != 65008 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	})
}
