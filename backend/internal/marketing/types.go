// Package marketing 是营销工作室的纯业务规则：四个环节（stepkit 的 Step）、平台预设、广告法极限词、
// 软问题扫描和环节输入的组装。不依赖数据库和 service，可以直接单元测试。
//
// 设计见 docs/design/营销工作室/营销工作室.md §6.5、§6.9、§6.10；接口形状见同目录「接口契约.md」。
// 视频提示词格式化器在子包 format（依赖本包的类型）。
package marketing

import "video-canvas/internal/stepkit"

// Studio 是营销环节在后台「环节技能」页的分组。
const Studio = "marketing"

// 四个环节的 key。
const (
	StepFacts   = "marketing.facts"   // 商品事实卡（看商品图）
	StepReverse = "marketing.reverse" // 拆解参考片（看视频）
	StepFollow  = "marketing.follow"  // 跟拍改写
	StepCreate  = "marketing.create"  // 创意脚本
)

// 环节名（接口里的 step 字段）。
const (
	NameFacts   = "facts"
	NameReverse = "reverse"
	NameFollow  = "follow"
	NameCreate  = "create"
)

// 软问题类型（flag.type）。
const (
	FlagForbidden = "forbidden" // 广告法极限词
	FlagClaim     = "claim"     // 背书无依据
	FlagBrand     = "brand"     // 第三方品牌
)

// 脚本种类与跟拍脚本的角度。
const (
	KindFollow  = "follow"
	KindCreate  = "create"
	AngleFollow = "follow"
)

// 商品在镜头里的角色（reverse.shots[].product_role）。
const (
	RoleNone        = "none"
	RoleDynamic     = "dynamic"
	RoleHeroReal    = "hero_real"
	RolePackageText = "package_text"
)

// 旁白语言。
const (
	LangZH = "zh"
	LangEN = "en"
)

// SellingPoint 是事实卡里的一个卖点：文字、画面如何证明、来源（description 用户描述 / image 图里看得见）。
type SellingPoint struct {
	Text   string `json:"text"`
	Proof  string `json:"proof"`
	Source string `json:"source"`
}

// ProductFacts 是商品事实卡（marketing.facts 的输出，也是用户可编辑的项目事实卡）。
type ProductFacts struct {
	Name          string         `json:"name"`
	Category      string         `json:"category"`
	Anchors       []string       `json:"anchors"`
	SellingPoints []SellingPoint `json:"selling_points"`
	Prohibited    []string       `json:"prohibited"`
	Audience      string         `json:"audience"`
	CTA           string         `json:"cta"`
}

// ReverseOverall 是拆解的整体判断。
type ReverseOverall struct {
	Style        string `json:"style"`
	NarrativeArc string `json:"narrative_arc"`
	WhyViral     string `json:"why_viral"`
	Transcript   string `json:"transcript"`
	Category     string `json:"category"`
}

// ReverseShot 是拆解的一个镜头。
type ReverseShot struct {
	ID             int      `json:"id"`
	Start          float64  `json:"start"`
	End            float64  `json:"end"`
	IsOpening3s    bool     `json:"is_opening_3s"`
	ShotSize       string   `json:"shot_size"`
	Camera         string   `json:"camera"`
	Subject        string   `json:"subject"`
	Action         string   `json:"action"`
	Scene          string   `json:"scene"`
	Lighting       string   `json:"lighting"`
	Person         string   `json:"person"`
	ProductInFrame string   `json:"product_in_frame"`
	ProductRole    string   `json:"product_role"`
	Dialogue       string   `json:"dialogue"`
	OnscreenText   string   `json:"onscreen_text"`
	KeyColors      []string `json:"key_colors"`
}

// Reverse 是参考片拆解（marketing.reverse 的输出）。
type Reverse struct {
	Overall ReverseOverall `json:"overall"`
	Shots   []ReverseShot  `json:"shots"`
	Brands  []string       `json:"brands"`
	Claims  []string       `json:"claims"`
}

// ScriptShot 是脚本的一个镜头。RefShot 只有跟拍脚本有：对应参考镜头的 id。
type ScriptShot struct {
	Start     float64 `json:"start"`
	End       float64 `json:"end"`
	Purpose   string  `json:"purpose"` // hook / pain / proof / cta 等，自由文本
	Shot      string  `json:"shot"`    // 景别
	Camera    string  `json:"camera"`  // 运镜
	Scene     string  `json:"scene"`   // 画面：主体 + 动作 + 场景
	Narration string  `json:"narration"`
	RefShot   *int    `json:"ref_shot,omitempty"`
}

// Script 是一份脚本。Angle 创意脚本取六个角度之一；跟拍脚本固定 AngleFollow。
type Script struct {
	Angle string       `json:"angle"`
	Title string       `json:"title"`
	Hook  string       `json:"hook"`
	Shots []ScriptShot `json:"shots"`
	Music string       `json:"music"`
	Voice string       `json:"voice"`
}

// FollowMapItem 是跟拍的一条对应：参考镜头 id（reverse.shots[].id）对应本脚本的镜头下标（从 0 起）。
type FollowMapItem struct {
	RefShot int `json:"ref_shot"`
	Shot    int `json:"shot"`
}

// FollowOutput 是 marketing.follow 的输出：脚本结构加 follow_map（不含 angle，由系统固定为 follow）。
type FollowOutput struct {
	Title     string          `json:"title"`
	Hook      string          `json:"hook"`
	Shots     []ScriptShot    `json:"shots"`
	Music     string          `json:"music"`
	Voice     string          `json:"voice"`
	FollowMap []FollowMapItem `json:"follow_map"`
}

// CreateOutput 是 marketing.create 的输出。
type CreateOutput struct {
	Scripts []Script `json:"scripts"`
}

// Flag 是脚本的一条软问题，Shot 是从 0 起的镜头下标。
type Flag = stepkit.Flag

// Brief 是四个环节共用的轮次设置摘要。
type Brief struct {
	Platform    string // 平台 key（douyin 等），不认识按 generic
	Language    string // zh / en
	Duration    int    // 成片时长（秒）
	AspectRatio string // 画幅，如 9:16
	Description string // 用户描述，可空
}
