package drama_test

import (
	"errors"
	"strings"
	"testing"

	"video-canvas/internal/drama"
)

func TestStoryboardStrictOutput(t *testing.T) {
	valid := `{"segments":[{"seconds":15,"prompt":"0-8秒全景平视固定：村民走近。8-15秒近景缓慢推进：女子抬头说「你来了」。"}]}`
	for _, tc := range []struct {
		name, text string
		valid      bool
	}{
		{"合法", valid, true},
		{"空片段", `{"segments":[]}`, false},
		{"空提示词", strings.Replace(valid, `"prompt":"0-8秒全景平视固定：村民走近。8-15秒近景缓慢推进：女子抬头说「你来了」。"`, `"prompt":" "`, 1), false},
		{"零片段时长", strings.Replace(valid, `"seconds":15`, `"seconds":0`, 1), false},
		{"负片段时长", strings.Replace(valid, `"seconds":15`, `"seconds":-1`, 1), false},
		{"超全局上限", strings.Replace(valid, `"seconds":15`, `"seconds":61`, 1), false},
		{"不再接受分镜表", strings.Replace(valid, `"seconds":15,`, `"seconds":15,"shots":[],`, 1), false},
		{"缺提示词", `{"segments":[{"seconds":15}]}`, false},
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
	for _, word := range []string{"segments", "max_seconds", "@[", "连续", "声音", "景别", "运镜"} {
		if !strings.Contains(prompt, word) {
			t.Fatalf("提示词缺少 %s", word)
		}
	}
	// 不再让模型输出分镜表
	for _, word := range []string{"shots", "shot_size"} {
		if strings.Contains(prompt, word) {
			t.Fatalf("提示词不该再出现 %s", word)
		}
	}
}
