package repository_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"video-canvas/internal/model"
	. "video-canvas/internal/repository"
)

func observeDB(t *testing.T) *gorm.DB {
	return isolatedDB(t, &model.AgentSession{}, &model.AgentRun{}, &model.AgentEvent{}, &model.AgentMutation{}, &model.AgentApproval{},
		&model.AgentModelCall{}, &model.AgentModelCallBody{}, &model.AgentToolCall{}, &model.AgentFeedback{}, &model.AgentRunDigest{}, &model.AdminAPIToken{})
}

var t0 = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

// seedRun 建一个会话和一次运行，返回运行。
func seedRun(t *testing.T, db *gorm.DB, userID uint64, status string, created time.Time) *model.AgentRun {
	t.Helper()
	sess := &model.AgentSession{UserID: userID, CanvasID: 7, Title: "t"}
	if err := db.Create(sess).Error; err != nil {
		t.Fatal(err)
	}
	run := &model.AgentRun{SessionID: sess.ID, CanvasID: uint64(1000 + sess.ID), UserID: userID, Status: status, Mode: "all", CreatedAt: created,
		PromptVersion: 9, ToolsHash: "abc", BuildSHA: "7baa1a3", HarnessKey: "hk9"}
	if err := db.Create(run).Error; err != nil {
		t.Fatal(err)
	}
	return run
}

func TestObserve_ToolCalls(t *testing.T) {
	ctx := context.Background()
	db := observeDB(t)
	r := NewAgentObserveRepository(db)
	run := seedRun(t, db, 1, model.RunRunning, t0)

	t.Run("开始、结束：补记结果、摘要、耗时", func(t *testing.T) {
		c := &model.AgentToolCall{RunID: run.ID, SessionID: run.SessionID, ToolCallID: "tc1", Name: "canvas_get_state", ArgsJSON: datatypes.JSON(`{"a":1}`), StartedAt: t0}
		if err := r.StartToolCall(ctx, c); err != nil {
			t.Fatal(err)
		}
		err := r.FinishToolCall(ctx, run.ID, "tc1", model.AgentToolCallResult{ResultText: "结果", Truncated: true, IsError: true, Summary: "摘要", EndedAt: t0.Add(250 * time.Millisecond), DurationMs: 250})
		if err != nil {
			t.Fatal(err)
		}
		var got model.AgentToolCall
		db.Where("run_id = ? AND tool_call_id = ?", run.ID, "tc1").First(&got)
		if got.ResultText == nil || *got.ResultText != "结果" || !got.ResultTruncated || !got.IsError || got.Summary != "摘要" || got.DurationMs != 250 || got.EndedAt == nil {
			t.Errorf("got=%+v", got)
		}
		if string(got.ArgsJSON) != `{"a": 1}` && string(got.ArgsJSON) != `{"a":1}` {
			t.Errorf("参数应原样保存：%s", got.ArgsJSON)
		}
	})

	t.Run("同一调用重复开始：不覆盖也不报错", func(t *testing.T) {
		c := &model.AgentToolCall{RunID: run.ID, SessionID: run.SessionID, ToolCallID: "tc1", Name: "other", StartedAt: t0.Add(time.Hour)}
		if err := r.StartToolCall(ctx, c); err != nil {
			t.Fatal(err)
		}
		var n int64
		db.Model(&model.AgentToolCall{}).Where("run_id = ?", run.ID).Count(&n)
		var got model.AgentToolCall
		db.Where("tool_call_id = ?", "tc1").First(&got)
		if n != 1 || got.Name != "canvas_get_state" {
			t.Errorf("n=%d name=%s", n, got.Name)
		}
	})

	t.Run("耗时为 0 时按开始时间推算", func(t *testing.T) {
		c := &model.AgentToolCall{RunID: run.ID, SessionID: run.SessionID, ToolCallID: "tc2", Name: "codemode", StartedAt: t0}
		_ = r.StartToolCall(ctx, c)
		if err := r.FinishToolCall(ctx, run.ID, "tc2", model.AgentToolCallResult{EndedAt: t0.Add(1500 * time.Millisecond)}); err != nil {
			t.Fatal(err)
		}
		var got model.AgentToolCall
		db.Where("tool_call_id = ?", "tc2").First(&got)
		if got.DurationMs != 1500 {
			t.Errorf("duration=%d", got.DurationMs)
		}
	})

	t.Run("找不到开始记录：不是错误", func(t *testing.T) {
		if err := r.FinishToolCall(ctx, run.ID, "ghost", model.AgentToolCallResult{EndedAt: t0}); err != nil {
			t.Errorf("err=%v", err)
		}
	})
}

func TestObserve_Feedback(t *testing.T) {
	ctx := context.Background()
	db := observeDB(t)
	r := NewAgentObserveRepository(db)
	run := seedRun(t, db, 1, model.RunSucceeded, t0)

	if _, err := r.GetFeedback(ctx, run.ID, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("没评价应返回 ErrNotFound：%v", err)
	}
	fb := &model.AgentFeedback{RunID: run.ID, UserID: 1, Rating: model.FeedbackDown, Reasons: datatypes.JSON(`["misread"]`), Comment: "不对"}
	if err := r.UpsertFeedback(ctx, fb); err != nil {
		t.Fatal(err)
	}
	// 改选：同一人同一运行覆盖
	if err := r.UpsertFeedback(ctx, &model.AgentFeedback{RunID: run.ID, UserID: 1, Rating: model.FeedbackUp, Reasons: datatypes.JSON(`[]`)}); err != nil {
		t.Fatal(err)
	}
	got, err := r.GetFeedback(ctx, run.ID, 1)
	if err != nil || got.Rating != model.FeedbackUp || got.Comment != "" {
		t.Fatalf("改选应覆盖：%+v %v", got, err)
	}
	var n int64
	db.Model(&model.AgentFeedback{}).Where("run_id = ?", run.ID).Count(&n)
	if n != 1 {
		t.Errorf("每人每运行只有一条，得到 %d", n)
	}
	// 取消
	if err := r.DeleteFeedback(ctx, run.ID, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := r.GetFeedback(ctx, run.ID, 1); !errors.Is(err, ErrNotFound) {
		t.Error("取消后应查不到")
	}
	if err := r.DeleteFeedback(ctx, run.ID, 1); err != nil {
		t.Errorf("重复取消是幂等的：%v", err)
	}
}

func TestObserve_RunFacts(t *testing.T) {
	ctx := context.Background()
	db := observeDB(t)
	r := NewAgentObserveRepository(db)
	run := seedRun(t, db, 1, model.RunSucceeded, t0)
	other := seedRun(t, db, 2, model.RunSucceeded, t0)

	db.Create(&model.AgentToolCall{RunID: run.ID, SessionID: run.SessionID, ToolCallID: "a", Name: "x", ArgsJSON: datatypes.JSON(`{"big":1}`), StartedAt: t0, IsError: true})
	db.Create(&model.AgentToolCall{RunID: other.ID, SessionID: other.SessionID, ToolCallID: "b", Name: "x", StartedAt: t0})
	db.Create(&model.AgentModelCall{RunID: run.ID, UserID: 1, ModelKey: "m", Status: model.ModelCallSettled, InputTokens: 10, OutputTokens: 5})
	db.Create(&model.AgentApproval{RunID: run.ID, SessionID: run.SessionID, CanvasID: run.CanvasID, UserID: 1, Kind: model.ApprovalGenerate, Status: model.ApprovalRejected, ExpiresAt: t0})
	db.Create(&model.AgentMutation{RunID: run.ID, CanvasID: run.CanvasID, UserID: 1, Seq: 1, Kind: model.MutationApplyOps})
	for i, typ := range []string{"message.user", "message.done", "message.user"} {
		db.Create(&model.AgentEvent{SessionID: run.SessionID, RunID: run.ID, Seq: int64(i + 1), Type: typ, PayloadJSON: datatypes.JSON(`{"text":"hi"}`)})
	}
	db.Create(&model.AgentFeedback{RunID: run.ID, UserID: 1, Rating: -1, Reasons: datatypes.JSON(`[]`)})
	db.Create(&model.AgentFeedback{RunID: run.ID, UserID: 99, Rating: 1, Reasons: datatypes.JSON(`[]`)}) // 别人的评价不算

	f, err := r.RunFacts(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if f.Run.ID != run.ID || len(f.ToolCalls) != 1 || len(f.ModelCalls) != 1 || len(f.Approvals) != 1 || len(f.Mutations) != 1 {
		t.Errorf("事实不全或串了别的运行：%+v", f)
	}
	if len(f.UserMessages) != 2 || f.UserMessages[0].Seq != 1 {
		t.Errorf("本轮的用户消息按序号升序：%+v", f.UserMessages)
	}
	if f.Feedback == nil || f.Feedback.Rating != -1 {
		t.Errorf("评价只取运行所有者的：%+v", f.Feedback)
	}
	if len(f.ToolCalls[0].ArgsJSON) != 0 {
		t.Errorf("摘要用不到参数，不该读大字段：%s", f.ToolCalls[0].ArgsJSON)
	}
	if f.SessionDeleted {
		t.Error("会话没删")
	}
	db.Delete(&model.AgentSession{}, run.SessionID)
	f, _ = r.RunFacts(ctx, run.ID)
	if !f.SessionDeleted {
		t.Error("会话被软删除后要能标出来")
	}
	if _, err := r.RunFacts(ctx, 999999); !errors.Is(err, ErrNotFound) {
		t.Errorf("运行不存在：%v", err)
	}
}

func TestObserve_DigestUpsertAndList(t *testing.T) {
	ctx := context.Background()
	db := observeDB(t)
	r := NewAgentObserveRepository(db)

	mk := func(i int, user uint64, status, harness string, pv int, fb int, undone bool, model_ string) *model.AgentRunDigest {
		end := t0.Add(time.Duration(i) * time.Hour)
		return &model.AgentRunDigest{RunID: uint64(i), SessionID: 1, UserID: user, CanvasID: 1, Status: status, HarnessKey: harness, PromptVersion: pv,
			Feedback: fb, Undone: undone, ModelKey: model_, EndedAt: &end, CreatedAt: end, Skills: datatypes.JSON(`{}`)}
	}
	rows := []*model.AgentRunDigest{
		mk(1, 1, model.RunSucceeded, "h9", 9, 0, false, "claude"),
		mk(2, 1, model.RunSucceeded, "h9", 9, -1, true, "claude"),
		mk(3, 2, model.RunFailed, "h9", 9, 0, false, "deepseek"),
		mk(4, 2, model.RunSucceeded, "h10", 10, 1, false, "claude"),
		mk(5, 1, model.RunStepLimit, "h10", 10, -1, false, "claude"),
	}
	for _, d := range rows {
		if err := r.UpsertDigest(ctx, d); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("再次写同一运行：覆盖而不是重复", func(t *testing.T) {
		d := mk(1, 1, model.RunSucceeded, "h9", 9, 1, false, "claude")
		d.Steps = 12
		if err := r.UpsertDigest(ctx, d); err != nil {
			t.Fatal(err)
		}
		got, err := r.GetDigest(ctx, 1)
		if err != nil || got.Feedback != 1 || got.Steps != 12 {
			t.Fatalf("got=%+v err=%v", got, err)
		}
		var n int64
		db.Model(&model.AgentRunDigest{}).Count(&n)
		if n != 5 {
			t.Errorf("行数=%d", n)
		}
		// 恢复
		_ = r.UpsertDigest(ctx, rows[0])
	})

	ids := func(items []model.AgentRunDigest) string {
		var s []string
		for _, d := range items {
			s = append(s, fmt.Sprint(d.RunID))
		}
		return strings.Join(s, ",")
	}
	cases := []struct {
		name string
		q    model.DigestQuery
		want string
	}{
		{"不限：按创建时间倒序", model.DigestQuery{}, "5,4,3,2,1"},
		{"点踩", model.DigestQuery{Feedback: "down"}, "5,2"},
		{"点赞", model.DigestQuery{Feedback: "up"}, "4"},
		{"没评价", model.DigestQuery{Feedback: "none"}, "3,1"},
		{"any 等于不限", model.DigestQuery{Feedback: "any"}, "5,4,3,2,1"},
		{"被撤销", model.DigestQuery{Undone: true}, "2"},
		{"状态", model.DigestQuery{Statuses: []string{model.RunFailed, model.RunStepLimit}}, "5,3"},
		{"Harness", model.DigestQuery{HarnessKey: "h10"}, "5,4"},
		{"提示词版本", model.DigestQuery{PromptVersion: ptr(9)}, "3,2,1"},
		{"模型", model.DigestQuery{ModelKey: "deepseek"}, "3"},
		{"用户", model.DigestQuery{UserID: 2}, "4,3"},
		{"时间范围含起不含止", model.DigestQuery{Since: t0.Add(2 * time.Hour), Until: t0.Add(4 * time.Hour)}, "3,2"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			items, _, err := r.ListDigests(ctx, c.q)
			if err != nil {
				t.Fatal(err)
			}
			if got := ids(items); got != c.want {
				t.Errorf("got %s want %s", got, c.want)
			}
		})
	}

	t.Run("分页：游标不重不漏", func(t *testing.T) {
		var all []string
		cursor := ""
		for pages := 0; pages < 10; pages++ {
			items, next, err := r.ListDigests(ctx, model.DigestQuery{Limit: 2, Cursor: cursor})
			if err != nil {
				t.Fatal(err)
			}
			all = append(all, ids(items))
			if next == "" {
				break
			}
			cursor = next
		}
		if got := strings.Join(all, "|"); got != "5,4|3,2|1" {
			t.Errorf("got %s", got)
		}
	})

	t.Run("页码分页：按页取、带总数，页码越界返回空页", func(t *testing.T) {
		var got []string
		for page := 1; page <= 3; page++ {
			items, next, err := r.ListDigests(ctx, model.DigestQuery{Page: page, Limit: 2})
			if err != nil {
				t.Fatal(err)
			}
			if next != "" {
				t.Errorf("页码模式不返回游标：%q", next)
			}
			got = append(got, ids(items))
		}
		if strings.Join(got, "|") != "5,4|3,2|1" {
			t.Errorf("got %v", got)
		}
		if items, _, _ := r.ListDigests(ctx, model.DigestQuery{Page: 9, Limit: 2}); len(items) != 0 {
			t.Errorf("越界页应为空：%d", len(items))
		}
		total, err := r.CountDigests(ctx, model.DigestQuery{})
		if err != nil || total != 5 {
			t.Errorf("总数：%d %v", total, err)
		}
		total, _ = r.CountDigests(ctx, model.DigestQuery{Feedback: "down"})
		if total != 2 {
			t.Errorf("总数要带筛选条件：%d", total)
		}
		// 翻页时筛选条件照样生效
		items, _, _ := r.ListDigests(ctx, model.DigestQuery{Page: 1, Limit: 1, Feedback: "down"})
		if ids(items) != "5" {
			t.Errorf("筛选 + 分页：%s", ids(items))
		}
	})

	t.Run("同一创建时间的多条：靠 run_id 保证游标稳定", func(t *testing.T) {
		same := t0.Add(100 * time.Hour)
		for i := 10; i < 13; i++ {
			d := mk(i, 1, model.RunSucceeded, "hx", 9, 0, false, "claude")
			d.CreatedAt = same
			_ = r.UpsertDigest(ctx, d)
		}
		var got []string
		cursor := ""
		for pages := 0; pages < 5; pages++ {
			items, next, _ := r.ListDigests(ctx, model.DigestQuery{HarnessKey: "hx", Limit: 1, Cursor: cursor})
			got = append(got, ids(items))
			if next == "" {
				break
			}
			cursor = next
		}
		if strings.Join(got, ",") != "12,11,10" {
			t.Errorf("got %v", got)
		}
	})

	t.Run("非法游标：返回错误而不是从头再来", func(t *testing.T) {
		if _, _, err := r.ListDigests(ctx, model.DigestQuery{Cursor: "garbage"}); !errors.Is(err, ErrInvalidCursor) {
			t.Errorf("err=%v", err)
		}
	})

	t.Run("条数上限 500", func(t *testing.T) {
		items, _, err := r.ListDigests(ctx, model.DigestQuery{Limit: 100000})
		if err != nil || len(items) == 0 {
			t.Errorf("err=%v n=%d", err, len(items))
		}
	})
}

func TestObserve_Stats(t *testing.T) {
	ctx := context.Background()
	db := observeDB(t)
	r := NewAgentObserveRepository(db)
	mk := func(id uint64, harness string, status string, fb int, muts int, undone bool, steps int, dur int64, credits int) *model.AgentRunDigest {
		return &model.AgentRunDigest{RunID: id, UserID: 1, SessionID: 1, CanvasID: 1, HarnessKey: harness, PromptVersion: 9, ModelKey: "claude", Status: status, Feedback: fb,
			MutationCount: muts, Undone: undone, Steps: steps, DurationMs: dur, SpentCredits: credits, CreatedAt: t0, Skills: datatypes.JSON(`{}`)}
	}
	for _, d := range []*model.AgentRunDigest{
		mk(1, "h9", model.RunSucceeded, 0, 2, false, 10, 1000, 10),
		mk(2, "h9", model.RunSucceeded, -1, 3, true, 20, 3000, 30),
		mk(3, "h9", model.RunFailed, 1, 0, false, 30, 2000, 20),
		mk(4, "h9", model.RunStepLimit, 0, 1, false, 40, 4000, 40),
		mk(5, "h10", model.RunSucceeded, 1, 1, false, 8, 500, 5),
	} {
		if err := r.UpsertDigest(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := r.Stats(ctx, model.StatsQuery{GroupBy: "harness_key"})
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]model.AgentStatsRow{}
	for _, row := range rows {
		by[row.Key] = row
	}
	h9 := by["h9"]
	approx := func(name string, got, want float64) {
		t.Helper()
		if got < want-0.001 || got > want+0.001 {
			t.Errorf("%s: got %v want %v", name, got, want)
		}
	}
	if h9.N != 4 {
		t.Fatalf("h9=%+v", h9)
	}
	approx("反馈率", h9.FeedbackRate, 0.5)        // 2/4
	approx("点踩率", h9.DownRate, 0.5)            // 1 点踩 / 2 有评价
	approx("撤销率", h9.UndoRate, 1.0/3)          // 1 个被撤销 ÷ 3 个改过画布的运行（run 3 没改画布，不进分母）
	approx("失败结束率", h9.FailEndRate, 0.5)       // failed + step_limit
	approx("耗时中位数", h9.MedianDurationMs, 2500) // 1000,2000,3000,4000
	approx("步数中位数", h9.MedianSteps, 25)
	approx("平均积分", h9.AvgCredits, 25)
	if h9.PromptVersion != 9 {
		t.Errorf("按 harness_key 分组时带上提示词版本：%+v", h9)
	}
	if by["h10"].N != 1 {
		t.Errorf("h10=%+v", by["h10"])
	}

	if _, err := r.Stats(ctx, model.StatsQuery{GroupBy: "user_id; DROP TABLE x"}); !errors.Is(err, ErrInvalidGroupBy) {
		t.Errorf("分组字段只能来自白名单：%v", err)
	}
	pv, err := r.Stats(ctx, model.StatsQuery{GroupBy: "prompt_version"})
	if err != nil || len(pv) != 1 || pv[0].Key != "9" || pv[0].N != 5 {
		t.Errorf("按提示词版本分组：%+v %v", pv, err)
	}
	none, _ := r.Stats(ctx, model.StatsQuery{GroupBy: "harness_key", Since: t0.Add(time.Hour)})
	if len(none) != 0 {
		t.Errorf("时间范围外没有数据：%+v", none)
	}
}

func TestObserve_Purge(t *testing.T) {
	ctx := context.Background()
	db := observeDB(t)
	r := NewAgentObserveRepository(db)
	run := seedRun(t, db, 1, model.RunSucceeded, t0)
	res := "结果"
	old := t0.Add(-40 * 24 * time.Hour)
	fresh := t0.Add(-1 * 24 * time.Hour)
	db.Create(&model.AgentToolCall{RunID: run.ID, SessionID: run.SessionID, ToolCallID: "old", Name: "x", ArgsJSON: datatypes.JSON(`{"a":1}`), ResultText: &res, StartedAt: old, Summary: "留下"})
	db.Create(&model.AgentToolCall{RunID: run.ID, SessionID: run.SessionID, ToolCallID: "new", Name: "x", ArgsJSON: datatypes.JSON(`{"a":1}`), ResultText: &res, StartedAt: fresh})
	tail := "stderr"
	oldEnd, freshEnd := old, fresh
	_ = r.UpsertDigest(ctx, &model.AgentRunDigest{RunID: 1, UserID: 1, SessionID: 1, CanvasID: 1, RuntimeTail: &tail, EndedAt: &oldEnd, CreatedAt: old, Skills: datatypes.JSON(`{}`)})
	_ = r.UpsertDigest(ctx, &model.AgentRunDigest{RunID: 2, UserID: 1, SessionID: 1, CanvasID: 1, RuntimeTail: &tail, EndedAt: &freshEnd, CreatedAt: fresh, Skills: datatypes.JSON(`{}`)})

	cutoff := t0.Add(-30 * 24 * time.Hour)
	tools, digests, err := r.PurgeObserveBefore(ctx, cutoff, t0)
	if err != nil || tools != 1 || digests != 1 {
		t.Fatalf("tools=%d digests=%d err=%v", tools, digests, err)
	}
	var oldC, newC model.AgentToolCall
	db.Where("tool_call_id = ?", "old").First(&oldC)
	db.Where("tool_call_id = ?", "new").First(&newC)
	if len(oldC.ArgsJSON) != 0 && string(oldC.ArgsJSON) != "null" || oldC.ResultText != nil || oldC.PurgedAt == nil || oldC.Summary != "留下" || oldC.Name != "x" {
		t.Errorf("旧的大字段应清空，其余保留：%+v", oldC)
	}
	if newC.ResultText == nil || newC.PurgedAt != nil {
		t.Errorf("没过期的不动：%+v", newC)
	}
	d1, _ := r.GetDigest(ctx, 1)
	d2, _ := r.GetDigest(ctx, 2)
	if d1.RuntimeTail != nil || d2.RuntimeTail == nil {
		t.Errorf("只清过期摘要的 runtime_tail：%v %v", d1.RuntimeTail, d2.RuntimeTail)
	}
	// 再跑一次：没有新的可清，幂等
	tools, digests, _ = r.PurgeObserveBefore(ctx, cutoff, t0)
	if tools != 0 || digests != 0 {
		t.Errorf("重复清理应为 0：%d %d", tools, digests)
	}
}

func TestObserve_Tokens(t *testing.T) {
	ctx := context.Background()
	db := observeDB(t)
	r := NewAgentObserveRepository(db)
	tok := &model.AdminAPIToken{Name: "本机", Prefix: "vca_9f2c", TokenHash: strings.Repeat("a", 64), Scope: model.TokenScopeAgentRead, CreatedBy: 5, ExpiresAt: t0.Add(24 * time.Hour)}
	if err := r.CreateToken(ctx, tok); err != nil || tok.ID == 0 {
		t.Fatalf("err=%v id=%d", err, tok.ID)
	}
	dup := &model.AdminAPIToken{Name: "x", Prefix: "vca_xxxx", TokenHash: strings.Repeat("a", 64), Scope: model.TokenScopeAgentRead, CreatedBy: 5, ExpiresAt: t0}
	if err := r.CreateToken(ctx, dup); !errors.Is(err, ErrDuplicate) {
		t.Errorf("hash 重复：%v", err)
	}
	got, err := r.TokenByHash(ctx, strings.Repeat("a", 64))
	if err != nil || got.ID != tok.ID {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if _, err := r.TokenByHash(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err=%v", err)
	}
	if err := r.TouchToken(ctx, tok.ID, t0); err != nil {
		t.Fatal(err)
	}
	list, _ := r.ListTokens(ctx)
	if len(list) != 1 || list[0].LastUsedAt == nil {
		t.Errorf("list=%+v", list)
	}
	if err := r.RevokeToken(ctx, tok.ID, t0); err != nil {
		t.Fatal(err)
	}
	got, _ = r.TokenByHash(ctx, strings.Repeat("a", 64))
	if got.RevokedAt == nil {
		t.Error("应已注销")
	}
	// 注销后列表里不再返回；按哈希仍查得到（鉴权要据此判断已注销）
	if list, _ := r.ListTokens(ctx); len(list) != 0 {
		t.Errorf("已注销的令牌不出现在列表里：%+v", list)
	}
	first := *got.RevokedAt
	if err := r.RevokeToken(ctx, tok.ID, t0.Add(time.Hour)); err != nil {
		t.Errorf("重复吊销是幂等的：%v", err)
	}
	got, _ = r.TokenByHash(ctx, strings.Repeat("a", 64))
	if !got.RevokedAt.Equal(first) {
		t.Error("重复吊销不改变第一次吊销的时间")
	}
	if err := r.RevokeToken(ctx, 9999, t0); !errors.Is(err, ErrNotFound) {
		t.Errorf("不存在的令牌：%v", err)
	}
}

func TestObserve_ExportFacts(t *testing.T) {
	ctx := context.Background()
	db := observeDB(t)
	r := NewAgentObserveRepository(db)
	sess := &model.AgentSession{UserID: 1, CanvasID: 7, Title: "t"}
	db.Create(sess)
	mkRun := func(canvas uint64) *model.AgentRun {
		run := &model.AgentRun{SessionID: sess.ID, CanvasID: canvas, UserID: 1, Status: model.RunSucceeded, Mode: "all", CreatedAt: t0}
		db.Create(run)
		return run
	}
	runs := []*model.AgentRun{mkRun(11), mkRun(12), mkRun(13), mkRun(14)}
	seq := int64(0)
	ev := func(run *model.AgentRun, typ, payload string) {
		seq++
		db.Create(&model.AgentEvent{SessionID: sess.ID, RunID: run.ID, Seq: seq, Type: typ, PayloadJSON: datatypes.JSON(payload)})
	}
	for i, run := range runs {
		ev(run, "message.user", fmt.Sprintf(`{"text":"问题%d"}`, i+1))
		ev(run, "tool.start", `{"id":"x"}`)
		ev(run, "message.done", fmt.Sprintf(`{"text":"回答%d"}`, i+1))
	}
	cur := runs[3]
	res := "完整结果"
	db.Create(&model.AgentToolCall{RunID: cur.ID, SessionID: sess.ID, ToolCallID: "tc", Name: "x", ArgsJSON: datatypes.JSON(`{"a":1}`), ResultText: &res, StartedAt: t0})
	_ = r.UpsertDigest(ctx, &model.AgentRunDigest{RunID: cur.ID, UserID: 1, SessionID: sess.ID, CanvasID: 14, Skills: datatypes.JSON(`{}`), CreatedAt: t0})

	f, err := r.ExportFacts(ctx, cur.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Events) != 3 || f.Events[0].Seq != seq-2 {
		t.Errorf("本轮事件按序号升序：%+v", f.Events)
	}
	if len(f.ToolCalls) != 1 || f.ToolCalls[0].ResultText == nil || *f.ToolCalls[0].ResultText != "完整结果" {
		t.Errorf("导出要带工具参数和结果：%+v", f.ToolCalls)
	}
	if f.Digest.RunID != cur.ID {
		t.Errorf("digest=%+v", f.Digest)
	}
	// 上下文：同会话更早两轮（第 2、3 轮）的用户消息和最终回复，不含工具事件，不含本轮
	var texts []string
	for _, e := range f.Context {
		if e.Type != "message.user" && e.Type != "message.done" {
			t.Errorf("上下文只要用户消息和最终回复：%s", e.Type)
		}
		texts = append(texts, string(e.PayloadJSON))
	}
	if len(f.Context) != 4 || !strings.Contains(texts[0], "问题2") || !strings.Contains(texts[3], "回答3") {
		t.Errorf("context=%v", texts)
	}
	if _, err := r.ExportFacts(ctx, 99999); !errors.Is(err, ErrNotFound) {
		t.Errorf("err=%v", err)
	}
}

// 进程退出时留下的 stderr 末尾，之后无论摘要怎么刷新都要沿用：RunFacts 要从旧摘要里把它带出来。
func TestObserve_RunFacts_KeepsRuntimeTail(t *testing.T) {
	ctx := context.Background()
	db := observeDB(t)
	r := NewAgentObserveRepository(db)
	run := seedRun(t, db, 1, model.RunSucceeded, t0)
	tail := "诊断：摘要压缩失败"
	if err := r.UpsertDigest(ctx, &model.AgentRunDigest{RunID: run.ID, UserID: 1, SessionID: run.SessionID, CanvasID: run.CanvasID, RuntimeTail: &tail, CreatedAt: t0, Skills: datatypes.JSON(`{}`)}); err != nil {
		t.Fatal(err)
	}
	f, err := r.RunFacts(ctx, run.ID)
	if err != nil || f.RuntimeTail != tail {
		t.Fatalf("tail=%q err=%v", f.RuntimeTail, err)
	}
}

// 用真实的仓储和服务走一遍：事件、工具调用、反馈写进库 → 刷新摘要 → 导出。
// 假仓储测不到的是真实的 jsonb 读写、可空列和排序。
func TestObserve_EndToEnd(t *testing.T) {
	ctx := context.Background()
	db := observeDB(t)
	repo := NewAgentObserveRepository(db)
	run := seedRun(t, db, 1, model.RunSucceeded, t0)
	end := t0.Add(95 * time.Second)
	db.Model(run).Update("ended_at", end)
	run.EndedAt = &end

	ev := func(seq int64, typ, payload string) {
		db.Create(&model.AgentEvent{SessionID: run.SessionID, RunID: run.ID, Seq: seq, Type: typ, PayloadJSON: datatypes.JSON(payload)})
	}
	ev(1, "message.user", `{"text":"把剧本拆成 6 个镜头"}`)
	ev(2, "message.done", `{"text":"先读技能","tool_calls":[{"id":"tc1","name":"skill_read"}]}`)
	ev(3, "tool.start", `{"id":"tc1","name":"skill_read"}`)
	ev(4, "tool.end", `{"id":"tc1","name":"skill_read","is_error":false,"summary":"读取技能 script-breakdown@v3"}`)
	ev(5, "tool.end", `{"id":"rej","name":"canvas_delete","is_error":true,"summary":"模式不允许"}`)

	start := &model.AgentToolCall{RunID: run.ID, SessionID: run.SessionID, ToolCallID: "tc1", Name: "skill_read", ArgsJSON: datatypes.JSON(`{"name":"script-breakdown"}`), StartedAt: t0.Add(time.Second)}
	if err := repo.StartToolCall(ctx, start); err != nil {
		t.Fatal(err)
	}
	if err := repo.FinishToolCall(ctx, run.ID, "tc1", model.AgentToolCallResult{ResultText: "正文", Summary: "读取技能 script-breakdown@v3", EndedAt: t0.Add(2 * time.Second), DurationMs: 1000}); err != nil {
		t.Fatal(err)
	}
	// 被拒绝的调用没有参数（NULL jsonb）：导出不能因此出错
	_ = repo.StartToolCall(ctx, &model.AgentToolCall{RunID: run.ID, SessionID: run.SessionID, ToolCallID: "rej", Name: "canvas_delete", StartedAt: t0.Add(3 * time.Second)})
	_ = repo.FinishToolCall(ctx, run.ID, "rej", model.AgentToolCallResult{ResultText: "模式不允许", IsError: true, Summary: "模式不允许", EndedAt: t0.Add(3 * time.Second)})
	db.Create(&model.AgentModelCall{RunID: run.ID, UserID: 1, ModelKey: "claude", Status: model.ModelCallSettled, InputTokens: 100, OutputTokens: 20, Purpose: "main", ChannelKey: "ch1", StopReason: "stop"})

	svc := agentsvcNewObserve(repo, run)
	if _, err := svc.SetFeedback(ctx, 1, run.ID, &model.SetAgentFeedbackReq{Rating: -1, Reasons: []string{"misread"}, Comment: "不对"}); err != nil {
		t.Fatal(err)
	}
	d, err := repo.GetDigest(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d.Feedback != -1 || d.ToolCalls != 2 || d.ToolErrors != 1 || d.ModelCalls != 1 || d.InputTokens != 100 || d.RequestHead != "把剧本拆成 6 个镜头" || d.DurationMs != 95000 {
		t.Errorf("摘要：%+v", d)
	}
	if string(d.Skills) != `{"script-breakdown": "v3"}` && string(d.Skills) != `{"script-breakdown":"v3"}` {
		t.Errorf("skills=%s", d.Skills)
	}

	out, err := svc.ExportRun(ctx, agentsvcActor(), run.ID, "both")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"把剧本拆成 6 个镜头", `参数：{"name":"script-breakdown"}`, "结果：正文", "canvas_delete", "✘ 报错", "模式不允许", "没理解我的需求", "「不对」", "script-breakdown: v3"} {
		if !strings.Contains(out.Content, want) {
			t.Errorf("Markdown 缺少 %q\n%s", want, out.Content)
		}
	}
	if out.Data == nil || len(out.Data.ToolCalls) != 2 || out.Data.Digest == nil || out.Data.Feedback == nil {
		t.Errorf("data=%+v", out.Data)
	}
}

// 版本对比卡片要「最近几个版本」：分组按最近一次运行的时间倒序，并带上 last_at。
func TestObserve_StatsOrdersByRecency(t *testing.T) {
	ctx := context.Background()
	db := observeDB(t)
	r := NewAgentObserveRepository(db)
	mk := func(id uint64, harness string, created time.Time) *model.AgentRunDigest {
		return &model.AgentRunDigest{RunID: id, UserID: 1, SessionID: 1, CanvasID: 1, HarnessKey: harness, ModelKey: "m", Status: model.RunSucceeded, CreatedAt: created, Skills: datatypes.JSON(`{}`)}
	}
	// h-old 运行最多但都是旧的；h-new 只有一次，但最近；h-mid 居中
	for _, d := range []*model.AgentRunDigest{
		mk(1, "h-old", t0), mk(2, "h-old", t0.Add(time.Hour)), mk(3, "h-old", t0.Add(2*time.Hour)),
		mk(4, "h-mid", t0.Add(5*time.Hour)),
		mk(5, "h-new", t0.Add(9*time.Hour)),
	} {
		if err := r.UpsertDigest(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := r.Stats(ctx, model.StatsQuery{GroupBy: "harness_key"})
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, row := range rows {
		keys = append(keys, row.Key)
	}
	if strings.Join(keys, ",") != "h-new,h-mid,h-old" {
		t.Errorf("应按最近一次运行倒序：%v", keys)
	}
	if rows[0].PromptVersion != 0 || rows[0].BuildSHA != "" {
		t.Errorf("这组摘要没填版本，应是零值：%+v", rows[0])
	}
	if !rows[0].LastAt.Equal(t0.Add(9*time.Hour)) || !rows[2].LastAt.Equal(t0.Add(2*time.Hour)) {
		t.Errorf("last_at：%v %v", rows[0].LastAt, rows[2].LastAt)
	}
}

func TestObserve_ModelCallBodies(t *testing.T) {
	ctx := context.Background()
	db := observeDB(t)
	r := NewAgentObserveRepository(db)
	run := seedRun(t, db, 1, model.RunSucceeded, t0)
	other := seedRun(t, db, 1, model.RunSucceeded, t0)
	mk := func(call, runID uint64, created time.Time, body string) *model.AgentModelCallBody {
		b := &model.AgentModelCallBody{CallID: call, RunID: runID, MessageCount: 2, ToolCount: 1, RawBytes: len(body), Params: datatypes.JSON(`{"model":"m"}`), CreatedAt: created}
		if body != "" {
			b.Body = []byte(body)
		}
		return b
	}

	t.Run("保存后按运行读出：按调用 id 升序，只取自己运行的", func(t *testing.T) {
		for _, b := range []*model.AgentModelCallBody{mk(12, run.ID, t0, "BODY-12"), mk(11, run.ID, t0, "BODY-11"), mk(13, other.ID, t0, "BODY-13")} {
			if err := r.SaveModelCallBody(ctx, b); err != nil {
				t.Fatal(err)
			}
		}
		got, err := r.ModelCallRequests(ctx, run.ID, true)
		if err != nil || len(got) != 2 || got[0].CallID != 11 || got[1].CallID != 12 || string(got[0].Body) != "BODY-11" {
			t.Fatalf("got=%+v err=%v", got, err)
		}
		if got[0].MessageCount != 2 || got[0].ToolCount != 1 || !strings.Contains(string(got[0].Params), `"model"`) {
			t.Errorf("摘要字段应一起保存: %+v", got[0])
		}
	})

	t.Run("withBody=false 不读请求体，摘要照常返回", func(t *testing.T) {
		got, err := r.ModelCallRequests(ctx, run.ID, false)
		if err != nil || len(got) != 2 || len(got[0].Body) != 0 || got[0].RawBytes != len("BODY-11") {
			t.Fatalf("got=%+v err=%v", got, err)
		}
	})

	t.Run("同一次调用重复保存：保留第一条，不报错", func(t *testing.T) {
		if err := r.SaveModelCallBody(ctx, mk(11, run.ID, t0, "CHANGED")); err != nil {
			t.Fatalf("重复保存不该报错: %v", err)
		}
		got, _ := r.ModelCallRequests(ctx, run.ID, true)
		if string(got[0].Body) != "BODY-11" {
			t.Errorf("应保留第一条: %s", got[0].Body)
		}
	})

	t.Run("导出事实带请求摘要，不带请求体", func(t *testing.T) {
		f, err := r.ExportFacts(ctx, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(f.Requests) != 2 || f.Requests[0].CallID != 11 || len(f.Requests[0].Body) != 0 || f.Requests[0].RawBytes == 0 {
			t.Errorf("Requests=%+v", f.Requests)
		}
	})

	t.Run("清理：整行删除早于 cutoff 的，没过期的不动，重复执行幂等", func(t *testing.T) {
		old := t0.Add(-8 * 24 * time.Hour)
		if err := r.SaveModelCallBody(ctx, mk(20, run.ID, old, "OLD")); err != nil {
			t.Fatal(err)
		}
		cutoff := t0.Add(-7 * 24 * time.Hour)
		n, err := r.PurgeModelCallBodiesBefore(ctx, cutoff)
		if err != nil || n != 1 {
			t.Fatalf("n=%d err=%v", n, err)
		}
		got, _ := r.ModelCallRequests(ctx, run.ID, false)
		if len(got) != 2 {
			t.Errorf("过期的删掉，其余保留: %+v", got)
		}
		if n, _ := r.PurgeModelCallBodiesBefore(ctx, cutoff); n != 0 {
			t.Errorf("重复清理应为 0: %d", n)
		}
	})
}

// 运行本身在 Agent 服务里（agent_runs 没有这一行）时，Go 自己持有的那部分：工具调用、模型调用、画布改动。
func TestObserve_LocalFactsForRunsOwnedByAgent(t *testing.T) {
	ctx := context.Background()
	db := observeDB(t)
	r := NewAgentObserveRepository(db)
	const runID, other = uint64(281474976710657123), uint64(281474976710657124)
	res := "完整结果"
	db.Create(&model.AgentToolCall{RunID: runID, SessionID: 1, ToolCallID: "a", Name: "x", ArgsJSON: datatypes.JSON(`{"big":1}`), ResultText: &res, StartedAt: t0, IsError: true})
	db.Create(&model.AgentToolCall{RunID: other, SessionID: 1, ToolCallID: "b", Name: "x", StartedAt: t0})
	db.Create(&model.AgentModelCall{RunID: runID, UserID: 1, ModelKey: "m", Status: model.ModelCallSettled, InputTokens: 10, OutputTokens: 5})
	db.Create(&model.AgentMutation{RunID: runID, CanvasID: 7, UserID: 1, Seq: 1, Kind: model.MutationApplyOps, ChangesJSON: datatypes.JSON(`[{"x":1}]`)})
	tail := "stderr"
	_ = r.UpsertDigest(ctx, &model.AgentRunDigest{RunID: runID, UserID: 1, SessionID: 1, CanvasID: 7, Skills: datatypes.JSON(`{}`), RuntimeTail: &tail, CreatedAt: t0})

	f, err := r.LocalRunFacts(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.ToolCalls) != 1 || len(f.ModelCalls) != 1 || len(f.Mutations) != 1 || f.RuntimeTail != "stderr" {
		t.Errorf("事实不全或串了别的运行：%+v", f)
	}
	if len(f.ToolCalls[0].ArgsJSON) != 0 || f.ToolCalls[0].ResultText != nil || len(f.Mutations[0].ChangesJSON) != 0 {
		t.Errorf("摘要用不到大字段，不该读：%s %v %s", f.ToolCalls[0].ArgsJSON, f.ToolCalls[0].ResultText, f.Mutations[0].ChangesJSON)
	}
	empty, err := r.LocalRunFacts(ctx, 999)
	if err != nil || len(empty.ToolCalls)+len(empty.ModelCalls)+len(empty.Mutations) != 0 {
		t.Errorf("没有记录不是错误：%v %+v", err, empty)
	}

	x, err := r.LocalExportFacts(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if x.Digest.RunID != runID || len(x.ToolCalls) != 1 || x.ToolCalls[0].ResultText == nil || *x.ToolCalls[0].ResultText != "完整结果" || len(x.ModelCalls) != 1 || len(x.Mutations) != 1 || string(x.Mutations[0].ChangesJSON) == "" {
		t.Errorf("导出要带参数、结果和改动明细：%+v", x)
	}
	if x.RuntimeTail != "stderr" {
		t.Errorf("runtime_tail：%q", x.RuntimeTail)
	}
	none, err := r.LocalExportFacts(ctx, 999)
	if err != nil || none.Digest.RunID != 0 {
		t.Errorf("没有摘要也不是错误：%v %+v", err, none.Digest)
	}
}
