package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/datatypes"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/repository"
)

// agent.runtime=durable 时，运行、事件、审批、评价归独立的 Agent 服务；工具调用的参数结果、模型调用（计费）、
// 模型请求体、画布改动仍在 Go 的库里。后台的运行记录、导出、统计要看到完整的一次运行，所以：
//   - 导出：Agent 给它那一半（RemoteRunFacts），和 Go 自己的那一半合成一份；
//   - 摘要（列表、统计、版本对比）：仍然只有 Go 的 agent_run_digests 一张表，由 SyncRemote 定时把 Agent 里变过的运行汇总进来。
// Go 自己库里有的运行（切换到 durable 之前的历史运行）照旧，不问 Agent。

// RemoteRun 是 Agent 服务里的一次运行（GET /observe/runs/:id 的 run）。id 是十进制字符串，时间是 RFC3339。
type RemoteRun struct {
	ID            string  `json:"id"`
	SessionID     string  `json:"session_id"`
	CanvasID      string  `json:"canvas_id"`
	UserID        string  `json:"user_id"`
	Status        string  `json:"status"`
	Mode          string  `json:"mode"`
	BudgetCredits int     `json:"budget_credits"`
	SpentCredits  int     `json:"spent_credits"`
	ContextTokens int     `json:"context_tokens"`
	Steps         int     `json:"steps"`
	MaxSteps      int     `json:"max_steps"`
	ErrorCode     string  `json:"error_code"`
	ErrorMessage  string  `json:"error_message"`
	PromptVersion int     `json:"prompt_version"`
	ToolsHash     string  `json:"tools_hash"`
	BuildSHA      string  `json:"build_sha"`
	HarnessKey    string  `json:"harness_key"`
	CreatedAt     string  `json:"created_at"`
	EndedAt       *string `json:"ended_at"`
}

// RemoteEvent 是 Agent 服务里的一条落库事件。
type RemoteEvent struct {
	RunID     string          `json:"run_id"`
	Seq       int64           `json:"seq"`
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data"`
	CreatedAt string          `json:"created_at"`
}

// RemoteApproval 是 Agent 服务里的一次审批。
type RemoteApproval struct {
	ID           string          `json:"id"`
	Kind         string          `json:"kind"`
	ToolCallID   string          `json:"tool_call_id"`
	Status       string          `json:"status"`
	QuoteCredits int             `json:"quote_credits"`
	Payload      json.RawMessage `json:"payload"`
	Decision     json.RawMessage `json:"decision"`
	Result       json.RawMessage `json:"result"`
	DecidedAt    *string         `json:"decided_at"`
	CreatedAt    string          `json:"created_at"`
}

// RemoteFeedback 是运行所有者的评价。
type RemoteFeedback struct {
	Rating    int      `json:"rating"`
	Reasons   []string `json:"reasons"`
	Comment   string   `json:"comment"`
	CreatedAt string   `json:"created_at"`
}

// RemoteRunFacts 是 Agent 服务持有的那一半事实。
type RemoteRunFacts struct {
	Run            RemoteRun        `json:"run"`
	SessionDeleted bool             `json:"session_deleted"`
	Events         []RemoteEvent    `json:"events"` // 本轮的全部落库事件，按序号升序
	Approvals      []RemoteApproval `json:"approvals"`
	Feedback       *RemoteFeedback  `json:"feedback"`
	Context        []RemoteEvent    `json:"context"` // 同会话更早两轮的用户消息和最终回复
}

// RemoteRunRef 是 Agent 服务里变过的一次运行：UpdatedAt 是毫秒时间戳，同步靠它做游标。
type RemoteRunRef struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	UpdatedAt int64  `json:"updated_at"`
}

// RemoteRuns 读 Agent 服务里的运行（后台只读）。
type RemoteRuns interface {
	// Facts 读一次运行的事实；运行不存在返回 repository.ErrNotFound，Agent 不通返回其他错误。
	Facts(ctx context.Context, runID uint64) (*RemoteRunFacts, error)
	// ListUpdated 列出更新时间晚于 afterMs 的运行，按更新时间升序，最多 limit 条。
	ListUpdated(ctx context.Context, afterMs int64, limit int) ([]RemoteRunRef, error)
}

// ---- 转成 Go 的模型 ----

func parseRemoteID(s string) (uint64, error) {
	id, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("来自 Agent 服务的 id %q 不是正整数", s)
	}
	return id, nil
}

func parseRemoteTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("来自 Agent 服务的时间 %q 不是 RFC3339", s)
	}
	return t, nil
}

func optTime(s *string) (*time.Time, error) {
	if s == nil {
		return nil, nil
	}
	t, err := parseRemoteTime(*s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *RemoteRun) toModel() (*model.AgentRun, error) {
	out := &model.AgentRun{
		Status: r.Status, Mode: r.Mode, BudgetCredits: r.BudgetCredits, SpentCredits: r.SpentCredits, ContextTokens: r.ContextTokens, Steps: r.Steps, MaxSteps: r.MaxSteps,
		ErrorCode: r.ErrorCode, ErrorMessage: r.ErrorMessage, PromptVersion: r.PromptVersion, ToolsHash: r.ToolsHash, BuildSHA: r.BuildSHA, HarnessKey: r.HarnessKey,
	}
	var err error
	for _, f := range []struct {
		dst *uint64
		src string
	}{{&out.ID, r.ID}, {&out.SessionID, r.SessionID}, {&out.CanvasID, r.CanvasID}, {&out.UserID, r.UserID}} {
		if *f.dst, err = parseRemoteID(f.src); err != nil {
			return nil, err
		}
	}
	if out.CreatedAt, err = parseRemoteTime(r.CreatedAt); err != nil {
		return nil, err
	}
	if out.EndedAt, err = optTime(r.EndedAt); err != nil {
		return nil, err
	}
	return out, nil
}

func (e *RemoteEvent) toModel(sessionID uint64) (model.AgentEvent, error) {
	runID, err := strconv.ParseUint(e.RunID, 10, 64)
	if err != nil {
		return model.AgentEvent{}, fmt.Errorf("来自 Agent 服务的事件 run_id %q 不是整数", e.RunID)
	}
	at, err := parseRemoteTime(e.CreatedAt)
	if err != nil {
		return model.AgentEvent{}, err
	}
	data := e.Data
	if len(data) == 0 {
		data = json.RawMessage("{}")
	}
	return model.AgentEvent{SessionID: sessionID, RunID: runID, Seq: e.Seq, Type: e.Type, PayloadJSON: datatypes.JSON(data), CreatedAt: at}, nil
}

func rawOr(b json.RawMessage, def string) datatypes.JSON {
	if len(b) == 0 {
		return datatypes.JSON(def)
	}
	return datatypes.JSON(b)
}

func (a *RemoteApproval) toModel(run *model.AgentRun) (model.AgentApproval, error) {
	id, err := parseRemoteID(a.ID)
	if err != nil {
		return model.AgentApproval{}, err
	}
	created, err := parseRemoteTime(a.CreatedAt)
	if err != nil {
		return model.AgentApproval{}, err
	}
	decided, err := optTime(a.DecidedAt)
	if err != nil {
		return model.AgentApproval{}, err
	}
	return model.AgentApproval{ID: id, RunID: run.ID, SessionID: run.SessionID, CanvasID: run.CanvasID, UserID: run.UserID, Kind: a.Kind, ToolCallID: a.ToolCallID,
		PayloadJSON: rawOr(a.Payload, "{}"), QuoteCredits: a.QuoteCredits, Status: a.Status, DecisionJSON: rawOr(a.Decision, "{}"), ResultJSON: rawOr(a.Result, "{}"),
		DecidedAt: decided, CreatedAt: created}, nil
}

// split 把 Agent 给的事实转成 Go 的模型：运行、事件、审批、评价、上下文。
type remoteParts struct {
	run       *model.AgentRun
	events    []model.AgentEvent
	approvals []model.AgentApproval
	feedback  *model.AgentFeedback
	context   []model.AgentEvent
	deleted   bool
}

func (f *RemoteRunFacts) split() (*remoteParts, error) {
	run, err := f.Run.toModel()
	if err != nil {
		return nil, err
	}
	p := &remoteParts{run: run, deleted: f.SessionDeleted}
	for i := range f.Events {
		ev, err := f.Events[i].toModel(run.SessionID)
		if err != nil {
			return nil, err
		}
		p.events = append(p.events, ev)
	}
	for i := range f.Context {
		ev, err := f.Context[i].toModel(run.SessionID)
		if err != nil {
			return nil, err
		}
		p.context = append(p.context, ev)
	}
	for i := range f.Approvals {
		a, err := f.Approvals[i].toModel(run)
		if err != nil {
			return nil, err
		}
		p.approvals = append(p.approvals, a)
	}
	if f.Feedback != nil {
		at, err := parseRemoteTime(f.Feedback.CreatedAt)
		if err != nil {
			return nil, err
		}
		reasons := f.Feedback.Reasons
		if reasons == nil {
			reasons = []string{}
		}
		b, _ := json.Marshal(reasons)
		p.feedback = &model.AgentFeedback{RunID: run.ID, UserID: run.UserID, Rating: f.Feedback.Rating, Reasons: datatypes.JSON(b), Comment: f.Feedback.Comment, CreatedAt: at, UpdatedAt: at}
	}
	return p, nil
}

// ---- 合并两边的事实 ----

// runFacts 读生成摘要需要的事实：先问 Go 自己的库，没有这次运行且配了 Agent 服务时，运行本身、审批、评价、用户消息取自 Agent，
// 工具调用、模型调用、画布改动取自 Go 的库。Agent 不通是错误，不会被当成「运行不存在」。
func (s *ObserveService) runFacts(ctx context.Context, runID uint64) (*model.AgentRunFacts, error) {
	f, err := s.d.Repo.RunFacts(ctx, runID)
	if !errors.Is(err, repository.ErrNotFound) || s.d.Remote == nil {
		return f, err
	}
	rf, err := s.d.Remote.Facts(ctx, runID)
	if err != nil {
		return nil, err
	}
	p, err := rf.split()
	if err != nil {
		return nil, err
	}
	local, err := s.d.Repo.LocalRunFacts(ctx, runID)
	if err != nil {
		return nil, err
	}
	out := &model.AgentRunFacts{Run: *p.run, SessionDeleted: p.deleted, ToolCalls: local.ToolCalls, ModelCalls: local.ModelCalls, Mutations: local.Mutations,
		Approvals: p.approvals, Feedback: p.feedback, RuntimeTail: local.RuntimeTail}
	for _, ev := range p.events {
		if ev.Type == "message.user" {
			out.UserMessages = append(out.UserMessages, ev)
		}
	}
	return out, nil
}

// exportFacts 读导出一次运行的全部事实，规则同 runFacts。
func (s *ObserveService) exportFacts(ctx context.Context, runID uint64) (*model.AgentExportFacts, error) {
	f, err := s.d.Repo.ExportFacts(ctx, runID)
	if !errors.Is(err, repository.ErrNotFound) || s.d.Remote == nil {
		return f, err
	}
	rf, err := s.d.Remote.Facts(ctx, runID)
	if err != nil {
		return nil, err
	}
	p, err := rf.split()
	if err != nil {
		return nil, err
	}
	local, err := s.d.Repo.LocalExportFacts(ctx, runID)
	if err != nil {
		return nil, err
	}
	local.Run, local.Events, local.Approvals, local.Feedback, local.Context = *p.run, p.events, p.approvals, p.feedback, p.context
	return local, nil
}

// ---- 摘要同步 ----

const (
	remoteSyncPage     = 200
	remoteSyncOverlap  = 5 * time.Second // 游标往回多读一点：容忍提交顺序和同一毫秒的并列，汇总是幂等的
	remoteSyncLookback = 24 * time.Hour  // 第一次同步（进程刚启动）回看多久
)

// SyncRemote 把 Agent 服务里变过的运行汇总进摘要表（列表、统计、版本对比读它）：问 Agent 哪些运行更新过，
// 对每个不在「排队、运行中」的运行重新汇总。游标只在内存里，重启后回看 24 小时；汇总是幂等的。
// 某个运行汇总失败（比如库暂时不通）时游标停在它前面，下一轮重试；返回成功汇总的条数和第一个错误。没有配置 Agent 服务时什么都不做。
func (s *ObserveService) SyncRemote(ctx context.Context) (int, error) {
	if s.d.Remote == nil {
		return 0, nil
	}
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	after := s.syncCursor - remoteSyncOverlap.Milliseconds()
	if s.syncCursor == 0 {
		after = s.d.Now().Add(-remoteSyncLookback).UnixMilli()
	}
	var t syncTally
	for {
		refs, err := s.d.Remote.ListUpdated(ctx, after, remoteSyncPage)
		if err != nil {
			return t.refreshed, err
		}
		for _, ref := range refs {
			s.syncOne(ctx, ref, &t)
		}
		if len(refs) < remoteSyncPage {
			break
		}
		after = refs[len(refs)-1].UpdatedAt
	}
	next := t.maxOK
	if t.firstFail > 0 {
		next = t.firstFail - 1 // 游标停在第一个失败的前面，下一轮从它重试
	}
	if next > s.syncCursor {
		s.syncCursor = next
	}
	return t.refreshed, t.firstErr
}

// syncTally 是一轮同步的统计：成功汇总的条数、处理完的最大更新时间、最早的失败和它的错误。
type syncTally struct {
	refreshed        int
	maxOK, firstFail int64
	firstErr         error
}

// syncOne 汇总一个变过的运行并记入统计。
func (s *ObserveService) syncOne(ctx context.Context, ref RemoteRunRef, t *syncTally) {
	if ref.Status == model.RunQueued || ref.Status == model.RunRunning {
		t.maxOK = max(t.maxOK, ref.UpdatedAt) // 运行中的不汇总：结束时更新时间会再变大
		return
	}
	id, err := parseRemoteID(ref.ID)
	if err == nil {
		err = s.RefreshDigest(ctx, id)
	}
	switch {
	case err == nil:
		t.refreshed++
		t.maxOK = max(t.maxOK, ref.UpdatedAt)
	case errors.Is(err, repository.ErrNotFound):
		t.maxOK = max(t.maxOK, ref.UpdatedAt) // 刚列出来就没了（不该发生），没必要反复重试
	default:
		logger.Warn("汇总 Agent 服务里的运行摘要失败", zap.Error(err), zap.String("run_id", ref.ID))
		if t.firstFail == 0 || ref.UpdatedAt < t.firstFail {
			t.firstFail = ref.UpdatedAt
		}
		if t.firstErr == nil {
			t.firstErr = err
		}
	}
}

// RunRemoteSync 每隔 interval 同步一次，直到 ctx 取消。失败只记日志（一分钟最多一条），下一轮继续。
func (s *ObserveService) RunRemoteSync(ctx context.Context, interval time.Duration) {
	if s.d.Remote == nil {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var lastWarn time.Time
	for {
		if _, err := s.SyncRemote(ctx); err != nil && ctx.Err() == nil && time.Since(lastWarn) > time.Minute {
			lastWarn = time.Now()
			logger.Warn("同步 Agent 服务的运行摘要失败，下一轮重试", zap.Error(err))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// ---- HTTP 客户端 ----

// HTTPRemoteRuns 用 HTTP 读 Agent 服务里的运行（GET /observe/…），靠服务令牌认证。
type HTTPRemoteRuns struct {
	BaseURL      string
	ServiceToken string
	Client       *http.Client // 为 nil 时用带 15 秒超时的默认客户端
}

const remoteRunsTimeout = 15 * time.Second

func (h *HTTPRemoteRuns) get(ctx context.Context, path string, out any) error {
	c := h.Client
	if c == nil {
		c = &http.Client{Timeout: remoteRunsTimeout}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(h.BaseURL, "/")+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Agent-Service-Token", h.ServiceToken)
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("连接 Agent 服务失败：%w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return err
	}
	var env struct {
		Code int             `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&env); err != nil {
		return fmt.Errorf("无法解析 Agent 服务的响应（HTTP %d）", resp.StatusCode)
	}
	switch {
	case env.Code == 60012: // 运行不存在
		return repository.ErrNotFound
	case env.Code != 0 || resp.StatusCode >= 300:
		return fmt.Errorf("服务端出错（Agent，HTTP %d，code %d）：%s", resp.StatusCode, env.Code, env.Msg)
	}
	return json.Unmarshal(env.Data, out)
}

// Facts 实现 RemoteRuns。
func (h *HTTPRemoteRuns) Facts(ctx context.Context, runID uint64) (*RemoteRunFacts, error) {
	var f RemoteRunFacts
	if err := h.get(ctx, "/observe/runs/"+strconv.FormatUint(runID, 10), &f); err != nil {
		return nil, err
	}
	return &f, nil
}

// ListUpdated 实现 RemoteRuns。
func (h *HTTPRemoteRuns) ListUpdated(ctx context.Context, afterMs int64, limit int) ([]RemoteRunRef, error) {
	var refs []RemoteRunRef
	q := url.Values{"updated_after": {strconv.FormatInt(afterMs, 10)}, "limit": {strconv.Itoa(limit)}}
	if err := h.get(ctx, "/observe/runs?"+q.Encode(), &refs); err != nil {
		return nil, err
	}
	return refs, nil
}
