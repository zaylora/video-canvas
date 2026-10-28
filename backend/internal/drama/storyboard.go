package drama

import (
	"encoding/json"
	"regexp"
)

// StoryboardShot 是片段中的一个镜头；秒数使用整数以便严格核对总时长。
type StoryboardShot struct {
	Seconds  int    `json:"seconds"`
	ShotSize string `json:"shot_size"`
	Camera   string `json:"camera"`
	Action   string `json:"action"`
	Dialogue string `json:"dialogue"`
}

// StoryboardSegment 是可单独生成视频的连续片段。
type StoryboardSegment struct {
	Seconds int              `json:"seconds"`
	Prompt  string           `json:"prompt"`
	Shots   []StoryboardShot `json:"shots"`
}

// StoryboardOutput 是完整单集分镜，成功后整体替换原片段。
type StoryboardOutput struct {
	Segments []StoryboardSegment `json:"segments"`
}

// Validate 拒绝空分镜、非法秒数和镜头时长不守恒；动态上限由 ValidateMaxSeconds 校验。
func (o StoryboardOutput) Validate() error {
	return o.ValidateMaxSeconds(60)
}

// ValidateMaxSeconds 检查每段正整数时长不超过本次视频模型上限，镜头秒数精确守恒。
func (o StoryboardOutput) ValidateMaxSeconds(limit int) error {
	if len(o.Segments) == 0 || limit < 4 || limit > 60 {
		return invalidOutput("分镜不能为空且时长上限须为4至60秒")
	}
	for _, segment := range o.Segments {
		if segment.Seconds < 1 || segment.Seconds > limit || !nonemptyText(segment.Prompt) || len(segment.Shots) == 0 {
			return invalidOutput("片段须有提示词和镜头，时长须为正整数且不超过max_seconds")
		}
		total := 0
		for _, shot := range segment.Shots {
			if shot.Seconds < 1 || shot.Seconds > segment.Seconds || !nonemptyText(shot.ShotSize) || !nonemptyText(shot.Camera) || !nonemptyText(shot.Action) {
				return invalidOutput("镜头须有景别、运镜、动作和正整数秒数")
			}
			total += shot.Seconds
			if total > segment.Seconds {
				return invalidOutput("镜头秒数之和须等于片段秒数")
			}
		}
		if total != segment.Seconds {
			return invalidOutput("镜头秒数之和须等于片段秒数")
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

// checkStoryboardShots 对嵌套镜头执行精确字段检查，防止空值或未知字段被忽略。
func checkStoryboardShots(data []byte) error {
	var root struct {
		Segments []struct {
			Shots []json.RawMessage `json:"shots"`
		} `json:"segments"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		return invalidOutput("分镜结构不合法")
	}
	for _, segment := range root.Segments {
		for _, shot := range segment.Shots {
			if _, err := requireObject(shot, []string{"seconds", "shot_size", "camera", "action", "dialogue"}); err != nil {
				return err
			}
		}
	}
	return nil
}
