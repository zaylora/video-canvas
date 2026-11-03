package agent

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/provider/modelcfg"
)

// AdminActionPayload 同时承载两类卡片字段，Input 和 Signature 绑定实际批准的试跑参数。
// 密钥只从原有 HTTP 接口写入，任何卡片字段都不接收明文凭证。
type AdminActionPayload struct {
	ChannelKey       string             `json:"channel_key,omitempty"`
	ChannelLabel     string             `json:"channel_label,omitempty"`
	Exists           *bool              `json:"exists,omitempty"`
	SecretConfigured *bool              `json:"secret_configured,omitempty"`
	ModelKey         string             `json:"model_key,omitempty"`
	ModelLabel       string             `json:"model_label,omitempty"`
	InputSummary     string             `json:"input_summary,omitempty"`
	Estimate         *AdminTestEstimate `json:"estimate,omitempty"`
	Attempt          *int               `json:"attempt,omitempty"`
	AttemptsLeft     *int               `json:"attempts_left,omitempty"`
	Input            map[string]any     `json:"input,omitempty"`
	Reason           string             `json:"reason"`
	RequiredRole     string             `json:"required_role"`
	Signature        string             `json:"signature"`
}

// AdminTestEstimate 成本价预估，缺少成本或换算比例时使用 null。
type AdminTestEstimate struct {
	Credits *int     `json:"credits"`
	Yuan    *float64 `json:"yuan"`
}

// actionSignature 绑定卡片全部内容和运行身份，防止客户端更换渠道或试跑输入。
func (b *AgentBridge) actionSignature(rc RunContext, kind string, p AdminActionPayload) string {
	p.Signature = ""
	return b.dramaMAC(rc.run(), kind, "admin-action", p)
}

// toolAdminRequestSecret 只读取渠道和凭证元信息，渠道尚未创建也能展示禁用卡片。
func (b *AgentBridge) toolAdminRequestSecret(ctx context.Context, tc *toolCall) (*toolOut, error) {
	var req struct {
		ChannelKey string `json:"channel_key"`
		Reason     string `json:"reason"`
	}
	if json.Unmarshal(tc.args, &req) != nil || req.ChannelKey == "" || strings.TrimSpace(req.Reason) == "" {
		return nil, fail("需要 channel_key 和 reason")
	}
	exists, label, configured, err := b.d.Admin.channelSecretState(ctx, req.ChannelKey)
	if err != nil {
		return nil, err
	}
	p := AdminActionPayload{ChannelKey: req.ChannelKey, ChannelLabel: label, Exists: &exists, SecretConfigured: &configured, Reason: req.Reason, RequiredRole: model.RoleSuperAdmin}
	return b.proposeAdminAction(ctx, tc, "admin_secret", p, "请在密钥卡片填写渠道凭证。")
}

// channelSecretState 实时查库，只返回是否设置，从不读取密文或解密凭证。
func (s *AdminAssistantService) channelSecretState(ctx context.Context, key string) (bool, string, bool, error) {
	rows, err := s.d.Channels.ListChannels(ctx)
	if err != nil {
		return false, "", false, err
	}
	exists, label := false, key
	for _, ch := range rows {
		if ch.Key == key {
			exists = true
			label = ch.Name
			if label == "" {
				label = key
			}
			break
		}
	}
	secrets, err := s.d.Models.ListSecrets(ctx)
	if err != nil {
		return false, "", false, err
	}
	configured := false
	for _, secret := range secrets {
		if secret.Name == model.ChannelSecretName(key) {
			configured = true
		}
	}
	return exists, label, configured, nil
}

// toolAdminTestRun 校验真实输入并预估成本，提案阶段绝不创建任务或发网络请求。
func (b *AgentBridge) toolAdminTestRun(ctx context.Context, tc *toolCall) (*toolOut, error) {
	var req struct {
		ModelKey string         `json:"model_key"`
		Input    map[string]any `json:"input"`
		Reason   string         `json:"reason"`
	}
	if json.Unmarshal(tc.args, &req) != nil || req.ModelKey == "" || req.Input == nil || strings.TrimSpace(req.Reason) == "" {
		return nil, fail("需要 model_key、input 和 reason")
	}
	if containsAdminCredential(req.Input) {
		return nil, fail("试跑 input 不能包含凭证")
	}
	body, _, _, err := b.d.Admin.ReadModel(ctx, req.ModelKey)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(body)
	cfg, issues := modelcfg.ParseModel(raw)
	if cfg == nil || len(issues) > 0 {
		return nil, fail("模型配置尚未通过校验")
	}
	input, inputIssues := modelcfg.ValidateInput(cfg.Kind, cfg.Capabilities, req.Input)
	if len(inputIssues) > 0 {
		return nil, fail("试跑输入未通过校验：%s", modelcfg.JoinFieldErrors(inputIssues))
	}
	attempt, err := b.d.Admin.claimAttempt(ctx, tc.run.ID, "test_run", req.ModelKey)
	if err != nil {
		return nil, err
	}
	if attempt == 0 {
		return nil, fail("已达试跑上限")
	}
	left := 3 - attempt
	estimate, err := b.d.Admin.estimateAdminTrial(ctx, cfg, input)
	if err != nil {
		return nil, err
	}
	prompt, _ := input["prompt"].(string)
	p := AdminActionPayload{ModelKey: req.ModelKey, ModelLabel: cfg.Label, Input: input, InputSummary: fmt.Sprintf("%s · %v · 提示词 %d 字", cfg.Kind, input["op"], len([]rune(prompt))), Estimate: estimate, Attempt: &attempt, AttemptsLeft: &left, Reason: req.Reason, RequiredRole: model.RoleAdmin}
	return b.proposeAdminAction(ctx, tc, "admin_test_run", p, "请在试跑卡片同意真实调用，可能产生上游费用。")
}

// proposeAdminAction 只接受 Go 生成的安全字段；卡片签名缺失时不生成可执行审批。
func (b *AgentBridge) proposeAdminAction(ctx context.Context, tc *toolCall, kind string, p AdminActionPayload, text string) (*toolOut, error) {
	if len(b.d.DramaApprovalKey) == 0 {
		return nil, fail("后台审批签名未配置")
	}
	rc := RunContext{RunID: tc.run.ID, SessionID: tc.run.SessionID, UserID: tc.run.UserID, Mode: tc.run.Mode}
	p.Signature = b.actionSignature(rc, kind, p)
	if err := b.requestApproval(ctx, tc, kind, p, 0); err != nil {
		return nil, err
	}
	return &toolOut{content: text, summary: text, terminate: true}, nil
}

// checkAdminAction 验证类型、身份与卡片签名，不允许在普通画布运行执行。
func (b *AgentBridge) checkAdminAction(rc RunContext, kind string, p AdminActionPayload) error {
	if rc.Mode != model.AgentModeAdmin || len(b.d.DramaApprovalKey) == 0 || p.Signature == "" || !hmac.Equal([]byte(p.Signature), []byte(b.actionSignature(rc, kind, p))) {
		return errcode.ErrInvalidParams.WithMsg("后台审批内容已改变或模式不正确")
	}
	return nil
}

// adminActionOutcomeSignature 绑定用户决定，执行方不能扩大授权范围。
func (b *AgentBridge) adminActionOutcomeSignature(rc RunContext, kind string, o ApprovalOutcome) string {
	o.AdminActionSignature = ""
	return b.dramaMAC(rc.run(), kind, "admin-action-outcome", o)
}

// resolveAdminAction 只持久化固定决定文字，任意 answer 都不得成为密钥输入通道。
func (b *AgentBridge) resolveAdminAction(ctx context.Context, rc RunContext, kind string, raw json.RawMessage, req model.DecideAgentApprovalReq) (*ApprovalOutcome, error) {
	// 1. 决定阶段和执行阶段均查询实时角色，JWT 中的旧权限不能放行。
	if err := b.requireAdminRun(ctx, rc); err != nil {
		return nil, err
	}
	var p AdminActionPayload
	if json.Unmarshal(raw, &p) != nil {
		return nil, errcode.ErrInvalidParams
	}
	if err := b.checkAdminAction(rc, kind, p); err != nil {
		return nil, err
	}
	if len(req.Items) > 0 || (req.Decision != "approve" && req.Decision != "reject") {
		return nil, errcode.ErrInvalidParams.WithMsg("只支持整体批准或拒绝")
	}
	if kind == "admin_secret" && req.Decision == "approve" && req.Answer != "configured" {
		return nil, errcode.ErrInvalidParams.WithMsg("密钥决定只接受 answer=configured")
	}
	o := &ApprovalOutcome{Status: model.ApprovalApproved, AdminAction: &p, Decision: map[string]any{"decision": req.Decision}}
	if req.Decision == "reject" {
		o.Status = model.ApprovalRejected
	} else if kind == "admin_secret" {
		o.Decision["answer"] = "configured"
	}
	o.AdminActionSignature = b.adminActionOutcomeSignature(rc, kind, *o)
	return o, nil
}

// executeAdminAction 验签后执行卡片；密钥批准只确认保存状态，试跑才创建真实任务。
func (b *AgentBridge) executeAdminAction(ctx context.Context, rc RunContext, kind string, approvalID uint64, o ApprovalOutcome) (*ApprovalExecution, error) {
	// 1. 卡片和决定双重验签，真实角色不满足时不得发起任何外部操作。
	if o.AdminAction == nil || o.AdminActionSignature == "" || !hmac.Equal([]byte(o.AdminActionSignature), []byte(b.adminActionOutcomeSignature(rc, kind, o))) {
		return nil, errcode.ErrInvalidParams.WithMsg("后台审批决定已改变")
	}
	if err := b.checkAdminAction(rc, kind, *o.AdminAction); err != nil {
		return nil, err
	}
	if o.Status == model.ApprovalRejected {
		if kind == "admin_secret" {
			return adminActionExecution(model.ApprovalRejected, map[string]any{"configured": false}, "用户已跳过密钥配置。"), nil
		}
		return &ApprovalExecution{Status: ExecNone, OutcomeText: "用户拒绝了真实试跑，没有提交任务。"}, nil
	}
	u, err := b.d.Admin.d.Users.GetByID(ctx, rc.UserID)
	if err != nil {
		return nil, err
	}
	if !adminRoleAllows(u.Role, o.AdminAction.RequiredRole) {
		message := "当前角色不足，需要 " + o.AdminAction.RequiredRole
		if kind == "admin_secret" {
			return adminActionExecution(ExecFailed, map[string]any{"configured": false}, message), nil
		}
		return adminActionExecution(ExecFailed, map[string]any{"test_run_id": "", "status": "failed", "error": message}, message), nil
	}
	if kind == "admin_secret" {
		exists, _, configured, err := b.d.Admin.channelSecretState(ctx, o.AdminAction.ChannelKey)
		if err != nil {
			return nil, err
		}
		if !exists || !configured {
			return adminActionExecution(ExecFailed, map[string]any{"configured": false}, "密钥未保存"), nil
		}
		return adminActionExecution(ExecExecuted, map[string]any{"configured": true}, "密钥已配置。"), nil
	}
	return b.executeAdminTestRun(ctx, rc, approvalID, *o.AdminAction)
}

// adminActionExecution 固定 JSON 结果与给模型的安全文字，绝不透传底层错误正文。
func adminActionExecution(status string, result any, text string) *ApprovalExecution {
	raw, _ := json.Marshal(result)
	return &ApprovalExecution{Status: status, Result: raw, OutcomeText: text}
}

// executeAdminTestRun 使用审批 ID 幂等创建原有试跑，按状态对账直到终态或十分钟。
func (b *AgentBridge) executeAdminTestRun(ctx context.Context, rc RunContext, approvalID uint64, p AdminActionPayload) (*ApprovalExecution, error) {
	s := b.d.Admin
	view, err := s.d.Trials.TestRunIdempotent(ctx, rc.UserID, p.ModelKey, p.Input, "admin-trial-"+strconv.FormatUint(approvalID, 10))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return adminActionExecution(ExecFailed, map[string]any{"test_run_id": "", "status": "failed", "error": publicAdminError(err)}, "试跑失败："+publicAdminError(err)), nil
	}
	view, timedOut, err := s.waitAdminTrial(ctx, rc.UserID, view)
	if err != nil {
		return nil, err
	}
	status := "failed"
	execution := ExecFailed
	if timedOut {
		status = "timeout"
		execution = ExecExecuted
	} else if view.Status == model.TaskSucceeded {
		status = "succeeded"
		execution = ExecExecuted
	}
	result := map[string]any{"test_run_id": idcodec.Encode(view.ID), "status": status}
	if status == "failed" {
		result["error"] = view.ErrorCode + "：" + view.ErrorMessage
	}
	if len(view.Outputs) > 0 && view.Outputs[0].URL != "" {
		result["output_url"] = view.Outputs[0].URL
	}
	summary := s.testTraceSummary(ctx, rc.UserID, view.ID)
	if timedOut {
		summary = "等待超过 10 分钟，任务仍在运行。" + summary
	}
	raw, _ := json.Marshal(result)
	return adminActionExecution(execution, result, "试跑结果："+string(raw)+"；错误分类："+view.ErrorCode+"；"+summary), nil
}

// testTraceSummary 仅返回钩子是否成功、HTTP 状态码和前 20 条已脱敏日志，不复制请求报文。
func (s *AdminAssistantService) testTraceSummary(ctx context.Context, userID, taskID uint64) string {
	trace, err := s.d.Trials.GetTestTrace(ctx, userID, taskID)
	if err != nil {
		return "追踪暂不可用，请稍后查看。"
	}
	lines := []string{}
	logs := 0
	for _, step := range trace.Steps {
		if step.Hook != nil {
			lines = append(lines, fmt.Sprintf("%s 成功=%t", step.Hook.Name, step.Error == ""))
			for _, log := range step.Hook.Logs {
				if logs < 20 {
					lines = append(lines, log)
					logs++
				}
			}
		}
		if step.Response != nil {
			lines = append(lines, fmt.Sprintf("HTTP %d", step.Response.Status))
		}
	}
	return strings.Join(lines, "；")
}

// estimateAdminTrial 只按成本积分计算试跑预估，缺少成本或积分比例时保持 null。
func (s *AdminAssistantService) estimateAdminTrial(ctx context.Context, cfg *modelcfg.ModelConfig, input map[string]any) (*AdminTestEstimate, error) {
	estimate := &AdminTestEstimate{}
	if cfg.Pricing.Cost != nil && cfg.Pricing.Cost.On {
		cost := cfg.Pricing
		cost.Unit = cfg.Pricing.Cost.Unit
		cost.PerSecond = cfg.Pricing.Cost.PerSecond
		cost.Token = cfg.Pricing.Cost.Token
		cost.Tiers = append([]modelcfg.Tier(nil), cfg.Pricing.Tiers...)
		for i := range cost.Tiers {
			if c := cost.Tiers[i].Cost; c != nil && c.On {
				cost.Tiers[i].Unit = c.Unit
				if cfg.Pricing.Billing == modelcfg.BillingPerSecond {
					cost.Tiers[i].Unit = c.PerSecond
				}
			} else {
				cost.Tiers[i].On = false
			}
		}
		credits := modelcfg.Quote(cost, cfg.Capabilities, modelcfg.SpecFromInput(input, "")) * modelcfg.FanoutCount(cfg.Capabilities, input)
		estimate.Credits = &credits
		settings, err := s.d.Settings.Get(ctx)
		if err != nil {
			return nil, err
		}
		if settings.CreditsPerYuan != nil {
			yuan := float64(credits) / *settings.CreditsPerYuan
			estimate.Yuan = &yuan
		}
	}
	return estimate, nil
}

// waitAdminTrial 对账原有试跑任务；十分钟从创建时间算起，重放不能重新开始计时。
func (s *AdminAssistantService) waitAdminTrial(ctx context.Context, userID uint64, view *model.GenerationTaskView) (*model.GenerationTaskView, bool, error) {
	deadline := view.CreatedAt.Add(10 * time.Minute)
	if view.CreatedAt.IsZero() {
		deadline = s.d.Now().Add(10 * time.Minute)
	}
	// 2. 不取消超时后的上游任务；模型得到 timeout 结论并保留可继续查询的试跑 ID。
	waitCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	timedOut := false
	for !model.IsTerminalStatus(view.Status) {
		select {
		case <-waitCtx.Done():
			if ctx.Err() != nil {
				return view, false, ctx.Err()
			}
			timedOut = true
		case <-ticker.C:
			var err error
			next, pollErr := s.d.Trials.GetTestRun(waitCtx, userID, view.ID)
			err = pollErr
			if err != nil {
				if waitCtx.Err() != nil {
					continue
				}
				return view, false, err
			}
			if next != nil {
				view = next
			}
		}
		if timedOut {
			break
		}
	}
	return view, timedOut, nil
}
