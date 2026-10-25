package agent_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
	. "video-canvas/internal/service/agent"
)

// fakeRemote 是 Agent 服务里的运行：事实和变过的运行列表由测试直接给。
type fakeRemote struct {
	mu       sync.Mutex
	facts    map[uint64]*RemoteRunFacts
	refs     []RemoteRunRef
	err      error
	factCall int
	listArgs []int64
}

func (r *fakeRemote) Facts(_ context.Context, runID uint64) (*RemoteRunFacts, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.factCall++
	if r.err != nil {
		return nil, r.err
	}
	f, ok := r.facts[runID]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return f, nil
}

func (r *fakeRemote) ListUpdated(_ context.Context, afterMs int64, limit int) ([]RemoteRunRef, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.listArgs = append(r.listArgs, afterMs)
	if r.err != nil {
		return nil, r.err
	}
	var out []RemoteRunRef
	for _, ref := range r.refs {
		if ref.UpdatedAt > afterMs && len(out) < limit {
			out = append(out, ref)
		}
	}
	return out, nil
}

// 雪花 id 超过 2^53：十进制字符串走完整条链路不能丢精度
const (
	durRun     = "281474976710657123"
	durSession = "281474976710657001"
)

func durableFacts() *RemoteRunFacts {
	end := obsT0.Add(90 * time.Second).UTC().Format(time.RFC3339Nano)
	decided := obsT0.Add(40 * time.Second).UTC().Format(time.RFC3339Nano)
	at := obsT0.Format(time.RFC3339Nano)
	return &RemoteRunFacts{
		Run: RemoteRun{
			ID: durRun, SessionID: durSession, CanvasID: "7", UserID: "1", Status: "succeeded", Mode: "all", BudgetCredits: 50, SpentCredits: 12, ContextTokens: 800,
			Steps: 2, MaxSteps: 40, PromptVersion: 9, ToolsHash: "abc", BuildSHA: "7baa1a3", HarnessKey: "hk9", CreatedAt: at, EndedAt: &end,
		},
		Events: []RemoteEvent{
			{RunID: durRun, Seq: 1, Type: "message.user", Data: []byte(`{"text":"把剧本拆成 6 个镜头"}`), CreatedAt: at},
			{RunID: durRun, Seq: 2, Type: "message.user", Data: []byte(`{"text":"别太多","steer":true}`), CreatedAt: at},
			{RunID: durRun, Seq: 3, Type: "message.done", Data: []byte(`{"text":"好的"}`), CreatedAt: at},
		},
		Approvals: []RemoteApproval{{ID: "281474976710657999", Kind: "delete", ToolCallID: "c1", Status: "rejected", QuoteCredits: 0,
			Payload: []byte(`{"n":1}`), Decision: []byte(`{"decision":"reject"}`), Result: []byte(`{}`), DecidedAt: &decided, CreatedAt: at}},
		Feedback: &RemoteFeedback{Rating: -1, Reasons: []string{"slow"}, Comment: "慢", CreatedAt: at},
		Context:  []RemoteEvent{{RunID: "281474976710600001", Seq: 0, Type: "message.user", Data: []byte(`{"text":"之前说过的话"}`), CreatedAt: at}},
	}
}

// Go 自己持有的那部分：工具调用、模型调用（计费）、画布改动
func localFacts(runID uint64) (*model.AgentRunFacts, *model.AgentExportFacts) {
	tools := []model.AgentToolCall{{RunID: runID, Name: "canvas_apply_ops", Summary: "新建 1 个节点"}, {RunID: runID, Name: "canvas_apply_ops", IsError: true, Summary: "出错"}}
	calls := []model.AgentModelCall{{ID: 1, RunID: runID, ModelKey: "claude", InputTokens: 100, OutputTokens: 20, Charged: 6}, {ID: 2, RunID: runID, ModelKey: "claude", InputTokens: 120, OutputTokens: 30, Charged: 6}}
	undone := obsT0.Add(time.Hour)
	muts := []model.AgentMutation{{RunID: runID, Seq: 1, Kind: model.MutationApplyOps, UndoneAt: &undone}}
	return &model.AgentRunFacts{ToolCalls: tools, ModelCalls: calls, Mutations: muts, RuntimeTail: ""},
		&model.AgentExportFacts{ToolCalls: tools, ModelCalls: calls, Mutations: muts}
}

type remoteEnv struct {
	*observeEnv
	remote *fakeRemote
	runID  uint64
}

func newRemoteEnv(t *testing.T) *remoteEnv {
	t.Helper()
	e := newObserveEnv(t)
	remote := &fakeRemote{facts: map[uint64]*RemoteRunFacts{}}
	var runID uint64 = 281474976710657123
	remote.facts[runID] = durableFacts()
	lf, lx := localFacts(runID)
	e.repo.local[runID], e.repo.localExport[runID] = lf, lx
	e.svc = NewObserveService(ObserveDeps{
		Repo: e.repo, Runs: e.runs, Events: e.events, Audit: e.audit, Remote: remote,
		Roles: func(_ context.Context, id uint64) (string, error) { return e.roles[id], nil },
		Now:   func() time.Time { return e.now },
	})
	return &remoteEnv{observeEnv: e, remote: remote, runID: runID}
}

// durable 的运行不在 Go 的 agent_runs 里：摘要由 Agent 给的运行事实 + Go 自己的工具、模型、画布改动汇总出来。
func TestObserveRemote_DigestMergesAgentAndGoFacts(t *testing.T) {
	e := newRemoteEnv(t)
	if err := e.svc.RefreshDigest(context.Background(), e.runID); err != nil {
		t.Fatal(err)
	}
	d := e.repo.digests[e.runID]
	if d == nil {
		t.Fatal("应写出摘要")
	}
	if d.RunID != e.runID || d.SessionID != 281474976710657001 || d.UserID != 1 || d.CanvasID != 7 || d.Status != model.RunSucceeded || d.Steps != 2 || d.SpentCredits != 12 {
		t.Errorf("运行本身的字段来自 Agent（大 id 不丢精度）: %+v", d)
	}
	if d.HarnessKey != "hk9" || d.PromptVersion != 9 || d.ToolsHash != "abc" || d.BuildSHA != "7baa1a3" {
		t.Errorf("版本字段来自 Agent: %+v", d)
	}
	if d.ToolCalls != 2 || d.ToolErrors != 1 || d.ModelCalls != 2 || d.InputTokens != 220 || d.OutputTokens != 50 || d.ModelKey != "claude" {
		t.Errorf("工具和模型调用来自 Go 的库: %+v", d)
	}
	if d.MutationCount != 1 || !d.Undone {
		t.Errorf("画布改动来自 Go 的库: %+v", d)
	}
	if d.RequestHead != "把剧本拆成 6 个镜头" || d.SteerCount != 1 || d.ApprovalsTotal != 1 || d.ApprovalsRejected != 1 || d.Feedback != model.FeedbackDown {
		t.Errorf("用户消息、审批、评价来自 Agent: %+v", d)
	}
	if d.DurationMs != 90_000 || d.EndedAt == nil {
		t.Errorf("耗时按创建和结束时间算: %+v", d)
	}
}

func TestObserveRemote_PGRunsDoNotTouchAgent(t *testing.T) {
	e := newRemoteEnv(t)
	if err := e.svc.RefreshDigest(context.Background(), 5); err != nil {
		t.Fatal(err)
	}
	if e.remote.factCall != 0 {
		t.Errorf("Go 自己库里有的运行不该问 Agent: %d", e.remote.factCall)
	}
}

func TestObserveRemote_MissingEverywhereIsNotFound(t *testing.T) {
	e := newRemoteEnv(t)
	if err := e.svc.RefreshDigest(context.Background(), 12345); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("两边都没有: %v", err)
	}
	_, err := e.svc.ExportRun(context.Background(), ObserveActor{UserID: 1}, 12345, ExportJSON)
	var ce *errcode.Error
	if !errors.As(err, &ce) || ce.Code != errcode.ErrAgentRunMissing.Code {
		t.Errorf("导出不存在的运行应是 60012: %v", err)
	}
	// 没有配置 Agent 服务（process / http 模式）：行为和以前一样
	plain := newObserveEnv(t)
	if err := plain.svc.RefreshDigest(context.Background(), 281474976710657123); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("没有 Remote: %v", err)
	}
}

func TestObserveRemote_AgentDownIsAnErrorNotNotFound(t *testing.T) {
	e := newRemoteEnv(t)
	e.remote.err = errors.New("连不上 Agent")
	err := e.svc.RefreshDigest(context.Background(), e.runID)
	if err == nil || errors.Is(err, repository.ErrNotFound) {
		t.Errorf("Agent 不通是基础设施错误，不能被当成运行不存在: %v", err)
	}
}

// 导出：事件、审批、评价、上下文来自 Agent，工具参数结果、模型调用、画布改动来自 Go，合成一份。
func TestObserveRemote_ExportMergesBothSides(t *testing.T) {
	e := newRemoteEnv(t)
	out, err := e.svc.ExportRun(context.Background(), ObserveActor{UserID: 1}, e.runID, ExportBoth)
	if err != nil {
		t.Fatal(err)
	}
	v := out.Data
	if v.Run == nil || uint64(v.Run.ID) != e.runID || v.Run.Status != model.RunSucceeded {
		t.Fatalf("运行: %+v", v.Run)
	}
	if len(v.Events) != 3 || v.Events[0].Type != "message.user" {
		t.Errorf("事件来自 Agent: %d", len(v.Events))
	}
	if len(v.ToolCalls) != 2 || len(v.ModelCalls) != 2 || len(v.Mutations) != 1 {
		t.Errorf("工具、模型、改动来自 Go: %d %d %d", len(v.ToolCalls), len(v.ModelCalls), len(v.Mutations))
	}
	if len(v.Approvals) != 1 || v.Approvals[0].Kind != "delete" || v.Approvals[0].Status != "rejected" || v.Approvals[0].DecidedAt == nil {
		t.Errorf("审批来自 Agent: %+v", v.Approvals)
	}
	if v.Feedback == nil || v.Feedback.Rating != -1 || len(v.Feedback.Reasons) != 1 || v.Feedback.Comment != "慢" {
		t.Errorf("评价来自 Agent: %+v", v.Feedback)
	}
	if len(v.Context) != 1 {
		t.Errorf("上下文来自 Agent: %d", len(v.Context))
	}
	if !strings.Contains(out.Content, "把剧本拆成 6 个镜头") || !strings.Contains(out.Content, "apply_ops") {
		t.Errorf("Markdown 里应有用户消息和工具调用:\n%s", out.Content)
	}
}

func TestObserveRemote_RunRequestsKnowsRemoteRuns(t *testing.T) {
	e := newRemoteEnv(t)
	if _, err := e.svc.RunRequests(context.Background(), ObserveActor{UserID: 1}, e.runID); err != nil {
		t.Errorf("Agent 里的运行也能取请求体（请求体在 Go）: %v", err)
	}
	_, err := e.svc.RunRequests(context.Background(), ObserveActor{UserID: 1}, 777)
	var ce *errcode.Error
	if !errors.As(err, &ce) || ce.Code != errcode.ErrAgentRunMissing.Code {
		t.Errorf("不存在应是 60012: %v", err)
	}
}

// ---- 同步 ----

// ms 把相对偏移变成毫秒时间戳：同步第一次回看 24 小时，测试里的更新时间要落在这个窗口内
func ms(n int64) int64 { return obsT0.UnixMilli() + n }

func (e *remoteEnv) addRemote(id uint64, status string, updatedAt int64) {
	f := durableFacts()
	f.Run.ID = decimal(id)
	f.Run.Status = status
	e.remote.facts[id] = f
	e.remote.refs = append(e.remote.refs, RemoteRunRef{ID: decimal(id), Status: status, UpdatedAt: updatedAt})
	lf, lx := localFacts(id)
	e.repo.local[id], e.repo.localExport[id] = lf, lx
}

func decimal(id uint64) string { return strconv.FormatUint(id, 10) }

func TestObserveRemote_SyncRefreshesChangedRunsExceptActiveOnes(t *testing.T) {
	e := newRemoteEnv(t)
	e.remote.refs = nil
	e.addRemote(901, "succeeded", ms(1000))
	e.addRemote(902, "running", ms(1100)) // 还在跑：不汇总，结束时更新时间会再变
	e.addRemote(903, "waiting_approval", ms(1200))
	n, err := e.svc.SyncRemote(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 || e.repo.digests[901] == nil || e.repo.digests[903] == nil || e.repo.digests[902] != nil {
		t.Errorf("结束和等审批的汇总，运行中的不汇总: n=%d digests=%v", n, len(e.repo.digests))
	}

	// 902 结束：更新时间变大，下一轮同步汇总它
	for i := range e.remote.refs {
		if e.remote.refs[i].ID == "902" {
			e.remote.refs[i].Status, e.remote.refs[i].UpdatedAt = "succeeded", ms(1300)
		}
	}
	e.remote.facts[902].Run.Status = "succeeded"
	if _, err := e.svc.SyncRemote(context.Background()); err != nil {
		t.Fatal(err)
	}
	if e.repo.digests[902] == nil || e.repo.digests[902].Status != "succeeded" {
		t.Error("结束后应汇总")
	}
}

func TestObserveRemote_SyncCursorAdvancesWithOverlapAndRetriesFailures(t *testing.T) {
	e := newRemoteEnv(t)
	e.remote.refs = nil
	e.addRemote(911, "succeeded", ms(100_000))
	if _, err := e.svc.SyncRemote(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.SyncRemote(context.Background()); err != nil {
		t.Fatal(err)
	}
	last := e.remote.listArgs[len(e.remote.listArgs)-1]
	if last >= ms(100_000) || ms(100_000)-last > 10_000 {
		t.Errorf("第二轮从游标稍早一点开始（容忍提交顺序和同一毫秒），实际 after=%d", last)
	}

	// 汇总失败（比如 Go 的库暂时不通）：游标不能越过它，下一轮重试
	e.addRemote(912, "succeeded", ms(200_000))
	e.repo.failRun = 912 // 912 汇总失败，但更晚的 913 成功：游标不能因此越过 912
	e.addRemote(913, "succeeded", ms(300_000))
	e.repo.upsertErr = errors.New("库不通")
	if _, err := e.svc.SyncRemote(context.Background()); err == nil {
		t.Error("有运行汇总失败应返回错误")
	}
	if e.repo.digests[913] == nil {
		t.Error("别的运行不受影响")
	}
	e.repo.upsertErr = nil
	if _, err := e.svc.SyncRemote(context.Background()); err != nil {
		t.Fatal(err)
	}
	if e.repo.digests[912] == nil {
		t.Error("恢复后应补上失败的那条")
	}
}

func TestObserveRemote_SyncAgentDownReturnsError(t *testing.T) {
	e := newRemoteEnv(t)
	e.remote.err = errors.New("连不上 Agent")
	if _, err := e.svc.SyncRemote(context.Background()); err == nil {
		t.Error("Agent 不通应返回错误，由调用方记日志、下一轮重试")
	}
	plain := newObserveEnv(t)
	if n, err := plain.svc.SyncRemote(context.Background()); n != 0 || err != nil {
		t.Errorf("没有 Remote 什么都不做: %d %v", n, err)
	}
}

// ---- HTTP 客户端 ----

func remoteServer(t *testing.T, status int, body string) (*HTTPRemoteRuns, func() (string, string)) {
	t.Helper()
	var mu sync.Mutex
	var gotURL, gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotURL, gotToken = r.URL.RequestURI(), r.Header.Get("X-Agent-Service-Token")
		mu.Unlock()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return &HTTPRemoteRuns{BaseURL: srv.URL + "/", ServiceToken: "tok-0123456789abcdef0123456789abcdef"}, func() (string, string) {
		mu.Lock()
		defer mu.Unlock()
		return gotURL, gotToken
	}
}

func TestHTTPRemoteRuns_FactsAndList(t *testing.T) {
	c, seen := remoteServer(t, 200, `{"code":0,"msg":"success","data":{"run":{"id":"281474976710657123","session_id":"1","canvas_id":"7","user_id":"1","status":"succeeded","mode":"all","created_at":"2026-10-20T12:00:00.000Z","ended_at":null},"session_deleted":true,"events":[],"approvals":[],"feedback":null,"context":[]}}`)
	f, err := c.Facts(context.Background(), 281474976710657123)
	if err != nil {
		t.Fatal(err)
	}
	if f.Run.ID != "281474976710657123" || !f.SessionDeleted || f.Run.EndedAt != nil {
		t.Errorf("解析不对: %+v", f)
	}
	if u, tok := seen(); u != "/observe/runs/281474976710657123" || tok != c.ServiceToken {
		t.Errorf("路径或令牌不对: %s %s", u, tok)
	}

	c, seen = remoteServer(t, 200, `{"code":0,"msg":"success","data":[{"id":"9","status":"succeeded","updated_at":1760961600000}]}`)
	refs, err := c.ListUpdated(context.Background(), 123, 50)
	if err != nil || len(refs) != 1 || refs[0].UpdatedAt != 1760961600000 {
		t.Fatalf("%v %+v", err, refs)
	}
	if u, _ := seen(); u != "/observe/runs?limit=50&updated_after=123" {
		t.Errorf("查询参数不对: %s", u)
	}
}

func TestHTTPRemoteRuns_Errors(t *testing.T) {
	c, _ := remoteServer(t, 404, `{"code":60012,"msg":"运行不存在"}`)
	if _, err := c.Facts(context.Background(), 1); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("60012 是运行不存在: %v", err)
	}
	c, _ = remoteServer(t, 500, `{"code":10000,"msg":"服务内部错误"}`)
	if _, err := c.Facts(context.Background(), 1); err == nil || errors.Is(err, repository.ErrNotFound) {
		t.Errorf("服务出错不能当成不存在: %v", err)
	}
	c, _ = remoteServer(t, 401, `{"code":10003,"msg":"未登录或登录已过期"}`)
	if _, err := c.ListUpdated(context.Background(), 0, 10); err == nil || errors.Is(err, repository.ErrNotFound) {
		t.Errorf("令牌不对是错误: %v", err)
	}
	c, _ = remoteServer(t, 502, `<html>bad gateway</html>`)
	if _, err := c.Facts(context.Background(), 1); err == nil || errors.Is(err, repository.ErrNotFound) {
		t.Errorf("响应不是 JSON 是错误: %v", err)
	}
	down := &HTTPRemoteRuns{BaseURL: "http://127.0.0.1:1", ServiceToken: "x"}
	if _, err := down.ListUpdated(context.Background(), 0, 10); err == nil || errors.Is(err, repository.ErrNotFound) {
		t.Errorf("连不上是错误: %v", err)
	}
}
