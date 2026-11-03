package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/configbody"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/service"
)

// requireAdminRun 在平台入口使用数据库角色做后台授权。
func (b *AgentBridge) requireAdminRun(ctx context.Context, rc RunContext) error {
	if rc.Mode != "admin" {
		return nil
	}
	if b.d.Admin == nil {
		return fail("后台助手未配置")
	}
	return b.d.Admin.RequireAdmin(ctx, rc.UserID)
}

// toolAdminListModels 返回后台模型概要，可按类型筛选，不发出渠道凭证。
func (b *AgentBridge) toolAdminListModels(ctx context.Context, tc *toolCall) (*toolOut, error) {
	var req struct {
		Kind    string `json:"kind"`
		Enabled *bool  `json:"enabled"`
		Channel string `json:"channel"`
		Q       string `json:"q"`
	}
	if json.Unmarshal(tc.args, &req) != nil {
		return nil, fail("参数格式不正确")
	}
	rows, err := b.d.Admin.d.Models.LoadModels(ctx)
	if err != nil {
		return nil, err
	}
	usage, err := b.d.Admin.d.Audit.RecentModelUsage(ctx, b.d.Admin.d.Now().Add(-7*24*time.Hour))
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for _, row := range rows {
		if req.Kind != "" && row.Kind != req.Kind {
			continue
		}
		var body map[string]any
		_ = json.Unmarshal(row.Body, &body)
		if req.Enabled != nil && row.Enabled != *req.Enabled {
			continue
		}
		var header struct {
			Channels []struct {
				Channel string `json:"channel"`
			} `json:"channels"`
		}
		_ = json.Unmarshal(row.Body, &header)
		channel := ""
		if len(header.Channels) > 0 {
			channel = header.Channels[0].Channel
		}
		if req.Channel != "" && channel != req.Channel {
			continue
		}
		if req.Q != "" && !strings.Contains(strings.ToLower(row.Key+" "+fmt.Sprint(body["label"])), strings.ToLower(req.Q)) {
			continue
		}
		pricing, _ := body["pricing"].(map[string]any)
		cost, _ := pricing["cost"].(map[string]any)
		out = append(out, map[string]any{"key": row.Key, "kind": row.Kind, "enabled": row.Enabled, "label": body["label"], "channel": channel, "billing": pricing["billing"], "pricing": body["pricing"], "cost_configured": cost["on"] == true, "calls_7d": usage[row.Key]})
	}
	return adminToolOut(out)
}

// toolAdminGetModel 给出原始配置和用于提案的摘要。
func (b *AgentBridge) toolAdminGetModel(ctx context.Context, tc *toolCall) (*toolOut, error) {
	var req struct {
		Key string `json:"key"`
	}
	if json.Unmarshal(tc.args, &req) != nil || req.Key == "" {
		return nil, fail("需要模型 key")
	}
	body, sha, live, err := b.d.Admin.ReadModel(ctx, req.Key)
	if err != nil {
		return nil, err
	}
	return adminToolOut(map[string]any{"body": body, "body_sha256": sha, "live": live})
}

// toolAdminListChannels 只投影安全的渠道概要，HeadersJSON 可能包含密钥，绝不下发。
func (b *AgentBridge) toolAdminListChannels(ctx context.Context, _ *toolCall) (*toolOut, error) {
	rows, err := b.d.Admin.d.Channels.ListChannels(ctx)
	if err != nil {
		return nil, err
	}
	secrets, err := b.d.Admin.d.Models.ListSecrets(ctx)
	if err != nil {
		return nil, err
	}
	configured := map[string]bool{}
	for _, secret := range secrets {
		configured[secret.Name] = true
	}
	out := []map[string]any{}
	for _, row := range rows {
		out = append(out, map[string]any{"key": row.Key, "name": row.Name, "base_url": row.BaseURL, "enabled": row.Enabled, "has_key": configured[model.ChannelSecretName(row.Key)], "secret_configured": configured[model.ChannelSecretName(row.Key)]})
	}
	return adminToolOut(out)
}

// toolAdminGetPricing 返回定价规则和只包含人工设置字段的摘要。
func (b *AgentBridge) toolAdminGetPricing(ctx context.Context, _ *toolCall) (*toolOut, error) {
	v, err := b.d.Admin.d.Settings.Get(ctx)
	if err != nil {
		return nil, err
	}
	return adminToolOut(map[string]any{"settings": v, "body_sha256": configbody.SHA256(service.PricingSettingsBody(v))})
}

// toolAdminQuotePreview 补齐算价占位提示词，再使用现有输入校验和 quote.go 预览积分，不创建任务或审批。
func (b *AgentBridge) toolAdminQuotePreview(ctx context.Context, tc *toolCall) (*toolOut, error) {
	var req struct {
		Key   string          `json:"key"`
		Body  json.RawMessage `json:"body"`
		Input map[string]any  `json:"input"`
	}
	if json.Unmarshal(tc.args, &req) != nil {
		return nil, fail("参数格式不正确")
	}
	if len(req.Body) == 0 {
		body, _, _, err := b.d.Admin.ReadModel(ctx, req.Key)
		if err != nil {
			return nil, err
		}
		req.Body, _ = json.Marshal(body)
	}
	cfg, issues := modelcfg.ParseModel(req.Body)
	if len(issues) > 0 || cfg == nil {
		return nil, fail("模型配置未通过校验：%v", issues)
	}
	// 算价允许省略提示词；用单字占位兼容最小长度上限，其余默认值和校验仍由模型能力决定。
	if req.Input == nil {
		req.Input = map[string]any{}
	}
	if prompt, ok := req.Input["prompt"].(string); req.Input["prompt"] == nil || (ok && strings.TrimSpace(prompt) == "") {
		req.Input["prompt"] = "估"
	}
	normalized, errors := modelcfg.ValidateInput(cfg.Kind, cfg.Capabilities, req.Input)
	if len(errors) > 0 {
		return nil, fail("输入未通过校验：%v", errors)
	}
	spec := modelcfg.SpecFromInput(normalized, "")
	perTask := modelcfg.Quote(cfg.Pricing, cfg.Capabilities, spec)
	count := modelcfg.FanoutCount(cfg.Capabilities, normalized)
	return adminToolOut(map[string]any{"per_task_credits": perTask, "count": count, "quote_credits": perTask * count, "billing": cfg.Pricing.Billing, "input": normalized})
}

// toolAdminPropose 只有至少一项可应用才出审批卡，所有快照由平台签名。
func (b *AgentBridge) toolAdminPropose(ctx context.Context, tc *toolCall) (*toolOut, error) {
	if len(b.d.DramaApprovalKey) == 0 {
		return nil, fail("后台审批签名未配置")
	}
	var req AdminChangeRequest
	if json.Unmarshal(tc.args, &req) != nil {
		return nil, fail("参数格式不正确")
	}
	p, err := b.d.Admin.buildChanges(ctx, req, tc.run.ID)
	if err != nil {
		return nil, err
	}
	applicable := 0
	for _, item := range p.Items {
		if item.Status == "ok" {
			applicable++
		}
	}
	if applicable == 0 {
		lines := []string{"所有条目均不可应用，请重新读取并修正："}
		for _, item := range p.Items {
			// 错误只返回修正所需信息，避免完整配置快照挤占模型上下文。
			raw, _ := json.Marshal(struct {
				Key      string   `json:"key"`
				Status   string   `json:"status"`
				Issues   []string `json:"issues"`
				Warnings []string `json:"warnings,omitempty"`
			}{item.Key, item.Status, item.Issues, item.Warnings})
			lines = append(lines, string(raw))
		}
		// 逐项摘要已精简，使用业务错误保留全部条目，避免通用工具错误截断最后一行。
		return nil, errcode.ErrInvalidParams.WithMsg(strings.Join(lines, "\n"))
	}
	rc := RunContext{RunID: tc.run.ID, SessionID: tc.run.SessionID, UserID: tc.run.UserID, CanvasID: tc.run.CanvasID, Mode: tc.run.Mode}
	p.Signature = b.adminProposalSignature(rc, *p)
	if err := b.requestApproval(ctx, tc, "admin_change", p, 0); err != nil {
		return nil, err
	}
	return &toolOut{content: "变更单已提交，等待用户决定。", summary: fmt.Sprintf("%s（%d 项可应用）", p.Title, applicable), terminate: true}, nil
}

// adminToolOut 将只读结果序列化成模型可用的文本。
func adminToolOut(value any) (*toolOut, error) {
	text, err := jsonOut(value)
	return &toolOut{content: text}, err
}
