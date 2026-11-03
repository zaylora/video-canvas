package agent

import (
	"context"

	"video-canvas/internal/imageproc"
	"video-canvas/internal/model"
	"video-canvas/internal/service"
	"video-canvas/internal/storage"
	"video-canvas/pkg/pagination"
)

// AdminManagedUsers 是后台用户查询与管控服务，权限与积分规则仍由原服务负责。
type AdminManagedUsers interface {
	// List 分页查询安全用户视图。
	List(context.Context, *model.ListAdminUserReq) ([]model.AdminUserListItem, int64, pagination.Query, error)
	// Get 返回不含密码的用户详情。
	Get(context.Context, uint64) (*model.AdminUserDetail, error)
	// AdjustCredits 复用积分账户行锁与流水。
	AdjustCredits(context.Context, uint64, uint64, *model.AdjustCreditsReq) (*model.AdminCreditView, error)
	// SetMaxActiveTasks 设置覆盖值，nil 恢复默认。
	SetMaxActiveTasks(context.Context, uint64, uint64, *int) error
	// SetStatus 复用封禁保护和取消退款规则。
	SetStatus(context.Context, uint64, uint64, string, bool) (*model.AdminStatusResult, error)
	// SetRole 复用最后一个超级管理员保护。
	SetRole(context.Context, uint64, uint64, string) error
}

// AdminManagedRegister 提供注册设置读取和保存。
type AdminManagedRegister interface {
	// RegisterSettings 返回当前生效配置。
	RegisterSettings(context.Context) (*model.RegisterSettingsView, error)
	// UpdateRegisterSettings 校验范围并记录审计。
	UpdateRegisterSettings(context.Context, uint64, *model.RegisterSettingsView) (*model.RegisterSettingsView, error)
}

// AdminManagedSMTP 不暴露密码读取或替换能力。
type AdminManagedSMTP interface {
	// Get 返回不含密码的邮件配置。
	Get(context.Context) (*model.SMTPSettingsView, error)
	// Update 保存非凭证字段。
	Update(context.Context, uint64, *model.UpdateSMTPSettingsReq) (*model.SMTPSettingsView, error)
	// Test 使用已存凭证发送测试邮件。
	Test(context.Context, string) error
}

// AdminManagedStorages 管理已有存储，不暴露密钥读取、新建或替换能力。
type AdminManagedStorages interface {
	// Presets 返回服务商字段规则。
	Presets() []storage.Preset
	// List 返回安全分页视图。
	List(context.Context, *model.ListStorageReq) (*service.StoragePage, error)
	// Get 返回不含明文凭证的视图。
	Get(context.Context, uint64) (*service.StorageView, error)
	// Update 带版本保存并保留素材定位锁。
	Update(context.Context, uint64, uint64, service.StorageUpdateInput) (*service.StorageView, error)
	// Check 使用已存凭证测试连接。
	Check(context.Context, uint64, uint64) (storage.ProbeResult, error)
	// SetDefault 只允许通过检查的存储。
	SetDefault(context.Context, uint64, uint64) error
	// Delete 复用内置、默认和引用保护。
	Delete(context.Context, uint64, uint64) error
}

// AdminManagedProcessors 管理图片服务的草稿与发布生命周期。
type AdminManagedProcessors interface {
	// Presets 返回配置规则与可选格式。
	Presets() []imageproc.Preset
	// List 查询分页视图。
	List(context.Context, *model.ListProcessorReq) (*service.ProcessorPage, error)
	// Get 读取当前草稿和线上状态。
	Get(context.Context, uint64) (*service.ProcessorView, error)
	// Create 创建绑定已有存储的草稿。
	Create(context.Context, uint64, service.ProcessorCreateInput) (*service.ProcessorView, error)
	// Update 带版本更新草稿。
	Update(context.Context, uint64, uint64, service.ProcessorUpdateInput) (*service.ProcessorView, error)
	// Check 校验当前草稿并测试。
	Check(context.Context, uint64, uint64) (*service.ProcessorView, error)
	// Publish 仅发布校验通过的固定版本。
	Publish(context.Context, uint64, uint64, int) (*service.ProcessorView, error)
	// Rollback 回滚上一发布版本。
	Rollback(context.Context, uint64, uint64) (*service.ProcessorView, error)
	// Disable 停用线上配置。
	Disable(context.Context, uint64, uint64) (*service.ProcessorView, error)
	// Delete 删除草稿或已停用配置。
	Delete(context.Context, uint64, uint64) error
}

// AdminManagedQueue 只查询队列，不能修改并发或暂停队列。
type AdminManagedQueue interface {
	// Overview 返回队列、渠道占用和实例状态。
	Overview(context.Context) (*model.QueueOverview, error)
	// Throughput 返回指定时长的吞吐曲线。
	Throughput(context.Context, int) ([]model.QueueThroughputPoint, error)
}

// AdminManagedUsage 只查询生成任务使用记录与汇总。
type AdminManagedUsage interface {
	// List 按后台原有筛选规则查询。
	List(context.Context, *model.ListUsageReq) (*pagination.Result[model.UsageItem], error)
	// Summary 汇总同条件的任务和积分。
	Summary(context.Context, *model.UsageFilterReq) (*model.UsageSummary, error)
}

// AdminManagedLogs 只读取已脱敏操作审计。
type AdminManagedLogs interface {
	// List 查询合并后的运营与 AI 配置审计。
	List(context.Context, *model.ListAuditReq) (*pagination.Result[model.AuditItem], error)
}

// AdminManagedRuns 只读取运行摘要，不提供会话修改或取消能力。
type AdminManagedRuns interface {
	// ListRuns 查询画布或后台运行记录。
	ListRuns(context.Context, ObserveActor, *RunListFilter) (*RunListPage, error)
}
