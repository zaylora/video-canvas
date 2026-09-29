// Package worker 是生成任务的调度循环与执行池：按 next_poll_at 领取到期任务，
// 驱动 pending → queued → running → finalizing → succeeded 的状态流转（提交、轮询、转存、超时）。
//
// 本包不依赖数据库：所有状态迁移都通过 Store 接口（由 service.GenerationTaskService 实现），
// 事务与积分逻辑只在 service + repository 里。任务状态全部在 DB 里，worker 自己没有需要独占的进程内状态
// （唯一的内存缓存是转存进度，它只是重试时避免重复转存的优化，丢了只会多转存一次），
// 所以进程重启后，租约过期的任务会被自然重新领取。
package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/dsl"
)

// Store 是 worker 需要的任务存储与状态迁移能力，由 service.GenerationTaskService 实现。
// 除 ClaimDue / ExtendLease 外，迁移方法的第一个返回值 applied=false 表示任务已被其它流程
// （用户取消、超时、别的实例）迁移走了，worker 应放弃后续处理，这不是错误。
type Store interface {
	// ClaimDue 领取最多 limit 个到期且租约已过期的非终态任务，并写入 lease 时长的租约。
	ClaimDue(ctx context.Context, limit int, lease time.Duration) ([]model.GenerationTask, error)
	// ExtendLease 续租，任务已终态返回 false。
	ExtendLease(ctx context.Context, id uint64, lease time.Duration) (bool, error)
	// MarkSubmitted pending → queued，记录平台任务 id，安排第一次查询。
	MarkSubmitted(ctx context.Context, t *model.GenerationTask, providerTaskID string, nextPollAt time.Time) (bool, error)
	// MarkPolled 记录一次查询结果（queued / running），安排下一次查询。
	MarkPolled(ctx context.Context, t *model.GenerationTask, status string, progress *int, attempts int, nextPollAt time.Time) (bool, error)
	// MarkFinalizing queued / running → finalizing，立即转存。
	MarkFinalizing(ctx context.Context, t *model.GenerationTask) (bool, error)
	// Retry 可重试的失败：状态不变，记录重试次数与下次处理时间。
	Retry(ctx context.Context, t *model.GenerationTask, attempts int, nextPollAt time.Time) (bool, error)
	// Complete finalizing → succeeded，写产物并结算积分。
	Complete(ctx context.Context, t *model.GenerationTask, outputs []model.TaskOutput) (bool, error)
	// Fail 非终态 → failed 并退回积分。errorCode 是统一错误码，message 是给用户看的文案。
	Fail(ctx context.Context, t *model.GenerationTask, errorCode, message string) (bool, error)
	// Expire 非终态 → expired 并退回积分。
	Expire(ctx context.Context, t *model.GenerationTask) (bool, error)
}

// Options 是调度参数，零值使用默认值。
type Options struct {
	Concurrency int           // 执行池大小，默认 8
	Tick        time.Duration // 调度循环间隔，默认 1s
	BatchSize   int           // 每次最多领取的任务数，默认 50（同时受空闲执行槽数限制）
	Lease       time.Duration // 租约时长，默认 60s

	MaxSubmitRetries    int           // 提交遇到可重试错误时最多重试几次，默认 5
	TransferMaxAttempts int           // 转存最多尝试几次（含第一次），默认 10；退避到顶后总窗口约 40 分钟，远小于平台 URL 的 24 小时有效期
	TransferGrace       time.Duration // finalizing 阶段允许超出 deadline_at 的最长时间，默认 2 小时
	TransferTimeout     time.Duration // 单次转存的整体超时，默认 30 分钟
	MaxDownloadBytes    int64         // 单个产物转存的大小上限，0 表示用存储层默认值
	OperationTimeout    time.Duration // 单次 Submit / Query / Cancel 的超时保护，默认 2 分钟
	ShutdownTimeout     time.Duration // 优雅退出时等待在途任务的最长时间，默认 30 秒

	// WebhookURL 返回注册给平台的回调地址，为空串表示纯轮询。可选。
	WebhookURL func(providerKey string) string

	Now  func() time.Time // 时间来源，测试用
	Rand func() float64   // [0,1) 随机数，测试用（抖动）
}

func (o *Options) applyDefaults() {
	if o.Concurrency <= 0 {
		o.Concurrency = 8
	}
	if o.Tick <= 0 {
		o.Tick = time.Second
	}
	if o.BatchSize <= 0 {
		o.BatchSize = 50
	}
	if o.Lease <= 0 {
		o.Lease = 60 * time.Second
	}
	if o.MaxSubmitRetries <= 0 {
		o.MaxSubmitRetries = 5
	}
	if o.TransferMaxAttempts <= 0 {
		o.TransferMaxAttempts = 10
	}
	if o.TransferGrace <= 0 {
		o.TransferGrace = 2 * time.Hour
	}
	if o.TransferTimeout <= 0 {
		o.TransferTimeout = 30 * time.Minute
	}
	if o.OperationTimeout <= 0 {
		o.OperationTimeout = 2 * time.Minute
	}
	if o.ShutdownTimeout <= 0 {
		o.ShutdownTimeout = 30 * time.Second
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Rand == nil {
		o.Rand = rand.Float64
	}
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

	// transferred 记录 finalizing 任务已转存成功的产物（task_id → 按顺序已完成的前 N 个），
	// 重试时跳过它们，避免重复下载、重复写存储、留下孤儿素材。
	// 它只是优化：进程重启后丢失只会让这个任务多转存一次，不影响正确性，所以不算“进程内独占状态”。
	transferMu  sync.Mutex
	transferred map[uint64][]model.TaskOutput
}

// New 创建 worker。kick 是 service 提供的唤醒信号（可为 nil）。
func New(store Store, exec provider.Executor, saver provider.AssetSaver, kick <-chan struct{}, opts Options) *Worker {
	opts.applyDefaults()
	return &Worker{
		store: store, exec: exec, saver: saver, kick: kick, opts: opts,
		transferred: map[uint64][]model.TaskOutput{},
	}
}

// Run 运行调度循环直到 ctx 取消，然后等待在途任务处理完（最多 ShutdownTimeout）再返回。
// 启动时会立即调度一次，这样重启后租约已过期的任务马上被接手。
func (w *Worker) Run(ctx context.Context) error {
	// 在途任务用独立的 workCtx：外部 ctx 取消只停止领取新任务，让手上的活做完；
	// 超过 ShutdownTimeout 还没做完才取消 workCtx，剩下的靠租约过期后被重新领取。
	workCtx, cancelWork := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelWork()

	logger.Info("任务 worker 已启动",
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

	// 优雅退出：等在途任务，超时后强制取消并再等一小会儿
	done := make(chan struct{})
	go func() { w.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(w.opts.ShutdownTimeout):
		logger.Warn("任务 worker 退出超时，取消在途任务（租约过期后会被重新领取）", zap.Int32("inflight", w.inflight.Load()))
		cancelWork()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	}
	logger.Info("任务 worker 已停止")
	return nil
}

// RunOnce 领取一批到期任务并同步处理完，返回领取的数量。主要给测试用。
func (w *Worker) RunOnce(ctx context.Context) (int, error) {
	n, err := w.claimAndRun(ctx)
	w.wg.Wait()
	return n, err
}

// dispatch 领取一批任务并交给执行池，不等待处理完成。
func (w *Worker) dispatch(ctx context.Context) {
	if _, err := w.claimAndRun(ctx); err != nil && ctx.Err() == nil {
		logger.Error("领取到期任务失败", zap.Error(err))
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
	log := logger.L().With(zap.Uint64("task_id", t.ID), zap.String("status", t.Status), zap.String("provider", t.Provider))
	defer func() {
		if r := recover(); r != nil {
			log.Error("处理任务时 panic，租约过期后会重新处理", zap.Any("panic", r), zap.ByteString("stack", debug.Stack()))
		}
	}()

	// 1. 解析创建任务时冻结的配置快照：worker 只读快照，不读当前配置。快照损坏无法恢复，直接失败退款
	snap, err := decodeSnapshot(t.ConfigSnapshot)
	if err != nil {
		log.Error("任务配置快照损坏，直接失败", zap.Error(err))
		w.fail(ctx, &t, provider.ClassTerminal, "", "invalid snapshot")
		return
	}

	// 2. 续租心跳：转存大文件可能超过一个租约周期，续租防止被别的实例重复领取
	stop := w.startHeartbeat(ctx, t.ID)
	defer stop()

	// 3. 超时判断。finalizing（平台已出片）不受普通 deadline 约束，否则临近超时才出片的视频会白白浪费；
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

// startHeartbeat 每 Lease/3 续租一次，返回停止函数。任务终态或续租失败时不再续。
func (w *Worker) startHeartbeat(ctx context.Context, id uint64) (stop func()) {
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(w.opts.Lease / 3)
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
					logger.Warn("任务续租失败", zap.Uint64("task_id", id), zap.Error(err))
				}
				if err != nil || !ok {
					return
				}
			}
		}
	}()
	return func() { close(done) }
}

// ---------------------------------------------------------------------------
// pending：提交给平台
// ---------------------------------------------------------------------------

func (w *Worker) submit(ctx context.Context, t *model.GenerationTask, snap *dsl.Snapshot) {
	log := logger.L().With(zap.Uint64("task_id", t.ID), zap.String("provider", t.Provider), zap.String("model", t.ModelKey))

	// 1. 解出已校验的规范化输入。媒体字段是 asset id，engine 会在提交前再做一次防御性校验和规范化
	var input map[string]any
	if err := json.Unmarshal(t.InputJSON, &input); err != nil {
		log.Error("任务输入损坏，直接失败", zap.Error(err))
		w.fail(ctx, t, provider.ClassTerminal, "invalid_param", "invalid input")
		return
	}

	// 2. 提交。回调地址为空表示纯轮询
	cctx, cancel := context.WithTimeout(ctx, w.opts.OperationTimeout)
	defer cancel()
	in := provider.SubmitInput{
		Task:       provider.TaskRef{ID: t.ID, UserID: t.UserID},
		Input:      input,
		WebhookURL: w.webhookURL(t, snap),
	}
	providerTaskID, err := w.exec.Submit(cctx, snap, in)
	if err != nil {
		w.onSubmitError(ctx, t, err)
		return
	}
	if providerTaskID == "" {
		log.Error("平台提交成功但没有返回任务 id")
		w.fail(ctx, t, provider.ClassTerminal, "", "empty provider task id")
		return
	}

	// 3. 平台已受理：记录平台任务 id，安排第一次查询。
	//    这一步失败会导致任务保持 pending、下次又被重新提交给平台（重复计费），所以先在进程内重试几次，
	//    最终仍失败时把平台任务 id 写进错误日志，方便人工核对
	next := w.opts.Now().Add(firstDelay(snap.Provider.Poll))
	var applied bool
	err = w.retryDB(ctx, func() (e error) {
		applied, e = w.store.MarkSubmitted(ctx, t, providerTaskID, next)
		return e
	})
	if err != nil {
		log.Error("平台已受理但记录平台任务 id 失败，任务可能被重复提交，请人工核对",
			zap.String("provider_task_id", providerTaskID), zap.Error(err))
		return
	}
	if !applied {
		// 提交期间任务被取消 / 超时了：平台上已经产生了任务，尽力取消它，避免白白消耗平台额度
		log.Warn("提交期间任务已被取消或超时，尽力取消平台任务", zap.String("provider_task_id", providerTaskID))
		w.cancelProvider(ctx, t, snap, providerTaskID)
		return
	}
	log.Info("任务已提交给平台", zap.String("provider_task_id", providerTaskID))
}

// onSubmitError 按错误分类处理提交失败：可重试的退避重试（有上限），其余直接失败退款。
func (w *Worker) onSubmitError(ctx context.Context, t *model.GenerationTask, err error) {
	class := provider.ClassOf(err)
	if class != provider.ClassRetryable {
		w.fail(ctx, t, class, errCode(err), err.Error())
		return
	}
	attempts := t.PollAttempts + 1
	if attempts > w.opts.MaxSubmitRetries {
		logger.Warn("提交重试次数耗尽", zap.Uint64("task_id", t.ID), zap.Int("attempts", attempts), zap.Error(err))
		w.fail(ctx, t, provider.ClassRetryable, errCode(err), err.Error())
		return
	}
	delay := w.jitter(submitBackoff(attempts), defaultJitter)
	logger.Warn("提交失败，稍后重试", zap.Uint64("task_id", t.ID), zap.Int("attempts", attempts), zap.Duration("delay", delay), zap.Error(err))
	w.retry(ctx, t, attempts, w.opts.Now().Add(delay))
}

// ---------------------------------------------------------------------------
// queued / running：轮询平台
// ---------------------------------------------------------------------------

func (w *Worker) poll(ctx context.Context, t *model.GenerationTask, snap *dsl.Snapshot) {
	log := logger.L().With(zap.Uint64("task_id", t.ID), zap.String("provider", t.Provider))
	if t.ProviderTaskID == "" {
		log.Error("queued / running 任务缺少平台任务 id，直接失败")
		w.fail(ctx, t, provider.ClassTerminal, "", "missing provider task id")
		return
	}

	// 1. 查询平台
	cctx, cancel := context.WithTimeout(ctx, w.opts.OperationTimeout)
	defer cancel()
	res, err := w.exec.Query(cctx, snap, provider.TaskRef{ID: t.ID, UserID: t.UserID, ProviderTaskID: t.ProviderTaskID})
	attempts := t.PollAttempts + 1

	// 2. 查询自身出错：可重试的按轮询节奏继续（间隔指数增长到上限，本身就是退避）；其余直接失败
	if err != nil {
		if provider.ClassOf(err) == provider.ClassRetryable {
			delay := pollDelay(snap.Provider.Poll, attempts, w.opts.Rand)
			log.Warn("查询平台失败，稍后重试", zap.Int("attempts", attempts), zap.Duration("delay", delay), zap.Error(err))
			w.retry(ctx, t, attempts, w.opts.Now().Add(delay))
			return
		}
		w.fail(ctx, t, provider.ClassOf(err), errCode(err), err.Error())
		return
	}

	// 3. 按平台返回的统一状态处理
	switch res.Status {
	case provider.StatusSucceeded:
		// 平台出片：进入 finalizing（立即转存）。平台侧成本只写日志用于对账
		log.Info("平台生成成功，开始转存", zap.Int("outputs", len(res.Outputs)), zap.Any("provider_cost", res.ProviderCost))
		if _, err := w.store.MarkFinalizing(ctx, t); err != nil {
			log.Error("迁移到 finalizing 失败，租约过期后重试", zap.Error(err))
		}
	case provider.StatusFailed:
		class := res.ErrorClass
		if class == "" {
			class = provider.ClassTerminal
		}
		w.fail(ctx, t, class, res.ErrorCode, res.ErrorMessage)
	case provider.StatusQueued, provider.StatusRunning:
		delay := pollDelay(snap.Provider.Poll, attempts, w.opts.Rand)
		if _, err := w.store.MarkPolled(ctx, t, res.Status, res.Progress, attempts, w.opts.Now().Add(delay)); err != nil {
			log.Error("更新任务进度失败，租约过期后重试", zap.Error(err))
		}
	default:
		// 引擎按 status_map 映射后不应出现其它值；当作 running 继续轮询，避免任务卡死
		log.Warn("平台返回了未知的统一状态，按 running 处理", zap.String("platform_status", res.Status))
		delay := pollDelay(snap.Provider.Poll, attempts, w.opts.Rand)
		if _, err := w.store.MarkPolled(ctx, t, model.TaskRunning, res.Progress, attempts, w.opts.Now().Add(delay)); err != nil {
			log.Error("更新任务进度失败，租约过期后重试", zap.Error(err))
		}
	}
}

// ---------------------------------------------------------------------------
// finalizing：转存产物
// ---------------------------------------------------------------------------

func (w *Worker) finalize(ctx context.Context, t *model.GenerationTask, snap *dsl.Snapshot) {
	log := logger.L().With(zap.Uint64("task_id", t.ID), zap.String("provider", t.Provider))

	// 1. 重新查询一次拿到产物地址：不把平台 URL 存进 DB，重启后也能继续，且总是用最新的 URL
	qctx, qcancel := context.WithTimeout(ctx, w.opts.OperationTimeout)
	res, err := w.exec.Query(qctx, snap, provider.TaskRef{ID: t.ID, UserID: t.UserID, ProviderTaskID: t.ProviderTaskID})
	qcancel()
	if err != nil {
		if provider.ClassOf(err) == provider.ClassRetryable {
			w.retryTransfer(ctx, t, fmt.Errorf("查询产物地址失败：%w", err))
			return
		}
		w.fail(ctx, t, provider.ClassOf(err), errCode(err), err.Error())
		return
	}
	switch {
	case res.Status == provider.StatusFailed:
		class := res.ErrorClass
		if class == "" {
			class = provider.ClassTerminal
		}
		w.fail(ctx, t, class, res.ErrorCode, res.ErrorMessage)
		return
	case res.Status != provider.StatusSucceeded:
		w.retryTransfer(ctx, t, fmt.Errorf("平台状态回退为 %s", res.Status))
		return
	case len(res.Outputs) == 0:
		log.Error("平台返回成功但没有可转存的产物")
		w.fail(ctx, t, provider.ClassTerminal, "", "no outputs")
		return
	}

	// 2. 逐个下载并转存（已成功的跳过），失败按退避重试，耗尽后失败退款
	tctx, tcancel := context.WithTimeout(ctx, w.opts.TransferTimeout)
	defer tcancel()
	outputs, err := w.transferAll(tctx, t, snap, res.Outputs)
	if err != nil {
		w.retryTransfer(ctx, t, err)
		return
	}

	// 3. 全部转存成功：一个事务里写 output_json、置 succeeded、结算积分（在 store.Complete 内完成）
	applied, err := w.store.Complete(ctx, t, outputs)
	if err != nil {
		// 事务失败：产物已转存好并记在内存里，租约过期后重试时不会重复转存
		log.Error("完成任务的事务失败，租约过期后重试", zap.Error(err))
		return
	}
	w.forgetTransfer(t.ID)
	if !applied {
		log.Warn("转存完成时任务已被取消或超时，已转存的素材成为孤儿素材")
		return
	}
	log.Info("任务完成", zap.Int("outputs", len(outputs)))
}

// transferAll 按顺序转存全部产物。已转存成功的前 N 个从内存进度里复用，只处理剩下的；
// 任何一个失败就返回错误，已完成的进度保留给下次重试。
func (w *Worker) transferAll(ctx context.Context, t *model.GenerationTask, snap *dsl.Snapshot, outs []provider.Output) ([]model.TaskOutput, error) {
	w.transferMu.Lock()
	done := append([]model.TaskOutput(nil), w.transferred[t.ID]...)
	w.transferMu.Unlock()
	if len(done) > len(outs) {
		done = done[:len(outs)] // 平台产物变少了（异常），以本次为准
	}

	for i := len(done); i < len(outs); i++ {
		o, err := w.transferOne(ctx, t, snap, outs[i], i)
		if err != nil {
			return nil, fmt.Errorf("转存第 %d 个产物失败：%w", i+1, err)
		}
		done = append(done, o)
		w.transferMu.Lock()
		w.transferred[t.ID] = append([]model.TaskOutput(nil), done...)
		w.transferMu.Unlock()
	}
	return done, nil
}

// transferOne 下载一个产物（Download 内部校验 allowed_hosts / 内网 IP / 重定向）并写入自有存储。
func (w *Worker) transferOne(ctx context.Context, t *model.GenerationTask, snap *dsl.Snapshot, out provider.Output, idx int) (model.TaskOutput, error) {
	dl, err := w.exec.Download(ctx, snap, out.URL)
	if err != nil {
		return model.TaskOutput{}, fmt.Errorf("下载：%w", err)
	}
	defer dl.Body.Close()

	kind := snap.Model.Output.Media
	if kind == "" {
		kind = snap.Model.Kind
	}
	fileName := dl.FileName
	if fileName == "" {
		fileName = fmt.Sprintf("task-%d-%d%s", t.ID, idx+1, extOf(out.Type))
	}
	asset, url, err := w.saver.SaveGenerated(ctx, provider.SaveGeneratedInput{
		UserID:   t.UserID,
		TaskID:   t.ID,
		Kind:     kind,
		MimeType: dl.ContentType,
		FileName: fileName,
		Body:     dl.Body,
		MaxBytes: w.opts.MaxDownloadBytes,
	})
	if err != nil {
		return model.TaskOutput{}, fmt.Errorf("保存：%w", err)
	}
	return model.TaskOutput{
		AssetID:    asset.ID,
		URL:        url,
		MediaType:  kind,
		DurationMs: asset.DurationMs,
		Width:      asset.Width,
		Height:     asset.Height,
	}, nil
}

// retryTransfer 转存阶段的一次失败：次数没耗尽就退避重试（必须在平台 URL 24 小时有效期内完成，
// 默认最多 10 次、总窗口约 40 分钟），耗尽后置 failed（transfer_failed）并退款。
func (w *Worker) retryTransfer(ctx context.Context, t *model.GenerationTask, err error) {
	attempts := t.PollAttempts + 1
	if attempts >= w.opts.TransferMaxAttempts {
		logger.Error("转存重试次数耗尽，任务失败并退款", zap.Uint64("task_id", t.ID), zap.Int("attempts", attempts), zap.Error(err))
		w.failTransfer(ctx, t)
		return
	}
	delay := w.jitter(transferBackoff(attempts), defaultJitter)
	logger.Warn("转存失败，稍后重试", zap.Uint64("task_id", t.ID), zap.Int("attempts", attempts), zap.Duration("delay", delay), zap.Error(err))
	w.retry(ctx, t, attempts, w.opts.Now().Add(delay))
}

func (w *Worker) failTransfer(ctx context.Context, t *model.GenerationTask) {
	w.forgetTransfer(t.ID)
	code, msg := failureFor(failTransfer, "")
	if _, err := w.store.Fail(ctx, t, code, msg); err != nil {
		logger.Error("标记转存失败时出错，租约过期后重试", zap.Uint64("task_id", t.ID), zap.Error(err))
	}
}

func (w *Worker) forgetTransfer(id uint64) {
	w.transferMu.Lock()
	delete(w.transferred, id)
	w.transferMu.Unlock()
}

// ---------------------------------------------------------------------------
// 失败 / 超时 / 重试
// ---------------------------------------------------------------------------

// fail 按错误分类把任务置为 failed 并退款。给用户看的文案与错误码统一映射，平台原始信息只进日志。
func (w *Worker) fail(ctx context.Context, t *model.GenerationTask, class provider.ErrorClass, platformCode, rawMessage string) {
	w.forgetTransfer(t.ID)
	code, msg := failureFor(class, platformCode)

	fields := []zap.Field{
		zap.Uint64("task_id", t.ID), zap.String("provider", t.Provider), zap.String("model", t.ModelKey),
		zap.String("class", string(class)), zap.String("platform_code", platformCode), zap.String("raw_message", rawMessage),
	}
	switch class {
	case provider.ClassProviderBalance:
		// 平台账户余额不足是运维问题，用户只看到“服务繁忙”，必须告警
		logger.Error("【告警】生成平台账户余额不足，请尽快充值", fields...)
	case provider.ClassSubmitUnknown:
		// 提交读超时，平台可能已经创建了任务并计费，需要人工核对
		logger.Warn("提交结果未知（读超时），任务已失败并退积分，请人工核对平台侧是否产生了任务", fields...)
	default:
		logger.Warn("任务失败", fields...)
	}

	if _, err := w.store.Fail(ctx, t, code, msg); err != nil {
		logger.Error("标记任务失败时出错，租约过期后重试", zap.Uint64("task_id", t.ID), zap.Error(err))
	}
}

// expire 处理超过 deadline_at 的任务：先尽力通知平台取消（忽略错误），再迁移到 expired 并退款。
func (w *Worker) expire(ctx context.Context, t *model.GenerationTask, snap *dsl.Snapshot) {
	logger.Warn("任务超时，置为 expired 并退款", zap.Uint64("task_id", t.ID), zap.String("status", t.Status), zap.Time("deadline_at", t.DeadlineAt))
	w.forgetTransfer(t.ID)
	if t.ProviderTaskID != "" {
		w.cancelProvider(ctx, t, snap, t.ProviderTaskID)
	}
	if _, err := w.store.Expire(ctx, t); err != nil {
		logger.Error("标记任务超时时出错，租约过期后重试", zap.Uint64("task_id", t.ID), zap.Error(err))
	}
}

// cancelProvider 尽力通知平台取消，失败只记日志。
func (w *Worker) cancelProvider(ctx context.Context, t *model.GenerationTask, snap *dsl.Snapshot, providerTaskID string) {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	err := w.exec.Cancel(cctx, snap, provider.TaskRef{ID: t.ID, UserID: t.UserID, ProviderTaskID: providerTaskID})
	if err != nil && !errors.Is(err, provider.ErrCancelUnsupported) {
		logger.Warn("通知平台取消任务失败", zap.Uint64("task_id", t.ID), zap.String("provider_task_id", providerTaskID), zap.Error(err))
	}
}

// retry 记录一次可重试的失败并安排下次处理。
func (w *Worker) retry(ctx context.Context, t *model.GenerationTask, attempts int, next time.Time) {
	if _, err := w.store.Retry(ctx, t, attempts, next); err != nil {
		logger.Error("安排任务重试失败，租约过期后重试", zap.Uint64("task_id", t.ID), zap.Error(err))
	}
}

// retryDB 对短暂的数据库错误做几次进程内重试；ctx 取消时立即返回。
func (w *Worker) retryDB(ctx context.Context, fn func() error) error {
	var err error
	for i := 0; i < 3; i++ {
		if err = fn(); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(time.Duration(i+1) * 200 * time.Millisecond):
		}
	}
	return err
}

func (w *Worker) webhookURL(t *model.GenerationTask, snap *dsl.Snapshot) string {
	if w.opts.WebhookURL == nil {
		return ""
	}
	key := snap.Provider.Key
	if key == "" {
		key = t.Provider
	}
	return w.opts.WebhookURL(key)
}

// jitter 给 d 加上 ±ratio 的随机抖动。
func (w *Worker) jitter(d time.Duration, ratio float64) time.Duration {
	return applyJitter(d, ratio, w.opts.Rand)
}

func decodeSnapshot(raw []byte) (*dsl.Snapshot, error) {
	var snap dsl.Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}

// errCode 取分类错误携带的平台 / 统一错误码。
func errCode(err error) string {
	var e *provider.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}
