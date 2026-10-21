package agent

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"
	"gorm.io/datatypes"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/repository"
)

// 观测的固定参数。
const (
	maxFeedbackComment  = 500                 // 反馈说明最多多少字
	maxFeedbackReasons  = 5                   // 反馈原因标签最多几个
	maxTokenNameRunes   = 64                  // 令牌用途最多多少字
	maxTokenDays        = 90                  // 令牌最长有效天数
	tokenRatePerMinute  = 120                 // 每个令牌每分钟最多请求数：pull 一条运行要两次请求（md 和 json 合并成一次后是一次），留足余量
	tokenTouchInterval  = time.Minute         // 最近使用时间最多一分钟写一次库，免得每次请求都写
	observeRetention    = 30 * 24 * time.Hour // 工具参数、结果和 runtime_tail 保留 30 天
	requestHeadRunes    = 200                 // 摘要里用户请求首句的最大字数
	maxStatusFilter     = 20                  // 列表按状态筛选最多几个
	analysisTokenRandom = 32                  // 令牌随机部分的字节数
)

// ObserveRepo 是观测所需的数据访问，由 repository.AgentObserveRepository 实现。
type ObserveRepo interface {
	// RunFacts 读生成运行摘要需要的事实，运行不存在返回 repository.ErrNotFound。
	RunFacts(ctx context.Context, runID uint64) (*model.AgentRunFacts, error)
	// UpsertDigest 写运行摘要，一次运行一行，整行覆盖。
	UpsertDigest(ctx context.Context, d *model.AgentRunDigest) error
	// GetDigest 读一条运行摘要，没有返回 repository.ErrNotFound。
	GetDigest(ctx context.Context, runID uint64) (*model.AgentRunDigest, error)
	// ListDigests 按创建时间倒序游标分页；游标无效返回 repository.ErrInvalidCursor。
	ListDigests(ctx context.Context, q model.DigestQuery) ([]model.AgentRunDigest, string, error)
	// ExportFacts 读导出一次运行的全部事实，运行不存在返回 repository.ErrNotFound。
	ExportFacts(ctx context.Context, runID uint64) (*model.AgentExportFacts, error)
	// Stats 按版本或模型分组聚合指标；分组字段不在白名单返回 repository.ErrInvalidGroupBy。
	Stats(ctx context.Context, q model.StatsQuery) ([]model.AgentStatsRow, error)
	// GetFeedback 读某人对某运行的评价，没有返回 repository.ErrNotFound。
	GetFeedback(ctx context.Context, runID, userID uint64) (*model.AgentFeedback, error)
	// UpsertFeedback 写评价，同一人对同一运行只有一条。
	UpsertFeedback(ctx context.Context, fb *model.AgentFeedback) error
	// DeleteFeedback 取消评价，本来就没有不是错误。
	DeleteFeedback(ctx context.Context, runID, userID uint64) error
	// PurgeObserveBefore 清空早于 cutoff 的工具参数、结果和 runtime_tail，返回两类各清了多少行。
	PurgeObserveBefore(ctx context.Context, cutoff, now time.Time) (toolCalls, digests int64, err error)
	// CreateToken 保存新签发的分析令牌。
	CreateToken(ctx context.Context, t *model.AdminAPIToken) error
	// ListTokens 列出分析令牌（不含已注销的），新的在前。
	ListTokens(ctx context.Context) ([]model.AdminAPIToken, error)
	// TokenByHash 按明文的 sha256 查令牌，没有返回 repository.ErrNotFound。
	TokenByHash(ctx context.Context, hash string) (*model.AdminAPIToken, error)
	// TouchToken 记下令牌最近一次使用的时间。
	TouchToken(ctx context.Context, id uint64, at time.Time) error
	// RevokeToken 注销令牌，重复注销是幂等的；令牌不存在返回 repository.ErrNotFound。
	RevokeToken(ctx context.Context, id uint64, at time.Time) error
}

// ObserveRuns 按用户读运行：反馈只能对自己的运行打。
type ObserveRuns interface {
	// GetRun 按 id + user_id 读运行，不存在或不属于该用户返回 repository.ErrNotFound。
	GetRun(ctx context.Context, userID, id uint64) (*model.AgentRun, error)
}

// EventEmitter 给会话追加一条事件并推送给用户（*AgentService 实现它）。
type EventEmitter interface {
	// Emit 追加一条落库的事件并推送。
	Emit(ctx context.Context, userID, sessionID, runID, canvasID uint64, typ string, data any)
}

// AuditWriter 写后台运营审计（只增不改不删），由 repository.AdminAuditRepository 实现。
type AuditWriter interface {
	// Insert 写一条审计日志。
	Insert(ctx context.Context, l *model.AdminAuditLog) error
}

// RoleLookup 按用户 ID 查角色；用户不存在返回空串。
type RoleLookup func(ctx context.Context, userID uint64) (string, error)

// ObserveDeps 是创建 ObserveService 需要的依赖。
type ObserveDeps struct {
	Repo   ObserveRepo      // 观测数据访问
	Runs   ObserveRuns      // 按用户读运行
	Events EventEmitter     // 发反馈事件；为 nil 时不发
	Audit  AuditWriter      // 审计；为 nil 时不写
	Roles  RoleLookup       // 令牌签发人的角色查询
	Now    func() time.Time // 为 nil 取系统时间
}

// ObserveService 是 Agent 运行观测：运行摘要、用户反馈、导出、统计和分析令牌。
// 它记录和读取的都是观测数据，出错时不能影响用户正在进行的运行。
type ObserveService struct {
	d ObserveDeps

	mu      sync.Mutex
	windows map[uint64]*rateWindow // 令牌 id → 当前分钟的请求计数
	touched map[uint64]time.Time   // 令牌 id → 上次写最近使用时间的时刻
}

// rateWindow 是一个令牌当前一分钟窗口内的请求数。
type rateWindow struct {
	start time.Time
	n     int
}

// NewObserveService 创建服务。
func NewObserveService(d ObserveDeps) *ObserveService {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &ObserveService{d: d, windows: map[uint64]*rateWindow{}, touched: map[uint64]time.Time{}}
}

// skillRefPattern 取工具摘要里的技能引用：「读取技能 x@v2」或「读取技能文件 x@v2/path」。
var skillRefPattern = regexp.MustCompile(`^读取技能(?:文件)? ([^/\s]+)`)

// BuildDigest 把一次运行的事实汇总成摘要。纯函数：只数数和分类，不访问存储。
func BuildDigest(f *model.AgentRunFacts, now time.Time) *model.AgentRunDigest {
	run := &f.Run
	d := &model.AgentRunDigest{
		RunID: run.ID, SessionID: run.SessionID, UserID: run.UserID, CanvasID: run.CanvasID, Mode: run.Mode,
		PromptVersion: run.PromptVersion, ToolsHash: run.ToolsHash, BuildSHA: run.BuildSHA, HarnessKey: run.HarnessKey,
		Status: run.Status, ErrorCode: run.ErrorCode, Steps: run.Steps, SpentCredits: run.SpentCredits,
		ToolCalls: len(f.ToolCalls), ModelCalls: len(f.ModelCalls), ApprovalsTotal: len(f.Approvals),
		EndedAt: run.EndedAt, CreatedAt: run.CreatedAt, UpdatedAt: now,
	}
	if run.EndedAt != nil {
		d.DurationMs = run.EndedAt.Sub(run.CreatedAt).Milliseconds()
	}
	digestTools(d, f.ToolCalls)
	digestModelCalls(d, f.ModelCalls)
	digestMutations(d, f.Mutations)
	digestUserMessages(d, f.UserMessages)
	for _, a := range f.Approvals {
		if a.Status == model.ApprovalRejected {
			d.ApprovalsRejected++
		}
	}
	if f.Feedback != nil {
		d.Feedback = f.Feedback.Rating
	}
	if f.RuntimeTail != "" {
		tail := f.RuntimeTail
		d.RuntimeTail = &tail
	}
	return d
}

// digestTools 数工具报错次数，并从技能读取的摘要里取出这次运行读过的技能及版本。
func digestTools(d *model.AgentRunDigest, calls []model.AgentToolCall) {
	skills := map[string]string{}
	for _, c := range calls {
		if c.IsError {
			d.ToolErrors++
			continue // 读失败的技能不算读过
		}
		if c.Name != "skill_read" {
			continue
		}
		if name, ver, ok := parseSkillRef(c.Summary); ok {
			skills[name] = ver
		}
	}
	d.Skills = mustJSON(skills, "{}")
}

// digestModelCalls 汇总模型调用的 Token，并取第一次调用用的模型。摘要调用也计入：它们同样花钱。
func digestModelCalls(d *model.AgentRunDigest, calls []model.AgentModelCall) {
	for _, m := range calls {
		d.InputTokens += m.InputTokens
		d.OutputTokens += m.OutputTokens
		if d.ModelKey == "" {
			d.ModelKey = m.ModelKey
		}
	}
}

// digestMutations 数画布改动：撤销本身不算一次改动，但说明用户撤销过；原改动被标了撤销时间也一样。
func digestMutations(d *model.AgentRunDigest, muts []model.AgentMutation) {
	for _, m := range muts {
		if m.Kind == model.MutationUndo {
			d.Undone = true
			continue
		}
		d.MutationCount++
		if m.UndoneAt != nil {
			d.Undone = true
		}
	}
}

// digestUserMessages 数用户插话，并取第一条非插话的消息当请求首句。损坏的载荷跳过，不影响其他统计。
func digestUserMessages(d *model.AgentRunDigest, evs []model.AgentEvent) {
	for _, ev := range evs {
		var p struct {
			Text  string `json:"text"`
			Steer bool   `json:"steer"`
		}
		if json.Unmarshal(ev.PayloadJSON, &p) != nil {
			continue
		}
		if p.Steer {
			d.SteerCount++
		} else if d.RequestHead == "" {
			d.RequestHead = truncateRunes(p.Text, requestHeadRunes)
		}
	}
}

// parseSkillRef 从技能读取的摘要里取技能名和版本：导入技能是 x@v2，内置技能没有版本号记 builtin。
func parseSkillRef(summary string) (name, version string, ok bool) {
	m := skillRefPattern.FindStringSubmatch(summary)
	if m == nil {
		return "", "", false
	}
	ref := m[1]
	if n, v, found := strings.Cut(ref, "@"); found {
		return n, v, true
	}
	return ref, "builtin", true
}

// mustJSON 把值编码成 JSON；编码失败（不会发生）时用兜底。
func mustJSON(v any, fallback string) datatypes.JSON {
	b, err := json.Marshal(v)
	if err != nil {
		return datatypes.JSON(fallback)
	}
	return b
}

// RefreshDigest 重新汇总一次运行并写入摘要。运行进入非活跃状态、用户评价或撤销之后调用。
// 返回错误由调用方决定怎么处理：事件钩子里只记日志，不能让用户的运行因观测数据失败。
func (s *ObserveService) RefreshDigest(ctx context.Context, runID uint64) error {
	f, err := s.d.Repo.RunFacts(ctx, runID)
	if err != nil {
		return err
	}
	return s.d.Repo.UpsertDigest(ctx, BuildDigest(f, s.d.Now()))
}

// maxRuntimeTailBytes 是 runtime_tail 的最大长度，与进程里保留的 stderr 末尾一致。
const maxRuntimeTailBytes = 4 << 10

// RecordRuntimeTail 把 Node 进程 stderr 的末尾写进运行摘要（最多 4KB，不切坏字符）。空串不写。
// 摘要不存在时顺带创建：进程退出可能早于运行进入终态的那次刷新；之后的刷新会沿用这段文字。
func (s *ObserveService) RecordRuntimeTail(ctx context.Context, runID uint64, tail string) error {
	if tail == "" {
		return nil
	}
	f, err := s.d.Repo.RunFacts(ctx, runID)
	if err != nil {
		return err
	}
	if len(tail) > maxRuntimeTailBytes {
		// 保留末尾：诊断信息最要紧的是最后几行
		tail = tail[len(tail)-maxRuntimeTailBytes:]
		for len(tail) > 0 && !utf8.RuneStart(tail[0]) {
			tail = tail[1:]
		}
	}
	f.RuntimeTail = tail
	return s.d.Repo.UpsertDigest(ctx, BuildDigest(f, s.d.Now()))
}

// refreshQuietly 刷新摘要，失败只记日志。用不会被取消的上下文：用户停止运行时请求的上下文已取消，摘要照样要写。
func (s *ObserveService) refreshQuietly(ctx context.Context, runID uint64) {
	if err := s.RefreshDigest(context.WithoutCancel(ctx), runID); err != nil {
		logger.Warn("刷新 Agent 运行摘要失败", zap.Error(err), zap.Uint64("run_id", runID))
	}
}

// FeedbackView 是返回给前端的评价。
type FeedbackView struct {
	Rating  int      `json:"rating"`  // 1 有帮助，-1 没帮上忙
	Reasons []string `json:"reasons"` // 点踩原因标签
	Comment string   `json:"comment"` // 补充说明
}

// SetFeedback 记下用户对一次运行的评价（👍/👎），再次调用就是改选。
func (s *ObserveService) SetFeedback(ctx context.Context, userID, runID uint64, req *model.SetAgentFeedbackReq) (*FeedbackView, error) {
	// 1. 参数：评分只能是 1/-1；原因必须是已知标签，去重，点赞时忽略；说明最多 500 字
	if req.Rating != model.FeedbackUp && req.Rating != model.FeedbackDown {
		return nil, errcode.ErrInvalidParams.WithMsg("评价只能是 1 或 -1")
	}
	reasons, err := normalizeReasons(req.Rating, req.Reasons)
	if err != nil {
		return nil, err
	}
	comment := strings.TrimSpace(req.Comment)
	if utf8.RuneCountInString(comment) > maxFeedbackComment {
		return nil, errcode.ErrInvalidParams.WithMsg("补充说明最多 500 字")
	}
	// 2. 运行必须是自己的，别人的和不存在的统一返回「运行不存在」；还在跑或在等用户时不能评价，评价的是一整轮的结果
	run, err := s.d.Runs.GetRun(ctx, userID, runID)
	if err != nil {
		return nil, runErr(err)
	}
	if model.IsActiveRun(run.Status) {
		return nil, errcode.ErrAgentRunNotEnded
	}
	// 3. 写评价（同一人对同一运行覆盖），发事件让其他标签页和刷新后的页面看到，再同步摘要
	fb := &model.AgentFeedback{RunID: runID, UserID: userID, Rating: req.Rating, Reasons: mustJSON(reasons, "[]"), Comment: comment}
	if err := s.d.Repo.UpsertFeedback(ctx, fb); err != nil {
		return nil, err
	}
	view := &FeedbackView{Rating: req.Rating, Reasons: reasons, Comment: comment}
	// 载荷里不放原始 run_id：前端按事件自带的编码后的 run_id 对账，放原始数字反而会对不上
	s.emit(ctx, run, "feedback.set", view)
	s.refreshQuietly(ctx, runID)
	return view, nil
}

// ClearFeedback 取消评价。本来就没有评价是幂等的，不发事件。
func (s *ObserveService) ClearFeedback(ctx context.Context, userID, runID uint64) error {
	run, err := s.d.Runs.GetRun(ctx, userID, runID)
	if err != nil {
		return runErr(err)
	}
	if _, err := s.d.Repo.GetFeedback(ctx, runID, userID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil
		}
		return err
	}
	if err := s.d.Repo.DeleteFeedback(ctx, runID, userID); err != nil {
		return err
	}
	s.emit(ctx, run, "feedback.cleared", map[string]any{})
	s.refreshQuietly(ctx, runID)
	return nil
}

// emit 给运行所在会话发一条事件；没有配置发射器时不发。
func (s *ObserveService) emit(ctx context.Context, run *model.AgentRun, typ string, data any) {
	if s.d.Events != nil {
		s.d.Events.Emit(ctx, run.UserID, run.SessionID, run.ID, run.CanvasID, typ, data)
	}
}

// normalizeReasons 校验并整理点踩原因：未知标签报参数错误，重复的去掉，点赞时一律忽略。
func normalizeReasons(rating int, in []string) ([]string, error) {
	out := []string{}
	if rating != model.FeedbackDown {
		return out, nil
	}
	if len(in) > maxFeedbackReasons {
		return nil, errcode.ErrInvalidParams.WithMsg("原因标签太多")
	}
	for _, r := range in {
		if !model.ValidFeedbackReason(r) {
			return nil, errcode.ErrInvalidParams.WithMsg("未知的原因标签：" + truncateRunes(r, 32))
		}
		if !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	return out, nil
}

// PurgeExpired 清理过期的观测大字段：工具调用的参数和结果、摘要里的 runtime_tail，保留 30 天；摘要和反馈长期保留。
func (s *ObserveService) PurgeExpired(ctx context.Context) (toolCalls, digests int64, err error) {
	now := s.d.Now()
	return s.d.Repo.PurgeObserveBefore(ctx, now.Add(-observeRetention), now)
}

// ---- 分析令牌 ----

// AnalysisTokenView 是返回给后台的分析令牌，不含哈希。
type AnalysisTokenView struct {
	ID         uint64     `json:"id"`           // 令牌 ID
	Name       string     `json:"name"`         // 用途
	Prefix     string     `json:"prefix"`       // 明文的前 8 位，用来在列表里辨认
	Scope      string     `json:"scope"`        // 权限范围
	ExpiresAt  time.Time  `json:"expires_at"`   // 到期时间
	LastUsedAt *time.Time `json:"last_used_at"` // 最近使用
	RevokedAt  *time.Time `json:"revoked_at"`   // 注销时间
	CreatedAt  time.Time  `json:"created_at"`   // 签发时间
}

func tokenView(t *model.AdminAPIToken) *AnalysisTokenView {
	return &AnalysisTokenView{ID: t.ID, Name: t.Name, Prefix: t.Prefix, Scope: t.Scope, ExpiresAt: t.ExpiresAt, LastUsedAt: t.LastUsedAt, RevokedAt: t.RevokedAt, CreatedAt: t.CreatedAt}
}

// IssuedToken 是签发的结果：明文只在这一次返回。
type IssuedToken struct {
	Item  *AnalysisTokenView `json:"item"`  // 令牌信息
	Token string             `json:"token"` // 明文，之后无法再取回
}

// IssueToken 签发分析令牌：随机生成，库里只存 sha256，明文只返回这一次。写审计（不含明文）。
func (s *ObserveService) IssueToken(ctx context.Context, adminID uint64, req *model.IssueAnalysisTokenReq) (*IssuedToken, error) {
	// 1. 用途 1–64 字；有效期 1–90 天（令牌放在本机文件里，泄露风险靠过期兜底，不允许永久令牌）
	name := strings.TrimSpace(req.Name)
	if name == "" || utf8.RuneCountInString(name) > maxTokenNameRunes {
		return nil, errcode.ErrInvalidParams.WithMsg("用途需要 1–64 个字")
	}
	if req.Days < 1 || req.Days > maxTokenDays {
		return nil, errcode.ErrInvalidParams.WithMsg("有效期必须在 1–90 天之间")
	}
	// 2. 随机明文 vca_ + 64 位十六进制；库里只存哈希
	raw := make([]byte, analysisTokenRandom)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	plain := model.TokenPrefix + hex.EncodeToString(raw)
	now := s.d.Now()
	t := &model.AdminAPIToken{Name: name, Prefix: plain[:8], TokenHash: hashToken(plain), Scope: model.TokenScopeAgentRead, CreatedBy: adminID, ExpiresAt: now.AddDate(0, 0, req.Days)}
	if err := s.d.Repo.CreateToken(ctx, t); err != nil {
		return nil, err
	}
	s.audit(ctx, adminID, model.AdminAuditAgentTokenIssue, t.ID, map[string]any{"name": name, "days": req.Days, "prefix": t.Prefix, "scope": t.Scope})
	return &IssuedToken{Item: tokenView(t), Token: plain}, nil
}

// ListTokens 列出分析令牌（含已过期的，不含已注销的）。
func (s *ObserveService) ListTokens(ctx context.Context) ([]*AnalysisTokenView, error) {
	items, err := s.d.Repo.ListTokens(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*AnalysisTokenView, len(items))
	for i := range items {
		out[i] = tokenView(&items[i])
	}
	return out, nil
}

// RevokeToken 注销令牌并写审计；令牌不存在返回 10002，重复注销是幂等的。注销后该令牌立即失效，也不再出现在列表里。
func (s *ObserveService) RevokeToken(ctx context.Context, adminID, id uint64) error {
	if err := s.d.Repo.RevokeToken(ctx, id, s.d.Now()); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return errcode.ErrNotFound
		}
		return err
	}
	s.audit(ctx, adminID, model.AdminAuditAgentTokenRevoke, id, nil)
	return nil
}

// TokenIdentity 是令牌认证通过后的身份。
type TokenIdentity struct {
	TokenID uint64 // 令牌 ID
	UserID  uint64 // 签发人
}

// errBadToken 是令牌认证失败的统一回复：不区分不存在、已注销、已过期还是签发人被降权，免得被拿来探测。
var errBadToken = errcode.ErrUnauthorized.WithMsg("分析令牌无效、已过期或已注销")

// AuthenticateToken 校验分析令牌：哈希存在、没注销、没过期、权限范围对、签发人仍是管理员，且没超出每分钟请求上限。
// 签发人降权后令牌立即失效，所以每次都实时查角色。
func (s *ObserveService) AuthenticateToken(ctx context.Context, plain string) (*TokenIdentity, error) {
	if !strings.HasPrefix(plain, model.TokenPrefix) {
		return nil, errBadToken
	}
	t, err := s.d.Repo.TokenByHash(ctx, hashToken(plain))
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errBadToken
	}
	if err != nil {
		return nil, err
	}
	now := s.d.Now()
	if t.RevokedAt != nil || !now.Before(t.ExpiresAt) || t.Scope != model.TokenScopeAgentRead {
		return nil, errBadToken
	}
	role, err := s.d.Roles(ctx, t.CreatedBy)
	if err != nil {
		return nil, err
	}
	if role != model.RoleAdmin && role != model.RoleSuperAdmin {
		return nil, errBadToken
	}
	if !s.allow(t.ID, now) {
		return nil, errcode.ErrTooManyReqs
	}
	if s.shouldTouch(t.ID, now) {
		if err := s.d.Repo.TouchToken(ctx, t.ID, now); err != nil {
			logger.Warn("记录分析令牌使用时间失败", zap.Error(err), zap.Uint64("token_id", t.ID))
		}
	}
	return &TokenIdentity{TokenID: t.ID, UserID: t.CreatedBy}, nil
}

// allow 固定窗口限流：每个令牌每分钟最多 tokenRatePerMinute 次。多实例各自计数，对一个本地 CLI 的用量足够。
func (s *ObserveService) allow(tokenID uint64, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	w := s.windows[tokenID]
	if w == nil || now.Sub(w.start) >= time.Minute {
		w = &rateWindow{start: now}
		s.windows[tokenID] = w
	}
	if w.n >= tokenRatePerMinute {
		return false
	}
	w.n++
	return true
}

// shouldTouch 判断这次要不要写最近使用时间：距上次写超过 tokenTouchInterval 才写。
func (s *ObserveService) shouldTouch(tokenID uint64, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if last, ok := s.touched[tokenID]; ok && now.Sub(last) < tokenTouchInterval {
		return false
	}
	s.touched[tokenID] = now
	return true
}

func hashToken(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// audit 写一条分析相关的后台审计；写失败只记日志，不回滚已完成的操作。
func (s *ObserveService) audit(ctx context.Context, actorID uint64, action string, targetID uint64, detail map[string]any) {
	if s.d.Audit == nil {
		return
	}
	entry := &model.AdminAuditLog{ActorID: actorID, Action: action, TargetType: model.AdminAuditTargetAgent, TargetID: targetID}
	if len(detail) > 0 {
		if b, err := json.Marshal(detail); err == nil {
			entry.DetailJSON = model.JSONText(b)
		}
	}
	if err := s.d.Audit.Insert(ctx, entry); err != nil {
		logger.Error("写 Agent 观测审计失败", zap.Error(err), zap.String("action", action), zap.Uint64("actor_id", actorID))
	}
}
