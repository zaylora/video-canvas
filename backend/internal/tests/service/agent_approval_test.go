package service_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	. "video-canvas/internal/service"
)

// runningRun 起一个运行并推进到 running，返回它（模拟 runtime 已接手）。
func (e *agentEnv) runningRun(t *testing.T) *model.AgentRun {
	t.Helper()
	v := e.start(t, "拆分镜")
	e.setStatus(uint64(v.ID), model.RunRunning)
	r := e.run(uint64(v.ID))
	return &r
}

func genPayload() GeneratePayload {
	return GeneratePayload{Reason: "先锁定角色", Items: []GenerateItem{
		{NodeID: "n1", Label: "林夏", ModelKey: "seedream", ModelName: "Seedream", Price: 8, Count: 2},
		{NodeID: "n2", Label: "老周", ModelKey: "seedream", ModelName: "Seedream", Price: 8, Count: 1},
	}}
}

func TestAgentCreateApproval(t *testing.T) {
	ctx := context.Background()

	t.Run("生成审批：运行进入 waiting_approval，24 小时有效，推事件", func(t *testing.T) {
		e := newAgentEnv(t)
		run := e.runningRun(t)
		v, err := e.svc.CreateApproval(ctx, run, "tc1", model.ApprovalGenerate, genPayload(), 24)
		if err != nil {
			t.Fatal(err)
		}
		if v.Status != model.ApprovalPending || v.QuoteCredits != 24 || !v.ExpiresAt.Equal(e.now.Add(24*time.Hour)) {
			t.Fatalf("v=%+v", v)
		}
		if e.run(run.ID).Status != model.RunWaitingApproval {
			t.Errorf("运行应等待批准: %s", e.run(run.ID).Status)
		}
		types := e.repo.eventTypes()
		if types[len(types)-2] != "approval.created" || types[len(types)-1] != "run.status" {
			t.Errorf("events=%v", types)
		}
	})

	t.Run("提问：运行进入 waiting_input", func(t *testing.T) {
		e := newAgentEnv(t)
		run := e.runningRun(t)
		if _, err := e.svc.CreateApproval(ctx, run, "tc1", model.ApprovalAsk, AskPayload{Question: "画幅？", Kind: "choice", Options: []string{"16:9", "9:16"}}, 0); err != nil {
			t.Fatal(err)
		}
		if e.run(run.ID).Status != model.RunWaitingInput {
			t.Errorf("status=%s", e.run(run.ID).Status)
		}
	})

	t.Run("运行已不在 running（比如刚被停止）：不产生审批", func(t *testing.T) {
		e := newAgentEnv(t)
		run := e.runningRun(t)
		e.setStatus(run.ID, model.RunCanceled)
		_, err := e.svc.CreateApproval(ctx, run, "tc1", model.ApprovalDelete, DeletePayload{NodeIDs: []string{"n"}}, 0)
		wantAgentCode(t, err, errcode.ErrAgentState)
	})

	t.Run("种类不认识", func(t *testing.T) {
		e := newAgentEnv(t)
		_, err := e.svc.CreateApproval(ctx, e.runningRun(t), "tc1", "explode", nil, 0)
		wantAgentCode(t, err, errcode.ErrInvalidParams)
	})
}

// pendingGenerate 建好一个待批准的生成审批并返回它的 id。
func pendingGenerate(t *testing.T, e *agentEnv, quote int) (*model.AgentRun, uint64) {
	t.Helper()
	run := e.runningRun(t)
	v, err := e.svc.CreateApproval(context.Background(), run, "tc1", model.ApprovalGenerate, genPayload(), quote)
	if err != nil {
		t.Fatal(err)
	}
	return run, uint64(v.ID)
}

func TestAgentDecideGenerate(t *testing.T) {
	ctx := context.Background()
	approve := &model.DecideAgentApprovalReq{Decision: "approve"}

	t.Run("整体批准：按申请的数量执行、记入已花积分、运行继续", func(t *testing.T) {
		e := newAgentEnv(t)
		run, id := pendingGenerate(t, e, 24)
		v, err := e.svc.Decide(ctx, 1, id, approve)
		if err != nil {
			t.Fatal(err)
		}
		if v.Status != model.ApprovalExecuted || v.QuoteCredits != 24 {
			t.Fatalf("v=%+v", v)
		}
		if len(e.gen.calls) != 1 || len(e.gen.calls[0]) != 2 || e.gen.calls[0][0].Count != 2 {
			t.Errorf("应按申请的数量执行: %+v", e.gen.calls)
		}
		got := e.run(run.ID)
		if got.Status != model.RunRunning || got.SpentCredits != 24 {
			t.Errorf("run=%+v", got)
		}
		if len(e.rt.resumed) != 1 {
			t.Error("应让 runtime 继续")
		}
	})

	t.Run("部分批准：只留一项、张数改少，价格按批准的算", func(t *testing.T) {
		e := newAgentEnv(t)
		_, id := pendingGenerate(t, e, 24)
		v, err := e.svc.Decide(ctx, 1, id, &model.DecideAgentApprovalReq{Decision: "approve", Items: []model.ApprovalItemDecision{
			{Index: 0, Approve: true, Count: 1}, {Index: 1, Approve: false},
		}})
		if err != nil {
			t.Fatal(err)
		}
		if v.QuoteCredits != 8 {
			t.Errorf("1 张 × 8 = 8: %d", v.QuoteCredits)
		}
		items := e.gen.calls[0]
		if len(items) != 1 || items[0].NodeID != "n1" || items[0].Count != 1 {
			t.Errorf("执行内容不对: %+v", items)
		}
		var d map[string]any
		_ = json.Unmarshal(v.Decision, &d)
		if d["decision"] != "approve" {
			t.Errorf("decision=%s", v.Decision)
		}
	})

	t.Run("张数只能少不能多：超出申请的收回到申请值", func(t *testing.T) {
		e := newAgentEnv(t)
		_, id := pendingGenerate(t, e, 24)
		_, err := e.svc.Decide(ctx, 1, id, &model.DecideAgentApprovalReq{Decision: "approve", Items: []model.ApprovalItemDecision{{Index: 1, Approve: true, Count: 4}}})
		if err != nil {
			t.Fatal(err)
		}
		if got := e.gen.calls[0][0]; got.NodeID != "n2" || got.Count != 1 {
			t.Errorf("申请 1 张不能被改成 4 张: %+v", got)
		}
	})

	t.Run("一项都没批准：参数错误，审批保持待处理", func(t *testing.T) {
		e := newAgentEnv(t)
		_, id := pendingGenerate(t, e, 24)
		_, err := e.svc.Decide(ctx, 1, id, &model.DecideAgentApprovalReq{Decision: "approve", Items: []model.ApprovalItemDecision{{Index: 0}, {Index: 1}}})
		wantAgentCode(t, err, errcode.ErrInvalidParams)
		if a, _ := e.repo.GetApproval(ctx, 1, id); a.Status != model.ApprovalPending {
			t.Errorf("status=%s", a.Status)
		}
	})

	t.Run("条目序号越界或重复：参数错误", func(t *testing.T) {
		e := newAgentEnv(t)
		_, id := pendingGenerate(t, e, 24)
		_, err := e.svc.Decide(ctx, 1, id, &model.DecideAgentApprovalReq{Decision: "approve", Items: []model.ApprovalItemDecision{{Index: 5, Approve: true}}})
		wantAgentCode(t, err, errcode.ErrInvalidParams)
		_, err = e.svc.Decide(ctx, 1, id, &model.DecideAgentApprovalReq{Decision: "approve", Items: []model.ApprovalItemDecision{{Index: 0, Approve: true}, {Index: 0, Approve: true}}})
		wantAgentCode(t, err, errcode.ErrInvalidParams)
	})

	t.Run("超出本轮预算：60008；追加预算后可批准", func(t *testing.T) {
		e := newAgentEnv(t)
		run, id := pendingGenerate(t, e, 24)
		e.repo.mu.Lock()
		e.repo.runs[run.ID].BudgetCredits = 10 // 预算只剩 10，申请要 24
		e.repo.mu.Unlock()
		_, err := e.svc.Decide(ctx, 1, id, approve)
		wantAgentCode(t, err, errcode.ErrAgentOverBudget)
		if a, _ := e.repo.GetApproval(ctx, 1, id); a.Status != model.ApprovalPending || len(e.gen.calls) != 0 {
			t.Error("超预算时审批保持待处理，也不能执行")
		}
		if _, err := e.svc.Decide(ctx, 1, id, &model.DecideAgentApprovalReq{Decision: "approve", AddBudget: 14}); err != nil {
			t.Fatalf("追加 14 后恰好够: %v", err)
		}
		if got := e.run(run.ID); got.BudgetCredits != 24 || got.SpentCredits != 24 {
			t.Errorf("run=%+v", got)
		}
	})

	t.Run("拒绝：不执行、不扣积分、运行继续；拒绝时的追加预算不生效", func(t *testing.T) {
		e := newAgentEnv(t)
		run, id := pendingGenerate(t, e, 24)
		v, err := e.svc.Decide(ctx, 1, id, &model.DecideAgentApprovalReq{Decision: "reject", AddBudget: 99})
		if err != nil || v.Status != model.ApprovalRejected {
			t.Fatalf("v=%+v err=%v", v, err)
		}
		got := e.run(run.ID)
		if len(e.gen.calls) != 0 || got.SpentCredits != 0 || got.BudgetCredits != 50 || got.Status != model.RunRunning {
			t.Errorf("run=%+v calls=%d", got, len(e.gen.calls))
		}
	})

	t.Run("执行失败：审批标为 failed，但决定已生效、运行继续，让 Agent 知道失败了", func(t *testing.T) {
		e := newAgentEnv(t)
		e.gen.err = errAgentBoom
		run, id := pendingGenerate(t, e, 24)
		v, err := e.svc.Decide(ctx, 1, id, approve)
		if err != nil || v.Status != model.ApprovalFailed {
			t.Fatalf("v=%+v err=%v", v, err)
		}
		if e.run(run.ID).Status != model.RunRunning {
			t.Error("运行应继续")
		}
	})
}

func TestAgentDecideGuards(t *testing.T) {
	ctx := context.Background()
	approve := &model.DecideAgentApprovalReq{Decision: "approve"}

	t.Run("重复处理：60004，只执行一次", func(t *testing.T) {
		e := newAgentEnv(t)
		_, id := pendingGenerate(t, e, 24)
		if _, err := e.svc.Decide(ctx, 1, id, approve); err != nil {
			t.Fatal(err)
		}
		_, err := e.svc.Decide(ctx, 1, id, approve)
		wantAgentCode(t, err, errcode.ErrAgentApprovalGone)
		if len(e.gen.calls) != 1 {
			t.Errorf("只能执行一次: %d", len(e.gen.calls))
		}
	})

	t.Run("超过 24 小时：60004", func(t *testing.T) {
		e := newAgentEnv(t)
		_, id := pendingGenerate(t, e, 24)
		e.now = e.now.Add(25 * time.Hour)
		_, err := e.svc.Decide(ctx, 1, id, approve)
		wantAgentCode(t, err, errcode.ErrAgentApprovalGone)
	})

	t.Run("运行已被停止：60004", func(t *testing.T) {
		e := newAgentEnv(t)
		run, id := pendingGenerate(t, e, 24)
		if _, err := e.svc.Cancel(ctx, 1, run.ID); err != nil {
			t.Fatal(err)
		}
		_, err := e.svc.Decide(ctx, 1, id, approve)
		wantAgentCode(t, err, errcode.ErrAgentApprovalGone)
	})

	t.Run("别人的审批：60013", func(t *testing.T) {
		e := newAgentEnv(t)
		_, id := pendingGenerate(t, e, 24)
		_, err := e.svc.Decide(ctx, 2, id, approve)
		wantAgentCode(t, err, errcode.ErrAgentApprovalMiss)
		_, err = e.svc.Decide(ctx, 1, 987654, approve)
		wantAgentCode(t, err, errcode.ErrAgentApprovalMiss)
	})

	t.Run("runtime 不在了：决定仍然生效，运行标为中断可以稍后继续", func(t *testing.T) {
		e := newAgentEnv(t)
		run, id := pendingGenerate(t, e, 24)
		e.rt.resumeErr = errAgentBoom
		v, err := e.svc.Decide(ctx, 1, id, approve)
		if err != nil || v.Status != model.ApprovalExecuted {
			t.Fatalf("v=%+v err=%v", v, err)
		}
		if e.run(run.ID).Status != model.RunInterrupted {
			t.Errorf("status=%s", e.run(run.ID).Status)
		}
	})
}

func TestAgentDecideDeleteAndAsk(t *testing.T) {
	ctx := context.Background()

	t.Run("批准删除：真的改了画布，审批执行完", func(t *testing.T) {
		e := newAgentEnv(t)
		run := e.runningRun(t)
		v, _ := e.svc.CreateApproval(ctx, run, "tc9", model.ApprovalDelete, DeletePayload{Reason: "废稿", NodeIDs: []string{"n_script"}, Labels: []string{"剧本"}, Outputs: []bool{false}}, 0)
		out, err := e.svc.Decide(ctx, 1, uint64(v.ID), &model.DecideAgentApprovalReq{Decision: "approve"})
		if err != nil {
			t.Fatal(err)
		}
		if out.Status != model.ApprovalExecuted {
			t.Fatalf("status=%s", out.Status)
		}
		if g := mustParseGraph(t, e.repo); g.Node("n_script") != nil {
			t.Error("节点应被删除")
		}
		if last := e.repo.muts[len(e.repo.muts)-1]; last.Kind != model.MutationDelete || last.ToolCallID != "tc9" {
			t.Errorf("应记一条 delete 改动: %+v", last)
		}
	})

	t.Run("拒绝删除：画布不变", func(t *testing.T) {
		e := newAgentEnv(t)
		run := e.runningRun(t)
		v, _ := e.svc.CreateApproval(ctx, run, "tc9", model.ApprovalDelete, DeletePayload{NodeIDs: []string{"n_script"}}, 0)
		if _, err := e.svc.Decide(ctx, 1, uint64(v.ID), &model.DecideAgentApprovalReq{Decision: "reject"}); err != nil {
			t.Fatal(err)
		}
		if g := mustParseGraph(t, e.repo); g.Node("n_script") == nil || e.repo.commits != 0 {
			t.Error("拒绝后画布不能变")
		}
	})

	t.Run("删的节点已经不在了：审批标为 failed，不让请求失败", func(t *testing.T) {
		e := newAgentEnv(t)
		run := e.runningRun(t)
		v, _ := e.svc.CreateApproval(ctx, run, "tc9", model.ApprovalDelete, DeletePayload{NodeIDs: []string{"ghost"}}, 0)
		out, err := e.svc.Decide(ctx, 1, uint64(v.ID), &model.DecideAgentApprovalReq{Decision: "approve"})
		if err != nil || out.Status != model.ApprovalFailed {
			t.Fatalf("out=%+v err=%v", out, err)
		}
	})

	t.Run("提问：必须有回答，回答记入决定", func(t *testing.T) {
		e := newAgentEnv(t)
		run := e.runningRun(t)
		v, _ := e.svc.CreateApproval(ctx, run, "tc2", model.ApprovalAsk, AskPayload{Question: "画幅？", Kind: "choice", Options: []string{"16:9", "9:16"}}, 0)
		_, err := e.svc.Decide(ctx, 1, uint64(v.ID), &model.DecideAgentApprovalReq{Decision: "approve"})
		wantAgentCode(t, err, errcode.ErrInvalidParams)
		out, err := e.svc.Decide(ctx, 1, uint64(v.ID), &model.DecideAgentApprovalReq{Decision: "approve", Answer: "16:9 横屏"})
		if err != nil || out.Status != model.ApprovalApproved {
			t.Fatalf("out=%+v err=%v", out, err)
		}
		var d struct{ Answer string }
		_ = json.Unmarshal(out.Decision, &d)
		if d.Answer != "16:9 横屏" {
			t.Errorf("decision=%s", out.Decision)
		}
		if e.run(run.ID).Status != model.RunRunning {
			t.Error("回答后运行应继续")
		}
	})
}
