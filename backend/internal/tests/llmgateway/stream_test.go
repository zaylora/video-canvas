package llmgateway_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"video-canvas/internal/llmgateway"
)

func TestStream_TextDeltasUsageAndRequestShape(t *testing.T) {
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		sse(w,
			chunk(`{"role":"assistant","content":""}`, ""),
			chunk(`{"content":"你好，"}`, ""),
			chunk(`{"content":"林夏"}`, ""),
			chunk(`{}`, "stop"),
			usageChunk(120, 8, 100),
			"[DONE]")
	})
	emit, events := collect()
	tg := target(up)
	tg.BaseURL += "/" // 末尾斜杠不能导致 // 路径
	maxTok := 2048
	res, err := gateway().Stream(ctxTimeout(t, 5*time.Second), tg, llmgateway.Request{
		System:    "你是画布助手",
		Messages:  []llmgateway.Message{userMsg("拆分镜")},
		MaxTokens: maxTok,
		Tools:     []llmgateway.ToolDef{{Name: "canvas_get_state", Description: "读画布", Parameters: []byte(`{"type":"object","properties":{}}`)}},
	}, emit)
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "你好，林夏" || res.FinishReason != "stop" {
		t.Fatalf("res=%+v", res)
	}
	if res.Usage.InputTokens != 120 || res.Usage.OutputTokens != 8 || res.Usage.CachedTokens != 100 || res.UsageMissing {
		t.Errorf("usage=%+v missing=%v", res.Usage, res.UsageMissing)
	}
	var texts []string
	for _, e := range events() {
		if e.Type == llmgateway.EventText {
			texts = append(texts, e.Text)
		}
	}
	if len(texts) != 2 || texts[0] != "你好，" || texts[1] != "林夏" {
		t.Errorf("文本增量应逐段到达（不要攒成一次）: %v", texts)
	}

	reqs := up.requests()
	if len(reqs) != 1 {
		t.Fatalf("应只发一次请求: %d", len(reqs))
	}
	r := reqs[0]
	if r.Path != "/v1/chat/completions" || r.Auth != "Bearer sk-secret-key" {
		t.Errorf("path=%q auth=%q", r.Path, r.Auth)
	}
	b := r.Body
	if b["model"] != "gpt-up" || b["stream"] != true || b["max_tokens"] != float64(maxTok) {
		t.Errorf("body=%v", b)
	}
	if so, _ := b["stream_options"].(map[string]any); so["include_usage"] != true {
		t.Errorf("必须请求带用量，计费靠它: %v", b["stream_options"])
	}
	msgs := b["messages"].([]any)
	if len(msgs) != 2 || msgs[0].(map[string]any)["role"] != "system" || msgs[0].(map[string]any)["content"] != "你是画布助手" {
		t.Errorf("系统提示应在最前: %v", msgs)
	}
	tools := b["tools"].([]any)
	fn := tools[0].(map[string]any)["function"].(map[string]any)
	if tools[0].(map[string]any)["type"] != "function" || fn["name"] != "canvas_get_state" || fn["parameters"] == nil {
		t.Errorf("tools=%v", tools)
	}
}

func TestStream_ToolCallsAssembledFromFragments(t *testing.T) {
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		sse(w,
			chunk(`{"role":"assistant","tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"canvas_apply_ops","arguments":""}}]}`, ""),
			chunk(`{"tool_calls":[{"index":0,"function":{"arguments":"{\"ops\":[{\"op\":"}}]}`, ""),
			chunk(`{"tool_calls":[{"index":1,"id":"call_b","type":"function","function":{"name":"plan_update","arguments":"{\"steps\":[]}"}}]}`, ""),
			chunk(`{"tool_calls":[{"index":0,"function":{"arguments":"\"connect\"}]}"}}]}`, ""),
			chunk(`{}`, "tool_calls"),
			usageChunk(50, 30, 0),
			"[DONE]")
	})
	emit, events := collect()
	res, err := gateway().Stream(ctxTimeout(t, 5*time.Second), target(up), llmgateway.Request{Messages: []llmgateway.Message{userMsg("x")}}, emit)
	if err != nil {
		t.Fatal(err)
	}
	if res.FinishReason != "tool_calls" || len(res.ToolCalls) != 2 {
		t.Fatalf("res=%+v", res)
	}
	a, b := res.ToolCalls[0], res.ToolCalls[1]
	if a.ID != "call_a" || a.Name != "canvas_apply_ops" || a.Arguments != `{"ops":[{"op":"connect"}]}` {
		t.Errorf("第一个工具调用的参数应按序号拼起来: %+v", a)
	}
	if b.ID != "call_b" || b.Name != "plan_update" || b.Arguments != `{"steps":[]}` {
		t.Errorf("b=%+v", b)
	}
	var deltas int
	for _, e := range events() {
		if e.Type == llmgateway.EventToolDelta {
			deltas++
		}
	}
	if deltas < 4 {
		t.Errorf("工具参数应边到边转出去，实际 %d 次", deltas)
	}
}

func TestStream_ReasoningBecomesThinkingEvents(t *testing.T) {
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		sse(w, chunk(`{"reasoning_content":"先看看画布"}`, ""), chunk(`{"content":"好的"}`, ""), chunk(`{}`, "stop"), "[DONE]")
	})
	emit, events := collect()
	res, err := gateway().Stream(ctxTimeout(t, 5*time.Second), target(up), llmgateway.Request{Messages: []llmgateway.Message{userMsg("x")}}, emit)
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "好的" || res.Thinking != "先看看画布" {
		t.Errorf("思考内容不应混进正文: %+v", res)
	}
	if !res.UsageMissing {
		t.Error("上游没给用量时要标出来，计费要走估算")
	}
	var think int
	for _, e := range events() {
		if e.Type == llmgateway.EventThinking {
			think++
		}
	}
	if think != 1 {
		t.Errorf("thinking 事件 %d", think)
	}
}

func TestStream_RequestMessagesSerialization(t *testing.T) {
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		sse(w, chunk(`{"content":"ok"}`, "stop"), "[DONE]")
	})
	_, err := gateway().Stream(ctxTimeout(t, 5*time.Second), target(up), llmgateway.Request{Messages: []llmgateway.Message{
		{Role: llmgateway.RoleUser, Parts: []llmgateway.Part{{Text: "看看这张图"}, {ImageURL: "data:image/png;base64,AAAA"}}},
		{Role: llmgateway.RoleAssistant, ToolCalls: []llmgateway.ToolCall{{ID: "call_1", Name: "canvas_get_state", Arguments: `{}`}}},
		{Role: llmgateway.RoleTool, ToolCallID: "call_1", Text: `{"nodes":[]}`},
	}}, func(llmgateway.Event) {})
	if err != nil {
		t.Fatal(err)
	}
	msgs := up.requests()[0].Body["messages"].([]any)
	user := msgs[0].(map[string]any)["content"].([]any)
	if user[0].(map[string]any)["type"] != "text" || user[1].(map[string]any)["type"] != "image_url" ||
		user[1].(map[string]any)["image_url"].(map[string]any)["url"] != "data:image/png;base64,AAAA" {
		t.Errorf("多模态内容应是 text + image_url 分段: %v", user)
	}
	asst := msgs[1].(map[string]any)
	tc := asst["tool_calls"].([]any)[0].(map[string]any)
	if tc["id"] != "call_1" || tc["type"] != "function" || tc["function"].(map[string]any)["name"] != "canvas_get_state" {
		t.Errorf("assistant.tool_calls=%v", asst["tool_calls"])
	}
	tool := msgs[2].(map[string]any)
	if tool["role"] != "tool" || tool["tool_call_id"] != "call_1" || tool["content"] != `{"nodes":[]}` {
		t.Errorf("tool 消息=%v", tool)
	}
}

func TestStream_ToleratesProtocolVariants(t *testing.T) {
	t.Run("注释行、event 行、没有 [DONE] 但有 finish_reason 后关闭", func(t *testing.T) {
		up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(": keep-alive\n\nevent: message\ndata: " + chunk(`{"content":"A"}`, "") + "\n\ndata: " + chunk(`{}`, "stop") + "\n\n"))
		})
		res, err := gateway().Stream(ctxTimeout(t, 5*time.Second), target(up), llmgateway.Request{Messages: []llmgateway.Message{userMsg("x")}}, func(llmgateway.Event) {})
		if err != nil || res.Text != "A" {
			t.Fatalf("res=%+v err=%v", res, err)
		}
	})

	t.Run("上游无视 stream 返回整段 JSON：照样能用", func(t *testing.T) {
		up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"整段","tool_calls":[{"id":"c9","type":"function","function":{"name":"plan_update","arguments":"{}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`))
		})
		emit, events := collect()
		res, err := gateway().Stream(ctxTimeout(t, 5*time.Second), target(up), llmgateway.Request{Messages: []llmgateway.Message{userMsg("x")}}, emit)
		if err != nil {
			t.Fatal(err)
		}
		if res.Text != "整段" || len(res.ToolCalls) != 1 || res.ToolCalls[0].Name != "plan_update" || res.Usage.InputTokens != 10 {
			t.Errorf("res=%+v", res)
		}
		if len(events()) == 0 {
			t.Error("整段响应也应转成事件，调用方只认事件")
		}
	})
}

func TestStream_CanceledByCaller(t *testing.T) {
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		sse(w, chunk(`{"content":"开头"}`, ""))
		<-r.Context().Done() // 之后一直不发数据，等调用方断开
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// 客户端真正收到第一段文字时才取消，这才是「流到一半被取消」
	emit := func(e llmgateway.Event) {
		if e.Type == llmgateway.EventText {
			cancel()
		}
	}
	res, err := gateway().Stream(ctx, target(up), llmgateway.Request{Messages: []llmgateway.Message{userMsg("x")}}, emit)
	if err == nil || ctx.Err() == nil {
		t.Fatalf("取消应返回错误: %v", err)
	}
	if res == nil || res.Text != "开头" {
		t.Errorf("取消时要保留已收到的部分，计费还要按它结算: %+v", res)
	}
}
