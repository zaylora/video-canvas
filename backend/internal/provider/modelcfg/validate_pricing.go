// 本文件：模型定价（pricing）的语义校验：计费方式与种类匹配、价格为整数且在固定范围内、规格价格条件合法。

package modelcfg

import (
	"fmt"
	"sort"
	"strings"
)

const maxPrice = 1_000_000

// validatePricing 校验 pricing，问题的路径以 "pricing" 开头。能力（capabilities）已先校验过。
func validatePricing(kind string, c *Capabilities, p *Pricing, enabled bool, issues *[]Issue) {
	add := func(path, msg string) {
		*issues = append(*issues, Issue{Path: joinPath("pricing", path), Message: msg})
	}
	if p.Pending {
		validatePendingPricing(p, enabled, add)
	}
	if kind == KindAgent && p.Billing != BillingToken {
		add("billing", "Agent 模型只能按 Token 计费")
		return
	}
	switch p.Billing {
	case BillingPerCall:
		checkPrice("unit", p.Unit, !p.Pending, add)
	case BillingPerSecond:
		if f, ok := c.Params.Get(DurationParam); !ok || f.Type != ParamNumber {
			add("billing", "按秒计费需要一个名为 duration 的数字参数（视频时长）")
		}
		checkPrice("per_second", p.PerSecond, !p.Pending, add)
	case BillingToken:
		validateTokenPricing(kind, p, add)
	default:
		add("billing", oneOfMsg(Billings))
		return
	}
	if p.Billing != BillingToken {
		for i, t := range p.Tiers {
			validateTier(fmt.Sprintf("tiers[%d]", i), kind, c, t, add)
			validateCost(p.Billing, t.Cost, fmt.Sprintf("tiers[%d].cost", i), add)
		}
	}
	validateCost(p.Billing, p.Cost, "cost", add)
}

// validatePendingPricing 未知售价使用明确状态，不能同时夹带占位售价或被启用。
func validatePendingPricing(p *Pricing, enabled bool, add func(string, string)) {
	if enabled {
		add("pending", "售价待确认的草稿不能启用，请先完成定价")
	}
	if p.Unit != 0 || p.PerSecond != 0 || p.Token != nil || len(p.Tiers) != 0 {
		add("pending", "售价待确认时不能填写占位售价或规格价格")
	}
}

// validateTokenPricing 按 Token 校验售价；未定价草稿只省略价格，不放宽模型种类约束。
func validateTokenPricing(kind string, p *Pricing, add func(string, string)) {
	if kind != KindText && kind != KindAgent {
		add("billing", "只有文本、Agent 模型可以按 Token 计费")
	}
	if p.Token == nil {
		if !p.Pending {
			add("token", "按 Token 计费需要设置输入价与输出价")
		}
	} else {
		checkPrice("token.in", p.Token.In, false, add)
		checkPrice("token.out", p.Token.Out, false, add)
		if p.Token.In == 0 && p.Token.Out == 0 {
			add("token", "输入价与输出价不能都为 0")
		}
	}
	if len(p.Tiers) > 0 {
		add("tiers", "按 Token 计费不支持规格价格")
	}
}

// checkPrice 价格必须是 0 – maxPrice 的整数；positive 为 true 时必须大于 0（发布前当前计费方式下的默认价）。
func checkPrice(path string, v int, positive bool, add func(path, msg string)) {
	switch {
	case v < 0 || v > maxPrice:
		add(path, fmt.Sprintf("价格必须在 0 – %d 积分之间", maxPrice))
	case positive && v == 0:
		add(path, "价格必须大于 0 积分")
	}
}

func validateTier(base, kind string, c *Capabilities, t Tier, add func(path, msg string)) {
	checkPrice(base+".unit", t.Unit, false, add)
	keys := make([]string, 0, len(t.When))
	for k := range t.When {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if msg := tierCondIssue(kind, c, k, t.When[k]); msg != "" {
			add(base+".when."+k, msg)
		}
	}
}

// tierCondIssue 检查一个规格价格条件，合法返回空串。
func tierCondIssue(kind string, c *Capabilities, key string, v any) string {
	switch key {
	case TierKeyOp:
		s, _ := v.(string)
		if !inStrings(c.Ops, s) {
			if len(opsOfKind[kind]) == 0 {
				return "这个种类的模型没有生成方式"
			}
			return "生成方式必须是已勾选的 " + strings.Join(c.Ops, " / ") + " 之一"
		}
	case TierKeyRefVideo:
		if _, ok := v.(bool); !ok {
			return "必须是 true / false"
		}
		if !c.Refs.Video.On {
			return "参考视频没有开启，这个条件永远不会成立"
		}
	default:
		f, ok := c.Params.Get(key)
		switch {
		case !ok:
			return fmt.Sprintf("参数 %s 不存在", key)
		case !f.Spec:
			return fmt.Sprintf("参数 %s 没有设为规格价格维度", key)
		case f.Type == ParamBoolean:
			if _, ok := v.(bool); !ok {
				return "必须是 true / false"
			}
		case f.Type == ParamEnum:
			if _, ok := matchOption(f.Options, v); !ok {
				return fmt.Sprintf("条件里的 %s 已不可选", stringify(v))
			}
		}
	}
	return ""
}

// validateCost 校验一份积分成本（默认价的 cost，或某条规格价格的 cost）；base 是它在 pricing 下的路径。
func validateCost(billing string, c *Cost, base string, add func(path, msg string)) {
	if c == nil || !c.On {
		return
	}
	switch billing {
	case BillingPerCall:
		checkPrice(base+".unit", c.Unit, false, add)
	case BillingPerSecond:
		checkPrice(base+".per_second", c.PerSecond, false, add)
	case BillingToken:
		if c.Token == nil {
			add(base+".token", "按 Token 计费的成本需要输入价与输出价")
			return
		}
		checkPrice(base+".token.in", c.Token.In, false, add)
		checkPrice(base+".token.out", c.Token.Out, false, add)
	}
}
