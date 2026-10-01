package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	. "video-canvas/internal/service"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"video-canvas/internal/config"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
)

// 本文件是任务服务 + 真实仓储的集成测试：验证积分事务、幂等、并发迁移在真实 PostgreSQL 上的行为。
// 需要 TEST_DATABASE_DSN，未设置时跳过。

var (
	gtiSchemaSeq atomic.Int64
	gtiUserSeq   atomic.Uint64
)

// gtiTestDB 为本用例创建独立 schema 并迁移需要的表，用例结束后整体删除，避免与其它测试互相干扰。
func gtiTestDB(t *testing.T) *gorm.DB {
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
	schema := fmt.Sprintf("gti_%d_%d", time.Now().UnixNano()%1_000_000_000, gtiSchemaSeq.Add(1))
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

// gtiEnv 用真实仓储组装任务服务，其余依赖用单测里的 fake。
type gtiEnv struct {
	*taskSvcEnv
	store *repository.GenerationTaskRepository
	user  uint64
}

func newGTIEnv(t *testing.T, cfg config.AI) *gtiEnv {
	t.Helper()
	db := gtiTestDB(t)
	real := repository.NewGenerationTaskRepository(db)
	env := newTaskSvcEnv(cfg)
	env.now = time.Now()
	env.svc = NewGenerationTaskService(GenerationTaskDeps{
		Repo: real, Registry: env.registry, Executor: env.exec, Assets: env.assets, Broadcaster: env.bc, Config: cfg,
	})
	return &gtiEnv{taskSvcEnv: env, store: real, user: uint64(time.Now().UnixNano()/1000)*100 + gtiUserSeq.Add(1)%100}
}

func (e *gtiEnv) create(key string) (*model.GenerationTaskView, error) {
	return createSingle(context.Background(), e.svc, e.user, key, validCreateReq())
}

// assertReconciled 断言账户、流水、进行中任务三方对账零差异。
func (e *gtiEnv) assertReconciled(t *testing.T, initial int) *repository.CreditReconcile {
	t.Helper()
	rec, err := e.store.Reconcile(context.Background(), e.user)
	if err != nil {
		t.Fatalf("对账失败：%v", err)
	}
	if rec.FrozenDiff() != 0 || rec.ActiveDiff() != 0 || rec.BalanceDiff(initial) != 0 {
		t.Fatalf("对账有差异：%+v（frozenDiff=%d activeDiff=%d balanceDiff=%d）",
			rec, rec.FrozenDiff(), rec.ActiveDiff(), rec.BalanceDiff(initial))
	}
	return rec
}

func TestGenerationTaskService_Integration_Create(t *testing.T) {
	cfg := config.AI{MaxActiveTasksPerUser: 4, InitialCredits: 50}

	t.Run("提交冻结积分并写流水，对账零差异", func(t *testing.T) {
		env := newGTIEnv(t, cfg)
		v, err := env.create("")
		assertTaskCode(t, err, 0)
		if v.Status != model.TaskPending || v.ID == 0 {
			t.Fatalf("视图不对：%+v", v)
		}
		rec := env.assertReconciled(t, 50)
		if rec.Frozen != 10 || rec.Balance != 50 || rec.FreezeSum != 10 {
			t.Fatalf("对账结果不对：%+v", rec)
		}
	})

	t.Run("同一个 Idempotency-Key 并发提交只成功创建一次，只冻结一次", func(t *testing.T) {
		env := newGTIEnv(t, cfg)
		const n = 20
		ids := make([]uint64, n)
		errs := make([]error, n)
		var wg sync.WaitGroup
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				v, err := env.create("same-key")
				errs[i] = err
				if v != nil {
					ids[i] = v.ID
				}
			}()
		}
		wg.Wait()
		for i := 0; i < n; i++ {
			if errs[i] != nil {
				t.Fatalf("第 %d 个请求出错：%v", i, errs[i])
			}
			if ids[i] != ids[0] {
				t.Fatalf("所有请求都应返回同一个任务：%v", ids)
			}
		}
		rec := env.assertReconciled(t, 50)
		if rec.Frozen != 10 || rec.FreezeSum != 10 {
			t.Fatalf("只应冻结一次：%+v", rec)
		}
		count, _ := env.store.CountActive(context.Background(), env.user)
		if count != 1 {
			t.Fatalf("只应有一个任务，实际 %d", count)
		}
		// 幂等重复提交的推送只有第一次（赢家）
		if len(env.bc.msgs) != 1 {
			t.Fatalf("应只推送一次，实际 %d", len(env.bc.msgs))
		}
	})

	t.Run("并发提交受并发上限约束：超出返回 429，冻结额与任务数一致", func(t *testing.T) {
		c := cfg
		c.InitialCredits = 1000
		env := newGTIEnv(t, c)
		const n = 12
		var ok, tooMany atomic.Int32
		var wg sync.WaitGroup
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := env.create("")
				var e *errcode.Error
				switch {
				case err == nil:
					ok.Add(1)
				case errors.As(err, &e) && e.Code == errcode.ErrTooManyTasks.Code:
					tooMany.Add(1)
				default:
					t.Errorf("意外错误：%v", err)
				}
			}()
		}
		wg.Wait()
		if ok.Load() != 4 || tooMany.Load() != n-4 {
			t.Fatalf("期望 4 成功 %d 被限流，实际 %d / %d", n-4, ok.Load(), tooMany.Load())
		}
		rec := env.assertReconciled(t, 1000)
		if rec.Frozen != 40 {
			t.Fatalf("冻结额应为 40：%+v", rec)
		}
	})

	t.Run("并发提交不会把可用余额冻结成负数", func(t *testing.T) {
		c := cfg
		c.InitialCredits = 25 // 只够 2 个任务（每个 10）
		c.MaxActiveTasksPerUser = 100
		env := newGTIEnv(t, c)
		var ok, insufficient atomic.Int32
		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := env.create("")
				var e *errcode.Error
				switch {
				case err == nil:
					ok.Add(1)
				case errors.As(err, &e) && e.Code == errcode.ErrInsufficientCredits.Code:
					insufficient.Add(1)
				default:
					t.Errorf("意外错误：%v", err)
				}
			}()
		}
		wg.Wait()
		if ok.Load() != 2 || insufficient.Load() != 8 {
			t.Fatalf("期望 2 成功 8 积分不足，实际 %d / %d", ok.Load(), insufficient.Load())
		}
		rec := env.assertReconciled(t, 25)
		if rec.Frozen != 20 {
			t.Fatalf("冻结额应为 20：%+v", rec)
		}
	})
}

func TestGenerationTaskService_Integration_Transitions(t *testing.T) {
	ctx := context.Background()
	cfg := config.AI{MaxActiveTasksPerUser: 10, InitialCredits: 50}

	// makeTask 创建一个任务并推进到指定状态（走真实迁移方法）。
	makeTask := func(t *testing.T, env *gtiEnv, status string) *model.GenerationTask {
		t.Helper()
		v, err := env.create("")
		assertTaskCode(t, err, 0)
		task, err := env.store.GetByIDAny(ctx, v.ID)
		if err != nil {
			t.Fatal(err)
		}
		if status == model.TaskPending {
			return task
		}
		if _, err := env.svc.MarkSubmitted(ctx, task, "pt-"+fmt.Sprint(v.ID), nil, time.Now()); err != nil {
			t.Fatal(err)
		}
		task, _ = env.store.GetByIDAny(ctx, v.ID)
		switch status {
		case model.TaskFinalizing:
			if _, err := env.svc.MarkFinalizing(ctx, task); err != nil {
				t.Fatal(err)
			}
			task, _ = env.store.GetByIDAny(ctx, v.ID)
		case model.TaskQueued:
		default:
			t.Fatalf("不支持的状态 %s", status)
		}
		return task
	}

	t.Run("成功结算：余额和冻结各减 credits，流水与账户对账", func(t *testing.T) {
		env := newGTIEnv(t, cfg)
		task := makeTask(t, env, model.TaskFinalizing)
		applied, err := env.svc.Complete(ctx, task, []model.TaskOutput{{AssetID: 1, URL: "u", MediaType: "video"}}, nil)
		if err != nil || !applied {
			t.Fatalf("applied=%v err=%v", applied, err)
		}
		rec := env.assertReconciled(t, 50)
		if rec.Balance != 40 || rec.Frozen != 0 || rec.SettleSum != 10 {
			t.Fatalf("结算结果不对：%+v", rec)
		}
		got, _ := env.store.GetByIDAny(ctx, task.ID)
		if got.Status != model.TaskSucceeded || got.OutputJSON == nil {
			t.Fatalf("任务状态不对：%+v", got)
		}
	})

	t.Run("失败退款：只减冻结，余额不变", func(t *testing.T) {
		env := newGTIEnv(t, cfg)
		task := makeTask(t, env, model.TaskQueued)
		applied, err := env.svc.Fail(ctx, task, "provider_error", "平台繁忙，请稍后重试")
		if err != nil || !applied {
			t.Fatalf("applied=%v err=%v", applied, err)
		}
		rec := env.assertReconciled(t, 50)
		if rec.Balance != 50 || rec.Frozen != 0 || rec.RefundSum != 10 {
			t.Fatalf("退款结果不对：%+v", rec)
		}
	})

	t.Run("重复结算 / 重复退款不会重复扣退", func(t *testing.T) {
		env := newGTIEnv(t, cfg)
		a := makeTask(t, env, model.TaskFinalizing)
		b := makeTask(t, env, model.TaskQueued)
		for i := 0; i < 3; i++ {
			_, _ = env.svc.Complete(ctx, a, nil, nil)
			_, _ = env.svc.Fail(ctx, b, "provider_error", "x")
			_, _ = env.svc.Expire(ctx, b)
		}
		rec := env.assertReconciled(t, 50)
		if rec.Balance != 40 || rec.Frozen != 0 || rec.SettleSum != 10 || rec.RefundSum != 10 {
			t.Fatalf("不应重复扣退：%+v", rec)
		}
	})

	t.Run("流水已存在（重放）时事务不被污染且不再改余额", func(t *testing.T) {
		env := newGTIEnv(t, cfg)
		task := makeTask(t, env, model.TaskFinalizing)
		// 模拟“上一次已写流水但进程崩溃前状态没迁移”之类的重放：手工先插入 settle 流水
		if _, err := env.store.InsertLedger(ctx, &model.CreditLedger{UserID: env.user, TaskID: task.ID, Type: model.LedgerSettle, Amount: 10}); err != nil {
			t.Fatal(err)
		}
		applied, err := env.svc.Complete(ctx, task, nil, nil)
		if err != nil || !applied {
			t.Fatalf("applied=%v err=%v", applied, err)
		}
		acc, _ := env.store.GetCredit(ctx, env.user)
		if acc.Balance != 50 || acc.Frozen != 10 {
			t.Fatalf("已有流水时不应再改账户：%+v", acc)
		}
		got, _ := env.store.GetByIDAny(ctx, task.ID)
		if got.Status != model.TaskSucceeded {
			t.Fatalf("状态应已迁移：%s", got.Status)
		}
	})

	t.Run("并发的终态迁移只有一个成功，对账零差异", func(t *testing.T) {
		env := newGTIEnv(t, cfg)
		task := makeTask(t, env, model.TaskFinalizing)
		const n = 16
		var applied atomic.Int32
		var wg sync.WaitGroup
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				var ok bool
				var err error
				switch i % 4 {
				case 0:
					ok, err = env.svc.Complete(ctx, task, nil, nil)
				case 1:
					ok, err = env.svc.Fail(ctx, task, "provider_error", "x")
				case 2:
					ok, err = env.svc.Expire(ctx, task)
				case 3:
					var v *model.GenerationTaskView
					v, err = env.svc.Cancel(ctx, env.user, task.ID)
					ok = v != nil
					var e *errcode.Error
					if errors.As(err, &e) && e.Code == errcode.ErrTaskNotCancelable.Code {
						err = nil
					}
				}
				if err != nil {
					t.Errorf("意外错误：%v", err)
				}
				if ok {
					applied.Add(1)
				}
			}()
		}
		wg.Wait()
		if applied.Load() != 1 {
			t.Fatalf("期望只有 1 个迁移成功，实际 %d", applied.Load())
		}
		rec := env.assertReconciled(t, 50)
		if rec.Frozen != 0 || rec.SettleSum+rec.RefundSum != 10 {
			t.Fatalf("应只结算或只退款一次：%+v", rec)
		}
		got, _ := env.store.GetByIDAny(ctx, task.ID)
		if got.Version != 4 { // pending(1) → queued(2) → finalizing(3) → 终态(4)
			t.Fatalf("version 应只再 +1：%d", got.Version)
		}
	})

	t.Run("多个任务混合终态后对账零差异", func(t *testing.T) {
		env := newGTIEnv(t, cfg)
		succeeded := makeTask(t, env, model.TaskFinalizing)
		failed := makeTask(t, env, model.TaskQueued)
		expired := makeTask(t, env, model.TaskQueued)
		canceled := makeTask(t, env, model.TaskQueued)
		running := makeTask(t, env, model.TaskQueued)
		_ = running // 保持进行中

		_, _ = env.svc.Complete(ctx, succeeded, nil, nil)
		_, _ = env.svc.Fail(ctx, failed, "moderation", "内容未通过审核")
		_, _ = env.svc.Expire(ctx, expired)
		if _, err := env.svc.Cancel(ctx, env.user, canceled.ID); err != nil {
			t.Fatal(err)
		}

		rec := env.assertReconciled(t, 50)
		if rec.Balance != 40 || rec.Frozen != 10 || rec.SettleSum != 10 || rec.RefundSum != 30 || rec.FreezeSum != 50 {
			t.Fatalf("汇总不对：%+v", rec)
		}
		if len(env.exec.cancelCalls) != 1 {
			t.Fatalf("只有用户取消应通知平台：%d", len(env.exec.cancelCalls))
		}
	})

	t.Run("试跑任务不动积分", func(t *testing.T) {
		env := newGTIEnv(t, cfg)
		v, err := env.svc.SubmitTest(ctx, env.user, fakeTaskSnapshot(model.KindVideo, 10), map[string]any{"prompt": "x"})
		assertTaskCode(t, err, 0)
		task, _ := env.store.GetByIDAny(ctx, v.ID)
		_, _ = env.svc.MarkSubmitted(ctx, task, "pt", nil, time.Now())
		task, _ = env.store.GetByIDAny(ctx, v.ID)
		_, _ = env.svc.MarkFinalizing(ctx, task)
		task, _ = env.store.GetByIDAny(ctx, v.ID)
		applied, err := env.svc.Complete(ctx, task, nil, nil)
		if err != nil || !applied {
			t.Fatalf("applied=%v err=%v", applied, err)
		}
		if _, err := env.store.GetCredit(ctx, env.user); !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("试跑不应创建积分账户：%v", err)
		}
		if len(env.bc.msgs) != 0 {
			t.Fatalf("试跑不应推送：%d", len(env.bc.msgs))
		}
	})
}

// jsonb 列会规范化空白与键顺序，所以只比较解码后的语义，不比较原始字节。
func TestGenerationTaskService_Integration_ProviderColumns(t *testing.T) {
	ctx := context.Background()
	cfg := config.AI{MaxActiveTasksPerUser: 10, InitialCredits: 50}

	t.Run("config_snapshot 落库后能还原出冻结的模型 / 渠道 / 插件版本", func(t *testing.T) {
		env := newGTIEnv(t, cfg)
		v, err := env.create("")
		assertTaskCode(t, err, 0)
		task, err := env.store.GetByIDAny(ctx, v.ID)
		if err != nil {
			t.Fatal(err)
		}
		var snap provider.Snapshot
		if err := json.Unmarshal(task.ConfigSnapshot, &snap); err != nil {
			t.Fatalf("快照解码失败：%v", err)
		}
		if snap.ModelRevisionID != 42 || snap.Channel.PluginVersionID != 7 || snap.Plugin.SHA256 != "abc123" || snap.Model.Key != "m1" {
			t.Fatalf("快照内容不对：%+v", snap)
		}
	})

	t.Run("provider_state：提交时合并插件 state，保留准备阶段的 prepared", func(t *testing.T) {
		env := newGTIEnv(t, cfg)
		v, _ := env.create("")
		task, _ := env.store.GetByIDAny(ctx, v.ID)
		if err := env.svc.SaveProviderState(ctx, task, provider.ProviderState{Prepared: json.RawMessage(`{"file":"f-1"}`)}); err != nil {
			t.Fatal(err)
		}
		task, _ = env.store.GetByIDAny(ctx, v.ID)
		if applied, err := env.svc.MarkSubmitted(ctx, task, "pt-1", json.RawMessage(`{"cursor":"c1"}`), time.Now()); err != nil || !applied {
			t.Fatalf("applied=%v err=%v", applied, err)
		}
		got, _ := env.store.GetByIDAny(ctx, v.ID)
		st, ok := provider.DecodeProviderState(got.ProviderState)
		if !ok {
			t.Fatalf("provider_state 解码失败：%s", got.ProviderState)
		}
		var prepared, plugin map[string]string
		_ = json.Unmarshal(st.Prepared, &prepared)
		_ = json.Unmarshal(st.Plugin, &plugin)
		if prepared["file"] != "f-1" || plugin["cursor"] != "c1" {
			t.Fatalf("prepared / plugin 都应保留：%s", got.ProviderState)
		}
		if got.Status != model.TaskQueued || got.ProviderTaskID != "pt-1" || got.Version != 2 {
			t.Fatalf("任务状态不对：%+v", got)
		}
	})

	t.Run("provider_result：同步结果落库并进入 finalizing，随后结算积分", func(t *testing.T) {
		env := newGTIEnv(t, cfg)
		v, _ := env.create("")
		task, _ := env.store.GetByIDAny(ctx, v.ID)
		outs := json.RawMessage(`[{"type":"url","url":"https://up.example.com/a.png"}]`)
		if applied, err := env.svc.MarkImmediate(ctx, task, "sync-1", nil, outs); err != nil || !applied {
			t.Fatalf("applied=%v err=%v", applied, err)
		}
		got, _ := env.store.GetByIDAny(ctx, v.ID)
		var back []provider.Output
		if err := json.Unmarshal(got.ProviderResult, &back); err != nil || len(back) != 1 || back[0].URL != "https://up.example.com/a.png" {
			t.Fatalf("provider_result 不对：%s %v", got.ProviderResult, err)
		}
		if got.Status != model.TaskFinalizing || got.ProviderTaskID != "sync-1" {
			t.Fatalf("任务状态不对：%+v", got)
		}
		if applied, err := env.svc.Complete(ctx, got, nil, nil); err != nil || !applied {
			t.Fatalf("applied=%v err=%v", applied, err)
		}
		rec := env.assertReconciled(t, 50)
		if rec.Balance != 40 || rec.Frozen != 0 {
			t.Fatalf("结算结果不对：%+v", rec)
		}
	})

	t.Run("试跑任务：追踪写入 trace_json，GetTestTrace 读回；正式任务不记追踪", func(t *testing.T) {
		env := newGTIEnv(t, cfg)
		tv, err := env.svc.SubmitTest(ctx, env.user, fakeTaskSnapshot(model.KindVideo, 10), map[string]any{"prompt": "x"})
		assertTaskCode(t, err, 0)
		testTask, _ := env.store.GetByIDAny(ctx, tv.ID)

		steps := []provider.TraceStep{{Name: "submit", Kind: "hook", DurationMs: 3}}
		if err := env.svc.SaveTrace(ctx, testTask, steps); err != nil {
			t.Fatal(err)
		}
		got, err := env.svc.GetTestTrace(ctx, env.user, tv.ID)
		if err != nil || len(got) != 1 || got[0].Name != "submit" || got[0].DurationMs != 3 {
			t.Fatalf("追踪读回不对：%+v %v", got, err)
		}
		if _, err := env.svc.GetTestTask(ctx, env.user, tv.ID); err != nil {
			t.Fatalf("GetTestTask 应能查到：%v", err)
		}
		// 试跑任务不出现在普通接口里
		_, err = env.svc.Get(ctx, env.user, tv.ID)
		assertTaskCode(t, err, errcode.ErrTaskNotFound.Code)

		// 正式任务：SaveTrace 被忽略，GetTestTrace 当作不存在
		nv, _ := env.create("")
		normal, _ := env.store.GetByIDAny(ctx, nv.ID)
		if err := env.svc.SaveTrace(ctx, normal, steps); err != nil {
			t.Fatalf("正式任务应静默忽略：%v", err)
		}
		if _, err := env.svc.GetTestTrace(ctx, env.user, nv.ID); err == nil {
			t.Fatal("正式任务不应通过 GetTestTrace 查到")
		}
		// 真实仓储的 SaveTrace 对非试跑任务返回 ErrNotFound
		if err := env.store.SaveTrace(ctx, nv.ID, []byte(`[]`)); !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("仓储应返回 ErrNotFound：%v", err)
		}
	})
}
