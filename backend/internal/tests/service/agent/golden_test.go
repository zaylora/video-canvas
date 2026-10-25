package agent_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"

	"video-canvas/internal/llmgateway"
	"video-canvas/internal/model"
	. "video-canvas/internal/service/agent"
)

// 黄金文件：用 Go 现在的实现跑一遍完整的场景，把前端看到的东西（REST 响应的视图、14 种里的 12 类事件的内容）录下来，
// 放在 agent/test/golden/。阶段 3 的 Agent 服务（TypeScript）对同样的场景必须给出一致的结构，由那边的测试比对。
//
// 这个测试有两种模式：
//   - 默认：重新录一遍，和文件里的比，不一致就失败——Go 这边改了前端能看到的东西，必须同时更新黄金文件并让 TypeScript 跟上；
//   - UPDATE_AGENT_GOLDEN=1：把录到的写进文件。
//
// 录的时候把会变的东西换成占位符（对外的 id、时间、随机的节点 id），其余原样。

const goldenPath = "../../../../../agent/test/golden/go-contract.json"

var (
	reEncodedID = regexp.MustCompile(`^[0-9a-f]{32}$`)
	reTime      = regexp.MustCompile(`^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d`)
	reNodeID    = regexp.MustCompile(`^n_[0-9a-f]{10}$`)
)

// normalize 把 JSON 里会变的值换成占位符。
func normalize(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = normalize(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = normalize(val)
		}
		return out
	case string:
		switch {
		case reEncodedID.MatchString(x):
			return "<id>"
		case reTime.MatchString(x):
			return "<time>"
		case reNodeID.MatchString(x):
			return "<node>"
		}
	}
	return v
}

// roundTrip 把任意值先序列化再解析，得到和 HTTP 响应里一样的 JSON 结构。
func roundTrip(t *testing.T, v any) any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return normalize(out)
}

func recordGolden(t *testing.T) map[string]any {
	t.Helper()
	b := newBridgeEnv(t)
	ctx := context.Background()
	views := map[string]any{}

	// ---- 会话 ----
	other, err := b.svc.CreateSession(ctx, 1, 7, &model.CreateAgentSessionReq{Title: "角色设计", Mode: "storyboard", ModelKey: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	views["session"] = roundTrip(t, other)
	models, _ := b.svc.Models(ctx)
	views["models"] = roundTrip(t, models)

	// ---- 一轮完整的运行：发起 → 模型回答（思考、文字、工具调用）→ 工具 → 计划 → 审批 → 批准 → 完成 ----
	budget := 50
	rv, err := b.svc.StartRun(ctx, 1, b.sess, &model.StartAgentRunReq{Message: "拆分镜", Selection: []string{"n_script"}, BudgetCredits: &budget})
	if err != nil {
		t.Fatal(err)
	}
	views["run_started"] = roundTrip(t, rv)
	run := b.run(uint64(rv.ID))
	if err := b.br.MarkRunning(ctx, &run); err != nil {
		t.Fatal(err)
	}
	tok := b.br.IssueToken(&run, model.AgentRunInput{ModelKey: "claude", Mode: "all"})

	// 模型回答：思考 + 文字 + 一个工具调用；有用量，所以有 run.usage 和带积分的 message.done
	b.str.script = func(_ context.Context, emit func(llmgateway.Event), raw func([]byte)) (*llmgateway.Result, error) {
		emit(llmgateway.Event{Type: llmgateway.EventThinking, Text: "先看画布"})
		emit(llmgateway.Event{Type: llmgateway.EventText, Text: "我先看看画布。"})
		raw([]byte("data: [DONE]\n\n"))
		return &llmgateway.Result{Text: "我先看看画布。", Thinking: "先看画布", FinishReason: "tool_calls",
			ToolCalls: []llmgateway.ToolCall{{ID: "call_1", Name: "canvas_get_state", Arguments: "{}"}},
			Usage:     llmgateway.Usage{InputTokens: 1_000_000, OutputTokens: 100_000}}, nil
	}
	body := []byte(`{"model":"agent","messages":[{"role":"user","content":"拆分镜"}],"stream":true}`)
	if err := b.br.ProxyModel(ctx, tok, body, func([]byte) {}); err != nil {
		t.Fatal(err)
	}
	b.tool(t, tok, "canvas_get_state", map[string]any{})
	b.tool(t, tok, "plan_update", map[string]any{"steps": []map[string]any{{"title": "拆分镜", "status": "doing"}, {"title": "出图", "status": "todo"}}})
	b.tool(t, tok, "canvas_apply_ops", map[string]any{"ops": []map[string]any{{"op": "create_node", "tempId": "a", "kind": "image", "label": "角色"}, {"op": "connect", "source": "n_script", "target": "a"}}})

	// 审批：创建 → 批准（逐项 + 追加预算）→ 执行
	ap, err := b.svc.CreateApproval(ctx, &run, "call_gen", model.ApprovalGenerate, genPayload(), 24)
	if err != nil {
		t.Fatal(err)
	}
	decided, err := b.svc.Decide(ctx, 1, uint64(ap.ID), &model.DecideAgentApprovalReq{Decision: "approve", Items: []model.ApprovalItemDecision{{Index: 0, Approve: true, Count: 1}, {Index: 1, Approve: true}}, AddBudget: 10})
	if err != nil {
		t.Fatal(err)
	}
	views["approval_decided"] = roundTrip(t, decided)

	// 完成
	if err := b.br.Finish(ctx, tok, "done", ""); err != nil {
		t.Fatal(err)
	}
	// 撤销：真的写了画布，所以有 run.undone
	undo, err := b.svc.Undo(ctx, 1, uint64(rv.ID))
	if err != nil {
		t.Fatal(err)
	}
	views["undo"] = roundTrip(t, undo)

	// ---- 第二轮：插话、取消 ----
	rv2 := b.start(t, "再来一轮")
	run2 := b.run(uint64(rv2.ID))
	_ = b.br.MarkRunning(ctx, &run2)
	if err := b.svc.Interject(ctx, 1, uint64(rv2.ID), "再暖一点"); err != nil {
		t.Fatal(err)
	}
	cancelled, err := b.svc.Cancel(ctx, 1, uint64(rv2.ID))
	if err != nil {
		t.Fatal(err)
	}
	views["run_canceled"] = roundTrip(t, cancelled)

	// ---- 读：回放和历史 ----
	events, err := b.svc.Events(ctx, 1, b.sess, 0, 200)
	if err != nil {
		t.Fatal(err)
	}
	views["events"] = roundTrip(t, events)
	history, err := b.svc.History(ctx, 1, b.sess, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	views["history"] = roundTrip(t, history)

	// ---- 事件：每一类取第一条的内容；临时事件（文字、思考增量）取推送出去的 ----
	byType := map[string]any{}
	for _, ev := range events {
		var data any
		_ = json.Unmarshal(ev.Data, &data)
		if _, seen := byType[ev.Type]; !seen {
			byType[ev.Type] = normalize(data)
		}
	}
	// 同一类事件在不同状态下内容不同：run.status 把出现过的每一种都录下来
	statuses := map[string]any{}
	for _, ev := range events {
		if ev.Type != "run.status" {
			continue
		}
		var d map[string]any
		_ = json.Unmarshal(ev.Data, &d)
		statuses[d["status"].(string)] = normalize(d)
	}
	b.bc.mu.Lock()
	for _, m := range b.bc.msgs {
		v, ok := m.Data.(*AgentEventView)
		if !ok || v.Seq != 0 {
			continue
		}
		var data any
		_ = json.Unmarshal(v.Data, &data)
		if _, seen := byType[v.Type]; !seen {
			byType[v.Type] = normalize(data)
		}
	}
	b.bc.mu.Unlock()

	texts := map[string]any{}
	for _, st := range []string{"queued", "running", "waiting_approval", "waiting_input", "succeeded", "failed", "canceled", "budget_exhausted", "step_limit", "timeout", "interrupted", "expired"} {
		title, hint := RunStatusText(st)
		texts[st] = map[string]string{"title": title, "hint": hint}
	}
	return map[string]any{"views": views, "events": byType, "run_statuses": statuses, "status_texts": texts}
}

func TestGolden_RESTContract(t *testing.T) {
	got, err := json.MarshalIndent(recordGolden(t), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	if os.Getenv("UPDATE_AGENT_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("已写入 %s", goldenPath)
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("没有黄金文件 %s：先用 UPDATE_AGENT_GOLDEN=1 录一份：%v", goldenPath, err)
	}
	if !bytes.Equal(canon(t, want), canon(t, got)) {
		t.Fatalf("Go 现在对前端的输出和黄金文件不一致。如果是有意的改动：UPDATE_AGENT_GOLDEN=1 重新录制，并让 agent/ 里的实现跟上（agent/test/golden.test.ts 会比对）")
	}
}

// canon 把 JSON 规整成键有序的紧凑形式再比较，免得格式差异造成误报。
func canon(t *testing.T, raw []byte) []byte {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(sortKeys(v))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func sortKeys(v any) any {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make([]any, 0, len(keys)*2)
		for _, k := range keys {
			out = append(out, k, sortKeys(x[k]))
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = sortKeys(e)
		}
		return out
	}
	return v
}
