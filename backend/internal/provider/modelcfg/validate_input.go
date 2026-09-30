// 本文件：输入 schema（input_schema）的语义校验：字段名、类型、各类型专属属性、枚举选项、port、默认值。
// 模型配置解析与宿主导入模型草稿都用同一份规则。

package modelcfg

import (
	"fmt"
	"regexp"
	"strings"
)

var fieldNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)

var (
	validFieldTypes = []string{FieldText, FieldNumber, FieldEnum, FieldBoolean, FieldImage, FieldVideo, FieldAudio}
	validPorts      = []string{"text", "image", "video", "audio"}
)

// ValidateInputSchema 校验 input_schema 的定义本身是否合法，所有问题一次返回，Path 形如 "input_schema.prompt.type"。
// ParseModel 内部用它；宿主导入模型草稿（插件 parseImportResponse 的返回值）时也要先过这一关。
// 没有问题返回 nil。空 schema 是合法的。
func ValidateInputSchema(schema InputSchema) []Issue {
	var issues []Issue
	seen := map[string]bool{}
	for _, e := range schema {
		base := joinPath("input_schema", e.Name)
		if !fieldNameRe.MatchString(e.Name) {
			issues = append(issues, Issue{Path: base, Message: "字段名只能包含字母、数字和下划线，且不能以数字开头（它会作为 ctx.input 的属性名和文件引用 input:<字段名> 出现在插件里）"})
		}
		if seen[e.Name] {
			issues = append(issues, Issue{Path: base, Message: "字段名重复"})
		}
		seen[e.Name] = true
		validateField(base, e.InputField, &issues)
	}
	return issues
}

// validateField 校验单个字段；base 是字段的路径，例如 "input_schema.prompt"。
func validateField(base string, f InputField, issues *[]Issue) {
	add := func(sub, msg string) { *issues = append(*issues, Issue{Path: joinPath(base, sub), Message: msg}) }

	if !inStrings(validFieldTypes, f.Type) {
		add("type", oneOfMsg(validFieldTypes))
		return
	}
	if strings.TrimSpace(f.Label) == "" {
		add("label", "不能为空（前端用它作为控件标题和错误提示）")
	}
	validateTypeSpecific(f, add)
	if f.Type == FieldEnum {
		validateEnumOptions(f, add)
	}
	validatePort(f, add)
	validateDefault(f, add)
}

// validateTypeSpecific 检查各类型专属属性：min / max 只属于 number，max_length 只属于 text，options 只属于 enum。
func validateTypeSpecific(f InputField, add func(sub, msg string)) {
	if f.Type != FieldNumber {
		if f.Min != nil {
			add("min", "只有 number 类型可以设置")
		}
		if f.Max != nil {
			add("max", "只有 number 类型可以设置")
		}
	}
	if f.Type != FieldText && f.MaxLength != 0 {
		add("max_length", "只有 text 类型可以设置")
	}
	if f.MaxLength < 0 {
		add("max_length", "不能为负数")
	}
	if f.Type != FieldEnum && len(f.Options) > 0 {
		add("options", "只有 enum 类型可以设置")
	}
	if f.Min != nil && f.Max != nil && *f.Min > *f.Max {
		add("min", "不能大于 max")
	}
}

// validatePort 检查 port：只有 text 与媒体字段能由画布上游连线提供，且 port 必须和字段类型一致。
func validatePort(f InputField, add func(sub, msg string)) {
	if f.Port == "" {
		return
	}
	switch {
	case !inStrings(validPorts, f.Port):
		add("port", oneOfMsg(validPorts))
	case f.Type == FieldText && f.Port != "text":
		add("port", "text 字段的 port 只能是 text")
	case isMediaType(f.Type) && f.Port != f.Type:
		add("port", fmt.Sprintf("%s 字段的 port 只能是 %s", f.Type, f.Type))
	case f.Type == FieldNumber || f.Type == FieldEnum || f.Type == FieldBoolean:
		add("port", "只有 text 和媒体字段可以由上游连线提供")
	}
}

// validateEnumOptions 检查枚举选项：至少一个、值是字符串或数字、值不重复、label 非空。
func validateEnumOptions(f InputField, add func(sub, msg string)) {
	if len(f.Options) == 0 {
		add("options", "enum 类型至少需要一个选项")
		return
	}
	seen := map[string]bool{}
	for i, o := range f.Options {
		op := fmt.Sprintf("options[%d]", i)
		if _, isStr := o.Value.(string); !isStr {
			if _, isNum := toFloat(o.Value); !isNum {
				add(op+".value", "必须是字符串或数字")
				continue
			}
		}
		key := optionKey(o.Value)
		if seen[key] {
			add(op+".value", "选项值重复")
		}
		seen[key] = true
		if strings.TrimSpace(o.Label) == "" {
			add(op+".label", "不能为空")
		}
	}
}

// optionKey 把选项值规范成可比较的键，5 与 5.0 与 "5" 视为同一个值。
func optionKey(v any) string {
	if f, ok := toFloat(v); ok {
		return "n:" + stringify(f)
	}
	s := stringify(v)
	if f, ok := parseNumberString(s); ok {
		return "n:" + stringify(f)
	}
	return "s:" + s
}

// validateDefault 检查默认值的类型和范围；媒体字段不能设置默认值。
func validateDefault(f InputField, add func(sub, msg string)) {
	if f.Default == nil {
		return
	}
	d := f.Default
	switch f.Type {
	case FieldText:
		s, ok := d.(string)
		if !ok {
			add("default", "text 字段的默认值必须是字符串")
		} else if f.MaxLength > 0 && len([]rune(s)) > f.MaxLength {
			add("default", "默认值超过 max_length")
		}
	case FieldNumber:
		n, ok := toFloat(d)
		switch {
		case !ok:
			add("default", "number 字段的默认值必须是数字")
		case f.Min != nil && n < *f.Min:
			add("default", "默认值小于 min")
		case f.Max != nil && n > *f.Max:
			add("default", "默认值大于 max")
		}
	case FieldBoolean:
		if _, ok := d.(bool); !ok {
			add("default", "boolean 字段的默认值必须是 true / false")
		}
	case FieldEnum:
		if _, ok := matchOption(f.Options, d); !ok && len(f.Options) > 0 {
			add("default", "默认值必须是 options 里的某个值")
		}
	default:
		add("default", "媒体字段不能设置默认值")
	}
}
