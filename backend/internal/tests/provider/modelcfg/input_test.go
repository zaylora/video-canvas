// 本文件：input.go 的单元测试：按模型能力校验任务输入（提示词、生成方式、生成参数、参考素材）与素材提取。

package modelcfg_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"video-canvas/internal/provider/modelcfg"
)

func mcInt(v int) *int { return &v }

// mcVideoCaps 是视频模型的测试能力：三种生成方式，图片最多 2 张、音频最多 1 个，视频素材关闭。
func mcVideoCaps() modelcfg.Capabilities {
	return modelcfg.Capabilities{
		Ops: []string{modelcfg.OpT2V, modelcfg.OpI2V, modelcfg.OpOmni},
		Refs: modelcfg.Refs{
			Image: modelcfg.RefSpec{On: true, Max: 2, MaxMB: 10},
			Audio: modelcfg.RefSpec{On: true, Max: 1, MaxMB: 15},
		},
		Prompt: modelcfg.PromptSpec{MaxLength: 5},
		Params: modelcfg.ParamSet{
			{Name: "aspect_ratio", ParamField: modelcfg.ParamField{Type: modelcfg.ParamEnum, Label: "比例", Open: true, Options: []any{"Auto", "16:9"}, Default: "Auto"}},
			{Name: "duration", ParamField: modelcfg.ParamField{Type: modelcfg.ParamNumber, Label: "时长", Open: true, Min: mcInt(4), Max: mcInt(12), Default: 5.0}},
			{Name: "generate_audio", ParamField: modelcfg.ParamField{Type: modelcfg.ParamBoolean, Label: "生成音频", Open: true, Default: true}},
			{Name: "quality", ParamField: modelcfg.ParamField{Type: modelcfg.ParamEnum, Label: "质量", Open: false, Options: []any{1.0, 2.0}, Default: 2.0}},
		},
	}
}

func TestValidateInput_成功(t *testing.T) {
	tests := []struct {
		name  string
		input map[string]any
		want  map[string]any
	}{
		{"文生：补默认值，忽略不允许的素材", map[string]any{"prompt": "一只猫", "images": []any{12.0}},
			map[string]any{"prompt": "一只猫", "op": "t2v", "aspect_ratio": "Auto", "duration": 5.0, "generate_audio": true, "quality": 2.0}},
		{"图生：素材 id 规范成 uint64", map[string]any{"prompt": "猫", "op": "i2v", "images": []any{12.0, "2041791506008539138"}, "audios": []any{3.0}},
			map[string]any{"prompt": "猫", "op": "i2v", "aspect_ratio": "Auto", "duration": 5.0, "generate_audio": true, "quality": 2.0,
				"images": []uint64{12, 2041791506008539138}}},
		{"全能参考：图片和音频都保留", map[string]any{"prompt": "猫", "op": "omni", "images": []any{json.Number("7")}, "audios": []any{"9"}, "videos": []any{1.0}},
			map[string]any{"prompt": "猫", "op": "omni", "aspect_ratio": "Auto", "duration": 5.0, "generate_audio": true, "quality": 2.0,
				"images": []uint64{7}, "audios": []uint64{9}}},
		{"用户改参数", map[string]any{"prompt": "猫", "aspect_ratio": "16:9", "duration": 8.0, "generate_audio": false},
			map[string]any{"prompt": "猫", "op": "t2v", "aspect_ratio": "16:9", "duration": 8.0, "generate_audio": false, "quality": 2.0}},
		{"布尔参数接受字符串", map[string]any{"prompt": "猫", "generate_audio": "false"},
			map[string]any{"prompt": "猫", "op": "t2v", "aspect_ratio": "Auto", "duration": 5.0, "generate_audio": false, "quality": 2.0}},
		{"不开放的参数忽略用户值", map[string]any{"prompt": "猫", "quality": 1.0},
			map[string]any{"prompt": "猫", "op": "t2v", "aspect_ratio": "Auto", "duration": 5.0, "generate_audio": true, "quality": 2.0}},
		{"未知键忽略", map[string]any{"prompt": "猫", "evil": "x", "system": "覆盖"},
			map[string]any{"prompt": "猫", "op": "t2v", "aspect_ratio": "Auto", "duration": 5.0, "generate_audio": true, "quality": 2.0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, errs := modelcfg.ValidateInput(modelcfg.KindVideo, mcVideoCaps(), tt.input)
			if len(errs) > 0 {
				t.Fatalf("不该有错误：%v", errs)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("结果不符：\n got %#v\nwant %#v", got, tt.want)
			}
		})
	}
}

func TestValidateInput_失败(t *testing.T) {
	tests := []struct {
		name      string
		input     map[string]any
		wantField string
		wantMsg   string
	}{
		{"缺提示词", map[string]any{}, "prompt", "不能为空"},
		{"提示词只有空白", map[string]any{"prompt": "   "}, "prompt", "不能为空"},
		{"提示词超长", map[string]any{"prompt": "一二三四五六"}, "prompt", "不能超过 5"},
		{"生成方式不在列表里", map[string]any{"prompt": "猫", "op": "t2i"}, "op", "t2v"},
		{"比例不在可选值里", map[string]any{"prompt": "猫", "aspect_ratio": "1:1"}, "aspect_ratio", "Auto / 16:9"},
		{"时长超出范围", map[string]any{"prompt": "猫", "duration": 13.0}, "duration", "不能大于 12"},
		{"时长小于最小", map[string]any{"prompt": "猫", "duration": 3.0}, "duration", "不能小于 4"},
		{"时长不是整数", map[string]any{"prompt": "猫", "duration": 5.5}, "duration", "整数"},
		{"布尔参数乱填", map[string]any{"prompt": "猫", "generate_audio": "maybe"}, "generate_audio", "true 或 false"},
		{"图生没有图片", map[string]any{"prompt": "猫", "op": "i2v"}, "images", "至少 1 张"},
		{"图生空数组", map[string]any{"prompt": "猫", "op": "i2v", "images": []any{}}, "images", "至少 1 张"},
		{"全能参考没有任何素材", map[string]any{"prompt": "猫", "op": "omni"}, "images", "至少 1 个"},
		{"图片超过上限", map[string]any{"prompt": "猫", "op": "i2v", "images": []any{1.0, 2.0, 3.0}}, "images", "最多 2 个图片"},
		{"素材 id 无效", map[string]any{"prompt": "猫", "op": "i2v", "images": []any{0.0}}, "images", "无效"},
		{"素材不是数组", map[string]any{"prompt": "猫", "op": "i2v", "images": 5.0}, "images", "数组"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, errs := modelcfg.ValidateInput(modelcfg.KindVideo, mcVideoCaps(), tt.input)
			if got != nil || len(errs) == 0 {
				t.Fatalf("期望失败，得到 %v", got)
			}
			for _, e := range errs {
				if e.Field == tt.wantField && strings.Contains(e.Message, tt.wantMsg) {
					return
				}
			}
			t.Fatalf("期望字段 %s 的错误含 %q，实际 %v", tt.wantField, tt.wantMsg, errs)
		})
	}
}

func TestValidateInput_多个错误一次返回(t *testing.T) {
	_, errs := modelcfg.ValidateInput(modelcfg.KindVideo, mcVideoCaps(), map[string]any{"duration": 99.0, "aspect_ratio": "x"})
	fields := map[string]bool{}
	for _, e := range errs {
		fields[e.Field] = true
	}
	for _, f := range []string{"prompt", "duration", "aspect_ratio"} {
		if !fields[f] {
			t.Errorf("缺少字段 %s 的错误：%v", f, errs)
		}
	}
}

func TestValidateInput_关闭的素材类型被忽略(t *testing.T) {
	caps := mcVideoCaps()
	caps.Refs.Image.On = false
	got, errs := modelcfg.ValidateInput(modelcfg.KindVideo, caps, map[string]any{"prompt": "猫", "op": "omni", "images": []any{1.0}, "audios": []any{2.0}})
	if len(errs) > 0 {
		t.Fatalf("不该有错误：%v", errs)
	}
	if _, ok := got["images"]; ok {
		t.Fatalf("关闭的图片素材应被忽略：%v", got)
	}
	if !reflect.DeepEqual(got["audios"], []uint64{2}) {
		t.Fatalf("音频应保留：%v", got)
	}
}

func TestValidateInput_文本与音频没有生成方式(t *testing.T) {
	caps := modelcfg.Capabilities{Prompt: modelcfg.PromptSpec{MaxLength: 100}}
	got, errs := modelcfg.ValidateInput(modelcfg.KindText, caps, map[string]any{"prompt": "你好", "op": "t2v", "images": []any{1.0}})
	if len(errs) > 0 {
		t.Fatalf("不该有错误：%v", errs)
	}
	if !reflect.DeepEqual(got, map[string]any{"prompt": "你好"}) {
		t.Fatalf("文本输入只应有 prompt：%v", got)
	}
}

func TestValidateInput_枚举数字与字符串互认(t *testing.T) {
	caps := modelcfg.Capabilities{Prompt: modelcfg.PromptSpec{MaxLength: 100}, Params: modelcfg.ParamSet{
		{Name: "count", ParamField: modelcfg.ParamField{Type: modelcfg.ParamEnum, Label: "数量", Open: true, Options: []any{1.0, 2.0, 4.0}, Default: 1.0}},
	}}
	got, errs := modelcfg.ValidateInput(modelcfg.KindAudio, caps, map[string]any{"prompt": "x", "count": "4"})
	if len(errs) > 0 || got["count"] != 4.0 {
		t.Fatalf("字符串 \"4\" 应匹配数字选项 4：%v %v", got, errs)
	}
}

func TestMediaRefs_兼容落库后的形态(t *testing.T) {
	in := map[string]any{
		"images": []any{"2041791506008539138", 7.0, "bad"},
		"audios": []uint64{9},
		"videos": []string{"5"},
		"other":  []any{1.0},
	}
	refs := modelcfg.MediaRefs(in)
	var got []string
	for _, r := range refs {
		got = append(got, r.Ref()+"="+r.Kind)
	}
	want := "images.0=image,images.1=image,videos.0=video,audios.0=audio"
	if strings.Join(got, ",") != want {
		t.Fatalf("got %v，期望 %s", got, want)
	}
	if refs[0].ID != 2041791506008539138 {
		t.Fatalf("大 id 丢精度：%d", refs[0].ID)
	}
}

func TestAsAssetID(t *testing.T) {
	for _, tt := range []struct {
		in   any
		want uint64
		ok   bool
	}{
		{12.0, 12, true}, {"12", 12, true}, {json.Number("12"), 12, true}, {uint64(12), 12, true},
		{0.0, 0, false}, {-1.0, 0, false}, {1.5, 0, false}, {float64(1 << 54), 0, false}, {"abc", 0, false}, {nil, 0, false},
	} {
		got, ok := modelcfg.AsAssetID(tt.in)
		if ok != tt.ok || got != tt.want {
			t.Errorf("AsAssetID(%#v) = %d, %v，期望 %d, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}
