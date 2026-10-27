package llmgateway_test

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"video-canvas/internal/llmgateway"
)

func cleanSchema(t *testing.T, input string) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal(llmgateway.CleanSchemaForGemini(json.RawMessage(input)), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestCleanSchema_ConstAnyOfBecomesEnum(t *testing.T) {
	got := cleanSchema(t, `{"type":"object","properties":{"status":{"description":"状态","anyOf":[{"type":"string","const":"todo","description":"待办"},{"type":"string","enum":["doing","done"]}]}}}`)
	want := map[string]any{"type": "string", "description": "状态", "enum": []any{"todo", "doing", "done"}}
	if !reflect.DeepEqual(got["properties"].(map[string]any)["status"], want) {
		t.Fatalf("schema=%v", got)
	}
}

func TestCleanSchema_MixedAnyOfBecomesString(t *testing.T) {
	got := cleanSchema(t, `{"description":"截帧时刻","anyOf":[{"type":"string","enum":["first","last"]},{"type":"number","minimum":0}]}`)
	if !reflect.DeepEqual(got, map[string]any{"type": "string", "description": "截帧时刻（也可以写秒数，例如 \"3.5\"）"}) {
		t.Fatalf("schema=%v", got)
	}
}

func TestCleanSchema_DropsUnsupportedKeywords(t *testing.T) {
	got := cleanSchema(t, `{"$schema":"draft","type":"array","minItems":1,"maxItems":6,"description":"帧","items":{"type":"object","additionalProperties":false,"patternProperties":{".*":{}},"properties":{"at":{"type":"number","minimum":0,"maximum":600,"const":1}}}}`)
	if got["description"] != "帧（1 到 6 项）" {
		t.Fatalf("description=%v", got)
	}
	at := got["items"].(map[string]any)["properties"].(map[string]any)["at"].(map[string]any)
	if at["description"] != "（0 到 600）" {
		t.Fatalf("at=%v", at)
	}
	assertCleanSchema(t, got)
	for _, tc := range []struct{ input, want string }{
		{`{"type":"array","minItems":2}`, "（至少 2）"},
		{`{"type":"number","maximum":3.5}`, "（最多 3.5）"},
	} {
		if got := cleanSchema(t, tc.input); got["description"] != tc.want {
			t.Fatalf("schema=%v", got)
		}
	}
}

func assertCleanSchema(t *testing.T, value any) {
	t.Helper()
	switch v := value.(type) {
	case map[string]any:
		for k, x := range v {
			switch k {
			case "anyOf", "const", "patternProperties", "additionalProperties", "minItems", "maxItems", "minimum", "maximum", "$schema":
				t.Errorf("清洗后仍有 %s", k)
			}
			assertCleanSchema(t, x)
		}
	case []any:
		for _, x := range v {
			assertCleanSchema(t, x)
		}
	}
}

func TestCleanSchema_AllAgentToolsAreClean(t *testing.T) {
	data, err := os.ReadFile("../../service/agent/tool_decls.json")
	if err != nil {
		t.Fatal(err)
	}
	var tools []struct {
		Name       string          `json:"name"`
		Parameters json.RawMessage `json:"parameters"`
	}
	if err := json.Unmarshal(data, &tools); err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools {
		t.Run(tool.Name, func(t *testing.T) {
			got := cleanSchema(t, string(tool.Parameters))
			assertCleanSchema(t, got)
			if got["type"] != "object" {
				t.Fatalf("工具根类型丢失: %v", got)
			}
		})
	}
}

func TestCleanSchema_PreservesPropertyNames(t *testing.T) {
	got := cleanSchema(t, `{"type":"object","properties":{"minimum":{"type":"number","minimum":0},"const":{"type":"string"}},"required":["const"]}`)
	props := got["properties"].(map[string]any)
	if props["minimum"] == nil || props["const"] == nil || !reflect.DeepEqual(got["required"], []any{"const"}) {
		t.Fatalf("schema=%v", got)
	}
}

func TestToGemini_CleansToolParameters(t *testing.T) {
	out := geminiRequest(t, `{"messages":[{"role":"user","content":"x"}],"tools":[{"type":"function","function":{"name":"plan_update","description":"更新计划","parameters":{"type":"object","properties":{"status":{"anyOf":[{"type":"string","const":"todo"},{"type":"string","const":"done"}]}}}}}]}`)
	fn := out["tools"].([]any)[0].(map[string]any)["functionDeclarations"].([]any)[0].(map[string]any)
	if fn["name"] != "plan_update" || fn["description"] != "更新计划" {
		t.Fatalf("fn=%v", fn)
	}
	b, _ := json.Marshal(fn["parameters"])
	if strings.Contains(string(b), "anyOf") || !strings.Contains(string(b), `"enum":["todo","done"]`) {
		t.Fatalf("parameters=%s", b)
	}
}
