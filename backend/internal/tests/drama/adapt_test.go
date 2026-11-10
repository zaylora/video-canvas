package drama_test

import (
	"strings"
	"testing"

	"video-canvas/internal/drama"
)

const adaptSource = "【场景】@[垂钓台](n_scene)\n【角色】@[林阳](n_lin)、@[刘峰](n_liu)\n【时间轴】0-5秒：中景，@[林阳](n_lin)按住石碑。5-9秒：近景，@[刘峰](n_liu)嗤笑。"

func TestAdaptFormatSupported(t *testing.T) {
	for format, want := range map[string]bool{"h3": true, "generic": true, "seedance": false, "": false, "sora": false} {
		if got := drama.AdaptFormatSupported(format); got != want {
			t.Fatalf("format=%q got %v", format, got)
		}
	}
}

func TestSourceHash(t *testing.T) {
	// 与前端 Web Crypto 的 SHA-256(UTF-8) 小写 hex 一致
	if got := drama.SourceHash("abc"); got != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatalf("hash=%s", got)
	}
	if drama.SourceHash("原稿") == drama.SourceHash("原稿 ") {
		t.Fatal("内容不同指纹应不同")
	}
}

func TestAdaptPrompt(t *testing.T) {
	for _, tc := range []struct{ format, mark string }{
		{"h3", "integrated_multimodal_description"},
		{"generic", "起止秒数"},
	} {
		prompt, err := drama.AdaptPrompt(tc.format, drama.AdaptInput{Source: adaptSource, Seconds: 9, Ratio: "9:16", Style: "真人都市电影"})
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{tc.mark, `"source_prompt"`, `"seconds":9`, `"required":["prompt"]`, "@[名称](node_id)", "n_lin"} {
			if !strings.Contains(prompt, want) {
				t.Fatalf("%s 转写提示词缺少 %q", tc.format, want)
			}
		}
	}
	if _, err := drama.AdaptPrompt("seedance", drama.AdaptInput{Source: adaptSource, Seconds: 9}); err == nil {
		t.Fatal("seedance 不需要转写，应报错")
	}
}

func TestDecodeAdaptOutput(t *testing.T) {
	out, err := drama.DecodeOutput("adapt", []byte(`{"prompt":"转写稿"}`))
	if err != nil || out.(drama.AdaptOutput).Prompt != "转写稿" {
		t.Fatalf("out=%v err=%v", out, err)
	}
	for _, bad := range []string{`{"prompt":""}`, `{"prompt":"x","extra":1}`, `{}`} {
		if _, err := drama.DecodeOutput("adapt", []byte(bad)); err == nil {
			t.Fatalf("应拒绝 %s", bad)
		}
	}
}

func TestValidateAdapt(t *testing.T) {
	h3 := "integrated_multimodal_description: [Shot 1] @[垂钓台](n_scene), @[林阳](n_lin) presses the stele. [Shot 2] At 00:05.000, the camera cuts to @[刘峰](n_liu).\n\noverall_soundscape: hum.\n\nnon_diegetic_music: No non-diegetic music."
	generic := "@[垂钓台](n_scene) 0-5秒：中景，@[林阳](n_lin)按住石碑。5-9秒：近景，@[刘峰](n_liu)嗤笑。"
	for _, tc := range []struct {
		name, format, out string
		ok                bool
	}{
		{"h3 合格", "h3", h3, true},
		{"通用合格", "generic", generic, true},
		{"空稿", "generic", "  ", false},
		{"丢了引用", "generic", strings.Replace(generic, "@[刘峰](n_liu)", "刘峰", 1), false},
		{"编造引用", "generic", generic + "@[路人](n_x)", false},
		{"h3 缺字段", "h3", strings.Replace(h3, "overall_soundscape", "soundscape", 1), false},
		{"切点超时长", "h3", strings.Replace(h3, "At 00:05.000", "At 00:12.000", 1), false},
		{"时间轴超时长", "generic", strings.Replace(generic, "5-9秒", "5-12秒", 1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := drama.ValidateAdapt(tc.format, adaptSource, tc.out, 9)
			if (err == nil) != tc.ok {
				t.Fatalf("err=%v", err)
			}
		})
	}
}
