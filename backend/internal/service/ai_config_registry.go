package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"go.uber.org/zap"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/repository"
)

// aiRegModel 是 Registry 里的一个已发布模型：指针行的运行时开关 + 已解析的发布版本 + 它的渠道运行时。
type aiRegModel struct {
	key         string
	enabled     bool
	sort        int
	channelSort int // 所属渠道的顺序，决定用户看到的模型清单里渠道的先后
	revisionID  uint64
	cfg         *modelcfg.ModelConfig
	rt          *provider.ChannelRuntime // 渠道 + 模型固定的插件版本 + 协议设置；nil 表示渠道 / 插件当前不可用
}

// available 判断模型当前能否接新任务：已上架，且渠道、插件都可用。
func (m *aiRegModel) available() bool { return m.enabled && m.rt != nil }

// aiRegistryState 是一次完整加载得到的只读内存状态，替换时整体换指针，读方无需加锁遍历。
type aiRegistryState struct {
	loadedAt time.Time
	models   map[string]*aiRegModel
	ordered  []*aiRegModel // 按 sort、key 升序，ListModels 用
}

// ListModels 实现 provider.Registry：返回已发布、已上架且渠道与插件可用的模型（按 sort 升序），只含面向画布的公开字段，
// 不含 params / 渠道 / 插件细节。kind 为空表示全部。返回的切片永远不是 nil，JSON 输出 [] 而不是 null。
func (s *AIConfigService) ListModels(ctx context.Context, kind string) ([]provider.ModelInfo, error) {
	// 1. 取内存状态（过期或被标记失效时会重新加载）
	st, err := s.registryState(ctx)
	if err != nil {
		return nil, err
	}
	// 2. 过滤并转成公开结构
	out := make([]provider.ModelInfo, 0, len(st.ordered))
	for _, m := range st.ordered {
		if !m.available() || (kind != "" && m.cfg.Kind != kind) {
			continue
		}
		out = append(out, provider.ModelInfo{
			Key: m.key, Kind: m.cfg.Kind, Label: m.cfg.Label, Hint: m.cfg.Hint,
			Vendor: m.cfg.Vendor, Tags: nonNilTags(m.cfg.Tags),
			Pricing: m.cfg.Pricing.Public(), Capabilities: m.cfg.Capabilities.Public(),
		})
	}
	return out, nil
}

// Snapshot 实现 provider.Registry：冻结模型当前配置 + 渠道当前配置 + 模型固定的插件版本与协议设置。
// 模型不存在、未发布、已下架，或渠道 / 插件停用、渠道行或插件版本缺失都返回 provider.ErrModelUnavailable。
// 返回的快照与缓存共享内部的 map / slice（params、settings、input_schema），调用方（宿主、任务服务）只能读、不能改。
func (s *AIConfigService) Snapshot(ctx context.Context, modelKey string) (*provider.Snapshot, error) {
	// 1. 取内存状态；加载失败（且没有旧状态）属于基础设施故障，原样返回，不伪装成“模型不可用”
	st, err := s.registryState(ctx)
	if err != nil {
		return nil, err
	}
	// 2. 可用性判定在加载时已算好；指针行的 enabled 是运行时真值，正文里的 enabled 只是初始值
	m, ok := st.models[modelKey]
	if !ok || !m.available() {
		return nil, provider.ErrModelUnavailable
	}
	return provider.NewSnapshot(m.cfg, m.rt, m.revisionID), nil
}

// RefreshRegistry 强制从数据库重新加载已发布模型、渠道与插件版本。发布 / 回滚 / 上下架后本实例立即调用；
// 插件与渠道的变更由它们的服务调用；多实例时其他实例收到失效广播后也调用它。并发调用是安全的（刷新互斥串行）。
func (s *AIConfigService) RefreshRegistry(ctx context.Context) error {
	_, err := s.reload(ctx, true)
	return err
}

// registryState 返回可用的内存状态：未过期直接用；过期或未加载则重新加载。
// 重新加载失败时如果还有旧状态就继续用旧状态（数据库抖动不该让模型清单整体消失），并记录告警。
func (s *AIConfigService) registryState(ctx context.Context) (*aiRegistryState, error) {
	s.regMu.RLock()
	st := s.regState
	s.regMu.RUnlock()
	if st != nil && s.now().Sub(st.loadedAt) < s.regTTL {
		return st, nil
	}
	fresh, err := s.reload(ctx, false)
	if err != nil {
		if st != nil {
			logger.Warn("重新加载 Registry 失败，继续使用旧状态", zap.Error(err))
			return st, nil
		}
		return nil, err
	}
	return fresh, nil
}

// reload 加载所有模型与可用的渠道运行时，并原子替换内存状态。
// force=false 时，拿到加载锁后会再确认一次别的 goroutine 是否已经刷新过，避免过期瞬间的并发风暴。
func (s *AIConfigService) reload(ctx context.Context, force bool) (*aiRegistryState, error) {
	s.regLoadMu.Lock()
	defer s.regLoadMu.Unlock()

	if !force {
		s.regMu.RLock()
		cur := s.regState
		s.regMu.RUnlock()
		if cur != nil && s.now().Sub(cur.loadedAt) < s.regTTL {
			return cur, nil
		}
	}

	rows, err := s.repo.LoadModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("加载模型失败：%w", err)
	}
	deps, err := s.loadRegDeps(ctx)
	if err != nil {
		return nil, err
	}
	st := s.buildState(rows, deps)

	s.regMu.Lock()
	s.regState = st
	s.regMu.Unlock()
	return st, nil
}

// aiRegDeps 是组装模型运行时要用的渠道与插件数据：只含“当前可用”的渠道，插件的启停，以及所有插件版本（按 key@version 查）。
type aiRegDeps struct {
	channels map[string]*model.AIChannel
	pluginOn map[string]bool
	versions map[string]*model.AIPluginVersion
}

// buildState 由模型配置与渠道 / 插件数据组装内存状态（调用方持有 regLoadMu）。
// 配置正文解析不过的模型不进内存：已启用的记一条错误日志（线上有模型用不了，需要处理），没启用的是还没填完的半成品（含“待配置”），静默跳过。
// 排序：先按渠道顺序（ai_channels.sort），再按渠道内顺序（模型 sort），最后按 key，用户看到的模型清单就是这个顺序。
func (s *AIConfigService) buildState(rows []repository.PublishedModel, deps *aiRegDeps) *aiRegistryState {
	st := &aiRegistryState{loadedAt: s.now(), models: map[string]*aiRegModel{}}
	for _, row := range rows {
		cfg, issues := modelcfg.ParseModel(row.Body)
		if len(issues) > 0 || cfg == nil {
			if row.Enabled {
				logger.Error("已启用的模型配置无法解析，已忽略", zap.String("model", row.Key),
					zap.Uint64("revision_id", row.RevisionID), zap.String("issues", aiFormatIssues(issues)))
			}
			continue
		}
		m := &aiRegModel{key: row.Key, enabled: row.Enabled, sort: row.Sort, revisionID: row.RevisionID, cfg: cfg,
			rt: modelRuntime(cfg, deps)}
		if ch := deps.channelOf(cfg); ch != nil {
			m.channelSort = ch.Sort
		}
		st.models[row.Key] = m
		st.ordered = append(st.ordered, m)
	}
	sort.SliceStable(st.ordered, func(i, j int) bool {
		a, b := st.ordered[i], st.ordered[j]
		if a.channelSort != b.channelSort {
			return a.channelSort < b.channelSort
		}
		if a.sort != b.sort {
			return a.sort < b.sort
		}
		return a.key < b.key
	})
	return st
}

// channelOf 取模型 channels[0] 的渠道行（只含启用的渠道）；没有返回 nil。
func (d *aiRegDeps) channelOf(cfg *modelcfg.ModelConfig) *model.AIChannel {
	if len(cfg.Channels) == 0 {
		return nil
	}
	return d.channels[cfg.Channels[0].Channel]
}

// modelRuntime 由模型的渠道、协议组装运行时：渠道启用；Agent 模型只要渠道；其他模型还要插件启用、固定的版本存在、
// 版本声明了模型的 kind、协议设置合法（会补上默认值）。任何一项不满足返回 nil（模型不可用）。
// 保存与启用时检查过这些，但之后渠道、插件可能被停用，版本可能被删，这里再兜一次底，避免任务提交后才失败。
func modelRuntime(cfg *modelcfg.ModelConfig, deps *aiRegDeps) *provider.ChannelRuntime {
	ch := deps.channelOf(cfg)
	if ch == nil {
		return nil
	}
	if cfg.Kind == modelcfg.KindAgent {
		rt, err := provider.AgentRuntimeOf(ch)
		if err != nil {
			logger.Error("渠道配置无法解析，渠道暂不可用", zap.String("channel", ch.Key), zap.Error(err))
			return nil
		}
		return rt
	}
	proto := cfg.Channels[0].Protocol
	if proto == nil || !deps.pluginOn[proto.Plugin] {
		return nil
	}
	ver := deps.versions[proto.Plugin+"@"+proto.Version]
	if ver == nil {
		logger.Warn("模型固定的插件版本不存在，模型暂不可用", zap.String("model", cfg.Key),
			zap.String("plugin", proto.Plugin), zap.String("version", proto.Version))
		return nil
	}
	rt, err := provider.RuntimeOf(ch, ver, nil)
	if err != nil {
		logger.Error("渠道配置或插件 meta 无法解析，模型暂不可用", zap.String("model", cfg.Key), zap.Error(err))
		return nil
	}
	if !rt.Plugin.Meta.SupportsKind(cfg.Kind) {
		logger.Warn("模型选的协议不支持模型的种类，模型暂不可用", zap.String("model", cfg.Key),
			zap.String("plugin", proto.Plugin), zap.String("kind", cfg.Kind))
		return nil
	}
	settings, issues := rt.ValidateSettings(proto.Settings)
	if len(issues) > 0 {
		logger.Warn("模型的协议设置不合法，模型暂不可用", zap.String("model", cfg.Key), zap.String("issues", aiFormatIssues(issues)))
		return nil
	}
	rt.Channel.Settings = settings
	return rt
}

// loadRegDeps 加载组装模型运行时要用的数据：启用的渠道、插件启停、所有插件版本。只有仓储故障才返回错误；
// 渠道停用、插件停用是正常状态，数据不一致（版本缺失）由 modelRuntime 逐模型告警。
func (s *AIConfigService) loadRegDeps(ctx context.Context) (*aiRegDeps, error) {
	chans, err := s.channels.ListChannels(ctx)
	if err != nil {
		return nil, fmt.Errorf("加载渠道失败：%w", err)
	}
	vers, err := s.plugins.ListVersions(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("加载插件版本失败：%w", err)
	}
	deps := &aiRegDeps{
		channels: make(map[string]*model.AIChannel, len(chans)),
		pluginOn: map[string]bool{},
		versions: make(map[string]*model.AIPluginVersion, len(vers)),
	}
	for i := range chans {
		if chans[i].Enabled {
			deps.channels[chans[i].Key] = &chans[i]
		}
	}
	for i := range vers {
		v := &vers[i]
		deps.versions[v.PluginKey+"@"+v.Version] = v
		if _, seen := deps.pluginOn[v.PluginKey]; seen {
			continue
		}
		p, err := s.plugins.GetPlugin(ctx, v.PluginKey)
		if errors.Is(err, repository.ErrNotFound) {
			deps.pluginOn[v.PluginKey] = false // 版本行还在但插件行不存在：按停用处理
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("加载插件 %s 失败：%w", v.PluginKey, err)
		}
		deps.pluginOn[v.PluginKey] = p.Enabled
	}
	return deps, nil
}

// nonNilTags 让没有标签的模型在 JSON 里输出 [] 而不是 null，前端不用判空。
func nonNilTags(tags []string) []string {
	if tags == nil {
		return []string{}
	}
	return tags
}
