// Package worker 是生成任务“某一步”的执行逻辑：提交、轮询、转存、超时与失败处理。
// 它驱动 pending → queued → running → finalizing → succeeded 的状态流转，但不负责“什么时候执行、谁来执行、并发多少”：
// 这些由 provider/queue（基于 River 的任务队列）负责，它取出任务后调用 Handle。
//
// 本包不依赖数据库：所有状态迁移都通过 Store（provider.TaskStore，由 service.GenerationTaskService 实现），
// 事务与积分逻辑只在 service + repository 里；也不认识任何协议：上游调用全部经 provider.Executor（插件宿主）。
// 任务状态全部在 DB 里，worker 自己没有需要独占的进程内状态（内存里只有转存进度与 runner 连续崩溃计数，
// 都只是优化，丢了最多让任务多转存一次或多重试一次），所以任务换实例执行也不影响正确性。
//
// “下一次什么时候处理”写在任务行的 next_poll_at 里（store 的 MarkPolled / Retry 等写入），队列在 Handle 返回后读它来安排下一次。
//
// 文件分工：worker.go 入口与公共辅助；worker_submit.go 提交；worker_poll.go 轮询；worker_finalize.go 转存；
// worker_errors.go 失败 / 超时 / 重试；worker_trace.go 试跑追踪；policy.go 节奏与文案。
package worker

import (
	"context"
	"encoding/json"
	"math/rand/v2"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/provider"
)

// Store 是 worker 需要的任务存储与状态迁移能力，契约定义在 provider 包，让 service 与 worker 互不依赖。
type Store = provider.TaskStore

// 执行参数的默认值。
const (
	defaultMaxSubmitRetries    = 5
	defaultTransferMaxAttempts = 10 // 退避到顶后总窗口约 40 分钟，远小于上游 URL 常见的 24 小时有效期
	defaultTransferGrace       = 2 * time.Hour
	defaultTransferTimeout     = 30 * time.Minute
	defaultOperationTimeout    = 2 * time.Minute
)

// Options 是执行参数，零值使用默认值。
type Options struct {
	MaxSubmitRetries    int           // 提交遇到可重试错误时最多重试几次，默认 5
	TransferMaxAttempts int           // 转存最多尝试几次（含第一次），默认 10
	TransferGrace       time.Duration // finalizing 阶段允许超出 deadline_at 的最长时间，默认 2 小时
	TransferTimeout     time.Duration // 单次转存的整体超时，默认 30 分钟
	MaxDownloadBytes    int64         // 单个产物转存的大小上限，0 表示用存储层默认值
	OperationTimeout    time.Duration // 单次 Submit / Query / Cancel 的超时保护，默认 2 分钟

	Logger *zap.Logger      // 日志输出，默认 logger.L()；测试用来断言告警
	Now    func() time.Time // 时间来源，测试用
	Rand   func() float64   // [0,1) 随机数，测试用（抖动）
}

func (o *Options) applyDefaults() {
	o.MaxSubmitRetries = orDefault(o.MaxSubmitRetries, defaultMaxSubmitRetries)
	o.TransferMaxAttempts = orDefault(o.TransferMaxAttempts, defaultTransferMaxAttempts)
	o.TransferGrace = orDefault(o.TransferGrace, defaultTransferGrace)
	o.TransferTimeout = orDefault(o.TransferTimeout, defaultTransferTimeout)
	o.OperationTimeout = orDefault(o.OperationTimeout, defaultOperationTimeout)
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Rand == nil {
		o.Rand = rand.Float64
	}
}

// orDefault 在 v 不是正数时返回默认值：执行参数的零值和负数都没有意义。
func orDefault[T int | time.Duration](v, def T) T {
	if v <= 0 {
		return def
	}
	return v
}

// Worker 执行任务的某一步。可以被多个 goroutine 并发调用（每个任务一个调用）。
type Worker struct {
	store Store
	exec  provider.Executor
	saver provider.AssetSaver
	opts  Options

	// memMu 保护下面两张进程内缓存。它们都只是优化：进程重启或换实例后丢失，只会多转存一次 / 多重试一次，不影响正确性。
	memMu sync.Mutex
	// transferred 记录 finalizing 任务已转存成功的产物（task_id → 按顺序已完成的前 N 个），
	// 重试时跳过它们，避免重复下载、重复写存储、留下孤儿素材。
	transferred map[uint64][]model.TaskOutput
	// crashes 记录任务连续遇到 runner 崩溃的次数（调用成功即清零），用来实现“崩溃只重试一次”。
	// 不能复用 poll_attempts：轮询阶段它是查询次数，不是重试次数。
	crashes map[uint64]int

	// lastRunnerWarn 是上次记 runner 不可用 Warn 的时间（UnixNano），用于全局限频，避免每个任务每轮都刷一条
	lastRunnerWarn atomic.Int64
}

// New 创建 worker。
func New(store Store, exec provider.Executor, saver provider.AssetSaver, opts Options) *Worker {
	opts.applyDefaults()
	return &Worker{
		store: store, exec: exec, saver: saver, opts: opts,
		transferred: map[uint64][]model.TaskOutput{},
		crashes:     map[uint64]int{},
	}
}

// log 返回日志输出。没有注入时每次取 logger.L()，这样 logger.Init 晚于 New 也能生效。
func (w *Worker) log() *zap.Logger {
	if w.opts.Logger != nil {
		return w.opts.Logger
	}
	return logger.L()
}

// taskLog 返回带任务标识的日志：task_id（界面上展示的十六进制编号）、渠道 key（t.Provider 存的是渠道 key）、模型。
func (w *Worker) taskLog(t *model.GenerationTask) *zap.Logger {
	return w.log().With(logger.TaskID(t.ID), zap.String("channel", t.Provider), zap.String("model", t.ModelKey))
}

// Handle 处理一个任务的“这一步”：先做超时判断，再按状态分发（提交 / 轮询 / 转存）。任何 panic 只影响这一个任务，不会向上传播。
// 处理结果都写在任务行里：状态是否变了、下一次什么时候再处理（next_poll_at），由调用方（队列）在返回后读取。
// 任务在途中被取消 / 超时、迁移没有命中（applied=false）都不算错误，下一次读到终态就结束。
func (w *Worker) Handle(ctx context.Context, t model.GenerationTask) {
	// 试跑任务记录本次执行的钩子与 HTTP 追踪。保存放在 defer 里并先于 recover 注册（所以最后执行）：
	// 即使本次处理 panic，已经发生的步骤也会被保存，方便运营排查
	if t.IsTest {
		var tr *provider.Trace
		ctx, tr = provider.WithTrace(ctx)
		defer w.saveTrace(ctx, &t, tr)
	}
	log := w.taskLog(&t).With(zap.String("status", t.Status))
	defer func() {
		if r := recover(); r != nil {
			log.Error("处理任务时 panic，稍后会重新处理", zap.Any("panic", r), zap.ByteString("stack", debug.Stack()))
		}
	}()

	// 1. 解析创建任务时冻结的配置快照：worker 只读快照，不读当前配置。快照损坏无法恢复，直接失败退款
	snap, err := decodeSnapshot(t.ConfigSnapshot)
	if err != nil {
		log.Error("任务配置快照损坏，直接失败", zap.Error(err))
		w.fail(ctx, &t, nil, failure{class: provider.ClassTerminal, raw: "invalid snapshot"})
		return
	}

	// 2. 超时判断。finalizing（上游已出结果）不受普通 deadline 约束，否则临近超时才出片的视频会白白浪费；
	//    但要有上限：超过 deadline + TransferGrace 仍没转存成功就放弃
	now := w.opts.Now()
	if t.Status == model.TaskFinalizing {
		if now.After(t.DeadlineAt.Add(w.opts.TransferGrace)) {
			log.Warn("转存超过总窗口，放弃", zap.Time("deadline_at", t.DeadlineAt))
			w.failTransfer(ctx, &t)
			return
		}
	} else if now.After(t.DeadlineAt) {
		w.expire(ctx, &t, snap)
		return
	}

	// 3. 按状态处理
	switch t.Status {
	case model.TaskPending:
		w.submit(ctx, &t, snap)
	case model.TaskQueued, model.TaskRunning:
		w.poll(ctx, &t, snap)
	case model.TaskFinalizing:
		w.finalize(ctx, &t, snap)
	default:
		log.Warn("收到了非预期状态的任务，忽略")
	}
}

// Expire 让一个已经超过截止时间、还没拿到名额的 pending 任务直接走超时流程（置为 expired 并退款）。
// 队列在“排队等名额”时用它：这样的任务不会进入 Handle 的提交分支，也就不会去请求上游。
func (w *Worker) Expire(ctx context.Context, t model.GenerationTask) {
	log := w.taskLog(&t)
	defer func() {
		if r := recover(); r != nil {
			log.Error("处理超时任务时 panic", zap.Any("panic", r), zap.ByteString("stack", debug.Stack()))
		}
	}()
	snap, _ := decodeSnapshot(t.ConfigSnapshot) // 快照损坏时 snap 为空：expire 只在有上游任务 id 时才用它，排队中的任务没有
	w.expire(ctx, &t, snap)
}

// decodeSnapshot 解码任务创建时冻结的配置快照。
func decodeSnapshot(raw []byte) (*provider.Snapshot, error) {
	var snap provider.Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}

// providerState 解码任务的 provider_state。损坏时记警告并当空处理：丢掉 prepared 最多重新准备一次，
// 丢掉插件 state 由插件自己兜底，都比让任务直接失败好。
func (w *Worker) providerState(t *model.GenerationTask) provider.ProviderState {
	st, ok := provider.DecodeProviderState(t.ProviderState)
	if !ok {
		w.taskLog(t).Warn("任务的 provider_state 损坏，按空状态继续")
	}
	return st
}

// taskRef 组装 Executor 认识的任务信息：带上插件上次返回的 state 与准备阶段的结果。
func taskRef(t *model.GenerationTask, st provider.ProviderState) provider.TaskRef {
	return provider.TaskRef{
		ID: t.ID, UserID: t.UserID, ProviderTaskID: t.ProviderTaskID,
		State: st.Plugin, Prepared: st.Prepared,
	}
}
