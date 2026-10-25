package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"video-canvas/internal/agent/canvasgraph"
	"video-canvas/internal/model"
	"video-canvas/internal/repository"
	. "video-canvas/internal/service/agent"
)

// 让 fakeAgentCanvasRepo 满足 AgentRepo（会话侧只读画布）。运行记录在 Go 库里只剩旧数据，这里只存测试要用的几条。
var _ AgentRepo = (*fakeAgentCanvasRepo)(nil)

func (f *fakeAgentCanvasRepo) AddRunUsage(_ context.Context, id uint64, steps, credits int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.runs[id]
	if !ok {
		return repository.ErrNotFound
	}
	r.Steps += steps
	r.SpentCredits += credits
	return nil
}

type fakeAgentModels struct{ list []model.AgentModelView }

func (m fakeAgentModels) List(context.Context) ([]model.AgentModelView, error) { return m.list, nil }

type fakeGenerator struct {
	calls [][]GenerateItem
	err   error
}

func (g *fakeGenerator) Execute(_ context.Context, _ *model.AgentRun, id uint64, items []GenerateItem) (json.RawMessage, error) {
	g.calls = append(g.calls, items)
	if g.err != nil {
		return nil, g.err
	}
	return json.RawMessage(`{"task_ids":[1,2]}`), nil
}

var errAgentBoom = errors.New("boom")
var _ = time.Now

// mustParseGraph 读出假仓储里当前的画布。
func mustParseGraph(t *testing.T, f *fakeAgentCanvasRepo) *canvasgraph.Graph {
	t.Helper()
	g, err := canvasgraph.Parse(f.canvas.PayloadJSON)
	if err != nil {
		t.Fatal(err)
	}
	return g
}
