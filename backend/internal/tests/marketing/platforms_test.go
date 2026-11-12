package marketing_test

import (
	"strings"
	"testing"

	"video-canvas/internal/marketing"
)

func TestPlatforms_顺序与取值(t *testing.T) {
	want := []string{"douyin", "xhs", "tmall", "tiktok", "amazon", "reels", "generic"}
	got := marketing.Platforms()
	if len(got) != len(want) {
		t.Fatalf("平台数量应为 %d，实际 %d", len(want), len(got))
	}
	for i, p := range got {
		if p.Key != want[i] {
			t.Fatalf("第 %d 个平台应为 %s，实际 %s", i, want[i], p.Key)
		}
		if p.Name == "" || p.Style == "" || p.AspectRatio == "" || p.Language == "" {
			t.Fatalf("平台 %s 有空字段：%+v", p.Key, p)
		}
	}
	checks := map[string]struct {
		ratio, lang string
		domestic    bool
	}{
		"douyin": {"9:16", "zh", true}, "xhs": {"3:4", "zh", true}, "tmall": {"3:4", "zh", true},
		"tiktok": {"9:16", "en", false}, "amazon": {"16:9", "en", false},
		"reels": {"9:16", "en", false}, "generic": {"9:16", "zh", false},
	}
	for _, p := range got {
		c := checks[p.Key]
		if p.AspectRatio != c.ratio || p.Language != c.lang || p.Domestic != c.domestic {
			t.Errorf("平台 %s 设置不符：%+v", p.Key, p)
		}
	}
	amazon := marketing.PlatformOf("amazon")
	if !strings.Contains(strings.Join(amazon.Bans, "；"), "价格") {
		t.Errorf("亚马逊的禁令应提到价格：%v", amazon.Bans)
	}
}

func TestPlatformOf_不认识按通用(t *testing.T) {
	if p := marketing.PlatformOf("nope"); p.Key != "generic" {
		t.Fatalf("未知平台应回退 generic，实际 %s", p.Key)
	}
	if p := marketing.PlatformOf(""); p.Key != "generic" {
		t.Fatalf("空平台应回退 generic，实际 %s", p.Key)
	}
	if p := marketing.PlatformOf("tiktok"); p.Key != "tiktok" {
		t.Fatalf("tiktok 应原样返回")
	}
}

func TestAngles(t *testing.T) {
	want := []string{"pain", "feature", "lifestyle", "compare", "unbox", "mood"}
	got := marketing.Angles()
	if len(got) != len(want) {
		t.Fatalf("角度数量应为 6，实际 %d", len(got))
	}
	for i, a := range got {
		if a.Key != want[i] || a.Name == "" || a.Desc == "" {
			t.Errorf("第 %d 个角度不符：%+v", i, a)
		}
	}
	if !marketing.AngleValid("pain") || marketing.AngleValid("follow") || marketing.AngleValid("") || marketing.AngleValid("x") {
		t.Fatal("AngleValid 判断不对：follow 不属于创意角度")
	}
}

func TestScriptPlan(t *testing.T) {
	tests := []struct {
		name      string
		hasRef    bool
		angles    []string
		autoCount int
		want      int
		wantErr   string
	}{
		{"无参考按角度数", false, []string{"pain", "mood"}, 0, 2, ""},
		{"无参考四个角度", false, []string{"pain", "feature", "lifestyle", "compare"}, 0, 4, ""},
		{"无参考五个角度超限", false, []string{"pain", "feature", "lifestyle", "compare", "unbox"}, 0, 0, "1–4"},
		{"无参考自动份数", false, nil, 3, 3, ""},
		{"无参考自动 0 份不合法", false, nil, 0, 0, "1–4"},
		{"无参考自动 5 份不合法", false, nil, 5, 0, "1–4"},
		{"角度重复", false, []string{"pain", "pain"}, 0, 0, "重复"},
		{"角度不合法", false, []string{"pain", "follow"}, 0, 0, "不支持"},
		{"有参考按角度数", true, []string{"pain"}, 0, 1, ""},
		{"有参考三个角度", true, []string{"pain", "feature", "mood"}, 0, 3, ""},
		{"有参考四个角度超限", true, []string{"pain", "feature", "mood", "unbox"}, 0, 0, "0–3"},
		{"有参考自动 2 份", true, nil, 2, 2, ""},
		{"有参考自动 0 份即只跟拍", true, nil, 0, 0, ""},
		{"有参考自动 4 份超限", true, nil, 4, 0, "0–3"},
		{"角度优先于自动份数", false, []string{"pain"}, 3, 1, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := marketing.ScriptPlan(tt.hasRef, tt.angles, tt.autoCount)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("期望错误含 %q，实际 %v", tt.wantErr, err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("期望 %d，实际 %d，err=%v", tt.want, got, err)
			}
		})
	}
}

func TestNarrationLimit(t *testing.T) {
	tests := []struct {
		lang     string
		duration int
		want     int
	}{
		{"zh", 15, 67}, {"zh", 10, 45}, {"zh", 8, 36}, {"en", 15, 39}, {"en", 10, 26}, {"en", 5, 13},
	}
	for _, tt := range tests {
		if got := marketing.NarrationLimit(tt.lang, tt.duration); got != tt.want {
			t.Errorf("%s %d 秒应为 %d，实际 %d", tt.lang, tt.duration, tt.want, got)
		}
	}
}

func TestNarrationLength(t *testing.T) {
	tests := []struct {
		name, lang, text string
		want             int
	}{
		{"中文去标点和空白", "zh", "早八的你，需要它！ 一抹就好。", 11},
		{"中文数字也算字", "zh", "留香8小时", 5},
		{"英文按词", "en", "Squeeze it, rub it in.", 5},
		{"英文多空格", "en", "  one   two  ", 2},
		{"英文孤立标点不算词", "en", "great — really", 2},
		{"空串", "zh", "", 0},
	}
	for _, tt := range tests {
		if got := marketing.NarrationLength(tt.lang, tt.text); got != tt.want {
			t.Errorf("%s：期望 %d，实际 %d", tt.name, tt.want, got)
		}
	}
}
