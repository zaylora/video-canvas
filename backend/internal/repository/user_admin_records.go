package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"video-canvas/internal/model"
)

// 后台用户详情的三类记录查询：都是“按 id 倒序 + id < BeforeID 的游标分页”，由 service 多取一条判断是否还有下一页。

// TaskRecordFilter 是生成记录的查询条件。
type TaskRecordFilter struct {
	UserID   uint64
	Statuses []string // 为空不筛状态
	BeforeID uint64   // 非 0 时只返回 id 更小的记录（游标）
	Limit    int
}

// LedgerRecordFilter 是积分流水的查询条件。
type LedgerRecordFilter struct {
	UserID   uint64
	Types    []string // 为空不筛类型
	BeforeID uint64
	Limit    int
}

// LoginRecordFilter 是登录记录的查询条件。
type LoginRecordFilter struct {
	UserID   uint64
	Results  []string // 为空不筛结果
	BeforeID uint64
	Limit    int
}

// ListTasks 查询用户的生成记录（正式任务，排除 is_test 试跑），id 倒序。
// model_name 取任务快照 config_snapshot.model.label，没有或为空回落 model_id；
// duration_ms = finished_at - COALESCE(submitted_at, created_at)（毫秒，最小 0），未完成为 NULL；charged_credits 未结算按 0。
func (r *UserRepository) ListTasks(ctx context.Context, f TaskRecordFilter) ([]model.AdminTaskItem, error) {
	q := r.db.WithContext(ctx).Table("generation_tasks").
		Select(`id, kind, model_id AS model_key,
			COALESCE(NULLIF(config_snapshot->'model'->>'label', ''), model_id) AS model_name,
			status, credits, COALESCE(charged_credits, 0) AS charged_credits, error_message AS error,
			CASE WHEN finished_at IS NULL THEN NULL
			     ELSE GREATEST((EXTRACT(EPOCH FROM (finished_at - COALESCE(submitted_at, created_at))) * 1000)::bigint, 0) END AS duration_ms,
			created_at`).
		Where("user_id = ? AND is_test = FALSE", f.UserID)
	if len(f.Statuses) > 0 {
		q = q.Where("status IN ?", f.Statuses)
	}
	q = beforeID(q, f.BeforeID)
	var rows []model.AdminTaskItem
	err := q.Order("id DESC").Limit(f.Limit).Scan(&rows).Error
	return rows, err
}

// ListLedger 查询用户的积分流水，联操作人用户名（系统流水为空串），id 倒序。
func (r *UserRepository) ListLedger(ctx context.Context, f LedgerRecordFilter) ([]model.AdminLedgerItem, error) {
	q := r.db.WithContext(ctx).Table("credit_ledger l").
		Select(`l.id, l.type, l.amount, l.task_id, l.note, l.operator_id, COALESCE(u.username, '') AS operator_name, l.created_at`).
		Joins("LEFT JOIN users u ON u.id = l.operator_id").
		Where("l.user_id = ?", f.UserID)
	if len(f.Types) > 0 {
		q = q.Where("l.type IN ?", f.Types)
	}
	if f.BeforeID != 0 {
		q = q.Where("l.id < ?", f.BeforeID)
	}
	var rows []model.AdminLedgerItem
	err := q.Order("l.id DESC").Limit(f.Limit).Scan(&rows).Error
	return rows, err
}

// ListLogins 查询用户的登录 / 注册记录，id 倒序。
func (r *UserRepository) ListLogins(ctx context.Context, f LoginRecordFilter) ([]model.AdminLoginItem, error) {
	q := r.db.WithContext(ctx).Table("user_login_logs").
		Select("id, kind, result, ip, user_agent, created_at").
		Where("user_id = ?", f.UserID)
	if len(f.Results) > 0 {
		q = q.Where("result IN ?", f.Results)
	}
	q = beforeID(q, f.BeforeID)
	var rows []model.AdminLoginItem
	err := q.Order("id DESC").Limit(f.Limit).Scan(&rows).Error
	return rows, err
}

// beforeID 在 id > 0 时追加游标条件（单表查询，列名为 id）。
func beforeID(q *gorm.DB, id uint64) *gorm.DB {
	if id == 0 {
		return q
	}
	return q.Where("id < ?", id)
}

// DeleteLoginLogsBefore 删除 created_at 早于 before 的登录记录，一次最多 limit 行，返回删除的行数。
// 分批删除（调用方循环到返回值 < limit 为止）让每条 DELETE 只持有很短的事务，不会长时间锁表、也不会产生巨量 WAL。
func (r *UserRepository) DeleteLoginLogsBefore(ctx context.Context, before time.Time, limit int) (int64, error) {
	res := r.db.WithContext(ctx).Exec(
		`DELETE FROM user_login_logs WHERE id IN (SELECT id FROM user_login_logs WHERE created_at < ? ORDER BY id LIMIT ?)`, before, limit)
	return res.RowsAffected, res.Error
}
