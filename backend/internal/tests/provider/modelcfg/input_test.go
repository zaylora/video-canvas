// 本文件：input.go 的单元测试：输入校验（补默认值、类型转换、必填、枚举、长度、范围）与媒体字段提取。

package modelcfg_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"video-canvas/internal/provider/modelcfg"
)

func mcF64(v float64) *float64 { return &v }

func mcTestSchema() modelcfg.InputSchema {
	return modelcfg.InputSchema{
		{Name: "prompt", InputField: modelcfg.InputField{Type: modelcfg.FieldText, Label: "提示词", Required: true, MaxLength: 5}},
		{Name: "image", InputField: modelcfg.InputField{Type: modelcfg.FieldImage, Label: "首帧", Required: true}},
		{Name: "audio", InputField: modelcfg.InputField{Type: modelcfg.FieldAudio, Label: "配音"}},
		{Name: "duration", InputField: modelcfg.InputField{Type: modelcfg.FieldEnum, Label: "时长", Default: 5.0,
			Options: []modelcfg.EnumOption{{Value: 5.0, Label: "5 秒"}, {Value: 10.0, Label: "10 秒"}}}},
		{Name: "ratio", InputField: modelcfg.InputField{Type: modelcfg.FieldEnum, Label: "比例",
			Options: []modelcfg.EnumOption{{Value: "16:9", Label: "横屏"}, {Value: "9:16", Label: "竖屏"}}}},
		{Name: "seed", InputField: modelcfg.InputField{Type: modelcfg.FieldNumber, Label: "种子", Min: mcF64(0), Max: mcF64(100)}},
		{Name: "hd", InputField: modelcfg.InputField{Type: modelcfg.FieldBoolean, Label: "高清", Default: false}},
	}
}

func TestValidateInput_成功(t *testing.T) {
	tests := []struct {
		name  string
		input map[string]any
		want  map[string]any
	}{
		{"补默认值并规范化", map[string]any{"prompt": "一只猫", "image": 12.0},
			map[string]any{"prompt": "一只猫", "image": uint64(12), "duration": 5.0, "hd": false}},
		{"asset id 是数字字符串", map[string]any{"prompt": "猫", "image": "2041791506008539138"},
			map[string]any{"prompt": "猫", "image": uint64(2041791506008539138), "duration": 5.0, "hd": false}},
		{"asset id 是 json.Number", map[string]any{"prompt": "猫", "image": json.Number("2041791506008539138")},
			map[string]any{"prompt": "猫", "image": uint64(2041791506008539138), "duration": 5.0, "hd": false}},
		{"asset id 是 int64", map[string]any{"prompt": "猫", "image": int64(7)},
			map[string]any{"prompt": "猫", "image": uint64(7), "duration": 5.0, "hd": false}},
		{"数字枚举容错 10 与 10.0 与 \"10\"", map[string]any{"prompt": "猫", "image": 1, "duration": "10"},
			map[string]any{"prompt": "猫", "image": uint64(1), "duration": 10.0, "hd": false}},
		{"整型枚举值", map[string]any{"prompt": "猫", "image": 1, "duration": 10},
			map[string]any{"prompt": "猫", "image": uint64(1), "duration": 10.0, "hd": false}},
		{"字符串枚举", map[string]any{"prompt": "猫", "image": 1, "ratio": "9:16"},
			map[string]any{"prompt": "猫", "image": uint64(1), "duration": 5.0, "ratio": "9:16", "hd": false}},
		{"number 与 boolean", map[string]any{"prompt": "猫", "image": 1, "seed": 42, "hd": true},
			map[string]any{"prompt": "猫", "image": uint64(1), "duration": 5.0, "seed": 42.0, "hd": true}},
		{"boolean 字符串", map[string]any{"prompt": "猫", "image": 1, "hd": "true"},
			map[string]any{"prompt": "猫", "image": uint64(1), "duration": 5.0, "hd": true}},
		{"可选媒体字段", map[string]any{"prompt": "猫", "image": 1, "audio": "9"},
			map[string]any{"prompt": "猫", "image": uint64(1), "audio": uint64(9), "duration": 5.0, "hd": false}},
		{"未知字段被忽略", map[string]any{"prompt": "猫", "image": 1, "hack": "x"},
			map[string]any{"prompt": "猫", "image": uint64(1), "duration": 5.0, "hd": false}},
		{"中文按字符计长度（5 个字符）", map[string]any{"prompt": "一二三四五", "image": 1},
			map[string]any{"prompt": "一二三四五", "image": uint64(1), "duration": 5.0, "hd": false}},
		{"nil 输入 map 视为空", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, errs := modelcfg.ValidateInput(mcTestSchema(), tt.input)
			if tt.want == nil { // 缺必填，应失败
				if len(errs) == 0 {
					t.Fatal("期望失败")
				}
				return
			}
			if len(errs) > 0 {
				t.Fatalf("期望成功，实际：%v", errs)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("期望 %#v，实际 %#v", tt.want, got)
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
		{"文本必填为空", map[string]any{"image": 1}, "prompt", "提示词 不能为空"},
		{"文本只有空白", map[string]any{"prompt": "   ", "image": 1}, "prompt", "提示词 不能为空"},
		{"文本传 null", map[string]any{"prompt": nil, "image": 1}, "prompt", "不能为空"},
		{"文本类型错误", map[string]any{"prompt": 5, "image": 1}, "prompt", "必须是文本"},
		{"文本过长", map[string]any{"prompt": "一二三四五六", "image": 1}, "prompt", "不能超过 5 个字符"},
		{"媒体必填缺失", map[string]any{"prompt": "猫"}, "image", "首帧 不能为空"},
		{"媒体为 0", map[string]any{"prompt": "猫", "image": 0}, "image", "素材 ID"},
		{"媒体为负", map[string]any{"prompt": "猫", "image": -3.0}, "image", "素材 ID"},
		{"媒体为小数", map[string]any{"prompt": "猫", "image": 1.5}, "image", "素材 ID"},
		{"媒体 float64 超过 2^53", map[string]any{"prompt": "猫", "image": 2041791506008539138.0}, "image", "素材 ID"},
		{"媒体为非数字字符串", map[string]any{"prompt": "猫", "image": "abc"}, "image", "素材 ID"},
		{"媒体为布尔", map[string]any{"prompt": "猫", "image": true}, "image", "素材 ID"},
		{"枚举不在选项内", map[string]any{"prompt": "猫", "image": 1, "duration": 7}, "duration", "时长 必须是 5 秒 / 10 秒 之一"},
		{"枚举类型不符", map[string]any{"prompt": "猫", "image": 1, "ratio": 16}, "ratio", "必须是 横屏 / 竖屏 之一"},
		{"数字过小", map[string]any{"prompt": "猫", "image": 1, "seed": -1}, "seed", "种子 不能小于 0"},
		{"数字过大", map[string]any{"prompt": "猫", "image": 1, "seed": 100.5}, "seed", "种子 不能大于 100"},
		{"数字传字符串", map[string]any{"prompt": "猫", "image": 1, "seed": "5"}, "seed", "必须是数字"},
		{"布尔非法", map[string]any{"prompt": "猫", "image": 1, "hd": "yes"}, "hd", "必须是 true 或 false"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, errs := modelcfg.ValidateInput(mcTestSchema(), tt.input)
			if got != nil {
				t.Fatalf("失败时不应返回规范化结果：%v", got)
			}
			found := false
			for _, e := range errs {
				if e.Field == tt.wantField {
					found = true
					if !strings.Contains(e.Message, tt.wantMsg) {
						t.Fatalf("消息 %q 应包含 %q", e.Message, tt.wantMsg)
					}
				}
			}
			if !found {
				t.Fatalf("期望字段 %s 报错，实际 %v", tt.wantField, errs)
			}
		})
	}
}

func TestValidateInput_多个字段错误一起返回(t *testing.T) {
	_, errs := modelcfg.ValidateInput(mcTestSchema(), map[string]any{"duration": 3})
	fields := map[string]bool{}
	for _, e := range errs {
		fields[e.Field] = true
	}
	for _, f := range []string{"prompt", "image", "duration"} {
		if !fields[f] {
			t.Errorf("缺少字段 %s 的错误：%v", f, errs)
		}
	}
	// 错误顺序与 schema 书写顺序一致
	if len(errs) != 3 || errs[0].Field != "prompt" || errs[1].Field != "image" || errs[2].Field != "duration" {
		t.Errorf("错误顺序应与 schema 一致：%v", errs)
	}
}

func TestValidateInput_空文本使用默认值(t *testing.T) {
	schema := modelcfg.InputSchema{{Name: "p", InputField: modelcfg.InputField{Type: modelcfg.FieldText, Label: "P", Default: "默认"}}}
	got, errs := modelcfg.ValidateInput(schema, map[string]any{"p": ""})
	if len(errs) > 0 || got["p"] != "默认" {
		t.Fatalf("got=%v errs=%v", got, errs)
	}
}

func TestMediaFieldNames(t *testing.T) {
	got := modelcfg.MediaFieldNames(mcTestSchema())
	if !reflect.DeepEqual(got, []string{"image", "audio"}) {
		t.Fatalf("实际 %v", got)
	}
	if len(modelcfg.MediaFieldNames(nil)) != 0 {
		t.Fatal("空 schema 应无媒体字段")
	}
}
