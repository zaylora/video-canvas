package queue_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/datatypes"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"video-canvas/internal/model"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/queue"
	"video-canvas/internal/repository"
)

var (
	schemaSeq atomic.Int64
	userSeq   atomic.Uint64
)

// queueDB 连接专用测试库，建一个独立 schema（search_path 指向它），迁移任务相关的表和 River 自己的表，用例结束后整体删除。
// 未设置 TEST_DATABASE_DSN 时跳过：队列要真实的 PostgreSQL 才有意义（advisory lock、SKIP LOCKED、LISTEN）。
func queueDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("未设置 TEST_DATABASE_DSN，跳过队列集成测试")
	}
	quiet := &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)}
	admin, err := gorm.Open(postgres.Open(dsn), quiet)
	if err != nil {
		t.Fatalf("连接测试库失败：%v", err)
	}
	schema := fmt.Sprintf("qt_%d_%d", time.Now().UnixNano()%1_000_000_000, schemaSeq.Add(1))
	if err := admin.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatalf("创建 schema 失败：%v", err)
	}
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schema), quiet)
	if err != nil {
		t.Fatalf("连接测试 schema 失败：%v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(30)
	t.Cleanup(func() {
		_ = sqlDB.Close()
		_ = admin.Exec("DROP SCHEMA " + schema + " CASCADE").Error
		if a, err := admin.DB(); err == nil {
			_ = a.Close()
		}
	})
	if err := db.AutoMigrate(&model.GenerationTask{}, &model.GenerationTaskEvent{}, &model.UserCredit{}, &model.CreditLedger{},
		&model.AIChannel{}, &model.User{}, &model.WorkerInstance{}); err != nil {
		t.Fatalf("迁移业务表失败：%v", err)
	}
	if err := queue.Migrate(context.Background(), sqlDB, ""); err != nil {
		t.Fatalf("迁移 River 表失败：%v", err)
	}
	return db
}

func newUserID() uint64 { return uint64(time.Now().UnixNano()/1000)*100 + userSeq.Add(1)%100 }

// addChannel 建一个渠道，maxRunning 为 0 表示不限。
func addChannel(t *testing.T, db *gorm.DB, key string, maxRunning int) {
	t.Helper()
	ch := &model.AIChannel{Key: key, Name: key, BaseURL: "http://x", HeadersJSON: model.JSONText("{}"),
		RateLimitJSON: model.JSONText(fmt.Sprintf(`{"max_running":%d}`, maxRunning)), Enabled: true}
	if err := db.Create(ch).Error; err != nil {
		t.Fatalf("建渠道失败：%v", err)
	}
}

// dbStore 是 provider.TaskStore 的最小实现：任务读写走真实的表，名额申请走真实的仓储，
// 用户并发上限由测试指定。队列只用到 GetTask 和 AcquireSlot，其余迁移方法由 scriptHandler 直接改表模拟。
type dbStore struct {
	provider.TaskStore // 没用到的方法留空，用到了会 panic，暴露测试写漏的地方
	repo               *repository.GenerationTaskRepository
	userLimit          int
}

func (s *dbStore) GetTask(ctx context.Context, id uint64) (*model.GenerationTask, error) {
	t, err := s.repo.GetByIDAny(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, nil
	}
	return t, err
}

func (s *dbStore) AcquireSlot(ctx context.Context, t *model.GenerationTask) (provider.SlotOutcome, error) {
	now := time.Now()
	res, err := s.repo.AcquireSlot(ctx, repository.SlotRequest{
		TaskID: t.ID, UserLimit: s.userLimit, DefaultUserLimit: s.userLimit, Now: now, Deadline: now.Add(30 * time.Minute),
	})
	return provider.SlotOutcome(res), err
}

// scriptHandler 按任务状态模拟业务处理：pending → queued → running → finalizing → succeeded，每一步都直接改表，
// 并记录调用与并发情况，供测试断言。
type scriptHandler struct {
	db          *gorm.DB
	submitDelay time.Duration                // 提交这一步耗时，用来观察并发
	hold        func(t model.GenerationTask) // 非空时在每一步开头调用，测试用它把某个任务卡住

	mu      sync.Mutex
	calls   []string // "<状态>:<任务 id>"
	expired []uint64
	open    map[uint64]bool // 已经开始处理、还没到终态的任务：它们都占着并发名额
	maxOpen atomic.Int32    // 观察到的“同时在处理的任务数”最大值
	maxChan atomic.Int32    // 观察到的“渠道占用”最大值（拿到名额且 pending / queued / running）
	maxUser atomic.Int32    // 观察到的单个用户“占用名额”最大值（拿到名额且非终态）
}

func newHandler(db *gorm.DB) *scriptHandler {
	return &scriptHandler{db: db, open: map[uint64]bool{}}
}

func bump(peak *atomic.Int32, v int32) {
	for {
		cur := peak.Load()
		if v <= cur || peak.CompareAndSwap(cur, v) {
			return
		}
	}
}

func (h *scriptHandler) update(id uint64, fields map[string]any) {
	if err := h.db.Model(&model.GenerationTask{}).Where("id = ?", id).Updates(fields).Error; err != nil {
		panic(err)
	}
}

func (h *scriptHandler) Handle(_ context.Context, t model.GenerationTask) {
	if h.hold != nil {
		h.hold(t)
	}
	h.mu.Lock()
	h.calls = append(h.calls, fmt.Sprintf("%s:%d", t.Status, t.ID))
	h.open[t.ID] = true
	bump(&h.maxOpen, int32(len(h.open)))
	h.mu.Unlock()

	var chanHeld, userHeld int64
	h.db.Model(&model.GenerationTask{}).Where("provider = ? AND slot_acquired_at IS NOT NULL AND status IN ?", t.Provider, []string{"pending", "queued", "running"}).Count(&chanHeld)
	h.db.Model(&model.GenerationTask{}).Where("user_id = ? AND is_test = FALSE AND slot_acquired_at IS NOT NULL AND status IN ?", t.UserID, model.ActiveTaskStatuses).Count(&userHeld)
	bump(&h.maxChan, int32(chanHeld))
	bump(&h.maxUser, int32(userHeld))

	now := time.Now()
	switch t.Status {
	case model.TaskPending:
		time.Sleep(h.submitDelay)
		h.update(t.ID, map[string]any{"status": model.TaskQueued, "submitted_at": now, "next_poll_at": now.Add(50 * time.Millisecond), "version": gorm.Expr("version + 1")})
	case model.TaskQueued:
		h.update(t.ID, map[string]any{"status": model.TaskRunning, "next_poll_at": now.Add(50 * time.Millisecond), "version": gorm.Expr("version + 1")})
	case model.TaskRunning:
		h.update(t.ID, map[string]any{"status": model.TaskFinalizing, "next_poll_at": now, "version": gorm.Expr("version + 1")})
	case model.TaskFinalizing:
		h.update(t.ID, map[string]any{"status": model.TaskSucceeded, "finished_at": now, "version": gorm.Expr("version + 1")})
		h.mu.Lock()
		delete(h.open, t.ID)
		h.mu.Unlock()
	}
}

func (h *scriptHandler) Expire(_ context.Context, t model.GenerationTask) {
	h.mu.Lock()
	h.expired = append(h.expired, t.ID)
	h.mu.Unlock()
	h.update(t.ID, map[string]any{"status": model.TaskExpired, "finished_at": time.Now()})
}

func (h *scriptHandler) count(prefix string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, c := range h.calls {
		if len(c) >= len(prefix) && c[:len(prefix)] == prefix {
			n++
		}
	}
	return n
}

type fixedSettings struct {
	mu  sync.Mutex
	cur int
}

func (f *fixedSettings) QueueConcurrency(context.Context) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cur, nil
}

func (f *fixedSettings) set(n int) {
	f.mu.Lock()
	f.cur = n
	f.mu.Unlock()
}

// rig 把一套运行时跑起来：真实的 River、真实的仓储，业务处理由 scriptHandler 模拟。
type rig struct {
	db       *gorm.DB
	repo     *repository.GenerationTaskRepository
	queueRep *repository.QueueRepository
	rt       *queue.Runtime
	handler  *scriptHandler
	settings *fixedSettings
}

type rigOpts struct {
	userLimit   int
	submitDelay time.Duration
	conc        int  // 并发；缺省 8
	noStart     bool // 只建不跑（测试回滚、暂停前的准备）
}

func newRig(t *testing.T, db *gorm.DB, o rigOpts) *rig {
	t.Helper()
	if o.userLimit == 0 {
		o.userLimit = 100
	}
	repo := repository.NewGenerationTaskRepository(db)
	h := newHandler(db)
	h.submitDelay = o.submitDelay
	settings := &fixedSettings{cur: 8}
	if o.conc > 0 {
		settings.cur = o.conc
	}
	sqlDB, _ := db.DB()
	qrepo := repository.NewQueueRepository(db)
	rt, err := queue.New(queue.Config{
		DB: sqlDB, Store: &dbStore{repo: repo, userLimit: o.userLimit}, Handler: h, Settings: settings, Repo: qrepo,
		Enabled: true, HardCap: 16,
		Version: "test", Hostname: "testhost", StopTimeout: 5 * time.Second,
		HeartbeatEvery: 200 * time.Millisecond, SweepEvery: 300 * time.Millisecond, SettingsEvery: 100 * time.Millisecond,
		FetchPollInterval: 200 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("创建运行时失败：%v", err)
	}
	repo.SetEnqueuer(rt)
	r := &rig{db: db, repo: repo, queueRep: qrepo, rt: rt, handler: h, settings: settings}
	if !o.noStart {
		r.start(t)
	}
	return r
}

func (r *rig) start(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := r.rt.Run(ctx); err != nil {
			t.Errorf("Run 失败：%v", err)
		}
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			t.Error("运行时没有在 15 秒内退出")
		}
	})
}

// addTask 在一个事务里插入 pending 任务并入队（与创建任务的真实路径一致）。
func (r *rig) addTask(t *testing.T, user uint64, provider string, mut func(*model.GenerationTask)) *model.GenerationTask {
	t.Helper()
	tk := r.newTask(user, provider, mut)
	err := r.repo.WithTx(context.Background(), func(tx repository.GenerationTaskTx) error {
		if ok, err := tx.InsertTask(context.Background(), tk); err != nil || !ok {
			return fmt.Errorf("插入失败 %w inserted=%v", err, ok)
		}
		return tx.EnqueueTask(context.Background(), tk)
	})
	if err != nil {
		t.Fatalf("建任务失败：%v", err)
	}
	return tk
}

func (r *rig) newTask(user uint64, provider string, mut func(*model.GenerationTask)) *model.GenerationTask {
	tk := &model.GenerationTask{
		UserID: user, Kind: model.KindVideo, ModelKey: "m", Provider: provider, Status: model.TaskPending,
		InputJSON: datatypes.JSON(`{"prompt":"x"}`), ConfigSnapshot: datatypes.JSON(`{}`), Credits: 1, Version: 1,
		NextPollAt: time.Now(), DeadlineAt: time.Now().Add(time.Hour),
	}
	if mut != nil {
		mut(tk)
	}
	return tk
}

func (r *rig) status(id uint64) string {
	var tk model.GenerationTask
	if err := r.db.Select("status").First(&tk, id).Error; err != nil {
		return "?"
	}
	return tk.Status
}

// waitStatus 轮询到任务进入目标状态，超时则失败（异步测试统一用轮询加超时）。
func (r *rig) waitStatus(t *testing.T, id uint64, want string, timeout time.Duration) {
	t.Helper()
	waitFor(t, timeout, fmt.Sprintf("任务 %d 进入 %s（当前 %s）", id, want, r.status(id)), func() bool { return r.status(id) == want })
}

func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("等待超时：%s", what)
}

// liveJobs 返回某任务现在还活着的 job 数。
func (r *rig) liveJobs(taskID uint64) int64 {
	var n int64
	r.db.Raw(`SELECT COUNT(*) FROM river_job WHERE (args->>'task_id')::bigint = ? AND state IN ('available','pending','retryable','running','scheduled')`, taskID).Scan(&n)
	return n
}

// slotHeld 报告任务是否已经拿到名额。
func (r *rig) slotHeld(id uint64) bool {
	var tk model.GenerationTask
	if err := r.db.Select("slot_acquired_at").First(&tk, id).Error; err != nil {
		return false
	}
	return tk.SlotAcquiredAt != nil
}
