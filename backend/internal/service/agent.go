package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"
	"gorm.io/datatypes"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/pkg/ws"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
)

const (
	maxAgentSessionsPerCanvas = 50    // 每个画布最多的会话数
	defaultAgentTitle         = "新对话" // 新会话的默认标题
	defaultRunBudget          = 50    // 一轮运行的默认积分预算
	defaultRunMaxSteps        = 40    // 一轮运行最多的工具调用步数
	approvalTTL               = 24 * time.Hour
	maxEventPage              = 200 // 回放事件一页最多多少条
)

// AgentRepo 是会话、运行、事件、审批所需的数据访问。
type AgentRepo interface {
	// GetCanvas 按 id + user_id 读画布，不存在或不属于该用户返回 repository.ErrNotFound。
	GetCanvas(ctx context.Context, userID, id uint64) (*model.CanvasProject, error)
	// CreateSession 创建会话。
	CreateSession(ctx context.Context, s *model.AgentSession) error
	// GetSession 按 id + user_id 读会话，不存在或不属于该用户返回 repository.ErrNotFound。
	GetSession(ctx context.Context, userID, id uint64) (*model.AgentSession, error)
	// ListSessions 列出用户在某画布上的会话，最近更新的在前。
	ListSessions(ctx context.Context, userID, canvasID uint64) ([]model.AgentSession, error)
	// CountSessions 统计用户在某画布上的会话数。
	CountSessions(ctx context.Context, userID, canvasID uint64) (int64, error)
	// UpdateSession 更新会话的指定字段，没有命中返回 repository.ErrNotFound。
	UpdateSession(ctx context.Context, userID, id uint64, fields map[string]any) error
	// DeleteSession 软删除会话，没有命中返回 repository.ErrNotFound。
	DeleteSession(ctx context.Context, userID, id uint64) error
	// CreateRun 创建运行；画布上已有活跃运行时返回 repository.ErrDuplicate。
	CreateRun(ctx context.Context, run *model.AgentRun) error
	// GetRun 按 id + user_id 读运行，不存在或不属于该用户返回 repository.ErrNotFound。
	GetRun(ctx context.Context, userID, id uint64) (*model.AgentRun, error)
	// ActiveRun 读某画布上的活跃运行，没有返回 repository.ErrNotFound。
	ActiveRun(ctx context.Context, canvasID uint64) (*model.AgentRun, error)
	// UpdateRunIf 是运行状态迁移的 CAS，没命中返回 repository.ErrAgentStateConflict。
	UpdateRunIf(ctx context.Context, id uint64, from []string, fields map[string]any) (*model.AgentRun, error)
	// AddRunUsage 累加运行的步数和已花积分。
	AddRunUsage(ctx context.Context, id uint64, steps, credits int) error
	// AppendEvent 给会话追加一条事件，序号原子分配。
	AppendEvent(ctx context.Context, sessionID, runID uint64, typ string, payload datatypes.JSON) (*model.AgentEvent, error)
	// ListEvents 列出会话里序号大于 afterSeq 的事件。
	ListEvents(ctx context.Context, sessionID uint64, afterSeq int64, limit int) ([]model.AgentEvent, error)
	// CreateApproval 创建审批。
	CreateApproval(ctx context.Context, a *model.AgentApproval) error
	// GetApproval 按 id + user_id 读审批，不存在或不属于该用户返回 repository.ErrNotFound。
	GetApproval(ctx context.Context, userID, id uint64) (*model.AgentApproval, error)
	// UpdateApprovalIf 是审批状态迁移的 CAS，没命中返回 repository.ErrAgentStateConflict。
	UpdateApprovalIf(ctx context.Context, id uint64, from []string, fields map[string]any) (*model.AgentApproval, error)
	// ExpirePendingApprovals 把某运行所有待处理的审批置为已失效。
	ExpirePendingApprovals(ctx context.Context, runID uint64) (int64, error)
}

// AgentModels 提供已发布的 Agent 模型清单。
type AgentModels interface {
	// List 返回当前可用的 Agent 模型；没有任何已发布的模型时返回空。
	List(ctx context.Context) ([]model.AgentModelView, error)
}

// AgentRuntime 是 Agent 运行时（Node 里的 pi 循环）的控制接口。
type AgentRuntime interface {
	// Start 启动一轮运行，运行不可用时返回错误。
	Start(ctx context.Context, run *model.AgentRun, in model.AgentRunInput) error
	// Interject 向运行中的会话插话。
	Interject(ctx context.Context, runID uint64, message string) error
	// Cancel 中止运行，运行已经不在时不是错误。
	Cancel(ctx context.Context, runID uint64) error
	// Resume 在用户决定之后、或点「继续」之后让运行接着往下走。
	Resume(ctx context.Context, run *model.AgentRun, info ResumeInfo) error
}

// ResumeInfo 是续跑运行时交给运行时的信息。
type ResumeInfo struct {
	Reason     string // approval 是用户对审批做了决定；resume 是用户点了「继续」
	ToolCallID string // approval：要补上结果的那个工具调用
	Content    string // approval：给模型看的工具结果，说明用户批准了什么、拒绝了什么、回答了什么
}

// AgentService 是画布 Agent 的会话、运行、审批业务。改画布本身在 AgentCanvasService。
type AgentService struct {
	repo    AgentRepo
	canvas  *AgentCanvasService
	models  AgentModels
	runtime AgentRuntime
	exec    AgentGenerationExecutor
	// registry 用来校验消息里引用的生成模型；为 nil 时不校验模型引用
	registry provider.Registry
	bc       ws.Broadcaster
	now      func() time.Time
}

// AgentDeps 是创建 AgentService 需要的依赖。
type AgentDeps struct {
	Repo        AgentRepo               // 数据访问
	Canvas      *AgentCanvasService     // 改画布
	Models      AgentModels             // Agent 模型清单
	Runtime     AgentRuntime            // 运行时
	Generator   AgentGenerationExecutor // 批准生成后执行，为 nil 时只记录审批不执行
	Registry    provider.Registry       // 校验消息里引用的生成模型，为 nil 时不校验
	Broadcaster ws.Broadcaster          // 为 nil 时不推送
	Now         func() time.Time        // 为 nil 取系统时间，测试里注入
}

// NewAgentService 创建服务。
func NewAgentService(d AgentDeps) *AgentService {
	s := &AgentService{repo: d.Repo, canvas: d.Canvas, models: d.Models, runtime: d.Runtime, exec: d.Generator, registry: d.Registry, bc: d.Broadcaster, now: d.Now}
	if s.bc == nil {
		s.bc = ws.NopBroadcaster{}
	}
	if s.now == nil {
		s.now = time.Now
	}
	return s
}

// SetRuntime 绑定运行时。AgentService 和运行时互相依赖（运行时要写事件，AgentService 要启动运行），
// 装配时先创建 AgentService、再创建运行时、最后用它绑定。
func (s *AgentService) SetRuntime(rt AgentRuntime) { s.runtime = rt }

// Models 返回当前可用的 Agent 模型清单。
func (s *AgentService) Models(ctx context.Context) ([]model.AgentModelView, error) {
	list, err := s.models.List(ctx)
	if list == nil {
		list = []model.AgentModelView{}
	}
	return list, err
}

// ListSessions 列出画布上属于当前用户的会话。
func (s *AgentService) ListSessions(ctx context.Context, userID, canvasID uint64) ([]*AgentSessionView, error) {
	// 1. 先确认画布属于当前用户：别人的画布和不存在的画布统一返回「画布不存在」
	if err := s.checkCanvas(ctx, userID, canvasID); err != nil {
		return nil, err
	}
	// 2. 列出会话，最近更新的在前（不含大字段 session_jsonl）
	items, err := s.repo.ListSessions(ctx, userID, canvasID)
	if err != nil {
		return nil, err
	}
	out := make([]*AgentSessionView, len(items))
	for i := range items {
		out[i] = sessionView(&items[i])
	}
	return out, nil
}

// CreateSession 新建会话。
func (s *AgentService) CreateSession(ctx context.Context, userID, canvasID uint64, req *model.CreateAgentSessionReq) (*AgentSessionView, error) {
	// 1. 确认画布属于当前用户
	if err := s.checkCanvas(ctx, userID, canvasID); err != nil {
		return nil, err
	}
	// 2. 每个画布最多 50 个会话，避免历史无限增长
	n, err := s.repo.CountSessions(ctx, userID, canvasID)
	if err != nil {
		return nil, err
	}
	if n >= maxAgentSessionsPerCanvas {
		return nil, errcode.ErrAgentSessionLimit
	}
	// 3. 补默认值：标题「新对话」、模式「全能创作」
	sess := &model.AgentSession{UserID: userID, CanvasID: canvasID, Title: cleanTitle(req.Title, defaultAgentTitle), Mode: req.Mode, ModelKey: req.ModelKey}
	if sess.Mode == "" {
		sess.Mode = model.AgentModeAll
	}
	if err := s.repo.CreateSession(ctx, sess); err != nil {
		return nil, err
	}
	return sessionView(sess), nil
}

// RenameSession 重命名会话。
func (s *AgentService) RenameSession(ctx context.Context, userID, sessionID uint64, title string) (*AgentSessionView, error) {
	// 1. 规整标题：去首尾空白、多空白收成一个、最长 40 字；清空后不允许
	t := cleanTitle(title, "")
	if t == "" {
		return nil, errcode.ErrInvalidParams.WithMsg("标题不能为空")
	}
	// 2. 按 id + user_id 更新，别人的会话统一返回「会话不存在」
	if err := s.repo.UpdateSession(ctx, userID, sessionID, map[string]any{"title": t}); err != nil {
		return nil, sessionErr(err)
	}
	sess, err := s.repo.GetSession(ctx, userID, sessionID)
	if err != nil {
		return nil, sessionErr(err)
	}
	return sessionView(sess), nil
}

// DeleteSession 删除会话（软删除）。会话里还有运行中的任务时不允许删，免得 Agent 往一个看不见的会话里继续写。
func (s *AgentService) DeleteSession(ctx context.Context, userID, sessionID uint64) error {
	// 1. 会话必须是自己的
	sess, err := s.repo.GetSession(ctx, userID, sessionID)
	if err != nil {
		return sessionErr(err)
	}
	// 2. 这个会话在占用画布时不能删
	if active, err := s.repo.ActiveRun(ctx, sess.CanvasID); err == nil && active.SessionID == sessionID {
		return errcode.ErrAgentRunActive
	} else if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return err
	}
	// 3. 软删除
	return sessionErr(s.repo.DeleteSession(ctx, userID, sessionID))
}

// Events 回放会话里序号大于 after 的事件，断线重连后前端用它对账。
func (s *AgentService) Events(ctx context.Context, userID, sessionID uint64, after int64, limit int) ([]*AgentEventView, error) {
	// 1. 会话必须是自己的
	if _, err := s.repo.GetSession(ctx, userID, sessionID); err != nil {
		return nil, sessionErr(err)
	}
	// 2. 修正条数：<=0 取最大值，最大 200
	if limit <= 0 || limit > maxEventPage {
		limit = maxEventPage
	}
	items, err := s.repo.ListEvents(ctx, sessionID, after, limit)
	if err != nil {
		return nil, err
	}
	out := make([]*AgentEventView, len(items))
	for i := range items {
		out[i] = eventView(&items[i])
	}
	return out, nil
}

// checkCanvas 确认画布属于该用户。
func (s *AgentService) checkCanvas(ctx context.Context, userID, canvasID uint64) error {
	if _, err := s.repo.GetCanvas(ctx, userID, canvasID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return errcode.ErrCanvasNotFound
		}
		return err
	}
	return nil
}

// emit 追加一条事件并推送给该用户。写事件失败只记日志：动作本身已经成功，丢一条事件不该让用户的操作失败，
// 前端会按 seq 对账补回。
func (s *AgentService) emit(ctx context.Context, userID, sessionID, runID, canvasID uint64, typ string, data any) {
	payload, err := json.Marshal(data)
	if err != nil {
		logger.Error("序列化 Agent 事件失败", zap.Error(err), zap.String("type", typ))
		return
	}
	ev, err := s.repo.AppendEvent(ctx, sessionID, runID, typ, payload)
	if err != nil {
		logger.Error("写 Agent 事件失败", zap.Error(err), zap.String("type", typ), zap.Uint64("session_id", sessionID))
		return
	}
	// 推到用户频道而不是画布频道：用户频道连接时自动订阅，不需要订阅授权（默认拒绝所有手动订阅）；
	// 画布频道名用的是原始数字 id，前端手里只有编码串，也拼不出来。前端按事件里的 canvas_id 过滤
	channel := ws.UserChannel(userID)
	v := eventView(ev)
	v.CanvasID = idcodec.ID(canvasID)
	s.bc.Publish(ctx, channel, ws.Message{Type: ws.TypeAgentEvent, Channel: channel, Data: v})
}

// push 只推送不落库的临时事件（文本、思考增量）：每个 Token 一次写库代价太大，而且增量丢了不要紧，
// 结束时的 message.done 带着完整内容，前端按它对账。seq 为 0 表示这是临时事件，不参与回放。
func (s *AgentService) push(ctx context.Context, userID, sessionID, runID, canvasID uint64, typ string, data any) {
	payload, err := json.Marshal(data)
	if err != nil {
		logger.Error("序列化 Agent 临时事件失败", zap.Error(err), zap.String("type", typ))
		return
	}
	v := &AgentEventView{SessionID: idcodec.ID(sessionID), CanvasID: idcodec.ID(canvasID), Seq: 0, Type: typ, Data: payload, CreatedAt: s.now()}
	if runID != 0 {
		id := idcodec.ID(runID)
		v.RunID = &id
	}
	channel := ws.UserChannel(userID)
	s.bc.Publish(ctx, channel, ws.Message{Type: ws.TypeAgentEvent, Channel: channel, Data: v})
}

// cleanTitle 规整标题：多空白收成一个，最长 40 字，为空时取 def。
func cleanTitle(raw, def string) string {
	t := strings.Join(strings.Fields(raw), " ")
	if utf8.RuneCountInString(t) > 40 {
		t = string([]rune(t)[:40])
	}
	if t == "" {
		return def
	}
	return t
}

// sessionErr 把仓储的「不存在」翻译成会话不存在，其余原样返回。
func sessionErr(err error) error {
	if errors.Is(err, repository.ErrNotFound) {
		return errcode.ErrAgentSessionMissing
	}
	return err
}

// runErr 把仓储的「不存在」翻译成运行不存在，其余原样返回。
func runErr(err error) error {
	if errors.Is(err, repository.ErrNotFound) {
		return errcode.ErrAgentRunMissing
	}
	return err
}

// Undo 撤销某个运行对画布的全部改动，业务在 AgentCanvasService.Undo。
func (s *AgentService) Undo(ctx context.Context, userID, runID uint64) (*UndoResult, error) {
	return s.canvas.Undo(ctx, userID, runID)
}
