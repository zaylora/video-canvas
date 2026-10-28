package queue_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
)

// fakeNotifier 记录每次通知；failFirst 让前 N 次失败，用来验证退避重试。
type fakeNotifier struct {
	mu        sync.Mutex
	calls     []provider.TaskSettledNotice
	failFirst int
}

func (f *fakeNotifier) TaskSettled(_ context.Context, n provider.TaskSettledNotice) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, n)
	if len(f.calls) <= f.failFirst {
		return errors.New("Agent 暂时不可用")
	}
	return nil
}

func (f *fakeNotifier) snapshot() []provider.TaskSettledNotice {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]provider.TaskSettledNotice(nil), f.calls...)
}

// settleInTx 在一个事务里把任务置为终态并入队门铃，和 GenerationTaskService.finish 的写法一致。
func (r *rig) settleInTx(t *testing.T, tk *model.GenerationTask, status string, rollback error) error {
	t.Helper()
	return r.repo.WithTx(context.Background(), func(tx repository.GenerationTaskTx) error {
		if _, err := tx.UpdateIf(context.Background(), tk.ID, model.ActiveTaskStatuses, map[string]any{"status": status, "finished_at": time.Now()}, true); err != nil {
			return err
		}
		if err := tx.EnqueueAgentNotify(context.Background(), tk); err != nil {
			return err
		}
		return rollback
	})
}

func (r *rig) notifyJobs(taskID uint64) int64 {
	var n int64
	r.db.Raw(`SELECT COUNT(*) FROM river_job WHERE kind = 'agent.task_settled' AND (args->>'task_id')::bigint = ?`, taskID).Scan(&n)
	return n
}

// 任务终态的事务提交后，Agent 收到一次通知：带任务 id、发起它的运行 id 和最终状态。
func TestRuntime_AgentNotify_DeliversSettledTask(t *testing.T) {
	db := queueDB(t)
	addChannel(t, db, "ch", 0)
	n := &fakeNotifier{}
	r := newRig(t, db, rigOpts{notifier: n, agentNotify: true})
	tk := r.addTask(t, newUserID(), "ch", func(tk *model.GenerationTask) {
		tk.AgentRunID = 9001
		tk.NextPollAt = time.Now().Add(time.Hour) // 让队列里的生成 job 先睡着，只测门铃
	})

	if err := r.settleInTx(t, tk, model.TaskFailed, nil); err != nil {
		t.Fatal(err)
	}

	waitFor(t, 15*time.Second, "Agent 应收到通知", func() bool { return len(n.snapshot()) == 1 })
	got := n.snapshot()[0]
	if got.TaskID != tk.ID || got.AgentRunID != 9001 || got.Status != model.TaskFailed {
		t.Fatalf("通知内容不对：%+v", got)
	}
}

// 通知失败时按退避重试，直到送达；送达后不再重复。
func TestRuntime_AgentNotify_RetriesUntilDelivered(t *testing.T) {
	db := queueDB(t)
	addChannel(t, db, "ch", 0)
	n := &fakeNotifier{failFirst: 2}
	r := newRig(t, db, rigOpts{notifier: n, agentNotify: true})
	tk := r.addTask(t, newUserID(), "ch", func(tk *model.GenerationTask) {
		tk.AgentRunID = 9001
		tk.NextPollAt = time.Now().Add(time.Hour)
	})
	if err := r.settleInTx(t, tk, model.TaskSucceeded, nil); err != nil {
		t.Fatal(err)
	}

	waitFor(t, 20*time.Second, "失败两次后第三次应送达", func() bool { return len(n.snapshot()) >= 3 })
	time.Sleep(500 * time.Millisecond)
	if c := len(n.snapshot()); c != 3 {
		t.Fatalf("送达后不应再通知，实际调用 %d 次", c)
	}
}

// 事务回滚时门铃也不存在：任务没有真的结束，不能通知 Agent。
func TestRuntime_AgentNotify_RollsBackWithTransaction(t *testing.T) {
	db := queueDB(t)
	addChannel(t, db, "ch", 0)
	n := &fakeNotifier{}
	r := newRig(t, db, rigOpts{notifier: n, agentNotify: true, noStart: true})
	tk := r.addTask(t, newUserID(), "ch", func(tk *model.GenerationTask) { tk.AgentRunID = 9001 })

	rollback := fmt.Errorf("回滚")
	if err := r.settleInTx(t, tk, model.TaskFailed, rollback); !errors.Is(err, rollback) {
		t.Fatalf("应返回回滚用的错误：%v", err)
	}
	if c := r.notifyJobs(tk.ID); c != 0 {
		t.Fatalf("事务回滚后不应有门铃 job，实际 %d", c)
	}
}

// Agent 没有启用时不入队：没有人会处理这种 job。
func TestRuntime_AgentNotify_DisabledEnqueuesNothing(t *testing.T) {
	db := queueDB(t)
	addChannel(t, db, "ch", 0)
	r := newRig(t, db, rigOpts{noStart: true})
	tk := r.addTask(t, newUserID(), "ch", func(tk *model.GenerationTask) { tk.AgentRunID = 9001 })

	if err := r.settleInTx(t, tk, model.TaskSucceeded, nil); err != nil {
		t.Fatal(err)
	}
	if c := r.notifyJobs(tk.ID); c != 0 {
		t.Fatalf("Agent 未启用时不应入队，实际 %d", c)
	}
}

// 同一个任务重复入队（例如终态处理被重试）只会有一个活着的门铃 job。
func TestRuntime_AgentNotify_DedupesLiveJobs(t *testing.T) {
	db := queueDB(t)
	addChannel(t, db, "ch", 0)
	r := newRig(t, db, rigOpts{notifier: &fakeNotifier{}, agentNotify: true, noStart: true})
	tk := r.addTask(t, newUserID(), "ch", func(tk *model.GenerationTask) { tk.AgentRunID = 9001 })

	for i := 0; i < 3; i++ {
		err := r.repo.WithTx(context.Background(), func(tx repository.GenerationTaskTx) error {
			return tx.EnqueueAgentNotify(context.Background(), tk)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if c := r.notifyJobs(tk.ID); c != 1 {
		t.Fatalf("重复入队应被唯一约束挡住，实际 %d 个 job", c)
	}
}
