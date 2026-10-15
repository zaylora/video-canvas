package aiconfigfake

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
)

// ---------------------------------------------------------------------------
// 插件 / 渠道 / 审计 的写入侧（AIPluginRepo、AIChannelRepo、AIAuditWriter）
// ---------------------------------------------------------------------------

// SaveVersion 模拟 repository.AIPluginRepository.SaveVersion：插件行不存在就创建，已存在只更新 name；版本重复返回 ErrDuplicate。
func (m *MemRepo) SaveVersion(_ context.Context, p *model.AIPlugin, v *model.AIPluginVersion) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.SaveVersionErr != nil {
		return m.SaveVersionErr
	}
	if m.SaveVersionConflicts > 0 {
		m.SaveVersionConflicts--
		m.nextID++
		rival := &model.AIPluginVersion{ID: m.nextID, PluginKey: p.Key, Version: v.Version, SHA256: "rival-" + v.SHA256, CreatedAt: time.Now()}
		m.Versions[rival.ID] = rival
		return repository.ErrDuplicate
	}
	for _, old := range m.Versions {
		if old.PluginKey == p.Key && old.Version == v.Version {
			return repository.ErrDuplicate
		}
	}
	if row, ok := m.Plugins[p.Key]; ok {
		row.Name = p.Name
		row.UpdatedAt = time.Now()
		*p = *row
	} else {
		row := *p
		row.UpdatedAt = time.Now()
		m.Plugins[p.Key] = &row
		*p = row
	}
	m.nextID++
	v.ID, v.CreatedAt = m.nextID, time.Now()
	v.PluginKey = p.Key
	stored := *v
	m.Versions[v.ID] = &stored
	return nil
}

// ListPlugins 按 key 升序返回所有插件。
func (m *MemRepo) ListPlugins(_ context.Context) ([]model.AIPlugin, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]model.AIPlugin, 0, len(m.Plugins))
	for _, p := range m.Plugins {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// FindVersion 按 (plugin_key, version) 查询版本头信息。
func (m *MemRepo) FindVersion(_ context.Context, pluginKey, version string) (*model.AIPluginVersion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, v := range m.Versions {
		if v.PluginKey == pluginKey && v.Version == version {
			cp := *v
			cp.Code = ""
			return &cp, nil
		}
	}
	return nil, repository.ErrNotFound
}

// SetPluginEnabled 启用 / 停用插件。
func (m *MemRepo) SetPluginEnabled(_ context.Context, key string, enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.Plugins[key]
	if !ok {
		return repository.ErrNotFound
	}
	p.Enabled = enabled
	return nil
}

// protocolRefs 列出协议固定在 (pluginKey, version) 上的模型（调用方持有锁）；version 为空表示该插件的任意版本。
func (m *MemRepo) protocolRefs(pluginKey, version string) []repository.ChannelModelRef {
	var out []repository.ChannelModelRef
	for _, row := range m.Models {
		if row.PublishedRevisionID == nil {
			continue
		}
		rev := m.find(*row.PublishedRevisionID)
		if rev == nil {
			continue
		}
		var body struct {
			Label    string `json:"label"`
			Channels []struct {
				Protocol *struct {
					Plugin  string `json:"plugin"`
					Version string `json:"version"`
				} `json:"protocol"`
			} `json:"channels"`
		}
		if json.Unmarshal(rev.BodyJSON, &body) != nil || len(body.Channels) == 0 || body.Channels[0].Protocol == nil {
			continue
		}
		p := body.Channels[0].Protocol
		if p.Plugin == pluginKey && (version == "" || p.Version == version) {
			out = append(out, repository.ChannelModelRef{Key: rev.TargetKey, Label: body.Label})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// CountVersionRefs 统计版本被引用的情况：模型数按当前模型的协议计算，进行中的任务数由测试通过 ActiveTaskRefs 设定。
func (m *MemRepo) CountVersionRefs(_ context.Context, id uint64) (repository.VersionRefs, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.Versions[id]; !ok {
		return repository.VersionRefs{}, repository.ErrNotFound
	}
	v := m.Versions[id]
	return repository.VersionRefs{Models: int64(len(m.protocolRefs(v.PluginKey, v.Version))), ActiveTasks: m.ActiveTaskRefs[id]}, nil
}

// DeleteVersion 删除版本（仍被引用返回 ErrInUse）；插件没有版本后连插件行一起删。
func (m *MemRepo) DeleteVersion(_ context.Context, id uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.Versions[id]
	if !ok {
		return repository.ErrNotFound
	}
	if m.DeleteInUse {
		return repository.ErrInUse
	}
	if len(m.protocolRefs(v.PluginKey, v.Version)) > 0 {
		return repository.ErrInUse
	}
	if m.ActiveTaskRefs[id] > 0 {
		return repository.ErrInUse
	}
	delete(m.Versions, id)
	for _, other := range m.Versions {
		if other.PluginKey == v.PluginKey {
			return nil
		}
	}
	delete(m.Plugins, v.PluginKey)
	return nil
}

// CreateChannel 新建渠道：key 重复返回 ErrDuplicate。
func (m *MemRepo) CreateChannel(_ context.Context, c *model.AIChannel) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.Channels[c.Key]; ok {
		return repository.ErrDuplicate
	}
	now := time.Now()
	c.CreatedAt, c.UpdatedAt = now, now
	if len(c.HeadersJSON) == 0 {
		c.HeadersJSON = model.JSONText("{}")
	}
	if len(c.RateLimitJSON) == 0 {
		c.RateLimitJSON = model.JSONText("{}")
	}
	stored := *c
	m.Channels[c.Key] = &stored
	return nil
}

// UpdateChannel 更新渠道的可变字段（不含 sort）：渠道不存在返回 ErrNotFound。
func (m *MemRepo) UpdateChannel(_ context.Context, c *model.AIChannel) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	old, ok := m.Channels[c.Key]
	if !ok {
		return repository.ErrNotFound
	}
	c.CreatedAt, c.UpdatedAt, c.Sort = old.CreatedAt, time.Now(), old.Sort
	stored := *c
	m.Channels[c.Key] = &stored
	return nil
}

// SetChannelOrder 按 keys 重写渠道 sort；不是现有渠道的全集返回 ErrNotFound / ErrOrderMismatch，与真实仓储一致。
func (m *MemRepo) SetChannelOrder(_ context.Context, keys []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	seen := map[string]bool{}
	for _, k := range keys {
		if _, ok := m.Channels[k]; !ok {
			return repository.ErrNotFound
		}
		if seen[k] {
			return repository.ErrOrderMismatch
		}
		seen[k] = true
	}
	if len(seen) != len(m.Channels) {
		return repository.ErrOrderMismatch
	}
	for i, k := range keys {
		m.Channels[k].Sort = (i + 1) * 10
	}
	return nil
}

// DeleteSecret 删除凭证（不存在也算成功）。
func (m *MemRepo) DeleteSecret(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.Secrets, name)
	return nil
}

// InsertAudit 记录审计日志。
func (m *MemRepo) InsertAudit(_ context.Context, log *model.AIAuditLog) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.AuditErr != nil {
		return m.AuditErr
	}
	m.nextID++
	log.ID, log.CreatedAt = m.nextID, time.Now()
	m.Audits = append(m.Audits, *log)
	return nil
}

// AuditActions 返回已记录审计的动作列表（按写入顺序），断言用。
func (m *MemRepo) AuditActions() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.Audits))
	for _, a := range m.Audits {
		out = append(out, a.Action)
	}
	return out
}

// Prechecker 是 provider.Prechecker 的替身：按代码内容返回预设结果，没有预设时返回 Default。
type Prechecker struct {
	ByCode  map[string]*provider.PrecheckResult
	Default *provider.PrecheckResult
	Err     error
	Calls   int
}

// Precheck 记录调用并返回预设结果。
func (p *Prechecker) Precheck(_ context.Context, code string) (*provider.PrecheckResult, error) {
	p.Calls++
	if p.Err != nil {
		return nil, p.Err
	}
	if r, ok := p.ByCode[code]; ok {
		return r, nil
	}
	return p.Default, nil
}

// ModelLister 是 provider.ModelLister 的替身：返回预设的上游模型 id 或预设的错误，并记录最近一次调用收到的渠道与 Key。
type ModelLister struct {
	IDs []string
	Err error

	Calls   int
	Channel provider.ChannelSnapshot // 最近一次调用收到的渠道（地址、请求头、限流）
	APIKey  string                   // 最近一次调用收到的 API Key 明文
}

// ListUpstreamModels 记录调用并返回预设结果。
func (l *ModelLister) ListUpstreamModels(_ context.Context, ch provider.ChannelSnapshot, apiKey string) ([]string, error) {
	l.Calls++
	l.Channel, l.APIKey = ch, apiKey
	if l.Err != nil {
		return nil, l.Err
	}
	return append([]string(nil), l.IDs...), nil
}

// Notifier 记录 Registry 刷新通知。
type Notifier struct {
	mu      sync.Mutex
	Reasons []string
}

// NotifyChanged 记录一次通知。
func (n *Notifier) NotifyChanged(_ context.Context, reason string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.Reasons = append(n.Reasons, reason)
}
