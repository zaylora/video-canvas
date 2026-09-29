package middleware_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	. "video-canvas/internal/middleware"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
)

// adminTestRouter 组装：测试中间件模拟登录（userID 为 0 表示没经过 JWT）→ RequireAdmin → 一个返回 200 的接口。
func adminTestRouter(userID uint, lookup RoleLookup) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if userID != 0 {
			c.Set(CtxUserIDKey, userID)
		}
		c.Next()
	})
	r.GET("/admin", RequireAdmin(lookup), func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	return r
}

func TestRequireAdmin(t *testing.T) {
	tests := []struct {
		name       string
		userID     uint
		lookup     RoleLookup
		wantStatus int
		wantCode   int
	}{
		{"管理员放行", 1, func(context.Context, uint64) (string, error) { return model.RoleAdmin, nil }, http.StatusOK, 0},
		{"普通用户 403", 1, func(context.Context, uint64) (string, error) { return model.RoleUser, nil }, http.StatusForbidden, errcode.ErrForbidden.Code},
		{"用户不存在（角色为空）403", 1, func(context.Context, uint64) (string, error) { return "", nil }, http.StatusForbidden, errcode.ErrForbidden.Code},
		{"没经过 JWT 按未登录 401", 0, func(context.Context, uint64) (string, error) { return model.RoleAdmin, nil }, http.StatusUnauthorized, errcode.ErrUnauthorized.Code},
		{"查询角色失败返回 500 而不是 403", 1, func(context.Context, uint64) (string, error) { return "", errors.New("db down") }, http.StatusInternalServerError, errcode.ErrInternal.Code},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			adminTestRouter(tt.userID, tt.lookup).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin", nil))
			if w.Code != tt.wantStatus {
				t.Fatalf("HTTP 状态期望 %d，实际 %d：%s", tt.wantStatus, w.Code, w.Body.String())
			}
			if tt.wantCode != 0 {
				var body struct {
					Code int `json:"code"`
				}
				_ = json.Unmarshal(w.Body.Bytes(), &body)
				if body.Code != tt.wantCode {
					t.Fatalf("业务码期望 %d，实际 %d", tt.wantCode, body.Code)
				}
			}
		})
	}
}

func TestRequireAdmin_LookupReceivesUserID(t *testing.T) {
	var got uint64
	lookup := func(_ context.Context, id uint64) (string, error) { got = id; return model.RoleAdmin, nil }
	w := httptest.NewRecorder()
	adminTestRouter(42, lookup).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin", nil))
	if got != 42 {
		t.Fatalf("应按当前用户 ID 查角色，实际 %d", got)
	}
}

func TestNewCachedRoleLookup(t *testing.T) {
	ctx := context.Background()

	t.Run("ttl 内命中缓存", func(t *testing.T) {
		calls := 0
		l := NewCachedRoleLookup(func(context.Context, uint64) (string, error) { calls++; return model.RoleAdmin, nil }, time.Minute)
		for i := 0; i < 3; i++ {
			if role, err := l(ctx, 1); err != nil || role != model.RoleAdmin {
				t.Fatalf("结果不符合预期：%q %v", role, err)
			}
		}
		if calls != 1 {
			t.Fatalf("期望只查一次库，实际 %d", calls)
		}
		_, _ = l(ctx, 2)
		if calls != 2 {
			t.Fatalf("不同用户应分别缓存，实际查库 %d 次", calls)
		}
	})

	t.Run("ttl 过期后重新查询", func(t *testing.T) {
		calls := 0
		l := NewCachedRoleLookup(func(context.Context, uint64) (string, error) { calls++; return model.RoleUser, nil }, time.Millisecond)
		_, _ = l(ctx, 1)
		time.Sleep(5 * time.Millisecond)
		_, _ = l(ctx, 1)
		if calls != 2 {
			t.Fatalf("过期后应重新查询，实际 %d 次", calls)
		}
	})

	t.Run("错误不缓存", func(t *testing.T) {
		calls := 0
		l := NewCachedRoleLookup(func(context.Context, uint64) (string, error) {
			calls++
			if calls == 1 {
				return "", errors.New("temporary")
			}
			return model.RoleAdmin, nil
		}, time.Minute)
		if _, err := l(ctx, 1); err == nil {
			t.Fatal("第一次应返回错误")
		}
		if role, err := l(ctx, 1); err != nil || role != model.RoleAdmin {
			t.Fatalf("第二次应重新查询并成功：%q %v", role, err)
		}
	})
}
