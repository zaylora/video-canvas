package logger

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// 事件类型：每行日志的 event 字段，稳定的英文取值，给 jq 过滤和计数。message 仍按规范写中文短句。
const (
	EventHTTPRequest  = "http.request"  // 访问日志：每个 HTTP 请求一行汇总
	EventRunCreate    = "run.create"    // 运行入队或创建
	EventRunStart     = "run.start"     // 运行真正开始干活
	EventRunStep      = "run.step"      // 运行里的业务过程节点
	EventLLMCall      = "llm.call"      // 一次文本模型调用（网关）
	EventProviderCall = "provider.call" // 一次供应商插件调用（插件宿主）
	EventExtCall      = "ext.call"      // 一次其他外部 HTTP 调用（统一出站客户端）
	EventRunRetry     = "run.retry"     // 后台任务出错，队列将重试
	EventRunFinish    = "run.finish"    // 运行终态：每次运行一行汇总
)

// 运行终态。
const (
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusCanceled  = "canceled"
	StatusExpired   = "expired"
	StatusPaused    = "paused"
	StatusSkipped   = "skipped" // 依赖没满足，本次没有执行（不算失败）
	StatusUnknown   = "unknown" // 兜底收尾：业务开始了运行却没有收尾
)

// ErrorTypeInternal 是需要人处理的失败类型，run.finish 用 Error 级输出。
const ErrorTypeInternal = "internal"

// FieldNeedsAttention 是 run.finish 的可选字段：业务认为这次失败需要人处理（如合成时 ffmpeg 出错）时带上 true，收尾行用 Error 级。
const FieldNeedsAttention = "needs_attention"

// Event 返回 event 字段。
func Event(name string) zap.Field { return zap.String("event", name) }

// RunRef 标识一次运行。ID 和这次运行在界面上显示的编号相同（生成任务、短剧、营销为 idcodec 十六进制，Agent 为十进制）。
type RunRef struct {
	Kind       string // gen / drama / mkt_round / mkt / agent / adapt / preflight / step_trial / system
	ID         string
	UserID     uint64
	RequestID  string // 由某个请求入队的后台任务，在执行时还原请求编号
	ParentKind string // 由另一个运行派生时才有
	ParentID   string
}

// Run 是一次运行在本进程里的日志状态：开始 / 收尾各打一次，调用计数累加到收尾行。nil 接收者的方法都是空操作。
type Run struct {
	ref      RunRef
	userID   atomic.Uint64
	begun    time.Time
	started  atomic.Bool
	ended    atomic.Bool
	mu       sync.Mutex
	startAt  time.Time
	lastCall string          // 最后一次上游调用的 call_id，失败收尾时带上
	ctx      context.Context // 打开始 / 收尾行时取关联字段用

	llmCalls, providerCalls, extCalls atomic.Int64
	inputTokens, outputTokens         atomic.Int64
}

// BeginRun 把一次运行挂到 ctx 上，之后 Ctx(ctx) 打的每一行都带 run_kind / run_id。它本身不打日志：
// 真正开始干活时调用 Start，到终态调用 End。在 HTTP 请求里调用时，访问日志会带上这次运行。
func BeginRun(ctx context.Context, ref RunRef) (context.Context, *Run) {
	r := &Run{ref: ref, begun: time.Now()}
	r.userID.Store(ref.UserID)
	ctx = with(ctx, func(s *state) { s.run = r })
	r.ctx = ctx
	NoteRun(ctx, ref.Kind, ref.ID)
	return ctx, r
}

// EnsureRun 返回 ctx 上已有的同一个运行（队列中间件挂上的）；没有或不是同一个时新开一个。
// 业务入口用它，这样不经过队列直接调用（测试、同步路径）时也有运行上下文。
func EnsureRun(ctx context.Context, ref RunRef) (context.Context, *Run) {
	if r := RunOf(ctx); r != nil && r.ref.Kind == ref.Kind && r.ref.ID == ref.ID {
		if ref.UserID != 0 {
			r.SetUser(ref.UserID)
		}
		return ctx, r
	}
	return BeginRun(ctx, ref)
}

// RunOf 返回 ctx 上的运行；没有时返回 nil（方法都可以安全调用）。
func RunOf(ctx context.Context) *Run { return from(ctx).run }

// Ref 返回运行标识。
func (r *Run) Ref() RunRef {
	if r == nil {
		return RunRef{}
	}
	return r.ref
}

// SetUser 补上运行所属的用户（任务参数里没有、执行时才查到的情况）。
func (r *Run) SetUser(userID uint64) {
	if r != nil {
		r.userID.Store(userID)
	}
}

// user 返回运行所属的用户，没有为 0。
func (r *Run) user() uint64 {
	if r == nil {
		return 0
	}
	return r.userID.Load()
}

// Start 打 run.start，只打一次。fields 是业务字段（如短剧的 resumed）。
func (r *Run) Start(fields ...zap.Field) {
	if r == nil || !r.started.CompareAndSwap(false, true) {
		return
	}
	r.mu.Lock()
	r.startAt = time.Now()
	r.mu.Unlock()
	Ctx(r.ctx).WithOptions(zap.AddCallerSkip(1)).Info("运行开始", append([]zap.Field{Event(EventRunStart)}, fields...)...)
}

// Started 表示 Start 已调用。
func (r *Run) Started() bool { return r != nil && r.started.Load() }

// Ended 表示 End 已调用。
func (r *Run) Ended() bool { return r != nil && r.ended.Load() }

// AddLLM 记一次文本模型调用和它的用量，收尾行汇总。由网关观察者调用。
func (r *Run) AddLLM(inputTokens, outputTokens int) {
	if r == nil {
		return
	}
	r.llmCalls.Add(1)
	r.inputTokens.Add(int64(inputTokens))
	r.outputTokens.Add(int64(outputTokens))
}

// NoteCall 记下最后一次上游调用的 call_id；运行没有成功收尾时，收尾行带 last_call_id 指向它。
func (r *Run) NoteCall(callID string) {
	if r == nil || callID == "" {
		return
	}
	r.mu.Lock()
	r.lastCall = callID
	r.mu.Unlock()
}

// AddProvider 记一次供应商插件调用。由插件宿主调用。
func (r *Run) AddProvider() {
	if r != nil {
		r.providerCalls.Add(1)
	}
}

// AddExt 记一次其他外部 HTTP 调用。由统一出站客户端调用。
func (r *Run) AddExt() {
	if r != nil {
		r.extCalls.Add(1)
	}
}

// End 打 run.finish，只认第一次。fields 是业务字段：积分、error_code、error_message、error_type、error、last_call_id 等。
// 级别：成功和暂停为 Info；error_type=internal 为 Error；其他失败、取消、超时、未知为 Warn。
func (r *Run) End(status string, fields ...zap.Field) {
	if r == nil || !r.ended.CompareAndSwap(false, true) {
		return
	}
	r.finish("运行结束", status, fields)
}

// Fallback 是兜底收尾：业务没调 End 时由队列中间件调用，带 finish_fallback=true。已经收尾过则是空操作。
func (r *Run) Fallback(status string, fields ...zap.Field) {
	if r == nil || !r.ended.CompareAndSwap(false, true) {
		return
	}
	r.finish("运行结束（兜底）", status, append(fields, zap.Bool("finish_fallback", true)))
}

// finish 拼出收尾行：状态、耗时、调用计数、用量，再加业务字段。
func (r *Run) finish(msg, status string, fields []zap.Field) {
	out := []zap.Field{Event(EventRunFinish), zap.String("status", status)}
	r.mu.Lock()
	startAt, lastCall := r.startAt, r.lastCall
	r.mu.Unlock()
	if !startAt.IsZero() {
		out = append(out, zap.Int64("duration_ms", time.Since(startAt).Milliseconds()))
	}
	if n := r.llmCalls.Load(); n > 0 {
		out = append(out, zap.Int64("llm_calls", n), zap.Int64("input_tokens", r.inputTokens.Load()), zap.Int64("output_tokens", r.outputTokens.Load()))
	}
	if n := r.providerCalls.Load(); n > 0 {
		out = append(out, zap.Int64("provider_calls", n))
	}
	if n := r.extCalls.Load(); n > 0 {
		out = append(out, zap.Int64("ext_calls", n))
	}
	if lastCall != "" && status != StatusSucceeded {
		out = append(out, zap.String("last_call_id", lastCall))
	}
	out = append(out, fields...)
	log := Ctx(r.ctx).WithOptions(zap.AddCallerSkip(2))
	if ce := log.Check(finishLevel(status, fields), msg); ce != nil {
		ce.Write(out...)
	}
}

// finishLevel 按终态和错误类型选收尾行的级别。
func finishLevel(status string, fields []zap.Field) zapcore.Level {
	switch status {
	case StatusSucceeded, StatusPaused, StatusSkipped:
		return zapcore.InfoLevel
	}
	for _, f := range fields {
		if (f.Key == "error_type" && f.String == ErrorTypeInternal) || (f.Key == FieldNeedsAttention && f.Integer == 1) {
			return zapcore.ErrorLevel
		}
	}
	return zapcore.WarnLevel
}
