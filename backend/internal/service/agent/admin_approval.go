package agent

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

// adminProposalSignature 绑定运行身份和全部展示、执行快照。
func (b *AgentBridge) adminProposalSignature(rc RunContext, p AdminChangePayload) string {
	p.Signature = ""
	return b.dramaMAC(rc.run(), "admin_change", "admin-proposal", p)
}

// checkAdminPayload 拒绝跨运行、跨用户以及被篡改的变更单。
func (b *AgentBridge) checkAdminPayload(rc RunContext, p AdminChangePayload) error {
	if rc.Mode != model.AgentModeAdmin || len(b.d.DramaApprovalKey) == 0 || p.Signature == "" || !hmac.Equal([]byte(p.Signature), []byte(b.adminProposalSignature(rc, p))) {
		return errcode.ErrInvalidParams.WithMsg("后台变更单已改变或模式不正确，请重新申请审批")
	}
	return nil
}

// adminOutcomeSignature 将逐条决定一起签名，阻止执行时扩大批准范围。
func (b *AgentBridge) adminOutcomeSignature(rc RunContext, o ApprovalOutcome) string {
	o.AdminSignature = ""
	return b.dramaMAC(rc.run(), "admin_change", "admin-outcome", o)
}

// resolveAdminApproval 解析整单或逐条决定；无效或角色不足的条目留给执行阶段报告 skipped。
func (b *AgentBridge) resolveAdminApproval(rc RunContext, raw json.RawMessage, req model.DecideAgentApprovalReq) (*ApprovalOutcome, error) {
	var p AdminChangePayload
	if json.Unmarshal(raw, &p) != nil {
		return nil, errcode.ErrInvalidParams.WithMsg("后台变更单格式不正确")
	}
	if err := b.checkAdminPayload(rc, p); err != nil {
		return nil, err
	}
	if req.Decision != "approve" && req.Decision != "reject" {
		return nil, errcode.ErrInvalidParams.WithMsg("decision 只能是 approve 或 reject")
	}
	chosen := make([]bool, len(p.Items))
	if req.Decision == "approve" && len(req.Items) == 0 {
		for i := range chosen {
			chosen[i] = true
		}
	}
	seen := map[int]bool{}
	for _, item := range req.Items {
		if item.Index < 0 || item.Index >= len(chosen) || seen[item.Index] {
			return nil, errcode.ErrInvalidParams.WithMsg("变更条目 index 越界或重复")
		}
		seen[item.Index] = true
		chosen[item.Index] = item.Approve
	}
	o := &ApprovalOutcome{Status: model.ApprovalApproved, Decision: map[string]any{"decision": req.Decision, "selected": chosen, "answer": req.Answer}, AdminChange: &p}
	if req.Decision == "reject" {
		o.Status = model.ApprovalRejected
	} else {
		for _, yes := range chosen {
			if !yes {
				o.Status = model.ApprovalPartial
				break
			}
		}
	}
	o.AdminSignature = b.adminOutcomeSignature(rc, *o)
	return o, nil
}

// executeAdminApproval 验签后逐条执行，成功条目不因其他条目失败而回滚。
func (b *AgentBridge) executeAdminApproval(ctx context.Context, rc RunContext, approvalID uint64, o ApprovalOutcome) (*ApprovalExecution, error) {
	if o.AdminChange == nil || o.AdminSignature == "" || !hmac.Equal([]byte(o.AdminSignature), []byte(b.adminOutcomeSignature(rc, o))) {
		return nil, errcode.ErrInvalidParams.WithMsg("后台变更单决定已改变，请重新申请审批")
	}
	if err := b.checkAdminPayload(rc, *o.AdminChange); err != nil {
		return nil, err
	}
	if b.d.Admin == nil {
		return nil, errcode.ErrAgentUnavailable
	}
	result, err := b.d.Admin.ExecuteChanges(ctx, rc, approvalID, *o.AdminChange, o)
	if err != nil {
		return nil, err
	}
	applied := 0
	for _, item := range result.Items {
		if item.Status == "applied" {
			applied++
		}
	}
	status := ExecFailed
	switch {
	case o.Status == model.ApprovalRejected:
		status = model.ApprovalRejected
	case applied == len(result.Items):
		status = ExecExecuted
	case applied > 0:
		status = model.ApprovalPartial
	}
	raw, _ := json.Marshal(result)
	return &ApprovalExecution{Status: status, Result: raw, OutcomeText: fmt.Sprintf("变更单 %s：%d/%d 项已应用。逐条结果：%s", o.AdminChange.OrderNo, applied, len(result.Items), raw)}, nil
}

// ExecuteChanges 执行已验签的固定快照并实时查角色；模型与定价原子写入，管理操作持久化占位后复查快照。
func (s *AdminAssistantService) ExecuteChanges(ctx context.Context, rc RunContext, approvalID uint64, p AdminChangePayload, o ApprovalOutcome) (*AdminChangeResult, error) {
	// 1. 已成功项以原子审计为准；重放不重新创建或重复写审计。
	prior, err := s.d.Audit.ChangeAudits(ctx, p.OrderNo)
	if err != nil {
		return nil, err
	}
	replay := replayAuditIDs(prior, approvalID)
	chosenRaw, _ := json.Marshal(o.Decision["selected"])
	var chosen []bool
	_ = json.Unmarshal(chosenRaw, &chosen)
	result := &AdminChangeResult{Items: make([]AdminChangeItemResult, len(p.Items)), AuditIDs: []uint64{}, AppliedAt: s.d.Now().UTC().Format(time.RFC3339)}
	order, graphErr := adminTopologicalOrder(p.Items)
	if graphErr != nil {
		for i := range p.Items {
			order = append(order, i)
		}
	}
	// 2. 按拓扑逐条提交，公开结果仍保持原始下标顺序。
	for _, i := range order {
		if id, ok := replay[i]; ok {
			result.Items[i] = AdminChangeItemResult{Index: i, Status: "applied"}
			if isAdminCreateTarget(p.Items[i].Target) {
				result.Items[i].Created = replayAdminCreated(prior, id)
			}
			result.AuditIDs = append(result.AuditIDs, id)
			continue
		}
		user, err := s.d.Users.GetByID(ctx, rc.UserID)
		if err != nil {
			return nil, err
		}
		result.AppliedBy = user.Username
		selected := o.Status != model.ApprovalRejected && i < len(chosen) && chosen[i]
		item, id, err := s.executeChangeItem(ctx, rc, approvalID, p, i, result.Items, user.Role, selected, graphErr)
		if err != nil {
			return nil, err
		}
		result.Items[i] = item
		if id > 0 {
			result.AuditIDs = append(result.AuditIDs, id)
		}
	}
	if result.AppliedBy == "" {
		if user, err := s.d.Users.GetByID(ctx, rc.UserID); err == nil {
			result.AppliedBy = user.Username
		}
	}
	return result, nil
}

// executeChangeItem 将拒绝、无效图、依赖和实时角色检查集中在写入前。
func (s *AdminAssistantService) executeChangeItem(ctx context.Context, rc RunContext, approvalID uint64, p AdminChangePayload, i int, results []AdminChangeItemResult, role string, selected bool, graphErr error) (AdminChangeItemResult, uint64, error) {
	item := p.Items[i]
	out := AdminChangeItemResult{Index: i, Status: "rejected"}
	if !selected {
		return out, 0, nil
	}
	out.Status = "skipped"
	switch {
	case graphErr != nil:
		out.Message = graphErr.Error()
	case adminDependencyFailure(item, results) != "":
		out.Message = adminDependencyFailure(item, results)
	case item.Status != "ok":
		out.Message = "条目校验未通过：" + item.Status
	case !adminRoleAllows(role, item.RequiredRole):
		out.Message = "当前角色不足，需要 " + item.RequiredRole
	default:
		return s.commitChangeItem(ctx, rc, approvalID, p, i, results)
	}
	return out, 0, nil
}

// commitChangeItem 写入失败后再对账审计，覆盖并发重放先提交成功的窗口。
func (s *AdminAssistantService) commitChangeItem(ctx context.Context, rc RunContext, approvalID uint64, p AdminChangePayload, i int, results []AdminChangeItemResult) (AdminChangeItemResult, uint64, error) {
	out := AdminChangeItemResult{Index: i, Status: "applied"}
	var id uint64
	var err error
	item := p.Items[i]
	if isAdminCreateTarget(item.Target) {
		id, out.Created, err = s.applyCreateItem(ctx, rc, approvalID, p, i, item, results)
	} else {
		id, err = s.applyChangeItem(ctx, rc, approvalID, p, i, item)
	}
	if ctx.Err() != nil {
		return out, 0, ctx.Err()
	}
	if err != nil {
		if rows, readErr := s.d.Audit.ChangeAudits(ctx, p.OrderNo); readErr == nil {
			if done, ok := replayAuditIDs(rows, approvalID)[i]; ok {
				out.Created = replayAdminCreated(rows, done)
				return out, done, nil
			}
		}
		out.Status = "failed"
		out.Message = publicAdminError(err)
		out.Created = nil
		return out, 0, nil
	}
	return out, id, nil
}

// adminDependencyFailure 只允许依赖实际 applied 的条目，权限不足、拒绝和失败均阻止后继项。
func adminDependencyFailure(item AdminChangeItem, results []AdminChangeItemResult) string {
	for _, dep := range item.DependsOn {
		if results[dep].Status != "applied" {
			return fmt.Sprintf("依赖的第 %d 项未应用", dep+1)
		}
	}
	return ""
}

// replayAdminCreated 从首次成功审计恢复版本和 key，不能拿当前最新版本代替首次结果。
func replayAdminCreated(rows []repository.AssistantAuditRecord, id uint64) *AdminCreated {
	for _, row := range rows {
		if row.ID == id {
			var detail struct {
				Created *AdminCreated `json:"created"`
			}
			_ = json.Unmarshal(row.Detail, &detail)
			return detail.Created
		}
	}
	return nil
}

// replayAuditIDs 只将本次审批已经落库的条目标为成功，防止关联到同单的其他审批。
func replayAuditIDs(rows []repository.AssistantAuditRecord, approvalID uint64) map[int]uint64 {
	out := map[int]uint64{}
	for _, row := range rows {
		var detail struct {
			ApprovalID string `json:"approval_id"`
			Index      int    `json:"index"`
			State      string `json:"state"`
		}
		if json.Unmarshal(row.Detail, &detail) == nil && (detail.State == "" || detail.State == "applied") && detail.ApprovalID == strconv.FormatUint(approvalID, 10) {
			out[detail.Index] = row.ID
		}
	}
	return out
}

// adminRoleAllows 保证普通用户和空角色不能满足管理员权限。
func adminRoleAllows(role, required string) bool {
	return role == model.RoleSuperAdmin || (role == model.RoleAdmin && required == model.RoleAdmin)
}

// applyChangeItem 分派管理操作或模型、定价变更；模型与定价的审计失败和 SHA 冲突会回滚该条。
func (s *AdminAssistantService) applyChangeItem(ctx context.Context, rc RunContext, approvalID uint64, p AdminChangePayload, index int, item AdminChangeItem) (uint64, error) {
	if isManagementTarget(item.Target) {
		return s.applyManagementItem(ctx, rc, approvalID, p, index, item)
	}
	detail := map[string]any{"via": "assistant", "order_no": p.OrderNo, "session_id": strconv.FormatUint(rc.SessionID, 10), "run_id": strconv.FormatUint(rc.RunID, 10), "approval_id": strconv.FormatUint(approvalID, 10), "index": index, "diff": item.Diff, "before": item.Before, "after": item.After}
	switch item.Target {
	case "pricing_settings":
		if p.RevertOf != "" {
			_, id, err := s.d.Settings.Restore(ctx, rc.UserID, item.After, item.BaseSHA256, detail)
			return id, err
		}
		var req model.PricingSettingsReq
		if err := json.Unmarshal(item.After, &req); err != nil {
			return 0, err
		}
		_, id, err := s.d.Settings.Update(ctx, rc.UserID, req, item.BaseSHA256, detail)
		return id, err
	case "model":
		var after map[string]any
		if err := json.Unmarshal(item.After, &after); err != nil {
			return 0, err
		}
		enabled, _ := after["enabled"].(bool)
		raw, _ := json.Marshal(detail)
		audit := &model.AIAuditLog{ActorID: rc.UserID, Action: model.AuditModelUpdate, TargetType: "model", TargetKey: item.Key, DetailJSON: model.JSONText(raw)}
		_, err := s.d.Writer.SaveModel(ctx, service.ModelSaveInput{Key: item.Key, Body: item.After, AdminID: rc.UserID, Note: p.Title, BaseSHA256: item.BaseSHA256, ExpectedEnabled: &item.Live, Enabled: &enabled, Audit: audit})
		return audit.ID, err
	}
	return 0, errcode.ErrInvalidParams
}
