package drama_test

import (
	"strings"
	"testing"

	. "video-canvas/internal/drama"
)

// TestPromptContentRequirements 锁定内置提示词的关键质量要求，防止被改回小说腔或笼统写法。
func TestPromptContentRequirements(t *testing.T) {
	tests := []struct {
		step string
		want []string
	}{
		{"scripts", []string{"△", "场次标题行", "集号-场号 日/夜 内/外", "（OS）", "（VO）", "【闪回】", "【字幕：", "集尾钩子", "禁止小说式写法"}},
		{"breakdown", []string{"角色表、场景表、道具表", "变体划分", "别名", "群演", "复用", "跨集"}},
		{"asset_prompts", []string{"三视图设定图", "正面", "背面", "空间格局图", "产品参考图", "纯净", "不出现人物"}},
	}
	for _, tt := range tests {
		t.Run(tt.step, func(t *testing.T) {
			prompt, err := Prompt(tt.step, map[string]any{})
			if err != nil {
				t.Fatal(err)
			}
			for _, w := range tt.want {
				if !strings.Contains(prompt, w) {
					t.Errorf("%s 提示词缺少关键要求 %q", tt.step, w)
				}
			}
			for _, banned := range []string{"Seedance", "Minimax", "MiniMax"} {
				if strings.Contains(prompt, banned) {
					t.Errorf("%s 提示词不应出现平台名 %q", tt.step, banned)
				}
			}
		})
	}
}
