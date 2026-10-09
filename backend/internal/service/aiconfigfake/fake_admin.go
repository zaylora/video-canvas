package aiconfigfake

import (
	"context"
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

// CountVersionRefs 统计版本被引用的情况：渠道数按当前渠道计算，进行中的任务数由测试通过 ActiveTaskRefs 设定。
func (m *MemRepo) CountVersionRefs(_ context.Context, id uint64) (repository.VersionRefs, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.Versions[id]; !ok {
		return repository.VersionRefs{}, repository.ErrNotFound
	}
	refs := repository.VersionRefs{ActiveTasks: m.ActiveTaskRefs[id]}
	for _, c := range m.Channels {
		if c.PluginVersionID == id {
			refs.Channels++
		}
	}
	return refs, nil
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
	for _, c := range m.Channels {
		if c.PluginVersionID == id {
			return repository.ErrInUse
		}
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

// CreateChannel 新建渠道：key 重复返回 ErrDuplicate，插件版本不存在返回 ErrNotFound。
func (m *MemRepo) CreateChannel(_ context.Context, c *model.AIChannel) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.Versions[c.PluginVersionID]; !ok {
		return repository.ErrNotFound
	}
	if _, ok := m.Channels[c.Key]; ok {
		return repository.ErrDuplicate
	}
	now := time.Now()
	c.CreatedAt, c.UpdatedAt = now, now
	if len(c.SettingsJSON) == 0 {
		c.SettingsJSON = model.JSONText("{}")
	}
	if len(c.RateLimitJSON) == 0 {
		c.RateLimitJSON = model.JSONText("{}")
	}
	stored := *c
	m.Channels[c.Key] = &stored
	return nil
}

// UpdateChannel 更新渠道的可变字段：渠道或新的插件版本不存在返回 ErrNotFound。
func (m *MemRepo) UpdateChannel(_ context.Context, c *model.AIChannel) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	old, ok := m.Channels[c.Key]
	if !ok {
		return repository.ErrNotFound
	}
	if _, ok := m.Versions[c.PluginVersionID]; !ok {
		return repository.ErrNotFound
	}
	c.CreatedAt, c.UpdatedAt = old.CreatedAt, time.Now()
	stored := *c
	m.Channels[c.Key] = &stored
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

// PluginOps 是 provider.PluginOps 的替身。
type PluginOps struct {
	CheckResult *provider.CheckResult
	CheckErr    error
	Drafts      []provider.ModelDraft
	ImportErr   error

	GotRuntime *provider.ChannelRuntime
	GotArgs    map[string]any
	GotSecret  string // 最近一次 CheckDraft 收到的草稿 Key
	DraftCalls int    // CheckDraft 的调用次数
	Calls      int
}

// Check 记录调用并返回预设结果。
func (o *PluginOps) Check(_ context.Context, rt *provider.ChannelRuntime) (*provider.CheckResult, error) {
	o.Calls++
	o.GotRuntime = rt
	return o.CheckResult, o.CheckErr
}

// CheckDraft 记录调用（含草稿 Key）并返回预设结果。
func (o *PluginOps) CheckDraft(_ context.Context, rt *provider.ChannelRuntime, secret string) (*provider.CheckResult, error) {
	o.Calls++
	o.DraftCalls++
	o.GotRuntime, o.GotSecret = rt, secret
	return o.CheckResult, o.CheckErr
}

// Import 记录调用并返回预设结果。
func (o *PluginOps) Import(_ context.Context, rt *provider.ChannelRuntime, args map[string]any) ([]provider.ModelDraft, error) {
	o.Calls++
	o.GotRuntime, o.GotArgs = rt, args
	return o.Drafts, o.ImportErr
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
