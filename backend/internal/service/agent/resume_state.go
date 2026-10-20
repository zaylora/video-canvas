package agent

import (
	"context"
	"encoding/json"
	"slices"

	"go.uber.org/zap"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/logger"
)

// 等待用户决定时，工具结果只是一条占位文字；用户决定后才由「继续」带着真实结果补回去。
// 这三条文字同时是占位的识别依据，改动时两边一起变。
const (
	placeholderConfirm = "已提交给用户确认，等待决定。用户决定后你会收到结果。"
	placeholderApprove = "已提交给用户批准，等待决定。用户决定后你会收到结果；批准后任务才会创建并开始生成。"
	placeholderAsk     = "已向用户提问，等待回答。用户回答后你会收到结果。"
)

var approvalPlaceholders = []string{placeholderConfirm, placeholderApprove, placeholderAsk}

// fillDecidedApprovals 在「继续」被中断的运行时，把历史里仍是占位的工具结果换成审批表里的真实结果。
// 场景：用户已经批准并且删除、生成都执行了，但续跑启动失败（等旧进程退出超时等），运行被标为中断；
// 这时历史里最后一条还是「等待决定」，不补回的话模型不知道事情已经发生，可能重复申请。
// 读审批失败只记日志、不拦住继续：最坏情况退回到以前的行为。
func (r *ProcessRuntime) fillDecidedApprovals(ctx context.Context, run *model.AgentRun, input map[string]any) {
	state, ok := input["messages"].(json.RawMessage)
	if !ok || len(state) == 0 {
		return
	}
	approvals, err := r.d.Repo.ListApprovals(ctx, run.ID)
	if err != nil {
		logger.Warn("继续运行时读取审批失败，历史里的等待占位不会被替换", zap.Error(err), zap.Uint64("run_id", run.ID))
		return
	}
	if filled, n := fillApprovalResults(state, approvals); n > 0 {
		input["messages"] = filled
	}
}

// fillApprovalResults 把 state（pi 的对话历史，JSON 数组）里与已决定审批对应的占位工具结果换成真实结果，返回新历史和替换条数。
// 只动文字内容和 isError，其余字段原样保留；解析不了就原样返回。
func fillApprovalResults(state json.RawMessage, approvals []model.AgentApproval) (json.RawMessage, int) {
	texts := map[string]string{} // toolCallId → 真实结果
	for i := range approvals {
		a := &approvals[i]
		if a.ToolCallID == "" {
			continue
		}
		if out, ok := rebuildOutcome(a); ok {
			texts[a.ToolCallID] = approvalOutcomeText(a, out)
		}
	}
	if len(texts) == 0 {
		return state, 0
	}
	var msgs []map[string]json.RawMessage
	if err := json.Unmarshal(state, &msgs); err != nil {
		return state, 0
	}
	n := 0
	for _, m := range msgs {
		var role, callID string
		_ = json.Unmarshal(m["role"], &role)
		_ = json.Unmarshal(m["toolCallId"], &callID)
		text, hit := texts[callID]
		if role != "toolResult" || !hit || !isPlaceholderResult(m["content"]) {
			continue
		}
		content, _ := json.Marshal([]map[string]string{{"type": "text", "text": text}})
		m["content"], m["isError"] = content, json.RawMessage("false")
		n++
	}
	if n == 0 {
		return state, 0
	}
	out, err := json.Marshal(msgs)
	if err != nil {
		return state, 0
	}
	return out, n
}

// isPlaceholderResult 判断工具结果的内容是不是等待决定的占位文字。
func isPlaceholderResult(content json.RawMessage) bool {
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(content, &parts) != nil || len(parts) != 1 {
		return false
	}
	return parts[0].Type == "text" && slices.Contains(approvalPlaceholders, parts[0].Text)
}

// rebuildOutcome 由落库的审批还原用户当时的决定，好复用 approvalOutcomeText 写出真实结果。
// 还没决定、已过期，或批准了但没有执行完（状态停在 approved，不知道结果）的审批返回 false：这时占位不能乱换。
func rebuildOutcome(a *model.AgentApproval) (*outcome, bool) {
	var decision map[string]any
	if json.Unmarshal(a.DecisionJSON, &decision) != nil || decision == nil {
		decision = map[string]any{}
	}
	if a.Status == model.ApprovalRejected {
		return &outcome{status: model.ApprovalRejected, decision: decision}, true
	}
	out := &outcome{status: model.ApprovalApproved, decision: decision, quote: a.QuoteCredits}
	switch a.Kind {
	case model.ApprovalAsk:
		return out, a.Status == model.ApprovalApproved && decision["answer"] != nil
	case model.ApprovalDelete:
		if a.Status != model.ApprovalExecuted && a.Status != model.ApprovalFailed {
			return nil, false
		}
		var p DeletePayload
		if json.Unmarshal(a.PayloadJSON, &p) != nil {
			return nil, false
		}
		out.nodeIDs = stringsOf(decision["node_ids"])
		if len(out.nodeIDs) < len(p.NodeIDs) {
			out.status = model.ApprovalPartial
		}
		return out, true
	case model.ApprovalGenerate:
		if a.Status != model.ApprovalExecuted && a.Status != model.ApprovalFailed {
			return nil, false
		}
		var p GeneratePayload
		var d struct {
			Items []GenerateItem `json:"items"`
		}
		if json.Unmarshal(a.PayloadJSON, &p) != nil || json.Unmarshal(a.DecisionJSON, &d) != nil || len(d.Items) == 0 {
			return nil, false
		}
		out.gen = d.Items
		out.status = generateStatus(p.Items, d.Items)
		return out, true
	}
	return nil, false
}

// generateStatus 对比申请的和批准的条目：少了条目或张数变少是部分批准。
func generateStatus(asked, approved []GenerateItem) string {
	if len(approved) < len(asked) {
		return model.ApprovalPartial
	}
	for _, it := range approved {
		for _, q := range asked {
			if q.NodeID == it.NodeID && it.Count < q.Count {
				return model.ApprovalPartial
			}
		}
	}
	return model.ApprovalApproved
}

// stringsOf 把 JSON 解出来的 []any 取成字符串切片，不是字符串的跳过。
func stringsOf(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, x := range list {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
