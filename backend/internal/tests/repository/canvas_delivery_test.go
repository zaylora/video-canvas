package repository_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

type canvasTestAssets struct {
	deliveryAssets
	posterReads int
}

func (a *canvasTestAssets) OpenCanvasPoster(ctx context.Context, user, id uint64) (*provider.AssetFile, error) {
	row, err := a.Get(ctx, user, id)
	if err != nil {
		return nil, err
	}
	a.posterReads++
	row.MimeType, row.ByteSize = "image/jpeg", 6
	return &provider.AssetFile{Asset: row, Body: io.NopCloser(strings.NewReader("poster"))}, nil
}

func (a *canvasTestAssets) StageCanvasAsset(ctx context.Context, src *model.Asset, user uint64, record func(uint64, string) error) (*model.Asset, error) {
	key := "stage-" + rand.Text()
	if err := record(1, key); err != nil {
		return nil, err
	}
	target, err := a.CopyBySha256(ctx, user, fmt.Sprint(src.ID), 1)
	if err != nil {
		return nil, err
	}
	target.StorageKey = key
	return target, nil
}
func (a *canvasTestAssets) Open(ctx context.Context, user, id uint64) (*provider.AssetFile, error) {
	row, err := a.Get(ctx, user, id)
	if err != nil {
		return nil, err
	}
	row.MimeType = "image/png"
	row.ByteSize = 6
	return &provider.AssetFile{Asset: row, Body: io.NopCloser(strings.NewReader("sample"))}, nil
}
func canvasSeed(t *testing.T) (*gorm.DB, *service.CanvasShareService, *canvasTestAssets) {
	t.Helper()
	db := isolatedDB(t, &model.User{}, &model.CanvasProject{}, &model.CanvasViewer{}, &model.CanvasDelivery{}, &model.CanvasDeliveryStage{}, &model.Asset{}, &model.GenerationTask{}, &model.GenerationTaskEvent{}, &model.StoryboardSplitJob{})
	graph := `{"nodes":[{"id":"n","type":"canvas","data":{"kind":"image","assetId":"10","src":"/files/source","outputs":[{"id":"10","assetId":"10","src":"/files/source"},{"id":"11","assetId":"11","src":"/files/older"}],"activeOutputId":"10","params":{"images":["11"]}}}],"edges":[]}`
	for _, row := range []any{&model.User{Username: "recipient", Email: "recv@test.example"}, &model.CanvasProject{BaseModel: model.BaseModel{ID: 8}, UserID: 42, Title: "test", CoverAssetID: 10, PayloadJSON: []byte(graph)}} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	assets := &canvasTestAssets{}
	return db, service.NewCanvasShareService(repository.NewCanvasShareRepository(db), assets, nil), assets
}
func TestCanvasShareReadAndRevoke(t *testing.T) {
	_, svc, _ := canvasSeed(t)
	ctx := context.Background()
	req := model.CanvasShareReq{Mode: "view", Email: "recv@test.example"}
	for range 2 {
		if err := svc.Share(ctx, 42, 8, req); err != nil {
			t.Fatal(err)
		}
	}
	items, total, err := svc.Shared(ctx, 1, "test", 1, 10)
	if err != nil || total != 1 || items[0].CoverAsset != idcodec.Encode(10) {
		t.Fatalf("共享列表或封面不符: %v %v", items, err)
	}
	view, err := svc.Read(ctx, 1, 8)
	if err != nil || bytes.Contains(view.PayloadJSON, []byte("/files/")) {
		t.Fatalf("共享视图: %v", err)
	}
	file, err := svc.Media(ctx, 1, 8, 10)
	if err != nil {
		t.Fatal(err)
	}
	file.Body.Close()
	if _, err := svc.Media(ctx, 1, 8, 99); err == nil {
		t.Fatal("范围外素材可访问")
	}
	if _, err := svc.Read(ctx, 2, 8); err == nil {
		t.Fatal("无授权仍可读")
	}
	if err := svc.Remove(ctx, 1, 8, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Media(ctx, 1, 8, 10); err == nil {
		t.Fatal("退出后可打开素材")
	}
}
func TestCanvasShareDeliveryReplay(t *testing.T) {
	for _, mode := range []string{"copy", "transfer"} {
		t.Run(mode, func(t *testing.T) {
			db, svc, assets := canvasSeed(t)
			ctx := context.Background()
			req := model.CanvasShareReq{Mode: "view", Email: "recv@test.example"}
			if err := svc.Share(ctx, 42, 8, req); err != nil {
				t.Fatal(err)
			}
			req.Mode = mode
			req.Revision = 1
			req.RequestKey = "delivery"
			for range 2 {
				if err := svc.Share(ctx, 42, 8, req); err != nil {
					t.Fatal(err)
				}
			}
			var targets []model.CanvasProject
			if err := db.Where("user_id=1").Find(&targets).Error; err != nil || len(targets) != 1 {
				t.Fatalf("重复交付: %d %v", len(targets), err)
			}
			next := targets[0]
			if next.CoverAssetID == 10 || next.Revision != 1 || assets.copies != 2 || bytes.Contains(next.PayloadJSON, []byte("/files/source")) || !bytes.Contains(next.PayloadJSON, []byte(`"outputs"`)) {
				t.Fatalf("目标素材引用不符: %s", next.PayloadJSON)
			}
			_, err := svc.Read(ctx, 1, 8)
			if (err == nil) != (mode == "copy") {
				t.Fatalf("原授权结果不符: %v", err)
			}
			req.Revision = 2
			if err := svc.Share(ctx, 42, 8, req); err == nil {
				t.Fatal("不同版本重用了请求键")
			}
			verifyCanvasDeliveryOwner(t, db, svc, next.ID, mode)
		})
	}
}

func verifyCanvasDeliveryOwner(t *testing.T, db *gorm.DB, svc *service.CanvasShareService, targetID uint64, mode string) {
	t.Helper()
	ctx := context.Background()
	projects := repository.NewCanvasProjectRepository(db)
	items, total, err := projects.List(ctx, 1, "", 0, 20)
	if err != nil || total != 1 || len(items) != 1 || items[0].ID != targetID {
		t.Fatalf("接收作品应出现在个人列表: %v, %d, %v", items, total, err)
	}
	shared, _, err := svc.Shared(ctx, 1, "", 1, 20)
	if err != nil || (mode == "copy" && len(shared) != 1) || (mode == "transfer" && len(shared) != 0) {
		t.Fatalf("共享列表只应保留有效查看授权: %v, %v", shared, err)
	}
	if _, err = svc.Read(ctx, 1, targetID); err == nil {
		t.Fatal("交付作品不应从共享读页打开")
	}
	projectService := service.NewCanvasProjectService(projects)
	title := "由接收方继续编辑"
	update := &model.UpdateCanvasProjectReq{Revision: 1, Title: &title}
	if saved, saveErr := projectService.Update(ctx, 1, targetID, update); saveErr != nil || saved.Title != title || saved.Revision != 2 {
		t.Fatalf("接收方应能用标准保存接口编辑: %+v, %v", saved, saveErr)
	}
	if _, err = projectService.Update(ctx, 1, 8, update); err == nil {
		t.Fatal("仅查看或已退役的原作不应被接收方写入")
	}
	tasks := repository.NewGenerationTaskRepository(db)
	ownedTask := gtTask(1, model.TaskPending, 1, time.Now())
	ownedTask.CanvasProjectID, ownedTask.NodeID = &targetID, "n"
	if inserted, taskErr := tasks.InsertTask(ctx, ownedTask); taskErr != nil || !inserted {
		t.Fatalf("接收方的生成任务应能挂到交付画布: %t, %v", inserted, taskErr)
	}
	foreignTask := gtTask(42, model.TaskPending, 1, time.Now())
	foreignTask.CanvasProjectID, foreignTask.NodeID = &targetID, "n"
	if inserted, taskErr := tasks.InsertTask(ctx, foreignTask); inserted || !errors.Is(taskErr, repository.ErrNotFound) {
		t.Fatalf("发送方不能给另一个用户的交付画布创建任务: %t, %v", inserted, taskErr)
	}
}
func TestCanvasShareFailedTransferRetainsOriginal(t *testing.T) {
	db, svc, assets := canvasSeed(t)
	ctx := context.Background()
	req := model.CanvasShareReq{Mode: "transfer", Revision: 1, RequestKey: "retry", Email: "recv@test.example"}
	assets.fail = true
	if err := svc.Share(ctx, 42, 8, req); err == nil {
		t.Fatal("存储失败仍成功")
	}
	if assets.discarded != 1 {
		t.Fatal("未回收独立对象")
	}
	var count int64
	if err := db.Model(&model.CanvasProject{}).Where("user_id=1").Count(&count).Error; err != nil || count != 0 {
		t.Fatal(count, err)
	}
	assets.fail = false
	if err := svc.Share(ctx, 42, 8, req); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewCanvasShareRepository(db)
	if _, err := repo.Source(ctx, 42, 8); !errors.Is(err, repository.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestCanvasShareConcurrentTransfer(t *testing.T) {
	db, svc, _ := canvasSeed(t)
	ctx := context.Background()
	secondRecipient := model.User{Username: "other", Email: "other@test.example"}
	if err := db.Create(&secondRecipient).Error; err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	for _, email := range []string{"recv@test.example", secondRecipient.Email} {
		go func() {
			results <- svc.Share(ctx, 42, 8, model.CanvasShareReq{Mode: "transfer", Revision: 1, RequestKey: "send-" + email, Email: email})
		}()
	}
	first, second := <-results, <-results
	if (first == nil) == (second == nil) {
		t.Fatalf("并发转移结果 %v / %v", first, second)
	}
	var count int64
	if err := db.Model(&model.CanvasProject{}).Where("user_id IN ?", []uint64{1, secondRecipient.ID}).Count(&count).Error; err != nil || count != 1 {
		t.Fatal(count, err)
	}
}

func TestCanvasDeliveryMissingNodeAssetAlsoUsedAsCover(t *testing.T) {
	db, svc, _ := canvasSeed(t)
	missing := `{"nodes":[{"type":"canvas","data":{"kind":"image","assetId":"99","src":"/files/unavailable"}}]}`
	if err := db.Model(&model.CanvasProject{}).Where("id = ?", 8).Updates(map[string]any{"cover_asset_id": 99, "payload_json": missing}).Error; err != nil {
		t.Fatal(err)
	}
	err := svc.Share(context.Background(), 42, 8, model.CanvasShareReq{Mode: "transfer", Revision: 1, RequestKey: "required", Email: "recv@test.example"})
	if err == nil {
		t.Fatal("缺失节点素材的转移应该失败")
	}
	if _, err := repository.NewCanvasShareRepository(db).Source(context.Background(), 42, 8); err != nil {
		t.Fatal(err)
	}
}

func TestCanvasDeliveryOnlyMissingCover(t *testing.T) {
	for _, mode := range []string{"copy", "transfer"} {
		t.Run(mode, func(t *testing.T) {
			db, svc, _ := canvasSeed(t)
			if err := db.Model(&model.CanvasProject{}).Where("id = ?", 8).Update("cover_asset_id", 99).Error; err != nil {
				t.Fatal(err)
			}
			err := svc.Share(context.Background(), 42, 8, model.CanvasShareReq{Mode: mode, Revision: 1, RequestKey: "no-cover", Email: "recv@test.example"})
			if err != nil {
				t.Fatal(err)
			}
			var target model.CanvasProject
			if err := db.Where("user_id = ?", 1).Take(&target).Error; err != nil || target.CoverAssetID != 0 {
				t.Fatal(target.CoverAssetID, err)
			}
		})
	}
}

func TestCanvasDeliveryReceiptSurvivesRetirement(t *testing.T) {
	_, svc, _ := canvasSeed(t)
	ctx := context.Background()
	req := model.CanvasShareReq{Mode: "transfer", Revision: 1, RequestKey: "lost-response", Email: "recv@test.example"}
	if err := svc.Share(ctx, 42, 8, req); err != nil {
		t.Fatal(err)
	}
	if status, err := svc.Receipt(ctx, 42, 8, req.RequestKey); err != nil || status != "complete" {
		t.Fatal(status, err)
	}
	for _, scope := range [][2]uint64{{1, 8}, {42, 9}} {
		if _, err := svc.Receipt(ctx, scope[0], scope[1], req.RequestKey); err == nil {
			t.Fatal("读到了其他用户或画布的交付结果")
		}
	}
}
