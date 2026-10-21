package agent_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
	. "video-canvas/internal/service/agent"
)

func sp(s string) *string { return &s }

func exportFacts() *model.AgentExportFacts {
	end := obsT0.Add(3*time.Minute + 12*time.Second)
	run := model.AgentRun{ID: 5, SessionID: 2, CanvasID: 7, UserID: 1, Status: model.RunSucceeded, Mode: "storyboard", Steps: 3, SpentCredits: 42,
		PromptVersion: 9, ToolsHash: "abc", BuildSHA: "7baa1a3", HarnessKey: "hk9", CreatedAt: obsT0, EndedAt: &end}
	digest := model.AgentRunDigest{RunID: 5, UserID: 1, Mode: "storyboard", ModelKey: "claude", PromptVersion: 9, ToolsHash: "abc", BuildSHA: "7baa1a3", HarnessKey: "hk9",
		Skills: rawJSON(`{"script-breakdown":"v3"}`), Status: model.RunSucceeded, Steps: 3, SpentCredits: 42, DurationMs: 192000, Feedback: -1, Undone: true,
		ApprovalsRejected: 1, SteerCount: 1, ToolErrors: 1, ToolCalls: 4, CreatedAt: obsT0, EndedAt: &end}
	t := func(sec int) time.Time { return obsT0.Add(time.Duration(sec) * time.Second) }
	e := func(seq int64, typ, payload string) model.AgentEvent {
		return model.AgentEvent{Seq: seq, Type: typ, PayloadJSON: rawJSON(payload), CreatedAt: t(int(seq))}
	}
	ended := t(5)
	return &model.AgentExportFacts{
		Digest: digest, Run: run,
		Events: []model.AgentEvent{
			e(1, "message.user", `{"text":"把这段剧本拆成 6 个镜头的分镜","mode":"storyboard"}`),
			e(2, "run.status", `{"status":"running"}`),
			e(3, "message.done", `{"text":"我来先读技能。","thinking":"剧本里有 12 个节拍，按节拍拆最细致","tool_calls":[{"id":"tc1","name":"skill_read"}],"partial":false,"credits":3}`),
			e(4, "tool.start", `{"id":"tc1","name":"skill_read","label":"读技能"}`),
			e(5, "tool.end", `{"id":"tc1","name":"skill_read","is_error":false,"summary":"读取技能 script-breakdown@v3"}`),
			e(6, "tool.start", `{"id":"cm1","name":"codemode"}`),
			e(7, "tool.start", `{"id":"in1","name":"canvas_get_state","parent_id":"cm1"}`),
			e(8, "tool.end", `{"id":"in1","name":"canvas_get_state","parent_id":"cm1","is_error":false,"summary":"1 个节点"}`),
			e(9, "tool.end", `{"id":"cm1","name":"codemode","is_error":false,"summary":"脚本完成"}`),
			e(10, "tool.end", `{"id":"rej1","name":"canvas_delete","is_error":true,"summary":"当前是「提示词优化」模式，不能使用 canvas_delete。"}`),
			e(11, "message.user", `{"text":"镜头别太多","steer":true}`),
			e(12, "approval.created", `{"id":"a1","kind":"generate","status":"pending","quote_credits":96}`),
			e(13, "approval.decided", `{"id":"a1","kind":"generate","status":"rejected"}`),
			e(14, "plan.updated", `{"steps":[{"title":"读技能","status":"done"},{"title":"建镜头","status":"doing"}]}`),
			e(15, "run.undone", `{"reverted":3}`),
			e(16, "feedback.set", `{"rating":-1,"reasons":["misread","slow"],"comment":"我要 6 个，它给了 12 个"}`),
			e(17, "runtime.notice", `{"text":"丢弃了一条插话"}`),
			e(18, "run.status", `{"status":"succeeded"}`),
		},
		ToolCalls: []model.AgentToolCall{
			{ToolCallID: "tc1", Name: "skill_read", ArgsJSON: rawJSON(`{"name":"script-breakdown"}`), ResultText: sp("<技能 script-breakdown@v3>…"), Summary: "读取技能 script-breakdown@v3", StartedAt: t(4), EndedAt: &ended, DurationMs: 120},
			{ToolCallID: "cm1", Name: "codemode", ArgsJSON: rawJSON(`{"code":"await canvas.get()"}`), ResultText: sp("ok"), StartedAt: t(6), EndedAt: &ended, DurationMs: 400},
			{ToolCallID: "in1", ParentID: "cm1", Name: "canvas_get_state", ArgsJSON: rawJSON(`{}`), ResultText: sp(`{"total":1}`), StartedAt: t(7), EndedAt: &ended, DurationMs: 40},
			{ToolCallID: "rej1", Name: "canvas_delete", ArgsJSON: rawJSON(`{"nodeIds":["n1"]}`), ResultText: sp("当前是「提示词优化」模式"), IsError: true, Summary: "当前是「提示词优化」模式，不能使用 canvas_delete。", StartedAt: t(10), EndedAt: &ended, DurationMs: 0},
		},
		Mutations: []model.AgentMutation{
			{Seq: 1, Kind: model.MutationApplyOps, ToolCallID: "cm1", ChangesJSON: rawJSON(`[{"a":1},{"a":2},{"a":3}]`), UndoneAt: &ended},
			{Seq: 2, Kind: model.MutationUndo, ChangesJSON: rawJSON(`[]`)},
		},
		Feedback: &model.AgentFeedback{Rating: -1, Reasons: rawJSON(`["misread","slow"]`), Comment: "我要 6 个，它给了 12 个"},
		Context: []model.AgentEvent{
			e(-4, "message.user", `{"text":"帮我写个雨夜的剧本"}`),
			e(-3, "message.done", `{"text":"剧本写好了，放在文本节点里。","tool_calls":[]}`),
		},
		RuntimeTail: "",
	}
}

func TestRenderRunMarkdown(t *testing.T) {
	md := RenderRunMarkdown(exportFacts())
	has := func(sub string) {
		t.Helper()
		if !strings.Contains(md, sub) {
			t.Errorf("缺少 %q\n---\n%s", sub, md)
		}
	}
	lacks := func(sub string) {
		t.Helper()
		if strings.Contains(md, sub) {
			t.Errorf("不该出现 %q", sub)
		}
	}

	t.Run("头部：运行、Harness、状态和信号", func(t *testing.T) {
		if !strings.HasPrefix(md, "---\n") {
			t.Fatalf("应以 YAML 头部开始：%q", md[:20])
		}
		has("run_id: " + idcodec.Encode(5))
		has("mode: storyboard")
		has("model: claude")
		has("prompt_version: 9")
		has("harness_key: hk9")
		has("build_sha: 7baa1a3")
		has("script-breakdown: v3")
		has("status: succeeded")
		has("duration: 3m12s")
		has("feedback: -1")
		has("undone: true")
		has("approvals_rejected: 1")
		has("steer_count: 1")
		has("tool_errors: 1")
	})

	t.Run("用户请求取第一条非插话消息", func(t *testing.T) {
		has("## 用户请求\n把这段剧本拆成 6 个镜头的分镜")
	})

	t.Run("上下文：更早的轮次", func(t *testing.T) {
		has("## 上下文")
		has("帮我写个雨夜的剧本")
		has("剧本写好了，放在文本节点里。")
	})

	t.Run("时间线：编号递增，并按事件顺序", func(t *testing.T) {
		has("## 时间线")
		idx := func(s string) int { return strings.Index(md, s) }
		order := []string{"#1 [用户]", "[模型回复] 我来先读技能。", "[工具调用] skill_read", "[工具调用] codemode", "↳ [工具调用] canvas_get_state", "[工具调用] canvas_delete", "[用户插话] 镜头别太多", "[审批请求]", "[审批结果]", "[计划]", "[用户撤销本轮]", "[用户反馈]", "[运行时提示] 丢弃了一条插话", "[状态] succeeded"}
		prev := -1
		for _, o := range order {
			i := idx(o)
			if i < 0 {
				t.Errorf("缺少 %q", o)
				continue
			}
			if i < prev {
				t.Errorf("%q 的位置不对", o)
			}
			prev = i
		}
		has("#2 ")
		has("#3 ")
	})

	t.Run("模型回复带思考摘要", func(t *testing.T) {
		has("思考：剧本里有 12 个节拍，按节拍拆最细致")
	})

	t.Run("工具调用：参数、结果、耗时", func(t *testing.T) {
		has(`参数：{"name":"script-breakdown"}`)
		has("结果：<技能 script-breakdown@v3>…")
		has("120ms")
		has("摘要：读取技能 script-breakdown@v3")
	})

	t.Run("被拒绝的调用（只有 tool.end）：标出报错和原因", func(t *testing.T) {
		has("[工具调用] canvas_delete")
		has("✘ 报错")
		has("当前是「提示词优化」模式")
	})

	t.Run("审批和计划", func(t *testing.T) {
		has("generate")
		has("96")
		has("rejected")
		has("读技能")
		has("建镜头")
	})

	t.Run("画布改动：种类、条数、是否撤销", func(t *testing.T) {
		has("## 画布改动")
		has("apply_ops")
		has("3 处")
		has("已撤销")
	})

	t.Run("结束：反馈带中文原因和原话", func(t *testing.T) {
		has("## 结束")
		has("👎")
		has("没理解我的需求")
		has("步骤太多或太慢")
		has("我要 6 个，它给了 12 个")
	})

	t.Run("不输出渠道、用户邮箱这类信息", func(t *testing.T) {
		lacks("sk-")
		lacks("@example.com")
	})
}

func TestRenderRunMarkdown_Truncation(t *testing.T) {
	f := exportFacts()
	big := strings.Repeat("长", 2000) // 6000 字节
	f.ToolCalls[0].ResultText = &big
	f.ToolCalls[0].ResultTruncated = false
	md := RenderRunMarkdown(f)
	if strings.Contains(md, strings.Repeat("长", 1500)) {
		t.Error("Markdown 里单段结果最多 2KB")
	}
	if !strings.Contains(md, "已截断") {
		t.Error("应标明被截断，完整内容在 JSON 里")
	}

	f = exportFacts()
	f.Events[2].PayloadJSON = rawJSON(`{"text":"好","thinking":"` + strings.Repeat("思", 1000) + `","tool_calls":[]}`)
	md = RenderRunMarkdown(f)
	if strings.Contains(md, strings.Repeat("思", 601)) || !strings.Contains(md, strings.Repeat("思", 600)) {
		t.Error("思考摘要只取前 600 字")
	}

	f = exportFacts()
	f.ToolCalls[0].ResultTruncated = true
	if !strings.Contains(RenderRunMarkdown(f), "已截断") {
		t.Error("落库时已被截断的结果也要标明")
	}
}

func TestRenderRunMarkdown_PurgedAndMissing(t *testing.T) {
	f := exportFacts()
	purged := obsT0.Add(31 * 24 * time.Hour)
	f.ToolCalls[0].ArgsJSON, f.ToolCalls[0].ResultText, f.ToolCalls[0].PurgedAt = nil, nil, &purged
	md := RenderRunMarkdown(f)
	if !strings.Contains(md, "已过保留期") {
		t.Errorf("过期清理后的工具调用要说明原因：\n%s", md)
	}

	f = exportFacts()
	f.ToolCalls = nil // 观测写库失败过，一条工具调用记录都没有
	md = RenderRunMarkdown(f)
	if !strings.Contains(md, "[工具调用] skill_read") || !strings.Contains(md, "未记录参数") {
		t.Errorf("没有调用记录时退回事件里的信息，并说明未记录参数：\n%s", md)
	}
}

func TestRenderRunMarkdown_Minimal(t *testing.T) {
	// 运行刚创建、摘要和事件都还没有：不能 panic，也不能输出一份空文档
	f := &model.AgentExportFacts{Run: model.AgentRun{ID: 9, UserID: 1, Status: model.RunQueued, CreatedAt: obsT0}}
	md := RenderRunMarkdown(f)
	if !strings.Contains(md, "run_id: "+idcodec.Encode(9)) || !strings.Contains(md, "status: queued") {
		t.Errorf("md=%s", md)
	}
	if strings.Contains(md, "## 上下文") || strings.Contains(md, "## 画布改动") {
		t.Error("没有内容的章节不输出")
	}
}

func TestExportRunJSON(t *testing.T) {
	e := newObserveEnv(t)
	e.repo.exports[5] = exportFacts()
	out, err := e.svc.ExportRun(t.Context(), ObserveActor{UserID: 9, TokenID: 3}, 5, ExportBoth)
	if err != nil {
		t.Fatal(err)
	}
	if out.Format != "both" || !strings.HasPrefix(out.Content, "---\n") || out.Data == nil {
		t.Fatalf("out=%+v", out)
	}
	raw, _ := json.Marshal(out.Data)
	s := string(raw)
	for _, want := range []string{`"digest"`, `"events"`, `"tool_calls"`, `"model_calls"`, `"mutations"`, `"approvals"`, `"feedback"`, `"context"`, `"run_id":"` + idcodec.Encode(5) + `"`} {
		if !strings.Contains(s, want) {
			t.Errorf("JSON 缺少 %s", want)
		}
	}
	if !strings.Contains(s, `"code":"await canvas.get()"`) {
		t.Error("JSON 里工具参数完整保留")
	}
	// 只要 md / 只要 json
	md, _ := e.svc.ExportRun(t.Context(), ObserveActor{UserID: 9}, 5, ExportMD)
	if md.Format != "md" || md.Content == "" || md.Data != nil {
		t.Errorf("md=%+v", md)
	}
	js, _ := e.svc.ExportRun(t.Context(), ObserveActor{UserID: 9}, 5, ExportJSON)
	if js.Format != "json" || js.Content != "" || js.Data == nil {
		t.Errorf("js=%+v", js)
	}
	if got := e.audit.actions(); len(got) != 1 || got[0] != model.AdminAuditAgentRunsRead {
		t.Errorf("只有令牌访问写审计，且每次导出一条：%v", got)
	}
}

func TestBuildTimeline(t *testing.T) {
	steps := BuildTimeline(exportFacts())
	if len(steps) == 0 {
		t.Fatal("时间线不应为空")
	}
	for i, st := range steps {
		if st.N != i+1 {
			t.Fatalf("步骤编号应连续从 1 起：第 %d 步编号是 %d", i+1, st.N)
		}
	}
	byKind := map[string][]TimelineStep{}
	for _, st := range steps {
		byKind[st.Kind] = append(byKind[st.Kind], st)
	}
	if len(byKind[TimelineUser]) != 1 || byKind[TimelineUser][0].Text != "把这段剧本拆成 6 个镜头的分镜" || byKind[TimelineUser][0].Label != "用户" {
		t.Errorf("用户步骤：%+v", byKind[TimelineUser])
	}
	if s := byKind[TimelineSteer]; len(s) != 1 || s[0].Text != "镜头别太多" {
		t.Errorf("插话：%+v", s)
	}
	if a := byKind[TimelineAssist]; len(a) != 1 || a[0].Thinking != "剧本里有 12 个节拍，按节拍拆最细致" || a[0].Partial {
		t.Errorf("模型回复和思考：%+v", a)
	}
	if len(byKind[TimelineApproval]) != 2 || len(byKind[TimelinePlan]) != 1 || len(byKind[TimelineUndo]) != 1 || len(byKind[TimelineFeedback]) != 1 || len(byKind[TimelineNotice]) != 1 {
		t.Errorf("各类步骤数量：%v", map[string]int{"approval": len(byKind[TimelineApproval]), "plan": len(byKind[TimelinePlan])})
	}

	tools := byKind[TimelineKindTool]
	if len(tools) != 4 {
		t.Fatalf("4 次工具调用：%d", len(tools))
	}
	first := tools[0].Tool
	if first.Name != "skill_read" || first.DurationMs != 120 || first.IsError || !first.Recorded || first.Args != `{"name":"script-breakdown"}` || first.Result != "<技能 script-breakdown@v3>…" || first.Summary != "读取技能 script-breakdown@v3" {
		t.Errorf("成功的工具调用：%+v", first)
	}
	if !tools[2].Nested || tools[2].Tool.Name != "canvas_get_state" || tools[0].Nested || tools[1].Nested {
		t.Errorf("codemode 子调用要标 nested：%+v %+v", tools[1], tools[2])
	}
	rej := tools[3].Tool
	if !rej.IsError || rej.Name != "canvas_delete" || rej.Result != "当前是「提示词优化」模式" {
		t.Errorf("被拒绝的调用（只有 tool.end）也要有：%+v", rej)
	}
	for _, st := range steps {
		if st.At == nil {
			t.Errorf("步骤应带事件时间：%+v", st)
			break
		}
	}
}

func TestBuildTimeline_ToolEdgeCases(t *testing.T) {
	// 没有调用记录：Recorded 为 false；过保留期：PurgedAt 有值且参数结果为空；被截断：Truncated
	f := exportFacts()
	f.ToolCalls = nil
	for _, st := range BuildTimeline(f) {
		if st.Tool != nil && st.Tool.Recorded {
			t.Errorf("没有记录时 Recorded 应为 false：%+v", st.Tool)
		}
	}
	f = exportFacts()
	purged := obsT0.Add(31 * 24 * time.Hour)
	f.ToolCalls[0].ArgsJSON, f.ToolCalls[0].ResultText, f.ToolCalls[0].PurgedAt = nil, nil, &purged
	for _, st := range BuildTimeline(f) {
		if st.Tool != nil && st.Tool.Name == "skill_read" && (st.Tool.PurgedAt == nil || st.Tool.Args != "" || st.Tool.Result != "") {
			t.Errorf("过保留期：%+v", st.Tool)
		}
	}
	f = exportFacts()
	big := strings.Repeat("长", 2000)
	f.ToolCalls[0].ResultText = &big
	for _, st := range BuildTimeline(f) {
		if st.Tool != nil && st.Tool.Name == "skill_read" && (!st.Tool.Truncated || len(st.Tool.Result) > 2<<10) {
			t.Errorf("超长结果应截断并标记：len=%d truncated=%v", len(st.Tool.Result), st.Tool.Truncated)
		}
	}
	// 半截回答
	f = exportFacts()
	f.Events[2].PayloadJSON = rawJSON(`{"text":"说到一半","partial":true,"tool_calls":[]}`)
	for _, st := range BuildTimeline(f) {
		if st.Kind == TimelineAssist && !st.Partial {
			t.Error("partial 应标出")
		}
	}
	// 没有事件：空列表（不是 nil，JSON 里是 []）
	if steps := BuildTimeline(&model.AgentExportFacts{}); steps == nil || len(steps) != 0 {
		t.Errorf("steps=%v", steps)
	}
}

// 抽屉和 Markdown 同源：JSON 里的每一步都能在 Markdown 里找到同样的编号和名称。
func TestExportRun_TimelineMatchesMarkdown(t *testing.T) {
	e := newObserveEnv(t)
	e.repo.exports[5] = exportFacts()
	out, err := e.svc.ExportRun(t.Context(), ObserveActor{UserID: 9}, 5, ExportBoth)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Data.Timeline) == 0 {
		t.Fatal("JSON 导出应带 timeline")
	}
	for _, st := range out.Data.Timeline {
		if !strings.Contains(out.Content, fmt.Sprintf("#%d ", st.N)) || !strings.Contains(out.Content, "["+st.Label+"]") {
			t.Errorf("Markdown 里找不到第 %d 步「%s」", st.N, st.Label)
		}
	}
	raw, _ := json.Marshal(out.Data)
	if !strings.Contains(string(raw), `"timeline":[`) {
		t.Error("timeline 字段名")
	}
}
