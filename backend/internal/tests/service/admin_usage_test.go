package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/datatypes"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
	. "video-canvas/internal/service"
)

// fakeUsageRepo 是内存版 AdminUsageRepo：记录收到的筛选条件，按注入的数据回放。
type fakeUsageRepo struct {
	rows      []model.UsageRow
	counts    []model.UsageStatusCount
	task      *model.GenerationTask
	user      *model.User
	events    []model.GenerationTaskEvent
	ledger    []model.CreditLedger
	errs      map[string]error
	lastList  repository.UsageFilter
	lastCount repository.UsageFilter
	total     int64
	lastSum   repository.UsageFilter
	lastRef   string
}

func (f *fakeUsageRepo) List(_ context.Context, fl repository.UsageFilter) ([]model.UsageRow, error) {
	f.lastList = fl
	return f.rows, f.errs["List"]
}

func (f *fakeUsageRepo) Count(_ context.Context, fl repository.UsageFilter) (int64, error) {
	f.lastCount = fl
	return f.total, f.errs["Count"]
}

func (f *fakeUsageRepo) Summary(_ context.Context, fl repository.UsageFilter) ([]model.UsageStatusCount, error) {
	f.lastSum = fl
	return f.counts, f.errs["Summary"]
}

func (f *fakeUsageRepo) GetTaskByRef(_ context.Context, ref string) (*model.GenerationTask, error) {
	f.lastRef = ref
	if err := f.errs["GetTaskByRef"]; err != nil {
		return nil, err
	}
	if f.task == nil {
		return nil, repository.ErrNotFound
	}
	return f.task, nil
}

func (f *fakeUsageRepo) GetUser(context.Context, uint64) (*model.User, error) {
	if err := f.errs["GetUser"]; err != nil {
		return nil, err
	}
	if f.user == nil {
		return nil, repository.ErrNotFound
	}
	return f.user, nil
}

func (f *fakeUsageRepo) ListEvents(context.Context, uint64) ([]model.GenerationTaskEvent, error) {
	return f.events, f.errs["ListEvents"]
}

func (f *fakeUsageRepo) ListTaskLedger(context.Context, uint64) ([]model.CreditLedger, error) {
	return f.ledger, f.errs["ListTaskLedger"]
}

var usageNow = time.Date(2026, 10, 18, 14, 30, 0, 0, time.UTC)

func newUsageSvc(repo *fakeUsageRepo) *AdminUsageService {
	return NewAdminUsageService(repo, WithUsageClock(func() time.Time { return usageNow }))
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

func assertInvalid(t *testing.T, err error) {
	t.Helper()
	var ec *errcode.Error
	if !errors.As(err, &ec) || ec.Code != errcode.ErrInvalidParams.Code {
		t.Fatalf("应返回 ErrInvalidParams，实际 %v", err)
	}
}

func TestAdminUsageService_List_Filters(t *testing.T) {
	ctx := context.Background()
	ref := idcodec.Encode(42)

	t.Run("默认范围：现在往前 7 天，不含试跑，第 1 页每页 20 条", func(t *testing.T) {
		repo := &fakeUsageRepo{}
		if _, err := newUsageSvc(repo).List(ctx, &model.ListUsageReq{}); err != nil {
			t.Fatal(err)
		}
		f := repo.lastList
		if !f.To.Equal(usageNow) || !f.From.Equal(usageNow.Add(-7*24*time.Hour)) || f.IncludeTest || f.Limit != 20 || f.Offset != 0 {
			t.Fatalf("默认筛选不对：%+v", f)
		}
	})

	t.Run("只给 from：to 取现在；只给 to：from 取 to 往前 7 天", func(t *testing.T) {
		repo := &fakeUsageRepo{}
		svc := newUsageSvc(repo)
		from := usageNow.Add(-24 * time.Hour)
		if _, err := svc.List(ctx, &model.ListUsageReq{UsageFilterReq: model.UsageFilterReq{From: from.Format(time.RFC3339)}}); err != nil {
			t.Fatal(err)
		}
		if !repo.lastList.From.Equal(from) || !repo.lastList.To.Equal(usageNow) {
			t.Fatalf("只给 from：%+v", repo.lastList)
		}
		to := usageNow.Add(-2 * 24 * time.Hour)
		if _, err := svc.List(ctx, &model.ListUsageReq{UsageFilterReq: model.UsageFilterReq{To: to.Format(time.RFC3339)}}); err != nil {
			t.Fatal(err)
		}
		if !repo.lastList.To.Equal(to) || !repo.lastList.From.Equal(to.Add(-7*24*time.Hour)) {
			t.Fatalf("只给 to：%+v", repo.lastList)
		}
	})

	t.Run("时间参数非法：格式错误、from 不早于 to、跨度超过 90 天", func(t *testing.T) {
		svc := newUsageSvc(&fakeUsageRepo{})
		at := func(d time.Duration) string { return usageNow.Add(d).Format(time.RFC3339) }
		for name, f := range map[string]model.UsageFilterReq{
			"from 格式错误":  {From: "yesterday"},
			"to 格式错误":    {To: "2026-10-18"},
			"from 等于 to": {From: at(0), To: at(0)},
			"from 晚于 to": {From: at(-time.Hour), To: at(-2 * time.Hour)},
			"跨度 91 天":    {From: at(-91 * 24 * time.Hour), To: at(0)},
		} {
			if _, err := svc.List(ctx, &model.ListUsageReq{UsageFilterReq: f}); err == nil {
				t.Fatalf("%s：应报参数错误", name)
			} else {
				assertInvalid(t, err)
			}
		}
		if _, err := svc.List(ctx, &model.ListUsageReq{UsageFilterReq: model.UsageFilterReq{From: at(-90 * 24 * time.Hour), To: at(0)}}); err != nil {
			t.Fatalf("恰好 90 天应允许：%v", err)
		}
	})

	t.Run("状态映射：all / 空不筛；active 是全部非终态；其余只筛自己", func(t *testing.T) {
		for status, want := range map[string][]string{
			"": nil, "all": nil, "active": model.ActiveTaskStatuses,
			"succeeded": {model.TaskSucceeded}, "failed": {model.TaskFailed},
			"canceled": {model.TaskCanceled}, "expired": {model.TaskExpired},
		} {
			repo := &fakeUsageRepo{}
			if _, err := newUsageSvc(repo).List(ctx, &model.ListUsageReq{UsageFilterReq: model.UsageFilterReq{Status: status}}); err != nil {
				t.Fatal(err)
			}
			if !sameStrings(repo.lastList.Statuses, want) {
				t.Fatalf("status=%q 应映射为 %v，实际 %v", status, want, repo.lastList.Statuses)
			}
		}
	})

	t.Run("关键字：32 位十六进制（不分大小写）按任务编号，否则按用户关键字（去首尾空格）", func(t *testing.T) {
		repo := &fakeUsageRepo{}
		svc := newUsageSvc(repo)
		upper := ""
		for _, c := range ref {
			if c >= 'a' && c <= 'f' {
				c -= 32
			}
			upper += string(c)
		}
		for _, q := range []string{ref, "  " + upper + " "} {
			if _, err := svc.List(ctx, &model.ListUsageReq{UsageFilterReq: model.UsageFilterReq{Q: q}}); err != nil {
				t.Fatal(err)
			}
			if repo.lastList.TaskRef != ref || repo.lastList.UserKeyword != "" {
				t.Fatalf("应按任务编号 %s 查：%+v", ref, repo.lastList)
			}
		}
		if _, err := svc.List(ctx, &model.ListUsageReq{UsageFilterReq: model.UsageFilterReq{Q: "  alice "}}); err != nil {
			t.Fatal(err)
		}
		if repo.lastList.UserKeyword != "alice" || repo.lastList.TaskRef != "" {
			t.Fatalf("应按用户关键字查：%+v", repo.lastList)
		}
		if _, err := svc.List(ctx, &model.ListUsageReq{UsageFilterReq: model.UsageFilterReq{Q: "   "}}); err != nil {
			t.Fatal(err)
		}
		if repo.lastList.UserKeyword != "" || repo.lastList.TaskRef != "" {
			t.Fatalf("纯空白应忽略：%+v", repo.lastList)
		}
	})

	t.Run("其余筛选原样传给仓储，含试跑开关", func(t *testing.T) {
		repo := &fakeUsageRepo{}
		_, err := newUsageSvc(repo).List(ctx, &model.ListUsageReq{UsageFilterReq: model.UsageFilterReq{UserID: 9, Kind: "video", Model: "kling-v2", IncludeTest: true}, PageSize: 5})
		if err != nil {
			t.Fatal(err)
		}
		f := repo.lastList
		if f.UserID != 9 || f.Kind != "video" || f.Model != "kling-v2" || !f.IncludeTest || f.Limit != 5 {
			t.Fatalf("筛选没有原样传递：%+v", f)
		}
	})

	t.Run("分页修正：page_size 超过 100 按 100，缺省 20；page 小于 1 按 1；offset = (page-1)*size", func(t *testing.T) {
		repo := &fakeUsageRepo{}
		svc := newUsageSvc(repo)
		for _, c := range []struct{ page, size, wantLimit, wantOffset int }{
			{0, 0, 20, 0}, {1, 1000, 100, 0}, {3, 10, 10, 20}, {-5, 10, 10, 0},
		} {
			if _, err := svc.List(ctx, &model.ListUsageReq{Page: c.page, PageSize: c.size}); err != nil {
				t.Fatal(err)
			}
			if repo.lastList.Limit != c.wantLimit || repo.lastList.Offset != c.wantOffset {
				t.Fatalf("page=%d size=%d：期望 limit=%d offset=%d，实际 %d/%d", c.page, c.size, c.wantLimit, c.wantOffset, repo.lastList.Limit, repo.lastList.Offset)
			}
		}
	})

	t.Run("仓储错误原样返回", func(t *testing.T) {
		boom := errors.New("boom")
		repo := &fakeUsageRepo{errs: map[string]error{"List": boom}}
		if _, err := newUsageSvc(repo).List(ctx, &model.ListUsageReq{}); !errors.Is(err, boom) {
			t.Fatalf("应透传仓储错误：%v", err)
		}
	})
}

func TestAdminUsageService_List_Page(t *testing.T) {
	ctx := context.Background()
	charged := 6
	dur := int64(4200)
	rows := []model.UsageRow{
		{ID: 30, TaskRef: "r30", UserID: 1, Username: "alice", Kind: "video", ModelKey: "m", ModelName: "可灵 v2", Status: model.TaskSucceeded, Credits: 8, ChargedCredits: &charged, DurationMs: &dur, CreatedAt: usageNow},
		{ID: 20, TaskRef: "r20", UserID: 2, Username: "bob", Kind: "image", ModelKey: "m2", ModelName: "m2", Status: model.TaskRunning, Credits: 8, IsTest: true, CreatedAt: usageNow},
		{ID: 10, TaskRef: "r10", UserID: 1, Username: "alice", Status: model.TaskFailed, ErrorCode: "plugin_error", ErrorMessage: "平台繁忙", CreatedAt: usageNow},
	}
	repo := &fakeUsageRepo{rows: rows, total: 41}
	page, err := newUsageSvc(repo).List(ctx, &model.ListUsageReq{Page: 2, PageSize: 3})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 41 || page.Page != 2 || page.PageSize != 3 || len(page.List) != 3 {
		t.Fatalf("应回显总数与修正后的页码 / 每页条数：%+v", page)
	}
	first := page.List[0]
	if first.User.ID != 1 || first.User.Username != "alice" || first.ModelName != "可灵 v2" || *first.ChargedCredits != 6 || *first.DurationMs != 4200 || first.TaskRef != "r30" {
		t.Fatalf("行映射不对：%+v", first)
	}
	if !page.List[1].IsTest || page.List[1].ChargedCredits != nil || page.List[1].DurationMs != nil {
		t.Fatalf("进行中的试跑任务映射不对：%+v", page.List[1])
	}
	// 总数与列表共用同一份筛选条件
	if repo.lastCount.UserID != repo.lastList.UserID || !repo.lastCount.From.Equal(repo.lastList.From) {
		t.Fatalf("总数与列表应使用同一份筛选：%+v vs %+v", repo.lastCount, repo.lastList)
	}
	// 空结果：List 是空数组而不是 nil，总数 0
	repo.rows, repo.total = nil, 0
	if page, _ = newUsageSvc(repo).List(ctx, &model.ListUsageReq{}); page.List == nil || page.Total != 0 {
		t.Fatalf("空结果 List 必须是空数组而不是 nil：%+v", page)
	}
}

func TestAdminUsageService_Summary(t *testing.T) {
	ctx := context.Background()
	repo := &fakeUsageRepo{counts: []model.UsageStatusCount{
		{Status: model.TaskSucceeded, Count: 5, Charged: 40},
		{Status: model.TaskFailed, Count: 2},
		{Status: model.TaskCanceled, Count: 1},
		{Status: model.TaskExpired, Count: 3},
		{Status: model.TaskRunning, Count: 2},
		{Status: model.TaskPending, Count: 1},
	}}
	got, err := newUsageSvc(repo).Summary(ctx, &model.UsageFilterReq{UserID: 3, Status: "failed"})
	if err != nil {
		t.Fatal(err)
	}
	want := model.UsageSummary{Total: 14, Succeeded: 5, Failed: 2, Canceled: 1, Expired: 3, Active: 3, ChargedCredits: 40}
	if *got != want {
		t.Fatalf("摘要折叠不对：%+v 期望 %+v", *got, want)
	}
	if repo.lastSum.UserID != 3 || !sameStrings(repo.lastSum.Statuses, []string{model.TaskFailed}) {
		t.Fatalf("摘要应与列表用同一组筛选：%+v", repo.lastSum)
	}
	_, err = newUsageSvc(repo).Summary(ctx, &model.UsageFilterReq{From: "bad"})
	assertInvalid(t, err)
}

func TestAdminUsageService_Get(t *testing.T) {
	ctx := context.Background()
	created := usageNow.Add(-time.Hour)
	canvas := uint64(77)
	charged := 0
	task := &model.GenerationTask{
		ID: 9, TaskRef: idcodec.Encode(9), UserID: 1, CanvasProjectID: &canvas, NodeID: "node-2",
		Kind: model.KindVideo, ModelKey: "kling-v2", Status: model.TaskFailed, Credits: 8, ChargedCredits: &charged,
		ErrorCode: "plugin_error", ErrorMessage: "平台繁忙", CreatedAt: created, DeadlineAt: created.Add(30 * time.Minute),
		InputJSON:      datatypes.JSON(`{"prompt":"海边日落","ratio":"16:9"}`),
		OutputJSON:     datatypes.JSON(`[{"asset_id":5,"url":"/files/a","media_type":"image","width":10,"height":20}]`),
		ConfigSnapshot: datatypes.JSON(`{"model":{"label":"可灵 v2"},"channel":{"key":"kling-official"},"plugin":{"key":"kling","version":"1.2.0"}}`),
	}
	newRepo := func(mut func(*fakeUsageRepo)) *fakeUsageRepo {
		cp := *task
		repo := &fakeUsageRepo{
			task: &cp,
			user: &model.User{BaseModel: model.BaseModel{ID: 1}, Username: "alice", Email: "a@x.com"},
			events: []model.GenerationTaskEvent{
				{ID: 1, TaskID: 9, Event: model.TaskEventCreated, ToStatus: model.TaskPending, CreatedAt: created},
				{ID: 2, TaskID: 9, Event: model.TaskEventSubmitted, FromStatus: model.TaskPending, ToStatus: model.TaskQueued, DetailJSON: datatypes.JSON(`{"provider_task_id_set":true}`), CreatedAt: created.Add(time.Second)},
				{ID: 3, TaskID: 9, Event: model.TaskEventFailed, FromStatus: model.TaskQueued, ToStatus: model.TaskFailed, DetailJSON: datatypes.JSON(`{"upstream":"HTTP 503 overloaded"}`), CreatedAt: created.Add(time.Minute)},
			},
			ledger: []model.CreditLedger{
				{ID: 1, Type: model.LedgerFreeze, Amount: 8, CreatedAt: created},
				{ID: 2, Type: model.LedgerRefund, Amount: 8, CreatedAt: created.Add(time.Minute)},
			},
		}
		if mut != nil {
			mut(repo)
		}
		return repo
	}

	t.Run("成功：基础信息、输入、产物、时间线、上游原文、带符号的积分流水", func(t *testing.T) {
		repo := newRepo(nil)
		d, err := newUsageSvc(repo).Get(ctx, "  "+idcodec.Encode(9)+" ")
		if err != nil {
			t.Fatal(err)
		}
		if repo.lastRef != idcodec.Encode(9) {
			t.Fatalf("编号应去空格后查询：%q", repo.lastRef)
		}
		tk := d.Task
		if tk.ModelName != "可灵 v2" || tk.Channel != "kling-official" || tk.Plugin != "kling@1.2.0" || tk.CanvasID != idcodec.Encode(canvas) || tk.NodeID != "node-2" {
			t.Fatalf("基础信息不对：%+v", tk)
		}
		if d.User.Username != "alice" || d.User.Email != "a@x.com" || d.User.ID != 1 {
			t.Fatalf("用户不对：%+v", d.User)
		}
		if d.Input["prompt"] != "海边日落" || len(d.Outputs) != 1 || d.Outputs[0].AssetID != 5 {
			t.Fatalf("输入 / 产物不对：%+v %+v", d.Input, d.Outputs)
		}
		if d.Error.Code != "plugin_error" || d.Error.Message != "平台繁忙" || d.Error.Upstream == nil || *d.Error.Upstream != "HTTP 503 overloaded" {
			t.Fatalf("失败信息不对：%+v", d.Error)
		}
		if len(d.Events) != 3 || d.Events[0].Event != model.TaskEventCreated || d.Events[0].Detail != nil || d.Events[2].Detail["upstream"] != "HTTP 503 overloaded" {
			t.Fatalf("时间线不对：%+v", d.Events)
		}
		if len(d.Ledger) != 2 || d.Ledger[0].Amount != -8 || d.Ledger[1].Amount != 8 {
			t.Fatalf("freeze 应为负、refund 为正：%+v", d.Ledger)
		}
		if d.Trace != nil {
			t.Fatalf("正式任务没有试跑追踪：%s", d.Trace)
		}
	})

	t.Run("老任务：没有事件时 events 是空数组、upstream 为 null", func(t *testing.T) {
		d, err := newUsageSvc(newRepo(func(r *fakeUsageRepo) { r.events = nil })).Get(ctx, task.TaskRef)
		if err != nil {
			t.Fatal(err)
		}
		if d.Events == nil || len(d.Events) != 0 || d.Error.Upstream != nil {
			t.Fatalf("老任务应降级：%+v", d)
		}
	})

	t.Run("快照 / 输入 / 产物损坏或为空：尽量降级而不是失败", func(t *testing.T) {
		d, err := newUsageSvc(newRepo(func(r *fakeUsageRepo) {
			r.task.ConfigSnapshot = datatypes.JSON(`{`)
			r.task.InputJSON = datatypes.JSON(`not json`)
			r.task.OutputJSON = nil
			r.task.CanvasProjectID = nil
		})).Get(ctx, task.TaskRef)
		if err != nil {
			t.Fatalf("损坏的 JSON 不应让详情失败：%v", err)
		}
		if d.Task.ModelName != "kling-v2" || d.Task.Channel != "" || d.Task.Plugin != "" || d.Task.CanvasID != "" {
			t.Fatalf("快照损坏时展示名回落 model key：%+v", d.Task)
		}
		if d.Input == nil || len(d.Input) != 0 || d.Outputs == nil || len(d.Outputs) != 0 {
			t.Fatalf("输入 / 产物应是空对象 / 空数组：%+v %+v", d.Input, d.Outputs)
		}
	})

	t.Run("试跑任务带 trace，正式任务不带", func(t *testing.T) {
		d, err := newUsageSvc(newRepo(func(r *fakeUsageRepo) {
			r.task.IsTest = true
			r.task.TraceJSON = datatypes.JSON(`[{"name":"submit","kind":"hook"}]`)
		})).Get(ctx, task.TaskRef)
		if err != nil || string(d.Trace) != `[{"name":"submit","kind":"hook"}]` {
			t.Fatalf("试跑任务应带 trace：%v %s", err, d.Trace)
		}
	})

	t.Run("用户已不存在：详情仍可看，用户名为空、保留 id", func(t *testing.T) {
		d, err := newUsageSvc(newRepo(func(r *fakeUsageRepo) { r.user = nil })).Get(ctx, task.TaskRef)
		if err != nil || d.User.ID != 1 || d.User.Username != "" {
			t.Fatalf("用户缺失应降级：%v %+v", err, d.User)
		}
	})

	t.Run("任务不存在返回 ErrTaskNotFound；其他仓储错误原样返回", func(t *testing.T) {
		_, err := newUsageSvc(newRepo(func(r *fakeUsageRepo) { r.task = nil })).Get(ctx, task.TaskRef)
		var ec *errcode.Error
		if !errors.As(err, &ec) || ec.Code != errcode.ErrTaskNotFound.Code {
			t.Fatalf("应返回 ErrTaskNotFound：%v", err)
		}
		boom := errors.New("boom")
		for _, m := range []string{"GetTaskByRef", "GetUser", "ListEvents", "ListTaskLedger"} {
			_, err := newUsageSvc(newRepo(func(r *fakeUsageRepo) { r.errs = map[string]error{m: boom} })).Get(ctx, task.TaskRef)
			if !errors.Is(err, boom) {
				t.Fatalf("%s 的错误应原样返回：%v", m, err)
			}
		}
	})
}
