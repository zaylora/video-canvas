package marketing_test

import (
	"strings"
	"testing"

	"video-canvas/internal/marketing"
	"video-canvas/internal/stepkit"
)

func createInput(angles []string, count int, rev *marketing.Reverse) stepkit.Input {
	return marketing.CreateInput(brief("douyin", "zh", 10), goodFacts(), angles, count, rev)
}

// script 构造一份 3 镜头的创意脚本。
func script(angle, narration string) marketing.Script {
	return marketing.Script{Angle: angle, Title: "标题", Hook: "钩子", Shots: goodShots(3, 10, narration), Music: "钢琴", Voice: "女声"}
}

func TestCreateInput_变量(t *testing.T) {
	rev := goodReverse()
	in := createInput([]string{"pain", "mood"}, 2, &rev)
	for _, k := range []string{"facts", "platform", "language", "duration", "angles", "count", "reverse"} {
		if _, ok := in.Vars[k]; !ok {
			t.Errorf("Vars 缺 %s", k)
		}
	}
	if in.Vars["count"] != 2 {
		t.Errorf("count 不对：%v", in.Vars["count"])
	}
	in = createInput(nil, 3, nil)
	if _, ok := in.Vars["reverse"]; ok {
		t.Error("没有参考拆解时不应带 reverse")
	}
	if as, ok := in.Vars["angles"].([]string); !ok || len(as) != 0 {
		t.Errorf("自动选角度时 angles 应为空数组：%#v", in.Vars["angles"])
	}
}

func TestCreateValidate_合格通过(t *testing.T) {
	st := getStep(t, marketing.StepCreate)
	out := marketing.CreateOutput{Scripts: []marketing.Script{script("pain", "早八来不及？"), script("mood", "一抹晨雾，整天在线")}}
	flags, hard := runValidate(t, st, out, createInput([]string{"pain", "mood"}, 2, nil))
	wantNoHard(t, hard)
	if len(flags) != 0 {
		t.Fatalf("不应有软问题：%+v", flags)
	}
}

func TestCreateValidate_自动选角度(t *testing.T) {
	st := getStep(t, marketing.StepCreate)
	in := createInput(nil, 2, nil)
	ok := marketing.CreateOutput{Scripts: []marketing.Script{script("feature", "留香约 8 小时"), script("unbox", "拆开看看")}}
	_, hard := runValidate(t, st, ok, in)
	wantNoHard(t, hard)

	dup := marketing.CreateOutput{Scripts: []marketing.Script{script("feature", "留香约 8 小时"), script("feature", "拆开看看")}}
	_, hard = runValidate(t, st, dup, in)
	wantHard(t, hard, "重复")

	bad := marketing.CreateOutput{Scripts: []marketing.Script{script("feature", "留香约 8 小时"), script("follow", "拆开看看")}}
	_, hard = runValidate(t, st, bad, in)
	wantHard(t, hard, "angle")
}

func TestCreateValidate_硬错误(t *testing.T) {
	st := getStep(t, marketing.StepCreate)
	two := func() marketing.CreateOutput {
		return marketing.CreateOutput{Scripts: []marketing.Script{script("pain", "早八来不及"), script("mood", "一抹晨雾")}}
	}
	long := strings.Repeat("好", 60) // 单份旁白超过 10 秒 × 4.5 = 45 字
	tests := []struct {
		name   string
		angles []string
		count  int
		mutate func(o *marketing.CreateOutput)
		want   string
	}{
		{"份数不够", []string{"pain", "mood"}, 2, func(o *marketing.CreateOutput) { o.Scripts = o.Scripts[:1] }, "份数"},
		{"份数过多", []string{"pain", "mood"}, 2, func(o *marketing.CreateOutput) { o.Scripts = append(o.Scripts, script("feature", "x")) }, "份数"},
		{"角度与给定不一致", []string{"pain", "mood"}, 2, func(o *marketing.CreateOutput) { o.Scripts[1].Angle = "feature" }, "mood"},
		{"给定角度重复出现", []string{"pain", "mood"}, 2, func(o *marketing.CreateOutput) { o.Scripts[1].Angle = "pain" }, "重复"},
		{"只有一个镜头", []string{"pain", "mood"}, 2, func(o *marketing.CreateOutput) {
			o.Scripts[0].Shots = goodShots(1, 10, "好")
		}, "shots"},
		{"七个镜头", []string{"pain", "mood"}, 2, func(o *marketing.CreateOutput) {
			o.Scripts[0].Shots = goodShots(7, 10, "好")
		}, "shots"},
		{"镜头有缺口", []string{"pain", "mood"}, 2, func(o *marketing.CreateOutput) { o.Scripts[0].Shots[0].End = 2 }, "首尾相接"},
		{"总长不符", []string{"pain", "mood"}, 2, func(o *marketing.CreateOutput) { o.Scripts[1].Shots[2].End = 7 }, "时长"},
		{"旁白超出字数上限", []string{"pain", "mood"}, 2, func(o *marketing.CreateOutput) {
			o.Scripts[0].Shots[0].Narration = long
		}, "旁白"},
		{"旁白是英文但设置为中文", []string{"pain", "mood"}, 2, func(o *marketing.CreateOutput) {
			for i := range o.Scripts[0].Shots {
				o.Scripts[0].Shots[i].Narration = "Squeeze a little cream"
			}
		}, "语言"},
		{"缺标题", []string{"pain", "mood"}, 2, func(o *marketing.CreateOutput) { o.Scripts[0].Title = "" }, "title"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := two()
			tt.mutate(&o)
			_, hard := runValidate(t, st, o, createInput(tt.angles, tt.count, nil))
			wantHard(t, hard, tt.want)
		})
	}
}

func TestCreateValidate_旁白长度按整份合计(t *testing.T) {
	st := getStep(t, marketing.StepCreate)
	// 每镜 14 字 × 3 镜 = 42 字 ≤ 45；每镜 16 字 × 3 = 48 > 45。
	okScript := script("pain", strings.Repeat("好", 14))
	_, hard := runValidate(t, st, marketing.CreateOutput{Scripts: []marketing.Script{okScript}}, createInput([]string{"pain"}, 1, nil))
	wantNoHard(t, hard)
	over := script("pain", strings.Repeat("好", 16))
	_, hard = runValidate(t, st, marketing.CreateOutput{Scripts: []marketing.Script{over}}, createInput([]string{"pain"}, 1, nil))
	wantHard(t, hard, "旁白")
}

func TestCreateValidate_英文设置要求英文旁白(t *testing.T) {
	st := getStep(t, marketing.StepCreate)
	in := marketing.CreateInput(brief("tiktok", "en", 10), goodFacts(), []string{"feature"}, 1, nil)
	en := script("feature", "Squeeze it")
	_, hard := runValidate(t, st, marketing.CreateOutput{Scripts: []marketing.Script{en}}, in)
	wantNoHard(t, hard)
	zh := script("feature", "一抹就好")
	_, hard = runValidate(t, st, marketing.CreateOutput{Scripts: []marketing.Script{zh}}, in)
	wantHard(t, hard, "语言")
}

func TestCreateValidate_极限词软问题(t *testing.T) {
	st := getStep(t, marketing.StepCreate)
	s := script("pain", "早八来不及")
	s.Shots[1].Narration = "顶级配方"
	flags, hard := runValidate(t, st, marketing.CreateOutput{Scripts: []marketing.Script{s}}, createInput([]string{"pain"}, 1, nil))
	wantNoHard(t, hard)
	// 多份脚本时 flag 没法区分来自哪份，所以只在单份时断言镜头下标；这里按「出现即报」检查。
	if !hasFlag(flags, 1, marketing.FlagForbidden, "顶级") {
		t.Fatalf("应报极限词：%+v", flags)
	}
}

func TestDecodeCreate(t *testing.T) {
	out := marketing.CreateOutput{Scripts: []marketing.Script{script("pain", "好")}}
	got, err := marketing.DecodeCreate(mustJSON(t, out))
	if err != nil || len(got.Scripts) != 1 || got.Scripts[0].Angle != "pain" {
		t.Fatalf("解码结果不对：%+v err=%v", got, err)
	}
}

// 防止 stepkit 引用被误删。
var _ = stepkit.Hard
