package llmgateway_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"video-canvas/internal/llmgateway"
	"video-canvas/internal/provider/netguard"
)

const geminiUserBody = `{"messages":[{"role":"user","content":"x"}]}`

func geminiTarget(up *upstream) llmgateway.Target {
	tg := target(up)
	tg.Format = llmgateway.FormatGemini
	return tg
}

func fromGemini(t *testing.T, chunks ...string) (*llmgateway.Result, string, []llmgateway.Event) {
	t.Helper()
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) { sse(w, chunks...) })
	emit, events := collect()
	var raw bytes.Buffer
	res, err := gateway().StreamRaw(ctxTimeout(t, 5*time.Second), geminiTarget(up), []byte(geminiUserBody), llmgateway.RawLimits{}, emit, func(b []byte) { raw.Write(b) })
	if err != nil {
		t.Fatal(err)
	}
	return res, raw.String(), events()
}

func openAIChunks(t *testing.T, raw string) []map[string]any {
	t.Helper()
	var chunks []map[string]any
	for _, line := range strings.Split(raw, "\n") {
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			continue
		}
		var value map[string]any
		if err := json.Unmarshal([]byte(data), &value); err != nil {
			t.Fatalf("非 OpenAI JSON: %v", err)
		}
		if value["candidates"] != nil {
			t.Fatal("不能透传 Gemini 形状")
		}
		chunks = append(chunks, value)
	}
	return chunks
}

func TestFromGemini_TextStream(t *testing.T) {
	res, raw, events := fromGemini(t, `{"candidates":[{"content":{"parts":[{"text":"你"}]}}]}`, `{"candidates":[{"content":{"parts":[{"text":"好"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":2}}`)
	if res.Text != "你好" || res.FinishReason != "stop" || res.UsageMissing {
		t.Fatalf("res=%+v", res)
	}
	var texts []string
	for _, e := range events {
		if e.Type == llmgateway.EventText {
			texts = append(texts, e.Text)
		}
	}
	if !reflect.DeepEqual(texts, []string{"你", "好"}) {
		t.Fatalf("文本需要逐段到达: %v", texts)
	}
	chunks := openAIChunks(t, raw)
	if len(chunks) != 4 || chunks[0]["choices"].([]any)[0].(map[string]any)["delta"].(map[string]any)["content"] != "你" || chunks[2]["choices"].([]any)[0].(map[string]any)["finish_reason"] != "stop" || chunks[3]["usage"] == nil || !strings.HasSuffix(strings.TrimSpace(raw), "data: [DONE]") {
		t.Fatalf("raw=%s", raw)
	}
}

func TestFromGemini_ThoughtBecomesReasoning(t *testing.T) {
	res, raw, _ := fromGemini(t, `{"candidates":[{"content":{"parts":[{"text":"推理","thought":true},{"text":"答案"}]},"finishReason":"STOP"}]}`)
	if res.Thinking != "推理" || res.Text != "答案" || !strings.Contains(raw, `"reasoning_content":"推理"`) || !res.UsageMissing || strings.Contains(raw, `"usage":`) {
		t.Fatalf("res=%+v raw=%s", res, raw)
	}
}

var geminiIDPattern = regexp.MustCompile(`^gc_[a-z0-9]{8}(_[A-Za-z0-9_-]+)?$`)

func assertGeminiCallID(t *testing.T, id, signature string) {
	t.Helper()
	if !geminiIDPattern.MatchString(id) || strings.Contains(id, "|") {
		t.Fatalf("不合法工具 id: %q", id)
	}
	if signature == "" {
		if len(id) != 11 {
			t.Fatalf("无签名 id 应只有随机部分: %q", id)
		}
		return
	}
	b, err := base64.RawURLEncoding.DecodeString(id[12:])
	if err != nil || string(b) != signature {
		t.Fatalf("签名未原样编码: %q err=%v", b, err)
	}
}

func TestFromGemini_FunctionCall(t *testing.T) {
	res, raw, _ := fromGemini(t, `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"x","args":{"n":1}},"thoughtSignature":"sig+/="}]},"finishReason":"STOP"}]}`)
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].Name != "x" || res.ToolCalls[0].Arguments != `{"n":1}` || res.FinishReason != "tool_calls" {
		t.Fatalf("res=%+v", res)
	}
	assertGeminiCallID(t, res.ToolCalls[0].ID, "sig+/=")
	call := openAIChunks(t, raw)[0]["choices"].([]any)[0].(map[string]any)["delta"].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)
	if call["index"] != float64(0) || call["type"] != "function" || call["id"] != res.ToolCalls[0].ID {
		t.Fatalf("call=%v", call)
	}
}

func TestFromGemini_ParallelCallsSignatureOnFirst(t *testing.T) {
	res, raw, events := fromGemini(t, `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"first","args":{}},"thoughtSignature":"sig"},{"functionCall":{"name":"second","args":{}}}]}}]}`, `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"third","args":{}}}]},"finishReason":"STOP"}]}`)
	if len(res.ToolCalls) != 3 || res.ToolCalls[0].ID == res.ToolCalls[1].ID || res.ToolCalls[1].ID == res.ToolCalls[2].ID {
		t.Fatalf("calls=%v", res.ToolCalls)
	}
	seen := map[string]bool{}
	var indices []int
	for i, call := range res.ToolCalls {
		sig := ""
		if i == 0 {
			sig = "sig"
		}
		assertGeminiCallID(t, call.ID, sig)
		if seen[call.ID] {
			t.Fatal("id 重复")
		}
		seen[call.ID] = true
	}
	for _, e := range events {
		if e.Type == llmgateway.EventToolDelta {
			indices = append(indices, e.ToolIndex)
		}
	}
	if !reflect.DeepEqual(indices, []int{0, 1, 2}) {
		t.Fatalf("index 需跨块递增: %v raw=%s", indices, raw)
	}
	// 按 pi 的白名单形状重放历史，验证签名只回到原来第一个调用。
	calls := make([]any, 0, 3)
	for _, call := range res.ToolCalls {
		calls = append(calls, map[string]any{"id": call.ID, "type": "function", "function": map[string]any{"name": call.Name, "arguments": call.Arguments}})
	}
	body, _ := json.Marshal(map[string]any{"messages": []any{map[string]any{"role": "assistant", "content": nil, "tool_calls": calls}}})
	parts := contentParts(t, geminiRequest(t, string(body)), 0)
	for i, p := range parts {
		got, _ := p.(map[string]any)["thoughtSignature"].(string)
		want := ""
		if i == 0 {
			want = "sig"
		}
		if got != want {
			t.Fatalf("part[%d] 签名=%q", i, got)
		}
	}
}

func TestFromGemini_FinishReasonMapping(t *testing.T) {
	for _, tc := range []struct{ reason, want string }{{"STOP", "stop"}, {"MAX_TOKENS", "length"}, {"SAFETY", "content_filter"}, {"RECITATION", "content_filter"}, {"BLOCKLIST", "content_filter"}, {"PROHIBITED_CONTENT", "content_filter"}, {"SPII", "content_filter"}, {"OTHER", "content_filter"}} {
		t.Run(tc.reason, func(t *testing.T) {
			res, _, _ := fromGemini(t, fmt.Sprintf(`{"candidates":[{"finishReason":%q}]}`, tc.reason))
			if res.FinishReason != tc.want {
				t.Fatalf("res=%+v", res)
			}
		})
	}
}

func TestFromGemini_UsageMapping(t *testing.T) {
	res, raw, _ := fromGemini(t, `{"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1}}`, `{"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":4,"thoughtsTokenCount":2,"cachedContentTokenCount":3}}`)
	if res.Usage != (llmgateway.Usage{InputTokens: 10, OutputTokens: 6, CachedTokens: 3}) || res.UsageMissing {
		t.Fatalf("res=%+v", res)
	}
	chunks := openAIChunks(t, raw)
	usage := chunks[len(chunks)-1]["usage"].(map[string]any)
	if usage["completion_tokens"] != float64(6) || usage["prompt_tokens_details"].(map[string]any)["cached_tokens"] != float64(3) {
		t.Fatalf("usage=%v", usage)
	}
}

func TestFromGemini_EOFWithoutFinishReason(t *testing.T) {
	res, raw, _ := fromGemini(t, `{"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":4}}`, `{"candidates":[{"content":{"parts":[{"text":"正常结束"}]}}]}`)
	if res.FinishReason != "stop" || res.UsageMissing || res.Usage.OutputTokens != 4 || !strings.Contains(raw, `"finish_reason":"stop"`) || !strings.Contains(raw, "data: [DONE]") {
		t.Fatalf("res=%+v raw=%s", res, raw)
	}
}

func TestFromGemini_EmptyStreamIsTruncated(t *testing.T) {
	for _, body := range []string{"", ": heartbeat\n\n", "data: invalid\n\n"} {
		t.Run(body, func(t *testing.T) {
			up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, body)
			})
			_, raw, err := streamRaw(t, gateway(), geminiTarget(up), geminiUserBody, 0)
			if !errors.Is(err, llmgateway.ErrStreamTruncated) || strings.Contains(raw, "[DONE]") {
				t.Fatalf("err=%v raw=%s", err, raw)
			}
		})
	}
}

func TestFromGemini_PromptBlocked(t *testing.T) {
	res, raw, _ := fromGemini(t, `{"promptFeedback":{"blockReason":"SAFETY"},"usageMetadata":{"promptTokenCount":2}}`)
	if res.FinishReason != "content_filter" || res.Text != "请求被上游安全策略拦截（SAFETY）" || !strings.Contains(raw, `"finish_reason":"content_filter"`) {
		t.Fatalf("res=%+v raw=%s", res, raw)
	}
}

func TestFromGemini_WholeJSON(t *testing.T) {
	for _, withUsage := range []bool{true, false} {
		t.Run(fmt.Sprint(withUsage), func(t *testing.T) {
			usage := ""
			if withUsage {
				usage = `,"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":4,"thoughtsTokenCount":2,"cachedContentTokenCount":3}`
			}
			up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"candidates":[{"content":{"parts":[{"text":"思考","thought":true},{"text":"整段"},{"functionCall":{"name":"x","args":{}},"thoughtSignature":"whole-sig"}]},"finishReason":"STOP"}]`+usage+`}`)
			})
			res, raw, err := streamRaw(t, gateway(), geminiTarget(up), geminiUserBody, 0)
			if err != nil {
				t.Fatal(err)
			}
			if res.Text != "整段" || res.Thinking != "思考" || len(res.ToolCalls) != 1 || res.FinishReason != "tool_calls" || res.UsageMissing == withUsage {
				t.Fatalf("res=%+v", res)
			}
			if withUsage && res.Usage != (llmgateway.Usage{InputTokens: 10, OutputTokens: 6, CachedTokens: 3}) {
				t.Fatalf("usage=%+v", res.Usage)
			}
			assertGeminiCallID(t, res.ToolCalls[0].ID, "whole-sig")
			openAIChunks(t, raw)
			for _, want := range []string{`"content":"整段"`, `"reasoning_content":"思考"`, `"name":"x"`, `"finish_reason":"tool_calls"`, "data: [DONE]"} {
				if !strings.Contains(raw, want) {
					t.Fatalf("raw 缺少 %s: %s", want, raw)
				}
			}
		})
	}
}

func TestGemini_EndToEndMatchesOpenAIResult(t *testing.T) {
	openai := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		sse(w, chunk(`{"content":"结果","reasoning_content":"推理","tool_calls":[{"index":0,"id":"call_x","type":"function","function":{"name":"x","arguments":"{\"n\":1}"}}]}`, "tool_calls"), usageChunk(10, 6, 3), "[DONE]")
	})
	gemini := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		sse(w, `{"candidates":[{"content":{"parts":[{"text":"推理","thought":true},{"text":"结果"},{"functionCall":{"name":"x","args":{"n":1}}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":4,"thoughtsTokenCount":2,"cachedContentTokenCount":3}}`)
	})
	a, _, err := streamRaw(t, gateway(), target(openai), geminiUserBody, 0)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := streamRaw(t, gateway(), geminiTarget(gemini), geminiUserBody, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.ToolCalls) != 1 || len(b.ToolCalls) != 1 {
		t.Fatalf("a=%+v b=%+v", a, b)
	}
	a.ToolCalls[0].ID = ""
	b.ToolCalls[0].ID = ""
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("两种协议应得到相同业务结果: a=%+v b=%+v", a, b)
	}
}

func TestGemini_SignatureRoundTrip(t *testing.T) {
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		sse(w, `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"x","args":{}},"thoughtSignature":"round+/=_签名"},{"functionCall":{"name":"y","args":{}}}]},"finishReason":"STOP"}]}`)
	})
	first, raw, err := streamRaw(t, gateway(), geminiTarget(up), geminiUserBody, 0)
	if err != nil {
		t.Fatal(err)
	}
	var calls []any
	for _, c := range openAIChunks(t, raw) {
		for _, ch := range c["choices"].([]any) {
			d, _ := ch.(map[string]any)["delta"].(map[string]any)
			if tcs, ok := d["tool_calls"].([]any); ok {
				calls = append(calls, tcs...)
			}
		}
	}
	if len(calls) != 2 || len(first.ToolCalls) != 2 {
		t.Fatalf("calls=%v", calls)
	}
	body, _ := json.Marshal(map[string]any{"messages": []any{map[string]any{"role": "user", "content": "x"}, map[string]any{"role": "assistant", "content": nil, "tool_calls": calls}, map[string]any{"role": "tool", "tool_call_id": first.ToolCalls[0].ID, "content": "ok"}, map[string]any{"role": "tool", "tool_call_id": first.ToolCalls[1].ID, "content": "ok"}}})
	if _, _, err := streamRaw(t, gateway(), geminiTarget(up), string(body), 0); err != nil {
		t.Fatal(err)
	}
	got := contentParts(t, up.requests()[1].Body, 1)
	if got[0].(map[string]any)["thoughtSignature"] != "round+/=_签名" || got[1].(map[string]any)["thoughtSignature"] != nil {
		t.Fatalf("签名位置变化: %v", got)
	}
}

func TestGemini_BlocksInternalAddresses(t *testing.T) {
	var hits atomic.Int32
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) { hits.Add(1) })
	strict := gateway(func(o *llmgateway.Options) { o.IPAllowed = nil })
	_, _, err := streamRaw(t, strict, geminiTarget(up), geminiUserBody, 0)
	if !netguard.IsGuardError(err) || hits.Load() != 0 {
		t.Fatalf("err=%v hits=%d", err, hits.Load())
	}
}

func TestGemini_DoesNotFollowRedirects(t *testing.T) {
	var hits atomic.Int32
	other := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) { hits.Add(1) })
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		http.Redirect(w, r, other.srv.URL+"/steal", http.StatusTemporaryRedirect)
	})
	_, raw, err := streamRaw(t, gateway(), geminiTarget(up), geminiUserBody, 0)
	if err == nil || hits.Load() != 0 || raw != "" {
		t.Fatalf("err=%v hits=%d raw=%s", err, hits.Load(), raw)
	}
}

func TestGemini_ErrorBodyRedactsKey(t *testing.T) {
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		w.WriteHeader(400)
		fmt.Fprint(w, `{"error":{"code":400,"message":"bad sk-secret-key","status":"INVALID_ARGUMENT"}}`)
	})
	_, raw, err := streamRaw(t, gateway(), geminiTarget(up), geminiUserBody, 0)
	var ue *llmgateway.UpstreamError
	if !errors.As(err, &ue) || ue.Status != 400 || strings.Contains(err.Error(), "sk-secret-key") || raw != "" {
		t.Fatalf("err=%v raw=%s", err, raw)
	}
}

func TestGemini_ErrorDoesNotExposeMedia(t *testing.T) {
	const media = "https://assets.example/private.png?signature=secret"
	for _, mode := range []string{"http", "sse", "json"} {
		t.Run(mode, func(t *testing.T) {
			up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
				payload, _ := json.Marshal(map[string]any{"error": map[string]any{"code": 400, "message": "bad sk-secret-key " + media}})
				if mode == "sse" {
					sse(w, string(payload))
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if mode == "http" {
					w.WriteHeader(400)
				}
				_, _ = w.Write(payload)
			})
			body := `{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"` + media + `"}}]}]}`
			_, raw, err := streamRaw(t, gateway(), geminiTarget(up), body, 0)
			if err == nil || strings.Contains(err.Error(), "sk-secret-key") || strings.Contains(err.Error(), "assets.example") || strings.Contains(raw, "assets.example") || strings.Contains(raw, "sk-secret-key") {
				t.Fatalf("敏感内容泄露: err=%v raw=%s", err, raw)
			}
		})
	}
}

func TestGemini_CanceledKeepsPartialResult(t *testing.T) {
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		sse(w, `{"candidates":[{"content":{"parts":[{"text":"开头"}]}}]}`)
		hang(r)
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	res, err := gateway().Stream(ctx, geminiTarget(up), llmgateway.Request{Messages: []llmgateway.Message{userMsg("x")}}, func(e llmgateway.Event) {
		if e.Type == llmgateway.EventText {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) || res == nil || res.Text != "开头" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestGemini_IdleRetryAndPartialStream(t *testing.T) {
	for _, delivered := range []bool{false, true} {
		t.Run(fmt.Sprint(delivered), func(t *testing.T) {
			var hits atomic.Int32
			up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
				if hits.Add(1) == 1 {
					if delivered {
						sse(w, `{"candidates":[{"content":{"parts":[{"text":"开头"}]}}]}`)
					}
					hang(r)
					return
				}
				sse(w, `{"candidates":[{"content":{"parts":[{"text":"重试成功"}]},"finishReason":"STOP"}]}`)
			})
			res, raw, err := streamRaw(t, gateway(func(o *llmgateway.Options) { o.IdleTimeout = 100 * time.Millisecond }), geminiTarget(up), geminiUserBody, 0)
			if delivered {
				if !errors.Is(err, llmgateway.ErrIdleTimeout) || res == nil || res.Text != "开头" || hits.Load() != 1 || strings.Contains(raw, "[DONE]") {
					t.Fatalf("res=%+v err=%v hits=%d raw=%s", res, err, hits.Load(), raw)
				}
			} else if err != nil || res.Text != "重试成功" || hits.Load() != 2 {
				t.Fatalf("res=%+v err=%v hits=%d", res, err, hits.Load())
			}
		})
	}
}

func TestGemini_HeartbeatsTouchUnderlyingBytes(t *testing.T) {
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		w.Header().Set("Content-Type", "text/event-stream")
		f := w.(http.Flusher)
		ticker := time.NewTicker(40 * time.Millisecond)
		defer ticker.Stop()
		for range 6 {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				fmt.Fprint(w, ": heartbeat\n\n")
				f.Flush()
			}
		}
		sse(w, `{"candidates":[{"content":{"parts":[{"text":"仍然活跃"}]},"finishReason":"STOP"}]}`)
	})
	res, _, err := streamRaw(t, gateway(func(o *llmgateway.Options) { o.IdleTimeout = 150 * time.Millisecond }), geminiTarget(up), geminiUserBody, 0)
	if err != nil || res.Text != "仍然活跃" || len(up.requests()) != 1 {
		t.Fatalf("res=%+v err=%v 请求数=%d", res, err, len(up.requests()))
	}
}

func TestFromGemini_FinishReasonCompletesBeforeClose(t *testing.T) {
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		sse(w, `{"candidates":[{"content":{"parts":[{"text":"已完成"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":1}}`)
		hang(r)
	})
	res, raw, err := streamRaw(t, gateway(func(o *llmgateway.Options) { o.IdleTimeout = 150 * time.Millisecond }), geminiTarget(up), geminiUserBody, 0)
	if err != nil || res.Text != "已完成" || res.UsageMissing || !strings.Contains(raw, "data: [DONE]") {
		t.Fatalf("res=%+v err=%v raw=%s", res, err, raw)
	}
}

func TestFromGemini_TransportFailureDoesNotSynthesizeDone(t *testing.T) {
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		sse(w, `{"candidates":[{"content":{"parts":[{"text":"部分结果"}]}}]}`)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = conn.Close() // 不写 HTTP chunked 结束块，模拟连接中途断开。
	})
	res, raw, err := streamRaw(t, gateway(), geminiTarget(up), geminiUserBody, 0)
	if err == nil || res == nil || res.Text != "部分结果" || strings.Contains(raw, "[DONE]") {
		t.Fatalf("res=%+v err=%v raw=%s", res, err, raw)
	}
}
