// Package format 是营销工作室的「格式化层」：把事实卡、脚本、轮次设置和素材清单拼成视频模型能直接用的提示词。
//
// 格式化器是纯代码，按视频模型的 PromptFormat 选用（seedance / h3 / generic），技能碰不到这一层；
// 跟拍版和改编版共用。设计见 docs/design/营销工作室/营销工作室.md §6.6。
//
// 超长时按固定顺序删减，规则确定、可测试（见 fit）：先去掉平台写法要点，再把外观锚点只留前 3 条，
// 再去掉关键颜色，最后把每镜画面描述逐步截短（保留开头的主体 + 动作）；旁白从不删减，仍超则返回 ErrTooLong。
package format

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"video-canvas/internal/marketing"
)

// ErrTooLong 表示按全部删减规则处理后，提示词仍超出模型上限。调用方把它翻译成业务错误码（68008）。
var ErrTooLong = errors.New("提示词超出模型上限")

// Input 是渲染一份提示词需要的全部信息。
type Input struct {
	Facts         marketing.ProductFacts
	Script        marketing.Script   // 含旁白（用户可能改过）
	Platform      marketing.Platform // 平台写法要点与禁令
	Language      string             // 旁白语言 zh / en
	AspectRatio   string             // 画幅，如 9:16
	Duration      int                // 成片时长（秒）
	ProductImages int                // 商品图张数（@图片1…@图片k）
	HasModel      bool               // 有模特图（@图片k+1）
	HasRefVideo   bool               // 有参考视频（@视频1）
	Reverse       *marketing.Reverse // 跟拍或改编时的拆解，取 key_colors 与参考镜头时间；可为 nil
	MaxLen        int                // 模型提示词上限（字符数，按 rune 计），0 表示不限
}

// VideoFormatter 把 Input 渲染成视频模型的提示词。
type VideoFormatter interface {
	// Key 是格式化器的标识：seedance / h3 / generic。
	Key() string
	// Name 是后台显示名。
	Name() string
	// Description 是后台显示的写法说明。
	Description() string
	// Render 渲染整份脚本的提示词；超长按规则删减，仍超返回 ErrTooLong。
	Render(in Input) (string, error)
	// RenderShot 渲染单个镜头的提示词（发到画布时每镜一个未生成视频节点用），idx 从 0 起。
	RenderShot(in Input, idx int) (string, error)
}

// 键值。
const (
	KeySeedance = "seedance"
	KeyH3       = "h3"
	KeyGeneric  = "generic"
)

// options 是一次渲染的删减选项，按 truncationSteps 的顺序逐步累加。
type options struct {
	dropStyle  bool // 去掉平台写法要点
	maxAnchors int  // 外观锚点最多保留几条，0 不限
	dropColors bool // 去掉关键颜色
	sceneMax   int  // 每镜画面描述最多保留几个字符，0 不限
	numberBase int  // 镜头编号的起点偏移（单镜头稿保持原编号），由 RenderShot 设置，不属于删减
	ref        func(s marketing.ScriptShot) string
}

// 删减的阈值。
const (
	trimmedAnchors = 3 // 第二步外观锚点只留前几条
	maxKeyColors   = 8 // 关键颜色最多列几个
)

// sceneLimits 是第四步逐步截短画面描述的字符数序列。
var sceneLimits = []int{80, 60, 40, 30, 20, 12}

// truncationSteps 返回按顺序累加的删减方案：第 0 项是不删减。
func truncationSteps(base options) []options {
	steps := []options{base}
	o := base
	o.dropStyle = true
	steps = append(steps, o)
	o.maxAnchors = trimmedAnchors
	steps = append(steps, o)
	o.dropColors = true
	steps = append(steps, o)
	for _, n := range sceneLimits {
		o.sceneMax = n
		steps = append(steps, o)
	}
	return steps
}

// layoutFunc 是一种写法：把输入按选项渲染成文本。
type layoutFunc func(in Input, o options) string

// formatter 是格式化器的通用实现：标识 + 一种写法。
type formatter struct {
	key, name, desc string
	layout          layoutFunc
}

func (f formatter) Key() string         { return f.key }
func (f formatter) Name() string        { return f.name }
func (f formatter) Description() string { return f.desc }

// Render 实现 VideoFormatter。
func (f formatter) Render(in Input) (string, error) {
	return fit(in, options{}, f.layout)
}

// RenderShot 实现 VideoFormatter：只渲染第 idx 个镜头，时间从 0 起，编号保持原镜头号。
func (f formatter) RenderShot(in Input, idx int) (string, error) {
	if idx < 0 || idx >= len(in.Script.Shots) {
		return "", fmt.Errorf("镜头下标 %d 越界，脚本共 %d 个镜头", idx, len(in.Script.Shots))
	}
	shot := in.Script.Shots[idx]
	dur := shot.End - shot.Start
	one := in
	one.Script.Shots = []marketing.ScriptShot{shot}
	one.Script.Shots[0].Start, one.Script.Shots[0].End = 0, dur
	one.Duration = 0 // 由单镜头时长推出
	return fit(one, options{numberBase: idx}, f.layout)
}

// fit 按删减方案逐步渲染，直到不超过 MaxLen；MaxLen 为 0 不删减。仍超返回 ErrTooLong。
func fit(in Input, base options, layout layoutFunc) (string, error) {
	base.ref = refNoteFunc(in)
	if in.MaxLen <= 0 {
		return layout(in, base), nil
	}
	for _, o := range truncationSteps(base) {
		out := layout(in, o)
		if utf8.RuneCountInString(out) <= in.MaxLen {
			return out, nil
		}
	}
	return "", ErrTooLong
}

// All 返回全部格式化器，顺序固定：seedance、h3、generic。给 GET /admin/formatters 用。
func All() []VideoFormatter {
	return []VideoFormatter{seedance, h3, generic}
}

// For 按视频模型的 PromptFormat 取格式化器：seedance → seedance；h3 → h3（内容用通用写法）；其他或空 → generic。
func For(promptFormat string) VideoFormatter {
	switch promptFormat {
	case KeySeedance:
		return seedance
	case KeyH3:
		return h3
	}
	return generic
}

// 三个内置格式化器。
var (
	seedance VideoFormatter = formatter{
		key: KeySeedance, name: "Seedance 分区块",
		desc:   "[基本参数] [参考素材] [整体视觉] [商品] [分镜] [音乐与配音] [硬性约束]",
		layout: seedanceLayout,
	}
	h3 VideoFormatter = formatter{
		key: KeyH3, name: "海螺 H3", desc: "暂用通用段落写法",
		layout: genericLayout,
	}
	generic VideoFormatter = formatter{
		key: KeyGeneric, name: "通用段落", desc: "连续段落，内容同上",
		layout: genericLayout,
	}
)

// ---- 两种写法共用的取数与拼句 ----

// isEN 判断旁白语言是否英文。
func isEN(in Input) bool { return in.Language == marketing.LangEN }

// langLabel 是旁白语言的中文说法。
func langLabel(in Input) string {
	if isEN(in) {
		return "英文"
	}
	return "中文"
}

// totalDuration 返回成片时长（秒）：优先用 Duration，没有就取最后一个镜头的结束时间。
func totalDuration(in Input) float64 {
	if in.Duration > 0 {
		return float64(in.Duration)
	}
	if n := len(in.Script.Shots); n > 0 {
		return in.Script.Shots[n-1].End
	}
	return 0
}

// aspect 返回画幅，没填时用平台默认，再没有用 9:16。
func aspect(in Input) string {
	switch {
	case strings.TrimSpace(in.AspectRatio) != "":
		return in.AspectRatio
	case in.Platform.AspectRatio != "":
		return in.Platform.AspectRatio
	}
	return "9:16"
}

// productName 返回商品名，没有时写「商品」。
func productName(in Input) string {
	if n := strings.TrimSpace(in.Facts.Name); n != "" {
		return n
	}
	return "商品"
}

// anchors 返回要写进提示词的外观锚点，按删减选项截断。
func anchors(in Input, o options) []string {
	list := in.Facts.Anchors
	if o.maxAnchors > 0 && len(list) > o.maxAnchors {
		list = list[:o.maxAnchors]
	}
	return list
}

// keyColors 返回拆解里 key_colors 的并集（按出现顺序去重，最多 maxKeyColors 个）；删减选项要求去掉时返回空。
func keyColors(in Input, o options) []string {
	if o.dropColors || in.Reverse == nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, s := range in.Reverse.Shots {
		for _, c := range s.KeyColors {
			c = strings.TrimSpace(c)
			if c != "" && !seen[c] && len(out) < maxKeyColors {
				seen[c] = true
				out = append(out, c)
			}
		}
	}
	return out
}

// hasRef 判断提示词里是否需要 @视频1：有参考视频，或本脚本是跟拍脚本。
func hasRef(in Input) bool { return in.HasRefVideo || in.Script.Angle == marketing.AngleFollow }

// imageRefs 返回商品图的引用写法：@图片1、@图片2……；超过 4 张写成 @图片1–@图片k。
func imageRefs(k int) string {
	switch {
	case k <= 0:
		return ""
	case k == 1:
		return "@图片1"
	case k <= 4:
		parts := make([]string, k)
		for i := range parts {
			parts[i] = fmt.Sprintf("@图片%d", i+1)
		}
		return strings.Join(parts, "、")
	}
	return fmt.Sprintf("@图片1–@图片%d", k)
}

// modelRef 返回模特图的引用：商品图之后的下一张。
func modelRef(in Input) string { return fmt.Sprintf("@图片%d", in.ProductImages+1) }

// 参考素材说明的固定文案，两种写法共用。
const (
	productRefNote = "外观的唯一依据，禁止重新设计、改色、改形、改 logo"
	modelRefNote   = "模特形象参考"
	videoRefNote   = "参考节奏与运镜，不照搬其中的商品、人物与品牌"
)

// styleParts 返回「整体视觉」的内容：平台写法要点（可被删减去掉）与角度。
func styleParts(in Input, o options) []string {
	var out []string
	if !o.dropStyle && strings.TrimSpace(in.Platform.Style) != "" {
		out = append(out, in.Platform.Style)
	}
	if in.Script.Angle == marketing.AngleFollow {
		out = append(out, "跟拍：沿用参考片的结构与节奏，只换商品与卖点")
	} else if in.Script.Angle != "" {
		out = append(out, "角度："+marketing.AngleName(in.Script.Angle))
	}
	return out
}

// hardClauses 返回硬性约束的各条：固定三条 + 平台禁令 + 关键颜色。
func hardClauses(in Input, o options) []string {
	out := []string{
		"商品外观与参考图一致",
		"商品不出画",
		"除商品本身印刷的文字外，画面不出现任何文字、字幕或水印",
	}
	out = append(out, in.Platform.Bans...)
	if colors := keyColors(in, o); len(colors) > 0 {
		out = append(out, "关键颜色："+strings.Join(colors, "、"))
	}
	return out
}

// soundLine 返回旁白与配乐的说明：「旁白：中文，音色；配乐：…」。
func soundLine(in Input) string {
	voice := strings.TrimSpace(in.Script.Voice)
	if voice == "" {
		voice = "音色自然清晰"
	}
	music := strings.TrimSpace(in.Script.Music)
	if music == "" {
		music = "按画面氛围选择"
	}
	return fmt.Sprintf("旁白：%s，%s；配乐：%s", langLabel(in), voice, music)
}

// refNoteFunc 返回「每镜对应的参考镜头」的写法：只有带 ref_shot 的镜头（跟拍脚本）才有。
func refNoteFunc(in Input) func(s marketing.ScriptShot) string {
	return func(s marketing.ScriptShot) string {
		if s.RefShot == nil {
			return ""
		}
		id := *s.RefShot
		if in.Reverse != nil {
			for _, rs := range in.Reverse.Shots {
				if rs.ID == id {
					return fmt.Sprintf("对应 @视频1 的 %s–%ss（参考镜头%d）", marketing.FmtSec(rs.Start), marketing.FmtSec(rs.End), id)
				}
			}
		}
		return fmt.Sprintf("对应 @视频1 的参考镜头%d", id)
	}
}

// shotLine 写一个镜头：主体 + 动作放在最前面，然后是景别运镜、对应的参考镜头、旁白。
// 英文脚本的标签用英文（Shot / Voiceover），内容语言与脚本一致。
func shotLine(in Input, o options, i int) string {
	s := in.Script.Shots[i]
	no := i + 1 + o.numberBase
	timeRange := fmt.Sprintf("%s–%ss", marketing.FmtSec(s.Start), marketing.FmtSec(s.End))
	scene := strings.TrimRight(strings.TrimSpace(truncateRunes(s.Scene, o.sceneMax)), "。.")
	camera := joinNonEmpty("，", s.Shot, s.Camera)

	var b strings.Builder
	if isEN(in) {
		fmt.Fprintf(&b, "Shot %d (%s) %s.", no, timeRange, scene)
	} else {
		fmt.Fprintf(&b, "镜头%d（%s）%s。", no, timeRange, scene)
	}
	if camera != "" {
		b.WriteString(camera + "。")
	}
	if o.ref != nil {
		if note := o.ref(s); note != "" {
			b.WriteString(note + "。")
		}
	}
	if n := strings.TrimSpace(s.Narration); n != "" {
		if isEN(in) {
			fmt.Fprintf(&b, "Voiceover: \"%s\"", n)
		} else {
			fmt.Fprintf(&b, "旁白：「%s」", n)
		}
	}
	return b.String()
}

// shotLines 写全部镜头，每镜一行。
func shotLines(in Input, o options) []string {
	out := make([]string, len(in.Script.Shots))
	for i := range in.Script.Shots {
		out[i] = shotLine(in, o, i)
	}
	return out
}

// truncateRunes 按字符数截断并去掉末尾的空白与逗顿号，n ≤ 0 不截断。保留开头的主体 + 动作。
func truncateRunes(s string, n int) string {
	if n <= 0 || utf8.RuneCountInString(s) <= n {
		return s
	}
	return strings.TrimRight(string([]rune(s)[:n]), " ，、；,;:：")
}

// joinNonEmpty 用 sep 连接非空串。
func joinNonEmpty(sep string, parts ...string) string {
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}
