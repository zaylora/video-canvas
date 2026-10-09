package aiconfigfake

import (
	"context"
	"time"

	"video-canvas/internal/model"
)

// StatsRepo 是 service.AIStatsRepo 的内存实现：返回测试预设的数据，并记录收到的查询参数。
type StatsRepo struct {
	Daily      []model.AIStatsDay       // DailyBuckets 的返回值（只放有任务的日子）
	ModelKinds []model.AIStatsModelKind // ModelKindCounts 的返回值
	Err        error                    // 非空时两个方法都返回它

	TZ   string    // 最近一次 DailyBuckets 收到的时区
	From time.Time // 最近一次查询的区间起点（含）
	To   time.Time // 最近一次查询的区间终点（不含）
}

// DailyBuckets 记录查询参数并返回预设的按日数据。
func (r *StatsRepo) DailyBuckets(_ context.Context, tz string, from, to time.Time) ([]model.AIStatsDay, error) {
	r.TZ, r.From, r.To = tz, from, to
	return r.Daily, r.Err
}

// ModelKindCounts 记录查询参数并返回预设的（模型, 类型）计数。
func (r *StatsRepo) ModelKindCounts(_ context.Context, from, to time.Time) ([]model.AIStatsModelKind, error) {
	r.From, r.To = from, to
	return r.ModelKinds, r.Err
}
