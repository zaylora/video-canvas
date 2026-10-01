// 本文件：导入草稿的参数预填建议（ParamHint）：插件在 parseImportResponse 里给出，只用来在导入那一刻预填模型编辑器
// （按种类套默认能力模板后，覆盖同名生成参数的取值设置）。之后能力以运营保存的配置为准，后端校验与下单都不读它。

package modelcfg

import (
	"fmt"
	"sort"
	"strings"
)

// ParamHint 是对一个生成参数的预填建议，键是模板里的参数名。字段都可省略，只覆盖给出的部分。
type ParamHint struct {
	Options []any `json:"options,omitempty"` // enum：可选值（字符串或数字）
	Default any   `json:"default,omitempty"` // 默认值，类型随参数
	Min     *int  `json:"min,omitempty"`     // number：最小值
	Max     *int  `json:"max,omitempty"`     // number：最大值
	Step    *int  `json:"step,omitempty"`    // number：步长
	Open    *bool `json:"open,omitempty"`    // 是否开放给用户
	Remove  bool  `json:"remove,omitempty"`  // true：从模板里去掉这个参数（模型没有这一项）
}

// maxParamHints 是一份草稿里最多的预填建议数，防止插件返回过大的结构。
const maxParamHints = maxParams

// ValidateParamHints 校验预填建议的格式（不对照模板：模板在前端，参数名对不上时由前端忽略并提示）。
// 返回中文问题列表，没有问题返回 nil。
func ValidateParamHints(hints map[string]ParamHint) []string {
	if len(hints) == 0 {
		return nil
	}
	var out []string
	if len(hints) > maxParamHints {
		out = append(out, fmt.Sprintf("最多 %d 个参数建议", maxParamHints))
	}
	names := make([]string, 0, len(hints))
	for name := range hints {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for _, msg := range paramHintIssues(name, hints[name]) {
			out = append(out, name+"："+msg)
		}
	}
	return out
}

// paramHintIssues 校验一条预填建议的格式，返回问题列表。
func paramHintIssues(name string, h ParamHint) []string {
	if !paramNameRe.MatchString(name) {
		return []string{"参数名只能包含小写字母、数字和下划线，且以字母开头"}
	}
	var out []string
	if len(h.Options) > maxEnumOptions {
		out = append(out, fmt.Sprintf("可选值最多 %d 个", maxEnumOptions))
	}
	for _, o := range h.Options {
		if !isHintOption(o) {
			out = append(out, "可选值必须是非空字符串或数字")
			break
		}
	}
	switch h.Default.(type) {
	case nil, string, bool, float64, int, int64:
	default:
		out = append(out, "默认值必须是字符串、数字或布尔值")
	}
	for _, f := range []struct {
		label string
		v     *int
	}{{"min", h.Min}, {"max", h.Max}, {"step", h.Step}} {
		if f.v != nil && (*f.v < 0 || *f.v > maxNumberValue) {
			out = append(out, fmt.Sprintf("%s 必须在 0 – %d 之间", f.label, maxNumberValue))
		}
	}
	return out
}

func isHintOption(o any) bool {
	if s, isStr := o.(string); isStr {
		return strings.TrimSpace(s) != ""
	}
	_, isNum := toFloat(o)
	return isNum
}
