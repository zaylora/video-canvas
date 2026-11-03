package agent

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
)

// AdminChangeDiff 是字段级的前后值，nil 表示不存在或删除。
type AdminChangeDiff struct {
	Path   string `json:"path"`
	Before any    `json:"before"`
	After  any    `json:"after"`
}

// AdminConfigDiff 递归生成稳定顺序的字段差异，数组条目使用契约中的 [index] 路径。
func AdminConfigDiff(before, after any) []AdminChangeDiff {
	out := []AdminChangeDiff{}
	appendDiff(&out, "", before, after)
	return out
}

// appendDiff 展开同类型容器；新增或删除的容器仍展开到字段，便于逐价检查依据。
func appendDiff(out *[]AdminChangeDiff, path string, before, after any) {
	if reflect.DeepEqual(before, after) {
		return
	}
	bm, bok := before.(map[string]any)
	am, aok := after.(map[string]any)
	if (bok || before == nil) && (aok || after == nil) && (bok || aok) {
		appendObjectDiff(out, path, bm, am)
		return
	}
	ba, bok := before.([]any)
	aa, aok := after.([]any)
	if (bok || before == nil) && (aok || after == nil) && (bok || aok) {
		appendArrayDiff(out, path, ba, aa)
		return
	}
	*out = append(*out, AdminChangeDiff{Path: path, Before: before, After: after})
}

// appendObjectDiff 按稳定键顺序展开对象，新增和删除字段一并参与比较。
func appendObjectDiff(out *[]AdminChangeDiff, path string, before, after map[string]any) {
	keys := map[string]bool{}
	for k := range before {
		keys[k] = true
	}
	for k := range after {
		keys[k] = true
	}
	ordered := make([]string, 0, len(keys))
	for k := range keys {
		ordered = append(ordered, k)
	}
	sort.Strings(ordered)
	for _, key := range ordered {
		next := key
		if path != "" {
			next = path + "." + key
		}
		appendDiff(out, next, before[key], after[key])
	}
}

// appendArrayDiff 用数组下标描述规格价或参数选项变化。
func appendArrayDiff(out *[]AdminChangeDiff, path string, before, after []any) {
	for i := 0; i < max(len(before), len(after)); i++ {
		var b, a any
		if i < len(before) {
			b = before[i]
		}
		if i < len(after) {
			a = after[i]
		}
		appendDiff(out, fmt.Sprintf("%s[%d]", path, i), b, a)
	}
}

// mergeAdminPatch 按 JSON Merge Patch 语义合并；null 删除字段，数组整体替换。
func mergeAdminPatch(before map[string]any, patch map[string]any) map[string]any {
	raw, _ := json.Marshal(before)
	var after map[string]any
	_ = json.Unmarshal(raw, &after)
	if after == nil {
		after = map[string]any{}
	}
	for key, value := range patch {
		if value == nil {
			delete(after, key)
			continue
		}
		if obj, ok := value.(map[string]any); ok {
			prior, _ := after[key].(map[string]any)
			after[key] = mergeAdminPatch(prior, obj)
		} else {
			after[key] = value
		}
	}
	return after
}
