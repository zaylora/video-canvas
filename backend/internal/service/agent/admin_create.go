package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/configbody"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

// isAdminCreateTarget 标识二期不支持自动回滚的三类条目。
func isAdminCreateTarget(target string) bool {
	return target == "plugin" || target == "channel" || target == "model_create"
}

// buildCreateItem 二期创建正文仍使用 patch；插件源码只进入 after，不进入字段 diff。
func (s *AdminAssistantService) buildCreateItem(ctx context.Context, in AdminChangeInput, revert bool, runID uint64) (AdminChangeItem, error) {
	item := AdminChangeItem{Target: in.Target, Key: in.Key, Label: in.Key, Status: "ok", RequiredRole: model.RoleAdmin, Issues: []string{}, Warnings: []string{}, Diff: []AdminChangeDiff{}, Basis: []PricingBasis{}, Before: json.RawMessage("null")}
	if in.Target != "model_create" {
		item.RequiredRole = model.RoleSuperAdmin
	}
	if revert {
		adminItemIssue(&item, "invalid", "新建的插件 / 渠道 / 模型请在对应页面手动删除")
		return item, nil
	}
	if in.Key == "" {
		adminItemIssue(&item, "invalid", "创建条目必须提供 key")
		return item, nil
	}
	if containsAdminCredential(in.Patch) {
		adminItemIssue(&item, "invalid", "变更单不能包含凭证；请使用密钥卡片")
		return item, nil
	}
	after := mergeAdminPatch(nil, in.Patch)
	var err error
	switch in.Target {
	case "plugin":
		err = s.buildPluginItem(ctx, &item, after, runID)
	case "channel":
		err = s.buildChannelItem(ctx, &item, after)
	case "model_create":
		err = s.buildModelCreateItem(ctx, &item, after)
	}
	if err != nil {
		return item, err
	}
	item.After, _ = json.Marshal(after)
	shown := mergeAdminPatch(nil, after)
	delete(shown, "code")
	if item.Target == "plugin" {
		if detail, ok := item.Detail.(map[string]any); ok {
			shown = map[string]any{"plugin_key": item.Key, "version": detail["version_after"], "meta": detail["meta"]}
		}
	}
	item.Diff = AdminConfigDiff(nil, shown)
	if len(item.Issues) > 0 {
		item.Status = "invalid"
	}
	return item, nil
}

// containsAdminCredential 拒绝凭证字段；模型 key 和插件源代码属于合法业务字段。
func containsAdminCredential(v any) bool {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			if k != "key" && sensitiveAdminField(k) {
				return true
			}
			if containsAdminCredential(e) {
				return true
			}
		}
	case []any:
		for _, e := range x {
			if containsAdminCredential(e) {
				return true
			}
		}
	}
	return false
}

// buildPluginItem 读取最近一次相同代码的静态检查，预检只读不登记。
func (s *AdminAssistantService) buildPluginItem(ctx context.Context, item *AdminChangeItem, after map[string]any, runID uint64) error {
	code, _ := after["code"].(string)
	if code == "" || len(code) > provider.MaxPluginDraftBytes {
		item.Issues = append(item.Issues, "code 必须是 1–512KB 的插件源码")
		return nil
	}
	pre, err := s.d.Checker.Precheck(ctx, code)
	if err != nil {
		return err
	}
	meta := &provider.PluginMeta{}
	err = json.Unmarshal(pre.Meta, meta)
	if err != nil {
		item.Issues = append(item.Issues, "插件 meta 无效")
		return nil
	}
	if meta.Key != item.Key {
		item.Issues = append(item.Issues, "插件 key 与 meta.key 不一致")
	}
	for k := range after {
		if k != "code" {
			item.Issues = append(item.Issues, "插件 patch 仅允许 code")
		}
	}
	if !pre.OK {
		item.Issues = append(item.Issues, "插件预检未通过")
	}
	existing, err := s.d.Plugins.GetPlugin(ctx, item.Key)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return err
	}
	if err == nil && existing.Source == model.PluginSourceBuiltin {
		item.Issues = append(item.Issues, "内置插件不能上传同名版本")
	}
	vs, err := s.d.Plugins.ListVersions(ctx, item.Key)
	if err != nil {
		return err
	}
	version, err := service.PreviewPluginVersion(meta.Version, []byte(code), vs)
	if err != nil {
		item.Issues = append(item.Issues, publicAdminError(err))
	}
	before, codeDiff, err := s.pluginBeforeDiff(ctx, vs, code)
	if err != nil {
		return err
	}
	s.checkMu.Lock()
	checked, ok := s.checks[fmt.Sprintf("%d:%s", runID, item.Key)]
	s.checkMu.Unlock()
	if !ok || checked.SHA != configbody.SHA256([]byte(code)) {
		checked.Check = map[string]any{"ok": false, "issues": []string{"请先对该代码运行 admin_plugin_check"}}
	}
	if checked.Check["ok"] != true {
		item.Issues = append(item.Issues, "最近一次静态验证未通过，请先验证相同代码")
	}
	item.Label = meta.Name
	endpoints, _ := json.Marshal(meta.Endpoints)
	item.Detail = map[string]any{"plugin_key": item.Key, "version_before": before, "version_after": version, "meta": map[string]any{"name": meta.Name, "vendor": meta.Vendor, "auth": meta.Auth.Type, "endpoints": string(endpoints)}, "code_lines": len(strings.Split(code, "\n")), "code_diff": codeDiff, "check": checked.Check}
	return nil
}

// pluginBeforeDiff 新插件返回空代码差异，已有插件读取当前版本生成 unified diff。
func (s *AdminAssistantService) pluginBeforeDiff(ctx context.Context, vs []model.AIPluginVersion, code string) (*string, string, error) {
	if len(vs) == 0 {
		return nil, "", nil
	}
	v := vs[0].Version
	old, err := s.d.Plugins.GetVersion(ctx, vs[0].ID)
	if err != nil {
		return nil, "", err
	}
	return &v, adminCodeDiff(old.Code, code), nil
}

// buildChannelItem 创建前只验证字段，不发连通性请求，密钥始终缺失。
func (s *AdminAssistantService) buildChannelItem(ctx context.Context, item *AdminChangeItem, after map[string]any) error {
	rows, err := s.d.Channels.ListChannels(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.Key == item.Key {
			item.Issues = append(item.Issues, "渠道已存在，二期只支持新建渠道")
		}
	}
	for k := range after {
		if !slices.Contains([]string{"name", "base_url", "headers", "rate_limit", "enabled"}, k) {
			item.Issues = append(item.Issues, "不允许渠道字段："+k)
		}
	}
	name, _ := after["name"].(string)
	base, _ := after["base_url"].(string)
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" {
		item.Issues = append(item.Issues, "base_url 必须是无鉴权信息的 HTTP(S) 地址")
	}
	if strings.TrimSpace(name) == "" {
		item.Issues = append(item.Issues, "渠道名称不能为空")
	}
	names := []string{}
	if headers, ok := after["headers"].(map[string]any); ok {
		for key := range headers {
			names = append(names, key)
		}
	}
	slices.Sort(names)
	item.Label = name
	item.Detail = map[string]any{"base_url": base, "headers": names, "rate_limit": after["rate_limit"], "secret": "missing"}
	return nil
}

// buildModelCreateItem 固定新模型为停用状态，协议 latest 在执行时解析实际版本。
func (s *AdminAssistantService) buildModelCreateItem(ctx context.Context, item *AdminChangeItem, after map[string]any) error {
	if _, err := s.d.Models.GetModelConfig(ctx, item.Key); err == nil {
		item.Issues = append(item.Issues, "模型已存在，请使用 model 更新")
	} else if !errors.Is(err, repository.ErrNotFound) {
		return err
	}
	after["key"] = item.Key
	after["enabled"] = false
	raw, _ := json.Marshal(after)
	// latest 是审批占位符，结构校验使用合法版本；执行时才固定实际上传版本。
	var validation map[string]any
	_ = json.Unmarshal(raw, &validation)
	channels, _ := validation["channels"].([]any)
	for _, value := range channels {
		ch, _ := value.(map[string]any)
		protocol, _ := ch["protocol"].(map[string]any)
		if protocol["version"] == "latest" {
			protocol["version"] = "1.0.0"
		}
	}
	validationRaw, _ := json.Marshal(validation)
	cfg, issues := modelcfg.ParseModel(validationRaw)
	for _, issue := range issues {
		item.Issues = append(item.Issues, issue.Path+"："+issue.Message)
	}
	if cfg == nil {
		return nil
	}
	item.Label = cfg.Label
	var body struct {
		Channels []struct {
			Channel  string `json:"channel"`
			Upstream string `json:"upstream_model"`
			Protocol struct {
				Plugin  string `json:"plugin"`
				Version string `json:"version"`
			} `json:"protocol"`
		} `json:"channels"`
	}
	_ = json.Unmarshal(raw, &body)
	if len(body.Channels) > 0 {
		ch := body.Channels[0]
		item.Detail = map[string]any{"kind": cfg.Kind, "channel": ch.Channel, "upstream_model": ch.Upstream, "plugin": ch.Protocol.Plugin + "@" + ch.Protocol.Version}
	}
	return nil
}

// adminCodeDiff 用完整单个 hunk 生成 unified diff，保留可核查的逐行增删，不把代码混入字段 diff。
func adminCodeDiff(before, after string) string {
	if before == after {
		return ""
	}
	old := strings.Split(strings.TrimSuffix(before, "\n"), "\n")
	next := strings.Split(strings.TrimSuffix(after, "\n"), "\n")
	return fmt.Sprintf("--- before\n+++ after\n@@ -1,%d +1,%d @@\n-%s\n+%s\n", len(old), len(next), strings.Join(old, "\n-"), strings.Join(next, "\n+"))
}

// adminTopologicalOrder 拒绝自依赖、重复、越界和环，并返回稳定拓扑顺序。
func adminTopologicalOrder(items []AdminChangeItem) ([]int, error) {
	state := make([]int, len(items))
	order := []int{}
	var visit func(int) error
	visit = func(i int) error {
		if state[i] == 1 {
			return fmt.Errorf("depends_on 存在循环依赖")
		}
		if state[i] == 2 {
			return nil
		}
		state[i] = 1
		seen := map[int]bool{}
		for _, dep := range items[i].DependsOn {
			if dep < 0 || dep >= len(items) || dep == i || seen[dep] {
				return fmt.Errorf("depends_on 下标越界、自依赖或重复")
			}
			seen[dep] = true
			if err := visit(dep); err != nil {
				return err
			}
		}
		state[i] = 2
		order = append(order, i)
		return nil
	}
	for i := range items {
		if err := visit(i); err != nil {
			return nil, err
		}
	}
	return order, nil
}

// validateAdminDependencies 图不合法时不执行任何条目，避免部分图引起意外写入。
func validateAdminDependencies(items []AdminChangeItem) {
	if _, err := adminTopologicalOrder(items); err != nil {
		for i := range items {
			adminItemIssue(&items[i], "invalid", err.Error())
		}
	}
}
