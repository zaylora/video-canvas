// 本文件：modelcfg 内部与插件宿主共用的小工具：数字转换、文本化、JSON 数字规范化。

package modelcfg

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
)

func isMediaType(t string) bool { return t == FieldImage || t == FieldVideo || t == FieldAudio }

func toFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int8:
		return float64(t), true
	case int16:
		return float64(t), true
	case int32:
		return float64(t), true
	case int64:
		return float64(t), true
	case uint:
		return float64(t), true
	case uint8:
		return float64(t), true
	case uint16:
		return float64(t), true
	case uint32:
		return float64(t), true
	case uint64:
		return float64(t), true
	case json.Number:
		f, err := t.Float64()
		return f, err == nil
	}
	return 0, false
}

// Stringify 把任意 JSON 值转成文本（如查询参数、日志）：
// nil -> ""；float64 整数不带小数点和科学计数法；对象 / 数组 -> JSON。
func Stringify(v any) string { return stringify(v) }

func stringify(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(t), 'f', -1, 32)
	case json.Number:
		return t.String()
	case fmt.Stringer:
		return t.String()
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(rv.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(rv.Uint(), 10)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// NormalizeJSON 把 UseNumber 解出来的 json.Number 转成 int（放得下的整数）或 float64，
// 这样既方便后续按普通数字使用，又不丢失大整数精度。会原地修改 map 与 slice。
func NormalizeJSON(v any) any {
	switch t := v.(type) {
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return int(i)
		}
		f, err := t.Float64()
		if err != nil {
			return t.String()
		}
		return f
	case map[string]any:
		for k, x := range t {
			t[k] = NormalizeJSON(x)
		}
		return t
	case []any:
		for i, x := range t {
			t[i] = NormalizeJSON(x)
		}
		return t
	}
	return v
}

// normalizeConfigNumber 把配置树里的 json.Number 规范化：
// 落在 ±2^53 内的整数与小数变成 float64（与标准库默认行为一致），更大的整数保留为 int，避免 ID 类数字丢精度。
func normalizeConfigNumber(v any) any {
	switch t := v.(type) {
	case json.Number:
		if i, err := t.Int64(); err == nil {
			if i > 1<<53 || i < -(1<<53) {
				return int(i)
			}
			return float64(i)
		}
		f, err := t.Float64()
		if err != nil {
			return t.String()
		}
		return f
	case map[string]any:
		for k, x := range t {
			t[k] = normalizeConfigNumber(x)
		}
		return t
	case []any:
		for i, x := range t {
			t[i] = normalizeConfigNumber(x)
		}
		return t
	}
	return v
}
