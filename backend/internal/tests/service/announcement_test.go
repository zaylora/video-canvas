package service_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	. "video-canvas/internal/service"
	"video-canvas/internal/service/announcementfake"
)

// 本文件测试首页公告服务（设计：docs/design/公告/公告.md §8）。仓储用内存 fake，时钟可控。

const annActor = uint64(7)

// annNow 是测试的「当前时间」。
var annNow = time.Date(2026, 11, 12, 20, 0, 0, 0, time.UTC)

// annAudit 实现 AdminAuditWriter，记录审计条目。
type annAudit struct {
	mu   sync.Mutex
	logs []model.AdminAuditLog
}

func (a *annAudit) Insert(_ context.Context, l *model.AdminAuditLog) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.logs = append(a.logs, *l)
	return nil
}

func (a *annAudit) RecentForUser(context.Context, uint64, int) ([]model.AdminAuditView, error) {
	return nil, nil
}

func (a *annAudit) actions() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []string
	for _, l := range a.logs {
		out = append(out, l.Action)
	}
	return out
}

type annEnv struct {
	svc   *AnnouncementService
	repo  *announcementfake.Repo
	audit *annAudit
	clock time.Time // 仓储写 updated_at 用的时钟，每次写入自增 1 秒，制造不同的版本号
}

func newAnnEnv() *annEnv {
	e := &annEnv{repo: &announcementfake.Repo{}, audit: &annAudit{}, clock: annNow.Add(-time.Hour)}
	e.repo.Clock = func() time.Time {
		e.clock = e.clock.Add(time.Second)
		return e.clock
	}
	e.svc = NewAnnouncementService(AnnouncementDeps{Repo: e.repo, Audit: e.audit, Now: func() time.Time { return annNow }})
	return e
}

func annBool(b bool) *bool { return &b }

func annAt(d time.Duration) *time.Time {
	t := annNow.Add(d)
	return &t
}

// annReq 返回一份合法的请求，按需改字段。
func annReq(title string) *model.SaveAnnouncementReq {
	return &model.SaveAnnouncementReq{Title: title, Enabled: annBool(true)}
}

func (e *annEnv) create(t *testing.T, req *model.SaveAnnouncementReq) *model.AnnouncementAdminItem {
	t.Helper()
	it, err := e.svc.Create(context.Background(), annActor, req)
	if err != nil {
		t.Fatalf("新建公告失败：%v", err)
	}
	return it
}

// annCode 断言 err 是指定业务码。
func annCode(t *testing.T, err error, want int) {
	t.Helper()
	var e *errcode.Error
	if !errors.As(err, &e) || e.Code != want {
		t.Fatalf("期望业务码 %d，实际 %v", want, err)
	}
}

func publicTitles(t *testing.T, s *AnnouncementService) []string {
	t.Helper()
	v, err := s.Public(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if v.Items == nil {
		t.Fatal("items 不能为 nil")
	}
	var out []string
	for _, it := range v.Items {
		out = append(out, it.Title)
	}
	return out
}

func TestAnnouncementService_PublicWindow(t *testing.T) {
	e := newAnnEnv()
	mk := func(title string, enabled bool, starts, ends *time.Time) {
		r := annReq(title)
		r.Enabled, r.StartsAt, r.EndsAt = annBool(enabled), starts, ends
		e.create(t, r)
	}
	mk("不限时", true, nil, nil)
	mk("开始时间恰好是现在", true, annAt(0), nil)
	mk("结束时间恰好是现在", true, nil, annAt(0))
	mk("还没开始", true, annAt(time.Minute), nil)
	mk("已经结束", true, annAt(-2*time.Hour), annAt(-time.Hour))
	mk("停用", false, nil, nil)
	mk("窗口内", true, annAt(-time.Hour), annAt(time.Hour))

	got := strings.Join(publicTitles(t, e.svc), ",")
	if got != "不限时,开始时间恰好是现在,窗口内" {
		t.Fatalf("生效判断不对：%s", got)
	}
}

func TestAnnouncementService_PublicOrderAndLimit(t *testing.T) {
	e := newAnnEnv()
	for _, title := range []string{"a", "b", "c", "d", "e", "f"} {
		e.create(t, annReq(title))
	}

	got := strings.Join(publicTitles(t, e.svc), ",")
	if got != "a,b,c,d,e" {
		t.Fatalf("应按排序、最多 5 条：%s", got)
	}

	v, _ := e.svc.Public(context.Background())
	first := v.Items[0]
	stored, _ := e.repo.GetByID(context.Background(), first.ID)
	if first.Version != stored.UpdatedAt.UnixMilli() || first.Version == 0 {
		t.Fatalf("version 应等于 updated_at 毫秒：%d vs %d", first.Version, stored.UpdatedAt.UnixMilli())
	}
}

func TestAnnouncementService_PublicEmpty(t *testing.T) {
	e := newAnnEnv()
	if got := publicTitles(t, e.svc); len(got) != 0 {
		t.Fatalf("没有公告应返回空数组：%v", got)
	}
}

func TestAnnouncementService_Validation(t *testing.T) {
	long := func(n int) string { return strings.Repeat("字", n) }
	cases := []struct {
		name string
		mod  func(r *model.SaveAnnouncementReq)
		ok   bool
	}{
		{"标题 60 个汉字可以", func(r *model.SaveAnnouncementReq) { r.Title = long(60) }, true},
		{"标题 61 个汉字超长", func(r *model.SaveAnnouncementReq) { r.Title = long(61) }, false},
		{"标题全是空白", func(r *model.SaveAnnouncementReq) { r.Title = "   " }, false},
		{"结束早于开始", func(r *model.SaveAnnouncementReq) { r.StartsAt, r.EndsAt = annAt(time.Hour), annAt(0) }, false},
		{"结束等于开始", func(r *model.SaveAnnouncementReq) { r.StartsAt, r.EndsAt = annAt(0), annAt(0) }, false},
		{"已过期的时间也能保存", func(r *model.SaveAnnouncementReq) { r.EndsAt = annAt(-time.Hour) }, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newAnnEnv()
			req := annReq("标题")
			c.mod(req)
			_, err := e.svc.Create(context.Background(), annActor, req)
			if c.ok && err != nil {
				t.Fatalf("应通过，实际 %v", err)
			}
			if !c.ok {
				annCode(t, err, errcode.ErrAnnouncementInvalid.Code)
				if len(e.repo.Items) != 0 || len(e.audit.actions()) != 0 {
					t.Fatal("校验失败不应写库或写审计")
				}
			}
		})
	}

	t.Run("缺 enabled 报参数错误", func(t *testing.T) {
		e := newAnnEnv()
		_, err := e.svc.Create(context.Background(), annActor, &model.SaveAnnouncementReq{Title: "x"})
		annCode(t, err, errcode.ErrInvalidParams.Code)
	})
}

func TestAnnouncementService_Create(t *testing.T) {
	e := newAnnEnv()
	first := e.create(t, annReq("第一条"))
	req := &model.SaveAnnouncementReq{
		Title:   "  Seedance 2.5 已上线  ",
		Enabled: annBool(true), StartsAt: annAt(-time.Hour), EndsAt: annAt(time.Hour),
	}
	it := e.create(t, req)

	if it.Title != "Seedance 2.5 已上线" {
		t.Fatalf("应去掉首尾空白：%+v", it)
	}
	if it.Sort != first.Sort+1 || !it.Enabled || it.Status != model.AnnouncementStatusLive {
		t.Fatalf("新公告应排最后、状态展示中：%+v", it)
	}
	stored, _ := e.repo.GetByID(context.Background(), it.ID)
	if stored.CreatedBy != annActor || stored.UpdatedBy != annActor {
		t.Fatalf("应记录创建人与修改人：%+v", stored)
	}
	if got := e.audit.actions(); len(got) != 2 || got[1] != model.AdminAuditAnnouncementCreate {
		t.Fatalf("应写创建审计：%v", got)
	}
	if e.audit.logs[1].TargetType != model.AdminAuditTargetAnnounce || e.audit.logs[1].TargetID != it.ID {
		t.Fatalf("审计目标不对：%+v", e.audit.logs[1])
	}
	if strings.Contains(string(e.audit.logs[1].DetailJSON), "详情") {
		t.Fatal("审计不应记录正文")
	}
}

func TestAnnouncementService_Update(t *testing.T) {
	t.Run("整体替换并刷新版本", func(t *testing.T) {
		e := newAnnEnv()
		r := annReq("旧标题")
		r.EndsAt = annAt(time.Hour)
		it := e.create(t, r)
		before, _ := e.repo.GetByID(context.Background(), it.ID)

		got, err := e.svc.Update(context.Background(), 9, it.ID, &model.SaveAnnouncementReq{Title: "新标题", Enabled: annBool(false)})
		if err != nil {
			t.Fatal(err)
		}
		if got.Title != "新标题" || got.EndsAt != nil || got.Enabled || got.Status != model.AnnouncementStatusDisabled {
			t.Fatalf("没传的字段应被清空：%+v", got)
		}
		after, _ := e.repo.GetByID(context.Background(), it.ID)
		if !after.UpdatedAt.After(before.UpdatedAt) || after.UpdatedBy != 9 || after.Sort != before.Sort {
			t.Fatalf("应刷新 updated_at、记修改人、保持排序：%+v", after)
		}
		if a := e.audit.actions(); a[len(a)-1] != model.AdminAuditAnnouncementUpdate {
			t.Fatalf("应写修改审计：%v", a)
		}
	})

	t.Run("不存在返回 56001", func(t *testing.T) {
		e := newAnnEnv()
		_, err := e.svc.Update(context.Background(), annActor, 99, annReq("x"))
		annCode(t, err, errcode.ErrAnnouncementNotFound.Code)
	})

	t.Run("校验失败不改库", func(t *testing.T) {
		e := newAnnEnv()
		it := e.create(t, annReq("原样"))
		_, err := e.svc.Update(context.Background(), annActor, it.ID, annReq(""))
		annCode(t, err, errcode.ErrAnnouncementInvalid.Code)
		stored, _ := e.repo.GetByID(context.Background(), it.ID)
		if stored.Title != "原样" {
			t.Fatal("校验失败不应改库")
		}
	})
}

func TestAnnouncementService_SetEnabled(t *testing.T) {
	e := newAnnEnv()
	it := e.create(t, annReq("开关"))
	before, _ := e.repo.GetByID(context.Background(), it.ID)

	got, err := e.svc.SetEnabled(context.Background(), annActor, it.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled || got.Status != model.AnnouncementStatusDisabled {
		t.Fatalf("应已停用：%+v", got)
	}
	after, _ := e.repo.GetByID(context.Background(), it.ID)
	if !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatal("启停不是内容变化，不应刷新版本")
	}
	if a := e.audit.actions(); a[len(a)-1] != model.AdminAuditAnnouncementEnable {
		t.Fatalf("应写启停审计：%v", a)
	}

	_, err = e.svc.SetEnabled(context.Background(), annActor, 99, true)
	annCode(t, err, errcode.ErrAnnouncementNotFound.Code)
}

func TestAnnouncementService_Delete(t *testing.T) {
	e := newAnnEnv()
	it := e.create(t, annReq("要删的"))
	if err := e.svc.Delete(context.Background(), annActor, it.ID); err != nil {
		t.Fatal(err)
	}
	v, _ := e.svc.AdminList(context.Background())
	if len(v.Items) != 0 {
		t.Fatalf("删除后不应再列出：%+v", v.Items)
	}
	if a := e.audit.actions(); a[len(a)-1] != model.AdminAuditAnnouncementDelete {
		t.Fatalf("应写删除审计：%v", a)
	}
	annCode(t, e.svc.Delete(context.Background(), annActor, it.ID), errcode.ErrAnnouncementNotFound.Code)
}

func TestAnnouncementService_Reorder(t *testing.T) {
	setup := func() (*annEnv, []uint64) {
		e := newAnnEnv()
		var ids []uint64
		for _, title := range []string{"a", "b", "c"} {
			ids = append(ids, e.create(t, annReq(title)).ID)
		}
		return e, ids
	}

	t.Run("按数组顺序重写排序，不刷新版本", func(t *testing.T) {
		e, ids := setup()
		before, _ := e.repo.GetByID(context.Background(), ids[0])
		if err := e.svc.Reorder(context.Background(), annActor, []uint64{ids[2], ids[0], ids[1]}); err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(publicTitles(t, e.svc), ","); got != "c,a,b" {
			t.Fatalf("顺序不对：%s", got)
		}
		after, _ := e.repo.GetByID(context.Background(), ids[0])
		if !after.UpdatedAt.Equal(before.UpdatedAt) {
			t.Fatal("排序不是内容变化，不应刷新版本")
		}
		if a := e.audit.actions(); a[len(a)-1] != model.AdminAuditAnnouncementOrder {
			t.Fatalf("应写排序审计：%v", a)
		}
	})

	for name, bad := range map[string]func(ids []uint64) []uint64{
		"少了一条": func(ids []uint64) []uint64 { return ids[:2] },
		"多了一条": func(ids []uint64) []uint64 { return append(append([]uint64(nil), ids...), 99) },
		"重复":   func(ids []uint64) []uint64 { return []uint64{ids[0], ids[0], ids[1]} },
	} {
		t.Run(name+"返回 56003", func(t *testing.T) {
			e, ids := setup()
			annCode(t, e.svc.Reorder(context.Background(), annActor, bad(ids)), errcode.ErrAnnouncementOrderMismatch.Code)
		})
	}

	t.Run("中途失败整体回滚", func(t *testing.T) {
		e, ids := setup()
		e.repo.FailSetSortAt = 2
		err := e.svc.Reorder(context.Background(), annActor, []uint64{ids[2], ids[1], ids[0]})
		if !errors.Is(err, announcementfake.ErrInjected) {
			t.Fatalf("应返回注入的故障：%v", err)
		}
		if got := strings.Join(publicTitles(t, e.svc), ","); got != "a,b,c" {
			t.Fatalf("应回滚到原顺序：%s", got)
		}
	})
}

func TestAnnouncementService_AdminStatus(t *testing.T) {
	e := newAnnEnv()
	mk := func(title string, mod func(r *model.SaveAnnouncementReq)) uint64 {
		r := annReq(title)
		if mod != nil {
			mod(r)
		}
		return e.create(t, r).ID
	}
	disabled := mk("停用", func(r *model.SaveAnnouncementReq) { r.Enabled = annBool(false) })
	scheduled := mk("待上线", func(r *model.SaveAnnouncementReq) { r.StartsAt = annAt(time.Hour) })
	expired := mk("已过期", func(r *model.SaveAnnouncementReq) { r.EndsAt = annAt(-time.Minute) })
	var live []uint64
	for _, title := range []string{"1", "2", "3", "4", "5", "6"} {
		live = append(live, mk(title, nil))
	}

	v, err := e.svc.AdminList(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	status := map[uint64]string{}
	for _, it := range v.Items {
		status[it.ID] = it.Status
	}
	want := map[uint64]string{
		disabled: model.AnnouncementStatusDisabled, scheduled: model.AnnouncementStatusScheduled, expired: model.AnnouncementStatusExpired,
		live[0]: model.AnnouncementStatusLive, live[4]: model.AnnouncementStatusLive,
		live[5]: model.AnnouncementStatusOverflow,
	}
	for id, s := range want {
		if status[id] != s {
			t.Fatalf("公告 %d 状态应为 %s，实际 %s（全部：%v）", id, s, status[id], status)
		}
	}
	if v.Items[0].ID != disabled {
		t.Fatal("后台列表应按排序升序")
	}
}

func TestAnnouncementService_RepoError(t *testing.T) {
	e := newAnnEnv()
	e.repo.Err = announcementfake.ErrInjected
	if _, err := e.svc.Public(context.Background()); !errors.Is(err, announcementfake.ErrInjected) {
		t.Fatalf("仓储错误应原样上抛（用 %%w 包装）：%v", err)
	}
}
