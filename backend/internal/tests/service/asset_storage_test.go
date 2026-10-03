package service_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	. "video-canvas/internal/service"

	"video-canvas/internal/config"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/storage"
)

// 多套存储：素材记录自己写在哪一套，读取、签名、清理都按它走，和当前默认存储无关。

func TestAssetService_WritesToDefaultAndRecordsStorage(t *testing.T) {
	assetIsolateTempDir(t)
	repo := newFakeAssetRepo()
	storeA, storeB := newFakeAssetStore(), newFakeAssetStore()
	reg := newFakeRegistry(2, objectHandle(2, storeA, time.Hour), objectHandle(3, storeB, time.Hour))
	svc := NewAssetService(repo, reg, config.Storage{MaxUpload: 1 << 20})
	png := assetTestPNG(t, 8, 8)

	up := func() *model.Asset {
		v, err := svc.Upload(context.Background(), 1, UploadInput{FileName: "a.png", Size: -1, Body: bytes.NewReader(png)})
		if err != nil {
			t.Fatal(err)
		}
		return repo.rows[v.ID]
	}

	first := up()
	if first.StorageID != 2 || len(storeA.objects) != 1 || len(storeB.objects) != 0 {
		t.Fatalf("默认是 2 时应写入存储 A：storage_id=%d A=%d B=%d", first.StorageID, len(storeA.objects), len(storeB.objects))
	}

	reg.defaultID = 3 // 管理员把默认切到 B
	second := up()
	if second.StorageID != 3 || len(storeB.objects) != 1 {
		t.Fatalf("切换默认后新上传应写入存储 B：storage_id=%d B=%d", second.StorageID, len(storeB.objects))
	}

	// 旧素材仍从 A 读取，不受默认存储切换影响
	f, err := svc.Open(context.Background(), 1, first.ID)
	if err != nil {
		t.Fatalf("旧素材应仍可读取：%v", err)
	}
	defer f.Body.Close()
	b, _ := io.ReadAll(f.Body)
	if !bytes.Equal(b, png) {
		t.Fatal("读到的内容不是存储 A 里的对象")
	}
	if storeB.urlCalls != 0 {
		t.Errorf("读取旧素材不应访问存储 B，B 的 URL 调用 %d 次", storeB.urlCalls)
	}
}

func TestAssetService_FailureCleansTheSameStoreItWroteTo(t *testing.T) {
	assetIsolateTempDir(t)
	repo := newFakeAssetRepo()
	repo.createErr = errors.New("db down")
	storeA, storeB := newFakeAssetStore(), newFakeAssetStore()
	reg := newFakeRegistry(2, objectHandle(2, storeA, time.Hour), objectHandle(3, storeB, time.Hour))
	svc := NewAssetService(repo, reg, config.Storage{MaxUpload: 1 << 20})

	_, err := svc.Upload(context.Background(), 1, UploadInput{FileName: "a.png", Size: -1, Body: bytes.NewReader(assetTestPNG(t, 8, 8))})
	if err == nil {
		t.Fatal("入库失败应返回错误")
	}
	if len(storeA.deleted) != 1 || len(storeB.deleted) != 0 {
		t.Errorf("应只清理写入的存储 A：A=%v B=%v", storeA.deleted, storeB.deleted)
	}
}

func TestAssetService_StableURL(t *testing.T) {
	repo := newFakeAssetRepo()
	repo.rows[5] = &model.Asset{ID: 5, UserID: 1, Kind: "image", StorageID: 2, StorageKey: "u1/202610/a.png", MimeType: "image/png"}
	store := newFakeAssetStore()
	svc := NewAssetService(repo, newFakeRegistry(2, objectHandle(2, store, time.Hour)), config.Storage{Local: config.LocalStorage{BaseURL: "https://api.example.com/"}})

	v, err := svc.View(context.Background(), 1, 5)
	if err != nil {
		t.Fatal(err)
	}
	if v.URL != "https://api.example.com/files/u1/202610/a.png" {
		t.Errorf("视图应返回带站点前缀的稳定地址，实际 %q", v.URL)
	}
	if store.urlCalls != 0 {
		t.Errorf("生成稳定地址不应访问存储签名，调用了 %d 次", store.urlCalls)
	}
}

func TestAssetService_ResolveFile(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	seed := func(repo *fakeAssetRepo, storageID uint64) {
		repo.rows[5] = &model.Asset{ID: 5, UserID: 1, Kind: "image", StorageID: storageID, StorageKey: "u1/202610/a.png", MimeType: "image/png"}
	}

	t.Run("本地存储：直接由后端提供文件", func(t *testing.T) {
		local, err := storage.NewLocal(config.LocalStorage{Dir: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		repo := newFakeAssetRepo()
		seed(repo, 1)
		reg := newFakeRegistry(1, &storage.Handle{ID: 1, Provider: storage.ProviderLocal, Storage: local})
		tgt, err := NewAssetService(repo, reg, config.Storage{}).ResolveFile(ctx, "u1/202610/a.png")
		if err != nil || tgt.Local != local || tgt.RedirectURL != "" {
			t.Fatalf("实际 %+v err=%v", tgt, err)
		}
	})

	t.Run("对象存储：跳转到该存储生成的签名地址，并在 ttl/2 内复用", func(t *testing.T) {
		repo := newFakeAssetRepo()
		seed(repo, 2)
		store := newFakeAssetStore()
		svc := NewAssetService(repo, newFakeRegistry(9, objectHandle(2, store, 2*time.Hour)), config.Storage{})
		clock := now
		svc.SetNow(func() time.Time { return clock })

		t1, err := svc.ResolveFile(ctx, "u1/202610/a.png")
		if err != nil || t1.RedirectURL != "https://cdn.test/u1/202610/a.png?ttl=2h0m0s" || t1.Local != nil {
			t.Fatalf("实际 %+v err=%v", t1, err)
		}
		if t1.MaxAge != 30*time.Minute {
			t.Errorf("客户端缓存时长应为 ttl/4=30m，实际 %v", t1.MaxAge)
		}
		_, _ = svc.ResolveFile(ctx, "u1/202610/a.png")
		if store.urlCalls != 1 {
			t.Errorf("同一时间窗内应复用签名结果，URL 调用了 %d 次", store.urlCalls)
		}
		clock = now.Add(61 * time.Minute) // 超过 ttl/2
		_, _ = svc.ResolveFile(ctx, "u1/202610/a.png")
		if store.urlCalls != 2 {
			t.Errorf("超过 ttl/2 应重新签名，URL 调用了 %d 次", store.urlCalls)
		}
	})

	t.Run("素材记录的存储决定跳转目标，和默认存储无关", func(t *testing.T) {
		repo := newFakeAssetRepo()
		seed(repo, 2)
		storeA, storeB := newFakeAssetStore(), newFakeAssetStore()
		reg := newFakeRegistry(3, objectHandle(2, storeA, time.Hour), objectHandle(3, storeB, time.Hour))
		if _, err := NewAssetService(repo, reg, config.Storage{}).ResolveFile(ctx, "u1/202610/a.png"); err != nil {
			t.Fatal(err)
		}
		if storeA.urlCalls != 1 || storeB.urlCalls != 0 {
			t.Errorf("应只访问素材所在的存储 A：A=%d B=%d", storeA.urlCalls, storeB.urlCalls)
		}
	})

	t.Run("素材不存在 / key 非法统一 404", func(t *testing.T) {
		svc := NewAssetService(newFakeAssetRepo(), newFakeRegistry(1, objectHandle(1, newFakeAssetStore(), time.Hour)), config.Storage{})
		for _, key := range []string{"u1/none.png", "../etc/passwd", ""} {
			_, err := svc.ResolveFile(ctx, key)
			if got := assetErrCode(t, err); got != errcode.ErrAssetNotFound.Code {
				t.Errorf("%q 应返回素材不存在，实际 %d", key, got)
			}
		}
	})

	t.Run("存储已不可用返回 502 业务错误，不泄露内部原因", func(t *testing.T) {
		repo := newFakeAssetRepo()
		seed(repo, 2)
		reg := newFakeRegistry(1)
		reg.getErr = errors.New("decrypt failed: secret key changed")
		_, err := NewAssetService(repo, reg, config.Storage{}).ResolveFile(ctx, "u1/202610/a.png")
		if got := assetErrCode(t, err); got != errcode.ErrStorageUnavailable.Code {
			t.Fatalf("期望 ErrStorageUnavailable，实际 %d", got)
		}
	})
}
