package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	// CountPluginRefs 统计插件全部版本被渠道与非终态任务引用的情况，插件不存在返回 repository.ErrNotFound。
	CountPluginRefs(ctx context.Context, key string) (repository.PluginRefs, error)
	// DeletePlugin 事务内按“插件行 → 版本行”加锁、重新统计引用后删除全部版本与插件行：
	// 仍被引用时返回当时的引用与 repository.ErrInUse，不存在返回 repository.ErrNotFound。
	DeletePlugin(ctx context.Context, key string) (repository.PluginRefs, error)
}

// AIChannelLister 只用来统计每个插件版本被多少个渠道固定（PluginView.versions[].channel_count），由 AIChannelRepository 实现。
type AIChannelLister interface {
	// ListChannels 返回所有渠道。
	ListChannels(ctx context.Context) ([]model.AIChannel, error)
}

// AIPluginService 管理协议插件：上传（预检 + 登记不可变版本）、启停、删除版本 / 整个插件、启动时登记内置插件。
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
	// VersionAdjusted 只在登记的版本号和代码里写的不一样时才有（没写、不合法、已被占用、比已有版本低）；没有调整时不返回。
	VersionAdjusted *VersionAdjusted `json:"version_adjusted,omitempty"`
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

// Upload 上传一个插件文件：预检 → 决定版本号 → 登记为不可变版本（记 sha256）→ 写审计。
// 预检不通过、代码与已有版本完全相同都不是 HTTP 错误，而是 Accepted=false 加问题列表（前端要把问题精确标在字段上）；
// 文件超限、runner 不可用、往内置插件下上传才是错误。版本号由服务端决定（见本文件 decideVersion）：代码里的能用就用，
// 不能用就接着最高版本往上加，所以改了代码不用再手动升版本号。
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
	// 4. 决定版本号并登记。sha256 用服务端对原文件自己算的值，不信任 runner 回传（它只用于展示）；
	//    并发上传撞上唯一索引时，重新读已有版本再决定一次，最多 maxVersionAttempts 次
	sum := codeSHA256(code)
	for attempt := 0; attempt < maxVersionAttempts; attempt++ {
		versions, err := s.repo.ListVersions(ctx, meta.Key)
		if err != nil {
			return nil, err
		}
		decision, err := decideVersion(meta.Version, sum, versions)
		if err != nil {
			return rejected([]modelcfg.Issue{{Path: aiIssuePathVersion, Message: "无法自动决定版本号：" + err.Error()}}), nil
		}
		if decision.reject != "" {
			return rejected([]modelcfg.Issue{{Path: aiIssuePathMeta, Message: decision.reject}}), nil
		}
		metaJSON := res.Meta
		if decision.version != meta.Version {
			// meta_json 里的 version 要和登记的版本号一致；代码原文和 sha256 不动
			if metaJSON, err = metaWithVersion(res.Meta, decision.version); err != nil {
				return nil, fmt.Errorf("回写版本号失败：%w", err)
			}
		}
		ver := &model.AIPluginVersion{
			PluginKey: meta.Key, Version: decision.version, SHA256: sum, Code: string(code),
			MetaJSON: model.JSONText(metaJSON), CreatedBy: actorID,
		}
		plugin := &model.AIPlugin{Key: meta.Key, Name: meta.Name, Source: model.PluginSourceUploaded, Enabled: true}
		if err := s.repo.SaveVersion(ctx, plugin, ver); errors.Is(err, repository.ErrDuplicate) {
			if attempt == maxVersionAttempts-1 {
				return rejected(versionDupIssuesFor(meta, decision.version)), nil
			}
			continue
		} else if err != nil {
			return nil, err
		}
		// 5. 审计：操作人、插件、版本、sha256（代码本身不进审计）
		aiAudit(ctx, s.audit, actorID, model.AuditPluginUpload, model.AuditTargetPluginVer, meta.Key,
			map[string]any{"version": ver.Version, "sha256": ver.SHA256})
		view := pluginVersionView(ver, 0)
		out := &UploadResult{Accepted: true, Issues: []modelcfg.Issue{}, Version: &view}
		if decision.version != meta.Version {
			out.VersionAdjusted = &VersionAdjusted{From: declaredVersion(meta.Version), To: decision.version}
		}
		return out, nil
	}
	return rejected(versionDupIssuesFor(meta, meta.Version)), nil // 不可达：循环里最后一次一定返回
}

// declaredVersion 返回“代码里声明的版本号”：不合法的写法（如 v1.0、1.0）视为没写，返回空串。
func declaredVersion(v string) string {
	if pluginmeta.IsValidVersion(v) {
		return v
	}
	return ""
}

// rejected 组装“预检不通过”的上传结果，问题列表永远是数组。
func rejected(issues []modelcfg.Issue) *UploadResult {
	if issues == nil {
		issues = []modelcfg.Issue{}
	}
	return &UploadResult{Accepted: false, Issues: issues}
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
	// 内置插件的版本号由发版决定，不会自动补：必须写合法的 semver
	if !pluginmeta.IsValidVersion(meta.Version) {
		return nil, fmt.Errorf("内置插件 %s 必须在 meta.version 里写合法的 semver 版本号（如 1.2.3），实际是 %q", meta.Key, meta.Version)
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

// 以下是上传插件时版本号的决定（docs/design/插件简化/插件简化.md §6.5）。
//
// 版本不可变，所以每改一次代码都得换一个版本号；让人（或 AI）手动改既容易忘，又要多一轮来回。
// 现在由服务端决定：代码里的版本号可以用就用，不能用就接着已有的最高版本往上加。
// 只对上传的插件生效；内置插件的版本号由发版决定，登记时必须是合法的 semver（见 registerOneBuiltin）。

// maxVersionAttempts 是并发上传撞上唯一索引时，重新决定版本号再保存的最多次数。
const maxVersionAttempts = 3

// aiIssuePathMeta 是“代码与已有版本完全相同”这类整体问题的 JSON 路径。
const aiIssuePathMeta = "meta"

// VersionAdjusted 说明登记的版本号和代码里写的不一样。From 是代码里写的版本号，没写或不合法为空串。
type VersionAdjusted struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// versionDecision 是一次版本号决定的结果：要么是最终版本号，要么是拒绝的原因。
type versionDecision struct {
	version string
	reject  string
}

// decideVersion 决定上传的代码登记成哪个版本号。declared 是代码里写的版本号（没写为空串），
// existing 是这个插件已登记的版本（可以为空），sha 是这份代码的 sha256。按顺序判断：
//  1. 还没有任何版本：代码里的版本号合法就用它，否则用 1.0.0；
//  2. 代码和某个已有版本完全相同：拒绝，没有产生新版本；
//  3. 代码里的版本号合法、没被占用、并且比已有的最高版本高：用它；
//  4. 其他情况：取已有的最高版本，补丁位加 1（预发布后缀去掉）。
func decideVersion(declared, sha string, existing []model.AIPluginVersion) (versionDecision, error) {
	if len(existing) == 0 {
		if pluginmeta.IsValidVersion(declared) {
			return versionDecision{version: declared}, nil
		}
		return versionDecision{version: "1.0.0"}, nil
	}
	taken := make([]string, 0, len(existing))
	for i := range existing {
		if existing[i].SHA256 == sha {
			return versionDecision{reject: fmt.Sprintf("代码与 v%s 完全相同，没有产生新版本", existing[i].Version)}, nil
		}
		taken = append(taken, existing[i].Version)
	}
	highest := pluginmeta.HighestVersion(taken)
	if pluginmeta.IsValidVersion(declared) && !containsString(taken, declared) &&
		(highest == "" || pluginmeta.CompareVersions(declared, highest) > 0) {
		return versionDecision{version: declared}, nil
	}
	if highest == "" {
		// 库里一个合法的版本号都没有（不该发生）：当作还没有版本
		if pluginmeta.IsValidVersion(declared) && !containsString(taken, declared) {
			return versionDecision{version: declared}, nil
		}
		return versionDecision{version: "1.0.0"}, nil
	}
	next, err := pluginmeta.NextPatchVersion(highest)
	if err != nil {
		return versionDecision{}, err
	}
	return versionDecision{version: next}, nil
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// metaWithVersion 把 meta JSON 里的 version 换成最终登记的版本号，其余字段原样保留。
// channelSettings 等有序对象的内部顺序不受影响：这里只把顶层字段当作原始 JSON 搬运，不解开嵌套内容。
func metaWithVersion(raw []byte, version string) ([]byte, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return nil, fmt.Errorf("meta 不是 JSON 对象：%w", err)
	}
	v, err := json.Marshal(version)
	if err != nil {
		return nil, err
	}
	obj["version"] = v
	return json.Marshal(obj)
}

// versionDupIssuesFor 是连续几次都撞上同一个版本号时的问题列表。
func versionDupIssuesFor(meta *pluginmeta.Meta, version string) []modelcfg.Issue {
	return []modelcfg.Issue{{Path: aiIssuePathVersion, Message: fmt.Sprintf("插件 %s 的版本 %s 已存在（版本不可变），请稍后重试或修改 meta.version", meta.Key, version)}}
}
