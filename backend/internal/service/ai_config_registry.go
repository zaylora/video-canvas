package service

import (
	"context"
	"fmt"
	"sort"
	"time"

	"go.uber.org/zap"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/dsl"
	"video-canvas/internal/pkg/logger"
)

// aiRegModel 是 Registry 里的一个已发布模型：指针行的运行时开关 + 已解析的发布版本。
type aiRegModel struct {
	key         string
	providerKey string
	enabled     bool
	sort        int
	revisionID  uint64
	cfg         *dsl.ModelConfig
}

// aiRegProvider 是 Registry 里的一个已发布平台。
type aiRegProvider struct {
	revisionID uint64
	cfg        *dsl.ProviderConfig
}

// aiRegistryState 是一次完整加载得到的只读内存状态，替换时整体换指针，读方无需加锁遍历。
type aiRegistryState struct {
	loadedAt  time.Time
	providers map[string]aiRegProvider
	models    map[string]*aiRegModel
	ordered   []*aiRegModel // 按 sort、key 升序，ListModels 用
}

// ListModels 实现 provider.Registry：返回已发布且上架的模型（按 sort 升序），只含面向画布的公开字段，
// 不含 params / mapping / provider 细节。它引用的平台不可用（未发布或配置异常）的模型不会出现。
func (s *AIConfigService) ListModels(ctx context.Context, kind string) ([]provider.ModelInfo, error) {
	// 1. 取内存状态（过期或被标记失效时会重新加载）
	st, err := s.registryState(ctx)
	if err != nil {
		return nil, err
	}
	// 2. 过滤并转成公开结构；用非 nil 切片保证 JSON 输出 [] 而不是 null
	out := make([]provider.ModelInfo, 0, len(st.ordered))
	for _, m := range st.ordered {
		if !m.enabled || (kind != "" && m.cfg.Kind != kind) {
			continue
		}
		out = append(out, provider.ModelInfo{
			Key: m.key, Kind: m.cfg.Kind, Label: m.cfg.Label, Hint: m.cfg.Hint,
			Credits: m.cfg.Credits, InputSchema: m.cfg.InputSchema,
		})
	}
	return out, nil
}

// Snapshot 实现 provider.Registry：冻结模型当前发布版本 + 所属平台当前发布版本。
// 模型不存在、未发布、已下线、或平台不可用都返回 provider.ErrModelUnavailable。
// 返回的快照与缓存共享内部的 map / slice，调用方（引擎、任务服务）只能读、不能改。
func (s *AIConfigService) Snapshot(ctx context.Context, modelKey string) (*dsl.Snapshot, error) {
	st, err := s.registryState(ctx)
	if err != nil {
		return nil, err
	}
	m, ok := st.models[modelKey]
	if !ok || !m.enabled {
		return nil, provider.ErrModelUnavailable
	}
	p, ok := st.providers[m.providerKey]
	if !ok {
		return nil, provider.ErrModelUnavailable
	}
	snap := &dsl.Snapshot{
		Provider:           *p.cfg,
		Model:              *m.cfg,
		ModelRevisionID:    m.revisionID,
		ProviderRevisionID: p.revisionID,
	}
	// 指针行的开关和排序才是运行时真值，正文里的 enabled / sort 只是初始值
	snap.Model.Enabled, snap.Model.Sort = m.enabled, m.sort
	return snap, nil
}

// Provider 实现 provider.Registry：返回平台当前发布版本（webhook 用），不存在返回 provider.ErrModelUnavailable。
func (s *AIConfigService) Provider(ctx context.Context, providerKey string) (*dsl.ProviderConfig, error) {
	st, err := s.registryState(ctx)
	if err != nil {
		return nil, err
	}
	p, ok := st.providers[providerKey]
	if !ok {
		return nil, provider.ErrModelUnavailable
	}
	return p.cfg, nil
}

// RefreshRegistry 强制从数据库重新加载已发布配置。发布 / 回滚 / 上下架后本实例立即调用；
// 多实例时其他实例收到失效广播后也调用它（RegistryInvalidator 的接收端）。
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

// reload 加载所有已发布的平台和模型并原子替换内存状态。
// 解析结果按 revision_id 缓存：发布版本不可变，同一个 revision 只解析一次；本次没用到的缓存项会被清理。
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

	provRows, err := s.repo.LoadPublishedProviders(ctx)
	if err != nil {
		return nil, fmt.Errorf("加载已发布平台失败：%w", err)
	}
	modelRows, err := s.repo.LoadPublishedModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("加载已发布模型失败：%w", err)
	}

	newProvParsed := map[uint64]*dsl.ProviderConfig{}
	newModelParsed := map[uint64]*dsl.ModelConfig{}
	st := &aiRegistryState{
		loadedAt:  s.now(),
		providers: map[string]aiRegProvider{},
		models:    map[string]*aiRegModel{},
	}

	for _, row := range provRows {
		cfg, ok := s.provParsed[row.RevisionID]
		if !ok {
			var issues []dsl.Issue
			cfg, issues = s.validator.ParseProvider(row.Body)
			if len(issues) > 0 || cfg == nil {
				// 已发布的版本理论上都通过了校验；万一不通过（比如手工改库），让它不可用而不是带病运行
				logger.Error("已发布的平台配置无法解析，已忽略", zap.String("provider", row.Key),
					zap.Uint64("revision_id", row.RevisionID), zap.String("issues", aiFormatIssues(issues)))
				continue
			}
		}
		newProvParsed[row.RevisionID] = cfg
		st.providers[row.Key] = aiRegProvider{revisionID: row.RevisionID, cfg: cfg}
	}

	for _, row := range modelRows {
		prov, ok := st.providers[row.ProviderKey]
		if !ok {
			// 平台没发布（或不可用）：模型不可用，等平台发布后下次刷新自然出现
			logger.Warn("模型引用的平台不可用，已忽略", zap.String("model", row.Key), zap.String("provider", row.ProviderKey))
			continue
		}
		cfg, ok := s.modelParse[row.RevisionID]
		if !ok {
			var issues []dsl.Issue
			cfg, issues = s.validator.ParseModel(row.Body, prov.cfg)
			if len(issues) > 0 || cfg == nil {
				logger.Error("已发布的模型配置无法解析，已忽略", zap.String("model", row.Key),
					zap.Uint64("revision_id", row.RevisionID), zap.String("issues", aiFormatIssues(issues)))
				continue
			}
		}
		newModelParsed[row.RevisionID] = cfg
		m := &aiRegModel{key: row.Key, providerKey: row.ProviderKey, enabled: row.Enabled, sort: row.Sort, revisionID: row.RevisionID, cfg: cfg}
		st.models[row.Key] = m
		st.ordered = append(st.ordered, m)
	}
	sort.SliceStable(st.ordered, func(i, j int) bool {
		if st.ordered[i].sort != st.ordered[j].sort {
			return st.ordered[i].sort < st.ordered[j].sort
		}
		return st.ordered[i].key < st.ordered[j].key
	})

	s.provParsed, s.modelParse = newProvParsed, newModelParsed
	s.regMu.Lock()
	s.regState = st
	s.regMu.Unlock()
	return st, nil
}
