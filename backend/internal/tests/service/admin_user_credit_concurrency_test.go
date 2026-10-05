package service_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
	. "video-canvas/internal/service"
)

// 本文件用真实 PostgreSQL 验证积分调整与任务冻结 / 结算并发时的正确性（行锁 + 事务）。需要 TEST_DATABASE_DSN。

type opsIntegration struct {
	*gtiEnv
	admin  *AdminUserService
	userID uint64
	opID   uint64
}

// slowLockStore 在“读到积分账户之后”人为停顿一小会儿，模拟真实环境里网络与负载造成的延迟，把“读余额 → 改余额”的竞态窗口撑大。
// 有行锁时别的事务会卡在 LockCredit 上排队，不受影响；没有行锁时一堆事务都会读到同一个过期余额，竞态必现，测试才有变异检测力。
type slowLockStore struct {
	*repository.GenerationTaskRepository
	delay time.Duration
}

func (s slowLockStore) WithTx(ctx context.Context, fn func(tx repository.GenerationTaskTx) error) error {
	return s.GenerationTaskRepository.WithTx(ctx, func(tx repository.GenerationTaskTx) error {
		return fn(slowLockTx{GenerationTaskTx: tx, delay: s.delay})
	})
}

type slowLockTx struct {
	repository.GenerationTaskTx
	delay time.Duration
}

func (s slowLockTx) LockCredit(ctx context.Context, userID uint64) (*model.UserCredit, error) {
	acc, err := s.GenerationTaskTx.LockCredit(ctx, userID)
	time.Sleep(s.delay)
	return acc, err
}

func newOpsIntegration(t *testing.T, initial int, delay time.Duration) *opsIntegration {
	t.Helper()
	env := newGTIEnv(t, &fakeLimits{initial: initial, defMax: 200})
	if err := env.db.AutoMigrate(&model.User{}, &model.AdminAuditLog{}); err != nil {
		t.Fatal(err)
	}
	target := &model.User{BaseModel: model.BaseModel{ID: env.user}, Username: fmt.Sprintf("u%d", env.user), Password: "x", Role: model.RoleUser, Status: model.UserStatusActive}
	op := &model.User{BaseModel: model.BaseModel{ID: env.user + 1}, Username: fmt.Sprintf("op%d", env.user), Password: "x", Role: model.RoleAdmin, Status: model.UserStatusActive}
	if err := env.db.Create(target).Error; err != nil {
		t.Fatal(err)
	}
	if err := env.db.Create(op).Error; err != nil {
		t.Fatal(err)
	}
	if err := env.store.EnsureCredit(context.Background(), env.user, initial); err != nil {
		t.Fatal(err)
	}
	// 预热连接池：连接是按需新建的，冷启动时 goroutine 会被“建连接”自然错开，并发窗口被无意间串行化，测试就测不出竞态
	runAll(30, func(int) { env.db.Exec("SELECT pg_sleep(0.05)") })
	settings := NewSettingsService(newFakeSettingsRepo(), nil)
	admin := NewAdminUserService(repository.NewUserRepository(env.db), repository.NewAdminAuditRepository(env.db), settings,
		WithAdminControls(AdminControls{Credits: slowLockStore{GenerationTaskRepository: env.store, delay: delay}}))
	return &opsIntegration{gtiEnv: env, admin: admin, userID: env.user, opID: op.ID}
}

func (e *opsIntegration) adjust(mode string, amount int) (*model.AdminCreditView, error) {
	return e.admin.AdjustCredits(context.Background(), e.opID, e.userID, &model.AdjustCreditsReq{Mode: mode, Amount: &amount, Note: "并发测试"})
}

// runAll 同时放行 n 个 goroutine 执行 fn，等待全部结束。
func runAll(n int, fn func(i int)) {
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			fn(i)
		}()
	}
	close(start)
	wg.Wait()
}

func TestAdminUser_CreditAdjust_Integration(t *testing.T) {
	ctx := context.Background()

	t.Run("并发扣减同一笔可用积分：恰好一个成功，其余被拒，余额不会变负", func(t *testing.T) {
		e := newOpsIntegration(t, 100, 5*time.Millisecond)
		var mu sync.Mutex
		ok, rejected := 0, 0
		runAll(30, func(int) {
			_, err := e.adjust(model.CreditModeSub, 100)
			mu.Lock()
			defer mu.Unlock()
			var ec *errcode.Error
			switch {
			case err == nil:
				ok++
			case errors.As(err, &ec) && ec.Code == errcode.ErrCreditAdjustInvalid.Code:
				rejected++
			default:
				t.Errorf("意外错误：%v", err)
			}
		})
		acc, _ := e.store.GetCredit(ctx, e.userID)
		if ok != 1 || rejected != 29 || acc.Balance != 0 {
			t.Fatalf("成功 %d 拒绝 %d 余额 %d：行锁应让 30 个扣减串行，只有第一个能扣", ok, rejected, acc.Balance)
		}
		rec, _ := e.store.Reconcile(ctx, e.userID)
		if rec.BalanceDiff() != 0 || rec.AdminAdjustSum != -100 {
			t.Fatalf("对账：%+v", rec)
		}
	})

	t.Run("并发 set：最终可用积分恰好是某一次 set 的目标值，流水逐笔对得上账", func(t *testing.T) {
		e := newOpsIntegration(t, 500, 5*time.Millisecond)
		const n = 25
		runAll(n, func(i int) {
			if _, err := e.adjust(model.CreditModeSet, 1000+i); err != nil {
				t.Errorf("set 失败：%v", err)
			}
		})
		acc, _ := e.store.GetCredit(ctx, e.userID)
		avail := acc.Balance - acc.Frozen
		if avail < 1000 || avail >= 1000+n {
			t.Fatalf("最终可用 %d 不是任何一次 set 的目标值：delta 基于过期余额算出，说明有并发丢失更新", avail)
		}
		var cnt int64
		e.db.Model(&model.CreditLedger{}).Where("user_id = ? AND type = ?", e.userID, model.LedgerAdminAdjust).Count(&cnt)
		rec, _ := e.store.Reconcile(ctx, e.userID)
		if cnt != n || rec.BalanceDiff() != 0 {
			t.Fatalf("流水 %d 条（应 %d），对账 %+v", cnt, n, rec)
		}
	})

	t.Run("调整与任务冻结 / 结算 / 取消同时进行：不丢更新，余额、冻结、流水、进行中任务四方对得上", func(t *testing.T) {
		e := newOpsIntegration(t, 1000, 0)
		const tasks, adds, subs = 30, 30, 10

		// 阶段一：并发提交任务（每个冻结 10）+ 并发加 3 / 减 1
		var mu sync.Mutex
		var created []uint64
		runAll(tasks+adds+subs, func(i int) {
			switch {
			case i < tasks:
				v, err := e.create(fmt.Sprintf("adj-%d", i))
				if err != nil {
					t.Errorf("提交任务失败：%v", err)
					return
				}
				mu.Lock()
				created = append(created, v.ID)
				mu.Unlock()
			case i < tasks+adds:
				if _, err := e.adjust(model.CreditModeAdd, 3); err != nil {
					t.Errorf("add 失败：%v", err)
				}
			default:
				if _, err := e.adjust(model.CreditModeSub, 1); err != nil {
					t.Errorf("sub 失败：%v", err)
				}
			}
		})
		if len(created) != tasks {
			t.Fatalf("应创建 %d 个任务：%d", tasks, len(created))
		}
		e.assertReconciled(t)

		// 阶段二：一半任务结算（成功扣费）、一半取消（退还冻结），同时继续并发调整
		completed := 0
		runAll(tasks+adds, func(i int) {
			if i >= tasks {
				if _, err := e.adjust(model.CreditModeAdd, 2); err != nil {
					t.Errorf("add 失败：%v", err)
				}
				return
			}
			id := created[i]
			if i%2 == 0 {
				task, err := e.store.GetByIDAny(ctx, id)
				if err != nil {
					t.Errorf("%v", err)
					return
				}
				// pending -> queued（上游受理）-> finalizing（出结果）-> 成功结算
				if ok, err := e.svc.MarkSubmitted(ctx, task, fmt.Sprintf("pt-%d", id), nil, time.Now()); err != nil || !ok {
					t.Errorf("MarkSubmitted：%v %v", ok, err)
					return
				}
				if ok, err := e.svc.MarkFinalizing(ctx, task); err != nil || !ok {
					t.Errorf("MarkFinalizing：%v %v", ok, err)
					return
				}
				task, _ = e.store.GetByIDAny(ctx, id)
				if ok, err := e.svc.Complete(ctx, task, nil, nil); err != nil || !ok {
					t.Errorf("Complete：%v %v", ok, err)
					return
				}
				mu.Lock()
				completed++
				mu.Unlock()
				return
			}
			if _, err := e.svc.Cancel(ctx, e.userID, id); err != nil {
				t.Errorf("Cancel：%v", err)
			}
		})

		rec := e.assertReconciled(t)
		wantBalance := 1000 + adds*3 - subs + adds*2 - completed*10
		if rec.Balance != wantBalance || rec.Frozen != 0 {
			t.Fatalf("余额 %d（应 %d）冻结 %d：并发下调整被丢失或重复", rec.Balance, wantBalance, rec.Frozen)
		}
	})

	t.Run("扣减与并发提交任务（冻结）同时进行：可用积分不会被扣成负数", func(t *testing.T) {
		e := newOpsIntegration(t, 100, 40*time.Millisecond)
		var mu sync.Mutex
		subOK := false
		runAll(16, func(i int) {
			if i == 0 {
				_, err := e.adjust(model.CreditModeSub, 100)
				mu.Lock()
				subOK = err == nil
				mu.Unlock()
				return
			}
			_, _ = e.create(fmt.Sprintf("race-%d", i)) // 每个冻结 10，可用不够时会被拒，这里不关心成败
		})
		acc, _ := e.store.GetCredit(ctx, e.userID)
		if acc.Balance-acc.Frozen < 0 {
			t.Fatalf("可用积分为负（余额 %d 冻结 %d，sub 成功=%v）：扣减基于过期的可用积分，与冻结没有互斥", acc.Balance, acc.Frozen, subOK)
		}
		e.assertReconciled(t)
	})
}

func TestGenerationTaskService_CancelActiveByUser_Integration(t *testing.T) {
	ctx := context.Background()
	e := newOpsIntegration(t, 100, 0)
	for i := range 3 {
		if _, err := e.create(fmt.Sprintf("ban-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	// 管理员调整与取消并发：取消退还冻结、调整改余额，两者互不丢失
	var res *CancelActiveResult
	runAll(2, func(i int) {
		if i == 0 {
			r, err := e.svc.CancelActiveByUser(ctx, e.userID)
			if err != nil {
				t.Errorf("%v", err)
			}
			res = r
			return
		}
		if _, err := e.adjust(model.CreditModeAdd, 7); err != nil {
			t.Errorf("%v", err)
		}
	})
	if res == nil || res.Canceled != 3 || len(res.Failed) != 0 {
		t.Fatalf("%+v", res)
	}
	rec := e.assertReconciled(t)
	if rec.Frozen != 0 || rec.Balance != 107 {
		t.Fatalf("冻结应全部退还、余额 = 100 + 7：%+v", rec)
	}
	if n, _ := e.store.CountActive(ctx, e.userID); n != 0 {
		t.Fatalf("不应再有进行中任务：%d", n)
	}
}
