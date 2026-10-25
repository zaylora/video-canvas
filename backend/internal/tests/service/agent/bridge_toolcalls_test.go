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
