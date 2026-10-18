package service

import (
	"context"
	"strconv"
	"strings"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
	"video-canvas/pkg/pagination"
)

// 后台「日志管理 › 操作审计」：把 admin_audit_logs 与 ai_audit_logs 合并成一个只读列表。admin 与 super_admin 同权。

const auditDefaultSpan = 30 * 24 * time.Hour // 没传时间范围时的默认跨度；审计变更频率低，不限制最大跨度

// AuditLogRepo 是操作审计的数据访问接口，由 repository.AdminAuditRepository 实现。
type AuditLogRepo interface {
	// ListMerged 返回合并后的一页审计记录（Offset / Limit），按 (created_at, source, id) 倒序。
	ListMerged(ctx context.Context, f repository.AuditFilter) ([]model.AuditRow, error)
	// CountMerged 返回满足筛选条件的总数，翻页用。
	CountMerged(ctx context.Context, f repository.AuditFilter) (int64, error)
	// UsernamesByIDs 批量查用户名（含已软删除的），不存在的 id 不在结果里。
	UsernamesByIDs(ctx context.Context, ids []uint64) (map[uint64]string, error)
}

// AuditLogService 实现操作审计的查询。
type AuditLogService struct {
	repo AuditLogRepo
	now  func() time.Time
}

// AuditLogOption 是 AuditLogService 的可选配置。
type AuditLogOption func(*AuditLogService)

// WithAuditClock 替换时钟（测试用，固定“现在”以验证默认时间范围）。
func WithAuditClock(now func() time.Time) AuditLogOption {
	return func(s *AuditLogService) { s.now = now }
}

// NewAuditLogService 创建操作审计服务。
func NewAuditLogService(repo AuditLogRepo, opts ...AuditLogOption) *AuditLogService {
	s := &AuditLogService{repo: repo, now: time.Now}
	for _, o := range opts {
		o(s)
	}
	return s
}

// List 返回合并后的操作审计（page / page_size 分页，沿用其他后台列表的 {list,total,page,page_size} 契约），按发生时间倒序。
func (s *AuditLogService) List(ctx context.Context, req *model.ListAuditReq) (*pagination.Result[model.AuditItem], error) {
	// 1. 时间范围：缺 to 取现在，缺 from 取 to 往前 30 天；格式错误或 from 不早于 to 是参数错误
	to := s.now()
	var err error
	if req.To != "" {
		if to, err = time.Parse(time.RFC3339, req.To); err != nil {
			return nil, errcode.ErrInvalidParams.WithMsg("结束时间格式不正确，应为 RFC3339")
		}
	}
	from := to.Add(-auditDefaultSpan)
	if req.From != "" {
		if from, err = time.Parse(time.RFC3339, req.From); err != nil {
			return nil, errcode.ErrInvalidParams.WithMsg("开始时间格式不正确，应为 RFC3339")
		}
	}
	if !from.Before(to) {
		return nil, errcode.ErrInvalidParams.WithMsg("开始时间必须早于结束时间")
	}
	// 2. 分页：页码最小 1，每页缺省 20、最多 100
	q := pagination.AdminQuery(req.Page, req.PageSize)
	f := repository.AuditFilter{From: from, To: to, Action: req.Action, Offset: q.Offset(), Limit: q.Limit()}
	// 3. 操作人：数字按 id；「系统」「system」是系统自动动作（actor_id=0）；其余当作用户名片段
	f.ActorID, f.ActorKeyword = parseAuditActor(req.Actor)
	// 4. 模块：动作名点号前的前缀，如 channel → channel.*
	if req.Module != "" {
		f.ActionPrefix = req.Module + "."
	}
	rows, err := s.repo.ListMerged(ctx, f)
	if err != nil {
		return nil, err
	}
	total, err := s.repo.CountMerged(ctx, f)
	if err != nil {
		return nil, err
	}
	// 5. 补用户名：只查本页出现的操作人和「目标是用户」的对象；ai 来源的 target_ref 是 key，不当用户 id
	names, err := s.repo.UsernamesByIDs(ctx, auditUserIDs(rows))
	if err != nil {
		return nil, err
	}
	items := make([]model.AuditItem, len(rows)) // 永远不是 nil，序列化为 []
	for i, r := range rows {
		it := model.AuditItem{
			Source: r.Source, ID: r.ID, ActorID: r.ActorID, ActorName: names[r.ActorID], Action: r.Action,
			TargetType: r.TargetType, TargetRef: r.TargetRef, DetailJSON: r.DetailJSON, CreatedAt: r.CreatedAt,
		}
		if id, ok := auditTargetUserID(r); ok {
			it.TargetName = names[id]
		}
		items[i] = it
	}
	return &pagination.Result[model.AuditItem]{List: items, Total: total, Page: q.Page, PageSize: q.PageSize}, nil
}

// parseAuditActor 把操作人输入拆成 id 精确匹配或用户名关键字；空白返回两个零值（不筛）。
func parseAuditActor(in string) (*uint64, string) {
	q := strings.TrimSpace(in)
	if q == "" {
		return nil, ""
	}
	if n, err := strconv.ParseUint(q, 10, 64); err == nil {
		return &n, ""
	}
	if q == "系统" || strings.EqualFold(q, "system") {
		zero := uint64(0)
		return &zero, ""
	}
	return nil, q
}

// auditTargetUserID 返回审计对象是 admin 来源的用户时的用户 id。
func auditTargetUserID(r model.AuditRow) (uint64, bool) {
	if r.Source != model.AuditSourceAdmin || r.TargetType != model.AdminAuditTargetUser {
		return 0, false
	}
	id, err := strconv.ParseUint(r.TargetRef, 10, 64)
	return id, err == nil && id > 0
}

// auditUserIDs 收集本页要查用户名的 id（去重，不含 0：系统动作没有用户名）。
func auditUserIDs(rows []model.AuditRow) []uint64 {
	seen := map[uint64]bool{}
	var ids []uint64
	add := func(id uint64) {
		if id != 0 && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	for _, r := range rows {
		add(r.ActorID)
		if id, ok := auditTargetUserID(r); ok {
			add(id)
		}
	}
	return ids
}
