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
	. "video-canvas/internal/repository"
)

// gtTestDB 为本用例创建独立 schema 并迁移任务与积分表；ClaimDue 这类“全表扫描”的 SQL 需要隔离。
func gtTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return isolatedDB(t, &model.GenerationTask{}, &model.UserCredit{}, &model.CreditLedger{}, &model.AIChannel{})
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

func TestGenerationTaskRepo_ClaimDue(t *testing.T) {
	ctx := context.Background()
	db := gtTestDB(t)
	repo := NewGenerationTaskRepository(db)
	now := time.Now()
	user := gtNewUserID()

	t.Run("只领取到期、非终态、无有效租约的任务", func(t *testing.T) {
		due := gtTask(user, model.TaskPending, 1, now.Add(-time.Second))
		future := gtTask(user, model.TaskPending, 1, now.Add(time.Hour))
		done := gtTask(user, model.TaskSucceeded, 1, now.Add(-time.Second))
		leased := gtTask(user, model.TaskRunning, 1, now.Add(-time.Second))
		lease := now.Add(time.Minute)
		leased.LeaseUntil = &lease
		expiredLease := gtTask(user, model.TaskRunning, 1, now.Add(-time.Second))
		old := now.Add(-time.Minute)
		expiredLease.LeaseUntil = &old
		for _, tk := range []*model.GenerationTask{due, future, done, leased, expiredLease} {
			if ok, err := repo.InsertTask(ctx, tk); err != nil || !ok {
				t.Fatalf("插入任务失败：ok=%v err=%v", ok, err)
			}
		}

		got, err := repo.ClaimDue(ctx, now, time.Minute, 50)
		if err != nil {
			t.Fatalf("ClaimDue 失败：%v", err)
		}
		ids := map[uint64]bool{}
		for _, g := range got {
			ids[g.ID] = true
			if g.LeaseUntil == nil || g.LeaseUntil.Before(now.Add(50*time.Second)) {
				t.Errorf("任务 %d 的租约没有写成 now+lease：%v", g.ID, g.LeaseUntil)
			}
		}
		if len(got) != 2 || !ids[due.ID] || !ids[expiredLease.ID] {
			t.Fatalf("应只领到 due 和 expiredLease，实际 %v", ids)
		}

		// 已被领取（租约有效）的任务不会被再次领取；租约过期后可被重新领取（服务重启恢复）
		again, _ := repo.ClaimDue(ctx, now, time.Minute, 50)
		if len(again) != 0 {
			t.Fatalf("租约有效期内不应重复领取，实际 %d 个", len(again))
		}
		later, _ := repo.ClaimDue(ctx, now.Add(2*time.Minute), time.Minute, 50)
		// 此时 leased（原租约 now+1m）的租约也已过期，共 3 个
		if len(later) != 3 {
			t.Fatalf("租约过期后应能重新领取 3 个，实际 %d 个", len(later))
		}
	})

	t.Run("limit 限制领取数量并按 next_poll_at 排序", func(t *testing.T) {
		u := gtNewUserID()
		base := now.Add(-time.Hour)
		var idsInOrder []uint64
		for i := 0; i < 5; i++ {
			tk := gtTask(u, model.TaskQueued, 1, base.Add(time.Duration(i)*time.Second))
			if _, err := repo.InsertTask(ctx, tk); err != nil {
				t.Fatal(err)
			}
			idsInOrder = append(idsInOrder, tk.ID)
		}
		// 前一个子用例遗留的任务租约已过期时也会被领到，这里把它们置为终态排除
		_ = db.Model(&model.GenerationTask{}).Where("user_id = ?", user).Update("status", model.TaskFailed).Error
		got, err := repo.ClaimDue(ctx, now, time.Minute, 3)
		if err != nil || len(got) != 3 {
			t.Fatalf("应领取 3 个：n=%d err=%v", len(got), err)
		}
		want := map[uint64]bool{idsInOrder[0]: true, idsInOrder[1]: true, idsInOrder[2]: true}
		for _, g := range got {
			if !want[g.ID] {
				t.Errorf("领到了不该先领的任务 %d", g.ID)
			}
		}
	})
}

// gtChannel 建一个渠道，maxRunning 写进 rate_limit_json；0 表示不写（不限）。
func gtChannel(t *testing.T, db *gorm.DB, key string, maxRunning int) {
	t.Helper()
	rl := `{}`
	if maxRunning > 0 {
		rl = fmt.Sprintf(`{"max_running":%d}`, maxRunning)
	}
	ch := &model.AIChannel{
		Key: key, Name: key, PluginKey: "p", PluginVersionID: 1, BaseURL: "http://x",
		SettingsJSON: model.JSONText(`{}`), RateLimitJSON: model.JSONText(rl),
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

func claimedIDs(got []model.GenerationTask) map[uint64]bool {
	ids := map[uint64]bool{}
	for _, g := range got {
		ids[g.ID] = true
	}
	return ids
}

// 渠道 max_running：pending 只能领到“上限 - 占用”那么多，先到先得；已在上游的任务不受限制。
func TestGenerationTaskRepo_ClaimDue_ChannelMaxRunning(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	t0 := now.Add(-time.Hour)

	t.Run("名额不足时只放行最早创建的 pending，且运行中的任务照常领取", func(t *testing.T) {
		db := gtTestDB(t)
		repo := NewGenerationTaskRepository(db)
		gtChannel(t, db, "ch", 2)
		running := gtChannelTask(t, repo, "ch", model.TaskRunning, t0, nil) // 占 1 个名额
		p1 := gtChannelTask(t, repo, "ch", model.TaskPending, t0.Add(1*time.Second), nil)
		p2 := gtChannelTask(t, repo, "ch", model.TaskPending, t0.Add(2*time.Second), nil)
		p3 := gtChannelTask(t, repo, "ch", model.TaskPending, t0.Add(3*time.Second), nil)

		got, err := repo.ClaimDue(ctx, now, time.Minute, 50)
		if err != nil {
			t.Fatalf("ClaimDue 失败：%v", err)
		}
		ids := claimedIDs(got)
		if len(got) != 2 || !ids[running.ID] || !ids[p1.ID] || ids[p2.ID] || ids[p3.ID] {
			t.Fatalf("应只领到 running 和最早的 p1，实际 %v（p1=%d p2=%d p3=%d）", ids, p1.ID, p2.ID, p3.ID)
		}
	})

	t.Run("正在提交的 pending（租约有效）占名额，退避中的不占", func(t *testing.T) {
		db := gtTestDB(t)
		repo := NewGenerationTaskRepository(db)
		gtChannel(t, db, "ch", 1)
		lease := now.Add(time.Minute)
		submitting := gtChannelTask(t, repo, "ch", model.TaskPending, t0, func(tk *model.GenerationTask) { tk.LeaseUntil = &lease })
		waiting := gtChannelTask(t, repo, "ch", model.TaskPending, t0.Add(time.Second), nil)

		if got, _ := repo.ClaimDue(ctx, now, time.Minute, 50); len(got) != 0 {
			t.Fatalf("名额被提交中的任务占满，不应领到任何 pending，实际 %v", claimedIDs(got))
		}
		// 租约过期（提交的 worker 挂了）后不再占用：名额空出来，按先到先得重新领出最早的那个
		got, _ := repo.ClaimDue(ctx, now.Add(2*time.Minute), time.Minute, 50)
		ids := claimedIDs(got)
		if len(got) != 1 || !ids[submitting.ID] || ids[waiting.ID] {
			t.Fatalf("租约过期后应只领到最早的 submitting，实际 %v", ids)
		}
	})

	t.Run("finalizing 不占名额，渠道之间互不影响，0 或没有渠道记录表示不限", func(t *testing.T) {
		db := gtTestDB(t)
		repo := NewGenerationTaskRepository(db)
		gtChannel(t, db, "limited", 1)
		gtChannel(t, db, "free", 0)
		gtChannelTask(t, repo, "limited", model.TaskFinalizing, t0, nil)
		a := gtChannelTask(t, repo, "limited", model.TaskPending, t0.Add(time.Second), nil)
		b := gtChannelTask(t, repo, "limited", model.TaskPending, t0.Add(2*time.Second), nil)
		f1 := gtChannelTask(t, repo, "free", model.TaskPending, t0, nil)
		f2 := gtChannelTask(t, repo, "free", model.TaskPending, t0, nil)
		n1 := gtChannelTask(t, repo, "no-record", model.TaskPending, t0, nil)

		got, _ := repo.ClaimDue(ctx, now, time.Minute, 50)
		ids := claimedIDs(got)
		if !ids[a.ID] || ids[b.ID] || !ids[f1.ID] || !ids[f2.ID] || !ids[n1.ID] {
			t.Fatalf("limited 只放行 a；free 与无记录的渠道不限：%v", ids)
		}
	})

	t.Run("已超过截止时间的 pending 绕过上限被领出（用来置为超时），且不占名额", func(t *testing.T) {
		db := gtTestDB(t)
		repo := NewGenerationTaskRepository(db)
		gtChannel(t, db, "ch", 1)
		gtChannelTask(t, repo, "ch", model.TaskRunning, t0, nil) // 名额已满
		stale := gtChannelTask(t, repo, "ch", model.TaskPending, t0.Add(time.Second), func(tk *model.GenerationTask) {
			tk.DeadlineAt = now.Add(-time.Minute)
		})
		fresh := gtChannelTask(t, repo, "ch", model.TaskPending, t0.Add(2*time.Second), nil)

		got, _ := repo.ClaimDue(ctx, now, time.Minute, 50)
		ids := claimedIDs(got)
		if !ids[stale.ID] || ids[fresh.ID] {
			t.Fatalf("应领到超时的 stale、不领 fresh：%v", ids)
		}
	})

	t.Run("上限取渠道当前配置：调大后排队中的任务立即可领", func(t *testing.T) {
		db := gtTestDB(t)
		repo := NewGenerationTaskRepository(db)
		gtChannel(t, db, "ch", 1)
		gtChannelTask(t, repo, "ch", model.TaskRunning, t0, nil)
		p := gtChannelTask(t, repo, "ch", model.TaskPending, t0.Add(time.Second), nil)
		if got, _ := repo.ClaimDue(ctx, now, time.Minute, 50); claimedIDs(got)[p.ID] {
			t.Fatal("名额已满，p 不应被领取")
		}
		if err := db.Model(&model.AIChannel{}).Where("key = ?", "ch").
			Update("rate_limit_json", model.JSONText(`{"max_running":2}`)).Error; err != nil {
			t.Fatal(err)
		}
		if got, _ := repo.ClaimDue(ctx, now, time.Minute, 50); !claimedIDs(got)[p.ID] {
			t.Fatal("调大上限后 p 应能被领取")
		}
	})
}

// 并发领取时渠道的同时生成数不会超过上限（建议锁串行化了“数名额 + 写租约”）。
func TestGenerationTaskRepo_ClaimDue_ChannelMaxRunning_Concurrent(t *testing.T) {
	ctx := context.Background()
	db := gtTestDB(t)
	repo := NewGenerationTaskRepository(db)
	gtChannel(t, db, "ch", 3)
	now := time.Now()
	for i := 0; i < 30; i++ {
		gtChannelTask(t, repo, "ch", model.TaskPending, now.Add(-time.Hour+time.Duration(i)*time.Second), nil)
	}

	var (
		mu      sync.Mutex
		claimed int
		wg      sync.WaitGroup
	)
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := repo.ClaimDue(ctx, now, time.Minute, 10)
			if err != nil {
				t.Errorf("ClaimDue 失败：%v", err)
				return
			}
			mu.Lock()
			claimed += len(got)
			mu.Unlock()
		}()
	}
	wg.Wait()
	if claimed != 3 {
		t.Fatalf("上限 3，8 个并发领取者一共应只领到 3 个，实际 %d", claimed)
	}
}

// ChannelLoads 的口径与 ClaimDue 的占用数一致，管理端看到的“生成中 x/y”就是闸门实际数的那个 x。
func TestAIChannelRepo_ChannelLoads(t *testing.T) {
	ctx := context.Background()
	db := gtTestDB(t)
	tasks := NewGenerationTaskRepository(db)
	channels := NewAIChannelRepository(db)
	now := time.Now()
	lease := now.Add(time.Minute)

	gtChannelTask(t, tasks, "a", model.TaskRunning, now, nil)
	gtChannelTask(t, tasks, "a", model.TaskQueued, now, nil)
	gtChannelTask(t, tasks, "a", model.TaskPending, now, func(tk *model.GenerationTask) { tk.LeaseUntil = &lease }) // 正在提交：算生成中
	gtChannelTask(t, tasks, "a", model.TaskPending, now, nil)                                                       // 排队
	gtChannelTask(t, tasks, "a", model.TaskPending, now, nil)                                                       // 排队
	gtChannelTask(t, tasks, "a", model.TaskFinalizing, now, nil)                                                    // 只剩转存：不算
	gtChannelTask(t, tasks, "a", model.TaskSucceeded, now, nil)                                                     // 终态：不算
	gtChannelTask(t, tasks, "b", model.TaskPending, now, nil)

	got, err := channels.ChannelLoads(ctx, now)
	if err != nil {
		t.Fatalf("ChannelLoads 失败：%v", err)
	}
	want := []model.ChannelLoad{{Channel: "a", Running: 3, Waiting: 2}, {Channel: "b", Running: 0, Waiting: 1}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("负载不对：got=%+v want=%+v", got, want)
	}
}

// 多个并发领取者不会领到同一个任务（FOR UPDATE SKIP LOCKED）。
func TestGenerationTaskRepo_ClaimDue_Concurrent(t *testing.T) {
	ctx := context.Background()
	db := gtTestDB(t)
	repo := NewGenerationTaskRepository(db)
	now := time.Now()
	user := gtNewUserID()

	const total = 60
	for i := 0; i < total; i++ {
		if _, err := repo.InsertTask(ctx, gtTask(user, model.TaskPending, 1, now.Add(-time.Second))); err != nil {
			t.Fatal(err)
		}
	}

	var (
		mu      sync.Mutex
		claimed = map[uint64]int{}
		wg      sync.WaitGroup
	)
	for w := 0; w < 6; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				got, err := repo.ClaimDue(ctx, now, time.Minute, 7)
				if err != nil {
					t.Errorf("ClaimDue 失败：%v", err)
					return
				}
				if len(got) == 0 {
					return
				}
				mu.Lock()
				for _, g := range got {
					claimed[g.ID]++
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if len(claimed) != total {
		t.Fatalf("应领取全部 %d 个任务，实际 %d 个", total, len(claimed))
	}
	for id, n := range claimed {
		if n != 1 {
			t.Fatalf("任务 %d 被领取了 %d 次", id, n)
		}
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
			return &model.CreditLedger{UserID: u, TaskID: 777, Type: model.LedgerSettle, Amount: 3}
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
			if _, err := tx.InsertLedger(ctx, &model.CreditLedger{UserID: u, TaskID: 1, Type: model.LedgerSettle, Amount: 5}); err != nil {
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
	t.Run("ExtendLease 只续非终态任务", func(t *testing.T) {
		until := time.Now().Add(time.Hour)
		if ok, err := repo.ExtendLease(ctx, mine.ID, until); err != nil || !ok {
			t.Fatalf("ok=%v err=%v", ok, err)
		}
		if ok, _ := repo.ExtendLease(ctx, done.ID, until); ok {
			t.Fatal("终态任务不应续租")
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
		_, _ = repo.InsertLedger(ctx, &model.CreditLedger{UserID: u, TaskID: tk.ID, Type: model.LedgerFreeze, Amount: tk.Credits})
	}
	_, _ = repo.InsertLedger(ctx, &model.CreditLedger{UserID: u, TaskID: settled.ID, Type: model.LedgerSettle, Amount: 4})
	_, _ = repo.InsertLedger(ctx, &model.CreditLedger{UserID: u, TaskID: refunded.ID, Type: model.LedgerRefund, Amount: 6})
	// 账户：冻结 10（active），余额 50 - 4
	_ = repo.AddCredit(ctx, u, -4, 10)

	rec, err := repo.Reconcile(ctx, u)
	if err != nil {
		t.Fatalf("Reconcile 失败：%v", err)
	}
	if rec.FrozenDiff() != 0 || rec.ActiveDiff() != 0 || rec.BalanceDiff(50) != 0 {
		t.Fatalf("对账应零差异：%+v", rec)
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
