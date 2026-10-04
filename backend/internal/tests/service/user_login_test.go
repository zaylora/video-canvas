package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"video-canvas/internal/cache"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/utils"
	. "video-canvas/internal/service"
)

const testJWTSecret = "unit-test-secret"

func hashPw(t *testing.T, pw string) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return string(h)
}

// newUserSvc 组装用户服务：默认开放注册、SMTP 未配置、初始积分 20。
func newUserSvc(repo *fakeUserRepo, mutate func(*UserDeps)) (*UserService, *fakeMail, *fakePolicy, cache.RegisterCodeStore) {
	mail := &fakeMail{}
	policy := &fakePolicy{enabled: true, initial: 20}
	codes := cache.NewMemoryRegisterCodeStore(time.Now)
	deps := UserDeps{
		Repo: repo, Cache: noCache(), Codes: codes, Policy: policy, Mail: mail,
		JWTSecret: testJWTSecret, JWTIssuer: "test", JWTExpireHours: 1,
	}
	if mutate != nil {
		mutate(&deps)
	}
	return NewUserService(deps), mail, policy, codes
}

func TestUserService_Login(t *testing.T) {
	ctx := context.Background()
	meta := ClientMeta{IP: "9.9.9.9", UserAgent: "ua"}

	t.Run("成功：返回 token 与角色，更新 last_login_at，写 login/ok 记录，token 带 token_version", func(t *testing.T) {
		repo := newFakeUserRepo()
		u := repo.seed(model.User{Username: "alice", Password: hashPw(t, "secret1"), Role: model.RoleAdmin, TokenVersion: 3})
		svc, _, _, _ := newUserSvc(repo, nil)
		view, err := svc.Login(ctx, "alice", "secret1", meta)
		if err != nil {
			t.Fatal(err)
		}
		if view.Role != model.RoleAdmin || view.Token == "" || view.ExpireAt == 0 {
			t.Fatalf("返回体不对：%+v", view)
		}
		claims, err := utils.ParseToken(view.Token, testJWTSecret)
		if err != nil || claims.UserID != uint(u.ID) || claims.TokenVersion != 3 {
			t.Fatalf("token 载荷不对：%+v %v", claims, err)
		}
		if repo.users[0].LastLoginAt == nil {
			t.Fatal("应更新 last_login_at")
		}
		if got := repo.logResults(); len(got) != 1 || got[0] != "login/ok" || repo.logs[0].IP != "9.9.9.9" || repo.logs[0].UserID != u.ID {
			t.Fatalf("登录记录不对：%v %+v", got, repo.logs)
		}
	})

	tests := []struct {
		name     string
		seed     *model.User
		password string
		user     string
		wantCode int
		wantLog  string
	}{
		{"密码错误：统一返回凭证错误并写 badpw", &model.User{Username: "alice"}, "wrongpw", "alice", errcode.ErrInvalidCredential.Code, "login/badpw"},
		{"用户不存在：与密码错误同一个错误，记录 user_id=0", nil, "secret1", "ghost", errcode.ErrInvalidCredential.Code, "login/badpw"},
		{"账号已停用：返回 53004 并写 blocked", &model.User{Username: "alice", Status: model.UserStatusDisabled}, "secret1", "alice", errcode.ErrAccountDisabled.Code, "login/blocked"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeUserRepo()
			if tt.seed != nil {
				tt.seed.Password = hashPw(t, "secret1")
				repo.seed(*tt.seed)
			}
			svc, _, _, _ := newUserSvc(repo, nil)
			_, err := svc.Login(ctx, tt.user, tt.password, meta)
			var ec *errcode.Error
			if !errors.As(err, &ec) || ec.Code != tt.wantCode {
				t.Fatalf("期望错误码 %d，实际 %v", tt.wantCode, err)
			}
			if got := repo.logResults(); len(got) != 1 || got[0] != tt.wantLog {
				t.Fatalf("登录记录期望 %s，实际 %v", tt.wantLog, got)
			}
			if tt.seed == nil && repo.logs[0].UserID != 0 {
				t.Fatalf("用户不存在时 user_id 应为 0：%+v", repo.logs[0])
			}
			if len(repo.users) > 0 && repo.users[0].LastLoginAt != nil {
				t.Fatal("失败登录不应更新 last_login_at")
			}
		})
	}

	t.Run("写登录记录失败只记日志，不影响登录", func(t *testing.T) {
		repo := newFakeUserRepo()
		repo.seed(model.User{Username: "alice", Password: hashPw(t, "secret1")})
		repo.errs["InsertLoginLog"] = errBoom
		repo.errs["Update"] = errBoom
		svc, _, _, _ := newUserSvc(repo, nil)
		if _, err := svc.Login(ctx, "alice", "secret1", meta); err != nil {
			t.Fatalf("登录应成功：%v", err)
		}
	})

	t.Run("查询用户失败透传内部错误", func(t *testing.T) {
		repo := newFakeUserRepo()
		repo.errs["GetByUsername"] = errBoom
		svc, _, _, _ := newUserSvc(repo, nil)
		if _, err := svc.Login(ctx, "alice", "secret1", meta); !errors.Is(err, errBoom) {
			t.Fatalf("应透传：%v", err)
		}
	})
}

func TestUserService_State(t *testing.T) {
	ctx := context.Background()
	repo := newFakeUserRepo()
	u := repo.seed(model.User{Username: "alice", Role: model.RoleAdmin, Status: model.UserStatusDisabled, TokenVersion: 2})
	svc, _, _, _ := newUserSvc(repo, nil)

	got, err := svc.State(ctx, u.ID)
	if err != nil || got == nil || got.Role != model.RoleAdmin || got.Status != model.UserStatusDisabled || got.TokenVersion != 2 {
		t.Fatalf("状态不对：%+v %v", got, err)
	}
	if got, err := svc.State(ctx, 999); got != nil || err != nil {
		t.Fatalf("用户不存在应返回 (nil, nil)：%+v %v", got, err)
	}
	repo.errs["GetByID"] = errBoom
	if _, err := svc.State(ctx, u.ID); !errors.Is(err, errBoom) {
		t.Fatalf("库故障应透传：%v", err)
	}
}
