package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"video-canvas/internal/config"
	"video-canvas/internal/model"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/worker"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
	"video-canvas/internal/storage"
)

type collaborationFixtureExecutor struct {
	fakeTaskExecutor
	downloads int
}

func (executor *collaborationFixtureExecutor) Submit(context.Context, *provider.Snapshot, provider.SubmitInput) (*provider.SubmitResult, error) {
	return &provider.SubmitResult{Immediate: &provider.QueryResult{
		Status: provider.StatusSucceeded,
		Outputs: []provider.Output{{
			Type: provider.OutputURL, URL: "https://example.test/collaboration-fixture.mp4", Mime: "video/mp4",
		}},
	}}, nil
}

func (executor *collaborationFixtureExecutor) Download(context.Context, *provider.Snapshot, string) (*provider.Download, error) {
	executor.downloads++
	if executor.downloads == 1 {
		return nil, errors.New("测试下载临时中断")
	}
	fixture, err := os.Open("../../marketing/samples/cream_ref.mp4")
	if err != nil {
		return nil, err
	}
	return &provider.Download{Body: fixture, ContentType: "video/mp4", Size: -1}, nil
}

func TestCanvasCollaborationWorkerRetry(t *testing.T) {
	t.Run("transfer-result", func(t *testing.T) { verifyCanvasWorkerRetry(t, false) })
	t.Run("delete-while-running", func(t *testing.T) { verifyCanvasWorkerRetry(t, true) })
}

func verifyCanvasWorkerRetry(t *testing.T, deleteBeforeResult bool) {
	t.Helper()
	ctx := t.Context()
	limits := &fakeLimits{initial: 50, defMax: 4}
	env := newGTIEnv(t, limits)
	scenario := seedMemberGeneration(t, env)
	if err := env.db.AutoMigrate(&model.Asset{}, &model.CanvasDelivery{}, &model.CanvasDeliveryStage{}); err != nil {
		t.Fatal(err)
	}
	entry := storage.Entry{ID: 1, Provider: "local", Local: config.LocalStorage{Dir: t.TempDir()}}
	registry := storage.NewRegistry(collaborationStorage{entry: entry}, 0)
	assets := service.NewAssetService(repository.NewAssetRepository(env.db), registry, config.Storage{})
	executor := &collaborationFixtureExecutor{}
	tasks := service.NewGenerationTaskService(service.GenerationTaskDeps{
		Repo: env.store, Registry: env.registry, Executor: executor, Assets: assets, Broadcaster: env.bc, Limits: limits,
	})
	runner := worker.New(tasks, executor, assets, worker.Options{})
	readTask := func() *model.GenerationTask {
		current, err := tasks.GetTask(ctx, scenario.memberTask)
		if err != nil {
			t.Fatal(err)
		}
		return current
	}
	runner.Handle(ctx, *readTask())
	ready := readTask()
	if ready.Status != model.TaskFinalizing || executor.downloads != 0 {
		t.Fatalf("提交后应等待转存: %s / %d", ready.Status, executor.downloads)
	}
	if deleteBeforeResult {
		deleteCanvasWorkerNode(t, env, scenario)
	}
	runner.Handle(ctx, *ready)
	retry := readTask()
	if retry.Status != model.TaskFinalizing || retry.PollAttempts != 1 || executor.downloads != 1 {
		t.Fatalf("下载失败后应保留成功的供应商结果并重试: %s / %d / %d", retry.Status, retry.PollAttempts, executor.downloads)
	}
	runner.Handle(ctx, *retry)
	done := readTask()
	if done.Status != model.TaskSucceeded || executor.downloads != 2 {
		t.Fatalf("工作进程重试后应成功: %s / %d", done.Status, executor.downloads)
	}
	if deleteBeforeResult {
		assertDeletedCanvasWorkerOutput(t, env, scenario, done)
		return
	}
	shared := assertCanvasWorkerOutput(t, env, scenario, done)
	if _, err := tasks.Cancel(ctx, env.user, scenario.ownerTask); err != nil {
		t.Fatal(err)
	}
	assertCanvasWorkerTransfer(t, env, scenario, assets, shared)
}

func deleteCanvasWorkerNode(t *testing.T, env *gtiEnv, scenario *memberGenerationScenario) {
	t.Helper()
	ctx := t.Context()
	canvasID := uint64(scenario.req.CanvasID)
	board, _, err := scenario.canvas.Snapshot(ctx, env.user, canvasID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := scenario.canvas.Commit(ctx, env.user, canvasID, repository.CanvasCollaborationBatch{
		OpID: "delete-during-transfer", Epoch: board.CollaborationEpoch, BaseRevision: board.Revision,
		Changes: []repository.CanvasCollaborationChange{{Kind: "node", ID: scenario.req.NodeID, Action: "delete"}},
	})
	if err != nil || result.Revision != board.Revision+1 || len(result.Changes) != 1 {
		t.Fatalf("工作进程运行中应可删除节点: %+v / %v", result, err)
	}
}

func assertDeletedCanvasWorkerOutput(t *testing.T, env *gtiEnv, scenario *memberGenerationScenario, task *model.GenerationTask) {
	t.Helper()
	ctx := t.Context()
	board, _, err := scenario.canvas.Snapshot(ctx, env.user, uint64(scenario.req.CanvasID))
	if err != nil || strings.Contains(string(board.PayloadJSON), `"id":"n1"`) || len(task.OutputJSON) <= 2 {
		t.Fatalf("任务仍应结算并保留私有产物，画布不得复活节点: %v", err)
	}
	var count int64
	if err := env.db.Model(&model.Asset{}).Where("user_id = ?", env.user).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("删除节点后不得创建公开结果副本: %d / %v", count, err)
	}
	assertCanvasWorkerSettlement(t, env, scenario)
}

func assertCanvasWorkerOutput(t *testing.T, env *gtiEnv, scenario *memberGenerationScenario, task *model.GenerationTask) model.Asset {
	t.Helper()
	ctx := t.Context()
	var outputs []model.TaskOutput
	if err := json.Unmarshal(task.OutputJSON, &outputs); err != nil || len(outputs) != 1 || outputs[0].AssetID == 0 {
		t.Fatalf("任务应保留原始产物: %v / %s", err, task.OutputJSON)
	}
	var source, shared model.Asset
	if err := env.db.First(&source, outputs[0].AssetID).Error; err != nil {
		t.Fatal(err)
	}
	if err := env.db.Where("user_id = ? AND mime_type = ?", env.user, "video/mp4").First(&shared).Error; err != nil {
		t.Fatal(err)
	}
	board, _, err := scenario.canvas.Snapshot(ctx, env.user, uint64(scenario.req.CanvasID))
	if err != nil || shared.StorageKey == source.StorageKey || source.UserID != scenario.member || !strings.Contains(string(board.PayloadJSON), shared.StorageKey) {
		t.Fatalf("确认画布应引用所有者独立副本: %v", err)
	}
	assertCanvasWorkerSettlement(t, env, scenario)
	return shared
}

func assertCanvasWorkerSettlement(t *testing.T, env *gtiEnv, scenario *memberGenerationScenario) {
	t.Helper()
	ctx := t.Context()
	account, err := env.store.Reconcile(ctx, scenario.member)
	if err != nil || account.SettleSum != 10 || account.Frozen != 0 {
		t.Fatalf("下载重试应只结算一次: %+v / %v", account, err)
	}
}

func assertCanvasWorkerTransfer(t *testing.T, env *gtiEnv, scenario *memberGenerationScenario, assets *service.AssetService, shared model.Asset) {
	t.Helper()
	ctx := t.Context()
	canvasID := uint64(scenario.req.CanvasID)
	board, _, err := scenario.canvas.Snapshot(ctx, env.user, canvasID)
	if err != nil {
		t.Fatal(err)
	}
	recipient := model.User{Username: "canvas-recipient", Email: "canvas-recipient@example.test"}
	if err := env.db.Create(&recipient).Error; err != nil {
		t.Fatal(err)
	}
	sharing := service.NewCanvasShareService(repository.NewCanvasShareRepository(env.db), assets, nil)
	req := model.CanvasShareReq{Mode: "transfer", Email: recipient.Email, RequestKey: "transfer-worker-output", Revision: board.Revision}
	if err := sharing.Share(ctx, env.user, canvasID, req); err != nil {
		t.Fatal(err)
	}
	var transferred model.CanvasProject
	var video model.Asset
	if err := env.db.Where("user_id = ?", recipient.ID).First(&transferred).Error; err != nil {
		t.Fatal(err)
	}
	if err := env.db.Where("user_id = ? AND mime_type = ?", recipient.ID, shared.MimeType).First(&video).Error; err != nil {
		t.Fatal(err)
	}
	if transferred.CollaborationActive || video.StorageKey == shared.StorageKey || !strings.Contains(string(transferred.PayloadJSON), video.StorageKey) {
		t.Fatal("接收人应获得包含独立视频产物的个人画布")
	}
	file, err := assets.Open(ctx, recipient.ID, video.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Body.Close()
	if size, err := io.Copy(io.Discard, file.Body); err != nil || size != shared.ByteSize || size <= 0 {
		t.Fatalf("接收人的独立产物字节应可读: %d / %v", size, err)
	}
}
