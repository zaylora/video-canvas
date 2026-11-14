package outbound_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"video-canvas/internal/config"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/pkg/outbound"
)

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

func extCalls(lines []map[string]any) []map[string]any {
	var out []map[string]any
	for _, m := range lines {
		if m["event"] == logger.EventExtCall {
			out = append(out, m)
		}
	}
	return out
}

func TestClient_LogsTextRequestAndResponse(t *testing.T) {
	readApp, readPayload := logFiles(t)
	var gotRequestID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRequestID = r.Header.Get("X-Request-ID")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":["网页正文"]}`))
	}))
	defer srv.Close()

	ctx := logger.WithRequest(context.Background(), "req-1")
	ctx, run := logger.BeginRun(ctx, logger.RunRef{Kind: "agent", ID: "88"})
	c := outbound.Client("agent.web", &http.Client{})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/search?q=x", bytes.NewReader([]byte(`{"query":"雨夜"}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != `{"results":["网页正文"]}` {
		t.Fatalf("包装不能改变响应：%s", body)
	}
	run.End(logger.StatusSucceeded)
	if gotRequestID != "req-1" {
		t.Errorf("应把请求编号带给对方：%q", gotRequestID)
	}

	app := readApp()
	calls := extCalls(app)
	if len(calls) != 1 {
		t.Fatalf("应有 1 行 ext.call：%v", app)
	}
	cl := calls[0]
	want := map[string]any{"purpose": "agent.web", "method": "POST", "path": "/search", "status": float64(200), "result": "ok",
		"request_bytes": float64(len(`{"query":"雨夜"}`)), "response_bytes": float64(len(body)), "run_kind": "agent", "level": "info"}
	for k, v := range want {
		if cl[k] != v {
			t.Errorf("ext.call 的 %s = %v，想要 %v", k, cl[k], v)
		}
	}
	for _, m := range app {
		if m["event"] == logger.EventRunFinish && m["ext_calls"] != float64(1) {
			t.Errorf("运行汇总应累计外部调用：%v", m)
		}
	}
	p := readPayload()
	if len(p) != 1 || p[0]["call_id"] != cl["call_id"] || p[0]["kind"] != "http" {
		t.Fatalf("payload 应有一条同 call_id 的 http 记录：%v", p)
	}
	preq := p[0]["request"].(map[string]any)
	presp := p[0]["response"].(map[string]any)
	if preq["body"] != `{"query":"雨夜"}` || preq["headers"].(map[string]any)["Authorization"] != "***" || presp["body"] != `{"results":["网页正文"]}` {
		t.Errorf("payload 应带请求体、响应体，鉴权头遮掉：%v", p[0])
	}
}

func TestClient_BinaryResponseOnlyRecordsSize(t *testing.T) {
	_, readPayload := logFiles(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(bytes.Repeat([]byte{0x89}, 5000))
	}))
	defer srv.Close()
	resp, err := outbound.Client("x", nil).Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if n != 5000 {
		t.Fatalf("包装不能截断二进制响应：%d", n)
	}
	presp := readPayload()[0]["response"].(map[string]any)
	if b, _ := presp["body"].(string); !strings.Contains(b, "<binary image/png 5000 bytes>") {
		t.Errorf("二进制响应只记类型和大小：%v", presp)
	}
}

func TestClient_FailuresAreWarn(t *testing.T) {
	readApp, _ := logFiles(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream down"))
	}))
	c := outbound.Client("x", nil)
	resp, err := c.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	srv.Close()
	if _, err := c.Get(srv.URL); err == nil {
		t.Fatal("服务关了应报错")
	}
	calls := extCalls(readApp())
	if len(calls) != 2 {
		t.Fatalf("应有 2 行：%v", calls)
	}
	if calls[0]["level"] != "warn" || calls[0]["result"] != "failed" || calls[0]["status"] != float64(502) {
		t.Errorf("非 2xx 记为失败：%v", calls[0])
	}
	if calls[1]["level"] != "warn" || calls[1]["result"] != "failed" || calls[1]["error"] == nil {
		t.Errorf("连接失败记为失败并带原因：%v", calls[1])
	}
}

func TestClient_CallPurposeFromContextWins(t *testing.T) {
	readApp, _ := logFiles(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	ctx, callID := logger.WithCall(context.Background(), "agent.web_fetch")
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := outbound.Client("agent.web", nil).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	c := extCalls(readApp())[0]
	if c["purpose"] != "agent.web_fetch" || c["call_id"] != callID {
		t.Errorf("业务声明的调用优先：%v", c)
	}
}
