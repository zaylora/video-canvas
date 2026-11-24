package service_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

// mcpStore 是内存版的项目与令牌存储，够跑令牌生命周期与权限校验。
type mcpStore struct {
	projects []model.MCPProject
	tokens   []model.MCPProjectToken
	nextID   uint64
}

func newMCPStore() *mcpStore { return &mcpStore{nextID: 1} }

func (s *mcpStore) id() uint64 { id := s.nextID; s.nextID++; return id }

func (s *mcpStore) CreateProject(_ context.Context, p *model.MCPProject) error {
	p.ID = s.id()
	s.projects = append(s.projects, *p)
	return nil
}

func (s *mcpStore) UpdateProject(_ context.Context, p *model.MCPProject) error {
	for i := range s.projects {
		if s.projects[i].ID == p.ID {
			s.projects[i] = *p
			return nil
		}
	}
	return repository.ErrNotFound
}

func (s *mcpStore) DeleteProject(_ context.Context, id uint64) error {
	for i := range s.projects {
		if s.projects[i].ID == id {
			s.projects = append(s.projects[:i], s.projects[i+1:]...)
			return nil
		}
	}
	return repository.ErrNotFound
}

func (s *mcpStore) ListProjects(_ context.Context, ownerID uint64) ([]model.MCPProject, error) {
	if ownerID == 0 {
		return s.projects, nil
	}
	var out []model.MCPProject
	for _, p := range s.projects {
		if p.OwnerUserID == ownerID {
			out = append(out, p)
		}
	}
	return out, nil
}

func (s *mcpStore) CountProjectsByOwner(_ context.Context, ownerID uint64) (int64, error) {
	var n int64
	for _, p := range s.projects {
		if p.OwnerUserID == ownerID {
			n++
		}
	}
	return n, nil
}

func (s *mcpStore) ProjectByID(_ context.Context, id uint64) (*model.MCPProject, error) {
	for i := range s.projects {
		if s.projects[i].ID == id {
			p := s.projects[i]
			return &p, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (s *mcpStore) CanvasTitles(context.Context, []uint64) (map[uint64]string, error) {
	return map[uint64]string{}, nil
}

func (s *mcpStore) CreateToken(_ context.Context, t *model.MCPProjectToken) error {
	t.ID = s.id()
	s.tokens = append(s.tokens, *t)
	return nil
}

func (s *mcpStore) ListTokens(_ context.Context, projectID uint64) ([]model.MCPProjectToken, error) {
	var out []model.MCPProjectToken
	for _, t := range s.tokens {
		if t.ProjectID == projectID {
			out = append(out, t)
		}
	}
	return out, nil
}

// TokensByProjects 每个项目取 id 最大的一枚，和生产那条 DISTINCT ON 查询同口径。
func (s *mcpStore) TokensByProjects(_ context.Context, ids []uint64) (map[uint64]model.MCPProjectToken, error) {
	out := make(map[uint64]model.MCPProjectToken, len(ids))
	for _, id := range ids {
		var latest model.MCPProjectToken
		found := false
		for _, t := range s.tokens {
			if t.ProjectID == id && (!found || t.ID > latest.ID) {
				latest, found = t, true
			}
		}
		if found {
			out[id] = latest
		}
	}
	return out, nil
}

func (s *mcpStore) RevokeToken(_ context.Context, projectID, tokenID uint64, at time.Time) error {
	for i := range s.tokens {
		if s.tokens[i].ID == tokenID && s.tokens[i].ProjectID == projectID {
			if s.tokens[i].RevokedAt == nil {
				s.tokens[i].RevokedAt = &at
			}
			return nil
		}
	}
	return repository.ErrNotFound
}

func (s *mcpStore) TokenByHash(_ context.Context, hash string) (*model.MCPProjectToken, error) {
	for i := range s.tokens {
		if s.tokens[i].TokenHash == hash {
			t := s.tokens[i]
			return &t, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (s *mcpStore) TokenByID(_ context.Context, projectID, tokenID uint64) (*model.MCPProjectToken, error) {
	for i := range s.tokens {
		if s.tokens[i].ID == tokenID && s.tokens[i].ProjectID == projectID {
			t := s.tokens[i]
			return &t, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (s *mcpStore) DeleteOtherTokens(_ context.Context, projectID, keepID uint64) error {
	kept := make([]model.MCPProjectToken, 0, len(s.tokens))
	for _, t := range s.tokens {
		if t.ProjectID == projectID && t.ID != keepID {
			continue
		}
		kept = append(kept, t)
	}
	s.tokens = kept
	return nil
}

func (s *mcpStore) TouchToken(_ context.Context, id uint64, at time.Time) error {
	for i := range s.tokens {
		if s.tokens[i].ID == id {
			s.tokens[i].LastUsedAt = &at
			return nil
		}
	}
	return nil
}

func (s *mcpStore) TokenStats(_ context.Context, ownerID uint64) (map[uint64]repository.MCPTokenStat, error) {
	owned := map[uint64]bool{}
	for _, p := range s.projects {
		if ownerID == 0 || p.OwnerUserID == ownerID {
			owned[p.ID] = true
		}
	}
	out := map[uint64]repository.MCPTokenStat{}
	for _, t := range s.tokens {
		if t.RevokedAt != nil || !owned[t.ProjectID] {
			continue
		}
		st := out[t.ProjectID]
		st.ProjectID = t.ProjectID
		st.Count++
		if t.LastUsedAt != nil && (st.LastUsedAt == nil || t.LastUsedAt.After(*st.LastUsedAt)) {
			st.LastUsedAt = t.LastUsedAt
		}
		out[t.ProjectID] = st
	}
	return out, nil
}

// mcpUsers 是内存版用户存储。
type mcpUsers map[uint64]*model.User

func (u mcpUsers) GetByID(_ context.Context, id uint64) (*model.User, error) {
	if user, ok := u[id]; ok {
		return user, nil
	}
	return nil, repository.ErrNotFound
}

func (u mcpUsers) ListUsernames(context.Context, []uint64) (map[uint64]string, error) {
	out := make(map[uint64]string, len(u))
	for id, user := range u {
		out[id] = user.Username
	}
	return out, nil
}

// mcpCanvases 是内存版画布存储：只有登记过的 (user, canvas) 组合才算存在。
type mcpCanvases map[uint64]map[uint64]string

func (c mcpCanvases) GetByID(_ context.Context, userID, id uint64) (*model.CanvasProject, error) {
	if titles, ok := c[userID]; ok {
		if title, ok := titles[id]; ok {
			return &model.CanvasProject{Title: title}, nil
		}
	}
	return nil, repository.ErrNotFound
}

// mcpDefaults 冒充「注册设置」里的令牌默认有效期（天）。
type mcpDefaults int

func (d mcpDefaults) MCPTokenDays(context.Context) (int, error) { return int(d), nil }

// mcpFixture 是一套可用的服务与它的依赖。
type mcpFixture struct {
	svc      *service.MCPProjectService
	store    *mcpStore
	users    mcpUsers
	canvases mcpCanvases
	now      time.Time
}

func newMCPFixture(t *testing.T) *mcpFixture {
	t.Helper()
	f := &mcpFixture{
		store: newMCPStore(),
		users: mcpUsers{
			7: {Username: "owner", Status: model.UserStatusActive},
			9: {Username: "admin", Status: model.UserStatusActive},
		},
		canvases: mcpCanvases{7: {18: "画布十八"}, 9: {88: "别人的画布"}},
		now:      time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC),
	}
	f.svc = service.NewMCPProjectService(service.MCPProjectDeps{
		Repo: f.store, Users: f.users, Canvases: f.canvases, Now: func() time.Time { return f.now },
	})
	return f
}

// create 建一个项目并返回它。项目创建时自带一枚令牌，这里只取项目本身。
func (f *mcpFixture) create(t *testing.T) *model.MCPProjectView {
	t.Helper()
	created, err := f.svc.Create(context.Background(), 9, &model.MCPProjectReq{
		Name: "本机 Codex", OwnerUserID: "7", CanvasCode: canvasCode(18),
		Scopes: []string{model.MCPScopeCanvasRead, model.MCPScopeCanvasWrite},
	})
	if err != nil {
		t.Fatalf("建项目失败：%v", err)
	}
	return created.Item
}

// issue 给项目签一个令牌并返回明文。
func (f *mcpFixture) issue(t *testing.T, projectID string) string {
	t.Helper()
	out, err := f.svc.IssueToken(context.Background(), 9, mustID(t, projectID))
	if err != nil {
		t.Fatalf("签令牌失败：%v", err)
	}
	return out.Token
}

// canvasCode 把画布主键换成地址里的那串十六进制，和后台拿到的一致。
func canvasCode(id uint64) string { return idcodec.Encode(id) }

func mustID(t *testing.T, s string) uint64 {
	t.Helper()
	id, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		t.Fatalf("id 不是数字：%s", s)
	}
	return id
}

func TestMCPTokenLifecycle(t *testing.T) {
	f := newMCPFixture(t)
	project := f.create(t)
	plain := f.issue(t, project.ID)

	// 1. 正常令牌能认证，并带回归属用户与默认画布
	who, err := f.svc.Authenticate(context.Background(), plain)
	if err != nil {
		t.Fatalf("新令牌应该能用：%v", err)
	}
	if who.UserID != 7 || who.DefaultCanvasID != 18 || who.ProjectID != mustID(t, project.ID) {
		t.Fatalf("身份不对：%+v", who)
	}
	if !who.HasScope(model.MCPScopeCanvasRead) || who.HasScope(model.MCPScopeCanvasCompose) {
		t.Fatalf("权限集合不对：%v", who.Scopes)
	}

	// 2. 注销之后立刻失效
	tokens, err := f.svc.ListTokens(context.Background(), mustID(t, project.ID))
	if err != nil || len(tokens) != 1 {
		t.Fatalf("令牌列表不对：%v %v", tokens, err)
	}
	if err := f.svc.RevokeToken(context.Background(), 9, mustID(t, project.ID)); err != nil {
		t.Fatalf("注销失败：%v", err)
	}
	if _, err := f.svc.Authenticate(context.Background(), plain); !isCode(err, errcode.ErrMCPTokenInvalid) {
		t.Fatalf("注销后应该拒绝，得到 %v", err)
	}

	// 3. 过期之后失效
	f = newMCPFixture(t)
	project = f.create(t)
	plain = f.issue(t, project.ID)
	f.now = f.now.AddDate(0, 0, 31)
	if _, err := f.svc.Authenticate(context.Background(), plain); !isCode(err, errcode.ErrMCPTokenInvalid) {
		t.Fatalf("过期后应该拒绝，得到 %v", err)
	}
}

// TestMCPTokenRotatesAndSecretCopyable 每个项目只留一枚令牌：再签发就是轮换，旧行被删掉；
// 新令牌的明文能按需取回（列表里「复制」），老令牌没有密文时明确要求重新签发。
func TestMCPTokenRotatesAndSecretCopyable(t *testing.T) {
	f := newMCPFixture(t)
	f.svc = service.NewMCPProjectService(service.MCPProjectDeps{
		Repo: f.store, Users: f.users, Canvases: f.canvases, Now: func() time.Time { return f.now },
		SecretKey: "test-master-key",
	})
	project := f.create(t)
	first := f.issue(t, project.ID)
	second := f.issue(t, project.ID)

	tokens, err := f.svc.ListTokens(context.Background(), mustID(t, project.ID))
	if err != nil || len(tokens) != 1 {
		t.Fatalf("一个项目应该只留一枚令牌：%v %v", tokens, err)
	}
	if _, err := f.svc.Authenticate(context.Background(), first); !isCode(err, errcode.ErrMCPTokenInvalid) {
		t.Fatalf("轮换后旧令牌必须失效，得到 %v", err)
	}
	if _, err := f.svc.Authenticate(context.Background(), second); err != nil {
		t.Fatalf("新令牌应该能用：%v", err)
	}
	if !tokens[0].HasSecret {
		t.Fatal("签发时应该存下密文，界面的「复制」才有东西可取")
	}
	got, err := f.svc.TokenSecret(context.Background(), 9, mustID(t, project.ID))
	if err != nil || got != second {
		t.Fatalf("取回明文应等于签发时的那串：%q %v", got, err)
	}

	// 老令牌（没有密文）只能重新签发，不能假装能复制
	f.store.tokens[0].SecretCipher = ""
	if _, err := f.svc.TokenSecret(context.Background(), 9, mustID(t, project.ID)); err == nil {
		t.Fatal("没有密文的令牌应该拒绝复制")
	}
}

// TestMCPTokenDaysFallBackToDefault 生成令牌时用后台配置的默认有效期；
// 后台没配或配坏了就退到内置 30 天。对外不再接收天数，所以只保留默认值这一条路。
func TestMCPTokenDaysFallBackToDefault(t *testing.T) {
	f := newMCPFixture(t)
	project := f.create(t)
	issue := func(defaults *mcpDefaults) time.Time {
		t.Helper()
		deps := service.MCPProjectDeps{
			Repo: f.store, Users: f.users, Canvases: f.canvases, Now: func() time.Time { return f.now },
		}
		if defaults != nil {
			deps.Defaults = *defaults
		}
		f.svc = service.NewMCPProjectService(deps)
		out, err := f.svc.IssueToken(context.Background(), 9, mustID(t, project.ID))
		if err != nil {
			t.Fatalf("生成令牌不该失败：%v", err)
		}
		got, perr := time.Parse(time.RFC3339, out.Item.ExpiresAt)
		if perr != nil {
			t.Fatalf("过期时间格式不对：%s", out.Item.ExpiresAt)
		}
		return got
	}

	// 1. 没接默认值来源时退到 30 天
	if got, want := issue(nil), f.now.AddDate(0, 0, 30); !got.Equal(want) {
		t.Fatalf("默认应该是 30 天，得到 %s", got)
	}
	// 2. 接了默认值来源就用库里的值
	d := mcpDefaults(7)
	if got, want := issue(&d), f.now.AddDate(0, 0, 7); !got.Equal(want) {
		t.Fatalf("应该用库里的 7 天，得到 %s", got)
	}
}

func TestMCPProjectDisabledAndOwnerDisabledReject(t *testing.T) {
	// 停用项目：令牌立刻失效
	f := newMCPFixture(t)
	project := f.create(t)
	plain := f.issue(t, project.ID)
	off := false
	if _, err := f.svc.Update(context.Background(), 9, mustID(t, project.ID), &model.MCPProjectReq{
		Name: "本机 Codex", OwnerUserID: "7", CanvasCode: canvasCode(18), Scopes: []string{model.MCPScopeCanvasRead}, Enabled: &off,
	}); err != nil {
		t.Fatalf("停用失败：%v", err)
	}
	if _, err := f.svc.Authenticate(context.Background(), plain); !isCode(err, errcode.ErrMCPTokenInvalid) {
		t.Fatalf("项目停用后应该拒绝，得到 %v", err)
	}

	// 归属用户被禁用：令牌立刻失效
	f = newMCPFixture(t)
	project = f.create(t)
	plain = f.issue(t, project.ID)
	f.users[7].Status = model.UserStatusDisabled
	if _, err := f.svc.Authenticate(context.Background(), plain); !isCode(err, errcode.ErrMCPTokenInvalid) {
		t.Fatalf("用户禁用后应该拒绝，得到 %v", err)
	}
}

func TestMCPProjectRejectsForeignCanvasAndUnknownScope(t *testing.T) {
	f := newMCPFixture(t)
	// 画布 88 不属于用户 7
	if _, err := f.svc.Create(context.Background(), 9, &model.MCPProjectReq{
		Name: "越权", OwnerUserID: "7", CanvasCode: canvasCode(88), Scopes: []string{model.MCPScopeCanvasRead},
	}); !isCode(err, errcode.ErrInvalidParams) {
		t.Fatalf("绑别人的画布应该被拒，得到 %v", err)
	}
	// 不认识的权限档位
	if _, err := f.svc.Create(context.Background(), 9, &model.MCPProjectReq{
		Name: "乱写", OwnerUserID: "7", CanvasCode: canvasCode(18), Scopes: []string{"admin:all"},
	}); !isCode(err, errcode.ErrInvalidParams) {
		t.Fatalf("不认识的权限应该被拒，得到 %v", err)
	}
	// 归属用户必须是可用的账号
	if _, err := f.svc.Create(context.Background(), 9, &model.MCPProjectReq{
		Name: "停用用户", OwnerUserID: "99", CanvasCode: canvasCode(18), Scopes: []string{model.MCPScopeCanvasRead},
	}); !isCode(err, errcode.ErrInvalidParams) {
		t.Fatalf("不存在的归属用户应该被拒，得到 %v", err)
	}
}

func TestMCPTokenPrefixAndLookalikes(t *testing.T) {
	f := newMCPFixture(t)
	if _, err := f.svc.Authenticate(context.Background(), "vca_deadbeef"); !isCode(err, errcode.ErrMCPTokenInvalid) {
		t.Fatalf("别的令牌前缀应该被拒，得到 %v", err)
	}
	if _, err := f.svc.Authenticate(context.Background(), "vcm_不存在的令牌"); !isCode(err, errcode.ErrMCPTokenInvalid) {
		t.Fatalf("不存在的令牌应该被拒，得到 %v", err)
	}
}

func TestMCPOwnedCreateForcesSelfAndAllowsNoDefaultCanvas(t *testing.T) {
	f := newMCPFixture(t)
	// 请求体里塞别人的归属：自助入口必须忽略它，落库是本人
	v, err := f.svc.OwnedCreate(context.Background(), 7, &model.MCPProjectReq{
		Name: "我的接入", OwnerUserID: "9", Scopes: []string{model.MCPScopeCanvasRead},
	})
	if err != nil {
		t.Fatalf("自助建项目失败：%v", err)
	}
	item := v.Item
	if item.OwnerUserID != "7" {
		t.Fatalf("归属必须固定为本人，得到 %s", item.OwnerUserID)
	}
	// 建项目时就把令牌签好：返回里必须带明文，用户不用再点一次「签发」
	if v.Issued == nil || v.Issued.Token == "" {
		t.Fatal("建项目应当同时返回首枚令牌的明文")
	}
	// 不填默认画布是允许的：调用方每次自己指定 canvas
	if item.CanvasCode != "" {
		t.Fatalf("没给默认画布时不该回画布 code，得到 %q", item.CanvasCode)
	}
	if item.CanvasTitle != "" {
		t.Fatalf("没给默认画布时不该回画布标题，得到 %q", item.CanvasTitle)
	}
}

func TestMCPOwnedCreateRejectsForeignCanvas(t *testing.T) {
	f := newMCPFixture(t)
	// 用户 7 拿用户 9 的画布当默认画布：必须拒绝
	_, err := f.svc.OwnedCreate(context.Background(), 7, &model.MCPProjectReq{
		Name: "越权默认画布", CanvasCode: canvasCode(88), Scopes: []string{model.MCPScopeCanvasRead},
	})
	if err == nil {
		t.Fatal("默认画布不属于自己时必须拒绝")
	}
	// 自己的画布可以
	if _, err := f.svc.OwnedCreate(context.Background(), 7, &model.MCPProjectReq{
		Name: "自己的画布", CanvasCode: canvasCode(18), Scopes: []string{model.MCPScopeCanvasRead},
	}); err != nil {
		t.Fatalf("默认画布属于自己时应该成功：%v", err)
	}
}

func TestMCPOwnedProjectsAreIsolated(t *testing.T) {
	f := newMCPFixture(t)
	mine, err := f.svc.OwnedCreate(context.Background(), 7, &model.MCPProjectReq{
		Name: "我的接入", Scopes: []string{model.MCPScopeCanvasRead},
	})
	if err != nil {
		t.Fatalf("自助建项目失败：%v", err)
	}
	projectID := mustID(t, mine.Item.ID)

	// 别人的列表里看不到
	others, err := f.svc.OwnedList(context.Background(), 9)
	if err != nil {
		t.Fatalf("列别人的项目失败：%v", err)
	}
	if len(others) != 0 {
		t.Fatalf("不该看到别人的项目，得到 %d 条", len(others))
	}
	own, err := f.svc.OwnedList(context.Background(), 7)
	if err != nil || len(own) != 1 {
		t.Fatalf("自己应该看到 1 条：%v %v", own, err)
	}

	// 改 / 删 / 签令牌 / 看令牌四件事，别人做都要按「不存在」回
	req := &model.MCPProjectReq{Name: "改别人的", Scopes: []string{model.MCPScopeCanvasRead}}
	if _, err := f.svc.OwnedUpdate(context.Background(), 9, projectID, req); !isCode(err, errcode.ErrMCPProjectNotFound) {
		t.Fatalf("改别人的项目应该拒绝，得到 %v", err)
	}
	if _, err := f.svc.OwnedListTokens(context.Background(), 9, projectID); !isCode(err, errcode.ErrMCPProjectNotFound) {
		t.Fatalf("看别人的令牌应该拒绝，得到 %v", err)
	}
	if _, err := f.svc.OwnedIssueToken(context.Background(), 9, projectID); !isCode(err, errcode.ErrMCPProjectNotFound) {
		t.Fatalf("给别人项目签令牌应该拒绝，得到 %v", err)
	}
	if err := f.svc.OwnedDelete(context.Background(), 9, projectID); !isCode(err, errcode.ErrMCPProjectNotFound) {
		t.Fatalf("删别人的项目应该拒绝，得到 %v", err)
	}

	// 本人操作正常
	out, err := f.svc.OwnedIssueToken(context.Background(), 7, projectID)
	if err != nil {
		t.Fatalf("本人签令牌失败：%v", err)
	}
	if _, err := f.svc.Authenticate(context.Background(), out.Token); err != nil {
		t.Fatalf("自助签出来的令牌应该能用：%v", err)
	}
}

func TestMCPOwnedProjectQuota(t *testing.T) {
	f := newMCPFixture(t)
	for i := 0; i < 10; i++ {
		if _, err := f.svc.OwnedCreate(context.Background(), 7, &model.MCPProjectReq{
			Name: "接入 " + strconv.Itoa(i), Scopes: []string{model.MCPScopeCanvasRead},
		}); err != nil {
			t.Fatalf("第 %d 个应该能建：%v", i+1, err)
		}
	}
	_, err := f.svc.OwnedCreate(context.Background(), 7, &model.MCPProjectReq{
		Name: "第十一个", Scopes: []string{model.MCPScopeCanvasRead},
	})
	if !isCode(err, errcode.ErrInvalidParams) {
		t.Fatalf("超过上限应该被拒，得到 %v", err)
	}
	// 别人的额度不受影响
	if _, err := f.svc.OwnedCreate(context.Background(), 9, &model.MCPProjectReq{
		Name: "别人的第一个", Scopes: []string{model.MCPScopeCanvasRead},
	}); err != nil {
		t.Fatalf("别人建项目不该受 7 的额度影响：%v", err)
	}
}
