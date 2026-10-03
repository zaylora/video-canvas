package service_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	. "video-canvas/internal/service"

	"video-canvas/internal/config"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
	"video-canvas/internal/storage"
)

// ---- fakes ----

// fakeDirectStore 在 fakeAssetStore 之上加上直传、Stat、Range 能力，模拟支持浏览器直传的对象存储。
type fakeDirectStore struct {
	*fakeAssetStore
	method   string
	req      storage.DirectUploadRequest
	signs    int
	signErr  error
	statErr  error
	rangeLen []int64
}

func newFakeDirectStore(method string) *fakeDirectStore {
	return &fakeDirectStore{fakeAssetStore: newFakeAssetStore(), method: method}
}

func (f *fakeDirectStore) DirectMethod() string { return f.method }

func (f *fakeDirectStore) DirectUpload(ctx context.Context, req storage.DirectUploadRequest) (*storage.DirectUpload, error) {
	f.signs++
	f.req = req
	if f.signErr != nil {
		return nil, f.signErr
	}
	if f.method == storage.DirectPresignedPut {
		return &storage.DirectUpload{Method: "put", URL: "https://bucket.test/" + req.Key + "?sig=1", Headers: map[string]string{"Content-Type": req.ContentType}}, nil
	}
	return &storage.DirectUpload{Method: "post", URL: "https://bucket.test/", Fields: map[string]string{"key": req.Key, "policy": "p"}}, nil
}

func (f *fakeDirectStore) Stat(ctx context.Context, key string) (storage.ObjectInfo, error) {
	if f.statErr != nil {
		return storage.ObjectInfo{}, f.statErr
	}
	b, ok := f.objects[key]
	if !ok {
		return storage.ObjectInfo{}, storage.ErrNotFound
	}
	return storage.ObjectInfo{Size: int64(len(b)), ContentType: f.types[key]}, nil
}

func (f *fakeDirectStore) OpenRange(ctx context.Context, key string, offset, length int64) (io.ReadCloser, error) {
	b, ok := f.objects[key]
	if !ok {
		return nil, storage.ErrNotFound
	}
	f.rangeLen = append(f.rangeLen, length)
	end := min(offset+length, int64(len(b)))
	return io.NopCloser(bytes.NewReader(b[offset:end])), nil
}

// fakeIntentRepo 实现 UploadIntentRepo。
type fakeIntentRepo struct {
	rows      map[uint64]*model.AssetUploadIntent
	nextID    uint64
	createErr error
	deleted   []uint64
}

func newFakeIntentRepo() *fakeIntentRepo {
	return &fakeIntentRepo{rows: map[uint64]*model.AssetUploadIntent{}, nextID: 500}
}

func (r *fakeIntentRepo) Create(ctx context.Context, in *model.AssetUploadIntent) error {
	if r.createErr != nil {
		return r.createErr
	}
	r.nextID++
	in.ID = r.nextID
	cp := *in
	r.rows[in.ID] = &cp
	return nil
}

func (r *fakeIntentRepo) GetByID(ctx context.Context, userID, id uint64) (*model.AssetUploadIntent, error) {
	in, ok := r.rows[id]
	if !ok || in.UserID != userID {
		return nil, repository.ErrNotFound
	}
	cp := *in
	return &cp, nil
}

func (r *fakeIntentRepo) Complete(ctx context.Context, id uint64, at time.Time) error {
	in, ok := r.rows[id]
	if !ok || in.CompletedAt != nil {
		return repository.ErrNotFound
	}
	in.CompletedAt = &at
	return nil
}

func (r *fakeIntentRepo) ListExpired(ctx context.Context, now time.Time, limit int) ([]model.AssetUploadIntent, error) {
	var out []model.AssetUploadIntent
	for id := uint64(0); id <= r.nextID; id++ {
		if in, ok := r.rows[id]; ok && in.CompletedAt == nil && !in.ExpiresAt.After(now) {
			out = append(out, *in)
		}
	}
	return out, nil
}

func (r *fakeIntentRepo) Delete(ctx context.Context, id uint64) error {
	delete(r.rows, id)
	r.deleted = append(r.deleted, id)
	return nil
}

type directEnv struct {
	svc    *AssetService
	repo   *fakeAssetRepo
	intent *fakeIntentRepo
	reg    *fakeRegistry
	store  *fakeDirectStore // 默认存储（id=2）
	clock  *time.Time
}

func newDirectEnv(t *testing.T, method string) *directEnv {
	t.Helper()
	assetIsolateTempDir(t)
	e := &directEnv{repo: newFakeAssetRepo(), intent: newFakeIntentRepo(), store: newFakeDirectStore(method)}
	h := objectHandle(2, e.store, time.Hour)
	h.DirectUpload = true
	e.reg = newFakeRegistry(2, h)
	e.svc = NewAssetService(e.repo, e.reg, config.Storage{MaxUpload: 1 << 20})
	e.svc.SetUploadIntents(e.intent)
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	e.clock = &now
	e.svc.SetNow(func() time.Time { return *e.clock })
	return e
}

func pngIntent(size int) UploadIntentInput {
	return UploadIntentInput{FileName: "海报.png", Size: int64(size), MimeType: "image/png"}
}

// ---- CreateUploadIntent ----

func TestAssetService_CreateUploadIntent(t *testing.T) {
	ctx := context.Background()

	t.Run("直传：记录意图并签发凭证，写到申请时的默认存储", func(t *testing.T) {
		e := newDirectEnv(t, storage.DirectPostPolicy)
		v, err := e.svc.CreateUploadIntent(ctx, 7, pngIntent(2048))
		if err != nil {
			t.Fatal(err)
		}
		if v.Mode != "direct" || v.Method != "post" || v.IntentID == 0 || v.Fields["key"] == "" || !v.ExpiresAt.Equal(e.clock.Add(15*time.Minute)) {
			t.Fatalf("实际 %+v", v)
		}
		in := e.intent.rows[v.IntentID]
		if in == nil || in.UserID != 7 || in.StorageID != 2 || in.DeclaredSize != 2048 || in.Kind != "image" || in.FileName != "海报.png" {
			t.Fatalf("意图记录不对：%+v", in)
		}
		if !strings.HasPrefix(in.StorageKey, "u7/202610/") || !strings.HasSuffix(in.StorageKey, ".png") || v.Fields["key"] != in.StorageKey {
			t.Errorf("key 应与意图一致且格式为 u{用户}/{年月}/{uuid}.png：%q %q", in.StorageKey, v.Fields["key"])
		}
		r := e.store.req
		if r.Key != in.StorageKey || r.ContentType != "image/png" || r.Size != 2048 || r.MaxSize != 1<<20 || r.TTL != 15*time.Minute {
			t.Errorf("签发请求不对：%+v", r)
		}
	})

	t.Run("预签名 PUT 方式", func(t *testing.T) {
		e := newDirectEnv(t, storage.DirectPresignedPut)
		v, err := e.svc.CreateUploadIntent(ctx, 7, pngIntent(2048))
		if err != nil || v.Method != "put" || v.Headers["Content-Type"] != "image/png" {
			t.Fatalf("实际 %+v err=%v", v, err)
		}
	})

	t.Run("存储没开直传 / 不支持直传：回退到后端中转，不记录意图", func(t *testing.T) {
		e := newDirectEnv(t, storage.DirectPostPolicy)
		e.reg.handles[2].DirectUpload = false
		v, err := e.svc.CreateUploadIntent(ctx, 7, pngIntent(2048))
		if err != nil || v.Mode != "proxy" || len(e.intent.rows) != 0 || e.store.signs != 0 {
			t.Fatalf("没开直传应回退中转：%+v err=%v", v, err)
		}
		// 本地存储（没有直传能力）
		e2 := newDirectEnv(t, storage.DirectPostPolicy)
		e2.reg.handles[2] = &storage.Handle{ID: 2, Provider: storage.ProviderLocal, Storage: newFakeAssetStore(), DirectUpload: true}
		if v, err := e2.svc.CreateUploadIntent(ctx, 7, pngIntent(2048)); err != nil || v.Mode != "proxy" {
			t.Fatalf("不支持直传的存储应回退中转：%+v err=%v", v, err)
		}
	})

	t.Run("入参校验", func(t *testing.T) {
		tests := []struct {
			name string
			in   UploadIntentInput
			want int
		}{
			{"没有声明大小", UploadIntentInput{FileName: "a.png", Size: 0, MimeType: "image/png"}, errcode.ErrAssetInvalid.Code},
			{"超过大小上限", UploadIntentInput{FileName: "a.png", Size: 2 << 20, MimeType: "image/png"}, errcode.ErrAssetTooLarge.Code},
			{"不支持的类型", UploadIntentInput{FileName: "a.exe", Size: 10, MimeType: "application/x-msdownload"}, errcode.ErrAssetInvalid.Code},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				e := newDirectEnv(t, storage.DirectPostPolicy)
				_, err := e.svc.CreateUploadIntent(ctx, 7, tt.in)
				if got := assetErrCode(t, err); got != tt.want {
					t.Fatalf("期望 %d，实际 %d", tt.want, got)
				}
				if len(e.intent.rows) != 0 {
					t.Error("校验失败不应记录意图")
				}
			})
		}
	})

	t.Run("签发失败要撤销已记录的意图，不留孤儿记录", func(t *testing.T) {
		e := newDirectEnv(t, storage.DirectPostPolicy)
		e.store.signErr = errors.New("sign failed")
		if _, err := e.svc.CreateUploadIntent(ctx, 7, pngIntent(10)); err == nil {
			t.Fatal("期望失败")
		}
		if len(e.intent.rows) != 0 {
			t.Errorf("意图应被撤销：%+v", e.intent.rows)
		}
	})

	t.Run("存储判定声明大小超限 -> 413", func(t *testing.T) {
		e := newDirectEnv(t, storage.DirectPresignedPut)
		e.store.signErr = storage.ErrTooLarge
		_, err := e.svc.CreateUploadIntent(ctx, 7, pngIntent(10))
		if got := assetErrCode(t, err); got != errcode.ErrAssetTooLarge.Code {
			t.Fatalf("实际 %d", got)
		}
	})
}

// ---- CompleteUpload ----

// uploadedIntent 申请一次直传，并模拟浏览器把内容直接写进了桶，返回意图 id 与对象内容。
func uploadedIntent(t *testing.T, e *directEnv, content []byte) uint64 {
	t.Helper()
	v, err := e.svc.CreateUploadIntent(context.Background(), 7, pngIntent(len(content)))
	if err != nil {
		t.Fatal(err)
	}
	key := e.intent.rows[v.IntentID].StorageKey
	e.store.objects[key], e.store.types[key] = content, "image/png"
	return v.IntentID
}

func TestAssetService_CompleteUpload(t *testing.T) {
	ctx := context.Background()
	png := assetTestPNG(t, 30, 20)

	t.Run("成功：嗅探类型、探测宽高、登记素材并返回稳定地址", func(t *testing.T) {
		e := newDirectEnv(t, storage.DirectPostPolicy)
		id := uploadedIntent(t, e, png)
		v, err := e.svc.CompleteUpload(ctx, 7, id)
		if err != nil {
			t.Fatal(err)
		}
		row := e.repo.rows[v.ID]
		if row == nil || row.StorageID != 2 || row.UserID != 7 || row.Kind != "image" || row.MimeType != "image/png" || row.Width != 30 || row.Height != 20 || row.ByteSize != int64(len(png)) || row.Source != model.AssetSourceUpload {
			t.Fatalf("入库行不对：%+v", row)
		}
		if v.URL != "/files/"+row.StorageKey || v.FileName != "海报.png" {
			t.Errorf("视图不对：%+v", v)
		}
		if e.intent.rows[id].CompletedAt == nil {
			t.Error("意图应被标记完成")
		}
		for _, n := range e.store.rangeLen {
			if n > 1<<20 {
				t.Errorf("只应读取文件头，不应整份下载：读了 %d 字节", n)
			}
		}
	})

	t.Run("申请之后管理员切换了默认存储：仍落在申请时的存储", func(t *testing.T) {
		e := newDirectEnv(t, storage.DirectPostPolicy)
		id := uploadedIntent(t, e, png)
		other := newFakeDirectStore(storage.DirectPostPolicy)
		oh := objectHandle(3, other, time.Hour)
		oh.DirectUpload = true
		e.reg.handles[3], e.reg.defaultID = oh, 3

		v, err := e.svc.CompleteUpload(ctx, 7, id)
		if err != nil {
			t.Fatal(err)
		}
		if e.repo.rows[v.ID].StorageID != 2 {
			t.Fatalf("应记录申请时的存储 2，实际 %d", e.repo.rows[v.ID].StorageID)
		}
	})

	t.Run("重复提交幂等：返回同一个素材，不重复登记", func(t *testing.T) {
		e := newDirectEnv(t, storage.DirectPostPolicy)
		id := uploadedIntent(t, e, png)
		v1, err := e.svc.CompleteUpload(ctx, 7, id)
		if err != nil {
			t.Fatal(err)
		}
		v2, err := e.svc.CompleteUpload(ctx, 7, id)
		if err != nil || v2.ID != v1.ID || len(e.repo.rows) != 1 {
			t.Fatalf("实际 %+v err=%v rows=%d", v2, err, len(e.repo.rows))
		}
	})

	t.Run("文件还没传到桶里：提示未完成，意图保留可重试", func(t *testing.T) {
		e := newDirectEnv(t, storage.DirectPostPolicy)
		v, _ := e.svc.CreateUploadIntent(ctx, 7, pngIntent(100))
		_, err := e.svc.CompleteUpload(ctx, 7, v.IntentID)
		if got := assetErrCode(t, err); got != errcode.ErrAssetInvalid.Code || !strings.Contains(err.Error(), "尚未") {
			t.Fatalf("实际：%v", err)
		}
		if _, ok := e.intent.rows[v.IntentID]; !ok {
			t.Error("意图应保留，方便稍后重试")
		}
	})

	t.Run("实际大小与申请不一致：删除对象和意图", func(t *testing.T) {
		e := newDirectEnv(t, storage.DirectPresignedPut)
		v, _ := e.svc.CreateUploadIntent(ctx, 7, pngIntent(len(png)))
		key := e.intent.rows[v.IntentID].StorageKey
		e.store.objects[key], e.store.types[key] = append(append([]byte{}, png...), make([]byte, 5000)...), "image/png"
		_, err := e.svc.CompleteUpload(ctx, 7, v.IntentID)
		if got := assetErrCode(t, err); got != errcode.ErrUploadSizeMismatch.Code {
			t.Fatalf("实际 %d", got)
		}
		if _, ok := e.store.objects[key]; ok || len(e.intent.rows) != 0 || len(e.repo.rows) != 0 {
			t.Errorf("应清理对象和意图：对象在=%v 意图=%d 素材=%d", ok, len(e.intent.rows), len(e.repo.rows))
		}
	})

	t.Run("内容不是允许的类型（伪造 Content-Type）：删除对象和意图", func(t *testing.T) {
		e := newDirectEnv(t, storage.DirectPostPolicy)
		id := uploadedIntent(t, e, []byte("MZ\x90\x00 this is an executable, not an image"))
		_, err := e.svc.CompleteUpload(ctx, 7, id)
		if got := assetErrCode(t, err); got != errcode.ErrAssetInvalid.Code {
			t.Fatalf("实际 %d", got)
		}
		if len(e.store.objects) != 0 || len(e.intent.rows) != 0 || len(e.repo.rows) != 0 {
			t.Errorf("应清理：对象=%d 意图=%d 素材=%d", len(e.store.objects), len(e.intent.rows), len(e.repo.rows))
		}
	})

	t.Run("意图已过期：删除对象，返回不存在", func(t *testing.T) {
		e := newDirectEnv(t, storage.DirectPostPolicy)
		id := uploadedIntent(t, e, png)
		*e.clock = e.clock.Add(16 * time.Minute)
		_, err := e.svc.CompleteUpload(ctx, 7, id)
		if got := assetErrCode(t, err); got != errcode.ErrUploadIntentNotFound.Code {
			t.Fatalf("实际 %d", got)
		}
		if len(e.store.objects) != 0 {
			t.Error("过期的上传应删除对象")
		}
	})

	t.Run("别人的意图 / 不存在的意图", func(t *testing.T) {
		e := newDirectEnv(t, storage.DirectPostPolicy)
		id := uploadedIntent(t, e, png)
		for _, tc := range []struct {
			user, id uint64
		}{{8, id}, {7, 9999}} {
			if _, err := e.svc.CompleteUpload(ctx, tc.user, tc.id); assetErrCode(t, err) != errcode.ErrUploadIntentNotFound.Code {
				t.Errorf("user=%d id=%d 应返回不存在：%v", tc.user, tc.id, err)
			}
		}
	})

	t.Run("入库失败：保留对象与意图，允许重试", func(t *testing.T) {
		e := newDirectEnv(t, storage.DirectPostPolicy)
		id := uploadedIntent(t, e, png)
		e.repo.createErr = errors.New("db down")
		if _, err := e.svc.CompleteUpload(ctx, 7, id); err == nil {
			t.Fatal("期望失败")
		}
		if len(e.store.objects) != 1 || e.intent.rows[id] == nil || e.intent.rows[id].CompletedAt != nil {
			t.Error("入库失败时对象与意图都应保留")
		}
		e.repo.createErr = nil
		if _, err := e.svc.CompleteUpload(ctx, 7, id); err != nil {
			t.Fatalf("重试应成功：%v", err)
		}
	})

	t.Run("存储不支持 Stat：返回直传未开启", func(t *testing.T) {
		e := newDirectEnv(t, storage.DirectPostPolicy)
		id := uploadedIntent(t, e, png)
		e.reg.handles[2].Storage = newFakeAssetStore() // 换成没有 Stat 能力的存储
		_, err := e.svc.CompleteUpload(ctx, 7, id)
		if got := assetErrCode(t, err); got != errcode.ErrUploadDirectDisabled.Code {
			t.Fatalf("实际 %d", got)
		}
	})
}

// ---- CleanupUploads ----

func TestAssetService_CleanupUploads(t *testing.T) {
	ctx := context.Background()
	e := newDirectEnv(t, storage.DirectPostPolicy)
	png := assetTestPNG(t, 8, 8)

	stale := uploadedIntent(t, e, png) // 申请后一直没人 complete，对象却已传到桶里
	staleKey := e.intent.rows[stale].StorageKey
	*e.clock = e.clock.Add(10 * time.Minute)
	fresh := uploadedIntent(t, e, png) // 还在有效期内
	done := uploadedIntent(t, e, png)
	if _, err := e.svc.CompleteUpload(ctx, 7, done); err != nil {
		t.Fatal(err)
	}
	*e.clock = e.clock.Add(10 * time.Minute) // stale 已过期（15 分钟），fresh 还没有

	n, err := e.svc.CleanupUploads(ctx, 50)
	if err != nil || n != 1 {
		t.Fatalf("应清理 1 条：n=%d err=%v", n, err)
	}
	if _, ok := e.store.objects[staleKey]; ok {
		t.Error("过期未登记的对象应被删除")
	}
	if e.intent.rows[stale] != nil {
		t.Error("过期意图应被删除")
	}
	if e.intent.rows[fresh] == nil || e.intent.rows[done] == nil {
		t.Error("未过期的、已完成的意图不能被清理")
	}
	if len(e.repo.rows) != 1 {
		t.Error("已登记的素材不能受影响")
	}
}
