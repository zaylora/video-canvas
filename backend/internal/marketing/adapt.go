package marketing

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"video-canvas/internal/stepkit"
)

//go:embed methods/adapt_h3.md
var adaptH3Method string

// AdaptDuty 是转写提示词开头的职责句，测试里的假模型据此认出这是转写调用。
const AdaptDuty = "把营销出片提示词转写成 MiniMax H3 的全参考写法"

// AdaptFormatH3 是需要转写的提示词格式：MiniMax H3（原稿按 Seedance 写，H3 要用 <Picture N> 标签和六段式）。
const AdaptFormatH3 = "h3"

// NeedsAdapt 判断某种视频模型提示词格式出片前要不要先转写。目前只有 H3。
func NeedsAdapt(promptFormat string) bool { return promptFormat == AdaptFormatH3 }

// AdaptRef 是一份参考素材：H3 标签、原稿里的写法、用途。
type AdaptRef struct {
	Label  string `json:"label"`
	Source string `json:"source"`
	Role   string `json:"role"`
}

// AdaptShot 是原稿的一个镜头：编号、切入时间（第一镜为空）、旁白原文。
type AdaptShot struct {
	No        int    `json:"no"`
	Cut       string `json:"cut,omitempty"`
	Narration string `json:"narration"`
}

// AdaptInput 是转写一份出片提示词需要的全部输入。
type AdaptInput struct {
	Source          string      // 原稿（Seedance 写法）
	Seconds         float64     // 成片时长
	ProductImages   int         // 商品图张数：<Picture 1>…<Picture k>
	HasModel        bool        // 有模特图：<Picture k+1>
	HasRefVideo     bool        // 有参考视频：<Video 1>
	Shots           []AdaptShot // 镜头表；原稿手改过时为空，镜头以原稿为准
	MaxLen          int         // H3 提示词上限（字符），0 不限
	ValidationError string      // 上次没通过校验的原因
}

// AdaptRefs 列出参考素材与 H3 标签的对应：商品图在前，模特图其后，参考视频单独编号。和出片任务的素材顺序一致。
func AdaptRefs(in AdaptInput) []AdaptRef {
	out := make([]AdaptRef, 0, in.ProductImages+2)
	for i := 1; i <= in.ProductImages; i++ {
		out = append(out, AdaptRef{Label: fmt.Sprintf("<Picture %d>", i), Source: fmt.Sprintf("@图片%d", i), Role: "商品图"})
	}
	if in.HasModel {
		n := in.ProductImages + 1
		out = append(out, AdaptRef{Label: fmt.Sprintf("<Picture %d>", n), Source: fmt.Sprintf("@图片%d", n), Role: "模特图（出镜人物）"})
	}
	if in.HasRefVideo {
		out = append(out, AdaptRef{Label: "<Video 1>", Source: "@视频1", Role: "参考视频（只取节奏、构图与运镜）"})
	}
	return out
}

// AdaptPrompt 拼出给文本模型的完整转写提示词：职责句 + 方法 + 输入 JSON（+ 上次的校验反馈）。
func AdaptPrompt(in AdaptInput) (string, error) {
	vars := map[string]any{
		"source_prompt": in.Source,
		"seconds":       in.Seconds,
		"references":    AdaptRefs(in),
		"shots":         in.Shots,
		"max_length":    in.MaxLen,
	}
	if in.ValidationError != "" {
		vars["validation_error"] = in.ValidationError
	}
	raw, err := json.MarshalIndent(vars, "", "  ")
	if err != nil {
		return "", err
	}
	return AdaptDuty + "。\n\n" + adaptH3Method + "\n\n## 本次输入\n\n```json\n" + string(raw) + "\n```\n", nil
}

// CutTime 把秒数写成 H3 的切点时间 MM:SS.mmm。
func CutTime(sec float64) string {
	ms := int(sec*1000 + 0.5)
	return fmt.Sprintf("%02d:%02d.%03d", ms/60000, ms/1000%60, ms%1000)
}

// DecodeAdapt 从模型回复里取出转写稿：整段必须是 {"prompt": "..."}。
func DecodeAdapt(text string) (string, error) {
	raw, err := stepkit.ExtractJSON(text)
	if err != nil {
		return "", err
	}
	var out struct {
		Prompt string `json:"prompt"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", errors.New("输出必须是 {\"prompt\": \"...\"} 形状的 JSON 对象")
	}
	return strings.TrimSpace(out.Prompt), nil
}

// SourceHash 是原稿全文的 SHA-256 小写 hex，用来判断转写稿是否过期。
func SourceHash(source string) string {
	sum := sha256.Sum256([]byte(source))
	return hex.EncodeToString(sum[:])
}

// adaptSections 是 H3 全参考写法的六段，顺序固定。
var adaptSections = []string{"subject_definitions", "summary", "retention_analysis", "detailed_description", "overall_soundscape", "non_diegetic_music"}

var (
	labelRe = regexp.MustCompile(`<(Picture|Video|Audio) (\d+)>`)
	shotRe  = regexp.MustCompile(`\[Shot (\d+)\]( At (\d{2}):(\d{2})\.(\d{3}))?`)
)

// ValidateAdapt 检查转写稿：六段齐全且有序、素材标签与清单一致、镜头数与切点、旁白原文都在、不超长。
// 返回的错误会作为 validation_error 交给模型重写一次。
func ValidateAdapt(in AdaptInput, out string) error {
	if strings.TrimSpace(out) == "" {
		return errors.New("prompt 不能为空")
	}
	if err := checkSections(out); err != nil {
		return err
	}
	if err := checkLabels(in, out); err != nil {
		return err
	}
	if err := checkShots(in, out); err != nil {
		return err
	}
	if err := checkNarrations(in, out); err != nil {
		return err
	}
	if n := utf8.RuneCountInString(out); in.MaxLen > 0 && n > in.MaxLen {
		return fmt.Errorf("全文 %d 字符，超过上限 %d，请精简描述（旁白不能删）", n, in.MaxLen)
	}
	return nil
}

// checkSections 要求六个段名各占一行开头、按顺序出现。
func checkSections(out string) error {
	last := -1
	for _, name := range adaptSections {
		i := sectionIndex(out, name)
		if i < 0 {
			return fmt.Errorf("缺少段落 %s（段名后要跟冒号）", name)
		}
		if i < last {
			return fmt.Errorf("段落 %s 的顺序不对，六段必须按 %s 的顺序", name, strings.Join(adaptSections, " → "))
		}
		last = i
	}
	return nil
}

// sectionIndex 找段名出现在行首的位置；没有返回 -1。
func sectionIndex(out, name string) int {
	head := name + ":"
	if strings.HasPrefix(out, head) {
		return 0
	}
	if i := strings.Index(out, "\n"+head); i >= 0 {
		return i + 1
	}
	return -1
}

// checkLabels 要求清单里的 <Picture N> / <Video 1> 都出现，且不出现清单外的标签、<Audio N> 和原稿的 @ 写法。
func checkLabels(in AdaptInput, out string) error {
	if strings.Contains(out, "@图片") || strings.Contains(out, "@视频") {
		return errors.New("还留着 @图片 / @视频 的写法，要换成 <Picture N> / <Video 1>")
	}
	pictures := in.ProductImages
	if in.HasModel {
		pictures++
	}
	seen := map[string]bool{}
	for _, m := range labelRe.FindAllStringSubmatch(out, -1) {
		n, _ := strconv.Atoi(m[2])
		switch {
		case m[1] == "Audio":
			return fmt.Errorf("出现了 %s，本次没有参考音频", m[0])
		case m[1] == "Picture" && (n < 1 || n > pictures):
			return fmt.Errorf("出现了 %s，参考图只有 %d 张", m[0], pictures)
		case m[1] == "Video" && (!in.HasRefVideo || n != 1):
			return fmt.Errorf("出现了 %s，本次参考视频只有 %s", m[0], refVideoCount(in))
		}
		seen[m[0]] = true
	}
	for _, r := range AdaptRefs(in) {
		if !seen[r.Label] {
			return fmt.Errorf("缺少 %s（%s），清单里的每个标签都要至少出现一次", r.Label, r.Role)
		}
	}
	return nil
}

// refVideoCount 是校验反馈里参考视频的说法。
func refVideoCount(in AdaptInput) string {
	if in.HasRefVideo {
		return "<Video 1> 一个"
	}
	return "零个"
}

// checkShots 只看 detailed_description 段（其他段会以 [Shot N] 引用镜头）：镜头编号从 1 连续、镜头数与镜头表相同（镜头表为空时不比）；
// 第二镜起带切点时间，严格递增且小于成片时长。
func checkShots(in AdaptInput, out string) error {
	body := out[sectionIndex(out, "detailed_description"):sectionIndex(out, "overall_soundscape")]
	seen := map[int]bool{}
	prev := -1.0
	for _, m := range shotRe.FindAllStringSubmatch(body, -1) {
		no, _ := strconv.Atoi(m[1])
		if seen[no] {
			continue // 同一段里回指前面的镜头
		}
		if no != len(seen)+1 {
			return fmt.Errorf("镜头编号要从 [Shot 1] 起连续递增，出现了 [Shot %d]", no)
		}
		seen[no] = true
		if no == 1 {
			continue
		}
		if m[2] == "" {
			return fmt.Errorf("[Shot %d] 要以 At MM:SS.mmm 写切入时间", no)
		}
		mm, _ := strconv.Atoi(m[3])
		ss, _ := strconv.Atoi(m[4])
		ms, _ := strconv.Atoi(m[5])
		at := float64(mm*60+ss) + float64(ms)/1000
		if at <= prev || at >= in.Seconds {
			return fmt.Errorf("[Shot %d] 的切入时间 %s 不对，切点要严格递增且小于 %s 秒", no, m[2][4:], FmtSec(in.Seconds))
		}
		prev = at
	}
	if len(in.Shots) > 0 && len(seen) != len(in.Shots) {
		return fmt.Errorf("写了 %d 个镜头，原稿是 %d 个，镜头数要相同", len(seen), len(in.Shots))
	}
	return nil
}

// checkNarrations 要求每条旁白原文都出现在转写稿里（比较时忽略空白和标点）。
func checkNarrations(in AdaptInput, out string) error {
	body := plainText(out)
	for _, s := range in.Shots {
		if n := plainText(s.Narration); n != "" && !strings.Contains(body, n) {
			return fmt.Errorf("镜头 %d 的旁白原文「%s」没有出现，旁白要一字不改放进 <d>[Chinese] …</d>", s.No, s.Narration)
		}
	}
	return nil
}

// plainText 去掉空白、标点和符号，只留文字与数字。
func plainText(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
