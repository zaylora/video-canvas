package service

import (
	"context"
	"strconv"

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
		RegisterEnabled:       kv[model.SettingRegisterEnabled] != "false",     // 缺省或任何非 "false" 的值都按开
		VerifyEmail:           kv[model.SettingRegisterVerifyEmail] != "false", // 同样缺省为开
		InitialCredits:        DefaultInitialCredits,
		DefaultMaxActiveTasks: DefaultMaxActiveTasks,
	}
	// 库值损坏（非数字、越界）时忽略它而不是报错：设置读取在注册、提交任务的热路径上，不能因为一行脏数据让它们不可用
	if n, err := strconv.Atoi(kv[model.SettingInitialCredits]); err == nil && n >= 0 && n <= maxInitialCredits {
		v.InitialCredits = n
	}
	if n, err := strconv.Atoi(kv[model.SettingDefaultMaxActiveTasks]); err == nil && n >= 1 && n <= maxActiveTasksUpper {
		v.DefaultMaxActiveTasks = n
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

// VerifyEmail 实现 RegisterPolicy：注册是否需要验证邮箱。
func (s *SettingsService) VerifyEmail(ctx context.Context) (bool, error) {
	v, err := s.RegisterSettings(ctx)
	if err != nil {
		return false, err
	}
	return v.VerifyEmail, nil
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

// UpdateRegisterSettings 保存注册设置（仅 super_admin 可调，由路由保证），写审计并返回最新值。
func (s *SettingsService) UpdateRegisterSettings(ctx context.Context, actorID uint64, req *model.RegisterSettingsView) (*model.RegisterSettingsView, error) {
	// 1. 范围校验：初始积分 >= 0，默认并发 1..64
	if req.InitialCredits < 0 || req.InitialCredits > maxInitialCredits {
		return nil, errcode.ErrInvalidParams.WithMsg("初始积分必须在 0 到 " + strconv.Itoa(maxInitialCredits) + " 之间")
	}
	if req.DefaultMaxActiveTasks < 1 || req.DefaultMaxActiveTasks > maxActiveTasksUpper {
		return nil, errcode.ErrInvalidParams.WithMsg("默认并发上限必须在 1 到 " + strconv.Itoa(maxActiveTasksUpper) + " 之间")
	}

	// 2. 记下改前的值用于审计，再一次性写库
	before, err := s.RegisterSettings(ctx)
	if err != nil {
		return nil, err
	}
	err = s.repo.SetMany(ctx, map[string]string{
		model.SettingRegisterEnabled:       strconv.FormatBool(req.RegisterEnabled),
		model.SettingRegisterVerifyEmail:   strconv.FormatBool(req.VerifyEmail),
		model.SettingInitialCredits:        strconv.Itoa(req.InitialCredits),
		model.SettingDefaultMaxActiveTasks: strconv.Itoa(req.DefaultMaxActiveTasks),
	}, actorID)
	if err != nil {
		return nil, err
	}

	// 3. 审计：只记前后值（都是开关与数字，没有敏感信息）
	adminAudit(ctx, s.audit, actorID, model.AdminAuditSettingsReg, model.AdminAuditTargetSettings, 0, map[string]any{
		"before": before, "after": req,
	})
	return &model.RegisterSettingsView{
		RegisterEnabled: req.RegisterEnabled, VerifyEmail: req.VerifyEmail, InitialCredits: req.InitialCredits, DefaultMaxActiveTasks: req.DefaultMaxActiveTasks,
	}, nil
}
