package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/repository"
)

// MCPEndpointPath 是对外端点的固定路径：签发令牌时一并返回，前端补上 origin 就能直接粘贴到外部工具。
const MCPEndpointPath = "/api/v1/mcp"

const (
	// mcpTokenRandom 是令牌明文的随机字节数：32 字节 → 64 位十六进制。
	mcpTokenRandom = 32
	// mcpTokenNameRunes 是用途备注的字数上限，和后台输入框一致。
	mcpTokenNameRunes = 64
	// mcpTokenMaxDays 是有效期上限：不允许永久令牌，泄露风险靠过期兜底。
	mcpTokenMaxDays = 90
	// mcpRatePerMinute 是单个令牌每分钟的调用上限。多实例各自计数，对单机 AI 工具足够。
	mcpRatePerMinute = 120
	// mcpTouchInterval 是两次回写「最近使用时间」的最小间隔，避免每次调用都写库。
	mcpTouchInterval = time.Minute
	// mcpMaxProjectsPerUser 限制单个用户能建几个接入项目：防止脚本刷项目与令牌。
	mcpMaxProjectsPerUser = 10
	// mcpDefaultTokenDays 是签发时没填有效期的兜底天数（后台「注册设置」里的库值优先）。
	mcpDefaultTokenDays = 30
)

// defaultTokenName 是新建项目自动签发、以及「重新生成」时用的令牌名。
// 一个项目只有一枚令牌，用途没有区分余地，名字只是给人看的。
//
//nolint:gosec // 这是令牌的显示名，不是凭据
const defaultTokenName = "接入令牌"

// MCPAuditWriter 写给后台操作审计用；MCP 项目的建改删与令牌签发注销都要留痕。
type MCPAuditWriter interface {
	Insert(ctx context.Context, audit *model.AdminAuditLog) error
}

// MCPProjectStore 是项目与令牌的存储。抽成接口是为了让令牌生命周期的用例能在不连库的情况下跑。
type MCPProjectStore interface {
	CreateProject(ctx context.Context, p *model.MCPProject) error
	UpdateProject(ctx context.Context, p *model.MCPProject) error
	DeleteProject(ctx context.Context, id uint64) error
	ListProjects(ctx context.Context, ownerID uint64) ([]model.MCPProject, error)
	CountProjectsByOwner(ctx context.Context, ownerID uint64) (int64, error)
	ProjectByID(ctx context.Context, id uint64) (*model.MCPProject, error)
	CanvasTitles(ctx context.Context, ids []uint64) (map[uint64]string, error)
	CreateToken(ctx context.Context, t *model.MCPProjectToken) error
	ListTokens(ctx context.Context, projectID uint64) ([]model.MCPProjectToken, error)
	RevokeToken(ctx context.Context, projectID, tokenID uint64, at time.Time) error
	TokenByHash(ctx context.Context, hash string) (*model.MCPProjectToken, error)
	TokenByID(ctx context.Context, projectID, tokenID uint64) (*model.MCPProjectToken, error)
	DeleteOtherTokens(ctx context.Context, projectID, keepID uint64) error
	// TokensByProjects 一次取多个项目的令牌（每个项目最新一枚），列表页直接挂在项目上回显。
	TokensByProjects(ctx context.Context, ids []uint64) (map[uint64]model.MCPProjectToken, error)
	TouchToken(ctx context.Context, id uint64, at time.Time) error
	TokenStats(ctx context.Context, ownerID uint64) (map[uint64]repository.MCPTokenStat, error)
}

// MCPUserStore 是认证与校验要用到的用户查询。
type MCPUserStore interface {
	GetByID(ctx context.Context, id uint64) (*model.User, error)
	ListUsernames(ctx context.Context, ids []uint64) (map[uint64]string, error)
}

// MCPCanvasStore 用来确认画布属于归属用户。
type MCPCanvasStore interface {
	GetByID(ctx context.Context, userID, id uint64) (*model.CanvasProject, error)
}

// MCPProjectDeps 是 MCP 接入服务的依赖。
type MCPProjectDeps struct {
	Repo     MCPProjectStore
	Users    MCPUserStore
	Canvases MCPCanvasStore
	// Audit 为空时不写审计（测试里可以省掉）。
	Audit MCPAuditWriter
	// SecretKey 是加解密令牌明文的主密钥（APP_AI_SECRET_KEY）。为空时令牌照常可用，只是事后取不回明文。
	SecretKey string
	// Defaults 提供默认有效期等可配置项；为空时用内置常量。
	Defaults MCPTokenDefaults
	// Now 返回当前时间，测试里注入固定值；为空时用 time.Now。
	Now func() time.Time
}

// MCPTokenDefaults 是签发表单留空时用的默认值（后台「注册设置」里配）。
type MCPTokenDefaults interface {
	MCPTokenDays(ctx context.Context) (int, error)
}

// MCPProjectService 管理 MCP 接入项目与令牌，并给请求侧提供令牌认证。
//
// 设计要点：令牌不是账号——它做的事全部记在项目归属用户名下，后台始终知道「这个动作是谁授权的」。
// 失效是立即的：令牌注销、项目停用、归属用户禁用，三者任一发生，下一次调用立刻被拒。
type MCPProjectService struct {
	d MCPProjectDeps
	// cipher 加解密令牌明文；主密钥没配时为 nil（令牌照常可用，取不回明文）
	cipher *aiSecretCipher
	mu     sync.Mutex
	// windows 是固定窗口限流：key 是令牌 id。
	windows map[uint64]*mcpRateWindow
}

// mcpRateWindow 是一个令牌在当前窗口内的计数。
type mcpRateWindow struct {
	start time.Time
	n     int
}

// MCPIdentity 是令牌认证通过后的身份：项目、归属用户、权限集合、默认画布。
//
// 权限单位是用户不是画布：这个身份可以访问归属用户名下的任何画布，
// 具体是哪一块由每次调用的 canvas 参数决定（DefaultCanvasID 只是「没指定时用哪块」）。
type MCPIdentity struct {
	ProjectID uint64
	UserID    uint64
	// DefaultCanvasID 是项目上配的默认画布，0 表示没配：此时每次调用都必须显式指定 canvas。
	DefaultCanvasID uint64
	Scopes          map[string]bool
}

// HasScope 判断这个身份有没有某一档权限。
func (i *MCPIdentity) HasScope(scope string) bool { return i.Scopes[scope] }

// NewMCPProjectService 创建 MCP 接入服务。
func NewMCPProjectService(d MCPProjectDeps) *MCPProjectService {
	if d.Now == nil {
		d.Now = time.Now
	}
	s := &MCPProjectService{d: d, windows: make(map[uint64]*mcpRateWindow)}
	s.initCipher()
	return s
}

// List 列出全部项目，带上令牌数量与最近使用时间。管理员视角。
func (s *MCPProjectService) List(ctx context.Context) ([]*model.MCPProjectView, error) {
	return s.list(ctx, 0)
}

// OwnedList 列出某个用户自己的项目。用户自助视角：归属校验由 ownerID 过滤兜住。
func (s *MCPProjectService) OwnedList(ctx context.Context, ownerID uint64) ([]*model.MCPProjectView, error) {
	return s.list(ctx, ownerID)
}

// list 是 List / OwnedList 的共同实现；ownerID 为 0 表示全部。
func (s *MCPProjectService) list(ctx context.Context, ownerID uint64) ([]*model.MCPProjectView, error) {
	projects, err := s.d.Repo.ListProjects(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	projectIDs := make([]uint64, 0, len(projects))
	for i := range projects {
		projectIDs = append(projectIDs, projects[i].ID)
	}
	stamps, err := s.d.Repo.TokensByProjects(ctx, projectIDs)
	if err != nil {
		return nil, err
	}
	owners := make([]uint64, 0, len(projects))
	for i := range projects {
		owners = append(owners, projects[i].OwnerUserID)
	}
	names, err := s.d.Users.ListUsernames(ctx, owners)
	if err != nil {
		return nil, err
	}
	canvasIDs := make([]uint64, 0, len(projects))
	for i := range projects {
		if id := derefCanvasID(projects[i].CanvasID); id != 0 {
			canvasIDs = append(canvasIDs, id)
		}
	}
	titles, err := s.d.Repo.CanvasTitles(ctx, canvasIDs)
	if err != nil {
		return nil, err
	}
	out := make([]*model.MCPProjectView, 0, len(projects))
	for i := range projects {
		v := projectView(&projects[i])
		v.OwnerName = names[projects[i].OwnerUserID]
		v.CanvasTitle = titles[derefCanvasID(projects[i].CanvasID)]
		if t, ok := stamps[projects[i].ID]; ok {
			v.Token = tokenView(&t)
			v.LastUsedAt = rfc3339Ptr(t.LastUsedAt)
		}
		out = append(out, v)
	}
	return out, nil
}

// Create 新建项目：校验名称、归属用户、权限档位与画布白名单，并把首枚令牌一起签好。
// 归属用户不填就落在操作者名下：建项目只要一个名字，归属是「谁建的算谁的」而不是必填项。
func (s *MCPProjectService) Create(ctx context.Context, adminID uint64, req *model.MCPProjectReq) (*model.MCPProjectCreated, error) {
	if strings.TrimSpace(req.OwnerUserID) == "" {
		req.OwnerUserID = strconv.FormatUint(adminID, 10)
	}
	p := &model.MCPProject{Name: strings.TrimSpace(req.Name), CreatedBy: adminID, Enabled: true}
	if err := s.fillProject(ctx, p, req); err != nil {
		return nil, err
	}
	if err := s.d.Repo.CreateProject(ctx, p); err != nil {
		return nil, err
	}
	s.audit(ctx, adminID, model.AdminAuditMCPProjectCreate, p.ID, map[string]any{
		"name": p.Name, "owner_user_id": p.OwnerUserID, "scopes": p.ScopeList(), "enabled": p.Enabled,
	})
	v := projectView(p)
	s.decorate(ctx, v, p.OwnerUserID, p.CanvasID)
	// 项目与首枚令牌一次交付：只建项目的话，用户还要再点一次「签发」才有能粘进客户端的配置。
	issued, err := s.issue(ctx, adminID, p.ID, defaultTokenName, 0)
	if err != nil {
		// 令牌没签成就把项目删掉：别在列表里留下一个没有令牌、连不上的空接入。
		_ = s.d.Repo.DeleteProject(ctx, p.ID)
		return nil, err
	}
	v.Token = issued.Item
	return &model.MCPProjectCreated{Item: v, Issued: issued}, nil
}

// Update 修改项目：名称、归属用户、权限档位、白名单、启停。停用会立刻让该项目全部令牌失效。
// 归属用户不填就沿用原来的归属：改权限、改默认画布时不该被迫再选一次人。
func (s *MCPProjectService) Update(ctx context.Context, adminID, projectID uint64, req *model.MCPProjectReq) (*model.MCPProjectView, error) {
	p, err := s.project(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.OwnerUserID) == "" {
		req.OwnerUserID = strconv.FormatUint(p.OwnerUserID, 10)
	}
	p.Name = strings.TrimSpace(req.Name)
	if err := s.fillProject(ctx, p, req); err != nil {
		return nil, err
	}
	if err := s.d.Repo.UpdateProject(ctx, p); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errcode.ErrMCPProjectNotFound
		}
		return nil, err
	}
	s.audit(ctx, adminID, model.AdminAuditMCPProjectUpdate, p.ID, map[string]any{
		"name": p.Name, "owner_user_id": p.OwnerUserID, "scopes": p.ScopeList(), "enabled": p.Enabled,
	})
	v := projectView(p)
	s.decorate(ctx, v, p.OwnerUserID, p.CanvasID)
	return v, nil
}

// decorate 补上归属用户名与默认画布标题：新建 / 修改的回显要和列表一致，界面不必再查一次。
// 查不到就留空，不影响这次写入的结果。
func (s *MCPProjectService) decorate(ctx context.Context, v *model.MCPProjectView, ownerID uint64, canvasID *uint64) {
	if names, err := s.d.Users.ListUsernames(ctx, []uint64{ownerID}); err == nil {
		v.OwnerName = names[ownerID]
	}
	id := derefCanvasID(canvasID)
	if id == 0 {
		return
	}
	if titles, err := s.d.Repo.CanvasTitles(ctx, []uint64{id}); err == nil {
		v.CanvasTitle = titles[id]
	}
}

// Delete 删除项目，令牌一并删除。
func (s *MCPProjectService) Delete(ctx context.Context, adminID, projectID uint64) error {
	if err := s.d.Repo.DeleteProject(ctx, projectID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return errcode.ErrMCPProjectNotFound
		}
		return err
	}
	s.audit(ctx, adminID, model.AdminAuditMCPProjectDelete, projectID, nil)
	return nil
}

// fillProject 校验并写入可改字段：名称、归属用户（必须存在且可用）、权限档位、画布白名单、启停。
func (s *MCPProjectService) fillProject(ctx context.Context, p *model.MCPProject, req *model.MCPProjectReq) error {
	if p.Name == "" || utf8.RuneCountInString(p.Name) > 64 {
		return errcode.ErrInvalidParams.WithMsg("名称需要 1–64 个字")
	}
	ownerID, err := strconv.ParseUint(strings.TrimSpace(req.OwnerUserID), 10, 64)
	if err != nil || ownerID == 0 {
		return errcode.ErrInvalidParams.WithMsg("归属用户不正确")
	}
	owner, err := s.d.Users.GetByID(ctx, ownerID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return errcode.ErrInvalidParams.WithMsg("归属用户不存在")
		}
		return err
	}
	if owner.Status != model.UserStatusActive {
		return errcode.ErrInvalidParams.WithMsg("归属用户已被停用，不能绑定")
	}
	// 默认画布可留空：不设的话调用方每次都要指定 canvas，这也正是多画布场景的默认姿势。
	canvasID, err := s.resolveDefaultCanvas(ctx, ownerID, req.CanvasCode)
	if err != nil {
		return err
	}
	scopes, err := normalizeScopes(req.Scopes)
	if err != nil {
		return err
	}
	p.OwnerUserID = ownerID
	if canvasID == 0 {
		p.CanvasID = nil
	} else {
		id := canvasID
		p.CanvasID = &id
	}
	p.SetScopes(scopes)
	if req.Enabled != nil {
		p.Enabled = *req.Enabled
	}
	return nil
}

// resolveDefaultCanvas 解析并校验可选的默认画布。
// 空串表示不设默认画布（返回 0）：调用方每次自己指定 canvas，多画布场景本来就这样。
// 填了就必须属于归属用户——它不是权限边界，但填一块别人的画布必然是配错了。
func (s *MCPProjectService) resolveDefaultCanvas(ctx context.Context, ownerID uint64, code string) (uint64, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return 0, nil
	}
	id, ok := idcodec.Decode(code)
	if !ok || id == 0 {
		return 0, errcode.ErrInvalidParams.WithMsg("默认画布地址不正确")
	}
	if _, err := s.d.Canvases.GetByID(ctx, ownerID, id); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return 0, errcode.ErrInvalidParams.WithMsg("这个用户没有该画布")
		}
		return 0, err
	}
	return id, nil
}

// ListTokens 列出项目的令牌（含已注销的；不含明文）。
func (s *MCPProjectService) ListTokens(ctx context.Context, projectID uint64) ([]*model.MCPTokenView, error) {
	if _, err := s.project(ctx, projectID); err != nil {
		return nil, err
	}
	tokens, err := s.d.Repo.ListTokens(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]*model.MCPTokenView, 0, len(tokens))
	for i := range tokens {
		out = append(out, tokenView(&tokens[i]))
	}
	return out, nil
}

// IssueToken 生成（或轮换）项目那枚令牌：明文只返回这一次，库里只存 sha256。
//
// 一个项目一枚令牌，所以不接收用途与天数：名字固定、有效期取后台默认，
// 「重新生成」和「首次生成」是同一个动作。
func (s *MCPProjectService) IssueToken(ctx context.Context, adminID, projectID uint64) (*model.MCPIssuedToken, error) {
	if _, err := s.project(ctx, projectID); err != nil {
		return nil, err
	}
	return s.issue(ctx, adminID, projectID, defaultTokenName, 0)
}

// issue 生成并保存一枚令牌，同时清掉同项目的旧令牌：明文进密文、鉴权只用 sha256、写审计。
//
// 建项目时自动签的那枚与「重新生成」走的都是这里，规则只有一份。
// name 留空用默认名；days 留空用后台配的默认有效期。
func (s *MCPProjectService) issue(ctx context.Context, actorID, projectID uint64, name string, days int) (*model.MCPIssuedToken, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = defaultTokenName
	}
	if utf8.RuneCountInString(name) > mcpTokenNameRunes {
		return nil, errcode.ErrInvalidParams.WithMsg("用途需要 1–64 个字")
	}
	d, err := s.tokenDays(ctx, days)
	if err != nil {
		return nil, err
	}
	raw := make([]byte, mcpTokenRandom)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	plain := model.MCPTokenPrefix + hex.EncodeToString(raw)
	now := s.d.Now()
	t := &model.MCPProjectToken{
		ProjectID: projectID, Name: name, Prefix: plain[:8], TokenHash: hashMCPToken(plain),
		SecretCipher: s.sealToken(plain, hashMCPToken(plain)),
		ExpiresAt:    now.AddDate(0, 0, d),
	}
	if err := s.d.Repo.CreateToken(ctx, t); err != nil {
		return nil, err
	}
	// 每个项目只留一枚：先建好新的再删旧的，中间失败也不会出现「一枚都没有」。
	if err := s.d.Repo.DeleteOtherTokens(ctx, projectID, t.ID); err != nil {
		return nil, err
	}
	s.audit(ctx, actorID, model.AdminAuditMCPTokenIssue, t.ID, map[string]any{
		"project_id": projectID, "name": name, "days": d, "prefix": t.Prefix,
	})
	return &model.MCPIssuedToken{Item: tokenView(t), Token: plain, Endpoint: MCPEndpointPath}, nil
}

// RevokeToken 注销项目那枚令牌：外部工具立刻断开，项目本身保留，之后可以再生成一枚。
func (s *MCPProjectService) RevokeToken(ctx context.Context, adminID, projectID uint64) error {
	if _, err := s.project(ctx, projectID); err != nil {
		return err
	}
	tokens, err := s.d.Repo.ListTokens(ctx, projectID)
	if err != nil {
		return err
	}
	if len(tokens) == 0 {
		return errcode.ErrMCPTokenNotFound
	}
	id := tokens[0].ID
	if err := s.d.Repo.RevokeToken(ctx, projectID, id, s.d.Now()); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return errcode.ErrMCPTokenNotFound
		}
		return err
	}
	s.audit(ctx, adminID, model.AdminAuditMCPTokenRevoke, id, map[string]any{"project_id": projectID})
	return nil
}

// project 读项目并翻译成业务错误。
func (s *MCPProjectService) project(ctx context.Context, id uint64) (*model.MCPProject, error) {
	p, err := s.d.Repo.ProjectByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errcode.ErrMCPProjectNotFound
		}
		return nil, err
	}
	return p, nil
}

// audit 写后台操作审计；失败只记日志，不影响业务结果。
func (s *MCPProjectService) audit(ctx context.Context, actorID uint64, action string, targetID uint64, detail map[string]any) {
	if s.d.Audit == nil {
		return
	}
	entry := &model.AdminAuditLog{ActorID: actorID, Action: action, TargetType: model.AdminAuditTargetMCP, TargetID: targetID}
	if len(detail) > 0 {
		if b, err := json.Marshal(detail); err == nil {
			entry.DetailJSON = model.JSONText(b)
		}
	}
	if err := s.d.Audit.Insert(ctx, entry); err != nil {
		logger.Error("写 MCP 接入审计失败", zap.Error(err), zap.String("action", action), zap.Uint64("actor_id", actorID))
	}
}

// normalizeScopes 校验权限档位并补全隐含依赖。
// 只接受认识的档位；勾了「改画布」会自动带上「读画布」「浏览目录」，
// 这样库里不会出现「给了写却读不了」的互斥配置（直接调接口绕过界面也一样）。
func normalizeScopes(scopes []string) ([]string, error) {
	for _, s := range scopes {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if !model.MCPScopeKnown(s) {
			return nil, errcode.ErrInvalidParams.WithMsg("不认识的权限：" + s)
		}
	}
	return model.ExpandMCPScopes(scopes), nil
}

// hashMCPToken 计算令牌明文的 sha256 十六进制，库里只存它。
func hashMCPToken(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// projectView 把项目转成回显结构；id 一律用十进制字符串，避免雪花 id 在 JS 里丢精度。
func projectView(p *model.MCPProject) *model.MCPProjectView {
	scopes := p.ScopeList()
	if scopes == nil {
		scopes = []string{}
	}
	v := &model.MCPProjectView{
		ID: strconv.FormatUint(p.ID, 10), Name: p.Name,
		OwnerUserID: strconv.FormatUint(p.OwnerUserID, 10),
		Scopes:      scopes, Enabled: p.Enabled,
		CreatedAt: rfc3339(p.CreatedAt), UpdatedAt: rfc3339(p.UpdatedAt),
	}
	// 没配默认画布就不给 code：界面据此显示「未设」，调用方则会被告知要自己指定。
	if id := derefCanvasID(p.CanvasID); id != 0 {
		v.CanvasCode = idcodec.Encode(id)
	}
	return v
}

// tokenView 把令牌转成回显结构（不含明文）。
func tokenView(t *model.MCPProjectToken) *model.MCPTokenView {
	return &model.MCPTokenView{
		ID: strconv.FormatUint(t.ID, 10), ProjectID: strconv.FormatUint(t.ProjectID, 10),
		Name: t.Name, Prefix: t.Prefix, HasSecret: t.SecretCipher != "", ExpiresAt: rfc3339(t.ExpiresAt),
		LastUsedAt: rfc3339Ptr(t.LastUsedAt), RevokedAt: rfc3339Ptr(t.RevokedAt),
		CreatedAt: rfc3339(t.CreatedAt),
	}
}

// rfc3339 统一时间格式，前端直接当日期字符串用。
func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// rfc3339Ptr 格式化可空时间。
func rfc3339Ptr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := rfc3339(*t)
	return &s
}
