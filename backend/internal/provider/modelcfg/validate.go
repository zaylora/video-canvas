// 本文件：模型配置（ModelConfig）的解析与语义校验：先做结构检查（未知字段、类型不符），再补默认值，
// 最后逐项检查 key / kind / label / deadline / channels / capabilities / pricing，所有问题一次报出。
// 能力（capabilities）的校验在 validate_caps.go。

package modelcfg

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	modelKeyRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
	vendorRe     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	channelKeyRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	pluginKeyRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,29}$`)
	semverRe     = regexp.MustCompile(`^[0-9]+.[0-9]+.[0-9]+(-[0-9A-Za-z.-]+)?$`)
)

const (
	// maxModelDeadline 是模型任务超时的上限。
	maxModelDeadline = 24 * time.Hour
	// minIdleTimeout / maxIdleTimeout 是 Agent 模型「上游响应超时」的合法范围：
	// 太短会把正常的推理等待当成故障，太长则用户要对着空白等很久才知道失败。
	minIdleTimeout = 10 * time.Second
	maxIdleTimeout = 10 * time.Minute
	// maxUpstreamModelLen 是上游模型名的最大字符数。
	maxUpstreamModelLen = 128
	// maxChannels 是首期每个模型允许绑定的渠道数。数据结构是数组，预留多渠道，但首期只接受一个。
	maxChannels = 1
	// maxHintLen 是模型描述的最大字符数。
	maxHintLen = 500
	// maxTags 是展示标签的最大个数，maxTagLen 是单个标签的最大字符数。
	maxTags   = 5
	maxTagLen = 12
)

func inStrings(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func oneOfMsg(list []string) string { return "必须是 " + strings.Join(list, " / ") + " 之一" }

// parseModel 解析并校验模型配置正文，实现见 ParseModel。
func parseModel(body []byte) (*ModelConfig, []Issue) {
	tree, iss := decodeTree(body)
	if iss != nil {
		return nil, []Issue{*iss}
	}
	// 模型早先有顶层固定参数 params，现在完全由 capabilities 描述、不再有这个字段；
	// 已保存的旧配置里还带着它，这里不当未知字段报错（解码时也会被忽略），否则这些模型会读不出来。
	delete(tree, "params")
	var issues []Issue
	checkShape("", tree, reflect.TypeOf(ModelConfig{}), &issues)
	if len(issues) > 0 {
		return nil, issues
	}
	var cfg ModelConfig
	if err := decodeInto(body, &cfg); err != nil {
		return nil, []Issue{{Message: "配置解析失败：" + err.Error()}}
	}
	cfg.normalize()
	if !deadlineExplicit(tree) {
		cfg.Deadline = Duration(DefaultModelDeadline)
	}
	validateModel(&cfg, &issues)
	if len(issues) > 0 {
		return nil, issues
	}
	return &cfg, nil
}

// deadlineExplicit 判断正文里是否明确写了 deadline。省略、null、空字符串都算“没写”，会取默认的 30m；
// 明确写了就必须大于 0（写 "0s" 是错误，而不是“用默认值”）。
func deadlineExplicit(tree map[string]any) bool {
	v, ok := tree["deadline"]
	if !ok || v == nil {
		return false
	}
	s, isStr := v.(string)
	return !isStr || s != ""
}

// normalize 规范化 any 字段里的数字（default、选项值）。
func (m *ModelConfig) normalize() {
	for i := range m.Capabilities.Params {
		f := &m.Capabilities.Params[i].ParamField
		f.Default = normalizeConfigNumber(f.Default)
		for j := range f.Options {
			f.Options[j] = normalizeConfigNumber(f.Options[j])
		}
	}
	for i := range m.Pricing.Tiers {
		if w := m.Pricing.Tiers[i].When; w != nil {
			m.Pricing.Tiers[i].When = normalizeConfigNumber(w).(map[string]any)
		}
	}
}

// validateModel 校验模型配置的各个字段（deadline 的默认值已在调用前补好）。
func validateModel(m *ModelConfig, issues *[]Issue) {
	add := func(path, msg string) { *issues = append(*issues, Issue{Path: path, Message: msg}) }

	if !modelKeyRe.MatchString(m.Key) {
		add("key", "必填，只能包含字母、数字、下划线、点和连字符，且以字母或数字开头（最长 128 位）")
	}
	if !inStrings(Kinds, m.Kind) {
		add("kind", oneOfMsg(Kinds))
	}
	if strings.TrimSpace(m.Label) == "" {
		add("label", "不能为空")
	}
	if n := utf8.RuneCountInString(m.Hint); n > maxHintLen {
		add("hint", fmt.Sprintf("不能超过 %d 个字符（当前 %d 个）", maxHintLen, n))
	}
	if m.Vendor != "" && !vendorRe.MatchString(m.Vendor) {
		add("vendor", "只能包含小写字母、数字和连字符，且以字母或数字开头（最长 64 位）")
	}
	validateTags(m.Tags, add)
	if m.Deadline <= 0 || m.Deadline.D() > maxModelDeadline {
		add("deadline", "必须大于 0 且不超过 24h（不填默认 30m）")
	}
	validateIdleTimeout(m, add)
	validateChannels(m.Kind, m.Channels, add)
	validateCapabilities(m.Kind, &m.Capabilities, issues)
	if m.Kind == KindAgent && m.Capabilities.Video && (len(m.Channels) != 1 || m.Channels[0].Format() != APIFormatGemini) {
		*issues = append(*issues, Issue{Path: "capabilities.video", Message: "只有 Gemini 接口格式的模型能看视频"})
	}
	validatePricing(m.Kind, &m.Capabilities, &m.Pricing, issues)
}

// validateIdleTimeout 校验 Agent 模型的上游响应超时：只有 Agent 模型走 LLM 网关，别的种类写了也不会生效，直接拒绝免得误导；0 表示用平台默认值。
func validateIdleTimeout(m *ModelConfig, add func(path, msg string)) {
	if m.IdleTimeout == 0 {
		return
	}
	if m.Kind != KindAgent {
		add("idle_timeout", "只有 Agent 模型可以配置上游响应超时")
		return
	}
	if d := m.IdleTimeout.D(); d < minIdleTimeout || d > maxIdleTimeout {
		add("idle_timeout", "必须在 10s – 10m 之间（不填用平台默认 90s）")
	}
}

// validateTags 校验展示标签：个数、单个长度、不能为空、不能重复。
func validateTags(tags []string, add func(path, msg string)) {
	if len(tags) > maxTags {
		add("tags", fmt.Sprintf("最多 %d 个标签（当前 %d 个）", maxTags, len(tags)))
	}
	seen := map[string]bool{}
	for i, t := range tags {
		path := fmt.Sprintf("tags[%d]", i)
		switch n := utf8.RuneCountInString(t); {
		case strings.TrimSpace(t) == "":
			add(path, "不能为空")
		case n > maxTagLen:
			add(path, fmt.Sprintf("不能超过 %d 个字符（当前 %d 个）", maxTagLen, n))
		case seen[t]:
			add(path, fmt.Sprintf("标签 %q 重复", t))
		}
		seen[t] = true
	}
}

// validateChannels 校验 channels：首期恰好一个元素，每个元素的渠道 key、上游模型名、协议与接口格式合法。
// 不检查渠道是否存在、插件是否存在或支持该 kind：那是 service 层的跨对象检查。
func validateChannels(kind string, list []ChannelRef, add func(path, msg string)) {
	switch {
	case len(list) == 0:
		add("channels", fmt.Sprintf("必须绑定 %d 个渠道（首期只支持一个渠道）", maxChannels))
	case len(list) > maxChannels:
		add("channels", fmt.Sprintf("首期只支持一个渠道，当前填了 %d 个", len(list)))
	}
	for i, c := range list {
		base := fmt.Sprintf("channels[%d]", i)
		if !channelKeyRe.MatchString(c.Channel) {
			add(base+".channel", "必填，渠道 key 只能包含小写字母、数字和连字符，且以字母或数字开头（最长 64 位）")
		}
		switch n := utf8.RuneCountInString(c.UpstreamModel); {
		case strings.TrimSpace(c.UpstreamModel) == "":
			add(base+".upstream_model", "不能为空，填写这个渠道上的上游模型名")
		case n > maxUpstreamModelLen:
			add(base+".upstream_model", fmt.Sprintf("不能超过 %d 个字符（当前 %d 个）", maxUpstreamModelLen, n))
		}
		validateProtocol(kind, c.Protocol, base+".protocol", add)
		switch {
		case kind != KindAgent && c.APIFormat != "":
			add(base+".api_format", "只有 Agent 模型可以选择接口格式")
		case kind == KindAgent && c.Format() != APIFormatOpenAI && c.Format() != APIFormatGemini:
			add(base+".api_format", "接口格式只能是 openai 或 gemini")
		}
	}
}

// validateProtocol 校验模型选的调用协议：Agent 模型不走插件，不能有；其他种类必须有，插件 key 与版本号格式合法。
// kind 不合法时（已由 kind 校验报告）不再重复报协议缺失。
func validateProtocol(kind string, p *Protocol, path string, add func(path, msg string)) {
	if kind == KindAgent {
		if p != nil {
			add(path, "Agent 模型由宿主的 LLM 网关直接请求渠道，不需要选择协议")
		}
		return
	}
	if p == nil {
		if inStrings(Kinds, kind) {
			add(path, "必须选择调用协议（插件、版本与协议设置）")
		}
		return
	}
	if !pluginKeyRe.MatchString(p.Plugin) {
		add(path+".plugin", "必填，插件 key 只能包含小写字母、数字和连字符，且以字母或数字开头（最长 30 位）")
	}
	if !semverRe.MatchString(p.Version) {
		add(path+".version", "必填，写成 semver，例如 1.0.0")
	}
}
