package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"video-canvas/internal/config"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
	"video-canvas/internal/storage"
)

// StorageHybridRepo 持久化启用任务、限额和参与存储的配置锁。
type StorageHybridRepo interface {
	// List 返回所有配置（包括搜索结果和分页之外的配置）。
	List(ctx context.Context) ([]model.StorageConfig, error)
	// Hybrid 返回当前混合策略。
	Hybrid(ctx context.Context) (model.StorageHybridPolicy, error)
	// BeginHybrid 记录启用请求。
	BeginHybrid(ctx context.Context, limit int64) (model.StorageHybridPolicy, error)
	// DisableHybrid 关闭当前策略。
	DisableHybrid(ctx context.Context) (model.StorageHybridPolicy, error)
	// ClaimHybrid 认领待执行的启用请求。
	ClaimHybrid(ctx context.Context, token string, now time.Time) (*model.StorageHybridPolicy, error)
	// RenewHybrid 延长工作者的认领。
	RenewHybrid(ctx context.Context, token string, now time.Time) error
	// PinHybrid 锁定通过检查的配置。
	PinHybrid(ctx context.Context, token string, cloudID, localID uint64, version int) error
	// FinishHybrid 保存完成结果。
	FinishHybrid(ctx context.Context, token, reason string) error
	// ReleaseHybrid 交还关停期间未完成的任务。
	ReleaseHybrid(ctx context.Context, token string) error
}

// StorageHybridView 是管理端对混合策略的状态与操作能力。
type StorageHybridView struct {
	model.StorageHybridPolicy
	HasCloud bool `json:"has_cloud"`
}

// StorageHybridService 协调异步启用并把素材 I/O 路由到当前策略选中的存储。
type StorageHybridService struct {
	repo   StorageHybridRepo
	stores StoreRegistry
	cfg    config.ReferenceCache
}

// NewStorageHybridService 创建混合策略服务，复用存储配置与已解密客户端。
func NewStorageHybridService(repo StorageHybridRepo, stores StoreRegistry, cfg config.ReferenceCache) *StorageHybridService {
	return &StorageHybridService{repo: repo, stores: stores, cfg: cfg.WithDefaults()}
}

// Status 返回全局策略和对象存储是否存在，不受列表分页影响。
func (s *StorageHybridService) Status(ctx context.Context) (StorageHybridView, error) {
	// 1. 策略状态与候选配置一并返回，列表筛选不会隐藏启用入口。
	policy, err := s.repo.Hybrid(ctx)
	if err != nil {
		return StorageHybridView{}, storageNotFound(err)
	}
	rows, err := s.repo.List(ctx)
	if err != nil {
		return StorageHybridView{}, storageNotFound(err)
	}
	return StorageHybridView{StorageHybridPolicy: policy, HasCloud: len(hybridCandidates(rows)) > 0}, nil
}

// Enable 提交一次启用请求；同步结束前仍使用原来的默认存储。
func (s *StorageHybridService) Enable(ctx context.Context, limit int64) (StorageHybridView, error) {
	// 1. 先验证限额和基本候选条件，耗时的磁盘与云端检查由后台任务执行。
	if limit < 0 {
		return StorageHybridView{}, errcode.ErrStorageInvalid.WithMsg("限额必须大于或等于 0")
	}
	view, err := s.Status(ctx)
	if err != nil {
		return StorageHybridView{}, err
	}
	if !view.HasCloud {
		return StorageHybridView{}, errcode.ErrStorageInvalid.WithMsg("至少需要一套对象存储配置")
	}
	// 2. 启用任务持久化后返回，浏览器超时或刷新不会打断文件同步。
	policy, err := s.repo.BeginHybrid(ctx, limit)
	return StorageHybridView{StorageHybridPolicy: policy, HasCloud: true}, storageNotFound(err)
}

// Disable 关闭策略并恢复原默认存储；已复制和已上传的素材维持各自位置。
func (s *StorageHybridService) Disable(ctx context.Context) (StorageHybridView, error) {
	// 1. 关闭仅更改策略；原有默认存储字段始终保留。
	if _, err := s.repo.DisableHybrid(ctx); err != nil {
		return StorageHybridView{}, storageNotFound(err)
	}
	return s.Status(ctx)
}

// Default 返回素材的有效写入存储，启用期间为本地磁盘。
func (s *StorageHybridService) Default(ctx context.Context) (*storage.Handle, error) {
	// 1. 每次读取已提交的策略，各实例不依赖旧默认指针的缓存到期。
	policy, err := s.repo.Hybrid(ctx)
	if err != nil {
		return nil, err
	}
	if policy.State == model.StorageHybridActive {
		return s.stores.Get(ctx, policy.LocalID)
	}
	return s.stores.Default(ctx)
}

// Get 按原有登记位置读取存储，用于完成已有上传意图或原始文件操作。
func (s *StorageHybridService) Get(ctx context.Context, id uint64) (*storage.Handle, error) {
	return s.stores.Get(ctx, id)
}

// currentHybridStore 避免跨实例启用时使用修改前的云端客户端。
func currentHybridStore(ctx context.Context, stores StoreRegistry, id uint64) (*storage.Handle, error) {
	if current, ok := stores.(interface {
		GetCurrent(context.Context, uint64) (*storage.Handle, error)
	}); ok {
		return current.GetCurrent(ctx, id)
	}
	return stores.Get(ctx, id)
}

// ForAsset 为入选云端存储的素材优先使用本地镜像，缺少镜像时仍可读原文件。
func (s *StorageHybridService) ForAsset(ctx context.Context, a *model.Asset) (*storage.Handle, error) {
	// 1. 使用素材登记的存储 ID 筛选，第三套存储的文件即使同名也照旧读取。
	policy, err := s.repo.Hybrid(ctx)
	if err != nil {
		return nil, err
	}
	if policy.State == model.StorageHybridActive && policy.CloudID == a.StorageID {
		if local := s.mirrorForAsset(ctx, policy.LocalID, a); local != nil {
			return local, nil
		}
		return currentHybridStore(ctx, s.stores, a.StorageID)
	}
	return s.stores.Get(ctx, a.StorageID)
}

// mirrorForAsset 按素材记录核对镜像大小和内容摘要。
func (s *StorageHybridService) mirrorForAsset(ctx context.Context, localID uint64, a *model.Asset) *storage.Handle {
	h, err := s.stores.Get(ctx, localID)
	if err != nil {
		return nil
	}
	l, ok := h.Storage.(*storage.LocalStorage)
	if !ok {
		return nil
	}
	if l.MirrorMatches(ctx, a.StorageKey, a.ByteSize, a.Sha256) {
		return h
	}
	return nil
}

// RunPending 认领并推进一个后台启用任务；重启时继续检查未完成的任务。
func (s *StorageHybridService) RunPending(ctx context.Context) error {
	// 1. PostgreSQL 租约避免多个 worker 同时启用。
	token := uuid.NewString()
	job, err := s.repo.ClaimHybrid(ctx, token, time.Now())
	if err != nil || job == nil {
		return err
	}

	// 2. 探测与复制可持续较长时间，独立续租覆盖只有一个大文件的场景。
	work, cancel := context.WithCancel(ctx)
	defer cancel()
	leaseDone := make(chan struct{})
	go s.renewHybridLease(work, cancel, token, leaseDone)
	reason := s.activateHybrid(work, token, job.LimitBytes)
	interrupted := work.Err() != nil
	cancel()
	<-leaseDone

	// 3. 只有仍持有租约才可发布结果；关停中把任务交给下一实例。
	saveCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer stop()
	if interrupted || ctx.Err() != nil {
		err = s.repo.ReleaseHybrid(saveCtx, token)
	} else {
		err = s.repo.FinishHybrid(saveCtx, token, reason)
	}
	if errors.Is(err, repository.ErrRevisionConflict) {
		return nil
	}
	return err
}

// renewHybridLease 续租失败就终止本轮复制，避免旧 worker 发布结果。
func (s *StorageHybridService) renewHybridLease(ctx context.Context, cancel context.CancelFunc, token string, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			renew, stop := context.WithTimeout(ctx, 5*time.Second)
			err := s.repo.RenewHybrid(renew, token, time.Now())
			stop()
			if err != nil {
				cancel()
				return
			}
		}
	}
}
