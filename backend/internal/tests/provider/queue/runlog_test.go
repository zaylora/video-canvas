package queue_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"video-canvas/internal/config"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/provider/queue"
)

// runLogArgs 是测试用的任务参数：实现 RunArgs，运行编号和用户来自参数。
type runLogArgs struct {
	ID     string `json:"id"`
	UserID uint64 `json:"user_id"`
}

func (runLogArgs) Kind() string { return "runlog_test" }
func (a runLogArgs) RunRef() logger.RunRef {
	return logger.RunRef{Kind: "drama", ID: a.ID, UserID: a.UserID, RequestID: "req-from-args"}
}

type runLogWorker struct {
	river.WorkerDefaults[runLogArgs]
}

func (runLogWorker) Work(context.Context, *river.Job[runLogArgs]) error { return nil }

// systemArgs 是不属于用户的系统任务。
type systemArgs struct{}

func (systemArgs) Kind() string                 { return "runlog_system" }
func (systemArgs) RunRef() logger.RunRef        { return logger.RunRef{Kind: queue.RunKindSystem} }
func (systemArgs) InsertOpts() river.InsertOpts { return river.InsertOpts{} }

type systemWorker struct {
	river.WorkerDefaults[systemArgs]
}

func (systemWorker) Work(context.Context, *river.Job[systemArgs]) error { return nil }

func init() {
	workers := river.NewWorkers()
	queue.AddWorker(workers, &runLogWorker{})
	queue.AddWorker(workers, &systemWorker{})
}

// runLogFile 把日志写进临时文件，返回读取全部行的函数。
func runLogFile(t *testing.T) func() []map[string]any {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app.log")
	if err := logger.Init(config.Log{Level: "debug", Format: "json", Filename: path, MaxSize: 10}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(logger.Close)
	return func() []map[string]any {
		logger.Close()
		f, err := os.Open(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil // 一行都没写时 lumberjack 不会建文件
		}
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		var out []map[string]any
		sc := bufio.NewScanner(f)
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

// work 用中间件执行一次任务。
func work(t *testing.T, kind string, args any, attempt, maxAttempts int, inner func(context.Context) error) error {
	t.Helper()
	raw, _ := json.Marshal(args)
	job := &rivertype.JobRow{ID: 777, Kind: kind, EncodedArgs: raw, Attempt: attempt, MaxAttempts: maxAttempts}
	return queue.NewRunLogMiddleware().Work(context.Background(), job, inner)
}

func events(lines []map[string]any, name string) []map[string]any {
	var out []map[string]any
	for _, m := range lines {
		if m["event"] == name {
			out = append(out, m)
		}
	}
	return out
}

func TestRunLogMiddlewareAttachesRunToContext(t *testing.T) {
	read := runLogFile(t)
	err := work(t, "runlog_test", runLogArgs{ID: "9f3c", UserID: 42}, 2, 25, func(ctx context.Context) error {
		if ref := logger.RunOf(ctx).Ref(); ref.Kind != "drama" || ref.ID != "9f3c" || ref.UserID != 42 {
			t.Errorf("ctx 上应挂着参数里的运行：%+v", ref)
		}
		logger.Ctx(ctx).Info("业务日志")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var biz map[string]any
	for _, m := range read() {
		if m["msg"] == "业务日志" {
			biz = m
		}
	}
	want := map[string]any{"run_kind": "drama", "run_id": "9f3c", "user_id": float64(42), "attempt": float64(2),
		"request_id": "req-from-args", "job_kind": "runlog_test"}
	for k, v := range want {
		if biz[k] != v {
			t.Errorf("业务日志的 %s = %v，想要 %v", k, biz[k], v)
		}
	}
}

func TestRunLogMiddlewareSnoozeIsSilent(t *testing.T) {
	read := runLogFile(t)
	_ = work(t, "runlog_test", runLogArgs{ID: "a"}, 1, 25, func(context.Context) error { return river.JobSnooze(time.Second) })
	if lines := read(); len(lines) != 0 {
		t.Errorf("延后的执行不应打任何日志：%v", lines)
	}
}

func TestRunLogMiddlewareNoWorkIsSilent(t *testing.T) {
	read := runLogFile(t)
	_ = work(t, "runlog_test", runLogArgs{ID: "a"}, 1, 25, func(context.Context) error { return nil })
	if lines := read(); len(lines) != 0 {
		t.Errorf("没开始运行就正常返回（如任务已是终态）不应打日志：%v", lines)
	}
}

func TestRunLogMiddlewareRetryAndFinalFailure(t *testing.T) {
	read := runLogFile(t)
	boom := errors.New("数据库断开")
	_ = work(t, "runlog_test", runLogArgs{ID: "r1"}, 1, 3, func(context.Context) error { return boom })
	_ = work(t, "runlog_test", runLogArgs{ID: "r2"}, 3, 3, func(context.Context) error { return boom })
	lines := read()

	retries := events(lines, logger.EventRunRetry)
	if len(retries) != 1 || retries[0]["run_id"] != "r1" || retries[0]["level"] != "warn" || retries[0]["max_attempts"] != float64(3) || retries[0]["error"] != "数据库断开" {
		t.Errorf("还会重试时应打一行 run.retry：%v", retries)
	}
	finishes := events(lines, logger.EventRunFinish)
	if len(finishes) != 1 || finishes[0]["run_id"] != "r2" || finishes[0]["status"] != "failed" || finishes[0]["level"] != "error" ||
		finishes[0]["finish_fallback"] != true || finishes[0]["error"] != "数据库断开" {
		t.Errorf("重试用完时应兜底打 failed 的 run.finish：%v", finishes)
	}
}

func TestRunLogMiddlewareFallbackWhenStartedButNotEnded(t *testing.T) {
	read := runLogFile(t)
	_ = work(t, "runlog_test", runLogArgs{ID: "f1"}, 1, 25, func(ctx context.Context) error {
		logger.RunOf(ctx).Start()
		return nil
	})
	_ = work(t, "runlog_test", runLogArgs{ID: "f2"}, 1, 25, func(ctx context.Context) error {
		run := logger.RunOf(ctx)
		run.Start()
		run.End(logger.StatusSucceeded)
		return nil
	})
	finishes := events(read(), logger.EventRunFinish)
	if len(finishes) != 2 {
		t.Fatalf("应有两行 run.finish：%v", finishes)
	}
	for _, m := range finishes {
		switch m["run_id"] {
		case "f1":
			if m["status"] != "unknown" || m["finish_fallback"] != true || m["level"] != "warn" {
				t.Errorf("开始了却没收尾应兜底 unknown：%v", m)
			}
		case "f2":
			if _, ok := m["finish_fallback"]; ok || m["status"] != "succeeded" {
				t.Errorf("业务收尾了就不应再兜底：%v", m)
			}
		}
	}
}

func TestRunLogMiddlewarePanicLogsAndRepanics(t *testing.T) {
	read := runLogFile(t)
	func() {
		defer func() {
			if recover() == nil {
				t.Error("panic 应重新抛给 River")
			}
		}()
		_ = work(t, "runlog_test", runLogArgs{ID: "p1"}, 1, 25, func(context.Context) error { panic("坏输入") })
	}()
	finishes := events(read(), logger.EventRunFinish)
	if len(finishes) != 1 || finishes[0]["level"] != "error" || finishes[0]["status"] != "failed" || finishes[0]["error"] != "panic: 坏输入" {
		t.Errorf("panic 应兜底打 Error 级 run.finish：%v", finishes)
	}
}

func TestRunLogMiddlewareSystemJob(t *testing.T) {
	read := runLogFile(t)
	_ = work(t, "runlog_system", systemArgs{}, 1, 25, func(ctx context.Context) error {
		if ref := logger.RunOf(ctx).Ref(); ref.Kind != queue.RunKindSystem || ref.ID != "777" {
			t.Errorf("系统任务的运行编号取 River 任务 ID：%+v", ref)
		}
		return nil
	})
	for _, m := range events(read(), logger.EventRunFinish) {
		if m["level"] != "debug" {
			t.Errorf("系统任务成功只打 Debug 级的收尾行：%v", m)
		}
	}
}

func TestRunLogMiddlewareUnregisteredKind(t *testing.T) {
	read := runLogFile(t)
	_ = work(t, "not_registered", map[string]any{}, 3, 3, func(ctx context.Context) error {
		if ref := logger.RunOf(ctx).Ref(); ref.Kind != "not_registered" || ref.ID != "777" {
			t.Errorf("没登记的任务类型按任务类型和 River 任务 ID 记：%+v", ref)
		}
		return errors.New("x")
	})
	if finishes := events(read(), logger.EventRunFinish); len(finishes) != 1 {
		t.Errorf("没登记的任务失败也要有收尾行：%v", finishes)
	}
}

func TestRunLogMiddlewareRestoresRequestIDFromMetadata(t *testing.T) {
	read := runLogFile(t)
	raw, _ := json.Marshal(systemArgs{})
	job := &rivertype.JobRow{ID: 9, Kind: "runlog_system", EncodedArgs: raw, Attempt: 1, MaxAttempts: 25, Metadata: []byte(`{"request_id":"req-meta"}`)}
	_ = queue.NewRunLogMiddleware().Work(context.Background(), job, func(ctx context.Context) error {
		logger.Ctx(ctx).Info("入队请求的编号")
		return nil
	})
	for _, m := range read() {
		if m["msg"] == "入队请求的编号" && m["request_id"] != "req-meta" {
			t.Errorf("应从任务 metadata 还原入队时的请求编号：%v", m)
		}
	}
}

func TestJobArgsImplementRunRef(t *testing.T) {
	cases := []struct {
		args queue.RunArgs
		kind string
	}{
		{queue.TaskArgs{TaskID: 5}, "gen"},
		{queue.DramaArgs{UserID: 3, DramaID: 4, JobID: 5}, "drama"},
		{queue.MarketingRoundArgs{RoundID: 5}, "mkt_round"},
		{queue.StepTrialArgs{TrialID: 5}, "step_trial"},
		{queue.NotifyArgs{TaskID: 5, AgentRunID: 8}, queue.RunKindSystem},
	}
	for _, c := range cases {
		ref := c.args.RunRef()
		if ref.Kind != c.kind {
			t.Errorf("%T 的运行类型 = %q，想要 %q", c.args, ref.Kind, c.kind)
		}
		if c.kind != queue.RunKindSystem && len(ref.ID) != 32 {
			t.Errorf("%T 的运行编号应是界面上的十六进制编号：%q", c.args, ref.ID)
		}
	}
	if ref := (queue.DramaArgs{UserID: 3, JobID: 5}).RunRef(); ref.UserID != 3 {
		t.Errorf("短剧任务应带用户：%+v", ref)
	}
	if ref := (queue.NotifyArgs{TaskID: 5, AgentRunID: 8}).RunRef(); ref.ParentKind != "agent" || ref.ParentID != "8" {
		t.Errorf("Agent 通知应指向那次 Agent 运行：%+v", ref)
	}
}
