package agent_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	. "video-canvas/internal/service/agent"
)

func TestAdminQuotePreviewOptionalPrompt(t *testing.T) {
	for _, tc := range []struct {
		name, args, wantPrompt string
		wantError              bool
	}{
		{name: "省略 input", args: `{"key":"a"}`},
		{name: "空 input", args: `{"key":"a","input":{}}`},
		{name: "null input", args: `{"key":"a","input":null}`},
		{name: "省略提示词", args: `{"key":"a","input":{"op":"t2v"}}`},
		{name: "空提示词", args: `{"key":"a","input":{"prompt":""}}`},
		{name: "空白提示词", args: `{"key":"a","input":{"prompt":" \t "}}`},
		{name: "null 提示词", args: `{"key":"a","input":{"prompt":null}}`},
		{name: "保留已有提示词", args: `{"key":"a","input":{"prompt":"用户提示词"}}`, wantPrompt: "用户提示词"},
		{name: "仍校验生成方式", args: `{"key":"a","input":{"op":"invalid"}}`, wantError: true},
		{name: "仍校验提示词长度", args: `{"key":"a","input":{"prompt":"` + strings.Repeat("字", 2001) + `"}}`, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, bridge, rc := adminEnv(newAdminFake())
			result, err := bridge.ExecutePlatformTool(context.Background(), rc, "quote", "admin_quote_preview", json.RawMessage(tc.args))
			if err != nil {
				t.Fatal(err)
			}
			if result.IsError != tc.wantError {
				t.Fatalf("算价结果不符：%+v", result)
			}
			if tc.wantError {
				return
			}
			var quote struct {
				PerTask int            `json:"per_task_credits"`
				Count   int            `json:"count"`
				Total   int            `json:"quote_credits"`
				Input   map[string]any `json:"input"`
			}
			if err := json.Unmarshal([]byte(result.Content), &quote); err != nil {
				t.Fatal(err)
			}
			prompt, _ := quote.Input["prompt"].(string)
			if quote.PerTask != 2 || quote.Count != 1 || quote.Total != 2 || quote.Input["op"] != "t2v" || strings.TrimSpace(prompt) == "" {
				t.Fatalf("价格或默认输入不符：%+v", quote)
			}
			if tc.wantPrompt != "" && prompt != tc.wantPrompt {
				t.Fatalf("已有提示词被覆盖：%q", prompt)
			}
		})
	}
}

func TestAdminQuotePreviewDescription(t *testing.T) {
	for _, decl := range AgentToolDecls("admin", false, false) {
		if decl.Name == "admin_quote_preview" {
			if !strings.Contains(decl.Description, "input 只需给影响价格的参数（规格、时长、数量），提示词和素材可省略") {
				t.Fatalf("算价说明缺少可省略项：%s", decl.Description)
			}
			return
		}
	}
	t.Fatal("未找到算价工具")
}

func TestAdminQuotePreviewPriceParameters(t *testing.T) {
	for _, tc := range []struct {
		name, input    string
		perTask, count int
		wantError      bool
	}{
		{name: "按默认规格时长数量算价", input: `{}`, perTask: 8, count: 1},
		{name: "按指定规格时长数量算价", input: `{"size":"large","duration":6,"count":3}`, perTask: 30, count: 3},
		{name: "非法规格仍拒绝", input: `{"size":"unknown"}`, wantError: true},
		{name: "超出时长仍拒绝", input: `{"duration":100}`, wantError: true},
		{name: "非法数量仍拒绝", input: `{"count":0}`, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newAdminFake()
			var body map[string]any
			_ = json.Unmarshal(f.bodies["a"], &body)
			caps := body["capabilities"].(map[string]any)
			caps["ops"] = []string{"i2v"}
			caps["refs"] = map[string]any{"image": map[string]any{"on": true, "max": 1, "max_mb": 10}}
			caps["params"] = map[string]any{
				"size":     map[string]any{"type": "enum", "label": "规格", "open": true, "spec": true, "options": []string{"small", "large"}, "default": "small"},
				"duration": map[string]any{"type": "number", "label": "时长", "open": true, "min": 1, "max": 10, "default": 4},
				"count":    map[string]any{"type": "enum", "label": "数量", "open": true, "options": []int{1, 3}, "default": 1, "fanout": true},
			}
			body["pricing"] = map[string]any{"billing": "per_second", "per_second": 2, "tiers": []any{map[string]any{"on": true, "when": map[string]any{"size": "large"}, "unit": 5}}}
			_, bridge, rc := adminEnv(f)
			args, _ := json.Marshal(map[string]any{"body": body, "input": json.RawMessage(tc.input)})
			result, err := bridge.ExecutePlatformTool(context.Background(), rc, "quote", "admin_quote_preview", args)
			if err != nil || result.IsError != tc.wantError {
				t.Fatalf("算价结果不符：%+v %v", result, err)
			}
			if tc.wantError {
				return
			}
			var quote struct {
				PerTask int `json:"per_task_credits"`
				Count   int `json:"count"`
				Total   int `json:"quote_credits"`
			}
			if err := json.Unmarshal([]byte(result.Content), &quote); err != nil {
				t.Fatal(err)
			}
			if quote.PerTask != tc.perTask || quote.Count != tc.count || quote.Total != tc.perTask*tc.count {
				t.Fatalf("规格时长数量未正确计价：%+v", quote)
			}
		})
	}
}
