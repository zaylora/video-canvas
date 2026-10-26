package agent_test

import (
	"testing"

	"video-canvas/internal/agent/prompts"
	. "video-canvas/internal/service/agent"
)

func TestHarness_Key(t *testing.T) {
	a := Harness{PromptVersion: 9, ToolsHash: "abc", BuildSHA: "7baa1a3"}
	if len(a.Key()) != 12 {
		t.Fatalf("key 应为 12 位十六进制，得到 %q", a.Key())
	}
	if a.Key() != (Harness{PromptVersion: 9, ToolsHash: "abc", BuildSHA: "7baa1a3"}).Key() {
		t.Error("同样的版本应得到同样的 key")
	}
	for name, other := range map[string]Harness{
		"提示词版本不同": {PromptVersion: 10, ToolsHash: "abc", BuildSHA: "7baa1a3"},
		"工具声明不同":  {PromptVersion: 9, ToolsHash: "abd", BuildSHA: "7baa1a3"},
		"构建不同":    {PromptVersion: 9, ToolsHash: "abc", BuildSHA: "0000000"},
	} {
		if a.Key() == other.Key() {
			t.Errorf("%s 时 key 应不同", name)
		}
	}
	// 字段边界不能串：9|1 与 91| 不能撞出同一个 key
	if (Harness{PromptVersion: 9, ToolsHash: "1"}).Key() == (Harness{PromptVersion: 91}).Key() {
		t.Error("字段之间要有分隔，不能拼出相同的 key")
	}
	if (Harness{}).Key() != "" {
		t.Error("没有任何版本信息时 key 为空，表示这次运行没记版本")
	}
}

func TestCurrentHarness(t *testing.T) {
	h := CurrentHarness("7baa1a3")
	if h.PromptVersion != prompts.Version {
		t.Errorf("提示词版本应取 prompts.Version=%d，得到 %d", prompts.Version, h.PromptVersion)
	}
	if len(h.ToolsHash) != 12 || h.BuildSHA != "7baa1a3" {
		t.Errorf("harness=%+v", h)
	}
	if h.ToolsHash != CurrentHarness("other").ToolsHash {
		t.Error("工具声明哈希只取决于声明内容，与构建号无关")
	}
	long := CurrentHarness("0123456789012345678901234567890123456789ABC")
	if len(long.BuildSHA) != 40 {
		t.Errorf("构建号最长 40 位（列宽），得到 %d 位", len(long.BuildSHA))
	}
}
