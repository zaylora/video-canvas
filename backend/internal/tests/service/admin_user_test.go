package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
	. "video-canvas/internal/service"
)

// fakeSettingsRepo 实现 service.SystemSettingRepo。
type fakeSettingsRepo struct {
	kv     map[string]string
	getErr error
	setErr error
	by     uint64
}

func newFakeSettingsRepo() *fakeSettingsRepo { return &fakeSettingsRepo{kv: map[string]string{}} }

func (r *fakeSettingsRepo) GetAll(context.Context) (map[string]string, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	out := map[string]string{}
	for k, v := range r.kv {
		out[k] = v
	}
	return out, nil
}

func (r *fakeSettingsRepo) SetMany(_ context.Context, kv map[string]string, by uint64) error {
	if r.setErr != nil {
		return r.setErr
	}
	for k, v := range kv {
		r.kv[k] = v
	}
	r.by = by
	return nil
}

func newAdminUserSvc(repo *fakeUserRepo, audit *fakeAudit, settings *fakeSettingsRepo) *AdminUserService {
	return NewAdminUserService(repo, audit, NewSettingsService(settings, nil))
}

func TestAdminUserService_List(t *testing.T) {
	ctx := context.Background()
	five := 5
	rows := []model.AdminUserRow{
		{ID: 3, Username: "carol", Role: model.RoleUser, Status: model.UserStatusDisabled, MaxActiveTasks: &five, Balance: 10, Frozen: 4, HasCreditAccount: true, ActiveTasks: 2},
		{ID: 1, Username: "alice", Role: model.RoleUser, Status: model.UserStatusActive},
	}

	t.Run("补算可用积分与实际生效的并发上限（单用户覆盖 > 全局设置 > 代码常量）", func(t *testing.T) {
		repo := newFakeUserRepo()
		repo.rows = rows
		settings := newFakeSettingsRepo()
		svc := newAdminUserSvc(repo, &fakeAudit{}, settings)
		items, total, _, err := svc.List(ctx, &model.ListAdminUserReq{Q: " ali ", Status: "active", Role: "user", Page: 2, PageSize: 20})
		if err != nil || total != 2 || len(items) != 2 {
			t.Fatalf("%v %d %d", err, total, len(items))
		}
		if items[0].Available != 6 || items[0].EffectiveMaxActiveTasks != 5 {
			t.Fatalf("carol：可用=10-4，并发取单用户覆盖 5：%+v", items[0])
		}
		if items[1].EffectiveMaxActiveTasks != DefaultMaxActiveTasks || items[1].Available != 0 || items[1].HasCreditAccount {
			t.Fatalf("alice：无覆盖且库里没有设置，取代码常量：%+v", items[1])
		}
		if repo.lastF.Q != "ali" || repo.lastF.Status != "active" || repo.lastF.Role != "user" || repo.lastF.Offset != 20 || repo.lastF.Limit != 20 {
			t.Fatalf("筛选 / 分页参数没传对：%+v", repo.lastF)
		}
		settings.kv[model.SettingDefaultMaxActiveTasks] = "7"
		items, _, _, _ = svc.List(ctx, &model.ListAdminUserReq{})
		if items[1].EffectiveMaxActiveTasks != 7 {
			t.Fatalf("系统设置应优先于代码常量：%+v", items[1])
		}
	})

	t.Run("分页修正：page<1 取 1，page_size 默认 10，最大 100", func(t *testing.T) {
		repo := newFakeUserRepo()
		svc := newAdminUserSvc(repo, &fakeAudit{}, newFakeSettingsRepo())
		_, _, q, _ := svc.List(ctx, &model.ListAdminUserReq{Page: -3, PageSize: 0})
		if q.Page != 1 || q.PageSize != 10 {
			t.Fatalf("%+v", q)
		}
		_, _, q, _ = svc.List(ctx, &model.ListAdminUserReq{Page: 1, PageSize: 500})
		if q.PageSize != 100 {
			t.Fatalf("page_size 应封顶 100：%+v", q)
		}
	})

	t.Run("空结果返回非 nil 切片", func(t *testing.T) {
		svc := newAdminUserSvc(newFakeUserRepo(), &fakeAudit{}, newFakeSettingsRepo())
		items, _, _, err := svc.List(ctx, &model.ListAdminUserReq{})
		if err != nil || items == nil {
			t.Fatalf("%v %v", items, err)
		}
	})

	t.Run("仓储 / 设置读取失败透传", func(t *testing.T) {
		repo := newFakeUserRepo()
		repo.errs["ListAdmin"] = errBoom
		if _, _, _, err := newAdminUserSvc(repo, &fakeAudit{}, newFakeSettingsRepo()).List(ctx, &model.ListAdminUserReq{}); !errors.Is(err, errBoom) {
			t.Fatalf("%v", err)
		}
		s := newFakeSettingsRepo()
		s.getErr = errBoom
		if _, _, _, err := newAdminUserSvc(newFakeUserRepo(), &fakeAudit{}, s).List(ctx, &model.ListAdminUserReq{}); !errors.Is(err, errBoom) {
			t.Fatalf("%v", err)
		}
	})
}

func TestAdminUserService_Get(t *testing.T) {
	ctx := context.Background()
	repo := newFakeUserRepo()
	verified := time.Now()
	u := repo.seed(model.User{Username: "alice", Email: "a@x.com", EmailVerifiedAt: &verified})
	repo.rows = []model.AdminUserRow{{ID: u.ID, Username: "alice", Balance: 9, Frozen: 1, HasCreditAccount: true}}
	repo.stats = &model.UserTaskStats{Total: 5, Success: 3, Failed: 1, Last7d: 2, SpentCredits: 12}
	audit := &fakeAudit{recent: []model.AdminAuditView{{ActorID: 1, ActorName: "root", Action: model.AdminAuditUserBan}}}
	svc := newAdminUserSvc(repo, audit, newFakeSettingsRepo())

	t.Run("成功：合并列表项、邮箱验证时间、任务统计与最近审计", func(t *testing.T) {
		d, err := svc.Get(ctx, u.ID)
		if err != nil {
			t.Fatal(err)
		}
		if d.Available != 8 || d.EmailVerifiedAt == nil || d.Total != 5 || d.Success != 3 || d.Failed != 1 || d.Last7d != 2 || d.SpentCredits != 12 {
			t.Fatalf("详情不对：%+v", d)
		}
		if len(d.RecentAudits) != 1 || d.RecentAudits[0].ActorName != "root" {
			t.Fatalf("最近审计不对：%+v", d.RecentAudits)
		}
	})
	t.Run("无审计时 recent_audits 是空数组而不是 null", func(t *testing.T) {
		d, err := newAdminUserSvc(repo, &fakeAudit{}, newFakeSettingsRepo()).Get(ctx, u.ID)
		if err != nil || d.RecentAudits == nil {
			t.Fatalf("%+v %v", d, err)
		}
	})
	t.Run("用户不存在返回 ErrUserNotFound", func(t *testing.T) {
		if _, err := svc.Get(ctx, 999); codeOf(err) != errcode.ErrUserNotFound.Code {
			t.Fatalf("%v", err)
		}
	})
	t.Run("统计失败透传", func(t *testing.T) {
		r2 := newFakeUserRepo()
		r2.seed(model.User{Username: "x"})
		r2.rows = []model.AdminUserRow{{ID: 1}}
		r2.errs["TaskStats"] = repository.ErrNotFound
		if _, err := newAdminUserSvc(r2, &fakeAudit{}, newFakeSettingsRepo()).Get(ctx, 1); !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("%v", err)
		}
	})
}
