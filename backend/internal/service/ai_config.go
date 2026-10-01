package service

import (
	"context"
	"encoding/json"
	"regexp"
	"sync"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/repository"
)

// AIConfigRepo 是 AI 配置服务依赖的数据访问接口（模型配置的 revision 与指针行、渠道凭证），
// 由 repository.AIConfigRepository 实现，测试里用内存 fake 替换。协议插件设计之后配置目标只有模型。
type AIConfigRepo interface {
	// SaveDraft 在事务里 upsert 指针行、归档旧草稿并插入新草稿。
	SaveDraft(ctx context.Context, in repository.SaveDraftInput) (*model.AIConfigRevision, error)
	// GetDraft 返回最新草稿，没有返回 repository.ErrNotFound。
	GetDraft(ctx context.Context, target, key string) (*model.AIConfigRevision, error)
	// GetRevision 按 id 查询 revision（含正文），不存在返回 repository.ErrNotFound。
	GetRevision(ctx context.Context, id uint64) (*model.AIConfigRevision, error)
	// GetPublishedRevision 返回当前发布的 revision，目标不存在或未发布返回 repository.ErrNotFound。
	GetPublishedRevision(ctx context.Context, target, key string) (*model.AIConfigRevision, error)
	// ListRevisions 返回历史版本（不含正文），新到旧。
	ListRevisions(ctx context.Context, target, key string, limit int) ([]model.AIConfigRevision, error)
	// ListRevisionHeads 返回所有当前的 draft 与 published revision，withBody=false 时不读正文。
	ListRevisionHeads(ctx context.Context, target string, withBody bool) ([]model.AIConfigRevision, error)
	// PublishDraft 发布指定草稿；不存在返回 ErrNotFound，已不是草稿返回 ErrRevisionConflict。
	PublishDraft(ctx context.Context, ptr repository.ConfigPointer, revisionID uint64) (*model.AIConfigRevision, error)
	// Rollback 把发布指针改回已归档的 revision，错误约定同 PublishDraft。
	Rollback(ctx context.Context, ptr repository.ConfigPointer, revisionID uint64) (*model.AIConfigRevision, error)

	// GetModelPointer 返回模型指针行，不存在返回 repository.ErrNotFound。
	GetModelPointer(ctx context.Context, key string) (*model.AIModel, error)
	// ListModelPointers 返回所有模型指针行，按 sort、key 升序。
	ListModelPointers(ctx context.Context) ([]model.AIModel, error)
	// SetModelEnabled 上下架，模型不存在返回 repository.ErrNotFound。
	SetModelEnabled(ctx context.Context, key string, enabled bool) error
	// SetModelSort 修改排序，模型不存在返回 repository.ErrNotFound。
	SetModelSort(ctx context.Context, key string, sort int) error
	// LoadPublishedModels 一次取出所有已发布的模型（指针行 + 发布版本正文）。
	LoadPublishedModels(ctx context.Context) ([]repository.PublishedModel, error)

	// UpsertSecret 写入或覆盖凭证密文。
	UpsertSecret(ctx context.Context, s *model.AISecret) error
	// GetSecret 读取凭证（含密文），不存在返回 repository.ErrNotFound。
	GetSecret(ctx context.Context, name string) (*model.AISecret, error)
}

// AIChannelReader 是本服务对渠道表的只读依赖（签名与 repository.AIChannelRepository 一致，接线时直接传真实仓储）。
type AIChannelReader interface {
	// GetChannel 按 key 查询渠道，不存在返回 repository.ErrNotFound。
	GetChannel(ctx context.Context, key string) (*model.AIChannel, error)
	// ListChannels 返回所有渠道，Registry 加载时用。
	ListChannels(ctx context.Context) ([]model.AIChannel, error)
}

// AIPluginReader 是本服务对插件表的只读依赖（签名与 repository.AIPluginRepository 一致）。
type AIPluginReader interface {
	// GetPlugin 按 key 查询插件，不存在返回 repository.ErrNotFound。
	GetPlugin(ctx context.Context, key string) (*model.AIPlugin, error)
	// ListVersions 返回插件版本头信息（不含代码）；pluginKey 为空表示所有插件的所有版本，Registry 加载时用。
	ListVersions(ctx context.Context, pluginKey string) ([]model.AIPluginVersion, error)
	// GetVersionHead 按 id 查询版本头信息（含 meta_json 与 sha256，不含代码），不存在返回 repository.ErrNotFound。
	GetVersionHead(ctx context.Context, id uint64) (*model.AIPluginVersion, error)
}

// DryRunner 渲染但不发送请求（dry-run）：由插件宿主实现，返回插件给出的请求描述与宿主注入鉴权后的最终请求，
// 结果已由宿主脱敏且可以序列化成 JSON。用接口隔离宿主的具体类型，接线时用 DryRunnerFunc 包一层。
type DryRunner interface {
	// DryRun 按快照与已规范化的输入渲染请求，不发出任何网络请求。
	DryRun(ctx context.Context, snap *provider.Snapshot, input map[string]any) (any, error)
}

// DryRunnerFunc 让普通函数满足 DryRunner。
type DryRunnerFunc func(ctx context.Context, snap *provider.Snapshot, input map[string]any) (any, error)

// DryRun 调用函数本身。
func (f DryRunnerFunc) DryRun(ctx context.Context, snap *provider.Snapshot, input map[string]any) (any, error) {
	return f(ctx, snap, input)
}

// TestTaskCreator 用试跑快照创建试跑任务（is_test，不扣积分），并查询试跑任务与它的执行追踪。
// 签名与 GenerationTaskService 一致，接线时直接把任务服务传进来。
type TestTaskCreator interface {
	// SubmitTest 创建试跑任务。
	SubmitTest(ctx context.Context, userID uint64, snap *provider.Snapshot, input map[string]any) (*model.GenerationTaskView, error)
	// GetTestTask 查询试跑任务视图，只能查自己创建的，查不到返回 errcode.ErrTaskNotFound（或 repository.ErrNotFound）。
	GetTestTask(ctx context.Context, userID, taskID uint64) (*model.GenerationTaskView, error)
	// GetTestTrace 读取试跑任务的执行追踪（已脱敏），还没有追踪时返回空切片；查不到的约定同 GetTestTask。
	GetTestTrace(ctx context.Context, userID, taskID uint64) ([]provider.TraceStep, error)
}

// RegistryInvalidator 是 Registry 缓存失效的广播钩子：本实例刷新内存后调用，
// 以后多实例部署时用 Redis pub/sub 实现，其他实例收到消息后调用 AIConfigService.RefreshRegistry。
type RegistryInvalidator interface {
	// Invalidate 广播一次失效，reason 只用于日志。
	Invalidate(ctx context.Context, reason string)
}

// 缓存与列表相关的默认参数。
const (
	aiRegistryTTL       = 30 * time.Second // Registry 内存缓存的兜底过期时间（多实例没有广播时，最迟 30 秒对齐）
	aiSecretCacheTTL    = 30 * time.Second // 凭证明文的内存缓存时间，SetSecret 会立即清除本实例缓存
	aiRevisionListLimit = 100              // 历史版本列表条数上限
	aiModelKeyMaxLen    = 128              // 模型 key 的最大长度，与 ai_models.key 列宽一致
	aiModelKindMaxLen   = 16               // kind 的最大长度，与 ai_models.kind 列宽一致
)

// aiConfigKeyRe 限制模型 key 的字符集：key 会出现在 URL 路径、缓存 key 和日志里，不允许特殊字符。
var aiConfigKeyRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.\-]*$`)

// AIConfigService 管理模型配置的草稿 / 发布 / 回滚与渠道凭证，同时实现 provider.Registry（只读已发布模型 + 渠道 + 插件版本）
// 与 provider.SecretResolver（解密渠道 Key 给插件宿主注入鉴权）。发布后立即刷新本实例内存，热生效。
// 插件与渠道的管理在别的服务里，它们变更后调用 RefreshRegistry 让 Registry 立即对齐。
type AIConfigService struct {
	repo     AIConfigRepo
	channels AIChannelReader
	plugins  AIPluginReader
	now      func() time.Time

	// 以下依赖有循环引用（宿主依赖本服务取凭证，本服务又要调宿主 dry-run / 任务服务），所以通过 Set* 在接线后注入。
	dryRunner   DryRunner
	testTasks   TestTaskCreator
	invalidator RegistryInvalidator

	// 凭证加解密；cipher 为 nil 表示没有配置主密钥。
	cipher   *aiSecretCipher
	secMu    sync.Mutex
	secCache map[string]aiSecretCacheItem

	// Registry 内存状态。regMu 保护 regState 的读写；regLoadMu 串行化刷新，避免并发刷新打爆数据库。
	regMu      sync.RWMutex
	regState   *aiRegistryState
	regLoadMu  sync.Mutex
	regTTL     time.Duration
	modelParse map[uint64]*modelcfg.ModelConfig // 按 revision_id 缓存的解析结果（只在 regLoadMu 保护下访问）
}

var (
	_ provider.Registry       = (*AIConfigService)(nil)
	_ provider.SecretResolver = (*AIConfigService)(nil)
)

// NewAIConfigService 创建配置服务。channels / plugins 是渠道与插件的只读仓储（发布前置检查与 Registry 加载用）；
// secretKey 是凭证主密钥（cfg.AI.SecretKey），为空时服务照常启动，但设置 / 读取凭证会返回明确错误。
func NewAIConfigService(repo AIConfigRepo, channels AIChannelReader, plugins AIPluginReader, secretKey string) *AIConfigService {
	return &AIConfigService{
		repo:       repo,
		channels:   channels,
		plugins:    plugins,
		now:        time.Now,
		cipher:     newAISecretCipher(secretKey),
		secCache:   map[string]aiSecretCacheItem{},
		regTTL:     aiRegistryTTL,
		modelParse: map[uint64]*modelcfg.ModelConfig{},
	}
}

// SetDryRunner 注入 dry-run 执行器（插件宿主适配器）。
func (s *AIConfigService) SetDryRunner(d DryRunner) { s.dryRunner = d }

// SetTestTaskCreator 注入试跑任务服务。
func (s *AIConfigService) SetTestTaskCreator(t TestTaskCreator) { s.testTasks = t }

// SetInvalidator 注入缓存失效广播（多实例时用 Redis 实现），不设置则只刷新本实例。
func (s *AIConfigService) SetInvalidator(i RegistryInvalidator) { s.invalidator = i }

// ---------------------------------------------------------------------------
// 请求 / 响应结构
// ---------------------------------------------------------------------------

// ModelDraftInput 是保存模型草稿的参数。
type ModelDraftInput struct {
	Key     string          // 更新时是路径里的 key；新建时忽略（取正文里的 key）
	Create  bool            // true 新建（key 已存在则报错），false 更新（key 必须已存在）
	Body    json.RawMessage // 配置正文（JSON 对象）
	Note    string          // 本次修改说明
	AdminID uint64          // 操作人
}

// SaveDraftResult 是保存草稿的结果：草稿一定已保存，Issues 是当前校验发现的问题（有问题时不能发布）。
type SaveDraftResult struct {
	Revision *model.AIConfigRevision `json:"revision"`
	Issues   []modelcfg.Issue        `json:"issues"`
}

// ValidateResult 是校验结果。
type ValidateResult struct {
	Valid  bool             `json:"valid"`
	Issues []modelcfg.Issue `json:"issues"`
}

// ConfigListItem 是管理端模型列表的一行：指针行信息 + 草稿 / 发布状态汇总，不含正文。
type ConfigListItem struct {
	Key                 string    `json:"key"`
	Kind                string    `json:"kind"`
	Vendor              string    `json:"vendor"`  // 厂商 slug，取值规则同 label；没有为空串
	Tags                []string  `json:"tags"`    // 展示标签，取值规则同 label；没有为 []
	Label               string    `json:"label"`   // 展示名：取已发布版本的 label，没发布过取最新草稿的
	Channel             string    `json:"channel"` // 绑定的渠道 key（channels[0].channel），同样先看已发布版本；正文里没写为空串
	Enabled             bool      `json:"enabled"`
	Sort                int       `json:"sort"`
	PublishedRevisionID *uint64   `json:"published_revision_id"`
	PublishedRevisionNo *int      `json:"published_revision_no"`
	DraftRevisionNo     *int      `json:"draft_revision_no"`
	HasUnpublishedDraft bool      `json:"has_unpublished_draft"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// ConfigDetail 是管理端模型详情：草稿与已发布正文（含 revision 元信息）。
type ConfigDetail struct {
	Target    string                  `json:"target"` // 固定为 model，保留字段以兼容前端
	Key       string                  `json:"key"`
	Kind      string                  `json:"kind"`
	Enabled   bool                    `json:"enabled"`
	Sort      int                     `json:"sort"`
	Draft     *model.AIConfigRevision `json:"draft"`
	Published *model.AIConfigRevision `json:"published"`
	UpdatedAt time.Time               `json:"updated_at"`
}

// TestTraceView 是试跑追踪的响应：GET /admin/ai/test-runs/:id/trace。
type TestTraceView struct {
	Steps []provider.TraceStep `json:"steps"`
}

// aiConfigMeta 是从配置正文里宽松取出的少量字段：key / kind / enabled / sort 要同步到指针行，channels 只用于日志。
// 这里不做完整校验（运营需要保存半成品），完整校验在 modelcfg.ParseModel。
type aiConfigMeta struct {
	Key      string `json:"key"`
	Kind     string `json:"kind"`
	Label    string `json:"label"`
	Vendor   string `json:"vendor"`
	Tags     []any  `json:"tags"` // 宽松解码：草稿里写错类型不能让整条列表项丢掉 label
	Enabled  bool   `json:"enabled"`
	Sort     int    `json:"sort"`
	Channels []struct {
		Channel string `json:"channel"`
	} `json:"channels"`
}

// channel 返回正文里 channels[0].channel，没有返回空串。
func (m *aiConfigMeta) channel() string {
	if len(m.Channels) == 0 {
		return ""
	}
	return m.Channels[0].Channel
}
