package marketing_test

import (
	"testing"

	"video-canvas/internal/marketing"
	"video-canvas/internal/stepkit"
)

func followInput() stepkit.Input {
	return marketing.FollowInput(brief("douyin", "zh", 10), goodFacts(), goodReverse(), marketing.AssetNote{ProductImages: 2, HasModel: true})
}

// goodFollow 是与 goodReverse 一一对应的 4 镜跟拍脚本。
func goodFollow() marketing.FollowOutput {
	shots := goodShots(4, 10, "清晨喷一下")
	return marketing.FollowOutput{
		Title: "晨雾通勤", Hook: "清晨出门前这一步", Shots: shots, Music: "轻快钢琴", Voice: "温柔女声",
		FollowMap: []marketing.FollowMapItem{{RefShot: 1, Shot: 0}, {RefShot: 2, Shot: 1}, {RefShot: 3, Shot: 2}, {RefShot: 4, Shot: 3}},
	}
}

func TestFollowInput_变量(t *testing.T) {
	in := followInput()
	for _, k := range []string{"reverse", "facts", "platform", "language", "duration", "assets"} {
		if _, ok := in.Vars[k]; !ok {
			t.Errorf("Vars 缺 %s", k)
		}
	}
	if len(in.Images) != 0 || in.Video != nil {
		t.Error("跟拍是纯文本环节，不带媒体")
	}
}

func TestFollowValidate_合格通过(t *testing.T) {
	st := getStep(t, marketing.StepFollow)
	flags, hard := runValidate(t, st, goodFollow(), followInput())
	wantNoHard(t, hard)
	if len(flags) != 0 {
		t.Fatalf("不应有软问题：%+v", flags)
	}
}

func TestFollowValidate_硬错误(t *testing.T) {
	st := getStep(t, marketing.StepFollow)
	tests := []struct {
		name   string
		mutate func(o *marketing.FollowOutput)
		want   string
	}{
		{"follow_map 没覆盖全部参考镜头", func(o *marketing.FollowOutput) { o.FollowMap = o.FollowMap[:3] }, "follow_map"},
		{"follow_map 指向越界的镜头", func(o *marketing.FollowOutput) { o.FollowMap[3].Shot = 4 }, "下标"},
		{"follow_map 指向负数", func(o *marketing.FollowOutput) { o.FollowMap[0].Shot = -1 }, "下标"},
		{"follow_map 引用不存在的参考镜头", func(o *marketing.FollowOutput) { o.FollowMap[3].RefShot = 9 }, "参考镜头"},
		{"同一参考镜头重复", func(o *marketing.FollowOutput) {
			o.FollowMap = append(o.FollowMap, marketing.FollowMapItem{RefShot: 2, Shot: 3})
		}, "重复"},
		{"镜头有缺口", func(o *marketing.FollowOutput) { o.Shots[1].End = 4 }, "首尾相接"},
		{"起点不是 0", func(o *marketing.FollowOutput) { o.Shots[0].Start = 1 }, "从 0"},
		{"总长与成片时长不符", func(o *marketing.FollowOutput) { o.Shots[3].End = 8 }, "时长"},
		{"旁白为空", func(o *marketing.FollowOutput) { o.Shots[2].Narration = "  " }, "旁白"},
		{"只有一个镜头", func(o *marketing.FollowOutput) { o.Shots = o.Shots[:1]; o.Shots[0].End = 10; o.FollowMap = nil }, "shots"},
		{"没有标题", func(o *marketing.FollowOutput) { o.Title = "" }, "title"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := goodFollow()
			tt.mutate(&o)
			_, hard := runValidate(t, st, o, followInput())
			wantHard(t, hard, tt.want)
		})
	}
}

func TestFollowValidate_多个参考镜头压成一个镜头(t *testing.T) {
	st := getStep(t, marketing.StepFollow)
	o := goodFollow()
	o.Shots = goodShots(2, 10, "清晨喷一下")
	o.FollowMap = []marketing.FollowMapItem{{RefShot: 1, Shot: 0}, {RefShot: 2, Shot: 0}, {RefShot: 3, Shot: 1}, {RefShot: 4, Shot: 1}}
	_, hard := runValidate(t, st, o, followInput())
	wantNoHard(t, hard)
}

func TestFollowValidate_软问题(t *testing.T) {
	st := getStep(t, marketing.StepFollow)
	o := goodFollow()
	o.Shots[0].Narration = "HERALIFE 的都说好" // 第三方品牌
	o.Shots[1].Narration = "全网销量第一的好物"     // 事实卡无依据的背书
	o.Shots[2].Scene = "手腕特写，顶级瓶身"         // 画面里的极限词
	o.Shots[3].Narration = "留香约 8 小时"      // 正常
	flags, hard := runValidate(t, st, o, followInput())
	wantNoHard(t, hard)
	if !hasFlag(flags, 0, marketing.FlagBrand, "HERALIFE") {
		t.Errorf("应报品牌：%+v", flags)
	}
	if !hasFlag(flags, 1, marketing.FlagClaim, "全网销量第一") {
		t.Errorf("应报背书无依据：%+v", flags)
	}
	if !hasFlag(flags, 2, marketing.FlagForbidden, "顶级") {
		t.Errorf("应报极限词：%+v", flags)
	}
	for _, f := range flags {
		if f.Shot == 3 {
			t.Errorf("镜头 3 正常：%+v", f)
		}
	}
}

func TestFollowValidate_非国内平台不报中文极限词(t *testing.T) {
	st := getStep(t, marketing.StepFollow)
	in := marketing.FollowInput(brief("generic", "zh", 10), goodFacts(), goodReverse(), marketing.AssetNote{ProductImages: 1})
	o := goodFollow()
	o.Shots[2].Scene = "手腕特写，顶级瓶身"
	flags, hard := runValidate(t, st, o, in)
	wantNoHard(t, hard)
	if len(flags) != 0 {
		t.Fatalf("generic 平台不扫中文极限词：%+v", flags)
	}
}

func TestFollowScript_回填参考镜头(t *testing.T) {
	o := goodFollow()
	o.FollowMap = []marketing.FollowMapItem{{RefShot: 1, Shot: 0}, {RefShot: 2, Shot: 0}, {RefShot: 3, Shot: 2}, {RefShot: 4, Shot: 3}}
	s := marketing.FollowScript(o)
	if s.Angle != marketing.AngleFollow || s.Title != "晨雾通勤" || len(s.Shots) != 4 {
		t.Fatalf("脚本结构不对：%+v", s)
	}
	want := []int{1, 0, 3, 4} // 0 表示没有对应（nil）
	for i, w := range want {
		got := s.Shots[i].RefShot
		if w == 0 {
			if got != nil {
				t.Errorf("镜头 %d 不应有参考镜头，实际 %d", i, *got)
			}
			continue
		}
		if got == nil || *got != w {
			t.Errorf("镜头 %d 应对应参考镜头 %d，实际 %v", i, w, got)
		}
	}
	if o.Shots[0].RefShot != nil {
		t.Error("不应改动入参")
	}
}

func TestDecodeFollow(t *testing.T) {
	got, err := marketing.DecodeFollow(mustJSON(t, goodFollow()))
	if err != nil || len(got.FollowMap) != 4 || got.Title != "晨雾通勤" {
		t.Fatalf("解码结果不对：%+v err=%v", got, err)
	}
}
