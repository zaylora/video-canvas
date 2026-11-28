package repository_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"gorm.io/gorm"

	"video-canvas/internal/config"
	"video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/utils"
	"video-canvas/internal/pkg/videoframe"
	"video-canvas/internal/provider/queue"
	"video-canvas/internal/repository"
	"video-canvas/internal/router"
	"video-canvas/internal/service"
	"video-canvas/internal/storage"
)

type canvasComposeEnqueueStub struct{ calls int }

func (stub *canvasComposeEnqueueStub) EnqueueCanvasComposeTx(context.Context, *sql.Tx, uint64) error {
	stub.calls++
	return nil
}

func TestCanvasComposeSubmission(t *testing.T) {
	ctx := t.Context()
	db, repo, owner, editor, viewer, project := newCollaborationFixtureWithPayload(t, `{"nodes":[{"id":"clip","type":"canvas","data":{"kind":"video","label":"片段"}}],"edges":[]}`)
	if err := db.AutoMigrate(&model.Asset{}, &model.CanvasComposeJob{}); err != nil {
		t.Fatal(err)
	}
	shares := repository.NewCanvasShareRepository(db)
	if err := shares.GrantRole(ctx, owner, project.ID, editor, model.CanvasRoleEditor); err != nil {
		t.Fatal(err)
	}
	asset := model.Asset{UserID: owner, Kind: "video", StorageID: 1, StorageKey: "clip.mp4"}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	baseline, err := repo.Commit(ctx, owner, project.ID, repository.CanvasCollaborationBatch{
		OpID: "attach-video", Epoch: project.CollaborationEpoch, BaseRevision: project.Revision,
		Changes: []repository.CanvasCollaborationChange{{Kind: "node", ID: "clip", Fields: map[string]json.RawMessage{"data.assetId": json.RawMessage(fmt.Sprintf(`"%d"`, asset.ID))}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	enqueue := &canvasComposeEnqueueStub{}
	svc := service.NewCanvasComposeService(repo, nil, nil, nil).WithEnqueuer(enqueue)
	req := model.CanvasComposeReq{Epoch: project.CollaborationEpoch, Revision: baseline.Revision, OpID: "compose-smoke", Nodes: []string{"clip"}}
	_, err = svc.Submit(ctx, viewer, project.ID, req)
	assertDraftError(t, err, errcode.ErrCanvasNotFound.Code)
	first, err := svc.Submit(ctx, editor, project.ID, req)
	if err != nil || first.Status != "pending" || first.SegmentCount != 1 {
		t.Fatalf("合法成片请求应进入队列: %+v %v", first, err)
	}
	if err := shares.Ready(ctx, owner, project.ID); !errors.Is(err, repository.ErrInUse) {
		t.Fatalf("成员的在途成片应阻止交付: %v", err)
	}
	retry, err := svc.Submit(ctx, editor, project.ID, req)
	if err != nil || first.ID != retry.ID || enqueue.calls != 1 {
		t.Fatalf("同编号重试应复用原任务: %+v %v", retry, err)
	}
	req.Nodes = []string{"another"}
	_, err = svc.Submit(ctx, editor, project.ID, req)
	assertDraftError(t, err, errcode.ErrCanvasConflict.Code)
	jobID, ok := idcodec.Decode(first.ID)
	if !ok {
		t.Fatalf("无效成片任务号: %s", first.ID)
	}
	if _, err := svc.Job(ctx, editor, project.ID, project.CollaborationEpoch, jobID, true); err != nil {
		t.Fatal(err)
	}
	if err := shares.Ready(ctx, owner, project.ID); err != nil {
		t.Fatalf("取消后应允许交付: %v", err)
	}
}

type composeFixture struct {
	db      *gorm.DB
	repo    *repository.CanvasCollaborationRepository
	shares  *repository.CanvasShareRepository
	assets  *service.AssetService
	project *model.CanvasProject
	owner   uint64
	editor  uint64
	request model.CanvasComposeReq
}

func newComposeFixture(t *testing.T) composeFixture {
	t.Helper()
	const payload = `{"nodes":[{"id":"red","type":"canvas","data":{"kind":"video"}},{"id":"blue","type":"canvas","data":{"kind":"video"}}],"edges":[]}`
	db, repo, owner, editor, _, project := newCollaborationFixtureWithPayload(t, payload)
	if err := db.AutoMigrate(&model.Asset{}, &model.CanvasDeliveryStage{}, &model.CanvasComposeJob{}); err != nil {
		t.Fatal(err)
	}
	shares := repository.NewCanvasShareRepository(db)
	if err := shares.GrantRole(t.Context(), owner, project.ID, editor, model.CanvasRoleEditor); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	registry := storage.NewRegistry(canvasImportStorage{dir: dir}, 0)
	assets := service.NewAssetService(repository.NewAssetRepository(db), registry, config.Storage{})
	var changes []repository.CanvasCollaborationChange
	for _, color := range []string{"red", "blue"} {
		asset := seedComposeClip(t, db, owner, dir, color)
		changes = append(changes, repository.CanvasCollaborationChange{Kind: "node", ID: color,
			Fields: map[string]json.RawMessage{"data.assetId": json.RawMessage(fmt.Sprintf(`"%d"`, asset))}})
	}
	seed, err := repo.Commit(t.Context(), owner, project.ID, repository.CanvasCollaborationBatch{
		OpID: "attach-sources", Epoch: project.CollaborationEpoch, BaseRevision: project.Revision, Changes: changes,
	})
	if err != nil || len(seed.Changes) != 2 {
		t.Fatalf("绑定输入素材失败: %+v %v", seed, err)
	}
	return composeFixture{db: db, repo: repo, shares: shares, assets: assets, project: project, owner: owner, editor: editor,
		request: model.CanvasComposeReq{Epoch: project.CollaborationEpoch, Revision: seed.Revision, OpID: "render", Nodes: []string{"blue", "red"}}}
}

func seedComposeClip(t *testing.T, db *gorm.DB, user uint64, dir, color string) uint64 {
	t.Helper()
	name := color + ".mp4"
	cmd := exec.CommandContext(t.Context(), "ffmpeg", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "color="+color+":s=64x64:r=30:d=0.4",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", filepath.Join(dir, name))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("生成视频样本: %v %s", err, output)
	}
	asset := model.Asset{UserID: user, Kind: "video", StorageID: 1, StorageKey: name, MimeType: "video/mp4"}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	return asset.ID
}

func (fixture composeFixture) submit(t *testing.T, composer service.DramaVideoComposer) (*service.CanvasComposeService, uint64) {
	t.Helper()
	svc := service.NewCanvasComposeService(fixture.repo, fixture.shares, fixture.assets, composer).WithEnqueuer(&canvasComposeEnqueueStub{})
	job, err := svc.Submit(t.Context(), fixture.editor, fixture.project.ID, fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	id, ok := idcodec.Decode(job.ID)
	if !ok {
		t.Fatalf("无效任务号: %s", job.ID)
	}
	return svc, id
}

func TestCanvasComposeRender(t *testing.T) {
	composer := videoframe.New(videoframe.Options{Timeout: time.Minute})
	if !composer.Available() {
		t.Skip("需安装 FFmpeg 和 FFprobe")
	}
	fixture := newComposeFixture(t)
	svc := startComposeRuntime(t, fixture, composer)
	job := submitComposeHTTP(t, fixture, svc)
	id, ok := idcodec.Decode(job.ID)
	if !ok {
		t.Fatalf("无效任务号: %s", job.ID)
	}
	ctx := t.Context()
	if terminal := waitComposeResult(t, fixture, svc, id); terminal.Status != model.TaskSucceeded {
		t.Fatalf("队列未成功产出成片: %+v", terminal)
	}
	if err := svc.HandleCanvasComposeAttempt(ctx, id, 2, true); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Job(ctx, fixture.owner, fixture.project.ID, fixture.request.Epoch, id, false); err == nil {
		t.Fatal("其他成员不能读取提交者的成片任务")
	}
	job, err := svc.Job(ctx, fixture.editor, fixture.project.ID, fixture.request.Epoch, id, true)
	if err != nil || job.Status != model.TaskSucceeded || job.ResultNode == "" || job.SegmentCount != 2 {
		t.Fatalf("终态必须与挂载结果一致: %+v %v", job, err)
	}
	assertComposeProvenance(t, fixture, job)
	assertComposePlayback(t, fixture, job)
}

func startComposeRuntime(t *testing.T, fixture composeFixture, composer service.DramaVideoComposer) *service.CanvasComposeService {
	t.Helper()
	ctx := t.Context()
	if err := fixture.db.AutoMigrate(&model.WorkerInstance{}); err != nil {
		t.Fatal(err)
	}
	conn, err := fixture.db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := queue.Migrate(ctx, conn, ""); err != nil {
		t.Fatal(err)
	}
	svc := service.NewCanvasComposeService(fixture.repo, fixture.shares, fixture.assets, composer)
	rt, err := queue.New(queue.Config{DB: conn, Repo: repository.NewQueueRepository(fixture.db), Enabled: true,
		CanvasCompose: svc, FetchPollInterval: 200 * time.Millisecond, StopTimeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	work, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- rt.Run(work) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	return svc.WithEnqueuer(rt)
}

func submitComposeHTTP(t *testing.T, fixture composeFixture, compose *service.CanvasComposeService) *service.CanvasComposeView {
	t.Helper()
	const secret = "test-compose-auth"
	svc := service.NewCanvasCollaborationService(fixture.repo, fixture.shares, nil, true).WithCompose(compose)
	engine := router.New("test", secret, router.Handlers{
		Health: &handler.HealthHandler{}, CanvasShare: &handler.CanvasShareHandler{},
		CanvasCollaboration: handler.NewCanvasCollaborationHandler(svc),
		UserState: func(context.Context, uint64) (*middleware.UserState, error) {
			return &middleware.UserState{Status: model.UserStatusActive, Role: "user", TokenVersion: 1}, nil
		},
	})
	body, err := json.Marshal(fixture.request)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := utils.GenerateToken(uint(fixture.editor), "", 1, secret, "test", 1)
	if err != nil {
		t.Fatal(err)
	}
	route := "/api/v1/canvas/" + idcodec.Encode(fixture.project.ID) + "/collaboration/compose"
	request := httptest.NewRequest(http.MethodPost, route, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	reply := httptest.NewRecorder()
	engine.ServeHTTP(reply, request)
	var result struct{ Data service.CanvasComposeView }
	if err := json.Unmarshal(reply.Body.Bytes(), &result); err != nil || reply.Code != http.StatusOK || result.Data.Status != model.TaskPending {
		t.Fatalf("成片 HTTP 提交失败: %d %s %v", reply.Code, reply.Body.String(), err)
	}
	return &result.Data
}

func waitComposeResult(t *testing.T, fixture composeFixture, svc *service.CanvasComposeService, id uint64) *service.CanvasComposeView {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		job, err := svc.Job(ctx, fixture.editor, fixture.project.ID, fixture.request.Epoch, id, false)
		if err != nil {
			t.Fatal(err)
		}
		if model.IsTerminalStatus(job.Status) {
			return job
		}
		select {
		case <-ctx.Done():
			t.Fatalf("队列等待超时，成片状态为 %s", job.Status)
		case <-ticker.C:
		}
	}
}

func assertComposeProvenance(t *testing.T, fixture composeFixture, job *service.CanvasComposeView) {
	t.Helper()
	canvas, _, err := fixture.repo.Snapshot(t.Context(), fixture.owner, fixture.project.ID)
	if err != nil || canvas.Revision != fixture.request.Revision+1 {
		t.Fatalf("成片只应发布一次: %+v %v", canvas, err)
	}
	var graph struct {
		Edges []struct{ Source, Target string }
	}
	if err := json.Unmarshal(canvas.PayloadJSON, &graph); err != nil {
		t.Fatal(err)
	}
	var sources []string
	for _, edge := range graph.Edges {
		if edge.Target == job.ResultNode {
			sources = append(sources, edge.Source)
		}
	}
	slices.Sort(sources)
	if !slices.Equal(sources, []string{"blue", "red"}) {
		t.Fatalf("成片缺少输入来源: %v", sources)
	}
}

func assertComposePlayback(t *testing.T, fixture composeFixture, job *service.CanvasComposeView) {
	t.Helper()
	assetID, err := strconv.ParseUint(job.ResultAssetID, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	file, err := fixture.assets.Open(t.Context(), fixture.owner, assetID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Body.Close() }()
	name := filepath.Join(t.TempDir(), "rendered.mp4")
	contents, err := io.ReadAll(file.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, sample := range []struct{ offset, color string }{{"0.1", "blue"}, {"0.6", "red"}} {
		pixel, err := exec.CommandContext(t.Context(), "ffmpeg", "-nostdin", "-v", "error", "-ss", sample.offset, "-i", name,
			"-frames:v", "1", "-vf", "scale=1:1", "-pix_fmt", "rgb24", "-f", "rawvideo", "-").Output()
		if err != nil || len(pixel) != 3 || (pixel[0] > pixel[2]) != (sample.color == "red") {
			t.Fatalf("%s 秒应播放 %s 片段: %v %v", sample.offset, sample.color, pixel, err)
		}
	}
}

type composeAction func() error

func (action composeAction) ComposeTimeline(_ context.Context, plan videoframe.ComposePlan, _ string, destination string) error {
	if err := action(); err != nil {
		return err
	}
	data, err := os.ReadFile(plan.Main[0].Path)
	if err != nil {
		return err
	}
	return os.WriteFile(destination, data, 0o600)
}

func TestCanvasComposeStopsWhenAuthorizationOrSourceChanges(t *testing.T) {
	if !videoframe.New(videoframe.Options{}).Available() {
		t.Skip("需安装 FFmpeg 和 FFprobe")
	}
	for _, scenario := range []string{"cancel", "replace-source", "revoke"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := newComposeFixture(t)
			ctx := t.Context()
			var svc *service.CanvasComposeService
			var id uint64
			action := composeAction(func() error {
				return changeComposeStatus(ctx, fixture, svc, id, scenario)
			})
			svc, id = fixture.submit(t, action)
			if err := svc.HandleCanvasComposeAttempt(ctx, id, 1, true); err != nil {
				t.Fatal(err)
			}
			var job model.CanvasComposeJob
			if err := fixture.db.First(&job, id).Error; err != nil {
				t.Fatal(err)
			}
			want := model.TaskFailed
			if scenario == "cancel" {
				want = model.TaskCanceled
			}
			if job.Status != want || job.AssetID != 0 || job.ResultNode != "" {
				t.Fatalf("失效任务必须禁止写回: %+v", job)
			}
			var count int64
			if err := fixture.db.Model(&model.Asset{}).Count(&count).Error; err != nil || count != 2 {
				t.Fatalf("失效任务不能登记成片素材: %d %v", count, err)
			}
		})
	}
}

func changeComposeStatus(ctx context.Context, fixture composeFixture, svc *service.CanvasComposeService, id uint64, scenario string) error {
	switch scenario {
	case "cancel":
		_, err := svc.Job(ctx, fixture.editor, fixture.project.ID, fixture.request.Epoch, id, true)
		return err
	case "revoke":
		return fixture.shares.Remove(ctx, fixture.owner, fixture.project.ID, fixture.editor)
	default:
		result, err := fixture.repo.Commit(ctx, fixture.owner, fixture.project.ID, repository.CanvasCollaborationBatch{
			OpID: "delete-clip", Epoch: fixture.request.Epoch, BaseRevision: fixture.request.Revision,
			Changes: []repository.CanvasCollaborationChange{{Kind: "node", ID: "red", Action: "delete"}},
		})
		if err == nil && len(result.Changes) != 1 {
			return fmt.Errorf("来源变更未生效: %+v", result)
		}
		return err
	}
}
