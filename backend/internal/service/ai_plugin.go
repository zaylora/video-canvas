package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/provider/pluginmeta"
	"video-canvas/internal/repository"
)

// AIPluginRepo 是插件服务的数据访问接口，由 repository.AIPluginRepository 实现，测试里用内存 fake 替换。
type AIPluginRepo interface {
	// SaveVersion 在一个事务里登记新版本（插件行不存在就创建）；(plugin_key, version) 重复返回 repository.ErrDuplicate。
	SaveVersion(ctx context.Context, p *model.AIPlugin, v *model.AIPluginVersion) error
	// GetPlugin 按 key 查询插件，不存在返回 repository.ErrNotFound。
	GetPlugin(ctx context.Context, key string) (*model.AIPlugin, error)
	// ListPlugins 返回所有插件，按 key 升序。
	ListPlugins(ctx context.Context) ([]model.AIPlugin, error)
	// ListVersions 返回版本头信息（不含代码），新到旧；pluginKey 为空表示所有插件。
	ListVersions(ctx context.Context, pluginKey string) ([]model.AIPluginVersion, error)
	// FindVersion 按 (plugin_key, version) 查询版本头信息，不存在返回 repository.ErrNotFound。
	FindVersion(ctx context.Context, pluginKey, version string) (*model.AIPluginVersion, error)
	// SetPluginEnabled 启用 / 停用插件，不存在返回 repository.ErrNotFound。
	SetPluginEnabled(ctx context.Context, key string, enabled bool) error
	// CountVersionRefs 统计版本被渠道与非终态任务引用的情况，版本不存在返回 repository.ErrNotFound。
	CountVersionRefs(ctx context.Context, id uint64) (repository.VersionRefs, error)
	// DeleteVersion 事务内重新统计引用后删除版本：仍被引用返回 repository.ErrInUse，不存在返回 repository.ErrNotFound。
	DeleteVersion(ctx context.Context, id uint64) error
}

// AIChannelLister 只用来统计每个插件版本被多少个渠道固定（PluginView.versions[].channel_count），由 AIChannelRepository 实现。
type AIChannelLister interface {
	// ListChannels 返回所有渠道。
	ListChannels(ctx context.Context) ([]model.AIChannel, error)
}

// AIPluginService 管理协议插件：上传（预检 + 登记不可变版本）、启停、删除版本、启动时登记内置插件。
// 写操作只有 super_admin 能调（由路由的中间件保证），并写审计日志。
type AIPluginService struct {
	repo     AIPluginRepo
	channels AIChannelLister
	audit    AIAuditWriter
	checker  provider.Prechecker
	notifier RegistryNotifier
}

// NewAIPluginService 创建插件服务。notifier 为 nil 时不刷新 Registry（只有测试会这样）。
func NewAIPluginService(repo AIPluginRepo, channels AIChannelLister, audit AIAuditWriter, checker provider.Prechecker, notifier RegistryNotifier) *AIPluginService {
	return &AIPluginService{repo: repo, channels: channels, audit: audit, checker: checker, notifier: notifier}
}

// PluginView 是管理端的插件视图：插件行 + 全部版本（新到旧）。
type PluginView struct {
	Key       string              `json:"key"`
	Name      string              `json:"name"`
	Source    string              `json:"source"` // builtin / uploaded
	Enabled   bool                `json:"enabled"`
	UpdatedAt time.Time           `json:"updated_at"`
	Versions  []PluginVersionView `json:"versions"`
}

// PluginVersionView 是一个插件版本（不含代码）。
type PluginVersionView struct {
	ID           uint64           `json:"id"`
	PluginKey    string           `json:"plugin_key"`
	Version      string           `json:"version"`
	SHA256       string           `json:"sha256"`
	CreatedAt    time.Time        `json:"created_at"`
	CreatedBy    uint64           `json:"created_by"`
	ChannelCount int              `json:"channel_count"` // 固定在这个版本上的渠道数
	Meta         *pluginmeta.Meta `json:"meta"`          // 预检时读出的 meta；库里的 meta_json 损坏时为 null
}

// UploadResult 是上传插件的结果：预检不通过也是正常结果（HTTP 200），问题列表精确到字段。
type UploadResult struct {
	Accepted bool               `json:"accepted"`
	Issues   []modelcfg.Issue   `json:"issues"`
	Version  *PluginVersionView `json:"version"` // 通过时是新登记的版本，否则为 null
}

// BuiltinRegistration 是一个内置插件的登记结果。
type BuiltinRegistration struct {
	Key        string
	Version    string
	Registered bool // true 表示本次新登记；false 表示该版本早已登记（且内容一致）
}

// aiIssuePathVersion 是“版本号已存在”这类问题的 JSON 路径。
const aiIssuePathVersion = "meta.version"

// List 返回所有插件与它们的版本（新到旧），带每个版本被渠道固定的数量。返回的切片永远不是 nil。
func (s *AIPluginService) List(ctx context.Context) ([]PluginView, error) {
	// 1. 插件行、全部版本头信息（不含代码）、渠道（只用来数每个版本被几个渠道固定）
	plugins, err := s.repo.ListPlugins(ctx)
	if err != nil {
		return nil, err
	}
	versions, err := s.repo.ListVersions(ctx, "")
	if err != nil {
		return nil, err
	}
	chans, err := s.channels.ListChannels(ctx)
	if err != nil {
		return nil, err
	}
	perVersion := make(map[uint64]int, len(chans))
	for _, ch := range chans {
		perVersion[ch.PluginVersionID]++
	}
	// 2. 版本按插件归并；ListVersions 已经是新到旧，归并时保持顺序
	byPlugin := make(map[string][]PluginVersionView, len(plugins))
	for i := range versions {
		v := &versions[i]
		byPlugin[v.PluginKey] = append(byPlugin[v.PluginKey], pluginVersionView(v, perVersion[v.ID]))
	}
	out := make([]PluginView, 0, len(plugins))
	for _, p := range plugins {
		vs := byPlugin[p.Key]
		if vs == nil {
			vs = []PluginVersionView{}
		}
		out = append(out, PluginView{Key: p.Key, Name: p.Name, Source: p.Source, Enabled: p.Enabled, UpdatedAt: p.UpdatedAt, Versions: vs})
	}
	return out, nil
}

// Upload 上传一个插件文件：预检 → 版本号查重 → 登记为不可变版本（记 sha256）→ 写审计。
// 预检不通过、版本号已存在都不是 HTTP 错误，而是 Accepted=false 加问题列表（前端要把问题精确标在字段上）；
// 文件超限、runner 不可用、往内置插件下上传才是错误。
func (s *AIPluginService) Upload(ctx context.Context, actorID uint64, code []byte) (*UploadResult, error) {
	// 1. 大小检查放在预检之前：超限的文件不该白白送进 runner
	if len(code) == 0 {
		return nil, errcode.ErrInvalidParams.WithMsg("插件文件不能为空")
	}
	if len(code) > pluginmeta.MaxPluginBytes {
		return nil, errcode.ErrPluginTooLarge.WithMsg(fmt.Sprintf("插件文件超过 %d KB 的大小限制", pluginmeta.MaxPluginBytes>>10))
	}
	// 2. 预检：在沙箱里编译执行并按契约检查。runner 连不上返回 503，让运维稍后重试
	res, err := s.checker.Precheck(ctx, string(code))
	if provider.CodeOf(err) == provider.CodeRunnerUnavailable {
		return nil, errcode.ErrRunnerUnavailable
	}
	if err != nil {
		return nil, fmt.Errorf("插件预检失败：%w", err)
	}
	if !res.OK {
		return rejected(res.Issues), nil
	}
	meta, err := pluginmeta.Parse(res.Meta)
	if err != nil {
		return nil, fmt.Errorf("预检通过但 meta 无法解析：%w", err)
	}
	// 3. 内置插件的 key 不能被上传的插件占用：内置版本随发版登记，上传方不能往里加版本、更不能覆盖
	existing, err := s.repo.GetPlugin(ctx, meta.Key)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	if err == nil && existing.Source == model.PluginSourceBuiltin {
		return nil, errcode.ErrPluginBuiltin.WithMsg(fmt.Sprintf("%q 是内置插件，不能上传同名插件", meta.Key))
	}
	// 4. 版本号查重（版本不可变）：已存在报在 meta.version 上，要求改版本号
	if _, err := s.repo.FindVersion(ctx, meta.Key, meta.Version); err == nil {
		return rejected(versionDupIssues(meta)), nil
	} else if !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	// 5. 登记。sha256 用服务端对原文件自己算的值，不信任 runner 回传（它只用于展示）；
	//    并发上传同一版本时唯一索引兜底，同样按“版本号已存在”处理
	ver := &model.AIPluginVersion{
		PluginKey: meta.Key, Version: meta.Version, SHA256: codeSHA256(code), Code: string(code),
		MetaJSON: model.JSONText(res.Meta), CreatedBy: actorID,
	}
	plugin := &model.AIPlugin{Key: meta.Key, Name: meta.Name, Source: model.PluginSourceUploaded, Enabled: true}
	if err := s.repo.SaveVersion(ctx, plugin, ver); errors.Is(err, repository.ErrDuplicate) {
		return rejected(versionDupIssues(meta)), nil
	} else if err != nil {
		return nil, err
	}
	// 6. 审计：操作人、插件、版本、sha256（代码本身不进审计）
	aiAudit(ctx, s.audit, actorID, model.AuditPluginUpload, model.AuditTargetPluginVer, meta.Key,
		map[string]any{"version": meta.Version, "sha256": ver.SHA256})
	view := pluginVersionView(ver, 0)
	return &UploadResult{Accepted: true, Issues: []modelcfg.Issue{}, Version: &view}, nil
}

// rejected 组装“预检不通过”的上传结果，问题列表永远是数组。
func rejected(issues []modelcfg.Issue) *UploadResult {
	if issues == nil {
		issues = []modelcfg.Issue{}
	}
	return &UploadResult{Accepted: false, Issues: issues}
}

// versionDupIssues 是版本号已存在的问题列表。
func versionDupIssues(meta *pluginmeta.Meta) []modelcfg.Issue {
	return []modelcfg.Issue{{Path: aiIssuePathVersion, Message: fmt.Sprintf("插件 %s 的版本 %s 已存在（版本不可变），请修改 meta.version", meta.Key, meta.Version)}}
}

// SetEnabled 启用 / 停用插件：停用后所有使用它的渠道不再接新任务（进行中的任务按快照继续）。
func (s *AIPluginService) SetEnabled(ctx context.Context, actorID uint64, key string, enabled bool) error {
	// 1. 写库；插件不存在返回 404
	if err := s.repo.SetPluginEnabled(ctx, key, enabled); errors.Is(err, repository.ErrNotFound) {
		return errcode.ErrPluginNotFound
	} else if err != nil {
		return err
	}
	// 2. 审计并热生效：Registry 里“插件是否启用”是加载时算好的，必须刷新
	aiAudit(ctx, s.audit, actorID, model.AuditPluginEnable, model.AuditTargetPlugin, key, map[string]any{"enabled": enabled})
	s.notifyChanged(ctx, "plugin enabled "+key)
	return nil
}

// DeleteVersion 删除一个插件版本。内置插件不能删；仍被渠道固定或被非终态任务的快照引用的版本不能删（409）。
// 删掉插件的最后一个版本后，插件行由仓储一并删除。
func (s *AIPluginService) DeleteVersion(ctx context.Context, actorID uint64, key, version string) error {
	// 1. 插件与版本必须存在；内置插件一律拒绝（它们随发版登记，删了下次启动又会回来，只会造成困惑）
	p, err := s.repo.GetPlugin(ctx, key)
	if errors.Is(err, repository.ErrNotFound) {
		return errcode.ErrPluginNotFound
	}
	if err != nil {
		return err
	}
	if p.Source == model.PluginSourceBuiltin {
		return errcode.ErrPluginBuiltin
	}
	ver, err := s.repo.FindVersion(ctx, key, version)
	if errors.Is(err, repository.ErrNotFound) {
		return errcode.ErrPluginNotFound.WithMsg(fmt.Sprintf("插件 %s 没有版本 %s", key, version))
	}
	if err != nil {
		return err
	}
	// 2. 先查引用给出清楚的原因；真正的删除在仓储事务里锁住版本行后重新统计，这里的结果可能过期，所以只是提示
	refs, err := s.repo.CountVersionRefs(ctx, ver.ID)
	if errors.Is(err, repository.ErrNotFound) {
		return errcode.ErrPluginNotFound.WithMsg(fmt.Sprintf("插件 %s 没有版本 %s", key, version))
	}
	if err != nil {
		return err
	}
	if refs.Channels > 0 || refs.ActiveTasks > 0 {
		return errcode.ErrPluginInUse.WithMsg(fmt.Sprintf("版本 %s@%s 被 %d 个渠道、%d 个进行中的任务使用，无法删除",
			key, version, refs.Channels, refs.ActiveTasks))
	}
	// 3. 删除；并发情况下被新渠道 / 新任务引用时仓储返回 ErrInUse
	if err := s.repo.DeleteVersion(ctx, ver.ID); errors.Is(err, repository.ErrInUse) {
		return errcode.ErrPluginInUse
	} else if errors.Is(err, repository.ErrNotFound) {
		return errcode.ErrPluginNotFound.WithMsg(fmt.Sprintf("插件 %s 没有版本 %s", key, version))
	} else if err != nil {
		return err
	}
	// 4. 审计（记被删版本的 sha256，出问题时能对上号）
	aiAudit(ctx, s.audit, actorID, model.AuditPluginDelete, model.AuditTargetPluginVer, key,
		map[string]any{"version": version, "sha256": ver.SHA256})
	return nil
}

// RegisterBuiltin 把内置插件的源码按 key + version 登记为 builtin 来源（启动时调用，幂等）。
// 每个插件独立处理，一个失败不影响其他，全部错误合并后返回；调用方（启动流程）记日志。
//   - 版本已登记且 sha256 一致：什么都不做；
//   - 版本已登记但 sha256 不一致：说明改了内置插件代码却没升版本号。版本不可变，保持库里的旧代码并报错提醒开发者升版本号；
//   - key 已被上传的插件占用：报错，不覆盖（运维需要先处理掉那个同名插件）。
func (s *AIPluginService) RegisterBuiltin(ctx context.Context, sources [][]byte) ([]BuiltinRegistration, error) {
	var (
		out  []BuiltinRegistration
		errs []error
	)
	for _, code := range sources {
		reg, err := s.registerOneBuiltin(ctx, code)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, *reg)
	}
	return out, errors.Join(errs...)
}

// registerOneBuiltin 登记一个内置插件。
func (s *AIPluginService) registerOneBuiltin(ctx context.Context, code []byte) (*BuiltinRegistration, error) {
	// 1. 预检拿到 meta：内置插件也走同一套预检，代码写错在启动时就暴露
	res, err := s.checker.Precheck(ctx, string(code))
	if err != nil {
		return nil, fmt.Errorf("内置插件预检失败：%w", err)
	}
	if !res.OK {
		return nil, fmt.Errorf("内置插件预检未通过：%s", aiFormatIssues(res.Issues))
	}
	meta, err := pluginmeta.Parse(res.Meta)
	if err != nil {
		return nil, fmt.Errorf("内置插件 meta 无法解析：%w", err)
	}
	reg := &BuiltinRegistration{Key: meta.Key, Version: meta.Version}
	sum := codeSHA256(code)

	// 2. key 不能被上传来源的插件占用
	existing, err := s.repo.GetPlugin(ctx, meta.Key)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	if err == nil && existing.Source != model.PluginSourceBuiltin {
		return nil, fmt.Errorf("内置插件 %s@%s 无法登记：key %q 已被上传的插件占用", meta.Key, meta.Version, meta.Key)
	}
	// 3. 版本已登记：核对内容，一致就是幂等的空操作
	if ver, err := s.repo.FindVersion(ctx, meta.Key, meta.Version); err == nil {
		if ver.SHA256 != sum {
			return nil, fmt.Errorf("内置插件 %s@%s 的代码与已登记的版本不一致（sha256 %s ≠ %s）：版本不可变，请升级 meta.version",
				meta.Key, meta.Version, shortSHA(sum), shortSHA(ver.SHA256))
		}
		return reg, nil
	} else if !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	// 4. 登记为 builtin；多实例同时启动时另一个实例先登记成功，唯一索引冲突按“已登记”处理
	ver := &model.AIPluginVersion{
		PluginKey: meta.Key, Version: meta.Version, SHA256: sum, Code: string(code), MetaJSON: model.JSONText(res.Meta), CreatedBy: 0,
	}
	plugin := &model.AIPlugin{Key: meta.Key, Name: meta.Name, Source: model.PluginSourceBuiltin, Enabled: true}
	if err := s.repo.SaveVersion(ctx, plugin, ver); errors.Is(err, repository.ErrDuplicate) {
		return reg, nil
	} else if err != nil {
		return nil, err
	}
	aiAudit(ctx, s.audit, 0, model.AuditPluginUpload, model.AuditTargetPluginVer, meta.Key,
		map[string]any{"version": meta.Version, "sha256": sum, "source": model.PluginSourceBuiltin})
	reg.Registered = true
	return reg, nil
}

// notifyChanged 让 Registry 对齐；notifier 为空（测试）时什么也不做。
func (s *AIPluginService) notifyChanged(ctx context.Context, reason string) {
	if s.notifier != nil {
		s.notifier.NotifyChanged(ctx, reason)
	}
}

// pluginVersionView 由版本行（含 meta_json）组装视图；meta 解析失败输出 null，不让一个损坏的版本拖垮整个列表。
func pluginVersionView(v *model.AIPluginVersion, channelCount int) PluginVersionView {
	view := PluginVersionView{
		ID: v.ID, PluginKey: v.PluginKey, Version: v.Version, SHA256: v.SHA256,
		CreatedAt: v.CreatedAt, CreatedBy: v.CreatedBy, ChannelCount: channelCount,
	}
	if meta, err := pluginmeta.Parse(v.MetaJSON); err == nil {
		view.Meta = meta
	}
	return view
}

// codeSHA256 返回代码的十六进制 sha256。
func codeSHA256(code []byte) string {
	sum := sha256.Sum256(code)
	return hex.EncodeToString(sum[:])
}

// shortSHA 取哈希的前 12 位用于错误提示。
func shortSHA(sum string) string {
	if len(sum) > 12 {
		return sum[:12]
	}
	return sum
}
