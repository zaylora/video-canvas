package repository_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
	. "video-canvas/internal/repository"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"video-canvas/internal/model"
)

// assetRepoTestDB 连接专用测试库并迁移 assets 表；未设置 TEST_DATABASE_DSN 时跳过。
func assetRepoTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("未设置 TEST_DATABASE_DSN，跳过仓储集成测试")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		t.Fatalf("连接测试库失败：%v", err)
	}
	if err := db.AutoMigrate(&model.Asset{}); err != nil {
		t.Fatalf("迁移失败：%v", err)
	}
	return db
}

func TestAssetRepository_CreateAndGetByID(t *testing.T) {
	db := assetRepoTestDB(t)
	r := NewAssetRepository(db)
	ctx := context.Background()

	// 每个用例用唯一的 user_id，避免与其他用例 / 历史数据互相污染
	owner := uint64(time.Now().UnixNano())
	other := owner + 1
	taskID := uint64(42)
	a := &model.Asset{
		UserID: owner, Kind: "video", StorageKey: "u1/202609/x.mp4", MimeType: "video/mp4",
		ByteSize: 1234, Width: 640, Height: 360, DurationMs: 2500,
		Source: model.AssetSourceGenerated, TaskID: &taskID, FileName: "x.mp4",
	}
	t.Cleanup(func() { db.Where("user_id IN ?", []uint64{owner, other}).Delete(&model.Asset{}) })

	t.Run("创建后回填自增 id 与创建时间", func(t *testing.T) {
		if err := r.Create(ctx, a); err != nil {
			t.Fatalf("创建失败：%v", err)
		}
		if a.ID == 0 || a.CreatedAt.IsZero() {
			t.Fatalf("id 或创建时间未回填：%+v", a)
		}
	})

	t.Run("本人可以按 id 查到，字段完整", func(t *testing.T) {
		got, err := r.GetByID(ctx, owner, a.ID)
		if err != nil {
			t.Fatalf("查询失败：%v", err)
		}
		if got.StorageKey != a.StorageKey || got.Kind != "video" || got.Width != 640 || got.Height != 360 ||
			got.DurationMs != 2500 || got.ByteSize != 1234 || got.TaskID == nil || *got.TaskID != 42 || got.FileName != "x.mp4" {
			t.Fatalf("字段不一致：%+v", got)
		}
	})

	t.Run("他人查询返回 ErrNotFound", func(t *testing.T) {
		if _, err := r.GetByID(ctx, other, a.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("期望 ErrNotFound，实际：%v", err)
		}
	})

	t.Run("id 不存在返回 ErrNotFound", func(t *testing.T) {
		if _, err := r.GetByID(ctx, owner, a.ID+1_000_000_000); !errors.Is(err, ErrNotFound) {
			t.Fatalf("期望 ErrNotFound，实际：%v", err)
		}
	})

	t.Run("上传素材 task_id 为空", func(t *testing.T) {
		up := &model.Asset{UserID: owner, Kind: "image", StorageKey: "u1/202609/y.png", MimeType: "image/png", Source: model.AssetSourceUpload}
		if err := r.Create(ctx, up); err != nil {
			t.Fatal(err)
		}
		got, err := r.GetByID(ctx, owner, up.ID)
		if err != nil || got.TaskID != nil || got.Source != model.AssetSourceUpload {
			t.Fatalf("实际：%+v %v", got, err)
		}
	})
}
