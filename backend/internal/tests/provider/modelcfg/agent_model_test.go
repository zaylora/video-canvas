package modelcfg_test

import (
	"testing"

	"video-canvas/internal/provider/modelcfg"
)

// mcAsAgent 把夹具改成合法的 Agent 模型：对话用的大语言模型，有上下文窗口，按 Token 计费，可以声明能看图。
func mcAsAgent(m map[string]any) {
	m["kind"] = "agent"
	m["capabilities"] = map[string]any{
		"prompt":  map[string]any{"max_length": 20000}, // Agent 模型里是「用户单条消息的字数上限」
		"context": map[string]any{"window": 200000, "output": 8192},
		"vision":  true,
	}
	m["pricing"] = map[string]any{"billing": "token", "token": map[string]any{"in": 3, "out": 15}}
}

func TestParseModel_Agent合法配置(t *testing.T) {
	m := mcLoadFixture(t, "model_video.json")
	mcAsAgent(m)
	got, issues := modelcfg.ParseModel(mcMarshal(t, m))
	if got == nil || len(issues) != 0 {
		t.Fatalf("合法的 Agent 模型应通过: %v", mcIssueList(issues))
	}
	if got.Kind != modelcfg.KindAgent || !got.Capabilities.Vision || got.Capabilities.Context.Window != 200000 {
		t.Errorf("解析结果不对: %+v", got)
	}
	if modelcfg.Kinds[len(modelcfg.Kinds)-1] != modelcfg.KindAgent {
		t.Errorf("Kinds 应包含 agent: %v", modelcfg.Kinds)
	}

	t.Run("vision 不写默认为 false", func(t *testing.T) {
		m := mcLoadFixture(t, "model_video.json")
		mcAsAgent(m)
		delete(mcCaps(m), "vision")
		got, issues := modelcfg.ParseModel(mcMarshal(t, m))
		if got == nil || got.Capabilities.Vision {
			t.Fatalf("got=%+v issues=%v", got, mcIssueList(issues))
		}
	})

	t.Run("对外清单带 vision，前端据此决定能不能看图", func(t *testing.T) {
		pub := got.Capabilities.Public()
		if !pub.Vision {
			t.Error("Public() 应保留 vision")
		}
	})
}

func TestParseModel_Agent限制(t *testing.T) {
	mcRunCases(t, []mcCase{
		{"缺上下文", func(m map[string]any) { mcAsAgent(m); delete(mcCaps(m), "context") }, "capabilities.context", "必须设置"},
		{"最大输出不小于窗口", func(m map[string]any) {
			mcAsAgent(m)
			mcCaps(m)["context"] = map[string]any{"window": 1000, "output": 1000}
		}, "capabilities.context.output", "小于"},
		{"不能有固定系统提示：由平台维护", func(m map[string]any) { mcAsAgent(m); mcCaps(m)["system"] = "你是助手" }, "capabilities.system", "平台"},
		{"只能按 Token 计费", func(m map[string]any) {
			mcAsAgent(m)
			m["pricing"] = map[string]any{"billing": "per_call", "unit": 5}
		}, "pricing.billing", "Token"},
		{"不能有生成方式", func(m map[string]any) { mcAsAgent(m); mcCaps(m)["ops"] = []any{"t2i"} }, "capabilities.ops", ""},
		{"不能有生成参数", func(m map[string]any) {
			mcAsAgent(m)
			mcCaps(m)["params"] = map[string]any{"temp": map[string]any{"type": "number", "label": "温度", "open": true, "default": 1, "min": 0, "max": 2}}
		}, "capabilities.params", "Agent"},
		{"不接收参考素材", func(m map[string]any) {
			mcAsAgent(m)
			mcCaps(m)["refs"] = map[string]any{"image": map[string]any{"on": true, "max": 3, "max_mb": 10}}
		}, "capabilities.refs.image.on", ""},
		{"单条消息字数上限必须设置", func(m map[string]any) { mcAsAgent(m); delete(mcCaps(m), "prompt") }, "capabilities.prompt.max_length", ""},
		{"vision 只有 Agent 模型能声明", func(m map[string]any) { mcAsText(m); mcCaps(m)["vision"] = true }, "capabilities.vision", "Agent"},
		{"图片模型也不能声明 vision", func(m map[string]any) { mcCaps(m)["vision"] = true }, "capabilities.vision", "Agent"},
	})
}

func TestParseModel_Agent不影响文本模型的既有规则(t *testing.T) {
	// 文本模型仍然可以有固定系统提示，也仍然不能用 vision。
	m := mcLoadFixture(t, "model_video.json")
	mcAsText(m)
	mcCaps(m)["system"] = "你是写作助手"
	if got, issues := modelcfg.ParseModel(mcMarshal(t, m)); got == nil {
		t.Fatalf("文本模型的固定系统提示应保持可用: %v", mcIssueList(issues))
	}
}
