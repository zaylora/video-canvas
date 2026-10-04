package service_test

import (
	"context"
	"errors"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	. "video-canvas/internal/service"
)

// fakeLimits 实现 service.TaskLimits：按用户返回并发上限，并给出初始积分。
type fakeLimits struct {
	initial int
	max     map[uint64]int
	defMax  int
	err     error
}

func (l *fakeLimits) InitialCredits(context.Context) (int, error) { return l.initial, l.err }
func (l *fakeLimits) MaxActiveTasks(_ context.Context, userID uint64) (int, error) {
	if v, ok := l.max[userID]; ok {
		return v, l.err
	}
	return l.defMax, l.err
}

func newLimitsEnv(limits TaskLimits) *taskSvcEnv {
	return newTaskSvcEnv(limits)
}

func TestGenerationTaskService_Limits(t *testing.T) {
	const user = uint64(1)
	ctx := context.Background()
	t.Run("单用户覆盖上限：默认是 2 但覆盖为 4，已有 3 个进行中仍可提交", func(t *testing.T) {
		env := newLimitsEnv(&fakeLimits{initial: 50, max: map[uint64]int{user: 4}, defMax: 2})
		env.repo.addCredit(user, 100, 0)
		for range 3 {
			env.repo.addTask(model.GenerationTask{UserID: user, Status: model.TaskRunning})
		}
		if _, err := createSingle(ctx, env.svc, user, "", validCreateReq()); err != nil {
			t.Fatalf("应放行：%v", err)
		}
	})
	t.Run("单用户覆盖为 1：已有 1 个进行中即被拒绝", func(t *testing.T) {
		env := newLimitsEnv(&fakeLimits{initial: 50, max: map[uint64]int{user: 1}, defMax: 5})
		env.repo.addCredit(user, 100, 0)
		env.repo.addTask(model.GenerationTask{UserID: user, Status: model.TaskRunning})
		_, err := createSingle(ctx, env.svc, user, "", validCreateReq())
		assertTaskCode(t, err, errcode.ErrTooManyTasks.Code)
	})
	t.Run("没有覆盖时用系统设置的默认上限（优先于代码常量）", func(t *testing.T) {
		env := newLimitsEnv(&fakeLimits{initial: 50, defMax: 1})
		env.repo.addCredit(user, 100, 0)
		env.repo.addTask(model.GenerationTask{UserID: user, Status: model.TaskPending})
		_, err := createSingle(ctx, env.svc, user, "", validCreateReq())
		assertTaskCode(t, err, errcode.ErrTooManyTasks.Code)
	})
	t.Run("新账户的初始积分取系统设置", func(t *testing.T) {
		env := newLimitsEnv(&fakeLimits{initial: 80, defMax: 2})
		got, err := env.svc.GetCredits(ctx, user)
		if err != nil || got.Balance != 80 {
			t.Fatalf("初始积分应为 80：%+v %v", got, err)
		}
	})
	t.Run("提交任务时惰性建账户同样用系统设置的初始积分", func(t *testing.T) {
		env := newLimitsEnv(&fakeLimits{initial: 60, defMax: 2})
		if _, err := createSingle(ctx, env.svc, user, "", validCreateReq()); err != nil {
			t.Fatal(err)
		}
		if acc := env.repo.credits[user]; acc == nil || acc.Balance != 60 || acc.Frozen != 10 {
			t.Fatalf("应以 60 建账户（余额 60、冻结 10）：%+v", acc)
		}
	})
	t.Run("读取设置失败透传（不静默回落）", func(t *testing.T) {
		env := newLimitsEnv(&fakeLimits{err: errBoom})
		if _, err := env.svc.GetCredits(ctx, user); !errors.Is(err, errBoom) {
			t.Fatalf("%v", err)
		}
		_, err := createSingle(ctx, env.svc, user, "", validCreateReq())
		assertPlainError(t, err)
	})
}

func TestTaskLimitService(t *testing.T) {
	ctx := context.Background()
	repo := newFakeUserRepo()
	three := 3
	u := repo.seed(model.User{Username: "a", MaxActiveTasks: &three})
	plain := repo.seed(model.User{Username: "b"})
	settings := newFakeSettingsRepo()
	settings.kv[model.SettingInitialCredits] = "33"
	settings.kv[model.SettingDefaultMaxActiveTasks] = "6"
	svc := NewTaskLimitService(NewSettingsService(settings, nil), repo)

	if n, err := svc.MaxActiveTasks(ctx, u.ID); err != nil || n != 3 {
		t.Fatalf("单用户覆盖优先：%d %v", n, err)
	}
	if n, err := svc.MaxActiveTasks(ctx, plain.ID); err != nil || n != 6 {
		t.Fatalf("无覆盖取系统设置：%d %v", n, err)
	}
	if n, err := svc.MaxActiveTasks(ctx, 999); err != nil || n != 6 {
		t.Fatalf("用户不存在取默认：%d %v", n, err)
	}
	if n, err := svc.InitialCredits(ctx); err != nil || n != 33 {
		t.Fatalf("初始积分取系统设置：%d %v", n, err)
	}
	repo.errs["GetByID"] = errBoom
	if _, err := svc.MaxActiveTasks(ctx, u.ID); !errors.Is(err, errBoom) {
		t.Fatalf("查询用户失败应透传：%v", err)
	}
}

// 没有注入 TaskLimits（也就是库里没有任何设置）时，任务服务回落到代码常量。
func TestGenerationTaskService_LimitsFallbackToConstants(t *testing.T) {
	const user = uint64(1)
	ctx := context.Background()

	if DefaultMaxActiveTasks != 4 || DefaultInitialCredits != 50 {
		t.Fatalf("常量应为并发 4、初始积分 50：%d %d", DefaultMaxActiveTasks, DefaultInitialCredits)
	}
	t.Run("并发上限回落到 4：3 个进行中放行，4 个进行中拒绝", func(t *testing.T) {
		env := newTaskSvcEnv(nil)
		env.repo.addCredit(user, 1000, 0)
		for range DefaultMaxActiveTasks - 1 {
			env.repo.addTask(model.GenerationTask{UserID: user, Status: model.TaskRunning})
		}
		if _, err := createSingle(ctx, env.svc, user, "", validCreateReq()); err != nil {
			t.Fatalf("第 4 个应放行：%v", err)
		}
		_, err := createSingle(ctx, env.svc, user, "", validCreateReq())
		assertTaskCode(t, err, errcode.ErrTooManyTasks.Code)
	})
	t.Run("初始积分回落到 50", func(t *testing.T) {
		env := newTaskSvcEnv(nil)
		got, err := env.svc.GetCredits(ctx, user)
		if err != nil || got.Balance != DefaultInitialCredits {
			t.Fatalf("初始积分应为 %d：%+v %v", DefaultInitialCredits, got, err)
		}
	})
}

// 系统设置里没有值时 TaskLimitService 给出代码常量；库值优先于常量。
func TestTaskLimitService_ConstantsAndOverride(t *testing.T) {
	ctx := context.Background()
	empty := NewTaskLimitService(NewSettingsService(newFakeSettingsRepo(), nil), newFakeUserRepo())
	if n, err := empty.MaxActiveTasks(ctx, 1); err != nil || n != DefaultMaxActiveTasks {
		t.Fatalf("库里没有值应取并发常量 %d：%d %v", DefaultMaxActiveTasks, n, err)
	}
	if n, err := empty.InitialCredits(ctx); err != nil || n != DefaultInitialCredits {
		t.Fatalf("库里没有值应取初始积分常量 %d：%d %v", DefaultInitialCredits, n, err)
	}
	repo := newFakeSettingsRepo()
	repo.kv[model.SettingInitialCredits] = "7"
	repo.kv[model.SettingDefaultMaxActiveTasks] = "9"
	set := NewTaskLimitService(NewSettingsService(repo, nil), newFakeUserRepo())
	if n, err := set.MaxActiveTasks(ctx, 1); err != nil || n != 9 {
		t.Fatalf("库值应优先：%d %v", n, err)
	}
	if n, err := set.InitialCredits(ctx); err != nil || n != 7 {
		t.Fatalf("库值应优先：%d %v", n, err)
	}
}
