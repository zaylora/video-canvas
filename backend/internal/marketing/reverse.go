package marketing

import (
	_ "embed" // 内置方法正文
	"encoding/json"
	"fmt"
	"strings"

	"video-canvas/internal/llmgateway"
	"video-canvas/internal/stepkit"
)

// reverseMethod 是拆解参考片的内置方法正文。
//
//go:embed methods/reverse.md
var reverseMethod string

// 拆解校验的常量。
const (
	openingSec      = 3.0 // 前 3 秒：start 小于它的镜头必须标 is_opening_3s
	minAvgShotSec   = 0.4 // 平均每镜不少于这么多秒
	minActionRunes  = 6   // 中文动作描述的最少字数（不含标点空白）
	minActionWords  = 3   // 英文动作描述的最少词数
	reverseShotsMax = 40  // 拆解镜头数上限（截帧最多 32 帧，漏切比过切安全）
)

// productRoles 是 product_role 的合法取值。

// reverseSchema 是拆解参考片的输出 Schema（字段改写自 daihuo-fanpai shotlist，MIT）。
var reverseSchema = json.RawMessage(fmt.Sprintf(`{
  "type": "object",
  "required": ["overall", "shots", "brands", "claims"],
  "properties": {
    "overall": {"type": "object", "required": ["style", "narrative_arc", "why_viral", "transcript", "category"], "description": "整体判断",
      "properties": {
        "style": {"type": "string", "minLength": 1, "maxLength": 100, "description": "整体风格"},
        "narrative_arc": {"type": "string", "minLength": 1, "maxLength": 300, "description": "叙事节奏：每一段在讲什么"},
        "why_viral": {"type": "string", "minLength": 1, "maxLength": 300, "description": "这条片子为什么会火，写具体的手法"},
        "transcript": {"type": "string", "maxLength": 1500, "description": "完整台词转写；没有声音或没有台词写空串"},
        "category": {"type": "string", "maxLength": 50, "description": "原片商品的品类"}
      }},
    "shots": {"type": "array", "minItems": 1, "maxItems": %d, "description": "逐镜头分镜表，按时间顺序，首尾相接",
      "items": {"type": "object",
        "required": ["id", "start", "end", "is_opening_3s", "shot_size", "camera", "subject", "action", "scene", "lighting", "person", "product_in_frame", "product_role", "dialogue", "onscreen_text", "key_colors"],
        "properties": {
          "id": {"type": "integer", "minimum": 1, "description": "镜头编号，从 1 起连续"},
          "start": {"type": "number", "minimum": 0, "description": "开始秒数"},
          "end": {"type": "number", "minimum": 0, "description": "结束秒数"},
          "is_opening_3s": {"type": "boolean", "description": "是否在前 3 秒内：start 小于 3 的镜头必须为 true，其余为 false"},
          "shot_size": {"type": "string", "maxLength": 30, "description": "景别"},
          "camera": {"type": "string", "maxLength": 60, "description": "运镜"},
          "subject": {"type": "string", "maxLength": 100, "description": "画面主体"},
          "action": {"type": "string", "minLength": 1, "maxLength": 300, "description": "主体 + 动作，写到发力过程，不写结果"},
          "scene": {"type": "string", "maxLength": 200, "description": "场景"},
          "lighting": {"type": "string", "maxLength": 100, "description": "光线"},
          "person": {"type": "string", "maxLength": 200, "description": "人物外貌与穿着；没有人物写空串"},
          "product_in_frame": {"type": "string", "maxLength": 200, "description": "商品如何出现 + 占画面比例"},
          "product_role": {"type": "string", "enum": ["none", "dynamic", "hero_real", "package_text"], "description": "商品在本镜的角色：none 没出现，dynamic 在使用或运动，hero_real 静态特写、必须用真图，package_text 包装文字清晰可读"},
          "dialogue": {"type": "string", "maxLength": 300, "description": "本镜台词；没有写空串"},
          "onscreen_text": {"type": "string", "maxLength": 200, "description": "画面上叠加的文字；没有写空串"},
          "key_colors": {"type": "array", "maxItems": 6, "items": {"type": "string", "maxLength": 30}, "description": "画面关键颜色，带上载体，如「橙色软管」"}
        }}},
    "brands": {"type": "array", "maxItems": 20, "items": {"type": "string", "maxLength": 100}, "description": "画面或台词里出现的第三方品牌、IP；没有写空数组"},
    "claims": {"type": "array", "maxItems": 20, "items": {"type": "string", "maxLength": 100}, "description": "促销、销量、权威背书类话术原文；没有写空数组"}
  }
}`, reverseShotsMax))

// ReverseInput 组装拆解参考片环节的输入。durationSec 是参考片时长，cuts 是镜头切点秒数（供模型参考，
// 漏切比过切安全）。video 与 frames 都放进 Input，协议层按模型能力二选一：能直接看视频就用 video，
// 否则用 frames（调用方准备好的截帧）。
func ReverseInput(b Brief, durationSec float64, cuts []float64, video *llmgateway.Video, frames []string) stepkit.Input {
	if cuts == nil {
		cuts = []float64{}
	}
	return stepkit.Input{
		Vars: map[string]any{
			"duration":    durationSec,
			"cuts":        cuts,
			"frame_count": len(frames),
		},
		Images: frames,
		Video:  video,
	}
}

// DecodeReverse 解码已通过校验的拆解输出。
func DecodeReverse(raw json.RawMessage) (Reverse, error) {
	return decode[Reverse](raw, "拆解")
}

// ValidateReverse 与 reverse 环节的硬校验同一套：Schema 检查（含长度、枚举）再加镜头时间线、前 3 秒标记、
// 动作描述等规则。给用户编辑拆解后保存用。返回 stepkit.Hard 的错误，stepkit.ErrorList 可拆。
func ValidateReverse(r Reverse, duration float64) error {
	r.Brands = stringsOrEmpty(r.Brands)
	r.Claims = stringsOrEmpty(r.Claims)
	if r.Shots == nil {
		r.Shots = []ReverseShot{}
	}
	shots := make([]ReverseShot, len(r.Shots))
	for i, s := range r.Shots {
		s.KeyColors = stringsOrEmpty(s.KeyColors)
		shots[i] = s
	}
	r.Shots = shots
	// 结构问题和规则问题一起返回，让用户一次看全。
	problems := stepkit.ErrorList(schemaCheck(reverseSchema, r))
	problems = append(problems, reverseProblems(r, duration)...)
	return stepkit.Hard(problems...)
}

// validateReverse 是环节的 Validate：Schema 已检查过，只做时间线等规则。拆解没有软问题。
func validateReverse(out json.RawMessage, in stepkit.Input) (stepkit.Flags, error) {
	r, err := DecodeReverse(out)
	if err != nil {
		return nil, err
	}
	return nil, stepkit.Hard(reverseProblems(r, varFloat(in, "duration"))...)
}

// reverseProblems 检查已通过 Schema 的拆解：镜头 id、时间线、前 3 秒标记、动作描述、镜头密度。
func reverseProblems(r Reverse, duration float64) []string {
	if len(r.Shots) == 0 {
		return []string{"shots 至少要有 1 个镜头"}
	}
	var out []string
	spans := make([]span, len(r.Shots))
	for i, s := range r.Shots {
		spans[i] = span{s.Start, s.End}
		if s.ID != i+1 {
			out = append(out, fmt.Sprintf("镜头 id 必须从 1 开始连续递增：第 %d 个镜头的 id 是 %d，应为 %d", i+1, s.ID, i+1))
		}
		out = append(out, openingProblem(i+1, s)...)
		if p := actionProblem(s.Action, s.Subject); p != "" {
			out = append(out, fmt.Sprintf("镜头 %d：%s", i+1, p))
		}
	}
	out = append(out, timelineProblems(spans, duration)...)
	total := r.Shots[len(r.Shots)-1].End
	if duration > 0 {
		total = duration
	}
	if avg := total / float64(len(r.Shots)); avg < minAvgShotSec {
		out = append(out, fmt.Sprintf("镜头切得太碎：%d 个镜头平均每镜 %.2f 秒，应不少于 %.1f 秒，请合并相邻的相似镜头", len(r.Shots), avg, minAvgShotSec))
	}
	return out
}

// openingProblem 检查前 3 秒标记：start 小于 3 的镜头必须标 is_opening_3s，start 不小于 3 的不能标。
func openingProblem(no int, s ReverseShot) []string {
	switch {
	case s.Start < openingSec && !s.IsOpening3s:
		return []string{fmt.Sprintf("镜头 %d 开始于 %.1f 秒，在前 3 秒内，is_opening_3s 必须为 true", no, s.Start)}
	case s.Start >= openingSec && s.IsOpening3s:
		return []string{fmt.Sprintf("镜头 %d 开始于 %.1f 秒，不在前 3 秒内，is_opening_3s 必须为 false", no, s.Start)}
	}
	return nil
}

// actionVerbs 是判断中文动作描述里有没有动作的标志词。启发式：命中任一个就认为写了动作。
// 刻意不收「着」「有」「是」这类能写静态状态的字。
var actionVerbs = []string{
	"拿", "握", "挤", "涂", "抹", "推", "拉", "转", "倒", "喷", "按", "擦", "拧", "举", "走", "跑", "笑", "看", "指", "翻",
	"揉", "摇", "搅", "洗", "放", "取", "递", "抬", "拍", "打", "开", "关", "摆", "站", "坐", "靠", "伸", "抓", "捏", "压",
	"滴", "喝", "吃", "闻", "嗅", "戴", "穿", "脱", "提", "拎", "背", "抱", "扔", "撕", "剥", "切", "插", "拔", "接", "送",
	"吹", "搓", "挥", "弹", "叠", "拆", "贴", "捧", "托", "端", "盖", "合", "甩", "滑", "跳", "扭", "蹲", "躺", "倾", "斟",
	"展示", "展开", "旋转", "微笑", "点头", "转身", "低头", "抬头", "对着", "面向", "入画", "出画", "晃动", "移动", "涌出",
	"流出", "升起", "落下", "亮起", "把", "将", "正在", "来回",
}

// actionProblem 判断动作描述是否合格，合格返回空串。
//
// 启发式（可解释、可测试）：
//   - 不能为空，也不能整句只是主体（或商品名）；
//   - 中文：去掉标点空白后不少于 6 个字，且含至少一个动作标志词（actionVerbs）；
//   - 英文（不含汉字）：不少于 3 个词。
func actionProblem(action, subject string) string {
	a := strings.TrimSpace(action)
	if a == "" {
		return "action 不能为空，要写主体 + 动作"
	}
	if s := normalize(subject); s != "" && normalize(a) == s {
		return "action 不能只是主体或商品名，要写主体在做什么"
	}
	if !hasHan(a) {
		if n := len(strings.Fields(a)); n < minActionWords {
			return fmt.Sprintf("action 太短（%d 个词），英文至少 %d 个词，要写主体 + 动作", n, minActionWords)
		}
		return ""
	}
	n := 0
	for _, r := range a {
		if !isPunctOrSpace(r) {
			n++
		}
	}
	if n < minActionRunes {
		return fmt.Sprintf("action 太短（%d 个字），至少 %d 个字，要写主体 + 动作", n, minActionRunes)
	}
	for _, v := range actionVerbs {
		if strings.Contains(a, v) {
			return ""
		}
	}
	return "action 里没有动作词，不能只写名词，要写主体做了什么（写到发力过程，不写结果）"
}

// previewReverse 把拆解写成人能读的摘要。
func previewReverse(out json.RawMessage) string {
	r, err := DecodeReverse(out)
	if err != nil {
		return clip(string(out), 300)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "共 %d 个镜头；风格：%s\n叙事：%s\n爆点：%s\n", len(r.Shots), r.Overall.Style, r.Overall.NarrativeArc, r.Overall.WhyViral)
	for _, s := range r.Shots {
		fmt.Fprintf(&b, "镜头 %d（%s–%ss）%s，%s：%s\n", s.ID, fmtSec(s.Start), fmtSec(s.End), s.ShotSize, s.Camera, s.Action)
	}
	fmt.Fprintf(&b, "第三方品牌：%s\n背书话术：%s", listOrNone(r.Brands), listOrNone(r.Claims))
	return b.String()
}

// listOrNone 把列表连成一行，空列表写「无」。
func listOrNone(l []string) string {
	if len(l) == 0 {
		return "无"
	}
	return strings.Join(l, "、")
}

// reverseStep 返回拆解参考片环节的协议定义。
func reverseStep() stepkit.Step {
	return stepkit.Step{
		Key:    StepReverse,
		Title:  "拆解参考片",
		Studio: Studio,
		Duty:   "把参考视频拆成逐镜头的分镜表：每镜的景别、运镜、主体 + 动作（写到发力过程）、商品怎么出现、台词、关键颜色、是否前 3 秒，以及整条为什么火、出现的第三方品牌与背书话术。",
		Inputs: []stepkit.Var{
			{Name: "video", Type: "视频 ≤15s", Desc: "参考视频（模型能直接看视频时为原生视频输入；否则为截帧图片，此时没有声音）"},
			{Name: "duration", Type: "number", Desc: "参考片时长（秒）"},
			{Name: "cuts", Type: "number[]", Desc: "镜头切点（秒），ffmpeg 场景检测所得，漏切比过切安全，仅供参考"},
			{Name: "frame_count", Type: "number", Desc: "截帧张数，0 表示直接看视频"},
		},
		Media:  stepkit.MediaVideo,
		Schema: reverseSchema,
		Rules: stepkit.Rules{
			Hard: []string{
				"镜头 id 从 1 起连续；首镜从 0 秒开始，相邻镜头首尾相接（容差 0.3 秒），末镜结束与参考片时长相差不超过 0.5 秒",
				"开始于 3 秒之前的镜头必须标 is_opening_3s=true，3 秒及之后的不能标",
				"action 非空且不只是名词：中文不少于 6 个字且含动作词，英文不少于 3 个词，不能整句只是主体或商品名",
				"平均每镜不少于 0.4 秒",
				"product_role 只能是 none / dynamic / hero_real / package_text；shots 至少 1 个",
			},
		},
		Validate: validateReverse,
		Builtin:  reverseMethod,
		Samples:  reverseSamples(),
		Preview:  previewReverse,
	}
}
