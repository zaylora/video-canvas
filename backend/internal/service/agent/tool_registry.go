package agent

import (
	"context"
	_ "embed"
	"encoding/json"
	"slices"
	"sync"

	"video-canvas/internal/model"
)

// 工具的执行方。
const (
	ToolExecServer = "server" // Go 执行：Agent 经桥的 /tool 回调
)

// toolDeclsJSON 是全部工具的说明和参数 JSON Schema（name、description、parameters），顺序无关紧要，
// 登记顺序以 toolSpecs 为准。说明和参数是给模型看的，改动会影响模型行为和提示词缓存，改前想清楚。
//
//go:embed tool_decls.json
var toolDeclsJSON []byte

// ToolDecl 是下发给 Agent 运行时的一个工具声明（启动参数里的 tools）。
type ToolDecl struct {
	Name        string          `json:"name"`
	Label       string          `json:"label"`       // 界面名称，也作为 pi 工具的 label
	Description string          `json:"description"` // 给模型看的说明
	Parameters  json.RawMessage `json:"parameters"`  // 参数的 JSON Schema
	Executor    string          `json:"executor"`    // 执行方，见 ToolExec*
	Scriptable  bool            `json:"scriptable"`  // 能否被 codemode 脚本调用：会创建审批并终止本轮的、会返回图片的工具不行
}

// toolSpec 是一个工具的登记项：规则和处理函数。说明和参数在 tool_decls.json。
type toolSpec struct {
	name        string
	label, verb string   // 界面名称和短动词（活动块收起后的摘要按动词计数）
	scriptable  bool     // 见 ToolDecl.Scriptable
	modes       []string // 能用的任务模式，nil 表示所有模式
	needsVision bool     // 只有能看图的模型才有
	run         func(b *AgentBridge, ctx context.Context, tc *toolCall) (*toolOut, error)
}

// toolSpecs 是全部工具，登记顺序就是下发给模型的顺序：固定下来，提示词缓存才稳定；新工具追加在末尾。
// 任务模式的限制是 Go 端强制的，不依赖提示词约束；改画布的工具所有模式都能用，但能做哪些操作由 modeAllowsOps 再限制。
var toolSpecs = []toolSpec{
	{name: "canvas_get_state", label: "读取画布", verb: "读取", scriptable: true, run: (*AgentBridge).toolGetState},
	{name: "canvas_apply_ops", label: "修改画布", verb: "修改", scriptable: true, run: (*AgentBridge).toolApplyOps},
	{name: "canvas_arrange", label: "整理布局", verb: "整理", scriptable: true, modes: layoutModes, run: (*AgentBridge).toolArrange},
	{name: "canvas_delete", label: "申请删除", verb: "删除", modes: layoutModes, run: (*AgentBridge).toolDelete},
	{name: "plan_update", label: "更新计划", verb: "计划", scriptable: true, run: (*AgentBridge).toolPlan},
	{name: "ask_user", label: "向你提问", verb: "提问", run: (*AgentBridge).toolAsk},
	{name: "model_list", label: "查看可用模型", verb: "查模型", scriptable: true, run: (*AgentBridge).toolModelList},
	{name: "generate_media", label: "申请生成", verb: "生成", modes: layoutModes, run: (*AgentBridge).toolGenerate},
	{name: "task_get", label: "查询任务", verb: "查任务", scriptable: true, run: (*AgentBridge).toolTaskGet},
	{name: "skill_search", label: "搜索技能", verb: "技能", scriptable: true, run: (*AgentBridge).toolSkillSearch},
	{name: "skill_read", label: "读取技能", verb: "技能", scriptable: true, run: (*AgentBridge).toolSkillRead},
	{name: "canvas_inspect_image", label: "查看图片", verb: "看图", needsVision: true, run: (*AgentBridge).toolInspectImage},
	// 联网：所有模式都能用；结果只是资料，可以被脚本调用。是否真的下发由运行时的停用清单决定（RuntimeConfig.DisabledTools）
	{name: "web_search", label: "搜索网页", verb: "搜索", scriptable: true, run: (*AgentBridge).toolWebSearch},
	{name: "web_fetch", label: "读取网页", verb: "读网页", scriptable: true, run: (*AgentBridge).toolWebFetch},
}

// layoutModes 是能排列、删除和生成的任务模式：全能创作和分镜搭建。
var layoutModes = []string{model.AgentModeAll, model.AgentModeStoryboard}

// allowsMode 判断任务模式能不能用这个工具；空模式按全能创作算。
func (s *toolSpec) allowsMode(mode string) bool {
	if s.modes == nil {
		return true
	}
	if mode == "" {
		mode = model.AgentModeAll
	}
	return slices.Contains(s.modes, mode)
}

// findTool 按名字找登记项，没有返回 nil。
func findTool(name string) *toolSpec {
	for i := range toolSpecs {
		if toolSpecs[i].name == name {
			return &toolSpecs[i]
		}
	}
	return nil
}

// ToolLabel 返回工具的界面名称和短动词；不认识的工具返回空串。
func ToolLabel(name string) (label, verb string) {
	if s := findTool(name); s != nil {
		return s.label, s.verb
	}
	return "", ""
}

// withToolLabel 把界面名称和动词补进 tool.start / tool.end 事件，前端优先用它，加后端工具时前端不用改。
func withToolLabel(ev map[string]any, name string) map[string]any {
	if label, verb := ToolLabel(name); label != "" {
		ev["label"], ev["verb"] = label, verb
	}
	return ev
}

type baseDecl struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// baseDecls 解析内嵌的说明和参数。登记项和 JSON 对不上是编码错误，启动时就该暴露，不等到运行时。
var baseDecls = sync.OnceValue(func() map[string]baseDecl {
	var list []baseDecl
	if err := json.Unmarshal(toolDeclsJSON, &list); err != nil {
		panic("agent: tool_decls.json 不是合法 JSON: " + err.Error())
	}
	out := make(map[string]baseDecl, len(list))
	for _, d := range list {
		out[d.Name] = d
	}
	for _, s := range toolSpecs {
		if _, ok := out[s.name]; !ok {
			panic("agent: tool_decls.json 里没有工具 " + s.name)
		}
	}
	return out
})

// AgentToolDecls 返回某个任务模式、某个模型能用的工具声明，运行时只把这些交给模型：
// 不能用的工具连看都看不到，比调用后被拒绝更省事，也更不容易被诱导。Go 端在执行时仍会再校验一次。
func AgentToolDecls(mode string, vision bool) []ToolDecl {
	decls := baseDecls()
	var out []ToolDecl
	for i := range toolSpecs {
		s := &toolSpecs[i]
		if !s.allowsMode(mode) || (s.needsVision && !vision) {
			continue
		}
		d := decls[s.name]
		out = append(out, ToolDecl{Name: s.name, Label: s.label, Description: d.Description, Parameters: d.Parameters, Executor: ToolExecServer, Scriptable: s.scriptable})
	}
	return out
}

// AgentToolsForMode 返回某个任务模式能用的工具名（不考虑模型能不能看图）。
func AgentToolsForMode(mode string) []string {
	var out []string
	for i := range toolSpecs {
		if toolSpecs[i].allowsMode(mode) {
			out = append(out, toolSpecs[i].name)
		}
	}
	return out
}

// AgentToolsFor 返回某个任务模式、某个模型能用的工具名：不能看图的模型拿不到 canvas_inspect_image。
func AgentToolsFor(mode string, vision bool) []string {
	var out []string
	for _, d := range AgentToolDecls(mode, vision) {
		out = append(out, d.Name)
	}
	return out
}
