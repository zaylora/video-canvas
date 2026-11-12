// Package stepkit 是「环节协议层」：把一次文本模型调用拆成固定的协议和可替换的方法。
//
// 三层分工（设计见 docs/design/营销工作室/营销工作室.md §6.9）：
//   - 协议层（本包里的 Step）：环节 key、本步职责、输入变量、媒体需求、输出 JSON Schema、校验函数、方法正文上限。由代码维护，技能改不了；
//   - 方法层（Method）：「怎么想、怎么写」，内置方法正文，或后台绑定的导入技能（钉死版本）；
//   - 格式化层：结构化输出到视频模型提示词的转换，由各业务包自己实现，不在本包。
//
// 本包不依赖任何业务（营销、短剧）。业务包定义自己的 Step 并注册到 Registry，调用时用 Resolver 取当前生效的方法，
// 再交给 Runner 拼提示词、调模型、解码、校验、必要时重写一次。
//
// 文件分工：step.go 类型与错误；registry.go 注册表；method.go 方法与绑定解析；prompt.go 提示词拼接；
// schema.go Schema 字段说明与输出检查；run.go 执行与多模态；gateway.go 经 LLM 网关调用模型。
package stepkit

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"video-canvas/internal/llmgateway"
)

// DefaultMethodMax 是方法正文的默认上限（字符数），Step.MethodMax 为 0 时使用。
const DefaultMethodMax = 16000

// MediaNeed 是环节对输入媒体的需求。
type MediaNeed string

// 媒体需求取值。
const (
	MediaNone   MediaNeed = "none"   // 只有文字
	MediaImages MediaNeed = "images" // 需要看图（模型要有 Vision）
	MediaVideo  MediaNeed = "video"  // 优先直接看视频（模型要有 Video），退回 Vision + 调用方准备好的截帧
)

// Valid 判断取值是否是已知的媒体需求。
func (m MediaNeed) Valid() bool {
	return m == MediaNone || m == MediaImages || m == MediaVideo
}

// 方法来源。
const (
	SourceBuiltin = "builtin" // 内置方法正文，随代码发布
	SourceSkill   = "skill"   // 后台绑定的导入技能（名字 + 版本）
)

// Var 是一个输入变量的声明（给后台「协议」标签展示用，不参与运行时校验）。
type Var struct {
	Name string `json:"name"` // 变量名，对应 Input.Vars 的键
	Type string `json:"type"` // 类型说明，如 string / number / object / array
	Desc string `json:"desc"` // 含义
}

// Rules 是校验规则的文字说明：硬错误会触发重写或失败，软问题只在界面标红。
// 它同时用于提示词的「校验规则」块和后台的协议详情；规则本身的判定在 Step.Validate 里。
type Rules struct {
	Hard []string `json:"hard"` // 硬错误：输出不合格，需要重写
	Soft []string `json:"soft"` // 软问题：输出可用，但要提醒
}

// Input 是一次运行的输入。
type Input struct {
	// Vars 是输入变量，编码成 JSON 放进提示词的「输入」块。
	Vars map[string]any
	// Images 是图片地址（可以是 data URL），环节需要看图时作为多模态 Part 附在同一条消息里。
	// 视频环节在模型不支持直接看视频时，调用方把截帧放在这里（具体降级由调用方准备好）。
	Images []string
	// Video 是视频输入；模型支持 Capabilities.Video 时直接附上。
	Video *llmgateway.Video
}

// Flag 是一条软问题：落库后在界面标红，不阻断。
type Flag struct {
	Shot int    `json:"shot"` // 出问题的镜头下标（从 0 起，对应脚本 shots 数组）
	Type string `json:"type"` // 问题类型，由业务定义（营销用 forbidden / claim / brand）
	Text string `json:"text"` // 命中的原文或说明
}

// Flags 是软问题列表。
type Flags []Flag

// HardErrors 是校验函数返回的硬错误列表，实现 error。Validate 返回它时 Run 会带上这些原因重写一次。
type HardErrors []string

// Error 实现 error，把全部原因用分号连起来。
func (e HardErrors) Error() string { return strings.Join(e, "；") }

// Hard 把若干硬错误原因包成 error；没有原因时返回 nil，方便校验函数收集后一次返回。
func Hard(reasons ...string) error {
	var out HardErrors
	for _, r := range reasons {
		if strings.TrimSpace(r) != "" {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ErrorList 把校验返回的 error 拆成原因列表：HardErrors 逐条返回，其他错误整体作为一条。
func ErrorList(err error) []string {
	if err == nil {
		return nil
	}
	var he HardErrors
	if errors.As(err, &he) {
		return append([]string(nil), he...)
	}
	return []string{err.Error()}
}

// Sample 是后台试跑的样例：随代码发布，素材打包进二进制；依赖上游环节的样例自带上游输出。
type Sample struct {
	Key      string                                   // 样例 key，如 perfume
	Name     string                                   // 样例名，如「晨雾香水 · 抖音 · 无参考」
	NeedsRef bool                                     // 是否带参考视频
	Build    func(ctx context.Context) (Input, error) // 构造本次试跑的输入
}

// Step 是一个环节的协议定义，注册后不可变。
type Step struct {
	Key    string    // 环节 key，如 marketing.facts；全局唯一
	Title  string    // 显示名，如「商品事实卡」
	Studio string    // 所属工作室，如 marketing、drama，后台按它分组
	Duty   string    // 本步职责，1 到 3 句，固定
	Inputs []Var     // 输入变量声明
	Media  MediaNeed // 媒体需求
	// Schema 是输出 JSON Schema（固定）。它既生成提示词里的字段说明，也由协议层做结构检查，
	// 所以任何方法（内置或技能）产出的内容都必须过同一份 Schema。
	Schema json.RawMessage
	// Validate 校验已通过 Schema 检查的输出：返回的 error 是硬错误（Hard 构造，触发重写，仍不合格则失败），
	// Flags 是软问题（落库后标红）。
	Validate func(out json.RawMessage, in Input) (Flags, error)
	Rules    Rules  // 校验规则的文字说明，同时写进提示词和后台详情
	Builtin  string // 内置方法正文（可被技能替换的那一块）
	// MethodMax 是方法正文上限（字符数），0 表示 DefaultMethodMax。
	MethodMax int
	Samples   []Sample // 后台试跑样例
	// Preview 把输出变成人能读的摘要（后台试跑结果用）；为空时用输出 JSON 的前若干字。
	Preview func(out json.RawMessage) string
}

// EffectiveMethodMax 返回方法正文的实际上限。
func (s Step) EffectiveMethodMax() int {
	if s.MethodMax > 0 {
		return s.MethodMax
	}
	return DefaultMethodMax
}

// Sample 按 key 取样例。
func (s Step) Sample(key string) (Sample, bool) {
	for _, sm := range s.Samples {
		if sm.Key == key {
			return sm, true
		}
	}
	return Sample{}, false
}
