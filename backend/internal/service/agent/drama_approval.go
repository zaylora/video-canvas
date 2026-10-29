package agent

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
)

// isDramaApproval 识别四个必须整体审批的短剧写工具。
func isDramaApproval(kind string) bool {
	switch kind {
	case "drama_update_outline", "drama_update_episode", "drama_update_segment", "drama_run_job":
		return true
	}
	return false
}

// dramaMAC 使用稳定服务密钥和运行身份签名；不同模式、用户或运行不能复用批准内容。
func (b *AgentBridge) dramaMAC(run *model.AgentRun, kind, domain string, value any) string {
	mac := hmac.New(sha256.New, b.d.DramaApprovalKey)
	_, _ = fmt.Fprintf(mac, "%s:%d:%d:%d:%d:%s:%s:", domain, run.UserID, run.CanvasID, run.SessionID, run.ID, run.Mode, kind)
	body, _ := json.Marshal(value) // 签名对象仅包含可序列化的审批结构。
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// dramaSignature 忽略签名自身后计算审批内容摘要。
func (b *AgentBridge) dramaSignature(run *model.AgentRun, kind string, p DramaApprovalPayload) string {
	p.Signature = ""
	return b.dramaMAC(run, kind, "proposal", p)
}

// checkDramaPayload 拒绝修改审批原文、切换运行身份和普通模式执行。
func (b *AgentBridge) checkDramaPayload(ctx context.Context, rc RunContext, kind string, p DramaApprovalPayload) (*model.DramaView, error) {
	if rc.Mode != model.AgentModeDrama || len(b.d.DramaApprovalKey) == 0 || p.Signature == "" || !hmac.Equal([]byte(p.Signature), []byte(b.dramaSignature(rc.run(), kind, p))) {
		return nil, errcode.ErrInvalidParams.WithMsg("短剧审批内容已改变或模式不正确，请重新申请审批")
	}
	project, err := b.dramaProject(ctx, rc.UserID, rc.CanvasID)
	if err != nil {
		return nil, err
	}
	if project.ID != p.DramaID {
		return nil, errcode.ErrDramaNotFound
	}
	return project, nil
}

// resolveDramaApproval 只允许整体批准或拒绝，并签名解析结果，执行时不能换回旧参数或修改积分。
func (b *AgentBridge) resolveDramaApproval(ctx context.Context, rc RunContext, kind string, raw json.RawMessage, req model.DecideAgentApprovalReq) (*ApprovalOutcome, error) {
	var p DramaApprovalPayload
	if json.Unmarshal(raw, &p) != nil {
		return nil, errcode.ErrInvalidParams.WithMsg("短剧审批内容格式不对")
	}
	if _, err := b.checkDramaPayload(ctx, rc, kind, p); err != nil {
		return nil, err
	}
	if len(req.Items) > 0 || (req.Decision != "approve" && req.Decision != "reject") {
		return nil, errcode.ErrInvalidParams.WithMsg("短剧修改只支持整体批准或拒绝")
	}
	o := &ApprovalOutcome{Status: model.ApprovalApproved, Decision: map[string]any{"decision": req.Decision}, QuoteCredits: p.Estimate, Drama: &p}
	if req.Decision == "reject" {
		o.Status = model.ApprovalRejected
		o.QuoteCredits = 0
		if req.Answer != "" {
			o.Decision["answer"] = req.Answer
		}
	} else if rc.BudgetCredits > 0 {
		run, err := b.platformRunState(ctx, rc)
		if err != nil {
			return nil, err
		}
		if p.Estimate > run.BudgetCredits+req.AddBudget-run.SpentCredits {
			return nil, errcode.ErrAgentOverBudget
		}
	}
	o.DramaSignature = b.dramaOutcomeSignature(rc.run(), kind, *o)
	return o, nil
}

// dramaOutcomeSignature 绑定用户决定，阻止将拒绝结果篡改成批准。
func (b *AgentBridge) dramaOutcomeSignature(run *model.AgentRun, kind string, o ApprovalOutcome) string {
	o.DramaSignature = ""
	return b.dramaMAC(run, kind, "outcome", o)
}

// executeDramaApproval 校验签名后只执行批准的固定内容；所有业务检查仍由原短剧服务执行。
func (b *AgentBridge) executeDramaApproval(ctx context.Context, rc RunContext, kind string, approvalID uint64, o ApprovalOutcome) (*ApprovalExecution, error) {
	if o.Drama == nil || o.DramaSignature == "" || !hmac.Equal([]byte(o.DramaSignature), []byte(b.dramaOutcomeSignature(rc.run(), kind, o))) {
		return nil, errcode.ErrInvalidParams.WithMsg("短剧审批决定已改变，请重新申请审批")
	}
	project, err := b.checkDramaPayload(ctx, rc, kind, *o.Drama)
	if err != nil {
		return nil, err
	}
	if o.Status == model.ApprovalRejected {
		return &ApprovalExecution{Status: ExecNone, OutcomeText: "用户拒绝了这次短剧操作，没有修改或提交任务。"}, nil
	}
	if o.Status != model.ApprovalApproved {
		return nil, errcode.ErrInvalidParams.WithMsg("短剧审批状态不对")
	}
	value, err := b.executeDramaWrite(ctx, rc, kind, approvalID, project, *o.Drama)
	if err != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		// 与工具入口一致只公开业务错误，避免把数据库或服务细节写进审批结果。
		text := b.finishTool(nil, err).Content
		body, _ := json.Marshal(map[string]string{"error": text})
		return &ApprovalExecution{Status: ExecFailed, Result: body, OutcomeText: "短剧操作执行失败：" + text}, nil
	}
	body, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return &ApprovalExecution{Status: ExecExecuted, Result: body, OutcomeText: "短剧操作已执行：" + string(body)}, nil
}

// executeDramaWrite 使用审批时的版本；仅已达到同一版本后相同最终值视为安全重放。
func (b *AgentBridge) executeDramaWrite(ctx context.Context, rc RunContext, kind string, approvalID uint64, p *model.DramaView, a DramaApprovalPayload) (any, error) {
	switch kind {
	case "drama_update_outline":
		var req model.UpdateDramaReq
		if json.Unmarshal(a.Args, &req) != nil {
			return nil, errcode.ErrInvalidParams
		}
		if p.Revision == req.Revision+1 && sameDramaText(req.OutlineMD, p.OutlineMD) && sameDramaText(req.CharactersMD, p.CharactersMD) {
			return p, nil
		}
		return b.d.Drama.Update(ctx, rc.UserID, uint64(p.ID), &req)
	case "drama_update_episode":
		return b.executeDramaEpisode(ctx, rc, p, a)
	case "drama_update_segment":
		return b.executeDramaSegment(ctx, rc, p, a)
	case "drama_run_job":
		var req model.DramaJobReq
		if json.Unmarshal(a.Args, &req) != nil {
			return nil, errcode.ErrInvalidParams
		}
		return b.d.Drama.CreateAgentJob(ctx, rc.UserID, uint64(p.ID), approvalID, &req)
	}
	return nil, errcode.ErrInvalidParams
}

// sameDramaText 未出现的字段不参与重放比较。
func sameDramaText(want *string, got string) bool { return want == nil || *want == got }

// executeDramaEpisode 锁定和版本冲突在批准后再次由分集服务校验。
func (b *AgentBridge) executeDramaEpisode(ctx context.Context, rc RunContext, p *model.DramaView, a DramaApprovalPayload) (any, error) {
	var req model.UpdateDramaEpisodeReq
	if json.Unmarshal(a.Args, &req) != nil {
		return nil, errcode.ErrInvalidParams
	}
	ep, err := b.d.Drama.GetEpisode(ctx, rc.UserID, uint64(p.ID), a.EpisodeNo)
	if err != nil {
		return nil, err
	}
	if ep.Revision == req.Revision+1 && sameDramaText(req.Title, ep.Title) && sameDramaText(req.Synopsis, ep.Synopsis) && sameDramaText(req.ScriptMD, ep.ScriptMD) {
		return ep, nil
	}
	return b.d.Drama.UpdateEpisode(ctx, rc.UserID, uint64(p.ID), a.EpisodeNo, &req)
}

// executeDramaSegment 重查项目内归属，防止审批后片段被删除或替换时修改其他数据。
func (b *AgentBridge) executeDramaSegment(ctx context.Context, rc RunContext, p *model.DramaView, a DramaApprovalPayload) (any, error) {
	var req model.UpdateDramaSegmentReq
	if json.Unmarshal(a.Args, &req) != nil {
		return nil, errcode.ErrInvalidParams
	}
	seg, err := b.dramaSegment(ctx, rc.UserID, p, uint64(a.SegmentID))
	if err != nil {
		return nil, err
	}
	if seg.Revision == req.Revision+1 && sameDramaText(req.Prompt, seg.Prompt) && (req.Seconds == nil || *req.Seconds == seg.Seconds) {
		return seg, nil
	}
	return b.d.DramaSegments.Update(ctx, rc.UserID, uint64(a.SegmentID), &req)
}
