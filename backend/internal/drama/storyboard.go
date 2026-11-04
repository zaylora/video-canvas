package drama

import (
	"regexp"
	"strconv"
	"unicode"
)

// StoryboardSegment 是可单独生成视频的连续片段；镜头、景别、运镜和台词都写在 Prompt 里，不另出分镜表。
type StoryboardSegment struct {
	Seconds int    `json:"seconds"`
	Prompt  string `json:"prompt"`
}

// StoryboardOutput 是完整单集分镜，成功后整体替换原片段。
type StoryboardOutput struct {
	Segments []StoryboardSegment `json:"segments"`
}

// Validate 拒绝空分镜、空提示词和非法秒数；动态上限由 ValidateMaxSeconds 校验。
func (o StoryboardOutput) Validate() error {
	return o.ValidateMaxSeconds(60)
}

// ValidateMaxSeconds 检查每段都有提示词，时长为正整数且不超过本次视频模型上限。
func (o StoryboardOutput) ValidateMaxSeconds(limit int) error {
	if len(o.Segments) == 0 || limit < 4 || limit > 60 {
		return invalidOutput("分镜不能为空且时长上限须为4至60秒")
	}
	for _, segment := range o.Segments {
		if segment.Seconds < 1 || segment.Seconds > limit || !nonemptyText(segment.Prompt) {
			return invalidOutput("片段须有提示词，时长须为正整数且不超过max_seconds")
		}
		if end, over := timelineOverrun(segment.Prompt, segment.Seconds); over {
			return invalidOutput("提示词时间轴写到" + strconv.FormatFloat(end, 'f', -1, 64) + "秒，超过该片段seconds，须改为不超过seconds或拆段")
		}
	}
	return nil
}

// storyboardMention 只识别产品约定的资产引用格式，其他文本保持原样。
var storyboardMention = regexp.MustCompile(`@\[([^\]\r\n]+)\]\(([^)\r\n]+)\)`)

// SanitizeReferences 把输入资产集合之外的引用降级为名字，避免输出虚构的画布节点。
func (o *StoryboardOutput) SanitizeReferences(ids map[string]bool) {
	for i := range o.Segments {
		o.Segments[i].Prompt = storyboardMention.ReplaceAllStringFunc(o.Segments[i].Prompt, func(token string) string {
			parts := storyboardMention.FindStringSubmatch(token)
			if ids[parts[2]] {
				return token
			}
			return parts[1]
		})
	}
}

// timelineRange 识别提示词里的时间区间终点：`0-3s`、`0~3秒`、`[0-10秒]`、`0.5–3.5s`。
// 起点前不能紧跟数字或冒号（排除 00:03-00:05 这类时钟写法），单位后不能紧跟字母（排除 30fps）。
var timelineRange = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*[-~～–—至到]\s*(\d+(?:\.\d+)?)\s*(?:秒|[sS])`)

// timelineCut 识别 H3 写法的切点：`At 00:03.500`。
var timelineCut = regexp.MustCompile(`\bAt\s+(\d{1,2}):(\d{2})(?:\.(\d{1,3}))?`)

// timelineOverrun 判断提示词里写的时间轴是否超出片段时长，返回最大的越界时间点。
// 宽松解析，只认明确的区间终点和切点；认不准的写法一律放过，不误杀。
func timelineOverrun(prompt string, seconds int) (float64, bool) {
	limit := float64(seconds) + 0.001
	worst := 0.0
	for _, loc := range timelineRange.FindAllStringSubmatchIndex(prompt, -1) {
		if clockOrFraction(prompt, loc[0]) || letterAfter(prompt, loc[1]) {
			continue
		}
		start, errStart := strconv.ParseFloat(prompt[loc[2]:loc[3]], 64)
		end, errEnd := strconv.ParseFloat(prompt[loc[4]:loc[5]], 64)
		if errStart != nil || errEnd != nil || end <= start {
			continue
		}
		worst = max(worst, end)
	}
	for _, m := range timelineCut.FindAllStringSubmatch(prompt, -1) {
		minutes, _ := strconv.Atoi(m[1])
		secs, _ := strconv.Atoi(m[2])
		at := float64(minutes*60 + secs)
		if m[3] != "" {
			frac, _ := strconv.ParseFloat("0."+m[3], 64)
			at += frac
		}
		worst = max(worst, at)
	}
	return worst, worst > limit
}

// clockOrFraction 起点前紧挨小数点或冒号时，说明匹配落在更长的数字或时钟写法中间。
func clockOrFraction(text string, at int) bool {
	if at == 0 {
		return false
	}
	switch text[at-1] {
	case ':', '.':
		return true
	}
	return false
}

// letterAfter 单位后紧跟字母时不是秒（如 30fps、3sec 之外的单词）。
func letterAfter(text string, at int) bool {
	for _, r := range text[at:] {
		return r < unicode.MaxASCII && unicode.IsLetter(r)
	}
	return false
}
