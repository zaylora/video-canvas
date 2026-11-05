package drama_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"video-canvas/internal/drama"
)

func TestKindSampleBounded(t *testing.T) {
	short := "很短的原文"
	if got := drama.KindSample(short); got != short {
		t.Fatalf("短文应原样返回: %q", got)
	}
	long := strings.Repeat("甲", 3000) + strings.Repeat("乙", 3000) + strings.Repeat("丙", 3000)
	got := drama.KindSample(long)
	if n := utf8.RuneCountInString(got); n > 4200 {
		t.Fatalf("采样过长: %d", n)
	}
	// 开头、中间、结尾都要取到，避免只凭序言判断
	for _, want := range []string{"甲", "乙", "丙"} {
		if !strings.Contains(got, want) {
			t.Fatalf("采样缺少 %s 段", want)
		}
	}
}

func TestKindPromptCarriesSampleAndAsksJSON(t *testing.T) {
	prompt := drama.KindPrompt("他走进房间。")
	for _, want := range []string{"他走进房间。", "script", "novel", `"kind"`} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("提示词缺少 %s:\n%s", want, prompt)
		}
	}
}

func TestParseKindGuess(t *testing.T) {
	for _, tc := range []struct {
		name, text, kind, reason string
		ok                       bool
	}{
		{"小说", `{"kind":"novel","reason":"叙述为主"}`, "novel", "叙述为主", true},
		{"剧本", `{"kind":"script","reason":"有场次和对白"}`, "script", "有场次和对白", true},
		{"带围栏", "```json\n{\"kind\":\"script\",\"reason\":\"x\"}\n```", "script", "x", true},
		{"前后有说明", "判断如下：{\"kind\":\"novel\",\"reason\":\"r\"} 完毕", "novel", "r", true},
		{"没有 reason 也行", `{"kind":"novel"}`, "novel", "", true},
		{"未知类型", `{"kind":"poem","reason":"r"}`, "", "", false},
		{"不是 JSON", "我觉得是小说", "", "", false},
		{"空", "", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kind, reason, ok := drama.ParseKindGuess(tc.text)
			if kind != tc.kind || reason != tc.reason || ok != tc.ok {
				t.Fatalf("got (%q,%q,%v)", kind, reason, ok)
			}
		})
	}
}
