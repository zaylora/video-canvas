package pluginrunner_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"video-canvas/internal/config"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/provider/pluginproto"
	"video-canvas/internal/provider/pluginrunner"
)

func runnerLog(t *testing.T) func() []map[string]any {
	t.Helper()
	path := filepath.Join(t.TempDir(), "runner.log")
	if err := logger.Init(config.Log{Level: "debug", Format: "json", Filename: path, MaxSize: 10, Service: "plugin-runner"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(logger.Close)
	return func() []map[string]any {
		logger.Close()
		f, err := os.Open(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		var out []map[string]any
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			var m map[string]any
			_ = json.Unmarshal(sc.Bytes(), &m)
			out = append(out, m)
		}
		return out
	}
}

// 宿主调 runner 时把关联字段放进请求头，runner 记钩子失败时带上它们：同一个编号能在两个进程的日志里 grep 到。
func TestRunnerLogsHookFailureWithCorrelation(t *testing.T) {
	read := runnerLog(t)
	c := newClient(pluginrunner.Options{})
	ctx := logger.WithRequest(context.Background(), "req-9")
	ctx, _ = logger.BeginRun(ctx, logger.RunRef{Kind: "gen", ID: "a81b"})
	ctx, callID := logger.WithCall(ctx, "gen.submit")
	resp, err := c.Call(ctx, sha("nope"), "buildSubmitRequest", []json.RawMessage{json.RawMessage(`{}`)}, 0)
	if err != nil || resp.Error == nil || resp.Error.Code != pluginproto.CodeUnknownPlugin {
		t.Fatalf("应返回 unknown_plugin：%v %+v", err, resp)
	}
	var failed map[string]any
	for _, m := range read() {
		if m["msg"] == "钩子执行失败" {
			failed = m
		}
	}
	if failed == nil {
		t.Fatal("钩子失败应打一行 Warn")
	}
	want := map[string]any{"level": "warn", "service": "plugin-runner", "request_id": "req-9", "run_kind": "gen", "run_id": "a81b",
		"call_id": callID, "hook": "buildSubmitRequest", "code": pluginproto.CodeUnknownPlugin}
	for k, v := range want {
		if failed[k] != v {
			t.Errorf("%s = %v，想要 %v（整行：%v）", k, failed[k], v, failed)
		}
	}
}
