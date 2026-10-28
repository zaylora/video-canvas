package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
)

// GenerateItem 是生成审批里的一项：给哪个节点、用哪个模型、生成几张、预估多少积分。
type GenerateItem struct {
	NodeID    string `json:"node_id"`    // 要生成的节点
	Label     string `json:"label"`      // 节点标题，给卡片显示
	ModelKey  string `json:"model_key"`  // 生成模型 key
	ModelName string `json:"model_name"` // 生成模型显示名
	Price     int    `json:"price"`      // 单张预估积分
	Count     int    `json:"count"`      // 申请生成的张数
}

// GeneratePayload 是生成审批的内容。
type GeneratePayload struct {
	Reason string         `json:"reason"` // Agent 给的理由
	Items  []GenerateItem `json:"items"`  // 要生成的条目
	Wait   bool           `json:"wait"`   // 批准后 Agent 是否等这批任务结束再继续（默认是；用户说提交就行时模型传 false）
}

// DeletePayload 是删除审批的内容。
type DeletePayload struct {
	Reason  string   `json:"reason"`   // Agent 给的理由
	NodeIDs []string `json:"node_ids"` // 要删的节点
	Labels  []string `json:"labels"`   // 对应的节点标题，给卡片显示
	Outputs []bool   `json:"outputs"`  // 对应的节点是否含产物，卡片上标红提醒
	EdgeIDs []string `json:"edge_ids"` // 要删的连线
}

// AskPayload 是提问的内容。
type AskPayload struct {
	Question    string     `json:"question"`               // 问题
	Kind        string     `json:"kind"`                   // choice 或 model
	Options     []string   `json:"options,omitempty"`      // 选项
	ModelKind   string     `json:"model_kind,omitempty"`   // kind=model 时要选哪类模型
	AllowCustom bool       `json:"allow_custom,omitempty"` // 是否允许自定义回答
	Models      []AskModel `json:"models,omitempty"`       // kind=model 时可选的已发布模型
}

// AskModel 是提问里可选的一个生成模型。
type AskModel struct {
	Key  string `json:"key"`  // 模型 key
	Name string `json:"name"` // 显示名
	Hint string `json:"hint"` // 简介
}

// AgentGenerationExecutor 在用户批准生成之后真正发起生成任务（以审批 id 作幂等键，续跑不会重复扣费）。
type AgentGenerationExecutor interface {
	// Execute 为批准的条目创建生成任务，返回写进审批结果的 JSON。
	Execute(ctx context.Context, run *model.AgentRun, approvalID uint64, items []GenerateItem) (json.RawMessage, error)
}

// 审批决定的结果：落库的状态、写进 decision_json 的内容，以及批准后要执行的东西。
type outcome struct {
	status   string
	decision map[string]any
	quote    int
	gen      []GenerateItem
	nodeIDs  []string
	edgeIDs  []string
}

// resolveDecision 把用户的决定按审批种类解析成 outcome。
func (s *AgentService) resolveDecision(a *model.AgentApproval, run *model.AgentRun, req *model.DecideAgentApprovalReq) (*outcome, error) {
	if req.Decision == "reject" {
		// 拒绝时可以附一句理由（「拒绝，并告诉 Agent 怎么改」），记进决定并转告 Agent
		decision := map[string]any{"decision": "reject"}
		if reason := strings.TrimSpace(req.Answer); reason != "" {
			decision["answer"] = reason
		}
		return &outcome{status: model.ApprovalRejected, decision: decision}, nil
	}
	switch a.Kind {
	case model.ApprovalAsk:
		return resolveAsk(req)
	case model.ApprovalDelete:
		return resolveDelete(a, req)
	case model.ApprovalGenerate:
		return resolveGenerate(a, run, req)
	}
	return nil, errcode.ErrAgentApprovalGone
}

// resolveAsk 回答提问：回答不能为空。
func resolveAsk(req *model.DecideAgentApprovalReq) (*outcome, error) {
	if len([]rune(req.Answer)) == 0 {
		return nil, errcode.ErrInvalidParams.WithMsg("请先回答")
	}
	return &outcome{status: model.ApprovalApproved, decision: map[string]any{"decision": "approve", "answer": req.Answer}}, nil
}

// resolveDelete 批准删除：可以逐项取消勾选，至少要留一项。
func resolveDelete(a *model.AgentApproval, req *model.DecideAgentApprovalReq) (*outcome, error) {
	var p DeletePayload
	if err := json.Unmarshal(a.PayloadJSON, &p); err != nil {
		return nil, err
	}
	keep, err := chosen(len(p.NodeIDs), req.Items)
	if err != nil {
		return nil, err
	}
	out := &outcome{status: model.ApprovalApproved, decision: map[string]any{"decision": "approve"}, edgeIDs: p.EdgeIDs}
	for i, id := range p.NodeIDs {
		if k, ok := keep[i]; ok && k != 0 {
			out.nodeIDs = append(out.nodeIDs, id)
		} else {
			out.status = model.ApprovalPartial
		}
	}
	if len(out.nodeIDs) == 0 && len(p.EdgeIDs) == 0 {
		return nil, errcode.ErrInvalidParams.WithMsg("至少批准一项")
	}
	out.decision["node_ids"] = out.nodeIDs
	return out, nil
}

// resolveGenerate 批准生成：逐项勾选，张数只能少不能多；按批准的内容重新算价，并检查本轮预算。
func resolveGenerate(a *model.AgentApproval, run *model.AgentRun, req *model.DecideAgentApprovalReq) (*outcome, error) {
	var p GeneratePayload
	if err := json.Unmarshal(a.PayloadJSON, &p); err != nil {
		return nil, err
	}
	keep, err := chosen(len(p.Items), req.Items)
	if err != nil {
		return nil, err
	}
	out := &outcome{status: model.ApprovalApproved, decision: map[string]any{"decision": "approve"}}
	for i, it := range p.Items {
		n, ok := keep[i]
		switch {
		case !ok || n == 0:
			out.status = model.ApprovalPartial
			continue
		case n < 0 || n > it.Count:
			n = it.Count // 0 表示按申请的数量；超过申请的数量一律收回到申请值，不能多生成
		}
		if n < it.Count {
			out.status = model.ApprovalPartial
		}
		it.Count = n
		out.gen = append(out.gen, it)
		out.quote += it.Price * n
	}
	if len(out.gen) == 0 {
		return nil, errcode.ErrInvalidParams.WithMsg("至少批准一项")
	}
	// 本轮预算为 0 表示不限制：只靠账户积分约束，执行生成时账户积分不够会失败
	if run.BudgetCredits > 0 && out.quote > run.BudgetCredits+req.AddBudget-run.SpentCredits {
		return nil, errcode.ErrAgentOverBudget
	}
	out.decision["items"] = out.gen
	return out, nil
}

// chosen 解析逐项决定：返回「下标 → 批准的数量」。没有逐项决定表示整体批准（每项按申请的数量，用 -1 表示）；
// 下标越界或重复按参数错误。数量 0 且 approve=true 表示按申请的数量（-1）。
func chosen(total int, items []model.ApprovalItemDecision) (map[int]int, error) {
	out := map[int]int{}
	if len(items) == 0 {
		for i := 0; i < total; i++ {
			out[i] = -1
		}
		return out, nil
	}
	for _, it := range items {
		if it.Index < 0 || it.Index >= total {
			return nil, errcode.ErrInvalidParams.WithMsg("条目序号超出范围")
		}
		if _, dup := out[it.Index]; dup {
			return nil, errcode.ErrInvalidParams.WithMsg("条目序号重复")
		}
		switch {
		case !it.Approve:
			out[it.Index] = 0
		case it.Count == 0:
			out[it.Index] = -1
		default:
			out[it.Index] = it.Count
		}
	}
	return out, nil
}

// runApproved 是执行的纯逻辑部分，不碰审批记录：平台 API 的执行接口（ExecuteApproval）用它。
// ran 为假表示这个决定不需要执行任何东西（拒绝、提问，或没有配置生成执行器）。
func (s *AgentService) runApproved(ctx context.Context, run *model.AgentRun, kind string, approvalID uint64, toolCallID string, out *outcome) (result json.RawMessage, ran bool, err error) {
	switch {
	case kind == model.ApprovalDelete && out.status != model.ApprovalRejected:
		var res *WriteResult
		if res, err = s.canvas.DeleteApproved(ctx, run, toolCallID, out.nodeIDs, out.edgeIDs); err == nil {
			result, _ = json.Marshal(res) // WriteResult 只含数字和字符串，序列化不会失败
		}
		return result, true, err
	case kind == model.ApprovalGenerate && out.status != model.ApprovalRejected && s.exec != nil:
		result, err = s.exec.Execute(ctx, run, approvalID, out.gen)
		return result, true, err
	}
	return nil, false, nil
}

// approvalExecResult 把执行结果变成审批的状态和 result_json：失败时 result_json 只记原因。
func approvalExecResult(result json.RawMessage, err error) (status string, body json.RawMessage) {
	if err != nil {
		body, _ = json.Marshal(map[string]string{"error": err.Error()}) // 只含字符串，序列化不会失败
		return model.ApprovalFailed, body
	}
	return model.ApprovalExecuted, result
}

// approvalOutcomeText 把审批的结果写成给模型看的工具结果：用户批准了什么、拒绝了什么、回答了什么，
// 以及批准后执行成功还是失败。模型据此决定下一步，所以要具体、不含糊。
func approvalOutcomeText(a *model.AgentApproval, out *outcome) string {
	text := approvalOutcomeBase(a, out)
	if reason, ok := out.decision["answer"].(string); ok && out.status == model.ApprovalRejected && reason != "" {
		text += "用户的说明：" + reason
	}
	return text
}

func approvalOutcomeBase(a *model.AgentApproval, out *outcome) string {
	rejected := out.status == model.ApprovalRejected
	switch a.Kind {
	case model.ApprovalAsk:
		if rejected {
			return "用户没有回答这个问题。"
		}
		return "用户回答：" + fmt.Sprint(out.decision["answer"])
	case model.ApprovalDelete:
		return deleteOutcome(a, out, rejected)
	case model.ApprovalGenerate:
		return generateOutcome(a, out, rejected)
	}
	return "用户已处理。"
}

func deleteOutcome(a *model.AgentApproval, out *outcome, rejected bool) string {
	if rejected {
		return "用户拒绝了删除，没有任何节点或连线被删除。"
	}
	scope := "用户批准了删除"
	if out.status == model.ApprovalPartial {
		scope = "用户只批准了部分删除"
	}
	if a.Status == model.ApprovalFailed {
		return fmt.Sprintf("%s，但执行失败：%s。画布没有改动。", scope, resultError(a))
	}
	return fmt.Sprintf("%s，已删除 %d 个节点。", scope, len(out.nodeIDs))
}

func generateOutcome(a *model.AgentApproval, out *outcome, rejected bool) string {
	if rejected {
		return "用户拒绝了这次生成，没有生成任何内容，也没有花费积分。"
	}
	scope := "用户批准了生成"
	if out.status == model.ApprovalPartial {
		scope = "用户只批准了部分生成"
	}
	names := make([]string, len(out.gen))
	for i, it := range out.gen {
		names[i] = fmt.Sprintf("%s×%d", it.Label, it.Count)
	}
	if a.Status == model.ApprovalFailed {
		return fmt.Sprintf("%s（%s），但任务提交失败：%s。没有扣积分。", scope, strings.Join(names, "、"), resultError(a))
	}
	return fmt.Sprintf("%s：%s，预估 %d 积分，任务已提交：%s", scope, strings.Join(names, "、"), out.quote, string(a.ResultJSON))
}

// resultError 取审批结果里记录的失败原因。
func resultError(a *model.AgentApproval) string {
	var r struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(a.ResultJSON, &r) == nil && r.Error != "" {
		return r.Error
	}
	return "未知原因"
}
