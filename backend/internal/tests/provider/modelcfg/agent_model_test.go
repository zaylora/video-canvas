package modelcfg_test

import (
	"encoding/json"
	"testing"
	"time"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
)

// mcAsAgent 把夹具改成合法的 Agent 模型：对话用的大语言模型，有上下文窗口，按 Token 计费，可以声明能看图。
func mcAsAgent(m map[string]any) {
	m["kind"] = "agent"
	delete(mcChannel0(m), "protocol") // Agent 不走插件，不选协议
	m["capabilities"] = map[string]any{
		"prompt":  map[string]any{"max_length": 20000}, // Agent 模型里是「用户单条消息的字数上限」
		"context": map[string]any{"window": 200000, "output": 8192},
		"vision":  true,
	}
	m["pricing"] = map[string]any{"billing": "token", "token": map[string]any{"in": 3, "out": 15}}
}

func TestParseModel_VideoCapability(t *testing.T) {
	for _, tc := range []struct {
		name, kind, format string
		vision, valid      bool
	}{
		{"Gemini 无须看图也能看视频", "agent", "gemini", false, true},
		{"Gemini 同时看图和视频", "agent", "gemini", true, true},
		{"OpenAI 不能开启", "agent", "openai", true, false},
		{"默认格式不能开启", "agent", "", true, false},
		{"非 Agent 不能开启", "text", "", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := mcLoadFixture(t, "model_video.json")
			mcAsAgent(m)
			if tc.kind == "text" {
				mcAsText(m)
			}
			mcChannel0(m)["api_format"] = tc.format
			mcCaps(m)["video"], mcCaps(m)["vision"] = true, tc.vision
			got, issues := modelcfg.ParseModel(mcMarshal(t, m))
			if tc.valid {
				if got == nil || !got.Capabilities.Video || !got.Capabilities.Public().Video {
					t.Fatalf("看视频能力应保留: got=%+v issues=%v", got, issues)
				}
			} else {
				found := false
				for _, issue := range issues {
					if issue.Path == "capabilities.video" {
						found = true
					}
				}
				if !found {
					t.Fatalf("应该在看视频字段报告错误: %v", issues)
				}
			}
		})
	}
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

// idle_timeout 是 Agent 模型的「上游响应超时」：上游多久没发数据就算超时（含等首字节）。
// 不写（或写 0）表示用平台默认值 90 秒；它和 deadline（异步生成任务的整体截止时间）是两回事。
func TestParseModel_Agent上游响应超时(t *testing.T) {
	t.Run("合法值被保留", func(t *testing.T) {
		m := mcLoadFixture(t, "model_video.json")
		mcAsAgent(m)
		m["idle_timeout"] = "3m"
		got, issues := modelcfg.ParseModel(mcMarshal(t, m))
		if got == nil || len(issues) != 0 {
			t.Fatalf("合法的 idle_timeout 应通过: %v", mcIssueList(issues))
		}
		if got.IdleTimeout.D() != 3*time.Minute {
			t.Errorf("IdleTimeout=%v", got.IdleTimeout.D())
		}
	})

	t.Run("不写就是 0，由网关取默认值", func(t *testing.T) {
		m := mcLoadFixture(t, "model_video.json")
		mcAsAgent(m)
		got, issues := modelcfg.ParseModel(mcMarshal(t, m))
		if got == nil || got.IdleTimeout != 0 {
			t.Fatalf("got=%+v issues=%v", got, mcIssueList(issues))
		}
	})

	t.Run("边界值 10s 和 10m 都合法", func(t *testing.T) {
		for _, v := range []string{"10s", "10m"} {
			m := mcLoadFixture(t, "model_video.json")
			mcAsAgent(m)
			m["idle_timeout"] = v
			if got, issues := modelcfg.ParseModel(mcMarshal(t, m)); got == nil {
				t.Errorf("%s 应合法: %v", v, mcIssueList(issues))
			}
		}
	})

	mcRunCases(t, []mcCase{
		{"太短", func(m map[string]any) { mcAsAgent(m); m["idle_timeout"] = "5s" }, "idle_timeout", "10s"},
		{"太长", func(m map[string]any) { mcAsAgent(m); m["idle_timeout"] = "11m" }, "idle_timeout", "10m"},
		{"负数", func(m map[string]any) { mcAsAgent(m); m["idle_timeout"] = "-30s" }, "idle_timeout", "10s"},
		{"格式不对", func(m map[string]any) { mcAsAgent(m); m["idle_timeout"] = "很久" }, "idle_timeout", "时长格式"},
		{"视频模型不能配：它走插件，没有这条链路", func(m map[string]any) { m["idle_timeout"] = "2m" }, "idle_timeout", "Agent"},
		{"文本模型也不能配", func(m map[string]any) { mcAsText(m); m["idle_timeout"] = "2m" }, "idle_timeout", "Agent"},
	})
}

func TestAgentModel_APIFormat(t *testing.T) {
	cases := []struct {
		name, kind, format string
		wantErrPath        string
	}{
		{"agent 默认", "agent", "", ""},
		{"agent openai", "agent", "openai", ""},
		{"agent gemini", "agent", "gemini", ""},
		{"agent 非法值", "agent", "claude", "channels[0].api_format"},
		{"agent 大小写不合法", "agent", "Gemini", "channels[0].api_format"},
		{"非 agent 默认", "text", "", ""},
		{"非 agent 不能写 gemini", "text", "gemini", "channels[0].api_format"},
		{"非 agent 不能写 openai", "text", "openai", "channels[0].api_format"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := mcLoadFixture(t, "model_video.json")
			if tc.kind == "agent" {
				mcAsAgent(m)
			} else {
				mcAsText(m)
			}
			mcChannel0(m)["api_format"] = tc.format
			got, issues := modelcfg.ParseModel(mcMarshal(t, m))
			if tc.wantErrPath != "" {
				for _, issue := range issues {
					if issue.Path == tc.wantErrPath {
						return
					}
				}
				t.Fatalf("应拒绝 %s: %v", tc.wantErrPath, mcIssueList(issues))
			}
			if got == nil || len(issues) != 0 {
				t.Fatalf("合法配置应通过: %v", mcIssueList(issues))
			}
			if got.Channels[0].APIFormat != tc.format {
				t.Errorf("APIFormat=%q，期望 %q", got.Channels[0].APIFormat, tc.format)
			}

			// 默认格式不写入快照 JSON，确保旧配置的快照形状保持不变。
			snap := provider.NewSnapshot(got, &provider.ChannelRuntime{}, 1)
			wantFormat := tc.format
			if wantFormat == "openai" {
				wantFormat = ""
			}
			if snap.Model.APIFormat != wantFormat {
				t.Errorf("快照 APIFormat=%q，期望 %q", snap.Model.APIFormat, wantFormat)
			}
			body, err := json.Marshal(snap.Model)
			if err != nil {
				t.Fatal(err)
			}
			var saved map[string]any
			if err := json.Unmarshal(body, &saved); err != nil {
				t.Fatal(err)
			}
			value, exists := saved["api_format"]
			if wantFormat == "" {
				if exists {
					t.Errorf("默认快照不应写 api_format: %s", body)
				}
			} else if value != wantFormat {
				t.Errorf("快照 api_format=%v，期望 %q", value, wantFormat)
			}
		})
	}
}

func TestChannelRef_FormatDefaultsToOpenAI(t *testing.T) {
	for _, tc := range []struct{ format, want string }{
		{"", modelcfg.APIFormatOpenAI},
		{modelcfg.APIFormatOpenAI, modelcfg.APIFormatOpenAI},
		{modelcfg.APIFormatGemini, modelcfg.APIFormatGemini},
		{"claude", "claude"},
	} {
		if got := (modelcfg.ChannelRef{APIFormat: tc.format}).Format(); got != tc.want {
			t.Errorf("Format(%q)=%q，期望 %q", tc.format, got, tc.want)
		}
	}
}
