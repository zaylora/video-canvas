package llmgateway_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"video-canvas/internal/llmgateway"
)

func geminiRequest(t *testing.T, body string) map[string]any {
	t.Helper()
	b, err := llmgateway.ToGemini([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func contentParts(t *testing.T, out map[string]any, index int) []any {
	t.Helper()
	contents, ok := out["contents"].([]any)
	if !ok || index >= len(contents) {
		t.Fatalf("contents=%v", out["contents"])
	}
	return contents[index].(map[string]any)["parts"].([]any)
}

func TestToGemini_SystemAndUser(t *testing.T) {
	out := geminiRequest(t, `{"messages":[{"role":"system","content":"规则一"},{"role":"system","content":"规则二"},{"role":"user","content":"你好"}]}`)
	parts := out["systemInstruction"].(map[string]any)["parts"].([]any)
	if !reflect.DeepEqual(parts, []any{map[string]any{"text": "规则一"}, map[string]any{"text": "规则二"}}) {
		t.Fatalf("system=%v", parts)
	}
	if out["contents"].([]any)[0].(map[string]any)["role"] != "user" || contentParts(t, out, 0)[0].(map[string]any)["text"] != "你好" {
		t.Fatalf("out=%v", out)
	}
}

func TestToGemini_Images(t *testing.T) {
	for _, tc := range []struct{ url, field, mime, valueKey, value string }{
		{"https://assets.example/a.PNG?signature=secret", "fileData", "image/png", "fileUri", "https://assets.example/a.PNG?signature=secret"},
		{"https://assets.example/download?id=1", "fileData", "image/jpeg", "fileUri", "https://assets.example/download?id=1"},
		{"https://assets.example/a.webp", "fileData", "image/webp", "fileUri", "https://assets.example/a.webp"},
		{"data:image/png;base64,AAAA", "inlineData", "image/png", "data", "AAAA"},
	} {
		t.Run(tc.field+tc.mime, func(t *testing.T) {
			b, _ := json.Marshal(map[string]any{"messages": []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": "看图"}, map[string]any{"type": "image_url", "image_url": map[string]any{"url": tc.url}}}}}})
			parts := contentParts(t, geminiRequest(t, string(b)), 0)
			got := parts[1].(map[string]any)[tc.field].(map[string]any)
			if got["mimeType"] != tc.mime || got[tc.valueKey] != tc.value {
				t.Fatalf("image=%v", got)
			}
		})
	}
}

const parallelHistory = `{"messages":[{"role":"assistant","content":"查一下","tool_calls":[{"id":"a","type":"function","function":{"name":"first","arguments":"{\"n\":1}"}},{"id":"b","type":"function","function":{"name":"second","arguments":"{}"}}]},{"role":"tool","tool_call_id":"a","content":"结果一"},{"role":"tool","tool_call_id":"b","content":"结果二"}]}`

func TestToGemini_ToolCallsAndResults(t *testing.T) {
	out := geminiRequest(t, parallelHistory)
	contents := out["contents"].([]any)
	if len(contents) != 2 || contents[0].(map[string]any)["role"] != "model" || contents[1].(map[string]any)["role"] != "user" {
		t.Fatalf("contents=%v", contents)
	}
	calls := contentParts(t, out, 0)
	if calls[0].(map[string]any)["text"] != "查一下" || calls[1].(map[string]any)["functionCall"].(map[string]any)["args"].(map[string]any)["n"] != float64(1) {
		t.Fatalf("calls=%v", calls)
	}
	results := contentParts(t, out, 1)
	for i, name := range []string{"first", "second"} {
		got := results[i].(map[string]any)["functionResponse"].(map[string]any)
		if got["name"] != name || got["response"].(map[string]any)["content"] != []string{"结果一", "结果二"}[i] {
			t.Fatalf("response=%v", got)
		}
	}
}

func TestToGemini_ToolResultImagesMerged(t *testing.T) {
	body := strings.TrimSuffix(parallelHistory, "]}") + `,{"role":"user","content":[{"type":"text","text":"Attached image(s) from tool result:"},{"type":"image_url","image_url":{"url":"https://assets.example/a.png"}},{"type":"image_url","image_url":{"url":"data:image/jpeg;base64,BBBB"}}]}]}`
	out := geminiRequest(t, body)
	parts := contentParts(t, out, 1)
	if len(out["contents"].([]any)) != 2 || len(parts) != 4 || parts[2].(map[string]any)["fileData"] == nil || parts[3].(map[string]any)["inlineData"] == nil {
		t.Fatalf("parts=%v", parts)
	}
}

func TestToGemini_SignatureFromCallID(t *testing.T) {
	for _, tc := range []struct{ id, want string }{
		{"gc_ab12cd34_" + base64.RawURLEncoding.EncodeToString([]byte("sig+/签名_")), "sig+/签名_"},
		{"call_regular", ""}, {"gc_ab12cd34", ""}, {"gc_short_c2ln", ""}, {"gc_AB12cd34_c2ln", ""}, {"gc_ab12cd34_%%%", ""},
	} {
		t.Run(tc.id, func(t *testing.T) {
			body, _ := json.Marshal(map[string]any{"messages": []any{map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"id": tc.id, "function": map[string]any{"name": "x", "arguments": "{}"}}}}}})
			p := contentParts(t, geminiRequest(t, string(body)), 0)[0].(map[string]any)
			got, _ := p["thoughtSignature"].(string)
			if got != tc.want {
				t.Fatalf("signature=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestToGemini_AdjacentSameRoleMerged(t *testing.T) {
	out := geminiRequest(t, `{"messages":[{"role":"user","content":"一"},{"role":"user","content":[{"type":"text","text":"二"}]},{"role":"assistant","content":"三"},{"role":"assistant","content":"四"}]}`)
	if len(out["contents"].([]any)) != 2 || !reflect.DeepEqual(contentParts(t, out, 0), []any{map[string]any{"text": "一"}, map[string]any{"text": "二"}}) || !reflect.DeepEqual(contentParts(t, out, 1), []any{map[string]any{"text": "三"}, map[string]any{"text": "四"}}) {
		t.Fatalf("out=%v", out)
	}
}

func TestToGemini_GenerationConfigAndToolChoice(t *testing.T) {
	for _, tc := range []struct {
		choice, stop string
		config       any
	}{
		{`"auto"`, `"END"`, nil},
		{`"none"`, `["END","STOP"]`, map[string]any{"mode": "NONE"}},
		{`{"type":"function","function":{"name":"x"}}`, `"END"`, map[string]any{"mode": "ANY", "allowedFunctionNames": []any{"x"}}},
	} {
		t.Run(tc.choice, func(t *testing.T) {
			out := geminiRequest(t, `{"messages":[{"role":"user","content":"x"}],"max_tokens":123,"temperature":0,"top_p":0.8,"stop":`+tc.stop+`,"tool_choice":`+tc.choice+`}`)
			config := out["generationConfig"].(map[string]any)
			stops := []any{"END"}
			if strings.HasPrefix(tc.stop, "[") {
				stops = append(stops, "STOP")
			}
			if !reflect.DeepEqual(config, map[string]any{"maxOutputTokens": float64(123), "temperature": float64(0), "topP": 0.8, "stopSequences": stops}) {
				t.Fatalf("config=%v", config)
			}
			if tc.config == nil {
				if out["toolConfig"] != nil {
					t.Fatalf("toolConfig=%v", out["toolConfig"])
				}
			} else if !reflect.DeepEqual(out["toolConfig"].(map[string]any)["functionCallingConfig"], tc.config) {
				t.Fatalf("toolConfig=%v", out["toolConfig"])
			}
		})
	}
}

func TestToGemini_ToolResultWithoutCall(t *testing.T) {
	for _, body := range []string{`{"messages":[{"role":"tool","tool_call_id":"lost","content":"https://private.example/result?key=secret"}]}`, `{"messages":[{"role":"tool","tool_call_id":"future","content":"x"},{"role":"assistant","tool_calls":[{"id":"future","function":{"name":"x","arguments":"{}"}}]}]}`} {
		_, err := llmgateway.ToGemini([]byte(body))
		if !errors.Is(err, llmgateway.ErrInvalidRequest) || !strings.Contains(err.Error(), "工具") || strings.Contains(err.Error(), "private.example") {
			t.Fatalf("err=%v", err)
		}
	}
}

func TestToGemini_EmptyArguments(t *testing.T) {
	for _, args := range []string{"", "{}", "{bad", "[]", "null"} {
		t.Run(args, func(t *testing.T) {
			body, _ := json.Marshal(map[string]any{"messages": []any{map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": "a", "function": map[string]any{"name": "x", "arguments": args}}}}}})
			out, err := llmgateway.ToGemini(body)
			if args != "" && args != "{}" {
				if !errors.Is(err, llmgateway.ErrInvalidRequest) {
					t.Fatalf("err=%v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var parsed map[string]any
			_ = json.Unmarshal(out, &parsed)
			got := contentParts(t, parsed, 0)[0].(map[string]any)["functionCall"].(map[string]any)["args"]
			if !reflect.DeepEqual(got, map[string]any{}) {
				t.Fatalf("args=%v", got)
			}
		})
	}
}

func TestToGemini_NoMessagesKeyInOutput(t *testing.T) {
	out := geminiRequest(t, piBody)
	for _, key := range []string{"messages", "model", "stream", "stream_options", "store", "max_completion_tokens"} {
		if _, has := out[key]; has {
			t.Errorf("不应保留 %s", key)
		}
	}
	fn := out["tools"].([]any)[0].(map[string]any)["functionDeclarations"].([]any)[0].(map[string]any)
	if fn["name"] != "canvas_get_state" || fn["parameters"].(map[string]any)["type"] != "object" {
		t.Fatalf("fn=%v", fn)
	}
}

func TestToGemini_GatewayTranslatesBothEntrypoints(t *testing.T) {
	for _, raw := range []bool{false, true} {
		t.Run(map[bool]string{true: "StreamRaw", false: "Stream"}[raw], func(t *testing.T) {
			up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
				sse(w, `{"candidates":[{"content":{"parts":[{"text":"ok"}]},"finishReason":"STOP"}]}`)
			})
			tg := target(up)
			tg.Format = llmgateway.FormatGemini
			if raw {
				_, _, err := streamRaw(t, gateway(), tg, piBody, 42)
				if err != nil && !errors.Is(err, llmgateway.ErrStreamTruncated) {
					t.Fatal(err)
				}
			} else {
				_, err := gateway().Stream(t.Context(), tg, llmgateway.Request{System: "规则", Messages: []llmgateway.Message{userMsg("x")}, MaxTokens: 42}, func(llmgateway.Event) {})
				if err != nil && !errors.Is(err, llmgateway.ErrStreamTruncated) {
					t.Fatal(err)
				}
			}
			got := up.requests()[0].Body
			if got["messages"] != nil || got["contents"] == nil || got["generationConfig"].(map[string]any)["maxOutputTokens"] != float64(42) {
				t.Fatalf("body=%v", got)
			}
		})
	}
}

func TestToGemini_InvalidInputDoesNotExposeMedia(t *testing.T) {
	for _, body := range []string{`{`, `null`, `{"messages":[]}`, `{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:private.example/secret"}}]}]}`} {
		_, err := llmgateway.ToGemini([]byte(body))
		if !errors.Is(err, llmgateway.ErrInvalidRequest) || strings.Contains(err.Error(), "private.example") {
			t.Fatalf("err=%v", err)
		}
	}
}
