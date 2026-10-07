package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"gorm.io/datatypes"

	"video-canvas/internal/agent/canvasgraph"
	"video-canvas/internal/model"
	"video-canvas/internal/repository"
	. "video-canvas/internal/service/agent"
)

// 以下让 fakeAgentCanvasRepo 同时满足 AgentRepo：会话、运行、事件、审批的内存实现，
// 语义与真实仓储一致（同一画布只能有一个活跃运行、状态迁移是 CAS、事件序号递增）。
var _ AgentRepo = (*fakeAgentCanvasRepo)(nil)

type agentStore struct {
	mu        sync.Mutex
	sessions  map[uint64]*model.AgentSession
	events    []model.AgentEvent
	approvals map[uint64]*model.AgentApproval
	nextID    uint64
}

var stores sync.Map // *fakeAgentCanvasRepo → *agentStore

func (f *fakeAgentCanvasRepo) st() *agentStore {
	v, _ := stores.LoadOrStore(f, &agentStore{sessions: map[uint64]*model.AgentSession{}, approvals: map[uint64]*model.AgentApproval{}})
	return v.(*agentStore)
}

func (s *agentStore) id() uint64 { s.nextID++; return s.nextID }

func (f *fakeAgentCanvasRepo) CreateSession(_ context.Context, sess *model.AgentSession) error {
	st := f.st()
	st.mu.Lock()
	defer st.mu.Unlock()
	sess.ID = st.id()
	c := *sess
	st.sessions[sess.ID] = &c
	return nil
}

func (f *fakeAgentCanvasRepo) GetSession(_ context.Context, userID, id uint64) (*model.AgentSession, error) {
	st := f.st()
	st.mu.Lock()
	defer st.mu.Unlock()
	s, ok := st.sessions[id]
	if !ok || s.UserID != userID {
		return nil, repository.ErrNotFound
	}
	c := *s
	return &c, nil
}

func (f *fakeAgentCanvasRepo) ListSessions(_ context.Context, userID, canvasID uint64) ([]model.AgentSession, error) {
	st := f.st()
	st.mu.Lock()
	defer st.mu.Unlock()
	var out []model.AgentSession
	for _, s := range st.sessions {
		if s.UserID == userID && s.CanvasID == canvasID {
			out = append(out, *s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

func (f *fakeAgentCanvasRepo) CountSessions(ctx context.Context, userID, canvasID uint64) (int64, error) {
	l, _ := f.ListSessions(ctx, userID, canvasID)
	return int64(len(l)), nil
}

func (f *fakeAgentCanvasRepo) UpdateSession(_ context.Context, userID, id uint64, fields map[string]any) error {
	st := f.st()
	st.mu.Lock()
	defer st.mu.Unlock()
	s, ok := st.sessions[id]
	if !ok || s.UserID != userID {
		return repository.ErrNotFound
	}
	for k, v := range fields {
		switch k {
		case "title":
			s.Title = v.(string)
		case "mode":
			s.Mode = v.(string)
		case "model_key":
			s.ModelKey = v.(string)
		case "session_jsonl":
			s.SessionJSONL = v.(string)
		}
	}
	return nil
}

func (f *fakeAgentCanvasRepo) DeleteSession(_ context.Context, userID, id uint64) error {
	st := f.st()
	st.mu.Lock()
	defer st.mu.Unlock()
	s, ok := st.sessions[id]
	if !ok || s.UserID != userID {
		return repository.ErrNotFound
	}
	delete(st.sessions, id)
	return nil
}

func (f *fakeAgentCanvasRepo) CreateRun(_ context.Context, run *model.AgentRun) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if model.IsActiveRun(run.Status) {
		for _, r := range f.runs {
			if r.CanvasID == run.CanvasID && model.IsActiveRun(r.Status) {
				return repository.ErrDuplicate
			}
		}
	}
	st := f.st()
	st.mu.Lock()
	run.ID = st.id() + 100
	st.mu.Unlock()
	c := *run
	f.runs[run.ID] = &c
	return nil
}

func (f *fakeAgentCanvasRepo) ActiveRun(_ context.Context, canvasID uint64) (*model.AgentRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.runs {
		if r.CanvasID == canvasID && model.IsActiveRun(r.Status) {
			c := *r
			return &c, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (f *fakeAgentCanvasRepo) UpdateRunIf(_ context.Context, id uint64, from []string, fields map[string]any) (*model.AgentRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.runs[id]
	if !ok {
		return nil, repository.ErrAgentStateConflict
	}
	hit := false
	for _, s := range from {
		hit = hit || s == r.Status
	}
	if !hit {
		return nil, repository.ErrAgentStateConflict
	}
	if st, ok := fields["status"].(string); ok && model.IsActiveRun(st) && !model.IsActiveRun(r.Status) {
		for _, o := range f.runs {
			if o.ID != id && o.CanvasID == r.CanvasID && model.IsActiveRun(o.Status) {
				return nil, repository.ErrDuplicate
			}
		}
	}
	for k, v := range fields {
		switch k {
		case "status":
			r.Status = v.(string)
		case "budget_credits":
			r.BudgetCredits = v.(int)
		case "max_steps":
			r.MaxSteps = v.(int)
		case "error_code":
			r.ErrorCode = v.(string)
		case "error_message":
			r.ErrorMessage = v.(string)
		}
	}
	c := *r
	return &c, nil
}

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

func (f *fakeAgentCanvasRepo) AppendEvent(_ context.Context, sessionID, runID uint64, typ string, payload datatypes.JSON) (*model.AgentEvent, error) {
	st := f.st()
	st.mu.Lock()
	defer st.mu.Unlock()
	if _, ok := st.sessions[sessionID]; !ok {
		return nil, repository.ErrNotFound
	}
	var seq int64
	for _, e := range st.events {
		if e.SessionID == sessionID && e.Seq > seq {
			seq = e.Seq
		}
	}
	ev := model.AgentEvent{ID: st.id(), SessionID: sessionID, RunID: runID, Seq: seq + 1, Type: typ, PayloadJSON: payload}
	st.events = append(st.events, ev)
	return &ev, nil
}

func (f *fakeAgentCanvasRepo) ListEvents(_ context.Context, sessionID uint64, after int64, limit int) ([]model.AgentEvent, error) {
	st := f.st()
	st.mu.Lock()
	defer st.mu.Unlock()
	var out []model.AgentEvent
	for _, e := range st.events {
		if e.SessionID == sessionID && e.Seq > after && len(out) < limit {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *fakeAgentCanvasRepo) eventTypes() []string {
	st := f.st()
	st.mu.Lock()
	defer st.mu.Unlock()
	out := make([]string, len(st.events))
	for i, e := range st.events {
		out[i] = e.Type
	}
	return out
}

func (f *fakeAgentCanvasRepo) CreateApproval(_ context.Context, a *model.AgentApproval) error {
	st := f.st()
	st.mu.Lock()
	defer st.mu.Unlock()
	a.ID = st.id() + 1000
	if len(a.DecisionJSON) == 0 {
		a.DecisionJSON = datatypes.JSON("{}")
	}
	if len(a.ResultJSON) == 0 {
		a.ResultJSON = datatypes.JSON("{}")
	}
	c := *a
	st.approvals[a.ID] = &c
	return nil
}

func (f *fakeAgentCanvasRepo) GetApproval(_ context.Context, userID, id uint64) (*model.AgentApproval, error) {
	st := f.st()
	st.mu.Lock()
	defer st.mu.Unlock()
	a, ok := st.approvals[id]
	if !ok || a.UserID != userID {
		return nil, repository.ErrNotFound
	}
	c := *a
	return &c, nil
}

func (f *fakeAgentCanvasRepo) UpdateApprovalIf(_ context.Context, id uint64, from []string, fields map[string]any) (*model.AgentApproval, error) {
	st := f.st()
	st.mu.Lock()
	defer st.mu.Unlock()
	a, ok := st.approvals[id]
	if !ok {
		return nil, repository.ErrAgentStateConflict
	}
	hit := false
	for _, s := range from {
		hit = hit || s == a.Status
	}
	if !hit {
		return nil, repository.ErrAgentStateConflict
	}
	for k, v := range fields {
		switch k {
		case "status":
			a.Status = v.(string)
		case "quote_credits":
			a.QuoteCredits = v.(int)
		case "decision_json":
			a.DecisionJSON = datatypes.JSON(v.([]byte))
		case "result_json":
			a.ResultJSON = datatypes.JSON(v.(json.RawMessage))
		}
	}
	c := *a
	return &c, nil
}

func (f *fakeAgentCanvasRepo) ExpirePendingApprovals(_ context.Context, runID uint64) (int64, error) {
	st := f.st()
	st.mu.Lock()
	defer st.mu.Unlock()
	var n int64
	for _, a := range st.approvals {
		if a.RunID == runID && a.Status == model.ApprovalPending {
			a.Status = model.ApprovalExpired
			n++
		}
	}
	return n, nil
}

// ---- 其他依赖的 fake ----

type fakeAgentRuntime struct {
	mu                                      sync.Mutex
	started, interjected, canceled, resumed []uint64
	resumeInfos                             []ResumeInfo
	inputs                                  []model.AgentRunInput
	startErr, interjectErr, resumeErr       error
}

func (r *fakeAgentRuntime) Start(_ context.Context, run *model.AgentRun, in model.AgentRunInput) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.started = append(r.started, run.ID)
	r.inputs = append(r.inputs, in)
	return r.startErr
}
func (r *fakeAgentRuntime) Interject(_ context.Context, id uint64, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.interjected = append(r.interjected, id)
	return r.interjectErr
}
func (r *fakeAgentRuntime) Cancel(_ context.Context, id uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.canceled = append(r.canceled, id)
	return nil
}
func (r *fakeAgentRuntime) Resume(_ context.Context, run *model.AgentRun, info ResumeInfo) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resumed = append(r.resumed, run.ID)
	r.resumeInfos = append(r.resumeInfos, info)
	return r.resumeErr
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
