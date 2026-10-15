package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	. "video-canvas/internal/handler"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/utils"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/repository"
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
	lister   *aiconfigfake.ModelLister // 拉取上游模型清单的替身
	stats    *aiconfigfake.StatsRepo
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
		lister:   &aiconfigfake.ModelLister{},
		stats:    &aiconfigfake.StatsRepo{},
		checker:  &aiconfigfake.Prechecker{ByCode: map[string]*provider.PrecheckResult{}},
		roles:    map[uint64]string{aicAdminUser: model.RoleAdmin, aicNormalUser: model.RoleUser, aicSuperUser: model.RoleSuperAdmin},
		tokens:   map[int]string{},
		versions: map[string]uint64{},
	}
	cfg.SetDryRunner(env.dry)
	cfg.SetTestTaskCreator(env.tasks)

	for id := range env.roles {
		tok, _, err := utils.GenerateToken(uint(id), fmt.Sprintf("u%d", id), 0, aicJWTSecret, "test", 1)
		if err != nil {
			t.Fatal(err)
		}
		env.tokens[int(id)] = tok
	}
	lookup := func(_ context.Context, id uint64) (string, error) { return env.roles[id], nil }
	pluginSvc := service.NewAIPluginService(repo, cfg, repo, env.checker, cfg)
	channelSvc := service.NewAIChannelService(repo, cfg, cfg, env.lister, repo, cfg)
	env.engine = router.New(gin.TestMode, aicJWTSecret, router.Handlers{
		AIModel:      NewAIModelHandler(cfg),
		AdminAI:      NewAdminAIHandler(cfg),
		AdminCatalog: NewAdminCatalogHandler(service.NewAICatalogService(channelSvc, pluginSvc, cfg)),
		AdminPlugin:  NewAdminPluginHandler(pluginSvc),
		AdminChannel: NewAdminChannelHandler(channelSvc, cfg),
		AdminStats:   NewAdminStatsHandler(service.NewAIStatsService(env.stats, cfg)),
		AdminMe:      NewAdminMeHandler(lookup),
		AdminRole:    lookup,
		UserState:    aicActiveState,
	})
	return env
}

// aicActiveState 让所有测试用户都是“正常、token_version 为 0”的状态：这组用例测的是权限与业务，不是停用逻辑。
func aicActiveState(context.Context, uint64) (*middleware.UserState, error) {
	return &middleware.UserState{Status: model.UserStatusActive}, nil
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

// aicMeta 生成插件 meta：bearer 鉴权，支持 video / text，协议设置项 region（有默认）与 tenant（必填）。
// 协议设置项现在填在模型正文的 channels[0].protocol.settings 里，不再属于渠道。
func aicMeta(key, version string) string {
	return `{"apiVersion":1,"key":"` + key + `","name":"插件-` + key + `","version":"` + version + `","auth":{"type":"bearer"},` +
		`"endpoints":{"video":{"mode":"async"},"text":{"mode":"sync"}},` +
		`"channelSettings":{"region":{"type":"enum","label":"区域","options":["cn","global"],"default":"cn"},"tenant":{"type":"string","label":"租户","required":true}}}`
}

// seedPlugin 直接往仓储里放一个插件版本（不经过接口）。
func (e *aicEnv) seedPlugin(t *testing.T, key, version, source string) uint64 {
	t.Helper()
	return e.seedPluginMeta(t, key, version, source, aicMeta(key, version))
}

// seedPluginMeta 同 seedPlugin，meta 由调用方给（升级失败等用例需要特殊的 meta）。
func (e *aicEnv) seedPluginMeta(t *testing.T, key, version, source, meta string) uint64 {
	t.Helper()
	p := &model.AIPlugin{Key: key, Name: "插件-" + key, Source: source, Enabled: true}
	v := &model.AIPluginVersion{PluginKey: key, Version: version, SHA256: "seed", Code: "code", MetaJSON: model.JSONText(meta)}
	if err := e.repo.SaveVersion(context.Background(), p, v); err != nil {
		t.Fatal(err)
	}
	e.versions[key+"@"+version] = v.ID
	return v.ID
}

// seedProtocolModel 直接往仓储里放一个协议固定在 plugin@version 上的模型（不经过接口，不校验渠道与插件是否存在）。
func (e *aicEnv) seedProtocolModel(t *testing.T, key, channel, plugin, version string) {
	t.Helper()
	body := `{"key":"` + key + `","kind":"video","label":"模型-` + key + `","channels":[{"channel":"` + channel + `","upstream_model":"u",` +
		`"protocol":{"plugin":"` + plugin + `","version":"` + version + `","settings":{"tenant":"t1"}}}]}`
	if _, err := e.repo.SaveModel(context.Background(), repository.SaveModelInput{
		Pointer: repository.ConfigPointer{Target: model.ConfigTargetModel, Key: key, Kind: "video"}, Body: []byte(body),
	}); err != nil {
		t.Fatal(err)
	}
}

// aicChannelJSON 是一份合法的新建渠道请求体：渠道只管连接信息，协议在模型里选。
func aicChannelJSON(key string) map[string]any {
	return map[string]any{
		"key": key, "name": "渠道-" + key, "base_url": "https://gw.example.com",
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

// aicProtocol 是模型正文里 channels[0].protocol：固定在 kling 的指定版本上，填上必填的协议设置项 tenant。
func aicProtocol(version string) map[string]any {
	return map[string]any{"plugin": "kling", "version": version, "settings": map[string]any{"tenant": "t1"}}
}

func aicModelJSON(key, kind, channel string) map[string]any {
	return aicModelJSONV(key, kind, channel, "1.0.0")
}

// aicModelJSONV 同 aicModelJSON，协议固定在 kling 的指定版本上。
func aicModelJSONV(key, kind, channel, version string) map[string]any {
	return map[string]any{
		"key": key, "kind": kind, "label": "模型-" + key, "hint": "小字", "enabled": true, "sort": 10,
		"pricing":      map[string]any{"billing": "per_call", "unit": 5, "cost": map[string]any{"on": true, "unit": 2}},
		"channels":     []any{map[string]any{"channel": channel, "upstream_model": "kling-v2", "protocol": aicProtocol(version)}},
		"params":       map[string]any{"instanceType": "default"},
		"capabilities": aicCapabilities(kind),
	}
}

// enableAll 通过接口完成：（预置插件）→ 建渠道 → 设 Key → 保存模型 → 启用。
func (e *aicEnv) enableAll(t *testing.T) {
	t.Helper()
	e.seedPlugin(t, "kling", "1.0.0", model.PluginSourceUploaded)
	steps := []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, aicBase + "/channels", aicChannelJSON("kling-main")},
		{http.MethodPut, aicBase + "/channels/kling-main/secret", map[string]any{"value": "sk-super-secret"}},
		{http.MethodPost, aicBase + "/models", map[string]any{"body": aicModelJSON("m1", "video", "kling-main")}},
		{http.MethodPut, aicBase + "/models/m1/enabled", map[string]any{"enabled": true}},
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
	env.enableAll(t)

	t.Run("返回模型清单且不泄露内部字段", func(t *testing.T) {
		r := env.do(http.MethodGet, "/api/v1/models", nil, aicNormalUser)
		aicWant(t, r, http.StatusOK, 0)
		var list []map[string]any
		if err := json.Unmarshal(r.Data, &list); err != nil || len(list) != 1 {
			t.Fatalf("应返回 1 个模型：%v %s", err, r.Raw)
		}
		m := list[0]
		if m["key"] != "m1" || m["kind"] != "video" || m["label"] != "模型-m1" || m["hint"] != "小字" || m["pricing"].(map[string]any)["unit"] != float64(5) {
			t.Fatalf("公开字段不符合预期：%v", m)
		}
		if caps, _ := m["capabilities"].(map[string]any); caps == nil || caps["prompt"] == nil || caps["ops"] == nil {
			t.Fatalf("应包含 capabilities：%v", m)
		}
		for _, bad := range []string{"params", "channel", "plugin", "secret", "instanceType", "kling", "sk-super-secret", "base_url", "upstream", "cost", "保密的系统提示"} {
			if strings.Contains(r.Raw, bad) {
				t.Fatalf("/models 响应泄露了 %q：%s", bad, r.Raw)
			}
		}
		if tags, ok := m["tags"].([]any); !ok || len(tags) != 0 {
			t.Fatalf("没有标签时 tags 应为 []：%v", m["tags"])
		}
		if len(m) != 8 {
			t.Fatalf("公开字段应恰好 8 个（key/kind/label/hint/vendor/tags/capabilities/pricing），实际 %d：%v", len(m), m)
		}
	})
	t.Run("kind=text 合法：文本节点能拿到清单", func(t *testing.T) {
		aicWant(t, env.super(http.MethodPost, aicBase+"/models", map[string]any{"body": aicModelJSON("t1", "text", "kling-main")}), http.StatusOK, 0)
		aicWant(t, env.super(http.MethodPut, aicBase+"/models/t1/enabled", map[string]any{"enabled": true}), http.StatusOK, 0)
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
		{http.MethodGet, "/channels/loads", false},
		{http.MethodGet, "/stats", false},
		{http.MethodPost, "/channels", true},
		{http.MethodGet, "/channels/x", false},
		{http.MethodPut, "/channels/x", true},
		{http.MethodPut, "/channels/x/secret", true},
		{http.MethodPut, "/channels/sort", true},
		{http.MethodPut, "/channels/x/secret-key", true},
		{http.MethodPost, "/channels/x/pull-models", false},
		{http.MethodPost, "/channels/x/models/import", false},
		{http.MethodPut, "/channels/x/models/sort", false},
		{http.MethodGet, "/channels/x/delete-check", true},
		{http.MethodDelete, "/channels/x", true},
		{http.MethodPost, "/plugins/x/upgrade-models", true},
		{http.MethodGet, "/models", false},
		{http.MethodPost, "/models", false},
		{http.MethodGet, "/models/m1", false},
		{http.MethodPut, "/models/m1", false},
		{http.MethodPost, "/models/m1/validate", false},
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

	t.Run("旧的 /secrets 接口，以及渠道的 check / check-draft / import 接口已删除（任何角色都是 404）", func(t *testing.T) {
		for _, rt := range []struct{ method, path string }{
			{http.MethodGet, "/secrets"}, {http.MethodPut, "/secrets/x"},
			{http.MethodPost, "/channels/x/check"}, {http.MethodPost, "/channels/check-draft"}, {http.MethodPost, "/channels/x/import"},
		} {
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

	t.Run("版本号没调整：响应里没有 version_adjusted", func(t *testing.T) {
		env := newEnv(t)
		r := env.upload("file", "a.js", []byte("good-v1"), aicSuperUser)
		aicWant(t, r, http.StatusOK, 0)
		if strings.Contains(r.Raw, "version_adjusted") {
			t.Fatalf("没有调整时不该返回 version_adjusted：%s", r.Raw)
		}
	})

	t.Run("代码改了、版本号没改：自动登记为下一个补丁版本，响应带 version_adjusted", func(t *testing.T) {
		env := newEnv(t)
		env.checker.ByCode["good-v1-fixed"] = &provider.PrecheckResult{OK: true, Meta: json.RawMessage(aicMeta("kling", "1.0.0"))}
		aicWant(t, env.upload("file", "a.js", []byte("good-v1"), aicSuperUser), http.StatusOK, 0)
		r := env.upload("file", "a.js", []byte("good-v1-fixed"), aicSuperUser)
		aicWant(t, r, http.StatusOK, 0)
		var d struct {
			Accepted        bool           `json:"accepted"`
			Version         map[string]any `json:"version"`
			VersionAdjusted map[string]any `json:"version_adjusted"`
		}
		if err := json.Unmarshal(r.Data, &d); err != nil || !d.Accepted || d.Version["version"] != "1.0.1" {
			t.Fatalf("应登记为 1.0.1：%v %s", err, r.Raw)
		}
		if d.VersionAdjusted["from"] != "1.0.0" || d.VersionAdjusted["to"] != "1.0.1" {
			t.Fatalf("version_adjusted 不符合预期：%v", d.VersionAdjusted)
		}
	})

	t.Run("代码与已有版本完全相同：200 + accepted=false，问题报在 meta 上", func(t *testing.T) {
		env := newEnv(t)
		aicWant(t, env.upload("file", "a.js", []byte("good-v1"), aicSuperUser), http.StatusOK, 0)
		r := env.upload("file", "a.js", []byte("good-v1"), aicSuperUser)
		aicWant(t, r, http.StatusOK, 0)
		if !strings.Contains(r.Raw, `"accepted":false`) || !strings.Contains(r.Raw, `"path":"meta"`) || !strings.Contains(r.Raw, "完全相同") ||
			!strings.Contains(r.Raw, `"version":null`) {
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
		var page struct {
			List []struct {
				Key      string `json:"key"`
				Versions []struct {
					Version    string         `json:"version"`
					ModelCount int            `json:"model_count"`
					Meta       map[string]any `json:"meta"`
				} `json:"versions"`
			} `json:"list"`
		}
		list := &page.List
		if err := json.Unmarshal(r.Data, &page); err != nil || len(*list) != 2 || len((*list)[0].Versions) != 1 || (*list)[0].Versions[0].Meta["key"] != "kling" {
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
	t.Run("删除：被模型的协议固定 409（50005）", func(t *testing.T) {
		env.seedProtocolModel(t, "m-pin", "c1", "kling", "1.0.0")
		aicWant(t, env.super(http.MethodDelete, aicBase+"/plugins/kling/versions/1.0.0", nil), http.StatusConflict, errcode.ErrPluginInUse.Code)
		delete(env.repo.Models, "m-pin")
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

func TestAdminPluginHandler_UpgradeModels(t *testing.T) {
	// 新版本 1.1.0 新增了必填设置项 must：已有的 1.0.0 模型填不出，会升级失败
	strictMeta := `{"apiVersion":1,"key":"kling","name":"插件-kling","version":"1.1.0","auth":{"type":"bearer"},` +
		`"endpoints":{"video":{"mode":"async"},"text":{"mode":"sync"}},` +
		`"channelSettings":{"tenant":{"type":"string","label":"租户","required":true},"must":{"type":"string","label":"必填新项","required":true}}}`
	// newEnv 准备：渠道 kling-main 就绪（带 Key），kling 1.0.0 与 1.1.0 两个版本。
	newEnv := func(t *testing.T, meta11 string) *aicEnv {
		t.Helper()
		env := aicNewEnv(t)
		env.seedPlugin(t, "kling", "1.0.0", model.PluginSourceUploaded)
		if meta11 == "" {
			meta11 = aicMeta("kling", "1.1.0")
		}
		env.seedPluginMeta(t, "kling", "1.1.0", model.PluginSourceUploaded, meta11)
		aicWant(t, env.super(http.MethodPost, aicBase+"/channels", aicChannelJSON("kling-main")), http.StatusOK, 0)
		aicWant(t, env.super(http.MethodPut, aicBase+"/channels/kling-main/secret", map[string]any{"value": "sk-1"}), http.StatusOK, 0)
		return env
	}
	protocolVersion := func(t *testing.T, env *aicEnv, key string) string {
		t.Helper()
		r := env.admin(http.MethodGet, aicBase+"/models/"+key, nil)
		aicWant(t, r, http.StatusOK, 0)
		var d struct {
			Body struct {
				Channels []struct {
					Protocol struct {
						Version string `json:"version"`
					} `json:"protocol"`
				} `json:"channels"`
			} `json:"body"`
		}
		if err := json.Unmarshal(r.Data, &d); err != nil || len(d.Body.Channels) != 1 {
			t.Fatalf("详情解析失败：%v %s", err, r.Raw)
		}
		return d.Body.Channels[0].Protocol.Version
	}
	url := aicBase + "/plugins/kling/upgrade-models"

	t.Run("成功：不传 body 升到最新版本，旧版本模型被升级，已是新版本的不动，写审计", func(t *testing.T) {
		env := newEnv(t, "")
		aicWant(t, env.super(http.MethodPost, aicBase+"/models", map[string]any{"body": aicModelJSON("old", "video", "kling-main")}), http.StatusOK, 0)
		aicWant(t, env.super(http.MethodPost, aicBase+"/models", map[string]any{"body": aicModelJSONV("fresh", "video", "kling-main", "1.1.0")}), http.StatusOK, 0)
		r := env.super(http.MethodPost, url, nil)
		aicWant(t, r, http.StatusOK, 0)
		var res struct {
			ToVersion string   `json:"to_version"`
			Upgraded  []string `json:"upgraded"`
			Failed    []any    `json:"failed"`
		}
		if err := json.Unmarshal(r.Data, &res); err != nil || res.ToVersion != "1.1.0" || len(res.Upgraded) != 1 || res.Upgraded[0] != "old" || res.Failed == nil || len(res.Failed) != 0 {
			t.Fatalf("响应不符合预期：%v %s", err, r.Raw)
		}
		if got := protocolVersion(t, env, "old"); got != "1.1.0" {
			t.Fatalf("old 的协议应升到 1.1.0，实际 %s", got)
		}
		if got := env.repo.AuditActions(); got[len(got)-1] != model.AuditPluginUpgrade {
			t.Fatalf("审计不符合预期：%v", got)
		}
	})
	t.Run("成功：指定目标版本；没有要升级的模型返回空数组", func(t *testing.T) {
		env := newEnv(t, "")
		r := env.super(http.MethodPost, url, map[string]any{"version": "1.1.0"})
		aicWant(t, r, http.StatusOK, 0)
		if !strings.Contains(r.Raw, `"to_version":"1.1.0"`) || !strings.Contains(r.Raw, `"upgraded":[]`) || !strings.Contains(r.Raw, `"failed":[]`) {
			t.Fatalf("响应不符合预期：%s", r.Raw)
		}
	})
	t.Run("新版本新增必填设置项：该模型进 failed 并说明原因，保持原版本，响应 200", func(t *testing.T) {
		env := newEnv(t, strictMeta)
		aicWant(t, env.super(http.MethodPost, aicBase+"/models", map[string]any{"body": aicModelJSON("m1", "video", "kling-main")}), http.StatusOK, 0)
		r := env.super(http.MethodPost, url, map[string]any{})
		aicWant(t, r, http.StatusOK, 0)
		var res struct {
			Upgraded []string `json:"upgraded"`
			Failed   []struct {
				Key    string `json:"key"`
				Reason string `json:"reason"`
			} `json:"failed"`
		}
		if err := json.Unmarshal(r.Data, &res); err != nil || len(res.Upgraded) != 0 || len(res.Failed) != 1 || res.Failed[0].Key != "m1" || !strings.Contains(res.Failed[0].Reason, "must") {
			t.Fatalf("应失败并说明缺哪个设置项：%v %s", err, r.Raw)
		}
		if got := protocolVersion(t, env, "m1"); got != "1.0.0" {
			t.Fatalf("失败的模型应保持原版本，实际 %s", got)
		}
	})
	t.Run("参数：version 超长、非法 JSON 400", func(t *testing.T) {
		env := newEnv(t, "")
		aicWant(t, env.super(http.MethodPost, url, map[string]any{"version": strings.Repeat("9", 33)}), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
		aicWant(t, env.super(http.MethodPost, url, "{not json"), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
	})
	t.Run("业务错误：插件不存在 404、目标版本不存在 404、插件已停用 409", func(t *testing.T) {
		env := newEnv(t, "")
		aicWant(t, env.super(http.MethodPost, aicBase+"/plugins/ghost/upgrade-models", nil), http.StatusNotFound, errcode.ErrPluginNotFound.Code)
		aicWant(t, env.super(http.MethodPost, url, map[string]any{"version": "9.9.9"}), http.StatusNotFound, errcode.ErrPluginNotFound.Code)
		aicWant(t, env.super(http.MethodPut, aicBase+"/plugins/kling/enabled", map[string]any{"enabled": false}), http.StatusOK, 0)
		aicWant(t, env.super(http.MethodPost, url, nil), http.StatusConflict, errcode.ErrPluginDisabled.Code)
	})
	t.Run("权限：admin 403，普通用户 403，未登录 401", func(t *testing.T) {
		env := newEnv(t, "")
		aicWant(t, env.admin(http.MethodPost, url, nil), http.StatusForbidden, errcode.ErrForbidden.Code)
		aicWant(t, env.do(http.MethodPost, url, nil, aicNormalUser), http.StatusForbidden, errcode.ErrForbidden.Code)
		aicWant(t, env.do(http.MethodPost, url, nil, 0), http.StatusUnauthorized, errcode.ErrUnauthorized.Code)
	})
}

// ---------------------------------------------------------------------------
// 渠道
// ---------------------------------------------------------------------------

func TestAdminChannelHandler_CreateGetUpdate(t *testing.T) {
	env := aicNewEnv(t)
	path := aicBase + "/channels"

	t.Run("新建成功：视图只有连接信息，secret_set / secret_key_set 都是 false，新渠道排在后面", func(t *testing.T) {
		body := aicChannelJSON("kling-main")
		body["headers"] = map[string]any{"X-Biz": "canvas"}
		r := env.super(http.MethodPost, path, body)
		aicWant(t, r, http.StatusOK, 0)
		var v map[string]any
		if err := json.Unmarshal(r.Data, &v); err != nil {
			t.Fatal(err)
		}
		if v["key"] != "kling-main" || v["name"] != "渠道-kling-main" || v["secret_set"] != false || v["secret_key_set"] != false ||
			v["enabled"] != true || v["base_url"] != "https://gw.example.com" || v["sort"] != float64(10) || v["updated_by"] != float64(aicSuperUser) {
			t.Fatalf("视图不符合预期：%v", v)
		}
		if h := v["headers"].(map[string]any); h["X-Biz"] != "canvas" {
			t.Fatalf("headers 应原样返回：%v", v["headers"])
		}
		if rl := v["rate_limit"].(map[string]any); rl["rps"] != float64(5) || rl["max_concurrency"] != float64(20) {
			t.Fatalf("rate_limit 不符合预期：%v", rl)
		}
		// 渠道不再绑协议：协议、设置项、允许读取 Key 这些字段都没有了；存储列名也不暴露
		for _, gone := range []string{"plugin_key", "plugin_version", "allow_credentials", "trusted_internal", `"settings"`, "settings_json", "rate_limit_json", "headers_json"} {
			if strings.Contains(r.Raw, gone) {
				t.Fatalf("渠道视图不应再有 %s：%s", gone, r.Raw)
			}
		}
	})
	t.Run("headers 缺省时是空对象，不是 null", func(t *testing.T) {
		r := env.super(http.MethodPost, path, aicChannelJSON("kling-plain"))
		aicWant(t, r, http.StatusOK, 0)
		if !strings.Contains(r.Raw, `"headers":{}`) || !strings.Contains(r.Raw, `"sort":20`) {
			t.Fatalf("响应不符合预期：%s", r.Raw)
		}
	})
	t.Run("key 重复 409（50012）", func(t *testing.T) {
		aicWant(t, env.super(http.MethodPost, path, aicChannelJSON("kling-main")), http.StatusConflict, errcode.ErrChannelExists.Code)
	})
	t.Run("不传 key：后端生成 16 位十六进制标识并返回", func(t *testing.T) {
		body := aicChannelJSON("ignored")
		delete(body, "key")
		r := env.super(http.MethodPost, path, body)
		aicWant(t, r, http.StatusOK, 0)
		var v struct {
			Key string `json:"key"`
		}
		if err := json.Unmarshal(r.Data, &v); err != nil || !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(v.Key) {
			t.Fatalf("应返回生成的 key：%q %v", v.Key, err)
		}
		// 清掉这个渠道，免得影响后面按渠道列表断言的用例
		aicWant(t, env.super(http.MethodDelete, path+"/"+v.Key, nil), http.StatusOK, 0)
	})
	t.Run("缺少必填字段 400（10001）", func(t *testing.T) {
		for _, missing := range []string{"name", "base_url"} {
			body := aicChannelJSON("kling-x")
			delete(body, missing)
			aicWant(t, env.super(http.MethodPost, path, body), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
		}
		aicWant(t, env.super(http.MethodPost, path, "{not json"), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
	})
	t.Run("业务校验失败 400（50013），原因在 msg", func(t *testing.T) {
		bad := aicChannelJSON("Bad_Key")
		bad["base_url"] = "https://u:p@x.com"
		bad["rate_limit"] = map[string]any{"rps": -1}
		r := env.super(http.MethodPost, path, bad)
		aicWant(t, r, http.StatusBadRequest, errcode.ErrChannelInvalid.Code)
		for _, want := range []string{"key", "用户名", "rate_limit"} {
			if !strings.Contains(r.Msg, want) {
				t.Fatalf("msg 应包含 %q：%s", want, r.Msg)
			}
		}
	})
	t.Run("请求头校验失败 400（50013）：Authorization、含换行的值都不行", func(t *testing.T) {
		for name, headers := range map[string]map[string]any{
			"Authorization": {"Authorization": "Bearer x"},
			"值里夹带换行（防注入）":   {"X-A": "a\r\nX-Evil: 1"},
		} {
			body := aicChannelJSON("kling-hdr")
			body["headers"] = headers
			r := env.super(http.MethodPost, path, body)
			aicWant(t, r, http.StatusBadRequest, errcode.ErrChannelInvalid.Code)
			if r.Msg == "" {
				t.Fatalf("%s：msg 应说明原因", name)
			}
		}
		if _, ok := env.repo.Channels["kling-hdr"]; ok {
			t.Fatal("校验失败不能落库")
		}
	})
	t.Run("enabled=false 可在创建时指定", func(t *testing.T) {
		body := aicChannelJSON("kling-off")
		body["enabled"] = false
		r := env.super(http.MethodPost, path, body)
		aicWant(t, r, http.StatusOK, 0)
		if !strings.Contains(r.Raw, `"enabled":false`) {
			t.Fatalf("响应不符合预期：%s", r.Raw)
		}
	})
	t.Run("列表与详情：admin 可读", func(t *testing.T) {
		r := env.admin(http.MethodGet, path, nil)
		aicWant(t, r, http.StatusOK, 0)
		var page struct {
			List  []map[string]any `json:"list"`
			Total int              `json:"total"`
		}
		if err := json.Unmarshal(r.Data, &page); err != nil || page.Total != 3 || len(page.List) != 3 || page.List[0]["key"] != "kling-main" {
			t.Fatalf("列表不符合预期：%v %s", err, r.Raw)
		}
		aicWant(t, env.admin(http.MethodGet, path+"/kling-main", nil), http.StatusOK, 0)
		aicWant(t, env.admin(http.MethodGet, path+"/ghost", nil), http.StatusNotFound, errcode.ErrChannelNotFound.Code)
	})
	t.Run("更新：字段可选，不传不改", func(t *testing.T) {
		r := env.super(http.MethodPut, path+"/kling-main", map[string]any{"name": "改名", "enabled": false})
		aicWant(t, r, http.StatusOK, 0)
		if !strings.Contains(r.Raw, `"name":"改名"`) || !strings.Contains(r.Raw, `"enabled":false`) ||
			!strings.Contains(r.Raw, `"base_url":"https://gw.example.com"`) || !strings.Contains(r.Raw, `"X-Biz":"canvas"`) {
			t.Fatalf("响应不符合预期：%s", r.Raw)
		}
	})
	t.Run("更新：headers 传了就整体替换（含空对象），base_url 可改", func(t *testing.T) {
		r := env.super(http.MethodPut, path+"/kling-main", map[string]any{"headers": map[string]any{"X-New": "1"}, "base_url": "https://gw2.example.com"})
		aicWant(t, r, http.StatusOK, 0)
		if !strings.Contains(r.Raw, `"headers":{"X-New":"1"}`) || strings.Contains(r.Raw, "X-Biz") || !strings.Contains(r.Raw, `"base_url":"https://gw2.example.com"`) {
			t.Fatalf("应整体替换请求头：%s", r.Raw)
		}
		r = env.super(http.MethodPut, path+"/kling-main", map[string]any{"headers": map[string]any{}})
		aicWant(t, r, http.StatusOK, 0)
		if !strings.Contains(r.Raw, `"headers":{}`) {
			t.Fatalf("空对象应清空请求头：%s", r.Raw)
		}
	})
	t.Run("更新：校验失败 400、不存在 404、name 超长 400", func(t *testing.T) {
		aicWant(t, env.super(http.MethodPut, path+"/kling-main", map[string]any{"base_url": "ftp://x"}), http.StatusBadRequest, errcode.ErrChannelInvalid.Code)
		aicWant(t, env.super(http.MethodPut, path+"/kling-main", map[string]any{"headers": map[string]any{"Cookie": "x"}}), http.StatusBadRequest, errcode.ErrChannelInvalid.Code)
		aicWant(t, env.super(http.MethodPut, path+"/ghost", map[string]any{"name": "x"}), http.StatusNotFound, errcode.ErrChannelNotFound.Code)
		aicWant(t, env.super(http.MethodPut, path+"/kling-main", map[string]any{"name": strings.Repeat("长", 200)}), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
	})
}

func TestAdminChannelHandler_Loads(t *testing.T) {
	env := aicNewEnv(t)
	path := aicBase + "/channels/loads"

	t.Run("没有未完成任务时返回空数组而不是 null", func(t *testing.T) {
		r := env.admin(http.MethodGet, path, nil)
		aicWant(t, r, http.StatusOK, 0)
		if string(r.Data) != "[]" {
			t.Fatalf("应返回 []，实际 %s", r.Data)
		}
	})
	t.Run("返回各渠道的生成中 / 排队数，admin 可读，路径不被 /:key 吞掉", func(t *testing.T) {
		env.repo.Loads = []model.ChannelLoad{{Channel: "yswg", Running: 2, Waiting: 5}}
		r := env.admin(http.MethodGet, path, nil)
		aicWant(t, r, http.StatusOK, 0)
		var list []model.ChannelLoad
		if err := json.Unmarshal(r.Data, &list); err != nil || len(list) != 1 || list[0] != env.repo.Loads[0] {
			t.Fatalf("响应不符合预期：%v %s", err, r.Raw)
		}
	})
}

func TestAdminChannelHandler_RateLimit(t *testing.T) {
	env := aicNewEnv(t)
	path := aicBase + "/channels"

	t.Run("max_running 随渠道保存并原样返回", func(t *testing.T) {
		body := aicChannelJSON("kling-rl")
		body["rate_limit"] = map[string]any{"rps": 5, "max_concurrency": 20, "max_running": 3}
		r := env.super(http.MethodPost, path, body)
		aicWant(t, r, http.StatusOK, 0)
		if !strings.Contains(r.Raw, `"max_running":3`) {
			t.Fatalf("响应应带 max_running：%s", r.Raw)
		}
	})
	t.Run("max_running 为负数返回 400", func(t *testing.T) {
		body := aicChannelJSON("kling-bad")
		body["rate_limit"] = map[string]any{"max_running": -1}
		aicWant(t, env.super(http.MethodPost, path, body), http.StatusBadRequest, errcode.ErrChannelInvalid.Code)
	})
}

func TestAdminChannelHandler_Secret(t *testing.T) {
	env := aicNewEnv(t)
	aicWant(t, env.super(http.MethodPost, aicBase+"/channels", aicChannelJSON("kling-main")), http.StatusOK, 0)
	path := aicBase + "/channels/kling-main/secret"

	t.Run("只写：成功不回显，详情里 secret_set=true，Key 出现在任何响应里都不行", func(t *testing.T) {
		r := env.super(http.MethodPut, path, map[string]any{"value": "sk-plain-text-123"})
		aicWant(t, r, http.StatusOK, 0)
		if strings.Contains(r.Raw, "sk-plain") || strings.Contains(r.Raw, `"data"`) {
			t.Fatalf("响应不得回显凭证，data 为空时应省略：%s", r.Raw)
		}
		for _, p := range []string{"/channels/kling-main", "/channels", "/channels/options"} {
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
		aicWant(t, env2.super(http.MethodPost, aicBase+"/channels", aicChannelJSON("c")), http.StatusOK, 0)
		noKey := service.NewAIConfigService(env2.repo, env2.repo, env2.repo, "")
		eng := router.New(gin.TestMode, aicJWTSecret, router.Handlers{
			AdminChannel: NewAdminChannelHandler(service.NewAIChannelService(env2.repo, noKey, noKey, env2.lister, env2.repo, noKey), noKey),
			AdminRole:    func(context.Context, uint64) (string, error) { return model.RoleSuperAdmin, nil },
			UserState:    aicActiveState,
		})
		env2.engine = eng
		r := env2.super(http.MethodPut, aicBase+"/channels/c/secret", map[string]any{"value": "v"})
		aicWant(t, r, http.StatusInternalServerError, errcode.ErrInternal.Code)
		if !strings.Contains(r.Msg, "APP_AI_SECRET_KEY") {
			t.Fatalf("提示应说明缺少主密钥：%s", r.Raw)
		}
	})
}

func TestAdminChannelHandler_SecretKey(t *testing.T) {
	env := aicNewEnv(t)
	aicWant(t, env.super(http.MethodPost, aicBase+"/channels", aicChannelJSON("kling-main")), http.StatusOK, 0)
	aicWant(t, env.super(http.MethodPut, aicBase+"/channels/kling-main/secret", map[string]any{"value": "sk-api-key"}), http.StatusOK, 0)
	path := aicBase + "/channels/kling-main/secret-key"

	t.Run("设置：只写不回显，详情里 secret_key_set=true，与 API Key 互不影响", func(t *testing.T) {
		r := env.super(http.MethodPut, path, map[string]any{"value": "sec-plain-456"})
		aicWant(t, r, http.StatusOK, 0)
		if strings.Contains(r.Raw, "sec-plain") || strings.Contains(r.Raw, `"data"`) {
			t.Fatalf("响应不得回显凭证：%s", r.Raw)
		}
		g := env.admin(http.MethodGet, aicBase+"/channels/kling-main", nil)
		if strings.Contains(g.Raw, "sec-plain") || !strings.Contains(g.Raw, `"secret_key_set":true`) || !strings.Contains(g.Raw, `"secret_set":true`) {
			t.Fatalf("详情不符合预期：%s", g.Raw)
		}
		if row := env.repo.Secrets["channel:kling-main:secret"]; row == nil || bytes.Contains(row.Ciphertext, []byte("sec-plain")) {
			t.Fatal("Secret Key 应以密文存入 channel:<key>:secret")
		}
		if env.repo.Secrets["channel:kling-main"] == nil {
			t.Fatal("API Key 不应被覆盖")
		}
		b, _ := json.Marshal(env.repo.Audits)
		if strings.Contains(string(b), "sec-plain") || !strings.Contains(string(b), model.AuditChannelSecKey) {
			t.Fatalf("审计不符合预期：%s", b)
		}
	})
	t.Run("空串清除：secret_key_set=false，API Key 保持", func(t *testing.T) {
		aicWant(t, env.super(http.MethodPut, path, map[string]any{"value": ""}), http.StatusOK, 0)
		g := env.admin(http.MethodGet, aicBase+"/channels/kling-main", nil)
		if !strings.Contains(g.Raw, `"secret_key_set":false`) || !strings.Contains(g.Raw, `"secret_set":true`) {
			t.Fatalf("详情不符合预期：%s", g.Raw)
		}
		if _, ok := env.repo.Secrets["channel:kling-main:secret"]; ok {
			t.Fatal("Secret Key 应已删除")
		}
		// 再清一次也成功（幂等）
		aicWant(t, env.super(http.MethodPut, path, map[string]any{"value": ""}), http.StatusOK, 0)
	})
	t.Run("value 超长 400；渠道不存在 404", func(t *testing.T) {
		aicWant(t, env.super(http.MethodPut, path, map[string]any{"value": strings.Repeat("k", 4097)}), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
		aicWant(t, env.super(http.MethodPut, path, "{not json"), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
		aicWant(t, env.super(http.MethodPut, aicBase+"/channels/ghost/secret-key", map[string]any{"value": "x"}), http.StatusNotFound, errcode.ErrChannelNotFound.Code)
	})
}

func TestAdminChannelHandler_Sort(t *testing.T) {
	env := aicNewEnv(t)
	for _, k := range []string{"a", "b", "c"} {
		aicWant(t, env.super(http.MethodPost, aicBase+"/channels", aicChannelJSON(k)), http.StatusOK, 0)
	}
	order := func() string {
		r := env.admin(http.MethodGet, aicBase+"/channels/options", nil)
		aicWant(t, r, http.StatusOK, 0)
		var list []map[string]any
		if err := json.Unmarshal(r.Data, &list); err != nil {
			t.Fatal(err)
		}
		keys := make([]string, 0, len(list))
		for _, it := range list {
			keys = append(keys, it["key"].(string))
		}
		return strings.Join(keys, ",")
	}
	path := aicBase + "/channels/sort"

	t.Run("成功：按新顺序重排，路径不被 /:key 吞掉，写审计", func(t *testing.T) {
		r := env.super(http.MethodPut, path, map[string]any{"keys": []string{"c", "a", "b"}})
		aicWant(t, r, http.StatusOK, 0)
		if got := order(); got != "c,a,b" {
			t.Fatalf("顺序不对：%s", got)
		}
		if got := env.repo.AuditActions(); got[len(got)-1] != model.AuditChannelSort {
			t.Fatalf("审计不符合预期：%v", got)
		}
	})
	t.Run("keys 不是全集（漏、重复、不存在）400（10001），顺序不变", func(t *testing.T) {
		for name, keys := range map[string][]string{
			"漏掉":  {"a", "b"},
			"重复":  {"a", "a", "b"},
			"不存在": {"a", "b", "ghost"},
			"空列表": {},
		} {
			aicWant(t, env.super(http.MethodPut, path, map[string]any{"keys": keys}), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
			if got := order(); got != "c,a,b" {
				t.Fatalf("%s：顺序不应改变：%s", name, got)
			}
		}
	})
	t.Run("缺少 keys、非法 JSON 400", func(t *testing.T) {
		aicWant(t, env.super(http.MethodPut, path, map[string]any{}), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
		aicWant(t, env.super(http.MethodPut, path, "{not json"), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
	})
}

// aicPullEnv 准备一个已设置 API Key 的渠道 pull-ch，上游有三个模型。
func aicPullEnv(t *testing.T, withKey bool) *aicEnv {
	t.Helper()
	env := aicNewEnv(t)
	body := aicChannelJSON("pull-ch")
	body["headers"] = map[string]any{"X-Biz": "canvas"}
	aicWant(t, env.super(http.MethodPost, aicBase+"/channels", body), http.StatusOK, 0)
	if withKey {
		aicWant(t, env.super(http.MethodPut, aicBase+"/channels/pull-ch/secret", map[string]any{"value": "sk-live-123"}), http.StatusOK, 0)
	}
	env.lister.IDs = []string{"gpt-image-2", "sd2.0-c1", "nano-banana2"}
	return env
}

func TestAdminChannelHandler_PullModels(t *testing.T) {
	url := aicBase + "/channels/pull-ch/pull-models"

	t.Run("成功：admin 可调，标记渠道下已有的模型，用渠道的地址 / 请求头 / Key 请求上游，不落库", func(t *testing.T) {
		env := aicPullEnv(t, true)
		aicWant(t, env.admin(http.MethodPost, aicBase+"/channels/pull-ch/models/import", map[string]any{"ids": []string{"sd2.0-c1"}}), http.StatusOK, 0)
		before := len(env.repo.Models)

		r := env.admin(http.MethodPost, url, nil)
		aicWant(t, r, http.StatusOK, 0)
		var res struct {
			Models []struct {
				ID     string `json:"id"`
				Exists bool   `json:"exists"`
			} `json:"models"`
			Total int `json:"total"`
		}
		if err := json.Unmarshal(r.Data, &res); err != nil || res.Total != 3 || len(res.Models) != 3 {
			t.Fatalf("响应不符合预期：%v %s", err, r.Raw)
		}
		exists := map[string]bool{}
		for _, m := range res.Models {
			exists[m.ID] = m.Exists
		}
		if !exists["sd2.0-c1"] || exists["gpt-image-2"] || exists["nano-banana2"] {
			t.Fatalf("exists 标记不对：%v", exists)
		}
		if env.lister.APIKey != "sk-live-123" || env.lister.Channel.BaseURL != "https://gw.example.com" || env.lister.Channel.Headers["X-Biz"] != "canvas" {
			t.Fatalf("请求上游用的渠道信息不对：%+v key=%q", env.lister.Channel, env.lister.APIKey)
		}
		if strings.Contains(r.Raw, "sk-live-123") {
			t.Fatalf("响应不得含 Key：%s", r.Raw)
		}
		if len(env.repo.Models) != before {
			t.Fatal("拉取不落库")
		}
	})
	t.Run("super_admin 也可调", func(t *testing.T) {
		env := aicPullEnv(t, true)
		aicWant(t, env.super(http.MethodPost, url, nil), http.StatusOK, 0)
	})
	t.Run("API Key 没设置 409（50015），不向上游发请求", func(t *testing.T) {
		env := aicPullEnv(t, false)
		aicWant(t, env.admin(http.MethodPost, url, nil), http.StatusConflict, errcode.ErrChannelSecretUnset.Code)
		if env.lister.Calls != 0 {
			t.Fatal("没有 Key 时不该向上游发请求")
		}
	})
	t.Run("渠道不存在 404；路径 key 超长 400", func(t *testing.T) {
		env := aicPullEnv(t, true)
		aicWant(t, env.admin(http.MethodPost, aicBase+"/channels/ghost/pull-models", nil), http.StatusNotFound, errcode.ErrChannelNotFound.Code)
		aicWant(t, env.admin(http.MethodPost, aicBase+"/channels/"+aicLongKey+"/pull-models", nil), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
	})
	t.Run("上游失败 502（50023），消息脱敏，响应里没有 Key 明文", func(t *testing.T) {
		for name, perr := range map[string]error{
			"401":  &provider.PullError{Reason: provider.PullUnauthorized, Status: 401, Message: "上游拒绝了请求（HTTP 401），key=sk-live-123"},
			"不可达":  &provider.PullError{Reason: provider.PullUnreachable, Message: "连接 gw.example.com 超时"},
			"格式":   &provider.PullError{Reason: provider.PullFormat, Message: "不是 OpenAI 形状"},
			"其他错误": errors.New("意外：sk-live-123"),
		} {
			env := aicPullEnv(t, true)
			env.lister.Err = perr
			r := env.admin(http.MethodPost, url, nil)
			aicWant(t, r, http.StatusBadGateway, errcode.ErrChannelPullFailed.Code)
			if strings.Contains(r.Raw, "sk-live-123") || r.Msg == "" {
				t.Fatalf("%s：消息不能为空，也不能含 Key 明文：%s", name, r.Raw)
			}
		}
	})
	t.Run("普通用户 403，未登录 401", func(t *testing.T) {
		env := aicPullEnv(t, true)
		aicWant(t, env.do(http.MethodPost, url, nil, aicNormalUser), http.StatusForbidden, errcode.ErrForbidden.Code)
		aicWant(t, env.do(http.MethodPost, url, nil, 0), http.StatusUnauthorized, errcode.ErrUnauthorized.Code)
	})
}

func TestAdminChannelHandler_ImportModels(t *testing.T) {
	env := aicPullEnv(t, true)
	url := aicBase + "/channels/pull-ch/models/import"

	t.Run("成功：admin 可调，创建“待配置”模型（未启用、没选协议），用户看不到", func(t *testing.T) {
		r := env.admin(http.MethodPost, url, map[string]any{"ids": []string{"gpt-image-2", "sd2.0-c1"}})
		aicWant(t, r, http.StatusOK, 0)
		var res struct {
			Created []struct {
				Key           string `json:"key"`
				UpstreamModel string `json:"upstream_model"`
			} `json:"created"`
			Skipped []string `json:"skipped"`
		}
		if err := json.Unmarshal(r.Data, &res); err != nil || len(res.Created) != 2 || res.Skipped == nil || len(res.Skipped) != 0 ||
			res.Created[0].Key != "gpt-image-2" || res.Created[0].UpstreamModel != "gpt-image-2" || res.Created[1].Key != "sd2.0-c1" {
			t.Fatalf("响应不符合预期：%v %s", err, r.Raw)
		}
		d := env.admin(http.MethodGet, aicBase+"/models/gpt-image-2", nil)
		aicWant(t, d, http.StatusOK, 0)
		if !strings.Contains(d.Raw, `"enabled":false`) || !strings.Contains(d.Raw, `"upstream_model":"gpt-image-2"`) || strings.Contains(d.Raw, `"protocol"`) {
			t.Fatalf("应是未启用、没有协议的待配置模型：%s", d.Raw)
		}
		list, err := aicPageList(env.admin(http.MethodGet, aicBase+"/models", nil))
		if err != nil || len(list) != 2 {
			t.Fatalf("后台列表应有 2 个模型：%v", err)
		}
		health, _ := list[0]["health"].(map[string]any)
		if list[0]["channel"] != "pull-ch" || list[0]["upstream"] != "gpt-image-2" || list[0]["plugin"] != "" || health["status"] != "pending" {
			t.Fatalf("待配置模型的列表行不符合预期：%v", list[0])
		}
		if u := env.do(http.MethodGet, "/api/v1/models", nil, aicNormalUser); strings.Contains(u.Raw, "gpt-image-2") {
			t.Fatalf("待配置的模型不应对用户可见：%s", u.Raw)
		}
	})
	t.Run("待配置的模型不能直接启用：400（40010）", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPut, aicBase+"/models/gpt-image-2/enabled", map[string]any{"enabled": true}), http.StatusBadRequest, errcode.ErrConfigInvalid.Code)
	})
	t.Run("渠道下已有的同名上游模型跳过，不重复创建", func(t *testing.T) {
		r := env.admin(http.MethodPost, url, map[string]any{"ids": []string{"sd2.0-c1", "nano-banana2"}})
		aicWant(t, r, http.StatusOK, 0)
		var res struct {
			Created []map[string]any `json:"created"`
			Skipped []string         `json:"skipped"`
		}
		if err := json.Unmarshal(r.Data, &res); err != nil || len(res.Created) != 1 || res.Created[0]["upstream_model"] != "nano-banana2" ||
			len(res.Skipped) != 1 || res.Skipped[0] != "sd2.0-c1" {
			t.Fatalf("响应不符合预期：%v %s", err, r.Raw)
		}
		if len(env.repo.Models) != 3 {
			t.Fatalf("应只有 3 个模型：%d", len(env.repo.Models))
		}
	})
	t.Run("参数：空 ids、全是空白、缺 ids、非法 JSON 都是 400（10001）", func(t *testing.T) {
		for name, body := range map[string]any{
			"空数组":      map[string]any{"ids": []string{}},
			"全是空白":     map[string]any{"ids": []string{"  ", ""}},
			"缺 ids":    map[string]any{},
			"单个 id 超长": map[string]any{"ids": []string{strings.Repeat("m", 257)}},
			"非法 JSON":  "{not json",
		} {
			r := env.admin(http.MethodPost, url, body)
			if r.Status != http.StatusBadRequest || r.Code != errcode.ErrInvalidParams.Code {
				t.Fatalf("%s：期望 400 / 10001，实际 HTTP %d / code %d：%s", name, r.Status, r.Code, r.Raw)
			}
		}
		many := make([]string, 501)
		for i := range many {
			many[i] = fmt.Sprintf("bulk-%d", i)
		}
		r := env.admin(http.MethodPost, url, map[string]any{"ids": many})
		aicWant(t, r, http.StatusBadRequest, errcode.ErrInvalidParams.Code)
		if !strings.Contains(r.Msg, "500") {
			t.Fatalf("应提示单次上限：%s", r.Msg)
		}
		if len(env.repo.Models) != 3 {
			t.Fatalf("参数不合法时不能创建任何模型：%d", len(env.repo.Models))
		}
	})
	t.Run("渠道不存在 404", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPost, aicBase+"/channels/ghost/models/import", map[string]any{"ids": []string{"a"}}), http.StatusNotFound, errcode.ErrChannelNotFound.Code)
	})
}

func TestAdminChannelHandler_ModelSort(t *testing.T) {
	env := aicPullEnv(t, true)
	aicWant(t, env.super(http.MethodPost, aicBase+"/channels", aicChannelJSON("other")), http.StatusOK, 0)
	aicWant(t, env.admin(http.MethodPost, aicBase+"/channels/pull-ch/models/import", map[string]any{"ids": []string{"a", "b", "c"}}), http.StatusOK, 0)
	aicWant(t, env.admin(http.MethodPost, aicBase+"/channels/other/models/import", map[string]any{"ids": []string{"x"}}), http.StatusOK, 0)
	path := aicBase + "/channels/pull-ch/models/sort"
	sorts := func() string {
		return fmt.Sprintf("%d,%d,%d", env.repo.Models["a"].Sort, env.repo.Models["b"].Sort, env.repo.Models["c"].Sort)
	}

	t.Run("成功：admin 可调，按新顺序重写该渠道内模型的 sort，不影响其他渠道", func(t *testing.T) {
		xBefore := env.repo.Models["x"].Sort
		aicWant(t, env.admin(http.MethodPut, path, map[string]any{"keys": []string{"c", "a", "b"}}), http.StatusOK, 0)
		if got := sorts(); got != "20,30,10" {
			t.Fatalf("sort 不对（a,b,c）：%s", got)
		}
		if env.repo.Models["x"].Sort != xBefore {
			t.Fatal("其他渠道的模型不应受影响")
		}
	})
	t.Run("keys 不是该渠道模型的全集（漏、重复、别的渠道的、不存在的）400（10001），顺序不变", func(t *testing.T) {
		for name, keys := range map[string][]string{
			"漏掉":    {"a", "b"},
			"重复":    {"a", "a", "b", "c"},
			"别的渠道的": {"a", "b", "c", "x"},
			"不存在的":  {"a", "b", "ghost"},
		} {
			aicWant(t, env.admin(http.MethodPut, path, map[string]any{"keys": keys}), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
			if got := sorts(); got != "20,30,10" {
				t.Fatalf("%s：顺序不应改变：%s", name, got)
			}
		}
	})
	t.Run("缺少 keys、非法 JSON 400；渠道里没有这些模型也是 400", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPut, path, map[string]any{}), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
		aicWant(t, env.admin(http.MethodPut, path, "{not json"), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
		aicWant(t, env.admin(http.MethodPut, aicBase+"/channels/ghost/models/sort", map[string]any{"keys": []string{"a"}}), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
	})
}

// ---------------------------------------------------------------------------
// 模型：保存 / 校验 / 启用 / 列表 / 试跑 / 追踪 / schema
// ---------------------------------------------------------------------------

func TestAdminAIHandler_ConfigLifecycle(t *testing.T) {
	env := aicNewEnv(t)
	env.seedPlugin(t, "kling", "1.0.0", model.PluginSourceUploaded)
	aicWant(t, env.super(http.MethodPost, aicBase+"/channels", aicChannelJSON("kling-main")), http.StatusOK, 0)

	t.Run("新建模型：成功并返回 issues 列表，没有版本信息", func(t *testing.T) {
		r := env.admin(http.MethodPost, aicBase+"/models", map[string]any{"body": aicModelJSON("m1", "video", "kling-main"), "note": "初版"})
		aicWant(t, r, http.StatusOK, 0)
		var d struct {
			Issues []any `json:"issues"`
		}
		if err := json.Unmarshal(r.Data, &d); err != nil || d.Issues == nil || len(d.Issues) != 0 {
			t.Fatalf("响应不符合预期：%v %s", err, r.Raw)
		}
		if strings.Contains(r.Raw, "revision") {
			t.Fatalf("响应不应带版本信息：%s", r.Raw)
		}
	})
	t.Run("新建的模型默认没启用，用户看不到", func(t *testing.T) {
		r := env.admin(http.MethodGet, aicBase+"/models/m1", nil)
		aicWant(t, r, http.StatusOK, 0)
		if !strings.Contains(r.Raw, `"enabled":false`) {
			t.Fatalf("新建的模型应未启用：%s", r.Raw)
		}
		r = env.do(http.MethodGet, "/api/v1/models", nil, aicNormalUser)
		if strings.Contains(r.Raw, `"m1"`) {
			t.Fatalf("未启用的模型不应出现在用户清单里：%s", r.Raw)
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
	t.Run("PUT：路径 key 与正文不一致 400；不存在 404；成功覆盖原配置", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPut, aicBase+"/models/other", map[string]any{"body": aicModelJSON("m1", "video", "kling-main")}), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
		aicWant(t, env.admin(http.MethodPut, aicBase+"/models/none", map[string]any{"body": aicModelJSON("none", "video", "kling-main")}), http.StatusNotFound, errcode.ErrConfigNotFound.Code)
		body := aicModelJSON("m1", "video", "kling-main")
		body["label"] = "改名后"
		aicWant(t, env.admin(http.MethodPut, aicBase+"/models/m1", map[string]any{"body": body}), http.StatusOK, 0)
		r := env.admin(http.MethodGet, aicBase+"/models/m1", nil)
		if !strings.Contains(r.Raw, "改名后") {
			t.Fatalf("详情应是最新保存的内容：%s", r.Raw)
		}
	})
	t.Run("校验：不传 body 校验已保存的配置；传 body 校验传入内容（渠道不存在也会报）", func(t *testing.T) {
		r := env.admin(http.MethodPost, aicBase+"/models/m1/validate", nil)
		aicWant(t, r, http.StatusOK, 0)
		if !strings.Contains(r.Raw, `"valid":true`) {
			t.Fatalf("已保存的配置应通过校验：%s", r.Raw)
		}
		r = env.admin(http.MethodPost, aicBase+"/models/m1/validate", map[string]any{"body": aicModelJSON("m1", "video", "ghost")})
		aicWant(t, r, http.StatusOK, 0)
		if !strings.Contains(r.Raw, `"valid":false`) || !strings.Contains(r.Raw, `"path":"channels[0].channel"`) {
			t.Fatalf("应返回带路径的问题：%s", r.Raw)
		}
		aicWant(t, env.admin(http.MethodPost, aicBase+"/models/none/validate", nil), http.StatusNotFound, errcode.ErrConfigNotFound.Code)
	})
	t.Run("校验：协议问题（插件不存在、版本不存在、必填设置项缺失）报在 channels[0].protocol 上，渠道问题仍在 channels[0].channel", func(t *testing.T) {
		ghostPlugin := aicModelJSON("m1", "video", "kling-main")
		ghostPlugin["channels"].([]any)[0].(map[string]any)["protocol"] = map[string]any{"plugin": "ghost", "version": "1.0.0"}
		noTenant := aicModelJSON("m1", "video", "kling-main")
		noTenant["channels"].([]any)[0].(map[string]any)["protocol"] = map[string]any{"plugin": "kling", "version": "1.0.0", "settings": map[string]any{}}
		for name, body := range map[string]map[string]any{
			"插件不存在":   ghostPlugin,
			"版本不存在":   aicModelJSONV("m1", "video", "kling-main", "9.9.9"),
			"必填设置项缺失": noTenant,
		} {
			r := env.admin(http.MethodPost, aicBase+"/models/m1/validate", map[string]any{"body": body})
			aicWant(t, r, http.StatusOK, 0)
			if !strings.Contains(r.Raw, `"valid":false`) || !strings.Contains(r.Raw, `"path":"channels[0].protocol`) {
				t.Fatalf("%s：应返回 channels[0].protocol 上的问题：%s", name, r.Raw)
			}
			if strings.Contains(r.Raw, `"path":"channels[0].channel"`) {
				t.Fatalf("%s：渠道是好的，不该报渠道问题：%s", name, r.Raw)
			}
		}
	})
	t.Run("非 Agent 模型必须选协议：没有 protocol 的正文校验不过", func(t *testing.T) {
		body := aicModelJSON("m1", "video", "kling-main")
		delete(body["channels"].([]any)[0].(map[string]any), "protocol")
		r := env.admin(http.MethodPost, aicBase+"/models/m1/validate", map[string]any{"body": body})
		aicWant(t, r, http.StatusOK, 0)
		if !strings.Contains(r.Raw, `"valid":false`) || !strings.Contains(r.Raw, "protocol") {
			t.Fatalf("应报缺少协议：%s", r.Raw)
		}
	})
	t.Run("渠道 Key 未设置时启用返回 409（50015）", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPut, aicBase+"/models/m1/enabled", map[string]any{"enabled": true}), http.StatusConflict, errcode.ErrChannelSecretUnset.Code)
	})
	t.Run("渠道停用时启用返回 409（50014）", func(t *testing.T) {
		aicWant(t, env.super(http.MethodPut, aicBase+"/channels/kling-main", map[string]any{"enabled": false}), http.StatusOK, 0)
		aicWant(t, env.admin(http.MethodPut, aicBase+"/models/m1/enabled", map[string]any{"enabled": true}), http.StatusConflict, errcode.ErrChannelDisabled.Code)
		aicWant(t, env.super(http.MethodPut, aicBase+"/channels/kling-main", map[string]any{"enabled": true}), http.StatusOK, 0)
	})
	t.Run("super_admin 设置 Key 后，admin 可以启用，用户随即可见", func(t *testing.T) {
		aicWant(t, env.super(http.MethodPut, aicBase+"/channels/kling-main/secret", map[string]any{"value": "sk-1"}), http.StatusOK, 0)
		aicWant(t, env.admin(http.MethodPut, aicBase+"/models/m1/enabled", map[string]any{"enabled": true}), http.StatusOK, 0)
		r := env.do(http.MethodGet, "/api/v1/models", nil, aicNormalUser)
		if !strings.Contains(r.Raw, `"m1"`) {
			t.Fatalf("启用后用户清单里应有 m1：%s", r.Raw)
		}
	})
	t.Run("已启用的模型保存即生效；校验不过的保存被拒绝", func(t *testing.T) {
		body := aicModelJSON("m1", "video", "kling-main")
		body["label"] = "线上新名"
		aicWant(t, env.admin(http.MethodPut, aicBase+"/models/m1", map[string]any{"body": body}), http.StatusOK, 0)
		r := env.do(http.MethodGet, "/api/v1/models", nil, aicNormalUser)
		if !strings.Contains(r.Raw, "线上新名") {
			t.Fatalf("保存后用户立刻看到新配置：%s", r.Raw)
		}
		aicWant(t, env.admin(http.MethodPut, aicBase+"/models/m1", map[string]any{"body": aicModelJSON("m1", "video", "ghost")}), http.StatusNotFound, errcode.ErrChannelNotFound.Code)
	})
	t.Run("详情包含配置正文与启用状态，不含版本信息；不存在 404", func(t *testing.T) {
		r := env.admin(http.MethodGet, aicBase+"/models/m1", nil)
		aicWant(t, r, http.StatusOK, 0)
		var d service.ConfigDetail
		if err := json.Unmarshal(r.Data, &d); err != nil || !d.Enabled || len(d.Body) == 0 {
			t.Fatalf("详情不符合预期：%v %s", err, r.Raw)
		}
		for _, bad := range []string{"revision", "draft", "published"} {
			if strings.Contains(r.Raw, bad) {
				t.Fatalf("详情不应带 %q：%s", bad, r.Raw)
			}
		}
		aicWant(t, env.admin(http.MethodGet, aicBase+"/models/none", nil), http.StatusNotFound, errcode.ErrConfigNotFound.Code)
	})
	t.Run("发布 / 回滚 / 历史版本接口已移除", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPost, aicBase+"/models/m1/publish", nil), http.StatusNotFound, errcode.ErrNotFound.Code)
		aicWant(t, env.admin(http.MethodPost, aicBase+"/models/m1/rollback", map[string]any{"revision_id": 1}), http.StatusNotFound, errcode.ErrNotFound.Code)
		aicWant(t, env.admin(http.MethodGet, aicBase+"/models/m1/revisions", nil), http.StatusNotFound, errcode.ErrNotFound.Code)
	})
	t.Run("列表带 label 与 channel，且不含正文", func(t *testing.T) {
		r := env.admin(http.MethodGet, aicBase+"/models", nil)
		aicWant(t, r, http.StatusOK, 0)
		list, err := aicPageList(r)
		if err != nil || len(list) != 1 {
			t.Fatalf("列表不符合预期：%v %s", err, r.Raw)
		}
		if list[0]["key"] != "m1" || list[0]["label"] != "线上新名" || list[0]["channel"] != "kling-main" || list[0]["kind"] != "video" {
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
		list, err := aicPageList(r)
		if err != nil {
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
	env.enableAll(t)
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
	env.enableAll(t)
	// 再保存一次用于试跑
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

func TestAdminStatsHandler_Stats(t *testing.T) {
	env := aicNewEnv(t)
	path := aicBase + "/stats"

	t.Run("不传 days 按 7 天，返回连续 7 天的柱形图数据与占比", func(t *testing.T) {
		env.stats.ModelKinds = []model.AIStatsModelKind{{Model: "kling-v2", Kind: "video", Count: 3}}
		r := env.admin(http.MethodGet, path, nil)
		aicWant(t, r, http.StatusOK, 0)
		var v model.AIStatsView
		if err := json.Unmarshal(r.Data, &v); err != nil {
			t.Fatalf("响应解析失败：%v %s", err, r.Raw)
		}
		if v.Days != 7 || len(v.Daily) != 7 {
			t.Fatalf("默认应是 7 天：days=%d len=%d", v.Days, len(v.Daily))
		}
		if len(v.ByModel) != 1 || v.ByModel[0].Model != "kling-v2" || v.ByModel[0].Count != 3 ||
			len(v.ByKind) != 1 || v.ByKind[0].Kind != "video" {
			t.Fatalf("占比数据不对：%s", r.Raw)
		}
	})
	t.Run("days=30 返回 30 天", func(t *testing.T) {
		r := env.admin(http.MethodGet, path+"?days=30", nil)
		aicWant(t, r, http.StatusOK, 0)
		var v model.AIStatsView
		if err := json.Unmarshal(r.Data, &v); err != nil || len(v.Daily) != 30 {
			t.Fatalf("应是 30 天：%v %s", err, r.Raw)
		}
	})
	t.Run("days 不是 7 或 30 返回 400", func(t *testing.T) {
		for _, q := range []string{"?days=14", "?days=0x", "?days=-7"} {
			aicWant(t, env.admin(http.MethodGet, path+q, nil), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
		}
	})
	t.Run("统计查询失败时返回 500 且不泄露内部错误", func(t *testing.T) {
		env.stats.Err = errors.New("pq: relation secret_table does not exist")
		defer func() { env.stats.Err = nil }()
		r := env.admin(http.MethodGet, path, nil)
		if r.Status != http.StatusInternalServerError || strings.Contains(r.Raw, "secret_table") {
			t.Fatalf("应是 500 且不带内部信息：%d %s", r.Status, r.Raw)
		}
	})
}
