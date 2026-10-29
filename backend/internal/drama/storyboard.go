package drama

import "regexp"

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
