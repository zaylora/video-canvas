package service_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"video-canvas/internal/config"
	"video-canvas/internal/model"
	"video-canvas/internal/provider"
	"video-canvas/internal/service"
	"video-canvas/internal/storage"
)

type hybridRows struct {
	service.StorageHybridRepo
	rows  []model.StorageConfig
	state model.StorageHybridPolicy
}

func (r *hybridRows) List(context.Context) ([]model.StorageConfig, error) { return r.rows, nil }
func (r *hybridRows) Hybrid(context.Context) (model.StorageHybridPolicy, error) {
	return r.state, nil
}

func (r *hybridRows) BeginHybrid(_ context.Context, limit int64) (model.StorageHybridPolicy, error) {
	r.state = model.StorageHybridPolicy{State: model.StorageHybridPreparing, LimitBytes: limit}
	return r.state, nil
}

func (r *hybridRows) DisableHybrid(context.Context) (model.StorageHybridPolicy, error) {
	r.state = model.StorageHybridPolicy{State: model.StorageHybridOff}
	return r.state, nil
}

func (r *hybridRows) ClaimHybrid(context.Context, string, time.Time) (*model.StorageHybridPolicy, error) {
	if r.state.State != model.StorageHybridPreparing {
		return nil, nil
	}
	return &r.state, nil
}

func (r *hybridRows) PinHybrid(_ context.Context, _ string, cloudID, localID uint64, _ int) error {
	r.state.CloudID, r.state.LocalID = cloudID, localID
	return nil
}

func (r *hybridRows) FinishHybrid(_ context.Context, _, reason string) error {
	r.state.State, r.state.Error = model.StorageHybridActive, reason
	if reason != "" {
		r.state.State = model.StorageHybridFailed
	}
	return nil
}

type hybridStores struct {
	stores map[uint64]*storage.Handle
	visits []uint64
}

func (s *hybridStores) Default(context.Context) (*storage.Handle, error) {
	return &storage.Handle{ID: 4}, nil
}

func (s *hybridStores) Get(_ context.Context, id uint64) (*storage.Handle, error) {
	s.visits = append(s.visits, id)
	if h := s.stores[id]; h != nil {
		return h, nil
	}
	return nil, storage.ErrStorageNotFound
}

type hybridCloud struct {
	referenceBucket
	content        map[string]string
	occupiedProbe  string
	forceCollision bool
}

func (b *hybridCloud) PutReference(ctx context.Context, key string, body io.Reader, size int64, mime, digest string) error {
	if b.forceCollision {
		b.forceCollision = false
		b.occupiedProbe = key
		b.objects[key] = storage.ReferenceObjectInfo{Size: 7, SHA256: "existing"}
	}
	return b.referenceBucket.PutReference(ctx, key, body, size, mime, digest)
}

func (b *hybridCloud) ListReferences(_ context.Context, _ string, visit func(string, int64, time.Time) error) error {
	for key, data := range b.content {
		if err := visit(key, int64(len(data)), time.Now()); err != nil {
			return err
		}
	}
	return nil
}

func (b *hybridCloud) Open(_ context.Context, key string) (io.ReadCloser, error) {
	data, ok := b.content[key]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return io.NopCloser(strings.NewReader(data)), nil
}

func hybridServiceFixture(t *testing.T) (*service.StorageHybridService, *hybridRows, *hybridStores, *storage.LocalStorage, *hybridCloud) {
	t.Helper()
	local, err := storage.NewLocal(config.LocalStorage{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	rows := &hybridRows{rows: []model.StorageConfig{
		{ID: 1, Provider: storage.ProviderLocal, Builtin: true},
		{ID: 2, Name: "可用", Provider: storage.ProviderR2, Version: 1},
		{ID: 3, Name: "备选", Provider: storage.ProviderS3, Version: 1},
		{ID: 4, Name: "默认云端", Provider: storage.ProviderR2, Version: 1, IsDefault: true},
	}}
	cloud := &hybridCloud{
		referenceBucket: referenceBucket{objects: make(map[string]storage.ReferenceObjectInfo)},
		content:         map[string]string{"u1/missing.png": "cloud-file", "u1/exists.png": "cloud", "ref-cache/temp.png": "temp"},
	}
	stores := &hybridStores{stores: map[uint64]*storage.Handle{
		1: {ID: 1, Provider: storage.ProviderLocal, Storage: local},
		2: {ID: 2, Version: 1, Provider: storage.ProviderR2, Storage: cloud, Spec: storage.Spec{UseSSL: true}},
	}}
	return service.NewStorageHybridService(rows, stores, config.ReferenceCache{}), rows, stores, local, cloud
}

func TestStorageHybridSelectsFallbackAndKeepsBothCopies(t *testing.T) {
	svc, rows, stores, local, cloud := hybridServiceFixture(t)
	ctx := t.Context()
	if err := local.Put(ctx, "u1/exists.png", bytes.NewBufferString("kept!"), 5, "image/png"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Enable(ctx, 0); err != nil {
		t.Fatal(err)
	}
	assertHybridDefault(t, svc, 4)
	if err := svc.RunPending(ctx); err != nil {
		t.Fatal(err)
	}
	if rows.state.State != model.StorageHybridActive || rows.state.CloudID != 2 || len(stores.visits) < 3 || stores.visits[1] != 4 {
		t.Fatalf("应优先检查默认云端，失败后选排序靠前的可用项: %+v visits=%v", rows.state, stores.visits)
	}
	assertHybridDefault(t, svc, 1)
	assertHybridAssetRoute(t, svc, "u1/missing.png", "cloud-file", 1)
	assertHybridAssetRoute(t, svc, "u1/exists.png", "cloud", 2)
	assertHybridCopy(t, local, "u1/missing.png", "cloud-file")
	assertHybridCopy(t, local, "u1/exists.png", "kept!")
	if _, err := local.MirrorSize("ref-cache/temp.png"); !errors.Is(err, storage.ErrNotFound) || len(cloud.content) != 3 {
		t.Fatal("只同步长期素材，云端已有对象应保留")
	}
	if _, err := svc.Disable(ctx); err != nil {
		t.Fatal(err)
	}
	assertHybridDefault(t, svc, 4)
	assertHybridCopy(t, local, "u1/missing.png", "cloud-file")
}

func assertHybridAssetRoute(t *testing.T, svc *service.StorageHybridService, key, source string, id uint64) {
	t.Helper()
	asset := &model.Asset{
		StorageID: 2, StorageKey: key, ByteSize: int64(len(source)),
		Sha256: fmt.Sprintf("%x", sha256.Sum256([]byte(source))),
	}
	h, err := svc.ForAsset(t.Context(), asset)
	if err != nil || h.ID != id {
		t.Fatalf("素材 %s 应从存储 %d 读取: %+v, %v", key, id, h, err)
	}
}

func assertHybridDefault(t *testing.T, svc *service.StorageHybridService, id uint64) {
	t.Helper()
	st, err := svc.Default(t.Context())
	if err != nil || st.ID != id {
		t.Fatalf("有效写入位置应为 %d：%+v, %v", id, st, err)
	}
}

func assertHybridCopy(t *testing.T, local *storage.LocalStorage, key, want string) {
	t.Helper()
	body, err := local.Open(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	got, err := io.ReadAll(body)
	if err != nil || string(got) != want {
		t.Fatalf("本地对象 %s 内容错误：%q, %v", key, got, err)
	}
}

func TestStorageHybridAllCloudsUnavailableLeavesOriginalDefault(t *testing.T) {
	svc, rows, stores, local, _ := hybridServiceFixture(t)
	delete(stores.stores, 2)
	if _, err := svc.Enable(t.Context(), 100); err != nil {
		t.Fatal(err)
	}
	if err := svc.RunPending(t.Context()); err != nil {
		t.Fatal(err)
	}
	if rows.state.State != model.StorageHybridFailed || !strings.Contains(rows.state.Error, "默认云端") {
		t.Fatalf("应保留检查失败的说明：%+v", rows.state)
	}
	assertHybridDefault(t, svc, 4)
	if _, err := local.MirrorSize("u1/missing.png"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatal("全部不可用时应停止同步")
	}
}

func TestStorageHybridDoesNotDeleteOccupiedProbeKey(t *testing.T) {
	svc, rows, _, _, cloud := hybridServiceFixture(t)
	cloud.forceCollision = true
	if _, err := svc.Enable(t.Context(), 0); err != nil {
		t.Fatal(err)
	}
	if err := svc.RunPending(t.Context()); err != nil {
		t.Fatal(err)
	}
	if rows.state.State != model.StorageHybridFailed || cloud.occupiedProbe == "" {
		t.Fatalf("条件写入冲突应阻止开启：%+v", rows.state)
	}
	info, err := cloud.StatReference(t.Context(), cloud.occupiedProbe)
	if err != nil || info.SHA256 != "existing" {
		t.Fatalf("启用检查应保留占用探针 key 的云端对象：%+v, %v", info, err)
	}
}

func TestStorageHybridDisablesTemporaryLimitAtZero(t *testing.T) {
	_, policy, stores, _, _ := hybridServiceFixture(t)
	policy.state = model.StorageHybridPolicy{State: model.StorageHybridActive, LocalID: 1, CloudID: 2}
	refs := &referenceRows{rows: make(map[string]model.ReferenceCacheObject)}
	runtime := service.NewReferenceCacheRuntime(config.ReferenceCache{MaxBytes: 1}, stores, refs, policy)
	ctx := t.Context()
	task := provider.TaskRef{ID: 17, UserID: 4}
	asset := &model.Asset{ID: 8, UserID: 4, StorageID: 1, StorageKey: "u4/file.png", ByteSize: 4, MimeType: "image/png"}
	file := func() *provider.AssetFile {
		return &provider.AssetFile{Asset: asset, Body: io.NopCloser(strings.NewReader("test"))}
	}
	if _, err := runtime.PrepareURL(ctx, task, file()); err != nil {
		t.Fatalf("混合限额为零应该关闭原的一字节限额: %v", err)
	}
	policy.state.LimitBytes = 1
	if _, err := runtime.PrepareURL(ctx, task, file()); provider.CodeOf(err) != provider.CodeReferenceCacheCapacity {
		t.Fatalf("正数限额应限制临时外链空间: %v", err)
	}
	policy.state.State = model.StorageHybridOff
	if _, err := runtime.PrepareURL(ctx, task, file()); !errors.Is(err, provider.ErrReferenceCacheDisabled) {
		t.Fatalf("关闭后应恢复未启用缓存时的行为: %v", err)
	}
}
