package agent_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"video-canvas/internal/llmgateway"
	"video-canvas/internal/model"
	. "video-canvas/internal/service/agent"
)

// gunzip 把落库的压缩请求体还原成 JSON 文本。
func gunzip(t *testing.T, b []byte) string {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

const requestBody = `{
  "model": "claude-up",
  "stream": true,
  "max_tokens": 4096,
  "temperature": 0.2,
  "tools": [{"type":"function","function":{"name":"canvas_get_state"}},{"type":"function","function":{"name":"canvas_apply_ops"}}],
  "messages": [
    {"role":"system","content":"你是画布助手"},
    {"role":"user","content":[
      {"type":"text","text":"看看这张图"},
      {"type":"image_url","image_url":{"url":"data:image/png;base64,AAAABBBBCCCC"}},
      {"type":"image_url","image_url":{"url":"https://oss.example.com/u/1/a.png?X-Amz-Signature=secret"}}
    ]}
  ]
}`

func TestBuildModelRequestRecord(t *testing.T) {
	t.Run("摘要：消息数、工具数、除消息和工具外的参数", func(t *testing.T) {
		rec := BuildModelRequestRecord(7, 3, []byte(requestBody), "")
		if rec.CallID != 7 || rec.RunID != 3 || rec.MessageCount != 2 || rec.ToolCount != 2 {
			t.Errorf("基本信息不对: %+v", rec)
		}
		var params map[string]any
		if err := json.Unmarshal(rec.Params, &params); err != nil {
			t.Fatal(err)
		}
		if params["model"] != "claude-up" || params["max_tokens"] != float64(4096) || params["temperature"] != 0.2 {
			t.Errorf("params 应含 model、max_tokens、temperature: %v", params)
		}
		if _, has := params["messages"]; has {
			t.Errorf("params 不含 messages: %v", params)
		}
		if _, has := params["tools"]; has {
			t.Errorf("params 不含 tools: %v", params)
		}
	})

	t.Run("完整请求体压缩留存，图片地址和图片数据一律脱敏，文字原样保留", func(t *testing.T) {
		rec := BuildModelRequestRecord(1, 1, []byte(requestBody), "")
		if len(rec.Body) == 0 {
			t.Fatal("应留存请求体")
		}
		got := gunzip(t, rec.Body)
		for _, leak := range []string{"AAAABBBBCCCC", "oss.example.com", "X-Amz-Signature", "secret"} {
			if strings.Contains(got, leak) {
				t.Errorf("请求体里不该留着 %q: %s", leak, got)
			}
		}
		for _, keep := range []string{"你是画布助手", "看看这张图", "canvas_apply_ops", "image_url"} {
			if !strings.Contains(got, keep) {
				t.Errorf("请求体应保留 %q: %s", keep, got)
			}
		}
		if rec.RawBytes != len(got) {
			t.Errorf("RawBytes=%d，脱敏后原文 %d 字节", rec.RawBytes, len(got))
		}
	})

	t.Run("超过上限：只留摘要，不留请求体", func(t *testing.T) {
		big := `{"model":"m","messages":[{"role":"user","content":"` + strings.Repeat("a", MaxModelRequestBytes) + `"}]}`
		rec := BuildModelRequestRecord(1, 1, []byte(big), "")
		if rec.Body != nil || rec.MessageCount != 1 || rec.RawBytes <= MaxModelRequestBytes {
			t.Errorf("超限应只留摘要: body=%d count=%d raw=%d", len(rec.Body), rec.MessageCount, rec.RawBytes)
		}
	})

	t.Run("不是 JSON 对象：什么也不留", func(t *testing.T) {
		if rec := BuildModelRequestRecord(1, 1, []byte(`[1,2]`), ""); rec != nil {
			t.Errorf("应返回 nil: %+v", rec)
		}
	})
}

func TestBuildModelRequestRecord_IncludesAPIFormat(t *testing.T) {
	for _, format := range []string{"", "openai", "gemini"} {
		t.Run("接口格式="+format, func(t *testing.T) {
			rec := BuildModelRequestRecord(7, 3, []byte(requestBody), format)
			if rec == nil {
				t.Fatal("合法请求应有留存记录")
			}
			var params map[string]any
			if err := json.Unmarshal(rec.Params, &params); err != nil {
				t.Fatal(err)
			}
			if got, has := params["api_format"]; has != (format != "") || has && got != format {
				t.Errorf("摘要接口格式=%v，存在=%v，传入=%q", got, has, format)
			}
			got := gunzip(t, rec.Body)
			var body map[string]any
			if err := json.Unmarshal([]byte(got), &body); err != nil {
				t.Fatal(err)
			}
			messages, ok := body["messages"].([]any)
			if !ok || len(messages) != 2 || rec.MessageCount != 2 || rec.ToolCount != 2 {
				t.Errorf("应保留 OpenAI 形状和消息、工具摘要: %v", body)
			}
			for _, key := range []string{"contents", "systemInstruction", "api_format"} {
				if _, has := body[key]; has {
					t.Errorf("留存体不应含 %s", key)
				}
			}
			for _, leak := range []string{"AAAABBBBCCCC", "oss.example.com", "X-Amz-Signature", "secret"} {
				if strings.Contains(got, leak) {
					t.Errorf("留存体不应含媒体原文 %q: %s", leak, got)
				}
			}
		})
	}
}

// fakeRequestRecorder 在内存里收模型请求记录；异步写入，测试里用 wait 轮询。
type fakeRequestRecorder struct {
	mu   sync.Mutex
	recs []*model.AgentModelCallBody
	err  error
}

func (r *fakeRequestRecorder) SaveModelCallBody(_ context.Context, b *model.AgentModelCallBody) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	cp := *b
	r.recs = append(r.recs, &cp)
	return nil
}

func (r *fakeRequestRecorder) wait(t *testing.T, n int) []*model.AgentModelCallBody {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		if len(r.recs) >= n {
			out := append([]*model.AgentModelCallBody(nil), r.recs...)
			r.mu.Unlock()
			return out
		}
		r.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("等了 3 秒还没收到 %d 条模型请求记录", n)
	return nil
}

func TestProxyModel_RecordsRequest(t *testing.T) {
	ctx := context.Background()
	body := []byte(`{"model":"agent-model","stream":false,"max_tokens":999999,"temperature":0.5,"messages":[{"role":"user","content":"拆分镜"}],"junk":"不在白名单"}`)

	t.Run("记下实际发给上游的请求：模型换成上游模型、强制流式、最大输出按模型上限收紧", func(t *testing.T) {
		b := newBridgeEnv(t)
		rr := &fakeRequestRecorder{}
		b.br.SetModelRequestRecorder(rr)
		tok, run := b.token(t, "all")
		if err := b.br.ProxyModelPlatform(ctx, b.rcs[tok], body, func(uint64) {}, func([]byte) {}); err != nil {
			t.Fatal(err)
		}
		rec := rr.wait(t, 1)[0]
		if rec.CallID != b.bill.calls[0].ID || rec.RunID != run.ID {
			t.Errorf("要对应这次调用和运行: %+v", rec)
		}
		var params map[string]any
		_ = json.Unmarshal(rec.Params, &params)
		if params["model"] != "claude-up" || params["stream"] != true {
			t.Errorf("应是实际发出的内容（上游模型、强制流式）而不是 Agent 发来的: %v", params)
		}
		if _, has := params["junk"]; has {
			t.Errorf("白名单外的字段不会发给上游，也不该出现在记录里: %v", params)
		}
		if !strings.Contains(gunzip(t, rec.Body), "拆分镜") {
			t.Errorf("应留存消息原文")
		}
	})

	t.Run("上游一个字都没返回的失败调用也要留下请求", func(t *testing.T) {
		b := newBridgeEnv(t)
		rr := &fakeRequestRecorder{}
		b.br.SetModelRequestRecorder(rr)
		b.str.script = func(_ context.Context, _ func(llmgateway.Event), _ func([]byte)) (*llmgateway.Result, error) {
			return nil, context.DeadlineExceeded
		}
		tok, _ := b.token(t, "all")
		_ = b.br.ProxyModelPlatform(ctx, b.rcs[tok], body, func(uint64) {}, func([]byte) {})
		rr.wait(t, 1)
	})

	t.Run("记录失败不影响对话；没绑定记录器也照常工作", func(t *testing.T) {
		b := newBridgeEnv(t)
		rr := &fakeRequestRecorder{err: context.Canceled}
		b.br.SetModelRequestRecorder(rr)
		tok, _ := b.token(t, "all")
		if err := b.br.ProxyModelPlatform(ctx, b.rcs[tok], body, func(uint64) {}, func([]byte) {}); err != nil {
			t.Fatalf("记录失败不该让调用失败: %v", err)
		}
		b2 := newBridgeEnv(t)
		tok2, _ := b2.token(t, "all")
		if err := b2.br.ProxyModelPlatform(ctx, b2.rcs[tok2], body, func(uint64) {}, func([]byte) {}); err != nil {
			t.Fatal(err)
		}
	})
}
