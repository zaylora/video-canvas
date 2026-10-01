// 本文件：模型定价（Pricing）：计费方式、默认价格、规格价格与积分成本。价格一律是整数积分。
// 计价公式在 quote.go，前端 web/src/utils/pricing/quote.ts 是同一份算法，两边共用测试向量。

package modelcfg

// 计费方式。
const (
	BillingPerCall   = "per_call"   // 按次：每个任务固定积分
	BillingPerSecond = "per_second" // 按秒：每秒积分 × 时长参数 duration
	BillingToken     = "token"      // 按 Token（仅文本）：按上限预冻结，完成后按实际用量多退少补
)

// Billings 是合法的计费方式。
var Billings = []string{BillingPerCall, BillingPerSecond, BillingToken}

// 规格价格条件里除了 spec 参数名之外的两个特殊键。
const (
	TierKeyOp       = "op"        // 生成方式
	TierKeyRefVideo = "ref_video" // 本次请求的参考素材里是否有视频
)

// DurationParam 是按秒计费读取时长的参数名。
const DurationParam = "duration"

// Pricing 是模型定价。
type Pricing struct {
	Billing   string      `json:"billing"`              // per_call / per_second / token
	Unit      int         `json:"unit,omitempty"`       // per_call：积分 / 次
	PerSecond int         `json:"per_second,omitempty"` // per_second：积分 / 秒
	Token     *TokenPrice `json:"token,omitempty"`      // token：积分 / 百万 Token
	Tiers     []Tier      `json:"tiers,omitempty"`      // 规格价格：满足全部条件时覆盖默认价；条件最多的一条胜出
	Cost      *Cost       `json:"cost,omitempty"`       // 积分成本，仅管理端展示，不参与扣费，也不下发画布
}

// TokenPrice 是 Token 计费的输入价与输出价（积分 / 百万 Token）。
type TokenPrice struct {
	In  int `json:"in"`
	Out int `json:"out"`
}

// Tier 是一条规格价格。When 的键是 spec 参数名、op 或 ref_video；Unit 的单位随计费方式（积分 / 次、积分 / 秒）。
type Tier struct {
	On   bool           `json:"on"`
	When map[string]any `json:"when"`
	Unit int            `json:"unit"`
}

// Cost 是积分成本，结构随计费方式：按次 Unit、按秒 PerSecond、Token 用 Token。
type Cost struct {
	On        bool        `json:"on"`
	Unit      int         `json:"unit,omitempty"`
	PerSecond int         `json:"per_second,omitempty"`
	Token     *TokenPrice `json:"token,omitempty"`
}

// Public 返回面向画布的定价：去掉积分成本。
func (p Pricing) Public() Pricing {
	p.Cost = nil
	return p
}
