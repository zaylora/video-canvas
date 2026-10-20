package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/repository"
	. "video-canvas/internal/service"
)

// fakeLimits 实现 service.TaskLimits：按用户返回并发上限，并给出初始积分。
type fakeLimits struct {
	initial int
	max     map[uint64]int
	defMax  int
	mult    int           // 排队倍数；0 按 1（排队上限等于并发上限，保持“并发满了就 429”的旧用例语义）
	wait    time.Duration // 排队超时；0 按 1 小时
	err     error
}

func (l *fakeLimits) InitialCredits(context.Context) (int, error) { return l.initial, l.err }
func (l *fakeLimits) MaxActiveTasks(_ context.Context, userID uint64) (int, error) {
	if v, ok := l.max[userID]; ok {
		return v, l.err
	}
	return l.defMax, l.err
}

func (l *fakeLimits) DefaultMaxActiveTasks(context.Context) (int, error) { return l.defMax, l.err }
func (l *fakeLimits) QueueMultiplier(context.Context) (int, error) {
	if l.mult > 0 {
		return l.mult, l.err
	}
	return 1, l.err
}
func (l *fakeLimits) QueueMaxWait(context.Context) (time.Duration, error) {
	if l.wait > 0 {
		return l.wait, l.err
	}
	return time.Hour, l.err
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
	t.Run("排队上限回落到 并发 4 × 倍数 5 = 20：19 个未完成放行，20 个拒绝", func(t *testing.T) {
		if DefaultQueueMultiplier != 5 || DefaultQueueMaxWaitMinutes != 60 {
			t.Fatalf("常量应为排队倍数 5、排队超时 60 分钟：%d %d", DefaultQueueMultiplier, DefaultQueueMaxWaitMinutes)
		}
		env := newTaskSvcEnv(nil)
		env.repo.addCredit(user, 1000, 0)
		for range DefaultMaxActiveTasks*DefaultQueueMultiplier - 1 {
			env.repo.addTask(model.GenerationTask{UserID: user, Status: model.TaskRunning})
		}
		if _, err := createSingle(ctx, env.svc, user, "", validCreateReq()); err != nil {
			t.Fatalf("第 20 个应放行：%v", err)
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

// 排队上限 = 用户并发上限 × 排队倍数：并发是 2、倍数是 3，最多同时有 6 个未完成的任务（含排队中的），第 7 个才被拒绝；
// 单用户覆盖了并发上限，排队上限也跟着变。
func TestGenerationTaskService_QueueLimitIsConcurrencyTimesMultiplier(t *testing.T) {
	const user = uint64(1)
	ctx := context.Background()

	t.Run("默认并发 2、倍数 3：已有 5 个未完成还能创建，已有 6 个就拒绝", func(t *testing.T) {
		env := newLimitsEnv(&fakeLimits{initial: 50, defMax: 2, mult: 3})
		env.repo.addCredit(user, 1000, 0)
		for range 5 {
			env.repo.addTask(model.GenerationTask{UserID: user, Status: model.TaskPending}) // 排队中的也算
		}
		if _, err := createSingle(ctx, env.svc, user, "", validCreateReq()); err != nil {
			t.Fatalf("第 6 个应放行：%v", err)
		}
		_, err := createSingle(ctx, env.svc, user, "", validCreateReq())
		assertTaskCode(t, err, errcode.ErrTooManyTasks.Code)
	})
	t.Run("单用户把并发覆盖成 4：排队上限变成 12", func(t *testing.T) {
		env := newLimitsEnv(&fakeLimits{initial: 50, max: map[uint64]int{user: 4}, defMax: 2, mult: 3})
		env.repo.addCredit(user, 1000, 0)
		for range 11 {
			env.repo.addTask(model.GenerationTask{UserID: user, Status: model.TaskPending})
		}
		if _, err := createSingle(ctx, env.svc, user, "", validCreateReq()); err != nil {
			t.Fatalf("第 12 个应放行：%v", err)
		}
		_, err := createSingle(ctx, env.svc, user, "", validCreateReq())
		assertTaskCode(t, err, errcode.ErrTooManyTasks.Code)
	})
	t.Run("终态的任务不占排队名额", func(t *testing.T) {
		env := newLimitsEnv(&fakeLimits{initial: 50, defMax: 1, mult: 2})
		env.repo.addCredit(user, 1000, 0)
		env.repo.addTask(model.GenerationTask{UserID: user, Status: model.TaskSucceeded})
		env.repo.addTask(model.GenerationTask{UserID: user, Status: model.TaskFailed})
		env.repo.addTask(model.GenerationTask{UserID: user, Status: model.TaskRunning})
		if _, err := createSingle(ctx, env.svc, user, "", validCreateReq()); err != nil {
			t.Fatalf("未完成的只有 1 个，第 2 个应放行：%v", err)
		}
		_, err := createSingle(ctx, env.svc, user, "", validCreateReq())
		assertTaskCode(t, err, errcode.ErrTooManyTasks.Code)
	})
	t.Run("读取排队倍数失败透传，不创建任务", func(t *testing.T) {
		env := newLimitsEnv(&fakeLimits{initial: 50, defMax: 2, mult: 3, err: errBoom})
		env.repo.addCredit(user, 1000, 0)
		_, err := createSingle(ctx, env.svc, user, "", validCreateReq())
		if !errors.Is(err, errBoom) || len(env.repo.tasks) != 0 {
			t.Fatalf("err=%v tasks=%d", err, len(env.repo.tasks))
		}
	})
}

// 创建任务时入队与冻结积分在同一个事务里：入队失败，任务、积分冻结、流水一起回滚。
func TestGenerationTaskService_CreateRollsBackWhenEnqueueFails(t *testing.T) {
	const user = uint64(1)
	env := newLimitsEnv(&fakeLimits{initial: 50, defMax: 2, mult: 3})
	env.repo.addCredit(user, 100, 0)
	env.repo.errs["EnqueueTask"] = errBoom

	_, err := createSingle(context.Background(), env.svc, user, "", validCreateReq())
	if !errors.Is(err, errBoom) {
		t.Fatalf("入队失败应透传：%v", err)
	}
	if len(env.repo.tasks) != 0 || len(env.repo.ledger) != 0 || env.repo.credits[user].Frozen != 0 || len(env.repo.enqueued) != 0 {
		t.Fatalf("入队失败应整体回滚：tasks=%d ledger=%d frozen=%d enqueued=%v", len(env.repo.tasks), len(env.repo.ledger), env.repo.credits[user].Frozen, env.repo.enqueued)
	}
	if len(env.bc.msgs) != 0 {
		t.Fatal("失败不应推送")
	}
}

// 队列取任务与申请名额用到的两个方法。
func TestGenerationTaskService_QueueStore(t *testing.T) {
	const user = uint64(1)
	ctx := context.Background()

	t.Run("GetTask：存在返回副本，不存在返回 (nil, nil)，仓储出错透传", func(t *testing.T) {
		env := newLimitsEnv(&fakeLimits{initial: 50, defMax: 2})
		task := env.repo.addTask(model.GenerationTask{UserID: user, Status: model.TaskPending})
		got, err := env.svc.GetTask(ctx, task.ID)
		if err != nil || got == nil || got.ID != task.ID {
			t.Fatalf("got=%+v err=%v", got, err)
		}
		if got, err := env.svc.GetTask(ctx, 99999); got != nil || err != nil {
			t.Fatalf("不存在应返回 (nil, nil)：%+v %v", got, err)
		}
		env.repo.errs["GetByIDAny"] = errBoom
		if _, err := env.svc.GetTask(ctx, task.ID); !errors.Is(err, errBoom) {
			t.Fatalf("应透传：%v", err)
		}
	})

	t.Run("AcquireSlot：带上用户生效并发（覆盖优先）、默认并发，并把截止时间重置为现在 + 模型超时", func(t *testing.T) {
		env := newLimitsEnv(&fakeLimits{initial: 50, max: map[uint64]int{user: 3}, defMax: 2})
		env.registry.snap.Model.Deadline = modelcfg.Duration(7 * time.Minute)
		snap, _ := json.Marshal(env.registry.snap)
		task := env.repo.addTask(model.GenerationTask{UserID: user, Status: model.TaskPending, ConfigSnapshot: snap})

		out, err := env.svc.AcquireSlot(ctx, task)
		if err != nil || out != provider.SlotAcquired {
			t.Fatalf("out=%s err=%v", out, err)
		}
		req := env.repo.slotReqs[0]
		if req.TaskID != task.ID || req.UserLimit != 3 || req.DefaultUserLimit != 2 {
			t.Fatalf("申请参数不对：%+v", req)
		}
		if !req.Now.Equal(env.now) || !req.Deadline.Equal(env.now.Add(7*time.Minute)) {
			t.Fatalf("截止时间应是 现在 + 模型超时 7 分钟：%+v", req)
		}
	})

	t.Run("AcquireSlot：模型没配超时用 30 分钟，快照损坏也用 30 分钟（任务照常处理）", func(t *testing.T) {
		env := newLimitsEnv(&fakeLimits{initial: 50, defMax: 2})
		task := env.repo.addTask(model.GenerationTask{UserID: user, Status: model.TaskPending, ConfigSnapshot: []byte("not json")})
		if _, err := env.svc.AcquireSlot(ctx, task); err != nil {
			t.Fatal(err)
		}
		if got := env.repo.slotReqs[0].Deadline; !got.Equal(env.now.Add(30 * time.Minute)) {
			t.Fatalf("应回落到 30 分钟：%v", got)
		}
	})

	t.Run("AcquireSlot：试跑任务不看用户并发上限", func(t *testing.T) {
		env := newLimitsEnv(&fakeLimits{initial: 50, max: map[uint64]int{user: 3}, defMax: 2})
		task := env.repo.addTask(model.GenerationTask{UserID: user, Status: model.TaskPending, IsTest: true})
		if _, err := env.svc.AcquireSlot(ctx, task); err != nil {
			t.Fatal(err)
		}
		if got := env.repo.slotReqs[0].UserLimit; got != 0 {
			t.Fatalf("试跑任务的用户上限应传 0（仓储忽略它），实际 %d", got)
		}
	})

	t.Run("AcquireSlot：把仓储的各种结果原样翻译，出错透传", func(t *testing.T) {
		env := newLimitsEnv(&fakeLimits{initial: 50, defMax: 2})
		task := env.repo.addTask(model.GenerationTask{UserID: user, Status: model.TaskPending})
		for _, want := range []provider.SlotOutcome{provider.SlotWaitUser, provider.SlotWaitChannel, provider.SlotGone} {
			env.repo.slotResult = repository.SlotResult(want)
			if got, err := env.svc.AcquireSlot(ctx, task); err != nil || got != want {
				t.Fatalf("want=%s got=%s err=%v", want, got, err)
			}
		}
		env.repo.errs["AcquireSlot"] = errBoom
		if _, err := env.svc.AcquireSlot(ctx, task); !errors.Is(err, errBoom) {
			t.Fatalf("应透传：%v", err)
		}
		lim := &fakeLimits{initial: 50, defMax: 2, err: errBoom}
		env2 := newLimitsEnv(lim)
		task2 := env2.repo.addTask(model.GenerationTask{UserID: user, Status: model.TaskPending})
		if _, err := env2.svc.AcquireSlot(ctx, task2); !errors.Is(err, errBoom) {
			t.Fatalf("读取并发设置失败应透传：%v", err)
		}
	})
}
