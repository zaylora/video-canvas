// 本文件：模型展示信息（hint / vendor / tags）的校验测试。

package modelcfg_test

import (
	"strings"
	"testing"

	"video-canvas/internal/provider/modelcfg"
)

func TestParseModelDisplayInfo(t *testing.T) {
	t.Run("合法的 vendor / tags / hint 通过并保留", func(t *testing.T) {
		m := mcLoadFixture(t, "model_video.json")
		m["vendor"] = "kling"
		m["tags"] = []any{"推荐", "带音轨"}
		m["hint"] = strings.Repeat("字", 500)
		cfg, issues := modelcfg.ParseModel(mcMarshal(t, m))
		if len(issues) > 0 {
			t.Fatalf("不该有问题：%v", issues)
		}
		if cfg.Vendor != "kling" || len(cfg.Tags) != 2 || cfg.Tags[0] != "推荐" {
			t.Fatalf("字段没有保留：%+v", cfg)
		}
	})

	cases := []struct {
		name string
		mut  func(m map[string]any)
		path string
	}{
		{"hint 超过 500 字", func(m map[string]any) { m["hint"] = strings.Repeat("字", 501) }, "hint"},
		{"vendor 含大写", func(m map[string]any) { m["vendor"] = "Kling" }, "vendor"},
		{"vendor 含空格", func(m map[string]any) { m["vendor"] = "a b" }, "vendor"},
		{"标签超过 5 个", func(m map[string]any) { m["tags"] = []any{"a", "b", "c", "d", "e", "f"} }, "tags"},
		{"单个标签超过 12 字", func(m map[string]any) { m["tags"] = []any{strings.Repeat("字", 13)} }, "tags[0]"},
		{"标签为空", func(m map[string]any) { m["tags"] = []any{"  "} }, "tags[0]"},
		{"标签重复", func(m map[string]any) { m["tags"] = []any{"推荐", "推荐"} }, "tags[1]"},
		{"标签类型不对", func(m map[string]any) { m["tags"] = []any{1} }, "tags[0]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := mcLoadFixture(t, "model_video.json")
			tc.mut(m)
			_, issues := modelcfg.ParseModel(mcMarshal(t, m))
			if _, ok := mcHasIssue(issues, tc.path); !ok {
				t.Fatalf("应在 %s 报问题，实际：%v", tc.path, issues)
			}
		})
	}

	t.Run("vendor 和 tags 可以省略", func(t *testing.T) {
		m := mcLoadFixture(t, "model_video.json")
		if _, issues := modelcfg.ParseModel(mcMarshal(t, m)); len(issues) > 0 {
			t.Fatalf("不该有问题：%v", issues)
		}
	})
}
