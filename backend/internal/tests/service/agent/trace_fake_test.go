package agent_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/repository"
	. "video-canvas/internal/service/agent"
	"video-canvas/internal/storage"
)

// ---- 轨迹测试的 fake：内存对象存储 + 内存仓储（同时满足记录器、后台服务、清理器的依赖接口）----

var (
	_ TraceRepo        = (*fakeTraceRepo)(nil)
	_ TraceAdminRepo   = (*fakeTraceRepo)(nil)
	_ TraceCleanupRepo = (*fakeTraceRepo)(nil)
	_ TraceStores      = (*fakeTraceStores)(nil)
)

// memStorage 是内存里的对象存储。
type memStorage struct {
	mu      sync.Mutex
	objs    map[string][]byte
	putErr  error
	delErr  error
	deleted []string
}

func newMemStorage() *memStorage { return &memStorage{objs: map[string][]byte{}} }

func (m *memStorage) Put(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.putErr != nil {
		return m.putErr
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	m.objs[key] = b
	return nil
}

func (m *memStorage) Open(_ context.Context, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.objs[key]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (m *memStorage) URL(context.Context, string, time.Duration) (string, error) { return "", nil }

func (m *memStorage) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.delErr != nil {
		return m.delErr
	}
	delete(m.objs, key)
	m.deleted = append(m.deleted, key)
	return nil
}

func (m *memStorage) keys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for k := range m.objs {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// lines 解压一个对象并按行解析成 map，供断言。
func (m *memStorage) lines(t *testing.T, key string) []map[string]any {
	t.Helper()
	m.mu.Lock()
	raw, ok := m.objs[key]
	m.mu.Unlock()
	if !ok {
		t.Fatalf("对象 %s 不存在，现有：%v", key, m.keys())
	}
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(string(plain)), "\n") {
		var v map[string]any
		if err := json.Unmarshal([]byte(l), &v); err != nil {
			t.Fatalf("对象里有不是 JSON 的行 %q: %v", l, err)
		}
		out = append(out, v)
	}
	return out
}

type fakeTraceStores struct {
	st      *memStorage
	id      uint64
	missing map[uint64]bool // 这些存储配置已被删除
}

func newFakeTraceStores() *fakeTraceStores {
	return &fakeTraceStores{st: newMemStorage(), id: 7, missing: map[uint64]bool{}}
}

func (f *fakeTraceStores) Default(context.Context) (*storage.Handle, error) {
	return &storage.Handle{ID: f.id, Storage: f.st}, nil
}

func (f *fakeTraceStores) Get(_ context.Context, id uint64) (*storage.Handle, error) {
	if f.missing[id] {
		return nil, storage.ErrStorageNotFound
	}
	return &storage.Handle{ID: id, Storage: f.st}, nil
}

// fakeTraceRepo 记下所有写入，并提供读取。
type fakeTraceRepo struct {
	mu        sync.Mutex
	spans     []model.AgentTraceSpan
	calls     []model.AgentModelCall
	updates   []repository.RunTraceUpdate
	canvas    *model.CanvasProject
	canvasErr error
	spanErr   error // 写索引时返回的错误
	runs      map[uint64]*model.AgentRun
	events    []model.AgentEvent
	muts      []model.AgentMutation
	approvals []model.AgentApproval
	listRows  []repository.AdminRunRow
	gotFilter repository.AdminRunFilter
	nextID    uint64
	eventsDel int64
}

func newFakeTraceRepo() *fakeTraceRepo {
	return &fakeTraceRepo{runs: map[uint64]*model.AgentRun{}, canvas: &model.CanvasProject{Revision: 3, PayloadJSON: []byte(`{"nodes":[],"edges":[]}`)}}
}

func (f *fakeTraceRepo) CreateTraceSpan(_ context.Context, s *model.AgentTraceSpan) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.spanErr != nil {
		return f.spanErr
	}
	f.nextID++
	c := *s
	c.ID = f.nextID
	f.spans = append(f.spans, c)
	return nil
}

func (f *fakeTraceRepo) TraceCounters(_ context.Context, runID uint64) (int, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	maxSeq, maxSeg := 0, 0
	for _, s := range f.spans {
		if s.RunID == runID {
			maxSeq, maxSeg = max(maxSeq, s.Seq), max(maxSeg, s.SegmentSeq)
		}
	}
	for _, c := range f.calls {
		if c.RunID == runID {
			maxSeq, maxSeg = max(maxSeq, c.Seq), max(maxSeg, c.SegmentSeq)
		}
	}
	return maxSeq, maxSeg, nil
}

func (f *fakeTraceRepo) UpdateRunTrace(_ context.Context, _ uint64, in repository.RunTraceUpdate) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, in)
	return nil
}

func (f *fakeTraceRepo) GetCanvas(context.Context, uint64, uint64) (*model.CanvasProject, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.canvasErr != nil {
		return nil, f.canvasErr
	}
	c := *f.canvas
	return &c, nil
}

func (f *fakeTraceRepo) GetRunAny(_ context.Context, id uint64) (*model.AgentRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.runs[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	c := *r
	return &c, nil
}

func (f *fakeTraceRepo) ListAdminRuns(_ context.Context, fl repository.AdminRunFilter) ([]repository.AdminRunRow, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gotFilter = fl
	return f.listRows, int64(len(f.listRows)), nil
}

func (f *fakeTraceRepo) ListTraceSpans(_ context.Context, runID uint64) ([]model.AgentTraceSpan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []model.AgentTraceSpan
	for _, s := range f.spans {
		if s.RunID == runID {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakeTraceRepo) ListModelCallsByRun(_ context.Context, runID uint64) ([]model.AgentModelCall, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []model.AgentModelCall
	for _, c := range f.calls {
		if c.RunID == runID {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeTraceRepo) ListRunEvents(_ context.Context, runID uint64) ([]model.AgentEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []model.AgentEvent
	for _, e := range f.events {
		if e.RunID == runID {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *fakeTraceRepo) ListMutations(_ context.Context, runID uint64) ([]model.AgentMutation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []model.AgentMutation
	for _, m := range f.muts {
		if m.RunID == runID {
			out = append(out, m)
		}
	}
	return out, nil
}

func (f *fakeTraceRepo) ListApprovals(_ context.Context, runID uint64) ([]model.AgentApproval, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []model.AgentApproval
	for _, a := range f.approvals {
		if a.RunID == runID {
			out = append(out, a)
		}
	}
	return out, nil
}

func (f *fakeTraceRepo) ListExpiredTraceSegments(_ context.Context, before time.Time, limit int) ([]model.AgentTraceSpan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []model.AgentTraceSpan
	for _, s := range f.spans {
		if s.Kind == model.TraceSpanSegment && s.ObjectKey != "" && s.StartedAt.Before(before) && len(out) < limit {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakeTraceRepo) DeleteTraceSpansByID(_ context.Context, ids []uint64) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	want := map[uint64]bool{}
	for _, id := range ids {
		want[id] = true
	}
	kept := f.spans[:0]
	var n int64
	for _, s := range f.spans {
		if want[s.ID] {
			n++
			continue
		}
		kept = append(kept, s)
	}
	f.spans = kept
	return n, nil
}

func (f *fakeTraceRepo) DeleteExpiredTraceSpans(_ context.Context, before time.Time, limit int) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	kept := f.spans[:0]
	var n int64
	for _, s := range f.spans {
		if s.StartedAt.Before(before) && !(s.Kind == model.TraceSpanSegment && s.ObjectKey != "") && n < int64(limit) {
			n++
			continue
		}
		kept = append(kept, s)
	}
	f.spans = kept
	return n, nil
}

func (f *fakeTraceRepo) DeleteEventsBefore(_ context.Context, before time.Time, limit int) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	kept := f.events[:0]
	var n int64
	for _, e := range f.events {
		if e.CreatedAt.Before(before) && n < int64(limit) {
			n++
			continue
		}
		kept = append(kept, e)
	}
	f.events = kept
	f.eventsDel += n
	return n, nil
}

// spansOf 按种类取已写入的索引行。
func (f *fakeTraceRepo) spansOf(kind string) []model.AgentTraceSpan {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []model.AgentTraceSpan
	for _, s := range f.spans {
		if s.Kind == kind {
			out = append(out, s)
		}
	}
	return out
}

// traceEnv 是记录器测试的公共环境：临时目录、内存存储、固定时钟。
type traceEnv struct {
	repo   *fakeTraceRepo
	stores *fakeTraceStores
	dir    string
	rec    *TraceRecorder
	now    time.Time
}

func newTraceEnv(t *testing.T, mut func(*TraceOptions)) *traceEnv {
	t.Helper()
	e := &traceEnv{repo: newFakeTraceRepo(), stores: newFakeTraceStores(), dir: filepath.Join(t.TempDir(), "agent-traces"), now: time.Date(2026, 10, 20, 8, 30, 0, 0, time.UTC)}
	opt := TraceOptions{Enabled: true, MaxSpanBytes: 256 << 10, MaxRunBytes: 8 << 20, Dir: e.dir, Now: func() time.Time { return e.now }}
	if mut != nil {
		mut(&opt)
	}
	e.rec = NewTraceRecorder(e.repo, e.stores, opt)
	return e
}

func (e *traceEnv) run() *model.AgentRun {
	return &model.AgentRun{ID: 42, UserID: 1, CanvasID: 7, SessionID: 3, Status: model.RunRunning, TraceEnabled: true, PromptVersion: 10,
		CreatedAt: time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)}
}

func startInput() map[string]any {
	return map[string]any{"mode": "start", "system_prompt": "你是画布助手", "tools": []ToolDecl{{Name: "canvas_get_state", Label: "读取画布", Description: "读画布", Parameters: json.RawMessage(`{"type":"object"}`), Executor: ToolExecServer}}}
}

// tempFiles 返回临时目录里的文件名。
func (e *traceEnv) tempFiles(t *testing.T) []string {
	t.Helper()
	ents, err := os.ReadDir(e.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, en := range ents {
		out = append(out, en.Name())
	}
	return out
}
