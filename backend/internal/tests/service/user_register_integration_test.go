package service_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
	. "video-canvas/internal/service"
)

// 本文件是注册服务 + 真实仓储的集成测试：验证并发注册在真实 PostgreSQL 上的行为（首个账号只有一个超管）。需要 TEST_DATABASE_DSN，未设置时跳过。

func TestUserService_Register_Integration_ConcurrentFirstUser(t *testing.T) {
	ctx := context.Background()
	db := gtiTestDB(t)
	if err := db.AutoMigrate(model.All()...); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewUserRepository(db)
	svc, _, _, _ := newUserSvc(newFakeUserRepo(), func(d *UserDeps) { d.Repo = repo })

	// 20 个并发请求同时注册（SMTP 未配置、表为空）：没启用邮件服务就不验证邮箱，所以全部成功，
	// 但只有抢到注册锁的第一个能成为 super_admin，其余都是普通用户
	const n = 20
	var wg sync.WaitGroup
	results := make([]error, n)
	roles := make([]string, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			view, err := svc.Register(ctx, regReq(fmt.Sprintf("user%02d", i), fmt.Sprintf("u%02d@x.com", i), ""), regMeta)
			results[i] = err
			if view != nil {
				roles[i] = view.Role
			}
		}()
	}
	wg.Wait()

	supers := 0
	for i, err := range results {
		if err != nil {
			t.Errorf("第 %d 个注册应成功：%v", i, err)
			continue
		}
		if roles[i] == model.RoleSuperAdmin {
			supers++
		}
	}
	if supers != 1 {
		t.Fatalf("并发注册应恰好产生 1 个 super_admin，实际 %d", supers)
	}
	var users, admins, credits, initials int64
	db.Model(&model.User{}).Count(&users)
	db.Model(&model.User{}).Where("role = ?", model.RoleSuperAdmin).Count(&admins)
	db.Model(&model.UserCredit{}).Count(&credits)
	db.Model(&model.CreditLedger{}).Where("type = ?", model.LedgerInitial).Count(&initials)
	if users != n || admins != 1 || credits != n || initials != n {
		t.Fatalf("库里应有 %d 个用户 / 1 个 super_admin / %d 个积分账户 / %d 条 initial 流水：%d %d %d %d", n, n, n, users, admins, credits, initials)
	}
}

func TestUserService_Register_Integration_EmailCaseInsensitive(t *testing.T) {
	ctx := context.Background()
	db := gtiTestDB(t)
	if err := db.AutoMigrate(model.All()...); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewUserRepository(db)
	svc, mail, _, _ := newUserSvc(newFakeUserRepo(), func(d *UserDeps) { d.Repo = repo })
	// 第一个账号（免验证码）占用邮箱
	if _, err := svc.Register(ctx, regReq("alice", "Alice@Example.com", ""), regMeta); err != nil {
		t.Fatal(err)
	}
	mail.enabled = true
	// 之后：邮箱只是大小写不同也算已注册
	if err := svc.SendRegisterCode(ctx, "ALICE@example.COM", "1.1.1.1"); codeOf(err) != errcode.ErrEmailExists.Code {
		t.Fatalf("发码应提示邮箱已注册：%v", err)
	}
	if err := svc.SendRegisterCode(ctx, "bob@example.com", "1.1.1.1"); err != nil {
		t.Fatal(err)
	}
	code := mail.lastCode()
	// 用大小写不同的同一邮箱注册：验证码按小写邮箱存取，所以验证通过，但邮箱唯一性拒绝
	_ = db.Create(&model.User{Username: "other", Password: "x", Email: "bob@example.com"}).Error
	if _, err := svc.Register(ctx, regReq("bob", "BOB@Example.com", code), regMeta); codeOf(err) != errcode.ErrEmailExists.Code {
		t.Fatalf("应 53002：%v", err)
	}
}
