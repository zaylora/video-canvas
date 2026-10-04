package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	. "video-canvas/internal/service"

	"video-canvas/internal/imageproc"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
	"video-canvas/internal/storage"
)

// ---- fakes ----

// procRepo 实现 ImageProcessorRepo，行为与 repository 的约定一致（乐观锁、发布时停用同存储旧服务、发布历史）。
type procRepo struct {
	rows     map[uint64]*model.ImageProcessor
	versions map[uint64][]model.ImageProcessorVersion
	assets   map[string]*model.Asset // "storageID/kind"
	nextID   uint64
}

func newProcRepo() *procRepo {
	return &procRepo{rows: map[uint64]*model.ImageProcessor{}, versions: map[uint64][]model.ImageProcessorVersion{}, assets: map[string]*model.Asset{}, nextID: 1}
}

func (r *procRepo) Create(_ context.Context, p *model.ImageProcessor) error {
	for _, x := range r.rows {
		if x.Name == p.Name {
			return repository.ErrDuplicate
		}
	}
	p.ID = r.nextID
	r.nextID++
	cp := *p
	r.rows[p.ID] = &cp
	return nil
}

func (r *procRepo) GetByID(_ context.Context, id uint64) (*model.ImageProcessor, error) {
	p, ok := r.rows[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	cp := *p
	return &cp, nil
}

func (r *procRepo) List(context.Context) ([]model.ImageProcessor, error) {
	var out []model.ImageProcessor
	for id := uint64(1); id < r.nextID; id++ {
		if p, ok := r.rows[id]; ok {
			out = append(out, *p)
		}
	}
	return out, nil
}

func (r *procRepo) GetPublishedByStorage(_ context.Context, storageID uint64) (*model.ImageProcessor, error) {
	for _, p := range r.rows {
		if p.StorageID == storageID && p.Status == model.ProcessorPublished {
			cp := *p
			return &cp, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (r *procRepo) Update(_ context.Context, id uint64, version int, fields map[string]any) error {
	p, ok := r.rows[id]
	if !ok {
		return repository.ErrNotFound
	}
	if p.Version != version {
		return repository.ErrRevisionConflict
	}
	if n, ok := fields["name"].(string); ok {
		for _, x := range r.rows {
			if x.ID != id && x.Name == n {
				return repository.ErrDuplicate
			}
		}
		p.Name = n
	}
	if c, ok := fields["config"].(model.JSONText); ok {
		p.Config = c
	}
	p.Version++
	return nil
}

func (r *procRepo) SetCheck(_ context.Context, id uint64, result model.JSONText) error {
	p, ok := r.rows[id]
	if !ok {
		return repository.ErrNotFound
	}
	p.CheckResult = result
	return nil
}

func (r *procRepo) Publish(_ context.Context, in repository.PublishParams) (int, error) {
	p, ok := r.rows[in.ID]
	if !ok {
		return 0, repository.ErrNotFound
	}
	if p.Version != in.Version {
		return 0, repository.ErrRevisionConflict
	}
	for _, x := range r.rows {
		if x.ID != p.ID && x.StorageID == p.StorageID && x.Status == model.ProcessorPublished {
			x.Status = model.ProcessorDisabled
		}
	}
	n := 1
	if vs := r.versions[p.ID]; len(vs) > 0 {
		n = vs[len(vs)-1].Version + 1
	}
	r.versions[p.ID] = append(r.versions[p.ID], model.ImageProcessorVersion{ProcessorID: p.ID, Version: n, Config: p.Config})
	p.Status, p.PublishedConfig, p.PublishedVersion = model.ProcessorPublished, p.Config, n
	return n, nil
}

func (r *procRepo) Rollback(_ context.Context, id uint64) (int, error) {
	p, ok := r.rows[id]
	if !ok {
		return 0, repository.ErrNotFound
	}
	for i := len(r.versions[id]) - 1; i >= 0; i-- {
		if v := r.versions[id][i]; v.Version < p.PublishedVersion {
			p.PublishedConfig, p.PublishedVersion = v.Config, v.Version
			return v.Version, nil
		}
	}
	return 0, repository.ErrNotFound
}

func (r *procRepo) Disable(_ context.Context, id uint64) error {
	p, ok := r.rows[id]
	if !ok || p.Status != model.ProcessorPublished {
		return repository.ErrNotFound
	}
	p.Status = model.ProcessorDisabled
	return nil
}

func (r *procRepo) Delete(_ context.Context, id uint64) error {
	if _, ok := r.rows[id]; !ok {
		return repository.ErrNotFound
	}
	delete(r.rows, id)
	delete(r.versions, id)
	return nil
}

func (r *procRepo) PreviousVersions(_ context.Context, ids ...uint64) (map[uint64]int, error) {
	out := map[uint64]int{}
	for id, p := range r.rows {
		if p.Status != model.ProcessorPublished {
			continue
		}
		for _, v := range r.versions[id] {
			if v.Version < p.PublishedVersion && v.Version > out[id] {
				out[id] = v.Version
			}
		}
	}
	return out, nil
}

func (r *procRepo) SampleAsset(_ context.Context, storageID uint64, kind string) (*model.Asset, error) {
	a, ok := r.assets[assetKey(storageID, kind)]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return a, nil
}

func assetKey(storageID uint64, kind string) string { return string(rune('0'+storageID)) + "/" + kind }

// procStorages 实现 ProcessorStorages。
type procStorages struct {
	rows map[uint64]*model.StorageConfig
}

func (s procStorages) GetByID(_ context.Context, id uint64) (*model.StorageConfig, error) {
	r, ok := s.rows[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	cp := *r
	return &cp, nil
}

func (s procStorages) List(context.Context) ([]model.StorageConfig, error) {
	var out []model.StorageConfig
	for id := uint64(1); id <= uint64(len(s.rows)); id++ {
		if r, ok := s.rows[id]; ok {
			out = append(out, *r)
		}
	}
	return out, nil
}

// procHandles 实现 ProcessorHandles：按存储 id 返回带 Spec 的句柄。
type procHandles struct{ specs map[uint64]storage.Spec }

func (h procHandles) Get(_ context.Context, id uint64) (*storage.Handle, error) {
	sp, ok := h.specs[id]
	if !ok {
		return nil, storage.ErrStorageNotFound
	}
	return &storage.Handle{ID: id, Provider: sp.Provider, Spec: sp, SignedTTL: time.Hour}, nil
}

// procFetcher 实现 ProcessorFetcher：按 URL 子串匹配预置响应；记录请求过的地址。
type procFetcher struct {
	mu    sync.Mutex
	rules []fetchRule
	urls  []string
}

type fetchRule struct {
	contains string
	res      FetchResult
	err      error
}

func (f *procFetcher) Fetch(_ context.Context, u string) (FetchResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.urls = append(f.urls, u)
	for _, r := range f.rules {
		if strings.Contains(u, r.contains) {
			return r.res, r.err
		}
	}
	return FetchResult{Status: 404}, nil
}

// procWorld 是一套预置好的测试环境：存储 1=本地磁盘，2=R2（公开域名），3=COS（私有读，有密钥），4=OSS（私有读），5=R2（没有公开域名）。
type procWorld struct {
	svc     *ImageProcessorService
	repo    *procRepo
	fetcher *procFetcher
	specs   map[uint64]storage.Spec
}

func newProcWorld(t *testing.T) *procWorld {
	t.Helper()
	stores := procStorages{rows: map[uint64]*model.StorageConfig{
		1: {ID: 1, Name: "本地磁盘", Provider: storage.ProviderLocal, Builtin: true},
		2: {ID: 2, Name: "R2 生产", Provider: storage.ProviderR2, PublicBaseURL: "https://assets.example.com"},
		3: {ID: 3, Name: "COS 首尔", Provider: storage.ProviderTencentCOS, Bucket: "canvas-1250000000", Region: "ap-seoul"},
		4: {ID: 4, Name: "OSS 杭州", Provider: storage.ProviderAliyunOSS, Bucket: "vc-bucket", Region: "cn-hangzhou"},
		5: {ID: 5, Name: "R2 无域名", Provider: storage.ProviderR2},
	}}
	specs := map[uint64]storage.Spec{
		2: {Provider: storage.ProviderR2, PublicBaseURL: "https://assets.example.com"},
		3: {Provider: storage.ProviderTencentCOS, Bucket: "canvas-1250000000", Region: "ap-seoul", AccessKey: "AKID", SecretKey: "sk"},
		4: {Provider: storage.ProviderAliyunOSS, Bucket: "vc-bucket", Region: "cn-hangzhou", AccessKey: "LTAI", SecretKey: "sk"},
		5: {Provider: storage.ProviderR2},
	}
	repo := newProcRepo()
	fetcher := &procFetcher{rules: []fetchRule{
		{contains: "/cdn-cgi/image/", res: FetchResult{Status: 200, Bytes: 38_000, ContentType: "image/webp"}},
		{contains: "/cdn-cgi/media/", res: FetchResult{Status: 200, Bytes: 41_000, ContentType: "image/jpeg"}},
		{contains: "https://assets.example.com/", res: FetchResult{Status: 404}},
	}}
	repo.assets[assetKey(2, "image")] = &model.Asset{ID: 1, Kind: "image", StorageID: 2, StorageKey: "u1/202610/a.png", ByteSize: 3_100_000}
	repo.assets[assetKey(2, "video")] = &model.Asset{ID: 2, Kind: "video", StorageID: 2, StorageKey: "u1/202610/a.mp4", ByteSize: 28_000_000}
	svc := NewImageProcessorService(repo, stores, procHandles{specs: specs}, fetcher)
	return &procWorld{svc: svc, repo: repo, fetcher: fetcher, specs: specs}
}

func cfCfg() imageproc.Config { return imageproc.Config{Domain: "assets.example.com"} }

func (w *procWorld) createCF(t *testing.T, name string) *ProcessorView {
	t.Helper()
	v, err := w.svc.Create(context.Background(), 1, ProcessorCreateInput{Name: name, Vendor: imageproc.VendorCloudflare, StorageID: 2, Config: cfCfg()})
	if err != nil {
		t.Fatalf("新建失败：%v", err)
	}
	return v
}

func wantCode(t *testing.T, err error, want *errcode.Error) {
	t.Helper()
	var e *errcode.Error
	if !errors.As(err, &e) || e.Code != want.Code {
		t.Fatalf("期望业务错误 %d，实际 %v", want.Code, err)
	}
}

func checkStatus(t *testing.T, c *ProcessorCheck, key string) string {
	t.Helper()
	for _, it := range c.Checks {
		if it.Key == key {
			return it.Status
		}
	}
	t.Fatalf("校验结果里没有 %q：%+v", key, c.Checks)
	return ""
}

// ---- tests ----

func TestProcessorPresets(t *testing.T) {
	w := newProcWorld(t)
	if got := w.svc.Presets(); len(got) != 3 {
		t.Fatalf("应有 3 个预设，实际 %d", len(got))
	}
}

func TestProcessorCreate(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name string
		in   ProcessorCreateInput
		want *errcode.Error
	}{
		{"未知厂商", ProcessorCreateInput{Name: "x", Vendor: "aws_dit", StorageID: 2, Config: cfCfg()}, errcode.ErrProcessorInvalid},
		{"名称为空", ProcessorCreateInput{Name: " ", Vendor: imageproc.VendorCloudflare, StorageID: 2, Config: cfCfg()}, errcode.ErrProcessorInvalid},
		{"存储不存在", ProcessorCreateInput{Name: "x", Vendor: imageproc.VendorCloudflare, StorageID: 99, Config: cfCfg()}, errcode.ErrStorageNotFound},
		{"腾讯云不能绑 R2", ProcessorCreateInput{Name: "x", Vendor: imageproc.VendorTencentCI, StorageID: 2, Config: imageproc.Config{Domain: "a.example.com"}}, errcode.ErrProcessorStorageMismatch},
		{"阿里云不能绑 COS", ProcessorCreateInput{Name: "x", Vendor: imageproc.VendorAliyunOSSImg, StorageID: 3, Config: imageproc.Config{Domain: "a.example.com"}}, errcode.ErrProcessorStorageMismatch},
		{"Cloudflare 不能绑本地磁盘", ProcessorCreateInput{Name: "x", Vendor: imageproc.VendorCloudflare, StorageID: 1, Config: cfCfg()}, errcode.ErrProcessorStorageMismatch},
		{"Cloudflare 的 R2 必须有公开域名", ProcessorCreateInput{Name: "x", Vendor: imageproc.VendorCloudflare, StorageID: 5, Config: cfCfg()}, errcode.ErrProcessorStorageMismatch},
		{"配置不合法", ProcessorCreateInput{Name: "x", Vendor: imageproc.VendorCloudflare, StorageID: 2, Config: imageproc.Config{Domain: "assets.example.com", Width: 5000}}, errcode.ErrProcessorInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := newProcWorld(t)
			_, err := w.svc.Create(ctx, 1, tc.in)
			wantCode(t, err, tc.want)
		})
	}

	t.Run("成功：草稿状态，补全默认配置，带存储信息", func(t *testing.T) {
		w := newProcWorld(t)
		v := w.createCF(t, "R2 处理")
		if v.Status != model.ProcessorDraft || v.StorageName != "R2 生产" || v.StorageProvider != storage.ProviderR2 || v.Version != 1 {
			t.Fatalf("实际 %+v", v)
		}
		if v.Config.Width != 512 || v.Config.Format != "auto" || v.PublishedConfig != nil || v.HasDraft || v.Check != nil || v.PublishedVersion != 0 || v.PreviousVersion != nil {
			t.Fatalf("默认值或状态不对：%+v", v)
		}
	})

	t.Run("名称重复", func(t *testing.T) {
		w := newProcWorld(t)
		w.createCF(t, "同名")
		_, err := w.svc.Create(ctx, 1, ProcessorCreateInput{Name: "同名", Vendor: imageproc.VendorTencentCI, StorageID: 3, Config: imageproc.Config{Domain: "a.example.com"}})
		wantCode(t, err, errcode.ErrProcessorNameDup)
	})
}

func TestProcessorUpdate(t *testing.T) {
	ctx := context.Background()
	w := newProcWorld(t)
	v := w.createCF(t, "A")

	t.Run("保存草稿：版本 +1，之前的校验结果落后", func(t *testing.T) {
		if _, err := w.svc.Check(ctx, 1, v.ID); err != nil {
			t.Fatal(err)
		}
		got, err := w.svc.Update(ctx, 1, v.ID, ProcessorUpdateInput{Version: 1, Name: "A2", Config: imageproc.Config{Domain: "assets.example.com", Width: 256}})
		if err != nil {
			t.Fatal(err)
		}
		if got.Version != 2 || got.Name != "A2" || got.Config.Width != 256 {
			t.Fatalf("实际 %+v", got)
		}
		if got.Check == nil || got.Check.Version != 1 {
			t.Fatalf("校验结果应保留且 version 仍是 1（已落后于 2）：%+v", got.Check)
		}
	})
	t.Run("版本冲突", func(t *testing.T) {
		_, err := w.svc.Update(ctx, 1, v.ID, ProcessorUpdateInput{Version: 1, Name: "A3", Config: cfCfg()})
		wantCode(t, err, errcode.ErrProcessorVersionConflict)
	})
	t.Run("不存在", func(t *testing.T) {
		_, err := w.svc.Update(ctx, 1, 99, ProcessorUpdateInput{Version: 1, Name: "x", Config: cfCfg()})
		wantCode(t, err, errcode.ErrProcessorNotFound)
	})
	t.Run("配置不合法", func(t *testing.T) {
		_, err := w.svc.Update(ctx, 1, v.ID, ProcessorUpdateInput{Version: 2, Name: "A", Config: imageproc.Config{Domain: ""}})
		wantCode(t, err, errcode.ErrProcessorInvalid)
	})
	t.Run("名称与其他处理服务重复", func(t *testing.T) {
		other, err := w.svc.Create(ctx, 1, ProcessorCreateInput{Name: "B", Vendor: imageproc.VendorTencentCI, StorageID: 3, Config: imageproc.Config{Domain: "c.example.com"}})
		if err != nil {
			t.Fatal(err)
		}
		_, err = w.svc.Update(ctx, 1, other.ID, ProcessorUpdateInput{Version: 1, Name: "A2", Config: imageproc.Config{Domain: "c.example.com"}})
		wantCode(t, err, errcode.ErrProcessorNameDup)
	})
}

func TestProcessorCheck(t *testing.T) {
	ctx := context.Background()

	t.Run("Cloudflare 全部通过", func(t *testing.T) {
		w := newProcWorld(t)
		v := w.createCF(t, "A")
		got, err := w.svc.Check(ctx, 1, v.ID)
		if err != nil {
			t.Fatal(err)
		}
		c := got.Check
		if c == nil || !c.OK || c.Version != v.Version {
			t.Fatalf("应通过且 version 对应当前草稿：%+v", c)
		}
		for _, k := range []string{"binding", "public_domain", "domain", "trial_image", "trial_video"} {
			if s := checkStatus(t, c, k); s != "ok" {
				t.Errorf("%s 应为 ok，实际 %s", k, s)
			}
		}
		if c.Trial.Image == nil || c.Trial.Image.Bytes != 38_000 || c.Trial.Image.SourceBytes != 3_100_000 || c.Trial.Video == nil || c.Trial.Video.Bytes != 41_000 {
			t.Fatalf("试跑结果不对：%+v", c.Trial)
		}
		// 校验用的是“生成变体地址”的同一套逻辑
		joined := strings.Join(w.fetcher.urls, "\n")
		if !strings.Contains(joined, "/cdn-cgi/image/width=512") || !strings.Contains(joined, "/cdn-cgi/media/mode=frame") {
			t.Errorf("试跑请求的地址不对：%s", joined)
		}
		again, _ := w.svc.Get(ctx, v.ID)
		if again.Check == nil || !again.Check.OK {
			t.Error("校验结果应已保存")
		}
	})

	t.Run("Cloudflare 域名与存储公开域名不一致", func(t *testing.T) {
		w := newProcWorld(t)
		v, err := w.svc.Create(ctx, 1, ProcessorCreateInput{Name: "A", Vendor: imageproc.VendorCloudflare, StorageID: 2, Config: imageproc.Config{Domain: "other.example.com"}})
		if err != nil {
			t.Fatal(err)
		}
		got, _ := w.svc.Check(ctx, 1, v.ID)
		if got.Check.OK || checkStatus(t, got.Check, "public_domain") != "fail" {
			t.Fatalf("应校验失败：%+v", got.Check)
		}
	})

	t.Run("域名不可访问", func(t *testing.T) {
		w := newProcWorld(t)
		w.fetcher.rules = append([]fetchRule{{contains: "https://assets.example.com/", err: errors.New("dial tcp: no such host")}}, w.fetcher.rules...)
		v := w.createCF(t, "A")
		got, _ := w.svc.Check(ctx, 1, v.ID)
		if got.Check.OK || checkStatus(t, got.Check, "domain") != "fail" {
			t.Fatalf("实际 %+v", got.Check)
		}
	})

	t.Run("试跑返回的不是图片", func(t *testing.T) {
		w := newProcWorld(t)
		w.fetcher.rules = append([]fetchRule{{contains: "/cdn-cgi/image/", res: FetchResult{Status: 200, Bytes: 10, ContentType: "text/html"}}}, w.fetcher.rules...)
		v := w.createCF(t, "A")
		got, _ := w.svc.Check(ctx, 1, v.ID)
		if got.Check.OK || checkStatus(t, got.Check, "trial_image") != "fail" {
			t.Fatalf("实际 %+v", got.Check)
		}
	})

	t.Run("试跑返回非 200", func(t *testing.T) {
		w := newProcWorld(t)
		w.fetcher.rules = append([]fetchRule{{contains: "/cdn-cgi/media/", res: FetchResult{Status: 415}}}, w.fetcher.rules...)
		v := w.createCF(t, "A")
		got, _ := w.svc.Check(ctx, 1, v.ID)
		if got.Check.OK || checkStatus(t, got.Check, "trial_video") != "fail" {
			t.Fatalf("实际 %+v", got.Check)
		}
	})

	t.Run("存储里没有视频素材：提示但不挡发布", func(t *testing.T) {
		w := newProcWorld(t)
		delete(w.repo.assets, assetKey(2, "video"))
		v := w.createCF(t, "A")
		got, _ := w.svc.Check(ctx, 1, v.ID)
		if !got.Check.OK || checkStatus(t, got.Check, "trial_video") != "warn" || got.Check.Trial.Video != nil {
			t.Fatalf("实际 %+v", got.Check)
		}
	})

	t.Run("腾讯云未开通媒体处理：提示，封面试跑跳过", func(t *testing.T) {
		w := newProcWorld(t)
		w.repo.assets[assetKey(3, "image")] = &model.Asset{Kind: "image", StorageID: 3, StorageKey: "u1/a.png", ByteSize: 1000}
		w.fetcher.rules = []fetchRule{
			{contains: "myqcloud.com/u1/a.png", res: FetchResult{Status: 200, Bytes: 500, ContentType: "image/jpeg"}},
			{contains: "myqcloud.com/", res: FetchResult{Status: 403}},
		}
		v, err := w.svc.Create(ctx, 1, ProcessorCreateInput{Name: "T", Vendor: imageproc.VendorTencentCI, StorageID: 3, Config: imageproc.Config{Domain: "canvas-1250000000.cos.ap-seoul.myqcloud.com"}})
		if err != nil {
			t.Fatal(err)
		}
		got, _ := w.svc.Check(ctx, 1, v.ID)
		c := got.Check
		if !c.OK || checkStatus(t, c, "media") != "warn" || checkStatus(t, c, "sign") != "ok" || checkStatus(t, c, "trial_image") != "ok" || checkStatus(t, c, "trial_video") != "warn" {
			t.Fatalf("实际 %+v", c)
		}
		if strings.Contains(strings.Join(w.fetcher.urls, "\n"), "ci-process=snapshot") {
			t.Error("未开通媒体处理时不应请求封面")
		}
	})

	t.Run("私有读桶缺少凭证：签名校验失败", func(t *testing.T) {
		w := newProcWorld(t)
		sp := w.specs[4]
		sp.AccessKey, sp.SecretKey = "", ""
		w.specs[4] = sp
		v, err := w.svc.Create(ctx, 1, ProcessorCreateInput{Name: "O", Vendor: imageproc.VendorAliyunOSSImg, StorageID: 4, Config: imageproc.Config{Domain: "img.example.com"}})
		if err != nil {
			t.Fatal(err)
		}
		got, _ := w.svc.Check(ctx, 1, v.ID)
		if got.Check.OK || checkStatus(t, got.Check, "sign") != "fail" {
			t.Fatalf("实际 %+v", got.Check)
		}
	})

	t.Run("阿里云使用默认域名：提示建议自定义域名", func(t *testing.T) {
		w := newProcWorld(t)
		w.fetcher.rules = []fetchRule{{contains: "aliyuncs.com", res: FetchResult{Status: 404}}}
		v, err := w.svc.Create(ctx, 1, ProcessorCreateInput{Name: "O", Vendor: imageproc.VendorAliyunOSSImg, StorageID: 4, Config: imageproc.Config{Domain: "vc-bucket.oss-cn-hangzhou.aliyuncs.com"}})
		if err != nil {
			t.Fatal(err)
		}
		got, _ := w.svc.Check(ctx, 1, v.ID)
		if checkStatus(t, got.Check, "custom_domain") != "warn" {
			t.Fatalf("实际 %+v", got.Check)
		}
	})

	t.Run("不存在", func(t *testing.T) {
		w := newProcWorld(t)
		_, err := w.svc.Check(ctx, 1, 99)
		wantCode(t, err, errcode.ErrProcessorNotFound)
	})
}

func TestProcessorPublishLifecycle(t *testing.T) {
	ctx := context.Background()

	t.Run("没有校验过不能发布", func(t *testing.T) {
		w := newProcWorld(t)
		v := w.createCF(t, "A")
		_, err := w.svc.Publish(ctx, 1, v.ID, v.Version)
		wantCode(t, err, errcode.ErrProcessorNotChecked)
	})

	t.Run("校验未通过不能发布", func(t *testing.T) {
		w := newProcWorld(t)
		v, _ := w.svc.Create(ctx, 1, ProcessorCreateInput{Name: "A", Vendor: imageproc.VendorCloudflare, StorageID: 2, Config: imageproc.Config{Domain: "other.example.com"}})
		_, _ = w.svc.Check(ctx, 1, v.ID)
		_, err := w.svc.Publish(ctx, 1, v.ID, v.Version)
		wantCode(t, err, errcode.ErrProcessorNotChecked)
	})

	t.Run("保存草稿后旧的校验结果失效", func(t *testing.T) {
		w := newProcWorld(t)
		v := w.createCF(t, "A")
		_, _ = w.svc.Check(ctx, 1, v.ID)
		u, _ := w.svc.Update(ctx, 1, v.ID, ProcessorUpdateInput{Version: 1, Name: "A", Config: imageproc.Config{Domain: "assets.example.com", Width: 300}})
		_, err := w.svc.Publish(ctx, 1, v.ID, u.Version)
		wantCode(t, err, errcode.ErrProcessorNotChecked)
	})

	t.Run("版本冲突", func(t *testing.T) {
		w := newProcWorld(t)
		v := w.createCF(t, "A")
		_, _ = w.svc.Check(ctx, 1, v.ID)
		_, err := w.svc.Publish(ctx, 1, v.ID, 99)
		wantCode(t, err, errcode.ErrProcessorVersionConflict)
	})

	t.Run("发布成功后线上立即生效，同存储旧的自动停用，可回滚、可停用", func(t *testing.T) {
		w := newProcWorld(t)
		a := w.createCF(t, "A")
		_, _ = w.svc.Check(ctx, 1, a.ID)
		pub, err := w.svc.Publish(ctx, 1, a.ID, a.Version)
		if err != nil {
			t.Fatal(err)
		}
		if pub.Status != model.ProcessorPublished || pub.PublishedVersion != 1 || pub.PublishedConfig == nil || pub.HasDraft || pub.PreviousVersion != nil {
			t.Fatalf("实际 %+v", pub)
		}
		h, _ := procHandles{specs: w.specs}.Get(ctx, 2)
		tgt, ok, err := w.svc.Resolve(ctx, 2, h, "u1/a.png", imageproc.VariantThumb)
		if err != nil || !ok || !strings.Contains(tgt.URL, "/cdn-cgi/image/width=512") || tgt.Signed {
			t.Fatalf("应解析到处理地址（公开读不带签名）：%+v %v %v", tgt, ok, err)
		}

		// 编辑已发布的：线上配置不变，出现草稿
		ed, _ := w.svc.Update(ctx, 1, a.ID, ProcessorUpdateInput{Version: pub.Version, Name: "A", Config: imageproc.Config{Domain: "assets.example.com", Width: 256}})
		if !ed.HasDraft || ed.PublishedConfig.Width != 512 || ed.Config.Width != 256 || ed.Status != model.ProcessorPublished {
			t.Fatalf("实际 %+v", ed)
		}
		tgt, _, _ = w.svc.Resolve(ctx, 2, h, "u1/a.png", imageproc.VariantThumb)
		if !strings.Contains(tgt.URL, "width=512") {
			t.Errorf("草稿未发布前线上仍应是 512：%s", tgt.URL)
		}
		// 校验并发布草稿 → v2，且可回滚到 v1
		_, _ = w.svc.Check(ctx, 1, a.ID)
		v2, err := w.svc.Publish(ctx, 1, a.ID, ed.Version)
		if err != nil || v2.PublishedVersion != 2 || v2.PreviousVersion == nil || *v2.PreviousVersion != 1 {
			t.Fatalf("实际 %+v %v", v2, err)
		}
		tgt, _, _ = w.svc.Resolve(ctx, 2, h, "u1/a.png", imageproc.VariantThumb)
		if !strings.Contains(tgt.URL, "width=256") {
			t.Errorf("发布后应立即用 256：%s", tgt.URL)
		}
		rb, err := w.svc.Rollback(ctx, 1, a.ID)
		if err != nil || rb.PublishedVersion != 1 || rb.PublishedConfig.Width != 512 {
			t.Fatalf("实际 %+v %v", rb, err)
		}
		tgt, _, _ = w.svc.Resolve(ctx, 2, h, "u1/a.png", imageproc.VariantThumb)
		if !strings.Contains(tgt.URL, "width=512") {
			t.Errorf("回滚后应立即回到 512：%s", tgt.URL)
		}
		if _, err := w.svc.Rollback(ctx, 1, a.ID); err == nil {
			t.Error("已是最早版本，不应再能回滚")
		} else {
			wantCode(t, err, errcode.ErrProcessorNoPrevious)
		}

		// 在同一存储上发布另一个 → A 自动停用
		b, err := w.svc.Create(ctx, 1, ProcessorCreateInput{Name: "B", Vendor: imageproc.VendorCloudflare, StorageID: 2, Config: imageproc.Config{Domain: "assets.example.com", Width: 128}})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.svc.Check(ctx, 1, b.ID)
		if _, err := w.svc.Publish(ctx, 1, b.ID, b.Version); err != nil {
			t.Fatal(err)
		}
		oldA, _ := w.svc.Get(ctx, a.ID)
		if oldA.Status != model.ProcessorDisabled {
			t.Fatalf("A 应被停用，实际 %s", oldA.Status)
		}
		tgt, _, _ = w.svc.Resolve(ctx, 2, h, "u1/a.png", imageproc.VariantThumb)
		if !strings.Contains(tgt.URL, "width=128") {
			t.Errorf("应切到 B：%s", tgt.URL)
		}

		// 停用 B → 该存储回退
		if _, err := w.svc.Disable(ctx, 1, b.ID); err != nil {
			t.Fatal(err)
		}
		if _, ok, _ := w.svc.Resolve(ctx, 2, h, "u1/a.png", imageproc.VariantThumb); ok {
			t.Error("停用后应没有处理服务（缓存必须已失效）")
		}
		if _, err := w.svc.Disable(ctx, 1, b.ID); err == nil {
			t.Error("重复停用应报错")
		} else {
			wantCode(t, err, errcode.ErrProcessorNotPublished)
		}
	})

	t.Run("回滚要求已发布", func(t *testing.T) {
		w := newProcWorld(t)
		v := w.createCF(t, "A")
		_, err := w.svc.Rollback(ctx, 1, v.ID)
		wantCode(t, err, errcode.ErrProcessorNotPublished)
	})
}

func TestProcessorDelete(t *testing.T) {
	ctx := context.Background()
	w := newProcWorld(t)
	a := w.createCF(t, "A")
	_, _ = w.svc.Check(ctx, 1, a.ID)
	_, _ = w.svc.Publish(ctx, 1, a.ID, a.Version)

	wantCode(t, w.svc.Delete(ctx, 1, a.ID), errcode.ErrProcessorPublished)
	if _, err := w.svc.Disable(ctx, 1, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := w.svc.Delete(ctx, 1, a.ID); err != nil {
		t.Fatalf("停用后应能删除：%v", err)
	}
	wantCode(t, w.svc.Delete(ctx, 1, a.ID), errcode.ErrProcessorNotFound)
}

func TestProcessorList(t *testing.T) {
	ctx := context.Background()
	w := newProcWorld(t)
	a := w.createCF(t, "A")
	_, _ = w.svc.Create(ctx, 1, ProcessorCreateInput{Name: "T", Vendor: imageproc.VendorTencentCI, StorageID: 3, Config: imageproc.Config{Domain: "x.example.com"}})
	_, _ = w.svc.Check(ctx, 1, a.ID)
	_, _ = w.svc.Publish(ctx, 1, a.ID, a.Version)

	list, err := w.svc.List(ctx)
	if err != nil || len(list) != 2 {
		t.Fatalf("实际 %v %+v", err, list)
	}
	if list[0].Name != "A" || list[0].Status != model.ProcessorPublished || list[1].Status != model.ProcessorDraft {
		t.Fatalf("实际 %+v", list)
	}
	// 视图能被 JSON 序列化，且不泄露密钥
	b, err := json.Marshal(list)
	if err != nil || strings.Contains(string(b), "secret") || strings.Contains(string(b), "AKID") {
		t.Fatalf("序列化异常或泄露凭证：%v %s", err, b)
	}
}

func TestProcessorResolve(t *testing.T) {
	ctx := context.Background()
	w := newProcWorld(t)
	h2, _ := procHandles{specs: w.specs}.Get(ctx, 2)

	t.Run("没有处理服务", func(t *testing.T) {
		if _, ok, err := w.svc.Resolve(ctx, 2, h2, "u1/a.png", imageproc.VariantThumb); ok || err != nil {
			t.Fatalf("应返回 false：%v %v", ok, err)
		}
	})

	t.Run("私有读桶：地址带签名并标记 Signed", func(t *testing.T) {
		v, err := w.svc.Create(ctx, 1, ProcessorCreateInput{Name: "T", Vendor: imageproc.VendorTencentCI, StorageID: 3, Config: imageproc.Config{Domain: "canvas-1250000000.cos.ap-seoul.myqcloud.com", MediaEnabled: true}})
		if err != nil {
			t.Fatal(err)
		}
		w.repo.assets[assetKey(3, "image")] = &model.Asset{Kind: "image", StorageID: 3, StorageKey: "u1/a.png"}
		w.fetcher.rules = []fetchRule{{contains: "myqcloud.com", res: FetchResult{Status: 200, Bytes: 5, ContentType: "image/jpeg"}}}
		_, _ = w.svc.Check(ctx, 1, v.ID)
		if _, err := w.svc.Publish(ctx, 1, v.ID, v.Version); err != nil {
			t.Fatal(err)
		}
		h3, _ := procHandles{specs: w.specs}.Get(ctx, 3)
		tgt, ok, err := w.svc.Resolve(ctx, 3, h3, "u1/a.png", imageproc.VariantThumb)
		if err != nil || !ok || !tgt.Signed || !strings.Contains(tgt.URL, "q-signature=") {
			t.Fatalf("实际 %+v %v %v", tgt, ok, err)
		}
		if _, ok, _ := w.svc.Resolve(ctx, 3, h3, "u1/a.mp4", imageproc.VariantPoster); !ok {
			t.Error("已开通媒体处理时应支持封面")
		}
	})

	t.Run("不支持的变体返回 false 而不是报错", func(t *testing.T) {
		w2 := newProcWorld(t)
		v, _ := w2.svc.Create(ctx, 1, ProcessorCreateInput{Name: "T", Vendor: imageproc.VendorTencentCI, StorageID: 3, Config: imageproc.Config{Domain: "canvas-1250000000.cos.ap-seoul.myqcloud.com"}})
		w2.repo.assets[assetKey(3, "image")] = &model.Asset{Kind: "image", StorageID: 3, StorageKey: "u1/a.png"}
		w2.fetcher.rules = []fetchRule{{contains: "myqcloud.com", res: FetchResult{Status: 200, Bytes: 5, ContentType: "image/jpeg"}}}
		_, _ = w2.svc.Check(ctx, 1, v.ID)
		_, _ = w2.svc.Publish(ctx, 1, v.ID, v.Version)
		h3, _ := procHandles{specs: w2.specs}.Get(ctx, 3)
		if _, ok, err := w2.svc.Resolve(ctx, 3, h3, "u1/a.mp4", imageproc.VariantPoster); ok || err != nil {
			t.Fatalf("未开通媒体处理时封面应回退：%v %v", ok, err)
		}
	})
}
