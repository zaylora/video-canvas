package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	. "video-canvas/internal/service"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/plugin"
	"video-canvas/internal/provider/pluginrunner"
	"video-canvas/internal/repository"
	"video-canvas/internal/service/aiconfigfake"
	"video-canvas/plugins"
)

// 编译期契约：各服务声明的依赖接口必须能被真实实现直接满足，接线时不需要写适配层。
var (
	_ AIConfigRepo     = (*repository.AIConfigRepository)(nil)
	_ AIConfigRepo     = (*aiconfigfake.MemRepo)(nil)
	_ AIChannelReader  = (*repository.AIChannelRepository)(nil)
	_ AIPluginReader   = (*repository.AIPluginRepository)(nil)
	_ AIPluginRepo     = (*repository.AIPluginRepository)(nil)
	_ AIPluginRepo     = (*aiconfigfake.MemRepo)(nil)
	_ AIChannelRepo    = (*repository.AIChannelRepository)(nil)
	_ AIChannelRepo    = (*aiconfigfake.MemRepo)(nil)
	_ AIChannelLister  = (*repository.AIChannelRepository)(nil)
	_ AIChannelPlugins = (*repository.AIPluginRepository)(nil)
	_ AIChannelSecrets = (*AIConfigService)(nil)
	_ AIAuditWriter    = (*repository.AIChannelRepository)(nil)
	_ AIAuditWriter    = (*aiconfigfake.MemRepo)(nil)
	_ RegistryNotifier = (*AIConfigService)(nil)
	_ DryRunner        = (*plugin.Executor)(nil)
	_ TestTaskCreator  = (*GenerationTaskService)(nil)
)

// e2eCodes 从 fake 仓储里取插件代码，给真实的插件宿主装载用。
type e2eCodes struct{ repo *aiconfigfake.MemRepo }

func (c e2eCodes) Code(_ context.Context, versionID uint64, _ string) (string, error) {
	v, ok := c.repo.Versions[versionID]
	if !ok {
		return "", errors.New("插件版本不存在")
	}
	return v.Code, nil
}

// e2eEnv 是“管理层 + 真实插件运行时”的端到端环境：真实的 runner（进程内）、真实的宿主、内置的 newapi.js，
// 只有数据库用内存仓储、上游 New API 用 httptest 模拟。
type e2eEnv struct {
	cfg      *AIConfigService
	plugins  *AIPluginService
	channels *AIChannelService
	repo     *aiconfigfake.MemRepo
	upstream *httptest.Server

	mu       sync.Mutex
	gotAuths []string // 上游收到的 Authorization 头
}

func newE2EEnv(t *testing.T) *e2eEnv {
	t.Helper()
	e := &e2eEnv{repo: aiconfigfake.NewMemRepo()}
	e.upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.mu.Lock()
		e.gotAuths = append(e.gotAuths, r.Header.Get("Authorization"))
		e.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/models" {
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpt-4o"},{"id":"kling-v2-master"},{"id":"text-embedding-3-small"}]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(e.upstream.Close)

	runner := plugin.NewInProcessRunnerClient(pluginrunner.NewServer(pluginrunner.Options{}).Handler())
	e.cfg = NewAIConfigService(e.repo, e.repo, e.repo, "e2e-master-key")
	exec := plugin.New(plugin.Options{
		Runner: runner, Codes: e2eCodes{e.repo}, Secrets: e.cfg,
		IPAllowed: func(net.IP) bool { return true }, // 上游是本机 httptest 服务
	})
	e.cfg.SetDryRunner(exec)
	e.plugins = NewAIPluginService(e.repo, e.repo, e.repo, plugin.NewPrechecker(runner), e.cfg)
	e.channels = NewAIChannelService(e.repo, e.repo, e.cfg, e.repo, exec, e.cfg)
	return e
}

// 端到端：启动登记内置 newapi.js → 建渠道 → 设 Key → 连通性检查 → 导入模型 → 保存草稿 → 发布 → dry-run，
// 验证管理层各服务与真实插件运行时首尾相接，且 Key 全程不出现在任何管理响应里。
func TestAdminLayer_BuiltinPluginEndToEnd(t *testing.T) {
	ctx := context.Background()
	e := newE2EEnv(t)
	const secret = "sk-e2e-very-secret"

	// 1. 启动时登记内置插件（真实预检）
	sources, err := plugins.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	regs, err := e.plugins.RegisterBuiltin(ctx, sources)
	if err != nil || len(regs) == 0 {
		t.Fatalf("登记内置插件失败：%+v %v", regs, err)
	}
	list, err := e.plugins.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var newapi *PluginView
	for i := range list {
		if list[i].Key == "newapi" {
			newapi = &list[i]
		}
	}
	if newapi == nil || newapi.Source != model.PluginSourceBuiltin || !newapi.Enabled || len(newapi.Versions) != 1 ||
		newapi.Versions[0].Meta == nil || newapi.Versions[0].Meta.Endpoints["text"].Mode != "sync" {
		t.Fatalf("newapi 应已登记为 builtin：%+v", list)
	}
	// 重启后再次登记是空操作
	if regs, err := e.plugins.RegisterBuiltin(ctx, sources); err != nil || regs[0].Registered {
		t.Fatalf("第二次登记应是空操作：%+v %v", regs, err)
	}
	// 内置插件不能删
	adminWantCode(t, e.plugins.DeleteVersion(ctx, 1, "newapi", newapi.Versions[0].Version), errcode.ErrPluginBuiltin.Code)

	// 2. 建渠道（自建网关在内网：trusted_internal），设 Key
	view, err := e.channels.Create(ctx, ChannelCreateInput{
		Key: "newapi-main", Name: "自建 New API", PluginKey: "newapi", PluginVersion: newapi.Versions[0].Version,
		BaseURL: e.upstream.URL, TrustedInternal: true, Enabled: true, ActorID: 1,
	})
	if err != nil || view.SecretSet {
		t.Fatalf("建渠道失败：%+v %v", view, err)
	}
	if err := e.channels.SetSecret(ctx, 1, "newapi-main", secret); err != nil {
		t.Fatal(err)
	}

	// 3. 连通性检查：真实执行 newapi.js 的 buildCheckRequest，宿主注入 Bearer
	res, err := e.channels.Check(ctx, "newapi-main")
	if err != nil || !res.OK {
		t.Fatalf("连通性检查应成功：%+v %v", res, err)
	}
	e.mu.Lock()
	if len(e.gotAuths) == 0 || e.gotAuths[len(e.gotAuths)-1] != "Bearer "+secret {
		e.mu.Unlock()
		t.Fatalf("上游应收到宿主注入的 Bearer：%v", e.gotAuths)
	}
	e.mu.Unlock()

	// 4. 导入模型：newapi.js 的导入钩子把 /v1/models 转成草稿（只有文本 / 视频模型）
	imp, err := e.channels.Import(ctx, "newapi-main", nil)
	if err != nil {
		t.Fatalf("导入失败：%v", err)
	}
	var textDraft *provider.ModelDraft
	for i := range imp.Drafts {
		if imp.Drafts[i].UpstreamModel == "gpt-4o" {
			textDraft = &imp.Drafts[i]
		}
	}
	if textDraft == nil || textDraft.Kind != "text" {
		t.Fatalf("应导入出文本模型 gpt-4o：%+v", imp.Drafts)
	}
	for _, d := range imp.Drafts {
		if d.UpstreamModel == "text-embedding-3-small" {
			t.Fatalf("不支持的种类不应被导入：%+v", d)
		}
	}

	// 5. 保存模型 → 启用 → 出现在清单里
	body := `{"key":"gpt","kind":"text","label":"GPT","enabled":true,"pricing":{"billing":"token","token":{"in":2,"out":8}},` +
		`"channels":[{"channel":"newapi-main","upstream_model":"gpt-4o"}],` +
		`"capabilities":{"prompt":{"max_length":8000},"context":{"window":128000,"output":4096},"system":"保密的系统提示"}}`
	saved, err := e.cfg.SaveModel(ctx, ModelSaveInput{Create: true, Body: json.RawMessage(body), AdminID: 1})
	if err != nil || len(saved.Issues) != 0 {
		t.Fatalf("保存失败：%+v %v", saved, err)
	}
	if err := e.cfg.SetModelEnabled(ctx, "gpt", true, 1); err != nil {
		t.Fatalf("启用失败：%v", err)
	}
	models, err := e.cfg.ListModels(ctx, "text")
	if err != nil || len(models) != 1 || models[0].Key != "gpt" {
		t.Fatalf("文本模型应出现在清单里：%+v %v", models, err)
	}
	items, _ := e.cfg.ListConfigs(ctx)
	if len(items) != 1 || items[0].Label != "GPT" || items[0].Channel != "newapi-main" {
		t.Fatalf("管理列表应带 label / channel：%+v", items)
	}

	// 6. dry-run：真实执行 buildSubmitRequest，返回的描述里不含 Key
	out, err := e.cfg.DryRun(ctx, "gpt", map[string]any{"prompt": "你好"})
	if err != nil {
		t.Fatalf("dry-run 失败：%v", err)
	}
	text := aicJSON(out)
	if strings.Contains(text, secret) || !strings.Contains(text, "/v1/chat/completions") || !strings.Contains(text, "你好") {
		t.Fatalf("dry-run 结果不符合预期：%s", text)
	}

	// 7. 全程管理响应都不含 Key
	all, _ := json.Marshal([]any{view, res, imp, saved, items, out})
	if v, _ := e.channels.Get(ctx, "newapi-main"); v != nil {
		b, _ := json.Marshal(v)
		all = append(all, b...)
		if !v.SecretSet {
			t.Fatal("secret_set 应为 true")
		}
	}
	if strings.Contains(string(all), secret) {
		t.Fatalf("管理响应里不能出现 Key：%s", all)
	}
	// 审计里也没有
	auditJSON, _ := json.Marshal(e.repo.Audits)
	if strings.Contains(string(auditJSON), secret) {
		t.Fatalf("审计里不能出现 Key：%s", auditJSON)
	}
}

// 端到端：上传一份用户写的插件（真实预检），再对预检不通过、版本号重复两种情况验证错误精确到字段。
func TestAdminLayer_UploadPluginEndToEnd(t *testing.T) {
	ctx := context.Background()
	e := newE2EEnv(t)
	code := func(version string) []byte {
		return []byte(`module.exports = {
  meta: { apiVersion: 1, key: "demo", name: "演示协议", version: "` + version + `", auth: { type: "bearer" },
          endpoints: { text: { mode: "sync" } } },
  buildSubmitRequest: function (ctx) { return { method: "POST", path: "/echo", json: { q: ctx.input.prompt } }; },
  parseSubmitResponse: function (ctx, resp) { return { immediate: { status: "succeeded", outputs: [{ type: "text", text: "ok" }] } }; }
};`)
	}

	res, err := e.plugins.Upload(ctx, 7, code("1.0.0"))
	if err != nil || !res.Accepted || res.Version == nil || res.Version.Meta == nil || res.Version.Meta.Key != "demo" {
		t.Fatalf("合法插件应被接受：%+v %v", res, err)
	}
	// 登记的代码与上传的一致，sha256 是它的哈希
	if stored := e.repo.Versions[res.Version.ID]; stored == nil || stored.Code != string(code("1.0.0")) || stored.SHA256 != res.Version.SHA256 {
		t.Fatalf("登记的版本不符合预期：%+v", stored)
	}

	dup, err := e.plugins.Upload(ctx, 7, code("1.0.0"))
	if err != nil || dup.Accepted || len(dup.Issues) != 1 || dup.Issues[0].Path != "meta.version" {
		t.Fatalf("重复版本应报在 meta.version：%+v %v", dup, err)
	}

	// 缺少 async 必需的 query 钩子：预检精确指出字段
	bad, err := e.plugins.Upload(ctx, 7, []byte(`module.exports = {
  meta: { apiVersion: 1, key: "bad", name: "坏插件", version: "1.0.0", auth: { type: "bearer" }, endpoints: { video: { mode: "async" } } },
  buildSubmitRequest: function (ctx) { return { method: "POST", path: "/x" }; },
  parseSubmitResponse: function (ctx, resp) { return { providerTaskId: "1" }; }
};`))
	if err != nil || bad.Accepted || len(bad.Issues) == 0 {
		t.Fatalf("缺少 query 钩子应被拒绝：%+v %v", bad, err)
	}
	if _, ok := e.repo.Plugins["bad"]; ok {
		t.Fatal("预检不通过不该登记")
	}

	// 语法错误、apiVersion 不支持
	syntax, err := e.plugins.Upload(ctx, 7, []byte(`module.exports = {`))
	if err != nil || syntax.Accepted || len(syntax.Issues) == 0 {
		t.Fatalf("语法错误应被拒绝：%+v %v", syntax, err)
	}
	apiV, err := e.plugins.Upload(ctx, 7, []byte(`module.exports = { meta: { apiVersion: 99, key: "v99", name: "x", version: "1.0.0", auth: { type: "none" }, endpoints: { text: { mode: "sync" } } },
  buildSubmitRequest: function () { return {}; }, parseSubmitResponse: function () { return {}; } };`))
	if err != nil || apiV.Accepted || len(apiV.Issues) == 0 || !strings.Contains(apiV.Issues[0].Path, "apiVersion") {
		t.Fatalf("apiVersion 不支持应被拒绝并指出字段：%+v %v", apiV, err)
	}
}
