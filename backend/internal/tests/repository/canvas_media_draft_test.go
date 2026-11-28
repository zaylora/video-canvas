package repository_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"video-canvas/internal/config"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
	"video-canvas/internal/storage"
)

const mediaDraftPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/wIAAgMBAhQXQgAAAABJRU5ErkJggg=="

func draftPNG(t *testing.T) []byte {
	t.Helper()
	image, err := base64.StdEncoding.DecodeString(mediaDraftPNG)
	if err != nil {
		t.Fatal(err)
	}
	return image
}

func TestCanvasMediaDraftLifecycle(t *testing.T) {
	verifyCanvasMediaDraftLifecycle(t, canvasImportStorage{dir: t.TempDir()})
}

func TestCanvasMediaDraftS3(t *testing.T) {
	endpoint := os.Getenv("TEST_CANVAS_COLLABORATION_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("需指定独立的协作测试 S3 endpoint 和 canvas-collaboration 测试桶")
	}
	objects := canvasImportStorage{spec: &storage.Spec{
		Provider: storage.ProviderS3, Endpoint: endpoint, Bucket: "canvas-collaboration", Region: "us-east-1",
		Addressing: storage.AddressingPath, AccessKey: "collaboration-test", SecretKey: "collaboration-test-fixture",
	}}
	t.Run("attach", func(t *testing.T) { verifyCanvasMediaDraftLifecycle(t, objects) })
	t.Run("recover", func(t *testing.T) { verifyCanvasMediaDraftRecovery(t, objects) })
}

func verifyCanvasMediaDraftLifecycle(t *testing.T, objects canvasImportStorage) {
	t.Helper()
	ctx := t.Context()
	db, repo, owner, editor, _, project := newCollaborationFixtureWithPayload(t, imageCollaborationFixture)
	if err := db.AutoMigrate(&model.Asset{}, &model.CanvasMediaDraft{}); err != nil {
		t.Fatal(err)
	}
	shares := repository.NewCanvasShareRepository(db)
	if err := shares.GrantRole(ctx, owner, project.ID, editor, model.CanvasRoleEditor); err != nil {
		t.Fatal(err)
	}
	asset := model.Asset{UserID: owner, Kind: "image", StorageID: 1, StorageKey: "source.png"}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	seed, err := repo.Commit(ctx, owner, project.ID, collaborationEdit(project, "source", "data.assetId", fmt.Sprintf(`"%d"`, asset.ID)))
	if err != nil || len(seed.Changes) != 1 {
		t.Fatalf("绑定源图: %+v %v", seed, err)
	}
	registry := storage.NewRegistry(objects, 0)
	assets := service.NewAssetService(repository.NewAssetRepository(db), registry, config.Storage{})
	svc := service.NewCanvasCollaborationService(repo, shares, nil, true).WithMediaDrafts(assets)
	image := draftPNG(t)
	request := model.CanvasMediaDraftReq{ID: "processed-image", Epoch: project.CollaborationEpoch, Revision: seed.Revision, SourceNode: "node", SourceAsset: asset.ID}
	for range 2 {
		if err := svc.StageMediaDraft(ctx, editor, project.ID, request, service.UploadInput{FileName: "mark.png", Body: bytes.NewReader(image)}); err != nil {
			t.Fatal(err)
		}
	}
	changed := append(append([]byte(nil), image...), 0)
	assertDraftError(t, svc.StageMediaDraft(ctx, editor, project.ID, request, service.UploadInput{FileName: "mark.png", Body: bytes.NewReader(changed)}), errcode.ErrCanvasConflict.Code)
	var count int64
	if err := db.Model(&model.Asset{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("暂存不能登记持久素材: %d %v", count, err)
	}
	if _, err := svc.AttachMediaDraft(ctx, owner, project.ID, request.Epoch, request.ID); err == nil {
		t.Fatal("其他账号不能挂载暂存产物")
	}
	first, err := svc.AttachMediaDraft(ctx, editor, project.ID, request.Epoch, request.ID)
	if err != nil || len(first.Changes) != 2 || first.Revision != seed.Revision+1 || first.Source == nil || first.Source.AssetID != fmt.Sprint(asset.ID) {
		t.Fatalf("挂载应原子确认节点和来源连线: %+v %v", first, err)
	}
	retry, err := svc.AttachMediaDraft(ctx, editor, project.ID, request.Epoch, request.ID)
	if err != nil || retry.Revision != first.Revision || len(retry.Changes) != 2 {
		t.Fatalf("重试应返回原收据: %+v %v", retry, err)
	}
	if err := db.Model(&model.Asset{}).Where("user_id = ?", owner).Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("挂载只应创建一份所有者素材: %d %v", count, err)
	}
	verifyAttachedCanvasMedia(t, db, registry, asset.ID, image)
}

func verifyAttachedCanvasMedia(t *testing.T, db *gorm.DB, registry *storage.Registry, source uint64, expected []byte) {
	t.Helper()
	ctx := t.Context()
	var asset model.Asset
	if err := db.Where("id <> ?", source).Take(&asset).Error; err != nil {
		t.Fatal(err)
	}
	handle, err := registry.Get(ctx, asset.StorageID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Storage.Delete(context.Background(), asset.StorageKey) })
	file, err := handle.Storage.Open(ctx, asset.StorageKey)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	actual, err := io.ReadAll(file)
	if err != nil || !bytes.Equal(actual, expected) {
		t.Fatalf("成品素材应保留上传内容: %v", err)
	}
}

func TestCanvasMediaDraftRejectsChangedSourceAndRevocation(t *testing.T) {
	verifyCanvasMediaDraftRecovery(t, canvasImportStorage{dir: t.TempDir()})
}

func verifyCanvasMediaDraftRecovery(t *testing.T, objects canvasImportStorage) {
	t.Helper()
	ctx := t.Context()
	db, repo, owner, editor, _, project := newCollaborationFixtureWithPayload(t, imageCollaborationFixture)
	if err := db.AutoMigrate(&model.Asset{}, &model.CanvasMediaDraft{}); err != nil {
		t.Fatal(err)
	}
	shares := repository.NewCanvasShareRepository(db)
	if err := shares.GrantRole(ctx, owner, project.ID, editor, model.CanvasRoleEditor); err != nil {
		t.Fatal(err)
	}
	asset := seedMediaDraftSource(t, db, repo, project, owner, "source.png")
	registry := storage.NewRegistry(objects, 0)
	assets := service.NewAssetService(repository.NewAssetRepository(db), registry, config.Storage{})
	svc := service.NewCanvasCollaborationService(repo, shares, nil, true).WithMediaDrafts(assets)
	req := model.CanvasMediaDraftReq{ID: "keep-unattached", Epoch: project.CollaborationEpoch, Revision: project.Revision, SourceNode: "node", SourceAsset: asset}
	if err := svc.StageMediaDraft(ctx, editor, project.ID, req, service.UploadInput{FileName: "mark.png", Body: bytes.NewReader(draftPNG(t))}); err != nil {
		t.Fatal(err)
	}
	if err := shares.Remove(ctx, owner, project.ID, editor); err != nil {
		t.Fatal(err)
	}
	_, err := svc.AttachMediaDraft(ctx, editor, project.ID, req.Epoch, req.ID)
	assertDraftError(t, err, errcode.ErrCanvasNotFound.Code)
	if err := shares.GrantRole(ctx, owner, project.ID, editor, model.CanvasRoleEditor); err != nil {
		t.Fatal(err)
	}
	seedMediaDraftSource(t, db, repo, project, owner, "replacement.png")
	_, err = svc.AttachMediaDraft(ctx, editor, project.ID, req.Epoch, req.ID)
	assertDraftError(t, err, errcode.ErrCanvasConflict.Code)
	var draft model.CanvasMediaDraft
	if err := db.Take(&draft).Error; err != nil {
		t.Fatal(err)
	}
	assertExpiredObjectRecovered(t, repo, assets, registry, draft, func() error {
		return db.Model(&draft).Update("expires_at", time.Now().Add(-time.Minute)).Error
	})
}

func seedMediaDraftSource(t *testing.T, db *gorm.DB, repo *repository.CanvasCollaborationRepository, project *model.CanvasProject, owner uint64, key string) uint64 {
	t.Helper()
	asset := model.Asset{UserID: owner, Kind: "image", StorageID: 1, StorageKey: key}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	seed, err := repo.Commit(t.Context(), owner, project.ID, collaborationEdit(project, strings.TrimSuffix(key, ".png"), "data.assetId", fmt.Sprintf(`"%d"`, asset.ID)))
	if err != nil || len(seed.Changes) != 1 {
		t.Fatalf("绑定源图: %+v %v", seed, err)
	}
	project.Revision = seed.Revision
	return asset.ID
}

func assertDraftError(t *testing.T, err error, code int) {
	t.Helper()
	var biz *errcode.Error
	if !errors.As(err, &biz) || biz.Code != code {
		t.Fatalf("错误码应为 %d，得到 %v", code, err)
	}
}

func assertExpiredObjectRecovered(t *testing.T, repo *repository.CanvasCollaborationRepository, assets *service.AssetService, registry *storage.Registry, draft model.CanvasMediaDraft, expire func() error) {
	t.Helper()
	ctx := t.Context()
	handle, err := registry.Get(ctx, draft.StorageID)
	if err != nil {
		t.Fatal(err)
	}
	file, err := handle.Storage.Open(ctx, draft.StorageKey)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, file)
	_ = file.Close()
	if err := expire(); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("对象存储临时不可用")
	if err := repo.SweepMediaDrafts(ctx, func(context.Context, uint64, string) error { return injected }); !errors.Is(err, injected) {
		t.Fatalf("回收失败应保留重试机会: %v", err)
	}
	if err := repo.SweepMediaDrafts(ctx, assets.DeleteStagedObject); err != nil {
		t.Fatal(err)
	}
	if file, err := handle.Storage.Open(ctx, draft.StorageKey); !errors.Is(err, storage.ErrNotFound) {
		if file != nil {
			_ = file.Close()
		}
		t.Fatalf("应回收暂存对象: %v", err)
	}
}
