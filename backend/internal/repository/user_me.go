package repository

import (
	"context"
	"time"

	"video-canvas/internal/model"
)

// 个人中心（/me/*）用到的查询：热力图、积分流水页码分页、头像反查。所有查询都带 user_id 条件（头像反查除外，见其注释）。

// LedgerPageFilter 是用户侧积分流水的页码分页条件。
type LedgerPageFilter struct {
	UserID uint64
	Types  []string // 为空不筛类型
	Offset int
	Limit  int
}

// ActivityDays 统计用户在 [from, to) 内创建的正式任务（is_test = FALSE），按 tz 时区的日期分桶，
// 只返回有任务的日子，按日期升序。tz 必须是调用方用 time.LoadLocation 校验过的 IANA 名称（作为参数绑定，不拼进 SQL）。
// created_at 是 timestamptz，AT TIME ZONE 把它换成该时区的本地时间后再取日期；范围条件用绝对时间，能走 idx_task_user_created。
func (r *UserRepository) ActivityDays(ctx context.Context, userID uint64, tz string, from, to time.Time) ([]model.ActivityDay, error) {
	var rows []model.ActivityDay
	err := r.db.WithContext(ctx).Raw(`
		SELECT to_char((created_at AT TIME ZONE ?)::date, 'YYYY-MM-DD') AS date,
		  COUNT(*) AS count,
		  COUNT(*) FILTER (WHERE kind = 'image') AS image,
		  COUNT(*) FILTER (WHERE kind = 'video') AS video,
		  COUNT(*) FILTER (WHERE kind = 'audio') AS audio,
		  COUNT(*) FILTER (WHERE kind = 'text') AS text
		FROM generation_tasks
		WHERE user_id = ? AND is_test = FALSE AND created_at >= ? AND created_at < ?
		GROUP BY 1 ORDER BY 1`, tz, userID, from, to).Scan(&rows).Error
	return rows, err
}

// ListLedgerPage 按 created_at DESC, id DESC 分页查询用户的积分流水（条件 user_id，可选 type IN）。
// id 作为次序键，保证同一时间戳的流水翻页时不重复、不遗漏；不查 operator_id（不向用户展示操作人）。
func (r *UserRepository) ListLedgerPage(ctx context.Context, f LedgerPageFilter) ([]model.MeLedgerItem, error) {
	q := r.db.WithContext(ctx).Table("credit_ledger").
		Select("id, type, amount, task_id, agent_call_id, note, created_at").
		Where("user_id = ?", f.UserID)
	if len(f.Types) > 0 {
		q = q.Where("type IN ?", f.Types)
	}
	var rows []model.MeLedgerItem
	err := q.Order("created_at DESC, id DESC").Offset(f.Offset).Limit(f.Limit).Scan(&rows).Error
	return rows, err
}

// CountLedger 统计用户的积分流水条数（条件 user_id，types 非空时再加 type IN）。
func (r *UserRepository) CountLedger(ctx context.Context, userID uint64, types []string) (int64, error) {
	q := r.db.WithContext(ctx).Table("credit_ledger").Where("user_id = ?", userID)
	if len(types) > 0 {
		q = q.Where("type IN ?", types)
	}
	var n int64
	err := q.Count(&n).Error
	return n, err
}

// AvatarStorageID 按头像 key 反查它所在的存储 id（条件 avatar_key = key 且用户未删除），给不鉴权的 /files 路由用；
// 只有“当前正在使用”的头像能查到，被替换 / 移除的旧 key 返回 ErrNotFound。key 为空直接返回 ErrNotFound。
func (r *UserRepository) AvatarStorageID(ctx context.Context, key string) (uint64, error) {
	if key == "" {
		return 0, ErrNotFound
	}
	var u model.User
	err := r.db.WithContext(ctx).Select("avatar_storage_id").Where("avatar_key = ?", key).First(&u).Error
	if err != nil {
		return 0, translate(err)
	}
	return u.AvatarStorageID, nil
}
