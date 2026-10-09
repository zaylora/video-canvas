// Package worker 是生成任务的调度循环与执行池：按 next_poll_at 领取到期任务，
// 驱动 pending → queued → running → finalizing → succeeded 的状态流转（提交、轮询、转存、超时）。
//
// 本包不依赖数据库：所有状态迁移都通过 Store（provider.TaskStore，由 service.GenerationTaskService 实现），
// 事务与积分逻辑只在 service + repository 里；也不认识任何协议：上游调用全部经 provider.Executor（插件宿主）。
// 任务状态全部在 DB 里，worker 自己没有需要独占的进程内状态（内存里只有转存进度与 runner 连续崩溃计数，
// 都只是优化，丢了最多让任务多转存一次或多重试一次），所以进程重启后，租约过期的任务会被自然重新领取。
//
// 文件分工：worker.go 调度循环；worker_submit.go 提交；worker_poll.go 轮询；worker_finalize.go 转存；
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

// 调度参数的默认值。
const (
	defaultConcurrency         = 8
	defaultTick                = time.Second
	defaultBatchSize           = 50
	defaultLease               = 60 * time.Second
	defaultMaxSubmitRetries    = 5
	defaultTransferMaxAttempts = 10 // 退避到顶后总窗口约 40 分钟，远小于上游 URL 常见的 24 小时有效期
	defaultTransferGrace       = 2 * time.Hour
	defaultTransferTimeout     = 30 * time.Minute
	defaultOperationTimeout    = 2 * time.Minute
	defaultShutdownTimeout     = 30 * time.Second
	// shutdownDrainTimeout 是 ShutdownTimeout 到期、取消在途任务之后，再等它们收尾的时间
	shutdownDrainTimeout = 5 * time.Second
	// heartbeatDivisor 决定续租频率：每 Lease/3 续一次，两次续租失败之间仍在租约内
	heartbeatDivisor = 3
)

// Options 是调度参数，零值使用默认值。
type Options struct {
	Concurrency int           // 执行池大小，默认 8
	Tick        time.Duration // 调度循环间隔，默认 1s
	BatchSize   int           // 每次最多领取的任务数，默认 50（同时受空闲执行槽数限制）
	Lease       time.Duration // 租约时长，默认 60s

	MaxSubmitRetries    int           // 提交遇到可重试错误时最多重试几次，默认 5
	TransferMaxAttempts int           // 转存最多尝试几次（含第一次），默认 10
	TransferGrace       time.Duration // finalizing 阶段允许超出 deadline_at 的最长时间，默认 2 小时
	TransferTimeout     time.Duration // 单次转存的整体超时，默认 30 分钟
	MaxDownloadBytes    int64         // 单个产物转存的大小上限，0 表示用存储层默认值
	OperationTimeout    time.Duration // 单次 Submit / Query / Cancel 的超时保护，默认 2 分钟
	ShutdownTimeout     time.Duration // 优雅退出时等待在途任务的最长时间，默认 30 秒

	Logger *zap.Logger      // 日志输出，默认 logger.L()；测试用来断言告警
	Now    func() time.Time // 时间来源，测试用
	Rand   func() float64   // [0,1) 随机数，测试用（抖动）
}

func (o *Options) applyDefaults() {
	o.Concurrency = orDefault(o.Concurrency, defaultConcurrency)
	o.Tick = orDefault(o.Tick, defaultTick)
	o.BatchSize = orDefault(o.BatchSize, defaultBatchSize)
	o.Lease = orDefault(o.Lease, defaultLease)
	o.MaxSubmitRetries = orDefault(o.MaxSubmitRetries, defaultMaxSubmitRetries)
	o.TransferMaxAttempts = orDefault(o.TransferMaxAttempts, defaultTransferMaxAttempts)
	o.TransferGrace = orDefault(o.TransferGrace, defaultTransferGrace)
	o.TransferTimeout = orDefault(o.TransferTimeout, defaultTransferTimeout)
	o.OperationTimeout = orDefault(o.OperationTimeout, defaultOperationTimeout)
	o.ShutdownTimeout = orDefault(o.ShutdownTimeout, defaultShutdownTimeout)
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Rand == nil {
		o.Rand = rand.Float64
	}
}

// orDefault 在 v 不是正数时返回默认值：调度参数的零值和负数都没有意义。
func orDefault[T int | time.Duration](v, def T) T {
	if v <= 0 {
		return def
	}
	return v
}

// Worker 是调度循环 + 固定上限的执行池。
type Worker struct {
	store Store
	exec  provider.Executor
	saver provider.AssetSaver
	kick  <-chan struct{}
	opts  Options

	inflight atomic.Int32
	wg       sync.WaitGroup

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

// New 创建 worker。kick 是 service 提供的唤醒信号（可为 nil）。
func New(store Store, exec provider.Executor, saver provider.AssetSaver, kick <-chan struct{}, opts Options) *Worker {
	opts.applyDefaults()
	return &Worker{
		store: store, exec: exec, saver: saver, kick: kick, opts: opts,
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

// Run 运行调度循环直到 ctx 取消，然后等待在途任务处理完（最多 ShutdownTimeout）再返回。
// 启动时会立即调度一次，这样重启后租约已过期的任务马上被接手。
func (w *Worker) Run(ctx context.Context) error {
	// 在途任务用独立的 workCtx：外部 ctx 取消只停止领取新任务，让手上的活做完；
	// 超过 ShutdownTimeout 还没做完才取消 workCtx，剩下的靠租约过期后被重新领取。
	workCtx, cancelWork := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelWork()

	w.log().Info("任务 worker 已启动",
		zap.Int("concurrency", w.opts.Concurrency), zap.Duration("tick", w.opts.Tick), zap.Duration("lease", w.opts.Lease))

	ticker := time.NewTicker(w.opts.Tick)
	defer ticker.Stop()

	w.dispatch(workCtx)
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case <-ticker.C:
		case <-w.kick:
		}
		if ctx.Err() != nil {
			break loop
		}
		w.dispatch(workCtx)
	}

	w.drain(cancelWork)
	w.log().Info("任务 worker 已停止")
	return nil
}

// drain 是优雅退出：等在途任务，超时后强制取消并再等一小会儿。等待用的 goroutine 在 wg 归零后退出。
func (w *Worker) drain(cancelWork context.CancelFunc) {
	done := make(chan struct{})
	go func() { w.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(w.opts.ShutdownTimeout):
		w.log().Warn("任务 worker 退出超时，取消在途任务（租约过期后会被重新领取）", zap.Int32("inflight", w.inflight.Load()))
		cancelWork()
		select {
		case <-done:
		case <-time.After(shutdownDrainTimeout):
		}
	}
}

// RunOnce 领取一批到期任务并同步处理完，返回领取的数量。主要给测试用。
func (w *Worker) RunOnce(ctx context.Context) (int, error) {
	n, err := w.claimAndRun(ctx)
	w.wg.Wait()
	return n, err
}

// dispatch 领取一批任务并交给执行池，不等待处理完成。领取失败在这里（后台任务的边界）记日志。
func (w *Worker) dispatch(ctx context.Context) {
	if _, err := w.claimAndRun(ctx); err != nil && ctx.Err() == nil {
		w.log().Error("领取到期任务失败", zap.Error(err))
	}
}

// claimAndRun 只领取“空闲执行槽”那么多任务：领了却没有槽位处理，租约会白白消耗甚至过期后被重复领取。
func (w *Worker) claimAndRun(ctx context.Context) (int, error) {
	free := w.opts.Concurrency - int(w.inflight.Load())
	if free <= 0 {
		return 0, nil
	}
	limit := min(free, w.opts.BatchSize)
	tasks, err := w.store.ClaimDue(ctx, limit, w.opts.Lease)
	if err != nil {
		return 0, err
	}
	for i := range tasks {
		t := tasks[i]
		w.inflight.Add(1)
		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			defer w.inflight.Add(-1)
			w.handle(ctx, t)
		}()
	}
	return len(tasks), nil
}

// handle 处理一个已领取的任务：先做超时判断，再按状态分发。任何 panic 只影响这一个任务。
func (w *Worker) handle(ctx context.Context, t model.GenerationTask) {
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
			log.Error("处理任务时 panic，租约过期后会重新处理", zap.Any("panic", r), zap.ByteString("stack", debug.Stack()))
		}
	}()

	// 1. 解析创建任务时冻结的配置快照：worker 只读快照，不读当前配置。快照损坏无法恢复，直接失败退款
	snap, err := decodeSnapshot(t.ConfigSnapshot)
	if err != nil {
		log.Error("任务配置快照损坏，直接失败", zap.Error(err))
		w.fail(ctx, &t, nil, failure{class: provider.ClassTerminal, raw: "invalid snapshot"})
		return
	}

	// 2. 续租心跳：转存大文件可能超过一个租约周期，续租防止被别的实例重复领取
	stop := w.startHeartbeat(ctx, t.ID)
	defer stop()

	// 3. 超时判断。finalizing（上游已出结果）不受普通 deadline 约束，否则临近超时才出片的视频会白白浪费；
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

	// 4. 按状态处理
	switch t.Status {
	case model.TaskPending:
		w.submit(ctx, &t, snap)
	case model.TaskQueued, model.TaskRunning:
		w.poll(ctx, &t, snap)
	case model.TaskFinalizing:
		w.finalize(ctx, &t, snap)
	default:
		log.Warn("领取到了非预期状态的任务，忽略")
	}
}

// startHeartbeat 每 Lease/3 续租一次，返回停止函数。任务终态、续租失败、ctx 取消或调用停止函数时 goroutine 退出。
func (w *Worker) startHeartbeat(ctx context.Context, id uint64) (stop func()) {
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(w.opts.Lease / heartbeatDivisor)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				ok, err := w.store.ExtendLease(ctx, id, w.opts.Lease)
				if err != nil {
					w.log().Warn("任务续租失败", logger.TaskID(id), zap.Error(err))
				}
				if err != nil || !ok {
					return
				}
			}
		}
	}()
	return func() { close(done) }
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
