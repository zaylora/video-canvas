package modelcfg_test

import (
	"encoding/json"
	"testing"

	"video-canvas/internal/provider/modelcfg"
)

// 渠道不再绑协议：模型 channels[0].protocol = {plugin, version, settings}，非 Agent 模型必须有，Agent 模型不能有。

func TestParseModel_protocol合法并原样解析(t *testing.T) {
	m := mcLoadFixture(t, "model_video.json")
	mcChannel0(m)["protocol"] = map[string]any{"plugin": "openai", "version": "1.2.3-rc.1", "settings": map[string]any{"api": "chat_completions"}}
	got, issues := modelcfg.ParseModel(mcMarshal(t, m))
	if got == nil || len(issues) != 0 {
		t.Fatalf("合法的 protocol 应通过：%v", mcIssueList(issues))
	}
	p := got.Channels[0].Protocol
	if p == nil || p.Plugin != "openai" || p.Version != "1.2.3-rc.1" || p.Settings["api"] != "chat_completions" {
		t.Fatalf("protocol 解析不对：%+v", p)
	}
}

func TestParseModel_protocol规则(t *testing.T) {
	mcRunCases(t, []mcCase{
		{"非 Agent 模型缺协议", func(m map[string]any) { delete(mcChannel0(m), "protocol") }, "channels[0].protocol", "调用协议"},
		{"插件 key 为空", func(m map[string]any) { mcChannel0(m)["protocol"] = map[string]any{"plugin": "", "version": "1.0.0"} }, "channels[0].protocol.plugin", ""},
		{"插件 key 含大写", func(m map[string]any) {
			mcChannel0(m)["protocol"] = map[string]any{"plugin": "OpenAI", "version": "1.0.0"}
		}, "channels[0].protocol.plugin", ""},
		{"版本号为空", func(m map[string]any) { mcChannel0(m)["protocol"] = map[string]any{"plugin": "openai", "version": ""} }, "channels[0].protocol.version", ""},
		{"版本号不是 semver", func(m map[string]any) {
			mcChannel0(m)["protocol"] = map[string]any{"plugin": "openai", "version": "latest"}
		}, "channels[0].protocol.version", "semver"},
		{"Agent 模型不能选协议", func(m map[string]any) {
			mcAsAgent(m)
			mcChannel0(m)["protocol"] = map[string]any{"plugin": "openai", "version": "1.0.0"}
		}, "channels[0].protocol", "Agent"},
	})
}

func TestJSONSchema_channels含protocol(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal(modelcfg.JSONSchema(), &schema); err != nil {
		t.Fatal(err)
	}
	props := schema["properties"].(map[string]any)
	items := props["channels"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
	proto, ok := items["protocol"].(map[string]any)
	if !ok {
		t.Fatal("缺少 channels[].protocol")
	}
	inner := proto["properties"].(map[string]any)
	for _, k := range []string{"plugin", "version", "settings"} {
		if _, ok := inner[k]; !ok {
			t.Errorf("缺少 channels[].protocol.%s", k)
		}
	}
}
