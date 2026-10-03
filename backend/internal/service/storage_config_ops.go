package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
)

// ---- 默认 / 删除 ----

// SetDefault 把一套存储设为默认：之后新上传和新生成的素材写入它，已有素材不动。
// 最近一次测试没通过（或没测过）的对象存储不允许设为默认，避免把全站新素材写进一个用不了的存储。
func (s *StorageConfigService) SetDefault(ctx context.Context, actorID, id uint64) error {
	// 1. 校验存储可用
	row, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return storageNotFound(err)
	}
	if !row.Builtin && (row.LastCheckOK == nil || !*row.LastCheckOK) {
		return errcode.ErrStorageNotChecked
	}
	prev, _ := s.repo.GetDefault(ctx) // 只用来写审计，没有旧默认也不算错

	// 2. 切换默认（同一事务里清掉旧的再设新的）
	if err := s.repo.SetDefault(ctx, id); err != nil {
		return storageNotFound(err)
	}

	// 3. 默认存储的指针也被缓存了，整体失效
	s.invalidate(0)
	detail := map[string]any{"name": row.Name}
	if prev != nil {
		detail["previous_id"] = prev.ID
	}
	aiAudit(ctx, s.audit, actorID, model.AuditStorageDefault, model.AuditTargetStorage, strconv.FormatUint(id, 10), detail)
	return nil
}

// DeleteCheck 回答一套存储能不能删：内置、默认、被素材或未过期的上传意图引用都不能删。
func (s *StorageConfigService) DeleteCheck(ctx context.Context, id uint64) (*StorageDeleteCheck, error) {
	row, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, storageNotFound(err)
	}
	assets, intents, err := s.repo.CountRefs(ctx, id, s.now())
	if err != nil {
		return nil, storageNotFound(err)
	}
	c := &StorageDeleteCheck{Deletable: true, AssetCount: assets, PendingUploads: intents}
	switch {
	case row.Builtin:
		c.Deletable, c.Reason = false, errcode.ErrStorageBuiltin.Msg
	case row.IsDefault:
		c.Deletable, c.Reason = false, errcode.ErrStorageIsDefault.Msg
	case assets+intents > 0:
		c.Deletable, c.Reason = false, usedReason(assets, intents)
	}
	return c, nil
}

func usedReason(assets, intents int64) string {
	return fmt.Sprintf("被 %d 个素材和 %d 个进行中的上传引用，无法删除", assets, intents)
}

// Delete 删除一套存储（连同它的密钥）。桶里的文件不会被清理。
func (s *StorageConfigService) Delete(ctx context.Context, actorID, id uint64) error {
	// 1. 内置与默认存储不能删
	row, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return storageNotFound(err)
	}
	if row.Builtin {
		return errcode.ErrStorageBuiltin
	}
	if row.IsDefault {
		return errcode.ErrStorageIsDefault
	}

	// 2. 仓储在事务里再次检查引用后删除；仍被引用时把数量写进提示
	if err := s.repo.Delete(ctx, id, s.now()); err != nil {
		if errors.Is(err, repository.ErrInUse) {
			assets, intents, _ := s.repo.CountRefs(ctx, id, s.now())
			return errcode.ErrStorageInUse.WithMsg(usedReason(assets, intents))
		}
		return storageNotFound(err)
	}

	// 3. 清掉本实例的密钥明文缓存与客户端缓存
	s.secrets.ForgetSecret(model.StorageSecretName(id))
	s.invalidate(id)
	aiAudit(ctx, s.audit, actorID, model.AuditStorageDelete, model.AuditTargetStorage, strconv.FormatUint(id, 10),
		map[string]any{"name": row.Name, "provider": row.Provider})
	return nil
}
