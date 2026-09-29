package worker_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	. "video-canvas/internal/provider/worker"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/dsl"
	"video-canvas/internal/model"
)

// ---------------------------------------------------------------------------
// fake 依赖
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

type wkFailCall struct {
	ID            uint64
	Code, Message string
}

// wkStore 是内存版 Store：复刻 service 里 CAS 迁移与租约的语义，供驱动完整状态流转。
type wkStore struct {
	mu    sync.Mutex
	clock *wkClock
	tasks map[uint64]*model.GenerationTask

	calls       []string // 调用流水，如 "MarkSubmitted:1"
	fails       []wkFailCall
	completed   map[uint64][]model.TaskOutput
	claimLimits []int
	extendCalls atomic.Int32
	claimErr    error
	// onMarkSubmitted 在 MarkSubmitted 内部触发（测试用来模拟“提交期间被取消”）
	beforeMarkSubmitted func(id uint64)
}

func newWkStore(clock *wkClock) *wkStore {
	return &wkStore{clock: clock, tasks: map[uint64]*model.GenerationTask{}, completed: map[uint64][]model.TaskOutput{}}
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

func (s *wkStore) rec(format string, args ...any) {
	s.calls = append(s.calls, fmt.Sprintf(format, args...))
}

func (s *wkStore) ClaimDue(ctx context.Context, limit int, lease time.Duration) ([]model.GenerationTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.claimLimits = append(s.claimLimits, limit)
	if s.claimErr != nil {
		return nil, s.claimErr
	}
	now := s.clock.Now()
	ids := make([]uint64, 0, len(s.tasks))
	for id := range s.tasks {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var out []model.GenerationTask
	for _, id := range ids {
		t := s.tasks[id]
		if len(out) >= limit || model.IsTerminalStatus(t.Status) || t.NextPollAt.After(now) {
			continue
		}
		if t.LeaseUntil != nil && !t.LeaseUntil.Before(now) {
			continue
		}
		until := now.Add(lease)
		t.LeaseUntil = &until
		out = append(out, *t)
	}
	return out, nil
}

func (s *wkStore) ExtendLease(ctx context.Context, id uint64, lease time.Duration) (bool, error) {
	s.extendCalls.Add(1)
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.tasks[id]
	if t == nil || model.IsTerminalStatus(t.Status) {
		return false, nil
	}
	until := s.clock.Now().Add(lease)
	t.LeaseUntil = &until
	return true, nil
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

func (s *wkStore) MarkSubmitted(ctx context.Context, t *model.GenerationTask, pid string, next time.Time) (bool, error) {
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
	s.rec("MarkSubmitted:%d", t.ID)
	return true, nil
}

func (s *wkStore) MarkPolled(ctx context.Context, t *model.GenerationTask, status string, progress *int, attempts int, next time.Time) (bool, error) {
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

func (s *wkStore) Complete(ctx context.Context, t *model.GenerationTask, outs []model.TaskOutput) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.cas(t.ID, model.TaskFinalizing)
	if cur == nil {
		s.rec("Complete:%d:conflict", t.ID)
		return false, nil
	}
	cur.Status, cur.LeaseUntil = model.TaskSucceeded, nil
	s.completed[t.ID] = outs
	s.rec("Complete:%d", t.ID)
	return true, nil
}

func (s *wkStore) Fail(ctx context.Context, t *model.GenerationTask, code, msg string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.cas(t.ID, model.ActiveTaskStatuses...)
	if cur == nil {
		return false, nil
	}
	cur.Status, cur.ErrorCode, cur.ErrorMessage, cur.LeaseUntil = model.TaskFailed, code, msg, nil
	s.fails = append(s.fails, wkFailCall{t.ID, code, msg})
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

// wkExec 是可编程的 Executor。
type wkExec struct {
	mu       sync.Mutex
	submitFn func(n int, in provider.SubmitInput) (string, error)
	queryFn  func(n int, ref provider.TaskRef) (*provider.QueryResult, error)
	dlFn     func(n int, url string) (*provider.Download, error)
	cancelFn func(ref provider.TaskRef) error

	submits, queries, downloads int
	submitIns                   []provider.SubmitInput
	cancels                     []provider.TaskRef
}

func (e *wkExec) Submit(ctx context.Context, snap *dsl.Snapshot, in provider.SubmitInput) (string, error) {
	e.mu.Lock()
	e.submits++
	n := e.submits
	e.submitIns = append(e.submitIns, in)
	fn := e.submitFn
	e.mu.Unlock()
	if fn == nil {
		return fmt.Sprintf("pt-%d", in.Task.ID), nil
	}
	return fn(n, in)
}

func (e *wkExec) Query(ctx context.Context, snap *dsl.Snapshot, ref provider.TaskRef) (*provider.QueryResult, error) {
	e.mu.Lock()
	e.queries++
	n := e.queries
	fn := e.queryFn
	e.mu.Unlock()
	if fn == nil {
		return &provider.QueryResult{Status: provider.StatusRunning}, nil
	}
	return fn(n, ref)
}

func (e *wkExec) Cancel(ctx context.Context, snap *dsl.Snapshot, ref provider.TaskRef) error {
	e.mu.Lock()
	e.cancels = append(e.cancels, ref)
	fn := e.cancelFn
	e.mu.Unlock()
	if fn != nil {
		return fn(ref)
	}
	return nil
}

func (e *wkExec) Download(ctx context.Context, snap *dsl.Snapshot, url string) (*provider.Download, error) {
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
	if s.errFn != nil {
		if err := s.errFn(n); err != nil {
			s.saves = append(s.saves, in)
			return nil, "", err
		}
	}
	s.saves = append(s.saves, in)
	s.bodies = append(s.bodies, string(body))
	s.nextID++
	id := 1000 + s.nextID
	return &model.Asset{ID: id, UserID: in.UserID, Kind: in.Kind, Width: 1280, Height: 720, DurationMs: 5000},
		fmt.Sprintf("https://cdn.example.com/%d", id), nil
}

// ---------------------------------------------------------------------------
// 测试辅助
// ---------------------------------------------------------------------------

type wkEnv struct {
	clock *wkClock
	store *wkStore
	exec  *wkExec
	saver *wkSaver
	w     *Worker
	kick  chan struct{}
}

func newWkEnv(mut func(o *Options)) *wkEnv {
	clock := newWkClock()
	env := &wkEnv{clock: clock, store: newWkStore(clock), exec: &wkExec{}, saver: &wkSaver{}, kick: make(chan struct{}, 1)}
	opts := Options{
		Now:              clock.Now,
		Rand:             func() float64 { return 0.5 }, // 抖动为 0，时间可精确断言
		MaxDownloadBytes: 1 << 20,
		WebhookURL:       func(key string) string { return "https://api.example.com/hook/" + key },
	}
	if mut != nil {
		mut(&opts)
	}
	env.w = New(env.store, env.exec, env.saver, env.kick, opts)
	return env
}

func wkSnapshotJSON() []byte {
	snap := dsl.Snapshot{
		Provider: dsl.ProviderConfig{
			Key: "p1",
			Poll: dsl.PollConfig{
				FirstDelay: dsl.Duration(10 * time.Second), Interval: dsl.Duration(5 * time.Second),
				MaxInterval: dsl.Duration(15 * time.Second), Jitter: 0.2,
			},
		},
		Model: dsl.ModelConfig{Key: "m1", Kind: model.KindVideo, Output: dsl.OutputConfig{Media: model.KindVideo}},
	}
	b, _ := json.Marshal(snap)
	return b
}

// wkTask 构造一个已到期的任务，deadline 在当前时钟之后 30 分钟。
func (e *wkEnv) task(id uint64, status string, mut func(t *model.GenerationTask)) *model.GenerationTask {
	t := model.GenerationTask{
		ID: id, UserID: 7, Kind: model.KindVideo, ModelKey: "m1", Provider: "p1", Status: status, Credits: 10, Version: 1,
		InputJSON: []byte(`{"prompt":"猫","image":5}`), ConfigSnapshot: wkSnapshotJSON(),
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

func (e *wkEnv) runOnce(t *testing.T) int {
	t.Helper()
	n, err := e.w.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce 失败：%v", err)
	}
	return n
}

func succeededResult(urls ...string) *provider.QueryResult {
	res := &provider.QueryResult{Status: provider.StatusSucceeded}
	for i, u := range urls {
		res.Outputs = append(res.Outputs, provider.Output{URL: u, Type: "mp4", Node: fmt.Sprint(i)})
	}
	return res
}

func intPtr(n int) *int { return &n }

// ---------------------------------------------------------------------------
// 完整状态流转
// ---------------------------------------------------------------------------

func TestWorker_FullFlow_SubmitPollFinalizeSucceed(t *testing.T) {
	env := newWkEnv(nil)
	env.task(1, model.TaskPending, nil)
	env.exec.queryFn = func(n int, ref provider.TaskRef) (*provider.QueryResult, error) {
		switch n {
		case 1:
			return &provider.QueryResult{Status: provider.StatusQueued}, nil
		case 2:
			return &provider.QueryResult{Status: provider.StatusRunning, Progress: intPtr(40)}, nil
		case 3:
			return &provider.QueryResult{Status: provider.StatusRunning, Progress: intPtr(80)}, nil
		default: // 第 4 次是平台出片，第 5 次（finalizing 里重新取产物地址）也返回产物
			return succeededResult("https://p.example.com/a.mp4", "https://p.example.com/cover.mp4"), nil
		}
	}

	// 1. pending → 提交 → queued，第一次查询安排在 +10s，回调地址已传给平台
	if n := env.runOnce(t); n != 1 {
		t.Fatalf("应领取 1 个任务：%d", n)
	}
	got := env.store.get(1)
	if got.Status != model.TaskQueued || got.ProviderTaskID != "pt-1" || !got.NextPollAt.Equal(env.clock.Now().Add(10*time.Second)) {
		t.Fatalf("提交后状态不对：%+v", got)
	}
	in := env.exec.submitIns[0]
	if in.WebhookURL != "https://api.example.com/hook/p1" || in.Task.ID != 1 || in.Task.UserID != 7 || in.Input["prompt"] != "猫" {
		t.Fatalf("提交参数不对：%+v", in)
	}

	// 2. 没到点不会被领取
	if n := env.runOnce(t); n != 0 {
		t.Fatalf("未到期不应领取：%d", n)
	}

	// 3. +10s：查询到 queued，下一次 +5s（attempts=1）
	env.clock.Advance(10 * time.Second)
	env.runOnce(t)
	got = env.store.get(1)
	if got.Status != model.TaskQueued || got.PollAttempts != 1 || !got.NextPollAt.Equal(env.clock.Now().Add(5*time.Second)) {
		t.Fatalf("第一次查询后状态不对：%+v", got)
	}

	// 4. +5s：running 40%，下一次 +10s（间隔翻倍）
	env.clock.Advance(5 * time.Second)
	env.runOnce(t)
	got = env.store.get(1)
	if got.Status != model.TaskRunning || got.Progress == nil || *got.Progress != 40 || !got.NextPollAt.Equal(env.clock.Now().Add(10*time.Second)) {
		t.Fatalf("第二次查询后状态不对：%+v", got)
	}

	// 5. +10s：running 80%，下一次 +15s（封顶 max_interval）
	env.clock.Advance(10 * time.Second)
	env.runOnce(t)
	got = env.store.get(1)
	if *got.Progress != 80 || !got.NextPollAt.Equal(env.clock.Now().Add(15*time.Second)) {
		t.Fatalf("第三次查询后状态不对：%+v", got)
	}

	// 6. +15s：平台出片 → finalizing，立即到期
	env.clock.Advance(15 * time.Second)
	env.runOnce(t)
	got = env.store.get(1)
	if got.Status != model.TaskFinalizing || !got.NextPollAt.Equal(env.clock.Now()) {
		t.Fatalf("应进入 finalizing 且立即可处理：%+v", got)
	}

	// 7. finalizing：转存 2 个产物，一次性 Complete
	env.runOnce(t)
	got = env.store.get(1)
	if got.Status != model.TaskSucceeded {
		t.Fatalf("应成功：%+v", got)
	}
	outs := env.store.completed[1]
	if len(outs) != 2 || outs[0].AssetID != 1001 || outs[1].AssetID != 1002 || outs[0].MediaType != model.KindVideo ||
		outs[0].URL != "https://cdn.example.com/1001" || outs[0].Width != 1280 || outs[0].DurationMs != 5000 {
		t.Fatalf("转存产物不对：%+v", outs)
	}
	if len(env.saver.saves) != 2 {
		t.Fatalf("应保存 2 次：%d", len(env.saver.saves))
	}
	s0 := env.saver.saves[0]
	if s0.UserID != 7 || s0.TaskID != 1 || s0.Kind != model.KindVideo || s0.MimeType != "video/mp4" || s0.MaxBytes != 1<<20 || s0.FileName != "task-1-1.mp4" {
		t.Fatalf("转存参数不对：%+v", s0)
	}
	if env.saver.bodies[0] != "video-bytes:https://p.example.com/a.mp4" {
		t.Fatalf("应保存下载到的内容：%q", env.saver.bodies[0])
	}
	if len(env.store.fails) != 0 {
		t.Fatalf("不应有失败：%+v", env.store.fails)
	}
	// 终态之后不会再被领取
	env.clock.Advance(time.Hour)
	if n := env.runOnce(t); n != 0 {
		t.Fatalf("终态任务不应再被领取：%d", n)
	}
}

// ---------------------------------------------------------------------------
// 提交阶段
// ---------------------------------------------------------------------------

func TestWorker_Submit_Failures(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		wantCode     string
		wantMsg      string
		wantNotInMsg string
	}{
		{"审核未通过", &provider.Error{Class: provider.ClassModeration, Message: "raw: nsfw detected id=abc"}, "moderation", "内容未通过审核", "nsfw"},
		{"平台余额不足对用户显示服务繁忙", &provider.Error{Class: provider.ClassProviderBalance, Message: "raw: insufficient balance 0.00"}, "provider_balance", "服务繁忙，请稍后再试", "balance"},
		{"提交结果未知", &provider.Error{Class: provider.ClassSubmitUnknown, Message: "raw: read timeout after 30s"}, "submit_unknown", "提交结果未知，积分已退回", "timeout after"},
		{"终态错误", &provider.Error{Class: provider.ClassTerminal, Message: "raw: node 12 missing"}, "provider_error", "平台繁忙，请稍后重试", "node 12"},
		{"参数错误", &provider.Error{Class: provider.ClassTerminal, Code: "invalid_param", Message: "raw: bad"}, "invalid_param", "参数不合法，请调整后重试", "raw"},
		{"未分类的普通错误按终态处理", errors.New("raw: boom secret-detail"), "provider_error", "平台繁忙，请稍后重试", "secret-detail"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newWkEnv(nil)
			env.task(1, model.TaskPending, nil)
			env.exec.submitFn = func(int, provider.SubmitInput) (string, error) { return "", tt.err }
			env.runOnce(t)

			got := env.store.get(1)
			if got.Status != model.TaskFailed || got.ErrorCode != tt.wantCode || got.ErrorMessage != tt.wantMsg {
				t.Fatalf("期望 failed/%s/%s，实际 %+v", tt.wantCode, tt.wantMsg, got)
			}
			if strings.Contains(got.ErrorMessage, tt.wantNotInMsg) {
				t.Fatalf("不能把平台原始信息透给用户：%s", got.ErrorMessage)
			}
			// 失败后不会再被处理
			env.clock.Advance(time.Hour)
			if n := env.runOnce(t); n != 0 {
				t.Fatalf("失败任务不应再被领取：%d", n)
			}
			if s, _, _ := env.exec.counts(); s != 1 {
				t.Fatalf("不应重试提交：%d 次", s)
			}
		})
	}

	t.Run("平台返回空任务 id 视为失败", func(t *testing.T) {
		env := newWkEnv(nil)
		env.task(1, model.TaskPending, nil)
		env.exec.submitFn = func(int, provider.SubmitInput) (string, error) { return "", nil }
		env.runOnce(t)
		if got := env.store.get(1); got.Status != model.TaskFailed || got.ErrorCode != "provider_error" {
			t.Fatalf("应失败：%+v", got)
		}
	})

	t.Run("输入损坏直接失败退款", func(t *testing.T) {
		env := newWkEnv(nil)
		env.task(1, model.TaskPending, func(t *model.GenerationTask) { t.InputJSON = []byte(`not json`) })
		env.runOnce(t)
		if got := env.store.get(1); got.Status != model.TaskFailed || got.ErrorCode != "invalid_param" {
			t.Fatalf("应失败：%+v", got)
		}
		if s, _, _ := env.exec.counts(); s != 0 {
			t.Fatal("不应调用平台")
		}
	})

	t.Run("快照损坏直接失败退款", func(t *testing.T) {
		env := newWkEnv(nil)
		env.task(1, model.TaskPending, func(t *model.GenerationTask) { t.ConfigSnapshot = []byte(`{`) })
		env.runOnce(t)
		if got := env.store.get(1); got.Status != model.TaskFailed {
			t.Fatalf("应失败：%+v", got)
		}
	})
}

func TestWorker_Submit_RetryableBackoff(t *testing.T) {
	env := newWkEnv(func(o *Options) { o.MaxSubmitRetries = 3 })
	env.task(1, model.TaskPending, nil)
	env.exec.submitFn = func(int, provider.SubmitInput) (string, error) {
		return "", &provider.Error{Class: provider.ClassRetryable, Message: "connection refused"}
	}

	// 前 3 次失败：指数退避 2s / 4s / 8s，任务保持 pending，不失败、不推送 version
	wantDelays := []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second}
	for i, d := range wantDelays {
		env.runOnce(t)
		got := env.store.get(1)
		if got.Status != model.TaskPending || got.PollAttempts != i+1 || !got.NextPollAt.Equal(env.clock.Now().Add(d)) {
			t.Fatalf("第 %d 次失败后状态不对：%+v（期望退避 %v）", i+1, got, d)
		}
		// 没到点不会重试
		if n := env.runOnce(t); n != 0 {
			t.Fatalf("退避期间不应领取：%d", n)
		}
		env.clock.Advance(d)
	}

	// 第 4 次失败：超过最多重试次数，直接失败退款
	env.runOnce(t)
	got := env.store.get(1)
	if got.Status != model.TaskFailed || got.ErrorCode != "provider_error" {
		t.Fatalf("重试耗尽后应失败：%+v", got)
	}
	if s, _, _ := env.exec.counts(); s != 4 {
		t.Fatalf("应提交 4 次（1 次 + 3 次重试）：%d", s)
	}
}

func TestWorker_Submit_RetryThenSuccess(t *testing.T) {
	env := newWkEnv(nil)
	env.task(1, model.TaskPending, nil)
	env.exec.submitFn = func(n int, in provider.SubmitInput) (string, error) {
		if n == 1 {
			return "", &provider.Error{Class: provider.ClassRetryable, Message: "429"}
		}
		return "pt-ok", nil
	}
	env.runOnce(t)
	env.clock.Advance(2 * time.Second)
	env.runOnce(t)
	got := env.store.get(1)
	if got.Status != model.TaskQueued || got.ProviderTaskID != "pt-ok" || got.PollAttempts != 0 {
		t.Fatalf("重试成功后应进入 queued 且计数清零：%+v", got)
	}
}

func TestWorker_Submit_CanceledDuringSubmitCancelsProviderTask(t *testing.T) {
	env := newWkEnv(nil)
	env.task(1, model.TaskPending, nil)
	// 提交返回后、写库之前，用户把任务取消了
	env.store.beforeMarkSubmitted = func(id uint64) {
		env.store.mu.Lock()
		env.store.tasks[id].Status = model.TaskCanceled
		env.store.mu.Unlock()
	}
	env.runOnce(t)

	if got := env.store.get(1); got.Status != model.TaskCanceled || got.ProviderTaskID != "" {
		t.Fatalf("任务应保持取消状态：%+v", got)
	}
	if len(env.exec.cancels) != 1 || env.exec.cancels[0].ProviderTaskID != "pt-1" {
		t.Fatalf("应尽力取消刚创建的平台任务：%+v", env.exec.cancels)
	}
}

// ---------------------------------------------------------------------------
// 轮询阶段
// ---------------------------------------------------------------------------

func TestWorker_Poll(t *testing.T) {
	tests := []struct {
		name       string
		task       func(t *model.GenerationTask)
		query      func(n int, ref provider.TaskRef) (*provider.QueryResult, error)
		wantStatus string
		wantCode   string
		wantMsg    string
		wantNext   time.Duration // 相对当前时钟
		wantTry    int
	}{
		{
			name: "平台失败：按 error_rules 的分类失败（审核）",
			query: func(int, provider.TaskRef) (*provider.QueryResult, error) {
				return &provider.QueryResult{Status: provider.StatusFailed, ErrorClass: provider.ClassModeration, ErrorCode: "805", ErrorMessage: "nsfw"}, nil
			},
			wantStatus: model.TaskFailed, wantCode: "moderation", wantMsg: "内容未通过审核",
		},
		{
			name: "平台失败但没给分类按终态处理",
			query: func(int, provider.TaskRef) (*provider.QueryResult, error) {
				return &provider.QueryResult{Status: provider.StatusFailed, ErrorMessage: "boom"}, nil
			},
			wantStatus: model.TaskFailed, wantCode: "provider_error", wantMsg: "平台繁忙，请稍后重试",
		},
		{
			name: "平台余额不足失败",
			query: func(int, provider.TaskRef) (*provider.QueryResult, error) {
				return &provider.QueryResult{Status: provider.StatusFailed, ErrorClass: provider.ClassProviderBalance}, nil
			},
			wantStatus: model.TaskFailed, wantCode: "provider_balance", wantMsg: "服务繁忙，请稍后再试",
		},
		{
			name: "查询可重试错误：按轮询节奏退避，状态不变",
			task: func(t *model.GenerationTask) { t.PollAttempts = 1 },
			query: func(int, provider.TaskRef) (*provider.QueryResult, error) {
				return nil, &provider.Error{Class: provider.ClassRetryable, Message: "502"}
			},
			wantStatus: model.TaskRunning, wantNext: 10 * time.Second, wantTry: 2,
		},
		{
			name: "查询终态错误：直接失败",
			query: func(int, provider.TaskRef) (*provider.QueryResult, error) {
				return nil, &provider.Error{Class: provider.ClassTerminal, Message: "task not found"}
			},
			wantStatus: model.TaskFailed, wantCode: "provider_error", wantMsg: "平台繁忙，请稍后重试",
		},
		{
			name:       "未知统一状态按 running 处理",
			query:      func(int, provider.TaskRef) (*provider.QueryResult, error) { return &provider.QueryResult{Status: "weird"}, nil },
			wantStatus: model.TaskRunning, wantNext: 5 * time.Second, wantTry: 1,
		},
		{
			name:       "缺少平台任务 id 直接失败",
			task:       func(t *model.GenerationTask) { t.ProviderTaskID = "" },
			wantStatus: model.TaskFailed, wantCode: "provider_error", wantMsg: "平台繁忙，请稍后重试",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newWkEnv(nil)
			env.task(1, model.TaskRunning, tt.task)
			env.exec.queryFn = tt.query
			env.runOnce(t)

			got := env.store.get(1)
			if got.Status != tt.wantStatus || got.ErrorCode != tt.wantCode || got.ErrorMessage != tt.wantMsg {
				t.Fatalf("期望 %s/%s/%s，实际 %+v", tt.wantStatus, tt.wantCode, tt.wantMsg, got)
			}
			if tt.wantNext > 0 && (!got.NextPollAt.Equal(env.clock.Now().Add(tt.wantNext)) || got.PollAttempts != tt.wantTry) {
				t.Fatalf("下次查询时间 / 次数不对：next=%v attempts=%d，期望 +%v / %d", got.NextPollAt.Sub(env.clock.Now()), got.PollAttempts, tt.wantNext, tt.wantTry)
			}
		})
	}
}

func TestWorker_Poll_TaskCanceledMeanwhileIsDropped(t *testing.T) {
	env := newWkEnv(nil)
	env.task(1, model.TaskRunning, nil)
	env.exec.queryFn = func(int, provider.TaskRef) (*provider.QueryResult, error) {
		// 查询期间用户取消了任务
		env.store.mu.Lock()
		env.store.tasks[1].Status = model.TaskCanceled
		env.store.mu.Unlock()
		return &provider.QueryResult{Status: provider.StatusRunning, Progress: intPtr(50)}, nil
	}
	env.runOnce(t)
	if got := env.store.get(1); got.Status != model.TaskCanceled || got.Progress != nil {
		t.Fatalf("已取消的任务不应被覆盖：%+v", got)
	}
	if env.store.count("MarkPolled:1:conflict") != 1 {
		t.Fatalf("应遇到 CAS 冲突并放弃：%v", env.store.callList())
	}
}

func TestPollDelay(t *testing.T) {
	half := func() float64 { return 0.5 }
	cfg := dsl.PollConfig{Interval: dsl.Duration(5 * time.Second), MaxInterval: dsl.Duration(15 * time.Second), Jitter: 0.2}
	tests := []struct {
		name     string
		cfg      dsl.PollConfig
		attempts int
		rnd      func() float64
		want     time.Duration
	}{
		{"第 1 次从 interval 开始", cfg, 1, half, 5 * time.Second},
		{"第 2 次翻倍", cfg, 2, half, 10 * time.Second},
		{"第 3 次封顶 max_interval", cfg, 3, half, 15 * time.Second},
		{"很大的次数仍封顶且不溢出", cfg, 200, half, 15 * time.Second},
		{"缺省 5s → 15s", dsl.PollConfig{}, 2, half, 10 * time.Second},
		{"抖动下限 -20%", cfg, 1, func() float64 { return 0 }, 4 * time.Second},
		{"抖动上限 +20%", cfg, 1, func() float64 { return 0.9999999 }, 6 * time.Second},
		{"max_interval 小于 interval 时以 interval 为准", dsl.PollConfig{Interval: dsl.Duration(20 * time.Second), MaxInterval: dsl.Duration(5 * time.Second)}, 3, half, 20 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PollDelay(tt.cfg, tt.attempts, tt.rnd)
			diff := got - tt.want
			if diff < -time.Millisecond || diff > time.Millisecond {
				t.Fatalf("got=%v want=%v", got, tt.want)
			}
		})
	}
}

func TestBackoffAndFailureMapping(t *testing.T) {
	if SubmitBackoff(1) != 2*time.Second || SubmitBackoff(4) != 16*time.Second || SubmitBackoff(20) != 60*time.Second {
		t.Fatalf("提交退避不对：%v %v %v", SubmitBackoff(1), SubmitBackoff(4), SubmitBackoff(20))
	}
	if TransferBackoff(1) != 10*time.Second || TransferBackoff(3) != 40*time.Second || TransferBackoff(50) != 10*time.Minute {
		t.Fatalf("转存退避不对")
	}
	if FirstDelay(dsl.PollConfig{}) != 10*time.Second || FirstDelay(dsl.PollConfig{FirstDelay: dsl.Duration(3 * time.Second)}) != 3*time.Second {
		t.Fatal("首次查询延迟不对")
	}
	// 转存总窗口：默认 10 次尝试，累计退避远小于平台 URL 的 24 小时有效期
	var total time.Duration
	for i := 1; i < 10; i++ {
		total += TransferBackoff(i)
	}
	if total >= time.Hour {
		t.Fatalf("转存总退避窗口过长：%v", total)
	}
	cases := map[provider.ErrorClass][2]string{
		provider.ClassModeration:       {"moderation", "内容未通过审核"},
		provider.ClassProviderBalance:  {"provider_balance", "服务繁忙，请稍后再试"},
		provider.ClassSubmitUnknown:    {"submit_unknown", "提交结果未知，积分已退回"},
		FailTimeout:              {"timeout", "生成超时，积分已退回"},
		FailTransfer:             {"transfer_failed", "生成结果保存失败，积分已退回"},
		provider.ClassTerminal:         {"provider_error", "平台繁忙，请稍后重试"},
		provider.ClassRetryable:        {"provider_error", "平台繁忙，请稍后重试"},
		provider.ErrorClass("unknown"): {"provider_error", "平台繁忙，请稍后重试"},
	}
	for class, want := range cases {
		if code, msg := FailureFor(class, ""); code != want[0] || msg != want[1] {
			t.Errorf("failureFor(%s) = %s/%s，期望 %v", class, code, msg, want)
		}
	}
	if code, _ := FailureFor(provider.ClassTerminal, "invalid_param"); code != "invalid_param" {
		t.Errorf("invalid_param 映射不对：%s", code)
	}
	for in, want := range map[string]string{"mp4": ".mp4", ".WEBM": ".webm", "": "", "a/b": "", "toolongtype": "", "m4a": ".m4a"} {
		if got := ExtOf(in); got != want {
			t.Errorf("extOf(%q)=%q 期望 %q", in, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// 转存阶段
// ---------------------------------------------------------------------------

func TestWorker_Finalize_Failures(t *testing.T) {
	tests := []struct {
		name       string
		task       func(t *model.GenerationTask)
		query      func(n int, ref provider.TaskRef) (*provider.QueryResult, error)
		wantStatus string
		wantCode   string
	}{
		{
			name: "取产物地址时平台报告失败",
			query: func(int, provider.TaskRef) (*provider.QueryResult, error) {
				return &provider.QueryResult{Status: provider.StatusFailed, ErrorClass: provider.ClassModeration}, nil
			},
			wantStatus: model.TaskFailed, wantCode: "moderation",
		},
		{
			name:       "平台成功但没有产物",
			query:      func(int, provider.TaskRef) (*provider.QueryResult, error) { return succeededResult(), nil },
			wantStatus: model.TaskFailed, wantCode: "provider_error",
		},
		{
			name:       "取产物地址时终态错误直接失败",
			query:      func(int, provider.TaskRef) (*provider.QueryResult, error) { return nil, &provider.Error{Class: provider.ClassTerminal} },
			wantStatus: model.TaskFailed, wantCode: "provider_error",
		},
		{
			name:       "取产物地址时可重试错误保持 finalizing",
			query:      func(int, provider.TaskRef) (*provider.QueryResult, error) { return nil, &provider.Error{Class: provider.ClassRetryable} },
			wantStatus: model.TaskFinalizing,
		},
		{
			name:       "平台状态回退（不是 succeeded）保持 finalizing 重试",
			query:      func(int, provider.TaskRef) (*provider.QueryResult, error) { return &provider.QueryResult{Status: provider.StatusRunning}, nil },
			wantStatus: model.TaskFinalizing,
		},
		{
			name: "超过 deadline + 转存窗口放弃",
			task: func(t *model.GenerationTask) { t.DeadlineAt = t.NextPollAt.Add(-3 * time.Hour) },
			query: func(int, provider.TaskRef) (*provider.QueryResult, error) {
				return succeededResult("https://p.example.com/a.mp4"), nil
			},
			wantStatus: model.TaskFailed, wantCode: "transfer_failed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newWkEnv(nil)
			env.task(1, model.TaskFinalizing, tt.task)
			env.exec.queryFn = tt.query
			env.runOnce(t)
			got := env.store.get(1)
			if got.Status != tt.wantStatus || got.ErrorCode != tt.wantCode {
				t.Fatalf("期望 %s/%s，实际 %+v", tt.wantStatus, tt.wantCode, got)
			}
		})
	}
}

func TestWorker_Finalize_NotBoundByDeadlineWithinGrace(t *testing.T) {
	env := newWkEnv(nil)
	// deadline 已过 10 分钟，但平台已出片，仍应完成转存而不是 expired
	env.task(1, model.TaskFinalizing, func(t *model.GenerationTask) { t.DeadlineAt = t.NextPollAt.Add(-10 * time.Minute) })
	env.exec.queryFn = func(int, provider.TaskRef) (*provider.QueryResult, error) {
		return succeededResult("https://p.example.com/a.mp4"), nil
	}
	env.runOnce(t)
	if got := env.store.get(1); got.Status != model.TaskSucceeded {
		t.Fatalf("finalizing 不应受普通 deadline 约束：%+v", got)
	}
}

func TestWorker_Finalize_RetryWithBackoffThenFailAndRefund(t *testing.T) {
	env := newWkEnv(func(o *Options) { o.TransferMaxAttempts = 3 })
	env.task(1, model.TaskFinalizing, nil)
	env.exec.queryFn = func(int, provider.TaskRef) (*provider.QueryResult, error) {
		return succeededResult("https://p.example.com/a.mp4"), nil
	}
	env.exec.dlFn = func(int, string) (*provider.Download, error) { return nil, errors.New("connection reset") }

	// 第 1、2 次失败：退避 10s / 20s，保持 finalizing
	for i, d := range []time.Duration{10 * time.Second, 20 * time.Second} {
		env.runOnce(t)
		got := env.store.get(1)
		if got.Status != model.TaskFinalizing || got.PollAttempts != i+1 || !got.NextPollAt.Equal(env.clock.Now().Add(d)) {
			t.Fatalf("第 %d 次转存失败后状态不对：%+v（期望退避 %v）", i+1, got, d)
		}
		env.clock.Advance(d)
	}
	// 第 3 次失败：次数耗尽，failed(transfer_failed) 并退款（由 Fail 完成）
	env.runOnce(t)
	got := env.store.get(1)
	if got.Status != model.TaskFailed || got.ErrorCode != "transfer_failed" || got.ErrorMessage != "生成结果保存失败，积分已退回" {
		t.Fatalf("耗尽后应失败：%+v", got)
	}
}

func TestWorker_Finalize_PartialProgressIsNotRepeated(t *testing.T) {
	env := newWkEnv(nil)
	env.task(1, model.TaskFinalizing, nil)
	env.exec.queryFn = func(int, provider.TaskRef) (*provider.QueryResult, error) {
		return succeededResult("https://p.example.com/0.mp4", "https://p.example.com/1.mp4", "https://p.example.com/2.mp4"), nil
	}
	// 第 2 个产物第一次保存失败
	failedOnce := false
	env.saver.errFn = func(n int) error {
		if n == 2 && !failedOnce {
			failedOnce = true
			return errors.New("磁盘写入失败")
		}
		return nil
	}

	env.runOnce(t) // 产物 0 成功，产物 1 失败 → 重试
	if got := env.store.get(1); got.Status != model.TaskFinalizing || got.PollAttempts != 1 {
		t.Fatalf("应保持 finalizing 等待重试：%+v", got)
	}
	env.clock.Advance(10 * time.Second)
	env.runOnce(t) // 重试：只处理产物 1、2

	got := env.store.get(1)
	if got.Status != model.TaskSucceeded || len(env.store.completed[1]) != 3 {
		t.Fatalf("应成功且有 3 个产物：%+v %+v", got, env.store.completed[1])
	}
	// 下载 / 保存总次数：产物 0 一次；产物 1 两次（一次失败一次成功）；产物 2 一次 = 4 次成功下载之外的 1 次失败保存
	_, _, downloads := env.exec.counts()
	if downloads != 4 {
		t.Fatalf("产物 0 不应被重复下载，期望共 4 次下载，实际 %d", downloads)
	}
	for _, o := range env.store.completed[1] {
		if o.AssetID == 0 {
			t.Fatalf("产物缺少素材 id：%+v", env.store.completed[1])
		}
	}
	if env.store.completed[1][0].AssetID != 1001 {
		t.Fatalf("产物 0 应沿用第一次转存的素材：%+v", env.store.completed[1])
	}
}

func TestWorker_Finalize_CompletedTaskConflictDropsSilently(t *testing.T) {
	env := newWkEnv(nil)
	env.task(1, model.TaskFinalizing, nil)
	env.exec.queryFn = func(int, provider.TaskRef) (*provider.QueryResult, error) {
		return succeededResult("https://p.example.com/a.mp4"), nil
	}
	env.exec.dlFn = func(n int, url string) (*provider.Download, error) {
		// 转存期间用户取消了任务
		env.store.mu.Lock()
		env.store.tasks[1].Status = model.TaskCanceled
		env.store.mu.Unlock()
		return &provider.Download{Body: io.NopCloser(bytes.NewReader([]byte("x"))), ContentType: "video/mp4"}, nil
	}
	env.runOnce(t)
	if got := env.store.get(1); got.Status != model.TaskCanceled {
		t.Fatalf("取消状态不应被覆盖：%+v", got)
	}
	if len(env.store.completed) != 0 {
		t.Fatal("不应写入产物")
	}
}

// ---------------------------------------------------------------------------
// 超时
// ---------------------------------------------------------------------------

func TestWorker_Expire(t *testing.T) {
	tests := []struct {
		name       string
		status     string
		wantCancel bool
		cancelErr  error
	}{
		{"pending 超时不通知平台（还没提交）", model.TaskPending, false, nil},
		{"queued 超时先取消平台任务再置 expired", model.TaskQueued, true, nil},
		{"running 超时先取消平台任务再置 expired", model.TaskRunning, true, nil},
		{"平台不支持取消时忽略", model.TaskRunning, true, provider.ErrCancelUnsupported},
		{"取消平台任务失败也照样置 expired", model.TaskRunning, true, errors.New("平台 500")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newWkEnv(nil)
			env.task(1, tt.status, func(t *model.GenerationTask) { t.DeadlineAt = t.NextPollAt.Add(-time.Second) })
			env.exec.cancelFn = func(provider.TaskRef) error { return tt.cancelErr }
			env.runOnce(t)

			got := env.store.get(1)
			if got.Status != model.TaskExpired {
				t.Fatalf("应 expired：%+v", got)
			}
			if tt.wantCancel != (len(env.exec.cancels) == 1) {
				t.Fatalf("取消平台任务次数不对：%d", len(env.exec.cancels))
			}
			if tt.wantCancel && env.exec.cancels[0].ProviderTaskID != "pt-1" {
				t.Fatalf("取消请求不对：%+v", env.exec.cancels[0])
			}
			if s, q, _ := env.exec.counts(); s != 0 || q != 0 {
				t.Fatalf("超时任务不应再提交 / 查询：%d %d", s, q)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 重启恢复 / 租约
// ---------------------------------------------------------------------------

func TestWorker_RestartRecovery_LeaseExpiredTaskIsReclaimed(t *testing.T) {
	clock := newWkClock()
	store := newWkStore(clock)
	exec, saver := &wkExec{}, &wkSaver{}
	newWorker := func() *Worker {
		return New(store, exec, saver, nil, Options{Now: clock.Now, Rand: func() float64 { return 0.5 }, Lease: time.Minute})
	}
	envTask := &wkEnv{clock: clock, store: store}
	envTask.task(1, model.TaskPending, nil)

	// 旧进程领取了任务（写了租约）然后崩溃，什么都没处理
	claimed, _ := store.ClaimDue(context.Background(), 10, time.Minute)
	if len(claimed) != 1 {
		t.Fatalf("旧进程应领到任务：%d", len(claimed))
	}

	// 新进程启动：租约还没过期，不会抢
	w2 := newWorker()
	if n, _ := w2.RunOnce(context.Background()); n != 0 {
		t.Fatalf("租约未过期不应领取：%d", n)
	}

	// 租约过期后被新进程接手并完成提交
	clock.Advance(61 * time.Second)
	if n, _ := w2.RunOnce(context.Background()); n != 1 {
		t.Fatalf("租约过期后应被重新领取：%d", n)
	}
	if got := store.get(1); got.Status != model.TaskQueued || got.ProviderTaskID != "pt-1" {
		t.Fatalf("新进程应完成提交：%+v", got)
	}
}

func TestWorker_RestartRecovery_FinalizingResumesWithFreshWorker(t *testing.T) {
	env := newWkEnv(nil)
	env.task(1, model.TaskFinalizing, nil)
	env.exec.queryFn = func(int, provider.TaskRef) (*provider.QueryResult, error) {
		return succeededResult("https://p.example.com/0.mp4", "https://p.example.com/1.mp4"), nil
	}
	failedOnce := false
	env.saver.errFn = func(n int) error {
		if n == 2 && !failedOnce {
			failedOnce = true
			return errors.New("磁盘写入失败")
		}
		return nil
	}
	env.runOnce(t) // 第一个产物成功、第二个失败，等待重试

	// “重启”：换一个全新的 worker（内存里的转存进度丢失），状态全在 store（DB）里
	w2 := New(env.store, env.exec, env.saver, nil, Options{Now: env.clock.Now, Rand: func() float64 { return 0.5 }})
	env.clock.Advance(10 * time.Second)
	if _, err := w2.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := env.store.get(1)
	if got.Status != model.TaskSucceeded || len(env.store.completed[1]) != 2 {
		t.Fatalf("重启后应能继续完成（进度丢失只会多转存一次）：%+v", got)
	}
}

func TestWorker_Heartbeat_ExtendsLeaseDuringLongTransfer(t *testing.T) {
	env := newWkEnv(func(o *Options) { o.Lease = 90 * time.Millisecond })
	env.task(1, model.TaskFinalizing, nil)
	env.exec.queryFn = func(int, provider.TaskRef) (*provider.QueryResult, error) {
		return succeededResult("https://p.example.com/a.mp4"), nil
	}
	env.exec.dlFn = func(int, string) (*provider.Download, error) {
		time.Sleep(250 * time.Millisecond) // 比租约还长的下载
		return &provider.Download{Body: io.NopCloser(strings.NewReader("x")), ContentType: "video/mp4"}, nil
	}
	env.runOnce(t)
	if env.store.extendCalls.Load() < 2 {
		t.Fatalf("长时间转存期间应续租，实际续租 %d 次", env.store.extendCalls.Load())
	}
	if env.store.get(1).Status != model.TaskSucceeded {
		t.Fatal("应成功")
	}
}

// ---------------------------------------------------------------------------
// 调度循环与执行池
// ---------------------------------------------------------------------------

func TestWorker_Concurrency_ClaimsOnlyFreeSlots(t *testing.T) {
	env := newWkEnv(func(o *Options) { o.Concurrency = 2; o.BatchSize = 50 })
	for i := uint64(1); i <= 5; i++ {
		env.task(i, model.TaskPending, nil)
	}
	var running, maxRunning atomic.Int32
	release := make(chan struct{})
	env.exec.submitFn = func(n int, in provider.SubmitInput) (string, error) {
		cur := running.Add(1)
		for {
			m := maxRunning.Load()
			if cur <= m || maxRunning.CompareAndSwap(m, cur) {
				break
			}
		}
		<-release
		running.Add(-1)
		return fmt.Sprintf("pt-%d", in.Task.ID), nil
	}

	// 第一轮：只领取 2 个（执行池大小），其余留在库里
	n, err := env.w.ClaimAndRun(context.Background())
	if err != nil || n != 2 {
		t.Fatalf("应只领取 2 个：n=%d err=%v", n, err)
	}
	// 执行池已满时不再领取
	if n, _ := env.w.ClaimAndRun(context.Background()); n != 0 {
		t.Fatalf("执行池已满不应领取：%d", n)
	}
	close(release)
	env.w.WaitGroup().Wait()
	for len(env.store.callList()) < 2 {
		time.Sleep(time.Millisecond)
	}

	for i := 0; i < 5 && env.store.count("MarkSubmitted") < 5; i++ {
		env.runOnce(t)
	}
	if maxRunning.Load() > 2 {
		t.Fatalf("并发数超过执行池大小：%d", maxRunning.Load())
	}
	if env.store.count("MarkSubmitted") != 5 {
		t.Fatalf("5 个任务最终都应提交：%v", env.store.callList())
	}
	for _, l := range env.store.claimLimits {
		if l > 2 {
			t.Fatalf("领取上限不应超过空闲槽位：%v", env.store.claimLimits)
		}
	}
}

func TestWorker_PanicInOneTaskDoesNotAffectOthers(t *testing.T) {
	env := newWkEnv(nil)
	env.task(1, model.TaskPending, nil)
	env.task(2, model.TaskPending, nil)
	env.exec.submitFn = func(n int, in provider.SubmitInput) (string, error) {
		if in.Task.ID == 1 {
			panic("boom")
		}
		return "pt-2", nil
	}
	env.runOnce(t)
	if got := env.store.get(2); got.Status != model.TaskQueued {
		t.Fatalf("另一个任务应正常处理：%+v", got)
	}
	if got := env.store.get(1); got.Status != model.TaskPending {
		t.Fatalf("panic 的任务保持原状态等租约过期：%+v", got)
	}
}

func TestWorker_ClaimErrorDoesNotCrash(t *testing.T) {
	env := newWkEnv(nil)
	env.store.claimErr = errors.New("db down")
	if _, err := env.w.RunOnce(context.Background()); err == nil {
		t.Fatal("RunOnce 应返回领取错误")
	}
	env.w.Dispatch(context.Background()) // 循环里只记日志，不应 panic
}

func TestWorker_Run_TickAndKickAndGracefulShutdown(t *testing.T) {
	env := newWkEnv(func(o *Options) { o.Tick = time.Hour; o.ShutdownTimeout = 5 * time.Second })
	release := make(chan struct{})
	started := make(chan struct{}, 4)
	env.exec.submitFn = func(n int, in provider.SubmitInput) (string, error) {
		started <- struct{}{}
		<-release
		return fmt.Sprintf("pt-%d", in.Task.ID), nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- env.w.Run(ctx) }()

	// Tick 是 1 小时：靠 kick 立即触发调度
	time.Sleep(50 * time.Millisecond)
	env.task(1, model.TaskPending, nil)
	env.kick <- struct{}{}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("kick 后应立即处理新任务")
	}

	// 取消 ctx：Run 要等在途任务处理完才返回
	cancel()
	select {
	case <-runDone:
		t.Fatal("在途任务未完成时 Run 不应返回")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("Run 应返回 nil：%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("在途任务完成后 Run 应返回")
	}
	if got := env.store.get(1); got.Status != model.TaskQueued {
		t.Fatalf("在途任务应处理完：%+v", got)
	}
}

func TestWorker_Run_ShutdownTimeoutCancelsInflight(t *testing.T) {
	env := newWkEnv(func(o *Options) { o.Tick = 10 * time.Millisecond; o.ShutdownTimeout = 100 * time.Millisecond })
	env.task(1, model.TaskPending, nil)
	started := make(chan struct{}, 1)
	env.exec.submitFn = func(n int, in provider.SubmitInput) (string, error) {
		started <- struct{}{}
		time.Sleep(1500 * time.Millisecond) // 模拟一个不响应取消的长操作
		return "", errors.New("late")
	}
	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan struct{})
	go func() { _ = env.w.Run(ctx); close(runDone) }()
	<-started
	cancel()
	start := time.Now()
	select {
	case <-runDone:
	case <-time.After(10 * time.Second):
		t.Fatal("超过 ShutdownTimeout 后 Run 最终应返回")
	}
	if time.Since(start) > 4*time.Second {
		t.Fatalf("退出耗时过长：%v", time.Since(start))
	}
}

func TestWorker_Run_PicksUpExistingDueTasksOnStart(t *testing.T) {
	env := newWkEnv(func(o *Options) { o.Tick = time.Hour })
	env.task(1, model.TaskRunning, nil) // 重启前留下的到期任务
	env.exec.queryFn = func(int, provider.TaskRef) (*provider.QueryResult, error) {
		return &provider.QueryResult{Status: provider.StatusRunning, Progress: intPtr(10)}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = env.w.Run(ctx); close(done) }()
	deadline := time.Now().Add(2 * time.Second)
	for env.store.count("MarkPolled") == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if env.store.count("MarkPolled") == 0 {
		t.Fatal("启动时应立即处理已到期的任务（重启恢复）")
	}
}
