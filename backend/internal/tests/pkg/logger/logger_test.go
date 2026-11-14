package logger_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"video-canvas/internal/config"
	"video-canvas/internal/pkg/logger"
)

// setup 把日志写进临时目录里的 app.log / payload.log，返回读取两个文件的函数（读之前先 Close 刷盘）。
func setup(t *testing.T, payload bool) (readApp, readPayload func() []map[string]any) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Log{Level: "debug", Format: "console", Filename: filepath.Join(dir, "app.log"), MaxSize: 10, Instance: "test-host",
		Payload: config.LogPayload{Enabled: payload, Filename: filepath.Join(dir, "payload.log"), MaxSize: 10, MaxAge: 7}}
	if err := logger.Init(cfg); err != nil {
		t.Fatalf("初始化日志失败：%v", err)
	}
	t.Cleanup(logger.Close)
	read := func(name string) func() []map[string]any {
		return func() []map[string]any {
			logger.Close()
			return readLines(t, filepath.Join(dir, name))
		}
	}
	return read("app.log"), read("payload.log")
}

// readLines 逐行解析 JSON；文件不存在返回空；有一行不是合法 JSON 就失败。
func readLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("打开 %s 失败：%v", path, err)
	}
	defer f.Close()
	var out []map[string]any
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("%s 有一行不是合法 JSON：%v\n%s", path, err, sc.Text())
		}
		out = append(out, m)
	}
	return out
}

// find 返回第一条 msg 匹配的记录。
func find(t *testing.T, lines []map[string]any, msg string) map[string]any {
	t.Helper()
	for _, m := range lines {
		if m["msg"] == msg {
			return m
		}
	}
	t.Fatalf("没找到 msg=%q 的日志，共 %d 行：%v", msg, len(lines), lines)
	return nil
}

func TestCtxCarriesCorrelationFields(t *testing.T) {
	readApp, _ := setup(t, false)
	ctx := logger.WithRequest(context.Background(), "req-1")
	ctx = logger.WithUser(ctx, 42)
	ctx = logger.WithAttempt(ctx, 2)
	ctx = logger.WithParent(ctx, "agent", "88213")
	ctx, _ = logger.BeginRun(ctx, logger.RunRef{Kind: "drama", ID: "9f3c"})
	logger.Ctx(ctx).Info("带全部字段")
	logger.Ctx(context.Background()).Info("没有字段")

	lines := readApp()
	full := find(t, lines, "带全部字段")
	want := map[string]any{"request_id": "req-1", "user_id": float64(42), "attempt": float64(2), "parent_kind": "agent",
		"parent_id": "88213", "run_kind": "drama", "run_id": "9f3c", "instance": "test-host"}
	for k, v := range want {
		if full[k] != v {
			t.Errorf("字段 %s = %v，想要 %v", k, full[k], v)
		}
	}
	bare := find(t, lines, "没有字段")
	for _, k := range []string{"request_id", "user_id", "run_kind", "run_id", "attempt", "parent_kind"} {
		if _, ok := bare[k]; ok {
			t.Errorf("没挂的字段 %s 不应输出", k)
		}
	}
}

func TestRunRefFieldsWinOverPlainContext(t *testing.T) {
	readApp, _ := setup(t, false)
	ctx := logger.WithUser(context.Background(), 1)
	ctx, run := logger.BeginRun(ctx, logger.RunRef{Kind: "gen", ID: "a1", UserID: 7, RequestID: "req-from-job"})
	logger.Ctx(ctx).Info("运行里的用户")
	run.SetUser(9)
	logger.Ctx(ctx).Info("后补的用户")

	lines := readApp()
	if m := find(t, lines, "运行里的用户"); m["user_id"] != float64(7) || m["request_id"] != "req-from-job" {
		t.Errorf("运行自带的用户和请求编号应优先：%v", m)
	}
	if m := find(t, lines, "后补的用户"); m["user_id"] != float64(9) {
		t.Errorf("SetUser 之后应输出新用户：%v", m)
	}
}

func TestCtxReadsFieldsThroughGinContext(t *testing.T) {
	readApp, _ := setup(t, false)
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	c.Request = req.WithContext(logger.WithRequest(req.Context(), "req-gin"))
	logger.Ctx(c).Info("直接传 gin.Context")

	if m := find(t, readApp(), "直接传 gin.Context"); m["request_id"] != "req-gin" {
		t.Errorf("handler 把 *gin.Context 当 ctx 传下去时也要带上请求字段：%v", m)
	}
}

func TestPayloadGoesOnlyToPayloadFile(t *testing.T) {
	readApp, readPayload := setup(t, true)
	ctx, _ := logger.BeginRun(context.Background(), logger.RunRef{Kind: "drama", ID: "9f3c", UserID: 42})
	callID := logger.NewCallID()
	logger.Payload(ctx, logger.PayloadRecord{CallID: callID, Kind: "llm", Purpose: "drama.scripts",
		Request:  &logger.PayloadRequest{Method: "POST", URL: "https://x/v1/chat", Body: `{"prompt":"全文"}`},
		Response: &logger.PayloadResponse{Status: 200, Text: "回复全文"}, LatencyMs: 12})

	payload := readPayload()
	if len(payload) != 1 {
		t.Fatalf("payload.log 应有 1 条，实际 %d", len(payload))
	}
	p := payload[0]
	if p["call_id"] != callID || p["kind"] != "llm" || p["run_kind"] != "drama" || p["user_id"] != float64(42) || p["instance"] != "test-host" {
		t.Errorf("payload 记录缺关联字段：%v", p)
	}
	if req, _ := p["request"].(map[string]any); req["body"] != `{"prompt":"全文"}` {
		t.Errorf("请求体应原样记录：%v", p["request"])
	}
	for _, m := range readApp() {
		if m["call_id"] == callID {
			t.Errorf("全文不应出现在 app.log：%v", m)
		}
	}
	if len(callID) != 12 {
		t.Errorf("call_id 应为 12 位十六进制：%q", callID)
	}
}

func TestPayloadDisabledWritesNothing(t *testing.T) {
	_, readPayload := setup(t, false)
	if logger.PayloadEnabled() {
		t.Fatal("关闭时 PayloadEnabled 应为 false")
	}
	logger.Payload(context.Background(), logger.PayloadRecord{CallID: "x", Kind: "llm"})
	if got := readPayload(); len(got) != 0 {
		t.Errorf("关闭时不应写 payload.log：%v", got)
	}
}

func TestPayloadRedactsAuthHeadersAndBase64AndTruncates(t *testing.T) {
	_, readPayload := setup(t, true)
	img := "data:image/png;base64," + strings.Repeat("QUJD", 600) // 2400 个字符，超过 1KB
	huge := strings.Repeat("字", logger.MaxPayloadField)           // 远超 4MB（每个字 3 字节）
	logger.Payload(context.Background(), logger.PayloadRecord{CallID: "c1", Kind: "http",
		Request: &logger.PayloadRequest{Method: "POST", URL: "https://x",
			Headers: map[string]string{"Authorization": "Bearer sk-secret", "X-Api-Key": "k1", "api-key": "k2", "x-goog-api-key": "k3", "Content-Type": "application/json"},
			Body:    `{"image":"` + img + `","small":"data:image/png;base64,QUJD"}`},
		Response: &logger.PayloadResponse{Status: 200, Body: huge}})

	p := readPayload()[0]
	req := p["request"].(map[string]any)
	headers := req["headers"].(map[string]any)
	for _, k := range []string{"Authorization", "X-Api-Key", "api-key", "x-goog-api-key"} {
		if headers[k] != "***" {
			t.Errorf("鉴权头 %s 应遮成 ***，实际 %v", k, headers[k])
		}
	}
	if headers["Content-Type"] != "application/json" {
		t.Errorf("普通请求头应原样保留：%v", headers)
	}
	body := req["body"].(string)
	if strings.Contains(body, "QUJDQUJD") || !strings.Contains(body, "<base64 image/png") || !strings.Contains(body, "sha256:") {
		t.Errorf("超过 1KB 的 base64 应换成摘要占位：%s", body[:min(len(body), 300)])
	}
	if !strings.Contains(body, "data:image/png;base64,QUJD\"") {
		t.Errorf("小于 1KB 的 base64 应保留原文：%s", body)
	}
	resp := p["response"].(map[string]any)
	if got := len(resp["body"].(string)); got > logger.MaxPayloadField {
		t.Errorf("超长字段应截断到 %d 字节以内，实际 %d", logger.MaxPayloadField, got)
	}
	if p["truncated"] != true {
		t.Errorf("截断过应标 truncated=true：%v", p["truncated"])
	}
}

func TestRunStartAndEndWithCounters(t *testing.T) {
	readApp, _ := setup(t, false)
	ctx, run := logger.BeginRun(context.Background(), logger.RunRef{Kind: "drama", ID: "9f3c", UserID: 42})
	run.Start(zap.Bool("resumed", false))
	run.Start() // 重复调用只打一次
	run.AddLLM(100, 20)
	run.AddLLM(50, 10)
	run.AddProvider()
	run.AddExt()
	run.End(logger.StatusFailed, zap.String("error_type", "internal"), zap.String("error", "boom"))
	run.End(logger.StatusSucceeded) // 重复收尾只认第一次
	if !run.Ended() || !run.Started() {
		t.Fatal("Start / End 之后 Started、Ended 应为 true")
	}
	logger.RunOf(ctx).AddLLM(1, 1) // 收尾后再计数不影响已打出的行

	lines := readApp()
	starts, finishes := 0, 0
	for _, m := range lines {
		switch m["event"] {
		case logger.EventRunStart:
			starts++
		case logger.EventRunFinish:
			finishes++
		}
	}
	if starts != 1 || finishes != 1 {
		t.Fatalf("应各打一次 run.start / run.finish，实际 %d / %d", starts, finishes)
	}
	var fin map[string]any
	for _, m := range lines {
		if m["event"] == logger.EventRunFinish {
			fin = m
		}
	}
	want := map[string]any{"status": "failed", "llm_calls": float64(2), "input_tokens": float64(150), "output_tokens": float64(30),
		"provider_calls": float64(1), "ext_calls": float64(1), "level": "error", "run_kind": "drama", "user_id": float64(42)}
	for k, v := range want {
		if fin[k] != v {
			t.Errorf("run.finish 的 %s = %v，想要 %v", k, fin[k], v)
		}
	}
	if _, ok := fin["duration_ms"]; !ok {
		t.Error("Start 过的运行收尾时应带 duration_ms")
	}
}

func TestRunFinishLevelByStatus(t *testing.T) {
	readApp, _ := setup(t, false)
	cases := []struct{ status, errType, level string }{
		{logger.StatusSucceeded, "", "info"},
		{logger.StatusPaused, "", "info"},
		{logger.StatusFailed, "upstream", "warn"},
		{logger.StatusCanceled, "", "warn"},
		{logger.StatusFailed, "internal", "error"},
	}
	for i, c := range cases {
		_, run := logger.BeginRun(context.Background(), logger.RunRef{Kind: "gen", ID: string(rune('a' + i))})
		var fields []zap.Field
		if c.errType != "" {
			fields = append(fields, zap.String("error_type", c.errType))
		}
		run.End(c.status, fields...)
	}
	lines := readApp()
	for i, c := range cases {
		id := string(rune('a' + i))
		for _, m := range lines {
			if m["run_id"] == id && m["level"] != c.level {
				t.Errorf("%s/%s 应为 %s 级，实际 %v", c.status, c.errType, c.level, m["level"])
			}
		}
	}
}

func TestRequestScopeRemembersRunForAccessLog(t *testing.T) {
	setup(t, false)
	ctx := logger.WithRequest(context.Background(), "req-9")
	if k, id := logger.RequestRun(ctx); k != "" || id != "" {
		t.Fatalf("还没有运行时应为空：%s %s", k, id)
	}
	logger.NoteRun(ctx, "gen", "a81b")
	if k, id := logger.RequestRun(ctx); k != "gen" || id != "a81b" {
		t.Errorf("NoteRun 之后应能读到：%s %s", k, id)
	}
	_, _ = logger.BeginRun(ctx, logger.RunRef{Kind: "adapt", ID: "3d5f"})
	if k, id := logger.RequestRun(ctx); k != "adapt" || id != "3d5f" {
		t.Errorf("BeginRun 也应记到请求上：%s %s", k, id)
	}
}

func TestNilRunIsSafe(t *testing.T) {
	setup(t, false)
	run := logger.RunOf(context.Background())
	run.Start()
	run.AddLLM(1, 1)
	run.End(logger.StatusSucceeded)
	if run.Ended() || run.Started() {
		t.Error("nil 运行的方法应是空操作")
	}
}
