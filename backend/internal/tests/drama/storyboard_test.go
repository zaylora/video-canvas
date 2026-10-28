package drama_test

import (
	"errors"
	"strings"
	"testing"

	"video-canvas/internal/drama"
)

func TestStoryboardStrictOutput(t *testing.T) {
	valid := `{"segments":[{"seconds":15,"prompt":"村口争执","shots":[{"seconds":8,"shot_size":"全景","camera":"平视固定","action":"村民走近","dialogue":""},{"seconds":7,"shot_size":"近景","camera":"缓慢推进","action":"女子抬头","dialogue":"你来了"}]}]}`
	for _, tc := range []struct {
		name, text string
		valid      bool
	}{
		{"合法", valid, true},
		{"空片段", `{"segments":[]}`, false},
		{"空镜头", strings.Replace(valid, `[{"seconds":8,"shot_size":"全景","camera":"平视固定","action":"村民走近","dialogue":""},{"seconds":7,"shot_size":"近景","camera":"缓慢推进","action":"女子抬头","dialogue":"你来了"}]`, `[]`, 1), false},
		{"总时长不符", strings.Replace(valid, `"seconds":15`, `"seconds":14`, 1), false},
		{"零镜头时长", strings.Replace(valid, `"seconds":8`, `"seconds":0`, 1), false},
		{"负片段时长", strings.Replace(valid, `"seconds":15`, `"seconds":-1`, 1), false},
		{"超全局上限", strings.ReplaceAll(valid, `"seconds":15`, `"seconds":61`), false},
		{"未知镜头字段", strings.Replace(valid, `"action":"村民走近"`, `"extra":1,"action":"村民走近"`, 1), false},
		{"缺镜头字段", strings.Replace(valid, `,"dialogue":""`, ``, 1), false},
		{"null镜头字段", strings.Replace(valid, `"dialogue":""`, `"dialogue":null`, 1), false},
		{"重复镜头字段", strings.Replace(valid, `"seconds":8`, `"seconds":8,"seconds":8`, 1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := drama.DecodeOutput("storyboard", []byte(tc.text))
			if tc.valid && err != nil {
				t.Fatal(err)
			}
			if !tc.valid && !errors.Is(err, drama.ErrOutputInvalid) {
				t.Fatalf("want invalid output, got %v", err)
			}
		})
	}
}

func TestStoryboardPromptResource(t *testing.T) {
	prompt, err := drama.Prompt("storyboard", map[string]any{"max_seconds": 15})
	if err != nil {
		t.Fatal(err)
	}
	for _, word := range []string{"segments", "shots", "max_seconds", "@[", "连续", "声音"} {
		if !strings.Contains(prompt, word) {
			t.Fatalf("提示词缺少 %s", word)
		}
	}
}
