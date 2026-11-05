package service

import (
	"context"
	"slices"
)

// CleanupDramaDrafts 由 River 周期任务回收过期正文，不影响有效草稿。
func (s *DramaService) CleanupDramaDrafts(ctx context.Context) error {
	// 1. 不持有用户事务，过期判定与草稿读取使用同一数据库时间口径。
	return s.settings.CleanupDrafts(ctx)
}

// dramaStepSeconds 采用最近五十次任务的中位数，无历史时按设计默认六十秒。
func (s *DramaService) dramaStepSeconds(ctx context.Context, modelID uint64, step string) (int, error) {
	if s.settings == nil {
		return 60, nil
	}
	values, err := s.settings.RecentDramaDurations(ctx, modelID, step)
	if err != nil {
		return 0, err
	}
	if len(values) == 0 {
		return 60, nil
	}
	slices.Sort(values)
	middle := len(values) / 2
	if len(values)%2 == 0 {
		return max(1, (values[middle-1]+values[middle])/2), nil
	}
	return max(1, values[middle]), nil
}
