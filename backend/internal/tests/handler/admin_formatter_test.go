package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	. "video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/utils"
	"video-canvas/internal/router"
)

// GET /admin/formatters：管理员能读，列出 seedance、h3、generic 三个格式化器；普通用户 403。
func TestAdminFormatters_ListAndAuth(t *testing.T) {
	roles := map[uint64]string{2: model.RoleAdmin, 3: model.RoleUser}
	stateLookup := func(_ context.Context, id uint64) (*middleware.UserState, error) {
		return &middleware.UserState{Role: roles[id], Status: model.UserStatusActive}, nil
	}
	roleLookup := func(_ context.Context, id uint64) (string, error) { return roles[id], nil }
	engine := router.New(gin.TestMode, "fmt-secret", router.Handlers{AdminFormatter: NewAdminFormatterHandler(), AdminStep: nil, AdminRole: roleLookup, UserState: stateLookup})
	get := func(uid uint) *httptest.ResponseRecorder {
		tok, _, err := utils.GenerateToken(uid, "u", 0, "fmt-secret", "test", 1)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/formatters", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)
		return w
	}
	if w := get(3); w.Code != http.StatusForbidden {
		t.Fatalf("普通用户应 403：%d", w.Code)
	}
	w := get(2)
	var resp struct {
		Data []model.FormatterView `json:"data"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &resp) != nil || len(resp.Data) != 3 {
		t.Fatalf("应返回三个格式化器：%d %s", w.Code, w.Body.String())
	}
	keys := map[string]bool{}
	for _, f := range resp.Data {
		keys[f.Key] = true
		if f.Name == "" || f.Sample == "" {
			t.Fatalf("名称与样例不能为空：%+v", f)
		}
	}
	if !keys["seedance"] || !keys["h3"] || !keys["generic"] {
		t.Fatalf("keys=%v", keys)
	}
}
