package repository_test

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"video-canvas/internal/agent/canvasgraph"
	agenthandler "video-canvas/internal/handler/agent"
	"video-canvas/internal/llmgateway"
	"video-canvas/internal/model"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	. "video-canvas/internal/repository"
	"video-canvas/internal/router"
	agentsvc "video-canvas/internal/service/agent"
)

// 端到端：真正的 Node 进程 + 真正的 Go 桥 HTTP 服务 + 真实 PostgreSQL + 假的 LLM 上游。
// 需要 Node（>= 22.19）和已安装依赖的 仓库根目录 agent/，以及 TEST_DATABASE_DSN；缺哪个就跳过哪个。

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
		Channel: provider.ChannelSnapshot{Key: "ch1", BaseURL: r.base},
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

// e2eRuntime 是端到端用例的运行方式：每个用例在两种方式下各跑一遍，保证行为一致。
type e2eRuntime string

const (
	e2eProcess e2eRuntime = "process" // 每个运行片段起一个 Node 子进程
	// 同上，但配了服务令牌（.env 里设了 AGENT_SERVICE_TOKEN 却没切到 http 时就是这样）：桥要求服务令牌，子进程也得带上
	e2eProcessToken e2eRuntime = "process-token"
	e2eHTTP         e2eRuntime = "http" // 一个常驻的 Node 服务，Go 通过 HTTP 调它
)

const e2eServiceToken = "e2e-service-token-0123456789abcdef0123456789"

func eachRuntime(t *testing.T, fn func(t *testing.T, rt e2eRuntime)) {
	t.Helper()
	for _, rt := range []e2eRuntime{e2eProcess, e2eProcessToken, e2eHTTP} {
		t.Run(string(rt), func(t *testing.T) { fn(t, rt) })
	}
}

// startAgentService 起一个真实的 Agent 服务（agent/src/serve.ts），返回它的地址；测试结束时关掉。
func startAgentService(t *testing.T, node, dir string) string {
	t.Helper()
	cmd := exec.Command(node, "--disable-warning=ExperimentalWarning", filepath.Join(dir, "src", "serve.ts"))
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "AGENT_SERVICE_TOKEN=" + e2eServiceToken, "AGENT_LISTEN=127.0.0.1:0"}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	addr := make(chan string, 1)
	go func() {
		re := regexp.MustCompile(`"addr":"([^"]+)"`)
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			if m := re.FindStringSubmatch(sc.Text()); m != nil {
				addr <- m[1]
				break
			}
		}
		_, _ = io.Copy(io.Discard, stderr) // 之后的日志丢掉，免得管道写满卡住服务
	}()
	select {
	case a := <-addr:
		return "http://" + a
	case <-time.After(15 * time.Second):
		t.Fatal("Agent 服务没有在 15 秒内启动")
		return ""
	}
}

// ---- 环境 ----

type e2eEnv struct {
	db     *gorm.DB
	repo   *AgentRepository
	svc    *agentsvc.AgentService
	rt     *agentsvc.ProcessRuntime
	bridge *agentsvc.AgentBridge
	llm    *fakeLLM
	sess   *model.AgentSession
	canvas *model.CanvasProject
	// bridgeURL 是桥的地址；serviceToken 不为空时同一个监听上还挂着平台接口
	bridgeURL    string
	serviceToken string
}

func newE2E(t *testing.T, mode e2eRuntime, turns ...llmTurn) *e2eEnv {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("没有 node，跳过端到端测试")
	}
	dir, _ := filepath.Abs("../../../../agent")
	if _, err := os.Stat(filepath.Join(dir, "node_modules")); err != nil {
		t.Skip("agent 目录没有安装依赖（npm ci），跳过端到端测试")
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

	canvasSvc := agentsvc.NewAgentCanvasService(repo, nil)
	svc := agentsvc.NewAgentService(agentsvc.AgentDeps{Repo: repo, Canvas: canvasSvc, Models: e2eModels{}, Runtime: agentsvc.NoAgentRuntime{}})
	bridge := agentsvc.NewAgentBridge(agentsvc.BridgeDeps{
		Repo: repo, Canvas: canvasSvc, Agent: svc, Billing: agentsvc.NewAgentBilling(repo),
		Registry: e2eRegistry{base: llm.srv.URL}, Secrets: e2eSecrets{}, RunUsage: repo, Streamer: llmgateway.New(llmgateway.Options{IdleTimeout: 10 * time.Second, IPAllowed: func(ip net.IP) bool { return ip.IsLoopback() }}), // 假上游在本机回环
	})
	// http 模式：Agent 是独立的 Node 服务，Go 和它互相靠服务令牌认；process 模式桥只靠运行令牌
	var launcher agentsvc.Launcher = agentsvc.ExecLauncher{}
	serviceToken := ""
	if mode != e2eProcess {
		serviceToken = e2eServiceToken
	}
	if mode == e2eHTTP {
		launcher = &agentsvc.HTTPLauncher{BaseURL: startAgentService(t, node, dir), ServiceToken: serviceToken, HeartbeatTimeout: 30 * time.Second}
	}
	bridgeSrv := httptest.NewServer(router.NewBridge("test", agenthandler.NewAgentBridgeHandler(bridge), agenthandler.NewPlatformHandler(bridge), serviceToken))
	t.Cleanup(bridgeSrv.Close)
	rt := agentsvc.NewProcessRuntime(agentsvc.RuntimeDeps{
		Repo: repo, Bridge: bridge, Canvas: canvasSvc, Agent: svc, Registry: e2eRegistry{base: llm.srv.URL}, Launcher: launcher,
		Config: agentsvc.RuntimeConfig{NodePath: node, ScriptPath: filepath.Join(dir, "src/main.ts"), BridgeURL: bridgeSrv.URL, ServiceToken: serviceToken, WorkRoot: t.TempDir(), CancelGrace: 3 * time.Second},
	})
	svc.SetRuntime(rt)
	t.Cleanup(rt.Shutdown)
	return &e2eEnv{db: db, repo: repo, svc: svc, rt: rt, bridge: bridge, llm: llm, sess: sess, canvas: canvas, bridgeURL: bridgeSrv.URL, serviceToken: serviceToken}
}

// waitApprovals 等运行的审批记录达到 n 条。CreateApproval 先把运行置为等待状态、再写审批（先 CAS 是为了运行已被取消时不产生审批），
// 两步之间有一个很短的窗口：状态已经是等待审批，审批记录还没写入。只等状态就去查审批，负载高时会落在这个窗口里。
func (e *e2eEnv) waitApprovals(t *testing.T, runID uint64, n int) []model.AgentApproval {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		list, err := e.repo.ListApprovals(context.Background(), runID)
		if err != nil {
			t.Fatal(err)
		}
		if len(list) >= n || time.Now().After(deadline) {
			return list
		}
		time.Sleep(20 * time.Millisecond)
	}
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
func TestE2E_BuildsCanvasAndBills(t *testing.T) { eachRuntime(t, testTestE2EBuildsCanvasAndBills) }

func testTestE2EBuildsCanvasAndBills(t *testing.T, rt e2eRuntime) {
	e := newE2E(t, rt,
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
	eachRuntime(t, testTestE2EDeleteNeedsApprovalThenContinues)
}

func testTestE2EDeleteNeedsApprovalThenContinues(t *testing.T, rt e2eRuntime) {
	e := newE2E(t, rt,
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

	approvals := e.waitApprovals(t, runID, 1)
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
	eachRuntime(t, testTestE2ECancelMidStreamStillBillsPartial)
}

func testTestE2ECancelMidStreamStillBillsPartial(t *testing.T, rt e2eRuntime) {
	e := newE2E(t, rt, llmTurn{text: strings.Repeat("很长的回答", 60), slow: 30 * time.Millisecond})
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
	eachRuntime(t, testTestE2EKilledProcessCanBeResumed)
}

func testTestE2EKilledProcessCanBeResumed(t *testing.T, rt e2eRuntime) {
	e := newE2E(t, rt,
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

// ---- 平台 API 的上下文模式：真实 HTTP、真实 Postgres ----

// platformPost 以 Agent 服务的身份调一次平台接口：服务令牌、上下文头（base64url 的 JSON，ID 是十进制字符串）、幂等键。
func (e *e2eEnv) platformPost(t *testing.T, tool, ctxJSON, idemKey, body string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, e.bridgeURL+"/internal/platform/v1/tools/"+tool, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-Service-Token", e.serviceToken)
	req.Header.Set("X-Agent-Context", base64.RawURLEncoding.EncodeToString([]byte(ctxJSON)))
	if idemKey != "" {
		req.Header.Set("X-Idempotency-Key", idemKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, out
}

// Agent 服务的 ID 是雪花算法生成的，超过 2^53：必须在头部解析、bigint 列里都不丢精度；运行记录在 Go 的库里根本不存在，
// 画布写入照常成功，并且不写任何事件（事件归 Agent 服务）。
func TestE2E_PlatformToolOverHTTPOnRealDB(t *testing.T) {
	e := newE2E(t, e2eProcessToken)
	const runID, sessID uint64 = 1152921504606846977, 1152921504606846978 // 2^60 + 1、2^60 + 2：转成 float64 会变成同一个数
	ctxJSON := fmt.Sprintf(`{"run_id":"%d","user_id":"1","session_id":"%d","canvas_id":"%d","mode":"all","model_key":"claude"}`, runID, sessID, e.canvas.ID)
	ops := `{"args":{"ops":[{"op":"create_node","tempId":"a","kind":"image","label":"关键帧"},{"op":"connect","source":"n_script","target":"a"}]}}`

	var runs, eventsBefore int64
	e.db.Model(&model.AgentRun{}).Where("id = ?", runID).Count(&runs)
	e.db.Model(&model.AgentEvent{}).Count(&eventsBefore)
	if runs != 0 {
		t.Fatal("前提：Go 的库里没有这个运行")
	}

	status, first := e.platformPost(t, "canvas_apply_ops", ctxJSON, "task-0192-aaaa", ops)
	if status != 200 || first["code"] != float64(0) {
		t.Fatalf("status=%d %v", status, first)
	}
	data := first["data"].(map[string]any)
	if data["is_error"] == true {
		t.Fatalf("写画布应成功: %v", data["content"])
	}
	// 事件内容由 Go 给出（Agent 服务落库和推送）：经真实 HTTP 也要在响应里
	end, _ := data["tool_end"].(map[string]any)
	if end["name"] != "canvas_apply_ops" || end["summary"] == nil || end["label"] == nil {
		t.Errorf("响应里要有 tool_end（名称、摘要、界面名称）: %v", data["tool_end"])
	}
	// 同一个幂等键再来一次（崩溃后重放）：返回同样的结果，画布不再变
	_, again := e.platformPost(t, "canvas_apply_ops", ctxJSON, "task-0192-aaaa", ops)
	if again["data"].(map[string]any)["content"] != data["content"] {
		t.Errorf("重放的结果要和第一次一样:\n%v\n%v", data["content"], again["data"].(map[string]any)["content"])
	}

	var muts []model.AgentMutation
	e.db.Where("run_id = ?", runID).Find(&muts)
	if len(muts) != 1 {
		t.Fatalf("重放只能留下一条改动记录，实际 %d", len(muts))
	}
	if muts[0].RunID != runID || muts[0].IdemKey != "task-0192-aaaa" {
		t.Errorf("run_id 要无损存进 bigint，幂等键要存下来: run_id=%d idem=%q", muts[0].RunID, muts[0].IdemKey)
	}
	if n := len(e.graph(t).Nodes); n != 3 {
		t.Errorf("画布应是 2 个原有节点 + 1 个新节点: %d", n)
	}
	var eventsAfter int64
	e.db.Model(&model.AgentEvent{}).Count(&eventsAfter)
	if eventsAfter != eventsBefore {
		t.Errorf("上下文模式下 Go 不写事件: %d → %d", eventsBefore, eventsAfter)
	}

	// 相邻的两个 ID 在 float64 下会撞成同一个：用另一个运行 id 调用，画布写入要记在它自己名下
	otherCtx := fmt.Sprintf(`{"run_id":"%d","user_id":"1","session_id":"%d","canvas_id":"%d","mode":"all"}`, runID+1, sessID, e.canvas.ID)
	e.platformPost(t, "canvas_apply_ops", otherCtx, "task-0192-bbbb", `{"args":{"ops":[{"op":"create_node","tempId":"b","kind":"image","label":"另一个"}]}}`)
	var n1, n2 int64
	e.db.Model(&model.AgentMutation{}).Where("run_id = ?", runID).Count(&n1)
	e.db.Model(&model.AgentMutation{}).Where("run_id = ?", runID+1).Count(&n2)
	if n1 != 1 || n2 != 1 {
		t.Errorf("相邻的两个大 ID 不能混成一个: %d / %d", n1, n2)
	}

	// 服务令牌错了：进不来
	req, _ := http.NewRequest(http.MethodPost, e.bridgeURL+"/internal/platform/v1/tools/canvas_get_state", strings.NewReader(`{}`))
	req.Header.Set("X-Agent-Context", base64.RawURLEncoding.EncodeToString([]byte(ctxJSON)))
	req.Header.Set("X-Agent-Service-Token", "wrong-wrong-wrong-wrong-wrong-wrong-wrong")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("令牌不对应 401: %d", resp.StatusCode)
	}
}

// platformApproval 以 Agent 服务的身份调用审批的三个接口之一，返回 HTTP 状态和解析后的响应。
func (e *e2eEnv) platformApproval(t *testing.T, op, ctxJSON, body string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, e.bridgeURL+"/internal/platform/v1/approvals/"+op, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-Service-Token", e.serviceToken)
	req.Header.Set("X-Agent-Context", base64.RawURLEncoding.EncodeToString([]byte(ctxJSON)))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, out
}

// 删除审批的完整流程走真实 HTTP 和真实 Postgres：预估 → 解析 → 执行 → 重放。
// 运行和审批 id 都是雪花大小（超过 2^53）；Go 的库里没有这个运行，也不会为它创建审批记录；
// 执行的幂等键（审批 id）存进 agent_mutations.idem_key，重放不再删一次。
func TestE2E_PlatformApprovalFlowOverHTTPOnRealDB(t *testing.T) {
	e := newE2E(t, e2eProcessToken)
	const runID, sessID, approvalID uint64 = 1152921504606846977, 1152921504606846978, 1152921504606846979
	ctxJSON := fmt.Sprintf(`{"run_id":"%d","user_id":"1","session_id":"%d","canvas_id":"%d","mode":"all","model_key":"claude"}`, runID, sessID, e.canvas.ID)

	// 预估：算出审批内容；不落库
	status, prop := e.platformApproval(t, "propose", ctxJSON, `{"kind":"delete","args":{"nodeIds":["n_old"],"reason":"废稿"}}`)
	if status != 200 || prop["code"] != float64(0) {
		t.Fatalf("预估: status=%d %v", status, prop)
	}
	pd := prop["data"].(map[string]any)
	if pd["is_error"] == true || pd["terminate"] != true {
		t.Fatalf("预估应成功并要求停下等用户: %v", pd)
	}
	payload, _ := json.Marshal(pd["payload"])
	var approvals, events int64
	e.db.Model(&model.AgentApproval{}).Count(&approvals)
	e.db.Model(&model.AgentEvent{}).Count(&events)
	if approvals != 0 || events != 0 {
		t.Fatalf("预估不落库（审批归 Agent 服务）: 审批 %d 事件 %d", approvals, events)
	}

	// 解析：用户批准
	status, res := e.platformApproval(t, "resolve", ctxJSON, fmt.Sprintf(`{"kind":"delete","payload":%s,"decision":{"decision":"approve"}}`, payload))
	if status != 200 || res["code"] != float64(0) {
		t.Fatalf("解析: status=%d %v", status, res)
	}
	outcome, _ := json.Marshal(res["data"])
	if rd := res["data"].(map[string]any); rd["status"] != "approved" {
		t.Fatalf("应整体批准: %v", rd)
	}

	// 执行；再执行一次（崩溃后重试）要得到同样的结果
	execBody := fmt.Sprintf(`{"kind":"delete","approval_id":"%d","tool_call_id":"call_del","outcome":%s}`, approvalID, outcome)
	status, first := e.platformApproval(t, "execute", ctxJSON, execBody)
	if status != 200 || first["code"] != float64(0) {
		t.Fatalf("执行: status=%d %v", status, first)
	}
	fd := first["data"].(map[string]any)
	if fd["status"] != "executed" || !strings.Contains(fd["outcome_text"].(string), "已删除 1 个节点") {
		t.Fatalf("执行结果: %v", fd)
	}
	_, again := e.platformApproval(t, "execute", ctxJSON, execBody)
	ad := again["data"].(map[string]any)
	if ad["status"] != "executed" || ad["outcome_text"] != fd["outcome_text"] {
		t.Errorf("重放要和第一次一样:\n%v\n%v", fd, ad)
	}

	var muts []model.AgentMutation
	e.db.Where("run_id = ?", runID).Find(&muts)
	if len(muts) != 1 || muts[0].Kind != model.MutationDelete || muts[0].IdemKey != fmt.Sprintf("approval-%d", approvalID) {
		t.Fatalf("只能留下一条删除改动，幂等键是审批 id: %+v", muts)
	}
	for _, n := range e.graph(t).Nodes {
		if n.ID() == "n_old" {
			t.Error("节点应已被删除")
		}
	}
	e.db.Model(&model.AgentApproval{}).Count(&approvals)
	if approvals != 0 {
		t.Errorf("Go 不为平台调用创建审批记录: %d", approvals)
	}

	// 服务令牌错了：进不来，也删不了东西
	req, _ := http.NewRequest(http.MethodPost, e.bridgeURL+"/internal/platform/v1/approvals/execute", strings.NewReader(execBody))
	req.Header.Set("X-Agent-Context", base64.RawURLEncoding.EncodeToString([]byte(ctxJSON)))
	req.Header.Set("X-Agent-Service-Token", "wrong-wrong-wrong-wrong-wrong-wrong-wrong")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("令牌不对应 401: %d", resp.StatusCode)
	}
}

// 运行准备经真实 HTTP：画布目录读自真实 Postgres（选中的节点优先），运行和会话 id 是雪花大小；不落任何记录。
func TestE2E_PlatformPrepareRunOverHTTPOnRealDB(t *testing.T) {
	e := newE2E(t, e2eProcessToken)
	const runID, sessID uint64 = 1152921504606846977, 1152921504606846978
	ctxJSON := fmt.Sprintf(`{"run_id":"%d","user_id":"1","session_id":"%d","canvas_id":"%d","mode":"storyboard","model_key":"claude","selection":["n_old"]}`, runID, sessID, e.canvas.ID)
	req, _ := http.NewRequest(http.MethodPost, e.bridgeURL+"/internal/platform/v1/runs/prepare", strings.NewReader(`{"message":"把剧本拆成分镜","max_steps":30}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-Service-Token", e.serviceToken)
	req.Header.Set("X-Agent-Context", base64.RawURLEncoding.EncodeToString([]byte(ctxJSON)))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Code int `json:"code"`
		Data struct {
			Model struct {
				ContextWindow int `json:"context_window"`
				MaxTokens     int `json:"max_tokens"`
			} `json:"model"`
			SystemPrompt string                  `json:"system_prompt"`
			Tools        []struct{ Name string } `json:"tools"`
			FirstMessage string                  `json:"first_message"`
			Harness      struct{ Key string }    `json:"harness"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || resp.StatusCode != 200 || out.Code != 0 {
		t.Fatalf("status=%d code=%d err=%v", resp.StatusCode, out.Code, err)
	}
	d := out.Data
	if d.SystemPrompt == "" || len(d.Tools) == 0 || d.Model.ContextWindow == 0 {
		t.Errorf("模型、提示词、工具都要有: %+v", d)
	}
	for _, want := range []string{"把剧本拆成分镜", "n_script", "n_old", "最多 30 步工具调用"} {
		if !strings.Contains(d.FirstMessage, want) {
			t.Errorf("第一条消息缺少 %q（目录要读自真实的库）:\n%s", want, d.FirstMessage)
		}
	}
	var runs, events int64
	e.db.Model(&model.AgentRun{}).Count(&runs)
	e.db.Model(&model.AgentEvent{}).Count(&events)
	if runs != 0 || events != 0 {
		t.Errorf("准备不落任何记录: 运行 %d 事件 %d", runs, events)
	}
}

// platformChat 以 Agent 服务的身份走一次上下文模式的模型代理，返回 HTTP 状态、响应头和响应体。
func (e *e2eEnv) platformChat(t *testing.T, ctxJSON, purpose string) (int, http.Header, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, e.bridgeURL+"/internal/platform/v1/model/chat/completions",
		strings.NewReader(`{"model":"agent","stream":true,"messages":[{"role":"user","content":"拆分镜"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-Service-Token", e.serviceToken)
	req.Header.Set("X-Agent-Context", base64.RawURLEncoding.EncodeToString([]byte(ctxJSON)))
	if purpose != "" {
		req.Header.Set("X-Agent-Purpose", purpose)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, string(b)
}

func (e *e2eEnv) platformUsage(t *testing.T, ctxJSON, runID, query string) map[string]any {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, e.bridgeURL+"/internal/platform/v1/runs/"+runID+"/usage"+query, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Agent-Service-Token", e.serviceToken)
	req.Header.Set("X-Agent-Context", base64.RawURLEncoding.EncodeToString([]byte(ctxJSON)))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

// 模型代理的整条链路：运行记录不在 Go 里（雪花 ID），请求头里的上下文 → 预检 → 假上游的流式响应 → 结算扣费 → 用量查询 → 预算拒绝。
func TestE2E_PlatformModelProxyOverHTTPOnRealDB(t *testing.T) {
	e := newE2E(t, e2eProcessToken, llmTurn{text: "好的"}, llmTurn{text: "再来一次"}, llmTurn{text: "不该走到上游"})
	const runID uint64 = 1152921504606846977
	ctxJSON := fmt.Sprintf(`{"run_id":"%d","user_id":"1","session_id":"1152921504606846978","canvas_id":"%d","mode":"all","model_key":"claude","budget_credits":10}`, runID, e.canvas.ID)

	var balanceBefore model.UserCredit
	e.db.Where("user_id = ?", 1).First(&balanceBefore)
	var eventsBefore int64
	e.db.Model(&model.AgentEvent{}).Count(&eventsBefore)

	status, hdr, body := e.platformChat(t, ctxJSON, "")
	// 上游是逐字流出的（"好"、"的"是两个分片），代理要原样转出，包括用量分片和结束标记
	if status != 200 || !strings.Contains(body, `"content":"好"`) || !strings.Contains(body, `"content":"的"`) || !strings.Contains(body, `"prompt_tokens":1000000`) || !strings.Contains(body, "[DONE]") {
		t.Fatalf("流应原样转出: status=%d %q", status, body)
	}
	callID := hdr.Get("X-Agent-Call-Id")
	if callID == "" || !strings.HasPrefix(hdr.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("响应头: %v", hdr)
	}

	// 库里：调用记录按这个运行记账并已结算，积分真的扣了，没有这个运行的记录，也没有事件
	var calls []model.AgentModelCall
	e.db.Where("run_id = ?", runID).Find(&calls)
	if len(calls) != 1 || calls[0].Status != model.ModelCallSettled || calls[0].Charged != 5 || fmt.Sprint(calls[0].ID) != callID {
		t.Fatalf("应有一条已结算的调用，实扣 5 积分，id 和响应头一致: %+v (header=%s)", calls, callID)
	}
	var after model.UserCredit
	e.db.Where("user_id = ?", 1).First(&after)
	if balanceBefore.Balance-after.Balance != 5 {
		t.Errorf("余额应少 5: %d → %d", balanceBefore.Balance, after.Balance)
	}
	var runs, eventsAfter int64
	e.db.Model(&model.AgentRun{}).Where("id = ?", runID).Count(&runs)
	e.db.Model(&model.AgentEvent{}).Count(&eventsAfter)
	if runs != 0 || eventsAfter != eventsBefore {
		t.Errorf("上下文模式下 Go 不建运行记录、不写事件: runs=%d events %d→%d", runs, eventsBefore, eventsAfter)
	}

	// 用量查询：本轮已花、上下文大小、这次调用的结算情况
	u := e.platformUsage(t, ctxJSON, fmt.Sprint(runID), "?call_id="+callID)["data"].(map[string]any)
	call := u["call"].(map[string]any)
	if u["spent_credits"] != float64(5) || u["context_tokens"] != float64(1_000_000) || call["status"] != "settled" || call["charged"] != float64(5) || call["id"] != callID {
		t.Errorf("用量: %v", u)
	}

	// 第二次：预算 10，已花 5，调用方又说已批准的生成花了 5（SpentExtra）→ 剩 0，最坏情况必然超出，被拒绝且不发往上游
	over := strings.Replace(ctxJSON, `"budget_credits":10`, `"budget_credits":10,"spent_extra":5`, 1)
	status, _, body = e.platformChat(t, over, "")
	if status != 402 || !strings.Contains(body, `"error"`) || !strings.Contains(body, "预算") {
		t.Errorf("预算用尽应 402 + OpenAI 格式错误: status=%d %s", status, body)
	}
	if n := len(e.llm.requests()); n != 1 {
		t.Errorf("被拒绝的请求不能发往上游: %d", n)
	}
	var count int64
	e.db.Model(&model.AgentModelCall{}).Where("run_id = ?", runID).Count(&count)
	if count != 1 {
		t.Errorf("被拒绝的请求不能创建调用记录: %d", count)
	}

	// 不限预算时第二次照常走，摘要用途要记成 summary
	free := strings.Replace(ctxJSON, `,"budget_credits":10`, "", 1)
	status, _, _ = e.platformChat(t, free, "summary")
	if status != 200 {
		t.Fatalf("status=%d", status)
	}
	e.db.Where("run_id = ?", runID).Order("id").Find(&calls)
	if len(calls) != 2 || calls[1].Purpose != "summary" {
		t.Errorf("第二次调用应记成摘要: %+v", calls)
	}
	u = e.platformUsage(t, ctxJSON, fmt.Sprint(runID), "")["data"].(map[string]any)
	if u["spent_credits"] != float64(10) || u["context_tokens"] != float64(1_000_000) {
		t.Errorf("两次都计费，上下文仍是对话调用的输入: %v", u)
	}
	// 别的用户查不到这个运行的调用
	other := strings.Replace(ctxJSON, `"user_id":"1"`, `"user_id":"2"`, 1)
	if res := e.platformUsage(t, other, fmt.Sprint(runID), "?call_id="+callID); res["code"] == float64(0) {
		t.Errorf("别的用户不能查到这次调用: %v", res)
	}
}
