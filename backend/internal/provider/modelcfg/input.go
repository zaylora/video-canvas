// 本文件：用户输入校验：按 input_schema 检查并规范化用户提交的字段（文本 / 数字 / 布尔 / 枚举 / 媒体资源），
// 一次返回全部字段级错误；另提供媒体字段名提取（MediaFieldNames）。

package modelcfg

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// maxSafeAssetID 是 float64 能精确表示的最大整数（2^53）；更大的 asset id 必须以字符串或 json.Number 传入。
const maxSafeAssetID = 1 << 53

// validateInput 按 input_schema 校验并规范化用户输入。
// 规范化结果只包含 schema 里声明的字段：text -> string，number -> float64，boolean -> bool，
// enum -> 选项里声明的值（数字选项统一为 float64），媒体 -> uint64（asset id）。
func validateInput(schema InputSchema, input map[string]any) (map[string]any, []FieldError) {
	out := make(map[string]any, len(schema))
	var errs []FieldError
	for _, e := range schema {
		label := e.Label
		if label == "" {
			label = e.Name
		}
		fail := func(msg string) { errs = append(errs, FieldError{Field: e.Name, Message: msg}) }

		raw, present := input[e.Name]
		if !present || raw == nil || isBlankString(e.Type, raw) {
			// 没传：先用默认值，再看是否必填
			if e.Default == nil {
				if e.Required {
					fail(label + " 不能为空")
				}
				continue
			}
			raw = e.Default
		}

		v, msg := normalizeField(e.InputField, label, raw)
		if msg != "" {
			fail(msg)
			continue
		}
		out[e.Name] = v
	}
	if len(errs) > 0 {
		return nil, errs
	}
	return out, nil
}

// isBlankString 文本 / 媒体字段传了空白字符串等同于没传（前端表单清空文本框时会传 ""）。
func isBlankString(fieldType string, raw any) bool {
	if fieldType != FieldText && !isMediaType(fieldType) {
		return false
	}
	s, ok := raw.(string)
	return ok && strings.TrimSpace(s) == ""
}

// normalizeField 校验并规范化单个字段的值，返回中文错误文案（空表示通过）。
func normalizeField(f InputField, label string, raw any) (any, string) {
	switch f.Type {
	case FieldText:
		return normalizeText(f, label, raw)
	case FieldNumber:
		return normalizeNumber(f, label, raw)
	case FieldBoolean:
		return normalizeBool(label, raw)
	case FieldEnum:
		return normalizeEnum(f, label, raw)
	case FieldImage, FieldVideo, FieldAudio:
		id, ok := asAssetID(raw)
		if !ok {
			return nil, label + " 必须是有效的素材 ID"
		}
		return id, ""
	}
	return nil, label + " 的类型不受支持"
}

func normalizeText(f InputField, label string, raw any) (any, string) {
	s, ok := raw.(string)
	if !ok {
		return nil, label + " 必须是文本"
	}
	if f.MaxLength > 0 && len([]rune(s)) > f.MaxLength {
		return nil, fmt.Sprintf("%s 不能超过 %d 个字符", label, f.MaxLength)
	}
	return s, ""
}

func normalizeNumber(f InputField, label string, raw any) (any, string) {
	n, ok := asNumber(raw)
	if !ok {
		return nil, label + " 必须是数字"
	}
	if f.Min != nil && n < *f.Min {
		return nil, fmt.Sprintf("%s 不能小于 %s", label, stringify(*f.Min))
	}
	if f.Max != nil && n > *f.Max {
		return nil, fmt.Sprintf("%s 不能大于 %s", label, stringify(*f.Max))
	}
	return n, ""
}

// normalizeBool 接受布尔值，以及表单常见的 "true" / "false" 字符串。
func normalizeBool(label string, raw any) (any, string) {
	switch t := raw.(type) {
	case bool:
		return t, ""
	case string:
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "true":
			return true, ""
		case "false":
			return false, ""
		}
	}
	return nil, label + " 必须是 true 或 false"
}

func normalizeEnum(f InputField, label string, raw any) (any, string) {
	opt, ok := matchOption(f.Options, raw)
	if !ok {
		labels := make([]string, 0, len(f.Options))
		for _, o := range f.Options {
			labels = append(labels, o.Label)
		}
		return nil, fmt.Sprintf("%s 必须是 %s 之一", label, strings.Join(labels, " / "))
	}
	// 数字选项统一成 float64，避免下游（插件的 ctx.input）拿到 int / float64 混杂的类型
	if n, isNum := toFloat(opt.Value); isNum {
		return n, ""
	}
	return opt.Value, ""
}

// asNumber 接受 float64 / 各种整型 / json.Number，拒绝 NaN、Inf 与字符串。
func asNumber(raw any) (float64, bool) {
	n, ok := toFloat(raw)
	if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, false
	}
	return n, true
}

// asAssetID 把 JSON 数字、float64、json.Number、数字字符串规范成 uint64 素材 id；0 与负数无效。
func asAssetID(raw any) (uint64, bool) {
	switch t := raw.(type) {
	case uint64:
		return t, t > 0
	case uint:
		return uint64(t), t > 0
	case uint32:
		return uint64(t), t > 0
	case int:
		return uint64(t), t > 0
	case int32:
		return uint64(t), t > 0
	case int64:
		return uint64(t), t > 0
	case float64:
		// float64 超过 2^53 已经不能精确表示整数，直接拒绝，避免悄悄指向别的素材
		if t <= 0 || t != math.Trunc(t) || t > maxSafeAssetID {
			return 0, false
		}
		return uint64(t), true
	case float32:
		return asAssetID(float64(t))
	case json.Number:
		return parseAssetIDString(t.String())
	case string:
		return parseAssetIDString(t)
	}
	return 0, false
}

func parseAssetIDString(s string) (uint64, bool) {
	s = strings.TrimSpace(s)
	id, err := strconv.ParseUint(s, 10, 64)
	if err != nil || id == 0 {
		return 0, false
	}
	return id, true
}

// parseNumberString 解析数字字符串。
func parseNumberString(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}

// matchOption 在枚举选项里找与 raw 相等的那一项。数字按数值比较（5 与 5.0 相等，"5" 也视为 5），字符串按原文比较。
func matchOption(options []EnumOption, raw any) (EnumOption, bool) {
	rawNum, rawIsNum := toFloat(raw)
	rawStr, rawIsStr := raw.(string)
	if rawIsStr {
		if n, ok := parseNumberString(rawStr); ok {
			rawNum, rawIsNum = n, true
		}
	}
	for _, o := range options {
		if optNum, ok := toFloat(o.Value); ok {
			if rawIsNum && optNum == rawNum {
				return o, true
			}
			continue
		}
		optStr, ok := o.Value.(string)
		if !ok {
			continue
		}
		if rawIsStr && optStr == rawStr {
			return o, true
		}
		// 选项写成 "5" 而前端传了数字 5
		if n, ok := parseNumberString(optStr); ok && rawIsNum && n == rawNum {
			return o, true
		}
	}
	return EnumOption{}, false
}

// mediaFieldNames 返回 schema 中所有媒体字段（image / video / audio）的名字，保持书写顺序。
func mediaFieldNames(schema InputSchema) []string {
	var out []string
	for _, e := range schema {
		if isMediaType(e.Type) {
			out = append(out, e.Name)
		}
	}
	return out
}
