package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"video-canvas/internal/model"
)

// 后台「操作审计」的合并查询：admin_audit_logs 与 ai_audit_logs 各自的主键空间不同，
// 所以排序用 (created_at, source, id) 三元组，保证两张表交错时翻页的顺序稳定、不重复、不遗漏。

// AuditFilter 是操作审计的查询条件。
type AuditFilter struct {
	ActorID      *uint64   // 非 nil：按操作人 id 精确匹配，0 表示系统自动动作
	ActorKeyword string    // 非空：操作人用户名模糊匹配（大小写不敏感，通配符按字面量）
	ActionPrefix string    // 非空：动作名前缀，如 "channel."（模块筛选）
	Action       string    // 非空：完整动作名
	From, To     time.Time // 发生时间范围，两端都包含；调用方保证都已设置
	Offset       int       // 跳过多少条
	Limit        int
}

// mergedAudits 把两张审计表统一成相同的列：target_ref 在 admin 来源是 target_id 的十进制串，在 ai 来源是 target_key。
const mergedAudits = `(
	SELECT 'admin' AS source, id, actor_id, action, target_type, target_id::text AS target_ref, detail_json, created_at FROM admin_audit_logs
	UNION ALL
	SELECT 'ai' AS source, id, actor_id, action, target_type, target_key AS target_ref, detail_json, created_at FROM ai_audit_logs
) a`

// auditQuery 把筛选条件套到合并后的审计上，列表与总数共用，保证两边口径一致。
func (r *AdminAuditRepository) auditQuery(ctx context.Context, f AuditFilter) *gorm.DB {
	q := r.db.WithContext(ctx).Table(mergedAudits).
		Where("a.created_at >= ? AND a.created_at <= ?", f.From, f.To)
	if f.ActorID != nil {
		q = q.Where("a.actor_id = ?", *f.ActorID)
	}
	if f.ActorKeyword != "" {
		q = q.Where("a.actor_id IN (SELECT id FROM users WHERE username ILIKE ?)", "%"+escapeLike(f.ActorKeyword)+"%")
	}
	if f.ActionPrefix != "" {
		q = q.Where("a.action LIKE ?", escapeLike(f.ActionPrefix)+"%")
	}
	if f.Action != "" {
		q = q.Where("a.action = ?", f.Action)
	}
	return q
}

// ListMerged 返回合并后的审计记录，按 (created_at, source, id) 倒序，Offset / Limit 做分页。
func (r *AdminAuditRepository) ListMerged(ctx context.Context, f AuditFilter) ([]model.AuditRow, error) {
	var rows []model.AuditRow
	err := r.auditQuery(ctx, f).
		Select("a.source, a.id, a.actor_id, a.action, a.target_type, a.target_ref, a.detail_json, a.created_at").
		Order("a.created_at DESC, a.source DESC, a.id DESC").Offset(f.Offset).Limit(f.Limit).Scan(&rows).Error
	return rows, err
}

// CountMerged 返回满足筛选条件的总数，列表翻页用。
func (r *AdminAuditRepository) CountMerged(ctx context.Context, f AuditFilter) (int64, error) {
	var n int64
	err := r.auditQuery(ctx, f).Count(&n).Error
	return n, err
}

// UsernamesByIDs 批量查用户名（含已软删除的：历史操作人不能因为账号被删而查不到名字），不存在的 id 不在结果里。
func (r *AdminAuditRepository) UsernamesByIDs(ctx context.Context, ids []uint64) (map[uint64]string, error) {
	out := map[uint64]string{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []struct {
		ID       uint64
		Username string
	}
	if err := r.db.WithContext(ctx).Unscoped().Table("users").Select("id, username").Where("id IN ?", ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, u := range rows {
		out[u.ID] = u.Username
	}
	return out, nil
}
