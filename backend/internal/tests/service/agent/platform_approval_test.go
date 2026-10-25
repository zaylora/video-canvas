package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	. "video-canvas/internal/service/agent"
)

// 方案二里审批记录和状态流转归 Agent 服务，Go 只提供三个无状态的平台操作：
//   预估（propose）：工具参数 → 审批内容和预估积分，不落库；
//   解析（resolve）：用户的决定 → 实际批准了什么，纯逻辑；
//   执行（execute）：批准的内容 → 删除或创建生成任务，并写出给模型看的结果文字。

// newApprovalEnv 在生成环境上重新装配桥：带上上下文模式要用的依赖，并保留假的生成任务服务。
func newApprovalEnv(t *testing.T, mode string) *genEnv {
	t.Helper()
	e := newGenEnv(t, mode)
	e.br = NewAgentBridge(BridgeDeps{
		Repo: e.repo, Canvas: NewAgentCanvasService(e.repo, e.bc), Agent: e.svc, Billing: NewAgentBilling(e.bill),
		Registry: e.reg, Secrets: fakeSecrets{}, Streamer: e.str, Tasks: e.tasks, RunUsage: &fakeRunUsage{bill: e.bill},
	})
	return e
}

func propose(t *testing.T, e *genEnv, rc RunContext, kind string, args any) *ApprovalProposal {
	t.Helper()
	raw, _ := json.Marshal(args)
	p, err := e.br.ProposeApproval(context.Background(), rc, kind, raw)
	if err != nil {
		t.Fatalf("ProposeApproval(%s): %v", kind, err)
	}
	return p
}

func TestProposeApproval_GenerateQuotesWithoutCreatingAnything(t *testing.T) {
	e := newApprovalEnv(t, "all")
	rc := platformCtx(900201, "all")
	p := propose(t, e, rc, "generate", map[string]any{"items": []map[string]string{{"nodeId": e.imgID(t)}}, "reason": "出角色图"})
	if p.IsError || !p.Terminate {
		t.Fatalf("预估应成功，并要求本轮停下等用户决定: %+v", p)
	}
	var payload GeneratePayload
	if err := json.Unmarshal(p.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Reason != "出角色图" || len(payload.Items) != 1 || payload.Items[0].NodeID != e.imgID(t) || payload.Items[0].Price != 5 || payload.Items[0].ModelKey != "seedream" {
		t.Errorf("审批内容: %+v", payload)
	}
	if p.QuoteCredits != 5 || !strings.Contains(p.Summary, "预估 5 积分") || !strings.Contains(p.Content, "等待决定") {
		t.Errorf("预估积分、摘要、给模型看的文字: %+v", p)
	}
}

func TestProposeApproval_GenerateExplainsWhyItCannotBeQuoted(t *testing.T) {
	e := newApprovalEnv(t, "all")
	rc := platformCtx(900202, "all")
	// 节点不存在、重复、没有 items：都是给模型看的工具错误，说明怎么改
	for name, args := range map[string]any{
		"没有 items": map[string]any{"items": []any{}},
		"节点重复":     map[string]any{"items": []map[string]string{{"nodeId": e.imgID(t)}, {"nodeId": e.imgID(t)}}},
		"节点不存在":    map[string]any{"items": []map[string]string{{"nodeId": "n_不存在"}}},
	} {
		t.Run(name, func(t *testing.T) {
			p := propose(t, e, rc, "generate", args)
			if !p.IsError || p.Content == "" || p.Payload != nil {
				t.Errorf("应是工具错误，没有审批内容: %+v", p)
			}
		})
	}
}

func TestProposeApproval_ModeRulesStillApply(t *testing.T) {
	e := newApprovalEnv(t, "all")
	for _, kind := range []string{"generate", "delete"} {
		p := propose(t, e, platformCtx(900203, "prompt"), kind, map[string]any{})
		if !p.IsError || !strings.Contains(p.Content, "不能使用") {
			t.Errorf("提示词模式不能%s，平台规则不依赖调用方自觉: %+v", kind, p)
		}
	}
	// 提问所有模式都能用
	p := propose(t, e, platformCtx(900203, "prompt"), "ask", map[string]any{"question": "要横屏还是竖屏？", "kind": "choice", "options": []string{"横屏", "竖屏"}})
	if p.IsError {
		t.Errorf("提问不受模式限制: %+v", p)
	}
}

func TestProposeApproval_DeleteListsLabelsAndOutputs(t *testing.T) {
	e := newApprovalEnv(t, "all")
	p := propose(t, e, platformCtx(900204, "all"), "delete", map[string]any{"nodeIds": []string{e.imgID(t)}, "reason": "废稿"})
	if p.IsError || !p.Terminate || p.QuoteCredits != 0 {
		t.Fatalf("%+v", p)
	}
	var d DeletePayload
	if err := json.Unmarshal(p.Payload, &d); err != nil {
		t.Fatal(err)
	}
	if len(d.NodeIDs) != 1 || d.Labels[0] != "角色" || d.Outputs[0] || d.EdgeIDs == nil || d.Reason != "废稿" {
		t.Errorf("卡片要显示标题和是否含产物，列表不能是 null: %+v", d)
	}
	if p := propose(t, e, platformCtx(900204, "all"), "delete", map[string]any{"nodeIds": []string{"n_不存在"}}); !p.IsError {
		t.Errorf("节点不存在要报错，不能提交一个删不掉的审批: %+v", p)
	}
	if p := propose(t, e, platformCtx(900204, "all"), "delete", map[string]any{}); !p.IsError || !strings.Contains(p.Content, "没有指定") {
		t.Errorf("没有指定要删的东西: %+v", p)
	}
}

func TestProposeApproval_AskValidatesAndListsModels(t *testing.T) {
	e := newApprovalEnv(t, "all")
	rc := platformCtx(900205, "all")
	p := propose(t, e, rc, "ask", map[string]any{"question": "用哪个模型？", "kind": "model", "modelKind": "image"})
	if p.IsError {
		t.Fatalf("%+v", p)
	}
	var a AskPayload
	if err := json.Unmarshal(p.Payload, &a); err != nil {
		t.Fatal(err)
	}
	if a.Kind != "model" || len(a.Models) != 1 || a.Models[0].Key != "seedream" {
		t.Errorf("选模型题要列出已发布的图片模型: %+v", a)
	}
	for name, args := range map[string]any{
		"问题为空":     map[string]any{"question": " ", "kind": "choice", "options": []string{"a", "b"}},
		"选项太少":     map[string]any{"question": "q", "kind": "choice", "options": []string{"a"}},
		"选项太多":     map[string]any{"question": "q", "kind": "choice", "options": []string{"a", "b", "c", "d", "e"}},
		"kind 不认识": map[string]any{"question": "q", "kind": "free"},
		"模型种类不对":   map[string]any{"question": "q", "kind": "model", "modelKind": "audio"},
	} {
		if p := propose(t, e, rc, "ask", args); !p.IsError || p.Content == "" {
			t.Errorf("%s应是工具错误: %+v", name, p)
		}
	}
}

func TestProposeApproval_RejectsBadInput(t *testing.T) {
	e := newApprovalEnv(t, "all")
	_, err := e.br.ProposeApproval(context.Background(), platformCtx(900206, "all"), "nuke", []byte(`{}`))
	wantAgentCode(t, err, errcode.ErrInvalidParams)
	_, err = e.br.ProposeApproval(context.Background(), RunContext{}, "generate", []byte(`{}`))
	wantAgentCode(t, err, errcode.ErrInvalidParams)
	// 别人的画布：节点读不到，按「画布不存在」处理
	rc := platformCtx(900206, "all")
	rc.UserID = 2
	p := propose(t, e, rc, "delete", map[string]any{"nodeIds": []string{"n_script"}})
	if !p.IsError || !strings.Contains(p.Content, "画布不存在") {
		t.Errorf("别人的画布: %+v", p)
	}
}

var _ = model.ApprovalGenerate

// ---- 解析：用户的决定 → 实际批准了什么 ----
//
// 解析是纯逻辑。Agent 服务管审批的状态流转，规则（逐项勾选、张数只能少不能多、按批准的内容重新算价、查本轮预算）
// 只在 Go 里有一份。下面的用例表把每条规则的结果冻结在 testdata/resolve-approval.golden.json 里（改规则要有意地更新它：
// UPDATE_GOLDEN=1 go test -run TestResolveApproval_Rules），报错码、决定内容、预估积分、要执行的条目、要删除的节点逐项对照。

type resolveCase struct {
	name          string
	kind          string
	payload       any
	budget, spent int
	req           model.DecideAgentApprovalReq
}

type resolved struct {
	code     int    // 0 表示没有出错
	decision string // 写进审批的决定
	quote    int
	gen      []GenerateItem
	nodeIDs  []string
}

func errCode(err error) int {
	var ec *errcode.Error
	if errors.As(err, &ec) {
		return ec.Code
	}
	if err != nil {
		return -1
	}
	return 0
}

// viaResolve 不建任何记录，只把审批内容、决定和本轮预算交给平台，读出解析的产物。
func viaResolve(t *testing.T, c resolveCase) resolved {
	t.Helper()
	b := newBridgeEnv(t)
	rc := platformCtx(900301, "all")
	rc.BudgetCredits, rc.SpentExtra = c.budget, c.spent
	payload, _ := json.Marshal(c.payload)
	out, err := b.br.ResolveApproval(context.Background(), rc, c.kind, payload, c.req)
	r := resolved{code: errCode(err)}
	if err != nil {
		return r
	}
	d, _ := json.Marshal(out.Decision)
	r.decision, r.quote, r.gen, r.nodeIDs = string(d), out.QuoteCredits, out.Items, out.NodeIDs
	return r
}

func TestResolveApproval_Rules(t *testing.T) {
	gen := genPayload() // n1 林夏 ×2 @8、n2 老周 ×1 @8
	del := DeletePayload{Reason: "废稿", NodeIDs: []string{"n_script", "n_b"}, Labels: []string{"剧本", "备用"}, Outputs: []bool{false, true}, EdgeIDs: []string{"e1"}}
	ask := AskPayload{Question: "画幅？", Kind: "choice", Options: []string{"16:9", "9:16"}}
	approve := model.DecideAgentApprovalReq{Decision: "approve"}
	cases := []resolveCase{
		{name: "生成：整体批准", kind: model.ApprovalGenerate, payload: gen, budget: 50, req: approve},
		{name: "生成：不限预算", kind: model.ApprovalGenerate, payload: gen, budget: 0, req: approve},
		{name: "生成：部分批准且改张数", kind: model.ApprovalGenerate, payload: gen, budget: 50, req: model.DecideAgentApprovalReq{Decision: "approve", Items: []model.ApprovalItemDecision{{Index: 0, Approve: true, Count: 1}, {Index: 1, Approve: false}}}},
		{name: "生成：张数只能少不能多", kind: model.ApprovalGenerate, payload: gen, budget: 50, req: model.DecideAgentApprovalReq{Decision: "approve", Items: []model.ApprovalItemDecision{{Index: 1, Approve: true, Count: 4}}}},
		{name: "生成：数量 0 表示按申请的", kind: model.ApprovalGenerate, payload: gen, budget: 50, req: model.DecideAgentApprovalReq{Decision: "approve", Items: []model.ApprovalItemDecision{{Index: 0, Approve: true}}}},
		{name: "生成：一项都没批准", kind: model.ApprovalGenerate, payload: gen, budget: 50, req: model.DecideAgentApprovalReq{Decision: "approve", Items: []model.ApprovalItemDecision{{Index: 0, Approve: false}, {Index: 1, Approve: false}}}},
		{name: "生成：序号越界", kind: model.ApprovalGenerate, payload: gen, budget: 50, req: model.DecideAgentApprovalReq{Decision: "approve", Items: []model.ApprovalItemDecision{{Index: 5, Approve: true}}}},
		{name: "生成：序号重复", kind: model.ApprovalGenerate, payload: gen, budget: 50, req: model.DecideAgentApprovalReq{Decision: "approve", Items: []model.ApprovalItemDecision{{Index: 0, Approve: true}, {Index: 0, Approve: true}}}},
		{name: "生成：超出本轮预算", kind: model.ApprovalGenerate, payload: gen, budget: 20, req: approve},
		{name: "生成：已花的也算进预算", kind: model.ApprovalGenerate, payload: gen, budget: 30, spent: 10, req: approve},
		{name: "生成：追加预算后够了", kind: model.ApprovalGenerate, payload: gen, budget: 20, req: model.DecideAgentApprovalReq{Decision: "approve", AddBudget: 10}},
		{name: "生成：拒绝", kind: model.ApprovalGenerate, payload: gen, budget: 50, req: model.DecideAgentApprovalReq{Decision: "reject"}},
		{name: "生成：拒绝并说明", kind: model.ApprovalGenerate, payload: gen, budget: 50, req: model.DecideAgentApprovalReq{Decision: "reject", Answer: "  太贵了  "}},
		{name: "删除：整体批准", kind: model.ApprovalDelete, payload: del, req: approve},
		{name: "删除：取消勾选一个节点", kind: model.ApprovalDelete, payload: del, req: model.DecideAgentApprovalReq{Decision: "approve", Items: []model.ApprovalItemDecision{{Index: 0, Approve: true}, {Index: 1, Approve: false}}}},
		{name: "删除：节点都取消但还有连线", kind: model.ApprovalDelete, payload: del, req: model.DecideAgentApprovalReq{Decision: "approve", Items: []model.ApprovalItemDecision{{Index: 0, Approve: false}, {Index: 1, Approve: false}}}},
		{name: "删除：什么都没剩", kind: model.ApprovalDelete, payload: DeletePayload{NodeIDs: []string{"n_script"}, Labels: []string{"剧本"}, Outputs: []bool{false}}, req: model.DecideAgentApprovalReq{Decision: "approve", Items: []model.ApprovalItemDecision{{Index: 0, Approve: false}}}},
		{name: "删除：拒绝", kind: model.ApprovalDelete, payload: del, req: model.DecideAgentApprovalReq{Decision: "reject"}},
		{name: "提问：回答", kind: model.ApprovalAsk, payload: ask, req: model.DecideAgentApprovalReq{Decision: "approve", Answer: "16:9"}},
		{name: "提问：回答不能为空", kind: model.ApprovalAsk, payload: ask, req: approve},
		{name: "提问：不回答", kind: model.ApprovalAsk, payload: ask, req: model.DecideAgentApprovalReq{Decision: "reject"}},
	}
	got := map[string]resolved{}
	for _, c := range cases {
		got[c.name] = viaResolve(t, c)
	}
	snap := map[string]map[string]any{}
	for name, r := range got {
		snap[name] = map[string]any{"code": r.code, "decision": r.decision, "quote": r.quote, "gen": r.gen, "node_ids": r.nodeIDs}
	}
	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	const path = "testdata/resolve-approval.golden.json"
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(want)) != strings.TrimSpace(string(b)) {
		t.Errorf("审批决定的解析结果和冻结的不一致（有意改规则就更新 %s）:\n现在:\n%s\n冻结:\n%s", path, b, want)
	}
}

func TestResolveApproval_StatusAndExecutionPlan(t *testing.T) {
	b := newBridgeEnv(t)
	rc := platformCtx(900302, "all")
	gen, _ := json.Marshal(genPayload())
	resolve := func(kind string, payload []byte, req model.DecideAgentApprovalReq) *ApprovalOutcome {
		t.Helper()
		out, err := b.br.ResolveApproval(context.Background(), rc, kind, payload, req)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	if o := resolve(model.ApprovalGenerate, gen, model.DecideAgentApprovalReq{Decision: "approve"}); o.Status != model.ApprovalApproved || o.QuoteCredits != 24 || len(o.Items) != 2 {
		t.Errorf("整体批准: %+v", o)
	}
	if o := resolve(model.ApprovalGenerate, gen, model.DecideAgentApprovalReq{Decision: "approve", Items: []model.ApprovalItemDecision{{Index: 0, Approve: true, Count: 1}}}); o.Status != model.ApprovalPartial || o.QuoteCredits != 8 {
		t.Errorf("部分批准要标成 partial: %+v", o)
	}
	if o := resolve(model.ApprovalGenerate, gen, model.DecideAgentApprovalReq{Decision: "reject", Answer: "太贵"}); o.Status != model.ApprovalRejected || len(o.Items) != 0 || o.QuoteCredits != 0 {
		t.Errorf("拒绝: %+v", o)
	}
	del, _ := json.Marshal(DeletePayload{NodeIDs: []string{"a", "b"}, EdgeIDs: []string{"e"}})
	if o := resolve(model.ApprovalDelete, del, model.DecideAgentApprovalReq{Decision: "approve", Items: []model.ApprovalItemDecision{{Index: 0, Approve: true}}}); o.Status != model.ApprovalPartial || len(o.NodeIDs) != 1 || o.NodeIDs[0] != "a" || len(o.EdgeIDs) != 1 {
		t.Errorf("部分批准删除，连线跟着批准: %+v", o)
	}
}

func TestResolveApproval_UsesModelSpendFromCallRecords(t *testing.T) {
	// 本轮已花 = 调用方声明的 spent_extra + Go 按调用记录累加的模型调用实扣：模型调用花掉的积分也要占预算
	b := newBridgeEnv(t)
	rc := platformCtx(900303, "all")
	rc.BudgetCredits = 30
	if _, _, err := proxyPlatform(context.Background(), b, rc); err != nil { // 模型调用花 5
		t.Fatal(err)
	}
	gen, _ := json.Marshal(genPayload()) // 批准要 24：5 + 24 = 29 ≤ 30 够
	if _, err := b.br.ResolveApproval(context.Background(), rc, model.ApprovalGenerate, gen, model.DecideAgentApprovalReq{Decision: "approve"}); err != nil {
		t.Fatalf("5 + 24 = 29 不超过预算 30: %v", err)
	}
	rc.SpentExtra = 2 // 再有 2 积分花在别处：5 + 2 + 24 = 31 > 30
	_, err := b.br.ResolveApproval(context.Background(), rc, model.ApprovalGenerate, gen, model.DecideAgentApprovalReq{Decision: "approve"})
	wantAgentCode(t, err, errcode.ErrAgentOverBudget)
}

func TestResolveApproval_RejectsBadInput(t *testing.T) {
	b := newBridgeEnv(t)
	rc := platformCtx(900304, "all")
	for name, c := range map[string]struct {
		kind, payload string
		rc            RunContext
	}{
		"审批种类不认识":     {"nuke", `{}`, rc},
		"审批内容不是 JSON": {model.ApprovalGenerate, `不是 JSON`, rc},
		"上下文不合法":      {model.ApprovalGenerate, `{}`, RunContext{}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := b.br.ResolveApproval(context.Background(), c.rc, c.kind, []byte(c.payload), model.DecideAgentApprovalReq{Decision: "approve"})
			if err == nil {
				t.Fatal("应返回错误")
			}
		})
	}
}

// ---- 执行 ----

const execApprovalID = uint64(1<<60 + 7) // 雪花大小的 id：审批 id 会进幂等键和任务键

// newExecEnv 在审批环境上换成真的生成执行器（任务服务仍是假的，会记下幂等键）：执行接口走的是 AgentService 里那一个。
func newExecEnv(t *testing.T) *genEnv {
	t.Helper()
	e := newApprovalEnv(t, "all")
	canvas := NewAgentCanvasService(e.repo, e.bc)
	svc := NewAgentService(AgentDeps{
		Repo: e.repo, Canvas: canvas, Models: fakeAgentModels{list: defaultAgentModels},
		Generator: NewAgentGenerator(e.tasks, canvas, e.reg), Now: func() time.Time { return *e.now },
	})
	e.br = NewAgentBridge(BridgeDeps{
		Repo: e.repo, Canvas: canvas, Agent: svc, Billing: NewAgentBilling(e.bill),
		Registry: e.reg, Secrets: fakeSecrets{}, Streamer: e.str, Tasks: e.tasks, RunUsage: &fakeRunUsage{bill: e.bill},
	})
	return e
}

func execApproval(t *testing.T, e *genEnv, rc RunContext, kind string, toolCallID string, out ApprovalOutcome) *ApprovalExecution {
	t.Helper()
	res, err := e.br.ExecuteApproval(context.Background(), rc, kind, execApprovalID, toolCallID, out)
	if err != nil {
		t.Fatalf("ExecuteApproval(%s): %v", kind, err)
	}
	return res
}

func countKind(t *testing.T, e *genEnv, runID uint64, kind string) int {
	t.Helper()
	muts, _ := e.repo.ListMutations(context.Background(), runID)
	n := 0
	for _, m := range muts {
		if m.Kind == kind {
			n++
		}
	}
	return n
}

func TestExecuteApproval_DeleteRemovesNodesAndReplaysTheSameResult(t *testing.T) {
	e := newExecEnv(t)
	rc := platformCtx(900401, "all")
	id := e.imgID(t)
	out := ApprovalOutcome{Status: model.ApprovalApproved, NodeIDs: []string{id}, EdgeIDs: []string{}}

	first := execApproval(t, e, rc, model.ApprovalDelete, "call_del", out)
	if first.Status != "executed" || !strings.Contains(first.OutcomeText, "已删除 1 个节点") {
		t.Fatalf("删除应成功，并写出给模型看的结果: %+v", first)
	}
	for _, n := range mustParseGraph(t, e.repo).Nodes {
		if n.ID() == id {
			t.Fatal("节点应已删除")
		}
	}
	// 崩溃后 Agent 服务重试同一个审批：节点已经没了，不能因此变成「执行失败」，要原样返回第一次的结果，也不再写第二条改动
	again := execApproval(t, e, rc, model.ApprovalDelete, "call_del", out)
	if again.Status != "executed" || string(again.Result) != string(first.Result) || again.OutcomeText != first.OutcomeText {
		t.Errorf("重放要和第一次一样:\n%+v\n%+v", first, again)
	}
	if n := countKind(t, e, rc.RunID, model.MutationDelete); n != 1 {
		t.Errorf("只能删一次: %d 条删除改动", n)
	}
	// 另一个审批（不同 id）是另一次真实的执行，不受这个键影响
	other, err := e.br.ExecuteApproval(context.Background(), rc, model.ApprovalDelete, execApprovalID+1, "call_del2", ApprovalOutcome{Status: model.ApprovalApproved, NodeIDs: []string{"n_script"}})
	if err != nil || other.Status != "executed" {
		t.Fatalf("不同审批各自执行: %+v %v", other, err)
	}
}

func TestExecuteApproval_PartialDeleteTextSaysSo(t *testing.T) {
	e := newExecEnv(t)
	res := execApproval(t, e, platformCtx(900402, "all"), model.ApprovalDelete, "c", ApprovalOutcome{Status: model.ApprovalPartial, NodeIDs: []string{e.imgID(t)}})
	if res.Status != "executed" || !strings.Contains(res.OutcomeText, "只批准了部分删除") {
		t.Errorf("%+v", res)
	}
}

func TestExecuteApproval_DeleteFailureIsReportedNotThrown(t *testing.T) {
	e := newExecEnv(t)
	res := execApproval(t, e, platformCtx(900403, "all"), model.ApprovalDelete, "c", ApprovalOutcome{Status: model.ApprovalApproved, NodeIDs: []string{"n_不存在"}})
	if res.Status != "failed" || !strings.Contains(res.OutcomeText, "执行失败") || !strings.Contains(res.OutcomeText, "画布没有改动") {
		t.Errorf("业务失败是审批的「失败」结果，要让模型知道: %+v", res)
	}
	var body struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(res.Result, &body) != nil || body.Error == "" {
		t.Errorf("失败原因要写进结果: %s", res.Result)
	}
}

func TestExecuteApproval_GenerateSubmitsTasksWithApprovalKeysAndReplaysWithoutResubmitting(t *testing.T) {
	e := newExecEnv(t)
	rc := platformCtx(900404, "all")
	out := ApprovalOutcome{
		Status: model.ApprovalApproved, QuoteCredits: 5,
		Items: []GenerateItem{{NodeID: e.imgID(t), Label: "角色", ModelKey: "seedream", Count: 1, Price: 5}},
	}
	first := execApproval(t, e, rc, model.ApprovalGenerate, "call_gen", out)
	if first.Status != "executed" || !strings.Contains(string(first.Result), `"task_id":"101"`) {
		t.Fatalf("%+v", first)
	}
	if want := fmt.Sprintf("agent-%d-0", execApprovalID); len(e.tasks.keys) != 1 || e.tasks.keys[0] != want {
		t.Errorf("任务幂等键应是 %s: %v", want, e.tasks.keys)
	}
	if !strings.Contains(first.OutcomeText, "用户批准了生成：角色×1") || !strings.Contains(first.OutcomeText, "预估 5 积分") || !strings.Contains(first.OutcomeText, `"task_id":"101"`) {
		t.Errorf("给模型看的文字: %s", first.OutcomeText)
	}
	if d := node(t, e.repo, e.imgID(t)).Data(); d["taskId"] != "101" {
		t.Errorf("任务应绑定到节点: %v", d)
	}
	// 绑定之后节点已经在生成，重放如果再走一遍提交会得到「节点正在生成」而误报失败；要原样返回第一次的结果，也不再向任务服务提交
	again := execApproval(t, e, rc, model.ApprovalGenerate, "call_gen", out)
	if again.Status != "executed" || string(again.Result) != string(first.Result) || again.OutcomeText != first.OutcomeText {
		t.Errorf("重放要和第一次一样:\n%+v\n%+v", first, again)
	}
	if len(e.tasks.keys) != 1 || countKind(t, e, rc.RunID, model.MutationBind) != 1 {
		t.Errorf("不能重复提交、重复绑定: keys=%v binds=%d", e.tasks.keys, countKind(t, e, rc.RunID, model.MutationBind))
	}
}

func TestExecuteApproval_GenerateFailureSaysNothingWasCharged(t *testing.T) {
	e := newExecEnv(t)
	e.tasks.errAt = map[int]error{0: errcode.ErrInsufficientCredits}
	out := ApprovalOutcome{Status: model.ApprovalApproved, QuoteCredits: 5, Items: []GenerateItem{{NodeID: e.imgID(t), Label: "角色", ModelKey: "seedream", Count: 1, Price: 5}}}
	res := execApproval(t, e, platformCtx(900405, "all"), model.ApprovalGenerate, "c", out)
	if res.Status != "failed" || !strings.Contains(res.OutcomeText, "任务提交失败") || !strings.Contains(res.OutcomeText, "没有扣积分") {
		t.Errorf("%+v", res)
	}
}

func TestExecuteApproval_RejectedAndAskRunNothing(t *testing.T) {
	e := newExecEnv(t)
	rc := platformCtx(900406, "all")
	before := len(e.tasks.keys)

	rej := execApproval(t, e, rc, model.ApprovalGenerate, "c1", ApprovalOutcome{Status: model.ApprovalRejected, Decision: map[string]any{"answer": "太贵"}})
	if rej.Status != "none" || len(rej.Result) != 0 || !strings.Contains(rej.OutcomeText, "用户拒绝了这次生成") || !strings.Contains(rej.OutcomeText, "用户的说明：太贵") {
		t.Errorf("拒绝生成: %+v", rej)
	}
	delRej := execApproval(t, e, rc, model.ApprovalDelete, "c2", ApprovalOutcome{Status: model.ApprovalRejected, NodeIDs: []string{e.imgID(t)}})
	if delRej.Status != "none" || !strings.Contains(delRej.OutcomeText, "没有任何节点或连线被删除") || countKind(t, e, rc.RunID, model.MutationDelete) != 0 {
		t.Errorf("拒绝删除不能动画布: %+v", delRej)
	}
	ask := execApproval(t, e, rc, model.ApprovalAsk, "c3", ApprovalOutcome{Status: model.ApprovalApproved, Decision: map[string]any{"answer": "第二个"}})
	if ask.Status != "none" || ask.OutcomeText != "用户回答：第二个" {
		t.Errorf("回答: %+v", ask)
	}
	if len(e.tasks.keys) != before {
		t.Error("拒绝和提问都不能创建任务")
	}
}

func TestExecuteApproval_OtherUsersCanvasIsUntouched(t *testing.T) {
	e := newExecEnv(t)
	rc := platformCtx(900407, "all")
	rc.UserID = 2
	res := execApproval(t, e, rc, model.ApprovalDelete, "c", ApprovalOutcome{Status: model.ApprovalApproved, NodeIDs: []string{e.imgID(t)}})
	if res.Status != "failed" || len(mustParseGraph(t, e.repo).Nodes) != 2 {
		t.Errorf("不能删别人画布上的节点: %+v", res)
	}
}

func TestExecuteApproval_RejectsBadInput(t *testing.T) {
	e := newExecEnv(t)
	rc := platformCtx(900408, "all")
	ok := ApprovalOutcome{Status: model.ApprovalApproved, NodeIDs: []string{"n_script"}}
	for name, c := range map[string]struct {
		rc   RunContext
		kind string
		id   uint64
		out  ApprovalOutcome
	}{
		"上下文不合法":    {RunContext{}, model.ApprovalDelete, execApprovalID, ok},
		"审批种类不认识":   {rc, "nuke", execApprovalID, ok},
		"审批 id 为 0": {rc, model.ApprovalDelete, 0, ok},
		"状态不认识":     {rc, model.ApprovalDelete, execApprovalID, ApprovalOutcome{Status: "hacked"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := e.br.ExecuteApproval(context.Background(), c.rc, c.kind, c.id, "c", c.out)
			wantAgentCode(t, err, errcode.ErrInvalidParams)
		})
	}
	if len(mustParseGraph(t, e.repo).Nodes) != 2 {
		t.Error("参数不对不能有任何副作用")
	}
}

func TestExecuteApproval_CanceledRequestIsAnErrorNotAFailedApproval(t *testing.T) {
	e := newExecEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	e.tasks.onCreate = cancel
	e.tasks.errAt = map[int]error{0: context.Canceled}
	out := ApprovalOutcome{Status: model.ApprovalApproved, QuoteCredits: 5, Items: []GenerateItem{{NodeID: e.imgID(t), Label: "角色", ModelKey: "seedream", Count: 1, Price: 5}}}
	// 提交过程中请求被取消是「不知道有没有执行」：要让调用方重试（执行是幂等的），不能记成审批失败、告诉模型「没有扣积分」
	_, err := e.br.ExecuteApproval(ctx, platformCtx(900409, "all"), model.ApprovalGenerate, execApprovalID, "c", out)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("应返回取消错误: %v", err)
	}
}

func genPayload() GeneratePayload {
	return GeneratePayload{Reason: "先锁定角色", Items: []GenerateItem{
		{NodeID: "n1", Label: "林夏", ModelKey: "seedream", ModelName: "Seedream", Price: 8, Count: 2},
		{NodeID: "n2", Label: "老周", ModelKey: "seedream", ModelName: "Seedream", Price: 8, Count: 1},
	}}
}
