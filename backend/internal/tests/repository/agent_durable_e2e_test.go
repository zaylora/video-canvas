package repository_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
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

	"net"

	"video-canvas/internal/agent/canvasgraph"
	"video-canvas/internal/handler"
	agenthandler "video-canvas/internal/handler/agent"
	"video-canvas/internal/llmgateway"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/utils"
	"video-canvas/internal/pkg/ws"
	"video-canvas/internal/provider"
	. "video-canvas/internal/repository"
	"video-canvas/internal/router"
	agentsvc "video-canvas/internal/service/agent"

	"gorm.io/gorm"
)

// agent.runtime=durable 的全栈端到端：浏览器侧的 HTTP（JWT）→ Go 反向代理 → 真正的 Agent 服务（Node 子进程，Pi Durable + SQLite）
// → 回调 Go 的平台 API 和模型代理（真实 PostgreSQL，假的 LLM 上游）→ 事件经 Go 推给 WebSocket。
// 需要 Node（>= 22.19）、已安装依赖的 agent/ 和 TEST_DATABASE_DSN；缺哪个就跳过哪个。

const durableJWTSecret = "durable-e2e-secret-0123456789abcdef"

// relayRecorder 记下 Go 推给 WebSocket 的消息。
type relayRecorder struct {
	mu   sync.Mutex
	msgs []struct {
		channel string
		msg     ws.Message
	}
}

func (r *relayRecorder) Publish(_ context.Context, channel string, msg ws.Message) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = append(r.msgs, struct {
		channel string
		msg     ws.Message
	}{channel, msg})
}

// events 返回推送过的 agent.event（序列化成 JSON 再解出来，和浏览器看到的一致）。
func (r *relayRecorder) events(t *testing.T) []map[string]any {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []map[string]any
	for _, m := range r.msgs {
		if m.msg.Type != ws.TypeAgentEvent {
			continue
		}
		if m.channel != ws.UserChannel(1) {
			t.Errorf("事件应推到用户频道 user:1，实际 %s", m.channel)
		}
		b, _ := json.Marshal(m.msg.Data)
		var v map[string]any
		_ = json.Unmarshal(b, &v)
		out = append(out, v)
	}
	return out
}

type durableEnv struct {
	db       *gorm.DB
	repo     *AgentRepository
	llm      *fakeLLM
	canvas   *model.CanvasProject
	api      *httptest.Server
	agentURL string
	relay    *relayRecorder
	jwt      string
	dbPath   string
	node     string
	dir      string
	bridge   string
	kill     func()
	observe  *agentsvc.ObserveService // 后台观测：读 Go 的库，durable 的运行经 Remote 读 Agent 服务
}

// startDurableAgent 起一个真实的 Agent 服务（durable 模式）：数据在 dbPath 的 SQLite 里，重启同一个路径就是恢复。
func startDurableAgent(t *testing.T, node, dir, bridgeURL, dbPath string, idKey ...string) (url string, kill func()) {
	t.Helper()
	cmd := exec.Command(node, "--disable-warning=ExperimentalWarning", filepath.Join(dir, "src", "serve.ts"))
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"), "AGENT_SERVICE_TOKEN=" + e2eServiceToken, "AGENT_LISTEN=127.0.0.1:0",
		"AGENT_DB_PATH=" + dbPath, "AGENT_BRIDGE_URL=" + bridgeURL, "AGENT_ID_KEY=" + append(idKey, "video-canvas-dev-id-key")[0], // 默认和 idcodec 没调用 Init 时的密钥一致
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	kill = func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }
	t.Cleanup(kill)
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
		_, _ = io.Copy(io.Discard, stderr)
	}()
	select {
	case a := <-addr:
		return "http://" + a, kill
	case <-time.After(20 * time.Second):
		t.Fatal("Agent 服务没有在 20 秒内启动")
		return "", nil
	}
}

func newDurableE2E(t *testing.T, turns ...llmTurn) *durableEnv { return newDurableEnv(t, turns, nil) }

// realUpstream 让全栈环境用真实的模型渠道代替假的 LLM（真实模型验证用，见 agent_realmodel_test.go）。
type realUpstream struct {
	registry  provider.Registry
	secrets   provider.SecretResolver
	ipAllowed func(net.IP) bool
}

func newDurableEnv(t *testing.T, turns []llmTurn, up *realUpstream) *durableEnv {
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
		&model.AgentApproval{}, &model.AgentModelCall{}, &model.UserCredit{}, &model.CreditLedger{}, &model.GenerationTask{},
		&model.AgentToolCall{}, &model.AgentModelCallBody{}, &model.AgentRunDigest{}, &model.AgentFeedback{})
	repo := NewAgentRepository(db)
	observeRepo := NewAgentObserveRepository(db)
	var llm *fakeLLM
	var registry provider.Registry
	var secrets provider.SecretResolver = e2eSecrets{}
	ipAllowed := func(ip net.IP) bool { return ip.IsLoopback() }
	if up != nil {
		registry, secrets, ipAllowed = up.registry, up.secrets, up.ipAllowed
	} else {
		llm = newFakeLLM(t, turns...)
		registry = e2eRegistry{base: llm.srv.URL}
	}
	canvas := &model.CanvasProject{UserID: 1, Title: "画布", Revision: 1, PayloadJSON: []byte(
		`{"nodes":[{"id":"n_script","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"script","label":"剧本","prompt":"雨夜便利店"}},` +
			`{"id":"n_old","type":"canvas","position":{"x":0,"y":300},"data":{"kind":"image","label":"废稿"}}],"edges":[]}`)}
	if err := db.Create(canvas).Error; err != nil {
		t.Fatal(err)
	}
	openAccount(t, db, 1, 100, 0)

	relay := &relayRecorder{}
	canvasSvc := agentsvc.NewAgentCanvasService(repo, nil)
	svc := agentsvc.NewAgentService(agentsvc.AgentDeps{Repo: repo, Canvas: canvasSvc, Models: e2eModels{}, Runtime: agentsvc.NoAgentRuntime{}})
	bridge := agentsvc.NewAgentBridge(agentsvc.BridgeDeps{
		Repo: repo, Canvas: canvasSvc, Agent: svc, Billing: agentsvc.NewAgentBilling(repo),
		Registry: registry, Secrets: secrets, RunUsage: repo,
		Streamer: llmgateway.New(llmgateway.Options{IdleTimeout: 10 * time.Second, IPAllowed: ipAllowed}),
	})
	bridge.SetToolRecorder(observeRepo)         // 工具调用的参数和结果留在 Go 的库里，后台导出要看
	bridge.SetModelRequestRecorder(observeRepo) // 模型请求体同理
	platform := agenthandler.NewPlatformHandler(bridge).WithEventRelay(agentsvc.NewEventRelay(relay))
	bridgeSrv := httptest.NewServer(router.NewBridge("test", agenthandler.NewAgentBridgeHandler(bridge), platform, e2eServiceToken))
	t.Cleanup(bridgeSrv.Close)

	dbPath := filepath.Join(t.TempDir(), "agent", "agent.sqlite")
	agentURL, kill := startDurableAgent(t, node, dir, bridgeSrv.URL, dbPath)

	proxy, err := agenthandler.NewAgentProxy(agentURL, e2eServiceToken)
	if err != nil {
		t.Fatal(err)
	}
	api := httptest.NewServer(router.New("test", durableJWTSecret, router.Handlers{
		Health: &handler.HealthHandler{}, AgentProxy: proxy,
		UserState: func(context.Context, uint64) (*middleware.UserState, error) {
			return &middleware.UserState{Role: "user", Status: "active", TokenVersion: 1}, nil
		},
	}))
	t.Cleanup(api.Close)
	jwt, _, err := utils.GenerateToken(1, "alice", 1, durableJWTSecret, "test", 1)
	if err != nil {
		t.Fatal(err)
	}
	observe := agentsvc.NewObserveService(agentsvc.ObserveDeps{Repo: observeRepo, Runs: repo, Remote: &agentsvc.HTTPRemoteRuns{BaseURL: agentURL, ServiceToken: e2eServiceToken}})
	return &durableEnv{observe: observe, db: db, repo: repo, llm: llm, canvas: canvas, api: api, agentURL: agentURL, relay: relay, jwt: jwt, dbPath: dbPath, node: node, dir: dir, bridge: bridgeSrv.URL, kill: kill}
}

// call 以登录用户的身份发一次请求，返回 HTTP 状态码和解析后的信封。
func (e *durableEnv) call(t *testing.T, method, path, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, e.api.URL+"/api/v1"+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+e.jwt)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var env map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("%s %s 的响应不是 JSON：%v", method, path, err)
	}
	return resp.StatusCode, env
}

func (e *durableEnv) data(t *testing.T, method, path, body string, wantStatus int) any {
	t.Helper()
	status, env := e.call(t, method, path, body)
	if status != wantStatus || env["code"] != float64(0) {
		t.Fatalf("%s %s 应是 %d/code 0：%d %v", method, path, wantStatus, status, env)
	}
	return env["data"]
}

func (e *durableEnv) newSession(t *testing.T) string {
	t.Helper()
	d := e.data(t, "POST", "/canvas/"+idcodec.Encode(uint64(e.canvas.ID))+"/agent/sessions", `{"title":"e2e"}`, 200).(map[string]any)
	return d["id"].(string)
}

func (e *durableEnv) startRun(t *testing.T, sid, msg string) string {
	t.Helper()
	d := e.data(t, "POST", "/agent/sessions/"+sid+"/runs", `{"message":`+jsonString(msg)+`}`, http.StatusAccepted).(map[string]any)
	return d["id"].(string)
}

func jsonString(s string) string { b, _ := json.Marshal(s); return string(b) }

// replay 取会话的全部落库事件（HTTP 回放，前端断线重连用的那条路）。
func (e *durableEnv) replay(t *testing.T, sid string) []map[string]any {
	t.Helper()
	list := e.data(t, "GET", "/agent/sessions/"+sid+"/events?after=0&limit=200", "", 200).([]any)
	out := make([]map[string]any, len(list))
	for i, v := range list {
		out[i] = v.(map[string]any)
	}
	return out
}

// waitEvent 轮询回放，直到出现满足条件的事件。
func (e *durableEnv) waitEvent(t *testing.T, sid, what string, match func(ev map[string]any) bool) map[string]any {
	t.Helper()
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		for _, ev := range e.replay(t, sid) {
			if match(ev) {
				return ev
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("等待超时：%s；事件：%v", what, e.replay(t, sid))
	return nil
}

func runStatus(want ...string) func(map[string]any) bool {
	return func(ev map[string]any) bool {
		if ev["type"] != "run.status" {
			return false
		}
		d, _ := ev["data"].(map[string]any)
		for _, w := range want {
			if d["status"] == w {
				return true
			}
		}
		return false
	}
}

func (e *durableEnv) graph(t *testing.T) *canvasgraph.Graph {
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

// 场景 1：浏览器发起运行 → Agent 服务驱动模型和工具 → 画布被改、积分被扣（都在 Go）→ 事件落在 Agent 的库里，同时经 Go 推给 WebSocket。
func TestE2EDurable_BuildsCanvasBillsAndPushesEvents(t *testing.T) {
	e := newDurableE2E(t,
		llmTurn{tools: []llmTool{{id: "call_1", name: "canvas_apply_ops", args: map[string]any{"ops": []any{
			map[string]any{"op": "create_group", "tempId": "g", "label": "镜头 01"},
			map[string]any{"op": "create_node", "tempId": "a", "kind": "image", "label": "关键帧", "prompt": "雨夜便利店门口", "parentGroup": "g"},
			map[string]any{"op": "connect", "source": "n_script", "target": "a"},
		}}}}},
		llmTurn{text: "搭好了镜头 01。"},
	)
	sid := e.newSession(t)
	runID := e.startRun(t, sid, "给剧本建一个镜头组")
	end := e.waitEvent(t, sid, "运行结束", runStatus("succeeded", "failed", "interrupted"))
	if st := end["data"].(map[string]any)["status"]; st != "succeeded" {
		t.Fatalf("运行应成功：%v", end)
	}

	if g := e.graph(t); len(g.Nodes) != 4 || len(g.Edges) != 1 {
		t.Fatalf("画布应新增 1 个组、1 个节点和 1 条连线（共 4 个节点）：nodes=%d edges=%d", len(g.Nodes), len(g.Edges))
	}
	var calls []model.AgentModelCall
	e.db.Order("id").Find(&calls)
	if len(calls) != 2 || calls[0].Status != model.ModelCallSettled || calls[0].Charged != 5 {
		t.Fatalf("两次模型调用都应结算，每次 5 积分：%+v", calls)
	}
	if a := account(t, e.db, 1); a.Balance != 90 {
		t.Errorf("余额应 100→90：%d", a.Balance)
	}
	// 运行详情来自 Agent 服务：已花积分是 Go 按调用累加的
	runs := e.data(t, "GET", "/agent/sessions/"+sid+"/history?limit=50", "", 200).(map[string]any)
	if runs["events"] == nil {
		t.Fatalf("聊天记录应有事件：%v", runs)
	}
	first := e.llm.requests()[0]
	if first["model"] != "claude-up" || first["stream"] != true {
		t.Errorf("上游收到的请求：model=%v stream=%v", first["model"], first["stream"])
	}
	if msgs := stringify(first["messages"]); !strings.Contains(msgs, "n_script") || !strings.Contains(msgs, "给剧本建一个镜头组") {
		t.Errorf("第一次请求应带画布目录和用户消息")
	}

	// 事件推送：落库的每一条（有序号的）都经 Go 推给了用户频道，id 是 Go 编码的，内容与回放一致
	replay := e.replay(t, sid)
	deadline := time.Now().Add(5 * time.Second)
	var pushed []map[string]any
	for time.Now().Before(deadline) {
		pushed = e.relay.events(t)
		if countSeqs(pushed) >= len(replay) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	bySeq := map[float64]map[string]any{}
	for _, ev := range pushed {
		if ev["seq"].(float64) > 0 {
			bySeq[ev["seq"].(float64)] = ev
		}
	}
	for _, ev := range replay {
		got, ok := bySeq[ev["seq"].(float64)]
		if !ok {
			t.Errorf("事件 seq=%v（%v）没有推送出去", ev["seq"], ev["type"])
			continue
		}
		if got["type"] != ev["type"] || got["session_id"] != sid || got["canvas_id"] != idcodec.Encode(uint64(e.canvas.ID)) {
			t.Errorf("推送的事件和回放不一致：%v vs %v", got, ev)
		}
		if ev["run_id"] != nil && got["run_id"] != runID {
			t.Errorf("run_id 应是 Go 编码的运行 id：%v vs %v", got["run_id"], runID)
		}
	}
	if !hasType(pushed, "message.delta") && !hasType(pushed, "message.done") {
		t.Error("至少应推送到 message.done")
	}
}

// 场景 2：删除要先审批；浏览器批准后 Agent 服务续跑，节点被删，改动记录在 Go，之后可以撤销。
func TestE2EDurable_DeleteNeedsApprovalThenUndo(t *testing.T) {
	e := newDurableE2E(t,
		llmTurn{tools: []llmTool{{id: "call_del", name: "canvas_delete", args: map[string]any{"nodeIds": []string{"n_old"}, "reason": "废稿"}}}},
		llmTurn{text: "已清理。"},
	)
	sid := e.newSession(t)
	runID := e.startRun(t, sid, "把废稿清掉")
	ap := e.waitEvent(t, sid, "出现审批", func(ev map[string]any) bool { return ev["type"] == "approval.created" })
	if e.graph(t).Node("n_old") == nil {
		t.Fatal("审批前不能删")
	}
	if len(e.llm.requests()) != 1 {
		t.Fatal("等审批时不能再请求模型")
	}
	aid := ap["data"].(map[string]any)["id"].(string)
	e.data(t, "POST", "/agent/approvals/"+aid+"/decision", `{"decision":"approve"}`, 200)
	end := e.waitEvent(t, sid, "运行结束", runStatus("succeeded", "failed", "interrupted"))
	if st := end["data"].(map[string]any)["status"]; st != "succeeded" {
		t.Fatalf("续跑后应成功：%v", end)
	}
	if e.graph(t).Node("n_old") != nil {
		t.Error("批准后节点应被删除")
	}
	if second := lastRequestText(e.llm); !strings.Contains(second, "已删除 1 个节点") || strings.Contains(second, "等待决定") {
		t.Errorf("续跑时模型应看到真实结果而不是等待占位：%s", second)
	}
	id, _ := idcodec.Decode(runID)
	muts, _ := e.repo.ListMutations(context.Background(), id)
	if len(muts) != 1 || muts[0].Kind != model.MutationDelete {
		t.Errorf("应记一条 delete 改动（在 Go 的库里）：%+v", muts)
	}

	// 撤销：Agent 校验运行归属并交给 Go 还原画布
	e.data(t, "POST", "/agent/runs/"+runID+"/undo", `{}`, 200)
	if e.graph(t).Node("n_old") == nil {
		t.Error("撤销后被删的节点应回来")
	}
	status, env := e.call(t, "POST", "/agent/runs/"+runID+"/undo", `{}`)
	if status != http.StatusConflict || env["code"] != float64(60007) {
		t.Errorf("重复撤销应是 60007：%d %v", status, env)
	}
}

// 场景 3：Agent 服务被杀（SIGKILL）后用同一个库重启：没跑完的运行在恢复窗口内自动续跑，不重复扣费、不重复改画布。
func TestE2EDurable_KilledAgentResumesRun(t *testing.T) {
	e := newDurableE2E(t,
		llmTurn{tools: []llmTool{{id: "call_1", name: "canvas_apply_ops", args: map[string]any{"ops": []any{
			map[string]any{"op": "create_node", "tempId": "a", "kind": "image", "label": "关键帧", "prompt": "雨夜", "parentGroup": ""},
		}}}}},
		llmTurn{text: "好了", slow: 600 * time.Millisecond},
	)
	sid := e.newSession(t)
	e.startRun(t, sid, "加一个关键帧")
	// 第二轮回答正在慢慢流出时杀掉 Agent
	e.waitEvent(t, sid, "第一轮工具完成", func(ev map[string]any) bool { return ev["type"] == "tool.end" })
	// 等第二轮的请求真的到了上游再杀：否则杀在请求发出之前，续跑第一次发请求就不是「重新请求被打断的那一轮」
	for deadline := time.Now().Add(20 * time.Second); len(e.llm.requests()) < 2; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("第二轮请求没有到达上游")
		}
	}
	for _, ev := range e.replay(t, sid) { // 杀进程前运行不能已经结束，否则这个用例什么都没验证
		if runStatus("succeeded", "failed", "interrupted")(ev) {
			t.Fatalf("杀进程前运行已经结束了：%v", ev)
		}
	}
	e.kill()
	// Agent 不在时，前端的请求得到明确的「暂不可用」，而不是挂起或泄露内部地址
	if status, env := e.call(t, "GET", "/agent/sessions/"+sid+"/events?after=0", ""); status != http.StatusServiceUnavailable || env["code"] != float64(60005) {
		t.Errorf("Agent 服务不在时应是 503/60005：%d %v", status, env)
	}
	if len(e.graph(t).Nodes) != 3 {
		t.Fatalf("杀掉之前画布应已改好：%d", len(e.graph(t).Nodes))
	}

	// 同一个库重启；前端的 REST 地址换了（测试里重建代理）
	url, _ := startDurableAgent(t, e.node, e.dir, e.bridge, e.dbPath)
	proxy, _ := agenthandler.NewAgentProxy(url, e2eServiceToken)
	api := httptest.NewServer(router.New("test", durableJWTSecret, router.Handlers{
		Health: &handler.HealthHandler{}, AgentProxy: proxy,
		UserState: func(context.Context, uint64) (*middleware.UserState, error) {
			return &middleware.UserState{Role: "user", Status: "active", TokenVersion: 1}, nil
		},
	}))
	t.Cleanup(api.Close)
	e.api = api

	end := e.waitEvent(t, sid, "恢复后运行结束", runStatus("succeeded", "failed", "interrupted"))
	if st := end["data"].(map[string]any)["status"]; st != "succeeded" {
		t.Fatalf("重启后运行应续跑并成功：%v", end)
	}
	if n := len(e.llm.requests()); n < 3 {
		t.Errorf("续跑应重新请求被打断的那一轮：上游只收到 %d 次请求", n)
	}
	if n := len(e.graph(t).Nodes); n != 3 {
		t.Errorf("续跑不能重复改画布：%d 个节点", n)
	}
	var settled int64
	e.db.Model(&model.AgentModelCall{}).Where("status = ?", model.ModelCallSettled).Count(&settled)
	var all int64
	e.db.Model(&model.AgentModelCall{}).Count(&all)
	if settled != all || all < 2 {
		var calls []model.AgentModelCall
		e.db.Order("id").Find(&calls)
		for _, c := range calls {
			t.Logf("调用 %d：状态 %s 实扣 %d 错误 %q 估算 %v", c.ID, c.Status, c.Charged, c.Error, c.UsageEstimated)
		}
		t.Errorf("模型调用都应结算（被杀的那次不留下未结算的记录）：settled=%d all=%d", settled, all)
	}
}

func stringify(v any) string { b, _ := json.Marshal(v); return string(b) }

func countSeqs(evs []map[string]any) int {
	n := 0
	for _, ev := range evs {
		if ev["seq"].(float64) > 0 {
			n++
		}
	}
	return n
}

func hasType(evs []map[string]any, typ string) bool {
	for _, ev := range evs {
		if ev["type"] == typ {
			return true
		}
	}
	return false
}

// 场景 4：后台观测。durable 的运行在 Agent 的库里，工具、模型调用、画布改动在 Go 的库里；
// 摘要同步把两边汇总成列表里的一行，导出把两边合成一份，评价和撤销之后再同步，摘要跟着变。
func TestE2EDurable_ObservabilityMergesAgentAndGo(t *testing.T) {
	e := newDurableE2E(t,
		llmTurn{tools: []llmTool{{id: "call_1", name: "canvas_apply_ops", args: map[string]any{"ops": []any{
			map[string]any{"op": "create_node", "tempId": "a", "kind": "image", "label": "关键帧", "prompt": "雨夜便利店门口", "parentGroup": ""},
		}}}}},
		llmTurn{text: "加好了。"},
	)
	sid := e.newSession(t)
	runID := e.startRun(t, sid, "加一个关键帧")
	e.waitEvent(t, sid, "运行结束", runStatus("succeeded", "failed", "interrupted"))
	id, _ := idcodec.Decode(runID)
	ctx := context.Background()
	who := agentsvc.ObserveActor{UserID: 1}

	// 同步前：Go 的摘要表里没有这次运行
	page, err := e.observe.ListRuns(ctx, who, &agentsvc.RunListFilter{})
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("同步前列表应为空：%v %+v", err, page)
	}
	// 同步：每一轮同步到的都是幂等的，反复做不会出错
	var n int
	for i := 0; i < 2; i++ {
		if n, err = e.observe.SyncRemote(ctx); err != nil {
			t.Fatal(err)
		}
	}
	page, err = e.observe.ListRuns(ctx, who, &agentsvc.RunListFilter{})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("同步后列表应有这一次运行：%v %+v", err, page)
	}
	d := page.Items[0]
	if uint64(d.RunID) != id || d.Status != model.RunSucceeded || d.UserID == 0 || d.RequestHead != "加一个关键帧" {
		t.Errorf("摘要里运行本身的字段来自 Agent：%+v", d)
	}
	if d.ToolCalls != 1 || d.ModelCalls != 2 || d.MutationCount != 1 || d.SpentCredits != 10 || d.ModelKey != "claude" || d.Feedback != 0 || d.Undone {
		t.Errorf("工具、模型调用、画布改动来自 Go，积分是 Go 结算的：%+v", d)
	}
	_ = n

	// 用户评价、撤销（都经 Go 的代理到 Agent）：再同步，摘要跟着变
	e.data(t, "PUT", "/agent/runs/"+runID+"/feedback", `{"rating":-1,"reasons":["slow"],"comment":"太慢"}`, 200)
	e.data(t, "POST", "/agent/runs/"+runID+"/undo", `{}`, 200)
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := e.observe.SyncRemote(ctx); err != nil {
			t.Fatal(err)
		}
		page, _ = e.observe.ListRuns(ctx, who, &agentsvc.RunListFilter{})
		if page.Items[0].Feedback == -1 && page.Items[0].Undone {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("评价和撤销之后摘要应更新：%+v", page.Items[0])
		}
		time.Sleep(50 * time.Millisecond)
	}
	down, _ := e.observe.ListRuns(ctx, who, &agentsvc.RunListFilter{Feedback: "down"})
	if len(down.Items) != 1 {
		t.Errorf("按评价筛选：%d", len(down.Items))
	}

	// 导出：两边合成一份
	out, err := e.observe.ExportRun(ctx, who, id, agentsvc.ExportBoth)
	if err != nil {
		t.Fatal(err)
	}
	v := out.Data
	if !hasEventType(v.Events, "message.user") || !hasEventType(v.Events, "message.done") || !hasEventType(v.Events, "tool.end") {
		t.Errorf("事件来自 Agent：%d 条", len(v.Events))
	}
	if len(v.ToolCalls) != 1 || v.ToolCalls[0].Name != "canvas_apply_ops" || !strings.Contains(string(v.ToolCalls[0].Args), "雨夜便利店门口") || v.ToolCalls[0].Result == nil {
		t.Errorf("工具调用的完整参数和结果来自 Go：%+v", v.ToolCalls)
	}
	if len(v.ModelCalls) != 2 || v.ModelCalls[0].Charged != 5 || v.ModelCalls[0].Request == nil {
		t.Errorf("模型调用（含请求摘要）来自 Go：%+v", v.ModelCalls)
	}
	if len(v.Mutations) < 1 || v.Feedback == nil || v.Feedback.Rating != -1 || v.Feedback.Comment != "太慢" {
		t.Errorf("画布改动来自 Go，评价来自 Agent：%+v %+v", v.Mutations, v.Feedback)
	}
	if !strings.Contains(out.Content, "加一个关键帧") || !strings.Contains(out.Content, "加好了。") {
		t.Errorf("Markdown 应有用户请求和 Agent 的最后回复：\n%s", out.Content)
	}
	reqs, err := e.observe.RunRequests(ctx, who, id)
	if err != nil || len(reqs.Items) != 2 {
		t.Errorf("模型请求体在 Go 的库里：%v %+v", err, reqs)
	}
	if _, err := e.observe.ExportRun(ctx, who, id+1, agentsvc.ExportMD); err == nil {
		t.Error("不存在的运行应报错")
	}
}

func hasEventType(evs []*agentsvc.AgentEventView, typ string) bool {
	for _, ev := range evs {
		if ev.Type == typ {
			return true
		}
	}
	return false
}

// ID 编码密钥和 Go 不一致时，Agent 经真实的 Go 平台接口核对出来，/health 报不健康（一键升级的就绪检查据此回退）；密钥一致时是健康的。
func TestE2EDurable_IDKeyMismatchMakesAgentUnhealthy(t *testing.T) {
	e := newDurableE2E(t)
	health := func(url string) int {
		resp, err := http.Get(url + "/health")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	bad, _ := startDurableAgent(t, e.node, e.dir, e.bridge, filepath.Join(t.TempDir(), "bad.sqlite"), "some-other-key")
	deadline := time.Now().Add(15 * time.Second)
	for health(bad) != http.StatusServiceUnavailable {
		if time.Now().After(deadline) {
			t.Fatal("密钥不一致时 /health 应变成 503")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got := health(e.agentURL); got != http.StatusOK {
		t.Errorf("密钥一致的 Agent 应是健康的：%d", got)
	}
}
