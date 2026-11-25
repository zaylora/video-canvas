package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"video-canvas/internal/config"
	"video-canvas/internal/model"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
	"video-canvas/internal/storage"
)

// ReferenceCacheRepository 持久化对象容量、任务租用以及跨 worker 的回收状态。
type ReferenceCacheRepository interface {
	// Reserve 原子复用或预留对象，并登记发起任务。
	Reserve(ctx context.Context, req model.ReferenceCacheReservation) (model.ReferenceCacheObject, error)
	// MarkReady 确认对象元信息已核验，同时保护签名有效期。
	MarkReady(ctx context.Context, key string, taskID uint64, until time.Time) error
	// ClaimCleanup 隔离可回收的 key；没有候选时返回 nil。
	ClaimCleanup(ctx context.Context, req model.ReferenceCacheSweep) (*model.ReferenceCacheObject, error)
	// TrackOrphan 登记没有数据库记录的临时对象；不覆盖已有记录。
	TrackOrphan(ctx context.Context, object model.ReferenceCacheObject) error
	// FinishCleanup 在对象确认不存在后释放容量。
	FinishCleanup(ctx context.Context, key string) error
	// Bytes 返回对象和预留的计费总量。
	Bytes(ctx context.Context) (int64, error)
}

const (
	referenceCacheLifecycle       = 7 * 24 * time.Hour
	referenceCacheRetention       = 3 * 24 * time.Hour
	referenceCacheLifecycleBuffer = time.Hour
	referenceCacheSettlement      = 10 * time.Minute
	referenceCacheRequestReserve  = 3 * time.Second
	referenceCacheMaxFile         = 5 << 30 // R2 单次条件 PUT 的对象大小上限
	referenceCacheKeyVersion      = "v1/"
)

// ReferenceCache 为本地参考素材创建临时外链，长期素材与缓存对象分别管理。
type ReferenceCache struct {
	cfg    config.ReferenceCache
	stores StoreRegistry
	repo   ReferenceCacheRepository
	*referenceCacheCounters
	hybrid bool
}

type referenceCacheCounters struct {
	hits, uploaded, failures, removed atomic.Int64
}

var _ provider.ReferenceURLPreparer = (*ReferenceCache)(nil)

// NewReferenceCache 使用既有存储注册表以及 PostgreSQL 容量与活动记录。
func NewReferenceCache(cfg config.ReferenceCache, stores StoreRegistry, repo ReferenceCacheRepository) *ReferenceCache {
	return &ReferenceCache{cfg: cfg.WithDefaults(), stores: stores, repo: repo, referenceCacheCounters: &referenceCacheCounters{}}
}

type referenceTarget struct {
	bucket    storage.ReferenceStorage
	namespace string
	prefix    string
}

// PrepareURL 按存储、源文件路径和内容识别临时对象；准备失败时没有发生上游生成请求。
func (s *ReferenceCache) PrepareURL(ctx context.Context, task provider.TaskRef, file *provider.AssetFile) (signed string, err error) {
	// 1. 给 R2 准备单列短时限，并留出上游提交窗口。
	defer func() {
		if err != nil {
			s.failures.Add(1)
		}
	}()
	start := time.Now()
	deadline, err := s.prepareDeadline(ctx, task, start)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	// 2. 从注册表获取独立的私有 R2 配置；按内容和源路径登记临时对象。
	target, err := s.referenceTarget(ctx)
	if err != nil {
		return "", err
	}
	source, err := s.referenceSource(ctx, task, file)
	if err != nil {
		return "", err
	}
	defer source.close()
	object, err := s.reserveReference(ctx, task.ID, target, source, start)
	if err != nil {
		return "", err
	}

	// 3. HEAD 核验并条件上传。只有确认对象与签名的任务保护已写入数据库才返回 URL。
	if err := s.ensureReferenceObject(ctx, target.bucket, object, source); err != nil {
		return "", err
	}
	return s.signReference(ctx, target.bucket, object.Key, task.ID)
}

// prepareDeadline 确保签名能覆盖任务窗口，并把所有准备工作限制在原请求截止时间内。
func (s *ReferenceCache) prepareDeadline(ctx context.Context, task provider.TaskRef, now time.Time) (time.Time, error) {
	if !validReferenceCacheLimits(s.cfg) {
		return time.Time{}, referenceCacheError(provider.ClassTerminal, provider.CodeReferenceCacheConfig, "临时素材的签名、准备时限或容量配置不合法")
	}
	if task.ID == 0 {
		return time.Time{}, referenceCacheError(provider.ClassTerminal, provider.CodeReferenceCacheConfig, "临时参考素材需要已登记的生成任务")
	}
	parent, hasDeadline := ctx.Deadline()
	fetchWindow := task.ReferenceFetchWindow
	if fetchWindow > 0 && hasDeadline {
		fetchWindow += max(0, parent.Sub(now))
	}
	required := max(task.DeadlineAt.Sub(now), fetchWindow)
	if required >= s.cfg.SignedTTL-referenceCacheRequestReserve {
		return time.Time{}, referenceCacheError(provider.ClassTerminal, provider.CodeReferenceCacheConfig, "临时参考素材的签名时长不足以覆盖生成任务")
	}
	deadline := now.Add(s.cfg.PrepareTimeout)
	if !task.DeadlineAt.IsZero() {
		deadline = deadlineMin(deadline, task.DeadlineAt.Add(-referenceCacheRequestReserve))
	}
	if hasDeadline {
		deadline = deadlineMin(deadline, parent.Add(-referenceCacheRequestReserve))
	}
	if !deadline.After(now) {
		return time.Time{}, referenceCacheError(provider.ClassRetryable, provider.CodeReferenceCacheTimeout, "临时参考素材准备窗口不足")
	}
	return deadline, nil
}

// validReferenceCacheLimits 核对签名及准备窗口与临时对象的清理时限能够共存。
func validReferenceCacheLimits(cfg config.ReferenceCache) bool {
	return cfg.SignedTTL >= time.Second && cfg.SignedTTL <= 24*time.Hour &&
		cfg.PrepareTimeout > 0 && cfg.PrepareTimeout < referenceCacheSettlement &&
		cfg.UnknownGrace > 0 && cfg.UnknownGrace < referenceCacheLifecycle-cfg.SignedTTL-referenceCacheLifecycleBuffer &&
		cfg.MaxBytes >= 0
}

// deadlineMin 返回两个截止时刻中较早的一个。
func deadlineMin(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

// referenceTarget 识别真实桶和业务前缀；更换到另一桶时不会签发或删除旧桶的对象。
func (s *ReferenceCache) referenceTarget(ctx context.Context) (referenceTarget, error) {
	prefix := strings.TrimSuffix(s.cfg.Prefix, "/") + "/"
	if s.cfg.StorageID == 0 || !validReferenceCachePrefix(prefix) {
		return referenceTarget{}, referenceCacheError(provider.ClassTerminal, provider.CodeReferenceCacheConfig, "临时素材需要有效的目标存储 ID 和 ref-cache/ 下的专用前缀")
	}
	get := s.stores.Get
	if s.hybrid {
		get = func(ctx context.Context, id uint64) (*storage.Handle, error) {
			return currentHybridStore(ctx, s.stores, id)
		}
	}
	handle, err := get(ctx, s.cfg.StorageID)
	if errors.Is(err, storage.ErrStorageNotFound) {
		return referenceTarget{}, referenceCacheError(provider.ClassTerminal, provider.CodeReferenceCacheConfig, "临时素材目标存储不存在")
	}
	if err != nil {
		return referenceTarget{}, referenceOperationError(ctx, err, "无法读取临时素材目标存储配置")
	}
	bucket, ok := handle.Storage.(storage.ReferenceStorage)
	if s.hybrid {
		compatible, err := hybridReferenceBucket(handle)
		if err != nil {
			return referenceTarget{}, referenceCacheError(provider.ClassTerminal, provider.CodeReferenceCacheConfig, err.Error())
		}
		bucket, ok = compatible, true
	}
	if !ok || (!s.hybrid && handle.Provider != storage.ProviderR2) || handle.Spec.PublicBaseURL != "" {
		return referenceTarget{}, referenceCacheError(provider.ClassTerminal, provider.CodeReferenceCacheConfig, "临时素材目标必须是支持 HTTPS 的私有 R2 配置")
	}
	prefix += referenceCacheKeyVersion
	account := handle.Spec.AccountID
	if handle.Provider != storage.ProviderR2 {
		account = handle.Spec.Endpoint
	}
	namespace := referenceHash(account, handle.Spec.Bucket, strings.Trim(handle.Spec.PathPrefix, "/"), prefix)
	return referenceTarget{bucket: bucket, namespace: namespace, prefix: prefix}, nil
}

// validReferenceCachePrefix 校验前缀及追加对象键后仍符合存储路径长度。
func validReferenceCachePrefix(prefix string) bool {
	prefix = strings.TrimSuffix(prefix, "/") + "/"
	return strings.HasPrefix(prefix, "ref-cache/") &&
		storage.ValidateKey(prefix+referenceCacheKeyVersion+strings.Repeat("a", 101)) == nil
}

// reserveReference 争取一个符合生命周期的对象；容量不足时先尝试回收。
func (s *ReferenceCache) reserveReference(ctx context.Context, taskID uint64, target referenceTarget, source referenceSource, now time.Time) (model.ReferenceCacheObject, error) {
	identity := referenceHash(fmt.Sprint(source.asset.StorageID), source.asset.StorageKey, source.digest)
	req := model.ReferenceCacheReservation{
		Object: model.ReferenceCacheObject{
			Key: target.prefix + identity + "/" + newAssetUUID(), Namespace: target.namespace,
			Identity: identity, Digest: source.digest, Size: source.size,
			CreatedAt: now, LastUsedAt: now, BusyUntil: now.Add(referenceCacheSettlement),
		},
		TaskID: taskID, MaxBytes: s.cfg.MaxBytes,
		ReuseAfter: now.Add(-referenceCacheLifecycle + s.cfg.SignedTTL + s.cfg.UnknownGrace + referenceCacheLifecycleBuffer),
	}
	obj, err := s.repo.Reserve(ctx, req)
	if errors.Is(err, repository.ErrReferenceCacheCapacity) {
		if err := s.reclaimReferences(ctx, target, 12); err != nil {
			return model.ReferenceCacheObject{}, err
		}
		obj, err = s.repo.Reserve(ctx, req)
	}
	if errors.Is(err, repository.ErrReferenceCacheCapacity) {
		return obj, referenceCacheError(provider.ClassRetryable, provider.CodeReferenceCacheCapacity, "R2 临时参考素材容量不足")
	}
	if err != nil {
		return obj, referenceOperationError(ctx, err, "无法登记临时参考素材容量")
	}
	return obj, nil
}

// ensureReferenceObject 只在对象级 404 后上传；并发写入或响应丢失时沿用同一个对象代际。
func (s *ReferenceCache) ensureReferenceObject(ctx context.Context, bucket storage.ReferenceStorage, object model.ReferenceCacheObject, source referenceSource) error {
	info, err := bucket.StatReference(ctx, object.Key)
	if err == nil {
		s.hits.Add(1)
		return verifyReferenceObject(info, object)
	}
	if !errors.Is(err, storage.ErrNotFound) {
		return referenceOperationError(ctx, err, "无法检查 R2 临时对象")
	}
	err = bucket.PutReference(ctx, object.Key, source.body, source.size, source.asset.MimeType, source.digest)
	switch {
	case err == nil:
		s.uploaded.Add(source.size)
	case errors.Is(err, storage.ErrObjectExists):
		s.hits.Add(1)
	default:
		return referenceOperationError(ctx, err, "无法上传 R2 临时参考素材")
	}
	info, err = bucket.StatReference(ctx, object.Key)
	if err != nil {
		return referenceOperationError(ctx, err, "无法复核 R2 临时参考素材")
	}
	return verifyReferenceObject(info, object)
}

// verifyReferenceObject 核对对象大小和摘要，不签发不同内容的对象。
func verifyReferenceObject(info storage.ReferenceObjectInfo, object model.ReferenceCacheObject) error {
	if info.Size != object.Size || info.SHA256 != object.Digest {
		return referenceCacheError(provider.ClassTerminal, provider.CodeReferenceCacheConfig, "R2 临时参考素材的大小或摘要不匹配")
	}
	return nil
}

// signReference 签发 HTTPS GET，并把使用保护写到持久记录。
func (s *ReferenceCache) signReference(ctx context.Context, bucket storage.ReferenceStorage, key string, taskID uint64) (string, error) {
	signed, err := bucket.URL(ctx, key, s.cfg.SignedTTL)
	if err != nil {
		return "", referenceOperationError(ctx, err, "无法签发 R2 临时参考素材地址")
	}
	u, err := url.Parse(signed)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Query().Get("X-Amz-Signature") == "" {
		return "", referenceCacheError(provider.ClassTerminal, provider.CodeReferenceCacheConfig, "R2 临时参考素材没有返回 HTTPS GET 签名")
	}
	if err := s.repo.MarkReady(ctx, key, taskID, time.Now().Add(s.cfg.SignedTTL)); err != nil {
		return "", referenceOperationError(ctx, err, "无法登记临时参考素材的任务使用记录")
	}
	return signed, nil
}

// referenceCacheError 只报告安全的操作与分类，不携带上游 URL、凭证或内容。
func referenceCacheError(class provider.ErrorClass, code, message string) *provider.Error {
	return &provider.Error{Class: class, Code: code, Message: message}
}

// referenceOperationError 在安全文案中区分权限、不合法的 R2 目标和可重试的网络错误。
func referenceOperationError(ctx context.Context, err error, message string) *provider.Error {
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded), errors.Is(err, context.DeadlineExceeded):
		return referenceCacheError(provider.ClassRetryable, provider.CodeReferenceCacheTimeout, message+"：准备超时")
	case errors.Is(err, storage.ErrReferenceAccessDenied):
		return referenceCacheError(provider.ClassTerminal, provider.CodeReferenceCachePermission, message+"：R2 拒绝访问（401/403）")
	case errors.Is(err, storage.ErrInvalidSpec):
		return referenceCacheError(provider.ClassTerminal, provider.CodeReferenceCacheConfig, message+"：目标存储配置无效")
	case errors.Is(err, storage.ErrReferenceBucketMissing):
		return referenceCacheError(provider.ClassTerminal, provider.CodeReferenceCacheConfig, message+"：R2 目标桶不存在")
	default:
		return referenceCacheError(provider.ClassRetryable, provider.CodeReferenceCacheUnavailable, message)
	}
}

type referenceSource struct {
	asset  *model.Asset
	body   io.Reader
	size   int64
	digest string
	close  func()
}
