package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"video-canvas/internal/cache"
	. "video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/utils"
	"video-canvas/internal/repository"
	"video-canvas/internal/router"
	"video-canvas/internal/service"
)

// 本文件测试 /auth/*、/admin/users、/admin/settings 接口。路由用真实的 router.New（含 JWTAuth、RequireActive、RequireAdmin、RequireSuperAdmin），
// 服务用真实实现，仓储 / 邮件用内存 fake。

const (
	uaJWTSecret = "user-admin-handler-secret"
	uaSuper     = 1
	uaAdmin     = 2
	uaNormal    = 3
	uaDisabled  = 4
)

// ---- 内存 fake ----

type uaUserRepo struct {
	users   map[uint64]*model.User
	rows    []model.AdminUserRow
	ledgers []model.AdminLedgerItem // 非空时作为流水列表的返回
}

func (r *uaUserRepo) LockRoleChange(context.Context) error { return nil }
func (r *uaUserRepo) CountByRole(_ context.Context, role string) (int64, error) {
	var n int64
	for _, u := range r.users {
		if u.Role == role {
			n++
		}
	}
	return n, nil
}
func (r *uaUserRepo) ResetPassword(_ context.Context, id uint64, hash string) error {
	u, ok := r.users[id]
	if !ok {
		return repository.ErrNotFound
	}
	u.Password = hash
	u.TokenVersion++
	return nil
}

func (r *uaUserRepo) GetByID(_ context.Context, id uint64) (*model.User, error) {
	if u, ok := r.users[id]; ok {
		cp := *u
		return &cp, nil
	}
	return nil, repository.ErrNotFound
}

func (r *uaUserRepo) GetByUsername(_ context.Context, name string) (*model.User, error) {
	for _, u := range r.users {
		if u.Username == name {
			cp := *u
			return &cp, nil
		}
	}
	return nil, repository.ErrNotFound
}
func (r *uaUserRepo) Update(_ context.Context, id uint64, fields map[string]any) error {
	u, ok := r.users[id]
	if !ok {
		return repository.ErrNotFound
	}
	if v, ok := fields["status"].(string); ok {
		u.Status = v
	}
	if v, ok := fields["role"].(string); ok {
		u.Role = v
	}
	if v, ok := fields["max_active_tasks"]; ok {
		if n, isInt := v.(int); isInt {
			u.MaxActiveTasks = &n
		} else {
			u.MaxActiveTasks = nil
		}
	}
	return nil
}
func (r *uaUserRepo) Count(context.Context) (int64, error) { return int64(len(r.users)), nil }
func (r *uaUserRepo) EmailExists(_ context.Context, email string) (bool, error) {
	for _, u := range r.users {
		if u.Email != "" && strings.EqualFold(u.Email, email) {
			return true, nil
		}
	}
	return false, nil
}
func (r *uaUserRepo) InsertLoginLog(context.Context, *model.UserLoginLog) error { return nil }
func (r *uaUserRepo) WithTx(_ context.Context, fn func(tx repository.UserTx) error) error {
	return fn(r)
}
func (r *uaUserRepo) LockRegistration(context.Context) error { return nil }
func (r *uaUserRepo) UsernameExists(ctx context.Context, name string) (bool, error) {
	_, err := r.GetByUsername(ctx, name)
	return err == nil, nil
}
func (r *uaUserRepo) Create(_ context.Context, u *model.User) error {
	u.ID = uint64(100 + len(r.users))
	cp := *u
	r.users[u.ID] = &cp
	return nil
}
func (r *uaUserRepo) CreateCredit(context.Context, uint64, int) error { return nil }
func (r *uaUserRepo) ListAdmin(_ context.Context, f repository.UserListFilter) ([]model.AdminUserRow, int64, error) {
	var out []model.AdminUserRow
	for _, row := range r.rows {
		if (f.ID == 0 || row.ID == f.ID) && (f.Status == "" || row.Status == f.Status) {
			out = append(out, row)
		}
	}
	return out, int64(len(out)), nil
}
func (r *uaUserRepo) TaskStats(context.Context, uint64) (*model.UserTaskStats, error) {
	return &model.UserTaskStats{Total: 2, Success: 1}, nil
}

func (r *uaUserRepo) ListTasks(_ context.Context, f repository.TaskRecordFilter) ([]model.AdminTaskItem, error) {
	out := []model.AdminTaskItem{{ID: 3, Status: "succeeded"}, {ID: 2, Status: "failed"}, {ID: 1, Status: "running"}}
	return out[:min(len(out), f.Limit)], nil
}

func (r *uaUserRepo) ListLedger(_ context.Context, f repository.LedgerRecordFilter) ([]model.AdminLedgerItem, error) {
	if r.ledgers != nil {
		return r.ledgers, nil
	}
	out := []model.AdminLedgerItem{{ID: 2, Type: model.LedgerAdminAdjust, Amount: 5, Note: "补偿", OperatorName: "ops"}, {ID: 1, Type: model.LedgerInitial, Amount: 50}}
	return out[:min(len(out), f.Limit)], nil
}

func (r *uaUserRepo) ListLogins(_ context.Context, f repository.LoginRecordFilter) ([]model.AdminLoginItem, error) {
	out := []model.AdminLoginItem{{ID: 1, Kind: "login", Result: "ok", IP: "1.1.1.1"}}
	return out[:min(len(out), f.Limit)], nil
}

type uaSettingsRepo struct{ kv map[string]string }

func (r *uaSettingsRepo) GetAll(context.Context) (map[string]string, error) { return r.kv, nil }
func (r *uaSettingsRepo) SetMany(_ context.Context, kv map[string]string, _ uint64) error {
	for k, v := range kv {
		r.kv[k] = v
	}
	return nil
}

type uaSMTPRepo struct{ row *model.SMTPSetting }

func (r *uaSMTPRepo) Get(context.Context) (*model.SMTPSetting, error) {
	if r.row == nil {
		return nil, repository.ErrNotFound
	}
	cp := *r.row
	return &cp, nil
}
func (r *uaSMTPRepo) SaveConfig(_ context.Context, s *model.SMTPSetting) error {
	if r.row == nil {
		r.row = &model.SMTPSetting{}
	}
	pwd, nonce := r.row.PasswordEnc, r.row.PasswordNonce
	*r.row = *s
	r.row.PasswordEnc, r.row.PasswordNonce = pwd, nonce
	return nil
}
func (r *uaSMTPRepo) SetPassword(_ context.Context, enc, nonce []byte, _ uint64) error {
	if r.row == nil {
		r.row = &model.SMTPSetting{}
	}
	r.row.PasswordEnc, r.row.PasswordNonce = enc, nonce
	return nil
}
func (r *uaSMTPRepo) RecordCheck(context.Context, bool, string, time.Time) error { return nil }

type uaMailer struct {
	err  error
	sent []model.MailMessage
}

func (m *uaMailer) Send(_ context.Context, _ model.MailConfig, msg model.MailMessage) error {
	m.sent = append(m.sent, msg)
	return m.err
}

type uaAudit struct{}

func (uaAudit) Insert(context.Context, *model.AdminAuditLog) error { return nil }
func (uaAudit) RecentForUser(context.Context, uint64, int) ([]model.AdminAuditView, error) {
	return nil, nil
}

// ---- 环境 ----

type uaEnv struct {
	engine *gin.Engine
	credit *uaCreditStore
	cancel *uaCanceler
	conns  *uaConns
	users  *uaUserRepo
	smtp   *uaSMTPRepo
	mailer *uaMailer
	tokens map[int]string
}

type uaResp struct {
	Status int
	Code   int
	Msg    string
	Data   json.RawMessage
	Raw    string
}

func newUAEnv(t *testing.T) *uaEnv {
	t.Helper()
	hash := "$2a$04$wMqLIGoxs5HVNQ5s5VJ0HOu1m1Wk/9CqgMEY2ld7jV9nA1bUn2gHe" // 任意：这些用例不走密码校验成功路径以外的比对
	users := &uaUserRepo{users: map[uint64]*model.User{
		uaSuper:    {BaseModel: model.BaseModel{ID: uaSuper}, Username: "root", Role: model.RoleSuperAdmin, Status: model.UserStatusActive, Password: hash},
		uaAdmin:    {BaseModel: model.BaseModel{ID: uaAdmin}, Username: "ops", Role: model.RoleAdmin, Status: model.UserStatusActive, Password: hash},
		uaNormal:   {BaseModel: model.BaseModel{ID: uaNormal}, Username: "tom", Role: model.RoleUser, Status: model.UserStatusActive, Password: hash},
		uaDisabled: {BaseModel: model.BaseModel{ID: uaDisabled}, Username: "bad", Role: model.RoleUser, Status: model.UserStatusDisabled, Password: hash},
	}}
	users.rows = []model.AdminUserRow{
		{ID: 3, Username: "tom", Role: model.RoleUser, Status: model.UserStatusActive, Balance: 10, Frozen: 2, HasCreditAccount: true},
		{ID: 4, Username: "bad", Role: model.RoleUser, Status: model.UserStatusDisabled},
	}
	settings := service.NewSettingsService(&uaSettingsRepo{kv: map[string]string{}}, uaAudit{})
	smtpRepo := &uaSMTPRepo{}
	mailer := &uaMailer{}
	smtpSvc := service.NewSMTPService(smtpRepo, mailer, uaAudit{}, "handler-test-master-key", func(_ context.Context, host string) error {
		if strings.HasPrefix(host, "10.") || host == "127.0.0.1" {
			return errors.New("blocked: " + host)
		}
		return nil
	})
	// SMTPService 的主机校验在 service 里把任何 checker 错误转成 53006；这里的错误文本不是 ErrBlockedAddress，会走“无法解析”分支，足够验证接口层行为
	userSvc := service.NewUserService(service.UserDeps{
		Repo: users, Cache: cache.NewUserCache(nil), Codes: cache.NewMemoryRegisterCodeStore(time.Now), Policy: settings, Mail: smtpSvc,
		JWTSecret: uaJWTSecret, JWTIssuer: "test", JWTExpireHours: 1,
	})
	stateLookup := func(ctx context.Context, id uint64) (*middleware.UserState, error) {
		u, err := userSvc.State(ctx, id)
		if err != nil || u == nil {
			return nil, err
		}
		return &middleware.UserState{Role: u.Role, Status: u.Status, TokenVersion: u.TokenVersion}, nil
	}
	roleLookup := func(ctx context.Context, id uint64) (string, error) {
		st, err := stateLookup(ctx, id)
		if err != nil || st == nil {
			return "", err
		}
		return st.Role, nil
	}
	env := &uaEnv{users: users, smtp: smtpRepo, mailer: mailer, tokens: map[int]string{}, credit: newUACreditStore(), cancel: &uaCanceler{}, conns: &uaConns{}}
	env.credit.accounts[uaNormal] = &model.UserCredit{UserID: uaNormal, Balance: 100, Frozen: 30}
	for id := range users.users {
		tok, _, err := utils.GenerateToken(uint(id), users.users[id].Username, 0, uaJWTSecret, "test", 1)
		if err != nil {
			t.Fatal(err)
		}
		env.tokens[int(id)] = tok
	}
	env.engine = router.New(gin.TestMode, uaJWTSecret, router.Handlers{
		User: NewUserHandler(userSvc),
		AdminUser: NewAdminUserHandler(service.NewAdminUserService(users, uaAudit{}, settings, service.WithAdminControls(service.AdminControls{
			Credits: env.credit, Users: userSvc, Tasks: env.cancel, Conns: env.conns,
		}))),
		AdminSettings: NewAdminSettingsHandler(settings, smtpSvc),
		AdminMe:       NewAdminMeHandler(roleLookup),
		AdminRole:     roleLookup,
		UserState:     stateLookup,
	})
	return env
}

func (e *uaEnv) do(method, path string, body any, user int) uaResp {
	var buf *bytes.Reader
	switch b := body.(type) {
	case nil:
		buf = bytes.NewReader(nil)
	case string:
		buf = bytes.NewReader([]byte(b))
	default:
		raw, _ := json.Marshal(b)
		buf = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, buf)
	req.Header.Set("Content-Type", "application/json")
	if user > 0 {
		req.Header.Set("Authorization", "Bearer "+e.tokens[user])
	}
	w := httptest.NewRecorder()
	e.engine.ServeHTTP(w, req)
	var parsed struct {
		Code int             `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &parsed)
	return uaResp{Status: w.Code, Code: parsed.Code, Msg: parsed.Msg, Data: parsed.Data, Raw: w.Body.String()}
}

func (r uaResp) want(t *testing.T, status, code int) {
	t.Helper()
	if r.Status != status || r.Code != code {
		t.Fatalf("期望 HTTP %d / code %d，实际 %d / %d：%s", status, code, r.Status, r.Code, r.Raw)
	}
}

// ---- /auth ----

func TestAuth_Config(t *testing.T) {
	env := newUAEnv(t)
	r := env.do(http.MethodGet, "/api/v1/auth/config", nil, 0)
	r.want(t, 200, 0)
	var d model.AuthConfigView
	_ = json.Unmarshal(r.Data, &d)
	// 已有用户且 SMTP 未配置：开放注册，但没启用邮件服务就不验证邮箱
	if !d.RegisterEnabled || d.EmailVerifyRequired {
		t.Fatalf("%+v", d)
	}
	// 配好 SMTP 后要求验证码
	env.smtp.row = &model.SMTPSetting{Host: "smtp.example.com", Port: 587, Enabled: true, Encryption: "starttls", FromAddress: "a@x.com"}
	r = env.do(http.MethodGet, "/api/v1/auth/config", nil, 0)
	_ = json.Unmarshal(r.Data, &d)
	if !d.RegisterEnabled || !d.EmailVerifyRequired {
		t.Fatalf("%+v", d)
	}
}

func TestAuth_Register(t *testing.T) {
	t.Run("成功：注册即登录，返回 token 与角色", func(t *testing.T) {
		env := newUAEnv(t)
		env.users.users = map[uint64]*model.User{} // 空表：首个账号免验证码
		r := env.do(http.MethodPost, "/api/v1/auth/register", map[string]any{"username": "alice", "email": "a@b.com", "password": "secret1"}, 0)
		r.want(t, 200, 0)
		var d model.LoginView
		_ = json.Unmarshal(r.Data, &d)
		if d.Token == "" || d.Role != model.RoleSuperAdmin || d.ExpireAt == 0 {
			t.Fatalf("%+v", d)
		}
	})
	t.Run("参数校验失败：400 + 10001", func(t *testing.T) {
		env := newUAEnv(t)
		for _, body := range []any{
			map[string]any{"username": "alice", "password": "secret1"},                          // 缺邮箱
			map[string]any{"username": "alice", "email": "not-an-email", "password": "secret1"}, // 邮箱格式错
			map[string]any{"username": "al", "email": "a@b.com", "password": "secret1"},         // 用户名太短
			map[string]any{"username": "alice", "email": "a@b.com", "password": "123"},          // 密码太短
			"not json",
		} {
			env.do(http.MethodPost, "/api/v1/auth/register", body, 0).want(t, 400, errcode.ErrInvalidParams.Code)
		}
	})
	t.Run("表非空且 SMTP 未配置：免验证码注册成功，角色是普通用户", func(t *testing.T) {
		env := newUAEnv(t)
		r := env.do(http.MethodPost, "/api/v1/auth/register", map[string]any{"username": "alice", "email": "a@b.com", "password": "secret1"}, 0)
		r.want(t, 200, 0)
		var d model.LoginView
		_ = json.Unmarshal(r.Data, &d)
		if d.Role != model.RoleUser {
			t.Fatalf("%+v", d)
		}
	})
	t.Run("业务错误：SMTP 已启用且没带验证码 -> 400 + 53003", func(t *testing.T) {
		env := newUAEnv(t)
		env.smtp.row = &model.SMTPSetting{Host: "smtp.example.com", Port: 587, Enabled: true, Encryption: "starttls", FromAddress: "a@x.com"}
		env.do(http.MethodPost, "/api/v1/auth/register", map[string]any{"username": "alice", "email": "a@b.com", "password": "secret1"}, 0).
			want(t, 400, errcode.ErrCodeInvalid.Code)
	})
}

func TestAuth_RegisterCode(t *testing.T) {
	env := newUAEnv(t)
	body := map[string]any{"email": "new@b.com"}
	env.do(http.MethodPost, "/api/v1/auth/register/code", body, 0).want(t, 403, errcode.ErrRegisterClosed.Code)

	env.do(http.MethodPost, "/api/v1/auth/register/code", map[string]any{"email": "bad"}, 0).want(t, 400, errcode.ErrInvalidParams.Code)

	env.smtp.row = &model.SMTPSetting{Host: "smtp.example.com", Port: 587, Enabled: true, Encryption: "starttls", FromAddress: "a@x.com"}
	r := env.do(http.MethodPost, "/api/v1/auth/register/code", body, 0)
	r.want(t, 200, 0)
	if len(env.mailer.sent) != 1 {
		t.Fatalf("应发出一封验证码邮件：%d", len(env.mailer.sent))
	}
	// 同邮箱冷却内再发：429
	env.do(http.MethodPost, "/api/v1/auth/register/code", body, 0).want(t, 429, errcode.ErrTooManyReqs.Code)
	// 已注册邮箱：409
	env.users.users[9] = &model.User{Username: "x", Email: "taken@b.com"}
	env.do(http.MethodPost, "/api/v1/auth/register/code", map[string]any{"email": "TAKEN@b.com"}, 0).want(t, 409, errcode.ErrEmailExists.Code)
}

func TestAuth_Login(t *testing.T) {
	env := newUAEnv(t)
	// 用真实 bcrypt 哈希覆盖两个用户的密码
	u := env.users.users[uaNormal]
	u.Password = mustBcrypt(t, "secret1")
	env.users.users[uaDisabled].Password = mustBcrypt(t, "secret1")

	r := env.do(http.MethodPost, "/api/v1/auth/login", map[string]any{"username": "tom", "password": "secret1"}, 0)
	r.want(t, 200, 0)
	var d model.LoginView
	_ = json.Unmarshal(r.Data, &d)
	if d.Token == "" || d.Role != model.RoleUser {
		t.Fatalf("登录响应应带 role：%+v", d)
	}
	env.do(http.MethodPost, "/api/v1/auth/login", map[string]any{"username": "tom", "password": "wrongpw"}, 0).want(t, 401, errcode.ErrInvalidCredential.Code)
	env.do(http.MethodPost, "/api/v1/auth/login", map[string]any{"username": "tom"}, 0).want(t, 400, errcode.ErrInvalidParams.Code)
	env.do(http.MethodPost, "/api/v1/auth/login", map[string]any{"username": "bad", "password": "secret1"}, 0).want(t, 403, errcode.ErrAccountDisabled.Code)
}

// ---- 鉴权与收口 ----

func TestAuth_RequireActiveOnAuthGroup(t *testing.T) {
	env := newUAEnv(t)
	// 停用用户的旧 token 访问任何登录后接口：403 + 53004
	env.do(http.MethodGet, "/api/v1/admin/ai/me", nil, uaDisabled).want(t, 403, errcode.ErrAccountDisabled.Code)
	// 未登录
	env.do(http.MethodGet, "/api/v1/admin/users", nil, 0).want(t, 401, errcode.ErrUnauthorized.Code)
	// token_version 与库里不一致
	env.users.users[uaNormal].TokenVersion = 3
	env.do(http.MethodGet, "/api/v1/admin/ai/me", nil, uaNormal).want(t, 401, errcode.ErrUnauthorized.Code)
}

func TestRemovedUserEndpoints(t *testing.T) {
	env := newUAEnv(t)
	// GET /users 与 GET /users/:id 已删除：任何登录用户（含超管）都是 404
	env.do(http.MethodGet, "/api/v1/users", nil, uaNormal).want(t, 404, errcode.ErrNotFound.Code)
	env.do(http.MethodGet, "/api/v1/users/1", nil, uaSuper).want(t, 404, errcode.ErrNotFound.Code)
}

// ---- /admin/users ----

func TestAdminUsers(t *testing.T) {
	env := newUAEnv(t)
	t.Run("列表：admin 与 super_admin 可读，普通用户 403", func(t *testing.T) {
		env.do(http.MethodGet, "/api/v1/admin/users", nil, uaNormal).want(t, 403, errcode.ErrForbidden.Code)
		for _, u := range []int{uaAdmin, uaSuper} {
			r := env.do(http.MethodGet, "/api/v1/admin/users", nil, u)
			r.want(t, 200, 0)
			var page struct {
				List  []model.AdminUserListItem `json:"list"`
				Total int64                     `json:"total"`
			}
			_ = json.Unmarshal(r.Data, &page)
			if page.Total != 2 || len(page.List) != 2 || page.List[0].Available != 8 || page.List[0].EffectiveMaxActiveTasks != 4 {
				t.Fatalf("列表不对：%+v", page)
			}
			if !strings.Contains(r.Raw, `"has_credit_account"`) || !strings.Contains(r.Raw, `"effective_max_active_tasks"`) {
				t.Fatalf("字段名应为 snake_case：%s", r.Raw)
			}
		}
	})
	t.Run("筛选透传；非法 status 400", func(t *testing.T) {
		r := env.do(http.MethodGet, "/api/v1/admin/users?status=disabled", nil, uaAdmin)
		r.want(t, 200, 0)
		if !strings.Contains(r.Raw, `"total":1`) {
			t.Fatalf("%s", r.Raw)
		}
		env.do(http.MethodGet, "/api/v1/admin/users?status=weird", nil, uaAdmin).want(t, 400, errcode.ErrInvalidParams.Code)
	})
	t.Run("详情：成功、id 非法 400、不存在 404", func(t *testing.T) {
		r := env.do(http.MethodGet, "/api/v1/admin/users/3", nil, uaAdmin)
		r.want(t, 200, 0)
		for _, f := range []string{`"task_total":2`, `"task_success":1`, `"recent_audits":[]`, `"spent_credits"`, `"tasks_last_7d"`, `"email_verified_at"`} {
			if !strings.Contains(r.Raw, f) {
				t.Fatalf("详情缺少 %s：%s", f, r.Raw)
			}
		}
		env.do(http.MethodGet, "/api/v1/admin/users/abc", nil, uaAdmin).want(t, 400, errcode.ErrInvalidParams.Code)
		env.do(http.MethodGet, "/api/v1/admin/users/999", nil, uaAdmin).want(t, 404, errcode.ErrUserNotFound.Code)
	})
}

// ---- /admin/settings ----

func TestAdminSettings_Register(t *testing.T) {
	env := newUAEnv(t)
	r := env.do(http.MethodGet, "/api/v1/admin/settings/register", nil, uaAdmin)
	r.want(t, 200, 0)
	if !strings.Contains(r.Raw, `"register_enabled":true`) || !strings.Contains(r.Raw, `"initial_credits":50`) || !strings.Contains(r.Raw, `"default_max_active_tasks":4`) {
		t.Fatalf("%s", r.Raw)
	}
	body := map[string]any{"register_enabled": false, "initial_credits": 10, "default_max_active_tasks": 6}
	env.do(http.MethodPut, "/api/v1/admin/settings/register", body, uaAdmin).want(t, 403, errcode.ErrForbidden.Code) // 写仅 SA
	env.do(http.MethodPut, "/api/v1/admin/settings/register", body, uaSuper).want(t, 200, 0)
	r = env.do(http.MethodGet, "/api/v1/admin/settings/register", nil, uaAdmin)
	if !strings.Contains(r.Raw, `"register_enabled":false`) || !strings.Contains(r.Raw, `"default_max_active_tasks":6`) {
		t.Fatalf("保存后读到的不对：%s", r.Raw)
	}
	env.do(http.MethodPut, "/api/v1/admin/settings/register", map[string]any{"register_enabled": true, "initial_credits": -1, "default_max_active_tasks": 4}, uaSuper).
		want(t, 400, errcode.ErrInvalidParams.Code)
	env.do(http.MethodPut, "/api/v1/admin/settings/register", map[string]any{"register_enabled": true, "initial_credits": 1, "default_max_active_tasks": 65}, uaSuper).
		want(t, 400, errcode.ErrInvalidParams.Code)
	env.do(http.MethodGet, "/api/v1/admin/settings/register", nil, uaNormal).want(t, 403, errcode.ErrForbidden.Code)
}

func TestAdminSettings_SMTP(t *testing.T) {
	env := newUAEnv(t)
	good := map[string]any{"host": "smtp.example.com", "port": 587, "encryption": "starttls", "username": "mailer", "from_address": "noreply@example.com", "from_name": "画布", "enabled": true}

	t.Run("权限：读 admin，写 super_admin", func(t *testing.T) {
		env.do(http.MethodGet, "/api/v1/admin/settings/smtp", nil, uaAdmin).want(t, 200, 0)
		env.do(http.MethodPut, "/api/v1/admin/settings/smtp", good, uaAdmin).want(t, 403, errcode.ErrForbidden.Code)
		env.do(http.MethodPut, "/api/v1/admin/settings/smtp/password", map[string]any{"password": "x"}, uaAdmin).want(t, 403, errcode.ErrForbidden.Code)
		env.do(http.MethodPost, "/api/v1/admin/settings/smtp/test", map[string]any{"to": "a@b.com"}, uaAdmin).want(t, 403, errcode.ErrForbidden.Code)
	})
	t.Run("保存配置与密码；任何接口都读不回密码", func(t *testing.T) {
		env.do(http.MethodPut, "/api/v1/admin/settings/smtp", good, uaSuper).want(t, 200, 0)
		const pw = "S3cret-p@ssw0rd"
		env.do(http.MethodPut, "/api/v1/admin/settings/smtp/password", map[string]any{"password": pw}, uaSuper).want(t, 200, 0)
		for _, r := range []uaResp{
			env.do(http.MethodGet, "/api/v1/admin/settings/smtp", nil, uaSuper),
			env.do(http.MethodPut, "/api/v1/admin/settings/smtp", good, uaSuper),
		} {
			r.want(t, 200, 0)
			if strings.Contains(r.Raw, pw) || strings.Contains(strings.ToLower(r.Raw), `"password"`) || !strings.Contains(r.Raw, `"has_password":true`) {
				t.Fatalf("响应不能含密码，只给 has_password：%s", r.Raw)
			}
		}
		// 库里存的是密文
		if strings.Contains(string(env.smtp.row.PasswordEnc), pw) {
			t.Fatal("库里不能是明文")
		}
	})
	t.Run("内网主机被拒：400 + 53006", func(t *testing.T) {
		bad := map[string]any{"host": "127.0.0.1", "port": 25, "encryption": "none", "from_address": "a@x.com", "enabled": true}
		env.do(http.MethodPut, "/api/v1/admin/settings/smtp", bad, uaSuper).want(t, 400, errcode.ErrSMTPInvalid.Code)
	})
	t.Run("参数校验失败：400 + 10001", func(t *testing.T) {
		env.do(http.MethodPut, "/api/v1/admin/settings/smtp", map[string]any{"host": "a.com", "port": 70000, "encryption": "tls"}, uaSuper).want(t, 400, errcode.ErrInvalidParams.Code)
		env.do(http.MethodPut, "/api/v1/admin/settings/smtp", map[string]any{"host": "a.com", "port": 25, "encryption": "weird"}, uaSuper).want(t, 400, errcode.ErrInvalidParams.Code)
		env.do(http.MethodPut, "/api/v1/admin/settings/smtp/password", map[string]any{}, uaSuper).want(t, 400, errcode.ErrInvalidParams.Code)
		env.do(http.MethodPost, "/api/v1/admin/settings/smtp/test", map[string]any{"to": "nope"}, uaSuper).want(t, 400, errcode.ErrInvalidParams.Code)
	})
	t.Run("测试发信：成功，失败返回 502 + 53007 且文案不含密码", func(t *testing.T) {
		env.do(http.MethodPost, "/api/v1/admin/settings/smtp/test", map[string]any{"to": "a@b.com"}, uaSuper).want(t, 200, 0)
		env.mailer.err = errors.New("535 bad credentials S3cret-p@ssw0rd")
		r := env.do(http.MethodPost, "/api/v1/admin/settings/smtp/test", map[string]any{"to": "a@b.com"}, uaSuper)
		r.want(t, 502, errcode.ErrSMTPSendFailed.Code)
		if strings.Contains(r.Raw, "S3cret-p@ssw0rd") {
			t.Fatalf("文案不能含密码：%s", r.Raw)
		}
	})
}

func mustBcrypt(t *testing.T, pw string) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return string(h)
}
