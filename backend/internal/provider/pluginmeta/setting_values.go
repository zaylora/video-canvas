// 本文件：按 channelSettings / import.args 的声明校验“取值”（渠道 settings、导入参数），与声明本身的预检（validate_setting.go）相对。

package pluginmeta

import (
	"fmt"
	"sort"
	"strings"

	"video-canvas/internal/provider/modelcfg"
)

// ValidateSettingValues 按 schema 校验一份取值并返回补全默认值后的副本：
//   - 取值里出现 schema 没声明的名字：报错（拼错名字比悄悄忽略更容易被发现）；
//   - 类型必须匹配：string / number / boolean / enum（enum 必须是 options 之一）；null 视同没填；
//   - 没填的项：有 default 就补上 default；没有 default 且 required（字符串 required 时空串也算没填）报错。
//
// Issue.Path 是设置项名字。values 为 nil 视为空对象；schema 为空时任何取值都会因“未声明”被拒绝（空对象通过）。
// 返回的 map 永远不是 nil，可以直接序列化成 JSON 对象。
func ValidateSettingValues(schema SettingSchema, values map[string]any) (map[string]any, []modelcfg.Issue) {
	out := make(map[string]any, len(schema))
	var issues []modelcfg.Issue
	add := func(name, format string, args ...any) {
		issues = append(issues, modelcfg.Issue{Path: name, Message: fmt.Sprintf(format, args...)})
	}

	// 1. 未声明的名字。按名字排序，报错顺序稳定
	for _, name := range sortedValueKeys(values) {
		if _, ok := schema.Get(name); !ok {
			add(name, "未声明的设置项")
		}
	}
	// 2. 逐项按声明校验，按声明顺序（也是表单顺序）
	for _, e := range schema {
		v, present := values[e.Name]
		if present && v != nil {
			if !settingTypeMatches(e.Setting, v) {
				add(e.Name, "%s", settingTypeHint(e.Setting))
				continue
			}
			if s, isStr := v.(string); isStr && e.Required && strings.TrimSpace(s) == "" {
				add(e.Name, "不能为空")
				continue
			}
			out[e.Name] = v
			continue
		}
		if e.Default != nil {
			out[e.Name] = e.Default
			continue
		}
		if e.Required {
			add(e.Name, "必填")
		}
	}
	return out, issues
}

// settingTypeMatches 判断取值是否符合声明的类型；数字接受 JSON 解码出的 float64 和常见的整数类型。
func settingTypeMatches(s Setting, v any) bool {
	switch s.Type {
	case SettingString:
		_, ok := v.(string)
		return ok
	case SettingNumber:
		switch v.(type) {
		case float64, float32, int, int32, int64, uint, uint32, uint64:
			return true
		}
		return false
	case SettingBoolean:
		_, ok := v.(bool)
		return ok
	case SettingEnum:
		str, ok := v.(string)
		return ok && contains(s.Options, str)
	}
	return false
}

// settingTypeHint 给类型不匹配一句能直接改的提示。
func settingTypeHint(s Setting) string {
	switch s.Type {
	case SettingString:
		return "应为字符串"
	case SettingNumber:
		return "应为数字"
	case SettingBoolean:
		return "应为 true / false"
	case SettingEnum:
		return "必须是 " + strings.Join(s.Options, " / ") + " 之一"
	}
	return "类型不合法"
}

// sortedValueKeys 返回取值里的名字，升序。
func sortedValueKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
