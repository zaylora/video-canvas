package modelcfg_test

import (
	"testing"

	"video-canvas/internal/provider/modelcfg"
)

func TestTokenCost(t *testing.T) {
	tokenPricing := modelcfg.Pricing{Billing: modelcfg.BillingToken, Token: &modelcfg.TokenPrice{In: 3, Out: 15}}
	cases := []struct {
		name    string
		p       modelcfg.Pricing
		in, out int
		want    int
	}{
		{"按百万 Token 单价向上取整", tokenPricing, 1_000_000, 1_000_000, 18},
		{"小用量最少收 1 积分", tokenPricing, 10, 5, 1},
		{"不封顶：用量大就收得多", tokenPricing, 10_000_000, 2_000_000, 60},
		{"零用量不收费", tokenPricing, 0, 0, 0},
		{"只有输出也收费", tokenPricing, 0, 100_000, 2},
		{"不是 Token 计费返回 0", modelcfg.Pricing{Billing: modelcfg.BillingPerCall, Unit: 5}, 1000, 1000, 0},
		{"没有单价返回 0", modelcfg.Pricing{Billing: modelcfg.BillingToken}, 1000, 1000, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := modelcfg.TokenCost(tc.p, tc.in, tc.out); got != tc.want {
				t.Errorf("TokenCost(%d,%d)=%d，期望 %d", tc.in, tc.out, got, tc.want)
			}
		})
	}
}
