package worker_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"video-canvas/internal/config"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/provider"
)

// logFilesWk 把日志写进临时目录，返回读 app.log / payload.log 的函数。
func logFilesWk(t *testing.T) (readApp, readPayload func() []map[string]any) {
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

func providerCalls(lines []map[string]any) []map[string]any {
	var out []map[string]any
	for _, m := range lines {
		if m["event"] == logger.EventProviderCall {
			out = append(out, m)
		}
	}
	return out
}

// 正式任务（不是试跑）也记供应商调用：app.log 一行 provider.call，payload.log 一条带钩子步骤的记录，两边 call_id 相同。
func TestWorker_LogsProviderCallForEveryTask(t *testing.T) {
	readApp, readPayload := logFilesWk(t)
	env := newWkEnv(nil)
	env.task(1, model.TaskPending, nil)
	env.exec.submitFn = func(int, provider.SubmitInput) (*provider.SubmitResult, error) {
		return &provider.SubmitResult{ProviderTaskID: "pt-1"}, nil
	}
	task, _ := env.store.GetTask(context.Background(), 1)
	ctx, run := logger.BeginRun(context.Background(), logger.RunRef{Kind: "gen", ID: "a1"})
	env.w.Handle(ctx, *task)
	run.End(logger.StatusSucceeded)

	app := readApp()
	calls := providerCalls(app)
	if len(calls) != 1 {
		t.Fatalf("应有 1 行 provider.call：%v", app)
	}
	c := calls[0]
	if c["phase"] != "submit" || c["result"] != "ok" || c["run_kind"] != "gen" || c["steps"] != float64(1) || c["user_id"] == nil {
		t.Errorf("provider.call 字段不对：%v", c)
	}
	payload := readPayload()
	if len(payload) != 1 || payload[0]["call_id"] != c["call_id"] || payload[0]["kind"] != "provider" {
		t.Fatalf("payload 应有一条同 call_id 的供应商记录：%v", payload)
	}
	steps, _ := payload[0]["steps"].([]any)
	if len(steps) != 1 || !strings.HasPrefix(steps[0].(map[string]any)["name"].(string), "submit") {
		t.Errorf("payload 应带本次的钩子步骤：%v", payload[0]["steps"])
	}
	for _, m := range app {
		if m["event"] == logger.EventRunFinish && m["provider_calls"] != float64(1) {
			t.Errorf("运行汇总应累计供应商调用：%v", m)
		}
	}
	if n := env.store.count("SaveTrace"); n != 0 {
		t.Errorf("正式任务的追踪不落库（只进 payload.log）：%d", n)
	}
}

// 连续轮询结果相同时折叠：只记第一次和最后一次，中间记一条 folded=N。
func TestWorker_FoldsIdenticalPolls(t *testing.T) {
	readApp, readPayload := logFilesWk(t)
	env := newWkEnv(nil)
	env.task(1, model.TaskRunning, func(tk *model.GenerationTask) { tk.ProviderTaskID = "pt-1" })
	for range 5 {
		task, _ := env.store.GetTask(context.Background(), 1)
		env.w.Handle(context.Background(), *task)
	}
	env.store.setStatus(1, model.TaskFinalizing) // 进入下一阶段时把攒着的折叠写出来
	task, _ := env.store.GetTask(context.Background(), 1)
	env.w.Handle(context.Background(), *task)

	payload := readPayload()
	var kinds []string
	for _, p := range payload {
		if n, ok := p["folded"]; ok {
			kinds = append(kinds, "folded:"+jsonNum(n))
			continue
		}
		kinds = append(kinds, p["phase"].(string))
	}
	// 转存阶段会再查一次上游取产物，所以最后还有一条 finalize
	if strings.Join(kinds, ",") != "query,folded:3,query,finalize" {
		t.Errorf("5 次相同轮询应记成 首次 + folded:3 + 末次，再加转存：%v", kinds)
	}
	if n := len(providerCalls(readApp())); n != 3 {
		t.Errorf("app.log 的 provider.call 也只记轮询首末两行 + 转存一行：%d", n)
	}
}

func jsonNum(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
