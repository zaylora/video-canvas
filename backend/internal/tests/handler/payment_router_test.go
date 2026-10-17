package handler_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"context"

	. "video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/payment"
	"video-canvas/internal/pkg/utils"
	"video-canvas/internal/router"
)

const payRouterSecret = "pay-router-secret"

// 用户 1 是普通用户，2 是运营（admin），3 是运维（super_admin）。
var payRouterRoles = map[uint64]string{1: model.RoleUser, 2: model.RoleAdmin, 3: model.RoleSuperAdmin}

// payRouterEnv 用真实 router（JWTAuth、RequireActive、RequireAdmin、RequireSuperAdmin 全都生效）加假 service 装配，
// 验证路由有没有注册冲突，以及每个接口要求的登录和角色。
type payRouterEnv struct {
	engine *gin.Engine
	tokens map[uint64]string
	user   *fakePayUser
	admin  *fakePayAdmin
}

func newPayRouterEnv(t *testing.T) *payRouterEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	u := &fakePayUser{ack: payment.Ack{Status: 200, ContentType: "text/plain; charset=utf-8", Body: "success"}}
	a := &fakePayAdmin{}
	stateLookup := func(_ context.Context, id uint64) (*middleware.UserState, error) {
		return &middleware.UserState{Role: payRouterRoles[id], Status: model.UserStatusActive}, nil
	}
	roleLookup := func(_ context.Context, id uint64) (string, error) { return payRouterRoles[id], nil }
	env := &payRouterEnv{tokens: map[uint64]string{}, user: u, admin: a}
	for id := range payRouterRoles {
		tok, _, err := utils.GenerateToken(uint(id), "u"+strconv.Itoa(int(id)), 0, payRouterSecret, "test", 1)
		if err != nil {
			t.Fatal(err)
		}
		env.tokens[id] = tok
	}
	env.engine = router.New(gin.TestMode, payRouterSecret, router.Handlers{
		Payment: NewPaymentHandler(u), AdminPayment: NewPaymentAdminHandler(a, payOrderOps{a}),
		AdminRole: roleLookup, UserState: stateLookup,
	})
	return env
}

// do 发请求；user 为 0 表示不带登录凭证。
func (e *payRouterEnv) do(method, path, body string, user uint64) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if user > 0 {
		req.Header.Set("Authorization", "Bearer "+e.tokens[user])
	}
	w := httptest.NewRecorder()
	e.engine.ServeHTTP(w, req)
	return w
}

func TestPaymentRouter_Auth(t *testing.T) {
	env := newPayRouterEnv(t) // 路由注册冲突会在这里 panic

	t.Run("渠道回调不需要登录：GET 和 POST 都能进到 handler", func(t *testing.T) {
		for _, m := range []string{http.MethodGet, http.MethodPost} {
			env.user.provider = ""
			w := env.do(m, "/api/v1/payment/notify/epay?a=1", "a=1", 0)
			if w.Code != 200 || w.Body.String() != "success" || env.user.provider != "epay" {
				t.Fatalf("%s：%d %q provider=%q", m, w.Code, w.Body.String(), env.user.provider)
			}
		}
	})

	userAPIs := []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/payment/store", ""},
		{http.MethodPost, "/api/v1/payment/orders", `{"product_id":1,"method":"wechat","idempotency_key":"idem-12345678"}`},
		{http.MethodGet, "/api/v1/payment/orders/pending", ""},
		{http.MethodGet, "/api/v1/payment/orders/VC1", ""},
		{http.MethodPost, "/api/v1/payment/orders/VC1/check", ""},
		{http.MethodPost, "/api/v1/payment/orders/VC1/close", ""},
	}
	for _, api := range userAPIs {
		t.Run("用户接口要登录："+api.method+" "+api.path, func(t *testing.T) {
			if w := env.do(api.method, api.path, api.body, 0); w.Code != http.StatusUnauthorized {
				t.Fatalf("未登录应 401：%d %s", w.Code, w.Body.String())
			}
			if w := env.do(api.method, api.path, api.body, 1); w.Code >= 400 {
				t.Fatalf("普通用户应能访问：%d %s", w.Code, w.Body.String())
			}
		})
	}
	t.Run("pending 是静态路径，不会被当成订单号", func(t *testing.T) {
		env.user.orderNo = ""
		env.do(http.MethodGet, "/api/v1/payment/orders/pending", "", 1)
		if env.user.orderNo != "" {
			t.Fatalf("pending 被当成订单号：%q", env.user.orderNo)
		}
	})
}

func TestPaymentRouter_AdminRoles(t *testing.T) {
	env := newPayRouterEnv(t)
	const base = "/api/v1/admin/payment"
	const channelBody = `{"version":0}`
	const settingsBody = `{"enabled":false,"max_pending_per_user":3}`
	const productBody = `{"name":"x","price_fen":100,"base_credits":100}`

	reads := []struct{ method, path, body string }{
		{http.MethodGet, base + "/settings", ""},
		{http.MethodGet, base + "/channels", ""},
		{http.MethodGet, base + "/products", ""},
		{http.MethodGet, base + "/orders", ""},
		{http.MethodGet, base + "/orders/VC1", ""},
	}
	// 商品和订单的写操作 admin 也可以做
	adminWrites := []struct{ method, path, body string }{
		{http.MethodPost, base + "/products", productBody},
		{http.MethodPut, base + "/products/1", productBody},
		{http.MethodPut, base + "/products/sort", `{"ids":[1]}`},
		{http.MethodDelete, base + "/products/1", ""},
		{http.MethodPost, base + "/orders/VC1/query", ""},
		{http.MethodPost, base + "/orders/VC1/close", ""},
	}
	// 全局设置和渠道配置的写操作只有 super_admin 能做：密钥和收款账户都在这里
	superOnly := []struct{ method, path, body string }{
		{http.MethodPut, base + "/settings", settingsBody},
		{http.MethodPut, base + "/channels/epay", channelBody},
		{http.MethodPost, base + "/channels/epay/validate", ""},
		{http.MethodPost, base + "/channels/epay/enable", ""},
		{http.MethodPost, base + "/channels/epay/disable", ""},
	}
	for _, api := range append(append(reads, adminWrites...), superOnly...) {
		t.Run("后台接口要登录："+api.method+" "+api.path, func(t *testing.T) {
			if w := env.do(api.method, api.path, api.body, 0); w.Code != http.StatusUnauthorized {
				t.Fatalf("未登录应 401：%d", w.Code)
			}
			if w := env.do(api.method, api.path, api.body, 1); w.Code != http.StatusForbidden {
				t.Fatalf("普通用户应 403：%d %s", w.Code, w.Body.String())
			}
		})
	}
	for _, api := range append(reads, adminWrites...) {
		t.Run("运营（admin）可访问："+api.method+" "+api.path, func(t *testing.T) {
			if w := env.do(api.method, api.path, api.body, 2); w.Code >= 400 {
				t.Fatalf("admin 应能访问：%d %s", w.Code, w.Body.String())
			}
		})
	}
	for _, api := range superOnly {
		t.Run("仅运维（super_admin）："+api.method+" "+api.path, func(t *testing.T) {
			if w := env.do(api.method, api.path, api.body, 2); w.Code != http.StatusForbidden {
				t.Fatalf("admin 应 403：%d %s", w.Code, w.Body.String())
			}
			if w := env.do(api.method, api.path, api.body, 3); w.Code >= 400 {
				t.Fatalf("super_admin 应能访问：%d %s", w.Code, w.Body.String())
			}
		})
	}
	t.Run("被 403 拒绝的写操作不会进到 service", func(t *testing.T) {
		env.admin.key = ""
		env.do(http.MethodPut, base+"/channels/epay", channelBody, 2)
		if env.admin.key != "" {
			t.Fatal("admin 的渠道写请求不应到达 service")
		}
	})
	t.Run("sort 是静态路径，不会被当成商品 id", func(t *testing.T) {
		env.admin.id = 0
		env.do(http.MethodPut, base+"/products/sort", `{"ids":[1,2]}`, 2)
		if env.admin.id != 0 || len(env.admin.sortIDs) != 2 {
			t.Fatalf("id=%d ids=%v", env.admin.id, env.admin.sortIDs)
		}
	})
}
