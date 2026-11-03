package agent

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/configbody"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

// RequireAdmin 实时读取数据库角色，保护所有后台读工具与运行准备。
func (s *AdminAssistantService) RequireAdmin(ctx context.Context, userID uint64) error {
	// 1. 不接受请求头中的角色作为业务授权依据，降权立即阻止下一次调用。
	u, err := s.d.Users.GetByID(ctx, userID)
	if errors.Is(err, repository.ErrNotFound) {
		return errcode.ErrForbidden
	}
	if err != nil {
		return err
	}
	if u.Role != model.RoleAdmin && u.Role != model.RoleSuperAdmin {
		return errcode.ErrForbidden
	}
	return nil
}

// ReadModel 返回实际启用状态覆盖后的配置正文以及稳定摘要。
func (s *AdminAssistantService) ReadModel(ctx context.Context, key string) (map[string]any, string, bool, error) {
	// 1. 正文和指针都是后台配置，只返回明确挑选的非密钥字段。
	rev, err := s.d.Models.GetModelConfig(ctx, key)
	if err != nil {
		return nil, "", false, err
	}
	ptr, err := s.d.Models.GetModelPointer(ctx, key)
	if err != nil {
		return nil, "", false, err
	}
	var body map[string]any
	if json.Unmarshal(rev.BodyJSON, &body) != nil || body == nil {
		return nil, "", false, errcode.ErrConfigInvalid
	}
	body["enabled"] = ptr.Enabled
	raw, _ := json.Marshal(body)
	return body, configbody.SHA256(raw), ptr.Enabled, nil
}

// BuildChanges 逐条读取并预检提案；问题条目仍保留，调用方判断是否还有可审批的条目。
func (s *AdminAssistantService) BuildChanges(ctx context.Context, req AdminChangeRequest) (*AdminChangePayload, error) {
	// 1. 兼容已有调用；平台工具另传运行 ID 以关联静态检查。
	return s.buildChanges(ctx, req, 0)
}

// buildChanges 把依赖验证与运行内静态检查绑定到同一张单。
func (s *AdminAssistantService) buildChanges(ctx context.Context, req AdminChangeRequest, runID uint64) (*AdminChangePayload, error) {
	// 1. 回滚只能来自已应用审计，模型不能伪造 before 或绕过价格依据校验。
	if req.RevertOf != "" {
		var err error
		req, err = s.reverseRequest(ctx, req.RevertOf)
		if err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(req.Title) == "" || strings.TrimSpace(req.Reason) == "" || len(req.Items) == 0 || len(req.Items) > 50 {
		return nil, errcode.ErrInvalidParams.WithMsg("变更单需要 title、reason 和 1–50 项 items")
	}
	order, err := s.newOrderNo(ctx)
	if err != nil {
		return nil, err
	}
	p := &AdminChangePayload{OrderNo: order, Title: req.Title, Reason: req.Reason, RevertOf: req.RevertOf, Items: make([]AdminChangeItem, 0, len(req.Items))}
	// 2. 一单内同一个目标只允许一次更新，避免条目互相制造 SHA 冲突。
	seen := map[string]bool{}
	for _, input := range req.Items {
		var item AdminChangeItem
		var err error
		if isManagementTarget(input.Target) {
			item, err = s.buildManagementItem(ctx, input, req.RevertOf != "")
		} else if isAdminCreateTarget(input.Target) {
			item, err = s.buildCreateItem(ctx, input, req.RevertOf != "", runID)
		} else {
			item, err = s.buildItem(ctx, input, req.RevertOf != "")
		}
		item.DependsOn = input.DependsOn
		if err != nil {
			return nil, err
		}
		key := input.Target + ":" + input.Key
		if seen[key] {
			item.Status = "invalid"
			item.Issues = append(item.Issues, "同一目标不能在同一单重复修改")
		}
		seen[key] = true
		p.Items = append(p.Items, item)
	}
	validateAdminDependencies(p.Items)
	return p, nil
}

// newOrderNo 生成六位随机短号；锁内查审计并永久保留本实例已发号码，避免并发及待审批提案碰撞。
func (s *AdminAssistantService) newOrderNo(ctx context.Context) (string, error) {
	s.orderMu.Lock()
	defer s.orderMu.Unlock()
	prefix := "CO-" + s.d.Now().Format("060102") + "-"
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		var nonce [3]byte
		if _, err := io.ReadFull(s.orderRandom, nonce[:]); err != nil {
			return "", err
		}
		order := prefix + strings.ToUpper(hex.EncodeToString(nonce[:]))
		if _, exists := s.orderNos[order]; exists {
			continue
		}
		audits, err := s.d.Audit.ChangeAudits(ctx, order)
		if err != nil {
			return "", err
		}
		if len(audits) > 0 {
			continue
		}
		s.orderNos[order] = struct{}{}
		return order, nil
	}
}

// buildItem 先比快照，再应用受限补丁、验证配置并复算价格。
func (s *AdminAssistantService) buildItem(ctx context.Context, input AdminChangeInput, revert bool) (AdminChangeItem, error) {
	item := AdminChangeItem{Target: input.Target, Key: input.Key, Label: input.Key, Status: "ok", RequiredRole: model.RoleAdmin, Issues: []string{}, Warnings: []string{}, Diff: []AdminChangeDiff{}, Basis: []PricingBasis{}, BaseSHA256: input.BaseSHA256}
	settings, err := s.d.Settings.Get(ctx)
	if err != nil {
		return item, err
	}
	before, sha, err := s.loadChangeBase(ctx, &item, settings)
	if err != nil {
		return item, err
	}
	if sha != "" && input.BaseSHA256 != sha {
		item.Status = "stale"
		item.Issues = append(item.Issues, "配置已被修改，请重新读取后提案")
	}
	for key := range input.Patch {
		if !allowedAdminField(input.Target, key) {
			item.Issues = append(item.Issues, "不允许修改字段："+key)
		}
	}
	after := mergeAdminPatch(before, input.Patch)
	if input.Target == "pricing_settings" {
		restoreNullableSettings(after)
	}
	item.Before, _ = json.Marshal(before)
	item.After, _ = json.Marshal(after)
	item.Diff = AdminConfigDiff(before, after)
	if len(item.Diff) == 0 {
		item.Issues = append(item.Issues, "补丁未产生任何字段变化")
	}
	if input.Target == "model" && before != nil {
		s.checkModelItem(ctx, &item, after)
	}
	if input.Target == "pricing_settings" {
		validatePricingItem(&item, after, revert)
	}
	if item.Status == "ok" && len(item.Issues) > 0 {
		item.Status = "invalid"
	}
	if input.Target == "model" && !revert {
		s.checkItemPricing(&item, input.PricingBasis, settings, before, after)
	}
	if item.Live {
		item.Warnings = append(item.Warnings, "保存即生效")
	}
	return item, nil
}

// loadChangeBase 读取对应目标的快照，缺失的模型作为问题条目保留。
func (s *AdminAssistantService) loadChangeBase(ctx context.Context, item *AdminChangeItem, settings *model.PricingSettingsView) (map[string]any, string, error) {
	switch item.Target {
	case "model":
		body, sha, live, err := s.ReadModel(ctx, item.Key)
		item.Live = live
		if errors.Is(err, repository.ErrNotFound) {
			item.Issues = append(item.Issues, "目标模型不存在")
			return nil, "", nil
		}
		if err != nil {
			return nil, "", err
		}
		if label, ok := body["label"].(string); ok {
			item.Label = label
		}
		return body, sha, nil
	case "pricing_settings":
		if item.Key != "pricing" {
			item.Issues = append(item.Issues, "定价设置 key 必须为 pricing")
		}
		item.Label = "定价设置"
		item.Live = true
		raw := service.PricingSettingsBody(settings)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		return body, configbody.SHA256(raw), nil
	default:
		item.Issues = append(item.Issues, "target 只能是 model 或 pricing_settings")
		return nil, "", nil
	}
}

// restoreNullableSettings 保留契约要求的 null 字段，设置中的 null 表示清除覆盖。
func restoreNullableSettings(after map[string]any) {
	for _, key := range []string{"usd_cny_override", "credits_per_yuan"} {
		if _, ok := after[key]; !ok {
			after[key] = nil
		}
	}
}

// validatePricingItem 回滚可恢复历史未设置状态，其余仍用普通定价设置校验。
func validatePricingItem(item *AdminChangeItem, after map[string]any, revert bool) {
	validationBody := item.After
	if revert && after["credits_per_yuan"] == nil {
		checked := mergeAdminPatch(after, map[string]any{"credits_per_yuan": float64(1)})
		validationBody, _ = json.Marshal(checked)
	}
	var req model.PricingSettingsReq
	if json.Unmarshal(validationBody, &req) != nil {
		item.Issues = append(item.Issues, "定价设置字段类型不正确")
		return
	}
	if err := service.ValidatePricingSettings(req); err != nil {
		item.Issues = append(item.Issues, publicAdminError(err))
	}
}

// allowedAdminField 将一期写入限定为模型展示、能力和价格，不能改渠道或凭证。
func allowedAdminField(target, key string) bool {
	if target == "pricing_settings" {
		return key == "credits_per_yuan" || key == "default_markup_pct" || key == "usd_cny_override"
	}
	switch key {
	case "pricing", "capabilities", "label", "hint", "tags", "enabled":
		return true
	}
	return false
}

// checkModelItem 使用 schema 和启用检查双重校验，已上线模型必须满足现有保存约束。
func (s *AdminAssistantService) checkModelItem(ctx context.Context, item *AdminChangeItem, after map[string]any) {
	_, issues := modelcfg.ParseModel(item.After)
	for _, issue := range issues {
		item.Issues = append(item.Issues, issue.Path+"："+issue.Message)
	}
	enabled, _ := after["enabled"].(bool)
	if item.Live || enabled {
		if err := s.d.Writer.CheckEnable(ctx, item.Key, item.After); err != nil {
			item.Issues = append(item.Issues, publicAdminError(err))
		}
	}
}

// checkItemPricing 不信任模型中间值，每一个变更的售价都必须有一条可复算的依据。
func (s *AdminAssistantService) checkItemPricing(item *AdminChangeItem, bases []PricingBasis, settings *model.PricingSettingsView, before, after map[string]any) {
	seen := map[string]bool{}
	for _, basis := range bases {
		basis.Path = normalizePricePath(basis.Path)
		if seen[basis.Path] {
			adminItemIssue(item, "invalid", "价格依据重复："+basis.Path)
			continue
		}
		seen[basis.Path] = true
		checkPriceBasis(item, basis, settings, before, after)
	}
	for _, diff := range item.Diff {
		if pricePath.MatchString(diff.Path) && !seen[diff.Path] {
			adminItemIssue(item, "invalid", diff.Path+" 缺少 pricing_basis")
		}
	}
}

// adminItemIssue 保留 stale 或已有校验错误的优先级，同时收集所有原因。
func adminItemIssue(item *AdminChangeItem, status, message string) {
	item.Issues = append(item.Issues, message)
	if item.Status == "ok" {
		item.Status = status
	}
}

// checkPriceBasis 比较平台复算的价格和成本，不信任模型提供的中间结果。
func checkPriceBasis(item *AdminChangeItem, basis PricingBasis, settings *model.PricingSettingsView, before, after map[string]any) {
	computed, err := recomputeBasis(basis, settings, before)
	if err != nil {
		adminItemIssue(item, "invalid", err.Error())
		return
	}
	item.Basis = append(item.Basis, computed)
	actual, present := valueAt(after, basis.Path).(float64)
	pricing, _ := after["pricing"].(map[string]any)
	billing, _ := pricing["billing"].(string)
	costValue, _ := valueAt(after, costPath(basis.Path, billing)).(float64)
	if !present || basis.Price != computed.Price || actual != float64(computed.Price) || (basis.Mode == "cost" && costValue != float64(computed.Cost)) {
		adminItemIssue(item, "mismatch", fmt.Sprintf("%s 价格复算不一致，应为 %d 积分，成本 %d", basis.Path, computed.Price, computed.Cost))
	}
	appendPriceWarnings(item, computed, actual, costValue)
}

// appendPriceWarnings 提醒过期汇率、低于成本与取整后的实际涨幅，但不阻止应用。
func appendPriceWarnings(item *AdminChangeItem, basis PricingBasis, actual, cost float64) {
	if basis.FX != nil && basis.FX.Stale {
		item.Warnings = append(item.Warnings, "汇率可能过期")
	}
	if actual < cost {
		item.Warnings = append(item.Warnings, basis.Path+" 售价低于成本")
	}
	if basis.Mode != "relative" || basis.From <= 0 {
		return
	}
	if basis.Price == int(basis.From) {
		item.Warnings = append(item.Warnings, basis.Path+" 取整后未变化")
		return
	}
	pct := (float64(basis.Price)/basis.From - 1) * 100
	if math.Abs(pct-basis.Pct) > 0.000001 {
		item.Warnings = append(item.Warnings, fmt.Sprintf("%s 取整后实际涨幅 %+.2f%%", basis.Path, pct))
	}
}

// publicAdminError 只公开业务错误，仓储故障不得写进审批或发给模型。
func publicAdminError(err error) string {
	if errors.Is(err, repository.ErrAdminOperationUncertain) {
		return "操作已开始但结果未确认，请先核对后台现状，再申请新的审批；本次未重复执行"
	}
	if errors.Is(err, repository.ErrAdminOperationFailed) {
		return "该审批条目此前执行失败，请核对现状后重新申请审批"
	}
	var e *errcode.Error
	if errors.As(err, &e) {
		return e.Msg
	}
	if errors.Is(err, repository.ErrRevisionConflict) {
		return "审核期间配置已被他人修改，未覆盖"
	}
	return "配置处理失败，请稍后重试"
}

// reverseRequest 只反向恢复原单实际修改的顶层字段，并使用原单 after 做冲突基准。
func (s *AdminAssistantService) reverseRequest(ctx context.Context, order string) (AdminChangeRequest, error) {
	rows, err := s.d.Audit.ChangeAudits(ctx, order)
	if err != nil {
		return AdminChangeRequest{}, err
	}
	req := AdminChangeRequest{Title: "回滚变更单 " + order, Reason: "恢复该单已应用条目的改前值", RevertOf: order, Items: []AdminChangeInput{}}
	if len(rows) == 0 {
		return req, errcode.ErrInvalidParams.WithMsg("没有找到该变更单的已应用记录")
	}
	for _, row := range rows {
		if isManagementTarget(row.Target) {
			return req, errcode.ErrInvalidParams.WithMsg("管理操作不支持整单自动回滚，请读取当前状态后重新申请变更")
		}
		var data struct {
			Before map[string]any    `json:"before"`
			After  map[string]any    `json:"after"`
			Diff   []AdminChangeDiff `json:"diff"`
		}
		if err := json.Unmarshal(row.Detail, &data); err != nil {
			return req, err
		}
		patch := map[string]any{}
		for _, diff := range data.Diff {
			top := strings.SplitN(diff.Path, ".", 2)[0]
			patch[top] = data.Before[top]
		}
		after, _ := json.Marshal(data.After)
		req.Items = append(req.Items, AdminChangeInput{Target: row.Target, Key: row.Key, BaseSHA256: configbody.SHA256(after), Patch: patch})
	}
	return req, nil
}
