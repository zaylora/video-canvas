package llmgateway_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"video-canvas/internal/config"
	"video-canvas/internal/llmgateway"
	"video-canvas/internal/llmgateway/llmlog"
	"video-canvas/internal/pkg/logger"
)

// logFiles 把日志写进临时目录，返回读 app.log / payload.log 的函数。
func logFiles(t *testing.T) (readApp, readPayload func() []map[string]any) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Log{Level: "debug", Format: "json", Filename: filepath.Join(dir, "app.log"), MaxSize: 10,
		Payload: config.LogPayload{Enabled: true, Filename: filepath.Join(dir, "payload.log"), MaxSize: 10}}
	if err := logger.Init(cfg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(logger.Close)
	read := func(name string) func() []map[string]any {
		return func() []map[string]any {
			logger.Close()
			f, err := os.Open(filepath.Join(dir, name))
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			var out []map[string]any
			sc := bufio.NewScanner(f)
			sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
			for sc.Scan() {
				var m map[string]any
				if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
					t.Fatalf("不是合法 JSON：%s", sc.Text())
				}
				out = append(out, m)
			}
			return out
		}
	}
	return read("app.log"), read("payload.log")
}

func byEvent(lines []map[string]any, ev string) []map[string]any {
	var out []map[string]any
	for _, m := range lines {
		if m["event"] == ev {
			out = append(out, m)
		}
	}
	return out
}

func TestLLMLog_SuccessWritesCallLineAndPayload(t *testing.T) {
	readApp, readPayload := logFiles(t)
	up := newUpstream(t, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		sse(w, `{"id":"c1","choices":[{"index":0,"delta":{"reasoning_content":"想一想"},"finish_reason":null}]}`,
			chunk(`{"content":"剧本全文"}`, "stop"), usageChunk(120, 30, 100), "[DONE]")
	})
	g := gateway(func(o *llmgateway.Options) { o.Observer = llmlog.Observer{} })

	ctx, run := logger.BeginRun(context.Background(), logger.RunRef{Kind: "drama", ID: "9f3c", UserID: 42})
	ctx, callID := logger.WithCall(ctx, "drama.scripts", zap.String("call", "episode=3"), zap.Int("try", 1))
	if _, err := g.Stream(ctx, target(up), llmgateway.Request{Messages: []llmgateway.Message{userMsg("写第三集")}}, func(llmgateway.Event) {}); err != nil {
		t.Fatal(err)
	}
	run.End(logger.StatusSucceeded)

	app := readApp()
	calls := byEvent(app, logger.EventLLMCall)
	if len(calls) != 1 {
		t.Fatalf("应有 1 行 llm.call：%v", app)
	}
	c := calls[0]
	want := map[string]any{"call_id": callID, "purpose": "drama.scripts", "call": "episode=3", "try": float64(1), "net_attempt": float64(1),
		"channel": "ch1", "upstream_model": "gpt-up", "input_tokens": float64(120), "output_tokens": float64(30), "cached_tokens": float64(100),
		"finish_reason": "stop", "result": "ok", "response_chars": float64(4), "run_kind": "drama", "user_id": float64(42), "level": "info"}
	for k, v := range want {
		if c[k] != v {
			t.Errorf("llm.call 的 %s = %v，想要 %v", k, c[k], v)
		}
	}
	for _, k := range []string{"latency_ms", "ttft_ms", "request_bytes"} {
		if _, ok := c[k]; !ok {
			t.Errorf("llm.call 应带 %s", k)
		}
	}
	fin := byEvent(app, logger.EventRunFinish)[0]
	if fin["llm_calls"] != float64(1) || fin["input_tokens"] != float64(120) {
		t.Errorf("运行汇总应累计这次调用：%v", fin)
	}

	payload := readPayload()
	if len(payload) != 1 {
		t.Fatalf("payload.log 应有 1 条：%v", payload)
	}
	p := payload[0]
	req := p["request"].(map[string]any)
	resp := p["response"].(map[string]any)
	if p["call_id"] != callID || p["kind"] != "llm" || !strings.Contains(req["body"].(string), "写第三集") ||
		resp["text"] != "剧本全文" || resp["thinking"] != "想一想" || resp["status"] != float64(200) {
		t.Errorf("payload 应带请求体全文和拼装后的回复：%v", p)
	}
	if strings.Contains(req["body"].(string), "sk-secret-key") {
		t.Error("payload 里不能有渠道 Key")
	}
}

func TestLLMLog_UpstreamErrorIsWarnWithBodyAndLastCallID(t *testing.T) {
	readApp, readPayload := logFiles(t)
	up := newUpstream(t, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte(`{"error":{"message":"Insufficient Balance","code":"invalid_request_error"}}`))
	})
	g := gateway(func(o *llmgateway.Options) { o.Observer = llmlog.Observer{} })
	ctx, run := logger.BeginRun(context.Background(), logger.RunRef{Kind: "drama", ID: "9f3c"})
	_, _ = g.Stream(ctx, target(up), llmgateway.Request{Messages: []llmgateway.Message{userMsg("x")}}, func(llmgateway.Event) {})
	run.End(logger.StatusFailed, zap.String("error_type", "upstream"))

	app := readApp()
	c := byEvent(app, logger.EventLLMCall)[0]
	callID, _ := c["call_id"].(string)
	if c["level"] != "warn" || c["result"] != "failed" || c["error_type"] != "upstream" || c["upstream_status"] != float64(402) ||
		!strings.Contains(c["error"].(string), "Insufficient Balance") || len(callID) != 12 {
		t.Errorf("上游报错的调用行不对：%v", c)
	}
	if fin := byEvent(app, logger.EventRunFinish)[0]; fin["last_call_id"] != callID {
		t.Errorf("失败的运行汇总应带最后一次调用的 call_id：%v", fin)
	}
	resp := readPayload()[0]["response"].(map[string]any)
	if !strings.Contains(resp["error_body"].(string), `"code":"invalid_request_error"`) {
		t.Errorf("payload 应带错误体原文：%v", resp)
	}
}

func TestLLMLog_ResultClassification(t *testing.T) {
	readApp, _ := logFiles(t)
	obs := llmlog.Observer{}
	cases := []struct {
		err    error
		result string
		level  string
	}{
		{llmgateway.ErrIdleTimeout, "timeout", "warn"},
		{context.DeadlineExceeded, "timeout", "warn"},
		{context.Canceled, "canceled", "info"},
		{errors.New("connection reset"), "failed", "warn"},
	}
	for i, c := range cases {
		ctx, _ := logger.WithCall(context.Background(), "t", zap.Int("case", i))
		obs.OnCall(ctx, llmgateway.CallRecord{Attempt: 1, Err: c.err, Latency: time.Millisecond})
	}
	calls := byEvent(readApp(), logger.EventLLMCall)
	for i, c := range cases {
		got := calls[i]
		if got["result"] != c.result || got["level"] != c.level {
			t.Errorf("%v 应记为 %s/%s：%v", c.err, c.result, c.level, got)
		}
	}
}

func TestLLMLog_WithoutCallContextGeneratesID(t *testing.T) {
	readApp, _ := logFiles(t)
	llmlog.Observer{}.OnCall(context.Background(), llmgateway.CallRecord{Attempt: 1, Result: &llmgateway.Result{Text: "x"}})
	c := byEvent(readApp(), logger.EventLLMCall)[0]
	if id, _ := c["call_id"].(string); len(id) != 12 {
		t.Errorf("没声明调用时也要生成 call_id：%v", c)
	}
}
