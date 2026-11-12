package marketing_test

import (
	"encoding/base64"
	"testing"

	"video-canvas/internal/llmgateway"
	"video-canvas/internal/marketing"
	"video-canvas/internal/stepkit"
)

func reverseInput(duration float64) stepkit.Input {
	return marketing.ReverseInput(brief("douyin", "zh", 10), duration, []float64{2.5, 5, 7.5}, nil, nil)
}

func TestReverseInput_视频与截帧都放进输入(t *testing.T) {
	video := &llmgateway.Video{MimeType: "video/mp4", Data: base64.StdEncoding.EncodeToString([]byte("v"))}
	in := marketing.ReverseInput(brief("douyin", "zh", 10), 10, []float64{2.5, 5}, video, []string{"data:image/jpeg;base64,AA"})
	if in.Video != video || len(in.Images) != 1 {
		t.Fatalf("video 与 frames 都应放进 Input：%+v", in)
	}
	if in.Vars["duration"] != 10.0 {
		t.Fatalf("Vars[duration] 应为参考片时长：%v", in.Vars["duration"])
	}
	if cuts, ok := in.Vars["cuts"].([]float64); !ok || len(cuts) != 2 {
		t.Fatalf("Vars[cuts] 不对：%v", in.Vars["cuts"])
	}
}

func TestReverseValidate_合格通过(t *testing.T) {
	st := getStep(t, marketing.StepReverse)
	flags, hard := runValidate(t, st, goodReverse(), reverseInput(10))
	wantNoHard(t, hard)
	if len(flags) != 0 {
		t.Fatalf("不应有软问题：%+v", flags)
	}
}

func TestReverseValidate_硬错误(t *testing.T) {
	st := getStep(t, marketing.StepReverse)
	tests := []struct {
		name   string
		mutate func(r *marketing.Reverse)
		want   string
	}{
		{"镜头 id 不连续", func(r *marketing.Reverse) { r.Shots[2].ID = 5 }, "id"},
		{"镜头 id 不从 1 起", func(r *marketing.Reverse) {
			for i := range r.Shots {
				r.Shots[i].ID = i
			}
		}, "id"},
		{"首镜没有从 0 开始", func(r *marketing.Reverse) { r.Shots[0].Start = 1 }, "从 0"},
		{"镜头之间有缺口", func(r *marketing.Reverse) { r.Shots[1].End = 4.2 }, "首尾相接"},
		{"镜头之间重叠", func(r *marketing.Reverse) { r.Shots[1].End = 5.6 }, "首尾相接"},
		{"没有覆盖到视频结尾", func(r *marketing.Reverse) { r.Shots[3].End = 8.9 }, "时长"},
		{"超出视频时长", func(r *marketing.Reverse) { r.Shots[3].End = 11 }, "时长"},
		{"镜头结束早于开始", func(r *marketing.Reverse) { r.Shots[2].End = 5 }, "结束"},
		{"前 3 秒镜头没标", func(r *marketing.Reverse) { r.Shots[1].IsOpening3s = false }, "is_opening_3s"},
		{"3 秒后的镜头标了前 3 秒", func(r *marketing.Reverse) { r.Shots[2].IsOpening3s = true }, "is_opening_3s"},
		{"动作为空", func(r *marketing.Reverse) { r.Shots[0].Action = "" }, "action"},
		{"动作只是名词", func(r *marketing.Reverse) { r.Shots[0].Action = "橙色护手霜软管和白色毛衣" }, "action"},
		{"动作太短", func(r *marketing.Reverse) { r.Shots[0].Action = "挤" }, "action"},
		{"动作就是主体", func(r *marketing.Reverse) {
			r.Shots[0].Subject = "Orange hand cream tube"
			r.Shots[0].Action = "orange hand cream tube"
		}, "action"},
		{"英文动作太短", func(r *marketing.Reverse) { r.Shots[0].Action = "woman squeezes" }, "action"},
		{"商品角色不合法", func(r *marketing.Reverse) { r.Shots[0].ProductRole = "star" }, "product_role"},
		{"没有镜头", func(r *marketing.Reverse) { r.Shots = nil }, "shots"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := goodReverse()
			tt.mutate(&r)
			_, hard := runValidate(t, st, r, reverseInput(10))
			wantHard(t, hard, tt.want)
		})
	}
}

func TestReverseValidate_容差内通过(t *testing.T) {
	st := getStep(t, marketing.StepReverse)
	r := goodReverse()
	r.Shots[0].Start = 0.2 // 首镜起点容差 0.3
	r.Shots[1].Start = 2.7 // 相邻容差 0.3
	r.Shots[3].End = 9.6   // 末镜与时长容差 0.5
	_, hard := runValidate(t, st, r, reverseInput(10))
	wantNoHard(t, hard)
}

func TestReverseValidate_英文动作合格(t *testing.T) {
	st := getStep(t, marketing.StepReverse)
	r := goodReverse()
	r.Shots[0].Action = "woman squeezes cream onto the back of her hand"
	_, hard := runValidate(t, st, r, reverseInput(10))
	wantNoHard(t, hard)
}

func TestReverseValidate_平均每镜不足零点四秒(t *testing.T) {
	st := getStep(t, marketing.StepReverse)
	n := 30
	r := goodReverse()
	r.Shots = r.Shots[:0]
	for i := 0; i < n; i++ {
		s := goodReverse().Shots[0]
		s.ID = i + 1
		s.Start = float64(i) * 10 / float64(n)
		s.End = float64(i+1) * 10 / float64(n)
		s.IsOpening3s = s.Start < 3
		r.Shots = append(r.Shots, s)
	}
	_, hard := runValidate(t, st, r, reverseInput(10))
	wantHard(t, hard, "0.4")
}

func TestValidateReverse_复用同一套规则(t *testing.T) {
	if err := marketing.ValidateReverse(goodReverse(), 10); err != nil {
		t.Fatalf("合格拆解应通过：%v", err)
	}
	r := goodReverse()
	r.Shots[0].Action = ""
	r.Shots[1].ProductRole = "star"
	r.Shots[3].End = 8
	list := stepkit.ErrorList(marketing.ValidateReverse(r, 10))
	if len(list) < 3 {
		t.Fatalf("应逐条返回问题：%v", list)
	}
	wantHard(t, list, "action")
	wantHard(t, list, "product_role")
	wantHard(t, list, "时长")
	// 用户改完后的 dialogue / key_colors 也要保持 Schema 合法：key_colors 不是 null。
	r = goodReverse()
	r.Shots[0].KeyColors = nil
	if err := marketing.ValidateReverse(r, 10); err != nil {
		t.Fatalf("key_colors 为空数组应通过：%v", err)
	}
}

func TestDecodeReverse(t *testing.T) {
	got, err := marketing.DecodeReverse(mustJSON(t, goodReverse()))
	if err != nil || len(got.Shots) != 4 || got.Brands[0] != "HERALIFE" {
		t.Fatalf("解码结果不对：%+v err=%v", got, err)
	}
	if _, err := marketing.DecodeReverse([]byte("[]")); err == nil {
		t.Fatal("结构不对应报错")
	}
}
