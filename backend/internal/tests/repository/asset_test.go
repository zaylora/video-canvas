package repository_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"video-canvas/internal/model"
	. "video-canvas/internal/repository"
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

// sha256TestHash 生成每个用例唯一的 64 位十六进制哈希：FindBySha256 不带用户条件，
// 用唯一哈希避免与其他用例 / 历史数据互相污染。
func sha256TestHash(seed uint64) string {
	return fmt.Sprintf("%064x", seed)
}

func TestAssetRepository_FindBySha256(t *testing.T) {
	db := assetRepoTestDB(t)
	r := NewAssetRepository(db)
	ctx := context.Background()

	owner := uint64(time.Now().UnixNano())
	hash := sha256TestHash(owner)
	t.Cleanup(func() { db.Where("user_id = ?", owner).Delete(&model.Asset{}) })

	newAsset := func(key string, size int64, sha string) *model.Asset {
		a := &model.Asset{
			UserID: owner, Kind: "image", StorageKey: fmt.Sprintf("sha-%d/%s", owner, key), MimeType: "image/png",
			ByteSize: size, Source: model.AssetSourceUpload, Sha256: sha,
		}
		if err := r.Create(ctx, a); err != nil {
			t.Fatalf("创建素材失败：%v", err)
		}
		return a
	}
	a := newAsset("a.png", 100, hash)
	b := newAsset("b.png", 100, hash)
	newAsset("c.png", 200, hash)         // 同哈希但大小不同，不应命中
	newAsset("d.png", 100, "")           // 未计算哈希，不应命中
	newAsset("e.png", 100, "f"+hash[1:]) // 其他哈希，不应命中

	t.Run("同哈希同大小按 id 升序返回", func(t *testing.T) {
		got, err := r.FindBySha256(ctx, hash, 100)
		if err != nil {
			t.Fatalf("查询失败：%v", err)
		}
		if len(got) != 2 || got[0].ID != a.ID || got[1].ID != b.ID {
			t.Fatalf("期望 [%d %d]，实际：%+v", a.ID, b.ID, got)
		}
		if got[0].Sha256 != hash {
			t.Fatalf("Sha256 字段未读出：%+v", got[0])
		}
	})

	t.Run("哈希不存在返回空切片", func(t *testing.T) {
		got, err := r.FindBySha256(ctx, sha256TestHash(owner+1), 100)
		if err != nil || got == nil || len(got) != 0 {
			t.Fatalf("期望非 nil 空切片，实际：%#v %v", got, err)
		}
	})

	t.Run("最多返回 5 条", func(t *testing.T) {
		many := sha256TestHash(owner + 2)
		for i := 0; i < 7; i++ {
			newAsset(fmt.Sprintf("m%d.png", i), 7, many)
		}
		got, err := r.FindBySha256(ctx, many, 7)
		if err != nil || len(got) != 5 {
			t.Fatalf("期望 5 条，实际：%d %v", len(got), err)
		}
	})
}

func TestAssetRepository_SetSha256IfEmpty(t *testing.T) {
	db := assetRepoTestDB(t)
	r := NewAssetRepository(db)
	ctx := context.Background()

	owner := uint64(time.Now().UnixNano())
	t.Cleanup(func() { db.Where("user_id = ?", owner).Delete(&model.Asset{}) })

	a := &model.Asset{
		UserID: owner, Kind: "image", StorageKey: fmt.Sprintf("sha-set-%d/a.png", owner), MimeType: "image/png",
		ByteSize: 1, Source: model.AssetSourceUpload,
	}
	if err := r.Create(ctx, a); err != nil {
		t.Fatal(err)
	}
	first, second := sha256TestHash(owner), sha256TestHash(owner+1)

	t.Run("第一次写入生效", func(t *testing.T) {
		if err := r.SetSha256IfEmpty(ctx, a.ID, first); err != nil {
			t.Fatalf("写入失败：%v", err)
		}
		got, err := r.GetByID(ctx, owner, a.ID)
		if err != nil || got.Sha256 != first {
			t.Fatalf("期望 %s，实际：%+v %v", first, got, err)
		}
	})

	t.Run("已有哈希时不覆盖", func(t *testing.T) {
		if err := r.SetSha256IfEmpty(ctx, a.ID, second); err != nil {
			t.Fatalf("重复写入应幂等：%v", err)
		}
		got, err := r.GetByID(ctx, owner, a.ID)
		if err != nil || got.Sha256 != first {
			t.Fatalf("期望仍为 %s，实际：%+v %v", first, got, err)
		}
	})

	t.Run("素材不存在不算错", func(t *testing.T) {
		if err := r.SetSha256IfEmpty(ctx, a.ID+1_000_000_000, first); err != nil {
			t.Fatalf("期望 nil，实际：%v", err)
		}
	})
}
