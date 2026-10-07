package repository_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"video-canvas/internal/canvasgraph"
	"video-canvas/internal/handler"
	"video-canvas/internal/llmgateway"
	"video-canvas/internal/model"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	. "video-canvas/internal/repository"
	"video-canvas/internal/router"
	"video-canvas/internal/service"
)

// 端到端：真正的 Node 进程 + 真正的 Go 桥 HTTP 服务 + 真实 PostgreSQL + 假的 LLM 上游。
// 需要 Node（>= 22.19）和已安装依赖的 backend/agent-runtime，以及 TEST_DATABASE_DSN；缺哪个就跳过哪个。

// ---- 假的 LLM 上游 ----

type llmTurn struct {
	text  string
	tools []llmTool
	slow  time.Duration // 每个文本片段之间的停顿，用来制造「正在流式输出」的窗口
	usage [2]int
}
type llmTool struct {
	id, name string
	args     any
}

type fakeLLM struct {
	srv   *httptest.Server
	mu    sync.Mutex
	turns []llmTurn
	n     int
	reqs  []map[string]any
}

func newFakeLLM(t *testing.T, turns ...llmTurn) *fakeLLM {
	l := &fakeLLM{turns: turns}
	l.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		l.mu.Lock()
		l.reqs = append(l.reqs, body)
		turn := llmTurn{text: "（脚本用完）"}
		if l.n < len(l.turns) {
			turn = l.turns[l.n]
		}
		l.n++
		l.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		send := func(v any) { b, _ := json.Marshal(v); fmt.Fprintf(w, "data: %s\n\n", b); fl.Flush() }
		delta := func(d map[string]any, fin any) map[string]any {
			return map[string]any{"choices": []any{map[string]any{"index": 0, "delta": d, "finish_reason": fin}}}
		}
		send(delta(map[string]any{"role": "assistant", "content": ""}, nil))
		for _, ch := range strings.Split(turn.text, "") {
			if ch == "" {
				continue
			}
			send(delta(map[string]any{"content": ch}, nil))
			if turn.slow > 0 {
				select {
				case <-time.After(turn.slow):
				case <-r.Context().Done():
					return
				}
			}
		}
		for i, tc := range turn.tools {
			a, _ := json.Marshal(tc.args)
			send(delta(map[string]any{"tool_calls": []any{map[string]any{"index": i, "id": tc.id, "type": "function", "function": map[string]any{"name": tc.name, "arguments": string(a)}}}}, nil))
		}
		fin := "stop"
		if len(turn.tools) > 0 {
			fin = "tool_calls"
		}
		send(delta(map[string]any{}, fin))
		in, out := turn.usage[0], turn.usage[1]
		if in == 0 {
			in, out = 1_000_000, 100_000 // 默认 3 + 1.5 → 5 积分
		}
		send(map[string]any{"choices": []any{}, "usage": map[string]any{"prompt_tokens": in, "completion_tokens": out}})
		fmt.Fprint(w, "data: [DONE]\n\n")
		fl.Flush()
	}))
	t.Cleanup(l.srv.Close)
	return l
}

func (l *fakeLLM) requests() []map[string]any {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]map[string]any(nil), l.reqs...)
}

// ---- 假的注册表与密钥 ----

type e2eRegistry struct{ base string }

func (r e2eRegistry) Snapshot(context.Context, string) (*provider.Snapshot, error) {
	return &provider.Snapshot{
		Model: provider.ModelSnapshot{Key: "claude", Kind: "agent", Label: "Claude", UpstreamModel: "claude-up",
			Pricing:      modelcfg.Pricing{Billing: modelcfg.BillingToken, Token: &modelcfg.TokenPrice{In: 3, Out: 15}},
			Capabilities: modelcfg.Capabilities{Context: &modelcfg.ContextSpec{Window: 200000, Output: 8192}, Vision: true}},
		Channel: provider.ChannelSnapshot{Key: "ch1", BaseURL: r.base, TrustedInternal: true},
	}, nil
}
func (r e2eRegistry) ListModels(context.Context, string) ([]provider.ModelInfo, error) {
	return []provider.ModelInfo{{Key: "claude", Kind: "agent", Label: "Claude"}}, nil
}

type e2eModels struct{}

func (e2eModels) List(context.Context) ([]model.AgentModelView, error) {
	return []model.AgentModelView{{Key: "claude", Name: "Claude", Vision: true}}, nil
}

type e2eSecrets struct{}

func (e2eSecrets) Get(context.Context, string) (string, error) { return "sk-e2e", nil }

// ---- 环境 ----

type e2eEnv struct {
	db     *gorm.DB
	repo   *AgentRepository
	svc    *service.AgentService
	rt     *service.ProcessRuntime
	bridge *service.AgentBridge
	llm    *fakeLLM
	sess   *model.AgentSession
	canvas *model.CanvasProject
}

func newE2E(t *testing.T, turns ...llmTurn) *e2eEnv {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("没有 node，跳过端到端测试")
	}
	dir, _ := filepath.Abs("../../../agent-runtime")
	if _, err := os.Stat(filepath.Join(dir, "node_modules")); err != nil {
		t.Skip("agent-runtime 没有安装依赖（npm ci），跳过端到端测试")
	}
	db := isolatedDB(t, &model.CanvasProject{}, &model.AgentSession{}, &model.AgentRun{}, &model.AgentEvent{}, &model.AgentMutation{},
		&model.AgentApproval{}, &model.AgentModelCall{}, &model.UserCredit{}, &model.CreditLedger{}, &model.GenerationTask{})
	repo := NewAgentRepository(db)
	llm := newFakeLLM(t, turns...)

	canvas := &model.CanvasProject{UserID: 1, Title: "画布", Revision: 1, PayloadJSON: []byte(
		`{"nodes":[{"id":"n_script","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"script","label":"剧本","prompt":"雨夜便利店"}},` +
			`{"id":"n_old","type":"canvas","position":{"x":0,"y":300},"data":{"kind":"image","label":"废稿"}}],"edges":[]}`)}
	if err := db.Create(canvas).Error; err != nil {
		t.Fatal(err)
	}
	openAccount(t, db, 1, 100, 0)
	sess := newSession(t, repo, 1, canvas.ID)

	canvasSvc := service.NewAgentCanvasService(repo, nil)
	svc := service.NewAgentService(service.AgentDeps{Repo: repo, Canvas: canvasSvc, Models: e2eModels{}, Runtime: service.NoAgentRuntime{}})
	bridge := service.NewAgentBridge(service.BridgeDeps{
		Repo: repo, Canvas: canvasSvc, Agent: svc, Billing: service.NewAgentBilling(repo),
		Registry: e2eRegistry{base: llm.srv.URL}, Secrets: e2eSecrets{}, Streamer: llmgateway.New(llmgateway.Options{IdleTimeout: 10 * time.Second}),
	})
	bridgeSrv := httptest.NewServer(router.NewBridge("test", handler.NewAgentBridgeHandler(bridge)))
	t.Cleanup(bridgeSrv.Close)
	rt := service.NewProcessRuntime(service.RuntimeDeps{
		Repo: repo, Bridge: bridge, Canvas: canvasSvc, Agent: svc, Registry: e2eRegistry{base: llm.srv.URL}, Launcher: service.ExecLauncher{},
		Config: service.RuntimeConfig{NodePath: node, ScriptPath: filepath.Join(dir, "src/main.mjs"), BridgeURL: bridgeSrv.URL, WorkRoot: t.TempDir(), CancelGrace: 3 * time.Second},
	})
	svc.SetRuntime(rt)
	t.Cleanup(rt.Shutdown)
	return &e2eEnv{db: db, repo: repo, svc: svc, rt: rt, bridge: bridge, llm: llm, sess: sess, canvas: canvas}
}

func (e *e2eEnv) waitStatus(t *testing.T, runID uint64, want ...string) *model.AgentRun {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var run *model.AgentRun
	for time.Now().Before(deadline) {
		run, _ = e.repo.GetRun(context.Background(), 1, runID)
		if run != nil {
			for _, w := range want {
				if run.Status == w {
					return run
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	status := "?"
	if run != nil {
		status = run.Status
	}
	t.Fatalf("等待运行进入 %v 超时，当前 %s（错误：%v）", want, status, run)
	return nil
}

func (e *e2eEnv) start(t *testing.T, msg, mode string) uint64 {
	t.Helper()
	v, err := e.svc.StartRun(context.Background(), 1, e.sess.ID, &model.StartAgentRunReq{Message: msg, Mode: mode})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	return uint64(v.ID)
}

func (e *e2eEnv) graph(t *testing.T) *canvasgraph.Graph {
	t.Helper()
	var c model.CanvasProject
	if err := e.db.First(&c, e.canvas.ID).Error; err != nil {
		t.Fatal(err)
	}
	g, err := canvasgraph.Parse(c.PayloadJSON)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func (e *e2eEnv) eventTypes(t *testing.T) []string {
	t.Helper()
	evs, _ := e.repo.ListEvents(context.Background(), e.sess.ID, 0, 500)
	out := make([]string, len(evs))
	for i, ev := range evs {
		out[i] = ev.Type
	}
	return out
}

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func lastRequestText(l *fakeLLM) string {
	reqs := l.requests()
	b, _ := json.Marshal(reqs[len(reqs)-1]["messages"])
	return string(b)
}

// 场景 1：用户说一句话，Agent 读目录、改画布、说完话；全程计费、记事件、存历史。
func TestE2E_BuildsCanvasAndBills(t *testing.T) {
	e := newE2E(t,
		llmTurn{tools: []llmTool{{id: "call_1", name: "canvas_apply_ops", args: map[string]any{"ops": []any{
			map[string]any{"op": "create_group", "tempId": "g", "label": "镜头 01"},
			map[string]any{"op": "create_node", "tempId": "a", "kind": "image", "label": "关键帧", "prompt": "雨夜便利店门口", "parentGroup": "g"},
			map[string]any{"op": "connect", "source": "n_script", "target": "a"},
		}}}}},
		llmTurn{text: "搭好了镜头 01。"},
	)
	runID := e.start(t, "给剧本建一个镜头组", "all")
	run := e.waitStatus(t, runID, model.RunSucceeded, model.RunFailed, model.RunInterrupted)
	if run.Status != model.RunSucceeded {
		t.Fatalf("运行应成功: %+v", run)
	}

	g := e.graph(t)
	if len(g.Nodes) != 4 || len(g.Edges) != 1 {
		t.Fatalf("画布应新增 1 个组、1 个节点和 1 条连线（共 4 个节点）: nodes=%d edges=%d", len(g.Nodes), len(g.Edges))
	}

	types := e.eventTypes(t)
	for _, want := range []string{"message.user", "tool.start", "tool.end", "message.done", "run.status"} {
		if !has(types, want) {
			t.Errorf("缺事件 %s: %v", want, types)
		}
	}

	var calls []model.AgentModelCall
	e.db.Order("id").Find(&calls)
	if len(calls) != 2 || calls[0].Status != model.ModelCallSettled || calls[0].Charged != 5 {
		t.Fatalf("两次模型调用都应结算，每次 5 积分: %+v", calls)
	}
	if a := account(t, e.db, 1); a.Balance != 90 {
		t.Errorf("余额应 100→90: %d", a.Balance)
	}
	rec, _ := NewGenerationTaskRepository(e.db).Reconcile(context.Background(), 1)
	if int64(rec.Balance) != rec.InitialSum+rec.AdminAdjustSum-rec.SettleSum || int64(rec.Frozen) != rec.FreezeSum-rec.SettleSum-rec.RefundSum {
		t.Errorf("对账等式应成立: %+v", rec)
	}

	if run.SpentCredits != 10 || run.Steps != 1 {
		t.Errorf("本轮已花 10、工具步数 1: spent=%d steps=%d", run.SpentCredits, run.Steps)
	}
	sess, _ := e.repo.GetSession(context.Background(), 1, e.sess.ID)
	if !strings.Contains(sess.SessionJSONL, "搭好了镜头 01") || strings.Contains(sess.SessionJSONL, `"role":"system"`) {
		t.Errorf("应保存对话历史（不含系统提示）: %.200s", sess.SessionJSONL)
	}
	if e.bridge.TokenCount() != 0 {
		t.Errorf("运行结束后令牌应全部收回: %d", e.bridge.TokenCount())
	}

	first := e.llm.requests()[0]
	if first["model"] != "claude-up" || first["stream"] != true {
		t.Errorf("上游收到的请求: model=%v stream=%v", first["model"], first["stream"])
	}
	if _, has := first["store"]; has {
		t.Error("不能带 store")
	}
	firstMsgs := fmt.Sprint(first["messages"]) // 解码后的内容；用 json.Marshal 会把 < 转义成 \u003c
	for _, must := range []string{"画布 Agent", "<画布目录>", "n_script", "给剧本建一个镜头组"} {
		if !strings.Contains(firstMsgs, must) {
			t.Errorf("第一次请求应包含 %q", must)
		}
	}
}

// 场景 2：删除要先审批；批准后续跑，模型看到的是真实结果。
func TestE2E_DeleteNeedsApprovalThenContinues(t *testing.T) {
	e := newE2E(t,
		llmTurn{tools: []llmTool{{id: "call_del", name: "canvas_delete", args: map[string]any{"nodeIds": []string{"n_old"}, "reason": "废稿"}}}},
		llmTurn{text: "已清理。"},
	)
	runID := e.start(t, "把废稿清掉", "all")
	e.waitStatus(t, runID, model.RunWaitingApproval)
	if e.graph(t).Node("n_old") == nil {
		t.Fatal("审批前不能删")
	}
	if len(e.llm.requests()) != 1 {
		t.Fatal("等审批时不能再请求模型")
	}

	approvals, _ := e.repo.ListApprovals(context.Background(), runID)
	if len(approvals) != 1 || approvals[0].Kind != model.ApprovalDelete {
		t.Fatalf("应有一张删除审批: %+v", approvals)
	}
	if _, err := e.svc.Decide(context.Background(), 1, approvals[0].ID, &model.DecideAgentApprovalReq{Decision: "approve"}); err != nil {
		t.Fatal(err)
	}
	run := e.waitStatus(t, runID, model.RunSucceeded, model.RunFailed, model.RunInterrupted)
	if run.Status != model.RunSucceeded {
		t.Fatalf("续跑后应成功: %+v", run)
	}
	if e.graph(t).Node("n_old") != nil {
		t.Error("批准后节点应被删除")
	}
	second := lastRequestText(e.llm)
	if !strings.Contains(second, "已删除 1 个节点") || strings.Contains(second, "等待决定") {
		t.Errorf("续跑时模型应看到真实结果而不是等待占位: %s", second)
	}
	muts, _ := e.repo.ListMutations(context.Background(), runID)
	if len(muts) != 1 || muts[0].Kind != model.MutationDelete {
		t.Errorf("应记一条 delete 改动: %+v", muts)
	}
}

// 场景 3：模型流到一半用户点停止：进程被中止，已产生的部分照样计费，运行是 canceled。
func TestE2E_CancelMidStreamStillBillsPartial(t *testing.T) {
	e := newE2E(t, llmTurn{text: strings.Repeat("很长的回答", 60), slow: 30 * time.Millisecond})
	runID := e.start(t, "说点什么", "all")
	deadline := time.Now().Add(15 * time.Second)
	for len(e.llm.requests()) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond) // 让文本流一会儿
	if _, err := e.svc.Cancel(context.Background(), 1, runID); err != nil {
		t.Fatal(err)
	}
	run := e.waitStatus(t, runID, model.RunCanceled)
	if run.Status != model.RunCanceled {
		t.Fatalf("status=%s", run.Status)
	}
	deadline = time.Now().Add(10 * time.Second)
	var calls []model.AgentModelCall
	for time.Now().Before(deadline) {
		calls = nil
		e.db.Order("id").Find(&calls)
		if len(calls) == 1 && calls[0].Status == model.ModelCallSettled {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(calls) != 1 || calls[0].Status != model.ModelCallSettled || !calls[0].UsageEstimated || calls[0].Credits == 0 {
		t.Fatalf("被取消的调用也要结算，用量按字数估: %+v", calls)
	}
	if !strings.Contains(calls[0].Error, "取消") {
		t.Errorf("应记下取消的原因: %q", calls[0].Error)
	}
	e.waitStatus(t, runID, model.RunCanceled)
	deadline = time.Now().Add(5 * time.Second)
	for e.bridge.TokenCount() != 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if e.bridge.TokenCount() != 0 {
		t.Errorf("令牌应收回: %d", e.bridge.TokenCount())
	}
}

// 场景 4：进程被杀（比如服务重启）：运行标为中断；点继续后新进程接着跑完。
func TestE2E_KilledProcessCanBeResumed(t *testing.T) {
	e := newE2E(t,
		llmTurn{text: strings.Repeat("很长的回答", 80), slow: 30 * time.Millisecond},
		llmTurn{text: "接着说完了。"},
	)
	runID := e.start(t, "说点什么", "all")
	deadline := time.Now().Add(15 * time.Second)
	for len(e.llm.requests()) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	e.rt.Shutdown() // 模拟服务重启：杀掉所有运行进程
	run := e.waitStatus(t, runID, model.RunInterrupted)
	if run.Status != model.RunInterrupted {
		t.Fatalf("被杀后应标为中断: %s", run.Status)
	}

	if _, err := e.svc.Resume(context.Background(), 1, runID, 0); err != nil {
		t.Fatal(err)
	}
	run = e.waitStatus(t, runID, model.RunSucceeded, model.RunFailed)
	if run.Status != model.RunSucceeded {
		t.Fatalf("继续后应跑完: %+v", run)
	}
	if !has(e.eventTypes(t), "message.done") {
		t.Error("续跑后应有 message.done")
	}
}
