//go:build legacy

package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	. "video-canvas/internal/handler"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/service"
	"video-canvas/internal/service/aiconfigfake"
)

const (
	aicAdminUser  = 1 // 管理员
	aicNormalUser = 2 // 普通用户
)

// aicEnv 是管理接口 handler 测试的完整环境：真实 service + 内存 fake，路由与线上一致（JWT 换成测试中间件）。
type aicEnv struct {
	engine *gin.Engine
	svc    *service.AIConfigService
	repo   *aiconfigfake.MemRepo
	dry    *aiconfigfake.DryRunner
	tasks  *aiconfigfake.TestTasks
}

func aicNewEnv(t *testing.T) *aicEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	repo := aiconfigfake.NewMemRepo()
	svc := service.NewAIConfigService(repo, &aiconfigfake.Validator{}, "handler-test-master-key")
	env := &aicEnv{
		svc:   svc,
		repo:  repo,
		dry:   &aiconfigfake.DryRunner{Result: map[string]any{"submit": map[string]any{"url": "https://example.com/run"}}},
		tasks: &aiconfigfake.TestTasks{Views: map[uint64]aiconfigfake.TestView{}},
	}
	svc.SetDryRunner(env.dry)
	svc.SetTestTaskCreator(env.tasks)

	r := gin.New()
	// 测试用的“登录”：X-Test-User 头里的用户 ID 注入 context，不依赖真实 JWT
	auth := r.Group("/api/v1", func(c *gin.Context) {
		if id, err := strconv.Atoi(c.GetHeader("X-Test-User")); err == nil && id > 0 {
			c.Set(middleware.CtxUserIDKey, uint(id))
		}
		c.Next()
	})
	auth.GET("/models", NewAIModelHandler(svc).List)
	lookup := func(_ context.Context, id uint64) (string, error) {
		if id == aicAdminUser {
			return model.RoleAdmin, nil
		}
		return model.RoleUser, nil
	}
	NewAdminAIHandler(svc).Register(auth.Group("/admin/ai", middleware.RequireAdmin(lookup)))
	env.engine = r
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
	if user > 0 {
		req.Header.Set("X-Test-User", strconv.Itoa(user))
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

// admin 以管理员身份请求。
func (e *aicEnv) admin(method, path string, body any) aicResp {
	return e.do(method, path, body, aicAdminUser)
}

func aicWant(t *testing.T, r aicResp, status, code int) {
	t.Helper()
	if r.Status != status || r.Code != code {
		t.Fatalf("期望 HTTP %d / code %d，实际 HTTP %d / code %d：%s", status, code, r.Status, r.Code, r.Raw)
	}
}

func aicProviderJSON(key, secret string) map[string]any {
	return map[string]any{
		"dsl": 1, "key": key, "name": "平台-" + key, "base_url": "https://example.com", "allowed_hosts": []string{"example.com"},
		"auth": map[string]any{"type": "bearer", "secret": secret},
	}
}

func aicModelJSON(key, provider string) map[string]any {
	return map[string]any{
		"key": key, "kind": "video", "provider": provider, "label": "模型-" + key, "hint": "小字", "credits": 5, "enabled": true, "sort": 10,
		"params":       map[string]any{"webappId": "123", "instanceType": "default"},
		"input_schema": map[string]any{"prompt": map[string]any{"type": "text", "label": "提示词", "required": true}},
		"mapping":      map[string]any{"nodeInfoList": []any{map[string]any{"nodeId": "6", "fieldName": "text", "fieldValue": "${ input.prompt }"}}},
	}
}

// aicPublishAll 通过接口完成：设置凭证 → 发布平台 p1 → 发布模型 m1 → 上架（新建时正文 enabled=true 已上架）。
func (e *aicEnv) publishAll(t *testing.T) {
	t.Helper()
	steps := []struct {
		method, path string
		body         any
	}{
		{http.MethodPut, "/api/v1/admin/ai/secrets/p1_key", map[string]any{"value": "sk-super-secret"}},
		{http.MethodPost, "/api/v1/admin/ai/providers", map[string]any{"body": aicProviderJSON("p1", "p1_key")}},
		{http.MethodPost, "/api/v1/admin/ai/providers/p1/publish", nil},
		{http.MethodPost, "/api/v1/admin/ai/models", map[string]any{"body": aicModelJSON("m1", "p1")}},
		{http.MethodPost, "/api/v1/admin/ai/models/m1/publish", nil},
	}
	for _, s := range steps {
		if r := e.admin(s.method, s.path, s.body); r.Status != http.StatusOK || r.Code != 0 {
			t.Fatalf("准备步骤 %s %s 失败：%s", s.method, s.path, r.Raw)
		}
	}
}

// ---------------------------------------------------------------------------
// GET /models
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
		if _, ok := m["input_schema"].(map[string]any)["prompt"]; !ok {
			t.Fatalf("应包含 input_schema：%v", m)
		}
		// 响应文本里不能出现任何内部字段名或敏感内容
		for _, bad := range []string{"params", "mapping", "provider", "secret", "webappId", "nodeInfoList", "sk-super-secret", "p1_key", "allowed_hosts", "base_url"} {
			if strings.Contains(r.Raw, bad) {
				t.Fatalf("/models 响应泄露了 %q：%s", bad, r.Raw)
			}
		}
		if len(m) != 6 {
			t.Fatalf("公开字段应恰好 6 个，实际 %d：%v", len(m), m)
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
		aicWant(t, env.do(http.MethodGet, "/api/v1/models?kind=text", nil, aicNormalUser), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
	})
	t.Run("下架后不再出现", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPut, "/api/v1/admin/ai/models/m1/enabled", map[string]any{"enabled": false}), http.StatusOK, 0)
		r := env.do(http.MethodGet, "/api/v1/models", nil, aicNormalUser)
		if strings.TrimSpace(string(r.Data)) != "[]" {
			t.Fatalf("下架后应为空：%s", r.Raw)
		}
	})
}

// ---------------------------------------------------------------------------
// 权限
// ---------------------------------------------------------------------------

func TestAdminAIHandler_RequireAdmin(t *testing.T) {
	env := aicNewEnv(t)
	routes := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/admin/ai/providers"},
		{http.MethodPost, "/api/v1/admin/ai/providers"},
		{http.MethodGet, "/api/v1/admin/ai/providers/p1"},
		{http.MethodPut, "/api/v1/admin/ai/providers/p1"},
		{http.MethodGet, "/api/v1/admin/ai/models"},
		{http.MethodPost, "/api/v1/admin/ai/models/m1/publish"},
		{http.MethodPost, "/api/v1/admin/ai/models/m1/dry-run"},
		{http.MethodPost, "/api/v1/admin/ai/models/m1/test-run"},
		{http.MethodGet, "/api/v1/admin/ai/test-runs/1"},
		{http.MethodPost, "/api/v1/admin/ai/import/runninghub"},
		{http.MethodGet, "/api/v1/admin/ai/secrets"},
		{http.MethodPut, "/api/v1/admin/ai/secrets/x"},
		{http.MethodGet, "/api/v1/admin/ai/schema/model"},
	}
	for _, rt := range routes {
		t.Run("普通用户 403："+rt.method+" "+rt.path, func(t *testing.T) {
			aicWant(t, env.do(rt.method, rt.path, "{}", aicNormalUser), http.StatusForbidden, errcode.ErrForbidden.Code)
		})
		t.Run("未登录 401："+rt.method+" "+rt.path, func(t *testing.T) {
			aicWant(t, env.do(rt.method, rt.path, "{}", 0), http.StatusUnauthorized, errcode.ErrUnauthorized.Code)
		})
	}
	t.Run("管理员可访问", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodGet, "/api/v1/admin/ai/providers", nil), http.StatusOK, 0)
	})
}

// ---------------------------------------------------------------------------
// 草稿 / 校验 / 发布 / 回滚
// ---------------------------------------------------------------------------

func TestAdminAIHandler_ConfigLifecycle(t *testing.T) {
	env := aicNewEnv(t)
	base := "/api/v1/admin/ai"

	t.Run("新建平台草稿：成功并返回 issues 列表", func(t *testing.T) {
		r := env.admin(http.MethodPost, base+"/providers", map[string]any{"body": aicProviderJSON("p1", "p1_key"), "note": "初版"})
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
		aicWant(t, env.admin(http.MethodPost, base+"/providers", map[string]any{"body": aicProviderJSON("p1", "p1_key")}), http.StatusBadRequest, errcode.ErrConfigInvalid.Code)
	})
	t.Run("缺少 body 返回参数错误", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPost, base+"/providers", map[string]any{"note": "x"}), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
	})
	t.Run("body 不是对象返回参数错误", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPost, base+"/providers", map[string]any{"body": []int{1}}), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
	})
	t.Run("请求体不是合法 JSON 返回参数错误", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPost, base+"/providers", "{not json"), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
	})
	t.Run("PUT 路径 key 与正文不一致", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPut, base+"/providers/other", map[string]any{"body": aicProviderJSON("p1", "p1_key")}), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
	})
	t.Run("PUT 不存在的配置 404", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPut, base+"/providers/none", map[string]any{"body": aicProviderJSON("none", "k")}), http.StatusNotFound, errcode.ErrConfigNotFound.Code)
	})
	t.Run("更新草稿版本号递增", func(t *testing.T) {
		r := env.admin(http.MethodPut, base+"/providers/p1", map[string]any{"body": aicProviderJSON("p1", "p1_key")})
		aicWant(t, r, http.StatusOK, 0)
		if !strings.Contains(r.Raw, `"revision_no":2`) {
			t.Fatalf("revision_no 应为 2：%s", r.Raw)
		}
	})
	t.Run("校验：不传 body 校验草稿；传 body 校验传入内容", func(t *testing.T) {
		r := env.admin(http.MethodPost, base+"/providers/p1/validate", nil)
		aicWant(t, r, http.StatusOK, 0)
		if !strings.Contains(r.Raw, `"valid":true`) {
			t.Fatalf("草稿应通过校验：%s", r.Raw)
		}
		bad := aicProviderJSON("p1", "p1_key")
		bad["bad"] = true
		r = env.admin(http.MethodPost, base+"/providers/p1/validate", map[string]any{"body": bad})
		aicWant(t, r, http.StatusOK, 0)
		if !strings.Contains(r.Raw, `"valid":false`) || !strings.Contains(r.Raw, `"path":"bad"`) {
			t.Fatalf("应返回带路径的问题：%s", r.Raw)
		}
	})
	t.Run("校验没有草稿的配置返回 409", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPost, base+"/providers/none/validate", nil), http.StatusConflict, errcode.ErrConfigNoDraft.Code)
	})
	t.Run("凭证未设置时发布返回 409", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPost, base+"/providers/p1/publish", nil), http.StatusConflict, errcode.ErrSecretNotSet.Code)
	})
	t.Run("设置凭证后发布成功", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPut, base+"/secrets/p1_key", map[string]any{"value": "sk-1"}), http.StatusOK, 0)
		r := env.admin(http.MethodPost, base+"/providers/p1/publish", nil)
		aicWant(t, r, http.StatusOK, 0)
		if !strings.Contains(r.Raw, `"status":"published"`) {
			t.Fatalf("应返回已发布的 revision：%s", r.Raw)
		}
	})
	t.Run("发布后再发布返回 409（没有草稿）", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPost, base+"/providers/p1/publish", nil), http.StatusConflict, errcode.ErrConfigNoDraft.Code)
	})
	t.Run("详情包含已发布正文与 revision 元信息", func(t *testing.T) {
		r := env.admin(http.MethodGet, base+"/providers/p1", nil)
		aicWant(t, r, http.StatusOK, 0)
		var d service.ConfigDetail
		if err := json.Unmarshal(r.Data, &d); err != nil || d.Published == nil || d.Published.Status != model.RevisionPublished || len(d.Published.BodyJSON) == 0 || d.Draft != nil {
			t.Fatalf("详情不符合预期：%v %s", err, r.Raw)
		}
	})
	t.Run("详情不存在 404", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodGet, base+"/providers/none", nil), http.StatusNotFound, errcode.ErrConfigNotFound.Code)
	})
	t.Run("再改一版并发布，然后回滚到 v1", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPut, base+"/providers/p1", map[string]any{"body": aicProviderJSON("p1", "p1_key")}), http.StatusOK, 0)
		aicWant(t, env.admin(http.MethodPost, base+"/providers/p1/publish", nil), http.StatusOK, 0)

		r := env.admin(http.MethodGet, base+"/providers/p1/revisions", nil)
		aicWant(t, r, http.StatusOK, 0)
		var revs []model.AIConfigRevision
		_ = json.Unmarshal(r.Data, &revs)
		if len(revs) < 3 || strings.Contains(r.Raw, "body_json\":{") {
			t.Fatalf("历史列表不符合预期（应不含正文）：%s", r.Raw)
		}
		var v1 uint64
		for _, rv := range revs {
			if rv.RevisionNo == 2 { // v2 是第一次发布的版本，此时已归档
				v1 = rv.ID
			}
		}
		aicWant(t, env.admin(http.MethodPost, base+"/providers/p1/rollback", map[string]any{"revision_id": v1}), http.StatusOK, 0)
		// 预览某个历史版本正文
		r = env.admin(http.MethodGet, fmt.Sprintf("%s/providers/p1/revisions/%d", base, v1), nil)
		aicWant(t, r, http.StatusOK, 0)
		if !strings.Contains(r.Raw, "body_json") {
			t.Fatalf("单个 revision 应带正文：%s", r.Raw)
		}
	})
	t.Run("回滚参数校验", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPost, base+"/providers/p1/rollback", map[string]any{}), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
		aicWant(t, env.admin(http.MethodPost, base+"/providers/p1/rollback", map[string]any{"revision_id": 99999}), http.StatusNotFound, errcode.ErrConfigNotFound.Code)
	})
	t.Run("revision id 非法", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodGet, base+"/providers/p1/revisions/abc", nil), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
	})
	t.Run("列表", func(t *testing.T) {
		r := env.admin(http.MethodGet, base+"/providers", nil)
		aicWant(t, r, http.StatusOK, 0)
		if !strings.Contains(r.Raw, `"key":"p1"`) || strings.Contains(r.Raw, "body_json") {
			t.Fatalf("列表不符合预期：%s", r.Raw)
		}
	})
}

func TestAdminAIHandler_ModelEnabledAndSort(t *testing.T) {
	env := aicNewEnv(t)
	env.publishAll(t)
	base := "/api/v1/admin/ai/models/m1"

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
		{"模型不存在", http.MethodPut, "/api/v1/admin/ai/models/none/enabled", map[string]any{"enabled": true}, 404, errcode.ErrConfigNotFound.Code},
		{"修改排序", http.MethodPut, base + "/sort", map[string]any{"sort": 5}, 200, 0},
		{"排序值可以是 0", http.MethodPut, base + "/sort", map[string]any{"sort": 0}, 200, 0},
		{"sort 缺失", http.MethodPut, base + "/sort", map[string]any{}, 400, errcode.ErrInvalidParams.Code},
		{"排序：模型不存在", http.MethodPut, "/api/v1/admin/ai/models/none/sort", map[string]any{"sort": 1}, 404, errcode.ErrConfigNotFound.Code},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			aicWant(t, env.admin(tt.method, tt.path, tt.body), tt.status, tt.code)
		})
	}
}

// ---------------------------------------------------------------------------
// dry-run / 试跑
// ---------------------------------------------------------------------------

func TestAdminAIHandler_DryRunAndTestRun(t *testing.T) {
	env := aicNewEnv(t)
	env.publishAll(t)
	// 再存一个模型草稿用于试跑
	aicWant(t, env.admin(http.MethodPut, "/api/v1/admin/ai/models/m1", map[string]any{"body": aicModelJSON("m1", "p1")}), http.StatusOK, 0)
	base := "/api/v1/admin/ai/models/m1"

	t.Run("dry-run 成功且响应不含凭证", func(t *testing.T) {
		env.dry.Result = map[string]any{"submit": map[string]any{"headers": map[string]any{"Authorization": "Bearer sk-super-secret"}}}
		r := env.admin(http.MethodPost, base+"/dry-run", map[string]any{"input": map[string]any{"prompt": "hi"}})
		aicWant(t, r, http.StatusOK, 0)
		if strings.Contains(r.Raw, "sk-super-secret") || !strings.Contains(r.Raw, "***") {
			t.Fatalf("响应不得包含凭证明文：%s", r.Raw)
		}
	})
	t.Run("dry-run 缺少 input 返回 400", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPost, base+"/dry-run", map[string]any{}), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
	})
	t.Run("dry-run 输入不合法返回 400 + 生成参数错误码", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPost, base+"/dry-run", map[string]any{"input": map[string]any{"invalid": true}}), http.StatusBadRequest, errcode.ErrTaskInput.Code)
	})
	t.Run("dry-run 模型不存在 404", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPost, "/api/v1/admin/ai/models/none/dry-run", map[string]any{"input": map[string]any{}}), http.StatusNotFound, errcode.ErrConfigNotFound.Code)
	})
	t.Run("试跑返回任务视图，并能轮询查询", func(t *testing.T) {
		env.tasks.View = &model.GenerationTaskView{ID: 88, Status: model.TaskPending}
		r := env.admin(http.MethodPost, base+"/test-run", map[string]any{"input": map[string]any{"prompt": "hi"}})
		aicWant(t, r, http.StatusOK, 0)
		if !strings.Contains(r.Raw, `"id":88`) || env.tasks.UserID != aicAdminUser {
			t.Fatalf("试跑响应不符合预期：%s user=%d", r.Raw, env.tasks.UserID)
		}
		env.tasks.Views[88] = aiconfigfake.TestView{UserID: aicAdminUser, View: &model.GenerationTaskView{ID: 88, Status: model.TaskRunning}}
		r = env.admin(http.MethodGet, "/api/v1/admin/ai/test-runs/88", nil)
		aicWant(t, r, http.StatusOK, 0)
		if !strings.Contains(r.Raw, `"status":"running"`) {
			t.Fatalf("轮询结果不符合预期：%s", r.Raw)
		}
	})
	t.Run("试跑缺少 input 返回 400", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPost, base+"/test-run", map[string]any{}), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
	})
	t.Run("查询不存在的试跑任务 404", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodGet, "/api/v1/admin/ai/test-runs/999", nil), http.StatusNotFound, errcode.ErrTaskNotFound.Code)
	})
	t.Run("试跑任务 id 非法 400", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodGet, "/api/v1/admin/ai/test-runs/abc", nil), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
	})
}

// ---------------------------------------------------------------------------
// 凭证 / schema / 导入
// ---------------------------------------------------------------------------

func TestAdminAIHandler_Secrets(t *testing.T) {
	env := aicNewEnv(t)
	base := "/api/v1/admin/ai/secrets"

	t.Run("只写：设置成功不回显任何内容", func(t *testing.T) {
		r := env.admin(http.MethodPut, base+"/rh_key", map[string]any{"value": "sk-plain-text-123"})
		aicWant(t, r, http.StatusOK, 0)
		if strings.Contains(r.Raw, "sk-plain") {
			t.Fatalf("响应不得回显凭证：%s", r.Raw)
		}
	})
	t.Run("列表只有是否已设置等元信息", func(t *testing.T) {
		r := env.admin(http.MethodGet, base, nil)
		aicWant(t, r, http.StatusOK, 0)
		for _, bad := range []string{"sk-plain", "ciphertext", "nonce", "value"} {
			if strings.Contains(r.Raw, bad) {
				t.Fatalf("列表泄露了 %q：%s", bad, r.Raw)
			}
		}
		if !strings.Contains(r.Raw, `"name":"rh_key"`) || !strings.Contains(r.Raw, `"is_set":true`) || !strings.Contains(r.Raw, `"updated_by":1`) {
			t.Fatalf("列表不符合预期：%s", r.Raw)
		}
	})
	t.Run("缺少 value 返回 400", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPut, base+"/rh_key", map[string]any{}), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
	})
	t.Run("凭证名非法返回 400", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPut, base+"/bad%20name", map[string]any{"value": "x"}), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
	})
	t.Run("没有主密钥时返回明确的 500 提示", func(t *testing.T) {
		env2 := aicNewEnv(t)
		svc := service.NewAIConfigService(aiconfigfake.NewMemRepo(), &aiconfigfake.Validator{}, "")
		env2.engine = gin.New()
		env2.engine.Use(func(c *gin.Context) { c.Set(middleware.CtxUserIDKey, uint(1)) })
		NewAdminAIHandler(svc).Register(env2.engine.Group("/api/v1/admin/ai"))
		r := env2.do(http.MethodPut, base+"/k", map[string]any{"value": "v"}, 1)
		aicWant(t, r, http.StatusInternalServerError, errcode.ErrInternal.Code)
		if !strings.Contains(r.Msg, "APP_AI_SECRET_KEY") {
			t.Fatalf("提示应说明缺少主密钥：%s", r.Raw)
		}
	})
}

func TestAdminAIHandler_Schema(t *testing.T) {
	env := aicNewEnv(t)
	r := env.admin(http.MethodGet, "/api/v1/admin/ai/schema/model", nil)
	aicWant(t, r, http.StatusOK, 0)
	if !json.Valid(r.Data) || !strings.Contains(r.Raw, "model") {
		t.Fatalf("应返回 JSON Schema：%s", r.Raw)
	}
	aicWant(t, env.admin(http.MethodGet, "/api/v1/admin/ai/schema/other", nil), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
}

func TestAdminAIHandler_ImportRunningHub(t *testing.T) {
	env := aicNewEnv(t)
	rh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":{"nodeInfoList":[{"nodeId":"6","fieldName":"text","fieldValue":"hi","description":"提示词"},{"nodeId":"12","fieldName":"image","fieldValue":"a.png"}]}}`))
	}))
	defer rh.Close()
	env.svc.SetHTTPClientFactory(&aiconfigfake.HTTPFactory{Client: rh.Client()})

	provider := aicProviderJSON("runninghub", "runninghub_api_key")
	provider["base_url"] = rh.URL
	aicWant(t, env.admin(http.MethodPut, "/api/v1/admin/ai/secrets/runninghub_api_key", map[string]any{"value": "rh-secret-key-xyz"}), http.StatusOK, 0)
	aicWant(t, env.admin(http.MethodPost, "/api/v1/admin/ai/providers", map[string]any{"body": provider}), http.StatusOK, 0)
	aicWant(t, env.admin(http.MethodPost, "/api/v1/admin/ai/providers/runninghub/publish", nil), http.StatusOK, 0)
	path := "/api/v1/admin/ai/import/runninghub"

	t.Run("成功：返回草稿建议与节点信息，不含凭证", func(t *testing.T) {
		r := env.admin(http.MethodPost, path, map[string]any{"webapp_id": "2093984571330498561"})
		aicWant(t, r, http.StatusOK, 0)
		var d struct {
			Draft    map[string]any   `json:"draft"`
			Nodes    []map[string]any `json:"nodes"`
			Warnings []string         `json:"warnings"`
		}
		if err := json.Unmarshal(r.Data, &d); err != nil || d.Draft["key"] != "rh-2093984571330498561" || len(d.Nodes) != 2 || d.Nodes[0]["input_name"] != "prompt" {
			t.Fatalf("响应不符合预期：%v %s", err, r.Raw)
		}
		if strings.Contains(r.Raw, "rh-secret-key-xyz") {
			t.Fatalf("响应不得包含凭证：%s", r.Raw)
		}
		// 只返回建议，不落库
		aicWant(t, env.admin(http.MethodGet, "/api/v1/admin/ai/models/rh-2093984571330498561", nil), http.StatusNotFound, errcode.ErrConfigNotFound.Code)
	})
	t.Run("缺少 webapp_id 返回 400", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPost, path, map[string]any{}), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
	})
	t.Run("webapp_id 含非法字符返回 400", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPost, path, map[string]any{"webapp_id": "12/../34"}), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
	})
	t.Run("kind 非法返回 400", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPost, path, map[string]any{"webapp_id": "1", "kind": "text"}), http.StatusBadRequest, errcode.ErrInvalidParams.Code)
	})
	t.Run("平台不存在 404", func(t *testing.T) {
		aicWant(t, env.admin(http.MethodPost, path, map[string]any{"webapp_id": "1", "provider": "ghost"}), http.StatusNotFound, errcode.ErrConfigNotFound.Code)
	})
}
