package worker

import (
	"context"
	"encoding/json"

	"go.uber.org/zap"

	"video-canvas/internal/model"
	"video-canvas/internal/provider"
)

// poll 处理 queued / running 任务：查询一次上游，按统一状态推进。
func (w *Worker) poll(ctx context.Context, t *model.GenerationTask, snap *provider.Snapshot) {
	log := w.taskLog(t)
	if t.ProviderTaskID == "" {
		log.Error("queued / running 任务缺少上游任务 id，直接失败")
		w.fail(ctx, t, snap, failure{class: provider.ClassTerminal, raw: "missing provider task id"})
		return
	}

	// 1. 查询上游：带上插件上次返回的 state
	st := w.providerState(t)
	cctx, cancel := context.WithTimeout(ctx, w.opts.OperationTimeout)
	defer cancel()
	res, err := w.exec.Query(cctx, snap, taskRef(t, st))
	if err != nil {
		w.onPollError(ctx, t, snap, err)
		return
	}
	w.resetCrash(t.ID)
	if res == nil {
		w.fail(ctx, t, snap, failure{class: provider.ClassTerminal, raw: "empty query result"})
		return
	}

	// 2. 按统一状态处理
	switch res.Status {
	case provider.StatusSucceeded:
		w.onPollSucceeded(ctx, t, st, res)
	case provider.StatusFailed:
		w.fail(ctx, t, snap, failureOfResult(res))
	case provider.StatusQueued, provider.StatusRunning:
		w.markPolled(ctx, t, snap, res)
	default:
		// 宿主校验过状态，不应出现其它值；当作 running 继续轮询，避免任务卡死
		log.Warn("上游返回了未知的统一状态，按 running 处理", zap.String("upstream_status", res.Status))
		w.markPolled(ctx, t, snap, res)
	}
}

// onPollError 查询自身出错：runner 相关的错误单独处理；可重试的按轮询节奏继续（间隔指数增长到上限，本身就是退避）；
// 其余直接失败。
func (w *Worker) onPollError(ctx context.Context, t *model.GenerationTask, snap *provider.Snapshot, err error) {
	if w.handleRunnerError(ctx, t, snap, err) {
		return
	}
	w.resetCrash(t.ID)
	if provider.ClassOf(err) != provider.ClassRetryable {
		w.fail(ctx, t, snap, failureOf(err))
		return
	}
	attempts := t.PollAttempts + 1
	delay := pollDelay(snap.Plugin.Meta.Poll, attempts, w.opts.Rand)
	w.taskLog(t).Warn("查询上游失败，稍后重试", zap.Int("attempts", attempts), zap.Duration("delay", delay), zap.Error(err))
	w.retry(ctx, t, attempts, w.opts.Now().Add(delay))
}

// onPollSucceeded 上游出结果：进入 finalizing（立即转存）。成功结果里的新 state 先落库，
// 因为 finalizing 会再查一次产物地址，插件可能需要它；落库失败只影响那次查询拿到旧 state，所以只记警告。
func (w *Worker) onPollSucceeded(ctx context.Context, t *model.GenerationTask, st provider.ProviderState, res *provider.QueryResult) {
	log := w.taskLog(t)
	if len(res.State) > 0 {
		if err := w.store.SaveProviderState(ctx, t, provider.ProviderState{Prepared: st.Prepared, Plugin: res.State}); err != nil {
			log.Warn("保存插件 state 失败，转存时按旧 state 查询", zap.Error(err))
		}
	}
	// 上游侧成本只写日志用于对账
	log.Info("上游生成成功，开始转存", zap.Int("outputs", len(res.Outputs)), zap.Any("provider_cost", res.ProviderCost))
	if _, err := w.store.MarkFinalizing(ctx, t); err != nil {
		log.Error("迁移到 finalizing 失败，租约过期后重试", zap.Error(err))
	}
}

// markPolled 记录一次 queued / running 的查询结果与插件新 state，并按轮询节奏安排下一次查询。
// 未知状态按 running 记录。
func (w *Worker) markPolled(ctx context.Context, t *model.GenerationTask, snap *provider.Snapshot, res *provider.QueryResult) {
	status := res.Status
	if status != provider.StatusQueued {
		status = model.TaskRunning
	}
	attempts := t.PollAttempts + 1
	delay := pollDelay(snap.Plugin.Meta.Poll, attempts, w.opts.Rand)
	var state json.RawMessage
	if len(res.State) > 0 {
		state = res.State
	}
	if _, err := w.store.MarkPolled(ctx, t, status, res.Progress, state, attempts, w.opts.Now().Add(delay)); err != nil {
		w.taskLog(t).Error("更新任务进度失败，租约过期后重试", zap.Error(err))
	}
}
