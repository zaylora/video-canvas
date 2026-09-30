package worker

import (
	"context"
	"errors"
	"time"

	"go.uber.org/zap"

	"video-canvas/internal/model"
	"video-canvas/internal/provider"
)

const (
	// runnerRetryDelay 是 plugin-runner 不可用 / 崩溃后下次处理的间隔：runner 由容器或父进程自动拉起，通常几秒内恢复
	runnerRetryDelay = 10 * time.Second
	// runnerWarnInterval 是“runner 不可用”Warn 日志的全局限频间隔：runner 挂掉时每个 pending 任务每轮都会撞上，
	//    不限频会刷屏；限频之外的记 Debug
	runnerWarnInterval = time.Minute
	// maxCrashRetries 是任务连续遇到 runner 崩溃时的重试次数：崩溃多半是插件耗尽内存，反复重试只会反复打挂 runner
	maxCrashRetries = 1
	// cancelTimeout 是尽力取消上游任务的超时
	cancelTimeout = 10 * time.Second
	// dbRetryTimes / dbRetryBase 是对短暂数据库错误的进程内重试：最多 3 次，间隔 200ms、400ms、600ms
	dbRetryTimes = 3
	dbRetryBase  = 200 * time.Millisecond
)

// failure 是一次要落成 failed 的失败：分类决定给用户的文案，其余只进日志。
type failure struct {
	class       provider.ErrorClass
	code        string // 上游或宿主的错误码（如 plugin_error、invalid_param）
	raw         string // 原始信息，只进日志，不给用户看
	pluginFault bool   // 插件级失败：记 Error 告警
}

// failureOf 从 Executor 返回的错误里取失败信息；不是 *provider.Error 的按 terminal 处理。
func failureOf(err error) failure {
	f := failure{class: provider.ClassOf(err), code: provider.CodeOf(err), raw: err.Error()}
	var pe *provider.Error
	if errors.As(err, &pe) {
		f.pluginFault = pe.PluginFault
	}
	return f
}

// failureOfResult 从统一结果（status=failed）里取失败信息；插件没给分类时按 terminal 处理。
func failureOfResult(res *provider.QueryResult) failure {
	class := res.ErrorClass
	if class == "" {
		class = provider.ClassTerminal
	}
	return failure{class: class, code: res.ErrorCode, raw: res.ErrorMessage}
}

// fail 把任务置为 failed 并退款。给用户看的文案与错误码走 failureFor 的统一映射，原始信息只进日志。
// snap 为 nil 表示快照本身损坏，日志里就没有渠道与插件信息。
func (w *Worker) fail(ctx context.Context, t *model.GenerationTask, snap *provider.Snapshot, f failure) {
	w.forgetTask(t.ID)
	code, msg := failureFor(f.class, f.code)

	log := w.taskLog(t)
	fields := []zap.Field{
		zap.String("class", string(f.class)), zap.String("upstream_code", f.code), zap.String("raw_message", f.raw),
	}
	switch {
	case f.pluginFault:
		// 插件抛异常、超时、返回值不合规、runner 崩溃：是插件或运维问题，用户只看到“平台繁忙”，必须告警
		log.Error("【告警】插件级失败，任务已失败并退积分", append(fields, pluginFields(snap)...)...)
	case f.class == provider.ClassProviderBalance:
		// 上游账户余额不足是运维问题，用户只看到“服务繁忙”，必须告警
		log.Error("【告警】上游账户余额不足，请尽快充值", append(fields, pluginFields(snap)...)...)
	case f.class == provider.ClassSubmitUnknown:
		// 提交读超时，上游可能已经创建了任务并计费，需要人工核对
		log.Warn("提交结果未知（读超时），任务已失败并退积分，请人工核对上游是否产生了任务", fields...)
	default:
		log.Warn("任务失败", fields...)
	}

	if _, err := w.store.Fail(ctx, t, code, msg); err != nil {
		log.Error("标记任务失败时出错，租约过期后重试", zap.Error(err))
	}
}

// pluginFields 是告警里定位问题用的渠道与插件信息。
func pluginFields(snap *provider.Snapshot) []zap.Field {
	if snap == nil {
		return nil
	}
	return []zap.Field{
		zap.String("channel_key", snap.Channel.Key),
		zap.String("plugin_key", snap.Plugin.Key),
		zap.String("plugin_version", snap.Plugin.Version),
		zap.String("plugin_sha256", snap.Plugin.SHA256),
	}
}

// handleRunnerError 处理与 plugin-runner 进程相关的错误，处理了返回 true。提交、轮询、转存查询三处共用：
//   - runner 连不上：不消耗重试次数、不失败，任务留在原状态稍后再试（pending 就停在 pending），由任务 deadline 兜底；
//   - 调用中 runner 崩溃：重试一次，连续第二次崩溃就失败退积分（按插件级失败告警）。
func (w *Worker) handleRunnerError(ctx context.Context, t *model.GenerationTask, snap *provider.Snapshot, err error) bool {
	switch provider.CodeOf(err) {
	case provider.CodeRunnerUnavailable:
		w.noteRunnerUnavailable(t, err)
		w.retry(ctx, t, t.PollAttempts, w.opts.Now().Add(w.jitter(runnerRetryDelay, defaultJitter)))
		return true
	case provider.CodeRunnerCrashed:
		w.onRunnerCrashed(ctx, t, snap, err)
		return true
	}
	return false
}

// noteRunnerUnavailable 记录 runner 不可用：全局每 runnerWarnInterval 最多一条 Warn（带触发它的 task_id），其余记 Debug。
func (w *Worker) noteRunnerUnavailable(t *model.GenerationTask, err error) {
	now := w.opts.Now().UnixNano()
	last := w.lastRunnerWarn.Load()
	log := w.taskLog(t).With(zap.String("status", t.Status), zap.Error(err))
	if now-last >= int64(runnerWarnInterval) && w.lastRunnerWarn.CompareAndSwap(last, now) {
		log.Warn("plugin-runner 不可用，任务保持原状态等待恢复（同类日志限频）")
		return
	}
	log.Debug("plugin-runner 不可用，任务保持原状态等待恢复")
}

// onRunnerCrashed 调用进行中 runner 崩溃：第一次按 retryable 重试（不消耗普通重试次数），连续再崩就失败。
func (w *Worker) onRunnerCrashed(ctx context.Context, t *model.GenerationTask, snap *provider.Snapshot, err error) {
	w.memMu.Lock()
	w.crashes[t.ID]++
	n := w.crashes[t.ID]
	w.memMu.Unlock()
	if n > maxCrashRetries {
		f := failureOf(err)
		f.pluginFault = true // 崩溃计入“插件级失败”，无论宿主是否标记
		w.fail(ctx, t, snap, f)
		return
	}
	delay := w.jitter(runnerRetryDelay, defaultJitter)
	w.taskLog(t).Warn("调用中 plugin-runner 崩溃，稍后重试一次",
		zap.String("status", t.Status), zap.Duration("delay", delay), zap.Error(err))
	w.retry(ctx, t, t.PollAttempts, w.opts.Now().Add(delay))
}

// resetCrash 在一次上游调用没有因 runner 崩溃而失败后清零计数：只有“连续”崩溃才让任务失败，
// 因为 runner 是所有插件共用的，别的插件把它打挂也会波及这个任务。
func (w *Worker) resetCrash(id uint64) {
	w.memMu.Lock()
	delete(w.crashes, id)
	w.memMu.Unlock()
}

// forgetTask 清掉任务的全部进程内缓存（任务终态或放弃处理时调用）。
func (w *Worker) forgetTask(id uint64) {
	w.memMu.Lock()
	delete(w.transferred, id)
	delete(w.crashes, id)
	w.memMu.Unlock()
}

// expire 处理超过 deadline_at 的任务：先尽力通知上游取消（忽略错误），再迁移到 expired 并退款。
func (w *Worker) expire(ctx context.Context, t *model.GenerationTask, snap *provider.Snapshot) {
	w.taskLog(t).Warn("任务超时，置为 expired 并退款", zap.String("status", t.Status), zap.Time("deadline_at", t.DeadlineAt))
	w.forgetTask(t.ID)
	if t.ProviderTaskID != "" {
		w.cancelProvider(ctx, t, snap, taskRef(t, w.providerState(t)))
	}
	if _, err := w.store.Expire(ctx, t); err != nil {
		w.taskLog(t).Error("标记任务超时时出错，租约过期后重试", zap.Error(err))
	}
}

// cancelProvider 尽力通知上游取消，失败只记日志：取消只是为了少花上游额度，任务本身的终态不依赖它。
func (w *Worker) cancelProvider(ctx context.Context, t *model.GenerationTask, snap *provider.Snapshot, ref provider.TaskRef) {
	cctx, cancel := context.WithTimeout(ctx, cancelTimeout)
	defer cancel()
	err := w.exec.Cancel(cctx, snap, ref)
	if err != nil && !errors.Is(err, provider.ErrCancelUnsupported) {
		w.taskLog(t).Warn("通知上游取消任务失败", zap.String("provider_task_id", ref.ProviderTaskID), zap.Error(err))
	}
}

// retry 记录一次可重试的失败并安排下次处理。
func (w *Worker) retry(ctx context.Context, t *model.GenerationTask, attempts int, next time.Time) {
	if _, err := w.store.Retry(ctx, t, attempts, next); err != nil {
		w.taskLog(t).Error("安排任务重试失败，租约过期后重试", zap.Error(err))
	}
}

// retryDB 对短暂的数据库错误做几次进程内重试；ctx 取消时立即返回。
func (w *Worker) retryDB(ctx context.Context, fn func() error) error {
	var err error
	for i := range dbRetryTimes {
		if err = fn(); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(time.Duration(i+1) * dbRetryBase):
		}
	}
	return err
}

// jitter 给 d 加上 ±ratio 的随机抖动。
func (w *Worker) jitter(d time.Duration, ratio float64) time.Duration {
	return applyJitter(d, ratio, w.opts.Rand)
}
