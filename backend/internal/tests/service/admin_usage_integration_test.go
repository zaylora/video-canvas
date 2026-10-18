package service_test

import (
	"context"
	"strings"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/repository"
	. "video-canvas/internal/service"
)

// 真实 PostgreSQL 上走一遍：任务服务推进状态（创建 → 受理 → 失败带上游原文），后台使用记录读详情，
// 确认时间线、上游错误原文和列表筛选都来自同一条真实数据链路。需要 TEST_DATABASE_DSN，未设置时跳过。
func TestAdminUsage_Integration_TimelineAndUpstream(t *testing.T) {
	ctx := context.Background()
	env := newGTIEnv(t, defaultTaskLimits())
	if err := env.db.AutoMigrate(&model.User{}); err != nil {
		t.Fatal(err)
	}
	// 1. 走真实创建路径（冻结积分、写 created 事件），再按 worker 的路径推进：受理 → 失败（带上游原文）
	u := &model.User{Username: "alice", Email: "a@x.com", Password: "x", Role: model.RoleUser, Status: model.UserStatusActive}
	u.ID = env.user // 任务属于集成环境里的这个用户
	if err := env.db.Create(u).Error; err != nil {
		t.Fatal(err)
	}
	view, err := env.create("")
	if err != nil {
		t.Fatal(err)
	}
	task, err := env.store.GetByID(ctx, env.user, view.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.store.UpdateIf(ctx, task.ID, []string{model.TaskPending}, map[string]any{"status": model.TaskQueued, "provider_task_id": "up-1"}, true); err != nil {
		t.Fatal(err)
	}
	const raw = `HTTP 503 {"error":"upstream overloaded"}`
	if applied, err := env.svc.FailWithUpstream(ctx, task, "provider_error", "平台繁忙，请稍后重试", raw); err != nil || !applied {
		t.Fatalf("失败迁移：applied=%v err=%v", applied, err)
	}

	// 2. 后台读详情
	// 用真实时钟：任务是用真实当前时间写进真实数据库的，固定时钟会让默认的「近 7 天」范围随日期推移而漏掉它
	svc := NewAdminUsageService(repository.NewAdminUsageRepository(env.db))
	d, err := svc.Get(ctx, task.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	if d.Task.Status != model.TaskFailed || d.User.Username != "alice" {
		t.Fatalf("基础信息不对：%+v %+v", d.Task, d.User)
	}
	var names []string
	for _, e := range d.Events {
		names = append(names, e.Event)
	}
	if got := strings.Join(names, ","); got != "created,submitted,failed" {
		t.Fatalf("时间线应为 created,submitted,failed，实际 %s", got)
	}
	if d.Error.Upstream == nil || *d.Error.Upstream != raw {
		t.Fatalf("上游错误原文应原样可见：%v", d.Error.Upstream)
	}
	if d.Error.Message != "平台繁忙，请稍后重试" || strings.Contains(d.Task.ErrorMessage, "503") {
		t.Fatalf("用户可见文案不应含上游原文：%+v", d.Error)
	}
	if len(d.Input) == 0 {
		t.Fatalf("输入应是创建时的规范化输入：%+v", d.Input)
	}
	// 冻结 → 退款：失败任务的积分流水要在详情里看得到
	if len(d.Ledger) != 2 || d.Ledger[0].Type != model.LedgerFreeze || d.Ledger[0].Amount >= 0 || d.Ledger[1].Type != model.LedgerRefund || d.Ledger[1].Amount <= 0 {
		t.Fatalf("积分流水应为 freeze(负) + refund(正)：%+v", d.Ledger)
	}

	// 3. 列表与摘要：按状态筛选命中这条，口径一致
	page, err := svc.List(ctx, &model.ListUsageReq{UsageFilterReq: model.UsageFilterReq{Status: "failed", Q: "ALI"}})
	if err != nil || page.Total != 1 || len(page.List) != 1 || page.List[0].TaskRef != task.TaskRef || page.List[0].User.Username != "alice" {
		t.Fatalf("列表应命中这条失败任务：%v %+v", err, page)
	}
	sum, err := svc.Summary(ctx, &model.UsageFilterReq{Status: "failed", Q: "ALI"})
	if err != nil || sum.Total != 1 || sum.Failed != 1 {
		t.Fatalf("摘要应与列表一致：%v %+v", err, sum)
	}
}
