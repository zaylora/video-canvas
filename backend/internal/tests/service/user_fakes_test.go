package service_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"video-canvas/internal/cache"
	"video-canvas/internal/model"
	"video-canvas/internal/repository"
)

// 本文件是用户 / 注册 / 后台用户 / 设置服务测试共用的 fake。

// fakeUserRepo 是内存版用户仓储，同时实现 service.UserRepo、service.AdminUserRepo 与事务原语 repository.UserTx。
// WithTx 直接在自身上执行（不做回滚）：回滚与并发语义由真实仓储的集成测试覆盖。
type fakeUserRepo struct {
	mu      sync.Mutex
	users   []*model.User
	credits map[uint64]*model.UserCredit
	ledger  []model.CreditLedger
	logs    []model.UserLoginLog
	rows    []model.AdminUserRow
	stats   *model.UserTaskStats
	lastF   repository.UserListFilter
	errs    map[string]error // 方法名 -> 注入的错误
	locked  int

	roleLocked int    // LockRoleChange 被调用的次数
	onTx       func() // WithTx 开始时执行，用于模拟并发竞态

	updates []map[string]any // Update 收到的字段，按调用顺序

	tasks   []model.AdminTaskItem // ListTasks 返回（已按 id 倒序，忽略过滤）
	ledgers []model.AdminLedgerItem
	logins  []model.AdminLoginItem
	lastTF  repository.TaskRecordFilter
	lastLF  repository.LedgerRecordFilter
	lastGF  repository.LoginRecordFilter
}

func newFakeUserRepo() *fakeUserRepo {
	return &fakeUserRepo{credits: map[uint64]*model.UserCredit{}, errs: map[string]error{}}
}

func (r *fakeUserRepo) seed(u model.User) *model.User {
	r.mu.Lock()
	defer r.mu.Unlock()
	u.ID = uint64(len(r.users) + 1)
	if u.Status == "" {
		u.Status = model.UserStatusActive
	}
	if u.Role == "" {
		u.Role = model.RoleUser
	}
	r.users = append(r.users, &u)
	return &u
}

func (r *fakeUserRepo) GetByID(_ context.Context, id uint64) (*model.User, error) {
	if err := r.errs["GetByID"]; err != nil {
		return nil, err
	}
	for _, u := range r.users {
		if u.ID == id {
			cp := *u
			return &cp, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (r *fakeUserRepo) GetByUsername(_ context.Context, name string) (*model.User, error) {
	if err := r.errs["GetByUsername"]; err != nil {
		return nil, err
	}
	for _, u := range r.users {
		if u.Username == name {
			cp := *u
			return &cp, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (r *fakeUserRepo) Update(_ context.Context, id uint64, fields map[string]any) error {
	if err := r.errs["Update"]; err != nil {
		return err
	}
	for _, u := range r.users {
		if u.ID == id {
			if t, ok := fields["last_login_at"].(time.Time); ok {
				u.LastLoginAt = &t
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
			r.updates = append(r.updates, fields)
			return nil
		}
	}
	return repository.ErrNotFound
}

func (r *fakeUserRepo) Count(context.Context) (int64, error) {
	if err := r.errs["Count"]; err != nil {
		return 0, err
	}
	return int64(len(r.users)), nil
}

func (r *fakeUserRepo) EmailExists(_ context.Context, email string) (bool, error) {
	for _, u := range r.users {
		if u.Email != "" && strings.EqualFold(u.Email, email) {
			return true, nil
		}
	}
	return false, nil
}

func (r *fakeUserRepo) UsernameExists(_ context.Context, name string) (bool, error) {
	for _, u := range r.users {
		if u.Username == name {
			return true, nil
		}
	}
	return false, nil
}

func (r *fakeUserRepo) LockRegistration(context.Context) error { r.locked++; return nil }

func (r *fakeUserRepo) Create(_ context.Context, u *model.User) error {
	if err := r.errs["Create"]; err != nil {
		return err
	}
	u.ID = uint64(len(r.users) + 1)
	cp := *u
	r.users = append(r.users, &cp)
	return nil
}

func (r *fakeUserRepo) CreateCredit(_ context.Context, userID uint64, initial int) error {
	if err := r.errs["CreateCredit"]; err != nil {
		return err
	}
	r.credits[userID] = &model.UserCredit{UserID: userID, Balance: initial}
	r.ledger = append(r.ledger, model.CreditLedger{UserID: userID, Type: model.LedgerInitial, Amount: initial})
	return nil
}

func (r *fakeUserRepo) InsertLoginLog(_ context.Context, l *model.UserLoginLog) error {
	if err := r.errs["InsertLoginLog"]; err != nil {
		return err
	}
	r.logs = append(r.logs, *l)
	return nil
}

func (r *fakeUserRepo) WithTx(_ context.Context, fn func(tx repository.UserTx) error) error {
	if r.onTx != nil {
		r.onTx() // 模拟“事务开始前，别的请求已改了数据”的竞态
	}
	return fn(r)
}

func (r *fakeUserRepo) LockRoleChange(context.Context) error {
	if err := r.errs["LockRoleChange"]; err != nil {
		return err
	}
	r.roleLocked++
	return nil
}

func (r *fakeUserRepo) CountByRole(_ context.Context, role string) (int64, error) {
	if err := r.errs["CountByRole"]; err != nil {
		return 0, err
	}
	var n int64
	for _, u := range r.users {
		if u.Role == role {
			n++
		}
	}
	return n, nil
}

// ResetPassword 模拟仓储的原子“改密码 + token_version +1”。
func (r *fakeUserRepo) ResetPassword(_ context.Context, id uint64, hash string) error {
	if err := r.errs["ResetPassword"]; err != nil {
		return err
	}
	for _, u := range r.users {
		if u.ID == id {
			u.Password = hash
			u.TokenVersion++
			return nil
		}
	}
	return repository.ErrNotFound
}

func (r *fakeUserRepo) ListAdmin(_ context.Context, f repository.UserListFilter) ([]model.AdminUserRow, int64, error) {
	if err := r.errs["ListAdmin"]; err != nil {
		return nil, 0, err
	}
	r.lastF = f
	rows := r.rows
	if f.ID != 0 {
		rows = nil
		for _, row := range r.rows {
			if row.ID == f.ID {
				rows = append(rows, row)
			}
		}
	}
	return rows, int64(len(rows)), nil
}

func (r *fakeUserRepo) TaskStats(context.Context, uint64) (*model.UserTaskStats, error) {
	if err := r.errs["TaskStats"]; err != nil {
		return nil, err
	}
	if r.stats == nil {
		return &model.UserTaskStats{}, nil
	}
	return r.stats, nil
}

func (r *fakeUserRepo) logResults() []string {
	var out []string
	for _, l := range r.logs {
		out = append(out, l.Kind+"/"+l.Result)
	}
	return out
}

// fakePolicy 实现 service.RegisterPolicy。
type fakePolicy struct {
	enabled  bool
	initial  int
	noVerify bool // 零值表示「注册需要验证邮箱」开着，和线上默认一致
	err      error
}

func (p *fakePolicy) RegisterEnabled(context.Context) (bool, error) { return p.enabled, p.err }
func (p *fakePolicy) VerifyEmail(context.Context) (bool, error)     { return !p.noVerify, p.err }
func (p *fakePolicy) InitialCredits(context.Context) (int, error)   { return p.initial, p.err }

// fakeMail 实现 service.RegisterMail。
type fakeMail struct {
	enabled bool
	sendErr error
	sent    []sentCode
}

type sentCode struct{ to, code string }

func (m *fakeMail) Enabled(context.Context) (bool, error) { return m.enabled, nil }
func (m *fakeMail) SendVerifyCode(_ context.Context, to, code string) error {
	if m.sendErr != nil {
		return m.sendErr
	}
	m.sent = append(m.sent, sentCode{to, code})
	return nil
}

func (m *fakeMail) lastCode() string {
	if len(m.sent) == 0 {
		return ""
	}
	return m.sent[len(m.sent)-1].code
}

// fakeAudit 实现 service.AdminAuditWriter。
type fakeAudit struct {
	mu     sync.Mutex
	logs   []model.AdminAuditLog
	recent []model.AdminAuditView
	err    error
}

func (a *fakeAudit) Insert(_ context.Context, l *model.AdminAuditLog) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.err != nil {
		return a.err
	}
	a.logs = append(a.logs, *l)
	return nil
}

func (a *fakeAudit) RecentForUser(context.Context, uint64, int) ([]model.AdminAuditView, error) {
	return a.recent, nil
}

func (a *fakeAudit) actions() []string {
	var out []string
	for _, l := range a.logs {
		out = append(out, l.Action)
	}
	return out
}

// fakeUserLimit 让测试能同时拿到缓存类型，避免各用例重复 new。
func noCache() *cache.UserCache { return cache.NewUserCache(nil) }

var errBoom = errors.New("boom")

// 以下三个方法让 fakeUserRepo 满足 service.AdminUserRepo 的记录查询：按 BeforeID / Limit 对预置数据做游标截取。
func (r *fakeUserRepo) ListTasks(_ context.Context, f repository.TaskRecordFilter) ([]model.AdminTaskItem, error) {
	if err := r.errs["ListTasks"]; err != nil {
		return nil, err
	}
	r.lastTF = f
	var out []model.AdminTaskItem
	for _, it := range r.tasks {
		if (f.BeforeID == 0 || it.ID < f.BeforeID) && len(out) < f.Limit {
			out = append(out, it)
		}
	}
	return out, nil
}

func (r *fakeUserRepo) ListLedger(_ context.Context, f repository.LedgerRecordFilter) ([]model.AdminLedgerItem, error) {
	if err := r.errs["ListLedger"]; err != nil {
		return nil, err
	}
	r.lastLF = f
	var out []model.AdminLedgerItem
	for _, it := range r.ledgers {
		if (f.BeforeID == 0 || it.ID < f.BeforeID) && len(out) < f.Limit {
			out = append(out, it)
		}
	}
	return out, nil
}

func (r *fakeUserRepo) ListLogins(_ context.Context, f repository.LoginRecordFilter) ([]model.AdminLoginItem, error) {
	if err := r.errs["ListLogins"]; err != nil {
		return nil, err
	}
	r.lastGF = f
	var out []model.AdminLoginItem
	for _, it := range r.logins {
		if (f.BeforeID == 0 || it.ID < f.BeforeID) && len(out) < f.Limit {
			out = append(out, it)
		}
	}
	return out, nil
}
