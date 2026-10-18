package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/repository"
	. "video-canvas/internal/service"
)

// fakeAuditListRepo 是内存版 AuditLogRepo：记录收到的筛选条件，按注入的数据回放。
type fakeAuditListRepo struct {
	rows  []model.AuditRow
	names map[uint64]string
	errs  map[string]error
	last  repository.AuditFilter
	total int64
	asked []uint64
}

func (f *fakeAuditListRepo) ListMerged(_ context.Context, fl repository.AuditFilter) ([]model.AuditRow, error) {
	f.last = fl
	return f.rows, f.errs["ListMerged"]
}

func (f *fakeAuditListRepo) CountMerged(_ context.Context, fl repository.AuditFilter) (int64, error) {
	return f.total, f.errs["CountMerged"]
}

func (f *fakeAuditListRepo) UsernamesByIDs(_ context.Context, ids []uint64) (map[uint64]string, error) {
	f.asked = ids
	return f.names, f.errs["UsernamesByIDs"]
}

func newAuditSvc(repo *fakeAuditListRepo) *AuditLogService {
	return NewAuditLogService(repo, WithAuditClock(func() time.Time { return usageNow }))
}

func TestAuditLogService_List_Filters(t *testing.T) {
	ctx := context.Background()
	list := func(repo *fakeAuditListRepo, req model.ListAuditReq) error {
		_, err := newAuditSvc(repo).List(ctx, &req)
		return err
	}

	t.Run("默认范围：现在往前 30 天，第 1 页每页 20 条", func(t *testing.T) {
		repo := &fakeAuditListRepo{}
		if err := list(repo, model.ListAuditReq{}); err != nil {
			t.Fatal(err)
		}
		f := repo.last
		if !f.To.Equal(usageNow) || !f.From.Equal(usageNow.Add(-30*24*time.Hour)) || f.Limit != 20 || f.Offset != 0 {
			t.Fatalf("默认筛选不对：%+v", f)
		}
	})

	t.Run("时间参数：只给 to 时 from 取 to 往前 30 天；格式错误 / from 不早于 to 是参数错误", func(t *testing.T) {
		repo := &fakeAuditListRepo{}
		to := usageNow.Add(-24 * time.Hour)
		if err := list(repo, model.ListAuditReq{To: to.Format(time.RFC3339)}); err != nil {
			t.Fatal(err)
		}
		if !repo.last.To.Equal(to) || !repo.last.From.Equal(to.Add(-30*24*time.Hour)) {
			t.Fatalf("只给 to：%+v", repo.last)
		}
		for name, r := range map[string]model.ListAuditReq{
			"from 格式错误": {From: "x"}, "to 格式错误": {To: "x"},
			"from 等于 to": {From: usageNow.Format(time.RFC3339), To: usageNow.Format(time.RFC3339)},
		} {
			assertInvalid(t, list(&fakeAuditListRepo{}, r))
			_ = name
		}
		// 审计变更频率低，不限制最大跨度（前端有「近 90 天」档位）
		if err := list(repo, model.ListAuditReq{From: usageNow.Add(-400 * 24 * time.Hour).Format(time.RFC3339)}); err != nil {
			t.Fatalf("审计不限制最大跨度：%v", err)
		}
	})

	t.Run("操作人：数字按 id，「系统」「system」按 0，其余按用户名片段；空白忽略", func(t *testing.T) {
		for q, want := range map[string]struct {
			id *uint64
			kw string
		}{
			"12": {u64(12), ""}, " 12 ": {u64(12), ""}, "系统": {u64(0), ""}, "System": {u64(0), ""},
			"ops": {nil, "ops"}, "  ops  ": {nil, "ops"}, "   ": {nil, ""}, "": {nil, ""},
		} {
			repo := &fakeAuditListRepo{}
			if err := list(repo, model.ListAuditReq{Actor: q}); err != nil {
				t.Fatal(err)
			}
			f := repo.last
			if (f.ActorID == nil) != (want.id == nil) || (f.ActorID != nil && *f.ActorID != *want.id) || f.ActorKeyword != want.kw {
				t.Fatalf("actor=%q：期望 id=%v kw=%q，实际 %+v", q, want.id, want.kw, f)
			}
		}
	})

	t.Run("模块变成动作前缀；完整动作原样传递", func(t *testing.T) {
		repo := &fakeAuditListRepo{}
		if err := list(repo, model.ListAuditReq{Module: "channel", Action: "channel.enable"}); err != nil {
			t.Fatal(err)
		}
		if repo.last.ActionPrefix != "channel." || repo.last.Action != "channel.enable" {
			t.Fatalf("%+v", repo.last)
		}
	})

	t.Run("分页修正：page_size 超过 100 按 100，缺省 20；page 小于 1 按 1；offset = (page-1)*size", func(t *testing.T) {
		repo := &fakeAuditListRepo{}
		for _, c := range []struct{ page, size, wantLimit, wantOffset int }{
			{0, 0, 20, 0}, {1, 1000, 100, 0}, {4, 25, 25, 75}, {-1, 5, 5, 0},
		} {
			if err := list(repo, model.ListAuditReq{Page: c.page, PageSize: c.size}); err != nil {
				t.Fatal(err)
			}
			if repo.last.Limit != c.wantLimit || repo.last.Offset != c.wantOffset {
				t.Fatalf("page=%d size=%d：期望 limit=%d offset=%d，实际 %d/%d", c.page, c.size, c.wantLimit, c.wantOffset, repo.last.Limit, repo.last.Offset)
			}
		}
	})

	t.Run("仓储错误原样返回", func(t *testing.T) {
		boom := errors.New("boom")
		for _, m := range []string{"ListMerged", "CountMerged", "UsernamesByIDs"} {
			repo := &fakeAuditListRepo{errs: map[string]error{m: boom}, rows: []model.AuditRow{{Source: "admin", ID: 1, ActorID: 1}}}
			if err := list(repo, model.ListAuditReq{}); !errors.Is(err, boom) {
				t.Fatalf("%s 的错误应原样返回：%v", m, err)
			}
		}
	})
}

func u64(n uint64) *uint64 { return &n }

func TestAuditLogService_List_Page(t *testing.T) {
	ctx := context.Background()
	t0 := usageNow.Add(-time.Hour).Truncate(time.Microsecond).Add(123 * time.Microsecond)
	rows := []model.AuditRow{
		{Source: "ai", ID: 5, ActorID: 0, Action: "channel.auto_off", TargetType: "channel", TargetRef: "newapi", DetailJSON: model.JSONText(`{"failures":5}`), CreatedAt: t0},
		{Source: "admin", ID: 9, ActorID: 1, Action: "user.ban", TargetType: "user", TargetRef: "7", CreatedAt: t0},
		{Source: "admin", ID: 8, ActorID: 2, Action: "settings.smtp", TargetType: "settings", TargetRef: "0", CreatedAt: t0.Add(-time.Minute)},
	}
	repo := &fakeAuditListRepo{rows: rows, total: 57, names: map[uint64]string{1: "root", 2: "ops", 7: "dora"}}
	page, err := newAuditSvc(repo).List(ctx, &model.ListAuditReq{Page: 2, PageSize: 3})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 57 || page.Page != 2 || page.PageSize != 3 || len(page.List) != 3 {
		t.Fatalf("应回显总数与修正后的页码 / 每页条数：%+v", page)
	}
	sys, ban := page.List[0], page.List[1]
	if sys.ActorName != "" || sys.TargetName != "" || sys.TargetRef != "newapi" || sys.Source != "ai" || string(sys.DetailJSON) != `{"failures":5}` {
		t.Fatalf("系统动作映射不对：%+v", sys)
	}
	if ban.ActorName != "root" || ban.TargetName != "dora" || ban.TargetRef != "7" {
		t.Fatalf("admin 来源的用户对象应解析出用户名：%+v", ban)
	}
	// 只查本页要展示的用户：操作人 1（0 是系统，不查）与目标用户 7；ai 来源的 target_ref 是 key，不当用户 id
	asked := map[uint64]bool{}
	for _, id := range repo.asked {
		asked[id] = true
	}
	if len(asked) != 3 || !asked[1] || !asked[2] || !asked[7] {
		t.Fatalf("应只查 1、2 和 7：%v", repo.asked)
	}
	// 用户已删除时名字为空；空结果 List 是空数组而不是 nil
	repo.names = map[uint64]string{}
	page, _ = newAuditSvc(repo).List(ctx, &model.ListAuditReq{})
	if page.List[1].ActorName != "" {
		t.Fatalf("查不到的用户名为空：%+v", page.List[1])
	}
	repo.rows, repo.total = nil, 0
	if page, _ = newAuditSvc(repo).List(ctx, &model.ListAuditReq{}); page.List == nil || page.Total != 0 {
		t.Fatalf("空结果 List 必须是空数组而不是 nil：%+v", page)
	}
}
