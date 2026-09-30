// 本文件：channelSettings / import.args 设置项声明的预检（保留书写顺序，管理端按此顺序渲染表单）。

package pluginmeta

import (
	"bytes"
	"encoding/json"
	"strings"
)

// rawEntry 是按书写顺序解出的一个对象成员。
type rawEntry struct {
	name string
	raw  json.RawMessage
}

// settings 按书写顺序解码并检查设置项声明；字段不存在返回 nil。
func (v *validator) settings(path string, raw json.RawMessage) SettingSchema {
	if len(raw) == 0 || isNull(raw) {
		return nil
	}
	entries, ok := orderedEntries(raw)
	if !ok {
		v.add(path, "类型不对，应为对象")
		return nil
	}
	out := SettingSchema{}
	for _, e := range entries {
		p := path + "." + e.name
		if !settingNameRe.MatchString(e.name) {
			v.add(p, "名字只能包含字母、数字和下划线，以字母或下划线开头，最长 32 个字符")
			continue
		}
		if s, ok := v.setting(p, e.raw); ok {
			out = append(out, SettingEntry{Name: e.name, Setting: s})
		}
	}
	return out
}

// setting 检查一个设置项：type 是枚举值、label 非空、enum 有非空 options、default 与类型匹配。
func (v *validator) setting(path string, raw json.RawMessage) (Setting, bool) {
	obj, ok := v.object(raw, path)
	if !ok {
		if isNull(raw) {
			v.add(path, "类型不对，应为对象")
		}
		return Setting{}, false
	}
	var s Setting
	v.field(obj, path, "type", &s.Type)
	v.field(obj, path, "label", &s.Label)
	v.field(obj, path, "description", &s.Description)
	v.field(obj, path, "required", &s.Required)
	v.field(obj, path, "options", &s.Options)
	if isPresent(obj, "default") {
		// 外层已按 JSON 解过，这里解到 any 不会失败
		_ = json.Unmarshal(obj["default"], &s.Default)
	}
	if strings.TrimSpace(s.Label) == "" {
		v.add(path+".label", "不能为空")
	}
	switch s.Type {
	case SettingString, SettingNumber, SettingBoolean:
	case SettingEnum:
		v.enumOptions(path, s.Options)
	default:
		v.add(path+".type", "必须是 %s / %s / %s / %s 之一", SettingString, SettingNumber, SettingBoolean, SettingEnum)
		return s, true
	}
	if s.Default != nil && !defaultMatches(s) {
		v.add(path+".default", "默认值与类型 %s 不匹配", s.Type)
	}
	return s, true
}

// enumOptions 检查 enum 的可选值：非空、每项非空、不重复。
func (v *validator) enumOptions(path string, options []string) {
	if len(options) == 0 {
		v.add(path+".options", "enum 必须提供非空的 options")
		return
	}
	seen := map[string]bool{}
	for _, o := range options {
		if strings.TrimSpace(o) == "" {
			v.add(path+".options", "可选值不能为空字符串")
			return
		}
		if seen[o] {
			v.add(path+".options", "可选值 %q 重复", o)
			return
		}
		seen[o] = true
	}
}

// defaultMatches 判断默认值是否与声明的类型一致；enum 的默认值必须在 options 里。
func defaultMatches(s Setting) bool {
	switch s.Type {
	case SettingString:
		_, ok := s.Default.(string)
		return ok
	case SettingNumber:
		_, ok := s.Default.(float64)
		return ok
	case SettingBoolean:
		_, ok := s.Default.(bool)
		return ok
	case SettingEnum:
		d, ok := s.Default.(string)
		return ok && contains(s.Options, d)
	}
	return false
}

// orderedEntries 按书写顺序拆开一个 JSON 对象；不是对象返回 false。
func orderedEntries(raw json.RawMessage) ([]rawEntry, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if d, ok := tok.(json.Delim); err != nil || !ok || d != '{' {
		return nil, false
	}
	var out []rawEntry
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, false
		}
		name, _ := kt.(string)
		var r json.RawMessage
		if err := dec.Decode(&r); err != nil {
			return nil, false
		}
		out = append(out, rawEntry{name: name, raw: r})
	}
	return out, true
}
