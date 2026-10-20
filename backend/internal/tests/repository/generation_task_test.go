package repository_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
	. "video-canvas/internal/repository"
)

// gtTestDB 为本用例创建独立 schema 并迁移任务与积分表；渠道负载这类“全表统计”的 SQL 需要隔离。
func gtTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return isolatedDB(t, &model.GenerationTask{}, &model.GenerationTaskEvent{}, &model.UserCredit{}, &model.CreditLedger{}, &model.AIChannel{})
}

var gtUserSeq atomic.Uint64

func gtNewUserID() uint64 { return uint64(time.Now().UnixNano()/1000)*100 + gtUserSeq.Add(1)%100 }

// gtTask 构造一个可直接插入的任务，nextPoll 决定是否“到期”。
func gtTask(userID uint64, status string, credits int, nextPoll time.Time) *model.GenerationTask {
	return &model.GenerationTask{
		UserID:         userID,
		Kind:           model.KindVideo,
		ModelKey:       "m1",
		Provider:       "p1",
		Status:         status,
		InputJSON:      datatypes.JSON(`{"prompt":"x"}`),
		ConfigSnapshot: datatypes.JSON(`{}`),
		Credits:        credits,
		Version:        1,
		NextPollAt:     nextPoll,
		DeadlineAt:     time.Now().Add(30 * time.Minute),
	}
}

// task_ref 在创建时和任务一起落库：数据库里能直接按界面上展示的编号查到原始数据，并且全局唯一。
func TestGenerationTaskRepo_InsertTaskStoresUniqueTaskRef(t *testing.T) {
	ctx := context.Background()
	db := gtTestDB(t)
	repo := NewGenerationTaskRepository(db)
	user := gtNewUserID()

	task := gtTask(user, model.TaskPending, 1, time.Now())
	inserted, err := repo.InsertTask(ctx, task)
	if err != nil || !inserted {
		t.Fatalf("插入失败：%v inserted=%v", err, inserted)
	}
	if want := idcodec.Encode(task.ID); task.TaskRef != want {
		t.Fatalf("task_ref 应为 id 的编码 %q，实际 %q", want, task.TaskRef)
	}

	var found model.GenerationTask
	if err := db.Where("task_ref = ?", task.TaskRef).First(&found).Error; err != nil {
		t.Fatalf("应能按 task_ref 查到原始数据：%v", err)
	}
	if found.ID != task.ID || found.UserID != user {
		t.Fatalf("查到的不是这个任务：%+v", found)
	}

	dup := gtTask(user, model.TaskPending, 1, time.Now())
	dup.TaskRef = task.TaskRef
	if err := db.Create(dup).Error; err == nil {
		t.Fatal("task_ref 重复应被唯一索引拒绝")
	}

	// 没有 task_ref 的旧行（空串）不受唯一约束影响，可以有多行
	for range 2 {
		legacy := gtTask(user, model.TaskSucceeded, 1, time.Now())
		legacy.TaskRef = ""
		if err := db.Create(legacy).Error; err != nil {
			t.Fatalf("空 task_ref 的旧行不应受唯一约束：%v", err)
		}
	}
}

// gtChannel 建一个渠道，maxRunning 写进 rate_limit_json；0 表示不写（不限）。
func gtChannel(t *testing.T, db *gorm.DB, key string, maxRunning int) {
	t.Helper()
	rl := `{}`
	if maxRunning > 0 {
		rl = fmt.Sprintf(`{"max_running":%d}`, maxRunning)
	}
	ch := &model.AIChannel{
		Key: key, Name: key, BaseURL: "http://x",
		HeadersJSON: model.JSONText(`{}`), RateLimitJSON: model.JSONText(rl),
	}
	if err := db.Create(ch).Error; err != nil {
		t.Fatalf("创建渠道失败：%v", err)
	}
}

// gtChannelTask 在指定渠道下插入一个任务；created 决定先来后到。
func gtChannelTask(t *testing.T, repo *GenerationTaskRepository, channel, status string, created time.Time, mut func(*model.GenerationTask)) *model.GenerationTask {
	t.Helper()
	tk := gtTask(gtNewUserID(), status, 1, created)
	tk.Provider = channel
	tk.CreatedAt = created
	if mut != nil {
		mut(tk)
	}
	if ok, err := repo.InsertTask(context.Background(), tk); err != nil || !ok {
		t.Fatalf("插入任务失败：ok=%v err=%v", ok, err)
	}
	return tk
}

// ChannelLoads 的口径与名额（AcquireSlot）一致：拿到名额的 pending（提交中）、queued、running 算“生成中”，
// 没拿到名额的 pending 算“排队”，管理端看到的“生成中 x/y”就是闸门实际数的那个 x。
func TestAIChannelRepo_ChannelLoads(t *testing.T) {
	ctx := context.Background()
	db := gtTestDB(t)
	tasks := NewGenerationTaskRepository(db)
	channels := NewAIChannelRepository(db)
	now := time.Now()
	held := func(tk *model.GenerationTask) { tk.SlotAcquiredAt = &now }

	gtChannelTask(t, tasks, "a", model.TaskRunning, now, held)
	gtChannelTask(t, tasks, "a", model.TaskQueued, now, held)
	gtChannelTask(t, tasks, "a", model.TaskPending, now, held)    // 拿到名额、正在提交：算生成中
	gtChannelTask(t, tasks, "a", model.TaskPending, now, nil)     // 排队
	gtChannelTask(t, tasks, "a", model.TaskPending, now, nil)     // 排队
	gtChannelTask(t, tasks, "a", model.TaskFinalizing, now, held) // 只剩转存：不算
	gtChannelTask(t, tasks, "a", model.TaskSucceeded, now, held)  // 终态：不算
	gtChannelTask(t, tasks, "b", model.TaskPending, now, nil)

	got, err := channels.ChannelLoads(ctx)
	if err != nil {
		t.Fatalf("ChannelLoads 失败：%v", err)
	}
	want := []model.ChannelLoad{{Channel: "a", Running: 3, Waiting: 2}, {Channel: "b", Running: 0, Waiting: 1}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("负载不对：got=%+v want=%+v", got, want)
	}
}

func TestGenerationTaskRepo_UpdateIf(t *testing.T) {
	ctx := context.Background()
	db := gtTestDB(t)
	repo := NewGenerationTaskRepository(db)
	user := gtNewUserID()

	tk := gtTask(user, model.TaskPending, 5, time.Now())
	if _, err := repo.InsertTask(ctx, tk); err != nil {
		t.Fatal(err)
	}

	t.Run("状态匹配时更新并返回新行，version+1", func(t *testing.T) {
		got, err := repo.UpdateIf(ctx, tk.ID, []string{model.TaskPending}, map[string]any{
			"status": model.TaskQueued, "provider_task_id": "p-1", "lease_until": nil,
		}, true)
		if err != nil {
			t.Fatalf("UpdateIf 失败：%v", err)
		}
		if got.Status != model.TaskQueued || got.ProviderTaskID != "p-1" || got.Version != 2 {
			t.Fatalf("返回的行不对：%+v", got)
		}
	})

	t.Run("不 bump 时 version 不变", func(t *testing.T) {
		got, err := repo.UpdateIf(ctx, tk.ID, []string{model.TaskQueued}, map[string]any{"poll_attempts": 3}, false)
		if err != nil || got.Version != 2 || got.PollAttempts != 3 {
			t.Fatalf("got=%+v err=%v", got, err)
		}
	})

	t.Run("状态不匹配返回 ErrStateConflict 且不改数据", func(t *testing.T) {
		_, err := repo.UpdateIf(ctx, tk.ID, []string{model.TaskPending}, map[string]any{"status": model.TaskFailed}, true)
		if !errors.Is(err, ErrStateConflict) {
			t.Fatalf("期望 ErrStateConflict，实际 %v", err)
		}
		cur, _ := repo.GetByIDAny(ctx, tk.ID)
		if cur.Status != model.TaskQueued {
			t.Fatalf("状态被误改为 %s", cur.Status)
		}
	})

	t.Run("并发迁移只有一个成功", func(t *testing.T) {
		c := gtTask(user, model.TaskRunning, 1, time.Now())
		if _, err := repo.InsertTask(ctx, c); err != nil {
			t.Fatal(err)
		}
		var ok, conflict atomic.Int32
		var wg sync.WaitGroup
		for i := 0; i < 12; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := repo.UpdateIf(ctx, c.ID, model.ActiveTaskStatuses, map[string]any{"status": model.TaskCanceled}, true)
				switch {
				case err == nil:
					ok.Add(1)
				case errors.Is(err, ErrStateConflict):
					conflict.Add(1)
				default:
					t.Errorf("意外错误：%v", err)
				}
			}()
		}
		wg.Wait()
		if ok.Load() != 1 || conflict.Load() != 11 {
			t.Fatalf("期望 1 成功 11 冲突，实际 %d / %d", ok.Load(), conflict.Load())
		}
		cur, _ := repo.GetByIDAny(ctx, c.ID)
		if cur.Version != 2 {
			t.Fatalf("version 应只 +1，实际 %d", cur.Version)
		}
	})
}

func TestGenerationTaskRepo_Idempotency(t *testing.T) {
	ctx := context.Background()
	db := gtTestDB(t)
	repo := NewGenerationTaskRepository(db)
	user := gtNewUserID()

	first := gtTask(user, model.TaskPending, 1, time.Now())
	first.IdempotencyKey = "k1"
	if ok, err := repo.InsertTask(ctx, first); err != nil || !ok {
		t.Fatalf("首次插入应成功：ok=%v err=%v", ok, err)
	}

	t.Run("同 key 再次插入返回 inserted=false 且不报错", func(t *testing.T) {
		dup := gtTask(user, model.TaskPending, 1, time.Now())
		dup.IdempotencyKey = "k1"
		ok, err := repo.InsertTask(ctx, dup)
		if err != nil || ok {
			t.Fatalf("期望 inserted=false：ok=%v err=%v", ok, err)
		}
		got, err := repo.FindByIdempotencyKey(ctx, user, "k1")
		if err != nil || got.ID != first.ID {
			t.Fatalf("应查到第一个任务：%+v %v", got, err)
		}
	})

	t.Run("冲突不会污染事务，之后的语句仍可执行", func(t *testing.T) {
		err := repo.WithTx(ctx, func(tx GenerationTaskTx) error {
			dup := gtTask(user, model.TaskPending, 1, time.Now())
			dup.IdempotencyKey = "k1"
			if ok, err := tx.InsertTask(ctx, dup); err != nil || ok {
				return fmt.Errorf("期望冲突：ok=%v err=%w", ok, err)
			}
			_, err := tx.CountActive(ctx, user)
			return err
		})
		if err != nil {
			t.Fatalf("事务应正常完成：%v", err)
		}
	})

	t.Run("不同用户可以用相同 key，空 key 不受唯一约束", func(t *testing.T) {
		other := gtTask(gtNewUserID(), model.TaskPending, 1, time.Now())
		other.IdempotencyKey = "k1"
		if ok, err := repo.InsertTask(ctx, other); err != nil || !ok {
			t.Fatalf("其他用户应可插入：ok=%v err=%v", ok, err)
		}
		for i := 0; i < 2; i++ {
			if ok, err := repo.InsertTask(ctx, gtTask(user, model.TaskPending, 1, time.Now())); err != nil || !ok {
				t.Fatalf("空 key 应可重复插入：ok=%v err=%v", ok, err)
			}
		}
	})

	t.Run("并发提交同一个 key 只有一个插入成功", func(t *testing.T) {
		var ok atomic.Int32
		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				tk := gtTask(user, model.TaskPending, 1, time.Now())
				tk.IdempotencyKey = "race"
				inserted, err := repo.InsertTask(ctx, tk)
				if err != nil {
					t.Errorf("意外错误：%v", err)
				}
				if inserted {
					ok.Add(1)
				}
			}()
		}
		wg.Wait()
		if ok.Load() != 1 {
			t.Fatalf("期望只有 1 个插入成功，实际 %d", ok.Load())
		}
	})
}

func TestGenerationTaskRepo_Credit(t *testing.T) {
	ctx := context.Background()
	db := gtTestDB(t)
	repo := NewGenerationTaskRepository(db)

	t.Run("EnsureCredit 惰性创建且不覆盖已有账户", func(t *testing.T) {
		u := gtNewUserID()
		if _, err := repo.GetCredit(ctx, u); !errors.Is(err, ErrNotFound) {
			t.Fatalf("账户应不存在：%v", err)
		}
		if err := repo.EnsureCredit(ctx, u, 50); err != nil {
			t.Fatal(err)
		}
		if err := repo.AddCredit(ctx, u, 0, 10); err != nil {
			t.Fatal(err)
		}
		if err := repo.EnsureCredit(ctx, u, 999); err != nil {
			t.Fatal(err)
		}
		acc, _ := repo.GetCredit(ctx, u)
		if acc.Balance != 50 || acc.Frozen != 10 {
			t.Fatalf("已有账户不应被覆盖：%+v", acc)
		}
	})

	t.Run("AddCredit 账户不存在返回 ErrNotFound", func(t *testing.T) {
		if err := repo.AddCredit(ctx, gtNewUserID(), 1, 1); !errors.Is(err, ErrNotFound) {
			t.Fatalf("期望 ErrNotFound，实际 %v", err)
		}
	})

	t.Run("InsertLedger 同 (task,type) 第二次 inserted=false", func(t *testing.T) {
		u := gtNewUserID()
		entry := func() *model.CreditLedger {
			return &model.CreditLedger{UserID: u, TaskID: model.TaskIDPtr(777), Type: model.LedgerSettle, Amount: 3}
		}
		if ok, err := repo.InsertLedger(ctx, entry()); err != nil || !ok {
			t.Fatalf("首次应插入：ok=%v err=%v", ok, err)
		}
		if ok, err := repo.InsertLedger(ctx, entry()); err != nil || ok {
			t.Fatalf("第二次应 inserted=false：ok=%v err=%v", ok, err)
		}
		refund := entry()
		refund.Type = model.LedgerRefund
		if ok, err := repo.InsertLedger(ctx, refund); err != nil || !ok {
			t.Fatalf("不同 type 应可插入：ok=%v err=%v", ok, err)
		}
	})

	t.Run("LockCredit 会阻塞其它事务直到提交", func(t *testing.T) {
		u := gtNewUserID()
		if err := repo.EnsureCredit(ctx, u, 10); err != nil {
			t.Fatal(err)
		}
		locked := make(chan struct{})
		release := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = repo.WithTx(ctx, func(tx GenerationTaskTx) error {
				if _, err := tx.LockCredit(ctx, u); err != nil {
					t.Errorf("加锁失败：%v", err)
				}
				close(locked)
				<-release
				return tx.AddCredit(ctx, u, 0, 1)
			})
		}()
		<-locked

		start := time.Now()
		got := make(chan int, 1)
		go func() {
			var frozen int
			_ = repo.WithTx(ctx, func(tx GenerationTaskTx) error {
				acc, err := tx.LockCredit(ctx, u)
				if err == nil {
					frozen = acc.Frozen
				}
				return err
			})
			got <- frozen
		}()
		select {
		case <-got:
			t.Fatal("第一个事务持锁期间，第二个 LockCredit 不应返回")
		case <-time.After(300 * time.Millisecond):
		}
		close(release)
		wg.Wait()
		select {
		case frozen := <-got:
			if frozen != 1 {
				t.Fatalf("第二个事务应读到第一个事务提交后的值 1，实际 %d", frozen)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("第一个事务提交后第二个 LockCredit 应返回")
		}
		_ = start
	})

	t.Run("事务内出错整体回滚", func(t *testing.T) {
		u := gtNewUserID()
		_ = repo.EnsureCredit(ctx, u, 10)
		boom := errors.New("boom")
		err := repo.WithTx(ctx, func(tx GenerationTaskTx) error {
			if err := tx.AddCredit(ctx, u, -5, 0); err != nil {
				return err
			}
			if _, err := tx.InsertLedger(ctx, &model.CreditLedger{UserID: u, TaskID: model.TaskIDPtr(1), Type: model.LedgerSettle, Amount: 5}); err != nil {
				return err
			}
			return boom
		})
		if !errors.Is(err, boom) {
			t.Fatalf("应透传回调错误：%v", err)
		}
		acc, _ := repo.GetCredit(ctx, u)
		if acc.Balance != 10 {
			t.Fatalf("余额应回滚，实际 %d", acc.Balance)
		}
		rec, err := repo.Reconcile(ctx, u)
		if err != nil || rec.SettleSum != 0 {
			t.Fatalf("流水应回滚：%+v %v", rec, err)
		}
	})
}

func TestGenerationTaskRepo_Queries(t *testing.T) {
	ctx := context.Background()
	db := gtTestDB(t)
	repo := NewGenerationTaskRepository(db)
	user, other := gtNewUserID(), gtNewUserID()

	mine := gtTask(user, model.TaskRunning, 1, time.Now())
	done := gtTask(user, model.TaskSucceeded, 1, time.Now())
	test := gtTask(user, model.TaskRunning, 0, time.Now())
	test.IsTest = true
	theirs := gtTask(other, model.TaskRunning, 1, time.Now())
	for _, tk := range []*model.GenerationTask{mine, done, test, theirs} {
		if _, err := repo.InsertTask(ctx, tk); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("GetByID 带 user_id 条件", func(t *testing.T) {
		if _, err := repo.GetByID(ctx, user, theirs.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("别人的任务应返回 ErrNotFound：%v", err)
		}
		if got, err := repo.GetByID(ctx, user, mine.ID); err != nil || got.ID != mine.ID {
			t.Fatalf("got=%v err=%v", got, err)
		}
	})
	t.Run("ListByIDs 不返回别人的任务和 is_test 任务", func(t *testing.T) {
		got, err := repo.ListByIDs(ctx, user, []uint64{mine.ID, done.ID, test.ID, theirs.ID, 999999999})
		if err != nil || len(got) != 2 {
			t.Fatalf("应返回 2 个：n=%d err=%v", len(got), err)
		}
	})
	t.Run("ListActive 只返回非终态的正式任务", func(t *testing.T) {
		got, err := repo.ListActive(ctx, user)
		if err != nil || len(got) != 1 || got[0].ID != mine.ID {
			t.Fatalf("got=%v err=%v", got, err)
		}
	})
	t.Run("CountAllActive 统计全站非终态正式任务（升级前提示会中断几个）", func(t *testing.T) {
		n, err := repo.CountAllActive(ctx)
		if err != nil || n != 2 {
			t.Fatalf("应为 mine + theirs 共 2 个：n=%d err=%v", n, err)
		}
	})
	t.Run("CountActive 不统计 is_test", func(t *testing.T) {
		n, err := repo.CountActive(ctx, user)
		if err != nil || n != 1 {
			t.Fatalf("n=%d err=%v", n, err)
		}
	})
	t.Run("TouchByProviderTask 只影响非终态任务", func(t *testing.T) {
		_, _ = repo.UpdateIf(ctx, mine.ID, model.ActiveTaskStatuses, map[string]any{"provider_task_id": "pt-1"}, false)
		_, _ = repo.UpdateIf(ctx, done.ID, []string{model.TaskSucceeded}, map[string]any{"provider_task_id": "pt-1"}, false)
		now := time.Now().Add(-time.Hour)
		n, err := repo.TouchByProviderTask(ctx, "p1", "pt-1", now)
		if err != nil || n != 1 {
			t.Fatalf("应只命中 1 个：n=%d err=%v", n, err)
		}
		if n, _ := repo.TouchByProviderTask(ctx, "p1", "no-such", now); n != 0 {
			t.Fatalf("不存在的平台任务应命中 0 个，实际 %d", n)
		}
	})
}

func TestGenerationTaskRepo_Reconcile(t *testing.T) {
	ctx := context.Background()
	db := gtTestDB(t)
	repo := NewGenerationTaskRepository(db)
	u := gtNewUserID()
	if err := repo.EnsureCredit(ctx, u, 50); err != nil {
		t.Fatal(err)
	}
	// 三个任务：一个进行中（冻结 10）、一个已结算（4）、一个已退款（6）
	active := gtTask(u, model.TaskRunning, 10, time.Now())
	settled := gtTask(u, model.TaskSucceeded, 4, time.Now())
	refunded := gtTask(u, model.TaskFailed, 6, time.Now())
	for _, tk := range []*model.GenerationTask{active, settled, refunded} {
		if _, err := repo.InsertTask(ctx, tk); err != nil {
			t.Fatal(err)
		}
		_, _ = repo.InsertLedger(ctx, &model.CreditLedger{UserID: u, TaskID: model.TaskIDPtr(tk.ID), Type: model.LedgerFreeze, Amount: tk.Credits})
	}
	_, _ = repo.InsertLedger(ctx, &model.CreditLedger{UserID: u, TaskID: model.TaskIDPtr(settled.ID), Type: model.LedgerSettle, Amount: 4})
	_, _ = repo.InsertLedger(ctx, &model.CreditLedger{UserID: u, TaskID: model.TaskIDPtr(refunded.ID), Type: model.LedgerRefund, Amount: 6})
	// 账户：冻结 10（active），余额 50 - 4
	_ = repo.AddCredit(ctx, u, -4, 10)

	rec, err := repo.Reconcile(ctx, u)
	if err != nil {
		t.Fatalf("Reconcile 失败：%v", err)
	}
	if rec.FrozenDiff() != 0 || rec.ActiveDiff() != 0 || rec.BalanceDiff() != 0 {
		t.Fatalf("对账应零差异：%+v", rec)
	}

	// 管理员调整（+20）与 initial 一样进流水：余额 = Σinitial + Σadmin_adjust - Σsettle，调整后对账仍为 0
	_, _ = repo.InsertLedger(ctx, &model.CreditLedger{UserID: u, Type: model.LedgerAdminAdjust, Amount: 20, OperatorID: model.TaskIDPtr(1), Note: "补偿"})
	_ = repo.AddCredit(ctx, u, 20, 0)
	_, _ = repo.InsertLedger(ctx, &model.CreditLedger{UserID: u, Type: model.LedgerAdminAdjust, Amount: -5, OperatorID: model.TaskIDPtr(1), Note: "扣减"})
	_ = repo.AddCredit(ctx, u, -5, 0)
	rec, err = repo.Reconcile(ctx, u)
	if err != nil {
		t.Fatal(err)
	}
	if rec.InitialSum != 50 || rec.AdminAdjustSum != 15 || rec.BalanceDiff() != 0 {
		t.Fatalf("管理员调整后对账应仍为 0：%+v diff=%d", rec, rec.BalanceDiff())
	}

	// 人为制造差异，确认对账能发现
	_ = repo.AddCredit(ctx, u, 0, 1)
	rec, _ = repo.Reconcile(ctx, u)
	if rec.FrozenDiff() != 1 || rec.ActiveDiff() != 1 {
		t.Fatalf("应发现 1 的差异：%+v", rec)
	}

	if _, err := repo.Reconcile(ctx, gtNewUserID()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("账户不存在应返回 ErrNotFound：%v", err)
	}
}
