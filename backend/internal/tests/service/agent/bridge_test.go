package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"video-canvas/internal/llmgateway"
	"video-canvas/internal/model"
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
	req    llmgateway.Request
}

func (s *fakeStreamer) Stream(ctx context.Context, tg llmgateway.Target, req llmgateway.Request, emit func(llmgateway.Event)) (*llmgateway.Result, error) {
	s.calls++
	s.tg, s.req = tg, req
	return s.script(ctx, emit, func([]byte) {})
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
	webAccess provider.WebAccess
	bill      *fakeBillingRepo
	reg       *fakeBridgeRegistry
	str       *fakeStreamer
	br        *AgentBridge
	rcs       map[string]RunContext // token() 发的句柄 → 运行上下文
	now       *time.Time
}

func newBridgeEnv(t *testing.T) *bridgeEnv { return newBridgeEnvWith(t, nil, &fakeWeb{}) }

// newBridgeEnvWithSkills 同 newBridgeEnv，并给桥配一个技能库（nil 表示只有内置技能）。
func newBridgeEnvWithSkills(t *testing.T, lib SkillLibrary) *bridgeEnv {
	t.Helper()
	return newBridgeEnvWith(t, lib, &fakeWeb{})
}

// newBridgeEnvWith 同 newBridgeEnv，可以指定技能库和联网能力（web 传 nil 表示没有联网能力）。
func newBridgeEnvWith(t *testing.T, lib SkillLibrary, web provider.WebAccess) *bridgeEnv {
	t.Helper()
	e := newAgentEnv(t)
	bill := &fakeBillingRepo{available: 100}
	caps := billingCaps
	caps.Vision = true
	reg := &fakeBridgeRegistry{snap: &provider.Snapshot{
		Model:   provider.ModelSnapshot{Key: "claude", Kind: "agent", UpstreamModel: "claude-up", Pricing: billingPricing, Capabilities: caps},
		Channel: provider.ChannelSnapshot{Key: "ch1", BaseURL: "https://llm.example", RateLimit: provider.RateLimit{RPS: 2, MaxConcurrency: 3}},
	}, models: []provider.ModelInfo{
		{Key: "seedream", Kind: "image", Label: "Seedream", Hint: "图片模型", Capabilities: modelcfg.Capabilities{Ops: []string{"t2i"}}},
		{Key: "claude", Kind: "agent", Label: "Claude"},
	}}
	str := &fakeStreamer{script: okStream("好的", llmgateway.Usage{InputTokens: 1_000_000, OutputTokens: 100_000})}
	now := e.now
	br := NewAgentBridge(BridgeDeps{
		Repo: e.repo, Canvas: NewAgentCanvasService(e.repo, e.bc), Agent: e.svc, Billing: NewAgentBilling(bill),
		Registry: reg, Secrets: fakeSecrets{"channel:ch1": "sk-test"}, Streamer: str, Skills: lib, Web: web,
		Now: func() time.Time { return now }, RunUsage: &fakeRunUsage{bill: bill},
	})
	return &bridgeEnv{agentEnv: e, bill: bill, reg: reg, str: str, br: br, now: &now, webAccess: web}
}

// web 取注入的假联网能力。
func (b *bridgeEnv) web() *fakeWeb { return b.webAccess.(*fakeWeb) }

// token 起一个运行中的运行，并返回它在平台 API 里的「句柄」（保存好的运行上下文）：之后的 tool 调用都用它。
func (b *bridgeEnv) token(t *testing.T, mode string, selection ...string) (string, *model.AgentRun) {
	t.Helper()
	run := b.runningRun(t)
	key := fmt.Sprintf("run-%d", run.ID)
	if b.rcs == nil {
		b.rcs = map[string]RunContext{}
	}
	b.rcs[key] = RunContext{RunID: run.ID, UserID: run.UserID, SessionID: run.SessionID, CanvasID: run.CanvasID, Mode: firstNonEmptyStr(mode, model.AgentModeAll), ModelKey: "claude", Selection: selection}
	return key, run
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// tool 经平台 API 执行一次工具调用（句柄见 token）。
func (b *bridgeEnv) tool(t *testing.T, tok, name string, args any) *ToolResult {
	t.Helper()
	raw, _ := json.Marshal(args)
	res, err := b.br.ExecutePlatformTool(context.Background(), b.rcs[tok], "tc-"+name, name, raw)
	if err != nil {
		t.Fatalf("ExecutePlatformTool(%s): %v", name, err)
	}
	return res
}

// ---- 令牌 ----

// ---- 模型代理 ----

func TestProxyModelPlatform_PassesGeminiFormat(t *testing.T) {
	for _, format := range []string{"", "openai", "gemini"} {
		t.Run("接口格式="+format, func(t *testing.T) {
			b := newBridgeEnv(t)
			b.reg.snap.Model.APIFormat = format
			rr := &fakeRequestRecorder{}
			b.br.SetModelRequestRecorder(rr)
			body := []byte(`{"model":"agent-model","stream":false,"max_tokens":999999,"messages":[{"role":"user","content":[{"type":"text","text":"看看这张图"},{"type":"image_url","image_url":{"url":"https://oss.example.com/a.png?X-Amz-Signature=secret"}}]}],"junk":"不在白名单"}`)
			if err := b.br.ProxyModelPlatform(context.Background(), platformCtx(900120, "all"), body, nil, func([]byte) {}); err != nil {
				t.Fatal(err)
			}
			if b.str.tg.Format != format {
				t.Errorf("目标接口格式=%q，应取快照的 %q", b.str.tg.Format, format)
			}
			rec := rr.wait(t, 1)[0]
			var params map[string]any
			if err := json.Unmarshal(rec.Params, &params); err != nil {
				t.Fatal(err)
			}
			if got, has := params["api_format"]; has != (format != "") || has && got != format {
				t.Errorf("摘要接口格式=%v，存在=%v，快照=%q", got, has, format)
			}
			var saved map[string]any
			if err := json.Unmarshal([]byte(gunzip(t, rec.Body)), &saved); err != nil {
				t.Fatal(err)
			}
			if saved["model"] != "claude-up" || saved["stream"] != true || saved["max_tokens"] != float64(b.reg.snap.Model.Capabilities.Context.Output) {
				t.Errorf("留存应经过 SanitizeRaw 收紧模型、流式与最大输出: %v", saved)
			}
			for _, key := range []string{"contents", "systemInstruction", "junk", "api_format"} {
				if _, has := saved[key]; has {
					t.Errorf("留存体不应含 %s", key)
				}
			}
			messages, ok := saved["messages"].([]any)
			if !ok || len(messages) != 1 {
				t.Fatalf("留存应是 OpenAI 消息: %v", saved)
			}
			message := messages[0].(map[string]any)
			parts := message["content"].([]any)
			if parts[0].(map[string]any)["text"] != "看看这张图" {
				t.Errorf("留存应保留用户文字: %v", message)
			}
			image := parts[1].(map[string]any)["image_url"].(map[string]any)
			if image["url"] != fmt.Sprintf("[已省略 %d 字符]", len("https://oss.example.com/a.png?X-Amz-Signature=secret")) {
				t.Errorf("留存图片应脱敏签名 URL: %v", image)
			}
		})
	}
}

// ---- 工具 ----

// ---- 任务模式 ----

// ---- 状态与收尾 ----

// toolEndSummary 取工具结果里 tool.end 的摘要（前端工具行、导出和统计读它）。
func toolEndSummary(res *ToolResult) string {
	s, _ := res.ToolEnd["summary"].(string)
	return s
}
