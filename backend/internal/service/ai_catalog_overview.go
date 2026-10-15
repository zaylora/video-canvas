package service

import (
	"context"
	"fmt"
	"sort"
)

// 总览页的汇总：各类数量、可用数、待处理清单，以及“渠道负载”卡片要的精简渠道。
// 待处理清单的规则与前端 adminTodos 逐条一致（用同一组用例对照）。

// OverviewTodoAction 是待处理项的修复动作：能就地修的（设 Key、一键升级）前端就地弹窗，其余深链到对应位置。
type OverviewTodoAction struct {
	Kind        string `json:"kind"` // set-key / open-plugin / open-channel / upgrade-plugin
	ChannelKey  string `json:"channel_key,omitempty"`
	ChannelName string `json:"channel_name,omitempty"` // set-key 弹窗里要显示渠道名
	PluginKey   string `json:"plugin_key,omitempty"`
}

// OverviewTodo 是待处理清单的一条。
type OverviewTodo struct {
	ID     string             `json:"id"`   // 稳定 id（前端当 React key）
	Tone   string             `json:"tone"` // bad / warn / info
	Text   string             `json:"text"`
	Action OverviewTodoAction `json:"action"`
}

// OverviewTodos 返回待处理清单，按严重度排：插件停用波及在线模型 → 渠道不可用 / 缺 Key → 插件可升级。
func OverviewTodos(models []CatalogModel, channels []CatalogChannel, plugins []CatalogPlugin) []OverviewTodo {
	todos := []OverviewTodo{}
	todos = append(todos, pluginOffTodos(models, plugins)...)
	todos = append(todos, channelTodos(models, channels)...)
	todos = append(todos, upgradeTodos(models, plugins)...)

	rank := map[string]int{"bad": 0, "warn": 1, "info": 2}
	sort.SliceStable(todos, func(i, j int) bool { return rank[todos[i].Tone] < rank[todos[j].Tone] })
	return todos
}

// modelCountsOf 统计使用某渠道的模型数：all 是全部，online 是已上架的。
func modelCountsOf(models []CatalogModel, channelKey string) (all, online int) {
	for _, m := range models {
		if m.Channel != channelKey {
			continue
		}
		all++
		if m.Enabled {
			online++
		}
	}
	return all, online
}

// pluginOffTodos 是“插件已停用”：有模型的协议用了已停用的插件，这些模型实际不可用，并统计波及的上架模型。
func pluginOffTodos(models []CatalogModel, plugins []CatalogPlugin) []OverviewTodo {
	var todos []OverviewTodo
	for _, p := range plugins {
		if p.Enabled {
			continue
		}
		used, online := 0, 0
		for _, m := range models {
			if m.Plugin != p.Key {
				continue
			}
			used++
			if m.Enabled {
				online++
			}
		}
		if used == 0 {
			continue
		}
		suffix := ""
		if online > 0 {
			suffix = fmt.Sprintf("（其中 %d 个已上架）", online)
		}
		todos = append(todos, OverviewTodo{
			ID: "plugin-off:" + p.Key, Tone: "bad",
			Text:   fmt.Sprintf("插件「%s」已停用，%d 个模型%s实际不可用", p.Name, used, suffix),
			Action: OverviewTodoAction{Kind: "open-plugin", PluginKey: p.Key},
		})
	}
	return todos
}

// channelTodos 是渠道自身的问题：缺 Key（优先），或已停用但仍有上架模型。
func channelTodos(models []CatalogModel, channels []CatalogChannel) []OverviewTodo {
	var todos []OverviewTodo
	for _, c := range channels {
		health := ChannelHealthOf(c)
		used, online := modelCountsOf(models, c.Key)
		switch {
		case health.Label == "未设 Key":
			suffix := ""
			if used > 0 {
				suffix = fmt.Sprintf("，%d 个模型因此无法上线", used)
			}
			todos = append(todos, OverviewTodo{
				ID: "key:" + c.Key, Tone: "warn",
				Text:   fmt.Sprintf("渠道「%s」还没设置 Key%s", c.Name, suffix),
				Action: OverviewTodoAction{Kind: "set-key", ChannelKey: c.Key, ChannelName: c.Name},
			})
		case !c.Enabled && online > 0:
			todos = append(todos, OverviewTodo{
				ID: "channel-off:" + c.Key, Tone: "bad",
				Text:   fmt.Sprintf("渠道「%s」已停用，%d 个上架模型实际不可用", c.Name, online),
				Action: OverviewTodoAction{Kind: "open-channel", ChannelKey: c.Key},
			})
		}
	}
	return todos
}

// upgradeTodos 是“插件可升级”：按插件合并成一条，总览页上一键把这个插件的旧版本模型全部升级。
func upgradeTodos(models []CatalogModel, plugins []CatalogPlugin) []OverviewTodo {
	var todos []OverviewTodo
	for _, p := range plugins {
		outdated := 0
		for _, m := range models {
			if m.Plugin == p.Key && AvailableUpgrade(p, m.PluginVersion) != "" {
				outdated++
			}
		}
		if outdated == 0 {
			continue
		}
		latest, _ := latestOf(p)
		todos = append(todos, OverviewTodo{
			ID: "upgrade:" + p.Key, Tone: "info",
			Text:   fmt.Sprintf("%s v%s 可用，%d 个模型还在旧版本", p.Name, latest.Version, outdated),
			Action: OverviewTodoAction{Kind: "upgrade-plugin", PluginKey: p.Key},
		})
	}
	return todos
}

// OverviewChannel 是“渠道负载”卡片要的精简渠道：名称、同时生成上限、健康状态。
type OverviewChannel struct {
	Key        string        `json:"key"`
	Name       string        `json:"name"`
	MaxRunning int           `json:"max_running"` // 0 表示不限
	Health     ChannelHealth `json:"health"`
}

// Overview 是总览页的汇总。
type Overview struct {
	Plugins struct {
		Total  int `json:"total"`
		Usable int `json:"usable"` // 已启用
	} `json:"plugins"`
	Channels struct {
		Total  int `json:"total"`
		Usable int `json:"usable"` // 健康状态为 ok
		NoKey  int `json:"no_key"` // 缺 Key
	} `json:"channels"`
	Models struct {
		Total   int `json:"total"`
		Online  int `json:"online"`
		Broken  int `json:"broken"`  // 已上架但渠道或协议用不了
		Pending int `json:"pending"` // 待配置：还没选能力或协议
	} `json:"models"`
	Todos         []OverviewTodo    `json:"todos"`
	ChannelBriefs []OverviewChannel `json:"channel_briefs"`
}

// Overview 读一遍渠道 / 插件 / 模型，算出总览页要的全部汇总，前端不再拉全量清单。
func (s *AICatalogService) Overview(ctx context.Context) (*Overview, error) {
	c, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	ov := &Overview{ChannelBriefs: []OverviewChannel{}}
	ov.Plugins.Total = len(c.plugins)
	for _, p := range c.cPlugins {
		if p.Enabled {
			ov.Plugins.Usable++
		}
	}
	ov.Channels.Total = len(c.channels)
	for i, ch := range c.channels {
		health := ChannelHealthOf(c.cChannels[i])
		switch {
		case health.Tone == HealthOK:
			ov.Channels.Usable++
		case health.Label == "未设 Key":
			ov.Channels.NoKey++
		}
		ov.ChannelBriefs = append(ov.ChannelBriefs, OverviewChannel{Key: ch.Key, Name: ch.Name, MaxRunning: ch.RateLimit.MaxRunning, Health: health})
	}
	ov.Models.Total = len(c.models)
	for _, m := range c.cModels {
		switch ModelHealthOf(m, c.cChannels, c.cPlugins).Status {
		case ModelOnline:
			ov.Models.Online++
		case ModelBroken:
			ov.Models.Broken++
		case ModelPending:
			ov.Models.Pending++
		}
	}
	ov.Todos = OverviewTodos(c.cModels, c.cChannels, c.cPlugins)
	return ov, nil
}
