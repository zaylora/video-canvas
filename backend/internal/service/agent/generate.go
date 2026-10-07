package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"video-canvas/internal/agent/canvasgraph"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
)

// maxGenerateItems 是一次 generate_media 最多申请几个节点。
const maxGenerateItems = 8

// AgentGenTasks 是 Agent 用到的生成任务能力：创建（批准后）和查询（task_get）。*GenerationTaskService 实现它。
type AgentGenTasks interface {
	// Create 提交生成任务，规则与用户在画布上点生成完全一致（校验、计价、冻结积分、并发上限）。
	Create(ctx context.Context, userID uint64, idempotencyKey string, req *model.CreateGenerationTaskReq) (*model.CreateGenerationTaskResp, error)
	// Get 查用户自己的一个任务。
	Get(ctx context.Context, userID, id uint64) (*model.GenerationTaskView, error)
}

// remoteKindOf 把节点种类换成生成任务的种类：文本节点走文本模型。
func remoteKindOf(nodeKind string) string {
	if nodeKind == canvasgraph.KindScript {
		return modelcfg.KindText
	}
	return nodeKind
}

// buildGenInput 按模型能力把节点读出来的内容组装成任务输入，与画布上点生成时的规则一致：
// 提示词手填优先，没有才用上游文字；生成方式取节点上选的，没选时有参考图就走图生；上游素材按种类放进 images / videos / audios。
// 参数里有「生成数量」的一律按 1，要多个结果请多建几个节点。校验和默认值由 modelcfg.ValidateInput 负责。
func buildGenInput(caps modelcfg.Capabilities, src *canvasgraph.GenSource) map[string]any {
	input := make(map[string]any, len(src.Params)+4)
	for k, v := range src.Params {
		input[k] = v
	}
	if name := modelcfg.FanoutParam(caps); name != "" {
		input[name] = 1
	}
	prompt := strings.TrimSpace(src.Prompt)
	if prompt == "" {
		prompt = strings.TrimSpace(strings.Join(src.Texts, "\n\n"))
	}
	input["prompt"] = prompt
	hasImage := slices.ContainsFunc(src.Assets, func(a canvasgraph.GenAsset) bool { return a.Kind == "image" })
	if op := pickOp(caps.Ops, src.Params["op"], hasImage); op != "" {
		input["op"] = op
	}
	for _, m := range modelcfg.MediaKinds {
		var ids []uint64
		for _, a := range src.Assets {
			if id, err := strconv.ParseUint(a.AssetID, 10, 64); err == nil && a.Kind == m.Kind {
				ids = append(ids, id)
			}
		}
		if len(ids) > 0 {
			input[m.Key] = ids
		}
	}
	return input
}

// pickOp 选生成方式：同时支持文生图和图生图的模型由有没有参考图决定；其它取节点上选的（仍被支持时），
// 没选时有参考图且模型支持图生视频就走图生，最后取模型支持的第一种。模型没有生成方式（文本、音频）返回空。
func pickOp(ops []string, chosen any, hasImage bool) string {
	switch {
	case len(ops) == 0:
		return ""
	case slices.Contains(ops, "t2i") && slices.Contains(ops, "i2i"):
		if hasImage {
			return "i2i"
		}
		return "t2i"
	}
	if c, ok := chosen.(string); ok && slices.Contains(ops, c) {
		return c
	}
	if hasImage && slices.Contains(ops, "i2v") {
		return "i2v"
	}
	return ops[0]
}

// toolGenerate 申请生成：逐个节点预检（有没有产物、选了模型、参数合不合法），算价后创建审批并让本轮停下。
// 不直接生成：生成花积分，必须用户批准。已有产物的节点不会被覆盖。
func (b *AgentBridge) toolGenerate(ctx context.Context, tc *toolCall) (*toolOut, error) {
	var a struct {
		Items []struct {
			NodeID string `json:"nodeId"`
		} `json:"items"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(tc.args, &a); err != nil {
		return nil, fail("参数格式不对：%v", err)
	}
	if len(a.Items) == 0 || len(a.Items) > maxGenerateItems {
		return nil, fail("items 需要 1 到 %d 项", maxGenerateItems)
	}
	ids := make([]string, len(a.Items))
	for i, it := range a.Items {
		if slices.Contains(ids[:i], it.NodeID) {
			return nil, fail("节点 %q 重复了", it.NodeID)
		}
		ids[i] = it.NodeID
	}
	srcs, err := b.d.Canvas.Generations(ctx, tc.run, ids)
	if err != nil {
		return nil, err
	}
	p := GeneratePayload{Reason: truncateRunes(a.Reason, 200)}
	total := 0
	for _, src := range srcs {
		it, err := b.precheck(ctx, src)
		if err != nil {
			return nil, err
		}
		p.Items = append(p.Items, it)
		total += it.Price
	}
	if _, err := b.d.Agent.CreateApproval(ctx, tc.run, tc.id, model.ApprovalGenerate, p, total); err != nil {
		return nil, err
	}
	return &toolOut{content: "已提交给用户批准，等待决定。用户决定后你会收到结果；批准后任务才会创建并开始生成。", terminate: true,
		summary: fmt.Sprintf("申请生成 %d 个节点（预估 %d 积分）", len(p.Items), total)}, nil
}

// precheck 预检一个节点能不能生成，通过时返回它的审批条目（含预估价）。不通过的原因是写给模型看的，说明怎么改。
func (b *AgentBridge) precheck(ctx context.Context, src *canvasgraph.GenSource) (GenerateItem, error) {
	name := fmt.Sprintf("节点 %q（%s）", src.NodeID, src.Label)
	switch {
	case src.HasOutput:
		return GenerateItem{}, fail("%s已经有产物，生成不会覆盖已有产物。需要新版本请用 canvas_apply_ops 新建一个节点再申请生成。", name)
	case src.Status == "running":
		return GenerateItem{}, fail("%s正在生成中。", name)
	case src.Model == "":
		return GenerateItem{}, fail("%s还没有选模型。先用 model_list 挑一个，再用 canvas_apply_ops 的 update_node 设置 model；用户没指定模型时用 ask_user（kind=model）问。", name)
	}
	snap, err := b.d.Registry.Snapshot(ctx, src.Model)
	if errors.Is(err, provider.ErrModelUnavailable) {
		return GenerateItem{}, fail("%s选的模型 %q 不可用或已下线，请用 model_list 重新挑选。", name, src.Model)
	}
	if err != nil {
		return GenerateItem{}, err
	}
	caps := snap.Model.Capabilities
	if snap.Model.Kind != remoteKindOf(src.Kind) {
		return GenerateItem{}, fail("%s是%s节点，模型 %q 是%s模型，种类对不上。", name, src.Kind, src.Model, snap.Model.Kind)
	}
	input, fieldErrs := modelcfg.ValidateInput(snap.Model.Kind, caps, buildGenInput(caps, src))
	if len(fieldErrs) > 0 {
		return GenerateItem{}, fail("%s的参数不合法：%s。请用 canvas_apply_ops 修正后再申请。", name, modelcfg.JoinFieldErrors(fieldErrs))
	}
	price := max(modelcfg.Quote(snap.Model.Pricing, caps, modelcfg.SpecFromInput(input, caps.System)), 0)
	return GenerateItem{NodeID: src.NodeID, Label: src.Label, ModelKey: snap.Model.Key, ModelName: snap.Model.Label, Price: price, Count: 1}, nil
}

// toolTaskGet 查一个生成任务的进度和结果：模型要知道生成有没有完成、失败原因是什么，才能决定下一步。
func (b *AgentBridge) toolTaskGet(ctx context.Context, tc *toolCall) (*toolOut, error) {
	var a struct {
		TaskID string `json:"taskId"`
	}
	if err := json.Unmarshal(tc.args, &a); err != nil {
		return nil, fail("参数格式不对：%v", err)
	}
	id, err := strconv.ParseUint(a.TaskID, 10, 64)
	if err != nil || id == 0 {
		return nil, fail("taskId 不对，应该是节点上记录的任务 id（十进制数字）")
	}
	if b.d.Tasks == nil {
		return nil, fail("暂时无法查询任务")
	}
	v, err := b.d.Tasks.Get(ctx, tc.run.UserID, id)
	var ec *errcode.Error
	if errors.As(err, &ec) && ec.HTTPStatus() == 404 {
		return nil, fail("任务 %s 不存在", a.TaskID)
	}
	if err != nil {
		return nil, err
	}
	outs := make([]map[string]any, len(v.Outputs))
	for i, o := range v.Outputs {
		outs[i] = map[string]any{"media_type": o.MediaType, "asset_id": strconv.FormatUint(o.AssetID, 10), "text": truncateRunes(o.Text, 500)}
	}
	content, err := jsonOut(map[string]any{"status": v.Status, "progress": v.Progress, "node_id": v.NodeID, "model": v.ModelID,
		"error": v.ErrorMessage, "credits": v.Credits, "charged_credits": v.ChargedCredits, "outputs": outs})
	return &toolOut{content: content, summary: "查询任务 " + a.TaskID + "：" + v.Status}, err
}

// AgentGenerator 在用户批准生成之后创建生成任务，并把任务绑定到节点（实现 AgentGenerationExecutor）。
type AgentGenerator struct {
	tasks    AgentGenTasks
	canvas   *AgentCanvasService
	registry provider.Registry
}

// NewAgentGenerator 创建执行器。
func NewAgentGenerator(tasks AgentGenTasks, canvas *AgentCanvasService, registry provider.Registry) *AgentGenerator {
	return &AgentGenerator{tasks: tasks, canvas: canvas, registry: registry}
}

// generateResult 是写进审批结果的 JSON，也是回给模型的内容：哪些节点开始生成了，哪些没成功以及原因。
type generateResult struct {
	Tasks  []generatedTask `json:"tasks"`
	Failed []failedItem    `json:"failed,omitempty"`
}

type generatedTask struct {
	NodeID  string `json:"node_id"`
	TaskID  string `json:"task_id"`
	Credits int    `json:"credits"`
}

type failedItem struct {
	NodeID string `json:"node_id"`
	Error  string `json:"error"`
}

// Execute 为批准的条目逐个创建任务：以审批 id + 序号作幂等键，续跑或重试不会重复扣积分；
// 每个条目都在最新的画布上重新读节点（批准期间用户可能改过），按当时的内容提交。
// 全部失败返回 error（审批记为失败）；部分成功时失败的条目写在结果里。成功的任务统一绑定到节点。
func (g *AgentGenerator) Execute(ctx context.Context, run *model.AgentRun, approvalID uint64, items []GenerateItem) (json.RawMessage, error) {
	var res generateResult
	bind := map[string]string{}
	for i, it := range items {
		task, err := g.submit(ctx, run, approvalID, i, it)
		if err != nil {
			res.Failed = append(res.Failed, failedItem{NodeID: it.NodeID, Error: err.Error()})
			continue
		}
		taskID := strconv.FormatUint(task.ID, 10)
		bind[it.NodeID] = taskID
		res.Tasks = append(res.Tasks, generatedTask{NodeID: it.NodeID, TaskID: taskID, Credits: task.Credits})
	}
	if len(res.Tasks) == 0 {
		return nil, errors.New(res.Failed[0].Error)
	}
	if _, err := g.canvas.BindTasks(ctx, run, "approval-"+strconv.FormatUint(approvalID, 10), bind); err != nil {
		return nil, fmt.Errorf("任务已创建但绑定到节点失败：%w", err)
	}
	return json.Marshal(res)
}

// submit 提交一个条目。
func (g *AgentGenerator) submit(ctx context.Context, run *model.AgentRun, approvalID uint64, idx int, it GenerateItem) (*model.GenerationTaskView, error) {
	srcs, err := g.canvas.Generations(ctx, run, []string{it.NodeID})
	if err != nil {
		return nil, errors.New("节点已经不存在")
	}
	src := srcs[0]
	if src.HasOutput || src.Status == "running" {
		return nil, errors.New("节点在批准期间已经有了产物或正在生成，没有重复提交")
	}
	snap, err := g.registry.Snapshot(ctx, it.ModelKey)
	if err != nil {
		return nil, errors.New("模型已不可用")
	}
	key := fmt.Sprintf("agent-%d-%d", approvalID, idx)
	resp, err := g.tasks.Create(ctx, run.UserID, key, &model.CreateGenerationTaskReq{
		Kind: snap.Model.Kind, ModelID: it.ModelKey, CanvasID: idcodec.ID(run.CanvasID), NodeID: it.NodeID,
		Input: buildGenInput(snap.Model.Capabilities, src),
	})
	if err != nil {
		return nil, userFacing(err)
	}
	if len(resp.Items) != 1 || resp.Items[0].Task == nil {
		if len(resp.Items) == 1 && resp.Items[0].Error != nil {
			return nil, errors.New(resp.Items[0].Error.Message)
		}
		return nil, errors.New("任务没有创建成功")
	}
	return resp.Items[0].Task, nil
}

// userFacing 把业务错误换成它的文案，其它错误只说内部错误（细节由任务服务记日志）。
func userFacing(err error) error {
	var ec *errcode.Error
	if errors.As(err, &ec) {
		return errors.New(ec.Msg)
	}
	return errors.New("内部错误")
}
