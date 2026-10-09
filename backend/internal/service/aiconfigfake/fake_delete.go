package aiconfigfake

import (
	"context"
	"encoding/json"
	"sort"

	"video-canvas/internal/model"
	"video-canvas/internal/repository"
)

// ---------------------------------------------------------------------------
// 删除相关：模型硬删除、渠道 / 插件的引用统计与删除
// ---------------------------------------------------------------------------

// DeleteModel 硬删除模型：删掉指针行与它的全部 revision；仍上架返回 ErrInUse，不存在返回 ErrNotFound。
func (m *MemRepo) DeleteModel(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.Models[key]
	if !ok {
		return repository.ErrNotFound
	}
	if row.Enabled || m.DeleteInUse {
		return repository.ErrInUse
	}
	kept := m.Revs[:0]
	for _, rev := range m.Revs {
		if rev.Target != model.ConfigTargetModel || rev.TargetKey != key {
			kept = append(kept, rev)
		}
	}
	m.Revs = kept
	delete(m.Models, key)
	return nil
}

// CountChannelRefs 统计渠道的引用：现存模型的草稿 / 已发布正文里 channels[].channel 指向它；任务数由 ChannelTaskRefs 设定。
func (m *MemRepo) CountChannelRefs(_ context.Context, key string) (repository.ChannelRefs, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.Channels[key]; !ok {
		return repository.ChannelRefs{}, repository.ErrNotFound
	}
	return m.channelRefs(key), nil
}

// channelRefs 计算渠道引用（调用方持有锁），与真实仓储一致：每个模型只有一份配置。
func (m *MemRepo) channelRefs(key string) repository.ChannelRefs {
	refs := repository.ChannelRefs{Models: []repository.ChannelModelRef{}, ActiveTasks: m.ChannelTaskRefs[key]}
	for _, rev := range m.Revs {
		if rev.Target != model.ConfigTargetModel {
			continue
		}
		if _, ok := m.Models[rev.TargetKey]; !ok {
			continue
		}
		var body struct {
			Label    string `json:"label"`
			Channels []struct {
				Channel string `json:"channel"`
			} `json:"channels"`
		}
		if json.Unmarshal(rev.BodyJSON, &body) != nil || !refsChannel(body.Channels, key) {
			continue
		}
		refs.Models = append(refs.Models, repository.ChannelModelRef{Key: rev.TargetKey, Label: body.Label})
	}
	sort.Slice(refs.Models, func(i, j int) bool { return refs.Models[i].Key < refs.Models[j].Key })
	return refs
}

func refsChannel(list []struct {
	Channel string `json:"channel"`
}, key string) bool {
	for _, c := range list {
		if c.Channel == key {
			return true
		}
	}
	return false
}

// DeleteChannel 删除渠道与它的 Key；仍被引用返回当时的引用与 ErrInUse，不存在返回 ErrNotFound。
func (m *MemRepo) DeleteChannel(_ context.Context, key string) (repository.ChannelRefs, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.Channels[key]; !ok {
		return repository.ChannelRefs{}, repository.ErrNotFound
	}
	refs := m.channelRefs(key)
	if refs.InUse() || m.DeleteInUse {
		return refs, repository.ErrInUse
	}
	delete(m.Channels, key)
	delete(m.Secrets, model.ChannelSecretName(key))
	return refs, nil
}

// CountPluginRefs 统计插件全部版本的引用：渠道按当前渠道计算，任务数为各版本 ActiveTaskRefs 之和。
func (m *MemRepo) CountPluginRefs(_ context.Context, key string) (repository.PluginRefs, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.Plugins[key]; !ok {
		return repository.PluginRefs{}, repository.ErrNotFound
	}
	return m.pluginRefs(key), nil
}

// pluginRefs 计算插件引用（调用方持有锁）。
func (m *MemRepo) pluginRefs(key string) repository.PluginRefs {
	refs := repository.PluginRefs{Channels: []model.AIChannel{}}
	ids := map[uint64]bool{}
	for id, v := range m.Versions {
		if v.PluginKey == key {
			ids[id] = true
			refs.ActiveTasks += m.ActiveTaskRefs[id]
		}
	}
	for _, c := range m.Channels {
		if ids[c.PluginVersionID] {
			refs.Channels = append(refs.Channels, model.AIChannel{Key: c.Key, Name: c.Name})
		}
	}
	sort.Slice(refs.Channels, func(i, j int) bool { return refs.Channels[i].Key < refs.Channels[j].Key })
	return refs
}

// DeletePlugin 删除插件的全部版本与插件行；仍被引用返回当时的引用与 ErrInUse，不存在返回 ErrNotFound。
func (m *MemRepo) DeletePlugin(_ context.Context, key string) (repository.PluginRefs, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.Plugins[key]; !ok {
		return repository.PluginRefs{}, repository.ErrNotFound
	}
	refs := m.pluginRefs(key)
	if refs.InUse() || m.DeleteInUse {
		return refs, repository.ErrInUse
	}
	for id, v := range m.Versions {
		if v.PluginKey == key {
			delete(m.Versions, id)
		}
	}
	delete(m.Plugins, key)
	return refs, nil
}
