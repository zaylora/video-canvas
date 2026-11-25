package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"video-canvas/internal/model"
	"video-canvas/internal/provider"
	"video-canvas/internal/storage"
)

// ReferenceCacheStats 是后台诊断使用的计数和当前在管字节数，不含任何对象内容或凭证。
type ReferenceCacheStats struct {
	Hits          int64
	UploadedBytes int64
	Failures      int64
	Removed       int64
	ManagedBytes  int64
}

// Maintain 按专用前缀补记遗漏对象，再回收符合条件的对象；数据库记录让多 worker 和重启接续安全。
func (s *ReferenceCache) Maintain(ctx context.Context) (ReferenceCacheStats, error) {
	// 1. 先对账，将过了上传保护期的孤儿计入容量。
	target, err := s.referenceTarget(ctx)
	if err != nil {
		return ReferenceCacheStats{}, err
	}
	listCtx, cancelList := context.WithTimeout(ctx, time.Minute)
	listErr := s.reconcileReferences(listCtx, target)
	cancelList()
	// 2. 列举有独立时限，给已入账对象的清理留出时间。
	cleanupErr := s.reclaimReferences(ctx, target, 40)
	bytes, err := s.repo.Bytes(ctx, target.namespace)
	if err != nil {
		err = referenceOperationError(ctx, err, "读取临时参考素材占用失败")
	}
	return ReferenceCacheStats{
		Hits: s.hits.Load(), UploadedBytes: s.uploaded.Load(), Failures: s.failures.Load(),
		Removed: s.removed.Load(), ManagedBytes: bytes,
	}, errors.Join(listErr, cleanupErr, err)
}

// reconcileReferences 只收录应用生成的合法 key，并且等在途上传的宽限期结束。
func (s *ReferenceCache) reconcileReferences(ctx context.Context, target referenceTarget) error {
	now := time.Now()
	err := target.bucket.ListReferences(ctx, target.prefix, func(key string, size int64, modified time.Time) error {
		if !isReferenceObjectKey(target.prefix, key) || size < 0 || modified.IsZero() || modified.After(now.Add(-referenceCacheSettlement)) {
			return nil
		}
		return s.repo.TrackOrphan(ctx, model.ReferenceCacheObject{
			Key: key, Namespace: target.namespace, Size: size,
			CreatedAt: modified, LastUsedAt: modified, BusyUntil: now, Deleting: true,
		})
	})
	if err != nil {
		return referenceOperationError(ctx, err, "R2 临时前缀对账失败")
	}
	return nil
}

// isReferenceObjectKey 限制对账清理只触及临时命名空间内的对象代际。
func isReferenceObjectKey(prefix, key string) bool {
	if !strings.HasPrefix(key, prefix) {
		return false
	}
	digest, generation, ok := strings.Cut(strings.TrimPrefix(key, prefix), "/")
	if !ok || !validReferenceDigest(digest) {
		return false
	}
	_, err := uuid.Parse(generation)
	return err == nil && len(generation) == 36
}

// reclaimReferences 认领到期的对象并确认远端已删除才归还容量。
func (s *ReferenceCache) reclaimReferences(ctx context.Context, target referenceTarget, limit int) error {
	for range limit {
		now := time.Now()
		object, err := s.repo.ClaimCleanup(ctx, model.ReferenceCacheSweep{
			Namespace: target.namespace, Now: now, UnusedSince: now.Add(-referenceCacheRetention),
			UncertainSince: now.Add(-s.cfg.UnknownGrace), ClaimUntil: now.Add(referenceCacheSettlement),
		})
		if err != nil {
			return referenceOperationError(ctx, err, "认领待清理 R2 临时对象失败")
		}
		if object == nil {
			return nil
		}
		if err := s.deleteReference(ctx, target, object.Key); err != nil {
			return err
		}
	}
	return nil
}

// deleteReference 先隔离旧 key，然后删除并用明确的 HEAD 404 复核；失败时保留容量等待重试。
func (s *ReferenceCache) deleteReference(ctx context.Context, target referenceTarget, key string) error {
	if !isReferenceObjectKey(target.prefix, key) {
		return referenceCacheError(provider.ClassTerminal, provider.CodeReferenceCacheConfig, "待清理对象不在临时参考素材前缀内")
	}
	if err := target.bucket.Delete(ctx, key); err != nil {
		return referenceOperationError(ctx, err, "删除 R2 临时对象失败")
	}
	if _, err := target.bucket.StatReference(ctx, key); !errors.Is(err, storage.ErrNotFound) {
		return referenceOperationError(ctx, err, "无法确认 R2 临时对象已删除")
	}
	if err := s.repo.FinishCleanup(ctx, key); err != nil {
		return referenceOperationError(ctx, err, "归还 R2 临时素材容量失败")
	}
	s.removed.Add(1)
	return nil
}
