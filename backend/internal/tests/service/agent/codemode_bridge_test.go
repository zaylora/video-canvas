package agent_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"video-canvas/internal/model"
	. "video-canvas/internal/service/agent"
)

func nested(t *testing.T, b *bridgeEnv, tok, parent, id, name string, args any) *ToolResult {
	t.Helper()
	raw, _ := json.Marshal(args)
	res, err := b.br.ExecuteNestedTool(context.Background(), tok, parent, id, name, raw)
	if err != nil {
		t.Fatalf("ExecuteNestedTool(%s): %v", name, err)
	}
	return res
}

func TestExecuteNestedTool_RunsWithoutCountingSteps(t *testing.T) {
	b := newBridgeEnv(t)
	tok, run := b.token(t, "all")
	res := nested(t, b, tok, "cm1", "cm1.1", "canvas_get_state", map[string]any{})
	if res.IsError || res.Terminate || res.Content == "" {
		t.Fatalf("res=%+v", res)
	}
	if got := b.run(run.ID).Steps; got != 0 {
		t.Errorf("脚本里的嵌套调用不单独计步数（整个脚本由报备计 1 步），实际 %d", got)
	}
	for _, typ := range []string{"tool.start", "tool.end"} {
		ev := b.repo.lastEvent(typ)
		if ev["parent_id"] != "cm1" || ev["id"] != "cm1.1" || ev["label"] != "读取画布" {
			t.Errorf("%s 要带父调用 id，前端才能把它归到脚本下: %v", typ, ev)
		}
	}
}

func TestExecuteNestedTool_IgnoresStepLimit(t *testing.T) {
	// 步数上限管的是模型一轮轮调用的次数；脚本整体已经在开始时算过 1 步，里面的调用不该再被它拦住
	b := newBridgeEnv(t)
	tok, run := b.token(t, "all")
	b.repo.mu.Lock()
	b.repo.runs[run.ID].Steps = run.MaxSteps
	b.repo.mu.Unlock()
	res := nested(t, b, tok, "cm1", "cm1.1", "canvas_get_state", map[string]any{})
	if res.IsError || res.Terminate {
		t.Errorf("嵌套调用不受步数上限影响: %+v", res)
	}
	if b.run(run.ID).Status != model.RunRunning {
		t.Errorf("不该因此暂停运行: %s", b.run(run.ID).Status)
	}
}

func TestExecuteNestedTool_RejectsToolsThatCannotRunInScripts(t *testing.T) {
	// 会创建审批并终止本轮的工具、返回图片的工具，在脚本里没法在「等审批」处停下，不能被调用
	for _, name := range []string{"canvas_delete", "ask_user", "generate_media", "canvas_inspect_image"} {
		b := newBridgeEnv(t)
		tok, run := b.token(t, "all")
		res := nested(t, b, tok, "cm1", "cm1.1", name, map[string]any{"question": "q", "kind": "choice", "reason": "r", "nodeIds": []string{"a"}, "items": []any{}})
		if !res.IsError || res.Terminate || !strings.Contains(res.Content, "不能在脚本里") {
			t.Errorf("%s 应被拒绝并说明原因: %+v", name, res)
		}
		if got := b.run(run.ID).Status; got != model.RunRunning {
			t.Errorf("%s 被拒绝后运行状态不该变: %s", name, got)
		}
		for _, ty := range b.persistedTypes() {
			if ty == "approval.created" {
				t.Errorf("%s 不能创建审批", name)
			}
		}
	}
}

func TestExecuteNestedTool_KeepsModeRestrictionsAndUnknownTools(t *testing.T) {
	b := newBridgeEnv(t)
	tok, _ := b.token(t, "prompt")
	if res := nested(t, b, tok, "cm1", "cm1.1", "canvas_arrange", map[string]any{"layout": "row"}); !res.IsError || !strings.Contains(res.Content, "模式") {
		t.Errorf("模式不允许的工具在脚本里一样不能用: %+v", res)
	}
	if res := nested(t, b, tok, "cm1", "cm1.2", "no_such_tool", map[string]any{}); !res.IsError || !strings.Contains(res.Content, "未知的工具") {
		t.Errorf("未知工具: %+v", res)
	}
}

func TestExecuteNestedTool_PerSegmentCap(t *testing.T) {
	b := newBridgeEnv(t)
	tok, _ := b.token(t, "all")
	for i := 1; i <= 300; i++ {
		if res := nested(t, b, tok, "cm1", "cm1."+itoaTest(i), "plan_update", map[string]any{"steps": []any{}}); res.IsError {
			t.Fatalf("第 %d 次嵌套调用不该被拦: %+v", i, res)
		}
	}
	res := nested(t, b, tok, "cm1", "cm1.301", "plan_update", map[string]any{"steps": []any{}})
	if !res.IsError || !strings.Contains(res.Content, "上限") {
		t.Errorf("第 301 次应被挡住并说明上限: %+v", res)
	}
}

func itoaTest(n int) string { b, _ := json.Marshal(n); return string(b) }

func TestExecuteTool_NormalCallsStillCountSteps(t *testing.T) {
	b := newBridgeEnv(t)
	tok, run := b.token(t, "all")
	b.tool(t, tok, "canvas_get_state", map[string]any{})
	if got := b.run(run.ID).Steps; got != 1 {
		t.Errorf("普通工具调用照旧计 1 步，实际 %d", got)
	}
}

// ---- 报备：Agent 自己的工具（不经 /tool）开始和结束 ----

func report(t *testing.T, b *bridgeEnv, tok string, ev AgentToolEvent) *AgentToolEventResult {
	t.Helper()
	res, err := b.br.ReportAgentTool(context.Background(), tok, ev)
	if err != nil {
		t.Fatalf("ReportAgentTool(%+v): %v", ev, err)
	}
	return res
}

func TestReportAgentTool_StartCountsOneStepAndRecordsEvent(t *testing.T) {
	b := newBridgeEnv(t)
	tok, run := b.token(t, "all")
	res := report(t, b, tok, AgentToolEvent{Phase: "start", ToolCallID: "cm1", Name: "codemode", Label: "运行脚本", Verb: "脚本"})
	if !res.Allowed || res.Terminate {
		t.Fatalf("res=%+v", res)
	}
	if got := b.run(run.ID).Steps; got != 1 {
		t.Errorf("整个脚本算 1 步，实际 %d", got)
	}
	ev := b.repo.lastEvent("tool.start")
	if ev["id"] != "cm1" || ev["name"] != "codemode" || ev["label"] != "运行脚本" || ev["verb"] != "脚本" {
		t.Errorf("tool.start 用 Agent 报备的名称，Go 不查注册表: %v", ev)
	}
}

func TestReportAgentTool_EndRecordsSummaryAndError(t *testing.T) {
	b := newBridgeEnv(t)
	tok, _ := b.token(t, "all")
	report(t, b, tok, AgentToolEvent{Phase: "start", ToolCallID: "cm1", Name: "codemode", Label: "运行脚本", Verb: "脚本"})
	report(t, b, tok, AgentToolEvent{Phase: "end", ToolCallID: "cm1", Name: "codemode", Label: "运行脚本", Verb: "脚本", IsError: true, Summary: "脚本失败：出错了"})
	ev := b.repo.lastEvent("tool.end")
	if ev["id"] != "cm1" || ev["is_error"] != true || ev["summary"] != "脚本失败：出错了" || ev["label"] != "运行脚本" {
		t.Errorf("tool.end: %v", ev)
	}
}

func TestReportAgentTool_StepLimitPausesRun(t *testing.T) {
	b := newBridgeEnv(t)
	tok, run := b.token(t, "all")
	b.repo.mu.Lock()
	b.repo.runs[run.ID].Steps = run.MaxSteps
	b.repo.mu.Unlock()
	res := report(t, b, tok, AgentToolEvent{Phase: "start", ToolCallID: "cm1", Name: "codemode", Label: "运行脚本", Verb: "脚本"})
	if res.Allowed || !res.Terminate || !strings.Contains(res.Content, "步数上限") {
		t.Errorf("步数用尽：不允许、让 Agent 停下: %+v", res)
	}
	if got := b.run(run.ID).Status; got != model.RunStepLimit {
		t.Errorf("运行应暂停为步数用尽: %s", got)
	}
	for _, ty := range b.persistedTypes() {
		if ty == "tool.start" {
			t.Error("没有开始就不记 tool.start")
		}
	}
}

func TestReportAgentTool_Validation(t *testing.T) {
	b := newBridgeEnv(t)
	tok, _ := b.token(t, "all")
	bad := []AgentToolEvent{
		{Phase: "start", ToolCallID: "x", Name: "canvas_delete", Label: "冒充"}, // 和 Go 的工具重名：不能借报备冒充 Go 的工具
		{Phase: "start", ToolCallID: "x", Name: ""},
		{Phase: "start", ToolCallID: "", Name: "codemode"},
		{Phase: "pause", ToolCallID: "x", Name: "codemode"},
	}
	for _, ev := range bad {
		if _, err := b.br.ReportAgentTool(context.Background(), tok, ev); err == nil {
			t.Errorf("%+v 应被拒绝", ev)
		}
	}
	if _, err := b.br.ReportAgentTool(context.Background(), "bad-token", AgentToolEvent{Phase: "start", ToolCallID: "x", Name: "codemode"}); err == nil {
		t.Error("令牌无效应被拒绝")
	}
	report(t, b, tok, AgentToolEvent{Phase: "start", ToolCallID: "x", Name: "codemode", Label: strings.Repeat("长", 100), Verb: strings.Repeat("词", 100)})
	ev := b.repo.lastEvent("tool.start")
	if l, _ := ev["label"].(string); len([]rune(l)) > 20 {
		t.Errorf("label 要截短: %d 字", len([]rune(l)))
	}
	if v, _ := ev["verb"].(string); len([]rune(v)) > 10 {
		t.Errorf("verb 要截短: %d 字", len([]rune(v)))
	}
	report(t, b, tok, AgentToolEvent{Phase: "end", ToolCallID: "x", Name: "codemode", Summary: strings.Repeat("摘", 1000)})
	if s, _ := b.repo.lastEvent("tool.end")["summary"].(string); len([]rune(s)) > 200 {
		t.Errorf("summary 要截短: %d 字", len([]rune(s)))
	}
}
