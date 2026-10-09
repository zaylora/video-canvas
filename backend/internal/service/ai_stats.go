package service

import (
	"context"
	"sort"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
)

// AIStatsRepo 是总览统计的数据访问接口，由 repository.AIStatsRepository 实现，测试里用内存 fake 替换。
type AIStatsRepo interface {
	// DailyBuckets 统计 [from, to) 内创建的正式任务（不含试跑），按 tz 时区的自然日分桶，只返回有任务的日子。
	DailyBuckets(ctx context.Context, tz string, from, to time.Time) ([]model.AIStatsDay, error)
	// ModelKindCounts 统计 [from, to) 内创建的正式任务（不含试跑），按（模型, 类型）分组计数。
	ModelKindCounts(ctx context.Context, from, to time.Time) ([]model.AIStatsModelKind, error)
}

// AIStatsModels 是统计服务取模型展示名的依赖，由 AIConfigService 实现。
type AIStatsModels interface {
	// ListConfigs 列出所有模型配置的概览，Label 是展示名。
	ListConfigs(ctx context.Context) ([]ConfigListItem, error)
}

// AIStatsService 提供后台总览页的任务统计：近 7 / 30 天的每日任务量，以及按模型、按类型的调用占比。
type AIStatsService struct {
	repo   AIStatsRepo
	models AIStatsModels
	// Now 返回当前时间，测试可注入；NewAIStatsService 默认 time.Now。
	Now func() time.Time
}

// NewAIStatsService 创建统计服务。
func NewAIStatsService(repo AIStatsRepo, models AIStatsModels) *AIStatsService {
	return &AIStatsService{repo: repo, models: models, Now: time.Now}
}

// Stats 返回最近 days 天（含今天）的任务统计，days 只能是 7 或 30。
// 页面上的所有数字用同一个口径：创建时间落在区间内的正式任务，试跑不算；
// 每天分成成功 / 失败（含过期）/ 其他（取消与进行中）三个桶，三个桶之和就是当天的任务数。
func (s *AIStatsService) Stats(ctx context.Context, days int) (*model.AIStatsView, error) {
	// 1. 只接受 7 或 30：页面只有这两个档位，限定取值也避免任意天数的大范围聚合拖慢库
	if days != 7 && days != 30 {
		return nil, errcode.ErrInvalidParams.WithMsg("统计天数只能是 7 或 30")
	}

	// 2. 区间按上海时区的自然日算（与个人中心热力图的默认时区一致）：[含今天往前 days 天的 0 点, 明天 0 点)
	loc := activityLocation("")
	today := dateOf(s.Now(), loc)
	start := today.AddDate(0, 0, -(days - 1))
	end := today.AddDate(0, 0, 1)

	// 3. 每日三个桶：库里只返回有任务的日子，这里补齐成连续 days 天，没有任务的日子是 0，柱形图才不会缺柱子
	rows, err := s.repo.DailyBuckets(ctx, loc.String(), start, end)
	if err != nil {
		return nil, err
	}
	byDate := make(map[string]model.AIStatsDay, len(rows))
	for _, r := range rows {
		byDate[r.Date] = r
	}
	daily := make([]model.AIStatsDay, 0, days)
	for i := 0; i < days; i++ {
		date := start.AddDate(0, 0, i).Format(activityDateLayout)
		d, ok := byDate[date]
		if !ok {
			d = model.AIStatsDay{Date: date}
		}
		daily = append(daily, d)
	}

	// 4. 按（模型, 类型）取计数，折成按模型、按类型两张表
	counts, err := s.repo.ModelKindCounts(ctx, start, end)
	if err != nil {
		return nil, err
	}
	items, err := s.models.ListConfigs(ctx)
	if err != nil {
		return nil, err
	}
	labels := make(map[string]string, len(items))
	for _, it := range items {
		labels[it.Key] = it.Label
	}
	return &model.AIStatsView{Days: days, Daily: daily, ByModel: foldByModel(counts, labels), ByKind: foldByKind(counts)}, nil
}

// foldByModel 把（模型, 类型）计数合并成按模型的调用数，数量降序、同数量按 key 升序（排序稳定，图例不会来回跳）。
// 展示名取模型清单里的 label；模型已被删除或没有展示名时回退为 key。返回的切片永远不是 nil。
func foldByModel(rows []model.AIStatsModelKind, labels map[string]string) []model.AIStatsModelCount {
	sum := map[string]int{}
	for _, r := range rows {
		sum[r.Model] += r.Count
	}
	out := make([]model.AIStatsModelCount, 0, len(sum))
	for key, n := range sum {
		label := labels[key]
		if label == "" {
			label = key
		}
		out = append(out, model.AIStatsModelCount{Model: key, Label: label, Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Model < out[j].Model
	})
	return out
}

// foldByKind 把（模型, 类型）计数合并成按类型的调用数，数量降序、同数量按类型名升序。返回的切片永远不是 nil。
func foldByKind(rows []model.AIStatsModelKind) []model.AIStatsKindCount {
	sum := map[string]int{}
	for _, r := range rows {
		sum[r.Kind] += r.Count
	}
	out := make([]model.AIStatsKindCount, 0, len(sum))
	for kind, n := range sum {
		out = append(out, model.AIStatsKindCount{Kind: kind, Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}
