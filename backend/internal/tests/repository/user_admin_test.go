package repository_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"video-canvas/internal/model"
	. "video-canvas/internal/repository"
)

func userDB(t *testing.T) *gorm.DB {
	t.Helper()
	return isolatedDB(t, model.All()...)
}

func mkUser(t *testing.T, db *gorm.DB, name, email, role, status string) *model.User {
	t.Helper()
	u := &model.User{Username: name, Password: "x", Email: email, Role: role, Status: status}
	if err := db.Create(u).Error; err != nil {
		t.Fatalf("建用户失败：%v", err)
	}
	return u
}

// mkTask 插入任务：补上 NOT NULL 的 JSON 字段，并检查错误（避免静默失败让断言看起来像统计错了）。
func mkTask(t *testing.T, db *gorm.DB, task model.GenerationTask) {
	t.Helper()
	task.InputJSON = datatypes.JSON(`{}`)
	task.ConfigSnapshot = datatypes.JSON(`{}`)
	if err := db.Create(&task).Error; err != nil {
		t.Fatalf("建任务失败：%v", err)
	}
}

func TestEnsureCredit_WritesInitialLedgerOnce(t *testing.T) {
	ctx := context.Background()
	db := userDB(t)
	repo := NewGenerationTaskRepository(db)
	u := mkUser(t, db, "alice", "a@x.com", model.RoleUser, model.UserStatusActive)

	for i := range 2 {
		if err := repo.EnsureCredit(ctx, u.ID, 30); err != nil {
			t.Fatalf("第 %d 次 EnsureCredit 失败：%v", i+1, err)
		}
	}
	var rows []model.CreditLedger
	db.Where("user_id = ?", u.ID).Find(&rows)
	if len(rows) != 1 || rows[0].Type != model.LedgerInitial || rows[0].Amount != 30 || rows[0].TaskID != nil {
		t.Fatalf("应只有一条 initial 流水（task_id 为空、金额 30）：%+v", rows)
	}
	acc, _ := repo.GetCredit(ctx, u.ID)
	if acc.Balance != 30 {
		t.Fatalf("余额应为 30：%+v", acc)
	}
}

func TestUserRepository_RegisterTx(t *testing.T) {
	ctx := context.Background()
	db := userDB(t)
	repo := NewUserRepository(db)

	t.Run("事务内建用户、积分账户、initial 流水与登录记录；出错整体回滚", func(t *testing.T) {
		err := repo.WithTx(ctx, func(tx UserTx) error {
			u := &model.User{Username: "bob", Password: "x", Email: "b@x.com", Role: model.RoleUser, Status: model.UserStatusActive}
			if err := tx.Create(ctx, u); err != nil {
				return err
			}
			if err := tx.CreateCredit(ctx, u.ID, 10); err != nil {
				return err
			}
			return tx.InsertLoginLog(ctx, &model.UserLoginLog{UserID: u.ID, Kind: model.LoginKindRegister, Result: model.LoginResultOK, IP: "1.1.1.1"})
		})
		if err != nil {
			t.Fatal(err)
		}
		u, err := repo.GetByUsername(ctx, "bob")
		if err != nil {
			t.Fatal(err)
		}
		var credit model.UserCredit
		var ledger, logs int64
		db.First(&credit, "user_id = ?", u.ID)
		db.Model(&model.CreditLedger{}).Where("user_id = ? AND type = 'initial' AND amount = 10", u.ID).Count(&ledger)
		db.Model(&model.UserLoginLog{}).Where("user_id = ? AND kind = 'register'", u.ID).Count(&logs)
		if credit.Balance != 10 || ledger != 1 || logs != 1 {
			t.Fatalf("事务内写入不完整：credit=%+v ledger=%d logs=%d", credit, ledger, logs)
		}

		boom := errBoom{}
		err = repo.WithTx(ctx, func(tx UserTx) error {
			if err := tx.Create(ctx, &model.User{Username: "rollback", Password: "x", Role: model.RoleUser, Status: model.UserStatusActive}); err != nil {
				return err
			}
			return boom
		})
		if !errors.Is(err, boom) {
			t.Fatalf("应透传错误：%v", err)
		}
		if _, err := repo.GetByUsername(ctx, "rollback"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("回滚后不应存在：%v", err)
		}
	})

	t.Run("用户名与邮箱存在性检查；邮箱按小写存储比较", func(t *testing.T) {
		_ = repo.WithTx(ctx, func(tx UserTx) error {
			if ok, _ := tx.UsernameExists(ctx, "bob"); !ok {
				t.Error("bob 应存在")
			}
			if ok, _ := tx.UsernameExists(ctx, "nobody"); ok {
				t.Error("nobody 不应存在")
			}
			if ok, _ := tx.EmailExists(ctx, "b@x.com"); !ok {
				t.Error("b@x.com 应存在")
			}
			if ok, _ := tx.EmailExists(ctx, "B@X.COM"); !ok {
				t.Error("邮箱大小写不敏感")
			}
			if n, _ := tx.Count(ctx); n < 1 {
				t.Error("用户数应 >= 1")
			}
			return nil
		})
	})

	t.Run("邮箱唯一索引挡住大小写之外的重复，返回 ErrDuplicate", func(t *testing.T) {
		err := repo.Create(ctx, &model.User{Username: "dup", Password: "x", Email: "b@x.com", Role: model.RoleUser, Status: model.UserStatusActive})
		if !errors.Is(err, ErrDuplicate) {
			t.Fatalf("应返回 ErrDuplicate：%v", err)
		}
	})

	t.Run("LockRegistration 串行化并发事务：并发判断“表是否为空”只有一个看到空表", func(t *testing.T) {
		db := userDB(t)
		repo := NewUserRepository(db)
		var wg sync.WaitGroup
		var mu sync.Mutex
		firsts := 0
		for i := range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = repo.WithTx(ctx, func(tx UserTx) error {
					if err := tx.LockRegistration(ctx); err != nil {
						return err
					}
					n, err := tx.Count(ctx)
					if err != nil {
						return err
					}
					if n == 0 {
						mu.Lock()
						firsts++
						mu.Unlock()
					}
					time.Sleep(20 * time.Millisecond)
					return tx.Create(ctx, &model.User{Username: "u" + string(rune('a'+i)), Password: "x", Role: model.RoleUser, Status: model.UserStatusActive})
				})
			}()
		}
		wg.Wait()
		if firsts != 1 {
			t.Fatalf("只应有一个事务看到空表，实际 %d", firsts)
		}
	})
}

type errBoom struct{}

func (errBoom) Error() string { return "boom" }

func TestUserRepository_ListAdmin(t *testing.T) {
	ctx := context.Background()
	db := userDB(t)
	repo := NewUserRepository(db)
	a := mkUser(t, db, "alice", "alice@x.com", model.RoleUser, model.UserStatusActive)
	b := mkUser(t, db, "bob_100%", "bob@x.com", model.RoleAdmin, model.UserStatusActive)
	c := mkUser(t, db, "carol", "carol@y.com", model.RoleUser, model.UserStatusDisabled)
	four := 4
	db.Model(&model.User{}).Where("id = ?", c.ID).Update("max_active_tasks", &four)
	db.Create(&model.UserCredit{UserID: a.ID, Balance: 50, Frozen: 8})
	// alice 有 2 个进行中任务、1 个已完成任务、1 个试跑任务（不计入）
	for _, st := range []string{model.TaskRunning, model.TaskPending, model.TaskSucceeded} {
		mkTask(t, db, model.GenerationTask{UserID: a.ID, Kind: "video", ModelKey: "m", Provider: "p", Status: st, Credits: 4, NextPollAt: time.Now(), DeadlineAt: time.Now()})
	}
	mkTask(t, db, model.GenerationTask{UserID: a.ID, Kind: "video", ModelKey: "m", Provider: "p", Status: model.TaskRunning, IsTest: true, NextPollAt: time.Now(), DeadlineAt: time.Now()})

	t.Run("无筛选：id 倒序，带积分与进行中任务数；无积分账户标记", func(t *testing.T) {
		rows, total, err := repo.ListAdmin(ctx, UserListFilter{Limit: 10})
		if err != nil || total != 3 || len(rows) != 3 {
			t.Fatalf("total=%d rows=%d err=%v", total, len(rows), err)
		}
		if rows[0].ID != c.ID || rows[2].ID != a.ID {
			t.Fatalf("应按 id 倒序：%+v", rows)
		}
		ra := rows[2]
		if ra.Balance != 50 || ra.Frozen != 8 || !ra.HasCreditAccount || ra.ActiveTasks != 2 {
			t.Fatalf("alice 视图不对：%+v", ra)
		}
		if rows[0].HasCreditAccount || rows[0].Balance != 0 || rows[0].MaxActiveTasks == nil || *rows[0].MaxActiveTasks != 4 {
			t.Fatalf("carol 视图不对：%+v", rows[0])
		}
	})

	for _, tt := range []struct {
		name string
		f    UserListFilter
		want []uint64
	}{
		{"q 匹配用户名（不区分大小写）", UserListFilter{Q: "ALI", Limit: 10}, []uint64{a.ID}},
		{"q 匹配邮箱", UserListFilter{Q: "y.com", Limit: 10}, []uint64{c.ID}},
		{"q 里的 % 当普通字符", UserListFilter{Q: "100%", Limit: 10}, []uint64{b.ID}},
		{"q 只有 % 不会匹配全部", UserListFilter{Q: "%", Limit: 10}, []uint64{b.ID}},
		{"status 筛选", UserListFilter{Status: model.UserStatusDisabled, Limit: 10}, []uint64{c.ID}},
		{"role 筛选", UserListFilter{Role: model.RoleAdmin, Limit: 10}, []uint64{b.ID}},
		{"分页", UserListFilter{Offset: 1, Limit: 1}, []uint64{b.ID}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rows, _, err := repo.ListAdmin(ctx, tt.f)
			if err != nil {
				t.Fatal(err)
			}
			var got []uint64
			for _, r := range rows {
				got = append(got, r.ID)
			}
			if len(got) != len(tt.want) || (len(got) > 0 && got[0] != tt.want[0]) {
				t.Fatalf("期望 %v，实际 %v", tt.want, got)
			}
		})
	}

	t.Run("详情统计：任务总数 / 成功 / 失败 / 近 7 天 / 已花积分", func(t *testing.T) {
		for _, st := range []string{model.TaskFailed, model.TaskExpired} {
			mkTask(t, db, model.GenerationTask{UserID: a.ID, Kind: "video", ModelKey: "m", Provider: "p", Status: st, NextPollAt: time.Now(), DeadlineAt: time.Now()})
		}
		old := time.Now().AddDate(0, 0, -10)
		mkTask(t, db, model.GenerationTask{UserID: a.ID, Kind: "video", ModelKey: "m", Provider: "p", Status: model.TaskSucceeded, NextPollAt: old, DeadlineAt: old, CreatedAt: old})
		db.Create(&model.CreditLedger{UserID: a.ID, TaskID: model.TaskIDPtr(1), Type: model.LedgerSettle, Amount: 7})
		db.Create(&model.CreditLedger{UserID: a.ID, TaskID: model.TaskIDPtr(2), Type: model.LedgerSettle, Amount: 3})
		st, err := repo.TaskStats(ctx, a.ID)
		if err != nil {
			t.Fatal(err)
		}
		// 正式任务 6 条：running/pending/succeeded/failed/expired + 10 天前 succeeded；试跑不计
		if st.Total != 6 || st.Success != 2 || st.Failed != 2 || st.Last7d != 5 || st.SpentCredits != 10 {
			t.Fatalf("统计不对：%+v", st)
		}
	})
}

func TestAdminAuditRepository(t *testing.T) {
	ctx := context.Background()
	db := userDB(t)
	repo := NewAdminAuditRepository(db)
	admin := mkUser(t, db, "root", "", model.RoleSuperAdmin, model.UserStatusActive)
	target := mkUser(t, db, "tom", "", model.RoleUser, model.UserStatusActive)
	for i := range 5 {
		_ = i
		if err := repo.Insert(ctx, &model.AdminAuditLog{ActorID: admin.ID, Action: model.AdminAuditUserBan, TargetType: model.AdminAuditTargetUser, TargetID: target.ID, DetailJSON: model.JSONText(`{"a":1}`)}); err != nil {
			t.Fatal(err)
		}
	}
	_ = repo.Insert(ctx, &model.AdminAuditLog{ActorID: admin.ID, Action: model.AdminAuditSettingsSMTP, TargetType: model.AdminAuditTargetSettings})
	list, err := repo.RecentForUser(ctx, target.ID, 3)
	if err != nil || len(list) != 3 {
		t.Fatalf("应只返回该用户最近 3 条：%d %v", len(list), err)
	}
	if list[0].ActorName != "root" || list[0].Action != model.AdminAuditUserBan {
		t.Fatalf("应带操作人用户名：%+v", list[0])
	}
}

func TestSystemSettingRepository(t *testing.T) {
	ctx := context.Background()
	repo := NewSystemSettingRepository(userDB(t))
	m, err := repo.GetAll(ctx)
	if err != nil || len(m) != 0 {
		t.Fatalf("空表应返回空 map：%v %v", m, err)
	}
	if err := repo.SetMany(ctx, map[string]string{model.SettingInitialCredits: "20", model.SettingRegisterEnabled: "false"}, 9); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetMany(ctx, map[string]string{model.SettingInitialCredits: "30"}, 9); err != nil {
		t.Fatal(err)
	}
	m, _ = repo.GetAll(ctx)
	if m[model.SettingInitialCredits] != "30" || m[model.SettingRegisterEnabled] != "false" {
		t.Fatalf("upsert 结果不对：%v", m)
	}
}

func TestSMTPSettingRepository(t *testing.T) {
	ctx := context.Background()
	repo := NewSMTPSettingRepository(userDB(t))
	if _, err := repo.Get(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("未配置应返回 ErrNotFound：%v", err)
	}
	// 先设密码（此时还没有配置行）：不能因为没有行而丢失
	if err := repo.SetPassword(ctx, []byte("ct"), []byte("nonce"), 3); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveConfig(ctx, &model.SMTPSetting{Host: "smtp.x.com", Port: 587, Encryption: "starttls", FromAddress: "a@x.com", Enabled: true, UpdatedBy: 3}); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx)
	if err != nil || got.Host != "smtp.x.com" || string(got.PasswordEnc) != "ct" || !got.Enabled {
		t.Fatalf("保存配置不应清掉密码：%+v %v", got, err)
	}
	if err := repo.SaveConfig(ctx, &model.SMTPSetting{Host: "smtp.y.com", Port: 465, Encryption: "tls", FromAddress: "a@y.com", Enabled: false, UpdatedBy: 4}); err != nil {
		t.Fatal(err)
	}
	got, _ = repo.Get(ctx)
	if got.Host != "smtp.y.com" || got.Enabled || string(got.PasswordEnc) != "ct" {
		t.Fatalf("再次保存应覆盖配置、保留密码：%+v", got)
	}
	at := time.Now()
	if err := repo.RecordCheck(ctx, false, "连接超时", at); err != nil {
		t.Fatal(err)
	}
	got, _ = repo.Get(ctx)
	if got.LastCheckOK == nil || *got.LastCheckOK || got.LastCheckError != "连接超时" || got.LastCheckAt == nil {
		t.Fatalf("检查结果未记录：%+v", got)
	}
}

func TestUserRepository_ResetPassword_And_CountByRole(t *testing.T) {
	ctx := context.Background()
	db := userDB(t)
	repo := NewUserRepository(db)
	u := mkUser(t, db, "alice", "a@x.com", model.RoleSuperAdmin, model.UserStatusActive)
	mkUser(t, db, "bob", "b@x.com", model.RoleSuperAdmin, model.UserStatusActive)
	mkUser(t, db, "carl", "c@x.com", model.RoleUser, model.UserStatusActive)

	if n, err := repo.CountByRole(ctx, model.RoleSuperAdmin); err != nil || n != 2 {
		t.Fatalf("超管数应为 2：%d %v", n, err)
	}

	// 并发重置：token_version 在库里原子 +1，不会因读后写丢失
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := repo.ResetPassword(ctx, u.ID, "$2a$hash"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	got, err := repo.GetByID(ctx, u.ID)
	if err != nil || got.TokenVersion != 10 || got.Password != "$2a$hash" {
		t.Fatalf("10 次并发重置后 token_version 应为 10：%+v %v", got, err)
	}

	if err := repo.ResetPassword(ctx, 9999, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("用户不存在应返回 ErrNotFound：%v", err)
	}
}
