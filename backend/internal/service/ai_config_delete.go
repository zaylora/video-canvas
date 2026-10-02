package service

import (
	"context"
	"errors"

	"go.uber.org/zap"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/repository"
)

// CheckModelDelete 删除预检：返回阻断删除模型的原因（blockers 为空数组表示可以删）。模型不存在（含已删除）返回 ErrConfigNotFound。
// 模型只有一个阻断原因：还在上架。不检查进行中的任务——worker 与结算只读任务自己的 config_snapshot，
// 从不回读 ai_models / ai_config_revisions，删除模型不影响它们跑完。
func (s *AIConfigService) CheckModelDelete(ctx context.Context, key string) (*model.DeleteCheck, error) {
	// 1. 模型必须存在
	m, err := s.repo.GetModelPointer(ctx, key)
	if err != nil {
		return nil, aiNotFound(err)
	}
	// 2. 上架中的模型用户还在用，必须先下架
	out := &model.DeleteCheck{Blockers: []model.DeleteBlocker{}}
	if m.Enabled {
		out.Blockers = append(out.Blockers, model.DeleteBlocker{
			Kind: model.BlockerModelEnabled, Message: errcode.ErrModelEnabled.Msg, Refs: []model.DeleteRef{},
		})
	}
	return out, nil
}

// DeleteModel 硬删除模型：只有已下架的模型能删（否则 ErrModelEnabled，409）。指针行与它的全部 revision 一起删掉，不可恢复；
// 之后同名 key 可以重新新建 / 导入（revision_no 从 1 开始）。模型不存在返回 ErrConfigNotFound。
// 进行中与历史任务不受影响：它们只读自己的 config_snapshot，generation_tasks 对 ai_models / revision 也没有外键。
func (s *AIConfigService) DeleteModel(ctx context.Context, key string, adminID uint64) error {
	// 1. 先查一次给出明确的原因；真正的判断在仓储事务里锁住指针行后再做，这里的结果可能过期
	m, err := s.repo.GetModelPointer(ctx, key)
	if err != nil {
		return aiNotFound(err)
	}
	if m.Enabled {
		return errcode.ErrModelEnabled
	}
	// 2. 事务内加锁复查并删除全部 revision 与指针行；并发被重新上架时仓储返回 ErrInUse，翻译成同一个 409
	err = s.repo.DeleteModel(ctx, key)
	if errors.Is(err, repository.ErrInUse) {
		return errcode.ErrModelEnabled
	}
	if err != nil {
		return aiNotFound(err)
	}
	// 3. 热生效：已下架的模型本来就不在公开清单里，刷新是为了让 Registry 里彻底没有它（试跑、dry-run 也找不到）。
	//    模型配置的写操作不写审计表（审计表只覆盖插件 / 渠道 / 凭证），与发布、上下架一样记业务日志
	logger.Info("删除模型", zap.String("key", key), zap.Uint64("admin_id", adminID))
	s.notifyChanged(ctx, "delete model/"+key)
	return nil
}
