package worker_test

import (
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/provider"
	. "video-canvas/internal/provider/worker"
)

// runner 崩溃：第一次按 retryable 重试（不消耗普通重试次数、任务保持 pending），连续第二次崩溃就失败并计入插件级失败。
func TestWorker_Submit_RunnerCrashRetriesOnceThenFails(t *testing.T) {
	env := newWkEnv(nil)
	env.task(1, model.TaskPending, nil)
	env.exec.submitFn = func(int, provider.SubmitInput) (*provider.SubmitResult, error) {
		return nil, &provider.Error{Class: provider.ClassRetryable, Code: provider.CodeRunnerCrashed, Message: "runner 崩溃", PluginFault: true}
	}

	env.runOnce(t)
	got := env.store.get(1)
	if got.Status != model.TaskPending || got.PollAttempts != 0 {
		t.Fatalf("第一次崩溃应保持 pending 且不消耗重试次数：%+v", got)
	}
	if !got.NextPollAt.Equal(env.clock.Now().Add(10 * time.Second)) {
		t.Fatalf("崩溃后应按 runner 恢复间隔重试：%v", got.NextPollAt)
	}

	env.clock.Advance(10 * time.Second)
	env.runOnce(t)
	got = env.store.get(1)
	if got.Status != model.TaskFailed {
		t.Fatalf("连续第二次崩溃应失败：%+v", got)
	}
	if s, _, _ := env.exec.counts(); s != 2 {
		t.Fatalf("应提交 2 次（原始一次 + 重试一次）：%d", s)
	}
}

// 崩溃后重试成功，计数清零：之后再崩一次仍然只是重试，而不是直接失败。
func TestWorker_Submit_RunnerCrashCounterResetsAfterSuccess(t *testing.T) {
	env := newWkEnv(nil)
	env.task(1, model.TaskPending, nil)
	crash := &provider.Error{Class: provider.ClassRetryable, Code: provider.CodeRunnerCrashed, Message: "runner 崩溃", PluginFault: true}
	env.exec.submitFn = func(n int, in provider.SubmitInput) (*provider.SubmitResult, error) {
		if n == 1 {
			return nil, crash
		}
		return &provider.SubmitResult{ProviderTaskID: "pt-ok"}, nil
	}
	env.exec.queryFn = func(n int, ref provider.TaskRef) (*provider.QueryResult, error) {
		if n == 1 {
			return nil, crash
		}
		return &provider.QueryResult{Status: provider.StatusRunning}, nil
	}
	env.runOnce(t)
	env.clock.Advance(10 * time.Second)
	env.runOnce(t)
	if got := env.store.get(1); got.Status != model.TaskQueued || got.ProviderTaskID != "pt-ok" {
		t.Fatalf("崩溃后重试成功应进入 queued：%+v", got)
	}
	// 轮询时再崩一次：提交成功已把计数清零，所以只是重试
	env.clock.Advance(time.Minute)
	env.runOnce(t)
	if got := env.store.get(1); got.Status == model.TaskFailed {
		t.Fatalf("计数清零后单次崩溃不应失败：%+v", got)
	}
}

// runner 不可用（连不上）：任务停在 pending，不消耗重试次数，不失败，恢复后继续。
func TestWorker_Submit_RunnerUnavailableKeepsPending(t *testing.T) {
	env := newWkEnv(func(o *Options) { o.MaxSubmitRetries = 1 })
	env.task(1, model.TaskPending, nil)
	env.exec.submitFn = func(n int, in provider.SubmitInput) (*provider.SubmitResult, error) {
		if n <= 5 {
			return nil, &provider.Error{Class: provider.ClassRetryable, Code: provider.CodeRunnerUnavailable, Message: "runner 不可用"}
		}
		return &provider.SubmitResult{ProviderTaskID: "pt-ok"}, nil
	}
	for i := 0; i < 5; i++ {
		env.runOnce(t)
		got := env.store.get(1)
		if got.Status != model.TaskPending || got.PollAttempts != 0 {
			t.Fatalf("第 %d 次不可用后应仍是 pending 且不计重试：%+v", i+1, got)
		}
		env.clock.Advance(10 * time.Second)
	}
	env.runOnce(t)
	if got := env.store.get(1); got.Status != model.TaskQueued || got.ProviderTaskID != "pt-ok" {
		t.Fatalf("runner 恢复后应正常提交：%+v", got)
	}
}
