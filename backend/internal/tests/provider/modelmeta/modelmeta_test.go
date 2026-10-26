package modelmeta_test

import (
	"testing"

	"video-canvas/internal/provider/modelmeta"
)

func TestLookup(t *testing.T) {
	cases := []struct {
		name string
		id   string
		want modelmeta.Limits
		ok   bool
	}{
		{"精确匹配", "gpt-4o", modelmeta.Limits{Window: 128000, Output: 16384}, true},
		{"大小写不敏感，前后空白忽略", "  GPT-4o ", modelmeta.Limits{Window: 128000, Output: 16384}, true},
		{"带厂商前缀", "openai/gpt-4o", modelmeta.Limits{Window: 128000, Output: 16384}, true},
		{"带 :标签（OpenRouter 的 :free）", "openai/gpt-4o:free", modelmeta.Limits{Window: 128000, Output: 16384}, true},
		{"带日期后缀，快照里只有不带日期的", "claude-sonnet-4-5-20251001", modelmeta.Limits{Window: 200000, Output: 64000}, true},
		{"6 位日期后缀，快照里是别的日期版本", "doubao-seed-1-6-250615", modelmeta.Limits{Window: 256000, Output: 64000}, true},
		{"-latest 后缀", "gpt-4o-latest", modelmeta.Limits{Window: 128000, Output: 16384}, true},
		{"最大输出不小于窗口时取窗口的一半", "kimi-k2-0905-preview", modelmeta.Limits{Window: 262144, Output: 131072}, true},
		{"不认识的模型", "my-private-model-v7", modelmeta.Limits{}, false},
		{"空串", "", modelmeta.Limits{}, false},
		{"只有厂商前缀", "openai/", modelmeta.Limits{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := modelmeta.Lookup(c.id)
			if ok != c.ok || got != c.want {
				t.Errorf("Lookup(%q) = %+v, %v；期望 %+v, %v", c.id, got, ok, c.want, c.ok)
			}
		})
	}
}

// 快照里的每一项查出来都必须能直接通过模型配置的校验：窗口 1–1000 万，最大输出 256–100 万且小于窗口。
func TestEverySnapshotEntryIsValid(t *testing.T) {
	names := modelmeta.Names()
	if len(names) < 500 {
		t.Fatalf("快照只有 %d 个模型，疑似没有生成成功", len(names))
	}
	for _, name := range names {
		l, ok := modelmeta.Lookup(name)
		if !ok {
			continue // 规格不合法的项会被整体丢弃，这是允许的
		}
		if l.Window < 1 || l.Window > 10_000_000 || l.Output < 256 || l.Output > 1_000_000 || l.Output >= l.Window {
			t.Errorf("%s 的规格 %+v 不满足校验范围", name, l)
		}
	}
}
