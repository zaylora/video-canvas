package drama_test

import (
	"strings"
	"testing"

	"video-canvas/internal/drama"
)

// 分集剧本提示词要规定文学剧本格式，否则模型会写成小说体
func TestScriptsPromptResource(t *testing.T) {
	prompt, err := drama.Prompt("scripts", map[string]any{"episode": map[string]any{"no": 1}})
	if err != nil {
		t.Fatal(err)
	}
	for _, word := range []string{"## 场次", "**场景：**", "**出场人物：**", "△", "（全景）", "（特写）", "OS", "♪", "本集钩子", "不写小说式叙述"} {
		if !strings.Contains(prompt, word) {
			t.Fatalf("提示词缺少 %s", word)
		}
	}
}
