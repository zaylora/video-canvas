package service

import (
	"context"
	"errors"
	"fmt"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
)

// CheckDelete 删除预检：返回阻断删除整个插件的原因（blockers 为空数组表示可以删）。插件不存在返回 ErrPluginNotFound。
// 阻断原因：内置插件（builtin_plugin）、任一版本被渠道固定（plugin_channels，列出渠道 key 与名称）、
// 任一版本被进行中的任务引用（active_tasks）。结果只给界面展示，真正能否删除以 Delete 在事务内的判断为准。
func (s *AIPluginService) CheckDelete(ctx context.Context, key string) (*model.DeleteCheck, error) {
	// 1. 插件必须存在
	p, err := s.repo.GetPlugin(ctx, key)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrPluginNotFound
	}
	if err != nil {
		return nil, err
	}
	// 2. 统计全部版本的引用；两次查询之间插件被并发删掉同样按 404 处理
	refs, err := s.repo.CountPluginRefs(ctx, key)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrPluginNotFound
	}
	if err != nil {
		return nil, err
	}
	// 3. 组装阻断原因：内置插件放第一条，它不是“处理掉引用就能删”的问题
	out := &model.DeleteCheck{Blockers: []model.DeleteBlocker{}}
	if p.Source == model.PluginSourceBuiltin {
		out.Blockers = append(out.Blockers, model.DeleteBlocker{
			Kind: model.BlockerBuiltinPlugin, Message: errcode.ErrPluginBuiltinDel.Msg, Refs: []model.DeleteRef{},
		})
	}
	out.Blockers = append(out.Blockers, pluginBlockers(refs)...)
	return out, nil
}

// Delete 硬删除整个插件（全部版本 + 插件行）。内置插件不能删（ErrPluginBuiltinDel，409，只能停用）；
// 任一版本仍被渠道固定或被非终态任务的快照引用返回 ErrPluginInUse（409，文案带数量）；插件不存在返回 ErrPluginNotFound。
// 删除后写审计（记全部版本号与 sha256，出问题时能对上号）。
func (s *AIPluginService) Delete(ctx context.Context, actorID uint64, key string) error {
	// 1. 插件必须存在；内置插件一律拒绝：它们随发版登记，删了下次启动又会回来，只会造成困惑
	p, err := s.repo.GetPlugin(ctx, key)
	if errors.Is(err, repository.ErrNotFound) {
		return errcode.ErrPluginNotFound
	}
	if err != nil {
		return err
	}
	if p.Source == model.PluginSourceBuiltin {
		return errcode.ErrPluginBuiltinDel
	}
	// 2. 先查引用给出清楚的原因；真正的删除在仓储事务里加锁后重新统计，这里的结果可能过期，所以只是提示
	refs, err := s.repo.CountPluginRefs(ctx, key)
	if errors.Is(err, repository.ErrNotFound) {
		return errcode.ErrPluginNotFound
	}
	if err != nil {
		return err
	}
	if refs.InUse() {
		return pluginInUse(key, refs)
	}
	// 3. 审计要记被删的版本，删之前先读出来（只读头信息，不含代码）
	versions, err := s.repo.ListVersions(ctx, key)
	if err != nil {
		return err
	}
	// 4. 删除；并发情况下被新渠道 / 新任务引用时仓储返回 ErrInUse 与当时的引用
	refs, err = s.repo.DeletePlugin(ctx, key)
	if errors.Is(err, repository.ErrInUse) {
		return pluginInUse(key, refs)
	}
	if errors.Is(err, repository.ErrNotFound) {
		return errcode.ErrPluginNotFound
	}
	if err != nil {
		return err
	}
	// 5. 审计。不刷新 Registry：没有渠道固定在它的版本上，Registry 里不会有依赖它的运行时；
	//    runner 里按 sha256 装载过的代码也不用清理（与删除单个版本一致），没有渠道再引用它就不会再被调用
	vers := make([]map[string]any, 0, len(versions))
	for _, v := range versions {
		vers = append(vers, map[string]any{"version": v.Version, "sha256": v.SHA256})
	}
	aiAudit(ctx, s.audit, actorID, model.AuditPluginDelete, model.AuditTargetPlugin, key, map[string]any{"versions": vers})
	return nil
}

// pluginBlockers 把插件的引用情况转成阻断原因列表（永不为 nil）。
func pluginBlockers(refs repository.PluginRefs) []model.DeleteBlocker {
	out := []model.DeleteBlocker{}
	if len(refs.Channels) > 0 {
		list := make([]model.DeleteRef, 0, len(refs.Channels))
		for _, ch := range refs.Channels {
			list = append(list, model.DeleteRef{Key: ch.Key, Name: ch.Name})
		}
		out = append(out, model.DeleteBlocker{
			Kind: model.BlockerPluginChannels, Message: fmt.Sprintf("%d 个渠道在用这个插件", len(refs.Channels)), Refs: list,
		})
	}
	if refs.ActiveTasks > 0 {
		out = append(out, model.DeleteBlocker{
			Kind: model.BlockerActiveTasks, Message: fmt.Sprintf("%d 个进行中的任务在用这个插件", refs.ActiveTasks), Refs: []model.DeleteRef{},
		})
	}
	return out
}

// pluginInUse 生成“插件仍被引用”的 409（复用 ErrPluginInUse 的错误码），文案带渠道数与任务数。
func pluginInUse(key string, refs repository.PluginRefs) error {
	return errcode.ErrPluginInUse.WithMsg(fmt.Sprintf("插件 %s 被 %d 个渠道、%d 个进行中的任务使用，无法删除",
		key, len(refs.Channels), refs.ActiveTasks))
}
