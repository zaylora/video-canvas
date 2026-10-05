package service

import (
	"context"
	"errors"

	"video-canvas/internal/model"
	"video-canvas/internal/repository"
)

// TaskLimits 提供生成任务服务需要的两个运行时参数：新账户的初始积分、用户的并发上限。
// 为 nil 时任务服务退回代码常量 DefaultInitialCredits / DefaultMaxActiveTasks（保持老行为，单测里不用组装设置服务）。
type TaskLimits interface {
	// InitialCredits 新积分账户的初始积分（系统设置优先，缺省 DefaultInitialCredits）。
	InitialCredits(ctx context.Context) (int, error)
	// MaxActiveTasks 用户实际生效的并发上限：单用户覆盖 > 系统设置默认 > DefaultMaxActiveTasks。
	MaxActiveTasks(ctx context.Context, userID uint64) (int, error)
}

// taskLimitUsers 是 TaskLimitService 读取用户覆盖值的依赖。
type taskLimitUsers interface {
	// GetByID 按 id 查询用户，不存在返回 repository.ErrNotFound。
	GetByID(ctx context.Context, id uint64) (*model.User, error)
}

// TaskLimitService 是 TaskLimits 的真实实现：把系统设置与用户表里的覆盖值合成任务服务要用的数字。
type TaskLimitService struct {
	settings *SettingsService
	users    taskLimitUsers
}

// NewTaskLimitService 创建任务限额服务。
func NewTaskLimitService(settings *SettingsService, users taskLimitUsers) *TaskLimitService {
	return &TaskLimitService{settings: settings, users: users}
}

// InitialCredits 实现 TaskLimits。
func (l *TaskLimitService) InitialCredits(ctx context.Context) (int, error) {
	return l.settings.InitialCredits(ctx)
}

// MaxActiveTasks 实现 TaskLimits：先看用户自己的覆盖值（直接读库，不走缓存，保证改完立刻生效），没有再用全局默认。
func (l *TaskLimitService) MaxActiveTasks(ctx context.Context, userID uint64) (int, error) {
	u, err := l.users.GetByID(ctx, userID)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return 0, err
	}
	if err == nil && u.MaxActiveTasks != nil && *u.MaxActiveTasks > 0 {
		return *u.MaxActiveTasks, nil
	}
	return l.settings.DefaultMaxActiveTasks(ctx)
}
