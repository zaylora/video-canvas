package queue

import (
	"context"
	crand "crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
	"github.com/riverqueue/river/rivertype"
	"go.uber.org/zap"
	"riverqueue.com/riverui"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/provider"
)

// 运行时的时间参数默认值。
const (
	defaultHeartbeatEvery = model.WorkerHeartbeatEvery
	defaultSweepEvery     = 30 * time.Second
	defaultStopTimeout    = 30 * time.Second
	// rescueStuckJobsAfter 是 River 自带的“救回卡住的 running job”的阈值，必须大于 job 的执行时限（jobTimeout）。
	// 实例崩溃时不会等到它：对账（sweep）会在实例心跳失联后立即把它的 job 放回队列，这个阈值只是兜底。
	rescueStuckJobsAfter = jobTimeout + time.Hour
	// sweepBatch 是一次对账最多补入队多少个丢了 job 的任务
	sweepBatch = 500
)

// SettingsSource 是后台可调并发的来源（真实实现是 service.SettingsService）。
type SettingsSource interface {
	// QueueConcurrency 返回当前设置的并发（每实例同时处理的任务数）。
	QueueConcurrency(ctx context.Context) (int, error)
}

// Repo 是运行时需要的数据访问（真实实现是 repository.QueueRepository）。
type Repo interface {
	// UpsertInstance 写入或刷新本实例的心跳记录。
	UpsertInstance(ctx context.Context, inst model.WorkerInstance) error
	// DeleteInstance 删除一个实例的记录（优雅退出时调用）。
	DeleteInstance(ctx context.Context, id string) error
	// DeleteStaleInstances 删除心跳早于 before 的实例记录。
	DeleteStaleInstances(ctx context.Context, before time.Time) error
	// LiveInstanceIDs 返回心跳不早于 since 的实例 id。
	LiveInstanceIDs(ctx context.Context, since time.Time) ([]string, error)
	// IsLeader 判断 clientID 是否是 River 当前的 leader。
	IsLeader(ctx context.Context, clientID string) (bool, error)
	// UnscheduledTasks 返回非终态、但队列里没有存活 job 的任务（最多 limit 个）。
	UnscheduledTasks(ctx context.Context, limit int) ([]model.UnscheduledTask, error)
	// RecoverOrphanedJobs 把“正由不在 liveIDs 里的实例执行”的 running job 立即放回队列，返回数量。
	RecoverOrphanedJobs(ctx context.Context, liveIDs []string) (int64, error)
}

// Config 是运行时的依赖与参数。
type Config struct {
	DB           *sql.DB       // 与 GORM 共用的连接池：入队因此能进业务事务
	ListenerPool *pgxpool.Pool // 只用来 LISTEN 新任务通知；为 nil 时退化成轮询（慢一些，不影响正确性）
	Schema       string        // 数据库 schema，空表示连接默认的；测试用

	Store    provider.TaskStore
	Handler  Handler
	Settings SettingsSource
	Repo     Repo
	Drama    DramaHandler // 短剧固定流水线步骤执行器

	Enabled bool // 本实例是否执行任务；为 false 时只能入队（API-only 实例）

	// Notifier 非空时，本实例（Enabled）还会执行“通知 Agent”的 job；AgentNotify 为 true 时，任务终态的事务里会入队这种 job
	//（API-only 实例也要入队：用户取消任务就发生在 API 实例上），两者通常同时由 Agent 是否启用决定。
	Notifier    provider.AgentNotifier
	AgentNotify bool
	HardCap     int // 并发的硬上限（配置文件），后台调整不能超过
	Version     string
	Hostname    string

	StopTimeout time.Duration // 优雅退出时等待在途任务的时间，默认 30 秒
	Logger      *zap.Logger
	Now         func() time.Time
	Rand        func() float64

	// 以下只给测试缩短间隔，零值用默认
	HeartbeatEvery    time.Duration
	SweepEvery        time.Duration
	SettingsEvery     time.Duration
	FetchPollInterval time.Duration
	NotifyBackoff     time.Duration // 通知重试退避的起点，默认 2 秒
}

// Runtime 是任务队列运行时：River 客户端、并发闸门、设置刷新、实例心跳、对账救回，以及后台的暂停 / 恢复 / 查看。
type Runtime struct {
	cfg      Config
	store    provider.TaskStore
	handler  Handler
	settings SettingsSource
	repo     Repo
	log      *zap.Logger

	client *river.Client[*sql.Tx]
	gate   *gate
	id     string
	start  time.Time
	ui     *riverui.Handler

	now  func() time.Time
	rand func() float64
}

// New 创建运行时（不启动）。创建后就可以入队（EnqueueTx），Run 之后才会执行任务。
func New(cfg Config) (*Runtime, error) {
	if cfg.Logger == nil {
		cfg.Logger = logger.L()
	}
	rt := &Runtime{
		cfg: cfg, store: cfg.Store, handler: cfg.Handler, settings: cfg.Settings, repo: cfg.Repo,
		log: cfg.Logger, start: time.Now(),
		now: cfg.Now, rand: cfg.Rand,
	}
	if rt.now == nil {
		rt.now = time.Now
	}
	if rt.rand == nil {
		rt.rand = rand.Float64
	}
	rt.id = newClientID(cfg.Hostname)

	// 闸门初始值取默认并发（启动后立即会按后台设置刷新一次）；硬上限缺失按默认并发的 4 倍兜底
	hardCap := cfg.HardCap
	if hardCap < 1 {
		hardCap = model.DefaultQueueConcurrency * 4
	}
	rt.gate = newGate(model.DefaultQueueConcurrency, hardCap)

	var driver *riverdatabasesql.Driver
	if cfg.ListenerPool != nil {
		driver = riverdatabasesql.NewWithPgxListener(cfg.DB, cfg.ListenerPool)
	} else {
		driver = riverdatabasesql.New(cfg.DB)
	}
	rc := &river.Config{
		ID:                   rt.id,
		Logger:               newSlog(rt.log),
		Schema:               cfg.Schema,
		MaxAttempts:          jobMaxAttempts,
		RescueStuckJobsAfter: rescueStuckJobsAfter,
		FetchPollInterval:    cfg.FetchPollInterval,
		RetryPolicy:          &retryPolicy{base: cmpOr(cfg.NotifyBackoff, defaultNotifyBackoff)},
	}
	if cfg.Enabled {
		workers := river.NewWorkers()
		river.AddWorker(workers, &taskWorker{rt: rt})
		rc.Workers = workers
		// River 队列的 MaxWorkers 启动后改不了，所以取硬上限；实际并发由 gate 控制（后台调整立即生效）
		rc.Queues = map[string]river.QueueConfig{riverQueue: {MaxWorkers: hardCap}}
		if cfg.Drama != nil {
			river.AddWorker(workers, &dramaWorker{rt: rt})
			rc.Queues["drama"] = river.QueueConfig{MaxWorkers: hardCap}
		}
		if cfg.Notifier != nil {
			river.AddWorker(workers, &notifyWorker{rt: rt})
			rc.Queues[notifyQueue] = river.QueueConfig{MaxWorkers: notifyWorkers}
		}
	}
	client, err := river.NewClient[*sql.Tx](driver, rc)
	if err != nil {
		return nil, fmt.Errorf("创建 River 客户端失败：%w", err)
	}
	rt.client = client
	return rt, nil
}

// cmpOr 返回 v，v 为零值时返回 def。
func cmpOr(v, def time.Duration) time.Duration {
	if v == 0 {
		return def
	}
	return v
}

// newClientID 生成本次启动的实例 id：主机名 + 随机后缀，重启后换新的（旧记录随心跳过期被清理）。
func newClientID(host string) string {
	var b [4]byte
	_, _ = crand.Read(b[:])
	if host == "" {
		host = "vc"
	}
	if len(host) > 40 {
		host = host[:40]
	}
	return host + "_" + hex.EncodeToString(b[:])
}

// ID 返回本实例的 id（也是 River 的 client id）。
func (rt *Runtime) ID() string { return rt.id }

// insertOptsAt 返回带排期的入队选项：还没到点的按 runAt 排期，已经到点（或没有）的立即可取，
// 立即可取的 job 入队时会通知，比定时扫描更快被取走。
func (rt *Runtime) insertOptsAt(runAt time.Time) *river.InsertOpts {
	opts := TaskArgs{}.InsertOpts()
	if runAt.After(rt.now().Add(dueSlack)) {
		opts.ScheduledAt = runAt
	}
	return &opts
}

// insert 为任务入队（不在事务里）。同一任务已有活着的 job 时什么都不做（唯一约束）。
func (rt *Runtime) insert(ctx context.Context, taskID uint64, runAt time.Time) error {
	_, err := rt.client.Insert(ctx, TaskArgs{TaskID: taskID}, rt.insertOptsAt(runAt))
	return err
}

// EnqueueTx 实现 repository.TaskEnqueuer：在 sqlTx 所在的事务里为任务入队，事务回滚时 job 也不存在。终态任务不入队。
func (rt *Runtime) EnqueueTx(ctx context.Context, sqlTx *sql.Tx, taskID uint64, status string, runAt time.Time) error {
	if model.IsTerminalStatus(status) {
		return nil
	}
	_, err := rt.client.InsertTx(ctx, sqlTx, TaskArgs{TaskID: taskID}, rt.insertOptsAt(runAt))
	return err
}

// Run 运行队列直到 ctx 取消，然后优雅退出（等在途任务，超时后强制取消）。API-only 实例（Enabled=false）只托管 River UI 的后台服务。
func (rt *Runtime) Run(ctx context.Context) error {
	if rt.ui != nil {
		if err := rt.ui.Start(ctx); err != nil {
			return fmt.Errorf("启动 River UI 失败：%w", err)
		}
	}
	if !rt.cfg.Enabled {
		<-ctx.Done()
		return nil
	}

	// 1. 先按后台设置刷新一次并发、写一次心跳（让本实例先算“在线”），再启动 River
	rt.RefreshSettings(ctx)
	rt.heartbeat(ctx)
	if err := rt.client.Start(ctx); err != nil {
		if ctx.Err() != nil {
			return nil // 还没启动完就收到了退出信号，不算失败
		}
		return fmt.Errorf("启动 River 失败：%w", err)
	}
	rt.log.Info("任务队列已启动", zap.String("instance", rt.id))

	// 2. 后台循环：设置刷新、实例心跳、对账救回
	loopCtx, stopLoops := context.WithCancel(ctx)
	var wg sync.WaitGroup
	every := func(d time.Duration, fn func(context.Context)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			t := time.NewTicker(d)
			defer t.Stop()
			for {
				select {
				case <-loopCtx.Done():
					return
				case <-t.C:
					fn(loopCtx)
				}
			}
		}()
	}
	every(orDur(rt.cfg.SettingsEvery, model.QueueSettingsRefreshEvery), rt.RefreshSettings)
	every(orDur(rt.cfg.HeartbeatEvery, defaultHeartbeatEvery), rt.heartbeat)
	every(orDur(rt.cfg.SweepEvery, defaultSweepEvery), rt.sweep)
	rt.sweep(loopCtx) // 启动后立即对账一次：升级前已在途的任务、崩溃遗留的任务马上接上

	// 3. 等退出信号，再优雅停止
	<-ctx.Done()
	stopLoops()
	wg.Wait()
	rt.shutdown()
	return nil
}

// shutdown 优雅退出：先让 River 停止领取并等在途 job（最多 StopTimeout），超时就取消它们——
// 被取消的 job 留在 running，会被其它实例（或下次启动）的对账立即救回；最后删掉本实例的心跳记录。
func (rt *Runtime) shutdown() {
	timeout := orDur(rt.cfg.StopTimeout, defaultStopTimeout)
	stopCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := rt.client.Stop(stopCtx); err != nil {
		rt.log.Warn("任务队列退出超时，取消在途任务", zap.Error(err))
		hardCtx, hardCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer hardCancel()
		_ = rt.client.StopAndCancel(hardCtx)
	}
	delCtx, delCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer delCancel()
	if err := rt.repo.DeleteInstance(delCtx, rt.id); err != nil {
		rt.log.Warn("删除实例心跳记录失败", zap.Error(err))
	}
	rt.log.Info("任务队列已停止", zap.String("instance", rt.id))
}

func orDur(d, def time.Duration) time.Duration {
	if d <= 0 {
		return def
	}
	return d
}

// RefreshSettings 按后台设置刷新并发（裁剪到 [1, 硬上限]）。读取失败只记日志，保持当前值。
// 后台修改并发后，本实例立即调用它，其它实例由定时循环在 QueueSettingsRefreshEvery 内跟上。
func (rt *Runtime) RefreshSettings(ctx context.Context) {
	if rt.settings == nil {
		return
	}
	n, err := rt.settings.QueueConcurrency(ctx)
	if err != nil {
		if ctx.Err() == nil {
			rt.log.Warn("读取队列并发设置失败，保持当前值", zap.Error(err))
		}
		return
	}
	rt.gate.setLimit(n)
}

// Stats 返回本实例的在途任务数与生效并发。
func (rt *Runtime) Stats() model.QueueInstanceStats {
	return model.QueueInstanceStats{Inflight: int(rt.gate.inflight.Load()), Limit: int(rt.gate.limit.Load())}
}

// HardCap 返回并发的硬上限。
func (rt *Runtime) HardCap() int { return int(rt.gate.cap) }

// Heartbeat 立即写一次本实例的心跳（后台改并发后调用，让“已生效于几个实例”马上包含本实例）。
func (rt *Runtime) Heartbeat(ctx context.Context) { rt.heartbeat(ctx) }

// heartbeat 写本实例的心跳，顺带清理过期很久的实例记录。写失败只记日志。
func (rt *Runtime) heartbeat(ctx context.Context) {
	stats, _ := json.Marshal(rt.Stats())
	leader, err := rt.repo.IsLeader(ctx, rt.id)
	if err != nil {
		leader = false // 查不到不影响心跳本身
	}
	now := rt.now()
	err = rt.repo.UpsertInstance(ctx, model.WorkerInstance{
		ID: rt.id, Hostname: rt.cfg.Hostname, Version: rt.cfg.Version,
		StartedAt: rt.start, HeartbeatAt: now, IsLeader: leader, StatsJSON: stats,
	})
	if err != nil && ctx.Err() == nil {
		rt.log.Warn("写实例心跳失败", zap.Error(err))
		return
	}
	if err := rt.repo.DeleteStaleInstances(ctx, now.Add(-model.WorkerInstanceKeepFor)); err != nil && ctx.Err() == nil {
		rt.log.Warn("清理过期实例记录失败", zap.Error(err))
	}
}

// sweep 是定时对账，做两件事，任何实例都可以跑（唯一约束和幂等的 SQL 保证并发安全）：
//  1. 救回失联实例的 job：由心跳已过期的实例正在执行的 running job，立即放回队列，不用等 River 自带的 1 小时阈值；
//  2. 补入队：任务还没终态、队列里却没有活着的 job（升级前已在途的任务、job 被误删），补一个。
func (rt *Runtime) sweep(ctx context.Context) {
	live, err := rt.repo.LiveInstanceIDs(ctx, rt.now().Add(-model.WorkerInstanceOfflineAfter))
	if err != nil {
		if ctx.Err() == nil {
			rt.log.Warn("对账：读取在线实例失败", zap.Error(err))
		}
		return
	}
	live = append(live, rt.id) // 本实例一定在线，哪怕心跳还没落库
	if n, err := rt.repo.RecoverOrphanedJobs(ctx, live); err != nil {
		rt.log.Warn("对账：救回失联实例的任务失败", zap.Error(err))
	} else if n > 0 {
		rt.log.Warn("对账：已把失联实例正在执行的任务放回队列", zap.Int64("jobs", n))
	}

	tasks, err := rt.repo.UnscheduledTasks(ctx, sweepBatch)
	if err != nil {
		if ctx.Err() == nil {
			rt.log.Warn("对账：查找丢失 job 的任务失败", zap.Error(err))
		}
		return
	}
	for _, t := range tasks {
		if err := rt.insert(ctx, t.ID, t.NextPollAt); err != nil {
			rt.log.Warn("对账：补入队失败", logger.TaskID(t.ID), zap.Error(err))
			continue
		}
		rt.log.Info("对账：已为没有 job 的任务补入队", logger.TaskID(t.ID))
	}
}

// State 返回队列的暂停状态与本实例的并发。队列在 River 里还不存在（从没启动过）时按未暂停处理。
func (rt *Runtime) State(ctx context.Context) (model.QueueState, error) {
	paused := false
	q, err := rt.client.QueueGet(ctx, riverQueue)
	switch {
	case err == nil:
		paused = q.PausedAt != nil
	case !errors.Is(err, rivertype.ErrNotFound):
		return model.QueueState{}, err
	}
	return model.QueueState{Paused: paused, Concurrency: int(rt.gate.limit.Load()), HardCap: int(rt.gate.cap)}, nil
}

// Pause 暂停队列：对所有实例生效（暂停状态存在数据库里），不再开始新任务，已入队的 job 保留、正在处理的任务继续，恢复后继续。幂等。
func (rt *Runtime) Pause(ctx context.Context) error {
	return rt.client.QueuePause(ctx, riverQueue, nil)
}

// Resume 恢复队列。幂等。
func (rt *Runtime) Resume(ctx context.Context) error {
	return rt.client.QueueResume(ctx, riverQueue, nil)
}

// UIHandler 返回 River UI 的 http.Handler（prefix 是它挂载的路径前缀，如 /api/v1/admin/river）。
// 鉴权由调用方负责：River UI 能重试、取消、删除 job。多次调用返回同一个 handler；它的后台服务在 Run 里启动。
func (rt *Runtime) UIHandler(prefix string) (http.Handler, error) {
	if rt.ui != nil {
		return rt.ui, nil
	}
	h, err := riverui.NewHandler(&riverui.HandlerOpts{
		Endpoints: riverui.NewEndpoints(rt.client, nil),
		Logger:    newSlog(rt.log),
		Prefix:    prefix,
	})
	if err != nil {
		return nil, fmt.Errorf("创建 River UI 失败：%w", err)
	}
	rt.ui = h
	return h, nil
}
