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

// 各种“睡一会再来”的时长。睡眠用 river.JobSnooze：不消耗重试次数，可以无限次。
const (
	// gateWaitBase / gateWaitJitter：本实例这个阶段的并发满了，睡 1~3 秒再试
	gateWaitBase   = time.Second
	gateWaitJitter = 2 * time.Second
	// slotWaitBase / slotWaitJitter：没拿到用户名额或渠道名额，睡 2~6 秒再试。
	// 名额释放没有主动通知，靠这个间隔轮询；越短排队的任务起步越快，越长对数据库的压力越小
	slotWaitBase   = 2 * time.Second
	slotWaitJitter = 4 * time.Second
	// noProgressWait：处理了一步但任务行没有任何变化（多半是写库失败，worker 已经记了日志），5 秒后再试，避免空转
	noProgressWait = 5 * time.Second
	// minSnooze：任务行里的下一次处理时间已经过了，最少睡 1 秒，给任务行的读写留出间隙
	minSnooze = time.Second
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

// submitWorker、pollWorker、finalizeWorker 是三个阶段的 River worker，只是把 job 里的任务 id 交给 Runtime.work，
// 各自带自己的执行时限。
type submitWorker struct {
	river.WorkerDefaults[SubmitArgs]
	rt *Runtime
}

func (w *submitWorker) Timeout(*river.Job[SubmitArgs]) time.Duration { return submitJobTimeout }

func (w *submitWorker) Work(ctx context.Context, job *river.Job[SubmitArgs]) error {
	return w.rt.work(ctx, model.QueueStageSubmit, job.Args.TaskID)
}

type pollWorker struct {
	river.WorkerDefaults[PollArgs]
	rt *Runtime
}

func (w *pollWorker) Timeout(*river.Job[PollArgs]) time.Duration { return pollJobTimeout }

func (w *pollWorker) Work(ctx context.Context, job *river.Job[PollArgs]) error {
	return w.rt.work(ctx, model.QueueStagePoll, job.Args.TaskID)
}

type finalizeWorker struct {
	river.WorkerDefaults[FinalizeArgs]
	rt *Runtime
}

func (w *finalizeWorker) Timeout(*river.Job[FinalizeArgs]) time.Duration { return finalizeJobTimeout }

func (w *finalizeWorker) Work(ctx context.Context, job *river.Job[FinalizeArgs]) error {
	return w.rt.work(ctx, model.QueueStageFinalize, job.Args.TaskID)
}

// jitter 返回 base 加上 [0, spread) 的随机量，让大量同时睡下的 job 不会同时醒来。
func (rt *Runtime) jitter(base, spread time.Duration) time.Duration {
	return base + time.Duration(rt.rand()*float64(spread))
}

// work 处理一个 job：读任务行 → 阶段核对 → 睡到处理时间 → 抢本阶段的并发闸门 → 提交阶段申请名额 → 调用业务处理一步 → 安排下一步。
// 返回 nil 表示这个 job 结束（任务终态，或已把任务交给别的阶段）；返回 river.JobSnooze 表示睡一会再来；
// 返回其它错误才会触发 River 的重试（只有读写数据库这类基础设施故障才会走到这里，业务失败都已经写进任务行）。
func (rt *Runtime) work(ctx context.Context, stage string, taskID uint64) error {
	// 1. 读任务行。任务不存在或已是终态：什么都不用做，job 结束（用户取消、超时、别的实例处理完都会走到这里）
	t, err := rt.store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	if t == nil || model.IsTerminalStatus(t.Status) {
		return nil
	}

	// 2. 阶段核对：任务已经进入别的阶段（例如救回后 job 比任务行慢了一步），把它交给对应阶段的队列，当前 job 结束
	cur, _ := model.QueueStageOfStatus(t.Status)
	if cur != stage {
		return rt.handOff(ctx, t)
	}

	// 3. 还没到处理时间（上一次安排的下一次查询 / 重试时间，或 job 被救回后提前醒来）：回去睡到点
	if d := t.NextPollAt.Sub(rt.now()); d > dueSlack {
		return river.JobSnooze(d)
	}

	// 4. 抢本阶段的并发闸门：本实例这个阶段同时执行的数量由后台设置控制，满了就睡一会再来
	g := rt.gates[stage]
	if !g.tryAcquire() {
		return river.JobSnooze(rt.jitter(gateWaitBase, gateWaitJitter))
	}
	defer g.release()

	// 5. 提交阶段：任务还没拿到名额时先排队等名额
	if t.Status == model.TaskPending && t.SlotAcquiredAt == nil {
		t, err = rt.waitForSlot(ctx, t)
		if err != nil || t == nil {
			return err
		}
	}

	// 6. 交给业务处理这一步。ctx 被取消（停机）时直接返回，任务行没变，下次启动会被救回
	rt.handler.Handle(ctx, *t)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return rt.afterStep(ctx, stage, t)
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

// afterStep 在业务处理完一步之后读任务行，决定这个 job 的去向：
//   - 终态：结束；
//   - 进入了别的阶段：把任务交给对应阶段的队列，当前 job 结束；
//   - 还在本阶段：按任务行里新的 next_poll_at 睡到点（没有任何变化时睡 noProgressWait 避免空转）。
func (rt *Runtime) afterStep(ctx context.Context, stage string, before *model.GenerationTask) error {
	t, err := rt.store.GetTask(ctx, before.ID)
	if err != nil {
		return err
	}
	if t == nil || model.IsTerminalStatus(t.Status) {
		return nil
	}
	if cur, _ := model.QueueStageOfStatus(t.Status); cur != stage {
		return rt.handOff(ctx, t)
	}
	if t.NextPollAt.Equal(before.NextPollAt) {
		return river.JobSnooze(noProgressWait)
	}
	return river.JobSnooze(max(t.NextPollAt.Sub(rt.now()), minSnooze))
}

// handOff 把任务交给它当前所处阶段的队列（按 next_poll_at 排期）。入队失败返回错误，当前 job 由 River 重试，
// 所以交接不会丢任务；真正可能留下孤儿的只有“任务状态已变、进程恰好在入队前崩溃”，那种情况由定时对账补上。
func (rt *Runtime) handOff(ctx context.Context, t *model.GenerationTask) error {
	stage, ok := model.QueueStageOfStatus(t.Status)
	if !ok {
		return nil
	}
	return rt.insert(ctx, stage, t.ID, t.NextPollAt)
}
