package agent

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/configbody"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/pkg/pagination"
)

// AdminOperationStore 持久化执行占位，避免积分调整、测试邮件等在进程重启后重复执行。
type AdminOperationStore interface {
	// ExecuteAdminOperation 先登记审批条目，再执行；中断项必须人工核对，不能自动重试。
	ExecuteAdminOperation(context.Context, *model.AIAuditLog, func() error) (uint64, error)
}

// AdminManagementDeps 复用管理页面的业务服务；所有写入仍须经过签名审批和实时角色检查。
type AdminManagementDeps struct {
	Users      AdminManagedUsers
	Register   AdminManagedRegister
	SMTP       AdminManagedSMTP
	Storages   AdminManagedStorages
	Processors AdminManagedProcessors
	Queue      AdminManagedQueue
	Usage      AdminManagedUsage
	Logs       AdminManagedLogs
	Runs       AdminManagedRuns
	Operations AdminOperationStore
}

// adminQuery 是只读工具的筛选参数，分页最多 100 条；详情只能读取现有后台公开视图。
type adminQuery struct {
	Resource    string `json:"resource"`
	Q           string `json:"q"`
	Status      string `json:"status"`
	Role        string `json:"role"`
	UserID      uint64 `json:"user_id"`
	Kind        string `json:"kind"`
	Model       string `json:"model"`
	From        string `json:"from"`
	To          string `json:"to"`
	Actor       string `json:"actor"`
	Module      string `json:"module"`
	Action      string `json:"action"`
	Scene       string `json:"scene"`
	Feedback    string `json:"feedback"`
	Page        int    `json:"page"`
	PageSize    int    `json:"page_size"`
	Minutes     int    `json:"minutes"`
	IncludeTest bool   `json:"include_test"`
}

// toolAdminQuery 查询运营记录和可管理资源，不提供任意 URL 或数据库查询能力。
func (b *AgentBridge) toolAdminQuery(ctx context.Context, tc *toolCall) (*toolOut, error) {
	var req adminQuery
	if json.Unmarshal(tc.args, &req) != nil {
		return nil, fail("参数格式不正确")
	}
	if err := validateManagementQuery(req); err != nil {
		return nil, err
	}
	v, err := b.d.Admin.queryManagement(ctx, tc.run.UserID, req)
	if err != nil {
		return nil, err
	}
	return adminToolOut(v)
}

// validateManagementQuery 补上 HTTP 表单入口原有的筛选校验，不能依赖模型端 Schema。
func validateManagementQuery(r adminQuery) error {
	var value any
	switch r.Resource {
	case "users":
		value = &model.ListAdminUserReq{Q: r.Q, Status: r.Status, Role: r.Role}
	case "usage", "usage_summary":
		value = &model.UsageFilterReq{Q: r.Q, Status: r.Status, Kind: r.Kind, Model: r.Model, From: r.From, To: r.To}
	case "audits":
		value = &model.ListAuditReq{Actor: r.Actor, Module: r.Module, Action: r.Action, From: r.From, To: r.To}
	case "storages":
		value = &model.ListStorageReq{AdminListReq: model.AdminListReq{Q: r.Q}}
	case "image_processors":
		value = &model.ListProcessorReq{AdminListReq: model.AdminListReq{Q: r.Q}, Status: r.Status}
	default:
		return nil
	}
	return validateManagementValue(value)
}

// queryManagement 将有限筛选映射到现有服务，沿用其时间范围、分页和业务错误。
func (s *AdminAssistantService) queryManagement(ctx context.Context, actorID uint64, r adminQuery) (any, error) {
	// 1. 缺省页码和条数与后台一致，拒绝过大请求，避免向模型倾倒全站记录。
	if r.Page < 0 || r.PageSize < 0 || r.PageSize > 100 {
		return nil, errcode.ErrInvalidParams
	}
	if r.Page == 0 {
		r.Page = 1
	}
	if r.PageSize == 0 {
		r.PageSize = 20
	}
	d := s.d.Management
	if d == nil {
		return nil, errcode.ErrAgentUnavailable
	}
	if !map[string]bool{
		"queue": d.Queue != nil, "queue_throughput": d.Queue != nil, "users": d.Users != nil,
		"usage": d.Usage != nil, "usage_summary": d.Usage != nil, "audits": d.Logs != nil, "runs": d.Runs != nil,
		"storages": d.Storages != nil, "storage_presets": d.Storages != nil, "image_processors": d.Processors != nil, "image_processor_presets": d.Processors != nil,
	}[r.Resource] {
		return nil, errcode.ErrInvalidParams.WithMsg("查询资源不支持或未配置")
	}
	switch r.Resource {
	case "queue":
		return d.Queue.Overview(ctx)
	case "queue_throughput":
		return d.Queue.Throughput(ctx, r.Minutes)
	case "users":
		rows, total, q, err := d.Users.List(ctx, &model.ListAdminUserReq{Q: r.Q, Status: r.Status, Role: r.Role, Page: r.Page, PageSize: r.PageSize})
		return pagination.Result[model.AdminUserListItem]{List: rows, Total: total, Page: q.Page, PageSize: q.PageSize}, err
	case "usage", "usage_summary":
		f := model.UsageFilterReq{Q: r.Q, UserID: r.UserID, Status: r.Status, Kind: r.Kind, Model: r.Model, From: r.From, To: r.To, IncludeTest: r.IncludeTest}
		if r.Resource == "usage_summary" {
			return d.Usage.Summary(ctx, &f)
		}
		return d.Usage.List(ctx, &model.ListUsageReq{UsageFilterReq: f, Page: r.Page, PageSize: r.PageSize})
	case "audits":
		return d.Logs.List(ctx, &model.ListAuditReq{Actor: r.Actor, Module: r.Module, Action: r.Action, From: r.From, To: r.To, Page: r.Page, PageSize: r.PageSize})
	case "runs":
		return s.queryManagementRuns(ctx, actorID, r)
	case "storages":
		return d.Storages.List(ctx, &model.ListStorageReq{AdminListReq: model.AdminListReq{Q: r.Q, Page: r.Page, PageSize: r.PageSize}})
	case "image_processors":
		return d.Processors.List(ctx, &model.ListProcessorReq{AdminListReq: model.AdminListReq{Q: r.Q, Page: r.Page, PageSize: r.PageSize}, Status: r.Status})
	case "image_processor_presets":
		return d.Processors.Presets(), nil
	case "storage_presets":
		return d.Storages.Presets(), nil
	}
	return nil, errcode.ErrInvalidParams.WithMsg("查询资源不支持或未配置")
}

// queryManagementRuns 校验时间范围后查询运行摘要。
func (s *AdminAssistantService) queryManagementRuns(ctx context.Context, actorID uint64, r adminQuery) (any, error) {
	f := &RunListFilter{Scene: r.Scene, Feedback: r.Feedback, UserID: r.UserID, ModelKey: r.Model, Page: r.Page, Limit: r.PageSize}
	if r.Status != "" {
		f.Statuses = []string{r.Status}
	}
	var err error
	if r.From != "" {
		f.Since, err = time.Parse(time.RFC3339, r.From)
		if err != nil {
			return nil, errcode.ErrInvalidParams.WithMsg("开始时间格式不正确")
		}
	}
	if r.To != "" {
		f.Until, err = time.Parse(time.RFC3339, r.To)
		if err != nil {
			return nil, errcode.ErrInvalidParams.WithMsg("结束时间格式不正确")
		}
	}
	if !f.Since.IsZero() && !f.Until.IsZero() && !f.Since.Before(f.Until) {
		return nil, errcode.ErrInvalidParams.WithMsg("开始时间必须早于结束时间")
	}
	return s.d.Management.Runs.ListRuns(ctx, ObserveActor{UserID: actorID}, f)
}

// toolAdminGetResource 返回安全视图和编辑快照，模型不得自行计算摘要或读取凭证明文。
func (b *AgentBridge) toolAdminGetResource(ctx context.Context, tc *toolCall) (*toolOut, error) {
	var r struct {
		Target string `json:"target"`
		Key    string `json:"key"`
	}
	if json.Unmarshal(tc.args, &r) != nil {
		return nil, fail("参数格式不正确")
	}
	body, label, err := b.d.Admin.readManagement(ctx, r.Target, r.Key)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(body)
	return adminToolOut(map[string]any{"body": body, "label": label, "body_sha256": configbody.SHA256(raw)})
}

// readManagement 只返回业务服务已脱敏的视图，排除会随查询变化的统计字段以形成稳定快照。
func (s *AdminAssistantService) readManagement(ctx context.Context, target, key string) (map[string]any, string, error) {
	// 1. 读取现有后台服务的安全视图。
	value, label, err := s.managementValue(ctx, target, key)
	if err != nil {
		return nil, "", err
	}
	if value == nil {
		return nil, "", errcode.ErrAgentUnavailable
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, "", err
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, "", err
	}
	// 2. 用户只需身份、状态、积分和并发；不把登录统计、审计正文带进审批和模型。
	switch target {
	case "user":
		body = map[string]any{"id": body["id"], "username": body["username"], "role": body["role"], "status": body["status"], "balance": body["balance"], "frozen": body["frozen"], "max_active_tasks": body["max_active_tasks"]}
		label, _ = body["username"].(string)
	case "storage", "image_processor":
		label, _ = body["name"].(string)
	}
	for _, k := range []string{"updated_at", "created_at", "last_check_at", "last_check_ok", "last_check_error", "check", "asset_count", "access_key_id"} {
		delete(body, k)
	}
	return body, label, nil
}

// isManagementTarget 标识需复用后台业务服务的审批条目。
func isManagementTarget(target string) bool {
	return target == "user" || target == "register_settings" || target == "smtp_settings" || target == "storage" || target == "image_processor"
}

// managementValue 只允许固定设置 key 和已有对象 ID，复用管理服务的安全视图。
func (s *AdminAssistantService) managementValue(ctx context.Context, target, key string) (any, string, error) {
	// 1. 系统设置使用固定 key；对象 ID 只接受后台的正整数 ID。
	d := s.d.Management
	if d == nil {
		return nil, "", errcode.ErrAgentUnavailable
	}
	var value any
	var err error
	label := ""
	switch target {
	case "register_settings":
		if key != "register" || d.Register == nil {
			return nil, "", errcode.ErrInvalidParams
		}
		value, err = d.Register.RegisterSettings(ctx)
		label = "注册设置"
	case "smtp_settings":
		if key != "smtp" || d.SMTP == nil {
			return nil, "", errcode.ErrInvalidParams
		}
		value, err = d.SMTP.Get(ctx)
		label = "邮件服务"
	case "user", "storage", "image_processor":
		value, err = s.managementObjectValue(ctx, target, key)
	default:
		return nil, "", errcode.ErrInvalidParams.WithMsg("不支持的管理资源")
	}
	return value, label, err
}

// managementObjectValue 按正整数 ID 查询一个用户、存储或图片服务。
func (s *AdminAssistantService) managementObjectValue(ctx context.Context, target, key string) (any, error) {
	d := s.d.Management
	var value any
	var err error
	id, e := strconv.ParseUint(key, 10, 64)
	if e != nil || id == 0 {
		return nil, errcode.ErrInvalidParams.WithMsg("需要正整数对象 ID")
	}
	switch target {
	case "user":
		if d.Users != nil {
			value, err = d.Users.Get(ctx, id)
		}
	case "storage":
		if d.Storages != nil {
			value, err = d.Storages.Get(ctx, id)
		}
	case "image_processor":
		if d.Processors != nil {
			value, err = d.Processors.Get(ctx, id)
		}
	}

	return value, err
}
