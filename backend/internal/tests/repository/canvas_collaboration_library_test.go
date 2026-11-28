package repository_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"

	"video-canvas/internal/config"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
	"video-canvas/internal/storage"
)

func TestCanvasCollaborationLibraryCopy(t *testing.T) {
	db, repo, owner, editor, viewer, project := newCollaborationFixtureWithPayload(t, imageCollaborationFixture)
	if err := db.AutoMigrate(&model.Asset{}, &model.AssetFolder{}, &model.AssetLibraryItem{}, &model.CanvasDeliveryStage{}); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	shares := repository.NewCanvasShareRepository(db)
	if err := shares.GrantRole(ctx, owner, project.ID, editor, model.CanvasRoleEditor); err != nil {
		t.Fatal(err)
	}
	if err := shares.GrantRole(ctx, owner, project.ID, viewer, model.CanvasRoleViewer); err != nil {
		t.Fatal(err)
	}
	registry := storage.NewRegistry(canvasImportStorage{dir: t.TempDir()}, 0)
	store, err := registry.Default(ctx)
	if err != nil {
		t.Fatal(err)
	}
	source := model.Asset{UserID: owner, Kind: "image", StorageID: store.ID, StorageKey: "owner/shared.png", MimeType: "image/png", ByteSize: 9}
	if err := db.Create(&source).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.Storage.Put(ctx, source.StorageKey, strings.NewReader("real-data"), source.ByteSize, source.MimeType); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Commit(ctx, owner, project.ID, collaborationEdit(project, "publish-image", "data.assetId", fmt.Sprintf(`"%d"`, source.ID))); err != nil {
		t.Fatal(err)
	}
	assets := service.NewAssetService(repository.NewAssetRepository(db), registry, config.Storage{})
	collaboration := service.NewCanvasCollaborationService(repo, shares, nil, true).WithMediaImports(assets)
	request := model.CanvasLibraryCopyReq{Epoch: project.CollaborationEpoch, NodeID: "node", AddAssetLibraryItemReq: model.AddAssetLibraryItemReq{AssetID: source.ID, Name: "保留素材"}}
	assertCanvasLibraryRejection(t, collaboration, project.ID, editor, request, errcode.ErrForbidden)
	assertCanvasLibraryRejection(t, collaboration, project.ID, viewer, request, errcode.ErrForbidden)
	if err := collaboration.SetLibraryPermission(ctx, owner, project.ID, editor, true); err != nil {
		t.Fatal(err)
	}
	assertCanvasLibraryScope(t, collaboration, repo, project, editor, request)
	assertCanvasLibraryInFlightRevocation(t, collaboration, assets, project, editor, request)
	if err := collaboration.SetLibraryPermission(ctx, owner, project.ID, editor, true); err != nil {
		t.Fatal(err)
	}
	copyCanvasLibraryConcurrently(t, collaboration, project.ID, editor, request)
	if err := collaboration.SetLibraryPermission(ctx, owner, project.ID, editor, false); err != nil {
		t.Fatal(err)
	}
	assertCanvasLibraryRejection(t, collaboration, project.ID, editor, request, errcode.ErrForbidden)
	var entries []model.AssetLibraryItem
	if err := db.Where("user_id = ?", editor).Find(&entries).Error; err != nil || len(entries) != 1 || entries[0].AssetID == source.ID || entries[0].Name != request.Name {
		t.Fatalf("个人库应恰好保留一个独立副本: %+v, %v", entries, err)
	}
	if _, err := assets.View(ctx, editor, entries[0].AssetID); err != nil {
		t.Fatal(err)
	}
	var copies int64
	if err := db.Model(&model.Asset{}).Where("user_id = ?", editor).Count(&copies).Error; err != nil || copies != 1 {
		t.Fatalf("仅一份副本应入库: %d, %v", copies, err)
	}
	if err := db.Model(&model.CanvasDeliveryStage{}).Where("1 = 1").Update("created_at", time.Now().Add(-time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	if err := shares.SweepStages(ctx, assets.DeleteStagedObject); err != nil {
		t.Fatal(err)
	}
	var stages int64
	if err := db.Model(&model.CanvasDeliveryStage{}).Count(&stages).Error; err != nil || stages != 0 {
		t.Fatalf("未提交的素材应全部回收: %d, %v", stages, err)
	}
}

func assertCanvasLibraryScope(t *testing.T, svc *service.CanvasCollaborationService, repo *repository.CanvasCollaborationRepository, project *model.CanvasProject, editor uint64, req model.CanvasLibraryCopyReq) {
	t.Helper()
	snapshot, err := svc.Snapshot(t.Context(), editor, project.ID)
	if err != nil || !snapshot.CanCopyToLibrary || snapshot.CanGenerate {
		t.Fatalf("收藏授权应独立于生成授权: %+v, %v", snapshot, err)
	}
	replay, err := repo.Replay(t.Context(), editor, project.ID, req.Epoch, project.Revision+1, 1)
	if err != nil || !replay.CanCopyToLibrary || replay.CanGenerate {
		t.Fatalf("增量应包含收藏授权: %+v, %v", replay, err)
	}
	invalid := req
	invalid.Epoch++
	assertCanvasLibraryRejection(t, svc, project.ID, editor, invalid, errcode.ErrCanvasCollaborationSession)
	invalid = req
	invalid.NodeID = "other-node"
	assertCanvasLibraryRejection(t, svc, project.ID, editor, invalid, errcode.ErrAssetNotFound)
	invalid = req
	invalid.FolderID = 1000
	assertCanvasLibraryRejection(t, svc, project.ID, editor, invalid, errcode.ErrAssetFolderNotFound)
}

func copyCanvasLibraryConcurrently(t *testing.T, svc *service.CanvasCollaborationService, canvas, editor uint64, req model.CanvasLibraryCopyReq) {
	t.Helper()
	group, ctx := errgroup.WithContext(t.Context())
	for range 4 {
		group.Go(func() error { return svc.CopyToLibrary(ctx, editor, canvas, req) })
	}
	if err := group.Wait(); err != nil {
		t.Fatal(err)
	}
}

type canvasLibraryPausedMedia struct {
	*service.AssetService
	staged chan struct{}
	resume chan struct{}
}

func (media canvasLibraryPausedMedia) StageCanvasAsset(ctx context.Context, source *model.Asset, recipient uint64, record func(uint64, string) error) (*model.Asset, error) {
	asset, err := media.AssetService.StageCanvasAsset(ctx, source, recipient, record)
	if err != nil {
		return nil, err
	}
	close(media.staged)
	select {
	case <-media.resume:
		return asset, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func assertCanvasLibraryInFlightRevocation(t *testing.T, svc *service.CanvasCollaborationService, assets *service.AssetService, project *model.CanvasProject, editor uint64, req model.CanvasLibraryCopyReq) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	media := canvasLibraryPausedMedia{AssetService: assets, staged: make(chan struct{}), resume: make(chan struct{})}
	svc.WithMediaImports(media)
	result := make(chan error, 1)
	go func() { result <- svc.CopyToLibrary(ctx, editor, project.ID, req) }()
	select {
	case <-media.staged:
	case err := <-result:
		t.Fatalf("素材应先进入暂存: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := svc.SetLibraryPermission(ctx, project.UserID, project.ID, editor, false); err != nil {
		t.Fatal(err)
	}
	close(media.resume)
	var failure *errcode.Error
	if err := <-result; !errors.As(err, &failure) || failure.Code != errcode.ErrForbidden.Code {
		t.Fatalf("确认事务应拒绝已撤权的在途收藏: %v", err)
	}
	svc.WithMediaImports(assets)
}

func assertCanvasLibraryRejection(t *testing.T, svc *service.CanvasCollaborationService, canvas, user uint64, req model.CanvasLibraryCopyReq, expected *errcode.Error) {
	t.Helper()
	var result *errcode.Error
	if err := svc.CopyToLibrary(t.Context(), user, canvas, req); !errors.As(err, &result) || result.Code != expected.Code {
		t.Fatalf("收藏错误应为 %d: %v", expected.Code, err)
	}
}
