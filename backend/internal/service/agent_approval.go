package service

import (
	"context"
	"encoding/json"
	"errors"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
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
	Question    string   `json:"question"`               // 问题
	Kind        string   `json:"kind"`                   // choice 或 model
	Options     []string `json:"options,omitempty"`      // 选项
	ModelKind   string   `json:"model_kind,omitempty"`   // kind=model 时要选哪类模型
	AllowCustom bool     `json:"allow_custom,omitempty"` // 是否允许自定义回答
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

var waitingStatuses = []string{model.RunWaitingApproval, model.RunWaitingInput}

// CreateApproval 为一次需要用户决定的工具调用创建审批，并让运行进入等待状态。由 runtime 的桥在工具里调用。
func (s *AgentService) CreateApproval(ctx context.Context, run *model.AgentRun, toolCallID, kind string, payload any, quote int) (*AgentApprovalView, error) {
	// 1. 种类必须认识；只有还在跑的运行才能发起审批
	next := model.RunWaitingApproval
	switch kind {
	case model.ApprovalGenerate, model.ApprovalDelete:
	case model.ApprovalAsk:
		next = model.RunWaitingInput
	default:
		return nil, errcode.ErrInvalidParams.WithMsg("审批种类不对")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	// 2. 先把运行置为等待状态（CAS）：运行已经被取消或结束时，不再产生审批
	if _, err := s.repo.UpdateRunIf(ctx, run.ID, []string{model.RunRunning}, map[string]any{"status": next}); err != nil {
		if errors.Is(err, repository.ErrAgentStateConflict) {
			return nil, errcode.ErrAgentState
		}
		return nil, err
	}
	// 3. 创建审批，24 小时内有效
	a := &model.AgentApproval{RunID: run.ID, SessionID: run.SessionID, CanvasID: run.CanvasID, UserID: run.UserID, Kind: kind,
		ToolCallID: toolCallID, PayloadJSON: body, QuoteCredits: quote, Status: model.ApprovalPending, ExpiresAt: s.now().Add(approvalTTL)}
	if err := s.repo.CreateApproval(ctx, a); err != nil {
		return nil, err
	}
	s.emit(ctx, run.SessionID, run.ID, run.CanvasID, "approval.created", approvalView(a))
	s.emit(ctx, run.SessionID, run.ID, run.CanvasID, "run.status", map[string]any{"status": next})
	return approvalView(a), nil
}

// Decide 处理用户对审批的决定：批准、拒绝或回答提问。批准后执行（删除或生成），然后让运行继续。
func (s *AgentService) Decide(ctx context.Context, userID, approvalID uint64, req *model.DecideAgentApprovalReq) (*AgentApprovalView, error) {
	// 1. 审批必须是自己的，并且还在等待；已处理、已过期、运行已被停止都按「审批已处理或已过期」
	a, err := s.repo.GetApproval(ctx, userID, approvalID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrAgentApprovalMiss
	}
	if err != nil {
		return nil, err
	}
	run, err := s.repo.GetRun(ctx, userID, a.RunID)
	if err != nil {
		return nil, runErr(err)
	}
	if a.Status != model.ApprovalPending || !s.now().Before(a.ExpiresAt) || !contains(waitingStatuses, run.Status) {
		return nil, errcode.ErrAgentApprovalGone
	}
	// 2. 按种类算出决定：校验选项、预算，得出最终批准的条目
	out, err := s.resolveDecision(a, run, req)
	if err != nil {
		return nil, err
	}
	// 3. 审批状态迁移是 CAS：重复点击、同时在另一个标签页点，只有一次生效
	decision, _ := json.Marshal(out.decision) // map[string]any 的内容都来自已校验的参数，序列化不会失败
	updated, err := s.repo.UpdateApprovalIf(ctx, a.ID, []string{model.ApprovalPending}, map[string]any{
		"status": out.status, "decision_json": decision, "quote_credits": out.quote, "decided_at": s.now(),
	})
	if errors.Is(err, repository.ErrAgentStateConflict) {
		return nil, errcode.ErrAgentApprovalGone
	}
	if err != nil {
		return nil, err
	}
	// 4. 运行回到 running（追加的预算一并生效）；被批准的生成先记入已花积分，防止同一轮反复批准超预算
	fields := map[string]any{"status": model.RunRunning}
	if req.AddBudget > 0 && out.status != model.ApprovalRejected {
		fields["budget_credits"] = run.BudgetCredits + req.AddBudget
	}
	if _, err := s.repo.UpdateRunIf(ctx, run.ID, waitingStatuses, fields); err != nil {
		return nil, errcode.ErrAgentApprovalGone
	}
	if out.quote > 0 {
		if err := s.repo.AddRunUsage(ctx, run.ID, 0, out.quote); err != nil {
			return nil, err
		}
	}
	// 5. 执行批准的内容，把结果写回审批；执行失败不让这次请求失败（决定已生效），Agent 会从结果里知道
	final := s.execute(ctx, run, updated, out)
	s.emit(ctx, run.SessionID, run.ID, run.CanvasID, "approval.decided", approvalView(final))
	// 6. 让运行接着往下走；runtime 不在了就标为中断，用户可以稍后点「继续」
	if err := s.runtime.Resume(ctx, run, "approval"); err != nil {
		_, _ = s.repo.UpdateRunIf(ctx, run.ID, []string{model.RunRunning}, map[string]any{"status": model.RunInterrupted}) // 已经不在 running 说明别处处理过了，无需再改
		s.emit(ctx, run.SessionID, run.ID, run.CanvasID, "run.status", map[string]any{"status": model.RunInterrupted})
	}
	return approvalView(final), nil
}

// resolveDecision 把用户的决定按审批种类解析成 outcome。
func (s *AgentService) resolveDecision(a *model.AgentApproval, run *model.AgentRun, req *model.DecideAgentApprovalReq) (*outcome, error) {
	if req.Decision == "reject" {
		return &outcome{status: model.ApprovalRejected, decision: map[string]any{"decision": "reject"}}, nil
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
	if out.quote > run.BudgetCredits+req.AddBudget-run.SpentCredits {
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

// execute 执行批准的内容并把结果写回审批：删除直接改画布，生成交给 executor。
func (s *AgentService) execute(ctx context.Context, run *model.AgentRun, a *model.AgentApproval, out *outcome) *model.AgentApproval {
	var result json.RawMessage
	var err error
	switch {
	case a.Kind == model.ApprovalDelete && out.status != model.ApprovalRejected:
		var res *WriteResult
		if res, err = s.canvas.DeleteApproved(ctx, run, a.ToolCallID, out.nodeIDs, out.edgeIDs); err == nil {
			result, _ = json.Marshal(res) // WriteResult 只含数字和字符串，序列化不会失败
		}
	case a.Kind == model.ApprovalGenerate && out.status != model.ApprovalRejected && s.exec != nil:
		result, err = s.exec.Execute(ctx, run, a.ID, out.gen)
	default:
		return a
	}
	status, body := model.ApprovalExecuted, result
	if err != nil {
		status = model.ApprovalFailed
		body, _ = json.Marshal(map[string]string{"error": err.Error()}) // 同上，只含字符串
	}
	final, uerr := s.repo.UpdateApprovalIf(ctx, a.ID, []string{model.ApprovalApproved, model.ApprovalPartial}, map[string]any{"status": status, "result_json": body})
	if uerr != nil {
		return a
	}
	return final
}
