package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	. "video-canvas/internal/service"

	"video-canvas/internal/config"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/storage"
)

// fakeVariantResolver 实现 AssetVariantResolver：按 变体 返回预置结果，并记录收到的参数。
type fakeVariantResolver struct {
	targets  map[string]VariantTarget // key: 变体
	err      error
	gotStore uint64
	gotKey   string
	calls    int
}

func (f *fakeVariantResolver) Resolve(_ context.Context, storageID uint64, _ *storage.Handle, key, variant string) (VariantTarget, bool, error) {
	f.calls++
	f.gotStore, f.gotKey = storageID, key
	if f.err != nil {
		return VariantTarget{}, false, f.err
	}
	t, ok := f.targets[variant]
	return t, ok, nil
}

func TestAssetService_ResolveVariant(t *testing.T) {
	ctx := context.Background()
	seed := func(repo *fakeAssetRepo, kind string) {
		repo.rows[5] = &model.Asset{ID: 5, UserID: 1, Kind: kind, StorageID: 2, StorageKey: "u1/202610/a.bin", MimeType: kind + "/x"}
	}
	newSvc := func(kind string, res AssetVariantResolver) (*AssetService, *fakeAssetStore) {
		repo := newFakeAssetRepo()
		seed(repo, kind)
		store := newFakeAssetStore()
		svc := NewAssetService(repo, newFakeRegistry(2, objectHandle(2, store, 2*time.Hour)), config.Storage{})
		if res != nil {
			svc.SetVariantResolver(res)
		}
		return svc, store
	}
	const key = "u1/202610/a.bin"

	t.Run("有处理服务：302 到处理地址；公开读缓存一天，并把存储与 key 交给解析器", func(t *testing.T) {
		res := &fakeVariantResolver{targets: map[string]VariantTarget{"thumb": {URL: "https://assets.example.com/cdn-cgi/image/width=512/x"}}}
		svc, store := newSvc("image", res)
		tgt, err := svc.ResolveVariant(ctx, key, "thumb")
		if err != nil || tgt.RedirectURL != "https://assets.example.com/cdn-cgi/image/width=512/x" || tgt.Local != nil {
			t.Fatalf("实际 %+v err=%v", tgt, err)
		}
		if tgt.MaxAge != 24*time.Hour {
			t.Errorf("公开读地址永久稳定，应缓存 24h，实际 %v", tgt.MaxAge)
		}
		if res.gotStore != 2 || res.gotKey != key {
			t.Errorf("应把素材所在存储和 key 交给解析器：%d %q", res.gotStore, res.gotKey)
		}
		if store.urlCalls != 0 {
			t.Error("命中处理服务时不应再给原图签名")
		}
	})

	t.Run("带签名的处理地址：缓存不超过签名有效期的 1/4", func(t *testing.T) {
		res := &fakeVariantResolver{targets: map[string]VariantTarget{"thumb": {URL: "https://x/y?sig=1", Signed: true}}}
		svc, _ := newSvc("image", res)
		tgt, err := svc.ResolveVariant(ctx, key, "thumb")
		if err != nil || tgt.MaxAge != 30*time.Minute {
			t.Fatalf("ttl 2h 的 1/4 应为 30m：%+v err=%v", tgt, err)
		}
	})

	t.Run("视频封面", func(t *testing.T) {
		res := &fakeVariantResolver{targets: map[string]VariantTarget{"poster": {URL: "https://assets.example.com/cdn-cgi/media/x"}}}
		svc, _ := newSvc("video", res)
		tgt, err := svc.ResolveVariant(ctx, key, "poster")
		if err != nil || tgt.RedirectURL != "https://assets.example.com/cdn-cgi/media/x" {
			t.Fatalf("实际 %+v err=%v", tgt, err)
		}
	})

	t.Run("缩略图没有处理服务：回退原图，缓存压到 60 秒以内", func(t *testing.T) {
		svc, store := newSvc("image", &fakeVariantResolver{targets: map[string]VariantTarget{}})
		tgt, err := svc.ResolveVariant(ctx, key, "thumb")
		if err != nil || tgt.RedirectURL != "https://cdn.test/u1/202610/a.bin?ttl=2h0m0s" || store.urlCalls != 1 {
			t.Fatalf("应回退到原图的签名地址：%+v err=%v", tgt, err)
		}
		if tgt.MaxAge != 60*time.Second {
			t.Errorf("回退地址缓存应不超过 60s，以便处理服务发布后尽快生效，实际 %v", tgt.MaxAge)
		}
	})

	t.Run("缩略图回退到本地存储：直接提供文件", func(t *testing.T) {
		local, err := storage.NewLocal(config.LocalStorage{Dir: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		repo := newFakeAssetRepo()
		repo.rows[5] = &model.Asset{ID: 5, UserID: 1, Kind: "image", StorageID: 1, StorageKey: key, MimeType: "image/png"}
		svc := NewAssetService(repo, newFakeRegistry(1, &storage.Handle{ID: 1, Provider: storage.ProviderLocal, Storage: local}), config.Storage{})
		tgt, err := svc.ResolveVariant(ctx, key, "thumb")
		if err != nil || tgt.Local != local {
			t.Fatalf("实际 %+v err=%v", tgt, err)
		}
	})

	t.Run("没有配置解析器：和没有处理服务一样回退", func(t *testing.T) {
		svc, _ := newSvc("image", nil)
		if tgt, err := svc.ResolveVariant(ctx, key, "thumb"); err != nil || tgt.RedirectURL == "" {
			t.Fatalf("实际 %+v err=%v", tgt, err)
		}
	})

	t.Run("封面没有处理服务：404，不回退到几十 MB 的原视频", func(t *testing.T) {
		svc, _ := newSvc("video", &fakeVariantResolver{targets: map[string]VariantTarget{}})
		_, err := svc.ResolveVariant(ctx, key, "poster")
		if got := assetErrCode(t, err); got != errcode.ErrAssetNotFound.Code {
			t.Fatalf("实际 %d", got)
		}
	})

	t.Run("解析器报错：缩略图回退原图，封面 404，都不暴露内部原因", func(t *testing.T) {
		res := &fakeVariantResolver{err: errors.New("sign failed: secret xyz")}
		svc, _ := newSvc("image", res)
		if tgt, err := svc.ResolveVariant(ctx, key, "thumb"); err != nil || tgt.RedirectURL == "" {
			t.Fatalf("缩略图应回退：%+v err=%v", tgt, err)
		}
		svc2, _ := newSvc("video", res)
		_, err := svc2.ResolveVariant(ctx, key, "poster")
		if got := assetErrCode(t, err); got != errcode.ErrAssetNotFound.Code {
			t.Fatalf("封面应 404，实际 %d", got)
		}
	})

	t.Run("素材种类与变体不匹配：404，且不会去找处理服务", func(t *testing.T) {
		for _, tc := range []struct{ kind, variant string }{{"image", "poster"}, {"video", "thumb"}, {"audio", "thumb"}, {"audio", "poster"}} {
			res := &fakeVariantResolver{targets: map[string]VariantTarget{"thumb": {URL: "u"}, "poster": {URL: "u"}}}
			svc, _ := newSvc(tc.kind, res)
			_, err := svc.ResolveVariant(ctx, key, tc.variant)
			if got := assetErrCode(t, err); got != errcode.ErrAssetNotFound.Code || res.calls != 0 {
				t.Errorf("%s + %s：期望 404 且不调用解析器，实际 %d，调用 %d 次", tc.kind, tc.variant, got, res.calls)
			}
		}
	})

	t.Run("变体取值非法 / 素材不存在 / key 非法：统一 404", func(t *testing.T) {
		svc, _ := newSvc("image", &fakeVariantResolver{})
		for _, c := range []struct{ key, variant string }{{key, "huge"}, {key, ""}, {"u1/none.png", "thumb"}, {"../etc/passwd", "thumb"}} {
			_, err := svc.ResolveVariant(ctx, c.key, c.variant)
			if got := assetErrCode(t, err); got != errcode.ErrAssetNotFound.Code {
				t.Errorf("%q %q 应 404，实际 %d", c.key, c.variant, got)
			}
		}
	})

	t.Run("存储不可用：502 业务错误", func(t *testing.T) {
		repo := newFakeAssetRepo()
		seed(repo, "image")
		reg := newFakeRegistry(1)
		reg.getErr = errors.New("decrypt failed")
		svc := NewAssetService(repo, reg, config.Storage{})
		svc.SetVariantResolver(&fakeVariantResolver{})
		_, err := svc.ResolveVariant(ctx, key, "thumb")
		if got := assetErrCode(t, err); got != errcode.ErrStorageUnavailable.Code {
			t.Fatalf("实际 %d", got)
		}
	})
}
