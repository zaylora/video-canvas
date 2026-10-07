package skills_test

import (
	"strings"
	"testing"

	"video-canvas/internal/agent/skills"
)

func TestAll_FiveBuiltInSkills(t *testing.T) {
	want := []string{"character-turnaround", "keyframe-prompt", "scene-setting", "script-breakdown", "video-motion-prompt"}
	got := skills.All()
	if len(got) != len(want) {
		t.Fatalf("应有 %d 个内置技能，实际 %d", len(want), len(got))
	}
	for i, b := range got {
		if b.Name != want[i] || b.Description == "" {
			t.Errorf("第 %d 个：%+v，应为 %s 且有说明", i, b, want[i])
		}
		s, ok := skills.Read(b.Name)
		if !ok || len(s.Body) < 200 || strings.HasPrefix(s.Body, "---") || len(s.Tags) == 0 {
			t.Errorf("技能 %s 的正文、标签不完整: ok=%v body=%d tags=%v", b.Name, ok, len(s.Body), s.Tags)
		}
	}
}

func TestSearch(t *testing.T) {
	cases := map[string]string{
		"拆镜":       "script-breakdown", // 说明里
		"三视图":      "character-turnaround",
		"Kling":    "video-motion-prompt", // 标签里，不区分大小写
		"kling 运镜": "video-motion-prompt", // 命中词越多越靠前
		"关键帧 构图":   "keyframe-prompt",
		"scene":    "scene-setting", // 名字里
		"  空镜  ":   "scene-setting",
		"seedance": "video-motion-prompt",
	}
	for q, first := range cases {
		got := skills.Search(q)
		if len(got) == 0 || got[0].Name != first {
			t.Errorf("搜 %q：第一个应是 %s，实际 %v", q, first, got)
		}
	}
	if got := skills.Search("完全不相关的词xyz"); len(got) != 0 {
		t.Errorf("没有命中应为空: %v", got)
	}
	if got := skills.Search(""); len(got) != skills.MaxResults {
		t.Errorf("空查询返回全部（受上限 %d）: %d", skills.MaxResults, len(got))
	}
}

func TestRead(t *testing.T) {
	s, ok := skills.Read(" script-breakdown ")
	if !ok || s.Name != "script-breakdown" || !strings.Contains(s.Body, "镜号") {
		t.Errorf("s=%+v ok=%v", s, ok)
	}
	if _, ok := skills.Read("../etc/passwd"); ok {
		t.Error("不存在的技能名返回 false，不做任何路径解析")
	}
}
