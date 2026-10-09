package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"video-canvas/internal/model"
)

// AIStatsRepository 是后台总览统计的数据访问：只读 generation_tasks 做聚合，不写任何数据。
type AIStatsRepository struct {
	db *gorm.DB
}

// NewAIStatsRepository 创建总览统计仓储。
func NewAIStatsRepository(db *gorm.DB) *AIStatsRepository {
	return &AIStatsRepository{db: db}
}

// DailyBuckets 统计 [from, to) 内创建的正式任务（is_test = FALSE），按 tz 时区的自然日分桶，
// 只返回有任务的日子，日期升序；调用方负责把缺的日子补成 0。
// 三个桶：succeeded = 成功；failed = failed + expired；other = 取消与所有进行中，三者相加就是当天的任务数。
// tz 必须是调用方用 time.LoadLocation 校验过的 IANA 名称（作为参数绑定，不拼进 SQL）。
// 范围条件用绝对时间，走 idx_task_created（created_at 单列索引）。
func (r *AIStatsRepository) DailyBuckets(ctx context.Context, tz string, from, to time.Time) ([]model.AIStatsDay, error) {
	var rows []model.AIStatsDay
	err := r.db.WithContext(ctx).Raw(`
		SELECT to_char((created_at AT TIME ZONE ?)::date, 'YYYY-MM-DD') AS date,
		  COUNT(*) FILTER (WHERE status = 'succeeded') AS succeeded,
		  COUNT(*) FILTER (WHERE status IN ('failed','expired')) AS failed,
		  COUNT(*) FILTER (WHERE status NOT IN ('succeeded','failed','expired')) AS other
		FROM generation_tasks
		WHERE is_test = FALSE AND created_at >= ? AND created_at < ?
		GROUP BY 1 ORDER BY 1`, tz, from, to).Scan(&rows).Error
	return rows, err
}

// ModelKindCounts 统计 [from, to) 内创建的正式任务（is_test = FALSE），按（model_id, kind）分组计数，没有任务时返回空切片。
// 与 DailyBuckets 同一个口径（状态不过滤），所以按模型的合计等于每日三个桶的合计。
func (r *AIStatsRepository) ModelKindCounts(ctx context.Context, from, to time.Time) ([]model.AIStatsModelKind, error) {
	var rows []model.AIStatsModelKind
	err := r.db.WithContext(ctx).Raw(`
		SELECT model_id AS model, kind, COUNT(*) AS count
		FROM generation_tasks
		WHERE is_test = FALSE AND created_at >= ? AND created_at < ?
		GROUP BY model_id, kind`, from, to).Scan(&rows).Error
	return rows, err
}
