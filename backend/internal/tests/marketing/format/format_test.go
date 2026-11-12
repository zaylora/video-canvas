package format_test

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"video-canvas/internal/marketing"
	"video-canvas/internal/marketing/format"
)

// repeatScene 生成不含标点的长画面描述，方便按字符数判断截断。
func repeatScene(prefix string, n int) string {
	return prefix + strings.Repeat("橙色软管", n)
}

// fixture 返回一份创意脚本的渲染输入：3 个镜头、5 条外观锚点、带参考拆解里的关键颜色。
func fixture() format.Input {
	return format.Input{
		Facts: marketing.ProductFacts{
			Name:     "晨雾香水 50ml",
			Category: "香水",
			Anchors:  []string{"方形磨砂玻璃瓶身", "拉丝金色圆柱瓶颈", "滚花金色瓶盖", "瓶身正面黑色 BRUME 字样", "瓶盖顶部压印 B 标"},
		},
		Script: marketing.Script{
			Angle: "pain", Title: "早八来不及", Hook: "出门前三十秒",
			Shots: []marketing.ScriptShot{
				{Start: 0, End: 5, Purpose: "hook", Shot: "近景", Camera: "手持推近", Scene: repeatScene("女性手握香水瓶对着镜头，", 25), Narration: "早八来不及？"},
				{Start: 5, End: 10, Purpose: "proof", Shot: "特写", Camera: "固定", Scene: repeatScene("瓶身特写光线流过，", 25), Narration: "喷一下就出门"},
				{Start: 10, End: 15, Purpose: "cta", Shot: "中景", Camera: "缓慢拉远", Scene: repeatScene("女性走出门，", 25), Narration: "点下方链接带走它"},
			},
			Music: "轻快钢琴", Voice: "温柔女声",
		},
		Platform:      marketing.PlatformOf("douyin"),
		Language:      "zh",
		AspectRatio:   "9:16",
		Duration:      15,
		ProductImages: 2,
		HasModel:      true,
		Reverse: &marketing.Reverse{Shots: []marketing.ReverseShot{
			{ID: 1, Start: 0, End: 7, KeyColors: []string{"橙色软管", "米白毛衣"}},
			{ID: 2, Start: 7, End: 15, KeyColors: []string{"橙色软管", "奶油色背景"}},
		}},
	}
}

// followFixture 返回跟拍脚本的渲染输入：每镜带参考镜头 id，并有参考视频。
func followFixture() format.Input {
	in := fixture()
	in.Script.Angle = marketing.AngleFollow
	in.HasRefVideo = true
	r1, r2, r3 := 1, 2, 2
	in.Script.Shots[0].RefShot = &r1
	in.Script.Shots[1].RefShot = &r2
	in.Script.Shots[2].RefShot = &r3
	return in
}

func mustRender(t *testing.T, f format.VideoFormatter, in format.Input) string {
	t.Helper()
	out, err := f.Render(in)
	if err != nil {
		t.Fatalf("渲染失败：%v", err)
	}
	return out
}

func runes(s string) int { return utf8.RuneCountInString(s) }

func TestFor_与_All(t *testing.T) {
	all := format.All()
	keys := []string{}
	for _, f := range all {
		keys = append(keys, f.Key())
		if f.Name() == "" || f.Description() == "" {
			t.Errorf("格式化器 %s 缺名称或说明", f.Key())
		}
	}
	if strings.Join(keys, ",") != "seedance,h3,generic" {
		t.Fatalf("All 应返回 seedance、h3、generic，实际 %v", keys)
	}
	for in, want := range map[string]string{"seedance": "seedance", "h3": "h3", "generic": "generic", "": "generic", "unknown": "generic"} {
		if got := format.For(in).Key(); got != want {
			t.Errorf("For(%q) 应为 %s，实际 %s", in, want, got)
		}
	}
}

func TestSeedance_分区块结构(t *testing.T) {
	out := mustRender(t, format.For("seedance"), fixture())
	last := -1
	for _, h := range []string{"[基本参数]", "[参考素材]", "[整体视觉]", "[商品]", "[分镜]", "[音乐与配音]", "[硬性约束]"} {
		i := strings.Index(out, h)
		if i < 0 {
			t.Fatalf("缺少区块 %s：\n%s", h, out)
		}
		if i <= last {
			t.Fatalf("区块 %s 的顺序不对：\n%s", h, out)
		}
		last = i
	}
	for _, want := range []string{
		"[基本参数] 9:16，15 秒，有声",
		"@图片1", "@图片2", "@图片3：出镜人物的唯一依据",
		"晨雾香水 50ml", "禁止重新设计、改色、改形、改 logo",
		"痛点",                     // 角度名
		"前 3 秒强钩子",               // 平台写法要点
		"方形磨砂玻璃瓶身", "瓶盖顶部压印 B 标", // 外观锚点逐条
		"镜头1（0–5s）", "镜头2（5–10s）", "镜头3（10–15s）",
		"近景，手持推近", "旁白：「早八来不及？」", "旁白：「点下方链接带走它」",
		"旁白：中文，温柔女声", "配乐：轻快钢琴",
		"商品外观与参考图一致", "商品不出画", "不出现任何文字、字幕、花字或水印",
		"橙色软管", "米白毛衣", "奶油色背景", // 关键颜色并集
	} {
		if !strings.Contains(out, want) {
			t.Errorf("缺少 %q：\n%s", want, out)
		}
	}
	if strings.Contains(out, "@视频1") {
		t.Errorf("没有参考视频时不应出现 @视频1")
	}
	for _, ban := range fixture().Platform.Bans {
		if !strings.Contains(out, ban) {
			t.Errorf("硬性约束应带平台禁令 %q", ban)
		}
	}
}

func TestSeedance_每镜开头是主体和动作(t *testing.T) {
	in := fixture()
	out := mustRender(t, format.For("seedance"), in)
	for i, s := range in.Script.Shots {
		line := ""
		for _, l := range strings.Split(out, "\n") {
			if strings.HasPrefix(l, "镜头"+string(rune('1'+i))+"（") {
				line = l
			}
		}
		if line == "" {
			t.Fatalf("找不到镜头 %d：\n%s", i+1, out)
		}
		idx := strings.Index(line, s.Scene)
		head := strings.Index(line, "）") + len("）")
		if idx != head {
			t.Errorf("镜头 %d 的主体+动作应紧跟在时间段后：%s", i+1, line)
		}
		if strings.Index(line, "旁白") < idx || strings.Index(line, s.Shot) < idx {
			t.Errorf("景别、运镜、旁白应在画面描述之后：%s", line)
		}
	}
}

func TestSeedance_跟拍带参考视频和对应镜头(t *testing.T) {
	out := mustRender(t, format.For("seedance"), followFixture())
	if !strings.Contains(out, "@视频1：只参考节奏、构图与运镜，不照搬其中的人物外貌、商品、品牌与画面文字") {
		t.Errorf("参考素材应说明 @视频1：\n%s", out)
	}
	if !strings.Contains(out, "跟拍") {
		t.Errorf("整体视觉应写明跟拍")
	}
	for _, want := range []string{"参考镜头1", "参考镜头2", "@视频1 的 0–7s", "@视频1 的 7–15s"} {
		if !strings.Contains(out, want) {
			t.Errorf("每镜应注明对应的参考镜头，缺 %q：\n%s", want, out)
		}
	}
}

func TestSeedance_有参考视频的改编脚本也带视频说明(t *testing.T) {
	in := fixture()
	in.HasRefVideo = true
	out := mustRender(t, format.For("seedance"), in)
	if !strings.Contains(out, "@视频1") {
		t.Errorf("HasRefVideo 时应出现 @视频1")
	}
	if strings.Contains(out, "参考镜头") {
		t.Errorf("改编脚本没有 ref_shot，不应写参考镜头")
	}
}

func TestSeedance_英文脚本(t *testing.T) {
	in := fixture()
	in.Language = "en"
	in.Platform = marketing.PlatformOf("tiktok")
	in.Script.Shots[0].Scene = "A woman holds the perfume bottle to the camera"
	in.Script.Shots[0].Narration = "Late again?"
	in.Script.Voice = "warm female voice"
	out := mustRender(t, format.For("seedance"), in)
	for _, want := range []string{"[基本参数]", "[分镜]", "A woman holds the perfume bottle to the camera", "Voiceover: \"Late again?\"", "旁白：英文", "warm female voice"} {
		if !strings.Contains(out, want) {
			t.Errorf("英文脚本缺 %q：\n%s", want, out)
		}
	}
}

func TestGenericAndH3_段落写法(t *testing.T) {
	for _, key := range []string{"generic", "h3"} {
		f := format.For(key)
		out := mustRender(t, f, fixture())
		if strings.Contains(out, "[基本参数]") || strings.Contains(out, "[分镜]") {
			t.Errorf("%s 不应使用分区块标题：\n%s", key, out)
		}
		for _, want := range []string{
			"9:16", "15 秒", "@图片1", "@图片3", "晨雾香水 50ml", "方形磨砂玻璃瓶身",
			"镜头1（0–5s）", "旁白：「早八来不及？」", "温柔女声", "轻快钢琴", "商品不出画", "橙色软管",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("%s 缺 %q：\n%s", key, want, out)
			}
		}
	}
	if mustRender(t, format.For("h3"), fixture()) != mustRender(t, format.For("generic"), fixture()) {
		t.Error("h3 暂用通用写法，内容应与 generic 相同")
	}
}

func TestGeneric_跟拍带参考视频(t *testing.T) {
	out := mustRender(t, format.For("generic"), followFixture())
	if !strings.Contains(out, "@视频1") || !strings.Contains(out, "参考镜头2") {
		t.Errorf("通用写法的跟拍也应带 @视频1 与对应参考镜头：\n%s", out)
	}
}

// 超长删减：每一步都与「已去掉该项的输入」渲染结果完全相同，保证规则确定。
func TestTruncate_按顺序逐步删减(t *testing.T) {
	for _, key := range []string{"seedance", "generic"} {
		t.Run(key, func(t *testing.T) {
			f := format.For(key)
			base := fixture()
			l0 := mustRender(t, f, base)

			noStyle := base
			noStyle.Platform.Style = ""
			l1 := mustRender(t, f, noStyle)

			anchors3 := noStyle
			anchors3.Facts.Anchors = anchors3.Facts.Anchors[:3]
			l2 := mustRender(t, f, anchors3)

			noColor := anchors3
			noColor.Reverse = nil
			l3 := mustRender(t, f, noColor)

			if runes(l0) <= runes(l1) || runes(l1) <= runes(l2) || runes(l2) <= runes(l3) {
				t.Fatalf("固定的前三步应各自变短：%d %d %d %d", runes(l0), runes(l1), runes(l2), runes(l3))
			}
			cases := []struct {
				name   string
				maxLen int
				want   string
			}{
				{"刚好够用不删", runes(l0), l0},
				{"第一步去掉平台写法要点", runes(l0) - 1, l1},
				{"第二步锚点只留前三条", runes(l1) - 1, l2},
				{"第三步去掉关键颜色", runes(l2) - 1, l3},
			}
			for _, c := range cases {
				in := base
				in.MaxLen = c.maxLen
				if got := mustRender(t, f, in); got != c.want {
					t.Errorf("%s：结果与预期不符\n--- got\n%s\n--- want\n%s", c.name, got, c.want)
				}
			}
		})
	}
}

func TestTruncate_第四步逐步截短画面描述(t *testing.T) {
	f := format.For("seedance")
	base := fixture()
	noColor := base
	noColor.Platform.Style = ""
	noColor.Facts.Anchors = noColor.Facts.Anchors[:3]
	noColor.Reverse = nil
	l3 := runes(mustRender(t, f, noColor))

	// 只差一点：每镜画面截到 80 字就够。
	in := base
	in.MaxLen = l3 - 1
	out := mustRender(t, f, in)
	for _, s := range base.Script.Shots {
		head := string([]rune(s.Scene)[:80])
		if !strings.Contains(out, head) || strings.Contains(out, string([]rune(s.Scene)[:81])) {
			t.Errorf("画面描述应截到 80 字并保留开头：%s", s.Scene)
		}
		if !strings.Contains(out, s.Narration) {
			t.Errorf("旁白不能删：%s", s.Narration)
		}
	}
	if runes(out) > in.MaxLen {
		t.Fatalf("结果 %d 字，超出上限 %d", runes(out), in.MaxLen)
	}

	// 差得多：继续逐步截短，但保留镜头开头的主体与动作，旁白始终完整。
	in.MaxLen = l3 - 200
	out = mustRender(t, f, in)
	if runes(out) > in.MaxLen {
		t.Fatalf("结果 %d 字，超出上限 %d", runes(out), in.MaxLen)
	}
	for _, s := range base.Script.Shots {
		if !strings.Contains(out, string([]rune(s.Scene)[:12])) {
			t.Errorf("开头的主体+动作应保留：%s", s.Scene)
		}
		if !strings.Contains(out, "旁白：「"+s.Narration+"」") {
			t.Errorf("旁白必须完整保留：%s", s.Narration)
		}
	}
}

func TestTruncate_仍超返回ErrTooLong(t *testing.T) {
	for _, key := range []string{"seedance", "h3", "generic"} {
		in := fixture()
		in.MaxLen = 40
		out, err := format.For(key).Render(in)
		if !errors.Is(err, format.ErrTooLong) || out != "" {
			t.Errorf("%s 应返回 ErrTooLong，实际 %q %v", key, out, err)
		}
	}
}

func TestRenderShot_单镜头稿(t *testing.T) {
	in := fixture()
	f := format.For("seedance")
	out, err := f.RenderShot(in, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[基本参数] 9:16，5 秒，有声", "镜头2（0–5s）", in.Script.Shots[1].Scene, "旁白：「喷一下就出门」", "[硬性约束]", "方形磨砂玻璃瓶身", "@图片1"} {
		if !strings.Contains(out, want) {
			t.Errorf("单镜头稿缺 %q：\n%s", want, out)
		}
	}
	for _, other := range []string{in.Script.Shots[0].Scene, in.Script.Shots[2].Scene, "早八来不及"} {
		if strings.Contains(out, other) {
			t.Errorf("单镜头稿不应含其他镜头的内容 %q", other)
		}
	}
	if _, err := f.RenderShot(in, 3); err == nil {
		t.Error("下标越界应报错")
	}
	if _, err := f.RenderShot(in, -1); err == nil {
		t.Error("负下标应报错")
	}
	g, err := format.For("generic").RenderShot(in, 0)
	if err != nil || !strings.Contains(g, "镜头1") || strings.Contains(g, "[基本参数]") {
		t.Errorf("通用写法的单镜头稿不对：%v\n%s", err, g)
	}
}

func TestRenderShot_跟拍带对应参考镜头并遵守上限(t *testing.T) {
	in := followFixture()
	out, err := format.For("seedance").RenderShot(in, 1)
	if err != nil || !strings.Contains(out, "参考镜头2") || !strings.Contains(out, "@视频1") {
		t.Fatalf("跟拍单镜头稿应带参考镜头：%v\n%s", err, out)
	}
	in.MaxLen = 30
	if _, err := format.For("seedance").RenderShot(in, 1); !errors.Is(err, format.ErrTooLong) {
		t.Fatalf("单镜头稿也应遵守上限：%v", err)
	}
}

func TestSampleOf(t *testing.T) {
	for _, f := range format.All() {
		a, b := format.SampleOf(f), format.SampleOf(f)
		if a == "" || a != b {
			t.Errorf("%s 样例稿应非空且稳定", f.Key())
		}
		if !strings.Contains(a, "镜头1") {
			t.Errorf("%s 样例稿应含分镜", f.Key())
		}
	}
	if s := format.SampleOf(format.For("seedance")); !strings.Contains(s, "[分镜]") {
		t.Errorf("seedance 样例稿应是分区块写法")
	}
}

func TestRender_没有模特图和商品图时省略对应说明(t *testing.T) {
	in := fixture()
	in.HasModel = false
	in.ProductImages = 1
	out := mustRender(t, format.For("seedance"), in)
	if strings.Contains(out, "@图片2") || strings.Contains(out, "模特") {
		t.Errorf("无模特图时不应出现 @图片2 / 模特：\n%s", out)
	}
	if !strings.Contains(out, "@图片1") {
		t.Errorf("应保留 @图片1")
	}
}

// 有模特图的跟拍：模特图是出镜人物的唯一依据，画面描述里的「模特图」换成 @图片k，
// 参考视频只取节奏运镜、不取人物与字幕，硬性约束写明人物不沿用参考视频。两种写法都要做到。
func TestRender_有模特图时人物以模特图为准(t *testing.T) {
	in := followFixture()
	in.Script.Shots[0].Scene = "模特图中的女性手握香水瓶对着镜头"
	for _, key := range []string{"seedance", "generic"} {
		out := mustRender(t, format.For(key), in)
		for _, want := range []string{
			"@图片3", "出镜人物的唯一依据",
			"@图片3中的女性手握香水瓶",
			"不照搬其中的人物外貌",
			"出镜人物始终是 @图片3 的模特，不出现 @视频1 里的人物",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("%s 缺 %q：\n%s", key, want, out)
			}
		}
		if strings.Contains(out, "模特图中的女性") {
			t.Errorf("%s 画面描述里的「模特图」应换成 @图片3：\n%s", key, out)
		}
	}
}

// 成片不加字幕：基本参数写「无字幕」，参考视频说明不照搬字幕，硬性约束写明旁白只配音不上字幕。
func TestRender_成片不加字幕(t *testing.T) {
	for _, key := range []string{"seedance", "generic"} {
		out := mustRender(t, format.For(key), followFixture())
		for _, want := range []string{"有声，无字幕", "不照搬其中的人物外貌、商品、品牌与画面文字", "不加字幕：旁白只配音"} {
			if !strings.Contains(out, want) {
				t.Errorf("%s 缺 %q：\n%s", key, want, out)
			}
		}
	}
}
