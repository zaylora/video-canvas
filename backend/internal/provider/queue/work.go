package queue

import (
	"context"
	"time"

	"github.com/riverqueue/river"
	"go.uber.org/zap"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/provider"
)

// 各种“睡一会再来”的时长。让出 job 用 river.JobSnooze：不消耗重试次数，可以无限次。
const (
	// gateWaitBase / gateWaitJitter：本实例的并发满了，睡 1~3 秒再试
	gateWaitBase   = time.Second
	gateWaitJitter = 2 * time.Second
	// slotWaitBase / slotWaitJitter：没拿到用户名额或渠道名额，睡 2~6 秒再试。
	// 名额释放没有主动通知，靠这个间隔轮询；越短排队的任务起步越快，越长对数据库的压力越小
	slotWaitBase   = 2 * time.Second
	slotWaitJitter = 4 * time.Second
	// noProgressWait：处理了一步但任务行没有任何变化（多半是写库失败，worker 已经记了日志），5 秒后再试，避免空转
	noProgressWait = 5 * time.Second
	// minStep：两步之间最少隔这么久，给任务行的读写留出间隙，也防止异常情况下的空转
	minStep = 200 * time.Millisecond
	// dueSlack：job 提前不到这么久醒来就直接处理，不再睡（调度本来就有秒级的误差）
	dueSlack = time.Second
)

// Handler 是队列取出任务后调用的业务执行者（真实实现是 provider/worker.Worker）。
type Handler interface {
	// Handle 处理任务的“这一步”（提交 / 轮询 / 转存 / 超时）。结果写在任务行里，不返回错误。
	Handle(ctx context.Context, t model.GenerationTask)
	// Expire 让一个已经超过截止时间、还没拿到名额的任务走超时流程（置为 expired 并退款）。
	Expire(ctx context.Context, t model.GenerationTask)
}

// taskWorker 是生成任务的 River worker，只是把 job 里的任务 id 交给 Runtime.work。
type taskWorker struct {
	river.WorkerDefaults[TaskArgs]
	rt *Runtime
}

func (w *taskWorker) Timeout(*river.Job[TaskArgs]) time.Duration { return jobTimeout }

func (w *taskWorker) Work(ctx context.Context, job *river.Job[TaskArgs]) error {
	return w.rt.work(ctx, job.Args.TaskID)
}

// jitter 返回 base 加上 [0, spread) 的随机量，让大量同时睡下的 job 不会同时醒来。
func (rt *Runtime) jitter(base, spread time.Duration) time.Duration {
	return base + time.Duration(rt.rand()*float64(spread))
}

// work 处理一个 job：读任务行 → 睡到处理时间 → 抢并发闸门 → 还没名额的先排队等名额 → 在本 job 里一直处理到任务终态。
// 返回 nil 表示这个 job 结束（任务终态）；返回 river.JobSnooze 表示睡一会再来（排队中，不占闸门）；
// 返回其它错误才会触发 River 的重试（读写数据库这类基础设施故障、停机被取消，业务失败都已经写进任务行）。
func (rt *Runtime) work(ctx context.Context, taskID uint64) error {
	// 1. 读任务行。任务不存在或已是终态：什么都不用做，job 结束（用户取消、超时、别的实例处理完都会走到这里）
	t, err := rt.store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	if t == nil || model.IsTerminalStatus(t.Status) {
		return nil
	}

	// 2. 还没到处理时间（job 被救回后任务行里记着下一次查询时间）：回去睡到点，睡着时不占闸门
	if d := t.NextPollAt.Sub(rt.now()); d > dueSlack {
		return river.JobSnooze(d)
	}

	// 3. 抢并发闸门：本实例同时处理的任务数由后台设置控制，满了就睡一会再来
	if !rt.gate.tryAcquire() {
		return river.JobSnooze(rt.jitter(gateWaitBase, gateWaitJitter))
	}
	defer rt.gate.release()

	// 4. 任务还没拿到名额：先排队等名额。拿不到时让出闸门、把 job 睡一会，所以排队的任务不占并发
	if t.Status == model.TaskPending && t.SlotAcquiredAt == nil {
		t, err = rt.waitForSlot(ctx, t)
		if err != nil || t == nil {
			return err
		}
	}

	// 5. 拿到名额，一路处理到终态
	return rt.drive(ctx, t)
}

// waitForSlot 为 pending 任务申请名额。返回的任务是拿到名额之后的最新行（deadline_at 已重置）；
// 返回 (nil, nil) 表示这个 job 不用继续处理了；返回 (nil, err) 的 err 可能是 river.JobSnooze（名额还没轮到）。
//
//  1. 排队已经超过截止时间：不再申请，直接走超时流程（置为 expired 并退款，文案是“排队超时”）；
//  2. 拿到名额：重读任务行（要用新的 deadline_at 与 slot_acquired_at）继续；
//  3. 任务已被取消 / 超时：job 结束；
//  4. 其它（用户名额满 / 渠道名额满 / 前面还有更早的等待者）：睡一会再来，不消耗重试次数。
func (rt *Runtime) waitForSlot(ctx context.Context, t *model.GenerationTask) (*model.GenerationTask, error) {
	if rt.now().After(t.DeadlineAt) {
		rt.handler.Expire(ctx, *t)
		return nil, nil
	}
	out, err := rt.store.AcquireSlot(ctx, t)
	if err != nil {
		return nil, err
	}
	switch out {
	case provider.SlotAcquired:
		fresh, err := rt.store.GetTask(ctx, t.ID)
		if err != nil || fresh == nil || model.IsTerminalStatus(fresh.Status) {
			return nil, err
		}
		return fresh, nil
	case provider.SlotGone:
		return nil, nil
	default:
		rt.log.Debug("任务排队等名额", logger.TaskID(t.ID), zap.String("reason", string(out)))
		return nil, river.JobSnooze(rt.jitter(slotWaitBase, slotWaitJitter))
	}
}

// drive 让一个已经拿到名额的任务在本 job 里一直走到终态：调用业务处理一步 → 读任务行 → 终态就结束，
// 否则按任务行里新的 next_poll_at 等到点（没有任何变化时等 noProgressWait 避免空转）→ 再处理下一步。
// 等待期间占着并发闸门（任务正在生成中，这就是“并发”的含义）。ctx 被取消（停机、job 超时）时返回 ctx 的错误：
// 任务行没丢，job 会被重试或被对账救回，从任务行记录的状态接着走。
func (rt *Runtime) drive(ctx context.Context, t *model.GenerationTask) error {
	for {
		rt.handler.Handle(ctx, *t)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		next, err := rt.store.GetTask(ctx, t.ID)
		if err != nil {
			return err
		}
		if next == nil || model.IsTerminalStatus(next.Status) {
			return nil
		}
		wait := max(next.NextPollAt.Sub(rt.now()), minStep)
		if next.NextPollAt.Equal(t.NextPollAt) && next.Status == t.Status {
			wait = max(wait, noProgressWait)
		}
		if err := sleepCtx(ctx, wait); err != nil {
			return err
		}
		t = next
	}
}

// sleepCtx 睡 d，ctx 取消时提前返回它的错误。
func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
