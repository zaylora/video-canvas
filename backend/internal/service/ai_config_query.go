package service

import (
	"context"
	"encoding/json"
	"errors"

	"video-canvas/internal/model"
	"video-canvas/internal/repository"
)

// ListConfigs 列出所有模型配置的概览（不含正文），带展示名（label）、厂商、标签与绑定的渠道 key（channel）。
// 返回的切片永远不是 nil。
func (s *AIConfigService) ListConfigs(ctx context.Context) ([]ConfigListItem, error) {
	// 1. 取所有模型的配置，按 key 归并。要读正文是因为 label 与 channel 只在正文里；
	//    管理端模型数量有限，一次读出可以接受
	configs, err := s.repo.ListModelConfigs(ctx)
	if err != nil {
		return nil, err
	}
	displays := make(map[string]aiDisplay, len(configs))
	for _, c := range configs {
		displays[c.TargetKey] = aiDisplayOf(c.BodyJSON)
	}

	// 2. 与指针行合并：指针行的 enabled / sort 是运行时真值
	rows, err := s.repo.ListModelPointers(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]ConfigListItem, 0, len(rows))
	for _, r := range rows {
		d := displays[r.Key]
		item := ConfigListItem{Key: r.Key, Kind: r.Kind, Enabled: r.Enabled, Sort: r.Sort, UpdatedAt: r.UpdatedAt,
			Label: d.label, Channel: d.channel, Vendor: d.vendor, Tags: d.tags}
		if item.Tags == nil {
			item.Tags = []string{}
		}
		items = append(items, item)
	}
	return items, nil
}

// aiDisplay 是列表要展示的几个正文字段。
type aiDisplay struct {
	label, channel, vendor string
	tags                   []string
}

// aiDisplayOf 从配置正文里宽松取出列表要展示的 label / channels[0].channel / vendor / tags；
// 正文不是合法 JSON 对象时全部置空。tags 里的非字符串元素丢弃。
func aiDisplayOf(body model.JSONText) aiDisplay {
	var m aiConfigMeta
	if err := json.Unmarshal(body, &m); err != nil {
		return aiDisplay{}
	}
	d := aiDisplay{label: m.Label, channel: m.channel(), vendor: m.Vendor}
	for _, t := range m.Tags {
		if str, ok := t.(string); ok {
			d.tags = append(d.tags, str)
		}
	}
	return d
}

// GetConfig 返回模型详情：指针行信息与配置正文。模型不存在返回 ErrConfigNotFound。
func (s *AIConfigService) GetConfig(ctx context.Context, key string) (*ConfigDetail, error) {
	// 1. 指针行：不存在就是 404
	m, err := s.repo.GetModelPointer(ctx, key)
	if err != nil {
		return nil, aiNotFound(err)
	}
	detail := &ConfigDetail{Target: model.ConfigTargetModel, Key: key, Kind: m.Kind, Enabled: m.Enabled, Sort: m.Sort, UpdatedAt: m.UpdatedAt}

	// 2. 配置正文：还没有就是 null，不算错误
	cur, err := s.repo.GetModelConfig(ctx, key)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}
	if err == nil {
		detail.Body = json.RawMessage(cur.BodyJSON)
	}
	return detail, nil
}
