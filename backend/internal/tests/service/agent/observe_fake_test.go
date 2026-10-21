package agent_test

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"gorm.io/datatypes"

	"video-canvas/internal/model"
	"video-canvas/internal/repository"
)

// fakeObserveRepo 是 ObserveRepo 的内存实现，语义与真实仓储一致（反馈每人每运行一条、摘要整行覆盖、令牌按哈希唯一）。
type fakeObserveRepo struct {
	mu        sync.Mutex
	facts     map[uint64]*model.AgentRunFacts
	exports   map[uint64]*model.AgentExportFacts
	digests   map[uint64]*model.AgentRunDigest
	feedback  map[[2]uint64]*model.AgentFeedback
	tokens    []*model.AdminAPIToken
	touched   map[uint64]int
	listQuery model.DigestQuery
	total     int64
	statsQ    model.StatsQuery
	listErr   error
	factsErr  error
	upsertErr error
	purgeAt   time.Time
	nextTok   uint64
}

func newFakeObserveRepo() *fakeObserveRepo {
	return &fakeObserveRepo{facts: map[uint64]*model.AgentRunFacts{}, exports: map[uint64]*model.AgentExportFacts{}, digests: map[uint64]*model.AgentRunDigest{},
		feedback: map[[2]uint64]*model.AgentFeedback{}, touched: map[uint64]int{}}
}

func (f *fakeObserveRepo) RunFacts(_ context.Context, runID uint64) (*model.AgentRunFacts, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.factsErr != nil {
		return nil, f.factsErr
	}
	x, ok := f.facts[runID]
	if !ok {
		return nil, repository.ErrNotFound
	}
	cp := *x
	if fb, ok := f.feedback[[2]uint64{runID, x.Run.UserID}]; ok {
		cp.Feedback = fb
	} else {
		cp.Feedback = nil
	}
	return &cp, nil
}

func (f *fakeObserveRepo) UpsertDigest(_ context.Context, d *model.AgentRunDigest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.upsertErr != nil {
		return f.upsertErr
	}
	cp := *d
	f.digests[d.RunID] = &cp
	return nil
}

func (f *fakeObserveRepo) GetDigest(_ context.Context, runID uint64) (*model.AgentRunDigest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.digests[runID]
	if !ok {
		return nil, repository.ErrNotFound
	}
	cp := *d
	return &cp, nil
}

func (f *fakeObserveRepo) ListDigests(_ context.Context, q model.DigestQuery) ([]model.AgentRunDigest, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listQuery = q
	if f.listErr != nil {
		return nil, "", f.listErr
	}
	var out []model.AgentRunDigest
	for _, d := range f.digests {
		out = append(out, *d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RunID > out[j].RunID })
	return out, "next-cursor", nil
}

func (f *fakeObserveRepo) CountDigests(_ context.Context, q model.DigestQuery) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listQuery = q
	return f.total, f.listErr
}

func (f *fakeObserveRepo) ExportFacts(_ context.Context, runID uint64) (*model.AgentExportFacts, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	x, ok := f.exports[runID]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return x, nil
}

func (f *fakeObserveRepo) Stats(_ context.Context, q model.StatsQuery) ([]model.AgentStatsRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statsQ = q
	return []model.AgentStatsRow{{Key: "h9", N: 3}}, nil
}

func (f *fakeObserveRepo) GetFeedback(_ context.Context, runID, userID uint64) (*model.AgentFeedback, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fb, ok := f.feedback[[2]uint64{runID, userID}]
	if !ok {
		return nil, repository.ErrNotFound
	}
	cp := *fb
	return &cp, nil
}

func (f *fakeObserveRepo) UpsertFeedback(_ context.Context, fb *model.AgentFeedback) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *fb
	f.feedback[[2]uint64{fb.RunID, fb.UserID}] = &cp
	return nil
}

func (f *fakeObserveRepo) DeleteFeedback(_ context.Context, runID, userID uint64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.feedback, [2]uint64{runID, userID})
	return nil
}

func (f *fakeObserveRepo) PurgeObserveBefore(_ context.Context, cutoff, _ time.Time) (int64, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.purgeAt = cutoff
	return 3, 1, nil
}

func (f *fakeObserveRepo) CreateToken(_ context.Context, t *model.AdminAPIToken) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextTok++
	t.ID = f.nextTok
	cp := *t
	f.tokens = append(f.tokens, &cp)
	return nil
}

func (f *fakeObserveRepo) ListTokens(_ context.Context) ([]model.AdminAPIToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []model.AdminAPIToken
	for i := len(f.tokens) - 1; i >= 0; i-- {
		if f.tokens[i].RevokedAt == nil { // 与真实仓储一致：已注销的不返回
			out = append(out, *f.tokens[i])
		}
	}
	return out, nil
}

func (f *fakeObserveRepo) TokenByHash(_ context.Context, hash string) (*model.AdminAPIToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, t := range f.tokens {
		if t.TokenHash == hash {
			cp := *t
			return &cp, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (f *fakeObserveRepo) TouchToken(_ context.Context, id uint64, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.touched[id]++
	for _, t := range f.tokens {
		if t.ID == id {
			t.LastUsedAt = &at
		}
	}
	return nil
}

func (f *fakeObserveRepo) RevokeToken(_ context.Context, id uint64, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, t := range f.tokens {
		if t.ID == id {
			if t.RevokedAt == nil {
				t.RevokedAt = &at
			}
			return nil
		}
	}
	return repository.ErrNotFound
}

// fakeObserveRuns 是按用户读运行的内存实现。
type fakeObserveRuns map[uint64]*model.AgentRun

func (r fakeObserveRuns) GetRun(_ context.Context, userID, id uint64) (*model.AgentRun, error) {
	run, ok := r[id]
	if !ok || run.UserID != userID {
		return nil, repository.ErrNotFound
	}
	cp := *run
	return &cp, nil
}

// fakeEmitter 记下发出的事件。
type emitted struct {
	userID, sessionID, runID, canvasID uint64
	typ                                string
	data                               map[string]any
}

type fakeEmitter struct {
	mu     sync.Mutex
	events []emitted
}

func (e *fakeEmitter) Emit(_ context.Context, userID, sessionID, runID, canvasID uint64, typ string, data any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	raw, _ := json.Marshal(data)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	e.events = append(e.events, emitted{userID, sessionID, runID, canvasID, typ, m})
}

func (e *fakeEmitter) types() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []string
	for _, ev := range e.events {
		out = append(out, ev.typ)
	}
	return out
}

// fakeAudit 记下审计。
type fakeAudit struct {
	mu   sync.Mutex
	logs []*model.AdminAuditLog
}

func (a *fakeAudit) Insert(_ context.Context, l *model.AdminAuditLog) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.logs = append(a.logs, l)
	return nil
}

func (a *fakeAudit) actions() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []string
	for _, l := range a.logs {
		out = append(out, l.Action)
	}
	return out
}

var errBadCursor = repository.ErrInvalidCursor

func rawJSON(s string) datatypes.JSON { return datatypes.JSON(s) }
