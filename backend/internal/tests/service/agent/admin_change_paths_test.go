package agent_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	. "video-canvas/internal/service/agent"
)

func TestAdminChangePricePaths(t *testing.T) {
	for _, tc := range []struct {
		name, path, canonical    string
		cost, duplicate, invalid bool
	}{
		{name: "原有点号路径", path: "pricing.unit", canonical: "pricing.unit"},
		{name: "默认价格 Pointer", path: "/pricing/unit", canonical: "pricing.unit"},
		{name: "按秒价格 Pointer", path: "/pricing/per_second", canonical: "pricing.per_second"},
		{name: "规格价格 Pointer", path: "/pricing/tiers/0/unit", canonical: "pricing.tiers[0].unit"},
		{name: "原有规格路径", path: "pricing.tiers[0].unit", canonical: "pricing.tiers[0].unit"},
		{name: "成本依据 Pointer", path: "/pricing/unit", canonical: "pricing.unit", cost: true},
		{name: "规格成本依据 Pointer", path: "/pricing/tiers/0/unit", canonical: "pricing.tiers[0].unit", cost: true},
		{name: "输入 Token Pointer", path: "/pricing/token/in", canonical: "pricing.token.in"},
		{name: "输出 Token Pointer", path: "/pricing/token/out", canonical: "pricing.token.out"},
		{name: "两种格式指向同一字段视为重复", path: "/pricing/unit", canonical: "pricing.unit", duplicate: true},
		{name: "成本字段不能冒充售价", path: "/pricing/cost/unit", canonical: "pricing.unit", invalid: true},
		{name: "负下标拒绝", path: "/pricing/tiers/-1/unit", canonical: "pricing.unit", invalid: true},
		{name: "空路径片段拒绝", path: "/pricing//unit", canonical: "pricing.unit", invalid: true},
		{name: "混用点号拒绝", path: "/pricing/token.in", canonical: "pricing.unit", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newAdminFake()
			var body map[string]any
			_ = json.Unmarshal(f.bodies["a"], &body)
			var pricing, patch map[string]any
			switch tc.canonical {
			case "pricing.per_second":
				body["capabilities"].(map[string]any)["params"] = map[string]any{"duration": map[string]any{"type": "number", "label": "时长", "open": true, "min": 1, "max": 10, "default": 5}}
				pricing = map[string]any{"billing": "per_second", "per_second": 2}
				patch = map[string]any{"per_second": float64(3)}
			case "pricing.tiers[0].unit":
				pricing = map[string]any{"billing": "per_call", "unit": 2, "tiers": []any{map[string]any{"on": true, "when": map[string]any{}, "unit": 2}}}
				tier := map[string]any{"on": true, "when": map[string]any{}, "unit": float64(3)}
				if tc.cost {
					tier["cost"] = map[string]any{"unit": float64(2)}
				}
				patch = map[string]any{"tiers": []any{tier}}
			case "pricing.token.in", "pricing.token.out":
				body["kind"] = "agent"
				delete(body["channels"].([]any)[0].(map[string]any), "protocol")
				body["capabilities"] = map[string]any{"prompt": map[string]any{"max_length": 2000}, "context": map[string]any{"window": 128000, "output": 4096}}
				pricing = map[string]any{"billing": "token", "token": map[string]any{"in": 2, "out": 2}}
				patch = map[string]any{"token": map[string]any{strings.TrimPrefix(tc.canonical, "pricing.token."): float64(3)}}
			default:
				pricing = map[string]any{"billing": "per_call", "unit": 2}
				patch = map[string]any{"unit": float64(3)}
				if tc.cost {
					patch["cost"] = map[string]any{"on": true, "unit": float64(2)}
				}
			}
			body["pricing"] = pricing
			f.bodies["a"], _ = json.Marshal(body)
			in := relativeInput(f, "a")
			in.Patch = map[string]any{"pricing": patch}
			in.PricingBasis[0].Path = tc.path
			if tc.cost {
				in.PricingBasis[0] = PricingBasis{Path: tc.path, Mode: "cost", Upstream: &PricingUpstream{Amount: 0.2, Currency: "CNY", Unit: "次", SourceURL: "https://official.example/pricing"}, MarkupPct: 50, Price: 3}
			}
			if tc.duplicate {
				in.PricingBasis = append(in.PricingBasis, PricingBasis{Path: tc.canonical, Mode: "relative", From: 2, Pct: 10, Price: 3})
			}
			svc, _, _ := adminEnv(f)
			p, err := svc.BuildChanges(context.Background(), AdminChangeRequest{Title: "改价", Reason: "要求", Items: []AdminChangeInput{in}})
			if err != nil {
				t.Fatal(err)
			}
			item := p.Items[0]
			if tc.invalid || tc.duplicate {
				if item.Status != "invalid" {
					t.Fatalf("非法路径或重复依据未拒绝：%+v", item)
				}
				if tc.duplicate && !strings.Contains(strings.Join(item.Issues, "\n"), "价格依据重复：pricing.unit") {
					t.Fatalf("未按规范路径去重：%v", item.Issues)
				}
				return
			}
			if item.Status != "ok" || len(item.Basis) != 1 || item.Basis[0].Path != tc.canonical {
				t.Fatalf("规范化失败：%+v", item)
			}
			found := false
			for _, diff := range item.Diff {
				if strings.HasPrefix(diff.Path, "/") {
					t.Fatalf("diff 未规范化：%+v", diff)
				}
				found = found || diff.Path == tc.canonical
			}
			if !found {
				t.Fatalf("diff 缺少规范路径：%+v", item.Diff)
			}
		})
	}
}

func TestAdminProposeFailureCompact(t *testing.T) {
	f := newAdminFake()
	f.live["a"] = true
	_, bridge, rc := adminEnv(f)
	first, second := relativeInput(f, "a"), relativeInput(f, "b")
	first.BaseSHA256 = "过期摘要"
	second.PricingBasis = nil
	raw, _ := json.Marshal(AdminChangeRequest{Title: "改价", Reason: "要求", Items: []AdminChangeInput{first, second}})
	result, err := bridge.ProposeApproval(context.Background(), rc, "admin_change", raw)
	if err != nil || !result.IsError {
		t.Fatalf("应返回工具错误：%+v %v", result, err)
	}
	lines := strings.Split(result.Content, "\n")
	if len(lines) != 3 {
		t.Fatalf("应为说明加两个条目，每项一行：%s", result.Content)
	}
	for i, line := range lines[1:] {
		var item map[string]any
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			t.Fatal(err)
		}
		wantKey, wantStatus := "a", "stale"
		if i == 1 {
			wantKey, wantStatus = "b", "invalid"
		}
		if item["key"] != wantKey || item["status"] != wantStatus || len(item["issues"].([]any)) == 0 {
			t.Fatalf("缺少修正信息：%v", item)
		}
		for key := range item {
			if key != "key" && key != "status" && key != "issues" && key != "warnings" {
				t.Fatalf("不应返回字段 %s", key)
			}
		}
		if i == 0 && len(item["warnings"].([]any)) == 0 {
			t.Fatal("应保留保存即生效警告")
		}
	}
}

func TestAdminPricePathDescription(t *testing.T) {
	for _, decl := range AgentToolDecls("admin", false, false) {
		if decl.Name != "admin_propose_changes" {
			continue
		}
		var schema map[string]any
		if err := json.Unmarshal(decl.Parameters, &schema); err != nil {
			t.Fatal(err)
		}
		path := schema["properties"].(map[string]any)["items"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)["pricing_basis"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)["path"].(map[string]any)
		description, _ := path["description"].(string)
		for _, example := range []string{"pricing.per_second", "pricing.tiers[0].unit", "/pricing/per_second", "/pricing/tiers/0/unit"} {
			if !strings.Contains(description, example) {
				t.Fatalf("path 说明缺少 %s：%s", example, description)
			}
		}
		return
	}
	t.Fatal("未找到提案工具")
}

func TestAdminProposeFailureCompactBatch(t *testing.T) {
	f := newAdminFake()
	_, bridge, rc := adminEnv(f)
	items := make([]AdminChangeInput, 50)
	for i := range items {
		items[i] = relativeInput(f, "a")
		items[i].PricingBasis = nil
	}
	raw, _ := json.Marshal(AdminChangeRequest{Title: "批量改价", Reason: "要求", Items: items})
	result, err := bridge.ProposeApproval(context.Background(), rc, "admin_change", raw)
	if err != nil || !result.IsError {
		t.Fatalf("应返回工具错误：%+v %v", result, err)
	}
	lines := strings.Split(result.Content, "\n")
	if len(lines) != 51 {
		t.Fatalf("批量错误应完整返回每项一行，实际 %d 行", len(lines))
	}
	for _, line := range lines[1:] {
		var item map[string]any
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			t.Fatalf("条目被截断：%v", err)
		}
		if len(item) != 3 || item["key"] != "a" || item["status"] != "invalid" {
			t.Fatalf("条目未精简：%v", item)
		}
	}
}
