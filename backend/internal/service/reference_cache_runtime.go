package service

import (
	"context"

	"video-canvas/internal/config"
	"video-canvas/internal/model"
	"video-canvas/internal/provider"
)

// ReferenceCacheRuntime 在混合策略关闭时使用原有部署配置，开启时使用管理端选择的云端和限额。
type ReferenceCacheRuntime struct {
	base   *ReferenceCache
	hybrid StorageHybridRepo
}

// NewReferenceCacheRuntime 创建动态配置的外链准备与日常清理入口。
func NewReferenceCacheRuntime(cfg config.ReferenceCache, stores StoreRegistry, repo ReferenceCacheRepository, hybrid StorageHybridRepo) *ReferenceCacheRuntime {
	return &ReferenceCacheRuntime{base: NewReferenceCache(cfg, stores, repo), hybrid: hybrid}
}

// PrepareURL 为当前有效策略下的本地素材准备外链；两种策略都关闭时回到原地址。
func (r *ReferenceCacheRuntime) PrepareURL(ctx context.Context, task provider.TaskRef, file *provider.AssetFile) (string, error) {
	// 1. 已提交的策略优先，准备中的策略不改变请求。
	cache, err := r.current(ctx)
	if err != nil {
		return "", referenceOperationError(ctx, err, "无法读取混合存储策略")
	}
	if cache == nil {
		return "", provider.ErrReferenceCacheDisabled
	}
	return cache.PrepareURL(ctx, task, file)
}

// Maintain 在当前有效策略上回收临时对象；关闭策略本身不处理资源。
func (r *ReferenceCacheRuntime) Maintain(ctx context.Context) (ReferenceCacheStats, error) {
	// 1. 此轮仅处理有有效配置的缓存命名空间。
	cache, err := r.current(ctx)
	if err != nil {
		return ReferenceCacheStats{}, referenceOperationError(ctx, err, "无法读取混合存储策略")
	}
	if cache == nil {
		return ReferenceCacheStats{}, nil
	}
	return cache.Maintain(ctx)
}

// current 由一份策略快照选择每次准备的参数，期间后台切换不会改变正在进行的请求。
func (r *ReferenceCacheRuntime) current(ctx context.Context) (*ReferenceCache, error) {
	policy, err := r.hybrid.Hybrid(ctx)
	if err != nil {
		return nil, err
	}
	if policy.State == model.StorageHybridActive {
		cache := *r.base
		cache.cfg.StorageID, cache.cfg.MaxBytes = policy.CloudID, policy.LimitBytes
		cache.hybrid = true
		return &cache, nil
	}
	if r.base.cfg.Enabled {
		return r.base, nil
	}
	return nil, nil
}
