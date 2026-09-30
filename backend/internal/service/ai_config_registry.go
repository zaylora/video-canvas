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
	key        string
	enabled    bool
	sort       int
	revisionID uint64
	cfg        *modelcfg.ModelConfig
	rt         *provider.ChannelRuntime // 渠道 + 固定的插件版本；nil 表示渠道 / 插件当前不可用
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
			Credits: m.cfg.Credits, InputSchema: m.cfg.InputSchema,
		})
	}
	return out, nil
}

// Snapshot 实现 provider.Registry：冻结模型当前发布版本 + 渠道当前配置 + 渠道固定的插件版本。
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

// reload 加载所有已发布模型与可用的渠道运行时，并原子替换内存状态。
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

	rows, err := s.repo.LoadPublishedModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("加载已发布模型失败：%w", err)
	}
	runtimes, err := s.loadRuntimes(ctx)
	if err != nil {
		return nil, err
	}
	st := s.buildState(rows, runtimes)

	s.regMu.Lock()
	s.regState = st
	s.regMu.Unlock()
	return st, nil
}

// buildState 由已发布模型与渠道运行时组装内存状态（调用方持有 regLoadMu）。
// 解析结果按 revision_id 缓存：发布版本不可变，同一个 revision 只解析一次；本次没用到的缓存项会被清理。
func (s *AIConfigService) buildState(rows []repository.PublishedModel, runtimes map[string]*provider.ChannelRuntime) *aiRegistryState {
	parsed := make(map[uint64]*modelcfg.ModelConfig, len(rows))
	st := &aiRegistryState{loadedAt: s.now(), models: map[string]*aiRegModel{}}
	for _, row := range rows {
		cfg, ok := s.modelParse[row.RevisionID]
		if !ok {
			var issues []modelcfg.Issue
			cfg, issues = modelcfg.ParseModel(row.Body)
			if len(issues) > 0 || cfg == nil {
				// 已发布的版本都通过了校验；万一不通过（比如手工改库），让它不可用而不是带病运行
				logger.Error("已发布的模型配置无法解析，已忽略", zap.String("model", row.Key),
					zap.Uint64("revision_id", row.RevisionID), zap.String("issues", aiFormatIssues(issues)))
				continue
			}
		}
		parsed[row.RevisionID] = cfg
		m := &aiRegModel{key: row.Key, enabled: row.Enabled, sort: row.Sort, revisionID: row.RevisionID, cfg: cfg,
			rt: modelRuntime(cfg, runtimes)}
		st.models[row.Key] = m
		st.ordered = append(st.ordered, m)
	}
	sort.SliceStable(st.ordered, func(i, j int) bool {
		if st.ordered[i].sort != st.ordered[j].sort {
			return st.ordered[i].sort < st.ordered[j].sort
		}
		return st.ordered[i].key < st.ordered[j].key
	})
	s.modelParse = parsed
	return st
}

// modelRuntime 取模型 channels[0] 对应的渠道运行时；渠道不可用，或它的插件版本不支持模型的 kind 时返回 nil（模型不可用）。
// 发布时检查过 kind，但之后渠道可能被切到一个不支持该 kind 的插件版本，这里再兜一次底，避免任务提交后才失败。
func modelRuntime(cfg *modelcfg.ModelConfig, runtimes map[string]*provider.ChannelRuntime) *provider.ChannelRuntime {
	if len(cfg.Channels) == 0 {
		return nil
	}
	rt := runtimes[cfg.Channels[0].Channel]
	if rt == nil {
		return nil
	}
	if _, ok := rt.Plugin.Meta.Endpoint(cfg.Kind); !ok {
		logger.Warn("渠道当前的插件版本不支持模型的种类，模型暂不可用", zap.String("model", cfg.Key),
			zap.String("channel", rt.Channel.Key), zap.String("kind", cfg.Kind))
		return nil
	}
	return rt
}

// loadRuntimes 加载所有“当前可用”的渠道运行时（渠道 key → 渠道 + 固定的插件版本）：渠道启用、插件启用、版本行存在且 meta 能解析。
// 停用是正常状态，直接跳过；数据不一致（版本缺失、meta 损坏）跳过并告警。只有仓储故障才返回错误。
func (s *AIConfigService) loadRuntimes(ctx context.Context) (map[string]*provider.ChannelRuntime, error) {
	chans, err := s.channels.ListChannels(ctx)
	if err != nil {
		return nil, fmt.Errorf("加载渠道失败：%w", err)
	}
	vers, err := s.plugins.ListVersions(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("加载插件版本失败：%w", err)
	}
	enabledPlugins, err := s.enabledPlugins(ctx, chans)
	if err != nil {
		return nil, err
	}
	byID := make(map[uint64]*model.AIPluginVersion, len(vers))
	for i := range vers {
		byID[vers[i].ID] = &vers[i]
	}
	out := make(map[string]*provider.ChannelRuntime, len(chans))
	for i := range chans {
		ch := &chans[i]
		if !ch.Enabled || !enabledPlugins[ch.PluginKey] {
			continue
		}
		if rt := channelRuntime(ch, byID[ch.PluginVersionID]); rt != nil {
			out[ch.Key] = rt
		}
	}
	return out, nil
}

// enabledPlugins 查询启用渠道所用插件的启停状态；插件行不存在按停用处理（渠道引用了已删除的插件）。
func (s *AIConfigService) enabledPlugins(ctx context.Context, chans []model.AIChannel) (map[string]bool, error) {
	out := map[string]bool{}
	for _, ch := range chans {
		if _, seen := out[ch.PluginKey]; seen || !ch.Enabled {
			continue
		}
		p, err := s.plugins.GetPlugin(ctx, ch.PluginKey)
		if errors.Is(err, repository.ErrNotFound) {
			out[ch.PluginKey] = false
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("加载插件 %s 失败：%w", ch.PluginKey, err)
		}
		out[ch.PluginKey] = p.Enabled
	}
	return out, nil
}

// channelRuntime 由渠道行与它固定的插件版本组装运行时；版本缺失、不属于该插件、渠道 JSON 或 meta 损坏时告警并返回 nil。
func channelRuntime(ch *model.AIChannel, ver *model.AIPluginVersion) *provider.ChannelRuntime {
	if ver == nil || ver.PluginKey != ch.PluginKey {
		logger.Warn("渠道固定的插件版本不存在，渠道暂不可用", zap.String("channel", ch.Key),
			zap.String("plugin", ch.PluginKey), zap.Uint64("plugin_version_id", ch.PluginVersionID))
		return nil
	}
	rt, err := provider.RuntimeOf(ch, ver)
	if err != nil {
		logger.Error("渠道配置或插件 meta 无法解析，渠道暂不可用", zap.String("channel", ch.Key), zap.Error(err))
		return nil
	}
	return rt
}
