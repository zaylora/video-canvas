package queue

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"go.uber.org/zap"

	"video-canvas/internal/model"
	"video-canvas/internal/provider"
)

// notifyWorker 是通知 Agent 的 River worker：读任务的最终状态，交给 provider.AgentNotifier 送达。
type notifyWorker struct {
	river.WorkerDefaults[NotifyArgs]
	rt *Runtime
}

func (w *notifyWorker) Timeout(*river.Job[NotifyArgs]) time.Duration { return 30 * time.Second }

func (w *notifyWorker) Work(ctx context.Context, job *river.Job[NotifyArgs]) error {
	t, err := w.rt.store.GetTask(ctx, job.Args.TaskID)
	if err != nil {
		return err
	}
	if t == nil || !model.IsTerminalStatus(t.Status) {
		return nil // 任务没了，或还没真的结束（不应发生）：没什么可通知的
	}
	if err := w.rt.cfg.Notifier.TaskSettled(ctx, provider.TaskSettledNotice{TaskID: t.ID, AgentRunID: job.Args.AgentRunID, Status: t.Status}); err != nil {
		w.rt.log.Warn("通知 Agent 任务结束失败，稍后重试", zap.Uint64("task_id", t.ID), zap.Int("attempt", job.Attempt), zap.Error(err))
		return err
	}
	return nil
}

// EnqueueAgentNotifyTx 实现 repository.TaskEnqueuer：在 sqlTx 所在的事务里入队“任务结束了”的通知。
// 没有启用 Agent（Config.AgentNotify=false）时什么都不做，因为没有人会处理这种 job。
func (rt *Runtime) EnqueueAgentNotifyTx(ctx context.Context, sqlTx *sql.Tx, taskID, agentRunID uint64) error {
	if !rt.cfg.AgentNotify {
		return nil
	}
	if _, err := rt.client.InsertTx(ctx, sqlTx, NotifyArgs{TaskID: taskID, AgentRunID: agentRunID}, nil); err != nil {
		return fmt.Errorf("入队 Agent 通知失败：%w", err)
	}
	return nil
}

// retryPolicy 是 River 客户端的重试策略：通知 job 用封顶的指数退避（起点 NotifyBackoff，翻倍，最长 AgentNotifyRetryCap），
// 其它 job 沿用 River 默认（生成任务的业务重试记在任务行里，这里只兜基础设施故障）。
type retryPolicy struct {
	base time.Duration
	def  river.DefaultClientRetryPolicy
}

func (p *retryPolicy) NextRetry(job *rivertype.JobRow) time.Time {
	if job.Kind != kindNotify {
		return p.def.NextRetry(job)
	}
	d := time.Duration(float64(p.base) * math.Pow(2, float64(max(job.Attempt-1, 0))))
	if d > provider.AgentNotifyRetryCap || d <= 0 {
		d = provider.AgentNotifyRetryCap
	}
	return time.Now().Add(d)
}
