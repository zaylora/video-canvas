package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"

	"video-canvas/internal/llmgateway"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/provider"
)

// 桥的限制。
const (
	bridgeTokenBytes = 24                    // 令牌的随机字节数，十六进制后 48 个字符
	maxStateBytes    = 8 << 20               // 一份会话历史最多 8MB
	deltaFlushEvery  = 50 * time.Millisecond // 文本增量最多每 50ms 推一次
)

// ErrBridgeToken 表示桥令牌无效、已过期或已被撤销。
var ErrBridgeToken = errors.New("桥令牌无效或已过期")

// ModelStreamer 是网关的透传入口（*llmgateway.Gateway 实现它）。
type ModelStreamer interface {
	// StreamRaw 把调用方拼好的 OpenAI 请求体收紧后发往渠道，响应的 SSE 行原样回调给 raw。
	StreamRaw(ctx context.Context, tg llmgateway.Target, body []byte, lim llmgateway.RawLimits, emit func(llmgateway.Event), raw func([]byte)) (*llmgateway.Result, error)
}

// BridgeDeps 是创建 AgentBridge 需要的依赖。
type BridgeDeps struct {
	Repo     AgentRepo               // 运行、会话的数据访问
	Canvas   *AgentCanvasService     // 读写画布
	Agent    *AgentService           // 审批、事件
	Billing  *AgentBilling           // 对话计费
	Registry provider.Registry       // 模型注册表
	Secrets  provider.SecretResolver // 渠道 Key
	Streamer ModelStreamer           // 大模型网关
	Now      func() time.Time        // 为 nil 取系统时间
	TokenTTL time.Duration           // 令牌有效期，0 取 2 小时
}

// bridgeSession 是一个令牌对应的运行片段：Node 进程用它调用桥。
type bridgeSession struct {
	runID, userID, sessionID, canvasID uint64
	modelKey, mode                     string
	selection                          []string
	expires                            time.Time
}

// AgentBridge 是 Go 与 Node 里 pi 之间的桥：Node 把 Go 当作 OpenAI 兼容端点来调模型，
// 工具调用回调 Go 执行，回合结束把对话历史交回 Go 落库。密钥、计费、画布写入、审批都留在 Go。
type AgentBridge struct {
	d      BridgeDeps
	mu     sync.Mutex
	tokens map[string]*bridgeSession
}

// NewAgentBridge 创建桥。
func NewAgentBridge(d BridgeDeps) *AgentBridge {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.TokenTTL <= 0 {
		d.TokenTTL = 2 * time.Hour
	}
	return &AgentBridge{d: d, tokens: map[string]*bridgeSession{}}
}

// IssueToken 为一个运行片段签发一次性令牌：Node 进程只拿到它，没有任何渠道密钥。
func (b *AgentBridge) IssueToken(run *model.AgentRun, in model.AgentRunInput) string {
	raw := make([]byte, bridgeTokenBytes)
	_, _ = rand.Read(raw) // crypto/rand 在受支持的平台上不会失败
	tok := hex.EncodeToString(raw)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tokens[tok] = &bridgeSession{runID: run.ID, userID: run.UserID, sessionID: run.SessionID, canvasID: run.CanvasID,
		modelKey: in.ModelKey, mode: firstNonEmpty(in.Mode, run.Mode, model.AgentModeAll), selection: in.Selection, expires: b.d.Now().Add(b.d.TokenTTL)}
	return tok
}

// TokenActive 判断令牌是否还有效。进程退出时用它区分「报告过结束的正常退出」（令牌已被 Finish 收回）
// 和「没报告就没了的崩溃」（令牌还在）。
func (b *AgentBridge) TokenActive(token string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.tokens[token]
	return ok
}

// TokenCount 返回当前有效的令牌数，监控和测试用：运行进程都退出后它应该回到 0。
func (b *AgentBridge) TokenCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.tokens)
}

// RevokeToken 让令牌立即失效。
func (b *AgentBridge) RevokeToken(token string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.tokens, token)
}

// session 校验令牌，过期的顺手清掉。
func (b *AgentBridge) session(token string) (*bridgeSession, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	s, ok := b.tokens[token]
	if !ok {
		return nil, ErrBridgeToken
	}
	if !b.d.Now().Before(s.expires) {
		delete(b.tokens, token)
		return nil, ErrBridgeToken
	}
	return s, nil
}

// activeRun 取令牌对应的运行，必须正在 running：已经被停止、在等用户或已结束的运行不能再调模型和工具。
func (b *AgentBridge) activeRun(ctx context.Context, s *bridgeSession) (*model.AgentRun, error) {
	run, err := b.d.Repo.GetRun(ctx, s.userID, s.runID)
	if err != nil {
		return nil, runErr(err)
	}
	if run.Status != model.RunRunning {
		return nil, errcode.ErrAgentState
	}
	return run, nil
}

// pauseRun 把运行置为可继续的暂停状态（预算用尽、步数用尽）：CAS 迁移，已经变了就不动。
func (b *AgentBridge) pauseRun(ctx context.Context, run *model.AgentRun, status string) {
	if _, err := b.d.Repo.UpdateRunIf(ctx, run.ID, []string{model.RunRunning}, map[string]any{"status": status}); err == nil {
		b.d.Agent.emit(ctx, run.UserID, run.SessionID, run.ID, run.CanvasID, "run.status", map[string]any{"status": status})
	}
}

// ProxyModel 代理一次模型调用：校验令牌和运行 → 取模型快照和渠道 Key → 预检预算与余额 → 透传流式响应 → 按用量结算。
// write 收到上游的每一行 SSE（原样）。错误发生在流开始之前时 write 一次都没调用，调用方据此返回 HTTP 错误码。
// 结算用不会被取消的上下文：调用方断开（用户停止）时，已经产生的部分也要照样收费。
func (b *AgentBridge) ProxyModel(ctx context.Context, token string, body []byte, write func([]byte)) error {
	sess, err := b.session(token)
	if err != nil {
		return err
	}
	run, err := b.activeRun(ctx, sess)
	if err != nil {
		return err
	}
	snap, key, err := b.resolveModel(ctx, sess.modelKey)
	if err != nil {
		return err
	}
	inputChars := utf8.RuneCount(body)
	if err := b.d.Billing.Preflight(ctx, run, snap.Model.Pricing, snap.Model.Capabilities, inputChars); err != nil {
		var ec *errcode.Error
		if errors.As(err, &ec) && ec.Code == errcode.ErrAgentOverBudget.Code {
			b.pauseRun(ctx, run, model.RunBudgetExhausted)
		}
		return err
	}
	call, err := b.d.Billing.Begin(ctx, run, sess.modelKey)
	if err != nil {
		return err
	}
	tg := llmgateway.Target{ChannelKey: snap.Channel.Key, BaseURL: snap.Channel.BaseURL, APIKey: key, UpstreamModel: snap.Model.UpstreamModel,
		TrustedInternal: snap.Channel.TrustedInternal, RPS: snap.Channel.RateLimit.RPS, MaxConcurrency: snap.Channel.RateLimit.MaxConcurrency}
	lim := llmgateway.RawLimits{}
	if c := snap.Model.Capabilities.Context; c != nil {
		lim.MaxTokens = c.Output
	}

	deltas := &deltaBuffer{flush: func(text, thinking string) {
		if text != "" {
			b.d.Agent.push(ctx, run.UserID, run.SessionID, run.ID, run.CanvasID, "message.delta", map[string]any{"text": text})
		}
		if thinking != "" {
			b.d.Agent.push(ctx, run.UserID, run.SessionID, run.ID, run.CanvasID, "thinking.delta", map[string]any{"text": thinking})
		}
	}}
	res, serr := b.d.Streamer.StreamRaw(ctx, tg, body, lim, deltas.onEvent, write)
	deltas.close()

	settled, berr := b.d.Billing.Settle(context.WithoutCancel(ctx), run, call, snap.Model.Pricing, CallOutcome{Result: res, InputChars: inputChars, Err: serr})
	if berr != nil {
		// 结算失败意味着这次调用没收到钱，只能留下线索供对账补扣；不能因此让用户的对话失败
		logger.Error("Agent 对话结算失败", zap.Error(berr), zap.Uint64("call_id", call.ID), zap.Uint64("run_id", run.ID))
	}
	b.recordMessage(ctx, run, res, settled, serr != nil)
	return serr
}

// resolveModel 取模型当前发布版本的快照和渠道 Key。不可用、不是 agent 种类、Key 没设置，都按「Agent 模型不可用」处理。
func (b *AgentBridge) resolveModel(ctx context.Context, modelKey string) (*provider.Snapshot, string, error) {
	snap, err := b.d.Registry.Snapshot(ctx, modelKey)
	if errors.Is(err, provider.ErrModelUnavailable) {
		return nil, "", errcode.ErrAgentModelNA
	}
	if err != nil {
		return nil, "", err
	}
	if snap.Model.Kind != "agent" {
		return nil, "", errcode.ErrAgentModelNA
	}
	key, err := b.d.Secrets.Get(ctx, model.ChannelSecretName(snap.Channel.Key))
	if err != nil {
		logger.Warn("Agent 渠道 Key 读取失败", zap.String("channel", snap.Channel.Key), zap.Error(err))
		return nil, "", errcode.ErrAgentModelNA
	}
	return snap, key, nil
}

// recordMessage 在一次调用结束后记一条 message.done：完整正文、思考和发起的工具调用。没有任何产出不记。
func (b *AgentBridge) recordMessage(ctx context.Context, run *model.AgentRun, res *llmgateway.Result, settled *SettleResult, partial bool) {
	if res == nil || (res.Text == "" && len(res.ToolCalls) == 0) {
		return
	}
	calls := make([]map[string]string, len(res.ToolCalls))
	for i, c := range res.ToolCalls {
		calls[i] = map[string]string{"id": c.ID, "name": c.Name}
	}
	data := map[string]any{"text": res.Text, "tool_calls": calls, "partial": partial}
	if res.Thinking != "" {
		data["thinking"] = res.Thinking
	}
	if settled != nil {
		data["credits"], data["estimated"] = settled.Charged, settled.Estimated
	}
	b.d.Agent.emit(ctx, run.UserID, run.SessionID, run.ID, run.CanvasID, "message.done", data)
}

// deltaBuffer 把文本和思考增量合并后再推送，最多每 50ms 一次：前端不需要每个 Token 一次渲染。
type deltaBuffer struct {
	flush    func(text, thinking string)
	text     []byte
	thinking []byte
	last     time.Time
}

func (d *deltaBuffer) onEvent(e llmgateway.Event) {
	switch e.Type {
	case llmgateway.EventText:
		d.text = append(d.text, e.Text...)
	case llmgateway.EventThinking:
		d.thinking = append(d.thinking, e.Text...)
	default:
		return
	}
	if time.Since(d.last) >= deltaFlushEvery {
		d.close()
	}
}

// close 把还没推送的增量推出去。
func (d *deltaBuffer) close() {
	if len(d.text) == 0 && len(d.thinking) == 0 {
		return
	}
	d.flush(string(d.text), string(d.thinking))
	d.text, d.thinking, d.last = d.text[:0], d.thinking[:0], time.Now()
}

// MarkRunning 把刚创建（或刚继续）的运行从 queued 标为 running，由运行时在真正启动进程前调用。
func (b *AgentBridge) MarkRunning(ctx context.Context, run *model.AgentRun) error {
	if _, err := b.d.Repo.UpdateRunIf(ctx, run.ID, []string{model.RunQueued}, map[string]any{"status": model.RunRunning}); err != nil {
		return errcode.ErrAgentState
	}
	b.d.Agent.emit(ctx, run.UserID, run.SessionID, run.ID, run.CanvasID, "run.status", map[string]any{"status": model.RunRunning})
	return nil
}

// SaveState 保存 Node 在回合结束时交回的对话历史（pi 的 state.messages，JSON 数组）。
func (b *AgentBridge) SaveState(ctx context.Context, token string, messages json.RawMessage) error {
	sess, err := b.session(token)
	if err != nil {
		return err
	}
	if len(messages) > maxStateBytes || !json.Valid(messages) || len(messages) == 0 || messages[0] != '[' {
		return errcode.ErrInvalidParams.WithMsg("对话历史必须是不超过 8MB 的 JSON 数组")
	}
	return sessionErr(b.d.Repo.UpdateSession(ctx, sess.userID, sess.sessionID, map[string]any{"session_jsonl": string(messages)}))
}

// LoadState 读出会话保存的对话历史，启动 Node 进程时交给它；没有历史返回空串。
func (b *AgentBridge) LoadState(ctx context.Context, userID, sessionID uint64) (string, error) {
	sess, err := b.d.Repo.GetSession(ctx, userID, sessionID)
	if err != nil {
		return "", sessionErr(err)
	}
	return sess.SessionJSONL, nil
}

// Finish 处理 Node 进程报告的片段结束：done → 运行成功，error → 运行失败。
// 只有运行还是 running 时才改：等审批、等回答、预算用尽、已停止的运行保持原状态（它们是别的流程设定的）。令牌随即失效。
func (b *AgentBridge) Finish(ctx context.Context, token, status, message string) error {
	sess, err := b.session(token)
	if err != nil {
		return err
	}
	defer b.RevokeToken(token)
	run, err := b.d.Repo.GetRun(ctx, sess.userID, sess.runID)
	if err != nil {
		return runErr(err)
	}
	fields := map[string]any{"ended_at": b.d.Now()}
	switch status {
	case "paused":
		// 因工具要求停下（等审批、等回答、步数用尽）而结束：运行的状态是别的流程设好的，这里只收回令牌。
		// 不能改状态：用户批准得快时，运行可能已经被续跑放回 running，旧进程的收尾不能误伤新片段
		return nil
	case "done":
		fields["status"] = model.RunSucceeded
	case "error":
		fields["status"], fields["error_code"], fields["error_message"] = model.RunFailed, "agent_error", truncateRunes(message, 200)
	default:
		return errcode.ErrInvalidParams.WithMsg("status 只能是 done、paused 或 error")
	}
	updated, err := b.d.Repo.UpdateRunIf(ctx, run.ID, []string{model.RunRunning}, fields)
	if err != nil {
		return nil // 不在 running 了：保持别的流程设定的状态
	}
	b.d.Agent.emit(ctx, run.UserID, run.SessionID, run.ID, run.CanvasID, "run.status", map[string]any{"status": updated.Status, "error": updated.ErrorMessage})
	return nil
}
