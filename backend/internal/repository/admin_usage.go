package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"video-canvas/internal/model"
)

// AdminUsageRepository 是后台「使用记录」的只读查询：全站生成任务的流水、摘要与详情。
type AdminUsageRepository struct {
	db *gorm.DB
}

// NewAdminUsageRepository 创建使用记录仓储。
func NewAdminUsageRepository(db *gorm.DB) *AdminUsageRepository {
	return &AdminUsageRepository{db: db}
}

// UsageFilter 是使用记录的查询条件；列表与摘要共用，摘要与总数忽略 Offset 与 Limit。
type UsageFilter struct {
	UserKeyword string    // 非空：用户名或邮箱模糊匹配（大小写不敏感，通配符按字面量）
	TaskRef     string    // 非空：按任务编号精确匹配
	UserID      uint64    // 非 0：只看该用户
	Statuses    []string  // 为空不筛状态
	Kind        string    // 为空不筛类型
	Model       string    // 为空不筛模型（模型 key）
	From, To    time.Time // 创建时间范围，两端都包含；调用方保证都已设置
	IncludeTest bool      // false 时排除运营试跑任务
	Offset      int       // 跳过多少条（页码 - 1）× 每页条数
	Limit       int
}

// usageQuery 把筛选条件套到 generation_tasks（别名 t）上，列表与摘要共用，保证两边口径一致。
func (r *AdminUsageRepository) usageQuery(ctx context.Context, f UsageFilter) *gorm.DB {
	q := r.db.WithContext(ctx).Table("generation_tasks t").
		Where("t.created_at >= ? AND t.created_at <= ?", f.From, f.To)
	if !f.IncludeTest {
		q = q.Where("t.is_test = FALSE")
	}
	if f.UserID != 0 {
		q = q.Where("t.user_id = ?", f.UserID)
	}
	if len(f.Statuses) > 0 {
		q = q.Where("t.status IN ?", f.Statuses)
	}
	if f.Kind != "" {
		q = q.Where("t.kind = ?", f.Kind)
	}
	if f.Model != "" {
		q = q.Where("t.model_id = ?", f.Model)
	}
	if f.TaskRef != "" {
		q = q.Where("t.task_ref = ?", f.TaskRef)
	}
	if f.UserKeyword != "" {
		like := "%" + escapeLike(f.UserKeyword) + "%"
		q = q.Where("t.user_id IN (SELECT id FROM users WHERE username ILIKE ? OR email ILIKE ?)", like, like)
	}
	return q
}

// List 返回使用记录，id 倒序（id 与创建时间同向），联用户名；Offset / Limit 做分页。
// model_name 取任务快照 config_snapshot.model.label，没有或为空回落 model_id；
// duration_ms = finished_at - created_at（毫秒，最小 0），未完成为 NULL；charged_credits 未结算为 NULL。
func (r *AdminUsageRepository) List(ctx context.Context, f UsageFilter) ([]model.UsageRow, error) {
	q := r.usageQuery(ctx, f).
		Select(`t.id, t.task_ref, t.user_id, COALESCE(u.username, '') AS username, t.kind, t.model_id AS model_key,
			COALESCE(NULLIF(t.config_snapshot->'model'->>'label', ''), t.model_id) AS model_name,
			t.status, t.credits, t.charged_credits, t.error_code, t.error_message,
			CASE WHEN t.finished_at IS NULL THEN NULL
			     ELSE GREATEST((EXTRACT(EPOCH FROM (t.finished_at - t.created_at)) * 1000)::bigint, 0) END AS duration_ms,
			t.is_test, t.created_at`).
		Joins("LEFT JOIN users u ON u.id = t.user_id")
	var rows []model.UsageRow
	err := q.Order("t.id DESC").Offset(f.Offset).Limit(f.Limit).Scan(&rows).Error
	return rows, err
}

// Count 返回满足筛选条件的总数，列表翻页用；与 List 共用同一份条件，两边口径一致。
func (r *AdminUsageRepository) Count(ctx context.Context, f UsageFilter) (int64, error) {
	var n int64
	err := r.usageQuery(ctx, f).Count(&n).Error
	return n, err
}

// Summary 按状态分组统计任务数与实扣积分，由 service 折成摘要；筛选条件与 List 相同。
func (r *AdminUsageRepository) Summary(ctx context.Context, f UsageFilter) ([]model.UsageStatusCount, error) {
	var rows []model.UsageStatusCount
	err := r.usageQuery(ctx, f).
		Select("t.status AS status, COUNT(*) AS count, COALESCE(SUM(t.charged_credits), 0) AS charged").
		Group("t.status").Scan(&rows).Error
	return rows, err
}

// GetTaskByRef 按任务编号取任务（含试跑，不限用户），不存在返回 ErrNotFound。
func (r *AdminUsageRepository) GetTaskByRef(ctx context.Context, ref string) (*model.GenerationTask, error) {
	var t model.GenerationTask
	if err := r.db.WithContext(ctx).Where("task_ref = ?", ref).First(&t).Error; err != nil {
		return nil, translate(err)
	}
	return &t, nil
}

// GetUser 按 id 取用户，包含已软删除的（任务属于谁不能因为用户被删而查不到），不存在返回 ErrNotFound。
func (r *AdminUsageRepository) GetUser(ctx context.Context, id uint64) (*model.User, error) {
	var u model.User
	if err := r.db.WithContext(ctx).Unscoped().First(&u, id).Error; err != nil {
		return nil, translate(err)
	}
	return &u, nil
}

// ListEvents 返回任务的状态时间线（id 升序即发生顺序）；上线前的老任务没有事件，返回空切片。
func (r *AdminUsageRepository) ListEvents(ctx context.Context, taskID uint64) ([]model.GenerationTaskEvent, error) {
	evs := []model.GenerationTaskEvent{}
	err := r.db.WithContext(ctx).Where("task_id = ?", taskID).Order("id ASC").Find(&evs).Error
	return evs, err
}

// ListTaskLedger 返回任务的积分流水（freeze / settle / refund），id 升序。
func (r *AdminUsageRepository) ListTaskLedger(ctx context.Context, taskID uint64) ([]model.CreditLedger, error) {
	led := []model.CreditLedger{}
	err := r.db.WithContext(ctx).Where("task_id = ?", taskID).Order("id ASC").Find(&led).Error
	return led, err
}
