package repository_test

import (
	"context"
	"testing"
	"time"

	"gorm.io/datatypes"

	"video-canvas/internal/model"
	. "video-canvas/internal/repository"
)

func TestUserRepository_ListTasks(t *testing.T) {
	ctx := context.Background()
	db := userDB(t)
	repo := NewUserRepository(db)
	u := mkUser(t, db, "alice", "a@x.com", model.RoleUser, model.UserStatusActive)
	other := mkUser(t, db, "bob", "b@x.com", model.RoleUser, model.UserStatusActive)

	now := time.Now().Truncate(time.Second)
	sub := now.Add(-10 * time.Second)
	fin := now.Add(-4 * time.Second)
	charged := 7
	mk := func(userID uint64, status string, snapshot string, isTest bool, mod func(*model.GenerationTask)) {
		task := model.GenerationTask{
			UserID: userID, Kind: model.KindVideo, ModelKey: "m1", Provider: "p1", Status: status, Credits: 8, Version: 1,
			ConfigSnapshot: datatypes.JSON(snapshot), IsTest: isTest, NextPollAt: now, DeadlineAt: now.Add(time.Hour), CreatedAt: now,
		}
		if mod != nil {
			mod(&task)
		}
		task.InputJSON = datatypes.JSON(`{}`)
		if err := db.Create(&task).Error; err != nil { // 不用 mkTask：它会把 config_snapshot 覆盖成 {}
			t.Fatal(err)
		}
	}
	mk(u.ID, model.TaskSucceeded, `{"model":{"label":"Seedance 2.0"}}`, false, func(k *model.GenerationTask) {
		k.SubmittedAt, k.FinishedAt, k.ChargedCredits = &sub, &fin, &charged
	}) // id1
	mk(u.ID, model.TaskFailed, `{}`, false, func(k *model.GenerationTask) { k.ErrorMessage = "上游报错"; k.FinishedAt = &fin }) // id2 无 submitted：用 created_at 算
	mk(u.ID, model.TaskRunning, `{"model":{"label":""}}`, false, nil)                                                       // id3
	mk(u.ID, model.TaskExpired, `{}`, false, func(k *model.GenerationTask) { k.FinishedAt = &fin })                         // id4
	mk(u.ID, model.TaskCanceled, `{}`, false, func(k *model.GenerationTask) { k.FinishedAt = &fin })                        // id5
	mk(u.ID, model.TaskRunning, `{}`, true, nil)                                                                            // id6 试跑：必须排除
	mk(other.ID, model.TaskRunning, `{}`, false, nil)                                                                       // id7 别人的任务

	t.Run("全部：排除试跑与别人的任务，id 倒序；字段映射正确", func(t *testing.T) {
		rows, err := repo.ListTasks(ctx, TaskRecordFilter{UserID: u.ID, Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 5 {
			t.Fatalf("应有 5 条（不含试跑与他人任务）：%d", len(rows))
		}
		for i := 1; i < len(rows); i++ {
			if rows[i-1].ID <= rows[i].ID {
				t.Fatalf("应按 id 倒序：%+v", rows)
			}
		}
		byStatus := map[string]model.AdminTaskItem{}
		for _, r := range rows {
			byStatus[r.Status] = r
		}
		ok := byStatus[model.TaskSucceeded]
		if ok.ModelName != "Seedance 2.0" || ok.ModelKey != "m1" || ok.ChargedCredits != 7 || ok.Credits != 8 {
			t.Fatalf("成功任务字段不对：%+v", ok)
		}
		if ok.DurationMs == nil || *ok.DurationMs != 6000 {
			t.Fatalf("耗时应为 finished - submitted = 6000ms：%v", ok.DurationMs)
		}
		bad := byStatus[model.TaskFailed]
		if bad.ModelName != "m1" || bad.Error != "上游报错" || bad.ChargedCredits != 0 {
			t.Fatalf("失败任务：快照没有显示名回落 model_key，error 取 error_message：%+v", bad)
		}
		if bad.DurationMs == nil {
			t.Fatal("没有 submitted_at 时用 created_at 算耗时，已完成任务耗时不应为 null")
		}
		if run := byStatus[model.TaskRunning]; run.DurationMs != nil || run.ModelName != "m1" {
			t.Fatalf("进行中耗时应为 null，空 label 回落 model_key：%+v", run)
		}
	})

	t.Run("状态筛选：success / failed（含 expired 与 canceled）/ running", func(t *testing.T) {
		for _, c := range []struct {
			statuses []string
			want     int
		}{
			{[]string{model.TaskSucceeded}, 1},
			{[]string{model.TaskFailed, model.TaskExpired, model.TaskCanceled}, 3},
			{model.ActiveTaskStatuses, 1},
		} {
			rows, err := repo.ListTasks(ctx, TaskRecordFilter{UserID: u.ID, Statuses: c.statuses, Limit: 10})
			if err != nil || len(rows) != c.want {
				t.Fatalf("%v：期望 %d 条，实际 %d，err=%v", c.statuses, c.want, len(rows), err)
			}
		}
	})

	t.Run("游标：BeforeID 只返回 id 更小的记录", func(t *testing.T) {
		first, _ := repo.ListTasks(ctx, TaskRecordFilter{UserID: u.ID, Limit: 2})
		next, err := repo.ListTasks(ctx, TaskRecordFilter{UserID: u.ID, BeforeID: first[1].ID, Limit: 10})
		if err != nil || len(next) != 3 || next[0].ID >= first[1].ID {
			t.Fatalf("%v %+v", err, next)
		}
	})
}

func TestUserRepository_ListLedger(t *testing.T) {
	ctx := context.Background()
	db := userDB(t)
	repo := NewUserRepository(db)
	u := mkUser(t, db, "alice", "a@x.com", model.RoleUser, model.UserStatusActive)
	op := mkUser(t, db, "ops", "o@x.com", model.RoleAdmin, model.UserStatusActive)
	other := mkUser(t, db, "bob", "b@x.com", model.RoleUser, model.UserStatusActive)

	tid := uint64(11)
	entries := []model.CreditLedger{
		{UserID: u.ID, Type: model.LedgerInitial, Amount: 100},
		{UserID: u.ID, Type: model.LedgerFreeze, Amount: 8, TaskID: &tid},
		{UserID: u.ID, Type: model.LedgerSettle, Amount: 8, TaskID: &tid},
		{UserID: u.ID, Type: model.LedgerAdminAdjust, Amount: -5, OperatorID: &op.ID, Note: "纠错"},
		{UserID: u.ID, Type: model.LedgerRefund, Amount: 3, TaskID: &tid},
		{UserID: other.ID, Type: model.LedgerAdminAdjust, Amount: 9, OperatorID: &op.ID, Note: "别人"},
	}
	for i := range entries {
		if err := db.Create(&entries[i]).Error; err != nil {
			t.Fatal(err)
		}
	}

	rows, err := repo.ListLedger(ctx, LedgerRecordFilter{UserID: u.ID, Limit: 10})
	if err != nil || len(rows) != 5 || rows[0].Type != model.LedgerRefund {
		t.Fatalf("全部应含 initial，只含本人，id 倒序：%v %+v", err, rows)
	}

	rows, _ = repo.ListLedger(ctx, LedgerRecordFilter{UserID: u.ID, Types: []string{model.LedgerAdminAdjust}, Limit: 10})
	if len(rows) != 1 || rows[0].Amount != -5 || rows[0].Note != "纠错" || rows[0].OperatorName != "ops" ||
		rows[0].OperatorID == nil || *rows[0].OperatorID != op.ID || rows[0].TaskID != nil {
		t.Fatalf("admin 只看管理员调整，并带操作人用户名：%+v", rows)
	}

	rows, _ = repo.ListLedger(ctx, LedgerRecordFilter{UserID: u.ID, Types: []string{model.LedgerFreeze, model.LedgerSettle, model.LedgerRefund}, Limit: 10})
	if len(rows) != 3 {
		t.Fatalf("task 只看 freeze/settle/refund（不含 initial）：%+v", rows)
	}
	for _, r := range rows {
		if r.OperatorName != "" || r.TaskID == nil {
			t.Fatalf("任务流水无操作人、有 task_id：%+v", r)
		}
	}

	page, _ := repo.ListLedger(ctx, LedgerRecordFilter{UserID: u.ID, BeforeID: rows[0].ID, Limit: 10})
	for _, r := range page {
		if r.ID >= rows[0].ID {
			t.Fatalf("游标应只返回更小的 id：%+v", page)
		}
	}
}

func TestUserRepository_ListLogins(t *testing.T) {
	ctx := context.Background()
	db := userDB(t)
	repo := NewUserRepository(db)
	u := mkUser(t, db, "alice", "a@x.com", model.RoleUser, model.UserStatusActive)
	for _, l := range []model.UserLoginLog{
		{UserID: u.ID, Kind: model.LoginKindRegister, Result: model.LoginResultOK, IP: "1.1.1.1"},
		{UserID: u.ID, Kind: model.LoginKindLogin, Result: model.LoginResultBadPw, IP: "2.2.2.2", UserAgent: "UA"},
		{UserID: u.ID, Kind: model.LoginKindLogin, Result: model.LoginResultOK, IP: "1.1.1.1"},
		{UserID: u.ID, Kind: model.LoginKindLogin, Result: model.LoginResultBlocked, IP: "3.3.3.3"},
		{UserID: 0, Kind: model.LoginKindLogin, Result: model.LoginResultBadPw, IP: "9.9.9.9"},
	} {
		if err := db.Create(&l).Error; err != nil {
			t.Fatal(err)
		}
	}
	rows, err := repo.ListLogins(ctx, LoginRecordFilter{UserID: u.ID, Limit: 10})
	if err != nil || len(rows) != 4 || rows[0].Result != model.LoginResultBlocked {
		t.Fatalf("全部：只含本人，id 倒序：%v %+v", err, rows)
	}
	rows, _ = repo.ListLogins(ctx, LoginRecordFilter{UserID: u.ID, Results: []string{model.LoginResultBadPw, model.LoginResultBlocked}, Limit: 10})
	if len(rows) != 2 || rows[1].UserAgent != "UA" || rows[1].IP != "2.2.2.2" {
		t.Fatalf("fail 只看 badpw / blocked：%+v", rows)
	}
	rows, _ = repo.ListLogins(ctx, LoginRecordFilter{UserID: u.ID, Limit: 2})
	more, _ := repo.ListLogins(ctx, LoginRecordFilter{UserID: u.ID, BeforeID: rows[1].ID, Limit: 10})
	if len(rows) != 2 || len(more) != 2 || more[0].ID >= rows[1].ID {
		t.Fatalf("游标分页：%+v %+v", rows, more)
	}
}

func TestAdminAuditRepository_DetailShape(t *testing.T) {
	ctx := context.Background()
	db := userDB(t)
	audit := NewAdminAuditRepository(db)
	actor := mkUser(t, db, "ops", "o@x.com", model.RoleAdmin, model.UserStatusActive)
	target := mkUser(t, db, "alice", "a@x.com", model.RoleUser, model.UserStatusActive)
	detail := model.JSONText(`{"mode":"add","amount":50,"delta":50,"note":"活动补偿"}`)
	if err := audit.Insert(ctx, &model.AdminAuditLog{ActorID: actor.ID, Action: model.AdminAuditCreditAdjust, TargetType: model.AdminAuditTargetUser, TargetID: target.ID, DetailJSON: detail}); err != nil {
		t.Fatal(err)
	}
	views, err := audit.RecentForUser(ctx, target.ID, 3)
	if err != nil || len(views) != 1 || views[0].ActorName != "ops" || views[0].Action != "user.credit_adjust" {
		t.Fatalf("%v %+v", err, views)
	}
	if !jsonEqual(views[0].DetailJSON, `{"mode":"add","amount":50,"delta":50,"note":"活动补偿"}`) {
		t.Fatalf("detail_json 应原样返回且含 note：%s", views[0].DetailJSON)
	}
}
