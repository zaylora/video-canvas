// 本文件：quote.go 的测试：跑与前端共用的计价测试向量（testdata/pricing_vectors.json），再补几条公开定价与校验的用例。

package modelcfg_test

import (
	"encoding/json"
	"os"
	"testing"

	"video-canvas/internal/provider/modelcfg"
)

type mcPricingVectors struct {
	Quote []struct {
		Name         string                `json:"name"`
		Capabilities modelcfg.Capabilities `json:"capabilities"`
		Pricing      modelcfg.Pricing      `json:"pricing"`
		Spec         struct {
			Op          string         `json:"op"`
			RefVideo    bool           `json:"ref_video"`
			Params      map[string]any `json:"params"`
			PromptChars int            `json:"prompt_chars"`
		} `json:"spec"`
		One   int `json:"one"`
		N     int `json:"n"`
		Total int `json:"total"`
	} `json:"quote"`
	Settle []struct {
		Name    string           `json:"name"`
		Pricing modelcfg.Pricing `json:"pricing"`
		Frozen  int              `json:"frozen"`
		Usage   *modelcfg.Usage  `json:"usage"`
		Charge  int              `json:"charge"`
	} `json:"settle"`
}

func mcLoadVectors(t *testing.T) mcPricingVectors {
	t.Helper()
	b, err := os.ReadFile("../../testdata/pricing_vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var v mcPricingVectors
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Quote) == 0 || len(v.Settle) == 0 {
		t.Fatal("测试向量为空")
	}
	return v
}

func TestQuote_共用向量(t *testing.T) {
	for _, c := range mcLoadVectors(t).Quote {
		t.Run(c.Name, func(t *testing.T) {
			spec := modelcfg.Spec{Op: c.Spec.Op, RefVideo: c.Spec.RefVideo, Params: c.Spec.Params, PromptChars: c.Spec.PromptChars}
			one := modelcfg.Quote(c.Pricing, c.Capabilities, spec)
			n := modelcfg.FanoutCount(c.Capabilities, c.Spec.Params)
			if one != c.One || n != c.N || one*n != c.Total {
				t.Fatalf("one=%d n=%d total=%d，期望 %d %d %d", one, n, one*n, c.One, c.N, c.Total)
			}
		})
	}
}

func TestSettle_共用向量(t *testing.T) {
	for _, c := range mcLoadVectors(t).Settle {
		t.Run(c.Name, func(t *testing.T) {
			if got := modelcfg.Settle(c.Pricing, c.Frozen, c.Usage); got != c.Charge {
				t.Fatalf("Settle = %d，期望 %d", got, c.Charge)
			}
		})
	}
}

func TestSpecFromInput(t *testing.T) {
	in := map[string]any{"prompt": "一只猫", "op": "omni", "videos": []uint64{3}, "duration": 5.0}
	s := modelcfg.SpecFromInput(in, "系统")
	if s.Op != "omni" || !s.RefVideo || s.PromptChars != 5 || s.Params["duration"] != 5.0 {
		t.Fatalf("规格不符：%+v", s)
	}
	if modelcfg.SpecFromInput(map[string]any{"prompt": "x"}, "").RefVideo {
		t.Fatal("没有参考视频时 RefVideo 应为 false")
	}
}

func TestPricing_Public不含成本(t *testing.T) {
	p := modelcfg.Pricing{Billing: modelcfg.BillingPerCall, Unit: 3, Cost: &modelcfg.Cost{On: true, Unit: 1}}
	if got := p.Public(); got.Cost != nil || got.Unit != 3 {
		t.Fatalf("Public() = %+v", got)
	}
	if p.Cost == nil {
		t.Fatal("Public 不应修改原值")
	}
}
