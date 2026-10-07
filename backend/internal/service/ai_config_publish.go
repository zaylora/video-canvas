package service

import (
	"context"
	"errors"
	"fmt"

	"go.uber.org/zap"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/provider/pluginmeta"
	"video-canvas/internal/repository"
)

// aiResolvedChannel 是模型 channels[0] 解析出来的渠道行、它固定的插件版本头信息与插件 meta。
type aiResolvedChannel struct {
	channel *model.AIChannel
	version *model.AIPluginVersion
	meta    *pluginmeta.Meta
}

// Publish 发布最新草稿。发布前置检查见 checkPublishable（正文无问题、渠道与插件可用、插件支持该 kind、需要鉴权时渠道 Key 已设置）。
// 发布成功后立即刷新 Registry 内存，新任务马上使用新版本，进行中的任务仍按各自的快照执行。
func (s *AIConfigService) Publish(ctx context.Context, key string, adminID uint64) (*model.AIConfigRevision, error) {
	// 1. 取最新草稿：没有草稿返回 409
	draft, err := s.repo.GetDraft(ctx, model.ConfigTargetModel, key)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrConfigNoDraft
	}
	if err != nil {
		return nil, err
	}
	// 2. 发布前置检查
	ptr, err := s.checkPublishable(ctx, key, draft.BodyJSON)
	if err != nil {
		return nil, err
	}
	// 3. 事务里切换发布指针。用 draft.ID 而不是“最新草稿”，
	//    这样校验之后草稿又被别人改掉时不会把没校验过的正文发布出去，而是返回冲突让运营刷新
	rev, err := s.repo.PublishDraft(ctx, ptr, draft.ID)
	if errors.Is(err, repository.ErrNotFound) || errors.Is(err, repository.ErrRevisionConflict) {
		return nil, errcode.ErrConfigNoDraft.WithMsg("草稿已发生变化，请刷新后重新发布")
	}
	if err != nil {
		return nil, err
	}
	logger.Info("发布模型配置", zap.String("key", key), zap.Uint64("revision_id", rev.ID),
		zap.Int("revision_no", rev.RevisionNo), zap.Uint64("admin_id", adminID))
	// 4. 热生效：刷新本实例内存并广播
	s.notifyChanged(ctx, "publish model/"+key)
	return rev, nil
}

// Rollback 把发布指针改回历史版本。只允许回到已归档的版本；回滚前按发布同样的标准重新检查，
// 避免回到一个当时没校验过的旧草稿，或者它的渠道 / 插件 / 凭证已经不满足条件的版本。
func (s *AIConfigService) Rollback(ctx context.Context, key string, revisionID, adminID uint64) (*model.AIConfigRevision, error) {
	// 1. 取出目标 revision 并核对归属
	rev, err := s.GetRevision(ctx, key, revisionID)
	if err != nil {
		return nil, err
	}
	// 2. 状态检查：当前已发布的没必要回滚，草稿不是历史版本
	switch rev.Status {
	case model.RevisionPublished:
		return nil, errcode.ErrConfigInvalid.WithMsg("该版本已是当前发布版本")
	case model.RevisionDraft:
		return nil, errcode.ErrConfigInvalid.WithMsg("草稿不是历史版本，请使用发布")
	}
	// 3. 发布前置检查
	ptr, err := s.checkPublishable(ctx, key, rev.BodyJSON)
	if err != nil {
		return nil, err
	}
	// 4. 事务里改回指针；并发被别人改动（状态变化）时返回冲突
	out, err := s.repo.Rollback(ctx, ptr, rev.ID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrConfigNotFound
	}
	if errors.Is(err, repository.ErrRevisionConflict) {
		return nil, errcode.ErrConfigInvalid.WithMsg("版本状态已变化，请刷新后重试")
	}
	if err != nil {
		return nil, err
	}
	logger.Info("回滚模型配置", zap.String("key", key), zap.Uint64("revision_id", out.ID),
		zap.Int("revision_no", out.RevisionNo), zap.Uint64("admin_id", adminID))
	// 5. 热生效
	s.notifyChanged(ctx, "rollback model/"+key)
	return out, nil
}

// SetModelEnabled 上架 / 下架模型。上架要求模型已经发布过；下架立即生效，进行中的任务不受影响。
func (s *AIConfigService) SetModelEnabled(ctx context.Context, key string, enabled bool, adminID uint64) error {
	// 1. 模型必须存在
	m, err := s.repo.GetModelPointer(ctx, key)
	if err != nil {
		return aiNotFound(err)
	}
	// 2. 上架前必须已发布：没发布的模型上架后也不会出现在清单里，属于误操作，提前拦下
	if enabled && m.PublishedRevisionID == nil {
		return errcode.ErrConfigInvalid.WithMsg("模型尚未发布，无法上架")
	}
	// 3. 写库并热生效
	if err := s.repo.SetModelEnabled(ctx, key, enabled); err != nil {
		return aiNotFound(err)
	}
	logger.Info("模型上下架", zap.String("key", key), zap.Bool("enabled", enabled), zap.Uint64("admin_id", adminID))
	s.notifyChanged(ctx, "enabled "+key)
	return nil
}

// SetModelSort 修改模型排序值（升序，越小越靠前）并热生效。
func (s *AIConfigService) SetModelSort(ctx context.Context, key string, sort int, adminID uint64) error {
	// 1. 写库；模型不存在返回 ErrConfigNotFound
	if err := s.repo.SetModelSort(ctx, key, sort); err != nil {
		return aiNotFound(err)
	}
	// 2. 热生效
	logger.Info("修改模型排序", zap.String("key", key), zap.Int("sort", sort), zap.Uint64("admin_id", adminID))
	s.notifyChanged(ctx, "sort "+key)
	return nil
}

// checkPublishable 是发布和回滚共用的前置检查（admin-ai-api.md“发布前置检查”），通过后返回要同步到指针行的冗余字段：
// 正文无校验问题 → 渠道存在且启用、插件启用、插件版本支持该 kind → 插件需要鉴权时渠道 Key 已设置。
// 每一项不满足都返回对应的业务错误，而不是笼统的“配置无效”，运营据此知道该找谁（渠道 / Key 由 super_admin 管）。
func (s *AIConfigService) checkPublishable(ctx context.Context, key string, body []byte) (repository.ConfigPointer, error) {
	// 1. 正文校验：有任何问题都不允许发布
	meta, cfg, issues := aiBodyIssues(key, body)
	if meta == nil {
		return repository.ConfigPointer{}, errcode.ErrConfigInvalid.WithMsg("配置正文不是合法的 JSON 对象")
	}
	if len(issues) > 0 || cfg == nil {
		return repository.ConfigPointer{}, errcode.ErrConfigInvalid.WithMsg("配置校验未通过：" + aiFormatIssues(issues))
	}
	// 2. 渠道与插件可用：否则发布后模型也不会出现在清单里，属于无效发布
	rc, err := s.resolveChannel(ctx, cfg)
	if err != nil {
		return repository.ConfigPointer{}, err
	}
	// 3. 渠道 Key 已设置：否则发布后每个任务都会因缺凭证而失败
	if err := s.checkChannelSecret(ctx, rc); err != nil {
		return repository.ConfigPointer{}, err
	}
	return aiPointer(meta), nil
}

// resolveChannel 按模型的 channels[0] 找到渠道、它固定的插件版本与 meta，并做可用性检查：
// 渠道存在（ErrChannelNotFound）且启用（ErrChannelDisabled）；插件存在且启用（ErrChannelInvalid / ErrPluginDisabled）；
// 版本行存在、属于该插件、meta 能解析（ErrChannelInvalid）；插件 endpoints 里有模型的 kind（ErrConfigInvalid：是模型选错了渠道）。
// 仓储的其他错误原样返回。发布检查、保存时的跨对象检查、试跑都用它，保证三处的标准一致。
func (s *AIConfigService) resolveChannel(ctx context.Context, cfg *modelcfg.ModelConfig) (*aiResolvedChannel, error) {
	// 1. 渠道：已校验过的配置 channels 恰好一个，这里的判空只是防御
	if len(cfg.Channels) == 0 {
		return nil, errcode.ErrConfigInvalid.WithMsg("模型没有绑定渠道")
	}
	chKey := cfg.Channels[0].Channel
	ch, err := s.channels.GetChannel(ctx, chKey)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrChannelNotFound.WithMsg(fmt.Sprintf("渠道 %q 不存在", chKey))
	}
	if err != nil {
		return nil, err
	}
	if !ch.Enabled {
		return nil, errcode.ErrChannelDisabled.WithMsg(fmt.Sprintf("渠道 %q 已停用", chKey))
	}
	// 2. 插件：插件停用等于所有用它的渠道不再接新任务
	p, err := s.plugins.GetPlugin(ctx, ch.PluginKey)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrChannelInvalid.WithMsg(fmt.Sprintf("渠道 %q 引用的插件 %q 不存在", chKey, ch.PluginKey))
	}
	if err != nil {
		return nil, err
	}
	if !p.Enabled {
		return nil, errcode.ErrPluginDisabled.WithMsg(fmt.Sprintf("渠道 %q 使用的插件 %q 已停用", chKey, ch.PluginKey))
	}
	// 3. 渠道固定的插件版本与 meta
	rc, err := s.loadChannelVersion(ctx, ch)
	if err != nil {
		return nil, err
	}
	// 4. 插件必须支持这种生成方式，否则宿主拿到任务也不知道该调哪个 endpoint
	if !rc.meta.SupportsKind(cfg.Kind) {
		why := "不支持"
		if cfg.Kind == modelcfg.KindAgent {
			why = "不支持（Agent 模型要求渠道用 Bearer 鉴权）"
		}
		return nil, errcode.ErrConfigInvalid.WithMsg(fmt.Sprintf("渠道 %q 使用的插件 %s@%s %s %s 类型的模型",
			chKey, rc.version.PluginKey, rc.version.Version, why, cfg.Kind))
	}
	return rc, nil
}

// loadChannelVersion 读出渠道固定的插件版本头信息并解析 meta；版本不存在、不属于渠道的插件、meta 损坏都是渠道数据问题（ErrChannelInvalid）。
func (s *AIConfigService) loadChannelVersion(ctx context.Context, ch *model.AIChannel) (*aiResolvedChannel, error) {
	ver, err := s.plugins.GetVersionHead(ctx, ch.PluginVersionID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrChannelInvalid.WithMsg(fmt.Sprintf("渠道 %q 固定的插件版本不存在", ch.Key))
	}
	if err != nil {
		return nil, err
	}
	if ver.PluginKey != ch.PluginKey {
		return nil, errcode.ErrChannelInvalid.WithMsg(fmt.Sprintf("渠道 %q 固定的插件版本不属于插件 %q", ch.Key, ch.PluginKey))
	}
	meta, err := pluginmeta.Parse(ver.MetaJSON)
	if err != nil {
		return nil, errcode.ErrChannelInvalid.WithMsg(fmt.Sprintf("插件 %s@%s 的 meta 无法解析", ver.PluginKey, ver.Version))
	}
	return &aiResolvedChannel{channel: ch, version: ver, meta: meta}, nil
}

// checkChannelSecret 插件需要宿主注入鉴权（auth.type 不是 none）时，要求渠道 Key 已设置，否则返回 ErrChannelSecretUnset。
func (s *AIConfigService) checkChannelSecret(ctx context.Context, rc *aiResolvedChannel) error {
	if rc.meta.Auth.Type == pluginmeta.AuthNone {
		return nil
	}
	set, err := s.SecretIsSet(ctx, model.ChannelSecretName(rc.channel.Key))
	if err != nil {
		return err
	}
	if !set {
		return errcode.ErrChannelSecretUnset.WithMsg(fmt.Sprintf("渠道 %q 的 Key 尚未设置，请联系超级管理员设置后再试", rc.channel.Key))
	}
	return nil
}

// notifyChanged 在配置变化后刷新本实例的 Registry 内存并广播失效。
// 刷新失败不回滚已提交的变更：把内存状态标记为过期，下次读取时会重新加载。
func (s *AIConfigService) notifyChanged(ctx context.Context, reason string) {
	if err := s.RefreshRegistry(ctx); err != nil {
		logger.Error("刷新 Registry 失败，将在下次读取时重试", zap.String("reason", reason), zap.Error(err))
		s.regMu.Lock()
		s.regState = nil
		s.regMu.Unlock()
	}
	if s.invalidator != nil {
		s.invalidator.Invalidate(ctx, reason)
	}
}
