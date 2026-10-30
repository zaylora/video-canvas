package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"

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
// 提示词里 @ 的引用展开、没被 @ 的上游文字追加在后面（规则见 canvasgraph.ComposePrompt）；生成方式取节点上选的，没选时有参考图就走图生；上游素材按种类放进 images / videos / audios，角色卡展开成主图、补充图和声音。
// 参数里有「生成数量」的一律按 1，要多个结果请多建几个节点。校验和默认值由 modelcfg.ValidateInput 负责。
func buildGenInput(caps modelcfg.Capabilities, src *canvasgraph.GenSource) (map[string]any, error) {
	input := make(map[string]any, len(src.Params)+4)
	for k, v := range src.Params {
		input[k] = v
	}
	if name := modelcfg.FanoutParam(caps); name != "" {
		input[name] = 1
	}
	hasImage := slices.ContainsFunc(src.Assets, func(a canvasgraph.GenAsset) bool { return a.Kind == "image" })
	op := pickOp(caps.Ops, src.Params["op"], hasImage)
	if op != "" {
		input["op"] = op
	}
	used := modelcfg.RefKindsUsed(caps, op)
	// 先确定实际提交的素材，再计算提示词编号，避免手动素材和角色补充图造成编号漂移。
	refs := src.RefIDs(used, func(kind string) int { return caps.Refs.Of(kind).Max })
	generateAudio := true
	if field, ok := caps.Params.Get("generate_audio"); ok {
		value := field.Default
		if field.Open && input["generate_audio"] != nil {
			value = input["generate_audio"]
		}
		generateAudio = value != false && value != "false"
	}
	if err := src.ValidateCharacterInput(refs, generateAudio); err != nil {
		return nil, err
	}
	input["prompt"] = canvasgraph.ComposePrompt(src, refs)
	for _, m := range modelcfg.MediaKinds {
		if ids := refs[m.Kind]; len(ids) > 0 {
			if len(ids) > caps.Refs.Of(m.Kind).Max {
				return nil, fmt.Errorf("%s最多 %d 个，请移除多余的参考素材", map[string]string{"image": "参考图片", "audio": "参考音频", "video": "参考视频"}[m.Kind], caps.Refs.Of(m.Kind).Max)
			}
			input[m.Key] = ids
		}
	}
	return input, nil
}

// pickOp 选生成方式，规则见 canvasgraph.PickOp：创建节点后给模型的提醒和这里用的是同一条规则。
func pickOp(ops []string, chosen any, hasImage bool) string {
	return canvasgraph.PickOp(ops, chosen, hasImage)
}

// toolGenerate 申请生成：逐个节点预检（有没有产物、选了模型、参数合不合法），算价后创建审批并让本轮停下。
// 不直接生成：生成花积分，必须用户批准。已有产物的节点不会被覆盖。
func (b *AgentBridge) toolGenerate(ctx context.Context, tc *toolCall) (*toolOut, error) {
	var a struct {
		Items []struct {
			NodeID string `json:"nodeId"`
		} `json:"items"`
		Reason string `json:"reason"`
		Wait   *bool  `json:"wait"` // 不传按要等
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
	p := GeneratePayload{Reason: truncateRunes(a.Reason, 200), Wait: a.Wait == nil || *a.Wait}
	total := 0
	for _, src := range srcs {
		it, err := b.precheck(ctx, tc.run.UserID, src)
		if err != nil {
			return nil, err
		}
		p.Items = append(p.Items, it)
		total += it.Price
	}
	if err := b.requestApproval(ctx, tc, model.ApprovalGenerate, p, total); err != nil {
		return nil, err
	}
	return &toolOut{content: "已提交给用户批准，等待决定。用户决定后你会收到结果；批准后任务才会创建并开始生成。", terminate: true,
		summary: fmt.Sprintf("申请生成 %d 个节点（预估 %d 积分）", len(p.Items), total)}, nil
}

// precheck 预检一个节点能不能生成，通过时返回它的审批条目（含预估价）。不通过的原因是写给模型看的，说明怎么改。
func (b *AgentBridge) precheck(ctx context.Context, userID uint64, src *canvasgraph.GenSource) (GenerateItem, error) {
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
	rawInput, err := buildGenInput(caps, src)
	if err != nil {
		return GenerateItem{}, fail("%s：%s", name, err.Error())
	}
	if err := validateCharacterVoiceTasks(ctx, b.d.Tasks, src, userID); err != nil {
		return GenerateItem{}, fail("%s：%s", name, err.Error())
	}
	input, fieldErrs := modelcfg.ValidateInput(snap.Model.Kind, caps, rawInput)
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
//
// 带了幂等键（平台 API 的执行接口以审批 id 为键）时，同一个批准重复到达会原样返回第一次的结果：绑定之后节点已经在生成，
// 再走一遍提交只会得到「节点正在生成」而误报失败。
func (g *AgentGenerator) Execute(ctx context.Context, run *model.AgentRun, approvalID uint64, items []GenerateItem) (json.RawMessage, error) {
	if key := IdempotencyKeyFrom(ctx); key != "" {
		if prior, ok, err := g.canvas.priorBindResult(ctx, run, key); err != nil {
			return nil, err
		} else if ok {
			return prior, nil
		}
	}
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
	body, err := json.Marshal(res)
	if err != nil {
		return nil, err
	}
	if _, err := g.canvas.bindTasks(ctx, run, "approval-"+strconv.FormatUint(approvalID, 10), bind, body); err != nil {
		return nil, fmt.Errorf("任务已创建但绑定到节点失败：%w", err)
	}
	return body, nil
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
	input, err := buildGenInput(snap.Model.Capabilities, src)
	if err != nil {
		return nil, err
	}
	if err := validateCharacterVoiceTasks(ctx, g.tasks, src, run.UserID); err != nil {
		return nil, err
	}
	resp, err := g.tasks.Create(ctx, run.UserID, key, &model.CreateGenerationTaskReq{
		Kind: snap.Model.Kind, ModelID: it.ModelKey, CanvasID: idcodec.ID(run.CanvasID), NodeID: it.NodeID,
		AgentRunID: run.ID, // 任务结束时据此通知 Agent；不从请求体读，只有这里能填
		Input:      input,
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

// validateCharacterVoiceTasks 在预检和实际提交时核对声音任务；成功但尚未回填也不能使用旧声音。
func validateCharacterVoiceTasks(ctx context.Context, tasks AgentGenTasks, src *canvasgraph.GenSource, userID uint64) error {
	if src.Kind != canvasgraph.KindVideo {
		return nil
	}
	for _, role := range src.Characters {
		if role.VoiceTaskID == "" {
			continue
		}
		id, err := strconv.ParseUint(role.VoiceTaskID, 10, 64)
		if err != nil || tasks == nil {
			return fmt.Errorf("角色「%s」的声音尚未就绪，请等待声音生成完成", role.Name)
		}
		view, err := tasks.Get(ctx, userID, id)
		if err != nil {
			return userFacing(err)
		}
		if !slices.Contains([]string{model.TaskFailed, model.TaskExpired, model.TaskCanceled}, view.Status) {
			return fmt.Errorf("角色「%s」的声音尚未就绪，请等待声音生成完成", role.Name)
		}
	}
	return nil
}

// userFacing 把业务错误换成它的文案，其它错误只说内部错误（细节由任务服务记日志）。
func userFacing(err error) error {
	var ec *errcode.Error
	if errors.As(err, &ec) {
		return errors.New(ec.Msg)
	}
	return errors.New("内部错误")
}
