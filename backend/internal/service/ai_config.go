package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/dsl"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/repository"
)

// AIConfigRepo 是 AI 配置服务依赖的数据访问接口，由 repository.AIConfigRepository 实现，测试里用内存 fake 替换。
type AIConfigRepo interface {
	SaveDraft(ctx context.Context, in repository.SaveDraftInput) (*model.AIConfigRevision, error)
	GetDraft(ctx context.Context, target, key string) (*model.AIConfigRevision, error)
	GetRevision(ctx context.Context, id uint64) (*model.AIConfigRevision, error)
	GetPublishedRevision(ctx context.Context, target, key string) (*model.AIConfigRevision, error)
	ListRevisions(ctx context.Context, target, key string, limit int) ([]model.AIConfigRevision, error)
	ListRevisionHeads(ctx context.Context, target string, withBody bool) ([]model.AIConfigRevision, error)
	PublishDraft(ctx context.Context, ptr repository.ConfigPointer, revisionID uint64) (*model.AIConfigRevision, error)
	Rollback(ctx context.Context, ptr repository.ConfigPointer, revisionID uint64) (*model.AIConfigRevision, error)

	GetProviderPointer(ctx context.Context, key string) (*model.AIProvider, error)
	ListProviderPointers(ctx context.Context) ([]model.AIProvider, error)
	GetModelPointer(ctx context.Context, key string) (*model.AIModel, error)
	ListModelPointers(ctx context.Context) ([]model.AIModel, error)
	SetModelEnabled(ctx context.Context, key string, enabled bool) error
	SetModelSort(ctx context.Context, key string, sort int) error

	LoadPublishedProviders(ctx context.Context) ([]repository.PublishedProvider, error)
	LoadPublishedModels(ctx context.Context) ([]repository.PublishedModel, error)

	UpsertSecret(ctx context.Context, s *model.AISecret) error
	GetSecret(ctx context.Context, name string) (*model.AISecret, error)
	ListSecrets(ctx context.Context) ([]model.AISecret, error)
}

// ConfigValidator 是配置校验器：把 dsl 包的解析 / 校验函数抽成接口，
// 方便单元测试注入 fake，也让本服务不依赖 dsl 的具体实现进度。
type ConfigValidator interface {
	// ParseProvider 解析并校验平台配置正文，没有 Issue 才算通过。
	ParseProvider(body []byte) (*dsl.ProviderConfig, []dsl.Issue)
	// ParseModel 解析并校验模型配置正文；provider 为 nil 时跳过跨对象检查。
	ParseModel(body []byte, provider *dsl.ProviderConfig) (*dsl.ModelConfig, []dsl.Issue)
	// ValidateInput 按 input_schema 校验并规范化用户输入。
	ValidateInput(schema dsl.InputSchema, input map[string]any) (map[string]any, []dsl.FieldError)
	// JSONSchema 返回配置正文的 JSON Schema（target 为 provider / model），供前端编辑器补全。
	JSONSchema(target string) ([]byte, error)
}

// DryRunner 渲染但不发送请求（dry-run）。返回值由引擎脱敏并已可序列化为 JSON，
// 用接口隔离 engine 包的具体类型，接线时用适配器包一层。
type DryRunner interface {
	DryRun(ctx context.Context, snap *dsl.Snapshot, input map[string]any) (any, error)
}

// DryRunnerFunc 让普通函数满足 DryRunner，接线时可直接包 engine.DryRun。
type DryRunnerFunc func(ctx context.Context, snap *dsl.Snapshot, input map[string]any) (any, error)

func (f DryRunnerFunc) DryRun(ctx context.Context, snap *dsl.Snapshot, input map[string]any) (any, error) {
	return f(ctx, snap, input)
}

// HTTPClientFactory 创建受 SSRF 防护（host 白名单、内网 IP 拦截、重定向校验）的 HTTP 客户端。
type HTTPClientFactory interface {
	NewClient(allowedHosts []string, timeout time.Duration) *http.Client
}

// HTTPClientFactoryFunc 让普通函数满足 HTTPClientFactory，接线时可直接包 engine.NewGuardedClient。
type HTTPClientFactoryFunc func(allowedHosts []string, timeout time.Duration) *http.Client

func (f HTTPClientFactoryFunc) NewClient(allowedHosts []string, timeout time.Duration) *http.Client {
	return f(allowedHosts, timeout)
}

// TestTaskCreator 用草稿快照创建试跑任务（is_test，不扣积分），并按 id 查询试跑任务。
type TestTaskCreator interface {
	SubmitTest(ctx context.Context, userID uint64, snap *dsl.Snapshot, input map[string]any) (*model.GenerationTaskView, error)
	// GetTestTask 查询试跑任务视图，只能查自己创建的试跑任务，查不到返回 errcode.ErrTaskNotFound（或 repository.ErrNotFound）。
	// 签名与 GenerationTaskService 一致，可以直接把任务服务传进来。
	GetTestTask(ctx context.Context, userID, taskID uint64) (*model.GenerationTaskView, error)
}

// RegistryInvalidator 是 Registry 缓存失效的广播钩子：本实例刷新内存后调用，
// 以后多实例部署时用 Redis pub/sub 实现，其他实例收到消息后调用 AIConfigService.RefreshRegistry。
type RegistryInvalidator interface {
	Invalidate(ctx context.Context, reason string)
}

// dslValidator 是 ConfigValidator 的默认实现，薄薄包一层 dsl 函数。
type dslValidator struct{}

// NewDSLValidator 创建默认校验器（直接调用 dsl 包）。
func NewDSLValidator() ConfigValidator { return dslValidator{} }

func (dslValidator) ParseProvider(body []byte) (*dsl.ProviderConfig, []dsl.Issue) {
	return dsl.ParseProvider(body)
}

func (dslValidator) ParseModel(body []byte, provider *dsl.ProviderConfig) (*dsl.ModelConfig, []dsl.Issue) {
	return dsl.ParseModel(body, provider)
}

func (dslValidator) ValidateInput(schema dsl.InputSchema, input map[string]any) (map[string]any, []dsl.FieldError) {
	return dsl.ValidateInput(schema, input)
}

// JSONSchema 返回 dsl 生成的 JSON Schema；target 不认识时 dsl 返回 nil，这里转成错误。
func (dslValidator) JSONSchema(target string) ([]byte, error) {
	b := dsl.JSONSchema(target)
	if len(b) == 0 {
		return nil, fmt.Errorf("没有 %q 的 JSON Schema", target)
	}
	return b, nil
}

// 缓存与列表相关的默认参数。
const (
	aiRegistryTTL       = 30 * time.Second // Registry 内存缓存的兜底过期时间（多实例没有广播时，最迟 30 秒对齐）
	aiSecretCacheTTL    = 30 * time.Second // 凭证明文的内存缓存时间，SetSecret 会立即清除本实例缓存
	aiRevisionListLimit = 100              // 历史版本列表条数上限
)

// aiConfigKeyRe 限制配置 key 的字符集：key 会出现在 URL 路径、缓存 key 和日志里，不允许特殊字符。
var aiConfigKeyRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.\-]*$`)

// AIConfigService 管理 AI 配置的草稿 / 发布 / 回滚 / 凭证，同时实现 provider.Registry（只读已发布配置）
// 与 provider.SecretResolver（解密凭证给引擎用）。发布后立即刷新本实例内存，热生效。
type AIConfigService struct {
	repo      AIConfigRepo
	validator ConfigValidator
	now       func() time.Time

	// 以下依赖有循环引用（引擎依赖本服务，本服务又要调引擎），所以通过 Set* 在接线后注入。
	dryRunner   DryRunner
	httpClients HTTPClientFactory
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
	provParsed map[uint64]*dsl.ProviderConfig // 按 revision_id 缓存的解析结果（只在 regLoadMu 保护下访问）
	modelParse map[uint64]*dsl.ModelConfig
}

var (
	_ provider.Registry       = (*AIConfigService)(nil)
	_ provider.SecretResolver = (*AIConfigService)(nil)
)

// NewAIConfigService 创建配置服务。secretKey 是凭证主密钥（cfg.AI.SecretKey），为空时服务照常启动，
// 但设置 / 读取凭证会返回明确错误。
func NewAIConfigService(repo AIConfigRepo, validator ConfigValidator, secretKey string) *AIConfigService {
	return &AIConfigService{
		repo:       repo,
		validator:  validator,
		now:        time.Now,
		cipher:     newAISecretCipher(secretKey),
		secCache:   map[string]aiSecretCacheItem{},
		regTTL:     aiRegistryTTL,
		provParsed: map[uint64]*dsl.ProviderConfig{},
		modelParse: map[uint64]*dsl.ModelConfig{},
	}
}

// SetDryRunner 注入 dry-run 执行器（引擎适配器）。
func (s *AIConfigService) SetDryRunner(d DryRunner) { s.dryRunner = d }

// SetHTTPClientFactory 注入受 SSRF 防护的 HTTP 客户端工厂（RunningHub 导入用）。
func (s *AIConfigService) SetHTTPClientFactory(f HTTPClientFactory) { s.httpClients = f }

// SetTestTaskCreator 注入试跑任务服务。
func (s *AIConfigService) SetTestTaskCreator(t TestTaskCreator) { s.testTasks = t }

// SetInvalidator 注入缓存失效广播（多实例时用 Redis 实现），不设置则只刷新本实例。
func (s *AIConfigService) SetInvalidator(i RegistryInvalidator) { s.invalidator = i }

// ---------------------------------------------------------------------------
// 请求 / 响应结构
// ---------------------------------------------------------------------------

// SaveDraftResult 是保存草稿的结果：草稿一定已保存，Issues 是当前校验发现的问题（有问题时不能发布）。
type SaveDraftResult struct {
	Revision *model.AIConfigRevision `json:"revision"`
	Issues   []dsl.Issue             `json:"issues"`
}

// ValidateResult 是校验结果。
type ValidateResult struct {
	Valid  bool        `json:"valid"`
	Issues []dsl.Issue `json:"issues"`
}

// ConfigListItem 是管理端列表的一行：指针行信息 + 草稿 / 发布状态汇总，不含正文。
type ConfigListItem struct {
	Key                 string    `json:"key"`
	Name                string    `json:"name,omitempty"`         // provider
	Kind                string    `json:"kind,omitempty"`         // model
	ProviderKey         string    `json:"provider_key,omitempty"` // model
	Enabled             *bool     `json:"enabled,omitempty"`      // model
	Sort                *int      `json:"sort,omitempty"`         // model
	PublishedRevisionID *uint64   `json:"published_revision_id"`
	PublishedRevisionNo *int      `json:"published_revision_no"`
	DraftRevisionNo     *int      `json:"draft_revision_no"`
	HasUnpublishedDraft bool      `json:"has_unpublished_draft"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// ConfigDetail 是管理端详情：草稿与已发布正文（含 revision 元信息）。
type ConfigDetail struct {
	Target      string                  `json:"target"`
	Key         string                  `json:"key"`
	Name        string                  `json:"name,omitempty"`
	Kind        string                  `json:"kind,omitempty"`
	ProviderKey string                  `json:"provider_key,omitempty"`
	Enabled     *bool                   `json:"enabled,omitempty"`
	Sort        *int                    `json:"sort,omitempty"`
	Draft       *model.AIConfigRevision `json:"draft"`
	Published   *model.AIConfigRevision `json:"published"`
	UpdatedAt   time.Time               `json:"updated_at"`
}

// aiConfigMeta 是从配置正文里取出的、要与指针表同步的少量字段。
type aiConfigMeta struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Provider string `json:"provider"`
	Enabled  bool   `json:"enabled"`
	Sort     int    `json:"sort"`
}

// ---------------------------------------------------------------------------
// 草稿 / 校验
// ---------------------------------------------------------------------------

// SaveDraft 保存配置草稿并返回校验发现的问题。create=true 是新建（key 取自正文，已存在则报错），
// create=false 是更新（key 来自路径，必须已存在，且与正文里的 key 一致）。
// 有校验问题也照常保存（运营需要保存半成品），只是发布时会被拦下。
func (s *AIConfigService) SaveDraft(ctx context.Context, target, key string, create bool, body json.RawMessage, note string, adminID uint64) (*SaveDraftResult, error) {
	// 1. 校验目标类型
	if err := aiCheckTarget(target); err != nil {
		return nil, err
	}

	// 2. 解析正文里的 key / name / kind / provider：这些字段要同步到指针表，所以正文必须至少是个带 key 的 JSON 对象，
	//    否则无法确定这份草稿属于谁，无法保存
	meta, err := aiParseMeta(body)
	if err != nil {
		return nil, err
	}
	if create {
		key = meta.Key
	} else if meta.Key != key {
		return nil, errcode.ErrInvalidParams.WithMsg("正文里的 key 与路径不一致")
	}
	if err := aiCheckMeta(target, meta); err != nil {
		return nil, err
	}

	// 3. 新建要求 key 未被占用，更新要求已存在（避免 PUT 拼错 key 悄悄新建一个配置）
	exists, err := s.pointerExists(ctx, target, key)
	if err != nil {
		return nil, err
	}
	if create && exists {
		return nil, errcode.ErrConfigInvalid.WithMsg(fmt.Sprintf("配置 %q 已存在，请使用 PUT 更新草稿", key))
	}
	if !create && !exists {
		return nil, errcode.ErrConfigNotFound
	}

	// 4. 校验正文（不阻塞保存）：模型的跨对象检查优先用已发布的平台，没有就用平台草稿
	issues, err := s.collectIssues(ctx, target, key, body, aiProviderPublishedThenDraft)
	if err != nil {
		return nil, err
	}

	// 5. 写入草稿：repo 事务里同步指针行、归档旧草稿、递增版本号。
	//    enabled / sort 只在模型首次创建时取正文里的值，之后由上下架接口独占，避免改 JSON 时悄悄改变上架状态
	rev, err := s.repo.SaveDraft(ctx, repository.SaveDraftInput{
		Pointer:        aiPointer(target, meta),
		Body:           body,
		CreatedBy:      adminID,
		Note:           note,
		InitialEnabled: meta.Enabled,
		InitialSort:    meta.Sort,
	})
	if err != nil {
		return nil, err
	}
	logger.Info("保存 AI 配置草稿", zap.String("target", target), zap.String("key", key),
		zap.Int("revision_no", rev.RevisionNo), zap.Uint64("admin_id", adminID), zap.Int("issues", len(issues)))
	return &SaveDraftResult{Revision: rev, Issues: issues}, nil
}

// Validate 校验配置。body 非空时校验传入的正文（编辑器实时校验，不落库）；
// 为空时校验已保存的最新草稿，没有草稿返回 ErrConfigNoDraft。
func (s *AIConfigService) Validate(ctx context.Context, target, key string, body json.RawMessage) (*ValidateResult, error) {
	// 1. 校验目标类型
	if err := aiCheckTarget(target); err != nil {
		return nil, err
	}
	// 2. 没有传正文就取最新草稿
	if len(body) == 0 {
		draft, err := s.repo.GetDraft(ctx, target, key)
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errcode.ErrConfigNoDraft
		}
		if err != nil {
			return nil, err
		}
		body = json.RawMessage(draft.BodyJSON)
	}
	// 3. 收集问题；Issues 保证是非 nil 切片，前端可以直接遍历
	issues, err := s.collectIssues(ctx, target, key, body, aiProviderPublishedThenDraft)
	if err != nil {
		return nil, err
	}
	return &ValidateResult{Valid: len(issues) == 0, Issues: issues}, nil
}

// JSONSchema 返回配置正文的 JSON Schema，供前端 Monaco 编辑器做补全。
func (s *AIConfigService) JSONSchema(target string) (json.RawMessage, error) {
	// 1. 校验目标类型
	if err := aiCheckTarget(target); err != nil {
		return nil, err
	}
	// 2. 取 schema；取不到属于服务端问题，记录日志后按内部错误返回
	b, err := s.validator.JSONSchema(target)
	if err != nil {
		return nil, fmt.Errorf("获取 %s 的 JSON Schema 失败：%w", target, err)
	}
	return json.RawMessage(b), nil
}

// ---------------------------------------------------------------------------
// 查询
// ---------------------------------------------------------------------------

// ListConfigs 列出某类配置的概览（不含正文），带草稿 / 发布状态汇总。
func (s *AIConfigService) ListConfigs(ctx context.Context, target string) ([]ConfigListItem, error) {
	// 1. 校验目标类型
	if err := aiCheckTarget(target); err != nil {
		return nil, err
	}
	// 2. 取所有 draft / published 的 revision 元信息，按 key 归并；不读正文，避免列表接口读出大字段
	heads, err := s.repo.ListRevisionHeads(ctx, target, false)
	if err != nil {
		return nil, err
	}
	drafts := map[string]model.AIConfigRevision{}
	published := map[string]model.AIConfigRevision{}
	for _, h := range heads {
		if h.Status == model.RevisionDraft {
			if old, ok := drafts[h.TargetKey]; !ok || h.RevisionNo > old.RevisionNo {
				drafts[h.TargetKey] = h
			}
		} else if h.Status == model.RevisionPublished {
			published[h.TargetKey] = h
		}
	}
	// 3. 与指针行合并
	items := []ConfigListItem{}
	fill := func(item *ConfigListItem) {
		if d, ok := drafts[item.Key]; ok {
			no := d.RevisionNo
			item.DraftRevisionNo = &no
			item.HasUnpublishedDraft = true
		}
		if p, ok := published[item.Key]; ok {
			no := p.RevisionNo
			item.PublishedRevisionNo = &no
		}
	}
	if target == model.ConfigTargetProvider {
		rows, err := s.repo.ListProviderPointers(ctx)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			item := ConfigListItem{Key: r.Key, Name: r.Name, PublishedRevisionID: r.PublishedRevisionID, UpdatedAt: r.UpdatedAt}
			fill(&item)
			items = append(items, item)
		}
		return items, nil
	}
	rows, err := s.repo.ListModelPointers(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		enabled, sort := r.Enabled, r.Sort
		item := ConfigListItem{Key: r.Key, Kind: r.Kind, ProviderKey: r.ProviderKey, Enabled: &enabled, Sort: &sort,
			PublishedRevisionID: r.PublishedRevisionID, UpdatedAt: r.UpdatedAt}
		fill(&item)
		items = append(items, item)
	}
	return items, nil
}

// GetConfig 返回配置详情：最新草稿与当前已发布版本的完整正文。目标不存在返回 ErrConfigNotFound。
func (s *AIConfigService) GetConfig(ctx context.Context, target, key string) (*ConfigDetail, error) {
	// 1. 校验目标类型
	if err := aiCheckTarget(target); err != nil {
		return nil, err
	}
	// 2. 指针行：不存在就是 404
	detail := &ConfigDetail{Target: target, Key: key}
	if target == model.ConfigTargetProvider {
		p, err := s.repo.GetProviderPointer(ctx, key)
		if err != nil {
			return nil, aiNotFound(err)
		}
		detail.Name, detail.UpdatedAt = p.Name, p.UpdatedAt
	} else {
		m, err := s.repo.GetModelPointer(ctx, key)
		if err != nil {
			return nil, aiNotFound(err)
		}
		enabled, sort := m.Enabled, m.Sort
		detail.Kind, detail.ProviderKey, detail.Enabled, detail.Sort, detail.UpdatedAt = m.Kind, m.ProviderKey, &enabled, &sort, m.UpdatedAt
	}
	// 3. 草稿与发布版本：没有就是 null，不算错误
	draft, err := s.repo.GetDraft(ctx, target, key)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	if err == nil {
		detail.Draft = draft
	}
	pub, err := s.repo.GetPublishedRevision(ctx, target, key)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	if err == nil {
		detail.Published = pub
	}
	return detail, nil
}

// ListRevisions 返回配置的历史版本（不含正文，新到旧）。目标不存在返回 ErrConfigNotFound。
func (s *AIConfigService) ListRevisions(ctx context.Context, target, key string) ([]model.AIConfigRevision, error) {
	// 1. 校验目标类型与目标存在
	if err := aiCheckTarget(target); err != nil {
		return nil, err
	}
	if exists, err := s.pointerExists(ctx, target, key); err != nil {
		return nil, err
	} else if !exists {
		return nil, errcode.ErrConfigNotFound
	}
	// 2. 查询历史
	return s.repo.ListRevisions(ctx, target, key, aiRevisionListLimit)
}

// GetRevision 返回某个历史版本的完整正文（回滚前预览用）；revision 不属于该目标视为不存在。
func (s *AIConfigService) GetRevision(ctx context.Context, target, key string, revisionID uint64) (*model.AIConfigRevision, error) {
	// 1. 校验目标类型
	if err := aiCheckTarget(target); err != nil {
		return nil, err
	}
	// 2. 查询并核对归属：其他目标的 revision 一律按不存在处理，不暴露它们的存在
	rev, err := s.repo.GetRevision(ctx, revisionID)
	if err != nil {
		return nil, aiNotFound(err)
	}
	if rev.Target != target || rev.TargetKey != key {
		return nil, errcode.ErrConfigNotFound
	}
	return rev, nil
}

// ---------------------------------------------------------------------------
// 发布 / 回滚 / 上下架
// ---------------------------------------------------------------------------

// Publish 发布最新草稿。发布前必须：正文无校验问题；模型引用的平台已发布；平台 auth.secret 引用的凭证已设置。
// 发布成功后立即刷新 Registry 内存，新任务马上使用新版本，进行中的任务仍按各自的快照执行。
func (s *AIConfigService) Publish(ctx context.Context, target, key string, adminID uint64) (*model.AIConfigRevision, error) {
	// 1. 校验目标类型
	if err := aiCheckTarget(target); err != nil {
		return nil, err
	}
	// 2. 取最新草稿：没有草稿返回 409
	draft, err := s.repo.GetDraft(ctx, target, key)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrConfigNoDraft
	}
	if err != nil {
		return nil, err
	}
	// 3. 发布前置检查（校验、平台已发布、凭证已设置）
	ptr, err := s.checkPublishable(ctx, target, key, draft.BodyJSON)
	if err != nil {
		return nil, err
	}
	// 4. 事务里切换发布指针。用 draft.ID 而不是“最新草稿”，
	//    这样校验之后草稿又被别人改掉时不会把没校验过的正文发布出去，而是返回冲突让运营刷新
	rev, err := s.repo.PublishDraft(ctx, ptr, draft.ID)
	if errors.Is(err, repository.ErrNotFound) || errors.Is(err, repository.ErrRevisionConflict) {
		return nil, errcode.ErrConfigNoDraft.WithMsg("草稿已发生变化，请刷新后重新发布")
	}
	if err != nil {
		return nil, err
	}
	logger.Info("发布 AI 配置", zap.String("target", target), zap.String("key", key),
		zap.Uint64("revision_id", rev.ID), zap.Int("revision_no", rev.RevisionNo), zap.Uint64("admin_id", adminID))
	// 5. 热生效：刷新本实例内存并广播
	s.notifyChanged(ctx, "publish "+target+"/"+key)
	return rev, nil
}

// Rollback 把发布指针改回历史版本。只允许回到已归档的版本；回滚前按发布同样的标准重新校验，
// 避免回到一个当时没校验过的旧草稿，或者引用的平台 / 凭证已经不满足条件的版本。
func (s *AIConfigService) Rollback(ctx context.Context, target, key string, revisionID, adminID uint64) (*model.AIConfigRevision, error) {
	// 1. 校验目标类型，并取出目标 revision，核对归属
	rev, err := s.GetRevision(ctx, target, key, revisionID)
	if err != nil {
		return nil, err
	}
	// 2. 状态检查：当前已发布的没必要回滚，草稿不是历史版本
	switch rev.Status {
	case model.RevisionPublished:
		return nil, errcode.ErrConfigInvalid.WithMsg("该版本已是当前发布版本")
	case model.RevisionDraft:
		return nil, errcode.ErrConfigInvalid.WithMsg("草稿不是历史版本，请使用发布")
	}
	// 3. 发布前置检查
	ptr, err := s.checkPublishable(ctx, target, key, rev.BodyJSON)
	if err != nil {
		return nil, err
	}
	// 4. 事务里改回指针；并发被别人改动（状态变化）时返回冲突
	out, err := s.repo.Rollback(ctx, ptr, rev.ID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrConfigNotFound
	}
	if errors.Is(err, repository.ErrRevisionConflict) {
		return nil, errcode.ErrConfigInvalid.WithMsg("版本状态已变化，请刷新后重试")
	}
	if err != nil {
		return nil, err
	}
	logger.Info("回滚 AI 配置", zap.String("target", target), zap.String("key", key),
		zap.Uint64("revision_id", out.ID), zap.Int("revision_no", out.RevisionNo), zap.Uint64("admin_id", adminID))
	// 5. 热生效
	s.notifyChanged(ctx, "rollback "+target+"/"+key)
	return out, nil
}

// SetModelEnabled 上架 / 下架模型。上架要求模型已经发布过；下架立即生效，进行中的任务不受影响。
func (s *AIConfigService) SetModelEnabled(ctx context.Context, key string, enabled bool, adminID uint64) error {
	// 1. 模型必须存在
	m, err := s.repo.GetModelPointer(ctx, key)
	if err != nil {
		return aiNotFound(err)
	}
	// 2. 上架前必须已发布：没发布的模型上架后也不会出现在清单里，属于误操作，提前拦下
	if enabled && m.PublishedRevisionID == nil {
		return errcode.ErrConfigInvalid.WithMsg("模型尚未发布，无法上架")
	}
	// 3. 写库并热生效
	if err := s.repo.SetModelEnabled(ctx, key, enabled); err != nil {
		return aiNotFound(err)
	}
	logger.Info("模型上下架", zap.String("key", key), zap.Bool("enabled", enabled), zap.Uint64("admin_id", adminID))
	s.notifyChanged(ctx, "enabled "+key)
	return nil
}

// SetModelSort 修改模型排序值（升序，越小越靠前）并热生效。
func (s *AIConfigService) SetModelSort(ctx context.Context, key string, sort int, adminID uint64) error {
	// 1. 写库；模型不存在返回 ErrConfigNotFound
	if err := s.repo.SetModelSort(ctx, key, sort); err != nil {
		return aiNotFound(err)
	}
	// 2. 热生效
	logger.Info("修改模型排序", zap.String("key", key), zap.Int("sort", sort), zap.Uint64("admin_id", adminID))
	s.notifyChanged(ctx, "sort "+key)
	return nil
}

// checkPublishable 是发布和回滚共用的前置检查，通过后返回要同步到指针行的冗余字段。
func (s *AIConfigService) checkPublishable(ctx context.Context, target, key string, body []byte) (repository.ConfigPointer, error) {
	meta, err := aiParseMeta(body)
	if err != nil {
		return repository.ConfigPointer{}, errcode.ErrConfigInvalid.WithMsg("配置正文不是合法的 JSON 对象")
	}
	// 1. 模型发布要求它引用的平台已发布：模型的校验也以“已发布的平台”为准，
	//    这样发布后引擎拿到的 provider 与校验时看到的一致
	if target == model.ConfigTargetModel {
		if _, err := s.repo.GetPublishedRevision(ctx, model.ConfigTargetProvider, meta.Provider); errors.Is(err, repository.ErrNotFound) {
			return repository.ConfigPointer{}, errcode.ErrConfigInvalid.WithMsg(fmt.Sprintf("模型引用的平台 %q 尚未发布，请先发布平台", meta.Provider))
		} else if err != nil {
			return repository.ConfigPointer{}, err
		}
	}
	// 2. 校验正文，有任何问题都不允许发布
	issues, err := s.collectIssues(ctx, target, key, body, aiProviderPublishedOnly)
	if err != nil {
		return repository.ConfigPointer{}, err
	}
	if len(issues) > 0 {
		return repository.ConfigPointer{}, errcode.ErrConfigInvalid.WithMsg("配置校验未通过：" + aiFormatIssues(issues))
	}
	// 3. 平台发布要求 auth.secret 引用的凭证已设置：否则发布后每个任务都会因缺凭证而失败
	if target == model.ConfigTargetProvider {
		cfg, _ := s.validator.ParseProvider(body)
		if cfg != nil && cfg.Auth.Type != dsl.AuthNone && cfg.Auth.Secret != "" {
			if _, err := s.repo.GetSecret(ctx, cfg.Auth.Secret); errors.Is(err, repository.ErrNotFound) {
				return repository.ConfigPointer{}, errcode.ErrSecretNotSet.WithMsg(fmt.Sprintf("凭证 %q 尚未设置，请先设置后再发布", cfg.Auth.Secret))
			} else if err != nil {
				return repository.ConfigPointer{}, err
			}
		}
	}
	return aiPointer(target, meta), nil
}

// ---------------------------------------------------------------------------
// 内部辅助
// ---------------------------------------------------------------------------

// 模型校验时取平台配置的来源顺序。
var (
	aiProviderPublishedOnly      = []string{"published"}
	aiProviderPublishedThenDraft = []string{"published", "draft"}
	aiProviderDraftThenPublished = []string{"draft", "published"}
)

// collectIssues 校验一份正文并返回问题列表（永不为 nil）。正文里的 key 必须与目标 key 一致。
// 模型的跨对象检查需要平台配置：按 order 找不到、或平台自己有问题时跳过跨对象检查（平台的问题会在平台自己的校验里暴露）。
func (s *AIConfigService) collectIssues(ctx context.Context, target, key string, body []byte, order []string) ([]dsl.Issue, error) {
	issues := []dsl.Issue{}
	meta, err := aiParseMeta(body)
	if err != nil {
		return append(issues, dsl.Issue{Message: "配置正文必须是包含 key 的 JSON 对象"}), nil
	}
	if meta.Key != key {
		issues = append(issues, dsl.Issue{Path: "key", Message: fmt.Sprintf("key 必须与目标一致（%q）", key)})
	}
	if target == model.ConfigTargetProvider {
		_, found := s.validator.ParseProvider(body)
		return append(issues, found...), nil
	}
	var provider *dsl.ProviderConfig
	if p, _, err := s.loadProvider(ctx, meta.Provider, order); err == nil {
		provider = p
	} else if !isAIConfigErr(err) {
		return nil, err
	}
	_, found := s.validator.ParseModel(body, provider)
	return append(issues, found...), nil
}

// loadProvider 按 order（published / draft）依次查找平台配置并解析，返回配置与它所在的 revision。
// 都找不到返回 ErrConfigNotFound；找到但自身校验不通过返回 ErrConfigInvalid。
func (s *AIConfigService) loadProvider(ctx context.Context, providerKey string, order []string) (*dsl.ProviderConfig, *model.AIConfigRevision, error) {
	for _, src := range order {
		var rev *model.AIConfigRevision
		var err error
		if src == "draft" {
			rev, err = s.repo.GetDraft(ctx, model.ConfigTargetProvider, providerKey)
		} else {
			rev, err = s.repo.GetPublishedRevision(ctx, model.ConfigTargetProvider, providerKey)
		}
		if errors.Is(err, repository.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		cfg, issues := s.validator.ParseProvider(rev.BodyJSON)
		if len(issues) > 0 || cfg == nil {
			return nil, rev, errcode.ErrConfigInvalid.WithMsg(fmt.Sprintf("平台 %q 的配置有误：%s", providerKey, aiFormatIssues(issues)))
		}
		return cfg, rev, nil
	}
	return nil, nil, errcode.ErrConfigNotFound.WithMsg(fmt.Sprintf("平台 %q 不存在或尚未保存", providerKey))
}

// pointerExists 判断指针行是否存在。
func (s *AIConfigService) pointerExists(ctx context.Context, target, key string) (bool, error) {
	var err error
	if target == model.ConfigTargetProvider {
		_, err = s.repo.GetProviderPointer(ctx, key)
	} else {
		_, err = s.repo.GetModelPointer(ctx, key)
	}
	if errors.Is(err, repository.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

// notifyChanged 在配置变化后刷新本实例的 Registry 内存并广播失效。
// 刷新失败不回滚已提交的变更：把内存状态标记为过期，下次读取时会重新加载。
func (s *AIConfigService) notifyChanged(ctx context.Context, reason string) {
	if err := s.RefreshRegistry(ctx); err != nil {
		logger.Error("刷新 Registry 失败，将在下次读取时重试", zap.String("reason", reason), zap.Error(err))
		s.regMu.Lock()
		s.regState = nil
		s.regMu.Unlock()
	}
	if s.invalidator != nil {
		s.invalidator.Invalidate(ctx, reason)
	}
}

func aiCheckTarget(target string) error {
	if target != model.ConfigTargetProvider && target != model.ConfigTargetModel {
		return errcode.ErrInvalidParams.WithMsg("target 只能是 provider 或 model")
	}
	return nil
}

// aiParseMeta 从正文取出同步字段；正文不是 JSON 对象、缺少 key 都返回参数错误。
func aiParseMeta(body []byte) (*aiConfigMeta, error) {
	trimmed := strings.TrimSpace(string(body))
	if !strings.HasPrefix(trimmed, "{") {
		return nil, errcode.ErrInvalidParams.WithMsg("配置正文必须是 JSON 对象")
	}
	var m aiConfigMeta
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, errcode.ErrInvalidParams.WithMsg("配置正文不是合法的 JSON，或 key / name / kind / provider 类型错误")
	}
	if strings.TrimSpace(m.Key) == "" {
		return nil, errcode.ErrInvalidParams.WithMsg("配置正文缺少 key")
	}
	return &m, nil
}

// aiCheckMeta 校验要写入指针表的字段：key 字符集与长度、以及各列的长度上限（超长写库会报错）。
func aiCheckMeta(target string, m *aiConfigMeta) error {
	maxKey := 128
	if target == model.ConfigTargetProvider {
		maxKey = 64
	}
	if len(m.Key) > maxKey || !aiConfigKeyRe.MatchString(m.Key) {
		return errcode.ErrInvalidParams.WithMsg(fmt.Sprintf("key 只能包含字母、数字、下划线、点和短横线，且以字母或数字开头，长度不超过 %d", maxKey))
	}
	if len(m.Name) > 128 {
		return errcode.ErrInvalidParams.WithMsg("name 不能超过 128 个字符")
	}
	if len(m.Kind) > 16 {
		return errcode.ErrInvalidParams.WithMsg("kind 不能超过 16 个字符")
	}
	if len(m.Provider) > 64 {
		return errcode.ErrInvalidParams.WithMsg("provider 不能超过 64 个字符")
	}
	return nil
}

// aiPointer 由正文里的字段生成要同步到指针行的冗余字段；平台没写 name 时用 key 兜底。
func aiPointer(target string, m *aiConfigMeta) repository.ConfigPointer {
	p := repository.ConfigPointer{Target: target, Key: m.Key}
	if target == model.ConfigTargetProvider {
		p.Name = m.Name
		if p.Name == "" {
			p.Name = m.Key
		}
		return p
	}
	p.Kind, p.ProviderKey = m.Kind, m.Provider
	return p
}

// aiFormatIssues 把问题列表拼成一句话，最多列 10 条。
func aiFormatIssues(issues []dsl.Issue) string {
	const maxShown = 10
	parts := make([]string, 0, maxShown+1)
	for i, is := range issues {
		if i >= maxShown {
			parts = append(parts, fmt.Sprintf("……共 %d 条", len(issues)))
			break
		}
		if is.Path != "" {
			parts = append(parts, is.Path+"："+is.Message)
		} else {
			parts = append(parts, is.Message)
		}
	}
	return strings.Join(parts, "；")
}

// aiNotFound 把仓储的“不存在”翻译成 ErrConfigNotFound（404），其他错误原样透传。
func aiNotFound(err error) error {
	if errors.Is(err, repository.ErrNotFound) {
		return errcode.ErrConfigNotFound
	}
	return err
}

// isAIConfigErr 判断错误是否属于“配置本身不可用”（不存在 / 校验未通过），这类错误在尽力而为的跨对象检查里可以忽略。
func isAIConfigErr(err error) bool {
	var e *errcode.Error
	return errors.As(err, &e) && (e.Code == errcode.ErrConfigNotFound.Code || e.Code == errcode.ErrConfigInvalid.Code)
}
