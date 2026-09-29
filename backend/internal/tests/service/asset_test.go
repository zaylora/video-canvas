package service_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	. "video-canvas/internal/service"

	"video-canvas/internal/config"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
	"video-canvas/internal/storage"
)

// ---- fakes ----

// fakeAssetRepo 实现 AssetRepo，数据存在内存里，按 user_id 校验归属。
type fakeAssetRepo struct {
	rows      map[uint64]*model.Asset
	nextID    uint64
	createErr error
	getErr    error
}

func newFakeAssetRepo() *fakeAssetRepo {
	return &fakeAssetRepo{rows: map[uint64]*model.Asset{}, nextID: 100}
}

func (f *fakeAssetRepo) Create(ctx context.Context, a *model.Asset) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.nextID++
	a.ID = f.nextID
	cp := *a
	f.rows[a.ID] = &cp
	return nil
}

func (f *fakeAssetRepo) GetByID(ctx context.Context, userID, id uint64) (*model.Asset, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	a, ok := f.rows[id]
	if !ok || a.UserID != userID {
		return nil, repository.ErrNotFound
	}
	cp := *a
	return &cp, nil
}

// fakeAssetStore 实现 storage.Storage，记录写入 / 删除，可注入各种失败。
type fakeAssetStore struct {
	objects   map[string][]byte
	types     map[string]string
	deleted   []string
	putErr    error
	putPartly bool // 写入部分内容后再失败，模拟对象已部分落地
	openErr   error
	urlErr    error
	delErr    error
	closers   []*fakeAssetBody
}

func newFakeAssetStore() *fakeAssetStore {
	return &fakeAssetStore{objects: map[string][]byte{}, types: map[string]string{}}
}

func (f *fakeAssetStore) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if size >= 0 && int64(len(b)) != size {
		return errors.New("size mismatch")
	}
	if f.putErr != nil {
		if f.putPartly {
			f.objects[key] = b[:len(b)/2]
		}
		return f.putErr
	}
	f.objects[key] = b
	f.types[key] = contentType
	return nil
}

func (f *fakeAssetStore) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if f.openErr != nil {
		return nil, f.openErr
	}
	b, ok := f.objects[key]
	if !ok {
		return nil, storage.ErrNotFound
	}
	body := &fakeAssetBody{Reader: bytes.NewReader(b)}
	f.closers = append(f.closers, body)
	return body, nil
}

func (f *fakeAssetStore) URL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if f.urlErr != nil {
		return "", f.urlErr
	}
	return "https://cdn.test/" + key + "?ttl=" + ttl.String(), nil
}

func (f *fakeAssetStore) Delete(ctx context.Context, key string) error {
	f.deleted = append(f.deleted, key)
	delete(f.objects, key)
	return f.delErr
}

type fakeAssetBody struct {
	*bytes.Reader
	closed bool
}

func (b *fakeAssetBody) Close() error { b.closed = true; return nil }

// errAssetReader 读一部分后失败，模拟下载 / 上传中断。
type errAssetReader struct {
	data []byte
	err  error
}

func (r *errAssetReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, r.err
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

func newAssetTestService(cfg config.Storage) (*AssetService, *fakeAssetRepo, *fakeAssetStore) {
	repo, store := newFakeAssetRepo(), newFakeAssetStore()
	svc := NewAssetService(repo, store, cfg)
	svc.SetNow(func() time.Time { return time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC) })
	return svc, repo, store
}

func assetErrCode(t *testing.T, err error) int {
	t.Helper()
	var e *errcode.Error
	if !errors.As(err, &e) {
		t.Fatalf("期望 *errcode.Error，实际：%v", err)
	}
	return e.Code
}

// assetIsolateTempDir 把本测试的系统临时目录指到私有目录：其它包的测试（如 handler）
// 会并发地往共享的系统临时目录里写同前缀文件，统计时会互相干扰。
func assetIsolateTempDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("TMP", dir)
	t.Setenv("TEMP", dir)
	t.Setenv("TMPDIR", dir)
}

// assetSpoolFiles 统计（私有）临时目录里残留的落盘文件，用来确认临时文件被清理。
func assetSpoolFiles(t *testing.T) int {
	t.Helper()
	m, err := filepath.Glob(filepath.Join(os.TempDir(), "vc-asset-*"))
	if err != nil {
		t.Fatal(err)
	}
	return len(m)
}

// ---- Upload ----

func TestAssetService_Upload(t *testing.T) {
	assetIsolateTempDir(t)
	png := assetTestPNG(t, 30, 20)
	mp4 := assetTestMP4(0, 1000, 2500, 640, 360)
	cfg := config.Storage{MaxUpload: 1 << 20, SignedTTL: 30 * time.Minute}

	tests := []struct {
		name     string
		fileName string
		size     int64
		body     io.Reader
		mutate   func(*fakeAssetRepo, *fakeAssetStore)
		wantCode int // 0 表示成功；-1 表示期望非业务错误（内部错误）
		check    func(*testing.T, *model.AssetView, *fakeAssetRepo, *fakeAssetStore)
	}{
		{
			name: "上传 png 成功并探测宽高", fileName: "海报.png", size: -1, body: bytes.NewReader(png),
			check: func(t *testing.T, v *model.AssetView, repo *fakeAssetRepo, store *fakeAssetStore) {
				if v.Kind != "image" || v.MimeType != "image/png" || v.Width != 30 || v.Height != 20 || v.ByteSize != int64(len(png)) {
					t.Fatalf("视图不对：%+v", v)
				}
				if v.FileName != "海报.png" || v.ID == 0 {
					t.Fatalf("文件名或 id 不对：%+v", v)
				}
				row := repo.rows[v.ID]
				if row == nil || row.UserID != 7 || row.Source != model.AssetSourceUpload || row.TaskID != nil {
					t.Fatalf("入库行不对：%+v", row)
				}
				// key 形如 u{userID}/{yyyymm}/{uuid}.{ext}
				if !strings.HasPrefix(row.StorageKey, "u7/202609/") || !strings.HasSuffix(row.StorageKey, ".png") || len(row.StorageKey) != len("u7/202609/")+36+4 {
					t.Fatalf("key 格式不对：%q", row.StorageKey)
				}
				if !bytes.Equal(store.objects[row.StorageKey], png) || store.types[row.StorageKey] != "image/png" {
					t.Fatal("存储内容或类型不对")
				}
				if want := "https://cdn.test/" + row.StorageKey + "?ttl=30m0s"; v.URL != want {
					t.Fatalf("URL 应使用 SignedTTL 生成：%q", v.URL)
				}
			},
		},
		{
			name: "上传 mp4 探测时长和宽高", fileName: "clip.mp4", size: int64(len(mp4)), body: bytes.NewReader(mp4),
			check: func(t *testing.T, v *model.AssetView, _ *fakeAssetRepo, _ *fakeAssetStore) {
				if v.Kind != "video" || v.MimeType != "video/mp4" || v.Width != 640 || v.Height != 360 || v.DurationMs != 2500 {
					t.Fatalf("视图不对：%+v", v)
				}
			},
		},
		{
			name: "客户端文件名和类型不可信：内容是 png 就是 png", fileName: "../../evil.exe", size: -1, body: bytes.NewReader(png),
			check: func(t *testing.T, v *model.AssetView, repo *fakeAssetRepo, _ *fakeAssetStore) {
				if v.Kind != "image" || v.FileName != "evil.exe" {
					t.Fatalf("视图不对：%+v", v)
				}
				if !strings.HasSuffix(repo.rows[v.ID].StorageKey, ".png") {
					t.Fatalf("扩展名应取自嗅探结果：%q", repo.rows[v.ID].StorageKey)
				}
			},
		},
		{
			name: "文件名清洗为空时用默认名", fileName: "..", size: -1, body: bytes.NewReader(png),
			check: func(t *testing.T, v *model.AssetView, _ *fakeAssetRepo, _ *fakeAssetStore) {
				if v.FileName != "unnamed.png" {
					t.Fatalf("文件名 %q", v.FileName)
				}
			},
		},
		{name: "Body 为空", fileName: "a.png", size: -1, body: nil, wantCode: errcode.ErrAssetInvalid.Code},
		{name: "空文件", fileName: "a.png", size: -1, body: bytes.NewReader(nil), wantCode: errcode.ErrAssetInvalid.Code},
		{name: "html 伪装成图片", fileName: "a.png", size: -1, body: strings.NewReader("<html><script>alert(1)</script></html>"), wantCode: errcode.ErrAssetInvalid.Code},
		{name: "svg 不在白名单", fileName: "a.svg", size: -1, body: strings.NewReader(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`), wantCode: errcode.ErrAssetInvalid.Code},
		{name: "可执行文件", fileName: "a.png", size: -1, body: bytes.NewReader(append([]byte("MZ"), make([]byte, 100)...)), wantCode: errcode.ErrAssetInvalid.Code},
		{
			name: "超过上限返回 413 且不写存储", fileName: "big.png", size: -1,
			body:     io.MultiReader(bytes.NewReader(png), bytes.NewReader(make([]byte, 1<<20))),
			wantCode: errcode.ErrAssetTooLarge.Code,
			check:    func(t *testing.T, _ *model.AssetView, _ *fakeAssetRepo, store *fakeAssetStore) {},
		},
		{name: "声明大小超限直接拒绝", fileName: "big.png", size: 2 << 20, body: bytes.NewReader(png), wantCode: errcode.ErrAssetTooLarge.Code},
		{
			name: "请求体被 MaxBytesReader 截断返回 413", fileName: "big.png", size: -1,
			body:     &errAssetReader{data: png, err: &http.MaxBytesError{Limit: 10}},
			wantCode: errcode.ErrAssetTooLarge.Code,
		},
		{
			name: "读取请求体中断返回参数错误", fileName: "a.png", size: -1,
			body:     &errAssetReader{data: png[:10], err: errors.New("connection reset")},
			wantCode: errcode.ErrAssetInvalid.Code,
		},
		{
			name: "存储写失败返回内部错误并清理对象", fileName: "a.png", size: -1, body: bytes.NewReader(png),
			mutate:   func(_ *fakeAssetRepo, s *fakeAssetStore) { s.putErr = errors.New("disk full"); s.putPartly = true },
			wantCode: -1,
		},
		{
			name: "入库失败要删除已写入的对象", fileName: "a.png", size: -1, body: bytes.NewReader(png),
			mutate:   func(r *fakeAssetRepo, _ *fakeAssetStore) { r.createErr = errors.New("db down") },
			wantCode: -1,
		},
		{
			name: "生成 URL 失败要删除已写入的对象", fileName: "a.png", size: -1, body: bytes.NewReader(png),
			mutate:   func(_ *fakeAssetRepo, s *fakeAssetStore) { s.urlErr = errors.New("sign failed") },
			wantCode: -1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, store := newAssetTestService(cfg)
			if tt.mutate != nil {
				tt.mutate(repo, store)
			}
			before := assetSpoolFiles(t)

			v, err := svc.Upload(context.Background(), 7, UploadInput{FileName: tt.fileName, Size: tt.size, Body: tt.body})

			if tt.wantCode == 0 {
				if err != nil {
					t.Fatalf("期望成功，实际：%v", err)
				}
				if tt.check != nil {
					tt.check(t, v, repo, store)
				}
			} else {
				if err == nil {
					t.Fatalf("期望失败，实际成功：%+v", v)
				}
				if tt.wantCode == -1 {
					var e *errcode.Error
					if errors.As(err, &e) {
						t.Fatalf("期望非业务错误（内部错误），实际：%v", err)
					}
				} else if got := assetErrCode(t, err); got != tt.wantCode {
					t.Fatalf("期望错误码 %d，实际 %d", tt.wantCode, got)
				}
				// 失败后不能留下素材行或存储对象
				if len(repo.rows) != 0 || len(store.objects) != 0 {
					t.Fatalf("失败后残留：rows=%d objects=%d", len(repo.rows), len(store.objects))
				}
			}
			if after := assetSpoolFiles(t); after != before {
				t.Fatalf("临时文件未清理：%d -> %d", before, after)
			}
		})
	}
}

func TestAssetService_Upload_FailureCleanup(t *testing.T) {
	png := assetTestPNG(t, 4, 4)
	cfg := config.Storage{MaxUpload: 1 << 20}

	t.Run("存储写失败时尝试删除对象", func(t *testing.T) {
		svc, _, store := newAssetTestService(cfg)
		store.putErr, store.putPartly = errors.New("disk full"), true
		if _, err := svc.Upload(context.Background(), 1, UploadInput{Body: bytes.NewReader(png), Size: -1}); err == nil {
			t.Fatal("期望失败")
		}
		if len(store.deleted) != 1 || !strings.HasPrefix(store.deleted[0], "u1/") {
			t.Fatalf("应删除已部分写入的对象：%v", store.deleted)
		}
	})
	t.Run("入库失败时删除对象", func(t *testing.T) {
		svc, repo, store := newAssetTestService(cfg)
		repo.createErr = errors.New("db down")
		if _, err := svc.Upload(context.Background(), 1, UploadInput{Body: bytes.NewReader(png), Size: -1}); err == nil {
			t.Fatal("期望失败")
		}
		if len(store.deleted) != 1 || len(store.objects) != 0 {
			t.Fatalf("应删除刚写入的对象：deleted=%v objects=%d", store.deleted, len(store.objects))
		}
	})
	t.Run("请求 ctx 已取消时清理仍然执行", func(t *testing.T) {
		svc, repo, store := newAssetTestService(cfg)
		ctx, cancel := context.WithCancel(context.Background())
		repo.createErr = errors.New("db down")
		// 入库时才取消，模拟“请求被取消导致入库失败”
		svc.SetRepo(&assetCancelRepo{AssetRepo: repo, cancel: cancel})
		if _, err := svc.Upload(ctx, 1, UploadInput{Body: bytes.NewReader(png), Size: -1}); err == nil {
			t.Fatal("期望失败")
		}
		if len(store.deleted) != 1 {
			t.Fatalf("取消后仍应清理对象：%v", store.deleted)
		}
	})
	t.Run("清理本身失败不影响返回原始错误", func(t *testing.T) {
		svc, repo, store := newAssetTestService(cfg)
		repo.createErr = errors.New("db down")
		store.delErr = errors.New("delete failed")
		_, err := svc.Upload(context.Background(), 1, UploadInput{Body: bytes.NewReader(png), Size: -1})
		if err == nil || !strings.Contains(err.Error(), "db down") {
			t.Fatalf("应返回入库错误：%v", err)
		}
	})
}

// assetCancelRepo 在 Create 时取消 ctx 再委托给内部 repo。
type assetCancelRepo struct {
	AssetRepo
	cancel context.CancelFunc
}

func (r *assetCancelRepo) Create(ctx context.Context, a *model.Asset) error {
	r.cancel()
	return r.AssetRepo.Create(ctx, a)
}

func TestAssetService_MaxUpload(t *testing.T) {
	if got := NewAssetService(nil, nil, config.Storage{}).MaxUpload(); got != DefaultMaxUpload {
		t.Fatalf("未配置时应使用默认上限，实际 %d", got)
	}
	if got := NewAssetService(nil, nil, config.Storage{MaxUpload: 123}).MaxUpload(); got != 123 {
		t.Fatalf("实际 %d", got)
	}
}

// ---- View / Get / Open ----

func TestAssetService_View(t *testing.T) {
	seed := func(repo *fakeAssetRepo) {
		repo.rows[5] = &model.Asset{ID: 5, UserID: 1, Kind: "video", StorageKey: "u1/202609/a.mp4", MimeType: "video/mp4", ByteSize: 9, Width: 16, Height: 9, DurationMs: 1234, FileName: "a.mp4"}
	}
	tests := []struct {
		name     string
		userID   uint64
		id       uint64
		mutate   func(*fakeAssetRepo, *fakeAssetStore)
		wantCode int // 0 成功，-1 非业务错误
	}{
		{"本人素材成功", 1, 5, nil, 0},
		{"他人素材返回不存在", 2, 5, nil, errcode.ErrAssetNotFound.Code},
		{"素材不存在", 1, 999, nil, errcode.ErrAssetNotFound.Code},
		{"repo 未知错误透传", 1, 5, func(r *fakeAssetRepo, _ *fakeAssetStore) { r.getErr = errors.New("db down") }, -1},
		{"生成 URL 失败", 1, 5, func(_ *fakeAssetRepo, s *fakeAssetStore) { s.urlErr = errors.New("sign failed") }, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, store := newAssetTestService(config.Storage{SignedTTL: time.Hour})
			seed(repo)
			if tt.mutate != nil {
				tt.mutate(repo, store)
			}
			v, err := svc.View(context.Background(), tt.userID, tt.id)
			switch tt.wantCode {
			case 0:
				if err != nil {
					t.Fatalf("期望成功：%v", err)
				}
				if v.ID != 5 || v.Kind != "video" || v.Width != 16 || v.DurationMs != 1234 || v.FileName != "a.mp4" || v.URL != "https://cdn.test/u1/202609/a.mp4?ttl=1h0m0s" {
					t.Fatalf("视图不对：%+v", v)
				}
			case -1:
				var e *errcode.Error
				if err == nil || errors.As(err, &e) {
					t.Fatalf("期望非业务错误，实际：%v", err)
				}
			default:
				if got := assetErrCode(t, err); got != tt.wantCode {
					t.Fatalf("期望错误码 %d，实际 %d", tt.wantCode, got)
				}
			}
		})
	}
}

func TestAssetService_GetOpen(t *testing.T) {
	setup := func() (*AssetService, *fakeAssetRepo, *fakeAssetStore) {
		svc, repo, store := newAssetTestService(config.Storage{SignedTTL: time.Hour})
		repo.rows[5] = &model.Asset{ID: 5, UserID: 1, Kind: "image", StorageKey: "u1/202609/a.png", MimeType: "image/png"}
		store.objects["u1/202609/a.png"] = []byte("PNGDATA")
		return svc, repo, store
	}

	t.Run("Get 本人素材", func(t *testing.T) {
		svc, _, _ := setup()
		a, err := svc.Get(context.Background(), 1, 5)
		if err != nil || a.ID != 5 {
			t.Fatalf("实际：%+v %v", a, err)
		}
	})
	t.Run("Get 他人素材与不存在都是 ErrAssetNotFound", func(t *testing.T) {
		svc, _, _ := setup()
		for _, tc := range []struct{ user, id uint64 }{{2, 5}, {1, 404}} {
			if _, err := svc.Get(context.Background(), tc.user, tc.id); !errors.Is(err, provider.ErrAssetNotFound) {
				t.Errorf("user=%d id=%d 期望 ErrAssetNotFound，实际：%v", tc.user, tc.id, err)
			}
		}
	})
	t.Run("Get repo 未知错误透传", func(t *testing.T) {
		svc, repo, _ := setup()
		repo.getErr = errors.New("db down")
		if _, err := svc.Get(context.Background(), 1, 5); err == nil || errors.Is(err, provider.ErrAssetNotFound) {
			t.Fatalf("应透传原始错误：%v", err)
		}
	})
	t.Run("Open 返回内容和 URL", func(t *testing.T) {
		svc, _, _ := setup()
		f, err := svc.Open(context.Background(), 1, 5)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Body.Close()
		b, _ := io.ReadAll(f.Body)
		if string(b) != "PNGDATA" || f.Asset.ID != 5 || f.URL != "https://cdn.test/u1/202609/a.png?ttl=1h0m0s" {
			t.Fatalf("实际：%q %+v %q", b, f.Asset, f.URL)
		}
	})
	t.Run("Open 他人素材不打开存储对象", func(t *testing.T) {
		svc, _, store := setup()
		if _, err := svc.Open(context.Background(), 2, 5); !errors.Is(err, provider.ErrAssetNotFound) {
			t.Fatalf("期望 ErrAssetNotFound，实际：%v", err)
		}
		if len(store.closers) != 0 {
			t.Fatal("不应打开存储对象")
		}
	})
	t.Run("Open 存储对象丢失按不存在处理", func(t *testing.T) {
		svc, _, store := setup()
		delete(store.objects, "u1/202609/a.png")
		if _, err := svc.Open(context.Background(), 1, 5); !errors.Is(err, provider.ErrAssetNotFound) {
			t.Fatalf("期望 ErrAssetNotFound，实际：%v", err)
		}
	})
	t.Run("Open 存储读取失败透传", func(t *testing.T) {
		svc, _, store := setup()
		store.openErr = errors.New("io error")
		if _, err := svc.Open(context.Background(), 1, 5); err == nil || errors.Is(err, provider.ErrAssetNotFound) {
			t.Fatalf("应透传存储错误：%v", err)
		}
	})
	t.Run("Open 生成 URL 失败要关闭 Body", func(t *testing.T) {
		svc, _, store := setup()
		store.urlErr = errors.New("sign failed")
		if _, err := svc.Open(context.Background(), 1, 5); err == nil {
			t.Fatal("期望失败")
		}
		if len(store.closers) != 1 || !store.closers[0].closed {
			t.Fatal("Body 未被关闭")
		}
	})
}

// ---- SaveGenerated ----

func TestAssetService_SaveGenerated(t *testing.T) {
	assetIsolateTempDir(t)
	png := assetTestPNG(t, 8, 6)
	mp4 := assetTestMP4(0, 1000, 5000, 1280, 720)
	base := func() provider.SaveGeneratedInput {
		return provider.SaveGeneratedInput{UserID: 3, TaskID: 42, Kind: "video", MimeType: "video/mp4", FileName: "out.mp4", Body: bytes.NewReader(mp4)}
	}
	cfg := config.Storage{MaxResult: 1 << 20, SignedTTL: time.Hour}

	tests := []struct {
		name     string
		mutateIn func(*provider.SaveGeneratedInput)
		mutate   func(*fakeAssetRepo, *fakeAssetStore)
		cfg      *config.Storage
		wantCode int    // 期望的业务错误码，0 表示不检查
		wantErr  string // 期望的错误文案子串（非业务错误用），空且 wantCode 为 0 表示成功
	}{
		{name: "视频转存成功"},
		{
			name: "图片产物", mutateIn: func(in *provider.SaveGeneratedInput) {
				in.Kind = "image"
				in.MimeType = "image/png"
				in.Body = bytes.NewReader(png)
			},
		},
		{
			name: "Kind 留空按内容决定", mutateIn: func(in *provider.SaveGeneratedInput) { in.Kind = "" },
		},
		{
			name: "mp4 容器允许按 audio 保存", mutateIn: func(in *provider.SaveGeneratedInput) { in.Kind = "audio" },
		},
		{
			name: "嗅探不出时用平台声明的白名单类型兜底", mutateIn: func(in *provider.SaveGeneratedInput) {
				in.Body = strings.NewReader("unknown-container-bytes-here-0123456789")
			},
		},
		{
			name: "嗅探不出且声明的类型不在白名单", mutateIn: func(in *provider.SaveGeneratedInput) {
				in.Body = strings.NewReader("unknown-container-bytes-here-0123456789")
				in.MimeType = "application/x-msdownload"
			},
			wantCode: errcode.ErrAssetInvalid.Code,
		},
		{
			name: "期望视频却拿到图片", mutateIn: func(in *provider.SaveGeneratedInput) { in.Body = bytes.NewReader(png) }, wantCode: errcode.ErrAssetInvalid.Code,
		},
		{
			name: "期望图片却拿到视频", mutateIn: func(in *provider.SaveGeneratedInput) { in.Kind = "image" }, wantCode: errcode.ErrAssetInvalid.Code,
		},
		{name: "不支持的 Kind", mutateIn: func(in *provider.SaveGeneratedInput) { in.Kind = "3d" }, wantErr: "不支持的产物种类"},
		{name: "Body 为空", mutateIn: func(in *provider.SaveGeneratedInput) { in.Body = nil }, wantErr: "缺少内容"},
		{name: "空内容", mutateIn: func(in *provider.SaveGeneratedInput) { in.Body = bytes.NewReader(nil) }, wantCode: errcode.ErrAssetInvalid.Code},
		{
			name: "超过调用方给的 MaxBytes", mutateIn: func(in *provider.SaveGeneratedInput) { in.MaxBytes = 100 }, wantCode: errcode.ErrAssetTooLarge.Code,
		},
		{
			name: "MaxBytes 为 0 时使用配置的 MaxResult", cfg: &config.Storage{MaxResult: 100}, wantCode: errcode.ErrAssetTooLarge.Code,
		},
		{
			name: "MaxBytes 恰好等于大小可通过", mutateIn: func(in *provider.SaveGeneratedInput) { in.MaxBytes = int64(len(mp4)) },
		},
		{
			name: "下载中断原样透传底层错误供 worker 重试", mutateIn: func(in *provider.SaveGeneratedInput) {
				in.Body = &errAssetReader{data: mp4[:20], err: errors.New("connection reset")}
			},
			wantErr: "connection reset",
		},
		{
			name: "存储写失败", mutate: func(_ *fakeAssetRepo, s *fakeAssetStore) { s.putErr = errors.New("disk full"); s.putPartly = true },
			wantErr: "disk full",
		},
		{
			name: "入库失败", mutate: func(r *fakeAssetRepo, _ *fakeAssetStore) { r.createErr = errors.New("db down") },
			wantErr: "db down",
		},
		{
			name: "生成 URL 失败", mutate: func(_ *fakeAssetRepo, s *fakeAssetStore) { s.urlErr = errors.New("sign failed") },
			wantErr: "sign failed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := cfg
			if tt.cfg != nil {
				c = *tt.cfg
			}
			svc, repo, store := newAssetTestService(c)
			if tt.mutate != nil {
				tt.mutate(repo, store)
			}
			in := base()
			if tt.mutateIn != nil {
				tt.mutateIn(&in)
			}
			before := assetSpoolFiles(t)

			a, url, err := svc.SaveGenerated(context.Background(), in)

			if tt.wantErr == "" && tt.wantCode == 0 {
				if err != nil {
					t.Fatalf("期望成功：%v", err)
				}
				if a.Source != model.AssetSourceGenerated || a.TaskID == nil || *a.TaskID != 42 || a.UserID != 3 {
					t.Fatalf("入库行不对：%+v", a)
				}
				if !strings.HasPrefix(a.StorageKey, "g3/202609/") || url != "https://cdn.test/"+a.StorageKey+"?ttl=1h0m0s" {
					t.Fatalf("key 或 URL 不对：%q %q", a.StorageKey, url)
				}
				if repo.rows[a.ID] == nil || len(store.objects) != 1 {
					t.Fatal("应恰好写入一个对象和一行记录")
				}
			} else {
				if err == nil {
					t.Fatalf("期望失败，实际成功：%+v", a)
				}
				if tt.wantCode != 0 {
					if got := assetErrCode(t, err); got != tt.wantCode {
						t.Fatalf("期望错误码 %d，实际 %d", tt.wantCode, got)
					}
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("期望错误包含 %q，实际：%v", tt.wantErr, err)
				}
				if len(repo.rows) != 0 || len(store.objects) != 0 {
					t.Fatalf("失败后残留：rows=%d objects=%d", len(repo.rows), len(store.objects))
				}
			}
			if after := assetSpoolFiles(t); after != before {
				t.Fatalf("临时文件未清理：%d -> %d", before, after)
			}
		})
	}
}

func TestAssetService_SaveGenerated_KindStored(t *testing.T) {
	mp4 := assetTestMP4(0, 1000, 5000, 1280, 720)
	svc, repo, _ := newAssetTestService(config.Storage{})
	a, _, err := svc.SaveGenerated(context.Background(), provider.SaveGeneratedInput{UserID: 1, TaskID: 1, Kind: "audio", Body: bytes.NewReader(mp4)})
	if err != nil {
		t.Fatal(err)
	}
	if a.Kind != "audio" || a.MimeType != "video/mp4" || a.Width != 1280 || a.DurationMs != 5000 || repo.rows[a.ID].Kind != "audio" {
		t.Fatalf("以期望种类为准入库：%+v", a)
	}
	if a.FileName != "unnamed.mp4" {
		t.Fatalf("文件名为空时使用默认名：%q", a.FileName)
	}
}
