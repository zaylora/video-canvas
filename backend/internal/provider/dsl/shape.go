// 本文件：配置 JSON 的结构校验：解码成通用树后对照 Go 结构体检查未知字段、类型不符，
// 并给出精确到字段的 JSON 路径。

package dsl

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
)

var (
	durationType    = reflect.TypeOf(Duration(0))
	inputSchemaType = reflect.TypeOf(InputSchema(nil))
)

// decodeTree 把配置正文解码成通用 JSON 树；数字保留为 json.Number，避免大整数丢精度。
// 语法错误返回带位置的 Issue。
func decodeTree(body []byte) (any, *Issue) {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, &Issue{Message: "配置正文不能为空"}
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var tree any
	if err := dec.Decode(&tree); err != nil {
		var se *json.SyntaxError
		if errors.As(err, &se) {
			return nil, &Issue{Message: fmt.Sprintf("JSON 格式错误（第 %d 个字符附近）：%v", se.Offset, se)}
		}
		return nil, &Issue{Message: "JSON 格式错误：" + err.Error()}
	}
	if dec.More() {
		return nil, &Issue{Message: "JSON 格式错误：正文之后还有多余内容"}
	}
	if _, ok := tree.(map[string]any); !ok {
		return nil, &Issue{Message: "配置正文必须是 JSON 对象"}
	}
	return tree, nil
}

// checkShape 对照 Go 结构体检查 JSON 树：未知字段、类型不符。它相当于“带路径的 DisallowUnknownFields”，
// 这样保存时能精确指出是哪个路径写错了，而不是笼统的“json: unknown field”。
func checkShape(path string, raw any, t reflect.Type, issues *[]Issue) {
	if raw == nil { // JSON null 等同于零值
		return
	}
	switch t {
	case durationType:
		b, _ := json.Marshal(raw)
		var d Duration
		if err := d.UnmarshalJSON(b); err != nil {
			*issues = append(*issues, Issue{Path: path, Message: err.Error()})
		}
		return
	case inputSchemaType:
		m, ok := raw.(map[string]any)
		if !ok {
			*issues = append(*issues, Issue{Path: path, Message: "应为对象（字段名 -> 字段定义）"})
			return
		}
		for _, k := range sortedKeys(m) {
			checkShape(joinPath(path, k), m[k], reflect.TypeOf(InputField{}), issues)
		}
		return
	}
	switch t.Kind() {
	case reflect.Ptr:
		checkShape(path, raw, t.Elem(), issues)
	case reflect.Interface:
		// any：内容由后续语义校验负责
	case reflect.Struct:
		m, ok := raw.(map[string]any)
		if !ok {
			*issues = append(*issues, Issue{Path: path, Message: "应为对象"})
			return
		}
		fields := jsonFields(t)
		for _, k := range sortedKeys(m) {
			ft, known := fields[k]
			if !known {
				*issues = append(*issues, Issue{Path: joinPath(path, k), Message: fmt.Sprintf("未知字段 %q（可用字段：%s）", k, strings.Join(sortedFieldNames(fields), "、"))})
				continue
			}
			checkShape(joinPath(path, k), m[k], ft, issues)
		}
	case reflect.Map:
		m, ok := raw.(map[string]any)
		if !ok {
			*issues = append(*issues, Issue{Path: path, Message: "应为对象"})
			return
		}
		for _, k := range sortedKeys(m) {
			checkShape(joinPath(path, k), m[k], t.Elem(), issues)
		}
	case reflect.Slice:
		arr, ok := raw.([]any)
		if !ok {
			*issues = append(*issues, Issue{Path: path, Message: "应为数组"})
			return
		}
		for i, v := range arr {
			checkShape(fmt.Sprintf("%s[%d]", path, i), v, t.Elem(), issues)
		}
	case reflect.String:
		if _, ok := raw.(string); !ok {
			*issues = append(*issues, Issue{Path: path, Message: "应为字符串"})
		}
	case reflect.Bool:
		if _, ok := raw.(bool); !ok {
			*issues = append(*issues, Issue{Path: path, Message: "应为布尔值 true / false"})
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, ok := raw.(json.Number)
		if !ok {
			*issues = append(*issues, Issue{Path: path, Message: "应为整数"})
			return
		}
		if _, err := n.Int64(); err != nil {
			*issues = append(*issues, Issue{Path: path, Message: "应为整数"})
		}
	case reflect.Float32, reflect.Float64:
		n, ok := raw.(json.Number)
		if !ok {
			*issues = append(*issues, Issue{Path: path, Message: "应为数字"})
			return
		}
		if f, err := n.Float64(); err != nil || math.IsInf(f, 0) {
			*issues = append(*issues, Issue{Path: path, Message: "应为数字"})
		}
	}
}

// jsonFields 返回结构体的 JSON 字段名 -> 类型。
func jsonFields(t reflect.Type) map[string]reflect.Type {
	out := map[string]reflect.Type{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if name == "-" {
			continue
		}
		if name == "" {
			name = f.Name
		}
		out[name] = f.Type
	}
	return out
}

func sortedFieldNames(m map[string]reflect.Type) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// decodeInto 用 UseNumber 把正文解码进结构体（shape 检查通过后才调用）。
func decodeInto(body []byte, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	return dec.Decode(dst)
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
