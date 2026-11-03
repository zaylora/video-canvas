package drama_test

import (
	"strings"
	"testing"

	"video-canvas/internal/drama"
)

// 每一步都要拼进对应的内置技能正文，步骤自己的输出契约放在最后
func TestPromptIncludesStepSkills(t *testing.T) {
	cases := map[string]string{
		"analyze":    "name: short-drama-novel-analyze",
		"outline":    "name: short-drama-develop",
		"scripts":    "钩子设计",
		"breakdown":  "name: short-drama-assets",
		"storyboard": "name: short-drama-video-prompts",
	}
	for step, want := range cases {
		prompt, err := drama.Prompt(step, map[string]any{})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(prompt, want) {
			t.Fatalf("%s 提示词缺少技能 %s", step, want)
		}
		skill := strings.Index(prompt, "参考技能")
		contract := strings.LastIndex(prompt, "## 输出 JSON Schema")
		if skill < 0 || contract < skill {
			t.Fatalf("%s 的参考技能应在步骤契约之前", step)
		}
	}
}

// 资产提示词按图片模型选方言，分镜按视频模型选；认不出的模型只用通用写法
func TestPromptPicksModelDialect(t *testing.T) {
	cases := []struct {
		name    string
		step    string
		targets drama.Targets
		want    string
		not     string
	}{
		{"Seedance 2.0", "storyboard", drama.Targets{Video: drama.TargetModel{Upstream: "doubao-seedance-2-0-pro"}}, "# Seedance 2.0 提示词方言", "# MiniMax H3 提示词方言"},
		{"Seedance 2.5", "storyboard", drama.Targets{Video: drama.TargetModel{Upstream: "seedance-2.5"}}, "# Seedance 2.5 提示词方言", "# Seedance 2.0 提示词方言"},
		{"可灵视频", "storyboard", drama.Targets{Video: drama.TargetModel{Vendor: "kling", Upstream: "kling-v3"}}, "# Kling reference", ""},
		{"Veo", "storyboard", drama.Targets{Video: drama.TargetModel{Upstream: "veo-3.1"}}, "# Veo reference", ""},
		{"海螺", "storyboard", drama.Targets{Video: drama.TargetModel{Vendor: "minimax", Upstream: "hailuo-02"}}, "# MiniMax H3 提示词方言", ""},
		{"万相", "storyboard", drama.Targets{Video: drama.TargetModel{Upstream: "wan2.6-i2v"}}, "# Wan 3.0 提示词方言", ""},
		{"Nano Banana", "asset_prompts", drama.Targets{Image: drama.TargetModel{Upstream: "gemini-3-pro-image-preview", Label: "Nano Banana Pro"}}, "### Nano Banana Pro", "### Seedream 4.5"},
		{"GPT Image", "asset_prompts", drama.Targets{Image: drama.TargetModel{Upstream: "gpt-image-2"}}, "### GPT Image 2", ""},
		{"Seedream", "asset_prompts", drama.Targets{Image: drama.TargetModel{Upstream: "doubao-seedream-5-0"}}, "### Seedream 5.0 Pro", "### Nano Banana Pro"},
		{"Flux", "asset_prompts", drama.Targets{Image: drama.TargetModel{Upstream: "flux-kontext-max"}}, "### Flux Kontext", "### Seedream 4.5"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			prompt, err := drama.PromptFor(c.step, map[string]any{}, c.targets)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(prompt, c.want) {
				t.Fatalf("缺少 %s 的写法", c.want)
			}
			if c.not != "" && strings.Contains(prompt, c.not) {
				t.Fatalf("不该带上 %s", c.not)
			}
		})
	}
	generic, err := drama.PromptFor("storyboard", map[string]any{}, drama.Targets{Video: drama.TargetModel{Upstream: "unknown-video"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, dialect := range []string{"# Seedance 2.0 提示词方言", "# MiniMax H3 提示词方言", "# Wan 3.0 提示词方言", "# Kling reference", "# Veo reference"} {
		if strings.Contains(generic, dialect) {
			t.Fatalf("认不出的模型不该带上 %s", dialect)
		}
	}
}

// 报价要把参考资料的长度算进去，否则冻结的积分不够
func TestReferenceChars(t *testing.T) {
	base := drama.ReferenceChars("storyboard", drama.Targets{})
	seedance := drama.ReferenceChars("storyboard", drama.Targets{Video: drama.TargetModel{Upstream: "seedance-2.0"}})
	if base <= 0 || seedance <= base {
		t.Fatalf("参考资料长度不对：通用 %d，Seedance %d", base, seedance)
	}
}

// 视频平台对参考音频的限制：认得的平台带上结构化上限，拆解按它算每个角色试音的秒数
func TestVideoTargetAudioLimit(t *testing.T) {
	seedance := drama.NewVideoTarget("vid", "Seedance 2.0", "bytedance", "doubao-seedance-2-0")
	if seedance.RefAudio == nil || seedance.RefAudio.MaxSeconds != 15 || seedance.RefAudio.TotalSeconds != 15 || seedance.RefAudio.MaxClips != 3 {
		t.Fatalf("Seedance 参考音频上限不对：%+v", seedance.RefAudio)
	}
	if got := drama.VoiceSeconds(drama.Targets{Video: seedance}); got != 5 {
		t.Fatalf("Seedance 每个角色试音应为 5 秒，得到 %d", got)
	}
	unknown := drama.NewVideoTarget("x", "某模型", "", "unknown")
	if unknown.RefAudio != nil {
		t.Fatalf("认不出的平台不该编造上限：%+v", unknown.RefAudio)
	}
	if got := drama.VoiceSeconds(drama.Targets{Video: unknown}); got != drama.DefaultVoiceSeconds {
		t.Fatalf("认不出的平台用缺省秒数，得到 %d", got)
	}
}

// 拆解也要按视频平台的写法：带上该平台的方言资料，提示词里写明试音秒数
func TestBreakdownPromptFollowsVideoPlatform(t *testing.T) {
	targets := drama.Targets{Video: drama.NewVideoTarget("vid", "MiniMax H3", "minimax", "MiniMax-H3")}
	prompt, err := drama.PromptFor("breakdown", map[string]any{"voice_seconds": drama.VoiceSeconds(targets)}, targets)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# MiniMax H3 提示词方言", "voice_seconds", "试音台词"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("拆解提示词缺少 %s", want)
		}
	}
}
