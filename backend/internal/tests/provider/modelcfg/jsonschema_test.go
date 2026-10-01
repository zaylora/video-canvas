// 本文件：jsonschema.go 的单元测试：输出是合法 JSON Schema，字段与 ParseModel 的规则一致。

package modelcfg_test

import (
	"encoding/json"
	"strings"
	"testing"

	"video-canvas/internal/provider/modelcfg"
)

func TestJSONSchema(t *testing.T) {
	b := modelcfg.JSONSchema()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("schema 不是合法 JSON：%v", err)
	}
	if m["$schema"] != "http://json-schema.org/draft-07/schema#" || m["type"] != "object" {
		t.Fatalf("根节点不符：%v", m)
	}
	props, _ := m["properties"].(map[string]any)
	for _, k := range []string{"key", "kind", "label", "hint", "pricing", "deadline", "enabled", "sort", "channels", "params", "capabilities", "vendor", "tags"} {
		if _, ok := props[k]; !ok {
			t.Errorf("properties 缺少 %s", k)
		}
	}
	// 新结构里不应再有旧的 provider / mapping / output
	for _, k := range []string{"provider", "mapping", "output"} {
		if _, ok := props[k]; ok {
			t.Errorf("properties 不应再有 %s", k)
		}
	}
	if m["additionalProperties"] != false {
		t.Error("应禁止未知字段")
	}
	req, _ := m["required"].([]any)
	if len(req) != 5 {
		t.Errorf("required = %v", req)
	}
}

func TestJSONSchema_channels与枚举(t *testing.T) {
	var m map[string]any
	if err := json.Unmarshal(modelcfg.JSONSchema(), &m); err != nil {
		t.Fatal(err)
	}
	props := m["properties"].(map[string]any)
	ch := props["channels"].(map[string]any)
	if ch["type"] != "array" || ch["minItems"] != 1.0 || ch["maxItems"] != 1.0 {
		t.Fatalf("channels 应是恰好一个元素的数组：%v", ch)
	}
	item := ch["items"].(map[string]any)["properties"].(map[string]any)
	if _, ok := item["channel"]; !ok {
		t.Fatal("缺少 channels[].channel")
	}
	if _, ok := item["upstream_model"]; !ok {
		t.Fatal("缺少 channels[].upstream_model")
	}
	kinds, _ := props["kind"].(map[string]any)["enum"].([]any)
	if len(kinds) != len(modelcfg.Kinds) {
		t.Fatalf("kind 枚举 = %v", kinds)
	}
	text := string(modelcfg.JSONSchema())
	for _, want := range []string{`"text"`, `"boolean"`, `"audio"`} {
		if !strings.Contains(text, want) {
			t.Errorf("schema 缺少 %s", want)
		}
	}
}

func TestJSONSchema_返回副本(t *testing.T) {
	a := modelcfg.JSONSchema()
	a[0] = 'X'
	if b := modelcfg.JSONSchema(); b[0] != '{' {
		t.Fatal("修改返回值不应影响缓存")
	}
}

// 夹具的每个字段都要在 Schema 里有定义，Schema 声明的必填字段 ParseModel 也必须拒绝缺失，保证两处规则没有漂移。
func TestJSONSchema_与ParseModel一致(t *testing.T) {
	var s map[string]any
	if err := json.Unmarshal(modelcfg.JSONSchema(), &s); err != nil {
		t.Fatal(err)
	}
	props := s["properties"].(map[string]any)
	for k := range mcLoadFixture(t, "model_video.json") {
		if _, ok := props[k]; !ok {
			t.Errorf("夹具字段 %s 不在 Schema 里", k)
		}
	}
	for _, r := range s["required"].([]any) {
		m := mcLoadFixture(t, "model_video.json")
		delete(m, r.(string))
		if got, issues := modelcfg.ParseModel(mcMarshal(t, m)); got != nil || len(issues) == 0 {
			t.Errorf("Schema 要求 %s 必填，但 ParseModel 没有拒绝", r)
		}
	}
}
