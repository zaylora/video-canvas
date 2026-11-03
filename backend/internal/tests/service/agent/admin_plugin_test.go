package agent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/plugin"
	"video-canvas/internal/provider/pluginrunner"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"

	. "video-canvas/internal/service/agent"
)

func TestAdminPhaseTwoToolsOnlyAdmin(t *testing.T) {
	for _, name := range []string{"admin_list_plugins", "admin_get_plugin", "admin_plugin_check", "admin_request_secret", "admin_test_run"} {
		if !slices.Contains(AgentToolsForMode("admin"), name) {
			t.Errorf("后台缺少工具 %s", name)
		}
		if slices.Contains(AgentToolsForMode("all"), name) {
			t.Errorf("画布不应获得 %s", name)
		}
	}
}

func TestAdminSecretRequiresSavedCredential(t *testing.T) {
	f, b, rc := phaseTwoEnv()
	p, err := b.ProposeApproval(context.Background(), rc, "admin_secret", json.RawMessage(`{"channel_key":"main","reason":"平台鉴权"}`))
	if err != nil || p.IsError {
		t.Fatalf("密钥卡片提案失败：%+v %v", p, err)
	}
	o, err := b.ResolveApproval(context.Background(), rc, "admin_secret", p.Payload, model.DecideAgentApprovalReq{Decision: "approve", Answer: "configured"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := b.ExecuteApproval(context.Background(), rc, "admin_secret", 1, "tool", *o)
	if err != nil || result.Status != ExecFailed || string(result.Result) != `{"configured":false}` {
		t.Fatalf("未保存也批准：%+v %v", result, err)
	}
	f.configured = true
	result, err = b.ExecuteApproval(context.Background(), rc, "admin_secret", 1, "tool", *o)
	if err != nil || result.Status != ExecExecuted || string(result.Result) != `{"configured":true}` {
		t.Fatalf("保存后未实时查库：%+v %v", result, err)
	}
	f.user.Role = model.RoleAdmin
	result, err = b.ExecuteApproval(context.Background(), rc, "admin_secret", 1, "tool", *o)
	if err != nil || result.Status != ExecFailed {
		t.Fatalf("降权仍通过：%+v %v", result, err)
	}
	_, err = b.ResolveApproval(context.Background(), rc, "admin_secret", p.Payload, model.DecideAgentApprovalReq{Decision: "approve", Answer: "LEAK-SENTINEL"})
	if err == nil {
		t.Fatal("密钥回答不应接受")
	}
}

const phaseTwoCode = `module.exports = {
 meta: {apiVersion:1,key:"phase-two",name:"二期测试",vendor:"test",auth:{type:"bearer"},endpoints:{video:{mode:"sync",method:"POST",path:"/generate"}}},
 buildSubmitRequest: function(ctx) { utils.log("静态日志"); return {method:"POST",path:"/generate",json:{prompt:ctx.input.prompt},responseType:"json"}; },
 parseSubmitResponse: function(ctx,res) { return {immediate:[{url:res.body.url,mediaType:"video"}]}; }
};`

type phaseTwoFake struct {
	*adminFake
	mu           sync.Mutex
	counts       map[string]map[string]int
	configured   bool
	versions     []model.AIPluginVersion
	plugins      []model.AIPlugin
	trialCalls   int
	trialKeys    map[string]bool
	trialStatus  string
	trialCreated time.Time
}

func phaseTwoEnv() (*phaseTwoFake, *AgentBridge, RunContext) {
	f := &phaseTwoFake{adminFake: newAdminFake(), counts: map[string]map[string]int{}, trialKeys: map[string]bool{}, trialStatus: model.TaskSucceeded}
	f.user.Role = model.RoleSuperAdmin
	checker := plugin.NewDraftChecker(plugin.NewInProcessRunnerClient(pluginrunner.NewServer(pluginrunner.Options{}).Handler()))
	s := NewAdminAssistantService(AdminAssistantDeps{Models: f, Writer: f, Users: f, Channels: f, Audit: f, Settings: service.NewPricingSettingsService(f, nil), Plugins: f, Checker: checker, Attempts: f, Trials: f})
	return f, NewAgentBridge(BridgeDeps{Admin: s, DramaApprovalKey: []byte("phase-two-service-key")}), RunContext{RunID: 11, UserID: 1, SessionID: 1, Mode: "admin"}
}
func (f *phaseTwoFake) ListSecrets(context.Context) ([]model.AISecret, error) {
	if f.configured {
		return []model.AISecret{{Name: model.ChannelSecretName("main")}}, nil
	}
	return nil, nil
}
func (f *phaseTwoFake) ClaimAdminAttempt(_ context.Context, run uint64, kind, key, token string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	group := fmt.Sprintf("%d:%s:%s", run, kind, key)
	if f.counts[group] == nil {
		f.counts[group] = map[string]int{}
	}
	rows := f.counts[group]
	if n := rows[token]; n > 0 {
		return n, nil
	}
	if len(rows) == 3 {
		return 0, nil
	}
	n := len(rows) + 1
	rows[token] = n
	return n, nil
}
func (f *phaseTwoFake) ListPlugins(context.Context) ([]model.AIPlugin, error) { return f.plugins, nil }
func (f *phaseTwoFake) GetPlugin(_ context.Context, key string) (*model.AIPlugin, error) {
	for _, p := range f.plugins {
		if p.Key == key {
			return &p, nil
		}
	}
	return nil, repository.ErrNotFound
}
func (f *phaseTwoFake) ListVersions(_ context.Context, key string) ([]model.AIPluginVersion, error) {
	out := []model.AIPluginVersion{}
	for _, v := range f.versions {
		if key == "" || v.PluginKey == key {
			out = append(out, v)
		}
	}
	return out, nil
}
func (f *phaseTwoFake) GetVersion(_ context.Context, id uint64) (*model.AIPluginVersion, error) {
	for _, v := range f.versions {
		if v.ID == id {
			return &v, nil
		}
	}
	return nil, repository.ErrNotFound
}
func (f *phaseTwoFake) TestRunIdempotent(_ context.Context, _ uint64, _ string, _ map[string]any, key string) (*model.GenerationTaskView, error) {
	if !f.trialKeys[key] {
		f.trialKeys[key] = true
		f.trialCalls++
	}
	return &model.GenerationTaskView{ID: 77, Status: f.trialStatus, CreatedAt: f.trialCreated, ErrorCode: "upstream_error", Outputs: []model.TaskOutput{{URL: "https://output.invalid/video.mp4"}}}, nil
}
func (f *phaseTwoFake) GetTestRun(context.Context, uint64, uint64) (*model.GenerationTaskView, error) {
	return &model.GenerationTaskView{ID: 77, Status: model.TaskSucceeded}, nil
}
func (f *phaseTwoFake) GetTestTrace(context.Context, uint64, uint64) (*service.TestTraceView, error) {
	logs := []string{}
	for i := 0; i < 25; i++ {
		logs = append(logs, fmt.Sprintf("日志%02d", i))
	}
	return &service.TestTraceView{Steps: []provider.TraceStep{{Hook: &provider.TraceHook{Name: "parseSubmitResponse", Logs: logs}}, {Response: &provider.TraceResponse{Status: 201}}}}, nil
}

func checkPhaseTwo(t *testing.T, b *AgentBridge, rc RunContext, code string, extra map[string]any) provider.PluginDraftResult {
	t.Helper()
	args := map[string]any{"code": code}
	for k, v := range extra {
		args[k] = v
	}
	raw, _ := json.Marshal(args)
	res, err := b.ExecutePlatformTool(context.Background(), rc, fmt.Sprint(time.Now().UnixNano()), "admin_plugin_check", raw)
	if err != nil {
		t.Fatalf("检查失败：%+v %v", res, err)
	}
	var out provider.PluginDraftResult
	if err := json.Unmarshal([]byte(res.Content), &out); err != nil {
		t.Fatal(err)
	}
	if res.IsError != !out.OK {
		t.Fatalf("工具成功状态与预检结论不一致：%+v %+v", res, out)
	}
	return out
}

func TestAdminPluginCheckSandboxAndLimits(t *testing.T) {
	_, b, rc := phaseTwoEnv()
	for i := 1; i <= 4; i++ {
		out := checkPhaseTwo(t, b, rc, phaseTwoCode, map[string]any{"responses": map[string]any{"submit": map[string]any{"status": 200, "body": map[string]any{"url": "https://output.invalid/a.mp4"}}}})
		if i == 4 {
			if out.OK || out.AttemptsLeft != 0 || !strings.Contains(strings.Join(out.Issues, ""), "已达自动修复上限") {
				t.Fatalf("第4次未拒绝：%+v", out)
			}
			continue
		}
		if !out.OK || out.Attempt != i || out.AttemptsLeft != 3-i || out.Request["url"] != "https://example.invalid/generate" || len(out.Parsed["submit"]) == 0 {
			t.Fatalf("静态验证结果不符：%+v", out)
		}
	}
	rc.RunID++
	if out := checkPhaseTwo(t, b, rc, phaseTwoCode, nil); out.Attempt != 1 || !out.OK {
		t.Fatalf("新运行应重新计次：%+v", out)
	}
}

func TestAdminPluginCheckErrorsAndRedaction(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		wantOK     bool
	}{
		{"语法错误", "module.exports = {", false},
		{"钩子异常", strings.Replace(phaseTwoCode, `utils.log("静态日志");`, `throw new Error("样例异常");`, 1), false},
		{"钩子超时", strings.Replace(phaseTwoCode, `utils.log("静态日志");`, `while(true) {}`, 1), false},
		{"禁止网络", strings.Replace(phaseTwoCode, `utils.log("静态日志");`, `fetch("https://example.com");`, 1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, b, rc := phaseTwoEnv()
			out := checkPhaseTwo(t, b, rc, tc.code, nil)
			if out.OK != tc.wantOK || len(out.Issues) == 0 {
				t.Fatalf("%+v", out)
			}
		})
	}
	_, b, rc := phaseTwoEnv()
	code := strings.Replace(phaseTwoCode, `utils.log("静态日志");`, `utils.log(ctx.channel.settings.api_key);`, 1)
	secret := "STATIC-KEY-SENTINEL"
	out := checkPhaseTwo(t, b, rc, code, map[string]any{"ctx_sample": map[string]any{"input": map[string]any{"prompt": "测试"}, "channel": map[string]any{"baseUrl": "https://example.invalid", "settings": map[string]any{"api_key": secret}}}})
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), secret) {
		t.Fatalf("静态日志泄露凭证：%s", raw)
	}
}

func TestAdminTestRunApprovalLimitAndTrace(t *testing.T) {
	f, b, rc := phaseTwoEnv()
	ctx := context.Background()
	for i := 1; i <= 4; i++ {
		p, err := b.ProposeApproval(ctx, rc, "admin_test_run", json.RawMessage(`{"model_key":"a","input":{"prompt":"试跑","op":"t2v"},"reason":"验证新平台"}`))
		if err != nil {
			t.Fatal(err)
		}
		if i == 4 {
			if !p.IsError || !strings.Contains(p.Content, "已达试跑上限") {
				t.Fatalf("第4次未拒绝：%+v", p)
			}
			break
		}
		if p.IsError || f.trialCalls != i-1 {
			t.Fatalf("审批前不应提交：%+v %d", p, f.trialCalls)
		}
		var payload map[string]any
		_ = json.Unmarshal(p.Payload, &payload)
		if payload["attempt"] != float64(i) || payload["attempts_left"] != float64(3-i) {
			t.Fatalf("计次错误：%s", p.Payload)
		}
		o, err := b.ResolveApproval(ctx, rc, "admin_test_run", p.Payload, model.DecideAgentApprovalReq{Decision: "approve"})
		if err != nil {
			t.Fatal(err)
		}
		out, err := b.ExecuteApproval(ctx, rc, "admin_test_run", uint64(i), "tool", *o)
		if err != nil || out.Status != ExecExecuted || !strings.Contains(out.OutcomeText, "HTTP 201") || strings.Contains(out.OutcomeText, "日志20") || !strings.Contains(out.OutcomeText, "https://output.invalid/video.mp4") {
			t.Fatalf("结果或trace错误：%+v %v", out, err)
		}
		_, err = b.ExecuteApproval(ctx, rc, "admin_test_run", uint64(i), "tool", *o)
		if err != nil || f.trialCalls != i {
			t.Fatalf("重放重复试跑：%v %d", err, f.trialCalls)
		}
	}
}

func TestAdminTestRunInputErrorIdentifiesMissingField(t *testing.T) {
	_, b, rc := phaseTwoEnv()
	res, err := b.ProposeApproval(context.Background(), rc, "admin_test_run", json.RawMessage(`{"model_key":"a","input":{},"reason":"验证字段错误"}`))
	if err != nil || !res.IsError || !strings.Contains(res.Content, "prompt") {
		t.Fatalf("试跑输入错误必须指出缺失字段：%+v %v", res, err)
	}
}

func TestAdminTrialRejectFailureTimeoutAndTampering(t *testing.T) {
	for _, scenario := range []string{"拒绝不提交", "失败带trace", "超时仍在运行", "篡改输入拒绝", "跨运行拒绝", "降权拒绝"} {
		t.Run(scenario, func(t *testing.T) {
			f, b, rc := phaseTwoEnv()
			ctx := context.Background()
			p, err := b.ProposeApproval(ctx, rc, "admin_test_run", json.RawMessage(`{"model_key":"a","input":{"prompt":"测试"},"reason":"接入平台"}`))
			if err != nil || p.IsError {
				t.Fatalf("%+v %v", p, err)
			}
			decision := "approve"
			if scenario == "拒绝不提交" {
				decision = "reject"
			}
			o, err := b.ResolveApproval(ctx, rc, "admin_test_run", p.Payload, model.DecideAgentApprovalReq{Decision: decision})
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "失败带trace":
				f.trialStatus = model.TaskFailed
			case "超时仍在运行":
				f.trialStatus = model.TaskRunning
				f.trialCreated = time.Now().Add(-11 * time.Minute)
			case "篡改输入拒绝":
				o.AdminAction.Input["prompt"] = "篡改"
			case "跨运行拒绝":
				rc.RunID++
			case "降权拒绝":
				f.user.Role = model.RoleUser
			}
			out, err := b.ExecuteApproval(ctx, rc, "admin_test_run", 101, "tool", *o)
			if scenario == "篡改输入拒绝" || scenario == "跨运行拒绝" {
				if err == nil || f.trialCalls != 0 {
					t.Fatalf("应拒绝：%+v %v", out, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "拒绝不提交" || scenario == "降权拒绝" {
				if f.trialCalls != 0 {
					t.Fatal("未授权发起了真实试跑")
				}
				return
			}
			if scenario == "失败带trace" && (out.Status != ExecFailed || !strings.Contains(out.OutcomeText, "HTTP 201")) {
				t.Fatalf("失败没有trace：%+v", out)
			}
			if scenario == "超时仍在运行" && (!strings.Contains(string(out.Result), `"status":"timeout"`) || !strings.Contains(out.OutcomeText, "仍在运行")) {
				t.Fatalf("超时错误：%+v", out)
			}
		})
	}
}

func TestAdminChangeInvalidDependencyGraphs(t *testing.T) {
	for _, deps := range [][]int{{0}, {8}, {1, 1}} {
		_, b, rc := phaseTwoEnv()
		input := []AdminChangeInput{{Target: "channel", Key: "new", Patch: map[string]any{"name": "新渠道", "base_url": "https://example.invalid"}, DependsOn: deps}}
		raw, _ := json.Marshal(AdminChangeRequest{Title: "新渠道", Reason: "用户要求", Items: input})
		p, err := b.ProposeApproval(context.Background(), rc, "admin_change", raw)
		if err != nil || !p.IsError || !strings.Contains(p.Content, "depends_on") {
			t.Fatalf("非法依赖不应出单：%+v %v", p, err)
		}
	}
}

func TestAdminPluginReadAndVersionDetail(t *testing.T) {
	f, b, rc := phaseTwoEnv()
	ctx := context.Background()
	meta := model.JSONText(`{"apiVersion":1,"key":"phase-two","name":"二期测试","version":"1.0.0","vendor":"test","auth":{"type":"bearer"},"endpoints":{"video":{"mode":"sync"}}}`)
	f.plugins = []model.AIPlugin{{Key: "phase-two", Name: "二期测试", Source: model.PluginSourceBuiltin, Enabled: true}}
	f.versions = []model.AIPluginVersion{{ID: 42, PluginKey: "phase-two", Version: "1.0.0", Code: phaseTwoCode, MetaJSON: meta}}
	for _, tool := range []string{"admin_list_plugins", "admin_get_plugin"} {
		res, err := b.ExecutePlatformTool(ctx, rc, "read", tool, json.RawMessage(`{"key":"phase-two"}`))
		if err != nil || res.IsError || !strings.Contains(res.Content, "1.0.0") {
			t.Fatalf("读插件失败：%+v %v", res, err)
		}
		if tool == "admin_list_plugins" && !strings.Contains(res.Content, `"builtin":true`) {
			t.Fatalf("缺内置状态：%s", res.Content)
		}
		if tool == "admin_get_plugin" && !strings.Contains(res.Content, "buildSubmitRequest") {
			t.Fatal("未返回内置插件完整代码")
		}
	}
	f.plugins[0].Source = model.PluginSourceUploaded
	code := phaseTwoCode + "\n// 调整响应解析"
	checkPhaseTwo(t, b, rc, code, nil)
	_, payload := proposeAdmin(t, b, rc, AdminChangeInput{Target: "plugin", Key: "phase-two", Patch: map[string]any{"code": code}})
	item := payload.Items[0]
	raw, _ := json.Marshal(item.Detail)
	if len(item.Diff) == 0 || !strings.Contains(string(raw), `"version_after":"1.0.1"`) || !strings.Contains(string(raw), "--- before") {
		t.Fatalf("版本与代码差异错误：%s %+v", raw, item.Diff)
	}
	for _, diff := range item.Diff {
		if strings.Contains(diff.Path, "code") {
			t.Fatal("源码不应进入字段diff")
		}
	}
}

func TestAdminCreateDependenciesAndManualRollback(t *testing.T) {
	f, b, rc := phaseTwoEnv()
	ctx := context.Background()
	f.user.Role = model.RoleAdmin
	checkPhaseTwo(t, b, rc, phaseTwoCode, nil)
	var body map[string]any
	_ = json.Unmarshal(f.bodies["a"], &body)
	body["key"] = "new"
	items := []AdminChangeInput{{Target: "model_create", Key: "new", Patch: body, DependsOn: []int{1, 2}}, {Target: "plugin", Key: "phase-two", Patch: map[string]any{"code": phaseTwoCode}}, {Target: "channel", Key: "new-channel", Patch: map[string]any{"name": "新渠道", "base_url": "https://example.invalid"}, DependsOn: []int{1}}}
	proposal, payload := proposeAdmin(t, b, rc, items...)
	if payload.Items[0].RequiredRole != "admin" || payload.Items[1].RequiredRole != "super_admin" || payload.Items[2].RequiredRole != "super_admin" {
		t.Fatalf("权限不符：%s", proposal.Payload)
	}
	o, err := b.ResolveApproval(ctx, rc, "admin_change", proposal.Payload, model.DecideAgentApprovalReq{Decision: "approve"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := b.ExecuteApproval(ctx, rc, "admin_change", 77, "tool", *o)
	if err != nil {
		t.Fatal(err)
	}
	var result AdminChangeResult
	_ = json.Unmarshal(out.Result, &result)
	if len(result.Items) != 3 || result.Items[0].Status != "skipped" || !strings.Contains(result.Items[0].Message, "依赖的") || !strings.Contains(result.Items[1].Message, "super_admin") {
		t.Fatalf("依赖权限执行错误：%s", out.Result)
	}
	for _, target := range []string{"plugin", "channel", "model_create"} {
		f.audits = append(f.audits, repository.AssistantAuditRecord{ID: 9, Target: target, Key: "new", Detail: model.JSONText(`{"order_no":"CO-ORIGINAL","before":null,"after":{},"diff":[]}`)})
	}
	raw := json.RawMessage(`{"revert_of":"CO-ORIGINAL"}`)
	revert, err := b.ProposeApproval(ctx, rc, "admin_change", raw)
	if err != nil || !revert.IsError || !strings.Contains(revert.Content, "手动删除") {
		t.Fatalf("回滚不应自动删除：%+v %v", revert, err)
	}
}
