package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

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
	s.emit(ctx, run.UserID, run.SessionID, run.ID, run.CanvasID, "approval.created", approvalView(a))
	s.emit(ctx, run.UserID, run.SessionID, run.ID, run.CanvasID, "run.status", statusEvent(next))
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
	// 审批过期了：除了拒绝这次决定，还要把运行迁出等待状态，否则它会一直占着这张画布
	if a.Status == model.ApprovalPending && !s.now().Before(a.ExpiresAt) {
		s.expireWaitingRun(ctx, run, a)
		return nil, errcode.ErrAgentApprovalGone
	}
	if a.Status != model.ApprovalPending || !contains(waitingStatuses, run.Status) {
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
	// 只靠 run.status 事件驱动的客户端，回答之后要靠它才知道运行不再等待了
	s.emit(ctx, run.UserID, run.SessionID, run.ID, run.CanvasID, "run.status", statusEvent(model.RunRunning))
	if out.quote > 0 {
		if err := s.repo.AddRunUsage(ctx, run.ID, 0, out.quote); err != nil {
			return nil, err
		}
		s.emitUsage(ctx, run)
	}
	// 5. 执行批准的内容，把结果写回审批；执行失败不让这次请求失败（决定已生效），Agent 会从结果里知道
	final := s.execute(ctx, run, updated, out)
	// 报价是批准时先记的；没有真正创建出任务的那部分没有扣积分，退回，别让本轮预算被虚占
	if refund := unusedQuote(final, out); refund > 0 {
		if err := s.repo.AddRunUsage(ctx, run.ID, 0, -refund); err == nil {
			s.emitUsage(ctx, run)
		}
	}
	s.emit(ctx, run.UserID, run.SessionID, run.ID, run.CanvasID, "approval.decided", approvalView(final))
	// 6. 让运行接着往下走；runtime 不在了就标为中断，用户可以稍后点「继续」
	if err := s.runtime.Resume(ctx, run, ResumeInfo{Reason: "approval", ToolCallID: a.ToolCallID, Content: approvalOutcomeText(final, out)}); err != nil {
		_, _ = s.repo.UpdateRunIf(ctx, run.ID, []string{model.RunRunning}, map[string]any{"status": model.RunInterrupted}) // 已经不在 running 说明别处处理过了，无需再改
		s.emit(ctx, run.UserID, run.SessionID, run.ID, run.CanvasID, "run.status", statusEvent(model.RunInterrupted))
	}
	return approvalView(final), nil
}

// expireWaitingRun 把等审批超时的运行置为 expired，审批一并置为 expired，并推送 run.status 让客户端知道这一轮结束了。
// 运行已经不在等待状态（刚被停止、刚被决定）就不动它；这些都是尽力而为的收尾，失败只意味着下次再碰到时再处理。
// approval 为 nil 时把这个运行所有待处理的审批都置为 expired。返回运行是不是这次被置为 expired 的。
func (s *AgentService) expireWaitingRun(ctx context.Context, run *model.AgentRun, approval *model.AgentApproval) bool {
	if approval != nil {
		_, _ = s.repo.UpdateApprovalIf(ctx, approval.ID, []string{model.ApprovalPending}, map[string]any{"status": model.ApprovalExpired}) // 已不是 pending 说明别处处理过了
	}
	if _, err := s.repo.UpdateRunIf(ctx, run.ID, waitingStatuses, map[string]any{"status": model.RunExpired, "ended_at": s.now()}); err != nil {
		return false
	}
	if approval == nil {
		_, _ = s.repo.ExpirePendingApprovals(ctx, run.ID) // 同上，尽力而为
	}
	s.emit(ctx, run.UserID, run.SessionID, run.ID, run.CanvasID, "run.status", statusEvent(model.RunExpired))
	return true
}

// expireStaleRun 在画布被占用时调用：占着画布的若是等审批超时的运行，就把它置为 expired，返回 true 表示画布空出来了。
func (s *AgentService) expireStaleRun(ctx context.Context, canvasID uint64) bool {
	run, err := s.repo.ActiveRun(ctx, canvasID)
	if err != nil || !contains(waitingStatuses, run.Status) {
		return false
	}
	approvals, err := s.repo.ListApprovals(ctx, run.ID)
	if err != nil {
		return false
	}
	for _, a := range approvals {
		if a.Status == model.ApprovalPending && !s.now().Before(a.ExpiresAt) {
			return s.expireWaitingRun(ctx, run, nil)
		}
	}
	return false
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
	text := fmt.Sprintf("%s：%s，预估 %d 积分，任务已提交：%s", scope, strings.Join(names, "、"), out.quote, string(a.ResultJSON))
	if warn := bindWarning(a); warn != "" {
		text += "\n注意：" + warn + "。任务已经在生成、积分已冻结，不要重复申请；节点上可能暂时看不到生成中的状态，用 task_get 查进度，结果出来后用户会在画布上看到。"
	}
	return text
}

// unusedQuote 算出批准时先记入已花积分、但没有真正扣掉的那部分，要退回本轮预算：
// 执行整体失败（一个任务都没创建）退全额报价；部分条目失败退这些条目的「价格×张数」。
// 只对生成审批有意义；任务已经创建的部分积分已冻结，保持已花。
func unusedQuote(a *model.AgentApproval, out *outcome) int {
	if a.Kind != model.ApprovalGenerate || out.quote <= 0 {
		return 0
	}
	if a.Status == model.ApprovalFailed {
		return out.quote
	}
	var r struct {
		Failed []struct {
			NodeID string `json:"node_id"`
		} `json:"failed"`
	}
	if json.Unmarshal(a.ResultJSON, &r) != nil || len(r.Failed) == 0 {
		return 0
	}
	failed := make(map[string]bool, len(r.Failed))
	for _, f := range r.Failed {
		failed[f.NodeID] = true
	}
	refund := 0
	for _, it := range out.gen {
		if failed[it.NodeID] {
			refund += it.Price * it.Count
		}
	}
	return min(refund, out.quote)
}

// bindWarning 取审批结果里记录的「任务已创建但绑定节点失败」的警告。
func bindWarning(a *model.AgentApproval) string {
	var r struct {
		BindWarning string `json:"bind_warning"`
	}
	if json.Unmarshal(a.ResultJSON, &r) == nil {
		return r.BindWarning
	}
	return ""
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
