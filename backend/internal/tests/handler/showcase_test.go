package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	. "video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/utils"
	"video-canvas/internal/router"
	"video-canvas/internal/service"
	"video-canvas/internal/service/showcasefake"
)

// 本文件测试 GET /showcase（公开）与 /admin/settings/showcase/*（后台）。路由用真实的 router.New
// （含 JWTAuth、RequireActive、RequireAdmin、RequireSuperAdmin），服务用真实实现，仓储 / 素材 / 设置用内存 fake。

const (
	shJWTSecret = "showcase-handler-secret"
	shSuper     = 1 // 超级管理员：拥有素材 10 / 11 / 14
	shAdmin     = 2 // 普通管理员：只能读
	shNormal    = 3 // 普通用户

	shVideo  = 10
	shPoster = 11
	shVideo2 = 14
	shOthers = 12 // 普通管理员名下的图片，超管不能拿来当封面

	shGenVideo   = 20 // 普通管理员名下平台生成的视频（任务 30），超管可以从素材库挑选
	shGenVideo2  = 21 // 超管名下平台生成的视频（没有任务）
	shOthersUp   = 22 // 普通管理员上传的视频，超管不能用
	shLibTaskID  = 30
	shLibBaseURL = "/api/v1/admin/settings/showcase/library"
)

// shSettings 实现 service.SystemSettingRepo。
type shSettings struct {
	mu sync.Mutex
	kv map[string]string
}

func (s *shSettings) GetAll(context.Context) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]string{}
	for k, v := range s.kv {
		out[k] = v
	}
	return out, nil
}

func (s *shSettings) SetMany(_ context.Context, kv map[string]string, _ uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range kv {
		s.kv[k] = v
	}
	return nil
}

// shAudit 实现 service.AdminAuditWriter，记录审计动作。
type shAudit struct {
	mu      sync.Mutex
	actions []string
}

func (a *shAudit) Insert(_ context.Context, l *model.AdminAuditLog) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.actions = append(a.actions, l.Action)
	return nil
}

func (a *shAudit) RecentForUser(context.Context, uint64, int) ([]model.AdminAuditView, error) {
	return nil, nil
}

type shEnv struct {
	engine *gin.Engine
	repo   *showcasefake.Repo
	assets *showcasefake.Assets
	audit  *shAudit
	tokens map[int]string
}

type shResp struct {
	Status int
	Code   int
	Msg    string
	Data   json.RawMessage
	Raw    string
	Header http.Header
}

func newShEnv(t *testing.T) *shEnv {
	t.Helper()
	repo := &showcasefake.Repo{}
	taskID := uint64(shLibTaskID)
	t0 := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	assets := showcasefake.NewAssets(
		model.Asset{ID: shVideo, UserID: shSuper, Kind: "video", StorageKey: "u1/v10.mp4", ByteSize: 3145728, Width: 1280, Height: 720, DurationMs: 5000, FileName: "开场.mp4"},
		model.Asset{ID: shPoster, UserID: shSuper, Kind: "image", StorageKey: "u1/p11.jpg"},
		model.Asset{ID: shOthers, UserID: shAdmin, Kind: "image", StorageKey: "u2/p12.jpg"},
		model.Asset{ID: shVideo2, UserID: shSuper, Kind: "video", StorageKey: "u1/v14.mp4", ByteSize: 100, Width: 640, Height: 360, DurationMs: 1000, FileName: "二.mp4"},
		model.Asset{ID: shGenVideo, UserID: shAdmin, Kind: "video", Source: model.AssetSourceGenerated, TaskID: &taskID, StorageKey: "u2/g20.mp4", ByteSize: 900, Width: 1920, Height: 1080, DurationMs: 8000, CreatedAt: t0.Add(2 * time.Hour)},
		model.Asset{ID: shGenVideo2, UserID: shSuper, Kind: "video", Source: model.AssetSourceGenerated, StorageKey: "u1/g21.mp4", CreatedAt: t0.Add(time.Hour)},
		model.Asset{ID: shOthersUp, UserID: shAdmin, Kind: "video", Source: model.AssetSourceUpload, StorageKey: "u2/u22.mp4", CreatedAt: t0.Add(3 * time.Hour)},
	)
	tasks := showcasefake.NewTasks(model.GenerationTask{
		ID: shLibTaskID, ModelKey: "monkey-v3", InputJSON: []byte(`{"prompt":"雨夜街头"}`), ConfigSnapshot: []byte(`{"model":{"label":"天才猴子三代"}}`),
	})
	users := &showcasefake.Users{Names: map[uint64]string{shSuper: "root", shAdmin: "ops"}}
	audit := &shAudit{}
	svc := service.NewShowcaseService(service.ShowcaseDeps{
		Items: repo, Assets: assets, Views: assets, Tasks: tasks, Users: users, Settings: &shSettings{kv: map[string]string{}}, Audit: audit,
	})
	roles := map[uint64]string{shSuper: model.RoleSuperAdmin, shAdmin: model.RoleAdmin, shNormal: model.RoleUser}
	stateLookup := func(_ context.Context, id uint64) (*middleware.UserState, error) {
		role, ok := roles[id]
		if !ok {
			return nil, nil
		}
		return &middleware.UserState{Role: role, Status: model.UserStatusActive}, nil
	}
	roleLookup := func(_ context.Context, id uint64) (string, error) { return roles[id], nil }
	env := &shEnv{repo: repo, assets: assets, audit: audit, tokens: map[int]string{}}
	for id := range roles {
		tok, _, err := utils.GenerateToken(uint(id), "u"+strconv.Itoa(int(id)), 0, shJWTSecret, "test", 1)
		if err != nil {
			t.Fatal(err)
		}
		env.tokens[int(id)] = tok
	}
	env.engine = router.New(gin.TestMode, shJWTSecret, router.Handlers{
		Showcase:  NewShowcaseHandler(svc),
		AdminRole: roleLookup,
		UserState: stateLookup,
	})
	return env
}

// do 发一个请求；user 为 0 表示不带登录凭证。body 可以是 nil、字符串（原样发送）或可序列化的值。
func (e *shEnv) do(method, path string, body any, user int) shResp {
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

func (r shResp) want(t *testing.T, status, code int) shResp {
	t.Helper()
	if r.Status != status || r.Code != code {
		t.Fatalf("期望 HTTP %d / code %d，实际 %d / %d：%s", status, code, r.Status, r.Code, r.Raw)
	}
	return r
}

// obj 把响应的 data 解成 map，方便断言字段名。
func (r shResp) obj(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(r.Data, &m); err != nil {
		t.Fatalf("data 不是对象：%s", r.Raw)
	}
	return m
}

const (
	shBase  = "/api/v1/admin/settings/showcase"
	shItems = shBase + "/items"
)

// addItem 通过后台接口新增一个条目并返回它的 id。
func (e *shEnv) addItem(t *testing.T, body map[string]any) int {
	t.Helper()
	r := e.do(http.MethodPost, shItems, body, shSuper).want(t, 200, 0)
	return int(r.obj(t)["id"].(float64))
}

// ---- 公开读取 ----

func TestShowcaseHandler_Public(t *testing.T) {
	t.Run("无需登录；响应结构与字段名符合契约，带 60 秒缓存头", func(t *testing.T) {
		e := newShEnv(t)
		id := e.addItem(t, map[string]any{"asset_id": shVideo, "poster_asset_id": shPoster, "prompt": "一只猫在雨里走", "model_label": "猴子三代", "start_sec": 1.5})
		e.addItem(t, map[string]any{"asset_id": shVideo2, "prompt": "禁用的", "enabled": false})

		r := e.do(http.MethodGet, "/api/v1/showcase", nil, 0).want(t, 200, 0)
		if got := r.Header.Get("Cache-Control"); got != "public, max-age=60" {
			t.Fatalf("Cache-Control 不对：%q", got)
		}
		data := r.obj(t)
		settings := data["settings"].(map[string]any)
		if settings["clip_seconds"] != float64(7) || settings["show_on_login"] != true || settings["poster_only_on_save_data"] != true || len(settings) != 3 {
			t.Fatalf("settings 不对：%v", settings)
		}
		items := data["items"].([]any)
		if len(items) != 1 {
			t.Fatalf("只应返回启用的条目：%v", items)
		}
		item := items[0].(map[string]any)
		want := map[string]any{
			"id": float64(id), "video_url": "/files/u1/v10.mp4", "poster_url": "/files/u1/p11.jpg", "prompt": "一只猫在雨里走",
			"model_label": "猴子三代", "start_sec": 1.5, "width": float64(1280), "height": float64(720), "byte_size": float64(3145728),
		}
		if len(item) != len(want) {
			t.Fatalf("字段个数不对（只能暴露契约里的字段）：%v", item)
		}
		for k, v := range want {
			if item[k] != v {
				t.Fatalf("字段 %s 期望 %v，实际 %v", k, v, item[k])
			}
		}
		for _, banned := range []string{"asset_id", "created_by", "enabled", "file_name"} {
			if strings.Contains(r.Raw, banned) {
				t.Fatalf("公开响应不应包含 %q：%s", banned, r.Raw)
			}
		}
	})

	t.Run("关闭登录页展示后 items 为空数组，settings 仍返回", func(t *testing.T) {
		e := newShEnv(t)
		e.addItem(t, map[string]any{"asset_id": shVideo, "prompt": "x"})
		e.do(http.MethodPut, shBase+"/settings", map[string]any{"clip_seconds": 9, "show_on_login": false, "poster_only_on_save_data": false}, shSuper).want(t, 200, 0)
		r := e.do(http.MethodGet, "/api/v1/showcase", nil, 0).want(t, 200, 0)
		data := r.obj(t)
		if items, ok := data["items"].([]any); !ok || len(items) != 0 {
			t.Fatalf("items 应为 []：%s", r.Raw)
		}
		if s := data["settings"].(map[string]any); s["clip_seconds"] != float64(9) || s["show_on_login"] != false {
			t.Fatalf("settings 不对：%v", s)
		}
	})

	t.Run("后台的修改立即反映到公开接口", func(t *testing.T) {
		e := newShEnv(t)
		id := e.addItem(t, map[string]any{"asset_id": shVideo, "prompt": "x"})
		count := func() int {
			return len(e.do(http.MethodGet, "/api/v1/showcase", nil, 0).want(t, 200, 0).obj(t)["items"].([]any))
		}
		if count() != 1 {
			t.Fatal("新增后应立即可见")
		}
		e.do(http.MethodPut, shItems+"/"+strconv.Itoa(id), map[string]any{"enabled": false}, shSuper).want(t, 200, 0)
		if count() != 0 {
			t.Fatal("禁用后应立即消失")
		}
		e.do(http.MethodPut, shItems+"/"+strconv.Itoa(id), map[string]any{"enabled": true}, shSuper).want(t, 200, 0)
		e.do(http.MethodDelete, shItems+"/"+strconv.Itoa(id), nil, shSuper).want(t, 200, 0)
		if count() != 0 {
			t.Fatal("删除后应立即消失")
		}
	})

	t.Run("下层故障返回 500，且不带缓存头", func(t *testing.T) {
		e := newShEnv(t)
		e.repo.Err = showcasefake.ErrInjected
		r := e.do(http.MethodGet, "/api/v1/showcase", nil, 0).want(t, 500, errcode.ErrInternal.Code)
		if got := r.Header.Get("Cache-Control"); got != "" {
			t.Fatalf("错误响应不应被缓存：%q", got)
		}
		if strings.Contains(r.Raw, "注入") {
			t.Fatalf("不应透传内部错误信息：%s", r.Raw)
		}
	})
}

// ---- 后台读取与权限 ----

func TestShowcaseHandler_AdminGet(t *testing.T) {
	e := newShEnv(t)
	e.addItem(t, map[string]any{"asset_id": shVideo, "poster_asset_id": shPoster, "prompt": "启用"})
	e.addItem(t, map[string]any{"asset_id": shVideo2, "prompt": "禁用", "enabled": false})

	r := e.do(http.MethodGet, shBase, nil, shAdmin).want(t, 200, 0) // 普通管理员可读
	items := r.obj(t)["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("后台应看到全部条目（含禁用）：%s", r.Raw)
	}
	first := items[0].(map[string]any)
	for _, k := range []string{"id", "asset_id", "poster_asset_id", "video_url", "poster_url", "prompt", "model_label", "start_sec", "enabled", "width", "height", "byte_size", "duration_ms", "file_name", "created_at"} {
		if _, ok := first[k]; !ok {
			t.Fatalf("后台条目缺少字段 %s：%v", k, first)
		}
	}
	if first["asset_id"] != float64(shVideo) || first["poster_asset_id"] != float64(shPoster) || first["file_name"] != "开场.mp4" || first["duration_ms"] != float64(5000) {
		t.Fatalf("后台条目字段值不对：%v", first)
	}
	if second := items[1].(map[string]any); second["poster_asset_id"] != nil || second["enabled"] != false {
		t.Fatalf("无封面时 poster_asset_id 应为 null：%v", second)
	}
	if strings.Contains(r.Raw, "created_by") {
		t.Fatalf("不应暴露 created_by：%s", r.Raw)
	}

	e.do(http.MethodGet, shBase, nil, shSuper).want(t, 200, 0)
	e.do(http.MethodGet, shBase, nil, shNormal).want(t, 403, errcode.ErrForbidden.Code)
	e.do(http.MethodGet, shBase, nil, 0).want(t, 401, errcode.ErrUnauthorized.Code)
}

// TestShowcaseHandler_WriteRoutesNeedSuperAdmin 验证全部 5 个写路由都已注册，且普通管理员 403、未登录 401。
func TestShowcaseHandler_WriteRoutesNeedSuperAdmin(t *testing.T) {
	e := newShEnv(t)
	id := e.addItem(t, map[string]any{"asset_id": shVideo, "prompt": "x"})
	one := shItems + "/" + strconv.Itoa(id)
	routes := []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, shItems, map[string]any{"asset_id": shVideo, "prompt": "y"}},
		{http.MethodPut, one, map[string]any{"enabled": false}},
		{http.MethodDelete, one, nil},
		{http.MethodPut, shBase + "/order", map[string]any{"ids": []int{id}}},
		{http.MethodPut, shBase + "/settings", map[string]any{"clip_seconds": 7, "show_on_login": true, "poster_only_on_save_data": true}},
	}
	for _, rt := range routes {
		t.Run(rt.method+" "+strings.TrimPrefix(rt.path, "/api/v1"), func(t *testing.T) {
			e.do(rt.method, rt.path, rt.body, shAdmin).want(t, 403, errcode.ErrForbidden.Code)
			e.do(rt.method, rt.path, rt.body, shNormal).want(t, 403, errcode.ErrForbidden.Code)
			e.do(rt.method, rt.path, rt.body, 0).want(t, 401, errcode.ErrUnauthorized.Code)
		})
	}
	if len(e.repo.Items) != 1 || e.repo.Items[0].Enabled != true {
		t.Fatalf("被拒绝的请求不应改动数据：%+v", e.repo.Items)
	}
}

// ---- 素材库 ----

func TestShowcaseHandler_Library(t *testing.T) {
	t.Run("成功：只含平台生成的视频，按创建时间倒序，字段名符合契约，added 随条目变化", func(t *testing.T) {
		e := newShEnv(t)
		e.addItem(t, map[string]any{"asset_id": shGenVideo2, "prompt": "已添加"})
		r := e.do(http.MethodGet, shLibBaseURL, nil, shAdmin).want(t, 200, 0) // 普通管理员可读
		o := r.obj(t)
		if o["total"] != float64(2) || o["page"] != float64(1) || o["page_size"] != float64(48) {
			t.Fatalf("分页信息不对：%s", r.Raw)
		}
		items := o["items"].([]any)
		if len(items) != 2 {
			t.Fatalf("应只有 2 个平台生成视频：%s", r.Raw)
		}
		first, second := items[0].(map[string]any), items[1].(map[string]any)
		for _, k := range []string{"asset_id", "video_url", "prompt", "model_label", "owner", "created_at", "width", "height", "byte_size", "duration_ms", "added"} {
			if _, ok := first[k]; !ok {
				t.Fatalf("缺少字段 %s：%v", k, first)
			}
		}
		if first["asset_id"] != float64(shGenVideo) || first["video_url"] != "/files/u2/g20.mp4" || first["prompt"] != "雨夜街头" ||
			first["model_label"] != "天才猴子三代" || first["owner"] != "ops" || first["width"] != float64(1920) || first["height"] != float64(1080) ||
			first["byte_size"] != float64(900) || first["duration_ms"] != float64(8000) || first["added"] != false || first["created_at"] != "2026-10-01T10:00:00Z" {
			t.Fatalf("字段值不对：%v", first)
		}
		if second["asset_id"] != float64(shGenVideo2) || second["prompt"] != "" || second["model_label"] != "" || second["owner"] != "root" || second["added"] != true {
			t.Fatalf("没有任务的素材应回落空串，且已被条目引用：%v", second)
		}
	})
	t.Run("权限：未登录 401、普通用户 403", func(t *testing.T) {
		e := newShEnv(t)
		e.do(http.MethodGet, shLibBaseURL, nil, shSuper).want(t, 200, 0)
		e.do(http.MethodGet, shLibBaseURL, nil, shNormal).want(t, 403, errcode.ErrForbidden.Code)
		e.do(http.MethodGet, shLibBaseURL, nil, 0).want(t, 401, errcode.ErrUnauthorized.Code)
	})
	t.Run("分页参数：正常分页、越界与非法值回落", func(t *testing.T) {
		e := newShEnv(t)
		for _, tt := range []struct {
			query        string
			wantItems    int
			wantPage     float64
			wantPageSize float64
		}{
			{"?page=2&page_size=1", 1, 2, 1},
			{"?page=3&page_size=1", 0, 3, 1},
			{"?page=0&page_size=0", 2, 1, 48},
			{"?page=-1&page_size=-9", 2, 1, 48},
			{"?page_size=100000", 2, 1, 100},
			{"?page=1&page_size=100", 2, 1, 100},
		} {
			o := e.do(http.MethodGet, shLibBaseURL+tt.query, nil, shSuper).want(t, 200, 0).obj(t)
			if len(o["items"].([]any)) != tt.wantItems || o["page"] != tt.wantPage || o["page_size"] != tt.wantPageSize || o["total"] != float64(2) {
				t.Fatalf("%s：%v", tt.query, o)
			}
		}
	})
	t.Run("page 不是数字返回 400 + 10001", func(t *testing.T) {
		e := newShEnv(t)
		e.do(http.MethodGet, shLibBaseURL+"?page=abc", nil, shSuper).want(t, 400, errcode.ErrInvalidParams.Code)
	})
	t.Run("没有素材时 items 是 []", func(t *testing.T) {
		e := newShEnv(t)
		delete(e.assets.Rows, shGenVideo)
		delete(e.assets.Rows, shGenVideo2)
		r := e.do(http.MethodGet, shLibBaseURL, nil, shSuper).want(t, 200, 0)
		if !strings.Contains(r.Raw, `"items":[]`) {
			t.Fatalf("items 应为 []：%s", r.Raw)
		}
	})
	t.Run("下层故障返回 500，不透传内部错误", func(t *testing.T) {
		e := newShEnv(t)
		e.assets.Err = showcasefake.ErrInjected
		r := e.do(http.MethodGet, shLibBaseURL, nil, shSuper).want(t, 500, errcode.ErrInternal.Code)
		if strings.Contains(r.Raw, "注入") {
			t.Fatalf("不应透传内部错误信息：%s", r.Raw)
		}
	})
}

// ---- 新增 ----

func TestShowcaseHandler_CreateItem(t *testing.T) {
	t.Run("成功：返回后台视图，写审计", func(t *testing.T) {
		e := newShEnv(t)
		r := e.do(http.MethodPost, shItems, map[string]any{"asset_id": shVideo, "poster_asset_id": nil, "prompt": " 雨夜 ", "model_label": "", "start_sec": 0, "enabled": true}, shSuper).want(t, 200, 0)
		o := r.obj(t)
		if o["prompt"] != "雨夜" || o["video_url"] != "/files/u1/v10.mp4" || o["poster_asset_id"] != nil || o["enabled"] != true {
			t.Fatalf("%v", o)
		}
		if len(e.audit.actions) != 1 || e.audit.actions[0] != model.AdminAuditShowcaseCreate {
			t.Fatalf("审计不对：%v", e.audit.actions)
		}
	})
	t.Run("参数校验失败返回 400 + 10001", func(t *testing.T) {
		e := newShEnv(t)
		for name, body := range map[string]any{
			"缺 asset_id":       map[string]any{"prompt": "x"},
			"asset_id 是字符串":    `{"asset_id":"abc","prompt":"x"}`,
			"asset_id 为 0":     map[string]any{"asset_id": 0, "prompt": "x"},
			"不是 JSON":          `{`,
			"poster 类型错误":      `{"asset_id":10,"prompt":"x","poster_asset_id":"p"}`,
			"enabled 类型错误":     `{"asset_id":10,"prompt":"x","enabled":"yes"}`,
			"start_sec 类型错误":   `{"asset_id":10,"prompt":"x","start_sec":"1"}`,
			"model_label 类型错误": `{"asset_id":10,"prompt":"x","model_label":1}`,
		} {
			e.do(http.MethodPost, shItems, body, shSuper).want(t, 400, errcode.ErrInvalidParams.Code)
			_ = name
		}
		if len(e.repo.Items) != 0 {
			t.Fatal("参数错误不应写库")
		}
	})
	t.Run("素材库挑选：别人的平台生成视频可用，别人上传的视频按不存在处理", func(t *testing.T) {
		e := newShEnv(t)
		r := e.do(http.MethodPost, shItems, map[string]any{"asset_id": shGenVideo, "prompt": "雨夜街头", "model_label": "天才猴子三代"}, shSuper).want(t, 200, 0)
		if o := r.obj(t); o["asset_id"] != float64(shGenVideo) || o["video_url"] != "/files/u2/g20.mp4" {
			t.Fatalf("%v", o)
		}
		e.do(http.MethodPost, shItems, map[string]any{"asset_id": shOthersUp, "prompt": "x"}, shSuper).want(t, 404, errcode.ErrAssetNotFound.Code)
	})
	t.Run("业务错误：素材不存在 404 / 文案为空 400 + 54002 / 素材属于别人按不存在处理", func(t *testing.T) {
		e := newShEnv(t)
		e.do(http.MethodPost, shItems, map[string]any{"asset_id": 999, "prompt": "x"}, shSuper).want(t, 404, errcode.ErrAssetNotFound.Code)
		e.do(http.MethodPost, shItems, map[string]any{"asset_id": shVideo, "prompt": "   "}, shSuper).want(t, 400, errcode.ErrShowcaseInvalid.Code)
		e.do(http.MethodPost, shItems, map[string]any{"asset_id": shVideo, "poster_asset_id": shOthers, "prompt": "x"}, shSuper).want(t, 404, errcode.ErrAssetNotFound.Code)
		e.do(http.MethodPost, shItems, map[string]any{"asset_id": shPoster, "prompt": "x"}, shSuper).want(t, 400, errcode.ErrShowcaseInvalid.Code)
		if len(e.repo.Items) != 0 || len(e.audit.actions) != 0 {
			t.Fatal("失败不应写库也不应写审计")
		}
	})
}

// ---- 修改 ----

func TestShowcaseHandler_UpdateItem(t *testing.T) {
	newOne := func(t *testing.T) (*shEnv, string) {
		e := newShEnv(t)
		id := e.addItem(t, map[string]any{"asset_id": shVideo, "poster_asset_id": shPoster, "prompt": "旧", "model_label": "旧模型"})
		return e, shItems + "/" + strconv.Itoa(id)
	}

	t.Run("没传封面字段不改封面；显式传 null 才清空；传数字换封面", func(t *testing.T) {
		e, path := newOne(t)
		r := e.do(http.MethodPut, path, `{"prompt":"新"}`, shSuper).want(t, 200, 0)
		if o := r.obj(t); o["prompt"] != "新" || o["poster_asset_id"] != float64(shPoster) || o["poster_url"] != "/files/u1/p11.jpg" || o["model_label"] != "旧模型" {
			t.Fatalf("没传封面时不应变：%v", o)
		}
		r = e.do(http.MethodPut, path, `{"poster_asset_id":null}`, shSuper).want(t, 200, 0)
		if o := r.obj(t); o["poster_asset_id"] != nil || o["poster_url"] != "" || o["prompt"] != "新" {
			t.Fatalf("传 null 应清空封面：%v", o)
		}
		r = e.do(http.MethodPut, path, `{"poster_asset_id":11}`, shSuper).want(t, 200, 0)
		if o := r.obj(t); o["poster_asset_id"] != float64(shPoster) {
			t.Fatalf("传数字应换封面：%v", o)
		}
		if len(e.audit.actions) != 4 || e.audit.actions[3] != model.AdminAuditShowcaseUpdate {
			t.Fatalf("审计不对：%v", e.audit.actions)
		}
	})
	t.Run("替换视频：只改视频，sort/enabled/封面不变；规则同新增", func(t *testing.T) {
		e, path := newOne(t)
		r := e.do(http.MethodPut, path, `{"asset_id":20}`, shSuper).want(t, 200, 0)
		if o := r.obj(t); o["asset_id"] != float64(shGenVideo) || o["video_url"] != "/files/u2/g20.mp4" || o["poster_asset_id"] != float64(shPoster) || o["enabled"] != true || o["prompt"] != "旧" {
			t.Fatalf("替换视频后其余字段不应变：%v", o)
		}
		r = e.do(http.MethodPut, path, `{"asset_id":14,"poster_asset_id":null}`, shSuper).want(t, 200, 0)
		if o := r.obj(t); o["asset_id"] != float64(shVideo2) || o["poster_asset_id"] != nil {
			t.Fatalf("可同时清空封面：%v", o)
		}
		e.do(http.MethodPut, path, `{"asset_id":22}`, shSuper).want(t, 404, errcode.ErrAssetNotFound.Code)   // 别人上传的视频
		e.do(http.MethodPut, path, `{"asset_id":999}`, shSuper).want(t, 404, errcode.ErrAssetNotFound.Code)  // 不存在
		e.do(http.MethodPut, path, `{"asset_id":11}`, shSuper).want(t, 400, errcode.ErrShowcaseInvalid.Code) // 是图片
		e.do(http.MethodPut, path, `{"asset_id":0}`, shSuper).want(t, 400, errcode.ErrInvalidParams.Code)
		e.do(http.MethodPut, path, `{"asset_id":"x"}`, shSuper).want(t, 400, errcode.ErrInvalidParams.Code)
		if got := e.repo.Items[0].AssetID; got != shVideo2 {
			t.Fatalf("被拒绝的请求不应改动视频：%d", got)
		}
	})
	t.Run("参数校验失败返回 400 + 10001", func(t *testing.T) {
		e, path := newOne(t)
		e.do(http.MethodPut, path, `{"poster_asset_id":"abc"}`, shSuper).want(t, 400, errcode.ErrInvalidParams.Code)
		e.do(http.MethodPut, path, `{"enabled":"yes"}`, shSuper).want(t, 400, errcode.ErrInvalidParams.Code)
		e.do(http.MethodPut, path, `{`, shSuper).want(t, 400, errcode.ErrInvalidParams.Code)
		e.do(http.MethodPut, shItems+"/abc", `{"enabled":false}`, shSuper).want(t, 400, errcode.ErrInvalidParams.Code)
		e.do(http.MethodPut, shItems+"/0", `{"enabled":false}`, shSuper).want(t, 400, errcode.ErrInvalidParams.Code)
	})
	t.Run("业务错误：条目不存在 404 + 54001；一个字段都没传 400 + 54002", func(t *testing.T) {
		e, path := newOne(t)
		e.do(http.MethodPut, shItems+"/9999", `{"enabled":false}`, shSuper).want(t, 404, errcode.ErrShowcaseNotFound.Code)
		e.do(http.MethodPut, path, `{}`, shSuper).want(t, 400, errcode.ErrShowcaseInvalid.Code)
		e.do(http.MethodPut, path, `{"poster_asset_id":12}`, shSuper).want(t, 404, errcode.ErrAssetNotFound.Code)
	})
}

// ---- 删除 ----

func TestShowcaseHandler_DeleteItem(t *testing.T) {
	e := newShEnv(t)
	id := e.addItem(t, map[string]any{"asset_id": shVideo, "prompt": "x"})
	path := shItems + "/" + strconv.Itoa(id)

	e.do(http.MethodDelete, path, nil, shSuper).want(t, 200, 0)
	if len(e.repo.Items) != 0 || len(e.assets.Rows) != 7 {
		t.Fatalf("只应删条目，不删素材：items=%d assets=%d", len(e.repo.Items), len(e.assets.Rows))
	}
	e.do(http.MethodDelete, path, nil, shSuper).want(t, 404, errcode.ErrShowcaseNotFound.Code)
	e.do(http.MethodDelete, shItems+"/abc", nil, shSuper).want(t, 400, errcode.ErrInvalidParams.Code)
}

// ---- 排序 ----

func TestShowcaseHandler_Reorder(t *testing.T) {
	setup := func(t *testing.T) (*shEnv, [3]int) {
		e := newShEnv(t)
		var ids [3]int
		for i, p := range []string{"a", "b", "c"} {
			ids[i] = e.addItem(t, map[string]any{"asset_id": shVideo, "prompt": p})
		}
		return e, ids
	}
	order := func(t *testing.T, e *shEnv) []string {
		var out []string
		for _, it := range e.do(http.MethodGet, shBase, nil, shSuper).want(t, 200, 0).obj(t)["items"].([]any) {
			out = append(out, it.(map[string]any)["prompt"].(string))
		}
		return out
	}

	t.Run("成功：后台与公开接口都按新顺序", func(t *testing.T) {
		e, ids := setup(t)
		e.do(http.MethodPut, shBase+"/order", map[string]any{"ids": []int{ids[2], ids[0], ids[1]}}, shSuper).want(t, 200, 0)
		if got := strings.Join(order(t, e), ""); got != "cab" {
			t.Fatalf("顺序不对：%s", got)
		}
		pub := e.do(http.MethodGet, "/api/v1/showcase", nil, 0).want(t, 200, 0).obj(t)["items"].([]any)
		if pub[0].(map[string]any)["prompt"] != "c" {
			t.Fatalf("公开接口顺序不对：%v", pub)
		}
	})
	t.Run("参数校验失败返回 400 + 10001", func(t *testing.T) {
		e, _ := setup(t)
		e.do(http.MethodPut, shBase+"/order", `{}`, shSuper).want(t, 400, errcode.ErrInvalidParams.Code)
		e.do(http.MethodPut, shBase+"/order", `{"ids":"1,2"}`, shSuper).want(t, 400, errcode.ErrInvalidParams.Code)
		e.do(http.MethodPut, shBase+"/order", `{"ids":[-1]}`, shSuper).want(t, 400, errcode.ErrInvalidParams.Code)
	})
	t.Run("业务错误：少了 / 多了 / 重复都返回 400 + 54003，顺序不变", func(t *testing.T) {
		e, ids := setup(t)
		for _, bad := range [][]int{{ids[0], ids[1]}, {ids[0], ids[1], ids[2], 9999}, {ids[0], ids[0], ids[1]}, {}} {
			e.do(http.MethodPut, shBase+"/order", map[string]any{"ids": bad}, shSuper).want(t, 400, errcode.ErrShowcaseOrderMismatch.Code)
		}
		if got := strings.Join(order(t, e), ""); got != "abc" {
			t.Fatalf("失败不应改变顺序：%s", got)
		}
	})
}

// ---- 设置 ----

func TestShowcaseHandler_UpdateSettings(t *testing.T) {
	e := newShEnv(t)
	r := e.do(http.MethodPut, shBase+"/settings", map[string]any{"clip_seconds": 12, "show_on_login": false, "poster_only_on_save_data": false}, shSuper).want(t, 200, 0)
	if o := r.obj(t); o["clip_seconds"] != float64(12) || o["show_on_login"] != false || o["poster_only_on_save_data"] != false || len(o) != 3 {
		t.Fatalf("应返回最新设置：%v", o)
	}
	if s := e.do(http.MethodGet, shBase, nil, shAdmin).want(t, 200, 0).obj(t)["settings"].(map[string]any); s["clip_seconds"] != float64(12) || s["show_on_login"] != false {
		t.Fatalf("后台读取应是最新设置：%v", s)
	}
	if len(e.audit.actions) != 1 || e.audit.actions[0] != model.AdminAuditShowcaseSettings {
		t.Fatalf("审计不对：%v", e.audit.actions)
	}

	// 参数校验失败：缺字段、类型错误 -> 400 + 10001（缺字段不能被当成 false 悄悄关掉开关）
	e.do(http.MethodPut, shBase+"/settings", `{"clip_seconds":7}`, shSuper).want(t, 400, errcode.ErrInvalidParams.Code)
	e.do(http.MethodPut, shBase+"/settings", `{"clip_seconds":"7","show_on_login":true,"poster_only_on_save_data":true}`, shSuper).want(t, 400, errcode.ErrInvalidParams.Code)
	// 业务错误：秒数越界 -> 400 + 54002
	for _, sec := range []int{3, 16, 0, -5} {
		e.do(http.MethodPut, shBase+"/settings", map[string]any{"clip_seconds": sec, "show_on_login": true, "poster_only_on_save_data": true}, shSuper).
			want(t, 400, errcode.ErrShowcaseInvalid.Code)
	}
	if s := e.do(http.MethodGet, shBase, nil, shAdmin).want(t, 200, 0).obj(t)["settings"].(map[string]any); s["clip_seconds"] != float64(12) {
		t.Fatalf("失败不应改变设置：%v", s)
	}
}
