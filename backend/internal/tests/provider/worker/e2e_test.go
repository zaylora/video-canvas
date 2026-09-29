package worker_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/dsl"
	"video-canvas/internal/provider/worker"
	"video-canvas/internal/config"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/ws"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

// 本文件是 worker + 真实 service + 真实仓储（PostgreSQL）的端到端测试：
// 验证状态流转、积分结算 / 退款、超时、租约过期后的重启恢复、对账零差异。
// 需要 TEST_DATABASE_DSN，未设置时跳过。

// 编译期确认 service 满足 worker 声明的 Store 接口。
var _ worker.Store = (*service.GenerationTaskService)(nil)

var e2eSeq atomic.Int64

func e2eDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("未设置 TEST_DATABASE_DSN，跳过集成测试")
	}
	quiet := &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)}
	admin, err := gorm.Open(postgres.Open(dsn), quiet)
	if err != nil {
		t.Fatalf("连接测试库失败：%v", err)
	}
	schema := fmt.Sprintf("wk_%d_%d", time.Now().UnixNano()%1_000_000_000, e2eSeq.Add(1))
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
	if err := db.AutoMigrate(&model.GenerationTask{}, &model.UserCredit{}, &model.CreditLedger{}); err != nil {
		t.Fatalf("迁移失败：%v", err)
	}
	return db
}

type e2eRegistry struct{ snap *dsl.Snapshot }

func (r *e2eRegistry) ListModels(context.Context, string) ([]provider.ModelInfo, error) { return nil, nil }
func (r *e2eRegistry) Snapshot(context.Context, string) (*dsl.Snapshot, error) {
	cp := *r.snap
	return &cp, nil
}
func (r *e2eRegistry) Provider(context.Context, string) (*dsl.ProviderConfig, error) {
	return &r.snap.Provider, nil
}

// e2eExec 是可编程的平台：按平台任务 id 记录提交，Query 由 statusFn 决定。
type e2eExec struct {
	mu       sync.Mutex
	submits  int
	cancels  int
	queryFn  func(providerTaskID string, n int) (*provider.QueryResult, error)
	queryN   map[string]int
	submitFn func(n int) (string, error)
}

func (e *e2eExec) Submit(ctx context.Context, snap *dsl.Snapshot, in provider.SubmitInput) (string, error) {
	e.mu.Lock()
	e.submits++
	n := e.submits
	fn := e.submitFn
	e.mu.Unlock()
	if fn != nil {
		return fn(n)
	}
	return fmt.Sprintf("pt-%d", in.Task.ID), nil
}

func (e *e2eExec) Query(ctx context.Context, snap *dsl.Snapshot, ref provider.TaskRef) (*provider.QueryResult, error) {
	e.mu.Lock()
	if e.queryN == nil {
		e.queryN = map[string]int{}
	}
	e.queryN[ref.ProviderTaskID]++
	n := e.queryN[ref.ProviderTaskID]
	fn := e.queryFn
	e.mu.Unlock()
	if fn == nil {
		return &provider.QueryResult{Status: provider.StatusRunning}, nil
	}
	return fn(ref.ProviderTaskID, n)
}

func (e *e2eExec) Cancel(context.Context, *dsl.Snapshot, provider.TaskRef) error {
	e.mu.Lock()
	e.cancels++
	e.mu.Unlock()
	return provider.ErrCancelUnsupported
}

func (e *e2eExec) Download(ctx context.Context, snap *dsl.Snapshot, url string) (*provider.Download, error) {
	return &provider.Download{Body: io.NopCloser(strings.NewReader("bytes")), ContentType: "video/mp4"}, nil
}

type e2eSaver struct{ next atomic.Uint64 }

func (s *e2eSaver) SaveGenerated(ctx context.Context, in provider.SaveGeneratedInput) (*model.Asset, string, error) {
	id := s.next.Add(1)
	return &model.Asset{ID: id, Kind: in.Kind, DurationMs: 5000, Width: 1280, Height: 720}, fmt.Sprintf("https://cdn/%d", id), nil
}

type e2eAssets struct{}

func (e2eAssets) Get(context.Context, uint64, uint64) (*model.Asset, error) {
	return nil, provider.ErrAssetNotFound
}
func (e2eAssets) Open(context.Context, uint64, uint64) (*provider.AssetFile, error) {
	return nil, provider.ErrAssetNotFound
}

type e2eBroadcaster struct {
	mu   sync.Mutex
	msgs []ws.Message
}

func (b *e2eBroadcaster) Publish(_ context.Context, _ string, msg ws.Message) {
	b.mu.Lock()
	b.msgs = append(b.msgs, msg)
	b.mu.Unlock()
}

func (b *e2eBroadcaster) views() []model.GenerationTaskView {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []model.GenerationTaskView
	for _, m := range b.msgs {
		if v, ok := m.Data.(*model.GenerationTaskView); ok {
			out = append(out, *v)
		}
	}
	return out
}

type e2eEnv struct {
	svc   *service.GenerationTaskService
	store *repository.GenerationTaskRepository
	exec  *e2eExec
	bc    *e2eBroadcaster
	reg   *e2eRegistry
	user  uint64
}

func newE2E(t *testing.T, deadline time.Duration) *e2eEnv {
	t.Helper()
	db := e2eDB(t)
	store := repository.NewGenerationTaskRepository(db)
	reg := &e2eRegistry{snap: &dsl.Snapshot{
		Provider: dsl.ProviderConfig{
			Key: "p1",
			Poll: dsl.PollConfig{
				FirstDelay: dsl.Duration(20 * time.Millisecond), Interval: dsl.Duration(20 * time.Millisecond),
				MaxInterval: dsl.Duration(40 * time.Millisecond), Jitter: 0.2,
			},
		},
		Model: dsl.ModelConfig{
			Key: "m1", Kind: model.KindVideo, Provider: "p1", Credits: 10, Deadline: dsl.Duration(deadline),
			InputSchema: dsl.InputSchema{{Name: "prompt", InputField: dsl.InputField{Type: dsl.FieldText, Required: true}}},
			Output:      dsl.OutputConfig{Media: model.KindVideo},
		},
	}}
	exec := &e2eExec{}
	bc := &e2eBroadcaster{}
	svc := service.NewGenerationTaskService(service.GenerationTaskDeps{
		Repo: store, Registry: reg, Executor: exec, Assets: e2eAssets{}, Broadcaster: bc,
		Config: config.AI{MaxActiveTasksPerUser: 10, InitialCredits: 50},
	},
		service.WithInputValidator(func(_ dsl.InputSchema, in map[string]any) (map[string]any, []dsl.FieldError) {
			if in["prompt"] == nil {
				return nil, []dsl.FieldError{{Field: "prompt", Message: "必填"}}
			}
			return in, nil
		}),
		service.WithMediaFieldNames(func(dsl.InputSchema) []string { return nil }),
	)
	return &e2eEnv{svc: svc, store: store, exec: exec, bc: bc, reg: reg, user: uint64(time.Now().UnixNano() / 1000)}
}

func (e *e2eEnv) newWorker(opts worker.Options) *worker.Worker {
	if opts.Tick == 0 {
		opts.Tick = 20 * time.Millisecond
	}
	return worker.New(e.svc, e.exec, &e2eSaver{}, e.svc.KickChan(), opts)
}

func (e *e2eEnv) create(t *testing.T) uint64 {
	t.Helper()
	v, err := e.svc.Create(context.Background(), e.user, "", &model.CreateGenerationTaskReq{
		Kind: model.KindVideo, ModelID: "m1", Input: map[string]any{"prompt": "猫"},
	})
	if err != nil {
		t.Fatalf("创建任务失败：%v", err)
	}
	return v.ID
}

func (e *e2eEnv) waitStatus(t *testing.T, id uint64, want string, timeout time.Duration) *model.GenerationTask {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		task, err := e.store.GetByIDAny(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if task.Status == want {
			return task
		}
		if time.Now().After(deadline) {
			t.Fatalf("等待任务 %d 变为 %s 超时，当前 %s（error_code=%s）", id, want, task.Status, task.ErrorCode)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (e *e2eEnv) assertReconciled(t *testing.T, initial int) *repository.CreditReconcile {
	t.Helper()
	rec, err := e.store.Reconcile(context.Background(), e.user)
	if err != nil {
		t.Fatal(err)
	}
	if rec.FrozenDiff() != 0 || rec.ActiveDiff() != 0 || rec.BalanceDiff(initial) != 0 {
		t.Fatalf("对账有差异：%+v", rec)
	}
	return rec
}

func runWorker(t *testing.T, w *worker.Worker) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = w.Run(ctx); close(done) }()
	return func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("worker 退出超时")
		}
	}
}

func TestE2E_SubmitPollFinalizeSucceedAndSettle(t *testing.T) {
	env := newE2E(t, 0)
	env.exec.queryFn = func(pid string, n int) (*provider.QueryResult, error) {
		switch {
		case n <= 1:
			return &provider.QueryResult{Status: provider.StatusQueued}, nil
		case n == 2:
			return &provider.QueryResult{Status: provider.StatusRunning, Progress: ptr(50)}, nil
		default:
			return &provider.QueryResult{Status: provider.StatusSucceeded, Outputs: []provider.Output{{URL: "https://p/a.mp4", Type: "mp4"}}}, nil
		}
	}
	stop := runWorker(t, env.newWorker(worker.Options{}))
	defer stop()

	id := env.create(t)
	task := env.waitStatus(t, id, model.TaskSucceeded, 10*time.Second)

	if task.FinishedAt == nil || task.OutputJSON == nil {
		t.Fatalf("成功任务应有产物和完成时间：%+v", task)
	}
	view, _ := env.svc.Get(context.Background(), env.user, id)
	if len(view.Outputs) != 1 || view.Outputs[0].AssetID == 0 || view.Outputs[0].MediaType != "video" {
		t.Fatalf("视图产物不对：%+v", view)
	}
	rec := env.assertReconciled(t, 50)
	if rec.Balance != 40 || rec.Frozen != 0 || rec.SettleSum != 10 {
		t.Fatalf("应结算 10 积分：%+v", rec)
	}

	// 推送：每次可见状态变化都推送，version 单调递增，最后一条是 succeeded
	views := env.bc.views()
	if len(views) < 4 {
		t.Fatalf("应至少推送 pending / queued / running / finalizing / succeeded：%d", len(views))
	}
	var last int64
	for _, v := range views {
		if v.Version <= last {
			t.Fatalf("推送的 version 应严格递增：%+v", views)
		}
		last = v.Version
	}
	if views[len(views)-1].Status != model.TaskSucceeded {
		t.Fatalf("最后一条推送应是 succeeded：%+v", views[len(views)-1])
	}
}

func TestE2E_PlatformFailureRefunds(t *testing.T) {
	env := newE2E(t, 0)
	env.exec.queryFn = func(string, int) (*provider.QueryResult, error) {
		return &provider.QueryResult{Status: provider.StatusFailed, ErrorClass: provider.ClassModeration, ErrorMessage: "nsfw"}, nil
	}
	stop := runWorker(t, env.newWorker(worker.Options{}))
	defer stop()

	id := env.create(t)
	task := env.waitStatus(t, id, model.TaskFailed, 10*time.Second)
	if task.ErrorCode != "moderation" || task.ErrorMessage != "内容未通过审核" {
		t.Fatalf("错误信息不对：%+v", task)
	}
	rec := env.assertReconciled(t, 50)
	if rec.Balance != 50 || rec.Frozen != 0 || rec.RefundSum != 10 {
		t.Fatalf("应全额退回：%+v", rec)
	}
}

func TestE2E_SubmitFailureRefunds(t *testing.T) {
	env := newE2E(t, 0)
	env.exec.submitFn = func(int) (string, error) {
		return "", &provider.Error{Class: provider.ClassSubmitUnknown, Message: "read timeout"}
	}
	stop := runWorker(t, env.newWorker(worker.Options{}))
	defer stop()

	id := env.create(t)
	task := env.waitStatus(t, id, model.TaskFailed, 10*time.Second)
	if task.ErrorCode != "submit_unknown" {
		t.Fatalf("错误码不对：%s", task.ErrorCode)
	}
	env.assertReconciled(t, 50)
}

func TestE2E_DeadlineExpiresAndRefunds(t *testing.T) {
	env := newE2E(t, 300*time.Millisecond)
	// 平台一直 running
	stop := runWorker(t, env.newWorker(worker.Options{}))
	defer stop()

	id := env.create(t)
	task := env.waitStatus(t, id, model.TaskExpired, 10*time.Second)
	if task.ErrorCode != "timeout" || task.ErrorMessage != "生成超时，积分已退回" {
		t.Fatalf("错误信息不对：%+v", task)
	}
	env.exec.mu.Lock()
	cancels := env.exec.cancels
	env.exec.mu.Unlock()
	if cancels != 1 {
		t.Fatalf("超时应尽力取消平台任务一次：%d", cancels)
	}
	rec := env.assertReconciled(t, 50)
	if rec.Frozen != 0 || rec.Balance != 50 {
		t.Fatalf("应退回：%+v", rec)
	}
}

func TestE2E_UserCancelStopsWorkerAndRefunds(t *testing.T) {
	env := newE2E(t, 0)
	stop := runWorker(t, env.newWorker(worker.Options{}))
	defer stop()

	id := env.create(t)
	env.waitStatus(t, id, model.TaskQueued, 10*time.Second)
	view, err := env.svc.Cancel(context.Background(), env.user, id)
	if err != nil || view.Status != model.TaskCanceled {
		t.Fatalf("取消失败：%+v %v", view, err)
	}
	time.Sleep(200 * time.Millisecond) // worker 不应再动这个任务
	task, _ := env.store.GetByIDAny(context.Background(), id)
	if task.Status != model.TaskCanceled || task.Version != 3 { // pending(1) → queued(2) → canceled(3)
		t.Fatalf("取消后任务不应再被改动：%+v", task)
	}
	rec := env.assertReconciled(t, 50)
	if rec.Frozen != 0 || rec.RefundSum != 10 {
		t.Fatalf("应退回：%+v", rec)
	}
}

func TestE2E_RestartRecovery_LeaseExpiredTaskIsPickedUp(t *testing.T) {
	env := newE2E(t, 0)
	env.exec.queryFn = func(string, int) (*provider.QueryResult, error) {
		return &provider.QueryResult{Status: provider.StatusSucceeded, Outputs: []provider.Output{{URL: "https://p/a.mp4", Type: "mp4"}}}, nil
	}
	id := env.create(t)

	// 旧进程领取了任务（写下 400ms 的租约）后崩溃：什么都没处理
	claimed, err := env.svc.ClaimDue(context.Background(), 10, 400*time.Millisecond)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("旧进程应领到任务：%d %v", len(claimed), err)
	}

	// 新进程启动：租约有效期内不会碰这个任务
	stop := runWorker(t, env.newWorker(worker.Options{Lease: 400 * time.Millisecond}))
	defer stop()
	time.Sleep(150 * time.Millisecond)
	task, _ := env.store.GetByIDAny(context.Background(), id)
	if task.Status != model.TaskPending {
		t.Fatalf("租约有效期内任务应保持 pending：%s", task.Status)
	}

	// 租约过期后被接手并走完整个流程；积分不重复扣
	env.waitStatus(t, id, model.TaskSucceeded, 10*time.Second)
	rec := env.assertReconciled(t, 50)
	if rec.Balance != 40 || rec.SettleSum != 10 {
		t.Fatalf("应只结算一次：%+v", rec)
	}
	env.exec.mu.Lock()
	submits := env.exec.submits
	env.exec.mu.Unlock()
	if submits != 1 {
		t.Fatalf("平台只应被提交一次：%d", submits)
	}
}

func TestE2E_TwoWorkersNeverProcessSameTaskTwice(t *testing.T) {
	env := newE2E(t, 0)
	env.exec.queryFn = func(string, int) (*provider.QueryResult, error) {
		return &provider.QueryResult{Status: provider.StatusSucceeded, Outputs: []provider.Output{{URL: "https://p/a.mp4", Type: "mp4"}}}, nil
	}
	stop1 := runWorker(t, env.newWorker(worker.Options{}))
	defer stop1()
	stop2 := runWorker(t, env.newWorker(worker.Options{}))
	defer stop2()

	const n = 5
	ids := make([]uint64, n)
	for i := range ids {
		ids[i] = env.create(t)
	}
	for _, id := range ids {
		env.waitStatus(t, id, model.TaskSucceeded, 15*time.Second)
	}
	env.exec.mu.Lock()
	submits := env.exec.submits
	env.exec.mu.Unlock()
	if submits != n {
		t.Fatalf("每个任务只应提交一次：%d != %d", submits, n)
	}
	rec := env.assertReconciled(t, 50)
	if rec.Balance != 50-n*10 || rec.Frozen != 0 {
		t.Fatalf("结算金额不对：%+v", rec)
	}
}

func TestE2E_SubmitTestRunsWithoutCreditsOrPush(t *testing.T) {
	env := newE2E(t, 0)
	env.exec.queryFn = func(string, int) (*provider.QueryResult, error) {
		return &provider.QueryResult{Status: provider.StatusSucceeded, Outputs: []provider.Output{{URL: "https://p/a.mp4", Type: "mp4"}}}, nil
	}
	stop := runWorker(t, env.newWorker(worker.Options{}))
	defer stop()

	v, err := env.svc.SubmitTest(context.Background(), env.user, env.reg.snap, map[string]any{"prompt": "x"})
	if err != nil {
		t.Fatal(err)
	}
	task := env.waitStatus(t, v.ID, model.TaskSucceeded, 10*time.Second)
	if !task.IsTest || task.Credits != 0 {
		t.Fatalf("应是 is_test 任务：%+v", task)
	}
	if _, err := env.store.GetCredit(context.Background(), env.user); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("试跑不应创建积分账户：%v", err)
	}
	if len(env.bc.views()) != 0 {
		t.Fatal("试跑不应推送")
	}
	got, err := env.svc.GetTestTask(context.Background(), env.user, v.ID)
	if err != nil || got.Status != model.TaskSucceeded || len(got.Outputs) != 1 {
		t.Fatalf("管理端应能查到试跑结果：%+v %v", got, err)
	}
}

func ptr(n int) *int { return &n }
