package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"video-canvas/internal/llmgateway"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/ws"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	. "video-canvas/internal/service/agent"
)

// ---- 桥的 fake 依赖 ----

type fakeBridgeRegistry struct {
	snap   *provider.Snapshot
	snapEr error
	models []provider.ModelInfo
	gotKey string
}

func (r *fakeBridgeRegistry) Snapshot(_ context.Context, key string) (*provider.Snapshot, error) {
	r.gotKey = key
	return r.snap, r.snapEr
}
func (r *fakeBridgeRegistry) ListModels(_ context.Context, kind string) ([]provider.ModelInfo, error) {
	var out []provider.ModelInfo
	for _, m := range r.models {
		if kind == "" || m.Kind == kind {
			out = append(out, m)
		}
	}
	return out, nil
}

type fakeSecrets map[string]string

func (s fakeSecrets) Get(_ context.Context, name string) (string, error) {
	if v, ok := s[name]; ok {
		return v, nil
	}
	return "", errors.New("凭证尚未设置")
}

// fakeStreamer 扮演网关的透传入口：记下收到的目标和请求体，按脚本回放。
type fakeStreamer struct {
	script func(ctx context.Context, emit func(llmgateway.Event), raw func([]byte)) (*llmgateway.Result, error)
	calls  int
	tg     llmgateway.Target
	body   []byte
	lim    llmgateway.RawLimits
}

func (s *fakeStreamer) StreamRaw(ctx context.Context, tg llmgateway.Target, body []byte, lim llmgateway.RawLimits, emit func(llmgateway.Event), raw func([]byte)) (*llmgateway.Result, error) {
	s.calls++
	s.tg, s.body, s.lim = tg, body, lim
	return s.script(ctx, emit, raw)
}

func okStream(text string, usage llmgateway.Usage) func(context.Context, func(llmgateway.Event), func([]byte)) (*llmgateway.Result, error) {
	return func(_ context.Context, emit func(llmgateway.Event), raw func([]byte)) (*llmgateway.Result, error) {
		emit(llmgateway.Event{Type: llmgateway.EventText, Text: text})
		raw([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"" + text + "\"}}]}\n\n"))
		raw([]byte("data: [DONE]\n\n"))
		return &llmgateway.Result{Text: text, FinishReason: "stop", Usage: usage}, nil
	}
}

type bridgeEnv struct {
	*agentEnv
	bill *fakeBillingRepo
	reg  *fakeBridgeRegistry
	str  *fakeStreamer
	br   *AgentBridge
	now  *time.Time
}

func newBridgeEnv(t *testing.T) *bridgeEnv {
	t.Helper()
	e := newAgentEnv(t)
	bill := &fakeBillingRepo{available: 100}
	caps := billingCaps
	caps.Vision = true
	reg := &fakeBridgeRegistry{snap: &provider.Snapshot{
		Model:   provider.ModelSnapshot{Key: "claude", Kind: "agent", UpstreamModel: "claude-up", Pricing: billingPricing, Capabilities: caps},
		Channel: provider.ChannelSnapshot{Key: "ch1", BaseURL: "https://llm.example", TrustedInternal: true, RateLimit: provider.RateLimit{RPS: 2, MaxConcurrency: 3}},
	}, models: []provider.ModelInfo{
		{Key: "seedream", Kind: "image", Label: "Seedream", Hint: "图片模型", Capabilities: modelcfg.Capabilities{Ops: []string{"t2i"}}},
		{Key: "claude", Kind: "agent", Label: "Claude"},
	}}
	str := &fakeStreamer{script: okStream("好的", llmgateway.Usage{InputTokens: 1_000_000, OutputTokens: 100_000})}
	now := e.now
	br := NewAgentBridge(BridgeDeps{
		Repo: e.repo, Canvas: NewAgentCanvasService(e.repo, e.bc), Agent: e.svc, Billing: NewAgentBilling(bill),
		Registry: reg, Secrets: fakeSecrets{"channel:ch1": "sk-test"}, Streamer: str,
		Now: func() time.Time { return now }, TokenTTL: time.Hour,
	})
	return &bridgeEnv{agentEnv: e, bill: bill, reg: reg, str: str, br: br, now: &now}
}

func (b *bridgeEnv) token(t *testing.T, mode string, selection ...string) (string, *model.AgentRun) {
	t.Helper()
	run := b.runningRun(t)
	return b.br.IssueToken(run, model.AgentRunInput{ModelKey: "claude", Mode: mode, Selection: selection}), run
}

func (b *bridgeEnv) tool(t *testing.T, tok, name string, args any) *ToolResult {
	t.Helper()
	raw, _ := json.Marshal(args)
	res, err := b.br.ExecuteTool(context.Background(), tok, "tc-"+name, name, raw)
	if err != nil {
		t.Fatalf("ExecuteTool(%s): %v", name, err)
	}
	return res
}

func (b *bridgeEnv) persistedTypes() []string { return b.repo.eventTypes() }

// ---- 令牌 ----

func TestAgentBridge_Tokens(t *testing.T) {
	b := newBridgeEnv(t)
	tok, run := b.token(t, "all")
	if len(tok) < 32 {
		t.Errorf("令牌应足够长（防猜）: %q", tok)
	}
	other := b.br.IssueToken(run, model.AgentRunInput{ModelKey: "claude"})
	if other == tok {
		t.Error("每次签发的令牌必须不同")
	}

	t.Run("未知令牌", func(t *testing.T) {
		_, err := b.br.ExecuteTool(context.Background(), "nope", "tc", "plan_update", []byte(`{}`))
		if !errors.Is(err, ErrBridgeToken) {
			t.Errorf("应 ErrBridgeToken: %v", err)
		}
	})
	t.Run("撤销后失效", func(t *testing.T) {
		b.br.RevokeToken(tok)
		_, err := b.br.ExecuteTool(context.Background(), tok, "tc", "plan_update", []byte(`{}`))
		if !errors.Is(err, ErrBridgeToken) {
			t.Errorf("err=%v", err)
		}
	})
	t.Run("过期后失效", func(t *testing.T) {
		*b.now = b.now.Add(2 * time.Hour)
		_, err := b.br.ExecuteTool(context.Background(), other, "tc", "plan_update", []byte(`{}`))
		if !errors.Is(err, ErrBridgeToken) {
			t.Errorf("err=%v", err)
		}
	})
}

// ---- 模型代理 ----

func TestAgentBridge_ProxyModel(t *testing.T) {
	ctx := context.Background()
	body := []byte(`{"model":"x","messages":[{"role":"user","content":"拆分镜"}]}`)

	t.Run("成功：按渠道和模型配置发起、原样转出流、结算、记事件", func(t *testing.T) {
		b := newBridgeEnv(t)
		tok, run := b.token(t, "all")
		var out []string
		if err := b.br.ProxyModel(ctx, tok, body, func(l []byte) { out = append(out, string(l)) }); err != nil {
			t.Fatal(err)
		}
		if len(out) != 2 || !strings.Contains(out[0], "好的") || out[1] != "data: [DONE]\n\n" {
			t.Errorf("流应原样转出: %v", out)
		}
		s := b.str
		if s.tg.BaseURL != "https://llm.example" || s.tg.APIKey != "sk-test" || s.tg.UpstreamModel != "claude-up" || s.tg.ChannelKey != "ch1" ||
			s.tg.RPS != 2 || s.tg.MaxConcurrency != 3 || !s.tg.TrustedInternal {
			t.Errorf("目标应来自渠道快照和凭证: %+v", s.tg)
		}
		if s.lim.MaxTokens != 8192 || string(s.body) != string(body) {
			t.Errorf("输出上限应取模型的 context.output: %+v", s.lim)
		}
		if len(b.bill.settled) != 1 || b.bill.settled[0].Credits != 5 || b.bill.usageRunID != run.ID {
			t.Errorf("应按用量结算并记入本轮已花: %+v", b.bill.settled)
		}
		types := b.persistedTypes()
		if types[len(types)-1] != "message.done" {
			t.Errorf("结束时应记一条 message.done: %v", types)
		}
		for _, ty := range types {
			if ty == "message.delta" {
				t.Error("文本增量只推送，不落库（否则每个 Token 一次写库）")
			}
		}
		pushedDelta := false
		for _, m := range b.bc.msgs {
			if m.Type == ws.TypeAgentEvent {
				if v, ok := m.Data.(*AgentEventView); ok && v.Type == "message.delta" && v.Seq == 0 {
					pushedDelta = true
				}
			}
		}
		if !pushedDelta {
			t.Error("文本增量应推送给前端（seq 为 0 表示临时事件）")
		}
	})

	t.Run("运行不在 running：60003，不发请求", func(t *testing.T) {
		b := newBridgeEnv(t)
		tok, run := b.token(t, "all")
		b.setStatus(run.ID, model.RunWaitingApproval)
		err := b.br.ProxyModel(ctx, tok, body, func([]byte) {})
		wantAgentCode(t, err, errcode.ErrAgentState)
		if b.str.calls != 0 {
			t.Error("不能发请求")
		}
	})

	t.Run("模型不可用或不是 agent 种类：60002", func(t *testing.T) {
		b := newBridgeEnv(t)
		tok, _ := b.token(t, "all")
		b.reg.snapEr = provider.ErrModelUnavailable
		wantAgentCode(t, b.br.ProxyModel(ctx, tok, body, func([]byte) {}), errcode.ErrAgentModelNA)

		b.reg.snapEr = nil
		b.reg.snap.Model.Kind = "text"
		wantAgentCode(t, b.br.ProxyModel(ctx, tok, body, func([]byte) {}), errcode.ErrAgentModelNA)
	})

	t.Run("渠道 Key 没设置：60002", func(t *testing.T) {
		b := newBridgeEnv(t)
		tok, _ := b.token(t, "all")
		b.reg.snap.Channel.Key = "other"
		wantAgentCode(t, b.br.ProxyModel(ctx, tok, body, func([]byte) {}), errcode.ErrAgentModelNA)
	})

	t.Run("本轮预算不够：60008，运行转为预算用尽，不发请求也不建调用记录", func(t *testing.T) {
		b := newBridgeEnv(t)
		tok, run := b.token(t, "all")
		b.repo.mu.Lock()
		b.repo.runs[run.ID].SpentCredits = 50
		b.repo.mu.Unlock()
		wantAgentCode(t, b.br.ProxyModel(ctx, tok, body, func([]byte) {}), errcode.ErrAgentOverBudget)
		if b.run(run.ID).Status != model.RunBudgetExhausted || b.str.calls != 0 || len(b.bill.calls) != 0 {
			t.Errorf("status=%s calls=%d", b.run(run.ID).Status, b.str.calls)
		}
	})

	t.Run("余额不够：40001", func(t *testing.T) {
		b := newBridgeEnv(t)
		tok, _ := b.token(t, "all")
		b.bill.available = 0
		wantAgentCode(t, b.br.ProxyModel(ctx, tok, body, func([]byte) {}), errcode.ErrInsufficientCredits)
	})

	t.Run("上游报错：原样返回，不收费，不记 message.done", func(t *testing.T) {
		b := newBridgeEnv(t)
		tok, _ := b.token(t, "all")
		upErr := &llmgateway.UpstreamError{Status: 429, Message: "上游请求太频繁", Retryable: true}
		b.str.script = func(context.Context, func(llmgateway.Event), func([]byte)) (*llmgateway.Result, error) {
			return nil, upErr
		}
		err := b.br.ProxyModel(ctx, tok, body, func([]byte) {})
		var ue *llmgateway.UpstreamError
		if !errors.As(err, &ue) || ue.Status != 429 {
			t.Fatalf("err=%v", err)
		}
		if b.bill.settled[0].Credits != 0 || b.bill.settled[0].Error == "" {
			t.Errorf("没有产出不该收费，但要记下原因: %+v", b.bill.settled[0])
		}
		for _, ty := range b.persistedTypes() {
			if ty == "message.done" {
				t.Error("失败时不记 message.done")
			}
		}
	})

	t.Run("被取消：已产生的部分照样结算（用不会被取消的上下文）", func(t *testing.T) {
		b := newBridgeEnv(t)
		tok, _ := b.token(t, "all")
		c, cancel := context.WithCancel(ctx)
		b.str.script = func(c2 context.Context, emit func(llmgateway.Event), raw func([]byte)) (*llmgateway.Result, error) {
			emit(llmgateway.Event{Type: llmgateway.EventText, Text: "说到一半"})
			cancel()
			return &llmgateway.Result{Text: "说到一半", UsageMissing: true}, context.Canceled
		}
		err := b.br.ProxyModel(c, tok, body, func([]byte) {})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err=%v", err)
		}
		if len(b.bill.settled) != 1 || b.bill.settled[0].Credits == 0 || !b.bill.settled[0].UsageEstimated {
			t.Errorf("取消后仍要结算已产生的部分: %+v", b.bill.settled)
		}
	})
}

// ---- 工具 ----

func TestAgentBridge_Tools(t *testing.T) {
	t.Run("运行不在 running：60003", func(t *testing.T) {
		b := newBridgeEnv(t)
		tok, run := b.token(t, "all")
		b.setStatus(run.ID, model.RunCanceled)
		_, err := b.br.ExecuteTool(context.Background(), tok, "tc", "plan_update", []byte(`{"steps":[]}`))
		wantAgentCode(t, err, errcode.ErrAgentState)
	})

	t.Run("canvas_get_state：目录优先放选中的节点；传 nodeIds 读详情", func(t *testing.T) {
		b := newBridgeEnv(t)
		tok, _ := b.token(t, "all", "n_script")
		res := b.tool(t, tok, "canvas_get_state", map[string]any{})
		if res.IsError || !strings.Contains(res.Content, "n_script") || !strings.Contains(res.Content, `"total":1`) {
			t.Fatalf("目录=%+v", res)
		}
		res = b.tool(t, tok, "canvas_get_state", map[string]any{"nodeIds": []string{"n_script"}})
		if res.IsError || !strings.Contains(res.Content, "雨夜") {
			t.Errorf("详情=%+v", res)
		}
		res = b.tool(t, tok, "canvas_get_state", map[string]any{"nodeIds": []string{"ghost"}})
		if !res.IsError || !strings.Contains(res.Content, "ghost") {
			t.Errorf("读不存在的节点应是工具错误: %+v", res)
		}
	})

	t.Run("canvas_apply_ops：写入画布，返回 id 映射；不合法时列出全部问题", func(t *testing.T) {
		b := newBridgeEnv(t)
		tok, _ := b.token(t, "all")
		res := b.tool(t, tok, "canvas_apply_ops", map[string]any{"ops": []map[string]any{
			{"op": "create_node", "tempId": "a", "kind": "image", "label": "角色"},
			{"op": "connect", "source": "n_script", "target": "a"}}})
		if res.IsError || res.Terminate || !strings.Contains(res.Content, `"id_map"`) || !strings.Contains(res.Content, `"a"`) {
			t.Fatalf("res=%+v", res)
		}
		if len(mustParseGraph(t, b.repo).Nodes) != 2 {
			t.Error("画布应写入")
		}
		bad := b.tool(t, tok, "canvas_apply_ops", map[string]any{"ops": []map[string]any{
			{"op": "update_node", "id": "ghost", "label": "x"}, {"op": "create_node", "kind": "podcast"}}})
		if !bad.IsError || !strings.Contains(bad.Content, "第 1 项") || !strings.Contains(bad.Content, "第 2 项") {
			t.Errorf("应一次列出全部问题: %+v", bad)
		}
	})

	t.Run("canvas_arrange", func(t *testing.T) {
		b := newBridgeEnv(t)
		tok, _ := b.token(t, "all")
		res := b.tool(t, tok, "canvas_arrange", map[string]any{"nodeIds": []string{"n_script"}, "layout": "row"})
		if res.IsError {
			t.Fatalf("res=%+v", res)
		}
		bad := b.tool(t, tok, "canvas_arrange", map[string]any{"nodeIds": []string{"n_script"}, "layout": "spiral"})
		if !bad.IsError {
			t.Error("未知排列方式应是工具错误")
		}
	})

	t.Run("canvas_delete：只建审批，不删；运行进入等待，工具返回终止标记", func(t *testing.T) {
		b := newBridgeEnv(t)
		tok, run := b.token(t, "all")
		res := b.tool(t, tok, "canvas_delete", map[string]any{"nodeIds": []string{"n_script"}, "reason": "废稿"})
		if res.IsError || !res.Terminate {
			t.Fatalf("res=%+v", res)
		}
		if mustParseGraph(t, b.repo).Node("n_script") == nil {
			t.Error("审批前不能删")
		}
		if b.run(run.ID).Status != model.RunWaitingApproval {
			t.Errorf("status=%s", b.run(run.ID).Status)
		}
		ghost := newBridgeEnv(t)
		tok2, _ := ghost.token(t, "all")
		if r := ghost.tool(t, tok2, "canvas_delete", map[string]any{"nodeIds": []string{"ghost"}}); !r.IsError || r.Terminate {
			t.Errorf("删不存在的节点应是普通工具错误，不进入审批: %+v", r)
		}
	})

	t.Run("ask_user：校验选项，建提问，运行进入 waiting_input", func(t *testing.T) {
		b := newBridgeEnv(t)
		tok, run := b.token(t, "all")
		if r := b.tool(t, tok, "ask_user", map[string]any{"question": "画幅？", "kind": "choice", "options": []string{"16:9"}}); !r.IsError {
			t.Errorf("选项少于 2 个应报错: %+v", r)
		}
		res := b.tool(t, tok, "ask_user", map[string]any{"question": "画幅？", "kind": "choice", "options": []string{"16:9", "9:16"}})
		if res.IsError || !res.Terminate || b.run(run.ID).Status != model.RunWaitingInput {
			t.Fatalf("res=%+v status=%s", res, b.run(run.ID).Status)
		}
	})

	t.Run("ask_user 选模型：列出已发布的该类模型", func(t *testing.T) {
		b := newBridgeEnv(t)
		tok, _ := b.token(t, "all")
		res := b.tool(t, tok, "ask_user", map[string]any{"question": "用哪个图片模型？", "kind": "model", "modelKind": "image"})
		if res.IsError || !res.Terminate {
			t.Fatalf("res=%+v", res)
		}
		st := b.repo.st()
		st.mu.Lock()
		defer st.mu.Unlock()
		found := false
		for _, a := range st.approvals {
			if strings.Contains(string(a.PayloadJSON), "seedream") {
				found = true
			}
		}
		if !found {
			t.Error("提问里应带出可选的图片模型")
		}
	})

	t.Run("plan_update：校验并记事件", func(t *testing.T) {
		b := newBridgeEnv(t)
		tok, _ := b.token(t, "all")
		if r := b.tool(t, tok, "plan_update", map[string]any{"steps": []map[string]any{{"title": "拆镜头", "status": "weird"}}}); !r.IsError {
			t.Error("状态不认识应报错")
		}
		if r := b.tool(t, tok, "plan_update", map[string]any{"steps": []map[string]any{{"title": "拆镜头", "status": "doing"}, {"title": "出图", "status": "todo"}}}); r.IsError {
			t.Fatalf("r=%+v", r)
		}
		found := false
		for _, ty := range b.persistedTypes() {
			found = found || ty == "plan.updated"
		}
		if !found {
			t.Error("应记 plan.updated 事件，前端据此显示置顶计划")
		}
	})

	t.Run("model_list：返回已发布的生成模型", func(t *testing.T) {
		b := newBridgeEnv(t)
		tok, _ := b.token(t, "all")
		res := b.tool(t, tok, "model_list", map[string]any{"kind": "image"})
		if res.IsError || !strings.Contains(res.Content, "seedream") || strings.Contains(res.Content, `"claude"`) {
			t.Errorf("res=%+v", res)
		}
	})

	t.Run("每次工具调用记 tool.start / tool.end，步数加一", func(t *testing.T) {
		b := newBridgeEnv(t)
		tok, run := b.token(t, "all")
		b.tool(t, tok, "canvas_get_state", map[string]any{})
		types := b.persistedTypes()
		if types[len(types)-2] != "tool.start" || types[len(types)-1] != "tool.end" {
			t.Errorf("events=%v", types)
		}
		if b.run(run.ID).Steps != 1 {
			t.Errorf("steps=%d", b.run(run.ID).Steps)
		}
	})

	t.Run("步数用到上限：运行转为 step_limit，工具返回终止标记", func(t *testing.T) {
		b := newBridgeEnv(t)
		tok, run := b.token(t, "all")
		b.repo.mu.Lock()
		b.repo.runs[run.ID].Steps = 40
		b.repo.mu.Unlock()
		res := b.tool(t, tok, "canvas_get_state", map[string]any{})
		if !res.IsError || !res.Terminate || b.run(run.ID).Status != model.RunStepLimit {
			t.Errorf("res=%+v status=%s", res, b.run(run.ID).Status)
		}
	})

	t.Run("未知工具", func(t *testing.T) {
		b := newBridgeEnv(t)
		tok, _ := b.token(t, "all")
		if r := b.tool(t, tok, "rm_rf", map[string]any{}); !r.IsError {
			t.Error("未知工具应是工具错误")
		}
	})
}

// ---- 任务模式 ----

func TestAgentBridge_ModeRestrictions(t *testing.T) {
	create := func(kind string) map[string]any {
		return map[string]any{"ops": []map[string]any{{"op": "create_node", "kind": kind, "label": "x"}}}
	}
	cases := []struct {
		name, mode, tool string
		args             any
		wantErr          bool
	}{
		{"剧本创编：能建文本节点", "script", "canvas_apply_ops", create("script"), false},
		{"剧本创编：不能建图片节点", "script", "canvas_apply_ops", create("image"), true},
		{"剧本创编：不能连线", "script", "canvas_apply_ops", map[string]any{"ops": []map[string]any{{"op": "connect", "source": "n_script", "target": "n_script"}}}, true},
		{"剧本创编：不能排列", "script", "canvas_arrange", map[string]any{"nodeIds": []string{"n_script"}, "layout": "row"}, true},
		{"剧本创编：不能删除", "script", "canvas_delete", map[string]any{"nodeIds": []string{"n_script"}}, true},
		{"提示词优化：能改提示词", "prompt", "canvas_apply_ops", map[string]any{"ops": []map[string]any{{"op": "update_node", "id": "n_script", "prompt": "黄昏"}}}, false},
		{"提示词优化：不能新建节点", "prompt", "canvas_apply_ops", create("image"), true},
		{"提示词优化：不能改标题", "prompt", "canvas_apply_ops", map[string]any{"ops": []map[string]any{{"op": "update_node", "id": "n_script", "label": "改名"}}}, true},
		{"提示词优化：不能删除", "prompt", "canvas_delete", map[string]any{"nodeIds": []string{"n_script"}}, true},
		{"全能创作：什么都能建", "all", "canvas_apply_ops", create("video"), false},
		{"分镜搭建：能删除（走审批）", "storyboard", "canvas_delete", map[string]any{"nodeIds": []string{"n_script"}}, false},
		{"所有模式都能读画布", "prompt", "canvas_get_state", map[string]any{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := newBridgeEnv(t)
			tok, _ := b.token(t, tc.mode)
			res := b.tool(t, tok, tc.tool, tc.args)
			if res.IsError != tc.wantErr {
				t.Errorf("IsError=%v，期望 %v：%s", res.IsError, tc.wantErr, res.Content)
			}
			if tc.wantErr && !strings.Contains(res.Content, "模式") {
				t.Errorf("应说明是当前模式不允许: %s", res.Content)
			}
		})
	}
}

// ---- 状态与收尾 ----

func TestAgentBridge_StateAndFinish(t *testing.T) {
	ctx := context.Background()

	t.Run("保存与读取会话历史", func(t *testing.T) {
		b := newBridgeEnv(t)
		tok, run := b.token(t, "all")
		if err := b.br.SaveState(ctx, tok, json.RawMessage(`[{"role":"user","content":"你好"}]`)); err != nil {
			t.Fatal(err)
		}
		got, err := b.br.LoadState(ctx, 1, run.SessionID)
		if err != nil || got != `[{"role":"user","content":"你好"}]` {
			t.Errorf("got=%q err=%v", got, err)
		}
		if err := b.br.SaveState(ctx, tok, json.RawMessage(`not json`)); err == nil {
			t.Error("不是 JSON 不能存")
		}
	})

	t.Run("正常结束：running → succeeded，令牌失效", func(t *testing.T) {
		b := newBridgeEnv(t)
		tok, run := b.token(t, "all")
		if err := b.br.Finish(ctx, tok, "done", ""); err != nil {
			t.Fatal(err)
		}
		if b.run(run.ID).Status != model.RunSucceeded {
			t.Errorf("status=%s", b.run(run.ID).Status)
		}
		if _, err := b.br.ExecuteTool(ctx, tok, "tc", "plan_update", []byte(`{}`)); !errors.Is(err, ErrBridgeToken) {
			t.Errorf("结束后令牌应失效: %v", err)
		}
	})

	t.Run("出错结束：running → failed，记下原因", func(t *testing.T) {
		b := newBridgeEnv(t)
		tok, run := b.token(t, "all")
		if err := b.br.Finish(ctx, tok, "error", "上游服务暂时不可用"); err != nil {
			t.Fatal(err)
		}
		got := b.run(run.ID)
		if got.Status != model.RunFailed || !strings.Contains(got.ErrorMessage, "上游") {
			t.Errorf("run=%+v", got)
		}
	})

	t.Run("paused：因为工具要求停下（等审批、等回答）而结束，不改运行状态，只收回令牌", func(t *testing.T) {
		b := newBridgeEnv(t)
		tok, run := b.token(t, "all")
		// 用户已经批准：Decide 把运行放回 running，而旧进程这时才报告结束
		if err := b.br.Finish(ctx, tok, "paused", ""); err != nil {
			t.Fatal(err)
		}
		if b.run(run.ID).Status != model.RunRunning {
			t.Errorf("paused 不能改运行状态（否则会误伤已经续跑的新片段）: %s", b.run(run.ID).Status)
		}
		if _, err := b.br.ExecuteTool(ctx, tok, "tc", "plan_update", []byte(`{}`)); !errors.Is(err, ErrBridgeToken) {
			t.Errorf("令牌应已收回: %v", err)
		}
	})

	t.Run("等审批 / 等回答 / 预算用尽 / 已停止：不覆盖", func(t *testing.T) {
		for _, st := range []string{model.RunWaitingApproval, model.RunWaitingInput, model.RunBudgetExhausted, model.RunStepLimit, model.RunCanceled} {
			b := newBridgeEnv(t)
			tok, run := b.token(t, "all")
			b.setStatus(run.ID, st)
			if err := b.br.Finish(ctx, tok, "error", "x"); err != nil {
				t.Fatal(err)
			}
			if b.run(run.ID).Status != st {
				t.Errorf("%s 不该被覆盖: %s", st, b.run(run.ID).Status)
			}
		}
	})

	t.Run("令牌是否有效：结束后失效，进程退出时据此区分正常结束和崩溃", func(t *testing.T) {
		b := newBridgeEnv(t)
		tok, _ := b.token(t, "all")
		if !b.br.TokenActive(tok) {
			t.Fatal("刚签发的令牌应有效")
		}
		_ = b.br.Finish(ctx, tok, "done", "")
		if b.br.TokenActive(tok) {
			t.Error("Finish 之后令牌应失效")
		}
	})

	t.Run("queued → running", func(t *testing.T) {
		b := newBridgeEnv(t)
		v := b.start(t, "x")
		run := b.run(uint64(v.ID))
		if err := b.br.MarkRunning(ctx, &run); err != nil {
			t.Fatal(err)
		}
		if b.run(run.ID).Status != model.RunRunning {
			t.Errorf("status=%s", b.run(run.ID).Status)
		}
		if err := b.br.MarkRunning(ctx, &run); err == nil {
			t.Error("已经在跑的运行不能再标一次")
		}
	})
}
