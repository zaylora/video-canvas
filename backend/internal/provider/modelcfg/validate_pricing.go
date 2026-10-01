// 本文件：模型定价（pricing）的语义校验：计费方式与种类匹配、价格为整数且在固定范围内、规格价格条件合法。

package modelcfg

import (
	"fmt"
	"sort"
	"strings"
)

const maxPrice = 1_000_000

// validatePricing 校验 pricing，问题的路径以 "pricing" 开头。能力（capabilities）已先校验过。
func validatePricing(kind string, c *Capabilities, p *Pricing, issues *[]Issue) {
	add := func(path, msg string) {
		*issues = append(*issues, Issue{Path: joinPath("pricing", path), Message: msg})
	}
	switch p.Billing {
	case BillingPerCall:
		checkPrice("unit", p.Unit, true, add)
	case BillingPerSecond:
		if f, ok := c.Params.Get(DurationParam); !ok || f.Type != ParamNumber {
			add("billing", "按秒计费需要一个名为 duration 的数字参数（视频时长）")
		}
		checkPrice("per_second", p.PerSecond, true, add)
	case BillingToken:
		if kind != KindText {
			add("billing", "只有文本模型可以按 Token 计费")
		}
		if p.Token == nil {
			add("token", "按 Token 计费需要设置输入价与输出价")
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
	default:
		add("billing", oneOfMsg(Billings))
		return
	}
	if p.Billing != BillingToken {
		for i, t := range p.Tiers {
			validateTier(fmt.Sprintf("tiers[%d]", i), kind, c, t, add)
		}
	}
	validateCost(p, add)
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
	if len(t.When) == 0 {
		add(base+".when", "至少要有一个条件，否则会盖掉默认价")
		return
	}
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

func validateCost(p *Pricing, add func(path, msg string)) {
	if p.Cost == nil || !p.Cost.On {
		return
	}
	switch p.Billing {
	case BillingPerCall:
		checkPrice("cost.unit", p.Cost.Unit, false, add)
	case BillingPerSecond:
		checkPrice("cost.per_second", p.Cost.PerSecond, false, add)
	case BillingToken:
		if p.Cost.Token == nil {
			add("cost.token", "按 Token 计费的成本需要输入价与输出价")
			return
		}
		checkPrice("cost.token.in", p.Cost.Token.In, false, add)
		checkPrice("cost.token.out", p.Cost.Token.Out, false, add)
	}
}
