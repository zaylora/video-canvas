package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"video-canvas/internal/model"
	. "video-canvas/internal/service/agent"
)

// fakeToolRecorder 在内存里记工具调用，语义与真实仓储一致：(run, tool_call_id) 唯一，重复开始不覆盖。
type fakeToolRecorder struct {
	mu       sync.Mutex
	calls    map[string]*model.AgentToolCall
	order    []string
	startErr error
	endErr   error
}

func newFakeToolRecorder() *fakeToolRecorder {
	return &fakeToolRecorder{calls: map[string]*model.AgentToolCall{}}
}

func (r *fakeToolRecorder) StartToolCall(_ context.Context, c *model.AgentToolCall) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.startErr != nil {
		return r.startErr
	}
	k := c.ToolCallID
	if _, ok := r.calls[k]; ok {
		return nil
	}
	cp := *c
	r.calls[k] = &cp
	r.order = append(r.order, k)
	return nil
}

func (r *fakeToolRecorder) FinishToolCall(_ context.Context, _ uint64, toolCallID string, res ToolCallResult) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.endErr != nil {
		return r.endErr
	}
	c, ok := r.calls[toolCallID]
	if !ok {
		return errors.New("没有这条工具调用")
	}
	c.ResultText, c.ResultTruncated, c.IsError, c.Summary = &res.ResultText, res.Truncated, res.IsError, res.Summary
	end := res.EndedAt
	c.EndedAt, c.DurationMs = &end, res.DurationMs
	return nil
}

func (r *fakeToolRecorder) get(t *testing.T, id string) *model.AgentToolCall {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.calls[id]
	if !ok {
		t.Fatalf("没有记录工具调用 %q，已有 %v", id, r.order)
	}
	return c
}

func newToolBridge(t *testing.T) (*bridgeEnv, *fakeToolRecorder) {
	t.Helper()
	b := newBridgeEnv(t)
	rec := newFakeToolRecorder()
	b.br.SetToolRecorder(rec)
	return b, rec
}

func TestToolCallRecording(t *testing.T) {
	ctx := context.Background()

	t.Run("成功的调用：记下参数、返回给模型的文本、摘要和结束时间", func(t *testing.T) {
		b, rec := newToolBridge(t)
		tok, run := b.token(t, "all", "n_script")
		res := b.tool(t, tok, "canvas_get_state", map[string]any{"nodeIds": []string{"n_script"}})
		c := rec.get(t, "tc-canvas_get_state")
		if c.RunID != run.ID || c.SessionID != run.SessionID || c.Name != "canvas_get_state" || c.ParentID != "" {
			t.Errorf("归属不对：%+v", c)
		}
		var args map[string]any
		if err := json.Unmarshal(c.ArgsJSON, &args); err != nil || args["nodeIds"] == nil {
			t.Errorf("参数应原样记下：%s %v", c.ArgsJSON, err)
		}
		if c.ResultText == nil || *c.ResultText != res.Content {
			t.Errorf("记下的结果应与返回给模型的一致")
		}
		if c.IsError || c.EndedAt == nil || c.StartedAt.IsZero() {
			t.Errorf("成功调用应有开始和结束时间：%+v", c)
		}
		if c.Summary == "" {
			t.Error("摘要应与 tool.end 事件一致，不能为空")
		}
		if end := b.repo.lastEvent("tool.end"); end["summary"] != c.Summary {
			t.Errorf("摘要应与 tool.end 一致：%v vs %q", end["summary"], c.Summary)
		}
	})

	t.Run("工具报错的调用：is_error 为真，结果是给模型看的错误说明", func(t *testing.T) {
		b, rec := newToolBridge(t)
		tok, _ := b.token(t, "all")
		res := b.tool(t, tok, "canvas_get_state", map[string]any{"nodeIds": []string{"ghost"}})
		c := rec.get(t, "tc-canvas_get_state")
		if !res.IsError || !c.IsError || c.ResultText == nil || !strings.Contains(*c.ResultText, "ghost") {
			t.Errorf("c=%+v", c)
		}
	})

	t.Run("被拒绝的调用（未知工具、模式不允许）也要记：模型选错工具正是要分析的失败", func(t *testing.T) {
		b, rec := newToolBridge(t)
		tok, _ := b.token(t, "prompt")
		b.tool(t, tok, "no_such_tool", map[string]any{"x": 1})
		c := rec.get(t, "tc-no_such_tool")
		if !c.IsError || c.EndedAt == nil || !strings.Contains(c.Summary, "no_such_tool") {
			t.Errorf("未知工具：%+v", c)
		}
		b.tool(t, tok, "canvas_delete", map[string]any{"nodeIds": []string{"n_script"}})
		c = rec.get(t, "tc-canvas_delete")
		if !c.IsError || !strings.Contains(c.Summary, "模式") {
			t.Errorf("模式不允许：%+v", c)
		}
	})

	t.Run("步数用尽时被拦下的调用也记，并标为报错", func(t *testing.T) {
		b, rec := newToolBridge(t)
		tok, run := b.token(t, "all")
		b.repo.mu.Lock()
		b.repo.runs[run.ID].Steps = run.MaxSteps
		b.repo.mu.Unlock()
		b.tool(t, tok, "plan_update", map[string]any{"steps": []map[string]any{{"title": "x"}}})
		c := rec.get(t, "tc-plan_update")
		if !c.IsError || c.EndedAt == nil {
			t.Errorf("c=%+v", c)
		}
	})

	t.Run("脚本里的嵌套调用记下 parent_id", func(t *testing.T) {
		b, rec := newToolBridge(t)
		tok, _ := b.token(t, "all", "n_script")
		raw, _ := json.Marshal(map[string]any{"nodeIds": []string{"n_script"}})
		if _, err := b.br.ExecuteNestedTool(ctx, tok, "cm1", "inner1", "canvas_get_state", raw); err != nil {
			t.Fatal(err)
		}
		if c := rec.get(t, "inner1"); c.ParentID != "cm1" {
			t.Errorf("parent_id=%q", c.ParentID)
		}
	})

	t.Run("记录器出错不影响工具本身的结果", func(t *testing.T) {
		b, rec := newToolBridge(t)
		rec.startErr, rec.endErr = errors.New("库不可用"), errors.New("库不可用")
		tok, _ := b.token(t, "all", "n_script")
		res := b.tool(t, tok, "canvas_get_state", map[string]any{})
		if res.IsError || !strings.Contains(res.Content, "n_script") {
			t.Errorf("观测失败不能让用户的运行失败：%+v", res)
		}
	})

	t.Run("没有配置记录器：照常工作", func(t *testing.T) {
		b := newBridgeEnv(t)
		tok, _ := b.token(t, "all", "n_script")
		if res := b.tool(t, tok, "canvas_get_state", map[string]any{}); res.IsError {
			t.Errorf("res=%+v", res)
		}
	})

	t.Run("令牌无效这类不该发生的回调不记", func(t *testing.T) {
		b, rec := newToolBridge(t)
		if _, err := b.br.ExecuteTool(ctx, "bad-token", "tc-x", "plan_update", []byte(`{}`)); err == nil {
			t.Fatal("应报错")
		}
		if len(rec.order) != 0 {
			t.Errorf("不应记录：%v", rec.order)
		}
	})
}

func TestReportAgentTool_RecordsToolCall(t *testing.T) {
	b, rec := newToolBridge(t)
	tok, _ := b.token(t, "all")
	args := json.RawMessage(`{"code":"await canvas.get()"}`)
	report(t, b, tok, AgentToolEvent{Phase: "start", ToolCallID: "cm1", Name: "codemode", Label: "运行脚本", Verb: "脚本", Args: args})
	c := rec.get(t, "cm1")
	if c.Name != "codemode" || string(c.ArgsJSON) != string(args) || c.EndedAt != nil {
		t.Errorf("开始时应记下脚本参数、尚未结束：%+v", c)
	}
	report(t, b, tok, AgentToolEvent{Phase: "end", ToolCallID: "cm1", Name: "codemode", IsError: true, Summary: "脚本失败：出错了", Result: "ReferenceError: x"})
	c = rec.get(t, "cm1")
	if !c.IsError || c.EndedAt == nil || c.Summary != "脚本失败：出错了" || c.ResultText == nil || *c.ResultText != "ReferenceError: x" {
		t.Errorf("结束时应记下结果和摘要：%+v", c)
	}
}

func TestTruncateToolResult(t *testing.T) {
	short, cut := TruncateToolResult("不长")
	if short != "不长" || cut {
		t.Errorf("短结果原样返回：%q %v", short, cut)
	}
	long := strings.Repeat("长", model.MaxToolResultBytes) // 每个字 3 字节，远超上限
	out, cut := TruncateToolResult(long)
	if !cut || len(out) > model.MaxToolResultBytes || !utf8.ValidString(out) {
		t.Errorf("超长应截断到上限以内且不切坏字符：len=%d cut=%v valid=%v", len(out), cut, utf8.ValidString(out))
	}
	exact := strings.Repeat("a", model.MaxToolResultBytes)
	if out, cut := TruncateToolResult(exact); cut || out != exact {
		t.Error("刚好等于上限不算截断")
	}
}

func TestToolArgsForRecord(t *testing.T) {
	if got := ToolArgsForRecord(nil); got != nil {
		t.Errorf("没有参数记 NULL：%s", got)
	}
	if got := ToolArgsForRecord([]byte(`{bad`)); got != nil {
		t.Errorf("不是合法 JSON 的参数记 NULL（jsonb 列放不下）：%s", got)
	}
	if got := ToolArgsForRecord([]byte(`{"a":1}`)); string(got) != `{"a":1}` {
		t.Errorf("合法参数原样记：%s", got)
	}
	big := []byte(`{"text":"` + strings.Repeat("x", 100<<10) + `"}`)
	got := ToolArgsForRecord(big)
	var m map[string]any
	if err := json.Unmarshal(got, &m); err != nil || m["_truncated"] != true {
		t.Fatalf("超大参数应替换成带 _truncated 标记的摘要：%s %v", got[:min(len(got), 120)], err)
	}
	if len(got) > 4<<10 {
		t.Errorf("摘要应很小，得到 %d 字节", len(got))
	}
}
