package service

import (
	"fmt"
	"strconv"
	"strings"
)

// 本文件是“渠道 / 插件 / 模型”三类配置的跨表状态规则。规则与前端 web/src/utils/admin/health.ts、plugin.ts
// 逐条一致（两边用同一组用例对照），前端不再自己算状态。纯函数，不碰数据库。
// 渠道只管连接信息，协议在模型里选：渠道健康只看启停与 Key；模型状态要同时看渠道和它选的协议插件。

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
	Key       string
	Name      string
	Enabled   bool
	SecretSet bool // API Key 已设置
}

// CatalogModel 是状态判定用的模型。
type CatalogModel struct {
	Key           string
	Kind          string // 没选能力（待配置）为空串
	Channel       string // 绑定的渠道 key；没选为空串
	Enabled       bool
	Plugin        string // 协议的插件 key；没选（待配置 / Agent）为空串
	PluginVersion string // 协议固定的插件版本号
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
	ModelPending = "pending" // 待配置：还没选能力或协议（拉取得到的模型的初始状态），没启用时才算
)

// ChannelHealth 是渠道实际能不能用：停用、没设 Key 都会让渠道不可用。
type ChannelHealth struct {
	Tone   string `json:"tone"`   // ok / bad / warn / off
	Label  string `json:"label"`  // 列表里的短标签
	Reason string `json:"reason"` // 不可用的原因（整句）；可用时为空串
}

// ModelHealth 是模型对用户的真实状态。Reason 是渠道或协议层面的问题：“未上线”的模型也可能有，
// 界面据此提示“上线前要先处理”；没有问题为空串。
type ModelHealth struct {
	Status string `json:"status"` // online / broken / offline / pending
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

// versionByNumber 按版本号找插件版本。
func versionByNumber(p *CatalogPlugin, version string) *CatalogVersion {
	if p == nil {
		return nil
	}
	for i := range p.Versions {
		if p.Versions[i].Version == version {
			return &p.Versions[i]
		}
	}
	return nil
}

// ChannelHealthOf 判定渠道的健康状态，顺序：已停用 → 缺 Key → 可用。
// 有的协议不需要 Key（auth: none），但渠道不认识协议，统一按“要有 Key”提示；真正的检查在模型启用时按协议做。
func ChannelHealthOf(c CatalogChannel) ChannelHealth {
	if !c.Enabled {
		return ChannelHealth{Tone: HealthOff, Label: "已停用", Reason: "渠道已停用"}
	}
	if !c.SecretSet {
		return ChannelHealth{Tone: HealthWarn, Label: "未设 Key", Reason: "还没有设置 Key"}
	}
	return ChannelHealth{Tone: HealthOK, Label: "可用"}
}

// isPending 判断模型是不是“待配置”：没选能力，或者非 Agent 模型没选协议。
func isPending(m CatalogModel) bool {
	return m.Kind == "" || (m.Kind != "agent" && m.Plugin == "")
}

// ModelHealthOf 判定模型状态：
//   - 待配置：没启用，且还没选能力或协议；
//   - 未上线：没启用，用户看不到；
//   - 不可用：已启用，但渠道不存在 / 停用 / 缺 Key，或协议插件停用 / 版本缺失 / 不支持该能力，用户实际用不了；
//   - 在线：其余。
//
// 渠道与协议层面的问题（Reason）在任何状态下都会带上。
func ModelHealthOf(m CatalogModel, channels []CatalogChannel, plugins []CatalogPlugin) ModelHealth {
	reason := modelProblem(m, channels, plugins)
	switch {
	case !m.Enabled && isPending(m):
		return ModelHealth{Status: ModelPending, Reason: reason}
	case !m.Enabled:
		return ModelHealth{Status: ModelOffline, Reason: reason}
	case reason != "":
		return ModelHealth{Status: ModelBroken, Reason: reason}
	}
	return ModelHealth{Status: ModelOnline}
}

// modelProblem 返回模型当前用不了的原因（整句），没有问题为空串。顺序：渠道 → 能力 / 协议 → 插件 → Key。
func modelProblem(m CatalogModel, channels []CatalogChannel, plugins []CatalogPlugin) string {
	if m.Channel == "" {
		return "还没有选择渠道"
	}
	ch := findCatalogChannel(channels, m.Channel)
	if ch == nil {
		return fmt.Sprintf("渠道 %s 不存在", m.Channel)
	}
	if !ch.Enabled {
		return fmt.Sprintf("渠道「%s」已停用", ch.Name)
	}
	if m.Kind == "" {
		return "还没有选择能力"
	}
	if m.Kind == "agent" {
		// Agent 不走插件：网关直接用渠道 Key 请求
		if !ch.SecretSet {
			return fmt.Sprintf("渠道「%s」还没有设置 Key", ch.Name)
		}
		return ""
	}
	if m.Plugin == "" {
		return "还没有选择调用协议"
	}
	p := findCatalogPlugin(plugins, m.Plugin)
	if p == nil {
		return fmt.Sprintf("协议插件 %s 不存在", m.Plugin)
	}
	if !p.Enabled {
		return fmt.Sprintf("协议插件「%s」已停用", p.Name)
	}
	v := versionByNumber(p, m.PluginVersion)
	if v == nil {
		return fmt.Sprintf("固定的插件版本 %s@%s 不存在", m.Plugin, m.PluginVersion)
	}
	if !MetaSupportsKind(*v, m.Kind) {
		return fmt.Sprintf("协议「%s」不支持%s类型的模型", p.Name, m.Kind)
	}
	if v.AuthType != "" && v.AuthType != "none" && !ch.SecretSet {
		return fmt.Sprintf("渠道「%s」还没有设置 Key", ch.Name)
	}
	return ""
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

// AvailableUpgrade 返回插件里比模型当前固定版本更新的最高版本号；没有更新返回空串。
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

// MetaSupportsKind 判断插件版本能不能承接某种模型：要在 endpoints 里声明。
// Agent 模型不走插件（由后端网关直接请求渠道的 OpenAI 兼容接口），所以任何插件都不承接 agent。
func MetaSupportsKind(v CatalogVersion, kind string) bool {
	if kind == "agent" {
		return false
	}
	for _, k := range v.Kinds {
		if k == kind {
			return true
		}
	}
	return false
}
