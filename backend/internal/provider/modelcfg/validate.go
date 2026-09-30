// 本文件：模型配置（ModelConfig）的解析与语义校验：先做结构检查（未知字段、类型不符），再补默认值，
// 最后逐项检查 key / kind / label / credits / deadline / channels，所有问题一次报出。
// 输入 schema 的校验在 validate_input.go。

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
	channelKeyRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
)

const (
	// maxModelDeadline 是模型任务超时的上限。
	maxModelDeadline = 24 * time.Hour
	// maxUpstreamModelLen 是上游模型名的最大字符数。
	maxUpstreamModelLen = 128
	// maxChannels 是首期每个模型允许绑定的渠道数。数据结构是数组，预留多渠道，但首期只接受一个。
	maxChannels = 1
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

// normalize 规范化 any 字段里的数字（params、default、选项值）。
func (m *ModelConfig) normalize() {
	if m.Params != nil {
		m.Params = normalizeConfigNumber(m.Params).(map[string]any)
	}
	for i := range m.InputSchema {
		f := &m.InputSchema[i].InputField
		f.Default = normalizeConfigNumber(f.Default)
		for j := range f.Options {
			f.Options[j].Value = normalizeConfigNumber(f.Options[j].Value)
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
	if m.Credits < 0 {
		add("credits", "不能为负数")
	}
	if m.Deadline <= 0 || m.Deadline.D() > maxModelDeadline {
		add("deadline", "必须大于 0 且不超过 24h（不填默认 30m）")
	}
	validateChannels(m.Channels, add)
	*issues = append(*issues, ValidateInputSchema(m.InputSchema)...)
}

// validateChannels 校验 channels：首期恰好一个元素，每个元素的渠道 key 与上游模型名合法。
// 不检查渠道是否存在、插件是否支持该 kind：那是 service 层的跨对象检查。
func validateChannels(list []ChannelRef, add func(path, msg string)) {
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
	}
}
