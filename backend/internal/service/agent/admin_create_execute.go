package agent

import (
	"context"
	"encoding/json"
	"strconv"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

// applyCreateItem 复用 Upload / Create / SaveModel，每条写入与助手审计独立原子提交。
func (s *AdminAssistantService) applyCreateItem(ctx context.Context, rc RunContext, approvalID uint64, p AdminChangePayload, index int, item AdminChangeItem, results []AdminChangeItemResult) (uint64, *AdminCreated, error) {
	detail := map[string]any{"via": "assistant", "order_no": p.OrderNo, "session_id": strconv.FormatUint(rc.SessionID, 10), "run_id": strconv.FormatUint(rc.RunID, 10), "approval_id": strconv.FormatUint(approvalID, 10), "index": index, "target": item.Target, "diff": item.Diff, "before": nil}
	// 1. 代码只在审批中；审计仅记录摘要与创建标识，避免复制草稿和潜在敏感内容。
	var after map[string]any
	if err := json.Unmarshal(item.After, &after); err != nil {
		return 0, nil, err
	}
	shown := mergeAdminPatch(nil, after)
	delete(shown, "code")
	detail["after"] = shown
	raw, _ := json.Marshal(detail)
	audit := &model.AIAuditLog{ActorID: rc.UserID, TargetKey: item.Key, DetailJSON: model.JSONText(raw)}
	ctx = repository.WithAssistantAudit(ctx, audit)
	created := &AdminCreated{}
	switch item.Target {
	case "plugin":
		audit.Action = model.AuditPluginUpload
		audit.TargetType = model.AuditTargetPluginVer
		code, _ := after["code"].(string)
		res, err := s.d.PluginWriter.Upload(ctx, rc.UserID, []byte(code))
		if err != nil {
			return 0, nil, err
		}
		if !res.Accepted || res.Version == nil {
			return 0, nil, errcode.ErrConfigInvalid.WithMsg("插件上传未通过预检或没有产生新版本")
		}
		created.PluginVersion = res.Version.Version
	case "channel":
		key, err := s.createApprovedChannel(ctx, rc.UserID, item, audit)
		if err != nil {
			return 0, nil, err
		}
		created.ChannelKey = key
	case "model_create":
		audit.Action = model.AuditModelUpdate
		audit.TargetType = "model"
		if err := s.resolveCreateVersions(ctx, after, p.Items, item.DependsOn, results); err != nil {
			return 0, nil, err
		}
		after["enabled"] = false
		body, _ := json.Marshal(after)
		created.ModelKey = item.Key
		detail["created"] = created
		detail["after"] = after
		auditRaw, _ := json.Marshal(detail)
		audit.DetailJSON = model.JSONText(auditRaw)
		enabled := false
		res, err := s.d.Writer.SaveModel(ctx, service.ModelSaveInput{Key: item.Key, Create: true, Body: body, AdminID: rc.UserID, Note: p.Title, Enabled: &enabled, Audit: audit})
		if err != nil {
			return 0, nil, err
		}
		created.ModelKey = res.Key
	}
	return audit.ID, created, nil
}

// resolveCreateVersions 使用本单依赖实际上传的版本；复用已有插件时再读取最新不可变版本。
func (s *AdminAssistantService) resolveCreateVersions(ctx context.Context, after map[string]any, items []AdminChangeItem, deps []int, results []AdminChangeItemResult) error {
	channels, _ := after["channels"].([]any)
	for _, v := range channels {
		ch, _ := v.(map[string]any)
		protocol, _ := ch["protocol"].(map[string]any)
		if protocol == nil {
			continue
		}
		version, _ := protocol["version"].(string)
		if version != "latest" {
			continue
		}
		key, _ := protocol["plugin"].(string)
		resolved := ""
		for _, dep := range deps {
			if items[dep].Target == "plugin" && items[dep].Key == key && results[dep].Created != nil {
				resolved = results[dep].Created.PluginVersion
			}
		}
		if resolved == "" {
			_, _, v, err := s.pluginVersion(ctx, key, "")
			if err != nil {
				return err
			}
			resolved = v
		}
		protocol["version"] = resolved
	}
	return nil
}

// createApprovedChannel 复用渠道创建校验，凭证不在创建参数里。
func (s *AdminAssistantService) createApprovedChannel(ctx context.Context, userID uint64, item AdminChangeItem, audit *model.AIAuditLog) (string, error) {
	audit.Action = model.AuditChannelCreate
	audit.TargetType = model.AuditTargetChannel
	var body struct {
		Name      string             `json:"name"`
		BaseURL   string             `json:"base_url"`
		Headers   map[string]string  `json:"headers"`
		RateLimit provider.RateLimit `json:"rate_limit"`
		Enabled   *bool              `json:"enabled"`
	}
	if err := json.Unmarshal(item.After, &body); err != nil {
		return "", errcode.ErrInvalidParams
	}
	enabled := body.Enabled == nil || *body.Enabled
	res, err := s.d.ChannelWriter.Create(ctx, service.ChannelCreateInput{Key: item.Key, Name: body.Name, BaseURL: body.BaseURL, Headers: body.Headers, RateLimit: body.RateLimit, Enabled: enabled, ActorID: userID})
	if err != nil {
		return "", err
	}
	return res.Key, nil
}
