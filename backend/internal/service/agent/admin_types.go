package agent

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"sync"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

// AdminModelReader 提供后台模型原始快照，包含未启用的配置。
type AdminModelReader interface {
	// ListSecrets 只读取凭证元信息，不读取密文，用于展示“是否配置”。
	ListSecrets(ctx context.Context) ([]model.AISecret, error)
	// LoadModels 返回后台所有模型的指针和正文。
	LoadModels(ctx context.Context) ([]repository.PublishedModel, error)
	// GetModelConfig 返回原始配置正文。
	GetModelConfig(ctx context.Context, key string) (*model.AIConfigRevision, error)
	// GetModelPointer 返回实际启用状态。
	GetModelPointer(ctx context.Context, key string) (*model.AIModel, error)
}

// AdminModelWriter 复用现有配置保存与启用检查。
type AdminModelWriter interface {
	// SaveModel 保存审批通过的配置并原子比较 SHA、写审计。
	SaveModel(ctx context.Context, in service.ModelSaveInput) (*service.SaveModelResult, error)
	// CheckEnable 校验候选正文对应的渠道、协议与凭证。
	CheckEnable(ctx context.Context, key string, body []byte) error
}

// AdminUserReader 必须从数据库取最新角色，不能依赖请求头或 JWT。
type AdminUserReader interface {
	// GetByID 返回当前用户，包括用户名与角色。
	GetByID(ctx context.Context, id uint64) (*model.User, error)
}

// AdminChannelReader 只读取渠道；发给模型时只投影安全的概要字段。
type AdminChannelReader interface {
	// ListChannels 返回渠道清单，调用方不得透传 HeadersJSON。
	ListChannels(ctx context.Context) ([]model.AIChannel, error)
}

// AdminAuditReader 为回滚读取已应用条目的前后值。
type AdminAuditReader interface {
	// RecentModelUsage 汇总指定时间之后的生成任务与 Agent 模型调用次数。
	RecentModelUsage(ctx context.Context, since time.Time) (map[string]int64, error)
	// ChangeAudits 只返回指定变更单的助手审计。
	ChangeAudits(ctx context.Context, order string) ([]repository.AssistantAuditRecord, error)
}

// AdminAssistantDeps 集中装配后台助手依赖。
type AdminAssistantDeps struct {
	Management    *AdminManagementDeps
	History       AdminDiagnosticReader
	Plugins       AdminPluginReader
	PluginWriter  AdminPluginWriter
	ChannelWriter AdminChannelWriter
	Checker       provider.PluginDraftChecker
	Trials        AdminTrialRunner
	Attempts      AdminAttemptStore
	Models        AdminModelReader
	Writer        AdminModelWriter
	Users         AdminUserReader
	Channels      AdminChannelReader
	Settings      *service.PricingSettingsService
	Audit         AdminAuditReader
	Now           func() time.Time
}

// AdminDiagnosticReader 只读取当前管理员自己的失败请求元信息。
type AdminDiagnosticReader interface {
	// RecentRequests 由仓储按 user_id 及模型/渠道过滤，不返回其他用户记录。
	RecentRequests(context.Context, repository.AdminRequestFilter) ([]repository.AdminRequestRecord, error)
}

// AdminAssistantService 为一期后台配置工具提供读、预检、审批和执行。
type AdminAssistantService struct {
	d           AdminAssistantDeps
	orderMu     sync.Mutex
	orderNos    map[string]struct{}
	orderRandom io.Reader
	checkMu     sync.Mutex
	checks      map[string]adminCheckedDraft
}

// NewAdminAssistantService 创建服务，写入始终经过审批入口。
func NewAdminAssistantService(d AdminAssistantDeps) *AdminAssistantService {
	if d.Now == nil {
		d.Now = time.Now
	}
	return &AdminAssistantService{d: d, orderNos: map[string]struct{}{}, orderRandom: rand.Reader, checks: map[string]adminCheckedDraft{}}
}

// AdminChangeRequest 是助手申请变更的工具参数。
type AdminChangeRequest struct {
	Title    string             `json:"title"`
	Reason   string             `json:"reason"`
	RevertOf string             `json:"revert_of,omitempty"`
	Items    []AdminChangeInput `json:"items"`
}

// AdminChangeInput 是读后修改的单条提案，patch 遵循 JSON Merge Patch。
type AdminChangeInput struct {
	DependsOn    []int          `json:"depends_on,omitempty"`
	Target       string         `json:"target"`
	Key          string         `json:"key"`
	BaseSHA256   string         `json:"base_sha256"`
	Patch        map[string]any `json:"patch"`
	PricingBasis []PricingBasis `json:"pricing_basis,omitempty"`
}

// AdminChangePayload 是签名后的变更单，快照字段由平台生成而非接受模型输入。
type AdminChangePayload struct {
	OrderNo   string            `json:"order_no"`
	Title     string            `json:"title"`
	Reason    string            `json:"reason"`
	RevertOf  string            `json:"revert_of,omitempty"`
	Items     []AdminChangeItem `json:"items"`
	Signature string            `json:"signature"`
}

// AdminChangeItem 包含展示差异及执行必需的固定快照。
type AdminChangeItem struct {
	Action       string            `json:"action,omitempty"`
	DependsOn    []int             `json:"depends_on,omitempty"`
	Detail       any               `json:"detail,omitempty"`
	Target       string            `json:"target"`
	Key          string            `json:"key"`
	Label        string            `json:"label"`
	Live         bool              `json:"live"`
	Status       string            `json:"status"`
	Issues       []string          `json:"issues"`
	Warnings     []string          `json:"warnings"`
	RequiredRole string            `json:"required_role"`
	Diff         []AdminChangeDiff `json:"diff"`
	Basis        []PricingBasis    `json:"basis"`
	BaseSHA256   string            `json:"base_sha256"`
	Before       json.RawMessage   `json:"before"`
	After        json.RawMessage   `json:"after"`
}

// AdminChangeResult 是每条独立执行后的公开结果。
type AdminChangeResult struct {
	Items     []AdminChangeItemResult `json:"items"`
	AppliedBy string                  `json:"applied_by"`
	AppliedAt string                  `json:"applied_at"`
	AuditIDs  []uint64                `json:"audit_ids"`
}

// AdminChangeItemResult 按原始下标报告执行状态。
type AdminChangeItemResult struct {
	Created *AdminCreated `json:"created,omitempty"`
	Index   int           `json:"index"`
	Status  string        `json:"status"`
	Message string        `json:"message,omitempty"`
}

// AdminCreated 返回真正创建的版本和标识，前端不能用预计版本代替它。
type AdminCreated struct {
	PluginVersion string `json:"plugin_version,omitempty"`
	ChannelKey    string `json:"channel_key,omitempty"`
	ModelKey      string `json:"model_key,omitempty"`
}

// AdminPluginReader 读取内置或上传插件及不可变版本。
type AdminPluginReader interface {
	// ListPlugins 读取插件清单。
	ListPlugins(context.Context) ([]model.AIPlugin, error)
	// GetPlugin 读取一个插件。
	GetPlugin(context.Context, string) (*model.AIPlugin, error)
	// ListVersions 返回从新到旧的版本头。
	ListVersions(context.Context, string) ([]model.AIPluginVersion, error)
	// GetVersion 返回包含代码的指定版本。
	GetVersion(context.Context, uint64) (*model.AIPluginVersion, error)
}

// AdminPluginWriter 复用现有上传预检和自动版本分配。
type AdminPluginWriter interface {
	// Upload 登记审批通过的代码。
	Upload(context.Context, uint64, []byte) (*service.UploadResult, error)
}

// AdminChannelWriter 复用渠道创建，密钥仅能从原有密钥接口保存。
type AdminChannelWriter interface {
	// Create 创建不带密钥的新渠道。
	Create(context.Context, service.ChannelCreateInput) (*service.ChannelView, error)
}

// AdminTrialRunner 提供不扣积分的幂等试跑和已脱敏追踪。
type AdminTrialRunner interface {
	// TestRunIdempotent 按审批标识去重创建试跑。
	TestRunIdempotent(context.Context, uint64, string, map[string]any, string) (*model.GenerationTaskView, error)
	// GetTestRun 对账管理员自己的试跑任务。
	GetTestRun(context.Context, uint64, uint64) (*model.GenerationTaskView, error)
	// GetTestTrace 返回已脱敏的执行追踪。
	GetTestTrace(context.Context, uint64, uint64) (*service.TestTraceView, error)
}

// AdminAttemptStore 将运行内次数持久化，Go 重启和并发不能绕过三次上限。
type AdminAttemptStore interface {
	// ClaimAdminAttempt 原子计次；同一 token 重放返回原次数，达到上限返回 0。
	ClaimAdminAttempt(context.Context, uint64, string, string, string) (int, error)
}
