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

func TestStoryboardTimelineOverrun(t *testing.T) {
	for _, tc := range []struct {
		name    string
		seconds int
		prompt  string
		valid   bool
	}{
		{"合法区间", 15, "0-8秒全景。8-15秒近景。", true},
		{"s写法合法", 10, "0-3s: 中景。3-10s: 特写。", true},
		{"方括号合法", 10, "[0-10秒]：钟面微距。", true},
		{"波浪号与小数", 6, "0~2.5秒全景；2.5~6秒近景", true},
		{"区间终点超过seconds", 15, "0-15s: 开场。15-30s: 收尾。", false},
		{"波浪号秒写法超限", 15, "0~30秒：全程长镜头", false},
		{"全角破折号超限", 8, "0–12秒推镜", false},
		{"H3切点合法", 12, "[Shot 1] A. [Shot 2] At 00:04.500, the camera cuts to B.", true},
		{"H3切点超限", 12, "[Shot 1] A. [Shot 2] At 00:13.000, the camera cuts to B.", false},
		{"时钟写法不当区间", 5, "监控画面 00:10-00:20 的时间戳", true},
		{"帧率不是秒", 5, "24-60fps 的质感", true},
		{"无时间轴", 5, "村民走近，女子抬头。", true},
		{"倒序区间忽略", 5, "10-3秒", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := drama.StoryboardOutput{Segments: []drama.StoryboardSegment{{Seconds: tc.seconds, Prompt: tc.prompt}}}
			err := out.ValidateMaxSeconds(30)
			if tc.valid && err != nil {
				t.Fatalf("不该判无效: %v", err)
			}
			if !tc.valid && !errors.Is(err, drama.ErrOutputInvalid) {
				t.Fatalf("应判无效，got %v", err)
			}
		})
	}
}

// 分镜固定写 Seedance 原稿，平台适配挪到片场转写；旧的平台变体不再存在。
func TestStoryboardPromptIsSeedance(t *testing.T) {
	prompt, err := drama.Prompt("storyboard", map[string]any{"max_seconds": 12})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"即梦 / Seedance", `"required":["segments"]`, "max_seconds", "@[名称](node_id)", "放不下就拆"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("分镜模板缺少 %s", want)
		}
	}
	for _, gone := range []string{"storyboard_seedance", "storyboard_h3"} {
		if _, err := drama.Prompt(gone, map[string]any{}); err == nil {
			t.Fatalf("%s 应已删除", gone)
		}
	}
}
