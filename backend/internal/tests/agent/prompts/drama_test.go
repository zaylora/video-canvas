package prompts_test

import (
	"strings"
	"testing"

	"video-canvas/internal/agent/prompts"
)

func TestSystem_DramaAppendsRulesOnlyForDrama(t *testing.T) {
	if prompts.Version != 20 {
		t.Fatalf("短剧提示词版本=%d", prompts.Version)
	}
	base := strings.TrimRight(prompts.System("all"), "\n")
	drama := prompts.System("drama")
	if !strings.HasPrefix(drama, base+"\n\n") {
		t.Fatal("短剧必须追加原提示词")
	}
	for _, word := range []string{"当前模式：短剧助手", "drama_read", "drama_update_outline", "drama_update_episode", "drama_update_segment", "drama_run_job", "revision", "67005", "67004", "审批", "不等待", "撤销本轮"} {
		if !strings.Contains(drama, word) {
			t.Errorf("缺少 %s", word)
		}
		if word == "当前模式：短剧助手" || strings.HasPrefix(word, "drama_") {
			for _, mode := range []string{"all", "script", "storyboard", "prompt"} {
				if strings.Contains(prompts.System(mode), word) {
					t.Errorf("%s 模式被加入 %s", mode, word)
				}
			}
		}
	}
}
