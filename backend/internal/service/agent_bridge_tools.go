package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"go.uber.org/zap"

	"video-canvas/internal/canvasgraph"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/provider"
)

// 工具参数的限制。
const (
	maxPlanSteps     = 12 // 计划最多几步
	maxPlanTitleLen  = 80 // 计划每步标题最多几个字
	maxQuestionLen   = 200
	maxToolErrorSize = 1500 // 回给模型的工具错误最多几个字，免得一个超长报错占掉上下文
)

// ToolResult 是一次工具调用的结果，Node 里的工具原样转给 pi。
type ToolResult struct {
	Content   string `json:"content"`   // 给模型看的文字
	IsError   bool   `json:"is_error"`  // 工具没做成，模型会看到并自行处理
	Terminate bool   `json:"terminate"` // 本批工具执行完后停下，不再请求模型（等审批、等回答、步数用尽）
}

// toolFailure 是工具层的失败：回给模型的是说明文字，不是 HTTP 错误。
type toolFailure struct{ msg string }

func (e *toolFailure) Error() string { return e.msg }

func fail(format string, a ...any) error { return &toolFailure{msg: fmt.Sprintf(format, a...)} }

// toolOut 是一个工具处理函数的产出。
type toolOut struct {
	content   string
	summary   string // 记进 tool.end 事件，前端工具行显示它
	terminate bool
	extra     map[string]any // 一并记进 tool.end 的附加信息，如被改动的节点
}

// toolCall 是一次工具调用的上下文。
type toolCall struct {
	run  *model.AgentRun
	sess *bridgeSession
	id   string
	args json.RawMessage
}

// ExecuteTool 执行 Node 回调的一次工具调用。工具层的失败（参数不对、校验不过、模式不允许）作为 IsError 的结果返回，
// 让模型看到并自己改正；只有令牌无效、运行不在 running 这类「这次回调本身就不该发生」的情况才返回 error。
func (b *AgentBridge) ExecuteTool(ctx context.Context, token, toolCallID, name string, args json.RawMessage) (*ToolResult, error) {
	sess, err := b.session(token)
	if err != nil {
		return nil, err
	}
	run, err := b.activeRun(ctx, sess)
	if err != nil {
		return nil, err
	}
	// 1. 步数上限：用到头就暂停运行并让循环停下，用户可以追加后继续
	if run.Steps >= run.MaxSteps {
		b.pauseRun(ctx, run, model.RunStepLimit)
		return &ToolResult{Content: "已达到本轮的步数上限，运行已暂停，等待用户继续。", IsError: true, Terminate: true}, nil
	}
	// 2. 当前任务模式不允许的工具，直接告诉模型；工具名认不得的也一样
	h, known := b.handlers()[name]
	if !known {
		return b.toolError(ctx, run, toolCallID, name, "未知的工具："+name), nil
	}
	if !modeAllowsTool(sess.mode, name) {
		return b.toolError(ctx, run, toolCallID, name, fmt.Sprintf("当前是「%s」模式，不能使用 %s。", modeLabel(sess.mode), name)), nil
	}
	// 3. 执行：先记 tool.start，结束后记 tool.end，并把这次调用计入步数
	b.d.Agent.emit(ctx, run.UserID, run.SessionID, run.ID, run.CanvasID, "tool.start", map[string]any{"id": toolCallID, "name": name})
	out, herr := h(ctx, &toolCall{run: run, sess: sess, id: toolCallID, args: args})
	if err := b.d.Repo.AddRunUsage(ctx, run.ID, 1, 0); err != nil {
		logger.Warn("记录 Agent 步数失败", zap.Error(err), zap.Uint64("run_id", run.ID))
	}
	res := b.finishTool(out, herr)
	end := map[string]any{"id": toolCallID, "name": name, "is_error": res.IsError, "summary": firstNonEmpty(outSummary(out), summarizeError(res))}
	if out != nil {
		for k, v := range out.extra {
			end[k] = v
		}
	}
	b.d.Agent.emit(ctx, run.UserID, run.SessionID, run.ID, run.CanvasID, "tool.end", end)
	return res, nil
}

// toolError 为被拒绝的调用（未知工具、模式不允许）记一对事件并返回工具错误。
func (b *AgentBridge) toolError(ctx context.Context, run *model.AgentRun, id, name, msg string) *ToolResult {
	b.d.Agent.emit(ctx, run.UserID, run.SessionID, run.ID, run.CanvasID, "tool.end", map[string]any{"id": id, "name": name, "is_error": true, "summary": msg})
	return &ToolResult{Content: msg, IsError: true}
}

// finishTool 把处理函数的结果转成对模型的回复：toolFailure 是普通工具错误；
// 画布校验错误列出每一项问题；其余基础设施错误只告诉模型「内部错误」，细节记日志，不外泄。
func (b *AgentBridge) finishTool(out *toolOut, err error) *ToolResult {
	if err == nil {
		return &ToolResult{Content: out.content, Terminate: out.terminate}
	}
	var tf *toolFailure
	var ve *canvasgraph.ValidationError
	switch {
	case errors.As(err, &tf):
		return &ToolResult{Content: truncateRunes(tf.msg, maxToolErrorSize), IsError: true}
	case errors.As(err, &ve):
		return &ToolResult{Content: "操作没有生效：" + truncateRunes(ve.Error(), maxToolErrorSize), IsError: true}
	case errors.Is(err, canvasgraph.ErrInvalid):
		return &ToolResult{Content: truncateRunes(err.Error(), maxToolErrorSize), IsError: true}
	}
	var ec *errcode.Error
	if errors.As(err, &ec) {
		return &ToolResult{Content: ec.Msg, IsError: true}
	}
	logger.Error("Agent 工具执行失败", zap.Error(err))
	return &ToolResult{Content: "工具执行时出现内部错误，请稍后重试。", IsError: true}
}

func outSummary(o *toolOut) string {
	if o == nil {
		return ""
	}
	return o.summary
}

func summarizeError(r *ToolResult) string {
	if r.IsError {
		return truncateRunes(r.Content, 120)
	}
	return ""
}

type toolHandler func(ctx context.Context, tc *toolCall) (*toolOut, error)

// handlers 是工具名到处理函数的映射。
func (b *AgentBridge) handlers() map[string]toolHandler {
	return map[string]toolHandler{
		"canvas_get_state": b.toolGetState,
		"canvas_apply_ops": b.toolApplyOps,
		"canvas_arrange":   b.toolArrange,
		"canvas_delete":    b.toolDelete,
		"plan_update":      b.toolPlan,
		"ask_user":         b.toolAsk,
		"model_list":       b.toolModelList,
	}
}

// toolOrder 是全部工具名，给模型的工具顺序固定下来，提示词缓存才稳定。
var toolOrder = []string{"canvas_get_state", "canvas_apply_ops", "canvas_arrange", "canvas_delete", "plan_update", "ask_user", "model_list"}

// AgentToolsForMode 返回某个任务模式能用的工具名，运行时只把这些声明给模型：
// 不能用的工具连看都看不到，比调用后被拒绝更省事，也更不容易被诱导。Go 端在执行时仍会再校验一次。
func AgentToolsForMode(mode string) []string {
	var out []string
	for _, t := range toolOrder {
		if modeAllowsTool(mode, t) {
			out = append(out, t)
		}
	}
	return out
}

// modeLabel 是任务模式的中文名。
func modeLabel(mode string) string {
	switch mode {
	case model.AgentModeScript:
		return "剧本创编"
	case model.AgentModeStoryboard:
		return "分镜搭建"
	case model.AgentModePrompt:
		return "提示词优化"
	}
	return "全能创作"
}

// modeAllowsTool 判断任务模式能不能用某个工具：读画布、选模型、计划、提问所有模式都能用；
// 排列和删除只有全能创作和分镜搭建能用；改画布的工具所有模式都能用，但能做哪些操作由 modeAllowsOps 再限制。
// 这是 Go 端强制的，不依赖提示词约束。
func modeAllowsTool(mode, tool string) bool {
	switch tool {
	case "canvas_arrange", "canvas_delete":
		return mode == "" || mode == model.AgentModeAll || mode == model.AgentModeStoryboard
	}
	return true
}

// modeAllowsOps 判断任务模式能不能做这批画布操作。剧本创编只能新建和修改文本节点，提示词优化只能改已有节点的提示词。
// 已知的限制：剧本创编里 update_node 改的是不是文本节点这里没有查（要读画布），只限制了操作种类。
func modeAllowsOps(mode string, ops []canvasgraph.Op) error {
	for _, op := range ops {
		switch mode {
		case model.AgentModeScript:
			ok := (op.Op == canvasgraph.OpCreateNode && op.Kind == canvasgraph.KindScript) || op.Op == canvasgraph.OpUpdateNode
			if !ok {
				return fail("当前是「剧本创编」模式，只能新建和修改文本节点，不能做 %s。", op.Op)
			}
		case model.AgentModePrompt:
			if op.Op != canvasgraph.OpUpdateNode || op.Label != nil || op.Model != nil {
				return fail("当前是「提示词优化」模式，只能修改已有节点的提示词，不能做 %s。", op.Op)
			}
		}
	}
	return nil
}

// jsonOut 把结果序列化成给模型看的 JSON 文本。
func jsonOut(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}

// toolGetState 读画布：不传参数返回目录（选中的节点排最前）；传 nodeIds 或 groupId 返回节点详情。
func (b *AgentBridge) toolGetState(ctx context.Context, tc *toolCall) (*toolOut, error) {
	var a struct {
		NodeIDs []string `json:"nodeIds"`
		GroupID string   `json:"groupId"`
	}
	if err := json.Unmarshal(tc.args, &a); err != nil {
		return nil, fail("参数格式不对：%v", err)
	}
	ids := a.NodeIDs
	if a.GroupID != "" {
		cat, err := b.d.Canvas.Catalog(ctx, tc.run, canvasgraph.CatalogOptions{Max: 1000})
		if err != nil {
			return nil, err
		}
		for _, n := range cat.Nodes {
			if n.Group == a.GroupID {
				ids = append(ids, n.ID)
			}
		}
		if len(ids) == 0 {
			return nil, fail("组 %q 不存在或里面没有节点", a.GroupID)
		}
	}
	if len(ids) == 0 {
		cat, err := b.d.Canvas.Catalog(ctx, tc.run, canvasgraph.CatalogOptions{Priority: tc.sess.selection})
		if err != nil {
			return nil, err
		}
		content, err := jsonOut(cat)
		return &toolOut{content: content, summary: fmt.Sprintf("读取画布目录（%d 个节点）", cat.Total)}, err
	}
	d, err := b.d.Canvas.Detail(ctx, tc.run, ids)
	if err != nil {
		return nil, err
	}
	content, err := jsonOut(d)
	return &toolOut{content: content, summary: fmt.Sprintf("读取 %d 个节点", len(d.Nodes)), extra: map[string]any{"node_ids": ids}}, err
}

// toolApplyOps 应用一批编辑操作。
func (b *AgentBridge) toolApplyOps(ctx context.Context, tc *toolCall) (*toolOut, error) {
	var a struct {
		Ops json.RawMessage `json:"ops"`
	}
	if err := json.Unmarshal(tc.args, &a); err != nil || len(a.Ops) == 0 {
		return nil, fail("参数里需要 ops 数组")
	}
	ops, err := canvasgraph.ParseOps(a.Ops)
	if err != nil {
		return nil, fail("%v", err)
	}
	if err := modeAllowsOps(tc.sess.mode, ops); err != nil {
		return nil, err
	}
	res, err := b.d.Canvas.ApplyOps(ctx, tc.run, tc.id, a.Ops)
	if err != nil {
		return nil, err
	}
	content, err := jsonOut(map[string]any{"ok": true, "id_map": res.IDMap, "changes": res.Changes, "revision": res.Revision})
	ids := make([]string, 0, len(res.IDMap))
	for _, id := range res.IDMap {
		ids = append(ids, id)
	}
	return &toolOut{content: content, summary: fmt.Sprintf("应用 %d 项操作", len(ops)), extra: map[string]any{"node_ids": ids}}, err
}

// toolArrange 整理布局。
func (b *AgentBridge) toolArrange(ctx context.Context, tc *toolCall) (*toolOut, error) {
	var a struct {
		GroupID string   `json:"groupId"`
		NodeIDs []string `json:"nodeIds"`
		Layout  string   `json:"layout"`
	}
	if err := json.Unmarshal(tc.args, &a); err != nil {
		return nil, fail("参数格式不对：%v", err)
	}
	res, err := b.d.Canvas.Arrange(ctx, tc.run, tc.id, canvasgraph.ArrangeTarget{GroupID: a.GroupID, NodeIDs: a.NodeIDs}, canvasgraph.Layout(a.Layout))
	if err != nil {
		return nil, err
	}
	content, err := jsonOut(map[string]any{"ok": true, "changes": res.Changes})
	return &toolOut{content: content, summary: "整理布局（" + a.Layout + "）", extra: map[string]any{"node_ids": a.NodeIDs}}, err
}

// toolDelete 申请删除：不直接删，创建审批并让本轮停下等用户决定。
func (b *AgentBridge) toolDelete(ctx context.Context, tc *toolCall) (*toolOut, error) {
	var a struct {
		NodeIDs []string `json:"nodeIds"`
		EdgeIDs []string `json:"edgeIds"`
		Reason  string   `json:"reason"`
	}
	if err := json.Unmarshal(tc.args, &a); err != nil {
		return nil, fail("参数格式不对：%v", err)
	}
	if len(a.NodeIDs)+len(a.EdgeIDs) == 0 {
		return nil, fail("没有指定要删除的节点或连线")
	}
	p := DeletePayload{Reason: truncateRunes(a.Reason, 200), NodeIDs: a.NodeIDs, EdgeIDs: a.EdgeIDs}
	if len(a.NodeIDs) > 0 {
		d, err := b.d.Canvas.Detail(ctx, tc.run, a.NodeIDs) // 顺便确认节点都存在，卡片上要显示标题和是否含产物
		if err != nil {
			return nil, err
		}
		for _, n := range d.Nodes {
			label, _ := n.Data["label"].(string)
			has, _ := n.Data["hasOutput"].(bool)
			p.Labels, p.Outputs = append(p.Labels, label), append(p.Outputs, has)
		}
	}
	if _, err := b.d.Agent.CreateApproval(ctx, tc.run, tc.id, model.ApprovalDelete, p, 0); err != nil {
		return nil, err
	}
	return &toolOut{content: "已提交给用户确认，等待决定。用户决定后你会收到结果。", terminate: true,
		summary: fmt.Sprintf("申请删除 %d 个节点", len(a.NodeIDs))}, nil
}

// toolPlan 更新计划：校验后记一条 plan.updated 事件，前端据此显示置顶计划。
func (b *AgentBridge) toolPlan(ctx context.Context, tc *toolCall) (*toolOut, error) {
	var a struct {
		Steps []struct {
			Title  string `json:"title"`
			Status string `json:"status"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(tc.args, &a); err != nil {
		return nil, fail("参数格式不对：%v", err)
	}
	if len(a.Steps) > maxPlanSteps {
		return nil, fail("计划最多 %d 步", maxPlanSteps)
	}
	steps := make([]map[string]string, len(a.Steps))
	for i, s := range a.Steps {
		title := strings.TrimSpace(s.Title)
		if title == "" || utf8.RuneCountInString(title) > maxPlanTitleLen {
			return nil, fail("第 %d 步的标题不能为空，且不超过 %d 个字", i+1, maxPlanTitleLen)
		}
		if s.Status != "todo" && s.Status != "doing" && s.Status != "done" {
			return nil, fail("第 %d 步的状态只能是 todo / doing / done", i+1)
		}
		steps[i] = map[string]string{"title": title, "status": s.Status}
	}
	b.d.Agent.emit(ctx, tc.run.UserID, tc.run.SessionID, tc.run.ID, tc.run.CanvasID, "plan.updated", map[string]any{"steps": steps})
	return &toolOut{content: "计划已更新。", summary: fmt.Sprintf("更新计划（%d 步）", len(steps))}, nil
}

// toolAsk 向用户提问：选项题或选模型题，创建提问并让本轮停下等回答。
func (b *AgentBridge) toolAsk(ctx context.Context, tc *toolCall) (*toolOut, error) {
	var a struct {
		Question    string   `json:"question"`
		Kind        string   `json:"kind"`
		Options     []string `json:"options"`
		ModelKind   string   `json:"modelKind"`
		AllowCustom bool     `json:"allowCustom"`
	}
	if err := json.Unmarshal(tc.args, &a); err != nil {
		return nil, fail("参数格式不对：%v", err)
	}
	q := strings.TrimSpace(a.Question)
	if q == "" || utf8.RuneCountInString(q) > maxQuestionLen {
		return nil, fail("问题不能为空，且不超过 %d 个字", maxQuestionLen)
	}
	p := AskPayload{Question: q, Kind: a.Kind, AllowCustom: a.AllowCustom}
	switch a.Kind {
	case "choice":
		if len(a.Options) < 2 || len(a.Options) > 4 {
			return nil, fail("选项题需要 2 到 4 个选项")
		}
		p.Options = a.Options
	case "model":
		models, err := b.askModels(ctx, a.ModelKind)
		if err != nil {
			return nil, err
		}
		p.ModelKind, p.Models = a.ModelKind, models
	default:
		return nil, fail("kind 只能是 choice 或 model")
	}
	if _, err := b.d.Agent.CreateApproval(ctx, tc.run, tc.id, model.ApprovalAsk, p, 0); err != nil {
		return nil, err
	}
	return &toolOut{content: "已向用户提问，等待回答。用户回答后你会收到结果。", terminate: true, summary: "向用户提问：" + truncateRunes(q, 40)}, nil
}

// askModels 列出某类已发布的生成模型，给「选模型」提问用。
func (b *AgentBridge) askModels(ctx context.Context, kind string) ([]AskModel, error) {
	if kind != "image" && kind != "video" {
		return nil, fail("modelKind 只能是 image 或 video")
	}
	infos, err := b.d.Registry.ListModels(ctx, kind)
	if err != nil {
		return nil, err
	}
	if len(infos) == 0 {
		return nil, fail("目前没有已发布的%s模型，请告诉用户需要管理员先配置。", map[string]string{"image": "图片", "video": "视频"}[kind])
	}
	out := make([]AskModel, len(infos))
	for i, m := range infos {
		out[i] = AskModel{Key: m.Key, Name: m.Label, Hint: m.Hint}
	}
	return out, nil
}

// toolModelList 列出某类已发布的生成模型和它们的能力摘要，模型据此挑模型、写参数。
func (b *AgentBridge) toolModelList(ctx context.Context, tc *toolCall) (*toolOut, error) {
	var a struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(tc.args, &a); err != nil || (a.Kind != "image" && a.Kind != "video" && a.Kind != "audio") {
		return nil, fail("kind 只能是 image / video / audio")
	}
	infos, err := b.d.Registry.ListModels(ctx, a.Kind)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, len(infos))
	for i, m := range infos {
		out[i] = modelSummary(m)
	}
	content, err := jsonOut(map[string]any{"models": out})
	return &toolOut{content: content, summary: fmt.Sprintf("列出 %d 个%s模型", len(out), a.Kind)}, err
}

// modelSummary 是给模型看的模型摘要：能力和定价的关键信息，不含任何内部配置。
func modelSummary(m provider.ModelInfo) map[string]any {
	out := map[string]any{"key": m.Key, "label": m.Label, "hint": m.Hint, "ops": m.Capabilities.Ops,
		"refs":    map[string]bool{"image": m.Capabilities.Refs.Image.On, "video": m.Capabilities.Refs.Video.On, "audio": m.Capabilities.Refs.Audio.On},
		"pricing": map[string]any{"billing": m.Pricing.Billing, "unit": m.Pricing.Unit, "per_second": m.Pricing.PerSecond}}
	params := make([]map[string]any, 0, len(m.Capabilities.Params))
	for _, p := range m.Capabilities.Params {
		params = append(params, map[string]any{"name": p.Name, "type": p.Type, "options": p.Options, "default": p.Default})
	}
	out["params"] = params
	return out
}
