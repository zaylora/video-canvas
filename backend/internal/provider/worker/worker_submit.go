package worker

import (
	"context"
	"encoding/json"
	"fmt"

	"go.uber.org/zap"

	"video-canvas/internal/model"
	"video-canvas/internal/provider"
)

// submit 处理 pending 任务：交给插件宿主提交，按结果进入轮询（异步）、直接转存（同步即时成功）或失败。
func (w *Worker) submit(ctx context.Context, t *model.GenerationTask, snap *provider.Snapshot) {
	log := w.taskLog(t)

	// 1. 解出已校验的规范化输入（媒体字段是 asset id，宿主在提交前再做一次归属与大小校验）
	var input map[string]any
	if err := json.Unmarshal(t.InputJSON, &input); err != nil {
		log.Error("任务输入损坏，直接失败", zap.Error(err))
		w.fail(ctx, t, snap, failure{class: provider.ClassTerminal, code: codeInvalidParam, raw: "invalid input"})
		return
	}

	// 2. 提交。带上已有的 provider_state：上次准备阶段成功、提交失败重试时，宿主看到 Prepared 非空就跳过准备，不会重复上传
	st := w.providerState(t)
	cctx, cancel := context.WithTimeout(ctx, w.opts.OperationTimeout)
	defer cancel()
	res, err := w.exec.Submit(cctx, snap, provider.SubmitInput{
		Task:       taskRef(t, st),
		Input:      input,
		OnPrepared: w.savePrepared(t, st),
	})
	if err != nil {
		w.onSubmitError(ctx, t, snap, err)
		return
	}
	w.resetCrash(t.ID)
	if res == nil {
		w.fail(ctx, t, snap, failure{class: provider.ClassTerminal, raw: "empty submit result"})
		return
	}

	// 3. 按结果分流。Immediate 的 queued / running 等同于异步受理（上游只是顺便报了个状态）
	if im := res.Immediate; im != nil {
		switch im.Status {
		case provider.StatusSucceeded:
			w.onImmediate(ctx, t, snap, res)
			return
		case provider.StatusFailed:
			w.fail(ctx, t, snap, failureOfResult(im))
			return
		}
	}
	w.onSubmitted(ctx, t, snap, res)
}

// savePrepared 返回 OnPrepared 回调：把准备阶段的结果并进 provider_state 落库（保留插件 state），
// 之后提交阶段重试不会重复上传。失败返回错误，宿主按 retryable 处理（没落库，下次会重新准备）。
func (w *Worker) savePrepared(t *model.GenerationTask, st provider.ProviderState) func(context.Context, json.RawMessage) error {
	return func(ctx context.Context, prepared json.RawMessage) error {
		merged := provider.ProviderState{Prepared: prepared, Plugin: st.Plugin}
		if err := w.store.SaveProviderState(ctx, t, merged); err != nil {
			return fmt.Errorf("保存准备阶段结果失败：%w", err)
		}
		return nil
	}
}

// submittedState 取提交后要持久化的插件 state：Immediate 带的 state 比外层的新，优先用它；都没有为 nil（不变）。
func submittedState(res *provider.SubmitResult) json.RawMessage {
	if res.Immediate != nil && len(res.Immediate.State) > 0 {
		return res.Immediate.State
	}
	return res.State
}

// submittedRef 是刚提交成功、还没落库的上游任务的引用，用于“提交期间任务被取消”时尽力取消上游。
func submittedRef(t *model.GenerationTask, providerTaskID string, state json.RawMessage) provider.TaskRef {
	return provider.TaskRef{ID: t.ID, UserID: t.UserID, ProviderTaskID: providerTaskID, State: state}
}

// onSubmitted 上游已异步受理：记录上游任务 id 与插件 state，安排第一次查询。
func (w *Worker) onSubmitted(ctx context.Context, t *model.GenerationTask, snap *provider.Snapshot, res *provider.SubmitResult) {
	log := w.taskLog(t)
	pid := res.ProviderTaskID
	if pid == "" {
		log.Error("上游提交成功但没有返回任务 id")
		w.fail(ctx, t, snap, failure{class: provider.ClassTerminal, raw: "empty provider task id"})
		return
	}

	// 这一步失败会导致任务保持 pending、下次又被重新提交给上游（重复计费），所以先在进程内重试几次，
	// 最终仍失败时把上游任务 id 写进错误日志，方便人工核对
	next := w.opts.Now().Add(firstDelay(snap.Plugin.Meta.Poll))
	state := submittedState(res)
	var applied bool
	err := w.retryDB(ctx, func() (e error) {
		applied, e = w.store.MarkSubmitted(ctx, t, pid, state, next)
		return e
	})
	if err != nil {
		log.Error("上游已受理但记录上游任务 id 失败，任务可能被重复提交，请人工核对",
			zap.String("provider_task_id", pid), zap.Error(err))
		return
	}
	if !applied {
		// 提交期间任务被取消 / 超时了：上游已经产生了任务，尽力取消它，避免白白消耗上游额度
		log.Warn("提交期间任务已被取消或超时，尽力取消上游任务", zap.String("provider_task_id", pid))
		w.cancelProvider(ctx, t, snap, submittedRef(t, pid, state))
		return
	}
	log.Info("任务已提交给上游", zap.String("provider_task_id", pid))
}

// onImmediate 提交即成功（同步接口）：把产物写进 provider_result 并迁到 finalizing。store 会立即安排并唤醒转存；
// 之后 finalizing 直接用 provider_result，不再调用上游，所以“转存前进程被杀”重启后上游仍只被调用一次。
func (w *Worker) onImmediate(ctx context.Context, t *model.GenerationTask, snap *provider.Snapshot, res *provider.SubmitResult) {
	log := w.taskLog(t)
	outs := res.Immediate.Outputs
	if len(outs) == 0 {
		log.Error("上游同步返回成功但没有产物")
		w.fail(ctx, t, snap, failure{class: provider.ClassTerminal, raw: "no outputs"})
		return
	}
	raw, err := json.Marshal(outs)
	if err != nil {
		log.Error("序列化即时产物失败", zap.Error(err))
		w.fail(ctx, t, snap, failure{class: provider.ClassTerminal, raw: "marshal outputs"})
		return
	}

	// 与 MarkSubmitted 同理：落库失败会让任务留在 pending 被重新提交（上游重复计费），先进程内重试
	pid, state := res.ProviderTaskID, submittedState(res)
	var applied bool
	err = w.retryDB(ctx, func() (e error) {
		applied, e = w.store.MarkImmediate(ctx, t, pid, state, raw)
		return e
	})
	if err != nil {
		log.Error("上游已同步出结果但落库失败，任务可能被重复提交，请人工核对",
			zap.String("provider_task_id", pid), zap.Int("outputs", len(outs)), zap.Error(err))
		return
	}
	if !applied {
		log.Warn("提交期间任务已被取消或超时，丢弃即时结果", zap.String("provider_task_id", pid))
		if pid != "" {
			w.cancelProvider(ctx, t, snap, submittedRef(t, pid, state))
		}
		return
	}
	log.Info("上游同步返回结果，开始转存", zap.String("provider_task_id", pid), zap.Int("outputs", len(outs)))
}

// onSubmitError 按错误分类处理提交失败：runner 相关的错误单独处理；可重试的退避重试（有上限）；其余直接失败退款。
func (w *Worker) onSubmitError(ctx context.Context, t *model.GenerationTask, snap *provider.Snapshot, err error) {
	if w.handleRunnerError(ctx, t, snap, err) {
		return
	}
	w.resetCrash(t.ID)
	if provider.ClassOf(err) != provider.ClassRetryable {
		w.fail(ctx, t, snap, failureOf(err))
		return
	}
	attempts := t.PollAttempts + 1
	if attempts > w.opts.MaxSubmitRetries {
		w.taskLog(t).Warn("提交重试次数耗尽", zap.Int("attempts", attempts), zap.Error(err))
		w.fail(ctx, t, snap, failureOf(err))
		return
	}
	delay := w.jitter(submitBackoff(attempts), defaultJitter)
	w.taskLog(t).Warn("提交失败，稍后重试", zap.Int("attempts", attempts), zap.Duration("delay", delay), zap.Error(err))
	w.retry(ctx, t, attempts, w.opts.Now().Add(delay))
}
