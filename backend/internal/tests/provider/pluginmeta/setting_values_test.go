package pluginmeta_test

import (
	"reflect"
	"strings"
	"testing"

	"video-canvas/internal/provider/pluginmeta"
)

// valuesSchema 是校验取值用的设置项声明：region 有默认值的 enum，tenant 必填字符串，burst 数字，debug 布尔，token 必填且有默认值。
func valuesSchema() pluginmeta.SettingSchema {
	return pluginmeta.SettingSchema{
		{Name: "region", Setting: pluginmeta.Setting{Type: pluginmeta.SettingEnum, Label: "区域", Options: []string{"cn", "global"}, Default: "cn"}},
		{Name: "tenant", Setting: pluginmeta.Setting{Type: pluginmeta.SettingString, Label: "租户", Required: true}},
		{Name: "burst", Setting: pluginmeta.Setting{Type: pluginmeta.SettingNumber, Label: "突发"}},
		{Name: "debug", Setting: pluginmeta.Setting{Type: pluginmeta.SettingBoolean, Label: "调试"}},
		{Name: "token", Setting: pluginmeta.Setting{Type: pluginmeta.SettingString, Label: "令牌", Required: true, Default: "anon"}},
	}
}

func TestValidateSettingValues(t *testing.T) {
	tests := []struct {
		name       string
		values     map[string]any
		want       map[string]any
		wantIssues []string // 期望出现问题的设置项名字（按顺序）
	}{
		{
			name:   "合法：补全默认值，必填项有默认值时视为已填",
			values: map[string]any{"tenant": "t1"},
			want:   map[string]any{"region": "cn", "tenant": "t1", "token": "anon"},
		},
		{
			name:   "合法：全部显式给出（数字接受整数与浮点）",
			values: map[string]any{"region": "global", "tenant": "t1", "burst": 3, "debug": true, "token": "x"},
			want:   map[string]any{"region": "global", "tenant": "t1", "burst": 3, "debug": true, "token": "x"},
		},
		{
			name:   "null 视同没填：取默认值",
			values: map[string]any{"tenant": "t1", "region": nil},
			want:   map[string]any{"region": "cn", "tenant": "t1", "token": "anon"},
		},
		{name: "缺少必填项", values: map[string]any{}, wantIssues: []string{"tenant"}},
		{name: "nil 视为空对象", values: nil, wantIssues: []string{"tenant"}},
		{name: "必填字符串给了空白", values: map[string]any{"tenant": "  "}, wantIssues: []string{"tenant"}},
		{name: "未声明的设置项", values: map[string]any{"tenant": "t", "zzz": 1, "aaa": 2}, wantIssues: []string{"aaa", "zzz"}},
		{name: "enum 取值不在 options", values: map[string]any{"tenant": "t", "region": "mars"}, wantIssues: []string{"region"}},
		{name: "enum 给了数字", values: map[string]any{"tenant": "t", "region": 1.0}, wantIssues: []string{"region"}},
		{name: "string 给了数字", values: map[string]any{"tenant": 3.0}, wantIssues: []string{"tenant"}},
		{name: "number 给了字符串", values: map[string]any{"tenant": "t", "burst": "3"}, wantIssues: []string{"burst"}},
		{name: "boolean 给了字符串", values: map[string]any{"tenant": "t", "debug": "true"}, wantIssues: []string{"debug"}},
		{name: "多个问题一次报出，先报未声明的、再按声明顺序", values: map[string]any{"zzz": 1, "region": "x", "burst": "y"}, wantIssues: []string{"zzz", "region", "tenant", "burst"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, issues := pluginmeta.ValidateSettingValues(valuesSchema(), tt.values)
			var names []string
			for _, is := range issues {
				names = append(names, is.Path)
				if strings.TrimSpace(is.Message) == "" {
					t.Fatalf("问题必须有说明：%+v", is)
				}
			}
			if !reflect.DeepEqual(names, tt.wantIssues) {
				t.Fatalf("问题设置项期望 %v，实际 %v（%+v）", tt.wantIssues, names, issues)
			}
			if got == nil {
				t.Fatal("返回的 map 永远不是 nil")
			}
			if len(tt.wantIssues) == 0 && !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("规范化结果期望 %v，实际 %v", tt.want, got)
			}
		})
	}

	t.Run("空 schema：空取值通过，任何取值都是未声明", func(t *testing.T) {
		got, issues := pluginmeta.ValidateSettingValues(nil, nil)
		if len(issues) != 0 || got == nil || len(got) != 0 {
			t.Fatalf("空取值应通过：%v %v", got, issues)
		}
		_, issues = pluginmeta.ValidateSettingValues(nil, map[string]any{"a": 1})
		if len(issues) != 1 || issues[0].Path != "a" {
			t.Fatalf("应报未声明：%+v", issues)
		}
	})

	t.Run("不会修改传入的取值", func(t *testing.T) {
		in := map[string]any{"tenant": "t"}
		_, _ = pluginmeta.ValidateSettingValues(valuesSchema(), in)
		if len(in) != 1 {
			t.Fatalf("入参不应被补上默认值：%v", in)
		}
	})
}
