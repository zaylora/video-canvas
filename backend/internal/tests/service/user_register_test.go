package service_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"video-canvas/internal/cache"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/utils"
	"video-canvas/internal/repository"
	. "video-canvas/internal/service"
)

func codeOf(err error) int {
	var ec *errcode.Error
	if errors.As(err, &ec) {
		return ec.Code
	}
	return -1
}

// testClock 是可拨动的时钟，用来让验证码过期。
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *testClock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

var regMeta = ClientMeta{IP: "8.8.4.4", UserAgent: "ua"}

func regReq(name, email, code string) *model.RegisterUserReq {
	return &model.RegisterUserReq{Username: name, Email: email, Password: "secret1", Code: code}
}

func TestUserService_AuthConfig(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name         string
		enabled      bool
		smtp         bool
		hasUser      bool
		wantEnabled  bool
		wantVerifyOn bool
	}{
		{"开关开 + SMTP 开 + 已有用户：开放且要验证码", true, true, true, true, true},
		{"开关开 + 无 SMTP + 已有用户：开放，但没启用邮件服务就不验证邮箱", true, false, true, true, false},
		{"开关开 + 无 SMTP + 空表：开放（首个账号），免验证码", true, false, false, true, false},
		{"开关开 + SMTP 开 + 空表：开放，首个账号免验证码", true, true, false, true, false},
		{"开关关：不开放", false, true, true, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeUserRepo()
			if tt.hasUser {
				repo.seed(model.User{Username: "root"})
			}
			svc, mail, policy, _ := newUserSvc(repo, nil)
			mail.enabled, policy.enabled = tt.smtp, tt.enabled
			got, err := svc.AuthConfig(ctx)
			if err != nil || got.RegisterEnabled != tt.wantEnabled || got.EmailVerifyRequired != tt.wantVerifyOn {
				t.Fatalf("期望 enabled=%v verify=%v，实际 %+v %v", tt.wantEnabled, tt.wantVerifyOn, got, err)
			}
		})
	}
}

func TestUserService_SendRegisterCode(t *testing.T) {
	ctx := context.Background()
	setup := func(mutate func(*fakeUserRepo, *fakeMail, *fakePolicy)) (*UserService, *fakeUserRepo, *fakeMail, *testClock) {
		repo := newFakeUserRepo()
		repo.seed(model.User{Username: "root", Email: "taken@x.com"})
		clock := &testClock{t: time.Now()}
		svc, mail, policy, _ := newUserSvc(repo, func(d *UserDeps) { d.Codes = cache.NewMemoryRegisterCodeStore(clock.now) })
		mail.enabled = true
		if mutate != nil {
			mutate(repo, mail, policy)
		}
		return svc, repo, mail, clock
	}

	t.Run("成功：发 6 位数字验证码，邮箱按小写发送", func(t *testing.T) {
		svc, _, mail, _ := setup(nil)
		if err := svc.SendRegisterCode(ctx, "New@X.com", "1.1.1.1"); err != nil {
			t.Fatal(err)
		}
		if len(mail.sent) != 1 || mail.sent[0].to != "new@x.com" {
			t.Fatalf("发送记录不对：%+v", mail.sent)
		}
		code := mail.lastCode()
		if len(code) != 6 || strings.Trim(code, "0123456789") != "" {
			t.Fatalf("验证码应为 6 位数字：%q", code)
		}
	})

	errCases := []struct {
		name     string
		mutate   func(*fakeUserRepo, *fakeMail, *fakePolicy)
		email    string
		wantCode int
	}{
		{"注册关闭", func(_ *fakeUserRepo, _ *fakeMail, p *fakePolicy) { p.enabled = false }, "n@x.com", errcode.ErrRegisterClosed.Code},
		{"未配置 SMTP", func(_ *fakeUserRepo, m *fakeMail, _ *fakePolicy) { m.enabled = false }, "n@x.com", errcode.ErrRegisterClosed.Code},
		{"邮箱已注册（大小写不敏感）", nil, "TAKEN@x.com", errcode.ErrEmailExists.Code},
		{"发信失败：返回 53007", func(_ *fakeUserRepo, m *fakeMail, _ *fakePolicy) {
			m.sendErr = errcode.ErrSMTPSendFailed.WithMsg("连接超时")
		}, "n@x.com", errcode.ErrSMTPSendFailed.Code},
	}
	for _, tt := range errCases {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, mail, _ := setup(tt.mutate)
			err := svc.SendRegisterCode(ctx, tt.email, "1.1.1.1")
			if codeOf(err) != tt.wantCode {
				t.Fatalf("期望 %d，实际 %v", tt.wantCode, err)
			}
			if tt.wantCode != errcode.ErrSMTPSendFailed.Code && len(mail.sent) != 0 {
				t.Fatal("被拒绝时不应发信")
			}
		})
	}

	t.Run("同邮箱 60 秒冷却，冷却后可再发（大小写视为同一邮箱）", func(t *testing.T) {
		svc, _, mail, clock := setup(nil)
		if err := svc.SendRegisterCode(ctx, "n@x.com", "1.1.1.1"); err != nil {
			t.Fatal(err)
		}
		if err := svc.SendRegisterCode(ctx, "N@x.com", "2.2.2.2"); codeOf(err) != errcode.ErrTooManyReqs.Code {
			t.Fatalf("冷却内应 429：%v", err)
		}
		clock.add(61 * time.Second)
		if err := svc.SendRegisterCode(ctx, "n@x.com", "1.1.1.1"); err != nil {
			t.Fatalf("冷却后应成功：%v", err)
		}
		if len(mail.sent) != 2 {
			t.Fatalf("应发出 2 封：%d", len(mail.sent))
		}
	})

	t.Run("同 IP 每小时最多 20 次，超过返回 429，一小时后恢复", func(t *testing.T) {
		svc, _, _, clock := setup(nil)
		for i := range 20 {
			email := "u" + string(rune('a'+i)) + "@x.com"
			if err := svc.SendRegisterCode(ctx, email, "3.3.3.3"); err != nil {
				t.Fatalf("第 %d 次应成功：%v", i+1, err)
			}
		}
		if err := svc.SendRegisterCode(ctx, "overflow@x.com", "3.3.3.3"); codeOf(err) != errcode.ErrTooManyReqs.Code {
			t.Fatalf("第 21 次应 429：%v", err)
		}
		if err := svc.SendRegisterCode(ctx, "other@x.com", "4.4.4.4"); err != nil {
			t.Fatalf("别的 IP 不受影响：%v", err)
		}
		clock.add(61 * time.Minute)
		if err := svc.SendRegisterCode(ctx, "late@x.com", "3.3.3.3"); err != nil {
			t.Fatalf("一小时后应恢复：%v", err)
		}
	})
}

func TestUserService_Register(t *testing.T) {
	ctx := context.Background()

	t.Run("首个账号成为 super_admin：免验证码（SMTP 未配），建积分账户并写 initial 流水与 register 记录，注册即登录", func(t *testing.T) {
		repo := newFakeUserRepo()
		svc, _, _, _ := newUserSvc(repo, nil)
		view, err := svc.Register(ctx, regReq("alice", "Alice@X.com", ""), regMeta)
		if err != nil {
			t.Fatal(err)
		}
		if view.Role != model.RoleSuperAdmin || view.Token == "" {
			t.Fatalf("返回体不对：%+v", view)
		}
		u := repo.users[0]
		if u.Role != model.RoleSuperAdmin || u.Email != "alice@x.com" || u.Status != model.UserStatusActive {
			t.Fatalf("用户不对（邮箱应存小写）：%+v", u)
		}
		if u.EmailVerifiedAt != nil {
			t.Fatal("未验证过邮箱不应写 email_verified_at")
		}
		if bc := u.Password; bc == "" || bc == "secret1" {
			t.Fatal("密码必须是哈希")
		}
		if repo.credits[u.ID] == nil || repo.credits[u.ID].Balance != 20 || len(repo.ledger) != 1 || repo.ledger[0].Type != model.LedgerInitial || repo.ledger[0].Amount != 20 {
			t.Fatalf("积分账户 / initial 流水不对：%+v %+v", repo.credits[u.ID], repo.ledger)
		}
		if got := repo.logResults(); len(got) != 1 || got[0] != "register/ok" {
			t.Fatalf("应写 register/ok：%v", got)
		}
		claims, err := utils.ParseToken(view.Token, testJWTSecret)
		if err != nil || claims.UserID != uint(u.ID) {
			t.Fatalf("注册即登录的 token 不对：%+v %v", claims, err)
		}
		if repo.locked != 1 {
			t.Fatal("注册事务必须先取注册锁")
		}
	})

	t.Run("非首个账号是普通用户；配了 SMTP 时需要验证码，验证通过写 email_verified_at", func(t *testing.T) {
		repo := newFakeUserRepo()
		repo.seed(model.User{Username: "root", Role: model.RoleSuperAdmin})
		svc, mail, _, _ := newUserSvc(repo, nil)
		mail.enabled = true
		if err := svc.SendRegisterCode(ctx, "bob@x.com", "1.1.1.1"); err != nil {
			t.Fatal(err)
		}
		view, err := svc.Register(ctx, regReq("bob", "bob@x.com", mail.lastCode()), regMeta)
		if err != nil || view.Role != model.RoleUser {
			t.Fatalf("应注册成功为普通用户：%+v %v", view, err)
		}
		if u := repo.users[1]; u.EmailVerifiedAt == nil || u.Role != model.RoleUser {
			t.Fatalf("验证过邮箱应写 email_verified_at：%+v", u)
		}
	})

	t.Run("首个账号即使配了 SMTP 也免验证码，但没验证过邮箱", func(t *testing.T) {
		repo := newFakeUserRepo()
		svc, mail, _, _ := newUserSvc(repo, nil)
		mail.enabled = true
		view, err := svc.Register(ctx, regReq("alice", "a@x.com", ""), regMeta)
		if err != nil || view.Role != model.RoleSuperAdmin || repo.users[0].EmailVerifiedAt != nil {
			t.Fatalf("%+v %v", view, err)
		}
	})

	t.Run("没启用邮件服务：非首个账号免验证码注册，不写 email_verified_at", func(t *testing.T) {
		repo := newFakeUserRepo()
		repo.seed(model.User{Username: "root", Role: model.RoleSuperAdmin})
		svc, _, _, _ := newUserSvc(repo, nil)
		view, err := svc.Register(ctx, regReq("bob", "bob@x.com", ""), regMeta)
		if err != nil || view.Role != model.RoleUser || repo.users[1].EmailVerifiedAt != nil {
			t.Fatalf("%+v %v", view, err)
		}
	})

	t.Run("初始积分来自设置", func(t *testing.T) {
		repo := newFakeUserRepo()
		svc, _, policy, _ := newUserSvc(repo, nil)
		policy.initial = 0
		if _, err := svc.Register(ctx, regReq("alice", "a@x.com", ""), regMeta); err != nil {
			t.Fatal(err)
		}
		if repo.credits[1].Balance != 0 || len(repo.ledger) != 1 || repo.ledger[0].Amount != 0 {
			t.Fatalf("初始积分 0 也要建账户并写流水：%+v %+v", repo.credits[1], repo.ledger)
		}
	})

	errCases := []struct {
		name     string
		setup    func(*fakeUserRepo, *fakeMail, *fakePolicy)
		req      *model.RegisterUserReq
		wantCode int
	}{
		{"注册关闭", func(_ *fakeUserRepo, _ *fakeMail, p *fakePolicy) { p.enabled = false }, regReq("bob", "b@x.com", ""), errcode.ErrRegisterClosed.Code},
		{"需要验证码但没带", func(r *fakeUserRepo, m *fakeMail, _ *fakePolicy) {
			r.seed(model.User{Username: "root"})
			m.enabled = true
		}, regReq("bob", "b@x.com", ""), errcode.ErrCodeInvalid.Code},
		{"验证码错误", func(r *fakeUserRepo, m *fakeMail, _ *fakePolicy) {
			r.seed(model.User{Username: "root"})
			m.enabled = true
		}, regReq("bob", "b@x.com", "000000"), errcode.ErrCodeInvalid.Code},
	}
	for _, tt := range errCases {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeUserRepo()
			svc, mail, policy, _ := newUserSvc(repo, nil)
			tt.setup(repo, mail, policy)
			before := len(repo.users)
			if _, err := svc.Register(ctx, tt.req, regMeta); codeOf(err) != tt.wantCode {
				t.Fatalf("期望 %d，实际 %v", tt.wantCode, err)
			}
			if len(repo.users) != before || len(repo.credits) != 0 {
				t.Fatal("被拒绝时不应写入任何数据")
			}
		})
	}

	t.Run("用户名 / 邮箱冲突（验证码通过后）分别返回 20002 与 53002", func(t *testing.T) {
		repo := newFakeUserRepo()
		repo.seed(model.User{Username: "root", Email: "root@x.com"})
		svc, mail, _, _ := newUserSvc(repo, nil)
		mail.enabled = true
		send := func(email string) string {
			t.Helper()
			mail.sent = nil
			if err := svc.SendRegisterCode(ctx, email, "5.5.5.5"); err != nil {
				t.Fatal(err)
			}
			return mail.lastCode()
		}
		if _, err := svc.Register(ctx, regReq("root", "new@x.com", send("new@x.com")), regMeta); codeOf(err) != errcode.ErrUserExists.Code {
			t.Fatalf("用户名重复应 20002：%v", err)
		}
		// 发码之后、注册之前，别人用同一个邮箱（大小写不同）抢先注册了
		code := send("dup@x.com")
		repo.seed(model.User{Username: "other", Email: "dup@x.com"})
		if _, err := svc.Register(ctx, regReq("fresh", "DUP@x.com", code), regMeta); codeOf(err) != errcode.ErrEmailExists.Code {
			t.Fatalf("邮箱重复（大小写不敏感）应 53002：%v", err)
		}
	})

	t.Run("验证码一次性：同一个码不能注册两次", func(t *testing.T) {
		repo := newFakeUserRepo()
		repo.seed(model.User{Username: "root"})
		svc, mail, _, _ := newUserSvc(repo, nil)
		mail.enabled = true
		_ = svc.SendRegisterCode(ctx, "b@x.com", "1.1.1.1")
		code := mail.lastCode()
		if _, err := svc.Register(ctx, regReq("bob", "b@x.com", code), regMeta); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Register(ctx, regReq("bob2", "b@x.com", code), regMeta); codeOf(err) != errcode.ErrCodeInvalid.Code {
			t.Fatalf("重放应 53003：%v", err)
		}
	})

	t.Run("验证码 10 分钟过期", func(t *testing.T) {
		repo := newFakeUserRepo()
		repo.seed(model.User{Username: "root"})
		clock := &testClock{t: time.Now()}
		svc, mail, _, _ := newUserSvc(repo, func(d *UserDeps) { d.Codes = cache.NewMemoryRegisterCodeStore(clock.now) })
		mail.enabled = true
		_ = svc.SendRegisterCode(ctx, "b@x.com", "1.1.1.1")
		code := mail.lastCode()
		clock.add(10*time.Minute + time.Second)
		if _, err := svc.Register(ctx, regReq("bob", "b@x.com", code), regMeta); codeOf(err) != errcode.ErrCodeInvalid.Code {
			t.Fatalf("过期应 53003：%v", err)
		}
	})

	t.Run("验证码错误 5 次后作废，之后正确码也不行", func(t *testing.T) {
		repo := newFakeUserRepo()
		repo.seed(model.User{Username: "root"})
		svc, mail, _, _ := newUserSvc(repo, nil)
		mail.enabled = true
		_ = svc.SendRegisterCode(ctx, "b@x.com", "1.1.1.1")
		code := mail.lastCode()
		wrong := "000000"
		if code == wrong {
			wrong = "111111"
		}
		for range 5 {
			if _, err := svc.Register(ctx, regReq("bob", "b@x.com", wrong), regMeta); codeOf(err) != errcode.ErrCodeInvalid.Code {
				t.Fatalf("错误码应 53003：%v", err)
			}
		}
		if _, err := svc.Register(ctx, regReq("bob", "b@x.com", code), regMeta); codeOf(err) != errcode.ErrCodeInvalid.Code {
			t.Fatalf("次数用尽后正确码也应 53003：%v", err)
		}
	})

	t.Run("竞态兜底：没启用邮件服务，预检时表为空但进入事务时已有人注册 -> 仍可注册为普通用户", func(t *testing.T) {
		repo := newFakeUserRepo()
		hook := &racingRepo{fakeUserRepo: repo}
		svc, _, _, _ := newUserSvc(repo, func(d *UserDeps) { d.Repo = hook })
		_, err := svc.Register(ctx, regReq("late", "late@x.com", ""), regMeta)
		if err != nil {
			t.Fatalf("没启用邮件服务时不需要验证邮箱，竞态后仍应按普通用户注册成功：%v", err)
		}
		if len(repo.users) != 2 || repo.users[0].Username != "winner" || repo.users[1].Role != model.RoleUser {
			t.Fatalf("应多出一个普通用户：%+v", repo.users)
		}
	})

	t.Run("竞态兜底：配了 SMTP，预检时表为空（没带码），事务里已非空 -> 验证码错误", func(t *testing.T) {
		repo := newFakeUserRepo()
		hook := &racingRepo{fakeUserRepo: repo}
		svc, mail, _, _ := newUserSvc(repo, func(d *UserDeps) { d.Repo = hook })
		mail.enabled = true
		if _, err := svc.Register(ctx, regReq("late", "late@x.com", ""), regMeta); codeOf(err) != errcode.ErrCodeInvalid.Code {
			t.Fatalf("应 53003：%v", err)
		}
	})

	t.Run("事务内写入失败透传内部错误", func(t *testing.T) {
		for _, op := range []string{"Create", "CreateCredit", "InsertLoginLog"} {
			repo := newFakeUserRepo()
			repo.errs[op] = errBoom
			svc, _, _, _ := newUserSvc(repo, nil)
			if _, err := svc.Register(ctx, regReq("alice", "a@x.com", ""), regMeta); !errors.Is(err, errBoom) {
				t.Fatalf("%s 失败应透传：%v", op, err)
			}
		}
	})
}

// racingRepo 在预检（非事务）Count 时返回 0，进入事务后返回真实数量，并在 LockRegistration 时塞进一个“抢先注册”的用户。
type racingRepo struct {
	*fakeUserRepo
	precheckDone bool
}

func (r *racingRepo) Count(ctx context.Context) (int64, error) {
	if !r.precheckDone {
		r.precheckDone = true
		return 0, nil
	}
	return r.fakeUserRepo.Count(ctx)
}

func (r *racingRepo) WithTx(ctx context.Context, fn func(tx repository.UserTx) error) error {
	r.seed(model.User{Username: "winner"})
	return r.fakeUserRepo.WithTx(ctx, fn)
}
