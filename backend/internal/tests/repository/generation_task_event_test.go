package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"video-canvas/internal/model"
	. "video-canvas/internal/repository"
)

// eventNames 取事件名序列，方便断言时间线。
func eventNames(evs []model.GenerationTaskEvent) []string {
	out := make([]string, 0, len(evs))
	for _, e := range evs {
		out = append(out, e.Event)
	}
	return out
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// 任务事件：状态真正变化时才在同一语句所在的事务里记一行，进度变化与 queued → running 不记。
func TestGenerationTaskRepo_Events(t *testing.T) {
	ctx := context.Background()
	db := gtTestDB(t)
	repo := NewGenerationTaskRepository(db)
	user := gtNewUserID()

	task := gtTask(user, model.TaskPending, 10, time.Now())
	if inserted, err := repo.InsertTask(ctx, task); err != nil || !inserted {
		t.Fatalf("插入失败：%v inserted=%v", err, inserted)
	}

	evs, err := repo.ListEvents(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !sameStrings(eventNames(evs), []string{model.TaskEventCreated}) || evs[0].ToStatus != model.TaskPending || evs[0].FromStatus != "" {
		t.Fatalf("创建后应有一条 created（pending）：%+v", evs)
	}

	step := func(from []string, fields map[string]any) {
		t.Helper()
		if _, err := repo.UpdateIf(ctx, task.ID, from, fields, true); err != nil {
			t.Fatalf("UpdateIf 失败：%v", err)
		}
	}
	// pending 里只写 submitted_at（MarkSubmitting）：状态没变，不记事件
	step([]string{model.TaskPending}, map[string]any{"submitted_at": time.Now()})
	// pending → queued：上游受理
	step([]string{model.TaskPending}, map[string]any{"status": model.TaskQueued})
	// queued → running 与同状态轮询：不记
	step([]string{model.TaskQueued, model.TaskRunning}, map[string]any{"status": model.TaskRunning})
	step([]string{model.TaskQueued, model.TaskRunning}, map[string]any{"status": model.TaskRunning, "progress": 40})
	// running → finalizing：from_status 要是真实的上一个状态 running（from 里有两个，仓储要自己读）
	step([]string{model.TaskQueued, model.TaskRunning}, map[string]any{"status": model.TaskFinalizing})
	// finalizing → failed：带上游错误原文，保留键不能被当成列
	step(model.ActiveTaskStatuses, map[string]any{
		"status": model.TaskFailed, "error_code": "plugin_error",
		EventDetailKey: map[string]any{"upstream": "boom: 503", "reason": "x"},
	})

	evs, err = repo.ListEvents(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{model.TaskEventCreated, model.TaskEventSubmitted, model.TaskEventFinalizing, model.TaskEventFailed}
	if !sameStrings(eventNames(evs), want) {
		t.Fatalf("时间线应为 %v，实际 %v", want, eventNames(evs))
	}
	if evs[1].FromStatus != model.TaskPending || evs[1].ToStatus != model.TaskQueued {
		t.Fatalf("submitted 应是 pending → queued：%+v", evs[1])
	}
	if evs[2].FromStatus != model.TaskRunning || evs[2].ToStatus != model.TaskFinalizing {
		t.Fatalf("finalizing 的 from_status 应是 running：%+v", evs[2])
	}
	if evs[3].FromStatus != model.TaskFinalizing || evs[3].ToStatus != model.TaskFailed {
		t.Fatalf("failed 应是 finalizing → failed：%+v", evs[3])
	}
	if !jsonEqual(evs[3].DetailJSON, `{"upstream":"boom: 503","reason":"x"}`) {
		t.Fatalf("failed 事件应带上游原文与原因：%s", evs[3].DetailJSON)
	}
	if evs[1].DetailJSON != nil && string(evs[1].DetailJSON) != "null" {
		t.Fatalf("没有 detail 的事件 detail_json 应为空：%s", evs[1].DetailJSON)
	}

	var got model.GenerationTask
	if err := db.First(&got, task.ID).Error; err != nil || got.Status != model.TaskFailed || got.ErrorCode != "plugin_error" {
		t.Fatalf("任务本身的更新应照常生效：%v %+v", err, got)
	}
}

// 同步接口：pending 直接 finalizing；CAS 没命中不留事件；事务回滚时事件一起回滚。
func TestGenerationTaskRepo_Events_EdgeCases(t *testing.T) {
	ctx := context.Background()
	db := gtTestDB(t)
	repo := NewGenerationTaskRepository(db)
	user := gtNewUserID()

	t.Run("pending 直接 finalizing（同步接口）记 finalizing，from 为 pending", func(t *testing.T) {
		task := gtTask(user, model.TaskPending, 1, time.Now())
		_, _ = repo.InsertTask(ctx, task)
		if _, err := repo.UpdateIf(ctx, task.ID, []string{model.TaskPending}, map[string]any{"status": model.TaskFinalizing}, true); err != nil {
			t.Fatal(err)
		}
		evs, _ := repo.ListEvents(ctx, task.ID)
		if len(evs) != 2 || evs[1].Event != model.TaskEventFinalizing || evs[1].FromStatus != model.TaskPending {
			t.Fatalf("应为 created + finalizing(pending→finalizing)：%+v", evs)
		}
	})

	t.Run("CAS 没命中：返回 ErrStateConflict，不留事件", func(t *testing.T) {
		task := gtTask(user, model.TaskSucceeded, 1, time.Now())
		_, _ = repo.InsertTask(ctx, task)
		_, err := repo.UpdateIf(ctx, task.ID, model.ActiveTaskStatuses, map[string]any{"status": model.TaskFailed}, true)
		if !errors.Is(err, ErrStateConflict) {
			t.Fatalf("应返回 ErrStateConflict：%v", err)
		}
		evs, _ := repo.ListEvents(ctx, task.ID)
		if len(evs) != 1 {
			t.Fatalf("只应有 created，实际 %+v", evs)
		}
	})

	t.Run("事务回滚：状态迁移与事件一起回滚", func(t *testing.T) {
		task := gtTask(user, model.TaskPending, 1, time.Now())
		_, _ = repo.InsertTask(ctx, task)
		boom := errors.New("boom")
		err := repo.WithTx(ctx, func(tx GenerationTaskTx) error {
			if _, err := tx.UpdateIf(ctx, task.ID, []string{model.TaskPending}, map[string]any{"status": model.TaskQueued}, true); err != nil {
				return err
			}
			return boom
		})
		if !errors.Is(err, boom) {
			t.Fatalf("应返回回调的错误：%v", err)
		}
		evs, _ := repo.ListEvents(ctx, task.ID)
		if len(evs) != 1 {
			t.Fatalf("回滚后只应有 created，实际 %+v", evs)
		}
	})

	t.Run("幂等键冲突没有写入任务：不产生 created 事件", func(t *testing.T) {
		a := gtTask(user, model.TaskPending, 1, time.Now())
		a.IdempotencyKey = "same-key"
		if ok, err := repo.InsertTask(ctx, a); err != nil || !ok {
			t.Fatalf("首次插入失败：%v %v", err, ok)
		}
		b := gtTask(user, model.TaskPending, 1, time.Now())
		b.IdempotencyKey = "same-key"
		if ok, err := repo.InsertTask(ctx, b); err != nil || ok {
			t.Fatalf("重复键应返回 inserted=false：%v %v", err, ok)
		}
		var n int64
		if err := db.Model(&model.GenerationTaskEvent{}).Where("task_id = ?", b.ID).Count(&n).Error; err != nil || n != 0 {
			t.Fatalf("没写入的任务不应有事件：%d %v", n, err)
		}
	})
}
