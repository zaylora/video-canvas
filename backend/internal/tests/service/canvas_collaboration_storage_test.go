package service_test

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"video-canvas/internal/config"
	"video-canvas/internal/model"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
	"video-canvas/internal/storage"
)

type collaborationStorage struct{ entry storage.Entry }

func (source collaborationStorage) DefaultID(context.Context) (uint64, error) {
	return source.entry.ID, nil
}

func (source collaborationStorage) Entry(context.Context, uint64) (storage.Entry, error) {
	return source.entry, nil
}

type collaborationStagedAssets struct {
	*service.AssetService
	staged     *model.Asset
	afterStage func() error
}

func (assets *collaborationStagedAssets) StageCanvasAsset(ctx context.Context, source *model.Asset, owner uint64, record func(uint64, string) error) (*model.Asset, error) {
	asset, err := assets.AssetService.StageCanvasAsset(ctx, source, owner, record)
	if err != nil {
		return nil, err
	}
	assets.staged = asset
	if assets.afterStage != nil {
		return asset, assets.afterStage()
	}
	return asset, nil
}

func TestCanvasCollaborationGenerationStorage(t *testing.T) {
	entry := storage.Entry{ID: 1, Provider: "local", Local: config.LocalStorage{Dir: t.TempDir()}}
	verifyCanvasGenerationStorageCases(t, entry)
}

func TestCanvasCollaborationGenerationS3(t *testing.T) {
	endpoint := os.Getenv("TEST_CANVAS_COLLABORATION_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("需指定独立的协作测试 S3 endpoint 和 canvas-collaboration 测试桶")
	}
	entry := storage.Entry{ID: 1, Provider: storage.ProviderS3, Spec: storage.Spec{
		Provider: storage.ProviderS3, Endpoint: endpoint, Bucket: "canvas-collaboration", Region: "us-east-1",
		Addressing: storage.AddressingPath, AccessKey: "collaboration-test", SecretKey: "collaboration-test-fixture",
	}}
	verifyCanvasGenerationStorageCases(t, entry)
}

func verifyCanvasGenerationStorageCases(t *testing.T, entry storage.Entry) {
	t.Helper()
	for _, scenario := range []string{"authorized", "revoked-after-stage"} {
		t.Run(scenario, func(t *testing.T) { verifyCanvasGenerationStorage(t, entry, scenario) })
	}
}

func verifyCanvasGenerationStorage(t *testing.T, entry storage.Entry, testCase string) {
	t.Helper()
	ctx := t.Context()
	limits := &fakeLimits{initial: 50, defMax: 4}
	env := newGTIEnv(t, limits)
	scenario := seedMemberGeneration(t, env)
	if err := env.db.AutoMigrate(&model.Asset{}, &model.CanvasDeliveryStage{}); err != nil {
		t.Fatal(err)
	}
	registry := storage.NewRegistry(collaborationStorage{entry: entry}, 0)
	store, err := registry.Default(ctx)
	if err != nil {
		t.Fatal(err)
	}
	assets := &collaborationStagedAssets{AssetService: service.NewAssetService(repository.NewAssetRepository(env.db), registry, config.Storage{})}
	canvasID := uint64(scenario.req.CanvasID)
	if testCase == "revoked-after-stage" {
		assets.afterStage = func() error {
			return scenario.share.SetGenerationPermission(ctx, env.user, canvasID, scenario.member, false)
		}
	}
	env.svc = service.NewGenerationTaskService(service.GenerationTaskDeps{
		Repo: env.store, Registry: env.registry, Executor: env.exec, Assets: assets, Broadcaster: env.bc, Limits: limits,
	})
	source := model.Asset{UserID: scenario.member, Kind: "video", Source: model.AssetSourceGenerated, StorageID: store.ID,
		StorageKey: "member/result.mp4", MimeType: "video/mp4", ByteSize: 14}
	if err := env.db.Create(&source).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.Storage.Put(ctx, source.StorageKey, strings.NewReader("fixture-video!"), source.ByteSize, source.MimeType); err != nil {
		t.Fatal(err)
	}
	completeCollaborationOutputs(t, env, scenario.memberTask, []model.TaskOutput{{AssetID: source.ID, URL: assets.FileURL(source.StorageKey), MediaType: "video"}})
	if assets.staged == nil || assets.staged.StorageKey == source.StorageKey {
		t.Fatalf("回填前应复制到独立存储对象: %+v", assets.staged)
	}
	account, err := env.store.Reconcile(ctx, scenario.member)
	if err != nil || account.SettleSum != 10 || account.Frozen != 0 {
		t.Fatalf("发起者应独立结算自己的任务: %+v, %v", account, err)
	}
	assertTerminalCanvasStorage(t, env, scenario, assets, store, testCase)
	assertCanvasStorageBytes(t, store, source.StorageKey)
}

func assertTerminalCanvasStorage(t *testing.T, env *gtiEnv, scenario *memberGenerationScenario, assets *collaborationStagedAssets, store *storage.Handle, testCase string) {
	t.Helper()
	ctx := t.Context()
	board, _, err := scenario.canvas.Snapshot(ctx, env.user, uint64(scenario.req.CanvasID))
	if err != nil {
		t.Fatal(err)
	}
	published := strings.Contains(string(board.PayloadJSON), assets.staged.StorageKey)
	var stages int64
	if err := env.db.Model(&model.CanvasDeliveryStage{}).Count(&stages).Error; err != nil {
		t.Fatal(err)
	}
	if testCase == "authorized" {
		if !published || stages != 0 {
			t.Fatalf("成功结算应确认独立资源并清除暂存记录: %v, %d", published, stages)
		}
		assertCanvasStorageBytes(t, store, assets.staged.StorageKey)
		return
	}
	if published || stages != 1 {
		t.Fatalf("撤权应阻止回填并保留待回收对象: %v, %d", published, stages)
	}
	if err := env.db.Model(&model.CanvasDeliveryStage{}).Where("storage_key = ?", assets.staged.StorageKey).Update("created_at", time.Now().Add(-11*time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	if err := scenario.share.SweepStages(ctx, assets.DeleteStagedObject); err != nil {
		t.Fatal(err)
	}
	file, err := store.Storage.Open(ctx, assets.staged.StorageKey)
	if err == nil {
		_ = file.Close()
	}
	if !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("回收后暂存对象应从存储删除: %v", err)
	}
}

func assertCanvasStorageBytes(t *testing.T, store *storage.Handle, key string) {
	t.Helper()
	file, err := store.Storage.Open(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	if value, err := io.ReadAll(file); err != nil || string(value) != "fixture-video!" {
		t.Fatalf("文件内容不匹配: %q, %v", value, err)
	}
}
