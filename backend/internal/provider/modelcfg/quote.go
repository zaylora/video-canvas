// 本文件：计价（Quote）：按定价规则和本次选定的规格算出每个任务要冻结的积分，以及 Token 任务完成后的实际扣费。
// 纯函数，不碰数据库。前端 web/src/utils/pricing/quote.ts 是同一份算法，
// 两边都用 backend/internal/tests/testdata/pricing_vectors.json 里的测试向量，防止算法漂移。

package modelcfg

import "math"

// Spec 是一次提交选定的规格。
type Spec struct {
	Op       string         // 生成方式；文本、音频为空
	RefVideo bool           // 参考素材里是否有视频（且当前方式允许）
	Params   map[string]any // 生成参数的取值（未开放的参数已按默认值补齐）
	// PromptChars 是提示词字数 + 固定系统提示字数，Token 计费按 1 字 = 1 Token 保守预估输入量
	PromptChars int
}

// Usage 是文本任务的实际 Token 用量（插件从上游响应里取）。
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// SpecFromInput 从规范化后的任务输入里取出计价用的规格。system 是模型的固定系统提示。
func SpecFromInput(input map[string]any, system string) Spec {
	s := Spec{Params: input}
	s.Op, _ = input["op"].(string)
	s.RefVideo = len(mediaRefsOf(input, MediaKeyVideos)) > 0
	prompt, _ := input["prompt"].(string)
	s.PromptChars = runeLen(prompt) + runeLen(system)
	return s
}

// MatchTier 返回命中的规格价格：on 且 when 的每一项都等于规格的取值；取条件最多的一条，条件数相同取靠前的。
// 没有任何条件的规格价格不参与匹配。没有命中返回 nil。
func MatchTier(p Pricing, s Spec) *Tier {
	var best *Tier
	for i := range p.Tiers {
		t := &p.Tiers[i]
		if !t.On || len(t.When) == 0 || !tierMatches(t.When, s) {
			continue
		}
		if best == nil || len(t.When) > len(best.When) {
			best = t
		}
	}
	return best
}

func tierMatches(when map[string]any, s Spec) bool {
	for k, want := range when {
		var got any
		switch k {
		case TierKeyOp:
			got = s.Op
		case TierKeyRefVideo:
			got = s.RefVideo
		default:
			got = s.Params[k]
		}
		if !sameValue(got, want) {
			return false
		}
	}
	return true
}

// sameValue 比较条件值与规格取值：布尔按布尔比，其余按文本比（"5" 与 5 视为相同）。
func sameValue(got, want any) bool {
	if wb, ok := want.(bool); ok {
		gb, ok := got.(bool)
		return ok && gb == wb
	}
	if got == nil {
		return false
	}
	return stringify(got) == stringify(want)
}

// Quote 返回每个任务要冻结的积分（整数，≥ 0）。
//   - per_call：命中规格价取它的 unit，否则取默认 unit；
//   - per_second：每秒价（规格价或默认 per_second）× 时长参数 duration；
//   - token：max(1, ceil((预估输入 × 输入价 + 最大输出 × 输出价) / 100 万))，按上限预冻结。
func Quote(p Pricing, c Capabilities, s Spec) int {
	switch p.Billing {
	case BillingPerCall:
		if t := MatchTier(p, s); t != nil {
			return t.Unit
		}
		return p.Unit
	case BillingPerSecond:
		rate := p.PerSecond
		if t := MatchTier(p, s); t != nil {
			rate = t.Unit
		}
		return rate * intParam(s.Params, DurationParam)
	case BillingToken:
		maxOut := 0
		if c.Context != nil {
			maxOut = c.Context.Output
		}
		return tokenCredits(p.Token, s.PromptChars, maxOut)
	}
	return 0
}

// Settle 返回任务成功时实际扣的积分。Token 计费按实际用量算，且不超过冻结额（多冻结的退回）；
// 插件没回传用量（usage 为 nil）时按冻结额扣。其他计费方式扣冻结额。
func Settle(p Pricing, frozen int, usage *Usage) int {
	if p.Billing != BillingToken || usage == nil {
		return frozen
	}
	return min(tokenCredits(p.Token, usage.InputTokens, usage.OutputTokens), frozen)
}

// tokenCredits 按百万 Token 单价算积分：只有这里会出现小数，向上取整，最少 1 积分。
func tokenCredits(price *TokenPrice, in, out int) int {
	if price == nil {
		return 0
	}
	raw := float64(in)*float64(price.In) + float64(out)*float64(price.Out)
	return max(1, int(math.Ceil(raw/1_000_000)))
}

// FanoutCount 返回“生成数量”参数的取值（一次提交拆成几个任务）；模型没有 fanout 参数时为 1。
func FanoutCount(c Capabilities, params map[string]any) int {
	for _, e := range c.Params {
		if e.Fanout {
			if n := intParam(params, e.Name); n > 0 {
				return n
			}
			return 1
		}
	}
	return 1
}

// FanoutParam 返回 fanout 参数名，没有返回空串。
func FanoutParam(c Capabilities) string {
	for _, e := range c.Params {
		if e.Fanout {
			return e.Name
		}
	}
	return ""
}

// intParam 取整数参数；数字或数字字符串都认（前端可能把枚举值存成字符串），取不到为 0。
func intParam(params map[string]any, name string) int {
	f, ok := toFloat(params[name])
	if s, isStr := params[name].(string); !ok && isStr {
		f, ok = parseNumberString(s)
	}
	if !ok {
		return 0
	}
	return int(f)
}

func runeLen(s string) int { return len([]rune(s)) }

func mediaRefsOf(input map[string]any, key string) []MediaRef {
	var out []MediaRef
	for _, r := range mediaRefs(input) {
		if r.Key == key {
			out = append(out, r)
		}
	}
	return out
}
