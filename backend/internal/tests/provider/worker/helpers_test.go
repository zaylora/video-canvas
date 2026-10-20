package worker_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gorm.io/datatypes"

	"video-canvas/internal/model"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/provider/pluginmeta"
	. "video-canvas/internal/provider/worker"
)

// 编译期确认 fake 满足 worker 需要的存储契约。
var _ Store = (*wkStore)(nil)

// ---------------------------------------------------------------------------
// 时钟
// ---------------------------------------------------------------------------

// wkClock 是可手动推进的时钟，worker 和 fake store 共用。
type wkClock struct {
	mu sync.Mutex
	t  time.Time
}

func newWkClock() *wkClock { return &wkClock{t: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)} }

func (c *wkClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *wkClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// ---------------------------------------------------------------------------
// fake Store：复刻 service 里 CAS 迁移与租约的语义，供驱动完整状态流转
// ---------------------------------------------------------------------------

type wkFailCall struct {
	ID            uint64
	Code, Message string
}

type wkStore struct {
	mu    sync.Mutex
	clock *wkClock
	tasks map[uint64]*model.GenerationTask

	calls        []string // 调用流水，如 "MarkSubmitted:1"
	fails        []wkFailCall
	upstreams    []string // 每次 Fail / FailWithUpstream 收到的上游错误原文（与 fails 一一对应）
	completed    map[uint64][]model.TaskOutput
	usage        map[uint64]*modelcfg.Usage
	traces       map[uint64][][]provider.TraceStep // 每次 SaveTrace 的入参
	saveStateErr error
	saveTraceErr error
	// beforeMarkSubmitted 在 MarkSubmitted / MarkImmediate 内部触发（测试用来模拟“提交期间被取消”）
	beforeMarkSubmitted func(id uint64)
	// beforeMarkSubmitting 在 MarkSubmitting 内部触发（测试用来模拟“领取后、开始调用上游前被取消”）
	beforeMarkSubmitting func(id uint64)
}

func newWkStore(clock *wkClock) *wkStore {
	return &wkStore{
		clock: clock, tasks: map[uint64]*model.GenerationTask{},
		completed: map[uint64][]model.TaskOutput{}, usage: map[uint64]*modelcfg.Usage{}, traces: map[uint64][][]provider.TraceStep{},
	}
}

func (s *wkStore) add(t model.GenerationTask) *model.GenerationTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := t
	s.tasks[t.ID] = &cp
	return &cp
}

func (s *wkStore) get(id uint64) model.GenerationTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	return *s.tasks[id]
}

// setStatus 直接改任务状态，模拟其它流程（用户取消、别的实例）的迁移。
func (s *wkStore) setStatus(id uint64, status string) {
	s.mu.Lock()
	s.tasks[id].Status = status
	s.mu.Unlock()
}

// state 解码任务当前的 provider_state。
func (s *wkStore) state(id uint64) provider.ProviderState {
	st, _ := provider.DecodeProviderState(s.get(id).ProviderState)
	return st
}

func (s *wkStore) callList() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

func (s *wkStore) count(prefix string) int {
	n := 0
	for _, c := range s.callList() {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func (s *wkStore) usageOf(id uint64) *modelcfg.Usage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.usage[id]
}

func (s *wkStore) completedOf(id uint64) []model.TaskOutput {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.completed[id]
}

func (s *wkStore) tracesOf(id uint64) [][]provider.TraceStep {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.traces[id]
}

func (s *wkStore) rec(format string, args ...any) {
	s.calls = append(s.calls, fmt.Sprintf(format, args...))
}

// GetTask 与队列用的读取一致：任务不存在返回 (nil, nil)。worker 本身不调用它，只是满足 provider.TaskStore。
func (s *wkStore) GetTask(ctx context.Context, id uint64) (*model.GenerationTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.tasks[id]
	if t == nil {
		return nil, nil
	}
	cp := *t
	return &cp, nil
}

// AcquireSlot 同上：worker 本身不调用，名额由队列在调用 Handle 之前申请，这里直接放行。
func (s *wkStore) AcquireSlot(ctx context.Context, t *model.GenerationTask) (provider.SlotOutcome, error) {
	return provider.SlotAcquired, nil
}

// cas 找到任务并检查状态在 from 里，否则返回 nil（applied=false）。
func (s *wkStore) cas(id uint64, from ...string) *model.GenerationTask {
	t := s.tasks[id]
	if t == nil {
		return nil
	}
	for _, f := range from {
		if t.Status == f {
			return t
		}
	}
	return nil
}

// setPluginState 把插件 state 并进 provider_state（nil 表示不变），保留 prepared，与 service 的语义一致。
func setPluginState(t *model.GenerationTask, plugin json.RawMessage) {
	if plugin == nil {
		return
	}
	st, _ := provider.DecodeProviderState(t.ProviderState)
	st.Plugin = plugin
	t.ProviderState, _ = json.Marshal(st)
}

func (s *wkStore) SaveProviderState(ctx context.Context, t *model.GenerationTask, st provider.ProviderState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.saveStateErr != nil {
		return s.saveStateErr
	}
	cur := s.cas(t.ID, model.ActiveTaskStatuses...)
	if cur == nil {
		return nil
	}
	cur.ProviderState, _ = json.Marshal(st)
	s.rec("SaveProviderState:%d", t.ID)
	return nil
}

func (s *wkStore) MarkSubmitting(ctx context.Context, t *model.GenerationTask) (bool, error) {
	if s.beforeMarkSubmitting != nil {
		s.beforeMarkSubmitting(t.ID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.cas(t.ID, model.TaskPending)
	if cur == nil {
		s.rec("MarkSubmitting:%d:conflict", t.ID)
		return false, nil
	}
	now := s.clock.Now()
	cur.SubmittedAt = &now
	s.rec("MarkSubmitting:%d", t.ID)
	return true, nil
}

func (s *wkStore) MarkSubmitted(ctx context.Context, t *model.GenerationTask, pid string, pluginState json.RawMessage, next time.Time) (bool, error) {
	if s.beforeMarkSubmitted != nil {
		s.beforeMarkSubmitted(t.ID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.cas(t.ID, model.TaskPending)
	if cur == nil {
		s.rec("MarkSubmitted:%d:conflict", t.ID)
		return false, nil
	}
	cur.Status, cur.ProviderTaskID, cur.NextPollAt, cur.PollAttempts, cur.LeaseUntil = model.TaskQueued, pid, next, 0, nil
	setPluginState(cur, pluginState)
	s.rec("MarkSubmitted:%d", t.ID)
	return true, nil
}

func (s *wkStore) MarkImmediate(ctx context.Context, t *model.GenerationTask, pid string, pluginState, outputs json.RawMessage) (bool, error) {
	if s.beforeMarkSubmitted != nil {
		s.beforeMarkSubmitted(t.ID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.cas(t.ID, model.TaskPending)
	if cur == nil {
		s.rec("MarkImmediate:%d:conflict", t.ID)
		return false, nil
	}
	cur.Status, cur.ProviderTaskID, cur.ProviderResult = model.TaskFinalizing, pid, datatypes.JSON(outputs)
	cur.NextPollAt, cur.PollAttempts, cur.LeaseUntil = s.clock.Now(), 0, nil
	setPluginState(cur, pluginState)
	s.rec("MarkImmediate:%d", t.ID)
	return true, nil
}

func (s *wkStore) MarkPolled(ctx context.Context, t *model.GenerationTask, status string, progress *int, pluginState json.RawMessage, attempts int, next time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.cas(t.ID, model.TaskQueued, model.TaskRunning)
	if cur == nil {
		s.rec("MarkPolled:%d:conflict", t.ID)
		return false, nil
	}
	cur.Status, cur.PollAttempts, cur.NextPollAt, cur.LeaseUntil = status, attempts, next, nil
	if progress != nil {
		p := *progress
		cur.Progress = &p
	}
	setPluginState(cur, pluginState)
	s.rec("MarkPolled:%d:%s", t.ID, status)
	return true, nil
}

func (s *wkStore) MarkFinalizing(ctx context.Context, t *model.GenerationTask) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.cas(t.ID, model.TaskQueued, model.TaskRunning)
	if cur == nil {
		return false, nil
	}
	cur.Status, cur.PollAttempts, cur.NextPollAt, cur.LeaseUntil = model.TaskFinalizing, 0, s.clock.Now(), nil
	s.rec("MarkFinalizing:%d", t.ID)
	return true, nil
}

func (s *wkStore) Retry(ctx context.Context, t *model.GenerationTask, attempts int, next time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.cas(t.ID, t.Status)
	if cur == nil {
		return false, nil
	}
	cur.PollAttempts, cur.NextPollAt, cur.LeaseUntil = attempts, next, nil
	s.rec("Retry:%d:%d", t.ID, attempts)
	return true, nil
}

func (s *wkStore) Complete(ctx context.Context, t *model.GenerationTask, outs []model.TaskOutput, usage *modelcfg.Usage) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.cas(t.ID, model.TaskFinalizing)
	if cur == nil {
		s.rec("Complete:%d:conflict", t.ID)
		return false, nil
	}
	cur.Status, cur.LeaseUntil = model.TaskSucceeded, nil
	s.completed[t.ID] = outs
	s.usage[t.ID] = usage
	s.rec("Complete:%d", t.ID)
	return true, nil
}

func (s *wkStore) Fail(ctx context.Context, t *model.GenerationTask, code, msg string) (bool, error) {
	return s.FailWithUpstream(ctx, t, code, msg, "")
}

// FailWithUpstream 与 Fail 记录相同的调用，并额外记下传来的上游错误原文。
func (s *wkStore) FailWithUpstream(ctx context.Context, t *model.GenerationTask, code, msg, upstream string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.cas(t.ID, model.ActiveTaskStatuses...)
	if cur == nil {
		return false, nil
	}
	cur.Status, cur.ErrorCode, cur.ErrorMessage, cur.LeaseUntil = model.TaskFailed, code, msg, nil
	s.fails = append(s.fails, wkFailCall{t.ID, code, msg})
	s.upstreams = append(s.upstreams, upstream)
	s.rec("Fail:%d:%s", t.ID, code)
	return true, nil
}

func (s *wkStore) Expire(ctx context.Context, t *model.GenerationTask) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.cas(t.ID, model.ActiveTaskStatuses...)
	if cur == nil {
		return false, nil
	}
	cur.Status, cur.ErrorCode, cur.LeaseUntil = model.TaskExpired, "timeout", nil
	s.rec("Expire:%d", t.ID)
	return true, nil
}

func (s *wkStore) SaveTrace(ctx context.Context, t *model.GenerationTask, steps []provider.TraceStep) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.traces[t.ID] = append(s.traces[t.ID], append([]provider.TraceStep(nil), steps...))
	if s.saveTraceErr != nil {
		return s.saveTraceErr
	}
	cur := s.tasks[t.ID]
	if cur == nil || !cur.IsTest {
		return nil
	}
	cur.TraceJSON, _ = json.Marshal(steps)
	s.rec("SaveTrace:%d:%d", t.ID, len(steps))
	return nil
}

// ---------------------------------------------------------------------------
// fake Executor：可编程；像真实宿主一样，每次 Submit / Query 往 ctx 里的追踪追加一步
// ---------------------------------------------------------------------------

type wkExec struct {
	mu       sync.Mutex
	submitFn func(n int, in provider.SubmitInput) (*provider.SubmitResult, error)
	queryFn  func(n int, ref provider.TaskRef) (*provider.QueryResult, error)
	dlFn     func(n int, url string) (*provider.Download, error)
	cancelFn func(ref provider.TaskRef) error

	submits, queries, downloads int
	submitLimits                []time.Duration // 每次 Submit 收到的 ctx 还剩多久到期，没有时限记 0
	submitIns                   []provider.SubmitInput
	queryRefs                   []provider.TaskRef
	cancels                     []provider.TaskRef
}

func (e *wkExec) Submit(ctx context.Context, snap *provider.Snapshot, in provider.SubmitInput) (*provider.SubmitResult, error) {
	e.mu.Lock()
	e.submits++
	n := e.submits
	e.submitIns = append(e.submitIns, in)
	var limit time.Duration
	if dl, ok := ctx.Deadline(); ok {
		limit = time.Until(dl)
	}
	e.submitLimits = append(e.submitLimits, limit)
	fn := e.submitFn
	e.mu.Unlock()
	provider.TraceFrom(ctx).Add(provider.TraceStep{Name: fmt.Sprintf("submit:%d", n), Kind: "hook"})
	if fn == nil {
		return &provider.SubmitResult{ProviderTaskID: fmt.Sprintf("pt-%d", in.Task.ID)}, nil
	}
	return fn(n, in)
}

func (e *wkExec) Query(ctx context.Context, snap *provider.Snapshot, ref provider.TaskRef) (*provider.QueryResult, error) {
	e.mu.Lock()
	e.queries++
	n := e.queries
	e.queryRefs = append(e.queryRefs, ref)
	fn := e.queryFn
	e.mu.Unlock()
	provider.TraceFrom(ctx).Add(provider.TraceStep{Name: fmt.Sprintf("query:%d", n), Kind: "hook"})
	if fn == nil {
		return &provider.QueryResult{Status: provider.StatusRunning}, nil
	}
	return fn(n, ref)
}

func (e *wkExec) Cancel(ctx context.Context, snap *provider.Snapshot, ref provider.TaskRef) error {
	e.mu.Lock()
	e.cancels = append(e.cancels, ref)
	fn := e.cancelFn
	e.mu.Unlock()
	if fn != nil {
		return fn(ref)
	}
	return nil
}

func (e *wkExec) Download(ctx context.Context, snap *provider.Snapshot, url string) (*provider.Download, error) {
	e.mu.Lock()
	e.downloads++
	n := e.downloads
	fn := e.dlFn
	e.mu.Unlock()
	if fn != nil {
		return fn(n, url)
	}
	return &provider.Download{Body: io.NopCloser(strings.NewReader("video-bytes:" + url)), ContentType: "video/mp4", Size: 10}, nil
}

func (e *wkExec) counts() (submits, queries, downloads int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.submits, e.queries, e.downloads
}

func (e *wkExec) queryRefList() []provider.TaskRef {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]provider.TaskRef(nil), e.queryRefs...)
}

func (e *wkExec) cancelList() []provider.TaskRef {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]provider.TaskRef(nil), e.cancels...)
}

// ---------------------------------------------------------------------------
// fake AssetSaver
// ---------------------------------------------------------------------------

type wkSaver struct {
	mu     sync.Mutex
	saves  []provider.SaveGeneratedInput
	bodies []string
	errFn  func(n int) error
	nextID uint64
}

func (s *wkSaver) SaveGenerated(ctx context.Context, in provider.SaveGeneratedInput) (*model.Asset, string, error) {
	body, _ := io.ReadAll(in.Body)
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.saves) + 1
	s.saves = append(s.saves, in)
	if s.errFn != nil {
		if err := s.errFn(n); err != nil {
			return nil, "", err
		}
	}
	s.bodies = append(s.bodies, string(body))
	s.nextID++
	id := 1000 + s.nextID
	return &model.Asset{ID: id, UserID: in.UserID, Kind: in.Kind, Width: 1280, Height: 720, DurationMs: 5000},
		fmt.Sprintf("https://cdn.example.com/%d", id), nil
}

func (s *wkSaver) saveCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.saves)
}

// ---------------------------------------------------------------------------
// 日志捕获：worker 通过 Options.Logger 输出 JSON 行，测试按级别与文案断言告警
// ---------------------------------------------------------------------------

type wkLogs struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *wkLogs) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *wkLogs) Sync() error { return nil }

func (l *wkLogs) logger() *zap.Logger {
	enc := zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig())
	return zap.New(zapcore.NewCore(enc, l, zapcore.DebugLevel))
}

// entries 返回级别为 level、message 含 msg 的日志行（解码成 map）。
func (l *wkLogs) entries(level, msg string) []map[string]any {
	l.mu.Lock()
	data := append([]byte(nil), l.buf.Bytes()...)
	l.mu.Unlock()
	var out []map[string]any
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for sc.Scan() {
		var m map[string]any
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			continue
		}
		if m["level"] == level && strings.Contains(fmt.Sprint(m["msg"]), msg) {
			out = append(out, m)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// 测试环境
// ---------------------------------------------------------------------------

type wkEnv struct {
	clock *wkClock
	store *wkStore
	exec  *wkExec
	saver *wkSaver
	logs  *wkLogs
	w     *Worker
}

func newWkEnv(mut func(o *Options)) *wkEnv {
	clock := newWkClock()
	env := &wkEnv{
		clock: clock, store: newWkStore(clock), exec: &wkExec{}, saver: &wkSaver{}, logs: &wkLogs{},
	}
	opts := Options{
		Now:              clock.Now,
		Rand:             func() float64 { return 0.5 }, // 抖动为 0，时间可精确断言
		MaxDownloadBytes: 1 << 20,
		Logger:           env.logs.logger(),
	}
	if mut != nil {
		mut(&opts)
	}
	env.w = New(env.store, env.exec, env.saver, opts)
	return env
}

// newWorker 在同一个 store / exec / saver 上建一个全新的 worker，模拟进程重启（内存进度丢失）。
func (e *wkEnv) newWorker() *Worker {
	return New(e.store, e.exec, e.saver, Options{Now: e.clock.Now, Rand: func() float64 { return 0.5 }, Logger: e.logs.logger()})
}

// defaultPoll 是测试快照里插件声明的轮询节奏，与宿主默认值相同。
var defaultPoll = &pluginmeta.PollMeta{FirstDelay: 10, Interval: 5, MaxInterval: 15, Jitter: 0.2}

// wkSnapshotJSON 构造一个渠道 ch1 + 插件 mock@1.0.0 的快照。
func wkSnapshotJSON(kind string, poll *pluginmeta.PollMeta) []byte {
	snap := provider.Snapshot{
		Model:   provider.ModelSnapshot{Key: "m1", Kind: kind, UpstreamModel: "up-1"},
		Channel: provider.ChannelSnapshot{Key: "ch1", PluginKey: "mock", PluginVersionID: 3, BaseURL: "https://api.example.com"},
		Plugin: provider.PluginSnapshot{
			Key: "mock", Version: "1.0.0", SHA256: "sha-abc",
			Meta: pluginmeta.Meta{APIVersion: 1, Key: "mock", Name: "Mock", Version: "1.0.0", Poll: poll},
		},
		ModelRevisionID: 9,
	}
	b, _ := json.Marshal(snap)
	return b
}

// task 构造一个已到期的视频任务，deadline 在当前时钟之后 30 分钟；非 pending 状态带上游任务 id。
func (e *wkEnv) task(id uint64, status string, mut func(t *model.GenerationTask)) *model.GenerationTask {
	t := model.GenerationTask{
		ID: id, UserID: 7, Kind: model.KindVideo, ModelKey: "m1", Provider: "ch1", Status: status, Credits: 10, Version: 1,
		InputJSON: []byte(`{"prompt":"猫","image":5}`), ConfigSnapshot: wkSnapshotJSON(model.KindVideo, defaultPoll),
		NextPollAt: e.clock.Now(), DeadlineAt: e.clock.Now().Add(30 * time.Minute),
	}
	if status != model.TaskPending {
		t.ProviderTaskID = fmt.Sprintf("pt-%d", id)
	}
	if mut != nil {
		mut(&t)
	}
	return e.store.add(t)
}

// runOnce 模拟队列取一轮任务：按 id 升序处理所有到期（next_poll_at <= 当前时钟）的非终态任务，每个任务调用一次 Handle，返回处理的数量。
// 真实的队列在 provider/queue 里，并发、名额、睡到点都由它负责；这里只验证 Handle 这一步的行为。
func (e *wkEnv) runOnce(t *testing.T) int {
	t.Helper()
	return e.runWith(e.w)
}

// runWith 与 runOnce 相同，但用指定的 worker（模拟进程重启后换了一个新的 worker）。
func (e *wkEnv) runWith(w *Worker) int {
	now := e.clock.Now()
	e.store.mu.Lock()
	ids := make([]uint64, 0, len(e.store.tasks))
	for id, tk := range e.store.tasks {
		if !model.IsTerminalStatus(tk.Status) && !tk.NextPollAt.After(now) {
			ids = append(ids, id)
		}
	}
	e.store.mu.Unlock()
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		tk, _ := e.store.GetTask(context.Background(), id)
		w.Handle(context.Background(), *tk)
	}
	return len(ids)
}

// succeededResult 是带若干 url 产物的成功结果。
func succeededResult(urls ...string) *provider.QueryResult {
	res := &provider.QueryResult{Status: provider.StatusSucceeded}
	for _, u := range urls {
		res.Outputs = append(res.Outputs, provider.Output{Type: provider.OutputURL, URL: u, Mime: "video/mp4"})
	}
	return res
}

// outputsJSON 序列化产物列表，用来构造 provider_result。
func outputsJSON(outs ...provider.Output) []byte {
	b, _ := json.Marshal(outs)
	return b
}

func intPtr(n int) *int { return &n }

// waitFor 等待条件成立（轮询 + 超时），不用固定 sleep。
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("等待超时：%s", what)
}

// runnerErr 构造宿主返回的 runner 相关错误。
func runnerErr(code string) error {
	return &provider.Error{Class: provider.ClassRetryable, Code: code, Message: "runner: " + code, PluginFault: code == provider.CodeRunnerCrashed}
}
