package modelcfg_test

import (
	"testing"

	"video-canvas/internal/provider/modelcfg"
)

func TestParseModel_PromptFormat(t *testing.T) {
	for _, format := range []string{"", "seedance", "h3"} {
		t.Run("合法取值/"+format, func(t *testing.T) {
			m := mcLoadFixture(t, "model_video.json")
			mcCaps(m)["prompt_format"] = format
			got, issues := modelcfg.ParseModel(mcMarshal(t, m))
			if got == nil || got.Capabilities.PromptFormat != format || got.Capabilities.Public().PromptFormat != format {
				t.Fatalf("prompt_format 应原样保留并对外透出: got=%+v issues=%v", got, mcIssueList(issues))
			}
		})
	}
	t.Run("不写等于通用", func(t *testing.T) {
		m := mcLoadFixture(t, "model_video.json")
		delete(mcCaps(m), "prompt_format")
		got, issues := modelcfg.ParseModel(mcMarshal(t, m))
		if got == nil || got.Capabilities.PromptFormat != "" {
			t.Fatalf("got=%+v issues=%v", got, mcIssueList(issues))
		}
	})
	mcRunCases(t, []mcCase{
		{"未知取值被拒", func(m map[string]any) { mcCaps(m)["prompt_format"] = "sora" }, "capabilities.prompt_format", "seedance"},
		{"大小写敏感", func(m map[string]any) { mcCaps(m)["prompt_format"] = "Seedance" }, "capabilities.prompt_format", ""},
		{"非视频模型不能声明", func(m map[string]any) { mcAsText(m); mcCaps(m)["prompt_format"] = "h3" }, "capabilities.prompt_format", "视频"},
	})
}
