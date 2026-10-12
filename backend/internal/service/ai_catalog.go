package service

import (
	"fmt"
	"strconv"
	"strings"
)

// 本文件是“渠道 / 插件 / 模型”三类配置的跨表状态规则。规则与前端 web/src/utils/admin/health.ts、plugin.ts
// 逐条一致（两边用同一组用例对照），前端不再自己算状态。纯函数，不碰数据库。

// CatalogVersion 是状态判定用的插件版本：版本号、鉴权类型、声明支持的生成方式。
type CatalogVersion struct {
	ID       uint64
	Version  string
	AuthType string   // meta.auth.type（meta 里没写按 none）；读不到 meta 时为空串，表示“未知”，不断言缺 Key
	Kinds    []string // meta.endpoints 的键（text / video / image / audio）
}

// CatalogPlugin 是状态判定用的插件。
type CatalogPlugin struct {
	Key      string
	Name     string
	Enabled  bool
	Versions []CatalogVersion
}

// CatalogChannel 是状态判定用的渠道。
type CatalogChannel struct {
	Key             string
	Name            string
	PluginKey       string
	PluginVersionID uint64
	PluginVersion   string
	Enabled         bool
	SecretSet       bool
}

// CatalogModel 是状态判定用的模型。
type CatalogModel struct {
	Key     string
	Kind    string
	Channel string // 绑定的渠道 key；没选为空串
	Enabled bool
}

// 渠道健康色调。
const (
	HealthOK   = "ok"
	HealthBad  = "bad"
	HealthWarn = "warn"
	HealthOff  = "off"
)

// 模型对运营露出的唯一状态。
const (
	ModelOnline  = "online"
	ModelBroken  = "broken"
	ModelOffline = "offline"
)

// ChannelHealth 是渠道实际能不能用：插件停用 / 不存在 / 缺 Key 都会让渠道不可用，即使渠道本身是“启用”。
type ChannelHealth struct {
	Tone   string `json:"tone"`   // ok / bad / warn / off
	Label  string `json:"label"`  // 列表里的短标签
	Reason string `json:"reason"` // 不可用的原因（整句）；可用时为空串
}

// ModelHealth 是模型对用户的真实状态。Reason 是渠道层面的问题：“未上线”的模型也可能有，
// 界面据此提示“上线前要先处理”；没有问题为空串。
type ModelHealth struct {
	Status string `json:"status"` // online / broken / offline
	Reason string `json:"reason"`
}

func findCatalogPlugin(plugins []CatalogPlugin, key string) *CatalogPlugin {
	for i := range plugins {
		if plugins[i].Key == key {
			return &plugins[i]
		}
	}
	return nil
}

// versionOf 按版本 id（优先）或版本号找渠道固定的插件版本。
func versionOf(p *CatalogPlugin, id uint64, version string) *CatalogVersion {
	if p == nil {
		return nil
	}
	if id != 0 {
		for i := range p.Versions {
			if p.Versions[i].ID == id {
				return &p.Versions[i]
			}
		}
	}
	if version != "" {
		for i := range p.Versions {
			if p.Versions[i].Version == version {
				return &p.Versions[i]
			}
		}
	}
	return nil
}

// ChannelHealthOf 判定渠道的健康状态，顺序：已停用 → 插件不存在 → 插件停用 → 缺 Key → 可用。
// 插件版本读不到（版本已删、meta 损坏）时不断言缺 Key。
func ChannelHealthOf(c CatalogChannel, plugins []CatalogPlugin) ChannelHealth {
	if !c.Enabled {
		return ChannelHealth{Tone: HealthOff, Label: "已停用", Reason: "渠道已停用"}
	}
	p := findCatalogPlugin(plugins, c.PluginKey)
	if p == nil {
		return ChannelHealth{Tone: HealthBad, Label: "不可用", Reason: fmt.Sprintf("插件 %s 不存在", c.PluginKey)}
	}
	if !p.Enabled {
		return ChannelHealth{Tone: HealthBad, Label: "不可用", Reason: fmt.Sprintf("插件「%s」已停用", p.Name)}
	}
	if v := versionOf(p, c.PluginVersionID, c.PluginVersion); v != nil && v.AuthType != "" && v.AuthType != "none" && !c.SecretSet {
		return ChannelHealth{Tone: HealthWarn, Label: "未设 Key", Reason: "还没有设置 Key"}
	}
	return ChannelHealth{Tone: HealthOK, Label: "可用"}
}

// ModelHealthOf 判定模型状态：
//   - 未上线：没启用，用户看不到；
//   - 不可用：已启用，但渠道不存在 / 停用 / 插件停用 / 缺 Key，用户实际用不了；
//   - 在线：其余。
//
// 渠道层面的问题（Reason）在任何状态下都会带上。
func ModelHealthOf(m CatalogModel, channels []CatalogChannel, plugins []CatalogPlugin) ModelHealth {
	reason := ""
	if m.Channel == "" {
		reason = "还没有选择渠道"
	} else if ch := findCatalogChannel(channels, m.Channel); ch == nil {
		reason = fmt.Sprintf("渠道 %s 不存在", m.Channel)
	} else if h := ChannelHealthOf(*ch, plugins); h.Reason != "" {
		reason = fmt.Sprintf("渠道「%s」%s", ch.Name, h.Reason)
	}
	if !m.Enabled {
		return ModelHealth{Status: ModelOffline, Reason: reason}
	}
	if reason != "" {
		return ModelHealth{Status: ModelBroken, Reason: reason}
	}
	return ModelHealth{Status: ModelOnline}
}

func findCatalogChannel(channels []CatalogChannel, key string) *CatalogChannel {
	for i := range channels {
		if channels[i].Key == key {
			return &channels[i]
		}
	}
	return nil
}

// compareSemver 比较两个 semver 版本号（只比 主.次.修订，预发布后缀忽略）；a 大返回正数。
func compareSemver(a, b string) int {
	parse := func(s string) []int {
		core, _, _ := strings.Cut(s, "-")
		parts := strings.Split(core, ".")
		out := make([]int, len(parts))
		for i, p := range parts {
			out[i], _ = strconv.Atoi(p)
		}
		return out
	}
	l, r := parse(a), parse(b)
	for i := 0; i < max(len(l), len(r)); i++ {
		var x, y int
		if i < len(l) {
			x = l[i]
		}
		if i < len(r) {
			y = r[i]
		}
		if x != y {
			return x - y
		}
	}
	return 0
}

// AvailableUpgrade 返回插件里比渠道当前固定版本更新的最高版本号；没有更新返回空串。
func AvailableUpgrade(p CatalogPlugin, current string) string {
	best := ""
	for _, v := range p.Versions {
		if compareSemver(v.Version, current) <= 0 {
			continue
		}
		if best == "" || compareSemver(v.Version, best) > 0 {
			best = v.Version
		}
	}
	return best
}

// MetaSupportsKind 判断插件版本能不能承接某种模型。agent 不走插件钩子（由后端网关直接请求渠道的
// OpenAI 兼容接口），只要求 auth.type 是 bearer；其余种类要在 endpoints 里声明。
func MetaSupportsKind(v CatalogVersion, kind string) bool {
	if kind == "agent" {
		return v.AuthType == "bearer"
	}
	for _, k := range v.Kinds {
		if k == kind {
			return true
		}
	}
	return false
}
