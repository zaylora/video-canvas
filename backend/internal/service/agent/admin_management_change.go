package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/go-playground/validator/v10"

	"video-canvas/internal/imageproc"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/configbody"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

// managementFields 为每个操作限定输入字段，禁止把未知字段、凭证或角色要求塞进签名快照。
func managementFields(target, action string) []string {
	switch target + "/" + action {
	case "register_settings/update":
		return []string{"register_enabled", "initial_credits", "default_max_active_tasks", "queue_multiplier", "queue_max_wait_minutes"}
	case "smtp_settings/update":
		return []string{"host", "port", "encryption", "username", "from_address", "from_name", "enabled"}
	case "smtp_settings/test":
		return []string{"to"}
	case "user/status":
		return []string{"status", "cancel_active"}
	case "user/limit":
		return []string{"max_active_tasks"}
	case "user/credits":
		return []string{"mode", "amount", "note"}
	case "user/role":
		return []string{"role"}
	case "storage/update":
		return []string{"name", "account_id", "region", "endpoint", "bucket", "path_prefix", "addressing", "use_ssl", "public_base_url", "signed_ttl_sec", "direct_upload"}
	case "storage/check", "storage/default", "storage/delete":
		return []string{}
	case "image_processor/create":
		return []string{"name", "vendor", "storage_id", "config"}
	case "image_processor/update":
		return []string{"name", "config"}
	case "image_processor/check", "image_processor/publish", "image_processor/rollback", "image_processor/disable", "image_processor/delete":
		return []string{}
	}
	return nil
}

// buildManagementItem 读取快照并生成可审阅差异，预检不发送邮件、不测试连接、不更改用户。
func (s *AdminAssistantService) buildManagementItem(ctx context.Context, in AdminChangeInput, revert bool) (AdminChangeItem, error) {
	item := AdminChangeItem{Target: in.Target, Key: in.Key, Label: in.Key, Status: "ok", RequiredRole: model.RoleSuperAdmin, BaseSHA256: in.BaseSHA256, Issues: []string{}, Warnings: []string{}, Diff: []AdminChangeDiff{}, Basis: []PricingBasis{}}
	// 1. 先做白名单校验；凭证不能进入审批 payload、数据库或工具返回内容。
	if revert {
		adminItemIssue(&item, "invalid", "该操作不支持自动回滚，请读取当前值后重新申请变更")
		return item, nil
	}
	action, _ := in.Patch["action"].(string)
	if action == "" {
		action = "update"
	}
	item.Action = action
	fields := managementFields(in.Target, action)
	if fields == nil {
		adminItemIssue(&item, "invalid", "不支持的管理操作")
		return item, nil
	}
	values := managementPatch(in, action, fields, &item)
	if len(item.Issues) > 0 {
		return item, nil
	}
	if in.Target == "user" && action != "role" {
		item.RequiredRole = model.RoleAdmin
	}
	// 2. 创建图片服务不需要已有 ID；其他操作必须绑定实际读取的稳定快照。
	before := map[string]any{}
	if action == "create" {
		if in.Key == "" {
			adminItemIssue(&item, "invalid", "创建图片服务需要稳定 key")
			return item, nil
		}
	} else {
		var err error
		before, item.Label, err = s.readManagement(ctx, in.Target, in.Key)
		if err != nil {
			return item, err
		}
		raw, _ := json.Marshal(before)
		if in.BaseSHA256 != configbody.SHA256(raw) {
			adminItemIssue(&item, "stale", "配置已改变，请重新读取后提案")
		}
	}
	after := mergeAdminPatch(before, values)
	// 并发覆盖的 null 表示恢复全局默认，不能被 Merge Patch 删除后误当成未传。
	if in.Target == "user" && action == "limit" {
		after["max_active_tasks"] = values["max_active_tasks"]
	}
	item.Before, _ = json.Marshal(before)
	item.After, _ = json.Marshal(after)
	item.Diff = AdminConfigDiff(before, after)
	if action != "update" {
		item.Diff = append(item.Diff, AdminChangeDiff{Path: "操作", After: managementActionLabel(action)})
	}
	if len(item.Diff) == 0 {
		adminItemIssue(&item, "invalid", "补丁未产生任何字段变化")
	}
	if err := validateManagementInput(in.Target, action, item.After); err != nil {
		adminItemIssue(&item, "invalid", publicAdminError(err))
	}
	item.Warnings = append(item.Warnings, "审批后执行；系统设置和用户变更保存即生效")
	if action == "delete" {
		item.Warnings = append(item.Warnings, "删除后不能自动恢复")
	}
	return item, nil
}

// managementActionLabel 把生命周期操作显示成中文，审批卡仍展示全部参数差异。
func managementActionLabel(action string) string {
	switch action {
	case "create":
		return "新建"
	case "status":
		return "封禁或启用用户"
	case "limit":
		return "设置并发上限"
	case "credits":
		return "调整积分"
	case "role":
		return "调整角色"
	case "test":
		return "发送测试邮件"
	case "check":
		return "测试连接与校验"
	case "default":
		return "设为默认存储"
	case "delete":
		return "删除"
	case "publish":
		return "发布图片服务"
	case "rollback":
		return "回滚图片服务"
	case "disable":
		return "停用图片服务"
	}
	return action
}

// decodeManagementInput 同时复用后台请求的字段类型和 binding 范围，不依赖模型遵守 JSON Schema。
func decodeManagementInput(raw []byte, v any) error {
	if err := json.Unmarshal(raw, v); err != nil {
		return errcode.ErrInvalidParams.WithMsg("管理操作字段类型不正确")
	}
	return validateManagementValue(v)
}

// validateManagementValue 在 HTTP 之外复用后台请求结构的 binding 约束。
func validateManagementValue(v any) error {
	check := validator.New()
	check.SetTagName("binding")
	if err := check.Struct(v); err != nil {
		return errcode.ErrInvalidParams.WithMsg("字段缺失或超出允许范围")
	}
	return nil
}

// processorManagementInput 采用管理页面相同的字段及限制，不接受凭证。
type processorManagementInput struct {
	Name      string           `json:"name" binding:"required,max=64"`
	Vendor    string           `json:"vendor" binding:"required,max=24"`
	StorageID uint64           `json:"storage_id" binding:"required"`
	Version   int              `json:"version"`
	Config    imageproc.Config `json:"config"`
}

// storageManagementInput 对齐存储更新表单；版本只从平台快照取得。
type storageManagementInput struct {
	Name          string `json:"name" binding:"required,max=64"`
	Version       int    `json:"version" binding:"min=1"`
	AccountID     string `json:"account_id"`
	Region        string `json:"region"`
	Endpoint      string `json:"endpoint"`
	Bucket        string `json:"bucket"`
	PathPrefix    string `json:"path_prefix"`
	Addressing    string `json:"addressing"`
	UseSSL        *bool  `json:"use_ssl"`
	PublicBaseURL string `json:"public_base_url"`
	SignedTTLSec  int    `json:"signed_ttl_sec" binding:"min=0"`
	DirectUpload  bool   `json:"direct_upload"`
}

// validateManagementInput 校验审批内容；最终业务校验仍由原服务在执行时完成。
func validateManagementInput(target, action string, raw []byte) error {
	switch target + "/" + action {
	case "register_settings/update":
		return decodeManagementInput(raw, &model.RegisterSettingsView{})
	case "smtp_settings/update":
		return decodeManagementInput(raw, &model.UpdateSMTPSettingsReq{})
	case "smtp_settings/test":
		return decodeManagementInput(raw, &model.TestSMTPReq{})
	case "user/status":
		return decodeManagementInput(raw, &model.SetUserStatusReq{})
	case "user/limit":
		return decodeManagementInput(raw, &model.SetUserLimitReq{})
	case "user/credits":
		return decodeManagementInput(raw, &model.AdjustCreditsReq{})
	case "user/role":
		return decodeManagementInput(raw, &model.SetUserRoleReq{})
	case "storage/update":
		return decodeManagementInput(raw, &storageManagementInput{})
	case "image_processor/create", "image_processor/update":
		var in processorManagementInput
		if err := decodeManagementInput(raw, &in); err != nil {
			return err
		}
		if _, err := imageproc.Normalize(in.Vendor, in.Config); err != nil {
			return errcode.ErrInvalidParams.WithMsg(err.Error())
		}
	}
	return nil
}

// applyManagementItem 先持久化执行占位，再读当前快照执行；崩溃或并发重放不得重复发积分或邮件。
func (s *AdminAssistantService) applyManagementItem(ctx context.Context, rc RunContext, approvalID uint64, p AdminChangePayload, index int, item AdminChangeItem) (uint64, error) {
	// 1. 记录一次性操作。业务服务自带的审计继续保留，助手审计补充审批关联和前后快照。
	store := s.d.Management.Operations
	if store == nil {
		return 0, errcode.ErrAgentUnavailable
	}
	detail, _ := json.Marshal(map[string]any{"via": "assistant", "order_no": p.OrderNo, "approval_id": strconv.FormatUint(approvalID, 10), "session_id": strconv.FormatUint(rc.SessionID, 10), "run_id": strconv.FormatUint(rc.RunID, 10), "index": index, "target": item.Target, "action": item.Action, "before": item.Before, "after": item.After, "diff": item.Diff})
	module := "settings"
	if item.Target == "user" || item.Target == "storage" {
		module = item.Target
	}
	audit := &model.AIAuditLog{ActorID: rc.UserID, Action: module + ".assistant", TargetType: item.Target, TargetKey: item.Key, DetailJSON: model.JSONText(detail)}
	return store.ExecuteAdminOperation(ctx, audit, func() error {
		// 2. 再查角色和快照；普通管理员不能借助手绕过系统设置页的超级管理员限制。
		u, err := s.d.Users.GetByID(ctx, rc.UserID)
		if err != nil {
			return err
		}
		if !adminRoleAllows(u.Role, item.RequiredRole) {
			return errcode.ErrForbidden
		}
		if item.Action != "create" {
			body, _, err := s.readManagement(ctx, item.Target, item.Key)
			if err != nil {
				return err
			}
			raw, _ := json.Marshal(body)
			if configbody.SHA256(raw) != item.BaseSHA256 {
				return repository.ErrRevisionConflict
			}
		}
		if err := validateManagementInput(item.Target, item.Action, item.After); err != nil {
			return err
		}
		return s.executeManagement(ctx, rc.UserID, item)
	})
}

// executeManagement 只分派已批准的固定参数，复用用户保护、SSRF、乐观锁、发布和删除限制。
func (s *AdminAssistantService) executeManagement(ctx context.Context, actorID uint64, item AdminChangeItem) error {
	// 1. 生命周期动作与更新分开分派；版本来自签名快照，不能通过补丁改写。
	d := s.d.Management
	id, _ := strconv.ParseUint(item.Key, 10, 64)
	raw := item.After
	switch item.Target {
	case "register_settings":
		var req model.RegisterSettingsView
		_ = json.Unmarshal(raw, &req)
		_, err := d.Register.UpdateRegisterSettings(ctx, actorID, &req)
		return err
	case "smtp_settings":
		if item.Action == "test" {
			var req model.TestSMTPReq
			_ = json.Unmarshal(raw, &req)
			return d.SMTP.Test(ctx, req.To)
		}
		var req model.UpdateSMTPSettingsReq
		_ = json.Unmarshal(raw, &req)
		_, err := d.SMTP.Update(ctx, actorID, &req)
		return err
	case "user":
		return s.executeManagementUser(ctx, actorID, id, item)
	case "storage":
		return s.executeManagementStorage(ctx, actorID, id, item)
	case "image_processor":
		return s.executeManagementProcessor(ctx, actorID, id, item)
	}
	return errcode.ErrInvalidParams.WithMsg("不支持的管理操作：" + strings.TrimSpace(item.Action))
}

// managementPatch 拒绝未知字段、凭证、无效 null 和缺失的操作参数。
func managementPatch(in AdminChangeInput, action string, fields []string, item *AdminChangeItem) map[string]any {
	values := map[string]any{}
	for k, v := range in.Patch {
		if k == "action" {
			continue
		}
		if !slices.Contains(fields, k) || containsAdminCredential(map[string]any{k: v}) {
			adminItemIssue(item, "invalid", "不允许修改字段："+k)
			continue
		}
		values[k] = v
		if v == nil && (in.Target != "user" || action != "limit" || k != "max_active_tasks") {
			adminItemIssue(item, "invalid", "字段不能为 null："+k)
		}
	}
	for _, required := range map[string][]string{"status": {"status"}, "limit": {"max_active_tasks"}, "role": {"role"}, "credits": {"mode", "amount", "note"}, "test": {"to"}}[action] {
		if _, ok := values[required]; !ok {
			adminItemIssue(item, "invalid", "缺少操作字段："+required)
		}
	}
	return values
}

// executeManagementUser 复用用户管控与部分取消结果。
func (s *AdminAssistantService) executeManagementUser(ctx context.Context, actorID, id uint64, item AdminChangeItem) error {
	d := s.d.Management
	raw := item.After
	switch item.Action {
	case "status":
		var req model.SetUserStatusReq
		_ = json.Unmarshal(raw, &req)
		res, err := d.Users.SetStatus(ctx, actorID, id, req.Status, req.CancelActive)
		if err == nil && (res.CancelFailed > 0 || res.CancelError != "") {
			return errcode.ErrInvalidParams.WithMsg(fmt.Sprintf("用户状态已更改，仍有任务取消失败：%d；%s", res.CancelFailed, res.CancelError))
		}
		return err
	case "limit":
		var req model.SetUserLimitReq
		_ = json.Unmarshal(raw, &req)
		return d.Users.SetMaxActiveTasks(ctx, actorID, id, req.MaxActiveTasks)
	case "credits":
		var req model.AdjustCreditsReq
		_ = json.Unmarshal(raw, &req)
		_, err := d.Users.AdjustCredits(ctx, actorID, id, &req)
		return err
	case "role":
		var req model.SetUserRoleReq
		_ = json.Unmarshal(raw, &req)
		return d.Users.SetRole(ctx, actorID, id, req.Role)
	}

	return errcode.ErrInvalidParams
}

// executeManagementStorage 复用存储版本、连接与删除约束。
func (s *AdminAssistantService) executeManagementStorage(ctx context.Context, actorID, id uint64, item AdminChangeItem) error {
	d := s.d.Management
	raw := item.After
	switch item.Action {
	case "check":
		result, err := d.Storages.Check(ctx, actorID, id)
		if err == nil && !result.OK {
			return errcode.ErrInvalidParams.WithMsg("存储连接测试未通过，请查询存储检查结果")
		}
		return err
	case "default":
		return d.Storages.SetDefault(ctx, actorID, id)
	case "delete":
		return d.Storages.Delete(ctx, actorID, id)
	case "update":
		var r storageManagementInput
		_ = json.Unmarshal(raw, &r)
		_, err := d.Storages.Update(ctx, actorID, id, service.StorageUpdateInput{Version: r.Version, Name: r.Name, AccountID: r.AccountID, Region: r.Region, Endpoint: r.Endpoint, Bucket: r.Bucket, PathPrefix: r.PathPrefix, Addressing: r.Addressing, UseSSL: r.UseSSL, PublicBaseURL: r.PublicBaseURL, SignedTTLSec: r.SignedTTLSec, DirectUpload: r.DirectUpload})
		return err
	}

	return errcode.ErrInvalidParams
}

// executeManagementProcessor 复用图片草稿与发布生命周期。
func (s *AdminAssistantService) executeManagementProcessor(ctx context.Context, actorID, id uint64, item AdminChangeItem) error {
	d := s.d.Management
	raw := item.After
	var r processorManagementInput
	_ = json.Unmarshal(raw, &r)
	switch item.Action {
	case "create":
		if d.Processors == nil {
			return errcode.ErrAgentUnavailable
		}
		_, err := d.Processors.Create(ctx, actorID, service.ProcessorCreateInput{Name: r.Name, Vendor: r.Vendor, StorageID: r.StorageID, Config: r.Config})
		return err
	case "update":
		_, err := d.Processors.Update(ctx, actorID, id, service.ProcessorUpdateInput{Version: r.Version, Name: r.Name, Config: r.Config})
		return err
	case "check":
		result, err := d.Processors.Check(ctx, actorID, id)
		if err == nil && (result.Check == nil || !result.Check.OK) {
			return errcode.ErrInvalidParams.WithMsg("图片服务校验未通过，请查询图片服务检查结果")
		}
		return err
	case "publish":
		_, err := d.Processors.Publish(ctx, actorID, id, r.Version)
		return err
	case "rollback":
		_, err := d.Processors.Rollback(ctx, actorID, id)
		return err
	case "disable":
		_, err := d.Processors.Disable(ctx, actorID, id)
		return err
	case "delete":
		return d.Processors.Delete(ctx, actorID, id)
	}
	return errcode.ErrInvalidParams
}
