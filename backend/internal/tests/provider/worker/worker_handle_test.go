package worker_test

import (
	"context"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/provider"
)

// Handle 只处理“这一步”：不会越级、不会自己循环；到点之前队列不会调用它，所以这里验证每个状态走对分支。
func TestWorker_Handle_DispatchesByStatus(t *testing.T) {
	env := newWkEnv(nil)
	env.task(1, model.TaskPending, nil)
	env.exec.submitFn = func(int, provider.SubmitInput) (*provider.SubmitResult, error) {
		return &provider.SubmitResult{ProviderTaskID: "pt-1"}, nil
	}
	task, _ := env.store.GetTask(context.Background(), 1)
	env.w.Handle(context.Background(), *task)
	if got := env.store.get(1); got.Status != model.TaskQueued || got.ProviderTaskID != "pt-1" {
		t.Fatalf("pending 应走提交并进入 queued：%+v", got)
	}
	if s, q, _ := env.exec.counts(); s != 1 || q != 0 {
		t.Fatalf("一次 Handle 只处理一步：submit=%d query=%d", s, q)
	}
}

// 排队超时：任务一直没拿到名额、已经超过截止时间，队列调用 Expire 走超时流程——置为 expired 并退款，且绝不去请求上游。
func TestWorker_Expire_QueueTimeoutDoesNotTouchUpstream(t *testing.T) {
	env := newWkEnv(nil)
	env.task(1, model.TaskPending, func(tk *model.GenerationTask) { tk.DeadlineAt = env.clock.Now().Add(-time.Minute) })
	task, _ := env.store.GetTask(context.Background(), 1)

	env.w.Expire(context.Background(), *task)

	if got := env.store.get(1); got.Status != model.TaskExpired {
		t.Fatalf("应置为 expired：%+v", got)
	}
	if s, q, d := env.exec.counts(); s != 0 || q != 0 || d != 0 {
		t.Fatalf("没拿到名额的任务没有上游任务，不能调用上游：submit=%d query=%d download=%d", s, q, d)
	}
	if env.store.count("Expire:") != 1 {
		t.Fatalf("应调用一次 Expire：%v", env.store.callList())
	}
}

// 快照损坏的排队任务也能超时退款：不能因为读不出快照就让它永远卡在队列里。
func TestWorker_Expire_WithCorruptSnapshot(t *testing.T) {
	env := newWkEnv(nil)
	env.task(1, model.TaskPending, func(tk *model.GenerationTask) {
		tk.ConfigSnapshot = []byte("not json")
		tk.DeadlineAt = env.clock.Now().Add(-time.Second)
	})
	task, _ := env.store.GetTask(context.Background(), 1)
	env.w.Expire(context.Background(), *task)
	if got := env.store.get(1); got.Status != model.TaskExpired {
		t.Fatalf("快照损坏也应能超时：%+v", got)
	}
}

// 处理中 panic 只影响这一个任务：Handle 不向上传播（队列的 worker goroutine 不会因此崩掉）。
func TestWorker_Handle_PanicIsContained(t *testing.T) {
	env := newWkEnv(nil)
	env.task(1, model.TaskPending, nil)
	env.exec.submitFn = func(int, provider.SubmitInput) (*provider.SubmitResult, error) { panic("boom") }
	task, _ := env.store.GetTask(context.Background(), 1)
	env.w.Handle(context.Background(), *task) // 不应 panic
	if got := env.store.get(1); got.Status != model.TaskPending {
		t.Fatalf("panic 后任务保持原状态，稍后重新处理：%+v", got)
	}
}
