package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	. "video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/utils"
	"video-canvas/internal/router"
	"video-canvas/internal/service"
	"video-canvas/internal/service/announcementfake"
)

// 本文件测试 GET /announcements（需登录）与 /admin/settings/announcements/*（admin 或 super_admin）。
// 路由用真实的 router.New（含 JWTAuth、RequireActive、RequireAdmin），服务用真实实现，仓储用内存 fake。

const (
	anJWTSecret = "announcement-handler-secret"
	anSuper     = 1
	anAdmin     = 2
	anNormal    = 3

	anBase = "/api/v1/admin/settings/announcements"
)

type anEnv struct {
	engine *gin.Engine
	repo   *announcementfake.Repo
	audit  *shAudit
	tokens map[int]string
}

func newAnEnv(t *testing.T) *anEnv {
	t.Helper()
	repo := &announcementfake.Repo{}
	audit := &shAudit{}
	svc := service.NewAnnouncementService(service.AnnouncementDeps{Repo: repo, Audit: audit})
	roles := map[uint64]string{anSuper: model.RoleSuperAdmin, anAdmin: model.RoleAdmin, anNormal: model.RoleUser}
	stateLookup := func(_ context.Context, id uint64) (*middleware.UserState, error) {
		role, ok := roles[id]
		if !ok {
			return nil, nil
		}
		return &middleware.UserState{Role: role, Status: model.UserStatusActive}, nil
	}
	roleLookup := func(_ context.Context, id uint64) (string, error) { return roles[id], nil }
	env := &anEnv{repo: repo, audit: audit, tokens: map[int]string{}}
	for id := range roles {
		tok, _, err := utils.GenerateToken(uint(id), "u"+strconv.Itoa(int(id)), 0, anJWTSecret, "test", 1)
		if err != nil {
			t.Fatal(err)
		}
		env.tokens[int(id)] = tok
	}
	env.engine = router.New(gin.TestMode, anJWTSecret, router.Handlers{
		Announcement: NewAnnouncementHandler(svc),
		AdminRole:    roleLookup,
		UserState:    stateLookup,
	})
	return env
}

// do 发一个请求；user 为 0 表示不带登录凭证。body 可以是 nil、字符串（原样发送）或可序列化的值。
func (e *anEnv) do(method, path string, body any, user int) shResp {
	var buf *bytes.Reader
	switch b := body.(type) {
	case nil:
		buf = bytes.NewReader(nil)
	case string:
		buf = bytes.NewReader([]byte(b))
	default:
		raw, _ := json.Marshal(b)
		buf = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, buf)
	req.Header.Set("Content-Type", "application/json")
	if user > 0 {
		req.Header.Set("Authorization", "Bearer "+e.tokens[user])
	}
	w := httptest.NewRecorder()
	e.engine.ServeHTTP(w, req)
	var parsed struct {
		Code int             `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &parsed)
	return shResp{Status: w.Code, Code: parsed.Code, Msg: parsed.Msg, Data: parsed.Data, Raw: w.Body.String(), Header: w.Header()}
}

// add 通过后台接口新建一条公告并返回 id。
func (e *anEnv) add(t *testing.T, body map[string]any) int {
	t.Helper()
	r := e.do(http.MethodPost, anBase, body, anAdmin).want(t, 200, 0)
	return int(r.obj(t)["id"].(float64))
}

func TestAnnouncementHandler_Public(t *testing.T) {
	t.Run("未登录 401", func(t *testing.T) {
		e := newAnEnv(t)
		if r := e.do(http.MethodGet, "/api/v1/announcements", nil, 0); r.Status != http.StatusUnauthorized {
			t.Fatalf("期望 401，实际 %d：%s", r.Status, r.Raw)
		}
	})

	t.Run("普通用户可读；字段符合契约，带私有缓存头", func(t *testing.T) {
		e := newAnEnv(t)
		id := e.add(t, map[string]any{"title": "Seedance 2.5 已上线", "enabled": true, "ends_at": "2099-01-01T00:00:00Z"})
		e.add(t, map[string]any{"title": "停用的", "enabled": false})

		r := e.do(http.MethodGet, "/api/v1/announcements", nil, anNormal).want(t, 200, 0)
		if got := r.Header.Get("Cache-Control"); got != "private, max-age=60" {
			t.Fatalf("Cache-Control 不对：%q", got)
		}
		items := r.obj(t)["items"].([]any)
		if len(items) != 1 {
			t.Fatalf("只应返回生效中的公告：%s", r.Raw)
		}
		item := items[0].(map[string]any)
		want := map[string]any{"id": float64(id), "title": "Seedance 2.5 已上线", "ends_at": "2099-01-01T00:00:00Z"}
		for k, v := range want {
			if item[k] != v {
				t.Fatalf("字段 %s 期望 %v，实际 %v", k, v, item[k])
			}
		}
		if v, ok := item["version"].(float64); !ok || v <= 0 {
			t.Fatalf("version 应为正数：%v", item["version"])
		}
		if len(item) != len(want)+1 {
			t.Fatalf("只能暴露契约里的字段：%v", item)
		}
		for _, banned := range []string{"created_by", "sort", "starts_at", "enabled"} {
			if strings.Contains(r.Raw, `"`+banned+`"`) {
				t.Fatalf("公开响应不应包含 %q：%s", banned, r.Raw)
			}
		}
	})

	t.Run("没有公告时 items 为空数组", func(t *testing.T) {
		e := newAnEnv(t)
		r := e.do(http.MethodGet, "/api/v1/announcements", nil, anNormal).want(t, 200, 0)
		if !strings.Contains(r.Raw, `"items":[]`) {
			t.Fatalf("items 应为 []：%s", r.Raw)
		}
	})
}

func TestAnnouncementHandler_AdminPermission(t *testing.T) {
	e := newAnEnv(t)
	if r := e.do(http.MethodGet, anBase, nil, anNormal); r.Status != http.StatusForbidden {
		t.Fatalf("普通用户读后台应 403，实际 %d", r.Status)
	}
	if r := e.do(http.MethodPost, anBase, map[string]any{"title": "x", "enabled": true}, anNormal); r.Status != http.StatusForbidden {
		t.Fatalf("普通用户写后台应 403，实际 %d", r.Status)
	}
	// admin 与 super_admin 都能写
	e.do(http.MethodPost, anBase, map[string]any{"title": "运营发的", "enabled": true}, anAdmin).want(t, 200, 0)
	e.do(http.MethodPost, anBase, map[string]any{"title": "超管发的", "enabled": true}, anSuper).want(t, 200, 0)
	items := e.do(http.MethodGet, anBase, nil, anAdmin).want(t, 200, 0).obj(t)["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("后台列表应有 2 条：%v", items)
	}
}

func TestAnnouncementHandler_Create(t *testing.T) {
	e := newAnEnv(t)
	r := e.do(http.MethodPost, anBase, map[string]any{"title": "  新公告 ", "enabled": true}, anAdmin).want(t, 200, 0)
	got := r.obj(t)
	if got["title"] != "新公告" || got["status"] != "live" || got["starts_at"] != nil {
		t.Fatalf("响应不对：%s", r.Raw)
	}

	// 参数校验失败：JSON 不合法、缺 enabled → 400 / 10001
	e.do(http.MethodPost, anBase, "{bad json", anAdmin).want(t, 400, 10001)
	e.do(http.MethodPost, anBase, map[string]any{"title": "x"}, anAdmin).want(t, 400, 10001)
	// 业务校验失败：标题超长 → 400 / 56002，原因写在 msg
	bad := e.do(http.MethodPost, anBase, map[string]any{"title": strings.Repeat("字", 61), "enabled": true}, anAdmin).want(t, 400, 56002)
	if !strings.Contains(bad.Msg, "标题") {
		t.Fatalf("msg 应说明是标题的问题：%s", bad.Msg)
	}
	if len(e.audit.actions) != 1 || e.audit.actions[0] != model.AdminAuditAnnouncementCreate {
		t.Fatalf("只有成功的那次写审计：%v", e.audit.actions)
	}
}

func TestAnnouncementHandler_UpdateEnabledDelete(t *testing.T) {
	e := newAnEnv(t)
	id := e.add(t, map[string]any{"title": "原标题", "enabled": true, "ends_at": "2099-01-01T00:00:00Z"})
	path := anBase + "/" + strconv.Itoa(id)

	r := e.do(http.MethodPut, path, map[string]any{"title": "改过的", "enabled": true}, anAdmin).want(t, 200, 0)
	if got := r.obj(t); got["title"] != "改过的" || got["ends_at"] != nil {
		t.Fatalf("应整体替换：%s", r.Raw)
	}
	e.do(http.MethodPut, anBase+"/99", map[string]any{"title": "x", "enabled": true}, anAdmin).want(t, 404, 56001)
	e.do(http.MethodPut, anBase+"/abc", map[string]any{"title": "x", "enabled": true}, anAdmin).want(t, 400, 10001)

	r = e.do(http.MethodPut, path+"/enabled", map[string]any{"enabled": false}, anAdmin).want(t, 200, 0)
	if got := r.obj(t); got["enabled"] != false || got["status"] != "disabled" {
		t.Fatalf("应已停用：%s", r.Raw)
	}
	e.do(http.MethodPut, path+"/enabled", map[string]any{}, anAdmin).want(t, 400, 10001)
	e.do(http.MethodPut, anBase+"/99/enabled", map[string]any{"enabled": true}, anAdmin).want(t, 404, 56001)

	e.do(http.MethodDelete, path, nil, anAdmin).want(t, 200, 0)
	e.do(http.MethodDelete, path, nil, anAdmin).want(t, 404, 56001)
}

func TestAnnouncementHandler_Reorder(t *testing.T) {
	e := newAnEnv(t)
	a := e.add(t, map[string]any{"title": "a", "enabled": true})
	b := e.add(t, map[string]any{"title": "b", "enabled": true})

	e.do(http.MethodPut, anBase+"/order", map[string]any{"ids": []int{b, a}}, anAdmin).want(t, 200, 0)
	items := e.do(http.MethodGet, "/api/v1/announcements", nil, anNormal).want(t, 200, 0).obj(t)["items"].([]any)
	if items[0].(map[string]any)["title"] != "b" {
		t.Fatalf("顺序应已调整：%v", items)
	}
	e.do(http.MethodPut, anBase+"/order", map[string]any{"ids": []int{a}}, anAdmin).want(t, 400, 56003)
	e.do(http.MethodPut, anBase+"/order", map[string]any{}, anAdmin).want(t, 400, 10001)
}
