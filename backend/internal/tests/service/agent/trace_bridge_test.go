package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"video-canvas/internal/llmgateway"
	"video-canvas/internal/model"
	. "video-canvas/internal/service/agent"
)

// tracedBridge 在桥的测试环境上挂一个记录器：运行开了轨迹，片段已开始记录并挂到令牌上。
type tracedBridge struct {
	*bridgeEnv
	te  *traceEnv
	seg *SegmentTrace
	tok string
	run *model.AgentRun
}

func newTracedBridge(t *testing.T) *tracedBridge {
	t.Helper()
	b := newBridgeEnv(t)
	te := newTraceEnv(t, nil)
	tok, run := b.token(t, "all")
	traced := *run
	traced.TraceEnabled = true
	traced.CreatedAt = te.run().CreatedAt
	seg := te.rec.BeginSegment(context.Background(), &traced, "start", startInput())
	if seg == nil {
		t.Fatal("应开始记录")
	}
	b.br.AttachTrace(tok, seg)
	t.Cleanup(func() { seg.End(context.Background(), SegmentEnd{}) }) // 没主动结束的测试也要关掉文件，Windows 上不关删不了临时目录
	return &tracedBridge{bridgeEnv: b, te: te, seg: seg, tok: tok, run: &traced}
}

// lines 结束片段并读回上传的原文。
func (tb *tracedBridge) lines(t *testing.T) []map[string]any {
	t.Helper()
	tb.seg.End(context.Background(), SegmentEnd{})
	return tb.te.stores.st.lines(t, "agent-traces/2026/10/20/"+itoa64(int64(tb.run.ID))+"/1.jsonl.gz")
}

func kindLines(lines []map[string]any, kind string) []map[string]any {
	var out []map[string]any
	for _, l := range lines {
		if l["kind"] == kind {
			out = append(out, l)
		}
	}
	return out
}

// 模型代理：请求体（不含渠道 Key）、响应、首字延迟、上游模型和渠道都进轨迹；计费记录带上 span 标识，结算时写回耗时等。
func TestTrace_ProxyModelRecordsChat(t *testing.T) {
	tb := newTracedBridge(t)
	body := []byte(`{"model":"x","messages":[{"role":"user","content":"拆分镜"}]}`)
	if err := tb.br.ProxyModel(context.Background(), tb.tok, body, func([]byte) {}); err != nil {
		t.Fatal(err)
	}
	if len(tb.bill.calls) != 1 {
		t.Fatalf("calls=%d", len(tb.bill.calls))
	}
	call := tb.bill.calls[0]
	if call.SpanID == "" || call.Seq == 0 || call.Purpose != "chat" || call.SegmentSeq != 1 {
		t.Errorf("计费记录应带轨迹字段: %+v", call)
	}
	in := tb.bill.settled[0]
	if in.DurationMS < 1 || in.FinishReason != "stop" || in.UpstreamModel != "claude-up" || in.ChannelID != "ch1" {
		t.Errorf("结算时应写回耗时、结束原因、上游模型、渠道: %+v", in)
	}
	chats := kindLines(tb.lines(t), "chat")
	if len(chats) != 1 {
		t.Fatalf("应有一条 chat 原文: %v", chats)
	}
	c := chats[0]
	if c["span_id"] != call.SpanID || c["purpose"] != "chat" || c["name"] != "claude" {
		t.Errorf("原文行与计费记录要对得上: %v", c)
	}
	raw, _ := json.Marshal(c)
	if strings.Contains(string(raw), "sk-test") || strings.Contains(string(raw), "Authorization") {
		t.Errorf("渠道 Key 和请求头一律不进轨迹: %s", raw)
	}
	if c["response"].(map[string]any)["text"] != "好的" || c["credits"].(float64) != 5 {
		t.Errorf("响应和扣费不对: %v", c)
	}
}

// 摘要调用的 purpose 是 summary（请求头 X-Agent-Purpose 经 WithModelPurpose 传进来）。
func TestTrace_ProxyModelSummaryPurpose(t *testing.T) {
	tb := newTracedBridge(t)
	err := tb.br.ProxyModel(WithModelPurpose(context.Background(), ModelPurposeSummary), tb.tok, []byte(`{"messages":[]}`), func([]byte) {})
	if err != nil {
		t.Fatal(err)
	}
	if tb.bill.calls[0].Purpose != "summary" {
		t.Errorf("purpose=%q", tb.bill.calls[0].Purpose)
	}
}

// 上游出错：错误照样原样返回给调用方，轨迹里记下错误；没有任何增量时首字延迟为 0。
func TestTrace_ProxyModelUpstreamErrorStillTraced(t *testing.T) {
	tb := newTracedBridge(t)
	boom := errors.New("上游 500")
	tb.str.script = func(context.Context, func(llmgateway.Event), func([]byte)) (*llmgateway.Result, error) {
		return nil, boom
	}
	err := tb.br.ProxyModel(context.Background(), tb.tok, []byte(`{"messages":[]}`), func([]byte) {})
	if !errors.Is(err, boom) {
		t.Fatalf("记录器不能吞掉或改写上游错误: %v", err)
	}
	if tb.bill.settled[0].FirstTokenMS != 0 {
		t.Errorf("没有增量就没有首字延迟: %+v", tb.bill.settled[0])
	}
	chats := kindLines(tb.lines(t), "chat")
	if len(chats) != 1 || chats[0]["error"] == nil {
		t.Errorf("轨迹应记下这次失败的调用: %v", chats)
	}
}

// 没开轨迹时桥照常工作（nil 句柄）。
func TestTrace_BridgeWithoutTraceUnchanged(t *testing.T) {
	b := newBridgeEnv(t)
	tok, _ := b.token(t, "all")
	if err := b.br.ProxyModel(context.Background(), tok, []byte(`{"messages":[]}`), func([]byte) {}); err != nil {
		t.Fatal(err)
	}
	if b.bill.calls[0].SpanID != "" {
		t.Errorf("没开轨迹计费记录不带 span: %+v", b.bill.calls[0])
	}
	b.br.AttachTrace(tok, nil) // 挂 nil 也不出错
}

// 工具调用：参数原文、返回原文、耗时、状态，改画布的调用关联 mutation_id；失败的调用记错误分类。
func TestTrace_ExecuteToolRecordsSpan(t *testing.T) {
	tb := newTracedBridge(t)
	ctx := context.Background()
	res, err := tb.br.ExecuteTool(ctx, tb.tok, "tc-apply", "canvas_apply_ops", json.RawMessage(`{"ops":[{"op":"create_node","tempId":"a","kind":"image","label":"角色"}]}`))
	if err != nil || res.IsError {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	bad, _ := tb.br.ExecuteTool(ctx, tb.tok, "tc-bad", "canvas_apply_ops", json.RawMessage(`{"ops":[{"op":"update_node","id":"ghost","label":"x"}]}`))
	if !bad.IsError {
		t.Fatal("应是工具错误")
	}
	unknown, _ := tb.br.ExecuteTool(ctx, tb.tok, "tc-unknown", "no_such_tool", json.RawMessage(`{}`))
	if !unknown.IsError {
		t.Fatal("未知工具应是工具错误")
	}

	spans := tb.te.repo.spansOf(model.TraceSpanExecuteTool)
	by := map[string]model.AgentTraceSpan{}
	for _, s := range spans {
		by[s.ToolCallID] = s
	}
	if len(by) != 3 {
		t.Fatalf("三次调用都应有索引: %+v", spans)
	}
	if s := by["tc-apply"]; s.Status != "ok" || s.Name != "canvas_apply_ops" || s.MutationID == nil || *s.MutationID == 0 {
		t.Errorf("成功的改画布调用应关联改动: %+v", s)
	}
	if s := by["tc-bad"]; s.Status != "error" || s.ErrorCode != "validation" || s.MutationID != nil {
		t.Errorf("校验失败应标 error / validation: %+v", s)
	}
	if s := by["tc-unknown"]; s.Status != "denied" || s.ErrorCode != "rejected" {
		t.Errorf("未知工具是被拒绝: %+v", s)
	}
	tools := kindLines(tb.lines(t), "execute_tool")
	if len(tools) != 3 {
		t.Fatalf("原文应有 3 条: %v", tools)
	}
	first := tools[0]
	if first["args"].(map[string]any)["ops"] == nil || !strings.Contains(first["result"].(string), "id_map") || first["is_error"] != false || first["mutation_id"] == nil {
		t.Errorf("参数、返回原文、改动 id 不对: %v", first)
	}
	if tools[1]["is_error"] != true {
		t.Errorf("失败的调用 is_error=true: %v", tools[1])
	}
}

// 步数用尽也要留痕。
func TestTrace_StepLimitIsDenied(t *testing.T) {
	tb := newTracedBridge(t)
	tb.repo.mu.Lock()
	tb.repo.runs[tb.run.ID].Steps = tb.repo.runs[tb.run.ID].MaxSteps
	tb.repo.mu.Unlock()
	res, err := tb.br.ExecuteTool(context.Background(), tb.tok, "tc1", "plan_update", json.RawMessage(`{"steps":[]}`))
	if err != nil || !res.Terminate {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	spans := tb.te.repo.spansOf(model.TraceSpanExecuteTool)
	if len(spans) != 1 || spans[0].Status != "denied" || spans[0].ErrorCode != "step_limit" {
		t.Errorf("步数用尽应记为 denied/step_limit: %+v", spans)
	}
}

// Agent 自己的工具（codemode）经报备记录，里面的嵌套调用指向它。
func TestTrace_CodemodeNestedCalls(t *testing.T) {
	tb := newTracedBridge(t)
	ctx := context.Background()
	if _, err := tb.br.ReportAgentTool(ctx, tb.tok, AgentToolEvent{Phase: "start", ToolCallID: "cm1", Name: "codemode", Label: "脚本", Verb: "脚本"}); err != nil {
		t.Fatal(err)
	}
	if _, err := tb.br.ExecuteNestedTool(ctx, tb.tok, "cm1", "cm1.1", "plan_update", json.RawMessage(`{"steps":[]}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := tb.br.ReportAgentTool(ctx, tb.tok, AgentToolEvent{Phase: "end", ToolCallID: "cm1", Name: "codemode", Summary: "跑了 1 步"}); err != nil {
		t.Fatal(err)
	}
	by := map[string]model.AgentTraceSpan{}
	for _, s := range tb.te.repo.spansOf(model.TraceSpanExecuteTool) {
		by[s.ToolCallID] = s
	}
	if by["cm1"].SpanID == "" || by["cm1.1"].ParentSpanID != by["cm1"].SpanID {
		t.Errorf("嵌套调用应带 parent_span_id: %+v", by)
	}
	if by["cm1"].Name != "codemode" || by["cm1"].Summary != "跑了 1 步" {
		t.Errorf("外层 codemode 的记录不对: %+v", by["cm1"])
	}
}

// 技能固定的版本在片段结束时合并进运行行。
func TestTrace_SkillPinsAreMergedAtSegmentEnd(t *testing.T) {
	tb := newTracedBridge(t)
	tb.br.AttachTrace(tb.tok, tb.seg) // 重复挂载无副作用
	// 内置技能没有版本不记；这里直接验证取 pins 的函数被用上：没有固定过的技能时 pins 为空
	tb.seg.End(context.Background(), SegmentEnd{})
	if len(tb.te.repo.updates) != 1 || len(tb.te.repo.updates[0].SkillPins) != 0 {
		t.Errorf("没有固定过技能版本时不写 skill_pins: %+v", tb.te.repo.updates)
	}
}
