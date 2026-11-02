package drama

import (
	"embed"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// skills 是原样内置的开源短剧技能（含子技能和 references），来源与许可见 prompts/NOTICE.md。
// 流水线不把整套技能塞进提示词，只按步骤和目标模型挑选下面登记的文件。
//
//go:embed all:skills
var skills embed.FS

// TargetModel 是这次生成的提示词最终要交给的图片或视频模型，取自后台模型配置。
type TargetModel struct {
	Key      string      `json:"key,omitempty"`
	Label    string      `json:"label,omitempty"`
	Vendor   string      `json:"vendor,omitempty"`
	Upstream string      `json:"upstream_model,omitempty"`
	RefAudio *AudioLimit `json:"ref_audio,omitempty"` // 仅视频：平台对参考音频的限制，认不出的平台为空
}

// AudioLimit 是视频平台对参考音频的限制：最多几段、每段的秒数范围、合计秒数上限。
type AudioLimit struct {
	MaxClips     int `json:"max_clips"`
	MinSeconds   int `json:"min_seconds"`
	MaxSeconds   int `json:"max_seconds"`
	TotalSeconds int `json:"total_seconds"`
}

// DefaultVoiceSeconds 是认不出视频平台时每个角色试音的秒数：多数平台参考音频合计 15 秒，按三人同镜估。
const DefaultVoiceSeconds = 5

// NewVideoTarget 构造视频目标模型，并按平台带上参考音频限制。
func NewVideoTarget(key, label, vendor, upstream string) TargetModel {
	m := TargetModel{Key: key, Label: label, Vendor: vendor, Upstream: upstream}
	if d := matchDialect(videoDialects, m); d != nil && d.audio != nil {
		limit := *d.audio
		m.RefAudio = &limit
	}
	return m
}

// VoiceSeconds 是拆解时每个角色试音的秒数：角色音频要当视频平台的参考音频，
// 一个镜头最多挂满全部段数，所以取「合计上限 / 段数」，再夹在单段范围里；认不出平台用缺省值。
func VoiceSeconds(targets Targets) int {
	limit := targets.Video.RefAudio
	if limit == nil || limit.MaxClips <= 0 {
		return DefaultVoiceSeconds
	}
	seconds := min(limit.MaxSeconds, limit.TotalSeconds/limit.MaxClips)
	return max(seconds, limit.MinSeconds)
}

// Targets 是项目在拆解时选定的图片模型和视频模型；没选时为空，提示词按通用写法。
type Targets struct {
	Image TargetModel `json:"image,omitzero"`
	Video TargetModel `json:"video,omitzero"`
}

// stepSkills 是每一步固定带上的技能文件，路径相对 skills/。
var stepSkills = map[string][]string{
	"analyze": {"zenstory-drama-skills/short-drama-novel-analyze/SKILL.md"},
	"outline": {
		"zenstory-drama-skills/short-drama-develop/SKILL.md",
		"zenstory-drama-skills/short-drama-develop/references/episode-design.md",
	},
	"scripts": {
		"0xsline-short-drama/SKILL.md",
		"0xsline-short-drama/references/opening-rules.md",
		"0xsline-short-drama/references/hook-design.md",
		"zenstory-drama-skills/short-drama-write/references/dialogue-craft.md",
	},
	"breakdown": {
		"zenstory-drama-skills/short-drama-assets/SKILL.md",
		"zenstory-drama-skills/short-drama-assets/references/identity-vs-variant.md",
		"zenstory-drama-skills/short-drama-assets/references/character-and-look.md",
		"zenstory-drama-skills/short-drama-assets/references/location-and-view.md",
		"zenstory-drama-skills/short-drama-assets/references/prop-and-state.md",
	},
	"asset_prompts": {
		"zenstory-drama-skills/short-drama-image-prompts/SKILL.md",
		"zenstory-drama-skills/short-drama-image-prompts/references/character-and-look.md",
		"zenstory-drama-skills/short-drama-image-prompts/references/location-plate.md",
		"zenstory-drama-skills/short-drama-image-prompts/references/prop-plate.md",
	},
	"storyboard": {
		"zenstory-drama-skills/short-drama-video-prompts/SKILL.md",
		"zenstory-drama-skills/short-drama-video-prompts/references/target-model-profile.md",
	},
}

// dialect 是某个模型家族的写法资料：模型的厂商、上游名、显示名或 key 命中 match 就带上 refs。
// ref 里的 "#标题" 表示只取该标题下的一节（到下一个同级或更高级标题为止）。
type dialect struct {
	match *regexp.Regexp
	refs  []string
	audio *AudioLimit // 仅视频：资料里写明的参考音频限制
}

// commonAudio 是 Seedance 2.0、MiniMax H3、Wan 3.0 资料里一致的参考音频限制：每段 2–15 秒、最多 3 段、合计 15 秒。
var commonAudio = &AudioLimit{MaxClips: 3, MinSeconds: 2, MaxSeconds: 15, TotalSeconds: 15}

const higgsfieldImages = "higgsfield-ai-prompt-skill/image-models.md"

// imageDialects 按顺序匹配，第一个命中的生效。
var imageDialects = []dialect{
	{regexp.MustCompile(`seedream|jimeng|即梦`), []string{higgsfieldImages + "#Seedream Family"}, nil},
	{regexp.MustCompile(`nano[ -]?banana|gemini.*image`), []string{higgsfieldImages + "#Nano Banana Family"}, nil},
	{regexp.MustCompile(`gpt[ -]?image|dall`), []string{higgsfieldImages + "#GPT Image Family"}, nil},
	{regexp.MustCompile(`flux`), []string{higgsfieldImages + "#FLUX Family"}, nil},
	{regexp.MustCompile(`kling|可灵`), []string{higgsfieldImages + "#Kling O1 Image", higgsfieldImages + "#Kling Image 3.0"}, nil},
	{regexp.MustCompile(`(^|[^a-z])wan|万相`), []string{higgsfieldImages + "#Wan 2.2"}, nil},
}

const zenstoryVideo = "zenstory-drama-skills/short-drama-video-prompts/references/"

// videoDialects 按顺序匹配；Seedance 2.5 要排在 2.0 之前。
var videoDialects = []dialect{
	{regexp.MustCompile(`seedance.*2[._-]?5`), []string{zenstoryVideo + "seedance-2.5.md"}, nil},
	{regexp.MustCompile(`seedance`), []string{zenstoryVideo + "seedance-2.0.md"}, commonAudio},
	{regexp.MustCompile(`minimax|hailuo|海螺|(^|[^a-z0-9])h3([^a-z0-9]|$)`), []string{zenstoryVideo + "minimax-h3.md"}, commonAudio},
	{regexp.MustCompile(`(^|[^a-z])wan|万相`), []string{zenstoryVideo + "wan-3.0.md"}, commonAudio},
	{regexp.MustCompile(`kling|可灵`), []string{"smixs-visual-skills/video/references/kling.md"}, nil},
	{regexp.MustCompile(`veo`), []string{"smixs-visual-skills/video/references/veo.md"}, nil},
}

// skillRefs 返回这一步要带上的全部技能文件：固定的步骤技能，加上目标模型的写法资料。
func skillRefs(step string, targets Targets) []string {
	refs := append([]string(nil), stepSkills[step]...)
	switch step {
	case "asset_prompts":
		refs = append(refs, pickDialect(imageDialects, targets.Image)...)
	case "breakdown", "storyboard":
		// 拆解也按视频平台来：角色音频要当它的参考音频，资产写法也要合它的参考素材规则
		refs = append(refs, pickDialect(videoDialects, targets.Video)...)
	}
	return refs
}

func pickDialect(dialects []dialect, m TargetModel) []string {
	if d := matchDialect(dialects, m); d != nil {
		return d.refs
	}
	return nil
}

// matchDialect 按厂商、上游名、显示名和 key 找第一个命中的模型家族；认不出返回 nil。
func matchDialect(dialects []dialect, m TargetModel) *dialect {
	name := strings.ToLower(strings.Join([]string{m.Vendor, m.Upstream, m.Label, m.Key}, " "))
	if strings.TrimSpace(name) == "" {
		return nil
	}
	for i := range dialects {
		if dialects[i].match.MatchString(name) {
			return &dialects[i]
		}
	}
	return nil
}

// readSkill 读一份技能文件，ref 带 "#标题" 时只截取那一节。登记的文件是随代码发布的，读不到是编程错误。
func readSkill(ref string) string {
	path, heading, _ := strings.Cut(ref, "#")
	data, err := skills.ReadFile("skills/" + path)
	if err != nil {
		panic(fmt.Errorf("读取内置短剧技能失败：%w", err))
	}
	text := string(data)
	if heading == "" {
		return text
	}
	section := markdownSection(text, heading)
	if section == "" {
		panic(fmt.Errorf("内置短剧技能 %s 里没有标题 %s", path, heading))
	}
	return section
}

// markdownSection 截取以 heading 开头的标题那一节，到下一个同级或更高级标题为止；代码围栏里的井号不算标题。
func markdownSection(text, heading string) string {
	lines := strings.Split(text, "\n")
	level, start := 0, -1
	fenced := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		hashes := len(line) - len(strings.TrimLeft(line, "#"))
		if hashes == 0 || hashes > 6 || !strings.HasPrefix(line[hashes:], " ") {
			continue
		}
		if start >= 0 && hashes <= level {
			return strings.Join(lines[start:i], "\n")
		}
		if start < 0 && strings.HasPrefix(strings.TrimSpace(line[hashes:]), heading) {
			level, start = hashes, i
		}
	}
	if start < 0 {
		return ""
	}
	return strings.Join(lines[start:], "\n")
}

// skillGuide 把技能文件拼成提示词里的「参考技能」一节；没有登记技能的步骤返回空串。
func skillGuide(step string, targets Targets) string {
	refs := skillRefs(step, targets)
	if len(refs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("# 参考技能\n\n以下是内置的开源短剧技能资料，用来提高本步骤的写作和提示词质量。")
	b.WriteString("它们原本写给能读写文件的智能体：其中关于读写文件、运行脚本、目录与输出路径、项目配置、交付记录和其他阶段的说明一律不适用；")
	b.WriteString("资料里的格式、字段或流程与后面「本步骤要求」冲突时，以本步骤要求为准。")
	if targets.Image != (TargetModel{}) || targets.Video != (TargetModel{}) {
		b.WriteString("输入里的 `target_models` 是最终使用的图片和视频模型，提示词按该模型的写法写；资料里没有该模型时按通用写法。")
	}
	b.WriteString("\n\n")
	for _, ref := range refs {
		b.WriteString("## 资料：")
		b.WriteString(ref)
		b.WriteString("\n\n")
		b.WriteString(strings.TrimSpace(readSkill(ref)))
		b.WriteString("\n\n")
	}
	b.WriteString("---\n\n# 本步骤要求\n\n")
	return b.String()
}

// ReferenceChars 是这一步参考技能的字数，报价时计入输入长度。
func ReferenceChars(step string, targets Targets) int {
	return utf8.RuneCountInString(skillGuide(step, targets))
}
