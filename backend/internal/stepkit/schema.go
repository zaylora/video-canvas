package stepkit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"
	"unicode/utf8"
)

// 本文件处理环节的输出 Schema：生成提示词里的字段说明，并对模型输出做结构检查。
// 只支持协议里用到的 JSON Schema 子集：type、required、properties、items、enum、
// minItems / maxItems、minLength / maxLength、minimum / maximum、description。其余关键字忽略。

// schemaNode 是 Schema 的一个节点，只保留本包用到的关键字。
type schemaNode struct {
	Types       []string
	Description string
	Required    []string
	Props       []schemaProp // 保持 Schema 里的书写顺序
	Items       *schemaNode
	Enum        []any
	MinItems    *int
	MaxItems    *int
	MinLength   *int
	MaxLength   *int
	Minimum     *float64
	Maximum     *float64
}

// schemaProp 是对象的一个属性。
type schemaProp struct {
	Name string
	Node *schemaNode
}

// parseSchema 把 Schema JSON 解析成节点树，属性顺序按书写顺序保留。
func parseSchema(raw json.RawMessage) (*schemaNode, error) {
	var head struct {
		Type        json.RawMessage `json:"type"`
		Description string          `json:"description"`
		Required    []string        `json:"required"`
		Properties  json.RawMessage `json:"properties"`
		Items       json.RawMessage `json:"items"`
		Enum        []any           `json:"enum"`
		MinItems    *int            `json:"minItems"`
		MaxItems    *int            `json:"maxItems"`
		MinLength   *int            `json:"minLength"`
		MaxLength   *int            `json:"maxLength"`
		Minimum     *float64        `json:"minimum"`
		Maximum     *float64        `json:"maximum"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return nil, fmt.Errorf("解析 Schema 失败：%w", err)
	}
	n := &schemaNode{
		Description: head.Description, Required: head.Required, Enum: head.Enum,
		MinItems: head.MinItems, MaxItems: head.MaxItems, MinLength: head.MinLength, MaxLength: head.MaxLength,
		Minimum: head.Minimum, Maximum: head.Maximum,
	}
	n.Types = parseTypes(head.Type)
	if len(head.Properties) > 0 {
		props, err := parseProps(head.Properties)
		if err != nil {
			return nil, err
		}
		n.Props = props
	}
	if len(head.Items) > 0 {
		items, err := parseSchema(head.Items)
		if err != nil {
			return nil, err
		}
		n.Items = items
	}
	return n, nil
}

// parseTypes 解析 type：字符串或字符串数组。
func parseTypes(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return []string{one}
	}
	var many []string
	if json.Unmarshal(raw, &many) == nil {
		return many
	}
	return nil
}

// parseProps 按书写顺序解析 properties。
func parseProps(raw json.RawMessage) ([]schemaProp, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if _, err := dec.Token(); err != nil { // 开头的 {
		return nil, fmt.Errorf("解析 Schema properties 失败：%w", err)
	}
	var out []schemaProp
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("解析 Schema properties 失败：%w", err)
		}
		var sub json.RawMessage
		if err := dec.Decode(&sub); err != nil {
			return nil, fmt.Errorf("解析 Schema properties 失败：%w", err)
		}
		node, err := parseSchema(sub)
		if err != nil {
			return nil, err
		}
		name, _ := keyTok.(string)
		out = append(out, schemaProp{Name: name, Node: node})
	}
	return out, nil
}

// SchemaFields 由 Schema 生成「输出格式」块的字段说明，每个字段一行，嵌套对象和数组元素用 a.b[].c 的路径。
func SchemaFields(schema json.RawMessage) ([]string, error) {
	root, err := parseSchema(schema)
	if err != nil {
		return nil, err
	}
	var lines []string
	describeNode(root, "", &lines)
	return lines, nil
}

// describeNode 递归生成字段说明；根节点本身不输出一行。
func describeNode(n *schemaNode, path string, lines *[]string) {
	for _, p := range n.Props {
		child := p.Name
		if path != "" {
			child = path + "." + p.Name
		}
		required := containsString(n.Required, p.Name)
		*lines = append(*lines, "- "+child+"（"+typeLabel(p.Node, required)+"）"+descSuffix(p.Node))
		describeNode(p.Node, child, lines)
		if p.Node.Items != nil {
			describeItems(p.Node.Items, child+"[]", lines)
		}
	}
}

// describeItems 说明数组元素：元素是对象时展开其属性，是标量时补一行元素说明。
func describeItems(items *schemaNode, path string, lines *[]string) {
	if len(items.Props) > 0 {
		describeNode(items, path, lines)
		return
	}
	*lines = append(*lines, "- "+path+"（"+typeLabel(items, false)+"）"+descSuffix(items))
	if items.Items != nil {
		describeItems(items.Items, path+"[]", lines)
	}
}

// descSuffix 返回「：说明」，没有说明时为空。
func descSuffix(n *schemaNode) string {
	if n.Description == "" {
		return ""
	}
	return "：" + n.Description
}

// typeLabel 把类型、必填和取值约束写成一段简短说明。
func typeLabel(n *schemaNode, required bool) string {
	parts := []string{}
	if len(n.Types) > 0 {
		parts = append(parts, strings.Join(n.Types, "|"))
	}
	if required {
		parts = append(parts, "必填")
	} else {
		parts = append(parts, "可选")
	}
	if len(n.Enum) > 0 {
		vals := make([]string, 0, len(n.Enum))
		for _, v := range n.Enum {
			vals = append(vals, fmt.Sprint(v))
		}
		parts = append(parts, "取值 "+strings.Join(vals, " / "))
	}
	if n.MinItems != nil {
		parts = append(parts, fmt.Sprintf("至少 %d 项", *n.MinItems))
	}
	if n.MaxItems != nil {
		parts = append(parts, fmt.Sprintf("最多 %d 项", *n.MaxItems))
	}
	if n.MinLength != nil {
		parts = append(parts, fmt.Sprintf("至少 %d 字", *n.MinLength))
	}
	if n.MaxLength != nil {
		parts = append(parts, fmt.Sprintf("最多 %d 字", *n.MaxLength))
	}
	if n.Minimum != nil {
		parts = append(parts, fmt.Sprintf("不小于 %v", *n.Minimum))
	}
	if n.Maximum != nil {
		parts = append(parts, fmt.Sprintf("不大于 %v", *n.Maximum))
	}
	return strings.Join(parts, "，")
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// CheckSchema 按 Schema 检查模型输出，返回全部结构问题（空表示通过）。
// 它是协议层的第一道检查：不管方法是内置还是技能，输出结构都必须符合同一份 Schema。
func CheckSchema(schema json.RawMessage, out []byte) []string {
	root, err := parseSchema(schema)
	if err != nil {
		return []string{err.Error()}
	}
	var v any
	if err := json.Unmarshal(out, &v); err != nil {
		return []string{"输出不是合法 JSON"}
	}
	var errs []string
	checkValue(root, v, "$", &errs)
	return errs
}

// checkValue 递归检查一个值；path 是出错位置，根为 $。
func checkValue(n *schemaNode, v any, path string, errs *[]string) {
	if len(n.Types) > 0 && !matchesType(n.Types, v) {
		*errs = append(*errs, fmt.Sprintf("%s 的类型应为 %s", path, strings.Join(n.Types, "|")))
		return
	}
	if len(n.Enum) > 0 && !inEnum(n.Enum, v) {
		*errs = append(*errs, fmt.Sprintf("%s 的取值不在允许范围内", path))
	}
	switch val := v.(type) {
	case map[string]any:
		checkObject(n, val, path, errs)
	case []any:
		checkArray(n, val, path, errs)
	case string:
		checkString(n, val, path, errs)
	case float64:
		checkNumber(n, val, path, errs)
	}
}

func checkObject(n *schemaNode, obj map[string]any, path string, errs *[]string) {
	for _, name := range n.Required {
		if _, ok := obj[name]; !ok {
			*errs = append(*errs, fmt.Sprintf("%s 缺少必填字段 %s", path, name))
		}
	}
	for _, p := range n.Props {
		if sub, ok := obj[p.Name]; ok {
			checkValue(p.Node, sub, path+"."+p.Name, errs)
		}
	}
}

func checkArray(n *schemaNode, arr []any, path string, errs *[]string) {
	if n.MinItems != nil && len(arr) < *n.MinItems {
		*errs = append(*errs, fmt.Sprintf("%s 至少要有 %d 项，实际 %d 项", path, *n.MinItems, len(arr)))
	}
	if n.MaxItems != nil && len(arr) > *n.MaxItems {
		*errs = append(*errs, fmt.Sprintf("%s 最多 %d 项，实际 %d 项", path, *n.MaxItems, len(arr)))
	}
	if n.Items == nil {
		return
	}
	for i, el := range arr {
		checkValue(n.Items, el, fmt.Sprintf("%s[%d]", path, i), errs)
	}
}

func checkString(n *schemaNode, s, path string, errs *[]string) {
	length := utf8.RuneCountInString(s)
	if n.MinLength != nil && length < *n.MinLength {
		*errs = append(*errs, fmt.Sprintf("%s 至少 %d 字，实际 %d 字", path, *n.MinLength, length))
	}
	if n.MaxLength != nil && length > *n.MaxLength {
		*errs = append(*errs, fmt.Sprintf("%s 最多 %d 字，实际 %d 字", path, *n.MaxLength, length))
	}
}

func checkNumber(n *schemaNode, f float64, path string, errs *[]string) {
	if n.Minimum != nil && f < *n.Minimum {
		*errs = append(*errs, fmt.Sprintf("%s 不能小于 %v", path, *n.Minimum))
	}
	if n.Maximum != nil && f > *n.Maximum {
		*errs = append(*errs, fmt.Sprintf("%s 不能大于 %v", path, *n.Maximum))
	}
}

// typeCheckers 是各 JSON Schema 类型对应的判断；integer 要求数值是整数。
var typeCheckers = map[string]func(v any) bool{
	"object":  func(v any) bool { _, ok := v.(map[string]any); return ok },
	"array":   func(v any) bool { _, ok := v.([]any); return ok },
	"string":  func(v any) bool { _, ok := v.(string); return ok },
	"boolean": func(v any) bool { _, ok := v.(bool); return ok },
	"null":    func(v any) bool { return v == nil },
	"number":  func(v any) bool { _, ok := v.(float64); return ok },
	"integer": func(v any) bool { f, ok := v.(float64); return ok && f == math.Trunc(f) },
}

// matchesType 判断值是否符合任一类型；不认识的类型视为不匹配。
func matchesType(types []string, v any) bool {
	for _, t := range types {
		if check, ok := typeCheckers[t]; ok && check(v) {
			return true
		}
	}
	return false
}

// inEnum 判断值是否在枚举里（按 JSON 数值与字符串的原值比较）。
func inEnum(enum []any, v any) bool {
	for _, e := range enum {
		if reflect.DeepEqual(e, v) {
			return true
		}
	}
	return false
}
