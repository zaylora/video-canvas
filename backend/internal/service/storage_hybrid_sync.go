package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"video-canvas/internal/model"
	"video-canvas/internal/storage"
)

// hybridCandidates 优先采用已设为默认的对象存储，其余按存储列表的 ID 顺序尝试。
func hybridCandidates(rows []model.StorageConfig) []model.StorageConfig {
	var out []model.StorageConfig
	for _, row := range rows {
		if row.Provider != storage.ProviderLocal {
			out = append(out, row)
		}
	}
	slices.SortStableFunc(out, func(a, b model.StorageConfig) int {
		switch {
		case a.IsDefault && !b.IsDefault:
			return -1
		case !a.IsDefault && b.IsDefault:
			return 1
		default:
			return 0
		}
	})
	return out
}

// activateHybrid 先检查本地磁盘与云端配置，选中后只把缺失对象写入本地。
func (s *StorageHybridService) activateHybrid(ctx context.Context, token string, limit int64) string {
	cfg := s.cfg
	cfg.MaxBytes = limit
	if !validReferenceCacheLimits(cfg) {
		return "部署的临时外链签名或准备时限配置不合法"
	}
	if !validReferenceCachePrefix(cfg.Prefix) {
		return "临时素材前缀必须位于 ref-cache/ 下"
	}
	rows, err := s.repo.List(ctx)
	if err != nil {
		return "无法读取存储配置，请稍后重试"
	}
	localID, local, reason := s.hybridLocal(ctx, rows)
	if reason != "" {
		return reason
	}
	var reasons []string
	for _, row := range hybridCandidates(rows) {
		bucket, err := s.checkHybridCloud(ctx, row)
		if err != nil {
			reasons = append(reasons, row.Name+"："+err.Error())
			continue
		}
		if err := s.repo.PinHybrid(ctx, token, row.ID, localID, row.Version); err != nil {
			reasons = append(reasons, row.Name+"：配置已变化，请重试")
			continue
		}
		if err := copyHybridObjects(ctx, local, bucket); err != nil {
			return row.Name + " 同步失败：" + err.Error()
		}
		return ""
	}
	if len(reasons) == 0 {
		return "没有可用的对象存储配置"
	}
	return "没有可用的对象存储配置。" + strings.Join(reasons, "；")
}

// hybridLocal 核对已挂载的本地磁盘可以写入并原子发布镜像。
func (s *StorageHybridService) hybridLocal(ctx context.Context, rows []model.StorageConfig) (uint64, *storage.LocalStorage, string) {
	for _, row := range rows {
		if !row.Builtin || row.Provider != storage.ProviderLocal {
			continue
		}
		h, err := s.stores.Get(ctx, row.ID)
		if err != nil {
			break
		}
		local, ok := h.Storage.(*storage.LocalStorage)
		if !ok {
			break
		}
		key := "hybrid-test-" + uuid.NewString()
		if err := local.PutIfMissing(ctx, key, strings.NewReader("ok"), 2); err != nil {
			return 0, nil, "本地存储无法写入或不支持原子创建，请检查挂载目录、权限和可用空间"
		}
		if err := local.Delete(ctx, key); err != nil {
			return 0, nil, "本地存储无法清理检查文件，请检查挂载目录权限"
		}
		return row.ID, local, ""
	}
	return 0, nil, "没有可用的内置本地存储，请检查数据目录配置"
}

// checkHybridCloud 用隔离探针检查读写权限、条件上传、列举和签名能力。
func (s *StorageHybridService) checkHybridCloud(ctx context.Context, row model.StorageConfig) (storage.ReferenceStorage, error) {
	ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	h, err := currentHybridStore(ctx, s.stores, row.ID)
	if err != nil {
		return nil, errors.New("无法取得云端存储配置或密钥")
	}
	if h.Version != row.Version {
		return nil, errors.New("配置已变化，请重试")
	}
	bucket, err := hybridReferenceBucket(h)
	if err != nil {
		return nil, err
	}
	key := strings.TrimSuffix(s.cfg.Prefix, "/") + "/hybrid-probe/" + uuid.NewString()
	if err := probeHybridObject(ctx, bucket, key); err != nil {
		return nil, err
	}
	return bucket, nil
}

// hybridReferenceBucket 确保临时引用可用 HTTPS 签名提供，云端资源能安全重新定位。
func hybridReferenceBucket(h *storage.Handle) (storage.ReferenceStorage, error) {
	bucket, ok := h.Storage.(storage.ReferenceStorage)
	if !ok || !h.Spec.UseSSL || h.Spec.PublicBaseURL != "" {
		return nil, errors.New("需要支持 HTTPS 签名的私有对象存储")
	}
	return bucket, nil
}

// probeHybridObject 校验只属于本次探针的临时对象，清理时也只使用探针 key。
func probeHybridObject(ctx context.Context, bucket storage.ReferenceStorage, key string) (err error) {
	const content = "storage-hybrid"
	digest := referenceHash(content)
	if err := bucket.PutReference(ctx, key, strings.NewReader(content), int64(len(content)), "text/plain", digest); err != nil {
		return errors.New("无法创建临时对象，请检查桶状态与写权限")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if deleteErr := bucket.Delete(cleanup, key); deleteErr != nil && err == nil {
			err = errors.New("缺少临时对象删除权限")
		}
	}()
	if err := bucket.PutReference(ctx, key, strings.NewReader(content), int64(len(content)), "text/plain", digest); !errors.Is(err, storage.ErrObjectExists) {
		return errors.New("云端配置未提供可验证的防覆盖条件写入")
	}
	info, err := bucket.StatReference(ctx, key)
	if err != nil || info.Size != int64(len(content)) || info.SHA256 != digest {
		return errors.New("云端读取或对象元信息校验失败")
	}
	signed, err := bucket.URL(ctx, key, time.Minute)
	u, parseErr := url.Parse(signed)
	if err != nil || parseErr != nil || u.Scheme != "https" || u.Query().Get("X-Amz-Signature") == "" {
		return errors.New("无法签发 HTTPS 下载地址")
	}
	seen := errors.New("已验证云端列举")
	if err := bucket.ListReferences(ctx, "", func(string, int64, time.Time) error { return seen }); err != nil && !errors.Is(err, seen) {
		return errors.New("缺少云端对象列举权限")
	}
	return nil
}

// copyHybridObjects 扫描配置前缀下的资源，临时外链对象不属于长期素材。
func copyHybridObjects(ctx context.Context, local *storage.LocalStorage, bucket storage.ReferenceStorage) error {
	var syncErr error
	err := bucket.ListReferences(ctx, "", func(key string, size int64, _ time.Time) error {
		if strings.HasPrefix(key, "ref-cache/") || (strings.HasSuffix(key, "/") && size == 0) {
			return nil
		}
		syncErr = copyMissingHybridObject(ctx, local, bucket, key, size)
		return syncErr
	})
	if syncErr != nil {
		return syncErr
	}
	if err != nil {
		return errors.New("云端清单读取中断，请检查网络和对象列举权限")
	}
	return nil
}

// copyMissingHybridObject 保留已有本地文件，按云端原路径分块复制缺失文件。
func copyMissingHybridObject(ctx context.Context, local *storage.LocalStorage, bucket storage.Storage, key string, size int64) error {
	if storage.ValidateKey(key) != nil || size < 0 || size == math.MaxInt64 {
		return errors.New("云端配置前缀中含有无法映射到本地的对象")
	}
	if _, err := local.MirrorSize(key); !errors.Is(err, storage.ErrNotFound) {
		if err != nil {
			return fmt.Errorf("无法检查本地文件 %s", key)
		}
		return nil
	}
	copyCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	body, err := bucket.Open(copyCtx, key)
	if errors.Is(err, storage.ErrNotFound) {
		return nil // 清单读取后已被源侧删除的对象无需迁回。
	}
	if err != nil {
		return fmt.Errorf("无法读取云端文件 %s", key)
	}
	defer body.Close()
	if err := local.PutIfMissing(copyCtx, key, body, size); err != nil && !errors.Is(err, storage.ErrObjectExists) {
		return fmt.Errorf("无法完整写入本地文件 %s，请检查磁盘空间和权限", key)
	}
	return nil
}
