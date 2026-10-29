package prompts_test

import (
	"strings"
	"testing"

	"video-canvas/internal/agent/prompts"
)

func TestSystem_DramaAppendsRulesOnlyForDrama(t *testing.T) {
	if prompts.Version != 21 {
		t.Fatalf("短剧提示词版本=%d", prompts.Version)
	}
	base := strings.TrimRight(prompts.System("all"), "\n")
	drama := prompts.System("drama")
	if !strings.HasPrefix(drama, base+"\n\n") {
		t.Fatal("短剧必须追加原提示词")
	}
	for _, word := range []string{"当前模式：短剧助手", "drama_read", "drama_update_outline", "drama_update_episode", "drama_update_segment", "drama_run_job", "revision", "67005", "67004", "审批", "不等待", "撤销本轮", "角色卡由资产拆解"} {
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

func TestSystem_TeachesCharacterCards(t *testing.T) {
	base := prompts.System("all")
	for _, word := range []string{"人物用角色卡", "kind 为 character", "data.character", "先看画布上有没有这个角色的卡", "每个角色一张角色卡", "绑定人物音色", "参考音频", "voicePrompt"} {
		if !strings.Contains(base, word) {
			t.Errorf("提示词应讲清角色卡的用法，缺少 %q", word)
		}
	}
	if strings.Contains(base, "每个角色一个图片节点") {
		t.Error("人物不再用普通图片节点代替角色卡")
	}
}
