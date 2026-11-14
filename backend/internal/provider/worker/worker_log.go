package worker

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"go.uber.org/zap"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/provider"
)

// 供应商调用日志：每次处理（提交 / 一轮轮询 / 转存）在 app.log 打一行 provider.call，在 payload.log 记本次的钩子步骤全文
// （插件钩子的入参、返回值和 utils.log；宿主替插件发的请求和收到的响应就在请求描述钩子的返回值和解析钩子的入参里）。
// 轮询结果连续相同时折叠：只记第一次和最后一次，中间记一条 folded=N。

// 阶段名：按处理开始时的任务状态。
const (
	phaseSubmit   = "submit"
	phaseQuery    = "query"
	phaseFinalize = "finalize"
)

// foldPruneAfter 是折叠状态的保留时长：任务被取消等原因不再轮询时，状态会留在内存里，超过这么久没动静就清掉。
const foldPruneAfter = 6 * time.Hour

// pollFold 是一个任务正在折叠的轮询：sig 是上一轮的结果签名，n 是之后又出现了几次相同结果，held 是最近一次被折叠掉的记录。
type pollFold struct {
	sig  string
	n    int
	held func()
	seen time.Time
}

// folds 是各任务的折叠状态，进程内有效；换实例或重启后从头记起，最多多记几条。
type folds struct {
	mu sync.Mutex
	m  map[uint64]*pollFold
}

// phaseOf 按任务状态取阶段名；其他状态不调插件，返回空串。
func phaseOf(status string) string {
	switch status {
	case model.TaskPending:
		return phaseSubmit
	case model.TaskQueued, model.TaskRunning:
		return phaseQuery
	case model.TaskFinalizing:
		return phaseFinalize
	}
	return ""
}

// traceFor 给本次处理开追踪：试跑任务按 8KB 截断（要落库给管理端看），正式任务放宽到日志的单字段上限（只写 payload.log）。
func traceFor(ctx context.Context, t *model.GenerationTask) (context.Context, *provider.Trace) {
	if t.IsTest {
		return provider.WithTrace(ctx)
	}
	return provider.WithTraceLimit(ctx, logger.MaxPayloadField)
}

// logProvider 在一次处理结束后记供应商调用；没有发生插件调用（没有步骤）时只把攒着的折叠写出来。
func (w *Worker) logProvider(ctx context.Context, t *model.GenerationTask, phase string, snap **provider.Snapshot, tr *provider.Trace, start time.Time) {
	steps := tr.Steps()
	if phase != phaseQuery || len(steps) == 0 {
		w.flushFold(t.ID)
	}
	if len(steps) == 0 {
		return
	}
	write := w.providerWriter(ctx, t, phase, *snap, steps, time.Since(start))
	if phase != phaseQuery {
		write()
		return
	}
	sig := stepsSignature(steps)
	w.folds.mu.Lock()
	f := w.folds.m[t.ID]
	if f != nil && f.sig == sig {
		f.n++
		f.held, f.seen = write, time.Now()
		w.folds.mu.Unlock()
		return
	}
	w.folds.mu.Unlock()
	w.flushFold(t.ID)
	write()
	w.folds.mu.Lock()
	w.pruneFolds()
	w.folds.m[t.ID] = &pollFold{sig: sig, seen: time.Now()}
	w.folds.mu.Unlock()
}

// flushFold 把一个任务攒着的折叠写出来：中间被省略的次数，加上最后一次的完整记录。
func (w *Worker) flushFold(taskID uint64) {
	w.folds.mu.Lock()
	f := w.folds.m[taskID]
	delete(w.folds.m, taskID)
	w.folds.mu.Unlock()
	if f == nil || f.n == 0 {
		return
	}
	if f.n > 1 {
		logger.Payload(context.Background(), logger.PayloadRecord{Kind: "provider", Extra: []zap.Field{
			logger.GenRun(taskID), zap.Int("folded", f.n-1)}})
	}
	f.held()
}

// pruneFolds 清掉太久没动静的折叠状态（调用方持有锁）。
func (w *Worker) pruneFolds() {
	if w.folds.m == nil {
		w.folds.m = map[uint64]*pollFold{}
	}
	cutoff := time.Now().Add(-foldPruneAfter)
	for id, f := range w.folds.m {
		if f.seen.Before(cutoff) {
			delete(w.folds.m, id)
		}
	}
}

// providerWriter 返回写一次供应商调用记录的函数（折叠时先攒着，之后再写）。
func (w *Worker) providerWriter(ctx context.Context, t *model.GenerationTask, phase string, snap *provider.Snapshot, steps []provider.TraceStep, latency time.Duration) func() {
	callID := logger.NewCallID()
	result, lastErr := "ok", ""
	for _, s := range steps {
		if s.Error != "" {
			result, lastErr = "failed", s.Error
		}
	}
	fields := []zap.Field{
		logger.Event(logger.EventProviderCall),
		zap.String("call_id", callID),
		zap.String("phase", phase),
		zap.Int("steps", len(steps)),
		zap.Int64("latency_ms", latency.Milliseconds()),
		zap.String("result", result),
		zap.String("channel", t.Provider),
		zap.String("model", t.ModelKey),
	}
	if snap != nil {
		fields = append(fields, zap.String("plugin", snap.Plugin.Key), zap.String("plugin_version", snap.Plugin.Version))
	}
	if lastErr != "" {
		fields = append(fields, zap.String("error", lastErr))
	}
	logger.RunOf(ctx).AddProvider()
	logger.RunOf(ctx).NoteCall(callID)
	return func() {
		log := logger.Ctx(ctx)
		if result == "ok" {
			log.Info("供应商调用", fields...)
		} else {
			log.Warn("供应商调用", fields...)
		}
		rec := logger.PayloadRecord{CallID: callID, Kind: "provider", Purpose: "gen." + phase, Steps: steps, LatencyMs: latency.Milliseconds(),
			Extra: []zap.Field{zap.String("phase", phase)}}
		if lastErr != "" {
			rec.Error = lastErr
		}
		logger.Payload(ctx, rec)
	}
}

// stepsSignature 是一次轮询的结果签名：钩子名、返回值和错误，不含入参（入参里有当前时间，每轮都不同）和耗时。
func stepsSignature(steps []provider.TraceStep) string {
	type sigStep struct {
		Kind   string          `json:"k"`
		Hook   string          `json:"h,omitempty"`
		Output json.RawMessage `json:"o,omitempty"`
		Error  string          `json:"e,omitempty"`
	}
	out := make([]sigStep, 0, len(steps))
	for _, s := range steps {
		ss := sigStep{Kind: s.Kind, Error: s.Error}
		if s.Hook != nil {
			ss.Hook, ss.Output = s.Hook.Name, s.Hook.Output
		}
		out = append(out, ss)
	}
	b, _ := json.Marshal(out)
	return string(b)
}
