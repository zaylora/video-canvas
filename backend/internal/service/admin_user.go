package service

import (
	"context"
	"errors"
	"strings"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
	"video-canvas/pkg/pagination"
)

// AdminUserRepo 是后台用户查询的数据访问接口（真实实现是 repository.UserRepository）。
type AdminUserRepo interface {
	// GetByID 按 id 查询用户，不存在返回 repository.ErrNotFound。
	GetByID(ctx context.Context, id uint64) (*model.User, error)
	// ListAdmin 分页查询用户列表（id 倒序，联积分与进行中任务数），同时返回总数。
	ListAdmin(ctx context.Context, f repository.UserListFilter) ([]model.AdminUserRow, int64, error)
	// TaskStats 汇总用户的正式任务统计与已结算积分。
	TaskStats(ctx context.Context, userID uint64) (*model.UserTaskStats, error)
	// Update 按 id 更新指定字段（并发上限、状态），用户不存在返回 repository.ErrNotFound。
	Update(ctx context.Context, id uint64, fields map[string]any) error
	// ResetPassword 原子地改密码哈希并把 token_version +1，用户不存在返回 repository.ErrNotFound。
	ResetPassword(ctx context.Context, id uint64, hash string) error
	// WithTx 在一个事务里执行 fn（调整角色用），fn 返回错误整体回滚。
	WithTx(ctx context.Context, fn func(tx repository.UserTx) error) error
	// ListTasks 查询用户的生成记录（排除试跑，id 倒序，游标分页）。
	ListTasks(ctx context.Context, f repository.TaskRecordFilter) ([]model.AdminTaskItem, error)
	// ListLedger 查询用户的积分流水（联操作人用户名，id 倒序，游标分页）。
	ListLedger(ctx context.Context, f repository.LedgerRecordFilter) ([]model.AdminLedgerItem, error)
	// ListLogins 查询用户的登录记录（id 倒序，游标分页）。
	ListLogins(ctx context.Context, f repository.LoginRecordFilter) ([]model.AdminLoginItem, error)
}

// AdminUserSettings 读取后台用户管理要用的系统设置，由 SettingsService 实现。
type AdminUserSettings interface {
	// DefaultMaxActiveTasks 全局默认并发上限（用户列表要用它算“实际生效上限”）。
	DefaultMaxActiveTasks(ctx context.Context) (int, error)
	// InitialCredits 新积分账户的初始积分（给还没有账户的老用户调整积分时，先按它建账户）。
	InitialCredits(ctx context.Context) (int, error)
}

// adminUserMaxPageSize 是后台用户列表的单页上限（比通用分页的 50 大，运营需要一屏看更多）。
const adminUserMaxPageSize = 100

// AdminUserService 提供后台用户管理：查询（列表、详情、三个记录列表）与管控（积分、并发上限、封禁 / 启用、批量）。
type AdminUserService struct {
	repo     AdminUserRepo
	audit    AdminAuditWriter
	defaults AdminUserSettings
	ctl      AdminControls
}

// NewAdminUserService 创建后台用户服务。管控依赖通过 WithAdminControls 注入；只做查询时可以不传。
func NewAdminUserService(repo AdminUserRepo, audit AdminAuditWriter, defaults AdminUserSettings, opts ...AdminUserOption) *AdminUserService {
	s := &AdminUserService{repo: repo, audit: audit, defaults: defaults}
	for _, o := range opts {
		o(s)
	}
	return s
}

// List 分页查询用户，返回列表项、总数和修正后的分页参数（供响应回显）。
func (s *AdminUserService) List(ctx context.Context, req *model.ListAdminUserReq) ([]model.AdminUserListItem, int64, pagination.Query, error) {
	// 1. 修正分页：沿用通用规则（page<1 取 1，page_size<1 取 10），上限放宽到 100
	q := pagination.Query{Page: req.Page, PageSize: req.PageSize}
	q.Normalize()
	if req.PageSize > q.PageSize {
		q.PageSize = min(req.PageSize, adminUserMaxPageSize)
	}

	// 2. 读全局默认并发上限：每行的“实际生效上限”要用它兜底单用户覆盖
	defMax, err := s.defaults.DefaultMaxActiveTasks(ctx)
	if err != nil {
		return nil, 0, q, err
	}

	// 3. 查询并补算可用积分与实际生效的并发上限；返回的切片永远不是 nil
	rows, total, err := s.repo.ListAdmin(ctx, repository.UserListFilter{
		Q: strings.TrimSpace(req.Q), Status: req.Status, Role: req.Role, Offset: q.Offset(), Limit: q.Limit(),
	})
	if err != nil {
		return nil, 0, q, err
	}
	items := make([]model.AdminUserListItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, toAdminListItem(r, defMax))
	}
	return items, total, q, nil
}

// toAdminListItem 把查询行补成列表项：可用 = 余额 - 冻结；实际并发上限 = 单用户覆盖，没有则用全局默认。
func toAdminListItem(r model.AdminUserRow, defMax int) model.AdminUserListItem {
	eff := defMax
	if r.MaxActiveTasks != nil {
		eff = *r.MaxActiveTasks
	}
	return model.AdminUserListItem{AdminUserRow: r, Available: r.Balance - r.Frozen, EffectiveMaxActiveTasks: eff}
}

// recentAuditLimit 是用户详情里展示的最近审计条数。
const recentAuditLimit = 3

// Get 返回用户详情：列表项字段 + 邮箱验证时间 + 任务统计 + 最近 3 条审计。用户不存在返回 ErrUserNotFound。
func (s *AdminUserService) Get(ctx context.Context, id uint64) (*model.AdminUserDetail, error) {
	// 1. 用户必须存在（顺带拿邮箱验证时间）
	u, err := s.repo.GetByID(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}

	// 2. 取该用户的列表行（与列表同一套联表查询，保证两处数字一致）
	defMax, err := s.defaults.DefaultMaxActiveTasks(ctx)
	if err != nil {
		return nil, err
	}
	rows, _, err := s.repo.ListAdmin(ctx, repository.UserListFilter{ID: id, Limit: 1})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errcode.ErrUserNotFound // 两次查询之间被删除
	}

	// 3. 任务统计与最近审计
	stats, err := s.repo.TaskStats(ctx, id)
	if err != nil {
		return nil, err
	}
	audits, err := s.audit.RecentForUser(ctx, id, recentAuditLimit)
	if err != nil {
		return nil, err
	}
	if audits == nil {
		audits = []model.AdminAuditView{}
	}
	return &model.AdminUserDetail{
		AdminUserListItem: toAdminListItem(rows[0], defMax),
		EmailVerifiedAt:   u.EmailVerifiedAt,
		UserTaskStats:     *stats,
		RecentAudits:      audits,
	}, nil
}
