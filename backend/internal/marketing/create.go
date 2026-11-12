package marketing

import (
	_ "embed" // 内置方法正文
	"encoding/json"
	"fmt"
	"strings"

	"video-canvas/internal/stepkit"
)

// createMethod 是创意脚本的内置方法正文。
//
//go:embed methods/create.md
var createMethod string

// 创意脚本的镜头数范围与份数上限。
const (
	createMinShots = 2
	createMaxShots = 6
)

// createSchema 是创意脚本的输出 Schema。
var createSchema = json.RawMessage(fmt.Sprintf(`{
  "type": "object",
  "required": ["scripts"],
  "properties": {
    "scripts": {"type": "array", "minItems": 1, "maxItems": %d, "description": "脚本列表：给定角度时每个角度一份，自动选角度时由你挑选互不重复的角度",
      "items": {"type": "object", "required": ["angle", "title", "hook", "shots", "music", "voice"], "properties": {
        "angle": {"type": "string", "enum": ["pain", "feature", "lifestyle", "compare", "unbox", "mood"], "description": "创意角度：pain 痛点-解决，feature 卖点拆解，lifestyle 生活方式种草，compare 前后对比，unbox 开箱体验，mood 质感大片"},
        "title": {"type": "string", "minLength": 1, "maxLength": 40, "description": "脚本标题"},
        "hook": {"type": "string", "minLength": 1, "maxLength": 60, "description": "前 3 秒钩子的一句话概括"},
        "shots": {"type": "array", "minItems": %d, "maxItems": %d, "description": "镜头表，首尾相接，总长等于成片时长",
          "items": %s},
        "music": {"type": "string", "maxLength": 60, "description": "配乐风格"},
        "voice": {"type": "string", "maxLength": 60, "description": "旁白音色"}
      }}}
  }
}`, maxCreateScripts, createMinShots, createMaxShots, scriptShotSchema(true)))

// CreateInput 组装创意脚本环节的输入。angles 为空表示「自动」：由模型挑 count 个互不重复的角度；
// 非空时每个角度写一份（count 应等于角度数）。rev 非 nil 时只借节奏和前 3 秒钩子手法。
func CreateInput(b Brief, facts ProductFacts, angles []string, count int, rev *Reverse) stepkit.Input {
	vars := baseVars(b)
	vars["facts"] = facts
	vars["angles"] = stringsOrEmpty(angles)
	vars["auto_angles"] = len(angles) == 0
	vars["count"] = count
	vars["angle_options"] = Angles()
	vars["narration_limit"] = NarrationLimit(resolveLanguage(b), b.Duration)
	if rev != nil {
		vars["reverse"] = *rev
	}
	return stepkit.Input{Vars: vars}
}

// DecodeCreate 解码已通过校验的创意脚本输出。
func DecodeCreate(raw json.RawMessage) (CreateOutput, error) {
	return decode[CreateOutput](raw, "创意脚本")
}

// validateCreate 是环节的 Validate：份数与角度、每份的镜头结构与旁白长度、语言一致；
// 软问题为极限词（rev=nil）。多份脚本的软问题合并返回，shot 是各脚本内的镜头下标，
// 需要逐份定位时由调用方对每份脚本再调 ScanScript。
func validateCreate(out json.RawMessage, in stepkit.Input) (stepkit.Flags, error) {
	o, err := DecodeCreate(out)
	if err != nil {
		return nil, err
	}
	angles, _ := varAs[[]string](in, "angles")
	count := int(varFloat(in, "count"))
	duration := varFloat(in, "duration")
	lang := varString(in, "language")
	if lang != LangEN {
		lang = LangZH
	}

	reasons := createCountProblems(o.Scripts, angles, count)
	for i, s := range o.Scripts {
		for _, p := range scriptProblems(s, duration, lang) {
			reasons = append(reasons, fmt.Sprintf("第 %d 份脚本（%s）：%s", i+1, s.Angle, p))
		}
	}
	if err := stepkit.Hard(reasons...); err != nil {
		return nil, err
	}
	facts, _ := varAs[ProductFacts](in, "facts")
	platform := varPlatformKey(in)
	var flags stepkit.Flags
	seen := map[flagKey]bool{}
	for _, s := range o.Scripts {
		for _, f := range ScanScript(s.Shots, platform, facts, nil) {
			if k := (flagKey{f.Shot, f.Type, f.Text}); !seen[k] {
				seen[k] = true
				flags = append(flags, f)
			}
		}
	}
	return flags, nil
}

// createCountProblems 检查份数与角度：份数等于 count；给定角度时角度集合与之一致，自动时角度互不重复。
// 角度是否在六个枚举内由 Schema 检查。
func createCountProblems(scripts []Script, angles []string, count int) []string {
	var out []string
	if len(scripts) != count {
		out = append(out, fmt.Sprintf("脚本份数应为 %d，实际 %d", count, len(scripts)))
	}
	seen := map[string]bool{}
	for _, s := range scripts {
		if seen[s.Angle] {
			out = append(out, fmt.Sprintf("角度 %s 重复，每个角度只写一份", s.Angle))
		}
		seen[s.Angle] = true
	}
	if len(angles) == 0 {
		return out
	}
	want := map[string]bool{}
	for _, a := range angles {
		want[a] = true
		if !seen[a] {
			out = append(out, fmt.Sprintf("缺少角度 %s 的脚本，给定的每个角度都要写一份", a))
		}
	}
	for _, s := range scripts {
		if !want[s.Angle] {
			out = append(out, fmt.Sprintf("角度 %s 不在要求的角度里（%s）", s.Angle, strings.Join(angles, "、")))
		}
	}
	return out
}

// scriptProblems 检查一份创意脚本：时间线、旁白长度（整份合计）、旁白语言。镜头数由 Schema 检查。
func scriptProblems(s Script, duration float64, lang string) []string {
	out := timelineProblems(scriptSpans(s.Shots), duration)
	var all []string
	total := 0
	for _, sh := range s.Shots {
		all = append(all, sh.Narration)
		total += NarrationLength(lang, sh.Narration)
	}
	if limit := NarrationLimit(lang, int(duration+0.5)); duration > 0 && total > limit {
		unit := "字"
		if lang == LangEN {
			unit = "词"
		}
		out = append(out, fmt.Sprintf("旁白共 %d %s，超出上限 %d %s（时长 %d 秒），请删减", total, unit, limit, unit, int(duration+0.5)))
	}
	if p := languageProblem(lang, strings.Join(all, " ")); p != "" {
		out = append(out, p)
	}
	return out
}

// maxHanShareEN 是英文旁白里汉字占（汉字 + 拉丁字母）的最大比例。
const maxHanShareEN = 0.1

// languageProblem 检查旁白语言与设置一致：zh 要求含汉字，en 要求主要是拉丁字母。没有旁白不检查。
func languageProblem(lang, text string) string {
	if blank(text) {
		return ""
	}
	han, latin := 0, 0
	for _, r := range text {
		switch {
		case isHan(r):
			han++
		case r < 0x250 && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= 0xC0):
			latin++
		}
	}
	if lang == LangEN {
		if latin == 0 || float64(han) > maxHanShareEN*float64(han+latin) {
			return "旁白语言应为英文，不能是中文或夹杂中文，请改写（语言与设置不一致）"
		}
		return ""
	}
	if han == 0 {
		return "旁白语言应为中文，请改写（语言与设置不一致）"
	}
	return ""
}

// isHan 判断是否汉字。
func isHan(r rune) bool { return hasHan(string(r)) }

// previewCreate 把创意脚本输出写成人能读的摘要。
func previewCreate(out json.RawMessage) string {
	o, err := DecodeCreate(out)
	if err != nil {
		return clip(string(out), 300)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "共 %d 份脚本\n", len(o.Scripts))
	for _, s := range o.Scripts {
		fmt.Fprintf(&b, "【%s】", AngleName(s.Angle))
		previewScript(&b, s)
	}
	return strings.TrimRight(b.String(), "\n")
}

// createStep 返回创意脚本环节的协议定义。
func createStep() stepkit.Step {
	return stepkit.Step{
		Key:    StepCreate,
		Title:  "创意脚本",
		Studio: Studio,
		Duty:   "按给定的创意角度各写一份带货脚本：钩子、镜头表、旁白、配乐与音色。有参考拆解时只借节奏与前 3 秒手法。",
		Inputs: []stepkit.Var{
			{Name: "facts", Type: "object", Desc: "商品事实卡 JSON"},
			{Name: "angles", Type: "枚举[] 0–4", Desc: "创意角度：pain 痛点-解决 / feature 卖点拆解 / lifestyle 生活方式种草 / compare 前后对比 / unbox 开箱体验 / mood 质感大片；空数组表示自动，由你挑"},
			{Name: "count", Type: "number", Desc: "要写的脚本份数"},
			{Name: "auto_angles", Type: "boolean", Desc: "angles 为空时为 true"},
			{Name: "angle_options", Type: "object[]", Desc: "六个创意角度的名称与说明"},
			{Name: "platform", Type: "object", Desc: "投放平台 {key, name, style, bans}"},
			{Name: "language", Type: "zh | en", Desc: "旁白语言"},
			{Name: "duration", Type: "number", Desc: "成片时长（秒）"},
			{Name: "aspect_ratio", Type: "string", Desc: "画幅"},
			{Name: "narration_limit", Type: "number", Desc: "每份脚本旁白合计的字数（中文）或词数（英文）上限"},
			{Name: "reverse", Type: "object", Desc: "参考片拆解 JSON，可选；有就只借节奏和前 3 秒钩子手法"},
		},
		Media:  stepkit.MediaNone,
		Schema: createSchema,
		Rules: stepkit.Rules{
			Hard: []string{
				"份数等于 count；给定角度时每个角度各一份，自动选角度时角度互不重复且都在六个枚举内",
				fmt.Sprintf("每份 %d–%d 个镜头，从 0 秒开始首尾相接（容差 0.3 秒），总长与成片时长相差不超过 0.5 秒", createMinShots, createMaxShots),
				"旁白长度整份合计不超过上限（中文 ≤ 时长 × 4.5 字，英文 ≤ 时长 × 2.6 词）",
				"旁白语言与设置一致（中文含汉字，英文以拉丁字母为主）",
			},
			Soft: []string{"广告法极限词（国内平台，forbidden）；多份脚本的 flags 合并返回，逐份定位请对每份脚本重扫"},
		},
		Validate: validateCreate,
		Builtin:  createMethod,
		Samples:  createSamples(),
		Preview:  previewCreate,
	}
}
