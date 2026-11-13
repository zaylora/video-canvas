package marketing

import (
	_ "embed" // 内置方法正文
	"encoding/json"
	"fmt"
	"strings"

	"video-canvas/internal/stepkit"
)

// factsMethod 是商品事实卡的内置方法正文。
//
//go:embed methods/facts.md
var factsMethod string

// 事实卡字段上限（字符数 / 条数）。
const (
	factsNameMax      = 80
	factsCategoryMax  = 40
	factsAnchorMax    = 120
	factsAnchorsMax   = 8
	factsTextMax      = 80
	factsProofMax     = 140
	factsPointsMax    = 5
	factsProhibitMax  = 120
	factsProhibitsMax = 10
	factsAudienceMax  = 80
	factsCTAMax       = 80
)

// 卖点来源。
const (
	SourceDescription = "description" // 用户描述里说过
	SourceImage       = "image"       // 图里看得见
)

// factsSchema 是商品事实卡的输出 Schema。
var factsSchema = json.RawMessage(fmt.Sprintf(`{
  "type": "object",
  "required": ["name", "category", "anchors", "selling_points", "prohibited", "audience", "cta"],
  "properties": {
    "name": {"type": "string", "minLength": 1, "maxLength": %d, "description": "商品名，带规格（如 50ml）；图上和描述里都没有就写品类"},
    "category": {"type": "string", "minLength": 1, "maxLength": %d, "description": "商品品类"},
    "anchors": {"type": "array", "minItems": 1, "maxItems": %d, "description": "外观锚点：图里看得见的材质、颜色、形状、logo 位置，每条一个特征",
      "items": {"type": "string", "minLength": 1, "maxLength": %d}},
    "selling_points": {"type": "array", "minItems": 1, "maxItems": %d, "description": "已证实的卖点，每条要有画面证明",
      "items": {"type": "object", "required": ["text", "proof", "source"], "properties": {
        "text": {"type": "string", "minLength": 1, "maxLength": %d, "description": "卖点文字"},
        "proof": {"type": "string", "minLength": 1, "maxLength": %d, "description": "用什么画面来证明它"},
        "source": {"type": "string", "enum": ["description", "image"], "description": "来源：description 用户描述里说过，image 图里看得见"}
      }}},
    "prohibited": {"type": "array", "minItems": 1, "maxItems": %d, "description": "禁用说法，默认带「不编造价格、销量、评价、认证与功效」",
      "items": {"type": "string", "minLength": 1, "maxLength": %d}},
    "audience": {"type": "string", "minLength": 1, "maxLength": %d, "description": "目标人群"},
    "cta": {"type": "string", "minLength": 1, "maxLength": %d, "description": "引导下单的一句话"}
  }
}`, factsNameMax, factsCategoryMax, factsAnchorsMax, factsAnchorMax, factsPointsMax,
	factsTextMax, factsProofMax, factsProhibitsMax, factsProhibitMax, factsAudienceMax, factsCTAMax))

// FactsInput 组装商品事实卡环节的输入。images 是商品图（data URL 或可访问 URL），作为多模态附件。
func FactsInput(b Brief, images []string) stepkit.Input {
	vars := baseVars(b)
	vars["description"] = b.Description
	vars["image_count"] = len(images)
	return stepkit.Input{Vars: vars, Images: images}
}

// DecodeFacts 解码已通过校验的事实卡输出。
func DecodeFacts(raw json.RawMessage) (ProductFacts, error) {
	return decode[ProductFacts](raw, "商品事实卡")
}

// ValidateFacts 校验项目事实卡（用户手动编辑后保存）：必填、数量、长度、source 枚举。
// 返回 stepkit.Hard 构造的错误，用 stepkit.ErrorList 可拆成逐条原因。
func ValidateFacts(f ProductFacts) error {
	f.Anchors = stringsOrEmpty(f.Anchors)
	f.Prohibited = stringsOrEmpty(f.Prohibited)
	if f.SellingPoints == nil {
		f.SellingPoints = []SellingPoint{}
	}
	return schemaCheck(factsSchema, f)
}

// validateFacts 是环节的 Validate：Schema 已检查过，这里补「没有描述就不能出现 source=description」，
// 并对卖点做极限词扫描（国内平台）。软问题的 Shot 是卖点下标（从 0 起）。
func validateFacts(out json.RawMessage, in stepkit.Input) (stepkit.Flags, error) {
	f, err := DecodeFacts(out)
	if err != nil {
		return nil, err
	}
	var reasons []string
	if blank(varString(in, "description")) {
		for i, sp := range f.SellingPoints {
			if sp.Source == SourceDescription {
				reasons = append(reasons, fmt.Sprintf("selling_points[%d] 的 source 是 description，但用户没有填写商品描述，只能写 image", i))
			}
		}
	}
	if err := stepkit.Hard(reasons...); err != nil {
		return nil, err
	}
	sc := newScanner(varPlatformKey(in), f, nil)
	var flags stepkit.Flags
	seen := map[flagKey]bool{}
	for i, sp := range f.SellingPoints {
		for _, m := range sc.forbidden(sp.Text + "\n" + sp.Proof) {
			if k := (flagKey{i, FlagForbidden, m}); !seen[k] {
				seen[k] = true
				flags = append(flags, Flag{Shot: i, Type: FlagForbidden, Text: m})
			}
		}
	}
	return flags, nil
}

// previewFacts 把事实卡输出写成人能读的摘要。
func previewFacts(out json.RawMessage) string {
	f, err := DecodeFacts(out)
	if err != nil {
		return clip(string(out), 300)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s（%s）\n", f.Name, f.Category)
	fmt.Fprintf(&b, "外观锚点：%s\n", strings.Join(f.Anchors, "；"))
	for i, sp := range f.SellingPoints {
		fmt.Fprintf(&b, "卖点 %d：%s（证明：%s；来源：%s）\n", i+1, sp.Text, sp.Proof, sp.Source)
	}
	fmt.Fprintf(&b, "禁用说法：%s\n目标人群：%s\n引导语：%s", strings.Join(f.Prohibited, "；"), f.Audience, f.CTA)
	return b.String()
}

// factsStep 返回商品事实卡环节的协议定义。
func factsStep() stepkit.Step {
	return stepkit.Step{
		Key:    StepFacts,
		Title:  "商品事实卡",
		Studio: Studio,
		Duty:   "从商品图和用户描述里提取商品事实：外观锚点、已证实卖点、禁用说法、目标人群与引导下单。只写图里看得见、描述里说过的，不编造。",
		Inputs: []stepkit.Var{
			{Name: "product_images", Type: "图片 1–4", Desc: "商品图（多模态附件）"},
			{Name: "description", Type: "string", Desc: "用户描述，可空串"},
			{Name: "platform", Type: "object", Desc: "投放平台 {key, name, style, bans}"},
			{Name: "language", Type: "zh | en", Desc: "旁白语言"},
			{Name: "image_count", Type: "number", Desc: "商品图张数"},
		},
		Media:  stepkit.MediaImages,
		Schema: factsSchema,
		Rules: stepkit.Rules{
			Hard: []string{
				"必须是合法 JSON 且字段齐全",
				fmt.Sprintf("anchors 1–%d 条，selling_points 1–%d 条，各字段不超过长度上限", factsAnchorsMax, factsPointsMax),
				"source 只能是 description 或 image",
				"用户没有填写描述时，selling_points 里不能出现 source=description",
			},
			Soft: []string{"卖点含广告法极限词（国内平台）；软问题的 shot 是卖点下标，从 0 起"},
		},
		Validate: validateFacts,
		Builtin:  factsMethod,
		Samples:  factsSamples(),
		Preview:  previewFacts,
	}
}
