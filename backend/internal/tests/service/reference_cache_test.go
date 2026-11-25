package service_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"video-canvas/internal/config"
	"video-canvas/internal/model"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
	"video-canvas/internal/storage"
)

type referenceBucket struct {
	storage.Storage
	objects      map[string]storage.ReferenceObjectInfo
	puts         int
	headErr      error
	putErr       error
	deleteSticks bool
	listed       map[string]time.Time
}

func (b *referenceBucket) StatReference(_ context.Context, key string) (storage.ReferenceObjectInfo, error) {
	if b.headErr != nil {
		return storage.ReferenceObjectInfo{}, b.headErr
	}
	if info, ok := b.objects[key]; ok {
		return info, nil
	}
	return storage.ReferenceObjectInfo{}, storage.ErrNotFound
}

func (b *referenceBucket) PutReference(_ context.Context, key string, body io.Reader, size int64, _, digest string) error {
	if _, ok := b.objects[key]; ok {
		return storage.ErrObjectExists
	}
	raw, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	if int64(len(raw)) != size || fmt.Sprintf("%x", sha256.Sum256(raw)) != digest {
		return fmt.Errorf("上传的内容和声明不匹配")
	}
	b.puts++
	b.objects[key] = storage.ReferenceObjectInfo{Size: size, SHA256: digest}
	return b.putErr
}

func (b *referenceBucket) URL(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://example.com/" + key + "?X-Amz-Signature=redact-me", nil
}

func (b *referenceBucket) ListReferences(_ context.Context, _ string, visit func(string, int64, time.Time) error) error {
	for key, updated := range b.listed {
		if err := visit(key, b.objects[key].Size, updated); err != nil {
			return err
		}
	}
	return nil
}

type referenceStores struct {
	service.StoreRegistry
	bucket *referenceBucket
}

func (s referenceStores) Get(_ context.Context, id uint64) (*storage.Handle, error) {
	return &storage.Handle{ID: id, Provider: storage.ProviderR2, Storage: s.bucket,
		Spec: storage.Spec{Endpoint: "abc.r2.cloudflarestorage.com", Bucket: "private-test", UseSSL: true}}, nil
}

type referenceRows struct {
	service.ReferenceCacheRepository
	rows       map[string]model.ReferenceCacheObject
	uses       []uint64
	cleanupKey string
	finished   int
	tracked    []string
}

func (r *referenceRows) Reserve(_ context.Context, req model.ReferenceCacheReservation) (model.ReferenceCacheObject, error) {
	r.uses = append(r.uses, req.TaskID)
	if row, ok := r.rows[req.Object.Identity]; ok {
		return row, nil
	}
	if req.MaxBytes > 0 && req.Object.Size > req.MaxBytes {
		return model.ReferenceCacheObject{}, repository.ErrReferenceCacheCapacity
	}
	r.rows[req.Object.Identity] = req.Object
	return req.Object, nil
}

func (r *referenceRows) MarkReady(context.Context, string, uint64, time.Time) error {
	return nil
}

func TestReferenceCacheMissHitAndLegacyDigest(t *testing.T) {
	bucket := &referenceBucket{objects: make(map[string]storage.ReferenceObjectInfo)}
	rows := &referenceRows{rows: make(map[string]model.ReferenceCacheObject)}
	svc := service.NewReferenceCache(config.ReferenceCache{StorageID: 13}, referenceStores{bucket: bucket}, rows)
	content := []byte("reference-image")
	asset := &model.Asset{ID: 17, UserID: 9, StorageID: 1, StorageKey: "u9/image.png", ByteSize: int64(len(content)), MimeType: "image/png"}
	first, err := svc.PrepareURL(t.Context(), provider.TaskRef{ID: 31, UserID: 9},
		&provider.AssetFile{Asset: asset, Body: io.NopCloser(bytes.NewReader(content))})
	if err != nil {
		t.Fatal(err)
	}
	asset.Sha256 = fmt.Sprintf("%x", sha256.Sum256(content))
	second, err := svc.PrepareURL(t.Context(), provider.TaskRef{ID: 32, UserID: 9},
		&provider.AssetFile{Asset: asset, Body: io.NopCloser(bytes.NewReader(content))})
	if err != nil {
		t.Fatal(err)
	}
	if first != second || !strings.Contains(second, "/ref-cache/") || bucket.puts != 1 || len(rows.uses) != 2 {
		t.Fatalf("应按旧素材计算的摘要复用对象并登记两个任务: puts=%d uses=%v", bucket.puts, rows.uses)
	}
}

func TestReferenceCacheHEADFailureDoesNotUploadOrLeak(t *testing.T) {
	bucket := &referenceBucket{headErr: fmt.Errorf("应被脱敏的对象存储错误: %w", storage.ErrReferenceAccessDenied)}
	rows := &referenceRows{rows: make(map[string]model.ReferenceCacheObject)}
	svc := service.NewReferenceCache(config.ReferenceCache{StorageID: 13}, referenceStores{bucket: bucket}, rows)
	asset := &model.Asset{UserID: 9, StorageID: 1, StorageKey: "u9/image.png", ByteSize: 1, Sha256: fmt.Sprintf("%x", sha256.Sum256([]byte("a")))}
	_, err := svc.PrepareURL(t.Context(), provider.TaskRef{ID: 31, UserID: 9},
		&provider.AssetFile{Asset: asset, Body: io.NopCloser(strings.NewReader("a"))})
	if provider.CodeOf(err) != provider.CodeReferenceCachePermission || strings.Contains(err.Error(), "应被脱敏") || bucket.puts != 0 {
		t.Fatalf("HEAD 失败必须脱敏且不能触发上传: err=%v puts=%d", err, bucket.puts)
	}
}

func (b *referenceBucket) Delete(_ context.Context, key string) error {
	if !b.deleteSticks {
		delete(b.objects, key)
	}
	return nil
}

func (r *referenceRows) ClaimCleanup(_ context.Context, _ model.ReferenceCacheSweep) (*model.ReferenceCacheObject, error) {
	if r.cleanupKey == "" {
		return nil, nil
	}
	return &model.ReferenceCacheObject{Key: r.cleanupKey, Size: 2}, nil
}

func (r *referenceRows) TrackOrphan(_ context.Context, obj model.ReferenceCacheObject) error {
	r.tracked = append(r.tracked, obj.Key)
	r.cleanupKey = obj.Key
	return nil
}

func (r *referenceRows) FinishCleanup(context.Context, string) error {
	r.cleanupKey = ""
	r.finished++
	return nil
}

func (r *referenceRows) Bytes(context.Context) (int64, error) {
	if r.cleanupKey != "" {
		return 2, nil
	}
	return 0, nil
}

func TestReferenceCacheRecoversLostUploadResponse(t *testing.T) {
	bucket := &referenceBucket{objects: make(map[string]storage.ReferenceObjectInfo), putErr: io.ErrUnexpectedEOF}
	rows := &referenceRows{rows: make(map[string]model.ReferenceCacheObject)}
	svc := service.NewReferenceCache(config.ReferenceCache{StorageID: 13}, referenceStores{bucket: bucket}, rows)
	asset := &model.Asset{UserID: 9, StorageID: 1, StorageKey: "u9/image.png", ByteSize: 1, Sha256: fmt.Sprintf("%x", sha256.Sum256([]byte("a")))}
	task := provider.TaskRef{ID: 31, UserID: 9}
	file := func() *provider.AssetFile {
		return &provider.AssetFile{Asset: asset, Body: io.NopCloser(strings.NewReader("a"))}
	}
	if _, err := svc.PrepareURL(t.Context(), task, file()); provider.ClassOf(err) != provider.ClassRetryable {
		t.Fatalf("响应丢失应在提交前重试：%v", err)
	}
	// 用新的服务实例重试，模拟进程重启，只保留对象和数据库记录。
	restarted := service.NewReferenceCache(config.ReferenceCache{StorageID: 13}, referenceStores{bucket: bucket}, rows)
	if _, err := restarted.PrepareURL(t.Context(), task, file()); err != nil || bucket.puts != 1 {
		t.Fatalf("重试应按持久记录做 HEAD 复用：puts=%d err=%v", bucket.puts, err)
	}
}

func TestReferenceCacheCleanupWaitsForConfirmedDeletion(t *testing.T) {
	key := "ref-cache/v1/" + strings.Repeat("a", 64) + "/9ab9bf78-0634-4c07-bbfa-58c07331a04e"
	bucket := &referenceBucket{
		objects: map[string]storage.ReferenceObjectInfo{key: {Size: 2}}, deleteSticks: true,
	}
	rows := &referenceRows{cleanupKey: key}
	svc := service.NewReferenceCache(config.ReferenceCache{StorageID: 13}, referenceStores{bucket: bucket}, rows)
	stats, err := svc.Maintain(t.Context())
	if err == nil || stats.ManagedBytes != 2 || rows.finished != 0 {
		t.Fatalf("删除后 HEAD 仍可见时必须保留容量：stats=%+v err=%v", stats, err)
	}
	bucket.deleteSticks = false
	stats, err = svc.Maintain(t.Context())
	if err != nil || stats.ManagedBytes != 0 || stats.Removed != 1 || rows.finished != 1 {
		t.Fatalf("明确 404 后应释放容量：stats=%+v err=%v", stats, err)
	}
}

func TestReferenceCacheReconciliationOnlyCollectsExpiredOwnedKeys(t *testing.T) {
	base := "ref-cache/v1/" + strings.Repeat("b", 64)
	orphan := base + "/28c6b230-f6a7-4ad0-a7a7-9d60dd2cbb2e"
	recent := base + "/08baa760-c9db-4fcd-97ab-4796b5fb9a51"
	unrelated := "user-generations/untouched.png"
	now := time.Now()
	bucket := &referenceBucket{
		objects: map[string]storage.ReferenceObjectInfo{
			orphan: {Size: 2}, recent: {Size: 2}, unrelated: {Size: 2},
		},
		listed: map[string]time.Time{orphan: now.Add(-time.Hour), recent: now, unrelated: now.Add(-time.Hour)},
	}
	rows := &referenceRows{}
	svc := service.NewReferenceCache(config.ReferenceCache{StorageID: 13}, referenceStores{bucket: bucket}, rows)
	stats, err := svc.Maintain(t.Context())
	if err != nil || stats.Removed != 1 || len(rows.tracked) != 1 || rows.tracked[0] != orphan {
		t.Fatalf("前缀对账应只采用已过宽限期的缓存对象：stats=%+v err=%v", stats, err)
	}
	if _, exists := bucket.objects[orphan]; exists || len(bucket.objects) != 2 {
		t.Fatal("对账只能删除被认领的孤儿对象")
	}
}
