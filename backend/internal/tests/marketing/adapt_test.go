package marketing_test

import (
	"encoding/json"
	"strings"
	"testing"

	"video-canvas/internal/marketing"
)

// adaptIn 是一份跟拍的转写输入：1 张商品图 + 模特图 + 参考视频，3 个镜头、15 秒。
func adaptIn() marketing.AdaptInput {
	return marketing.AdaptInput{
		Source: "[参考素材] @图片1：可口可乐……\n@图片2：模特……\n@视频1：只参考节奏……", Seconds: 15, ProductImages: 1, HasModel: true, HasRefVideo: true,
		Shots: []marketing.AdaptShot{
			{No: 1, Narration: "这瓶我真的是要说倦了！"},
			{No: 2, Cut: "00:09.300", Narration: "正面红底白字，清楚吧？"},
			{No: 3, Cut: "00:11.900", Narration: "下半部有凸起圆点。"},
		},
	}
}

// goodAdapt 是一份合格的 H3 转写稿。
func goodAdapt() string {
	return `subject_definitions:
<Subject 1> is the Coca-Cola bottle shown in <Picture 1>, with a red label and white "可口可乐" lettering.
<Subject 2> is the presenter whose face, hairstyle and outfit come from <Picture 2>.
<Video 1> provides only the cut, pacing and camera structure.

summary:
[reference generation] <Subject 2> presents <Subject 1> in a bright living room, following the pacing of <Video 1>.

retention_analysis:
<Subject 1> (appears in [Shot 1], [Shot 2], [Shot 3]): fully_preserved - the bottle shape and label are retained.
<Subject 2> (appears in [Shot 1], [Shot 2], [Shot 3]): fully_preserved - the presenter's face and outfit are retained.
<Video 1> (cut and pacing structure): weak_reference - only pacing and camera movement are reused; its people and products do not appear.

detailed_description:
The target video is a realistic vertical product video with soft daylight.
[Shot 1] A medium shot shows <Subject 2> holding a woven basket with three bottles of <Subject 1>. <Subject 2> (S1) says to the camera: <d>[Chinese] 这瓶我真的是要说倦了</d>
[Shot 2] At 00:09.300, the shot cuts to a close-up of one bottle of <Subject 1>. The presenter's voice (S1) says in an off-screen voiceover: <d>[Chinese] 正面红底白字，清楚吧？</d>
[Shot 3] At 00:11.900, the shot cuts to the lower half of the bottle. <Subject 2> (S1) says: <d>[Chinese] 下半部有凸起圆点。</d>

overall_soundscape:
Quiet indoor room tone and the soft clink of glass bottles.

non_diegetic_music:
A light acoustic pop track at low volume under the voice.`
}

func TestAdapt_合格的转写稿通过校验(t *testing.T) {
	if err := marketing.ValidateAdapt(adaptIn(), goodAdapt()); err != nil {
		t.Fatalf("应通过：%v", err)
	}
}

func TestAdapt_不合格的转写稿给出原因(t *testing.T) {
	cases := []struct {
		name string
		edit func(string) string
		want string
	}{
		{"缺段落", func(s string) string { return strings.Replace(s, "overall_soundscape:", "soundscape:", 1) }, "overall_soundscape"},
		{"段落顺序错", func(s string) string {
			return strings.Replace(strings.Replace(s, "summary:", "TMP:", 1), "subject_definitions:", "summary:", 1)
		}, "subject_definitions"},
		{"留着 @图片", func(s string) string { return strings.Replace(s, "<Picture 1>", "@图片1", 1) }, "@图片"},
		{"缺模特图标签", func(s string) string { return strings.ReplaceAll(s, "<Picture 2>", "the photo") }, "<Picture 2>"},
		{"多出参考图", func(s string) string { return s + " <Picture 3>" }, "<Picture 3>"},
		{"多出音频", func(s string) string { return s + " <Audio 1>" }, "<Audio 1>"},
		{"镜头数不对", func(s string) string {
			return strings.Replace(s, "[Shot 3] At 00:11.900, the shot cuts to the lower half of the bottle.", "Then the camera tilts down.", 1)
		}, "镜头数"},
		{"切点超出时长", func(s string) string { return strings.Replace(s, "At 00:11.900", "At 00:15.200", 1) }, "切入时间"},
		{"切点不递增", func(s string) string { return strings.Replace(s, "At 00:11.900", "At 00:08.000", 1) }, "切入时间"},
		{"第二镜没有时间", func(s string) string { return strings.Replace(s, "[Shot 2] At 00:09.300,", "[Shot 2]", 1) }, "At MM:SS.mmm"},
		{"旁白被改", func(s string) string { return strings.Replace(s, "正面红底白字", "正面是红色的", 1) }, "旁白原文"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := marketing.ValidateAdapt(adaptIn(), c.edit(goodAdapt()))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("应报含 %q 的错误，实际：%v", c.want, err)
			}
		})
	}
}

func TestAdapt_没有参考视频时不能出现Video标签(t *testing.T) {
	in := adaptIn()
	in.HasRefVideo = false
	err := marketing.ValidateAdapt(in, goodAdapt())
	if err == nil || !strings.Contains(err.Error(), "<Video 1>") {
		t.Fatalf("没有参考视频时出现 <Video 1> 应报错：%v", err)
	}
}

func TestAdapt_超过上限要精简(t *testing.T) {
	in := adaptIn()
	in.MaxLen = 200
	err := marketing.ValidateAdapt(in, goodAdapt())
	if err == nil || !strings.Contains(err.Error(), "超过上限 200") {
		t.Fatalf("超长应报错：%v", err)
	}
}

func TestAdapt_标签清单与提示词输入(t *testing.T) {
	refs := marketing.AdaptRefs(marketing.AdaptInput{ProductImages: 2, HasModel: true, HasRefVideo: true})
	got := []string{}
	for _, r := range refs {
		got = append(got, r.Label+"="+r.Source)
	}
	want := "<Picture 1>=@图片1 <Picture 2>=@图片2 <Picture 3>=@图片3 <Video 1>=@视频1"
	if strings.Join(got, " ") != want {
		t.Fatalf("标签对应不对：%v", got)
	}
	in := adaptIn()
	in.ValidationError = "缺少段落 summary:"
	prompt, err := marketing.AdaptPrompt(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{marketing.AdaptDuty, "subject_definitions", "weak_reference", `"source_prompt"`, `"cut": "00:09.300"`, `"validation_error": "缺少段落 summary:"`} {
		if !strings.Contains(prompt, want) {
			t.Errorf("转写提示词缺少 %q", want)
		}
	}
}

func TestAdapt_解码与切点时间(t *testing.T) {
	raw, _ := json.Marshal(map[string]string{"prompt": "  subject_definitions: x  "})
	out, err := marketing.DecodeAdapt("```json\n" + string(raw) + "\n```")
	if err != nil || out != "subject_definitions: x" {
		t.Fatalf("解码：%q %v", out, err)
	}
	if _, err := marketing.DecodeAdapt("好的，下面是转写稿"); err == nil {
		t.Fatal("夹带解释文字应报错")
	}
	for sec, want := range map[float64]string{0: "00:00.000", 9.3: "00:09.300", 75.25: "01:15.250"} {
		if got := marketing.CutTime(sec); got != want {
			t.Errorf("CutTime(%v) = %s，应为 %s", sec, got, want)
		}
	}
	if marketing.NeedsAdapt("seedance") || marketing.NeedsAdapt("generic") || !marketing.NeedsAdapt("h3") {
		t.Fatal("只有 h3 需要转写")
	}
}
