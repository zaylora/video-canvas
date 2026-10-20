package service

import (
	"context"
	"strconv"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
)

// SystemSettingRepo 是系统设置键值表的数据访问接口（真实实现是 repository.SystemSettingRepository）。
type SystemSettingRepo interface {
	// GetAll 返回全部设置（key -> value），表为空返回空 map。
	GetAll(ctx context.Context) (map[string]string, error)
	// SetMany 在一个事务里 upsert 多个设置。
	SetMany(ctx context.Context, kv map[string]string, updatedBy uint64) error
}

// 设置的合法范围与内置缺省。
const (
	maxInitialCredits   = 100_000_000
	maxActiveTasksUpper = 64

	// DefaultMaxActiveTasks 是每个用户进行中生成任务数的默认上限。
	// 后台「注册设置」里保存的库值优先，这个常量只是库里没有（或库值非法）时的兜底；单用户还可以再单独覆盖。
	// 它不再放在配置文件里：同一个数有「配置文件 + 后台」两个来源会让人不知道以谁为准。
	DefaultMaxActiveTasks = 4
	// DefaultInitialCredits 是新用户（新积分账户）的默认初始积分。
	// 后台「注册设置」里保存的库值优先，这个常量只是库里没有（或库值非法）时的兜底。
	DefaultInitialCredits = 50

	// DefaultQueueMultiplier 是排队倍数的默认值：用户非终态任务数达到“并发上限 × 倍数”才拒绝新任务。
	// 并发上限 4 时最多排 20 个。后台「注册设置」里的库值优先。
	DefaultQueueMultiplier = 5
	maxQueueMultiplier     = 20
	// DefaultQueueMaxWaitMinutes 是排队超时的默认值（分钟）：任务排队等名额超过它就过期并退回积分。
	DefaultQueueMaxWaitMinutes = 60
	minQueueMaxWaitMinutes     = 5
	maxQueueMaxWaitMinutes     = 1440 // 一天
)

// SettingsService 管理“库值优先、代码常量兜底”的系统设置：注册开关、初始积分、默认并发上限。
// 同时实现 RegisterPolicy。
type SettingsService struct {
	repo  SystemSettingRepo
	audit AdminAuditWriter
}

// NewSettingsService 创建系统设置服务；audit 可为 nil（不写审计，仅供只读场景）。
func NewSettingsService(repo SystemSettingRepo, audit AdminAuditWriter) *SettingsService {
	return &SettingsService{repo: repo, audit: audit}
}

// RegisterSettings 返回当前生效的注册设置：库里有合法值用库值，否则回落 DefaultInitialCredits / DefaultMaxActiveTasks（注册开关缺省为开）。
func (s *SettingsService) RegisterSettings(ctx context.Context) (*model.RegisterSettingsView, error) {
	kv, err := s.repo.GetAll(ctx)
	if err != nil {
		return nil, err
	}
	v := &model.RegisterSettingsView{
		RegisterEnabled:       kv[model.SettingRegisterEnabled] != "false", // 缺省或任何非 "false" 的值都按开
		InitialCredits:        DefaultInitialCredits,
		DefaultMaxActiveTasks: DefaultMaxActiveTasks,
		QueueMultiplier:       DefaultQueueMultiplier,
		QueueMaxWaitMinutes:   DefaultQueueMaxWaitMinutes,
	}
	// 库值损坏（非数字、越界）时忽略它而不是报错：设置读取在注册、提交任务的热路径上，不能因为一行脏数据让它们不可用
	if n, err := strconv.Atoi(kv[model.SettingInitialCredits]); err == nil && n >= 0 && n <= maxInitialCredits {
		v.InitialCredits = n
	}
	if n, err := strconv.Atoi(kv[model.SettingDefaultMaxActiveTasks]); err == nil && n >= 1 && n <= maxActiveTasksUpper {
		v.DefaultMaxActiveTasks = n
	}
	if n, err := strconv.Atoi(kv[model.SettingQueueMultiplier]); err == nil && n >= 1 && n <= maxQueueMultiplier {
		v.QueueMultiplier = n
	}
	if n, err := strconv.Atoi(kv[model.SettingQueueMaxWaitMinutes]); err == nil && n >= minQueueMaxWaitMinutes && n <= maxQueueMaxWaitMinutes {
		v.QueueMaxWaitMinutes = n
	}
	return v, nil
}

// RegisterEnabled 实现 RegisterPolicy：注册开关是否打开。
func (s *SettingsService) RegisterEnabled(ctx context.Context) (bool, error) {
	v, err := s.RegisterSettings(ctx)
	if err != nil {
		return false, err
	}
	return v.RegisterEnabled, nil
}

// InitialCredits 实现 RegisterPolicy：新用户初始积分。
func (s *SettingsService) InitialCredits(ctx context.Context) (int, error) {
	v, err := s.RegisterSettings(ctx)
	if err != nil {
		return 0, err
	}
	return v.InitialCredits, nil
}

// DefaultMaxActiveTasks 返回全局默认的并发上限。
func (s *SettingsService) DefaultMaxActiveTasks(ctx context.Context) (int, error) {
	v, err := s.RegisterSettings(ctx)
	if err != nil {
		return 0, err
	}
	return v.DefaultMaxActiveTasks, nil
}

// QueueMultiplier 返回排队倍数。
func (s *SettingsService) QueueMultiplier(ctx context.Context) (int, error) {
	v, err := s.RegisterSettings(ctx)
	if err != nil {
		return 0, err
	}
	return v.QueueMultiplier, nil
}

// QueueMaxWait 返回任务排队等名额的最长时间。
func (s *SettingsService) QueueMaxWait(ctx context.Context) (time.Duration, error) {
	v, err := s.RegisterSettings(ctx)
	if err != nil {
		return 0, err
	}
	return time.Duration(v.QueueMaxWaitMinutes) * time.Minute, nil
}

// QueueConcurrency 返回各阶段当前设置的并发（阶段短名 → 每实例并发）。库里没有或值非法的阶段用默认值；
// 不做硬上限裁剪（硬上限在配置文件里，由队列运行时在应用时裁剪）。读取在后台刷新循环里，脏数据按默认处理而不是报错。
func (s *SettingsService) QueueConcurrency(ctx context.Context) (map[string]int, error) {
	kv, err := s.repo.GetAll(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]int, len(model.QueueStages))
	for _, stage := range model.QueueStages {
		out[stage] = model.DefaultQueueConcurrency[stage]
		if n, err := strconv.Atoi(kv[model.SettingQueueConcurrencyPrefix+stage]); err == nil && n >= 1 {
			out[stage] = n
		}
	}
	return out, nil
}

// SetQueueConcurrency 保存一个阶段的并发设置（范围校验由调用方按硬上限做）。
func (s *SettingsService) SetQueueConcurrency(ctx context.Context, actorID uint64, stage string, n int) error {
	return s.repo.SetMany(ctx, map[string]string{model.SettingQueueConcurrencyPrefix + stage: strconv.Itoa(n)}, actorID)
}

// UpdateRegisterSettings 保存注册设置（仅 super_admin 可调，由路由保证），写审计并返回最新值。
func (s *SettingsService) UpdateRegisterSettings(ctx context.Context, actorID uint64, req *model.RegisterSettingsView) (*model.RegisterSettingsView, error) {
	// 1. 范围校验：初始积分 >= 0，默认并发 1..64，排队倍数 1..20，排队超时 5..1440 分钟
	if req.InitialCredits < 0 || req.InitialCredits > maxInitialCredits {
		return nil, errcode.ErrInvalidParams.WithMsg("初始积分必须在 0 到 " + strconv.Itoa(maxInitialCredits) + " 之间")
	}
	if req.DefaultMaxActiveTasks < 1 || req.DefaultMaxActiveTasks > maxActiveTasksUpper {
		return nil, errcode.ErrInvalidParams.WithMsg("默认并发上限必须在 1 到 " + strconv.Itoa(maxActiveTasksUpper) + " 之间")
	}
	if req.QueueMultiplier < 1 || req.QueueMultiplier > maxQueueMultiplier {
		return nil, errcode.ErrInvalidParams.WithMsg("排队倍数必须在 1 到 " + strconv.Itoa(maxQueueMultiplier) + " 之间")
	}
	if req.QueueMaxWaitMinutes < minQueueMaxWaitMinutes || req.QueueMaxWaitMinutes > maxQueueMaxWaitMinutes {
		return nil, errcode.ErrInvalidParams.WithMsg("排队超时必须在 " + strconv.Itoa(minQueueMaxWaitMinutes) + " 到 " + strconv.Itoa(maxQueueMaxWaitMinutes) + " 分钟之间")
	}

	// 2. 记下改前的值用于审计，再一次性写库
	before, err := s.RegisterSettings(ctx)
	if err != nil {
		return nil, err
	}
	err = s.repo.SetMany(ctx, map[string]string{
		model.SettingRegisterEnabled:       strconv.FormatBool(req.RegisterEnabled),
		model.SettingInitialCredits:        strconv.Itoa(req.InitialCredits),
		model.SettingDefaultMaxActiveTasks: strconv.Itoa(req.DefaultMaxActiveTasks),
		model.SettingQueueMultiplier:       strconv.Itoa(req.QueueMultiplier),
		model.SettingQueueMaxWaitMinutes:   strconv.Itoa(req.QueueMaxWaitMinutes),
	}, actorID)
	if err != nil {
		return nil, err
	}

	// 3. 审计：只记前后值（都是开关与数字，没有敏感信息）
	adminAudit(ctx, s.audit, actorID, model.AdminAuditSettingsReg, model.AdminAuditTargetSettings, 0, map[string]any{
		"before": before, "after": req,
	})
	return &model.RegisterSettingsView{
		RegisterEnabled: req.RegisterEnabled, InitialCredits: req.InitialCredits, DefaultMaxActiveTasks: req.DefaultMaxActiveTasks,
		QueueMultiplier: req.QueueMultiplier, QueueMaxWaitMinutes: req.QueueMaxWaitMinutes,
	}, nil
}
