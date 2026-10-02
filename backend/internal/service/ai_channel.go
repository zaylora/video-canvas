package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/provider/pluginmeta"
	"video-canvas/internal/repository"
)

// AIChannelRepo 是渠道服务的数据访问接口，由 repository.AIChannelRepository 实现，测试里用内存 fake 替换。
type AIChannelRepo interface {
	// CreateChannel 新建渠道；key 已存在返回 repository.ErrDuplicate，插件版本不存在返回 repository.ErrNotFound。
	CreateChannel(ctx context.Context, c *model.AIChannel) error
	// UpdateChannel 更新渠道的可变字段；渠道或它指向的插件版本不存在返回 repository.ErrNotFound。
	UpdateChannel(ctx context.Context, c *model.AIChannel) error
	// GetChannel 按 key 查询渠道，不存在返回 repository.ErrNotFound。
	GetChannel(ctx context.Context, key string) (*model.AIChannel, error)
	// ListChannels 返回所有渠道，按 key 升序。
	ListChannels(ctx context.Context) ([]model.AIChannel, error)
	// CountChannelRefs 统计渠道被模型（最新草稿 / 已发布版本）与非终态任务引用的情况，渠道不存在返回 repository.ErrNotFound。
	CountChannelRefs(ctx context.Context, key string) (repository.ChannelRefs, error)
	// DeleteChannel 事务内锁住渠道行、重新统计引用后删除渠道与它的 Key：仍被引用时返回当时的引用与 repository.ErrInUse，
	// 不存在返回 repository.ErrNotFound。
	DeleteChannel(ctx context.Context, key string) (repository.ChannelRefs, error)
}

// AIChannelPlugins 是渠道服务对插件表的只读依赖，由 repository.AIPluginRepository 实现。
type AIChannelPlugins interface {
	// GetPlugin 按 key 查询插件，不存在返回 repository.ErrNotFound。
	GetPlugin(ctx context.Context, key string) (*model.AIPlugin, error)
	// FindVersion 按 (plugin_key, version) 查询版本头信息，不存在返回 repository.ErrNotFound。
	FindVersion(ctx context.Context, pluginKey, version string) (*model.AIPluginVersion, error)
	// GetVersionHead 按 id 查询版本头信息，不存在返回 repository.ErrNotFound。
	GetVersionHead(ctx context.Context, id uint64) (*model.AIPluginVersion, error)
	// ListVersions 返回版本头信息；pluginKey 为空表示所有插件。
	ListVersions(ctx context.Context, pluginKey string) ([]model.AIPluginVersion, error)
}

// AIChannelSecrets 是渠道 Key 的存取依赖，由 AIConfigService 实现（凭证加解密都在那里）。
type AIChannelSecrets interface {
	// SetSecret 设置（覆盖）凭证，只写。
	SetSecret(ctx context.Context, name, value string, adminID uint64) error
	// SecretIsSet 判断凭证是否已设置，不解密。
	SecretIsSet(ctx context.Context, name string) (bool, error)
	// Get 解密并返回凭证明文，只用来对错误信息做防御性脱敏，绝不进响应。
	Get(ctx context.Context, name string) (string, error)
	// ForgetSecret 清掉本实例对该凭证的明文缓存（凭证行已被删除后调用）。
	ForgetSecret(name string)
}

// AIChannelService 管理渠道：创建 / 更新 / 详情 / 列表 / 删除、设置 Key、连通性检查、导入模型。
// 写操作只有 super_admin 能调（由路由的中间件保证），导入模型 admin 也能调。
type AIChannelService struct {
	repo     AIChannelRepo
	plugins  AIChannelPlugins
	secrets  AIChannelSecrets
	audit    AIAuditWriter
	ops      provider.PluginOps
	notifier RegistryNotifier
}

// NewAIChannelService 创建渠道服务。notifier 为 nil 时不刷新 Registry（只有测试会这样）。
func NewAIChannelService(repo AIChannelRepo, plugins AIChannelPlugins, secrets AIChannelSecrets, audit AIAuditWriter,
	ops provider.PluginOps, notifier RegistryNotifier) *AIChannelService {
	return &AIChannelService{repo: repo, plugins: plugins, secrets: secrets, audit: audit, ops: ops, notifier: notifier}
}

// 渠道字段的长度限制，与 ai_channels 的列宽一致。
const (
	channelNameMaxLen    = 128
	channelBaseURLMaxLen = 512
)

// channelKeyRe 限制渠道 key：它会出现在 URL 路径、凭证名（channel:<key>）和日志里。
var channelKeyRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// ChannelView 是管理端的渠道视图。Key 只写不读：只告诉调用方有没有设置（secret_set）。
type ChannelView struct {
	Key              string             `json:"key"`
	Name             string             `json:"name"`
	PluginKey        string             `json:"plugin_key"`
	PluginVersionID  uint64             `json:"plugin_version_id"`
	PluginVersion    string             `json:"plugin_version"` // 固定的插件版本号（semver）；版本行缺失时为空串
	BaseURL          string             `json:"base_url"`
	TrustedInternal  bool               `json:"trusted_internal"`
	AllowCredentials bool               `json:"allow_credentials"`
	Settings         map[string]any     `json:"settings"`
	RateLimit        provider.RateLimit `json:"rate_limit"`
	Enabled          bool               `json:"enabled"`
	SecretSet        bool               `json:"secret_set"`
	UpdatedBy        uint64             `json:"updated_by"`
	UpdatedAt        time.Time          `json:"updated_at"`
	CreatedAt        time.Time          `json:"created_at"`
}

// ChannelCreateInput 是新建渠道的参数。
type ChannelCreateInput struct {
	Key              string
	Name             string
	PluginKey        string
	PluginVersion    string // semver 字符串，固定到这个版本
	BaseURL          string
	TrustedInternal  bool
	AllowCredentials bool
	Settings         map[string]any
	RateLimit        provider.RateLimit
	Enabled          bool // 调用方在请求没传时按 true 传入
	ActorID          uint64
}

// ChannelUpdateInput 是更新渠道的参数：指针 / nil 的字段表示“不改”。插件本身不能换（要换插件请新建渠道），只能切版本。
type ChannelUpdateInput struct {
	Key              string
	Name             *string
	PluginVersion    *string
	BaseURL          *string
	TrustedInternal  *bool
	AllowCredentials *bool
	Settings         map[string]any      // nil 表示不改；非 nil（含空对象）表示整体替换
	RateLimit        *provider.RateLimit // nil 表示不改
	Enabled          *bool
	ActorID          uint64
}

// ChannelImportResult 是从渠道导入模型的结果：草稿只用来预填模型编辑器，不落库。
type ChannelImportResult struct {
	Drafts []provider.ModelDraft `json:"drafts"`
}

// List 返回所有渠道（按 key 升序），带 secret_set 与固定的插件版本号。返回的切片永远不是 nil。
func (s *AIChannelService) List(ctx context.Context) ([]ChannelView, error) {
	// 1. 渠道与全部版本（只取版本号用）
	chans, err := s.repo.ListChannels(ctx)
	if err != nil {
		return nil, err
	}
	vers, err := s.plugins.ListVersions(ctx, "")
	if err != nil {
		return nil, err
	}
	names := make(map[uint64]string, len(vers))
	for _, v := range vers {
		names[v.ID] = v.Version
	}
	// 2. 逐个组装视图；渠道数量很少，逐个查 Key 是否已设置足够
	out := make([]ChannelView, 0, len(chans))
	for i := range chans {
		view, err := s.view(ctx, &chans[i], names[chans[i].PluginVersionID])
		if err != nil {
			return nil, err
		}
		out = append(out, *view)
	}
	return out, nil
}

// Get 返回渠道详情。渠道不存在返回 ErrChannelNotFound。
func (s *AIChannelService) Get(ctx context.Context, key string) (*ChannelView, error) {
	ch, err := s.load(ctx, key)
	if err != nil {
		return nil, err
	}
	version := ""
	if v, err := s.plugins.GetVersionHead(ctx, ch.PluginVersionID); err == nil {
		version = v.Version
	} else if !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	return s.view(ctx, ch, version)
}

// Create 新建渠道。所有校验不通过都返回 ErrChannelInvalid（原因写进 msg），key 重复返回 ErrChannelExists。
// trusted_internal / allow_credentials 一开始就是 true 的，各补一条审计（这两个开关是安全边界，创建时打开同样要留痕）。
func (s *AIChannelService) Create(ctx context.Context, in ChannelCreateInput) (*ChannelView, error) {
	// 1. 基础字段
	var problems []string
	if !channelKeyRe.MatchString(in.Key) {
		problems = append(problems, "key 只能包含小写字母、数字和短横线，以字母或数字开头，长度不超过 64")
	}
	name, msg := checkChannelName(in.Name)
	problems = appendIf(problems, msg)
	baseURL, msg := checkBaseURL(in.BaseURL)
	problems = appendIf(problems, msg)
	problems = appendIf(problems, checkRateLimit(in.RateLimit))
	// 2. 插件版本存在且插件启用；之后 settings 才能按它的声明校验
	target, msg, err := s.resolveVersion(ctx, in.PluginKey, in.PluginVersion, true)
	if err != nil {
		return nil, err
	}
	problems = appendIf(problems, msg)
	var settings map[string]any
	if target != nil {
		var issues []modelcfg.Issue
		settings, issues = pluginmeta.ValidateSettingValues(target.meta.ChannelSettings, in.Settings)
		problems = append(problems, formatSettingIssues("settings", issues)...)
	}
	if len(problems) > 0 {
		return nil, invalidChannel(problems)
	}
	// 3. 写库；key 重复由唯一索引兜底
	settingsJSON, err := json.Marshal(settings)
	if err != nil {
		return nil, fmt.Errorf("编码渠道 settings 失败：%w", err)
	}
	rateJSON, err := json.Marshal(in.RateLimit)
	if err != nil {
		return nil, fmt.Errorf("编码渠道 rate_limit 失败：%w", err)
	}
	ch := &model.AIChannel{
		Key: in.Key, Name: name, PluginKey: in.PluginKey, PluginVersionID: target.version.ID, BaseURL: baseURL,
		TrustedInternal: in.TrustedInternal, AllowCredentials: in.AllowCredentials,
		SettingsJSON: model.JSONText(settingsJSON), RateLimitJSON: model.JSONText(rateJSON),
		Enabled: in.Enabled, UpdatedBy: in.ActorID,
	}
	if err := s.repo.CreateChannel(ctx, ch); errors.Is(err, repository.ErrDuplicate) {
		return nil, errcode.ErrChannelExists
	} else if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrChannelInvalid.WithMsg("插件版本已被删除，请刷新后重试")
	} else if err != nil {
		return nil, err
	}
	// 4. 审计与热生效
	aiAudit(ctx, s.audit, in.ActorID, model.AuditChannelCreate, model.AuditTargetChannel, ch.Key, map[string]any{
		"plugin_key": ch.PluginKey, "plugin_version": target.version.Version, "enabled": ch.Enabled,
	})
	if ch.TrustedInternal {
		aiAudit(ctx, s.audit, in.ActorID, model.AuditChannelTrusted, model.AuditTargetChannel, ch.Key, map[string]any{"from": false, "to": true})
	}
	if ch.AllowCredentials {
		aiAudit(ctx, s.audit, in.ActorID, model.AuditChannelCred, model.AuditTargetChannel, ch.Key, map[string]any{"from": false, "to": true})
	}
	s.notifyChanged(ctx, "channel create "+ch.Key)
	return s.view(ctx, ch, target.version.Version)
}

// load 按 key 取渠道，不存在翻译成 ErrChannelNotFound。
func (s *AIChannelService) load(ctx context.Context, key string) (*model.AIChannel, error) {
	ch, err := s.repo.GetChannel(ctx, key)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrChannelNotFound
	}
	return ch, err
}

// view 由渠道行组装视图；version 是它固定的插件版本号。settings / rate_limit 正文损坏时按空值输出，不让一行坏数据拖垮列表。
func (s *AIChannelService) view(ctx context.Context, ch *model.AIChannel, version string) (*ChannelView, error) {
	set, err := s.secrets.SecretIsSet(ctx, model.ChannelSecretName(ch.Key))
	if err != nil {
		return nil, err
	}
	v := &ChannelView{
		Key: ch.Key, Name: ch.Name, PluginKey: ch.PluginKey, PluginVersionID: ch.PluginVersionID, PluginVersion: version,
		BaseURL: ch.BaseURL, TrustedInternal: ch.TrustedInternal, AllowCredentials: ch.AllowCredentials,
		Settings: decodeObject(ch.SettingsJSON), Enabled: ch.Enabled, SecretSet: set,
		UpdatedBy: ch.UpdatedBy, UpdatedAt: ch.UpdatedAt, CreatedAt: ch.CreatedAt,
	}
	if len(ch.RateLimitJSON) > 0 {
		_ = json.Unmarshal(ch.RateLimitJSON, &v.RateLimit) // 损坏按“不限”输出
	}
	return v, nil
}

// notifyChanged 让 Registry 对齐；notifier 为空（测试）时什么也不做。
func (s *AIChannelService) notifyChanged(ctx context.Context, reason string) {
	if s.notifier != nil {
		s.notifier.NotifyChanged(ctx, reason)
	}
}
