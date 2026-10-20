package queue_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/repository"
)

// 一个任务从入队开始，自己走完 提交 → 查询 → 转存 → 成功，三个阶段各被调用，最后队列里不再有活着的 job。
func TestRuntime_TaskRunsThroughAllStages(t *testing.T) {
	db := queueDB(t)
	addChannel(t, db, "ch", 0)
	r := newRig(t, db, rigOpts{})
	tk := r.addTask(t, newUserID(), "ch", nil)

	r.waitStatus(t, tk.ID, model.TaskSucceeded, 40*time.Second)

	for _, stage := range []string{"pending", "queued", "running", "finalizing"} {
		if r.handler.count(fmt.Sprintf("%s:%d", stage, tk.ID)) == 0 {
			t.Fatalf("阶段 %s 应被处理过，调用记录 %v", stage, r.handler.calls)
		}
	}
	waitFor(t, 10*time.Second, "任务结束后队列里不应再有活着的 job", func() bool { return r.liveJobs(tk.ID) == 0 })

	var tt model.GenerationTask
	r.db.First(&tt, tk.ID)
	if tt.SlotAcquiredAt == nil {
		t.Fatal("提交阶段应先拿到名额再提交")
	}
}

// 入队和任务在同一个事务里：事务回滚时，队列里不能留下 job。
func TestRuntime_EnqueueRollsBackWithTransaction(t *testing.T) {
	db := queueDB(t)
	addChannel(t, db, "ch", 0)
	r := newRig(t, db, rigOpts{noStart: true})
	tk := r.newTask(newUserID(), "ch", nil)

	rollback := fmt.Errorf("回滚")
	err := r.repo.WithTx(context.Background(), func(tx repository.GenerationTaskTx) error {
		if _, err := tx.InsertTask(context.Background(), tk); err != nil {
			return err
		}
		if err := tx.EnqueueTask(context.Background(), tk); err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("应返回回滚用的错误，实际 %v", err)
	}
	var n int64
	db.Raw(`SELECT COUNT(*) FROM river_job`).Scan(&n)
	if n != 0 {
		t.Fatalf("事务回滚后不应有 job，实际 %d", n)
	}

	// 提交的事务则一定有 job
	committed := r.addTask(t, newUserID(), "ch", nil)
	if r.liveJobs(committed.ID) != 1 {
		t.Fatalf("事务提交后应恰有一个活着的 job，实际 %d", r.liveJobs(committed.ID))
	}
}

// 不在事务里调用 EnqueueTask 是编程错误：返回错误，而不是悄悄丢掉任务。
func TestRuntime_EnqueueOutsideTransactionFails(t *testing.T) {
	db := queueDB(t)
	r := newRig(t, db, rigOpts{noStart: true})
	if err := r.repo.EnqueueTask(context.Background(), r.newTask(1, "ch", nil)); err == nil {
		t.Fatal("不在事务里入队应返回错误")
	}
}

// 渠道 max_running=1：不同用户的任务排队，任何时刻占用渠道名额的任务不超过 1 个，最终全部完成。
func TestRuntime_ChannelLimitQueuesTasks(t *testing.T) {
	db := queueDB(t)
	addChannel(t, db, "ch", 1)
	r := newRig(t, db, rigOpts{submitDelay: 50 * time.Millisecond})
	var ids []uint64
	for range 4 {
		ids = append(ids, r.addTask(t, newUserID(), "ch", nil).ID)
	}
	for _, id := range ids {
		r.waitStatus(t, id, model.TaskSucceeded, 90*time.Second)
	}
	if got := r.handler.maxChan.Load(); got > 1 {
		t.Fatalf("渠道 max_running=1，观察到同时占用 %d 个名额", got)
	}
}

// 单用户并发 1：同一个用户一次创建 3 个任务，全部被接受，但同时占用名额的任务不超过 1 个，最终全部完成。
func TestRuntime_UserLimitQueuesTasks(t *testing.T) {
	db := queueDB(t)
	addChannel(t, db, "ch", 0)
	r := newRig(t, db, rigOpts{userLimit: 1, submitDelay: 50 * time.Millisecond})
	user := newUserID()
	var ids []uint64
	for range 3 {
		ids = append(ids, r.addTask(t, user, "ch", nil).ID)
	}
	for _, id := range ids {
		r.waitStatus(t, id, model.TaskSucceeded, 90*time.Second)
	}
	if got := r.handler.maxUser.Load(); got > 1 {
		t.Fatalf("用户并发上限 1，观察到同时占用 %d 个名额", got)
	}
}

// 提交阶段并发 1：同时在执行的任务不超过 1 个；后台把并发调大后立即生效（不需要重启）。
func TestRuntime_ConcurrencyGateIsAdjustable(t *testing.T) {
	db := queueDB(t)
	addChannel(t, db, "ch", 0)
	r := newRig(t, db, rigOpts{submitDelay: 300 * time.Millisecond, conc: map[string]int{model.QueueStageSubmit: 1, model.QueueStagePoll: 1, model.QueueStageFinalize: 1}})
	waitFor(t, 5*time.Second, "启动后应按设置生效并发 1", func() bool { return r.rt.Stats()[model.QueueStageSubmit].Limit == 1 })
	var ids []uint64
	for range 3 {
		ids = append(ids, r.addTask(t, newUserID(), "ch", nil).ID)
	}
	for _, id := range ids {
		r.waitStatus(t, id, model.TaskSucceeded, 90*time.Second)
	}
	if got := r.handler.maxAct.Load(); got > 3 {
		t.Fatalf("三个阶段并发各 1，同时处理的任务不会超过 3，实际 %d", got)
	}

	r.settings.set(model.QueueStageSubmit, 6)
	waitFor(t, 5*time.Second, "并发设置应在刷新周期内生效", func() bool { return r.rt.Stats()[model.QueueStageSubmit].Limit == 6 })
	// 超过硬上限的设置会被裁剪
	r.settings.set(model.QueueStageSubmit, 1000)
	waitFor(t, 5*time.Second, "超过硬上限应裁剪", func() bool { return r.rt.Stats()[model.QueueStageSubmit].Limit == 16 })
}

// 暂停提交阶段后不再提交，恢复后继续；暂停状态存在数据库里，States 能读到。
func TestRuntime_PauseAndResume(t *testing.T) {
	db := queueDB(t)
	addChannel(t, db, "ch", 0)
	r := newRig(t, db, rigOpts{})
	// 队列在 River 里首次启动时才会建出来，等它就绪再暂停
	waitFor(t, 10*time.Second, "队列就绪", func() bool {
		_, err := r.rt.States(context.Background())
		return err == nil
	})
	var paused bool
	waitFor(t, 10*time.Second, "暂停成功", func() bool {
		if err := r.rt.Pause(context.Background(), model.QueueStageSubmit); err != nil {
			return false
		}
		st, _ := r.rt.States(context.Background())
		for _, s := range st {
			if s.Stage == model.QueueStageSubmit {
				paused = s.Paused
			}
		}
		return paused
	})

	// 没有 LISTEN 通道（测试里是轮询模式）时，各个取任务的循环要过几秒才会发现队列被暂停
	time.Sleep(4 * time.Second)
	tk := r.addTask(t, newUserID(), "ch", nil)
	time.Sleep(2 * time.Second)
	if r.status(tk.ID) != model.TaskPending || r.handler.count("pending:") != 0 {
		t.Fatalf("暂停期间不应提交，状态 %s 调用 %v", r.status(tk.ID), r.handler.calls)
	}

	if err := r.rt.Resume(context.Background(), model.QueueStageSubmit); err != nil {
		t.Fatalf("恢复失败：%v", err)
	}
	r.waitStatus(t, tk.ID, model.TaskSucceeded, 40*time.Second)

	if err := r.rt.Pause(context.Background(), "nope"); err == nil {
		t.Fatal("未知阶段应返回错误")
	}
}

// 排队超过截止时间的任务直接走超时流程，不会去提交上游。
func TestRuntime_QueueTimeoutExpiresWithoutSubmitting(t *testing.T) {
	db := queueDB(t)
	addChannel(t, db, "ch", 0)
	r := newRig(t, db, rigOpts{})
	tk := r.addTask(t, newUserID(), "ch", func(tk *model.GenerationTask) { tk.DeadlineAt = time.Now().Add(-time.Minute) })
	r.waitStatus(t, tk.ID, model.TaskExpired, 20*time.Second)
	if r.handler.count("pending:") != 0 {
		t.Fatalf("已排队超时的任务不应被提交，调用 %v", r.handler.calls)
	}
}

// 任务在 job 睡着期间被取消：job 醒来读到终态，直接结束，不再处理。
func TestRuntime_CanceledTaskIsSkipped(t *testing.T) {
	db := queueDB(t)
	addChannel(t, db, "ch", 0)
	r := newRig(t, db, rigOpts{noStart: true})
	tk := r.addTask(t, newUserID(), "ch", nil)
	if err := db.Model(&model.GenerationTask{}).Where("id = ?", tk.ID).Update("status", model.TaskCanceled).Error; err != nil {
		t.Fatal(err)
	}
	r.start(t)
	waitFor(t, 20*time.Second, "已取消任务的 job 应结束", func() bool { return r.liveJobs(tk.ID) == 0 })
	if len(r.handler.calls) != 0 {
		t.Fatalf("已取消的任务不应被处理，调用 %v", r.handler.calls)
	}
}

// 对账：任务没终态但队列里没有 job（升级前在途、或进程在状态迁移后入队前崩溃），定时对账会按任务当前阶段补一个 job，任务继续跑完。
func TestRuntime_SweepEnqueuesTasksWithoutJob(t *testing.T) {
	db := queueDB(t)
	addChannel(t, db, "ch", 0)
	r := newRig(t, db, rigOpts{noStart: true})

	// 直接插入、不入队：一个 pending，一个已经在查询阶段（running，已有名额）
	pending := r.newTask(newUserID(), "ch", nil)
	running := r.newTask(newUserID(), "ch", func(tk *model.GenerationTask) {
		now := time.Now()
		tk.Status, tk.SlotAcquiredAt, tk.SubmittedAt = model.TaskRunning, &now, &now
		tk.ProviderTaskID = "up-1"
	})
	for _, tk := range []*model.GenerationTask{pending, running} {
		if _, err := r.repo.InsertTask(context.Background(), tk); err != nil {
			t.Fatal(err)
		}
	}
	r.start(t)

	r.waitStatus(t, pending.ID, model.TaskSucceeded, 60*time.Second)
	r.waitStatus(t, running.ID, model.TaskSucceeded, 60*time.Second)
	if r.handler.count(fmt.Sprintf("running:%d", running.ID)) == 0 || r.handler.count(fmt.Sprintf("pending:%d", running.ID)) != 0 {
		t.Fatalf("running 任务应从查询阶段接着跑，不能被重新提交，调用 %v", r.handler.calls)
	}
}

// 实例心跳：运行中的实例写入记录并带上各阶段的并发；退出后记录被删除。
func TestRuntime_InstanceHeartbeat(t *testing.T) {
	db := queueDB(t)
	r := newRig(t, db, rigOpts{noStart: true})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = r.rt.Run(ctx) }()

	var inst model.WorkerInstance
	waitFor(t, 10*time.Second, "出现实例心跳记录", func() bool {
		return db.Where("id = ?", r.rt.ID()).Take(&inst).Error == nil
	})
	if inst.Hostname != "testhost" || inst.Version != "test" || len(inst.StatsJSON) == 0 {
		t.Fatalf("心跳记录不完整：%+v", inst)
	}
	live, err := r.queueRep.LiveInstanceIDs(context.Background(), time.Now().Add(-30*time.Second))
	if err != nil || len(live) != 1 || live[0] != r.rt.ID() {
		t.Fatalf("应有一个在线实例 %s，实际 %v %v", r.rt.ID(), live, err)
	}

	cancel()
	<-done
	var n int64
	db.Model(&model.WorkerInstance{}).Where("id = ?", r.rt.ID()).Count(&n)
	if n != 0 {
		t.Fatal("优雅退出后应删除实例记录")
	}
}

// 救回：由失联实例正在执行的 running job 会被放回队列；在线实例的 job 不动。
func TestQueueRepo_RecoverOrphanedJobs(t *testing.T) {
	db := queueDB(t)
	repo := repository.NewQueueRepository(db)
	insert := func(id, by string) {
		err := db.Exec(`INSERT INTO river_job (state, queue, kind, args, attempt, max_attempts, attempted_by, attempted_at, scheduled_at, priority, metadata)
			VALUES ('running', 'gen_poll', 'gen.poll', ?::jsonb, 1, 100, ARRAY[?]::text[], now(), now(), 1, '{}'::jsonb)`, fmt.Sprintf(`{"task_id":%s}`, id), by).Error
		if err != nil {
			t.Fatalf("插入 job 失败：%v", err)
		}
	}
	insert("1", "dead_x")
	insert("2", "alive_y")
	// 同一个 job 先后被两个实例尝试过：以最后一个为准
	if err := db.Exec(`INSERT INTO river_job (state, queue, kind, args, attempt, max_attempts, attempted_by, attempted_at, scheduled_at, priority, metadata)
		VALUES ('running', 'gen_poll', 'gen.poll', '{"task_id":3}'::jsonb, 2, 100, ARRAY['dead_x','alive_y']::text[], now(), now(), 1, '{}'::jsonb)`).Error; err != nil {
		t.Fatal(err)
	}

	n, err := repo.RecoverOrphanedJobs(context.Background(), []string{"alive_y"})
	if err != nil || n != 1 {
		t.Fatalf("应只救回 1 个失联实例的 job，实际 %d %v", n, err)
	}
	state := func(task string) string {
		var s string
		db.Raw(`SELECT state FROM river_job WHERE args->>'task_id' = ?`, task).Scan(&s)
		return s
	}
	if state("1") != "retryable" || state("2") != "running" || state("3") != "running" {
		t.Fatalf("job 状态不符：1=%s 2=%s 3=%s", state("1"), state("2"), state("3"))
	}
	if n, _ := repo.RecoverOrphanedJobs(context.Background(), nil); n != 0 {
		t.Fatal("没有在线实例列表时不能动任何 job")
	}
}

// 统计：队列计数把“在等名额而睡着”的提交 job 算作排队；吞吐按分钟汇总；渠道占用含最早等待时间。
func TestQueueRepo_Stats(t *testing.T) {
	db := queueDB(t)
	addChannel(t, db, "ch", 1)
	r := newRig(t, db, rigOpts{noStart: true})
	ctx := context.Background()

	now := time.Now()
	held := r.addTask(t, newUserID(), "ch", func(tk *model.GenerationTask) {
		tk.Status, tk.SlotAcquiredAt = model.TaskRunning, &now
	})
	waiting := r.addTask(t, newUserID(), "ch", func(tk *model.GenerationTask) { tk.CreatedAt = now.Add(-2 * time.Minute) })
	// 把等名额的任务的 job 置为睡着（scheduled），模拟 snooze
	if err := db.Exec(`UPDATE river_job SET state = 'scheduled', scheduled_at = now() + interval '1 minute' WHERE (args->>'task_id')::bigint = ?`, waiting.ID).Error; err != nil {
		t.Fatal(err)
	}
	_ = held

	counts, err := r.queueRep.QueueCounts(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	submit := counts[model.QueueStageSubmit]
	if submit.Scheduled != 0 || submit.Available < 1 {
		t.Fatalf("等名额的睡着 job 应算在排队里：%+v", submit)
	}

	occ, err := r.queueRep.ChannelOccupancies(ctx)
	if err != nil || len(occ) != 1 {
		t.Fatalf("渠道占用不符：%v %v", occ, err)
	}
	if occ[0].Channel != "ch" || occ[0].Occupied != 1 || occ[0].Waiting != 1 || occ[0].OldestWaitingAt == nil {
		t.Fatalf("渠道占用内容不符：%+v", occ[0])
	}

	// 吞吐：一个成功（有排队时长）、一个失败、一个取消（不计）
	fin := now.Add(-30 * time.Second)
	slot := fin.Add(-10 * time.Second)
	mk := func(status string, withSlot bool) {
		tk := r.newTask(newUserID(), "ch", func(tk *model.GenerationTask) {
			tk.Status, tk.FinishedAt, tk.CreatedAt = status, &fin, slot.Add(-5*time.Second)
			if withSlot {
				tk.SlotAcquiredAt = &slot
			}
		})
		if _, err := r.repo.InsertTask(ctx, tk); err != nil {
			t.Fatal(err)
		}
	}
	mk(model.TaskSucceeded, true)
	mk(model.TaskFailed, false)
	mk(model.TaskCanceled, false)
	points, err := r.queueRep.Throughput(ctx, now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	var ok, bad int
	var wait int64
	for _, p := range points {
		ok += p.Succeeded
		bad += p.Failed
		wait += p.AvgWaitMs
	}
	if ok != 1 || bad != 1 || wait < 4000 || wait > 6000 {
		t.Fatalf("吞吐统计不符：成功 %d 失败 %d 排队 %dms（应为 1/1/约 5000）", ok, bad, wait)
	}
}
