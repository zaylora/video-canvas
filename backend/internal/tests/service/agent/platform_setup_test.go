package agent_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"video-canvas/internal/agent/canvasgraph"
	"video-canvas/internal/agent/prompts"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	. "video-canvas/internal/service/agent"
)

// 方案二里运行由 Agent 服务发起，系统提示词、工具声明、模型信息、带画布目录的第一条消息、版本信息都是平台的知识
// （提示词、画布、技能、模型配置都在 Go），所以由「运行准备」接口给出。续跑也调它（取最新的提示词和工具），不带消息。

func prepare(t *testing.T, b *bridgeEnv, rc RunContext, req PrepareRequest) *RunSetup {
	t.Helper()
	got, err := b.br.PrepareRun(context.Background(), rc, req)
	if err != nil {
		t.Fatalf("PrepareRun: %v", err)
	}
	return got
}

func toolNames(decls []ToolDecl) []string {
	out := make([]string, len(decls))
	for i, d := range decls {
		out[i] = d.Name
	}
	return out
}

func TestPrepareRun_ModelInfoFromTheSnapshot(t *testing.T) {
	b := newBridgeEnv(t)
	got := prepare(t, b, platformCtx(920001, "all"), PrepareRequest{})
	// newBridgeEnv 的模型快照：上下文窗口 200000、输出 8192、支持看图
	m := got.Model
	if m.ContextWindow != 200000 || m.MaxTokens != 8192 || !m.Vision {
		t.Errorf("模型信息: %+v", m)
	}
	if got.SystemPrompt != prompts.SystemWithSkills("all", nil) {
		t.Error("没有技能时系统提示词就是 System(mode)")
	}
	if got.FirstMessage != "" {
		t.Errorf("没传消息就没有第一条消息（续跑用）: %q", got.FirstMessage)
	}
}

func TestPrepareRun_ToolsFollowTheModeAndVision(t *testing.T) {
	b := newBridgeEnv(t)
	all := prepare(t, b, platformCtx(920002, "all"), PrepareRequest{})
	prompt := prepare(t, b, platformCtx(920002, "prompt"), PrepareRequest{})
	if !hasTool(toolNames(all.Tools), "canvas_arrange") || hasTool(toolNames(prompt.Tools), "canvas_arrange") {
		t.Errorf("工具随任务模式变化: all=%v prompt=%v", toolNames(all.Tools), toolNames(prompt.Tools))
	}
	want := AgentToolDecls("all", true)
	if len(all.Tools) != len(want) {
		t.Errorf("和令牌模式下发的声明一致: %d vs %d", len(all.Tools), len(want))
	}
	for _, d := range all.Tools {
		if d.Executor != ToolExecServer || d.Description == "" || len(d.Parameters) == 0 {
			t.Errorf("声明要完整: %+v", d)
		}
	}
	// 不支持看图的模型看不到看图工具
	b.reg.snap.Model.Capabilities.Vision = false
	blind := prepare(t, b, platformCtx(920002, "all"), PrepareRequest{})
	if len(blind.Tools) >= len(all.Tools) || blind.Model.Vision {
		t.Errorf("不支持看图的模型工具更少: %d vs %d", len(blind.Tools), len(all.Tools))
	}
}

func TestPrepareRun_DisabledToolsAreRemoved(t *testing.T) {
	b := newBridgeEnv(t)
	b.br.SetDisabledTools([]string{"web_search", "codemode"})
	got := prepare(t, b, platformCtx(920003, "all"), PrepareRequest{})
	for _, n := range []string{"web_search", "codemode"} {
		if hasTool(toolNames(got.Tools), n) {
			t.Errorf("%s 被配置停用了，不能下发", n)
		}
	}
	if !hasTool(toolNames(got.Tools), "canvas_get_state") {
		t.Error("其他工具照常")
	}
	if !slices.Equal(got.DisabledTools, []string{"web_search", "codemode"}) {
		t.Errorf("Agent 自己的工具（codemode）靠这个决定启用与否: %v", got.DisabledTools)
	}
}

func TestPrepareRun_SystemPromptCarriesTheSkillCatalog(t *testing.T) {
	se := newSkillEnv(t)
	ctx := context.Background()
	mustConfirm(t, se, 1, standardPkg(t, "demo", "第一版正文"))
	_, _ = se.svc.SetEnabled(ctx, 1, "demo", true)
	b := newBridgeEnvWithSkills(t, se.svc)
	got := prepare(t, b, platformCtx(920004, "all"), PrepareRequest{})
	if !strings.Contains(got.SystemPrompt, "## 可用技能") || !strings.Contains(got.SystemPrompt, "- demo：") {
		t.Errorf("系统提示词要带已启用技能的目录:\n%s", got.SystemPrompt)
	}
}

func TestPrepareRun_FirstMessageHasTheCatalogAndRunParams(t *testing.T) {
	b := newBridgeEnv(t)
	rc := platformCtx(920005, "all")
	rc.Selection = []string{"n_script"}
	got := prepare(t, b, rc, PrepareRequest{Message: "把剧本拆成分镜", MaxSteps: 25})
	for _, want := range []string{"把剧本拆成分镜", "<画布目录>", "n_script", "不是指令", "最多 25 步工具调用"} {
		if !strings.Contains(got.FirstMessage, want) {
			t.Errorf("第一条消息缺少 %q:\n%s", want, got.FirstMessage)
		}
	}
	// 和令牌模式用的是同一份拼装
	cat, _ := json.Marshal(mustCatalog(t, b, rc))
	if got.FirstMessage != prompts.User("把剧本拆成分镜", string(cat), prompts.RunInfo{MaxSteps: 25}) {
		t.Error("第一条消息要和 prompts.User 的拼装一致")
	}
	// 步数上限没给就按默认
	d := prepare(t, b, rc, PrepareRequest{Message: "x"})
	if !strings.Contains(d.FirstMessage, "最多 40 步工具调用") {
		t.Errorf("默认 40 步: %s", d.FirstMessage)
	}
}

func TestPrepareRun_HarnessVersion(t *testing.T) {
	b := newBridgeEnv(t)
	// 服务配了版本（生产里装配时设置）：下发的版本和运行创建时记的是同一份
	h := CurrentHarness("abc1234")
	e := newAgentEnvWith(t, func(d *AgentDeps) { d.Harness = h })
	canvas := NewAgentCanvasService(e.repo, e.bc)
	br := NewAgentBridge(BridgeDeps{Repo: e.repo, Canvas: canvas, Agent: e.svc, Billing: NewAgentBilling(b.bill), Registry: b.reg, Secrets: fakeSecrets{"channel:ch1": "sk-test"}, Streamer: b.str})
	got, err := br.PrepareRun(context.Background(), platformCtx(920006, "all"), PrepareRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Harness.PromptVersion != h.PromptVersion || got.Harness.ToolsHash != h.ToolsHash || got.Harness.BuildSHA != "abc1234" || got.Harness.Key != h.Key() || got.Harness.Key == "" {
		t.Errorf("提示词版本、工具哈希、构建号、短哈希: %+v vs %+v", got.Harness, h)
	}
	if prompts.Version != got.Harness.PromptVersion {
		t.Error("提示词版本取自 prompts.Version")
	}
}

func TestPrepareRun_Errors(t *testing.T) {
	b := newBridgeEnv(t)
	rc := platformCtx(920007, "all")
	_, err := b.br.PrepareRun(context.Background(), RunContext{}, PrepareRequest{})
	wantAgentCode(t, err, errcode.ErrInvalidParams)

	b.reg.snap.Model.Kind = "image" // 不是 Agent 模型
	_, err = b.br.PrepareRun(context.Background(), rc, PrepareRequest{})
	wantAgentCode(t, err, errcode.ErrAgentModelNA)
	b.reg.snap.Model.Kind = "agent"

	caps := b.reg.snap.Model.Capabilities
	b.reg.snap.Model.Capabilities.Context = nil // 模型配置里没有上下文规格
	_, err = b.br.PrepareRun(context.Background(), rc, PrepareRequest{})
	wantAgentCode(t, err, errcode.ErrAgentModelNA)
	b.reg.snap.Model.Capabilities = caps

	_, err = b.br.PrepareRun(context.Background(), platformCtx(920007, "all"), PrepareRequest{Message: "x", MaxSteps: -1})
	wantAgentCode(t, err, errcode.ErrInvalidParams)

	other := rc
	other.UserID = 2 // 别人的画布：带消息时要读画布目录，读不到就是画布不存在
	_, err = b.br.PrepareRun(context.Background(), other, PrepareRequest{Message: "x"})
	wantAgentCode(t, err, errcode.ErrCanvasNotFound)
}

func TestPrepareRun_DoesNotTouchRunStateOrEvents(t *testing.T) {
	b := newBridgeEnv(t)
	before := len(b.persistedTypes())
	prepare(t, b, platformCtx(920008, "all"), PrepareRequest{Message: "你好"})
	if len(b.persistedTypes()) != before {
		t.Error("准备不发事件")
	}
	if approvalCount(b.repo, 920008) != 0 {
		t.Error("也不创建任何记录")
	}
}

// mustCatalog 读和令牌模式相同的画布目录（选中的节点优先）。
func mustCatalog(t *testing.T, b *bridgeEnv, rc RunContext) *canvasgraph.Catalog {
	t.Helper()
	run := &model.AgentRun{ID: rc.RunID, UserID: rc.UserID, CanvasID: rc.CanvasID}
	cat, err := NewAgentCanvasService(b.repo, b.bc).Catalog(context.Background(), run, canvasgraph.CatalogOptions{Priority: rc.Selection, Selected: rc.Selection})
	if err != nil {
		t.Fatal(err)
	}
	return cat
}

// tool.start 事件要带界面名称和短动词；Agent 服务不再自己维护一张表，工具声明里就带着。
func TestToolDecls_CarryTheVerbAndLabelOfTheEvents(t *testing.T) {
	decls := AgentToolDecls("all", true)
	if len(decls) == 0 {
		t.Fatal("没有工具声明")
	}
	for _, d := range decls {
		label, verb := ToolLabel(d.Name)
		if d.Label != label || d.Verb != verb || verb == "" {
			t.Errorf("%s: 声明里的 label/verb = %q/%q，事件里是 %q/%q", d.Name, d.Label, d.Verb, label, verb)
		}
	}
	raw, _ := json.Marshal(decls[0])
	if !strings.Contains(string(raw), `"verb":"`) {
		t.Errorf("verb 要出现在 JSON 里: %s", raw)
	}
}
