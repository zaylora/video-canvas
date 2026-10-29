package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"gorm.io/datatypes"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
)

// 平台 API 里审批的三个无状态操作。方案二里审批记录和状态流转归 Agent 服务，Go 只保留和钱、画布、生成任务有关的规则：
//
//	预估（ProposeApproval）：工具参数 → 审批内容和预估积分，不落库；
//	解析（ResolveApproval）：用户的决定 → 实际批准了什么，纯逻辑；
//	执行（ExecuteApproval）：批准的内容 → 删除或创建生成任务，并写出给模型看的结果文字。
//
// 这样规则和措辞只有一份，不用在 Agent 服务里再写一遍。

// approvalTools 是审批种类对应的工具：模式限制、参数校验、预检和算价都沿用这些工具里已有的逻辑。
var approvalTools = map[string]string{
	model.ApprovalGenerate: "generate_media",
	model.ApprovalDelete:   "canvas_delete",
	model.ApprovalAsk:      "ask_user",
	"drama_update_outline": "drama_update_outline",
	"drama_update_episode": "drama_update_episode",
	"drama_update_segment": "drama_update_segment",
	"drama_run_job":        "drama_run_job",
}

// ApprovalProposal 是一次预估的结果。校验不过时 IsError 为真，Content 是写给模型看的原因（和令牌模式下的工具错误一致），没有审批内容。
type ApprovalProposal struct {
	IsError      bool            `json:"is_error"`
	Payload      json.RawMessage `json:"payload,omitempty"` // 审批内容（GeneratePayload、DeletePayload 或 AskPayload），原样存进审批
	QuoteCredits int             `json:"quote_credits"`     // 预估积分，只有生成不为 0
	Summary      string          `json:"summary,omitempty"` // 时间线上的一行摘要
	Content      string          `json:"content"`           // 给模型看的工具结果：成功时是「已提交，等待决定」，失败时是原因
	Terminate    bool            `json:"terminate"`         // 本轮执行完这一批工具后停下，等用户决定
}

// ProposeApproval 为一次需要用户决定的工具调用（生成、删除、提问）算出审批内容和预估积分，不创建审批、不发事件、不改任何状态。
// kind 取 model.ApprovalGenerate / ApprovalDelete / ApprovalAsk；任务模式的限制在这里强制。
// 工具层的失败（参数不对、节点没有产物、没选模型……）是 IsError 的结果；上下文或 kind 不合法才返回 error。
func (b *AgentBridge) ProposeApproval(ctx context.Context, rc RunContext, kind string, args json.RawMessage) (*ApprovalProposal, error) {
	if err := rc.validate(); err != nil {
		return nil, err
	}
	name, ok := approvalTools[kind]
	if !ok {
		return nil, errcode.ErrInvalidParams.WithMsg("审批种类不对")
	}
	spec := findTool(name)
	sess := b.platformSession(rc)
	if !spec.allowsMode(sess.mode) {
		return &ApprovalProposal{IsError: true, Content: "当前是「" + modeLabel(sess.mode) + "」模式，不能使用 " + name + "。"}, nil
	}
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	var draft approvalDraft
	out, herr := spec.run(b, ctx, &toolCall{run: rc.run(), sess: sess, args: args, propose: &draft})
	res := b.finishTool(out, herr)
	if res.IsError {
		return &ApprovalProposal{IsError: true, Content: res.Content}, nil
	}
	payload, err := json.Marshal(draft.payload)
	if err != nil {
		return nil, err
	}
	return &ApprovalProposal{Payload: payload, QuoteCredits: draft.quote, Summary: outSummary(out), Content: res.Content, Terminate: res.Terminate}, nil
}

// ApprovalOutcome 是用户的决定解析出来的结果：落库的状态、写进审批的决定、预估积分，以及批准后要执行的内容。
type ApprovalOutcome struct {
	Status         string                `json:"status"`                    // approved 整体批准、partial 部分批准、rejected 拒绝
	Decision       map[string]any        `json:"decision"`                  // 写进审批的决定（含实际批准的条目）
	QuoteCredits   int                   `json:"quote_credits"`             // 按批准的内容重新算出的积分，只有生成不为 0
	Items          []GenerateItem        `json:"items,omitempty"`           // 批准的生成条目（张数已按批准的算）
	NodeIDs        []string              `json:"node_ids,omitempty"`        // 批准删除的节点
	EdgeIDs        []string              `json:"edge_ids,omitempty"`        // 批准删除的连线
	Drama          *DramaApprovalPayload `json:"drama,omitempty"`           // 短剧审批原文和签名
	DramaSignature string                `json:"drama_signature,omitempty"` // 绑定决定与预估积分，阻止执行前篡改
}

func (o *outcome) export() *ApprovalOutcome {
	return &ApprovalOutcome{Status: o.status, Decision: o.decision, QuoteCredits: o.quote, Items: o.gen, NodeIDs: o.nodeIDs, EdgeIDs: o.edgeIDs}
}

// ResolveApproval 把用户对一个审批的决定解析成「实际批准了什么」。纯逻辑，不读写任何记录：
// 逐项勾选、张数只能少不能多、按批准的内容重新算价、检查本轮预算（调用方声明的预算和已花的，加上 Go 按调用记录累加的模型花费）。
// 规则就是令牌模式下 AgentService.Decide 用的那一份，没有复制。决定不合法（一项都没批准、序号越界、回答为空、超出预算）返回对应的业务错误。
func (b *AgentBridge) ResolveApproval(ctx context.Context, rc RunContext, kind string, payload json.RawMessage, req model.DecideAgentApprovalReq) (*ApprovalOutcome, error) {
	if err := rc.validate(); err != nil {
		return nil, err
	}
	if _, ok := approvalTools[kind]; !ok {
		return nil, errcode.ErrInvalidParams.WithMsg("审批种类不对")
	}
	if !json.Valid(payload) {
		return nil, errcode.ErrInvalidParams.WithMsg("审批内容不是合法的 JSON")
	}
	if isDramaApproval(kind) {
		return b.resolveDramaApproval(ctx, rc, kind, payload, req)
	}
	run, err := b.platformRunState(ctx, rc)
	if err != nil {
		return nil, err
	}
	out, err := b.d.Agent.resolveDecision(&model.AgentApproval{Kind: kind, PayloadJSON: datatypes.JSON(payload)}, run, &req)
	if err != nil {
		return nil, err
	}
	return out.export(), nil
}

// 审批执行的结果状态。
const (
	ExecNone     = "none"     // 这个决定不需要执行任何东西（拒绝、提问）
	ExecExecuted = "executed" // 已执行
	ExecFailed   = "failed"   // 执行失败，原因在 Result.error
)

// ApprovalExecution 是执行一个审批的结果：Agent 服务据此更新审批记录，并把 OutcomeText 作为工具结果交给模型。
type ApprovalExecution struct {
	Status      string          `json:"status"`
	Result      json.RawMessage `json:"result,omitempty"` // 写进审批的 result_json：成功时是删除或提交的结果，失败时是 {"error": 原因}
	OutcomeText string          `json:"outcome_text"`     // 给模型看的工具结果
}

// ExecuteApproval 执行用户批准的内容：删除节点和连线，或为批准的条目创建生成任务；并写出给模型看的结果文字。
// outcome 是 ResolveApproval 的返回值，原样传回来；拒绝和提问不执行任何东西，只给出文字。
//
// 执行是幂等的，键是审批 id：Agent 服务在记录结果之前崩溃，重启后重试同一个审批，得到的是第一次的结果，
// 不会再删一次（节点已经没了，会误报失败）、再提交一次任务、再扣一次积分。
// 业务上的失败（节点已不存在、积分不足、模型下线……）是 Status=failed 的结果，文字里写明原因，模型据此决定下一步；
// 请求被取消这类「不知道有没有执行」的情况返回 error，让调用方重试。
func (b *AgentBridge) ExecuteApproval(ctx context.Context, rc RunContext, kind string, approvalID uint64, toolCallID string, o ApprovalOutcome) (*ApprovalExecution, error) {
	if err := rc.validate(); err != nil {
		return nil, err
	}
	if _, ok := approvalTools[kind]; !ok {
		return nil, errcode.ErrInvalidParams.WithMsg("审批种类不对")
	}
	if approvalID == 0 {
		return nil, errcode.ErrInvalidParams.WithMsg("缺少审批 id")
	}
	switch o.Status {
	case model.ApprovalApproved, model.ApprovalPartial, model.ApprovalRejected:
	default:
		return nil, errcode.ErrInvalidParams.WithMsg("审批状态不对")
	}
	if isDramaApproval(kind) {
		return b.executeDramaApproval(ctx, rc, kind, approvalID, o)
	}
	out := &outcome{status: o.Status, decision: o.Decision, quote: o.QuoteCredits, gen: o.Items, nodeIDs: o.NodeIDs, edgeIDs: o.EdgeIDs}
	ctx = WithIdempotencyKey(ctx, fmt.Sprintf("approval-%d", approvalID))
	result, ran, err := b.d.Agent.runApproved(ctx, rc.run(), kind, approvalID, toolCallID, out)
	if err != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	rec := &model.AgentApproval{Kind: kind, Status: o.Status}
	res := &ApprovalExecution{Status: ExecNone}
	if ran {
		status, body := approvalExecResult(result, err)
		rec.Status, rec.ResultJSON = status, datatypes.JSON(body)
		res.Status, res.Result = ExecExecuted, body
		if status == model.ApprovalFailed {
			res.Status = ExecFailed
		}
	}
	res.OutcomeText = approvalOutcomeText(rec, out)
	return res, nil
}
