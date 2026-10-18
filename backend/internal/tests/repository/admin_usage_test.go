package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/datatypes"

	"video-canvas/internal/model"
	. "video-canvas/internal/repository"
)

type usageSeed struct {
	t    *testing.T
	repo *GenerationTaskRepository
}

// add 用真实仓储插入任务（会写 task_ref 与 created 事件），created 控制创建时间。
func (s usageSeed) add(userID uint64, status string, created time.Time, mut func(*model.GenerationTask)) *model.GenerationTask {
	s.t.Helper()
	task := gtTask(userID, status, 8, created)
	task.CreatedAt = created
	if mut != nil {
		mut(task)
	}
	if ok, err := s.repo.InsertTask(context.Background(), task); err != nil || !ok {
		s.t.Fatalf("插入任务失败：%v %v", err, ok)
	}
	return task
}

func ids(rows []model.UsageRow) []uint64 {
	out := make([]uint64, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
}

func TestAdminUsageRepo_List(t *testing.T) {
	ctx := context.Background()
	db := userDB(t)
	repo := NewAdminUsageRepository(db)
	seed := usageSeed{t, NewGenerationTaskRepository(db)}
	alice := mkUser(t, db, "alice", "alice@x.com", model.RoleUser, model.UserStatusActive)
	bob := mkUser(t, db, "bob", "bob@studio.cn", model.RoleUser, model.UserStatusActive)

	now := time.Now().Truncate(time.Second)
	fin := now.Add(-30 * time.Second)
	charged := 6
	t1 := seed.add(alice.ID, model.TaskSucceeded, now.Add(-2*time.Hour), func(k *model.GenerationTask) {
		k.ConfigSnapshot = datatypes.JSON(`{"model":{"label":"可灵 v2"}}`)
		k.FinishedAt = &fin
		k.ChargedCredits = &charged
	})
	t2 := seed.add(alice.ID, model.TaskFailed, now.Add(-90*time.Minute), func(k *model.GenerationTask) {
		k.Kind, k.ModelKey, k.ErrorCode, k.ErrorMessage = model.KindImage, "img-1", "plugin_error", "平台繁忙"
		k.FinishedAt = &fin
	})
	t3 := seed.add(bob.ID, model.TaskRunning, now.Add(-10*time.Minute), nil)
	t4 := seed.add(bob.ID, model.TaskExpired, now.Add(-40*24*time.Hour), nil) // 远超默认范围
	t5 := seed.add(alice.ID, model.TaskSucceeded, now.Add(-5*time.Minute), func(k *model.GenerationTask) { k.IsTest = true })

	from, to := now.Add(-7*24*time.Hour), now.Add(time.Minute)
	base := UsageFilter{From: from, To: to, Limit: 50}

	t.Run("默认：时间范围内、不含试跑，id 倒序，带用户名与字段映射", func(t *testing.T) {
		rows, err := repo.List(ctx, base)
		if err != nil {
			t.Fatal(err)
		}
		if got := ids(rows); len(got) != 3 || got[0] != t3.ID || got[1] != t2.ID || got[2] != t1.ID {
			t.Fatalf("应为 t3,t2,t1，实际 %v", got)
		}
		r1 := rows[2]
		if r1.Username != "alice" || r1.TaskRef != t1.TaskRef || r1.ModelName != "可灵 v2" || r1.ChargedCredits == nil || *r1.ChargedCredits != 6 {
			t.Fatalf("字段映射不对：%+v", r1)
		}
		if r1.DurationMs == nil || *r1.DurationMs != fin.Sub(t1.CreatedAt).Milliseconds() {
			t.Fatalf("耗时应为 finished_at - created_at：%v", r1.DurationMs)
		}
		if rows[1].ModelName != "img-1" || rows[1].ErrorCode != "plugin_error" {
			t.Fatalf("没有快照展示名时回落 model key：%+v", rows[1])
		}
		if rows[0].DurationMs != nil || rows[0].ChargedCredits != nil {
			t.Fatalf("进行中任务耗时与实扣应为 null：%+v", rows[0])
		}
	})

	t.Run("含试跑 / 放宽时间范围", func(t *testing.T) {
		f := base
		f.IncludeTest = true
		rows, _ := repo.List(ctx, f)
		if len(rows) != 4 || !rows[0].IsTest || rows[0].ID != t5.ID {
			t.Fatalf("含试跑应多出 t5：%v", ids(rows))
		}
		f = base
		f.From = now.Add(-60 * 24 * time.Hour)
		rows, _ = repo.List(ctx, f)
		// 列表按 id 倒序；这里 t4 是后插入的（id 最大），只是创建时间很早
		if len(rows) != 4 || rows[0].ID != t4.ID {
			t.Fatalf("放宽范围应多出 t4：%v", ids(rows))
		}
	})

	t.Run("按用户、状态、类型、模型筛选", func(t *testing.T) {
		f := base
		f.UserID = alice.ID
		if rows, _ := repo.List(ctx, f); len(rows) != 2 {
			t.Fatalf("alice 应有 2 条：%v", ids(rows))
		}
		f = base
		f.Statuses = []string{model.TaskFailed}
		if rows, _ := repo.List(ctx, f); len(rows) != 1 || rows[0].ID != t2.ID {
			t.Fatalf("failed 只应有 t2：%v", ids(rows))
		}
		f = base
		f.Statuses = model.ActiveTaskStatuses
		if rows, _ := repo.List(ctx, f); len(rows) != 1 || rows[0].ID != t3.ID {
			t.Fatalf("进行中只应有 t3：%v", ids(rows))
		}
		f = base
		f.Kind = model.KindImage
		if rows, _ := repo.List(ctx, f); len(rows) != 1 || rows[0].ID != t2.ID {
			t.Fatalf("image 只应有 t2：%v", ids(rows))
		}
		f = base
		f.Model = "img-1"
		if rows, _ := repo.List(ctx, f); len(rows) != 1 || rows[0].ID != t2.ID {
			t.Fatalf("模型 img-1 只应有 t2：%v", ids(rows))
		}
	})

	t.Run("关键字：用户名 / 邮箱模糊（大小写不敏感），任务编号精确；通配符按字面量处理", func(t *testing.T) {
		f := base
		f.UserKeyword = "ALI"
		if rows, _ := repo.List(ctx, f); len(rows) != 2 {
			t.Fatalf("按用户名片段应命中 alice 的 2 条：%v", ids(rows))
		}
		f.UserKeyword = "studio.cn"
		if rows, _ := repo.List(ctx, f); len(rows) != 1 || rows[0].ID != t3.ID {
			t.Fatalf("按邮箱片段应命中 bob 的 t3：%v", ids(rows))
		}
		f.UserKeyword = "%"
		if rows, _ := repo.List(ctx, f); len(rows) != 0 {
			t.Fatalf("百分号应按字面量匹配，不该命中全部：%v", ids(rows))
		}
		f = base
		f.TaskRef = t2.TaskRef
		if rows, _ := repo.List(ctx, f); len(rows) != 1 || rows[0].ID != t2.ID {
			t.Fatalf("按任务编号应精确命中：%v", ids(rows))
		}
	})

	t.Run("分页：Offset + Limit，Count 与筛选一致且不受分页影响", func(t *testing.T) {
		f := base
		f.Limit = 2
		page1, _ := repo.List(ctx, f)
		if len(page1) != 2 || page1[0].ID != t3.ID || page1[1].ID != t2.ID {
			t.Fatalf("第一页应是 t3,t2：%v", ids(page1))
		}
		f.Offset = 2
		page2, _ := repo.List(ctx, f)
		if len(page2) != 1 || page2[0].ID != t1.ID {
			t.Fatalf("第二页应只剩 t1：%v", ids(page2))
		}
		f.Offset = 10
		if beyond, _ := repo.List(ctx, f); len(beyond) != 0 {
			t.Fatalf("超出末页应为空：%v", ids(beyond))
		}
		total, err := repo.Count(ctx, f)
		if err != nil || total != 3 {
			t.Fatalf("总数应为 3（与分页参数无关）：%d %v", total, err)
		}
		f.Statuses = []string{model.TaskFailed}
		if total, _ := repo.Count(ctx, f); total != 1 {
			t.Fatalf("总数应遵守筛选：%d", total)
		}
	})
}

func TestAdminUsageRepo_Summary(t *testing.T) {
	ctx := context.Background()
	db := userDB(t)
	repo := NewAdminUsageRepository(db)
	seed := usageSeed{t, NewGenerationTaskRepository(db)}
	u := mkUser(t, db, "alice", "a@x.com", model.RoleUser, model.UserStatusActive)
	now := time.Now().Truncate(time.Second)
	c5, c3 := 5, 3
	seed.add(u.ID, model.TaskSucceeded, now.Add(-time.Hour), func(k *model.GenerationTask) { k.ChargedCredits = &c5 })
	seed.add(u.ID, model.TaskSucceeded, now.Add(-2*time.Hour), func(k *model.GenerationTask) { k.ChargedCredits = &c3 })
	seed.add(u.ID, model.TaskFailed, now.Add(-time.Hour), nil)
	seed.add(u.ID, model.TaskRunning, now.Add(-time.Minute), nil)
	seed.add(u.ID, model.TaskSucceeded, now.Add(-time.Minute), func(k *model.GenerationTask) { k.IsTest = true; k.ChargedCredits = &c5 })

	rows, err := repo.Summary(ctx, UsageFilter{From: now.Add(-24 * time.Hour), To: now.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]model.UsageStatusCount{}
	for _, r := range rows {
		got[r.Status] = r
	}
	if got[model.TaskSucceeded].Count != 2 || got[model.TaskSucceeded].Charged != 8 || got[model.TaskFailed].Count != 1 || got[model.TaskRunning].Count != 1 {
		t.Fatalf("摘要分组不对（试跑应被排除）：%+v", got)
	}
	rows, _ = repo.Summary(ctx, UsageFilter{From: now.Add(-24 * time.Hour), To: now.Add(time.Minute), IncludeTest: true})
	var total int
	for _, r := range rows {
		total += r.Count
	}
	if total != 5 {
		t.Fatalf("含试跑应共 5 条：%d", total)
	}
}

func TestAdminUsageRepo_Detail(t *testing.T) {
	ctx := context.Background()
	db := userDB(t)
	repo := NewAdminUsageRepository(db)
	taskRepo := NewGenerationTaskRepository(db)
	seed := usageSeed{t, taskRepo}
	u := mkUser(t, db, "alice", "a@x.com", model.RoleUser, model.UserStatusActive)
	task := seed.add(u.ID, model.TaskPending, time.Now(), nil)
	if _, err := taskRepo.UpdateIf(ctx, task.ID, model.ActiveTaskStatuses, map[string]any{
		"status": model.TaskFailed, EventDetailKey: map[string]any{"upstream": "boom"},
	}, true); err != nil {
		t.Fatal(err)
	}
	tid := task.ID
	for _, l := range []model.CreditLedger{
		{UserID: u.ID, TaskID: &tid, Type: model.LedgerFreeze, Amount: 8},
		{UserID: u.ID, TaskID: &tid, Type: model.LedgerRefund, Amount: 8},
	} {
		l := l
		if err := db.Create(&l).Error; err != nil {
			t.Fatal(err)
		}
	}

	got, err := repo.GetTaskByRef(ctx, task.TaskRef)
	if err != nil || got.ID != task.ID {
		t.Fatalf("按编号取任务失败：%v %+v", err, got)
	}
	if _, err := repo.GetTaskByRef(ctx, "00000000000000000000000000000000"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在应返回 ErrNotFound：%v", err)
	}
	if usr, err := repo.GetUser(ctx, u.ID); err != nil || usr.Username != "alice" {
		t.Fatalf("取用户失败：%v %+v", err, usr)
	}
	if _, err := repo.GetUser(ctx, 999999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("用户不存在应返回 ErrNotFound：%v", err)
	}
	evs, err := repo.ListEvents(ctx, task.ID)
	if err != nil || len(evs) != 2 || evs[0].Event != model.TaskEventCreated || evs[1].Event != model.TaskEventFailed {
		t.Fatalf("时间线不对：%v %+v", err, evs)
	}
	led, err := repo.ListTaskLedger(ctx, task.ID)
	if err != nil || len(led) != 2 || led[0].Type != model.LedgerFreeze || led[1].Type != model.LedgerRefund {
		t.Fatalf("积分流水应按 id 升序：%v %+v", err, led)
	}
}
