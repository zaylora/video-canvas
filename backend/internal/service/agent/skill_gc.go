package agent

import (
	"context"
	"errors"
	"time"

	"go.uber.org/zap"

	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/storage"
)

// gcBatch 是清理任务一次处理的行数上限。
const gcBatch = 50

// Sweep 清理垃圾：过期的导入暂存、已删除的版本、超时没转正的 pending 版本。
// 清理由数据库驱动（存储接口没有 List，无法扫描对象存储找孤儿）：先删对象再删行，
// 对象删除失败就留着行，下一轮重试；对象删除是幂等的，多实例同时清理也无害。
// 失败只记日志，不影响调用方。
func (s *SkillService) Sweep(ctx context.Context) {
	now := s.opt.Now()
	if imps, err := s.repo.ListExpiredImports(ctx, now, gcBatch); err != nil {
		logger.Warn("查询过期导入暂存失败", zap.Error(err))
	} else {
		for _, imp := range imps {
			if s.removeObject(ctx, imp.StorageID, imp.PackageKey) {
				if _, err := s.repo.DeleteImport(ctx, imp.ID); err != nil {
					logger.Warn("删除过期导入暂存行失败", zap.Error(err), zap.String("import_id", imp.ID))
				}
			}
		}
	}
	vs, err := s.repo.ListGarbageVersions(ctx, now.Add(-s.opt.PendingTTL), gcBatch)
	if err != nil {
		logger.Warn("查询待清理的技能版本失败", zap.Error(err))
		return
	}
	for _, v := range vs {
		if !s.removeObject(ctx, v.StorageID, v.PackageKey) {
			continue
		}
		s.pkgs.drop(v.ID)
		if err := s.repo.DeleteVersionRow(ctx, v.ID); err != nil {
			logger.Warn("删除技能版本行失败", zap.Error(err), zap.Uint64("version_id", v.ID))
		}
	}
}

// removeObject 删除对象并返回是否成功；存储配置已不存在时对象无从删起，也算完成（否则这一行会永远卡住清理）。
func (s *SkillService) removeObject(ctx context.Context, storageID uint64, key string) bool {
	h, err := s.stores.Get(ctx, storageID)
	if errors.Is(err, storage.ErrStorageNotFound) {
		return true
	}
	if err != nil {
		logger.Warn("取存储失败，跳过清理", zap.Error(err), zap.Uint64("storage_id", storageID), zap.String("key", key))
		return false
	}
	if err := h.Storage.Delete(ctx, key); err != nil {
		logger.Warn("删除技能包对象失败，下一轮重试", zap.Error(err), zap.String("key", key))
		return false
	}
	return true
}

// RunSweeper 周期性清理，直到 ctx 取消；由 App 在后台启动。
func (s *SkillService) RunSweeper(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Sweep(ctx)
		}
	}
}
