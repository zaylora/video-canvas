// 本文件：模型能力（capabilities）的语义校验：生成方式、参考素材、提示词、生成参数、上下文与固定上限。
// 运营手填的能力和上游平台的真实能力是否一致，这里查不出（运行时由上游报错暴露，发布前用“测试模型”发现）；
// 这里只用固定上下限拦住手误。

package modelcfg

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// 固定上下限（只拦手误，不代表某个平台的真实上限）。
const (
	maxRefCount     = 50
	minRefMB        = 1
	maxRefMB        = 500
	maxPromptLength = 1_000_000
	maxParams       = 20
	maxEnumOptions  = 20
	maxNumberValue  = 3600
	maxFanout       = 8
	maxContextWin   = 10_000_000
	minContextOut   = 256
	maxContextOut   = 1_000_000
	maxSystemLen    = 20_000
)

var paramNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

// validateCapabilities 校验 capabilities，问题的路径以 "capabilities" 开头。
func validateCapabilities(kind string, c *Capabilities, issues *[]Issue) {
	add := func(path, msg string) {
		*issues = append(*issues, Issue{Path: joinPath("capabilities", path), Message: msg})
	}
	validateOps(kind, c, add)
	validateRefs(kind, c, add)
	if c.Prompt.MaxLength < 1 || c.Prompt.MaxLength > maxPromptLength {
		add("prompt.max_length", fmt.Sprintf("提示词字数上限必须在 1 – %d 之间（Agent 模型里是用户单条消息的字数上限）", maxPromptLength))
	}
	if kind == KindAgent && len(c.Params) > 0 {
		add("params", "Agent 模型没有生成参数")
	}
	if c.Vision && kind != KindAgent {
		add("vision", "只有 Agent 模型可以声明 vision（能看图）")
	}
	validateParams(c.Params, add)
	validateContext(kind, c, add)
}

func validateOps(kind string, c *Capabilities, add func(path, msg string)) {
	allowed := opsOfKind[kind]
	if len(allowed) == 0 {
		if len(c.Ops) > 0 {
			add("ops", "文本、音频、Agent 模型没有生成方式")
		}
		return
	}
	if len(c.Ops) == 0 {
		add("ops", "至少选择一种生成方式")
	}
	seen := map[string]bool{}
	for i, op := range c.Ops {
		path := fmt.Sprintf("ops[%d]", i)
		switch {
		case !inStrings(allowed, op):
			add(path, oneOfMsg(allowed))
		case seen[op]:
			add(path, "生成方式重复")
		}
		seen[op] = true
	}
}

func validateRefs(kind string, c *Capabilities, add func(path, msg string)) {
	for _, m := range MediaKinds {
		validateRefSpec(kind, m.Kind, c.Refs.Of(m.Kind), add)
	}
	// 图生需要图片素材；全能参考至少要开一种素材
	for _, op := range c.Ops {
		switch op {
		case OpI2V, OpI2I:
			if !c.Refs.Image.On {
				add("refs.image.on", "选了图生方式，需要开启图片参考素材")
			}
		case OpOmni:
			if !c.Refs.Image.On && !c.Refs.Video.On && !c.Refs.Audio.On {
				add("refs", "选了全能参考，至少要开启一种参考素材")
			}
		}
	}
}

// validateRefSpec 校验一种参考素材：模型种类是否接收，开启后数量与大小是否在固定范围内。
func validateRefSpec(kind, refKind string, spec RefSpec, add func(path, msg string)) {
	path := "refs." + refKind
	supported := kind == KindVideo || (kind == KindImage && refKind == "image")
	if !supported {
		if spec.On {
			add(path+".on", "这个种类的模型不接收这种参考素材")
		}
		return
	}
	if !spec.On {
		return
	}
	if spec.Max < 1 || spec.Max > maxRefCount {
		add(path+".max", fmt.Sprintf("允许范围 1 – %d", maxRefCount))
	}
	if spec.MaxMB < minRefMB || spec.MaxMB > maxRefMB {
		add(path+".max_mb", fmt.Sprintf("允许范围 %d – %d MB", minRefMB, maxRefMB))
	}
}

func validateParams(params ParamSet, add func(path, msg string)) {
	if len(params) > maxParams {
		add("params", fmt.Sprintf("最多 %d 个生成参数", maxParams))
	}
	seen := map[string]bool{}
	fanouts := 0
	for _, e := range params {
		validateParamName(e.Name, seen, add)
		seen[e.Name] = true
		if validateParam("params."+e.Name, e.ParamField, add) && e.Fanout {
			fanouts++
		}
	}
	if fanouts > 1 {
		add("params", "生成数量（fanout）参数至多一个")
	}
}

func validateParamName(name string, seen map[string]bool, add func(path, msg string)) {
	base := "params." + name
	switch {
	case !paramNameRe.MatchString(name):
		add(base, "参数名只能包含小写字母、数字和下划线，且以字母开头（最长 32 位）；它会作为任务输入的键传给插件")
	case inStrings(reservedInputKeys, name):
		add(base, "参数名是宿主保留的，换一个："+strings.Join(reservedInputKeys, "、"))
	case seen[name]:
		add(base, "参数名重复")
	}
}

// validateParam 校验单个生成参数的定义；类型合法返回 true（调用方据此统计 fanout）。
func validateParam(base string, f ParamField, add func(path, msg string)) bool {
	if strings.TrimSpace(f.Label) == "" {
		add(base+".label", "不能为空（画布用它作为控件标题）")
	}
	switch f.Type {
	case ParamEnum:
		validateEnumParam(base, f, add)
	case ParamNumber:
		validateNumberParam(base, f, add)
	case ParamBoolean:
		validateBoolParam(base, f, add)
	default:
		add(base+".type", oneOfMsg([]string{ParamEnum, ParamNumber, ParamBoolean}))
		return false
	}
	if f.Type != ParamEnum && len(f.Options) > 0 {
		add(base+".options", "只有 enum 参数有可选值")
	}
	if f.Type != ParamNumber && (f.Min != nil || f.Max != nil || f.Step != nil) {
		add(base, "min / max / step 只属于 number 参数")
	}
	if f.Spec && f.Type == ParamNumber {
		add(base+".spec", "number 参数不能作为规格价格维度（时长对价格的影响用按秒计费表达）")
	}
	if f.Fanout {
		validateFanout(base, f, add)
	}
	if !f.Open && f.Default == nil {
		add(base+".default", "不开放给用户的参数必须有默认值")
	}
	return true
}

func validateEnumParam(base string, f ParamField, add func(path, msg string)) {
	if n := len(f.Options); n < 1 || n > maxEnumOptions {
		add(base+".options", fmt.Sprintf("可选值个数必须在 1 – %d 之间", maxEnumOptions))
	}
	seen := map[string]bool{}
	for i, o := range f.Options {
		path := fmt.Sprintf("%s.options[%d]", base, i)
		s, isStr := o.(string)
		_, isNum := toFloat(o)
		switch {
		case !isStr && !isNum:
			add(path, "可选值必须是字符串或数字")
		case isStr && strings.TrimSpace(s) == "":
			add(path, "不能为空")
		case seen[stringify(o)]:
			add(path, "可选值重复")
		}
		seen[stringify(o)] = true
	}
	if f.Default == nil {
		add(base+".default", "必须设置默认值")
	} else if _, ok := matchOption(f.Options, f.Default); !ok {
		add(base+".default", "默认值必须是可选值之一")
	}
}

func validateNumberParam(base string, f ParamField, add func(path, msg string)) {
	if f.Min == nil || f.Max == nil {
		add(base, "number 参数必须设置最小值 min 和最大值 max")
		return
	}
	lo, hi, step := *f.Min, *f.Max, 1
	if f.Step != nil {
		step = *f.Step
	}
	rangeMsg := fmt.Sprintf("允许范围 1 ≤ 最小 ≤ 默认 ≤ 最大 ≤ %d", maxNumberValue)
	if lo < 1 || hi > maxNumberValue || lo > hi {
		add(base+".min", rangeMsg)
	}
	if step < 1 {
		add(base+".step", "步长必须是不小于 1 的整数")
		return
	}
	d, ok := toFloat(f.Default)
	switch {
	case f.Default == nil || !ok || d != float64(int(d)):
		add(base+".default", "必须设置整数默认值")
	case int(d) < lo || int(d) > hi:
		add(base+".default", fmt.Sprintf("默认值必须在 %d – %d 之间", lo, hi))
	case (int(d)-lo)%step != 0:
		add(base+".default", fmt.Sprintf("默认值与最小值的差必须是步长 %d 的整数倍", step))
	}
}

func validateBoolParam(base string, f ParamField, add func(path, msg string)) {
	if f.Default != nil {
		if _, ok := f.Default.(bool); !ok {
			add(base+".default", "默认值必须是 true / false")
		}
	}
}

// validateFanout 校验“生成数量”参数：enum，取值是 1 – maxFanout 的正整数。
func validateFanout(base string, f ParamField, add func(path, msg string)) {
	if f.Type != ParamEnum {
		add(base+".fanout", "生成数量参数必须是 enum")
		return
	}
	for _, o := range f.Options {
		n, ok := toFloat(o)
		if !ok || n < 1 || n > maxFanout || n != float64(int(n)) {
			add(base+".options", fmt.Sprintf("生成数量的可选值必须是 1 – %d 的整数", maxFanout))
			return
		}
	}
}

func validateContext(kind string, c *Capabilities, add func(path, msg string)) {
	if kind != KindText && kind != KindAgent {
		if c.Context != nil {
			add("context", "只有文本、Agent 模型有上下文能力")
		}
		if c.System != "" {
			add("system", "只有文本模型有固定系统提示")
		}
		return
	}
	if kind == KindAgent && c.System != "" {
		add("system", "Agent 的系统提示词由平台维护，不能在模型上配置")
	}
	if c.Context == nil {
		add("context", "文本、Agent 模型必须设置上下文窗口和最大输出")
	} else {
		w, o := c.Context.Window, c.Context.Output
		if w < 1 || w > maxContextWin {
			add("context.window", fmt.Sprintf("允许范围 1 – %d", maxContextWin))
		}
		if o < minContextOut || o > maxContextOut {
			add("context.output", fmt.Sprintf("允许范围 %d – %d", minContextOut, maxContextOut))
		} else if o >= w {
			add("context.output", "最大输出必须小于上下文窗口")
		}
	}
	if n := utf8.RuneCountInString(c.System); n > maxSystemLen {
		add("system", fmt.Sprintf("不能超过 %d 个字符（当前 %d 个）", maxSystemLen, n))
	}
}

// matchOption 在 enum 可选值里找 raw 对应的那一项：字符串、数字都能比，"5" 与 5 视为相同。
func matchOption(options []any, raw any) (any, bool) {
	rawNum, rawIsNum := toFloat(raw)
	if s, ok := raw.(string); ok && !rawIsNum {
		rawNum, rawIsNum = parseNumberString(s)
	}
	for _, o := range options {
		if stringify(o) == stringify(raw) {
			return o, true
		}
		if n, ok := toFloat(o); ok && rawIsNum && n == rawNum {
			return o, true
		}
		if s, ok := o.(string); ok && rawIsNum {
			if n, ok := parseNumberString(s); ok && n == rawNum {
				return o, true
			}
		}
	}
	return nil, false
}
