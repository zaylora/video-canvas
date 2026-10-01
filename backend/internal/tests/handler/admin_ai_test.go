package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	. "video-canvas/internal/handler"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/utils"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/router"
	"video-canvas/internal/service"
	"video-canvas/internal/service/aiconfigfake"
)

// 本文件测试插件 / 渠道 / 模型配置管理接口。路由用真实的 router.New（含 JWTAuth、RequireAdmin、RequireSuperAdmin），
// 所以权限矩阵（admin 调写接口 403）测的就是线上的路由表；service 是真实实现，仓储与插件宿主用内存替身。

const (
	aicAdminUser  = 1 // admin（运营）
	aicNormalUser = 2 // 普通用户
	aicSuperUser  = 3 // super_admin（运维）

	aicJWTSecret = "handler-test-jwt-secret"
	aicBase      = "/api/v1/admin/ai"
)

// aicEnv 是管理接口 handler 测试的完整环境。
type aicEnv struct {
	engine   *gin.Engine
	cfg      *service.AIConfigService
	repo     *aiconfigfake.MemRepo
	dry      *aiconfigfake.DryRunner
	tasks    *aiconfigfake.TestTasks
	ops      *aiconfigfake.PluginOps
	checker  *aiconfigfake.Prechecker
	roles    map[uint64]string
	tokens   map[int]string
	versions map[string]uint64 // "key@version" → 版本 id
}

func aicNewEnv(t *testing.T) *aicEnv {
	t.Helper()
	repo := aiconfigfake.NewMemRepo()
	cfg := service.NewAIConfigService(repo, repo, repo, "handler-test-master-key")
	env := &aicEnv{
		cfg:  cfg,
		repo: repo,
		dry:  &aiconfigfake.DryRunner{Result: map[string]any{"submit": map[string]any{"url": "https://example.com/run"}}},
		tasks: &aiconfigfake.TestTasks{
			Views: map[uint64]aiconfigfake.TestView{},
		},
		ops:      &aiconfigfake.PluginOps{},
		checker:  &aiconfigfake.Prechecker{ByCode: map[string]*provider.PrecheckResult{}},
		roles:    map[uint64]string{aicAdminUser: model.RoleAdmin, aicNormalUser: model.RoleUser, aicSuperUser: model.RoleSuperAdmin},
		tokens:   map[int]string{},
		versions: map[string]uint64{},
	}
	cfg.SetDryRunner(env.dry)
	cfg.SetTestTaskCreator(env.tasks)

	for id := range env.roles {
		tok, _, err := utils.GenerateToken(uint(id), fmt.Sprintf("u%d", id), aicJWTSecret, "test", 1)
		if err != nil {
			t.Fatal(err)
		}
		env.tokens[int(id)] = tok
	}
	lookup := func(_ context.Context, id uint64) (string, error) { return env.roles[id], nil }
	env.engine = router.New(gin.TestMode, aicJWTSecret, router.Handlers{
		AIModel:      NewAIModelHandler(cfg),
		AdminAI:      NewAdminAIHandler(cfg),
		AdminPlugin:  NewAdminPluginHandler(service.NewAIPluginService(repo, repo, repo, env.checker, cfg)),
		AdminChannel: NewAdminChannelHandler(service.NewAIChannelService(repo, repo, cfg, repo, env.ops, cfg)),
		AdminMe:      NewAdminMeHandler(lookup),
		AdminRole:    lookup,
	})
	return env
}

// aicResp 是统一响应的解析结果。
type aicResp struct {
	Status int
	Code   int
	Msg    string
	Data   json.RawMessage
	Raw    string
}

func (e *aicEnv) serve(req *http.Request, user int) aicResp {
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
	return aicResp{Status: w.Code, Code: parsed.Code, Msg: parsed.Msg, Data: parsed.Data, Raw: w.Body.String()}
}

func (e *aicEnv) do(method, path string, body any, user int) aicResp {
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
	return e.serve(req, user)
}

// upload 以 multipart 上传插件文件；field 为空表示不带 file 字段。
func (e *aicEnv) upload(field, filename string, content []byte, user int) aicResp {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if field != "" {
		fw, _ := mw.CreateFormFile(field, filename)
		_, _ = fw.Write(content)
	} else {
		_ = mw.WriteField("other", "x")
	}
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, aicBase+"/plugins", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return e.serve(req, user)
}

// admin 以 admin 身份请求；super 以 super_admin 身份请求。
func (e *aicEnv) admin(method, path string, body any) aicResp {
	return e.do(method, path, body, aicAdminUser)
}

func (e *aicEnv) super(method, path string, body any) aicResp {
	return e.do(method, path, body, aicSuperUser)
}

func aicWant(t *testing.T, r aicResp, status, code int) {
	t.Helper()
	if r.Status != status || r.Code != code {
		t.Fatalf("期望 HTTP %d / code %d，实际 HTTP %d / code %d：%s", status, code, r.Status, r.Code, r.Raw)
	}
}

// aicMeta 生成插件 meta：bearer 鉴权，支持 video / text，渠道设置项 region（有默认）与 tenant（必填），导入参数 limit（必填）。
func aicMeta(key, version string) string {
	return `{"apiVersion":1,"key":"` + key + `","name":"插件-` + key + `","version":"` + version + `","auth":{"type":"bearer"},` +
		`"endpoints":{"video":{"mode":"async"},"text":{"mode":"sync"}},` +
		`"channelSettings":{"region":{"type":"enum","label":"区域","options":["cn","global"],"default":"cn"},"tenant":{"type":"string","label":"租户","required":true}},` +
		`"import":{"args":{"limit":{"type":"number","label":"数量","required":true}}}}`
}

// seedPlugin 直接往仓储里放一个插件版本（不经过接口）。
func (e *aicEnv) seedPlugin(t *testing.T, key, version, source string) uint64 {
	t.Helper()
	p := &model.AIPlugin{Key: key, Name: "插件-" + key, Source: source, Enabled: true}
	v := &model.AIPluginVersion{PluginKey: key, Version: version, SHA256: "seed", Code: "code", MetaJSON: model.JSONText(aicMeta(key, version))}
	if err := e.repo.SaveVersion(context.Background(), p, v); err != nil {
		t.Fatal(err)
	}
	e.versions[key+"@"+version] = v.ID
	return v.ID
}

func aicChannelJSON(key, pluginVersion string) map[string]any {
	return map[string]any{
		"key": key, "name": "渠道-" + key, "plugin_key": "kling", "plugin_version": pluginVersion,
		"base_url": "https://gw.example.com", "settings": map[string]any{"tenant": "t1"},
		"rate_limit": map[string]any{"rps": 5, "max_concurrency": 20},
	}
}

// aicCapabilities 返回合法的最小模型能力：video / image 有生成方式，text 有上下文与固定系统提示。
func aicCapabilities(kind string) map[string]any {
	caps := map[string]any{"prompt": map[string]any{"max_length": 2000}}
	switch kind {
	case "video":
		caps["ops"] = []any{"t2v"}
	case "image":
		caps["ops"] = []any{"t2i"}
	case "text":
		caps["context"] = map[string]any{"window": 128000, "output": 4096}
		caps["system"] = "保密的系统提示"
	}
	return caps
}

func aicModelJSON(key, kind, channel string) map[string]any {
	return map[string]any{
		"key": key, "kind": kind, "label": "模型-" + key, "hint": "小字", "credits": 5, "enabled": true, "sort": 10,
		"channels":     []any{map[string]any{"channel": channel, "upstream_model": "kling-v2"}},
		"params":       map[string]any{"instanceType": "default"},
		"capabilities": aicCapabilities(kind),
	}
}

// publishAll 通过接口完成：（预置插件）→ 建渠道 → 设 Key → 建模型草稿 → 发布。
func (e *aicEnv) publishAll(t *testing.T) {
	t.Helper()
	e.seedPlugin(t, "kling", "1.0.0", model.PluginSourceUploaded)
	steps := []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, aicBase + "/channels", aicChannelJSON("kling-main", "1.0.0")},
		{http.MethodPut, aicBase + "/channels/kling-main/secret", map[string]any{"value": "sk-super-secret"}},
		{http.MethodPost, aicBase + "/models", map[string]any{"body": aicModelJSON("m1", "video", "kling-main")}},
		{http.MethodPost, aicBase + "/models/m1/publish", nil},
	}
	for _, s := range steps {
		if r := e.super(s.method, s.path, s.body); r.Status != http.StatusOK || r.Code != 0 {
			t.Fatalf("准备步骤 %s %s 失败：%s", s.method, s.path, r.Raw)
		}
	}
}

// ---------------------------------------------------------------------------
// GET /models（面向画布）
// ---------------------------------------------------------------------------

func TestAIModelHandler_List(t *testing.T) {
	env := aicNewEnv(t)
	env.publishAll(t)

	t.Run("返回模型清单且不泄露内部字段", func(t *testing.T) {
		r := env.do(http.MethodGet, "/api/v1/models", nil, aicNormalUser)
		aicWant(t, r, http.StatusOK, 0)
		var list []map[string]any
		if err := json.Unmarshal(r.Data, &list); err != nil || len(list) != 1 {
			t.Fatalf("应返回 1 个模型：%v %s", err, r.Raw)
		}
		m := list[0]
		if m["key"] != "m1" || m["kind"] != "video" || m["label"] != "模型-m1" || m["hint"] != "小字" || m["credits"] != float64(5) {
			t.Fatalf("公开字段不符合预期：%v", m)
		}
		if caps, _ := m["capabilities"].(map[string]any); caps == nil || caps["prompt"] == nil || caps["ops"] == nil {
			t.Fatalf("应包含 capabilities：%v", m)
		}
		for _, bad := range []string{"params", "channel", "plugin", "secret", "instanceType", "kling", "sk-super-secret", "base_url", "upstream"} {
			if strings.Contains(r.Raw, bad) {
				t.Fatalf("/models 响应泄露了 %q：%s", bad, r.Raw)
			}
		}
		if tags, ok := m["tags"].([]any); !ok || len(tags) != 0 {
			t.Fatalf("没有标签时 tags 应为 []：%v", m["tags"])
		}
		if len(m) != 8 {
			t.Fatalf("公开字段应恰好 8 个（key/kind/label/hint/vendor/tags/credits/capabilities），实际 %d：%v", len(m), m)
		}
	})
	t.Run("kind=text 合法：文本节点能拿到清单", func(t *testing.T) {
		aicWant(t, env.super(http.MethodPost, aicBase+"/models", map[string]any{"body": aicModelJSON("t1", "text", "kling-main")}), http.StatusOK, 0)
		aicWant(t, env.super(http.MethodPost, aicBase+"/models/t1/publish", nil), http.StatusOK, 0)
		r := env.do(http.MethodGet, "/api/v1/models?kind=text", nil, aicNormalUser)
		aicWant(t, r, http.StatusOK, 0)
		var list []map[string]any
		if err := json.Unmarshal(r.Data, &list); err != nil || len(list) != 1 || list[0]["key"] != "t1" || list[0]["kind"] != "text" {
			t.Fatalf("应只返回文本模型：%v %s", err, r.Raw)
		}
	})
	t.Run("按 kind 过滤，没有结果返回空数组", func(t *testing.T) {
		r := env.do(http.MethodGet, "/api/v1/models?kind=image", nil, aicNormalUser)
		aicWant(t, r, http.StatusOK, 0)
		if strings.TrimSpace(string(r.Data)) != "[]" {
			t.Fatalf("应返回空数组：%s", r.Raw)
		}
	})
	t.Run("kind 非法返回 400", func(t *testing.T) {
		aicWant(t, env.do(http.MethodGet, "/api/v1/models?kind=bogus", nil, aicNormalUser), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
	})
	t.Run("下架后不再出现", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPut, aicBase+"/models/m1/enabled", map[string]any{"enabled": false}), http.StatusOK, 0)
		r := env.do(http.MethodGet, "/api/v1/models?kind=video", nil, aicNormalUser)
		if strings.TrimSpace(string(r.Data)) != "[]" {
			t.Fatalf("下架后应为空：%s", r.Raw)
		}
	})
}

// ---------------------------------------------------------------------------
// 权限矩阵
// ---------------------------------------------------------------------------

func TestAdminAI_PermissionMatrix(t *testing.T) {
	env := aicNewEnv(t)
	type route struct {
		method, path string
		superOnly    bool // 写插件 / 渠道：仅 super_admin
	}
	routes := []route{
		{http.MethodGet, "/me", false},
		{http.MethodGet, "/plugins", false},
		{http.MethodPost, "/plugins", true},
		{http.MethodPut, "/plugins/kling/enabled", true},
		{http.MethodDelete, "/plugins/kling/versions/1.0.0", true},
		{http.MethodGet, "/channels", false},
		{http.MethodPost, "/channels", true},
		{http.MethodGet, "/channels/x", false},
		{http.MethodPut, "/channels/x", true},
		{http.MethodPut, "/channels/x/secret", true},
		{http.MethodPost, "/channels/x/check", true},
		{http.MethodPost, "/channels/x/import", false},
		{http.MethodGet, "/models", false},
		{http.MethodPost, "/models", false},
		{http.MethodGet, "/models/m1", false},
		{http.MethodPut, "/models/m1", false},
		{http.MethodPost, "/models/m1/validate", false},
		{http.MethodPost, "/models/m1/publish", false},
		{http.MethodPost, "/models/m1/rollback", false},
		{http.MethodPost, "/models/m1/dry-run", false},
		{http.MethodPost, "/models/m1/test-run", false},
		{http.MethodPut, "/models/m1/enabled", false},
		{http.MethodPut, "/models/m1/sort", false},
		{http.MethodGet, "/test-runs/1", false},
		{http.MethodGet, "/test-runs/1/trace", false},
		{http.MethodGet, "/schema/model", false},
	}
	for _, rt := range routes {
		name := rt.method + " " + rt.path
		t.Run("未登录 401："+name, func(t *testing.T) {
			aicWant(t, env.do(rt.method, aicBase+rt.path, "{}", 0), http.StatusUnauthorized, errcode.ErrUnauthorized.Code)
		})
		t.Run("普通用户 403："+name, func(t *testing.T) {
			aicWant(t, env.do(rt.method, aicBase+rt.path, "{}", aicNormalUser), http.StatusForbidden, errcode.ErrForbidden.Code)
		})
		t.Run("admin："+name, func(t *testing.T) {
			r := env.do(rt.method, aicBase+rt.path, "{}", aicAdminUser)
			if rt.superOnly {
				aicWant(t, r, http.StatusForbidden, errcode.ErrForbidden.Code)
			} else if r.Status == http.StatusForbidden || r.Status == http.StatusUnauthorized {
				t.Fatalf("admin 应能访问：%s", r.Raw)
			}
		})
		t.Run("super_admin 放行："+name, func(t *testing.T) {
			r := env.do(rt.method, aicBase+rt.path, "{}", aicSuperUser)
			if r.Status == http.StatusForbidden || r.Status == http.StatusUnauthorized {
				t.Fatalf("super_admin 应能访问：%s", r.Raw)
			}
		})
	}

	t.Run("旧的 /secrets 接口已删除（任何角色都是 404）", func(t *testing.T) {
		for _, rt := range []struct{ method, path string }{{http.MethodGet, "/secrets"}, {http.MethodPut, "/secrets/x"}} {
			aicWant(t, env.super(rt.method, aicBase+rt.path, map[string]any{"value": "v"}), http.StatusNotFound, errcode.ErrNotFound.Code)
		}
	})
}

func TestAdminMeHandler_Me(t *testing.T) {
	env := aicNewEnv(t)
	tests := []struct {
		user     int
		wantRole string
	}{{aicAdminUser, "admin"}, {aicSuperUser, "super_admin"}}
	for _, tt := range tests {
		t.Run(tt.wantRole, func(t *testing.T) {
			r := env.do(http.MethodGet, aicBase+"/me", nil, tt.user)
			aicWant(t, r, http.StatusOK, 0)
			var d struct {
				UserID uint64 `json:"user_id"`
				Role   string `json:"role"`
			}
			if err := json.Unmarshal(r.Data, &d); err != nil || d.Role != tt.wantRole || d.UserID != uint64(tt.user) {
				t.Fatalf("响应不符合预期：%v %s", err, r.Raw)
			}
		})
	}
	t.Run("普通用户 403、未登录 401", func(t *testing.T) {
		aicWant(t, env.do(http.MethodGet, aicBase+"/me", nil, aicNormalUser), http.StatusForbidden, errcode.ErrForbidden.Code)
		aicWant(t, env.do(http.MethodGet, aicBase+"/me", nil, 0), http.StatusUnauthorized, errcode.ErrUnauthorized.Code)
	})
}

// ---------------------------------------------------------------------------
// 插件
// ---------------------------------------------------------------------------

func TestAdminPluginHandler_Upload(t *testing.T) {
	newEnv := func(t *testing.T) *aicEnv {
		env := aicNewEnv(t)
		env.checker.ByCode["good-v1"] = &provider.PrecheckResult{OK: true, Meta: json.RawMessage(aicMeta("kling", "1.0.0"))}
		env.checker.ByCode["bad"] = &provider.PrecheckResult{OK: false, Issues: []modelcfg.Issue{{Path: "meta.endpoints.video.mode", Message: "必须是 sync / async"}}}
		return env
	}

	t.Run("成功：accepted=true，返回新登记的版本（不含代码）", func(t *testing.T) {
		env := newEnv(t)
		r := env.upload("file", "kling.js", []byte("good-v1"), aicSuperUser)
		aicWant(t, r, http.StatusOK, 0)
		var d struct {
			Accepted bool             `json:"accepted"`
			Issues   []map[string]any `json:"issues"`
			Version  map[string]any   `json:"version"`
		}
		if err := json.Unmarshal(r.Data, &d); err != nil || !d.Accepted || d.Issues == nil || len(d.Issues) != 0 {
			t.Fatalf("响应不符合预期：%v %s", err, r.Raw)
		}
		if d.Version["plugin_key"] != "kling" || d.Version["version"] != "1.0.0" || len(fmt.Sprint(d.Version["sha256"])) != 64 || d.Version["created_by"] != float64(aicSuperUser) {
			t.Fatalf("版本不符合预期：%v", d.Version)
		}
		if strings.Contains(r.Raw, "good-v1") {
			t.Fatalf("响应不应回显插件代码：%s", r.Raw)
		}
		// 上传后列表里能看到，并写了审计
		list := env.admin(http.MethodGet, aicBase+"/plugins", nil)
		if !strings.Contains(list.Raw, `"key":"kling"`) || !strings.Contains(list.Raw, `"source":"uploaded"`) {
			t.Fatalf("列表不符合预期：%s", list.Raw)
		}
		if got := env.repo.AuditActions(); len(got) != 1 || got[0] != model.AuditPluginUpload || env.repo.Audits[0].ActorID != aicSuperUser {
			t.Fatalf("审计不符合预期：%v", got)
		}
	})

	t.Run("预检不通过也是 200：accepted=false + 问题列表 + version=null", func(t *testing.T) {
		env := newEnv(t)
		r := env.upload("file", "bad.js", []byte("bad"), aicSuperUser)
		aicWant(t, r, http.StatusOK, 0)
		if !strings.Contains(r.Raw, `"accepted":false`) || !strings.Contains(r.Raw, `"path":"meta.endpoints.video.mode"`) || !strings.Contains(r.Raw, `"version":null`) {
			t.Fatalf("响应不符合预期：%s", r.Raw)
		}
	})

	t.Run("版本号重复：200 + issues 报在 meta.version", func(t *testing.T) {
		env := newEnv(t)
		aicWant(t, env.upload("file", "a.js", []byte("good-v1"), aicSuperUser), http.StatusOK, 0)
		r := env.upload("file", "a.js", []byte("good-v1"), aicSuperUser)
		aicWant(t, r, http.StatusOK, 0)
		if !strings.Contains(r.Raw, `"accepted":false`) || !strings.Contains(r.Raw, `"path":"meta.version"`) {
			t.Fatalf("响应不符合预期：%s", r.Raw)
		}
	})

	t.Run("没有 file 字段 400", func(t *testing.T) {
		aicWant(t, newEnv(t).upload("", "", nil, aicSuperUser), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
	})
	t.Run("不是 multipart 400", func(t *testing.T) {
		aicWant(t, newEnv(t).super(http.MethodPost, aicBase+"/plugins", map[string]any{"file": "x"}), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
	})
	t.Run("空文件 400", func(t *testing.T) {
		aicWant(t, newEnv(t).upload("file", "a.js", nil, aicSuperUser), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
	})
	t.Run("文件超过 512KB：413（50007）", func(t *testing.T) {
		aicWant(t, newEnv(t).upload("file", "big.js", make([]byte, 513<<10), aicSuperUser), http.StatusRequestEntityTooLarge, errcode.ErrPluginTooLarge.Code)
	})
	t.Run("请求体远超上限：读取阶段就被截断，同样 413", func(t *testing.T) {
		aicWant(t, newEnv(t).upload("file", "huge.js", make([]byte, 2<<20), aicSuperUser), http.StatusRequestEntityTooLarge, errcode.ErrPluginTooLarge.Code)
	})
	t.Run("往内置插件上传 409（50006）", func(t *testing.T) {
		env := newEnv(t)
		env.seedPlugin(t, "kling", "0.1.0", model.PluginSourceBuiltin)
		aicWant(t, env.upload("file", "a.js", []byte("good-v1"), aicSuperUser), http.StatusConflict, errcode.ErrPluginBuiltin.Code)
	})
	t.Run("runner 不可用 503（50021）", func(t *testing.T) {
		env := newEnv(t)
		env.checker.Err = &provider.Error{Class: provider.ClassRetryable, Code: provider.CodeRunnerUnavailable, Message: "down"}
		aicWant(t, env.upload("file", "a.js", []byte("x"), aicSuperUser), http.StatusServiceUnavailable, errcode.ErrRunnerUnavailable.Code)
	})
}

func TestAdminPluginHandler_ListEnabledDelete(t *testing.T) {
	env := aicNewEnv(t)
	id := env.seedPlugin(t, "kling", "1.0.0", model.PluginSourceUploaded)
	env.seedPlugin(t, "newapi", "1.0.0", model.PluginSourceBuiltin)

	t.Run("列表：admin 可读，带版本与 meta", func(t *testing.T) {
		r := env.admin(http.MethodGet, aicBase+"/plugins", nil)
		aicWant(t, r, http.StatusOK, 0)
		var list []struct {
			Key      string `json:"key"`
			Versions []struct {
				Version      string         `json:"version"`
				ChannelCount int            `json:"channel_count"`
				Meta         map[string]any `json:"meta"`
			} `json:"versions"`
		}
		if err := json.Unmarshal(r.Data, &list); err != nil || len(list) != 2 || len(list[0].Versions) != 1 || list[0].Versions[0].Meta["key"] != "kling" {
			t.Fatalf("列表不符合预期：%v %s", err, r.Raw)
		}
		if strings.Contains(r.Raw, `"code":"`) {
			t.Fatalf("列表不应包含代码：%s", r.Raw)
		}
	})
	t.Run("启停", func(t *testing.T) {
		aicWant(t, env.super(http.MethodPut, aicBase+"/plugins/kling/enabled", map[string]any{"enabled": false}), http.StatusOK, 0)
		if env.repo.Plugins["kling"].Enabled {
			t.Fatal("应已停用")
		}
		aicWant(t, env.super(http.MethodPut, aicBase+"/plugins/kling/enabled", map[string]any{"enabled": true}), http.StatusOK, 0)
	})
	t.Run("启停参数校验与不存在", func(t *testing.T) {
		aicWant(t, env.super(http.MethodPut, aicBase+"/plugins/kling/enabled", map[string]any{}), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
		aicWant(t, env.super(http.MethodPut, aicBase+"/plugins/kling/enabled", map[string]any{"enabled": "yes"}), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
		aicWant(t, env.super(http.MethodPut, aicBase+"/plugins/ghost/enabled", map[string]any{"enabled": true}), http.StatusNotFound, errcode.ErrPluginNotFound.Code)
	})
	t.Run("删除：被渠道引用 409（50005）", func(t *testing.T) {
		env.repo.Channels["c1"] = &model.AIChannel{Key: "c1", PluginKey: "kling", PluginVersionID: id}
		aicWant(t, env.super(http.MethodDelete, aicBase+"/plugins/kling/versions/1.0.0", nil), http.StatusConflict, errcode.ErrPluginInUse.Code)
		delete(env.repo.Channels, "c1")
	})
	t.Run("删除：内置插件 409（50006）；不存在 404", func(t *testing.T) {
		aicWant(t, env.super(http.MethodDelete, aicBase+"/plugins/newapi/versions/1.0.0", nil), http.StatusConflict, errcode.ErrPluginBuiltin.Code)
		aicWant(t, env.super(http.MethodDelete, aicBase+"/plugins/kling/versions/9.9.9", nil), http.StatusNotFound, errcode.ErrPluginNotFound.Code)
		aicWant(t, env.super(http.MethodDelete, aicBase+"/plugins/ghost/versions/1.0.0", nil), http.StatusNotFound, errcode.ErrPluginNotFound.Code)
	})
	t.Run("删除成功", func(t *testing.T) {
		aicWant(t, env.super(http.MethodDelete, aicBase+"/plugins/kling/versions/1.0.0", nil), http.StatusOK, 0)
		if _, ok := env.repo.Versions[id]; ok {
			t.Fatal("版本应已删除")
		}
	})
}

// ---------------------------------------------------------------------------
// 渠道
// ---------------------------------------------------------------------------

func TestAdminChannelHandler_CreateGetUpdate(t *testing.T) {
	env := aicNewEnv(t)
	env.seedPlugin(t, "kling", "1.0.0", model.PluginSourceUploaded)
	env.seedPlugin(t, "kling", "1.1.0", model.PluginSourceUploaded)
	path := aicBase + "/channels"

	t.Run("新建成功：视图带 secret_set=false 与 plugin_version", func(t *testing.T) {
		r := env.super(http.MethodPost, path, aicChannelJSON("kling-main", "1.0.0"))
		aicWant(t, r, http.StatusOK, 0)
		var v map[string]any
		if err := json.Unmarshal(r.Data, &v); err != nil {
			t.Fatal(err)
		}
		if v["key"] != "kling-main" || v["plugin_version"] != "1.0.0" || v["secret_set"] != false || v["enabled"] != true ||
			v["trusted_internal"] != false || v["allow_credentials"] != false || v["base_url"] != "https://gw.example.com" {
			t.Fatalf("视图不符合预期：%v", v)
		}
		if st := v["settings"].(map[string]any); st["tenant"] != "t1" || st["region"] != "cn" {
			t.Fatalf("settings 应带默认值：%v", st)
		}
		if rl := v["rate_limit"].(map[string]any); rl["rps"] != float64(5) || rl["max_concurrency"] != float64(20) {
			t.Fatalf("rate_limit 不符合预期：%v", rl)
		}
		if strings.Contains(r.Raw, "settings_json") || strings.Contains(r.Raw, "rate_limit_json") {
			t.Fatalf("不应暴露存储列名：%s", r.Raw)
		}
	})
	t.Run("key 重复 409（50012）", func(t *testing.T) {
		aicWant(t, env.super(http.MethodPost, path, aicChannelJSON("kling-main", "1.0.0")), http.StatusConflict, errcode.ErrChannelExists.Code)
	})
	t.Run("缺少必填字段 400（10001）", func(t *testing.T) {
		for _, missing := range []string{"key", "name", "plugin_key", "plugin_version", "base_url"} {
			body := aicChannelJSON("kling-x", "1.0.0")
			delete(body, missing)
			aicWant(t, env.super(http.MethodPost, path, body), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
		}
		aicWant(t, env.super(http.MethodPost, path, "{not json"), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
	})
	t.Run("业务校验失败 400（50013），原因在 msg", func(t *testing.T) {
		bad := aicChannelJSON("Bad_Key", "1.0.0")
		bad["base_url"] = "https://u:p@x.com"
		bad["rate_limit"] = map[string]any{"rps": -1}
		r := env.super(http.MethodPost, path, bad)
		aicWant(t, r, http.StatusBadRequest, errcode.ErrChannelInvalid.Code)
		for _, want := range []string{"key", "用户名", "rate_limit"} {
			if !strings.Contains(r.Msg, want) {
				t.Fatalf("msg 应包含 %q：%s", want, r.Msg)
			}
		}
		noTenant := aicChannelJSON("kling-y", "1.0.0")
		noTenant["settings"] = map[string]any{}
		r = env.super(http.MethodPost, path, noTenant)
		aicWant(t, r, http.StatusBadRequest, errcode.ErrChannelInvalid.Code)
		if !strings.Contains(r.Msg, "settings.tenant") {
			t.Fatalf("msg 应指出必填设置项：%s", r.Msg)
		}
	})
	t.Run("enabled=false 与两个安全开关可在创建时指定", func(t *testing.T) {
		body := aicChannelJSON("kling-off", "1.0.0")
		body["enabled"], body["trusted_internal"], body["allow_credentials"] = false, true, true
		r := env.super(http.MethodPost, path, body)
		aicWant(t, r, http.StatusOK, 0)
		if !strings.Contains(r.Raw, `"enabled":false`) || !strings.Contains(r.Raw, `"trusted_internal":true`) || !strings.Contains(r.Raw, `"allow_credentials":true`) {
			t.Fatalf("响应不符合预期：%s", r.Raw)
		}
	})
	t.Run("列表与详情：admin 可读", func(t *testing.T) {
		r := env.admin(http.MethodGet, path, nil)
		aicWant(t, r, http.StatusOK, 0)
		var list []map[string]any
		if err := json.Unmarshal(r.Data, &list); err != nil || len(list) != 2 || list[0]["key"] != "kling-main" {
			t.Fatalf("列表不符合预期：%v %s", err, r.Raw)
		}
		aicWant(t, env.admin(http.MethodGet, path+"/kling-main", nil), http.StatusOK, 0)
		aicWant(t, env.admin(http.MethodGet, path+"/ghost", nil), http.StatusNotFound, errcode.ErrChannelNotFound.Code)
	})
	t.Run("更新：字段可选，改 plugin_version 即升级切换", func(t *testing.T) {
		r := env.super(http.MethodPut, path+"/kling-main", map[string]any{"name": "改名", "plugin_version": "1.1.0", "enabled": false})
		aicWant(t, r, http.StatusOK, 0)
		if !strings.Contains(r.Raw, `"name":"改名"`) || !strings.Contains(r.Raw, `"plugin_version":"1.1.0"`) || !strings.Contains(r.Raw, `"enabled":false`) ||
			!strings.Contains(r.Raw, `"base_url":"https://gw.example.com"`) {
			t.Fatalf("响应不符合预期：%s", r.Raw)
		}
	})
	t.Run("更新：校验失败 400、不存在 404、name 超长 400", func(t *testing.T) {
		aicWant(t, env.super(http.MethodPut, path+"/kling-main", map[string]any{"base_url": "ftp://x"}), http.StatusBadRequest, errcode.ErrChannelInvalid.Code)
		aicWant(t, env.super(http.MethodPut, path+"/kling-main", map[string]any{"plugin_version": "9.9.9"}), http.StatusBadRequest, errcode.ErrChannelInvalid.Code)
		aicWant(t, env.super(http.MethodPut, path+"/ghost", map[string]any{"name": "x"}), http.StatusNotFound, errcode.ErrChannelNotFound.Code)
		aicWant(t, env.super(http.MethodPut, path+"/kling-main", map[string]any{"name": strings.Repeat("长", 200)}), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
	})
}

func TestAdminChannelHandler_Secret(t *testing.T) {
	env := aicNewEnv(t)
	env.seedPlugin(t, "kling", "1.0.0", model.PluginSourceUploaded)
	aicWant(t, env.super(http.MethodPost, aicBase+"/channels", aicChannelJSON("kling-main", "1.0.0")), http.StatusOK, 0)
	path := aicBase + "/channels/kling-main/secret"

	t.Run("只写：成功不回显，详情里 secret_set=true，Key 出现在任何响应里都不行", func(t *testing.T) {
		r := env.super(http.MethodPut, path, map[string]any{"value": "sk-plain-text-123"})
		aicWant(t, r, http.StatusOK, 0)
		if strings.Contains(r.Raw, "sk-plain") || strings.Contains(r.Raw, `"data"`) {
			t.Fatalf("响应不得回显凭证，data 为空时应省略：%s", r.Raw)
		}
		for _, p := range []string{"/channels/kling-main", "/channels"} {
			g := env.admin(http.MethodGet, aicBase+p, nil)
			if strings.Contains(g.Raw, "sk-plain") || !strings.Contains(g.Raw, `"secret_set":true`) {
				t.Fatalf("GET %s 不符合预期：%s", p, g.Raw)
			}
		}
		if row := env.repo.Secrets["channel:kling-main"]; row == nil || bytes.Contains(row.Ciphertext, []byte("sk-plain")) {
			t.Fatal("凭证应以密文存入 channel:<key>")
		}
	})
	t.Run("审计不含凭证", func(t *testing.T) {
		b, _ := json.Marshal(env.repo.Audits)
		if strings.Contains(string(b), "sk-plain") || !strings.Contains(string(b), model.AuditChannelSecret) {
			t.Fatalf("审计不符合预期：%s", b)
		}
	})
	t.Run("缺少 value、value 超长 400；渠道不存在 404", func(t *testing.T) {
		aicWant(t, env.super(http.MethodPut, path, map[string]any{}), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
		aicWant(t, env.super(http.MethodPut, path, map[string]any{"value": strings.Repeat("k", 4097)}), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
		aicWant(t, env.super(http.MethodPut, aicBase+"/channels/ghost/secret", map[string]any{"value": "x"}), http.StatusNotFound, errcode.ErrChannelNotFound.Code)
	})
	t.Run("没有主密钥时返回明确的 500 提示", func(t *testing.T) {
		env2 := aicNewEnv(t)
		env2.seedPlugin(t, "kling", "1.0.0", model.PluginSourceUploaded)
		aicWant(t, env2.super(http.MethodPost, aicBase+"/channels", aicChannelJSON("c", "1.0.0")), http.StatusOK, 0)
		noKey := service.NewAIConfigService(env2.repo, env2.repo, env2.repo, "")
		eng := router.New(gin.TestMode, aicJWTSecret, router.Handlers{
			AdminChannel: NewAdminChannelHandler(service.NewAIChannelService(env2.repo, env2.repo, noKey, env2.repo, env2.ops, noKey)),
			AdminRole:    func(context.Context, uint64) (string, error) { return model.RoleSuperAdmin, nil },
		})
		env2.engine = eng
		r := env2.super(http.MethodPut, aicBase+"/channels/c/secret", map[string]any{"value": "v"})
		aicWant(t, r, http.StatusInternalServerError, errcode.ErrInternal.Code)
		if !strings.Contains(r.Msg, "APP_AI_SECRET_KEY") {
			t.Fatalf("提示应说明缺少主密钥：%s", r.Raw)
		}
	})
}

func TestAdminChannelHandler_CheckAndImport(t *testing.T) {
	env := aicNewEnv(t)
	env.seedPlugin(t, "kling", "1.0.0", model.PluginSourceUploaded)
	aicWant(t, env.super(http.MethodPost, aicBase+"/channels", aicChannelJSON("kling-main", "1.0.0")), http.StatusOK, 0)
	check := aicBase + "/channels/kling-main/check"
	imp := aicBase + "/channels/kling-main/import"

	t.Run("Key 未设置：检查与导入都是 409（50015）", func(t *testing.T) {
		aicWant(t, env.super(http.MethodPost, check, nil), http.StatusConflict, errcode.ErrChannelSecretUnset.Code)
		aicWant(t, env.admin(http.MethodPost, imp, map[string]any{"args": map[string]any{"limit": 1}}), http.StatusConflict, errcode.ErrChannelSecretUnset.Code)
	})

	aicWant(t, env.super(http.MethodPut, aicBase+"/channels/kling-main/secret", map[string]any{"value": "sk-secret-value"}), http.StatusOK, 0)

	t.Run("检查成功", func(t *testing.T) {
		env.ops.CheckResult = &provider.CheckResult{OK: true, Message: "HTTP 200", DurationMs: 120}
		r := env.super(http.MethodPost, check, nil)
		aicWant(t, r, http.StatusOK, 0)
		if !strings.Contains(r.Raw, `"ok":true`) || !strings.Contains(r.Raw, `"message":"HTTP 200"`) || !strings.Contains(r.Raw, `"duration_ms":120`) {
			t.Fatalf("响应不符合预期：%s", r.Raw)
		}
	})
	t.Run("插件不支持检查：200 + ok=false", func(t *testing.T) {
		env.ops.CheckErr = provider.ErrCheckUnsupported
		r := env.super(http.MethodPost, check, nil)
		aicWant(t, r, http.StatusOK, 0)
		if !strings.Contains(r.Raw, `"ok":false`) || !strings.Contains(r.Raw, "插件不支持连通性检查") {
			t.Fatalf("响应不符合预期：%s", r.Raw)
		}
		env.ops.CheckErr = nil
	})
	t.Run("runner 不可用 503；渠道不存在 404", func(t *testing.T) {
		env.ops.CheckErr = &provider.Error{Class: provider.ClassRetryable, Code: provider.CodeRunnerUnavailable, Message: "down"}
		aicWant(t, env.super(http.MethodPost, check, nil), http.StatusServiceUnavailable, errcode.ErrRunnerUnavailable.Code)
		env.ops.CheckErr = nil
		aicWant(t, env.super(http.MethodPost, aicBase+"/channels/ghost/check", nil), http.StatusNotFound, errcode.ErrChannelNotFound.Code)
	})
	t.Run("导入成功：admin 可调，返回草稿", func(t *testing.T) {
		env.ops.Drafts = []provider.ModelDraft{{UpstreamModel: "kling-v2", Kind: "video", Label: "可灵 v2"}}
		r := env.admin(http.MethodPost, imp, map[string]any{"args": map[string]any{"limit": 10}})
		aicWant(t, r, http.StatusOK, 0)
		if !strings.Contains(r.Raw, `"drafts":[`) || !strings.Contains(r.Raw, `"upstream_model":"kling-v2"`) {
			t.Fatalf("响应不符合预期：%s", r.Raw)
		}
		if env.ops.GotArgs["limit"] != float64(10) {
			t.Fatalf("参数应传给宿主：%v", env.ops.GotArgs)
		}
	})
	t.Run("导入：缺少必填参数 400；没有请求体等同空参数（同样 400）", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPost, imp, map[string]any{"args": map[string]any{}}), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
		aicWant(t, env.admin(http.MethodPost, imp, nil), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
		aicWant(t, env.admin(http.MethodPost, imp, "{not json"), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
	})
	t.Run("导入：插件不支持 502（50022）", func(t *testing.T) {
		env.ops.ImportErr = provider.ErrImportUnsupported
		aicWant(t, env.admin(http.MethodPost, imp, map[string]any{"args": map[string]any{"limit": 1}}), http.StatusBadGateway, errcode.ErrPluginOpFailed.Code)
		env.ops.ImportErr = nil
	})
}

// ---------------------------------------------------------------------------
// 模型：草稿 / 校验 / 发布 / 回滚 / 列表 / 试跑 / 追踪 / schema
// ---------------------------------------------------------------------------

func TestAdminAIHandler_ConfigLifecycle(t *testing.T) {
	env := aicNewEnv(t)
	env.seedPlugin(t, "kling", "1.0.0", model.PluginSourceUploaded)
	aicWant(t, env.super(http.MethodPost, aicBase+"/channels", aicChannelJSON("kling-main", "1.0.0")), http.StatusOK, 0)

	t.Run("新建模型草稿：成功并返回 issues 列表", func(t *testing.T) {
		r := env.admin(http.MethodPost, aicBase+"/models", map[string]any{"body": aicModelJSON("m1", "video", "kling-main"), "note": "初版"})
		aicWant(t, r, http.StatusOK, 0)
		var d struct {
			Revision model.AIConfigRevision `json:"revision"`
			Issues   []any                  `json:"issues"`
		}
		if err := json.Unmarshal(r.Data, &d); err != nil || d.Revision.RevisionNo != 1 || d.Revision.Note != "初版" || d.Revision.CreatedBy != aicAdminUser || d.Issues == nil || len(d.Issues) != 0 {
			t.Fatalf("响应不符合预期：%v %s", err, r.Raw)
		}
	})
	t.Run("重复新建返回 400 + 配置错误码", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPost, aicBase+"/models", map[string]any{"body": aicModelJSON("m1", "video", "kling-main")}), http.StatusBadRequest, errcode.ErrConfigInvalid.Code)
	})
	t.Run("参数错误：缺 body、body 不是对象、非法 JSON", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPost, aicBase+"/models", map[string]any{"note": "x"}), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
		aicWant(t, env.admin(http.MethodPost, aicBase+"/models", map[string]any{"body": []int{1}}), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
		aicWant(t, env.admin(http.MethodPost, aicBase+"/models", "{not json"), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
	})
	t.Run("PUT：路径 key 与正文不一致 400；不存在 404；成功版本号递增", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPut, aicBase+"/models/other", map[string]any{"body": aicModelJSON("m1", "video", "kling-main")}), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
		aicWant(t, env.admin(http.MethodPut, aicBase+"/models/none", map[string]any{"body": aicModelJSON("none", "video", "kling-main")}), http.StatusNotFound, errcode.ErrConfigNotFound.Code)
		r := env.admin(http.MethodPut, aicBase+"/models/m1", map[string]any{"body": aicModelJSON("m1", "video", "kling-main")})
		aicWant(t, r, http.StatusOK, 0)
		if !strings.Contains(r.Raw, `"revision_no":2`) {
			t.Fatalf("revision_no 应为 2：%s", r.Raw)
		}
	})
	t.Run("校验：不传 body 校验草稿；传 body 校验传入内容（渠道不存在也会报）", func(t *testing.T) {
		r := env.admin(http.MethodPost, aicBase+"/models/m1/validate", nil)
		aicWant(t, r, http.StatusOK, 0)
		if !strings.Contains(r.Raw, `"valid":true`) {
			t.Fatalf("草稿应通过校验：%s", r.Raw)
		}
		r = env.admin(http.MethodPost, aicBase+"/models/m1/validate", map[string]any{"body": aicModelJSON("m1", "video", "ghost")})
		aicWant(t, r, http.StatusOK, 0)
		if !strings.Contains(r.Raw, `"valid":false`) || !strings.Contains(r.Raw, `"path":"channels[0].channel"`) {
			t.Fatalf("应返回带路径的问题：%s", r.Raw)
		}
		aicWant(t, env.admin(http.MethodPost, aicBase+"/models/none/validate", nil), http.StatusConflict, errcode.ErrConfigNoDraft.Code)
	})
	t.Run("渠道 Key 未设置时发布返回 409（50015）", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPost, aicBase+"/models/m1/publish", nil), http.StatusConflict, errcode.ErrChannelSecretUnset.Code)
	})
	t.Run("渠道停用时发布返回 409（50014）", func(t *testing.T) {
		aicWant(t, env.super(http.MethodPut, aicBase+"/channels/kling-main", map[string]any{"enabled": false}), http.StatusOK, 0)
		aicWant(t, env.admin(http.MethodPost, aicBase+"/models/m1/publish", nil), http.StatusConflict, errcode.ErrChannelDisabled.Code)
		aicWant(t, env.super(http.MethodPut, aicBase+"/channels/kling-main", map[string]any{"enabled": true}), http.StatusOK, 0)
	})
	t.Run("super_admin 设置 Key 后，admin 可以发布", func(t *testing.T) {
		aicWant(t, env.super(http.MethodPut, aicBase+"/channels/kling-main/secret", map[string]any{"value": "sk-1"}), http.StatusOK, 0)
		r := env.admin(http.MethodPost, aicBase+"/models/m1/publish", nil)
		aicWant(t, r, http.StatusOK, 0)
		if !strings.Contains(r.Raw, `"status":"published"`) {
			t.Fatalf("应返回已发布的 revision：%s", r.Raw)
		}
	})
	t.Run("发布后再发布返回 409（没有草稿）", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPost, aicBase+"/models/m1/publish", nil), http.StatusConflict, errcode.ErrConfigNoDraft.Code)
	})
	t.Run("详情包含已发布正文与 revision 元信息；不存在 404", func(t *testing.T) {
		r := env.admin(http.MethodGet, aicBase+"/models/m1", nil)
		aicWant(t, r, http.StatusOK, 0)
		var d service.ConfigDetail
		if err := json.Unmarshal(r.Data, &d); err != nil || d.Published == nil || d.Published.Status != model.RevisionPublished || len(d.Published.BodyJSON) == 0 || d.Draft != nil {
			t.Fatalf("详情不符合预期：%v %s", err, r.Raw)
		}
		aicWant(t, env.admin(http.MethodGet, aicBase+"/models/none", nil), http.StatusNotFound, errcode.ErrConfigNotFound.Code)
	})
	t.Run("再改一版并发布，然后回滚到第一次发布的版本", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPut, aicBase+"/models/m1", map[string]any{"body": aicModelJSON("m1", "video", "kling-main")}), http.StatusOK, 0)
		aicWant(t, env.admin(http.MethodPost, aicBase+"/models/m1/publish", nil), http.StatusOK, 0)

		r := env.admin(http.MethodGet, aicBase+"/models/m1/revisions", nil)
		aicWant(t, r, http.StatusOK, 0)
		var revs []model.AIConfigRevision
		_ = json.Unmarshal(r.Data, &revs)
		if len(revs) < 3 || strings.Contains(r.Raw, `body_json":{`) {
			t.Fatalf("历史列表不符合预期（应不含正文）：%s", r.Raw)
		}
		var first uint64
		for _, rv := range revs {
			if rv.RevisionNo == 2 { // 第 2 版是第一次发布的版本，此时已归档
				first = rv.ID
			}
		}
		aicWant(t, env.admin(http.MethodPost, aicBase+"/models/m1/rollback", map[string]any{"revision_id": first}), http.StatusOK, 0)
		r = env.admin(http.MethodGet, fmt.Sprintf("%s/models/m1/revisions/%d", aicBase, first), nil)
		aicWant(t, r, http.StatusOK, 0)
		if !strings.Contains(r.Raw, "body_json") {
			t.Fatalf("单个 revision 应带正文：%s", r.Raw)
		}
	})
	t.Run("回滚参数校验；revision id 非法", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPost, aicBase+"/models/m1/rollback", map[string]any{}), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
		aicWant(t, env.admin(http.MethodPost, aicBase+"/models/m1/rollback", map[string]any{"revision_id": 99999}), http.StatusNotFound, errcode.ErrConfigNotFound.Code)
		aicWant(t, env.admin(http.MethodGet, aicBase+"/models/m1/revisions/abc", nil), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
	})
	t.Run("列表带 label 与 channel，且不含正文", func(t *testing.T) {
		r := env.admin(http.MethodGet, aicBase+"/models", nil)
		aicWant(t, r, http.StatusOK, 0)
		var list []map[string]any
		if err := json.Unmarshal(r.Data, &list); err != nil || len(list) != 1 {
			t.Fatalf("列表不符合预期：%v %s", err, r.Raw)
		}
		if list[0]["key"] != "m1" || list[0]["label"] != "模型-m1" || list[0]["channel"] != "kling-main" || list[0]["kind"] != "video" {
			t.Fatalf("列表项字段不符合预期：%v", list[0])
		}
		if strings.Contains(r.Raw, "body_json") || strings.Contains(r.Raw, "capabilities") {
			t.Fatalf("列表不应包含正文：%s", r.Raw)
		}
		if tags, ok := list[0]["tags"].([]any); !ok || len(tags) != 0 || list[0]["vendor"] != "" {
			t.Fatalf("没有 vendor / tags 时应为 \"\" 与 []：%v", list[0])
		}
	})
	t.Run("列表带 vendor 与 tags", func(t *testing.T) {
		body := aicModelJSON("m2", "video", "kling-main")
		body["vendor"], body["tags"] = "kling", []any{"推荐", "带音轨"}
		aicWant(t, env.super(http.MethodPost, aicBase+"/models", map[string]any{"body": body}), http.StatusOK, 0)
		r := env.admin(http.MethodGet, aicBase+"/models", nil)
		var list []map[string]any
		if err := json.Unmarshal(r.Data, &list); err != nil {
			t.Fatal(err)
		}
		for _, it := range list {
			if it["key"] != "m2" {
				continue
			}
			tags, _ := it["tags"].([]any)
			if it["vendor"] != "kling" || len(tags) != 2 || tags[0] != "推荐" {
				t.Fatalf("vendor / tags 不符合预期：%v", it)
			}
			return
		}
		t.Fatalf("列表里没有 m2：%s", r.Raw)
	})
}

func TestAdminAIHandler_ModelEnabledAndSort(t *testing.T) {
	env := aicNewEnv(t)
	env.publishAll(t)
	base := aicBase + "/models/m1"

	tests := []struct {
		name         string
		method, path string
		body         any
		status, code int
	}{
		{"下架", http.MethodPut, base + "/enabled", map[string]any{"enabled": false}, 200, 0},
		{"上架", http.MethodPut, base + "/enabled", map[string]any{"enabled": true}, 200, 0},
		{"enabled 缺失", http.MethodPut, base + "/enabled", map[string]any{}, 400, errcode.ErrInvalidParams.Code},
		{"enabled 类型错误", http.MethodPut, base + "/enabled", map[string]any{"enabled": "yes"}, 400, errcode.ErrInvalidParams.Code},
		{"模型不存在", http.MethodPut, aicBase + "/models/none/enabled", map[string]any{"enabled": true}, 404, errcode.ErrConfigNotFound.Code},
		{"修改排序", http.MethodPut, base + "/sort", map[string]any{"sort": 5}, 200, 0},
		{"排序值可以是 0", http.MethodPut, base + "/sort", map[string]any{"sort": 0}, 200, 0},
		{"sort 缺失", http.MethodPut, base + "/sort", map[string]any{}, 400, errcode.ErrInvalidParams.Code},
		{"排序：模型不存在", http.MethodPut, aicBase + "/models/none/sort", map[string]any{"sort": 1}, 404, errcode.ErrConfigNotFound.Code},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			aicWant(t, env.admin(tt.method, tt.path, tt.body), tt.status, tt.code)
		})
	}
}

func TestAdminAIHandler_DryRunTestRunAndTrace(t *testing.T) {
	env := aicNewEnv(t)
	env.publishAll(t)
	// 再存一个草稿用于试跑
	aicWant(t, env.admin(http.MethodPut, aicBase+"/models/m1", map[string]any{"body": aicModelJSON("m1", "video", "kling-main")}), http.StatusOK, 0)
	base := aicBase + "/models/m1"

	t.Run("dry-run 成功且响应不含凭证", func(t *testing.T) {
		env.dry.Result = map[string]any{"submit": map[string]any{"headers": map[string]any{"X-Echo": "Bearer sk-super-secret"}}}
		r := env.admin(http.MethodPost, base+"/dry-run", map[string]any{"input": map[string]any{"prompt": "hi"}})
		aicWant(t, r, http.StatusOK, 0)
		if strings.Contains(r.Raw, "sk-super-secret") || !strings.Contains(r.Raw, "***") {
			t.Fatalf("响应不得包含凭证明文：%s", r.Raw)
		}
	})
	t.Run("dry-run 缺少 input 400；输入不合法 400 + 生成参数错误码；模型不存在 404", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPost, base+"/dry-run", map[string]any{}), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
		aicWant(t, env.admin(http.MethodPost, base+"/dry-run", map[string]any{"input": map[string]any{"invalid": true}}), http.StatusBadRequest, errcode.ErrTaskInput.Code)
		aicWant(t, env.admin(http.MethodPost, aicBase+"/models/none/dry-run", map[string]any{"input": map[string]any{}}), http.StatusNotFound, errcode.ErrConfigNotFound.Code)
	})
	t.Run("试跑返回任务视图，并能轮询查询", func(t *testing.T) {
		env.tasks.View = &model.GenerationTaskView{ID: 88, Status: model.TaskPending}
		r := env.admin(http.MethodPost, base+"/test-run", map[string]any{"input": map[string]any{"prompt": "hi"}})
		aicWant(t, r, http.StatusOK, 0)
		if !strings.Contains(r.Raw, `"id":88`) || env.tasks.UserID != aicAdminUser {
			t.Fatalf("试跑响应不符合预期：%s user=%d", r.Raw, env.tasks.UserID)
		}
		env.tasks.Views[88] = aiconfigfake.TestView{UserID: aicAdminUser, View: &model.GenerationTaskView{ID: 88, Status: model.TaskRunning}}
		r = env.admin(http.MethodGet, aicBase+"/test-runs/88", nil)
		aicWant(t, r, http.StatusOK, 0)
		if !strings.Contains(r.Raw, `"status":"running"`) {
			t.Fatalf("轮询结果不符合预期：%s", r.Raw)
		}
	})
	t.Run("试跑缺少 input 400；不存在的试跑任务 404；id 非法 400", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPost, base+"/test-run", map[string]any{}), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
		aicWant(t, env.admin(http.MethodGet, aicBase+"/test-runs/999", nil), http.StatusNotFound, errcode.ErrTaskNotFound.Code)
		aicWant(t, env.admin(http.MethodGet, aicBase+"/test-runs/abc", nil), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
	})
	t.Run("追踪：没有追踪时 steps 是空数组", func(t *testing.T) {
		r := env.admin(http.MethodGet, aicBase+"/test-runs/88/trace", nil)
		aicWant(t, r, http.StatusOK, 0)
		if !strings.Contains(r.Raw, `"steps":[]`) {
			t.Fatalf("steps 应为空数组：%s", r.Raw)
		}
	})
	t.Run("追踪：返回每一步", func(t *testing.T) {
		env.tasks.Trace = []provider.TraceStep{
			{Name: "submit", Kind: "hook", Hook: &provider.TraceHook{Name: "buildSubmitRequest", Logs: []string{"hello"}}, DurationMs: 3},
			{Name: "submit", Kind: "http", Request: &provider.TraceRequest{Method: "POST"}, DurationMs: 120},
		}
		r := env.admin(http.MethodGet, aicBase+"/test-runs/88/trace", nil)
		aicWant(t, r, http.StatusOK, 0)
		if !strings.Contains(r.Raw, `"buildSubmitRequest"`) || !strings.Contains(r.Raw, `"kind":"http"`) || !strings.Contains(r.Raw, `"hello"`) {
			t.Fatalf("追踪不符合预期：%s", r.Raw)
		}
	})
	t.Run("追踪：只能查自己的试跑任务；不存在 404；id 非法 400", func(t *testing.T) {
		// 换成 super_admin 查 admin 创建的任务：查不到
		aicWant(t, env.super(http.MethodGet, aicBase+"/test-runs/88/trace", nil), http.StatusNotFound, errcode.ErrTaskNotFound.Code)
		aicWant(t, env.admin(http.MethodGet, aicBase+"/test-runs/999/trace", nil), http.StatusNotFound, errcode.ErrTaskNotFound.Code)
		aicWant(t, env.admin(http.MethodGet, aicBase+"/test-runs/abc/trace", nil), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
	})
}

func TestAdminAIHandler_Schema(t *testing.T) {
	env := aicNewEnv(t)
	t.Run("GET /schema/model 返回 JSON Schema（此前恒 400）", func(t *testing.T) {
		r := env.admin(http.MethodGet, aicBase+"/schema/model", nil)
		aicWant(t, r, http.StatusOK, 0)
		if !json.Valid(r.Data) || !strings.Contains(r.Raw, "capabilities") {
			t.Fatalf("应返回 JSON Schema：%s", r.Raw)
		}
	})
	t.Run("旧的 /schema/provider 已删除", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodGet, aicBase+"/schema/provider", nil), http.StatusNotFound, errcode.ErrNotFound.Code)
	})
}
