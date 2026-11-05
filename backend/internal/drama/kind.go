package drama

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// 采样长度（字）：开头、中间、结尾各取一段，总量够判断体裁又不浪费 Token。
const (
	kindHeadRunes = 1600
	kindMidRunes  = 1200
	kindTailRunes = 1200
)

// KindSample 从原文取开头、中间、结尾各一段交给模型判断体裁；原文不长时原样返回。
func KindSample(text string) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= kindHeadRunes+kindMidRunes+kindTailRunes {
		return string(runes)
	}
	mid := len(runes)/2 - kindMidRunes/2
	return string(runes[:kindHeadRunes]) + "\n……\n" +
		string(runes[mid:mid+kindMidRunes]) + "\n……\n" +
		string(runes[len(runes)-kindTailRunes:])
}

// KindPrompt 生成判断原文是剧本还是小说的提示词，要求只返回一个 JSON 对象。
func KindPrompt(sample string) string {
	return `你是影视编剧助理。判断下面的原文片段（开头、中间、结尾各取一段，用「……」隔开）是「剧本」还是「小说」。

判断依据：
- script（剧本）：以场次、对白、动作提示为主，如「1-1 日/内 地点」、「△」动作行、「人物名：台词」、「第N集」；几乎没有大段叙述和心理描写。
- novel（小说）：以叙述、描写、心理活动为主，台词夹在叙述里，常见章节标题；没有场次头和对白格式。
- 梗概、大纲、设定集、随笔等也按更接近的一类选，不要拒绝回答。

只返回一个 JSON 对象，不要代码围栏和其他文字：{"kind":"script 或 novel","reason":"一句话说明依据，不超过 30 字"}

原文片段：
<source>
` + sample + `
</source>`
}

// ParseKindGuess 从模型输出里取出体裁判断：容忍代码围栏和前后多余文字；
// 类型不是 script / novel 或解析不出时 ok 为 false，调用方应保持 unknown 让用户手选。
func ParseKindGuess(text string) (kind, reason string, ok bool) {
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return "", "", false
	}
	var out struct {
		Kind   string `json:"kind"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(text[start:end+1]), &out); err != nil {
		return "", "", false
	}
	kind = strings.TrimSpace(out.Kind)
	if kind != "script" && kind != "novel" {
		return "", "", false
	}
	reason = strings.TrimSpace(out.Reason)
	if utf8.RuneCountInString(reason) > 60 {
		reason = Clip(reason, 60)
	}
	return kind, reason, true
}
