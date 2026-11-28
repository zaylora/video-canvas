package repository_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"gorm.io/gorm"

	"video-canvas/internal/config"
	"video-canvas/internal/model"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
	"video-canvas/internal/storage"
)

const imageCollaborationFixture = `{"nodes":[{"id":"node","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"image","label":"图片"}}],"edges":[]}`

type canvasImportStorage struct {
	dir  string
	spec *storage.Spec
}

func (canvasImportStorage) DefaultID(context.Context) (uint64, error) { return 1, nil }

func (source canvasImportStorage) Entry(context.Context, uint64) (storage.Entry, error) {
	if source.spec != nil {
		return storage.Entry{ID: 1, Provider: storage.ProviderS3, Spec: *source.spec}, nil
	}
	return storage.Entry{ID: 1, Provider: "local", Local: config.LocalStorage{Dir: source.dir}}, nil
}

func TestCanvasCollaborationMemberImportStorage(t *testing.T) {
	db, repo, owner, editor, _, project := newCollaborationFixtureWithPayload(t, imageCollaborationFixture)
	if err := db.AutoMigrate(&model.Asset{}, &model.CanvasDeliveryStage{}); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	shares := repository.NewCanvasShareRepository(db)
	if err := shares.GrantRole(ctx, owner, project.ID, editor, model.CanvasRoleEditor); err != nil {
		t.Fatal(err)
	}
	registry := storage.NewRegistry(canvasImportStorage{dir: t.TempDir()}, 0)
	store, err := registry.Default(ctx)
	if err != nil {
		t.Fatal(err)
	}
	source := model.Asset{UserID: editor, Kind: "image", StorageID: store.ID, StorageKey: "editor/import.png", MimeType: "image/png", ByteSize: 9}
	if err := db.Create(&source).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.Storage.Put(ctx, source.StorageKey, strings.NewReader("real-data"), source.ByteSize, source.MimeType); err != nil {
		t.Fatal(err)
	}
	assets := service.NewAssetService(repository.NewAssetRepository(db), registry, config.Storage{})
	collaboration := service.NewCanvasCollaborationService(repo, shares, nil, true).WithMediaImports(assets)
	batch := repository.CanvasCollaborationBatch{Epoch: project.CollaborationEpoch, BaseRevision: project.Revision, OpID: "member-import",
		Changes: []repository.CanvasCollaborationChange{{Kind: "node", ID: "imported", Action: "create", Value: json.RawMessage(fmt.Sprintf(
			`{"position":{"x":0,"y":0},"data":{"kind":"image","label":"试拍","assetId":"%d"}}`, source.ID))},
			{Kind: "node", ID: "actor-card", Action: "create", Value: json.RawMessage(fmt.Sprintf(
				`{"position":{"x":20,"y":0},"data":{"kind":"character","label":"角色","character":{"name":"演员","look":"基础形象","description":"","extras":[{"assetId":"%d"}],"voice":null}}}`, source.ID))}}}
	result, err := collaboration.Commit(ctx, editor, project.ID, batch)
	if err != nil || len(result.Changes) != 2 || !strings.Contains(string(result.Changes[0].Value), "/api/v1/canvas-media/") || strings.Contains(string(result.Changes[0].Value), "/files/") {
		t.Fatalf("成员应收到已确认的共享引用: %+v, %v", result, err)
	}
	retry, err := collaboration.Commit(ctx, editor, project.ID, batch)
	if err != nil || retry.Revision != result.Revision || len(retry.Changes) != 2 {
		t.Fatalf("重试应使用原有确认: %+v, %v", retry, err)
	}
	var imported model.Asset
	if err := db.Where("user_id = ?", owner).Take(&imported).Error; err != nil || imported.ID == source.ID || imported.StorageKey == source.StorageKey {
		t.Fatalf("画布所有者应持有独立存储对象: %+v, %v", imported, err)
	}
	assertStructuredCopy(t, db, result, owner, imported.ID)
	assertSingleCanvasImport(t, collaboration, shares, assets, owner, editor, project, imported)
	var stages int64
	if err := db.Model(&model.CanvasDeliveryStage{}).Count(&stages).Error; err != nil || stages != 0 {
		t.Fatalf("成功导入及其重试不应遗留暂存记录: %d, %v", stages, err)
	}
}

func assertStructuredCopy(t *testing.T, db *gorm.DB, result *repository.CanvasCollaborationCommit, owner, imported uint64) {
	t.Helper()
	value := string(result.Changes[1].Value)
	if !strings.Contains(value, fmt.Sprintf(`"assetId":"%d"`, imported)) || !strings.Contains(value, "/api/v1/canvas-media/") || strings.Contains(value, "/files/") {
		t.Fatalf("嵌套素材应复用相同的独立副本: %s", value)
	}
	var count int64
	if err := db.Model(&model.Asset{}).Where("user_id = ?", owner).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("主素材和嵌套素材同批导入只复制一次: %d, %v", count, err)
	}
}

func assertSingleCanvasImport(t *testing.T, collaboration *service.CanvasCollaborationService, shares *repository.CanvasShareRepository, assets *service.AssetService, owner, editor uint64, project *model.CanvasProject, imported model.Asset) {
	t.Helper()
	ctx := t.Context()
	sharing := service.NewCanvasShareService(shares, assets, nil)
	file, err := sharing.Media(ctx, editor, project.ID, imported.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Body.Close() }()
	content, err := io.ReadAll(file.Body)
	if err != nil || string(content) != "real-data" {
		t.Fatalf("成员应能读取独立素材内容: %q, %v", content, err)
	}
	snapshot, err := collaboration.Snapshot(ctx, owner, project.ID)
	if err != nil || !strings.Contains(string(snapshot.Canvas.PayloadJSON), imported.StorageKey) {
		t.Fatalf("所有者的快照应保存已确认素材: %+v, %v", snapshot, err)
	}
	if err := shares.Remove(ctx, owner, project.ID, editor); err != nil {
		t.Fatal(err)
	}
	if _, err := sharing.Media(ctx, editor, project.ID, imported.ID); err == nil {
		t.Fatal("撤销成员权限后应关闭素材访问")
	}
}
