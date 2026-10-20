package repository_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"

	"video-canvas/internal/model"
	. "video-canvas/internal/repository"
)

// slotDB 为名额用例建独立 schema：名额的占用数是全表统计，需要和其它用例隔离。
func slotDB(t *testing.T) *gorm.DB {
	t.Helper()
	return isolatedDB(t, &model.GenerationTask{}, &model.GenerationTaskEvent{}, &model.UserCredit{}, &model.CreditLedger{}, &model.AIChannel{}, &model.User{})
}

// slotTask 插入一个任务。held 为 true 表示已经拿到名额（slot_acquired_at 非空）。
func slotTask(t *testing.T, repo *GenerationTaskRepository, user uint64, provider, status string, held bool, created time.Time, mut func(*model.GenerationTask)) *model.GenerationTask {
	t.Helper()
	tk := gtTask(user, status, 1, created)
	tk.Provider = provider
	tk.CreatedAt = created
	tk.DeadlineAt = time.Now().Add(time.Hour)
	if held {
		at := created
		tk.SlotAcquiredAt = &at
	}
	if mut != nil {
		mut(tk)
	}
	if ok, err := repo.InsertTask(context.Background(), tk); err != nil || !ok {
		t.Fatalf("插入任务失败：%v inserted=%v", err, ok)
	}
	return tk
}

func slotReq(id uint64, userLimit int) SlotRequest {
	return SlotRequest{TaskID: id, UserLimit: userLimit, DefaultUserLimit: 4, Now: time.Now(), Deadline: time.Now().Add(30 * time.Minute)}
}

func mustAcquire(t *testing.T, repo *GenerationTaskRepository, req SlotRequest) SlotResult {
	t.Helper()
	res, err := repo.AcquireSlot(context.Background(), req)
	if err != nil {
		t.Fatalf("AcquireSlot 失败：%v", err)
	}
	return res
}

func TestAcquireSlot_UserLimit(t *testing.T) {
	db := slotDB(t)
	repo := NewGenerationTaskRepository(db)
	user := gtNewUserID()
	t0 := time.Now().Add(-time.Hour)
	gtChannel(t, db, "ch", 0)

	// 用户上限 2：已有 running + finalizing 各一个占着名额
	slotTask(t, repo, user, "ch", model.TaskRunning, true, t0, nil)
	done := slotTask(t, repo, user, "ch", model.TaskFinalizing, true, t0.Add(time.Second), nil)
	waiting := slotTask(t, repo, user, "ch", model.TaskPending, false, t0.Add(2*time.Second), nil)

	t.Run("用户名额满时等待，不改任务", func(t *testing.T) {
		if got := mustAcquire(t, repo, slotReq(waiting.ID, 2)); got != SlotWaitUser {
			t.Fatalf("应因用户名额满而等待，实际 %s", got)
		}
		var cur model.GenerationTask
		db.First(&cur, waiting.ID)
		if cur.SlotAcquiredAt != nil {
			t.Fatal("没拿到名额不应写 slot_acquired_at")
		}
	})

	t.Run("占名额的任务进入终态后释放名额", func(t *testing.T) {
		if err := db.Model(&model.GenerationTask{}).Where("id = ?", done.ID).Update("status", model.TaskSucceeded).Error; err != nil {
			t.Fatal(err)
		}
		req := slotReq(waiting.ID, 2)
		if got := mustAcquire(t, repo, req); got != SlotAcquired {
			t.Fatalf("名额释放后应拿到，实际 %s", got)
		}
		var cur model.GenerationTask
		db.First(&cur, waiting.ID)
		if cur.SlotAcquiredAt == nil {
			t.Fatal("拿到名额应写 slot_acquired_at")
		}
		// 拿到名额后生成时长从此刻重新计算
		if d := cur.DeadlineAt.Sub(req.Deadline); d < -time.Second || d > time.Second {
			t.Fatalf("deadline_at 应重置为 %v，实际 %v", req.Deadline, cur.DeadlineAt)
		}
	})

	t.Run("已经拿到名额的任务重复申请直接成功，不重复占用", func(t *testing.T) {
		if got := mustAcquire(t, repo, slotReq(waiting.ID, 1)); got != SlotAcquired {
			t.Fatalf("已持有名额应直接返回已获取，实际 %s", got)
		}
	})

	t.Run("任务已被取消返回 SlotGone", func(t *testing.T) {
		gone := slotTask(t, repo, user, "ch", model.TaskCanceled, false, t0, nil)
		if got := mustAcquire(t, repo, slotReq(gone.ID, 10)); got != SlotGone {
			t.Fatalf("终态任务不应再申请名额，实际 %s", got)
		}
	})
}

func TestAcquireSlot_ChannelLimitAndFIFO(t *testing.T) {
	db := slotDB(t)
	repo := NewGenerationTaskRepository(db)
	t0 := time.Now().Add(-time.Hour)
	gtChannel(t, db, "ch", 1)
	gtChannel(t, db, "free", 0)

	userA, userB := gtNewUserID(), gtNewUserID()
	occupied := slotTask(t, repo, userA, "ch", model.TaskRunning, true, t0, nil)

	t.Run("渠道名额满时等待，finalizing 不占渠道名额", func(t *testing.T) {
		w := slotTask(t, repo, userB, "ch", model.TaskPending, false, t0.Add(time.Second), nil)
		if got := mustAcquire(t, repo, slotReq(w.ID, 4)); got != SlotWaitChannel {
			t.Fatalf("渠道 max_running=1 已被 running 占满，应等待，实际 %s", got)
		}
		if err := db.Model(&model.GenerationTask{}).Where("id = ?", occupied.ID).Update("status", model.TaskFinalizing).Error; err != nil {
			t.Fatal(err)
		}
		if got := mustAcquire(t, repo, slotReq(w.ID, 4)); got != SlotAcquired {
			t.Fatalf("转存阶段不占渠道名额，应拿到，实际 %s", got)
		}
	})

	t.Run("名额按创建顺序先到先得：后创建的先来申请也要让位", func(t *testing.T) {
		db := slotDB(t)
		repo := NewGenerationTaskRepository(db)
		gtChannel(t, db, "ch", 1)
		a, b := gtNewUserID(), gtNewUserID()
		early := slotTask(t, repo, a, "ch", model.TaskPending, false, t0, nil)
		late := slotTask(t, repo, b, "ch", model.TaskPending, false, t0.Add(time.Second), nil)

		if got := mustAcquire(t, repo, slotReq(late.ID, 4)); got != SlotWaitChannel {
			t.Fatalf("前面还有更早的等待者，后者不能插队，实际 %s", got)
		}
		if got := mustAcquire(t, repo, slotReq(early.ID, 4)); got != SlotAcquired {
			t.Fatalf("最早的应拿到，实际 %s", got)
		}
		if got := mustAcquire(t, repo, slotReq(late.ID, 4)); got != SlotWaitChannel {
			t.Fatalf("名额被占后应继续等待，实际 %s", got)
		}
	})

	t.Run("排在前面但自己用户名额已满的等待者不挡别人", func(t *testing.T) {
		db := slotDB(t)
		repo := NewGenerationTaskRepository(db)
		gtChannel(t, db, "ch", 1)
		gtChannel(t, db, "other", 0)
		greedy, other := gtNewUserID(), gtNewUserID()
		// greedy 在别的渠道上已经占满了默认上限 4
		for i := range 4 {
			slotTask(t, repo, greedy, "other", model.TaskRunning, true, t0.Add(time.Duration(i)*time.Second), nil)
		}
		slotTask(t, repo, greedy, "ch", model.TaskPending, false, t0.Add(10*time.Second), nil) // 更早，但拿不了名额
		mine := slotTask(t, repo, other, "ch", model.TaskPending, false, t0.Add(20*time.Second), nil)

		if got := mustAcquire(t, repo, slotReq(mine.ID, 4)); got != SlotAcquired {
			t.Fatalf("前面的等待者自己卡在用户上限上，不应占着渠道名额不放，实际 %s", got)
		}
	})

	t.Run("已超过截止时间的等待者不参与排名", func(t *testing.T) {
		db := slotDB(t)
		repo := NewGenerationTaskRepository(db)
		gtChannel(t, db, "ch", 1)
		a, b := gtNewUserID(), gtNewUserID()
		slotTask(t, repo, a, "ch", model.TaskPending, false, t0, func(tk *model.GenerationTask) { tk.DeadlineAt = time.Now().Add(-time.Minute) })
		mine := slotTask(t, repo, b, "ch", model.TaskPending, false, t0.Add(time.Second), nil)
		if got := mustAcquire(t, repo, slotReq(mine.ID, 4)); got != SlotAcquired {
			t.Fatalf("排在前面的已经超时，会被置为 expired，不该挡路，实际 %s", got)
		}
	})

	t.Run("渠道 max_running 为 0 或没有渠道记录表示不限", func(t *testing.T) {
		u := gtNewUserID()
		for _, p := range []string{"free", "no-record"} {
			for i := range 3 {
				tk := slotTask(t, repo, u, p, model.TaskPending, false, t0.Add(time.Duration(i)*time.Second), nil)
				if got := mustAcquire(t, repo, slotReq(tk.ID, 100)); got != SlotAcquired {
					t.Fatalf("%s 不限并发，应拿到，实际 %s", p, got)
				}
			}
		}
	})
}

func TestAcquireSlot_TestTaskSkipsUserLimit(t *testing.T) {
	db := slotDB(t)
	repo := NewGenerationTaskRepository(db)
	user := gtNewUserID()
	t0 := time.Now().Add(-time.Hour)
	gtChannel(t, db, "ch", 1)
	// 管理员试跑任务不受用户并发限制（与创建时 CountActive 排除 is_test 一致），但占渠道名额
	test := slotTask(t, repo, user, "ch", model.TaskPending, false, t0, func(tk *model.GenerationTask) { tk.IsTest = true })
	if got := mustAcquire(t, repo, slotReq(test.ID, 0)); got != SlotAcquired {
		t.Fatalf("试跑任务不看用户上限，应拿到，实际 %s", got)
	}
	other := slotTask(t, repo, gtNewUserID(), "ch", model.TaskPending, false, t0.Add(time.Second), nil)
	if got := mustAcquire(t, repo, slotReq(other.ID, 4)); got != SlotWaitChannel {
		t.Fatalf("试跑任务占着渠道名额，正式任务要等，实际 %s", got)
	}
}

// 多个 goroutine 同时申请：占用数不能超过上限（用户维度和渠道维度各验证一次）。
func TestAcquireSlot_Concurrent(t *testing.T) {
	t0 := time.Now().Add(-time.Hour)

	t.Run("渠道维度", func(t *testing.T) {
		db := slotDB(t)
		repo := NewGenerationTaskRepository(db)
		gtChannel(t, db, "ch", 5)
		var tasks []*model.GenerationTask
		for i := range 20 {
			tasks = append(tasks, slotTask(t, repo, gtNewUserID(), "ch", model.TaskPending, false, t0.Add(time.Duration(i)*time.Second), nil))
		}
		var got atomic.Int32
		var wg sync.WaitGroup
		for _, tk := range tasks {
			wg.Add(1)
			go func() {
				defer wg.Done()
				res, err := repo.AcquireSlot(context.Background(), slotReq(tk.ID, 4))
				if err != nil {
					t.Errorf("AcquireSlot 失败：%v", err)
					return
				}
				if res == SlotAcquired {
					got.Add(1)
				}
			}()
		}
		wg.Wait()
		if got.Load() != 5 {
			t.Fatalf("渠道上限 5，应恰好 5 个拿到，实际 %d", got.Load())
		}
	})

	t.Run("用户维度", func(t *testing.T) {
		db := slotDB(t)
		repo := NewGenerationTaskRepository(db)
		gtChannel(t, db, "ch", 0)
		user := gtNewUserID()
		var tasks []*model.GenerationTask
		for i := range 12 {
			tasks = append(tasks, slotTask(t, repo, user, "ch", model.TaskPending, false, t0.Add(time.Duration(i)*time.Second), nil))
		}
		var got atomic.Int32
		var wg sync.WaitGroup
		for _, tk := range tasks {
			wg.Add(1)
			go func() {
				defer wg.Done()
				res, err := repo.AcquireSlot(context.Background(), slotReq(tk.ID, 3))
				if err != nil {
					t.Errorf("AcquireSlot 失败：%v", err)
					return
				}
				if res == SlotAcquired {
					got.Add(1)
				}
			}()
		}
		wg.Wait()
		if got.Load() != 3 {
			t.Fatalf("用户上限 3，应恰好 3 个拿到，实际 %d", got.Load())
		}
	})
}
