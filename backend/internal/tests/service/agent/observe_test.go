package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	. "video-canvas/internal/service/agent"
)

var obsT0 = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

func baseFacts() *model.AgentRunFacts {
	end := obsT0.Add(3*time.Minute + 12*time.Second)
	f := &model.AgentRunFacts{
		Run: model.AgentRun{ID: 5, SessionID: 2, CanvasID: 7, UserID: 1, Status: model.RunSucceeded, Mode: "storyboard", Steps: 18, SpentCredits: 42,
			PromptVersion: 9, ToolsHash: "abc", BuildSHA: "7baa1a3", HarnessKey: "hk9", CreatedAt: obsT0, EndedAt: &end},
	}
	f.ToolCalls = []model.AgentToolCall{
		{Name: "skill_read", Summary: "读取技能 script-breakdown@v3"},
		{Name: "skill_read", Summary: "读取技能文件 storyboard@v2/ref.md"},
		{Name: "skill_read", Summary: "读取技能 character-turnaround"}, // 内置技能没有版本号
		{Name: "canvas_apply_ops", Summary: "新建 3 个节点"},
		{Name: "canvas_apply_ops", IsError: true, Summary: "第 1 项有问题"},
		{Name: "skill_read", IsError: true, Summary: "读取技能 ghost@v9"}, // 读失败的不算读过
	}
	f.ModelCalls = []model.AgentModelCall{
		{ModelKey: "claude", InputTokens: 100, OutputTokens: 50},
		{ModelKey: "claude", InputTokens: 200, OutputTokens: 70},
		{ModelKey: "claude", InputTokens: 300, OutputTokens: 80, Purpose: "summary"},
	}
	undone := obsT0.Add(time.Hour)
	f.Mutations = []model.AgentMutation{
		{Kind: model.MutationApplyOps, UndoneAt: &undone},
		{Kind: model.MutationArrange},
		{Kind: model.MutationUndo},
	}
	f.Approvals = []model.AgentApproval{{Status: model.ApprovalRejected}, {Status: model.ApprovalApproved}, {Status: model.ApprovalPartial}}
	f.UserMessages = []model.AgentEvent{
		{Seq: 1, PayloadJSON: rawJSON(`{"text":"把剧本拆成 6 个镜头","mode":"storyboard"}`)},
		{Seq: 5, PayloadJSON: rawJSON(`{"text":"镜头别太多","steer":true}`)},
		{Seq: 8, PayloadJSON: rawJSON(`{"text":"再少一点","steer":true}`)},
	}
	f.Feedback = &model.AgentFeedback{Rating: model.FeedbackDown}
	f.RuntimeTail = "stderr 末尾"
	return f
}

func TestBuildDigest(t *testing.T) {
	now := obsT0.Add(2 * time.Hour)

	t.Run("汇总运行、工具、模型、审批、改动、插话和反馈", func(t *testing.T) {
		d := BuildDigest(baseFacts(), now)
		if d.RunID != 5 || d.SessionID != 2 || d.UserID != 1 || d.CanvasID != 7 || d.Mode != "storyboard" || d.Status != model.RunSucceeded || d.Steps != 18 || d.SpentCredits != 42 {
			t.Errorf("运行本身：%+v", d)
		}
		if d.PromptVersion != 9 || d.ToolsHash != "abc" || d.BuildSHA != "7baa1a3" || d.HarnessKey != "hk9" {
			t.Errorf("Harness 版本：%+v", d)
		}
		if d.ToolCalls != 6 || d.ToolErrors != 2 {
			t.Errorf("工具调用 6 次、报错 2 次：%d %d", d.ToolCalls, d.ToolErrors)
		}
		if d.ModelCalls != 3 || d.InputTokens != 600 || d.OutputTokens != 200 || d.ModelKey != "claude" {
			t.Errorf("模型调用（摘要调用也计入）：%+v", d)
		}
		if d.ApprovalsTotal != 3 || d.ApprovalsRejected != 1 {
			t.Errorf("审批：%d %d", d.ApprovalsTotal, d.ApprovalsRejected)
		}
		if d.MutationCount != 2 || !d.Undone {
			t.Errorf("撤销本身不算改动；有改动被撤销就标 undone：%d %v", d.MutationCount, d.Undone)
		}
		if d.SteerCount != 2 || d.RequestHead != "把剧本拆成 6 个镜头" {
			t.Errorf("插话 2 次，请求取第一条非插话消息：%d %q", d.SteerCount, d.RequestHead)
		}
		if d.Feedback != model.FeedbackDown {
			t.Errorf("feedback=%d", d.Feedback)
		}
		if d.DurationMs != 192000 || d.EndedAt == nil || !d.CreatedAt.Equal(obsT0) || !d.UpdatedAt.Equal(now) {
			t.Errorf("耗时 3m12s：%+v", d)
		}
		if d.RuntimeTail == nil || *d.RuntimeTail != "stderr 末尾" {
			t.Errorf("runtime_tail=%v", d.RuntimeTail)
		}
	})

	t.Run("技能版本：读过的才算，内置技能记 builtin，同名取后一次", func(t *testing.T) {
		var got map[string]string
		if err := json.Unmarshal(BuildDigest(baseFacts(), now).Skills, &got); err != nil {
			t.Fatal(err)
		}
		want := map[string]string{"script-breakdown": "v3", "storyboard": "v2", "character-turnaround": "builtin"}
		if len(got) != len(want) {
			t.Fatalf("got=%v", got)
		}
		for k, v := range want {
			if got[k] != v {
				t.Errorf("%s: got %q want %q", k, got[k], v)
			}
		}
	})

	t.Run("没有撤销、没有评价、还没结束：零值而不是出错", func(t *testing.T) {
		f := baseFacts()
		f.Mutations, f.Feedback, f.Run.EndedAt, f.RuntimeTail, f.Run.Status = nil, nil, nil, "", model.RunWaitingApproval
		d := BuildDigest(f, now)
		if d.Undone || d.MutationCount != 0 || d.Feedback != 0 || d.DurationMs != 0 || d.EndedAt != nil || d.RuntimeTail != nil {
			t.Errorf("d=%+v", d)
		}
	})

	t.Run("撤销也可能只体现为一条 undo 改动（原改动没标 undone_at）", func(t *testing.T) {
		f := baseFacts()
		f.Mutations = []model.AgentMutation{{Kind: model.MutationApplyOps}, {Kind: model.MutationUndo}}
		if !BuildDigest(f, now).Undone {
			t.Error("有 undo 改动就是被撤销过")
		}
	})

	t.Run("请求首句最多 200 字，没有用户消息为空", func(t *testing.T) {
		f := baseFacts()
		long := strings.Repeat("长", 300)
		f.UserMessages = []model.AgentEvent{{PayloadJSON: rawJSON(`{"text":"` + long + `"}`)}}
		if got := BuildDigest(f, now).RequestHead; len([]rune(got)) != 200 {
			t.Errorf("len=%d", len([]rune(got)))
		}
		f.UserMessages = nil
		if BuildDigest(f, now).RequestHead != "" || BuildDigest(f, now).SteerCount != 0 {
			t.Error("没有用户消息应为空")
		}
	})

	t.Run("损坏的事件载荷被跳过，不影响其他字段", func(t *testing.T) {
		f := baseFacts()
		f.UserMessages = []model.AgentEvent{{PayloadJSON: rawJSON(`not json`)}, {PayloadJSON: rawJSON(`{"text":"好的"}`)}}
		if got := BuildDigest(f, now).RequestHead; got != "好的" {
			t.Errorf("head=%q", got)
		}
	})
}

type observeEnv struct {
	svc    *ObserveService
	repo   *fakeObserveRepo
	runs   fakeObserveRuns
	events *fakeEmitter
	audit  *fakeAudit
	now    time.Time
	roles  map[uint64]string
}

func newObserveEnv(t *testing.T) *observeEnv {
	t.Helper()
	e := &observeEnv{repo: newFakeObserveRepo(), runs: fakeObserveRuns{}, events: &fakeEmitter{}, audit: &fakeAudit{}, now: obsT0, roles: map[uint64]string{9: model.RoleAdmin}}
	// 运行 5 属于用户 1，已成功结束；运行 6 还在跑
	f := baseFacts()
	e.repo.facts[5] = f
	run5 := f.Run
	e.runs[5] = &run5
	run6 := model.AgentRun{ID: 6, SessionID: 2, CanvasID: 7, UserID: 1, Status: model.RunRunning, CreatedAt: obsT0}
	e.runs[6] = &run6
	e.repo.facts[6] = &model.AgentRunFacts{Run: run6}
	e.svc = NewObserveService(ObserveDeps{
		Repo: e.repo, Runs: e.runs, Events: e.events, Audit: e.audit,
		Roles: func(_ context.Context, id uint64) (string, error) { return e.roles[id], nil },
		Now:   func() time.Time { return e.now },
	})
	return e
}

func TestObserve_RefreshDigest(t *testing.T) {
	ctx := context.Background()
	e := newObserveEnv(t)
	if err := e.svc.RefreshDigest(ctx, 5); err != nil {
		t.Fatal(err)
	}
	d := e.repo.digests[5]
	if d == nil || d.ToolCalls != 6 || !d.UpdatedAt.Equal(obsT0) {
		t.Fatalf("d=%+v", d)
	}
	// 再来一次：覆盖
	e.now = obsT0.Add(time.Minute)
	_ = e.svc.RefreshDigest(ctx, 5)
	if !e.repo.digests[5].UpdatedAt.Equal(e.now) {
		t.Error("应覆盖并更新 updated_at")
	}
	if err := e.svc.RefreshDigest(ctx, 404); err == nil {
		t.Error("运行不存在应返回错误")
	}
	e.repo.upsertErr = errors.New("库不可用")
	if err := e.svc.RefreshDigest(ctx, 5); err == nil {
		t.Error("写库失败应返回错误，由调用方决定只记日志")
	}
}

func TestObserve_SetFeedback(t *testing.T) {
	ctx := context.Background()

	t.Run("点踩：记下评分、原因和说明；发事件；摘要同步", func(t *testing.T) {
		e := newObserveEnv(t)
		fb, err := e.svc.SetFeedback(ctx, 1, 5, &model.SetAgentFeedbackReq{Rating: -1, Reasons: []string{"misread", "slow", "misread"}, Comment: "我要 6 个，它给了 12 个"})
		if err != nil {
			t.Fatal(err)
		}
		if fb.Rating != -1 || len(fb.Reasons) != 2 || fb.Comment != "我要 6 个，它给了 12 个" {
			t.Errorf("原因去重后 2 个：%+v", fb)
		}
		got := e.repo.feedback[[2]uint64{5, 1}]
		if got == nil || got.Rating != -1 {
			t.Fatalf("应落库：%+v", got)
		}
		if types := e.events.types(); len(types) != 1 || types[0] != "feedback.set" {
			t.Fatalf("events=%v", types)
		}
		ev := e.events.events[0]
		if ev.userID != 1 || ev.sessionID != 2 || ev.runID != 5 || ev.canvasID != 7 {
			t.Errorf("事件归属：%+v", ev)
		}
		if ev.data["rating"] != float64(-1) || ev.data["comment"] != "我要 6 个，它给了 12 个" {
			t.Errorf("事件内容：%v", ev.data)
		}
		if _, has := ev.data["run_id"]; has {
			t.Error("事件载荷里不放原始 run_id：前端靠事件自带的编码后的 run_id 对账")
		}
		if d := e.repo.digests[5]; d == nil || d.Feedback != -1 {
			t.Errorf("摘要的 feedback 应同步：%+v", d)
		}
	})

	t.Run("点赞：忽略原因；改选覆盖", func(t *testing.T) {
		e := newObserveEnv(t)
		_, _ = e.svc.SetFeedback(ctx, 1, 5, &model.SetAgentFeedbackReq{Rating: -1, Reasons: []string{"slow"}})
		fb, err := e.svc.SetFeedback(ctx, 1, 5, &model.SetAgentFeedbackReq{Rating: 1, Reasons: []string{"slow"}})
		if err != nil {
			t.Fatal(err)
		}
		if fb.Rating != 1 || len(fb.Reasons) != 0 {
			t.Errorf("点赞不带原因：%+v", fb)
		}
		if e.repo.digests[5].Feedback != 1 || len(e.repo.feedback) != 1 {
			t.Error("改选应覆盖，不新增")
		}
	})

	t.Run("校验：评分、原因标签、说明长度", func(t *testing.T) {
		e := newObserveEnv(t)
		cases := map[string]*model.SetAgentFeedbackReq{
			"评分不是 1/-1":  {Rating: 2},
			"未知原因标签":     {Rating: -1, Reasons: []string{"nope"}},
			"说明超过 500 字": {Rating: -1, Comment: strings.Repeat("字", 501)},
		}
		for name, req := range cases {
			_, err := e.svc.SetFeedback(ctx, 1, 5, req)
			wantAgentCode(t, err, errcode.ErrInvalidParams)
			_ = name
		}
		if _, err := e.svc.SetFeedback(ctx, 1, 5, &model.SetAgentFeedbackReq{Rating: -1, Comment: strings.Repeat("字", 500)}); err != nil {
			t.Errorf("刚好 500 字合法：%v", err)
		}
	})

	t.Run("运行还在跑：60014；别人的运行：60012（不暴露是否存在）", func(t *testing.T) {
		e := newObserveEnv(t)
		_, err := e.svc.SetFeedback(ctx, 1, 6, &model.SetAgentFeedbackReq{Rating: 1})
		wantAgentCode(t, err, errcode.ErrAgentRunNotEnded)
		_, err = e.svc.SetFeedback(ctx, 2, 5, &model.SetAgentFeedbackReq{Rating: 1})
		wantAgentCode(t, err, errcode.ErrAgentRunMissing)
		_, err = e.svc.SetFeedback(ctx, 1, 404, &model.SetAgentFeedbackReq{Rating: 1})
		wantAgentCode(t, err, errcode.ErrAgentRunMissing)
		if len(e.events.events) != 0 || len(e.repo.feedback) != 0 {
			t.Error("被拒绝的请求不应有任何副作用")
		}
	})

	t.Run("暂停中的运行（步数用尽等）可以评价；等审批的不行", func(t *testing.T) {
		e := newObserveEnv(t)
		e.runs[5].Status = model.RunStepLimit
		if _, err := e.svc.SetFeedback(ctx, 1, 5, &model.SetAgentFeedbackReq{Rating: -1}); err != nil {
			t.Errorf("步数用尽后可评价：%v", err)
		}
		e.runs[5].Status = model.RunWaitingApproval
		_, err := e.svc.SetFeedback(ctx, 1, 5, &model.SetAgentFeedbackReq{Rating: -1})
		wantAgentCode(t, err, errcode.ErrAgentRunNotEnded)
	})

	t.Run("摘要同步失败不影响评价本身", func(t *testing.T) {
		e := newObserveEnv(t)
		e.repo.upsertErr = errors.New("库不可用")
		if _, err := e.svc.SetFeedback(ctx, 1, 5, &model.SetAgentFeedbackReq{Rating: 1}); err != nil {
			t.Errorf("评价已保存，摘要稍后会由别的事件刷新：%v", err)
		}
		if e.repo.feedback[[2]uint64{5, 1}] == nil {
			t.Error("评价应已保存")
		}
	})
}

func TestObserve_ClearFeedback(t *testing.T) {
	ctx := context.Background()
	e := newObserveEnv(t)
	_, _ = e.svc.SetFeedback(ctx, 1, 5, &model.SetAgentFeedbackReq{Rating: -1})
	e.events.events = nil
	if err := e.svc.ClearFeedback(ctx, 1, 5); err != nil {
		t.Fatal(err)
	}
	if len(e.repo.feedback) != 0 || e.repo.digests[5].Feedback != 0 {
		t.Error("评价应删除，摘要同步为 0")
	}
	if types := e.events.types(); len(types) != 1 || types[0] != "feedback.cleared" {
		t.Errorf("events=%v", types)
	}
	// 本来就没有：幂等，不再发事件
	e.events.events = nil
	if err := e.svc.ClearFeedback(ctx, 1, 5); err != nil || len(e.events.events) != 0 {
		t.Errorf("重复取消是幂等的，不发事件：err=%v events=%v", err, e.events.types())
	}
	wantAgentCode(t, e.svc.ClearFeedback(ctx, 2, 5), errcode.ErrAgentRunMissing)
}

func TestObserve_Tokens(t *testing.T) {
	ctx := context.Background()
	e := newObserveEnv(t)

	t.Run("签发：明文 vca_ 开头、库里只存哈希、到期按天数、写审计", func(t *testing.T) {
		out, err := e.svc.IssueToken(ctx, 9, &model.IssueAnalysisTokenReq{Name: " 本机 Claude Code ", Days: 30})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(out.Token, "vca_") || len(out.Token) != 4+64 {
			t.Fatalf("明文格式：%q", out.Token)
		}
		stored := e.repo.tokens[0]
		if stored.TokenHash == out.Token || len(stored.TokenHash) != 64 || stored.Prefix != out.Token[:8] || stored.Name != "本机 Claude Code" {
			t.Errorf("stored=%+v", stored)
		}
		if stored.Scope != model.TokenScopeAgentRead || stored.CreatedBy != 9 || !stored.ExpiresAt.Equal(obsT0.AddDate(0, 0, 30)) {
			t.Errorf("stored=%+v", stored)
		}
		if out.Item.Prefix != stored.Prefix || out.Item.ID != stored.ID {
			t.Errorf("item=%+v", out.Item)
		}
		if got := e.audit.actions(); len(got) != 1 || got[0] != model.AdminAuditAgentTokenIssue {
			t.Errorf("audit=%v", got)
		}
		if strings.Contains(string(e.audit.logs[0].DetailJSON), out.Token) {
			t.Error("审计里不能出现令牌明文")
		}
	})

	t.Run("签发校验：用途 1–64 字、有效期 1–90 天", func(t *testing.T) {
		for _, req := range []*model.IssueAnalysisTokenReq{
			{Name: "", Days: 30}, {Name: "   ", Days: 30}, {Name: strings.Repeat("长", 65), Days: 30}, {Name: "x", Days: 0}, {Name: "x", Days: 91},
		} {
			_, err := e.svc.IssueToken(ctx, 9, req)
			wantAgentCode(t, err, errcode.ErrInvalidParams)
		}
		for _, days := range []int{1, 90} {
			if _, err := e.svc.IssueToken(ctx, 9, &model.IssueAnalysisTokenReq{Name: "边界", Days: days}); err != nil {
				t.Errorf("%d 天合法：%v", days, err)
			}
		}
	})

	t.Run("列表不含哈希和明文", func(t *testing.T) {
		items, err := e.svc.ListTokens(ctx)
		if err != nil || len(items) == 0 {
			t.Fatalf("err=%v n=%d", err, len(items))
		}
		raw, _ := json.Marshal(items)
		if strings.Contains(string(raw), e.repo.tokens[0].TokenHash) || strings.Contains(string(raw), "token_hash") {
			t.Errorf("列表泄露了哈希：%s", raw)
		}
	})

	t.Run("注销：写审计；注销后从列表消失；不存在返回 10002；重复注销幂等", func(t *testing.T) {
		before := len(e.audit.logs)
		if err := e.svc.RevokeToken(ctx, 9, e.repo.tokens[0].ID); err != nil {
			t.Fatal(err)
		}
		if items, _ := e.svc.ListTokens(ctx); len(items) != len(e.repo.tokens)-1 {
			t.Errorf("注销后列表里少一条：%d / %d", len(items), len(e.repo.tokens))
		}
		if e.repo.tokens[0].RevokedAt == nil || len(e.audit.logs) != before+1 || e.audit.logs[before].Action != model.AdminAuditAgentTokenRevoke {
			t.Errorf("吊销与审计：%+v", e.audit.actions())
		}
		wantAgentCode(t, e.svc.RevokeToken(ctx, 9, 9999), errcode.ErrNotFound)
	})
}

func TestObserve_Authenticate(t *testing.T) {
	ctx := context.Background()
	issue := func(e *observeEnv, days int) (string, uint64) {
		out, err := e.svc.IssueToken(ctx, 9, &model.IssueAnalysisTokenReq{Name: "t", Days: days})
		if err != nil {
			t.Fatal(err)
		}
		return out.Token, out.Item.ID
	}

	t.Run("有效令牌：返回身份，并记下最近使用时间", func(t *testing.T) {
		e := newObserveEnv(t)
		tok, id := issue(e, 30)
		who, err := e.svc.AuthenticateToken(ctx, tok)
		if err != nil || who.TokenID != id || who.UserID != 9 {
			t.Fatalf("who=%+v err=%v", who, err)
		}
		if e.repo.touched[id] != 1 {
			t.Errorf("touched=%d", e.repo.touched[id])
		}
		// 一分钟内再用：不重复写库
		e.now = e.now.Add(30 * time.Second)
		_, _ = e.svc.AuthenticateToken(ctx, tok)
		if e.repo.touched[id] != 1 {
			t.Errorf("一分钟内最多写一次最近使用时间：%d", e.repo.touched[id])
		}
		e.now = e.now.Add(2 * time.Minute)
		_, _ = e.svc.AuthenticateToken(ctx, tok)
		if e.repo.touched[id] != 2 {
			t.Errorf("过了一分钟应再记一次：%d", e.repo.touched[id])
		}
	})

	t.Run("无效、吊销、过期、签发人不再是管理员：都是 401，且不泄露具体原因", func(t *testing.T) {
		e := newObserveEnv(t)
		tok, id := issue(e, 30)
		fail := func(name, token string) {
			t.Helper()
			_, err := e.svc.AuthenticateToken(ctx, token)
			var ec *errcode.Error
			if !errors.As(err, &ec) || ec.Code != errcode.ErrUnauthorized.Code {
				t.Errorf("%s：应为 10003，得到 %v", name, err)
			}
		}
		fail("不存在的令牌", "vca_"+strings.Repeat("0", 64))
		fail("不带前缀", strings.Repeat("a", 68))
		fail("空串", "")

		e.now = e.now.Add(31 * 24 * time.Hour)
		fail("过期", tok)
		e.now = obsT0

		e.roles[9] = model.RoleUser
		fail("签发人被降权", tok)
		e.roles[9] = model.RoleSuperAdmin
		if _, err := e.svc.AuthenticateToken(ctx, tok); err != nil {
			t.Errorf("super_admin 也算管理员：%v", err)
		}

		_ = e.svc.RevokeToken(ctx, 9, id)
		fail("已吊销", tok)
	})

	t.Run("限流：每个令牌每分钟 120 次，下一分钟恢复，互不影响", func(t *testing.T) {
		e := newObserveEnv(t)
		a, _ := issue(e, 30)
		b, _ := issue(e, 30)
		for i := 0; i < 120; i++ {
			if _, err := e.svc.AuthenticateToken(ctx, a); err != nil {
				t.Fatalf("第 %d 次不该被限流：%v", i+1, err)
			}
		}
		_, err := e.svc.AuthenticateToken(ctx, a)
		wantAgentCode(t, err, errcode.ErrTooManyReqs)
		if _, err := e.svc.AuthenticateToken(ctx, b); err != nil {
			t.Errorf("别的令牌不受影响：%v", err)
		}
		e.now = e.now.Add(61 * time.Second)
		if _, err := e.svc.AuthenticateToken(ctx, a); err != nil {
			t.Errorf("过了一分钟应恢复：%v", err)
		}
	})
}

func TestObserve_AdminReads(t *testing.T) {
	ctx := context.Background()
	jwtActor := ObserveActor{UserID: 9}
	tokActor := ObserveActor{UserID: 9, TokenID: 3}

	t.Run("列表：筛选条件转成查询；返回编码后的 id 和游标", func(t *testing.T) {
		e := newObserveEnv(t)
		_ = e.svc.RefreshDigest(ctx, 5)
		since := obsT0.Add(-24 * time.Hour)
		pv := 9
		page, err := e.svc.ListRuns(ctx, jwtActor, &RunListFilter{Since: since, Feedback: "down", Undone: true, Statuses: []string{"failed"}, PromptVersion: &pv, HarnessKey: "hk9", ModelKey: "claude", UserID: 1, Limit: 50, Cursor: ""})
		if err != nil {
			t.Fatal(err)
		}
		q := e.repo.listQuery
		if !q.Since.Equal(since) || q.Feedback != "down" || !q.Undone || q.Statuses[0] != "failed" || *q.PromptVersion != 9 || q.HarnessKey != "hk9" || q.ModelKey != "claude" || q.UserID != 1 || q.Limit != 50 {
			t.Errorf("query=%+v", q)
		}
		if page.NextCursor != "next-cursor" || len(page.Items) != 1 {
			t.Fatalf("page=%+v", page)
		}
		raw, _ := json.Marshal(page.Items[0])
		if !strings.Contains(string(raw), `"run_id":"`+idcodec.Encode(5)+`"`) {
			t.Errorf("run_id 应是编码串：%s", raw)
		}
		if strings.Contains(string(raw), "runtime_tail") {
			t.Error("摘要列表不含 runtime_tail 这类大字段")
		}
	})

	t.Run("非法筛选值：10001", func(t *testing.T) {
		e := newObserveEnv(t)
		_, err := e.svc.ListRuns(ctx, jwtActor, &RunListFilter{Feedback: "maybe"})
		wantAgentCode(t, err, errcode.ErrInvalidParams)
		e.repo.listErr = errBadCursor
		_, err = e.svc.ListRuns(ctx, jwtActor, &RunListFilter{Cursor: "x"})
		wantAgentCode(t, err, errcode.ErrInvalidParams)
	})

	t.Run("令牌访问写审计（含条件和条数）；管理员 JWT 自己看不写", func(t *testing.T) {
		e := newObserveEnv(t)
		_ = e.svc.RefreshDigest(ctx, 5)
		_, _ = e.svc.ListRuns(ctx, jwtActor, &RunListFilter{})
		if len(e.audit.logs) != 0 {
			t.Errorf("管理员自己看不审计：%v", e.audit.actions())
		}
		_, _ = e.svc.ListRuns(ctx, tokActor, &RunListFilter{Feedback: "down"})
		if got := e.audit.actions(); len(got) != 1 || got[0] != model.AdminAuditAgentRunsRead {
			t.Fatalf("audit=%v", got)
		}
		detail := string(e.audit.logs[0].DetailJSON)
		if !strings.Contains(detail, `"token_id":3`) || !strings.Contains(detail, `"count":1`) || !strings.Contains(detail, "down") {
			t.Errorf("detail=%s", detail)
		}
	})

	t.Run("统计：分组字段非法 10001", func(t *testing.T) {
		e := newObserveEnv(t)
		rows, err := e.svc.Stats(ctx, jwtActor, "harness_key", obsT0, time.Time{})
		if err != nil || len(rows) != 1 || e.repo.statsQ.GroupBy != "harness_key" {
			t.Fatalf("rows=%v err=%v q=%+v", rows, err, e.repo.statsQ)
		}
		_, err = e.svc.Stats(ctx, jwtActor, "user_id", time.Time{}, time.Time{})
		wantAgentCode(t, err, errcode.ErrInvalidParams)
	})

	t.Run("详情和导出：运行不存在 60012", func(t *testing.T) {
		e := newObserveEnv(t)
		_, err := e.svc.ExportRun(ctx, jwtActor, 404, ExportBoth)
		wantAgentCode(t, err, errcode.ErrAgentRunMissing)
	})
}

func TestObserve_PurgeExpired(t *testing.T) {
	e := newObserveEnv(t)
	tools, digests, err := e.svc.PurgeExpired(context.Background())
	if err != nil || tools != 3 || digests != 1 {
		t.Fatalf("tools=%d digests=%d err=%v", tools, digests, err)
	}
	if want := obsT0.AddDate(0, 0, -30); !e.repo.purgeAt.Equal(want) {
		t.Errorf("完整轨迹保留 30 天：cutoff=%v want %v", e.repo.purgeAt, want)
	}
}
