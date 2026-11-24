package mcp

import (
	"encoding/json"
	"sort"
	"strings"

	"video-canvas/internal/model"
	agentsvc "video-canvas/internal/service/agent"
)

// CanvasListToolName 是网关自带的工具名。
//
// 工具登记表里的工具都作用在「会话绑定的画布」上；外部接入没有会话，
// 所以必须在调画布类工具前先能发现「这个账号有哪些画布」，否则模型根本不知道该往 canvas 里填什么。
const CanvasListToolName = "canvas_list"

// canvasListTool 是唯一不由工具登记表派生的工具：它只查画布目录，没有任何副作用。
var canvasListTool = tool{
	Name: CanvasListToolName,
	Description: "列出这个账号名下的画布：code、标题、修订号、更新时间。外部接入的画布类工具都要在调用时用 canvas 参数填画布 code，" +
		"这里就是查 code 的地方。可以按标题关键字过滤，默认按更新时间倒序。" +
		"短剧项目不在这个列表里，要用 drama_list 查。",
	InputSchema: json.RawMessage(`{"type":"object","properties":{"keyword":{"type":"string","description":"按标题模糊过滤，可空"},"limit":{"type":"integer","description":"最多返回多少条，默认 50，上限 200"}}}`),
}

// DramaListToolName 是网关自带的短剧目录工具。
//
// 短剧也是画布，但网页端把两者分开展示（画布列表刻意排除 drama_id 不为空的），
// 所以 canvas_list 里看不到短剧画布。没有这个工具，外部工具永远不知道该往 drama_* 的 canvas 参数里填什么。
const DramaListToolName = "drama_list"

// dramaListTool 也只查目录，没有副作用。
var dramaListTool = tool{
	Name: DramaListToolName,
	Description: "列出这个账号名下的短剧项目：canvas（短剧画布的 code，drama_* 工具要用它）、标题、状态、阶段、目标集数。" +
		"短剧不在 canvas_list 里，要改剧本或跑任务先来这里拿 canvas。可以按标题过滤，默认按更新时间倒序。",
	InputSchema: json.RawMessage(`{"type":"object","properties":{"keyword":{"type":"string","description":"按标题模糊过滤，可空"},"limit":{"type":"integer","description":"最多返回多少条，默认 50，上限 200"}}}`),
}

// toolScopes 是开放给外部接入的能力与它需要的权限档位。
//
// 只登记「外部真的该用」的能力。没登记的一律不开放——新增工具时如果忘了分类，
// 覆盖测试会红（见 tools_test.go），不会无声无息地泄露出去。
var toolScopes = map[string]string{
	// 浏览目录：列画布、列模型、搜技能、读技能、查任务，全部无副作用
	CanvasListToolName: model.MCPScopeCanvasBrowse,
	"model_list":       model.MCPScopeCanvasBrowse,
	"skill_search":     model.MCPScopeCanvasBrowse,
	"skill_read":       model.MCPScopeCanvasBrowse,
	"task_get":         model.MCPScopeCanvasBrowse,
	// 读画布结构（同时是改画布的前置）
	"canvas_get_state": model.MCPScopeCanvasRead,
	// 查看素材
	"canvas_inspect_image": model.MCPScopeCanvasMedia,
	"canvas_inspect_video": model.MCPScopeCanvasMedia,
	"canvas_capture_frame": model.MCPScopeCanvasMedia,
	// 改画布
	"canvas_apply_ops": model.MCPScopeCanvasWrite,
	// 重排布局
	"canvas_arrange": model.MCPScopeCanvasLayout,
	// 合成成片（消耗算力，不消耗积分）
	"canvas_compose": model.MCPScopeCanvasCompose,
	// 生成素材：出图、出视频、出配音。花归属用户的积分，所以单独一档，默认不给
	"generate_media": model.MCPScopeCanvasGenerate,
	// 短剧是独立能力域：权限档用 drama:read / drama:write / drama:run，不沾 canvas: 前缀。
	// 短剧画布不在 canvas_list 里（网页端有意分开），要让外部工具能读到就得给它读短剧的权限。
	DramaListToolName:      model.MCPScopeDramaRead,
	"drama_read":           model.MCPScopeDramaRead,
	"drama_update_outline": model.MCPScopeDramaWrite,
	"drama_update_episode": model.MCPScopeDramaWrite,
	"drama_update_segment": model.MCPScopeDramaWrite,
	// 跑短剧任务：花归属用户的积分（compose 一步免费），与生成素材同级敏感
	"drama_run_job": model.MCPScopeDramaRun,
}

// deniedTools 是明确不开放的工具与原因。写在这里有两个作用：覆盖测试能说清「为什么不开放」，
// 以及以后有人想把它们加进 toolScopes 时先看到原因。
var deniedTools = map[string]string{ //nolint:gosec // 表里是工具名（含 admin_request_secret），不是凭证
	// 要经人工审批才生效：一期不打通审批链路，一律不开放，避免外部接入绕过审批花钱或删东西
	"canvas_delete":              "需要人工审批",
	"plan_update":                "只在 Agent 服务内维护计划",
	"ask_user":                   "只在内置对话里提问",
	"show_card":                  "只在内置对话里展示卡片",
	"admin_propose_changes":      "后台运营能力，需人工审批",
	"admin_request_secret":       "后台运营能力，需人工审批",
	"admin_test_run":             "后台运营能力，需人工审批",
	"web_search":                 "外部工具自带联网能力，不重复提供",
	"web_fetch":                  "外部工具自带联网能力，不重复提供",
	"admin_list_models":          "后台运营能力",
	"admin_get_model":            "后台运营能力",
	"admin_list_channels":        "后台运营能力",
	"admin_get_pricing_settings": "后台运营能力",
	"admin_quote_preview":        "后台运营能力",
	"admin_list_plugins":         "后台运营能力",
	"admin_get_plugin":           "后台运营能力",
	"admin_plugin_check":         "后台运营能力",
	"admin_get_recent_errors":    "后台运营能力",
	"admin_query":                "后台运营能力",
	"admin_get_resource":         "后台运营能力",
}

// tool 是对外暴露的一个工具：MCP 只要 name / description / inputSchema。
type tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// catalog 返回这个身份能看到的工具：先取「全能创作」模式下可用的声明（后台运营与短剧片场天然被模式过滤掉），
// 再与 toolScopes 求交，最后按权限档位过滤。
//
// 画布类工具的 schema 会额外注入 canvas 参数：工具登记表里它们作用在会话绑定的画布上，
// 外部接入没有会话，必须每次在参数里指明画布。
func catalog(scopes map[string]bool) []tool {
	decls := agentsvc.AgentToolDecls(model.AgentModeAll, true, true)
	// 短剧工具只登记在 drama 模式下，全能创作那份声明里没有它们：不取一次，勾了短剧也看不到工具。
	// 两份都来自同一张登记表并按 name 去重，不会因为合并多出别的模式专属能力。
	decls = append(decls, agentsvc.AgentToolDecls(model.AgentModeDrama, true, true)...)
	out := make([]tool, 0, len(toolScopes))
	seen := make(map[string]bool, len(decls))
	for i := range decls {
		d := &decls[i]
		if seen[d.Name] {
			continue
		}
		scope, open := toolScopes[d.Name]
		if !open || !scopes[scope] {
			continue
		}
		seen[d.Name] = true
		schema := d.Parameters
		if CanvasNeeded(d.Name) {
			schema = withCanvasParam(schema)
		}
		if d.Name == "generate_media" {
			// wait 只对内置流程有意义（外部接入是提交即返回），留着只会让模型以为一次调用能等到结果。
			schema = withoutParams(schema, "wait")
		}
		out = append(out, tool{Name: d.Name, Description: externalDescription(d.Name, d.Description), InputSchema: schema})
	}
	// canvas_list 不在工具登记表里：只要有任一画布权限就应该能查画布 code。
	if CanvasListVisible(scopes) {
		out = append(out, canvasListTool)
	}
	// drama_list 同理：短剧画布不在 canvas_list 里，勾了读短剧就必须能查它的 code。
	if scopes[model.MCPScopeDramaRead] {
		out = append(out, dramaListTool)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// CanvasNeeded 判断一个工具是不是必须落在某块画布上：画布类工具都要，模型 / 技能 / 任务类不要。
// generate_media 名字不带 canvas_ 前缀，但它作用在具体节点上，同样要 canvas；
// drama_* 作用在短剧画布上，也一样。
func CanvasNeeded(name string) bool {
	return name == "generate_media" || strings.HasPrefix(name, "drama_") ||
		(name != CanvasListToolName && name != DramaListToolName && strings.HasPrefix(name, "canvas_"))
}

// CanvasListVisible 判断是否给这个身份看 canvas_list。
// 归一化后任何画布档位都会带上 canvas:browse，所以判它一个就够；
// 换成直接判 browse，就不再是「有写权限却查不到画布 code」。
func CanvasListVisible(scopes map[string]bool) bool {
	return scopes[model.MCPScopeCanvasBrowse]
}

// externalDescriptions 覆盖少数工具给外部接入看的说明。
//
// 工具声明表里的话是写给内置对话的，直接照抄会误导外部模型：
// generate_media 在内置流程里可以「等这批任务全跑完再继续」，外部接入是提交即返回，
// 让模型先规划「一次调用等结果」它就会白等（还会撞上单次调用超时）。
var externalDescriptions = map[string]string{
	"generate_media": "出图 / 出视频 / 出配音，作用在画布节点上。花归属用户自己的积分。" +
		"提交后立刻返回任务号，不等结果；之后用 task_get（taskId 传十进制字符串）查进度，" +
		"或者用 canvas_get_state 看节点状态。",
	// 短剧这一族在内置对话里每步都要点「批准」，外部接入没有那张卡：勾了对应权限就直接执行。
	// 说明里必须写清这一点，否则模型会照着内置话术说「已提交审批」，用户干等。
	"drama_read": "读短剧：overview 是项目、大纲、人物、各集摘要和进行中的任务；episode 是单集全文，" +
		"segments 是某集的片段列表。canvas 传 drama_list 返回的短剧画布 code。",
	"drama_update_outline": "直接改写短剧大纲或人物传记（外部接入没有审批卡，勾了「短剧剧本」就立即生效）。" +
		"只传要改的字段；覆盖前先用 drama_read 读一遍 overview。版本冲突会失败，重读后再改。",
	"drama_update_episode": "直接改写某一集的标题、梗概或剧本（立即生效，不经审批）。已锁定的集只能改标题，" +
		"改剧本或梗概会被拒绝；先 drama_read 读该集，再按读到的 revision 提交。",
	"drama_update_segment": "直接改写某个片段的提示词或秒数（立即生效，不经审批）。" +
		"segment_id 用 drama_read 的 segments 返回的十六进制 ID。",
	"drama_run_job": "提交短剧任务：scripts 分集剧本、breakdown 资产拆解、asset_prompts 资产提示词、" +
		"storyboard 分镜、compose 合成整集（compose 不扣积分）。**会花归属用户的积分**，提交前先想清楚范围。" +
		"提交后立刻返回任务，不等跑完；用 drama_read 的 overview 看 active_jobs 进度。" +
		"同一短剧同时只能有一个进行中的任务，撞上会被拒绝，等前一个跑完再提交。",
}

// externalDescription 取外部接入该看到的工具说明：有覆盖用覆盖，没有就原样用声明表里的。
func externalDescription(name, fallback string) string {
	if text, ok := externalDescriptions[name]; ok {
		return text
	}
	return fallback
}

// withCanvasParam 往工具入参 schema 里加 canvas 字段。
// 解析失败就原样返回：宁可少一个提示，也不能让工具从清单里消失。
func withCanvasParam(raw json.RawMessage) json.RawMessage {
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return raw
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		props = map[string]any{}
		schema["properties"] = props
	}
	props["canvas"] = map[string]any{
		"type": "string",
		"description": "画布 code（32 位十六进制，用 canvas_list 查）。留空则用项目配的默认画布；" +
			"两种情况都没有时这次调用会被拒绝。",
	}
	b, err := json.Marshal(schema)
	if err != nil {
		return raw
	}
	return b
}

// withoutParams 从入参 schema 里摘掉对这条通道没有意义的字段。
func withoutParams(raw json.RawMessage, names ...string) json.RawMessage {
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return raw
	}
	if props, ok := schema["properties"].(map[string]any); ok {
		for _, n := range names {
			delete(props, n)
		}
	}
	b, err := json.Marshal(schema)
	if err != nil {
		return raw
	}
	return b
}

// ScopeFor 返回这个工具需要的权限档位；不开放的工具返回 false。
func ScopeFor(name string) (string, bool) {
	scope, open := toolScopes[name]
	return scope, open
}

// DenialReason 返回一个工具不开放的原因；开放的工具返回 false。后台与测试用来说明「为什么没有这项能力」。
func DenialReason(name string) (string, bool) {
	reason, denied := deniedTools[name]
	return reason, denied
}
