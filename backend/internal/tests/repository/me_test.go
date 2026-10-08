package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"video-canvas/internal/model"
	. "video-canvas/internal/repository"
)

// 本文件测试个人中心用到的查询：热力图按时区分桶、积分流水页码分页、画布计数、头像反查。需要 TEST_DATABASE_DSN。

func meDB(t *testing.T) *gorm.DB {
	t.Helper()
	return isolatedDB(t, &model.User{}, &model.GenerationTask{}, &model.CreditLedger{}, &model.CanvasProject{})
}

func mkMeTask(t *testing.T, db *gorm.DB, userID uint64, kind string, at time.Time, isTest bool) {
	t.Helper()
	tk := &model.GenerationTask{
		UserID: userID, Kind: kind, ModelKey: "m", Provider: "p", Status: model.TaskSucceeded,
		InputJSON: datatypes.JSON(`{}`), ConfigSnapshot: datatypes.JSON(`{}`), IsTest: isTest,
		NextPollAt: at, DeadlineAt: at.Add(time.Hour), CreatedAt: at,
	}
	if err := db.Create(tk).Error; err != nil {
		t.Fatal(err)
	}
}

func TestUserRepository_ActivityDays(t *testing.T) {
	ctx := context.Background()
	db := meDB(t)
	repo := NewUserRepository(db)
	sh, _ := time.LoadLocation("Asia/Shanghai")

	// UTC 2026-10-08 16:30 = 上海 2026-10-09 00:30，应算到 10-09
	mkMeTask(t, db, 1, "image", time.Date(2026, 10, 8, 16, 30, 0, 0, time.UTC), false)
	// UTC 2026-10-08 15:59 = 上海 10-08 23:59
	mkMeTask(t, db, 1, "video", time.Date(2026, 10, 8, 15, 59, 0, 0, time.UTC), false)
	mkMeTask(t, db, 1, "audio", time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC), false)
	mkMeTask(t, db, 1, "text", time.Date(2026, 10, 8, 11, 0, 0, 0, time.UTC), false)
	// 试跑：不计
	mkMeTask(t, db, 1, "image", time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC), true)
	// 别人的：不计
	mkMeTask(t, db, 2, "image", time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC), false)
	// 区间外：不计
	mkMeTask(t, db, 1, "image", time.Date(2026, 10, 9, 16, 0, 0, 0, time.UTC), false) // 上海 10-10 00:00

	from := time.Date(2026, 10, 1, 0, 0, 0, 0, sh)
	to := time.Date(2026, 10, 10, 0, 0, 0, 0, sh)
	days, err := repo.ActivityDays(ctx, 1, "Asia/Shanghai", from, to)
	if err != nil {
		t.Fatal(err)
	}
	want := []model.ActivityDay{
		{Date: "2026-10-08", Count: 3, Video: 1, Audio: 1, Text: 1},
		{Date: "2026-10-09", Count: 1, Image: 1},
	}
	if len(days) != len(want) {
		t.Fatalf("days = %+v", days)
	}
	for i := range want {
		if days[i] != want[i] {
			t.Fatalf("第 %d 天 = %+v，期望 %+v", i, days[i], want[i])
		}
	}

	// 同样的数据按 UTC 分桶：16:30 那条仍是 10-08
	days, err = repo.ActivityDays(ctx, 1, "UTC", time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 1 || days[0].Date != "2026-10-08" || days[0].Count != 4 {
		t.Fatalf("UTC 分桶 = %+v", days)
	}
}

func TestUserRepository_LedgerPage(t *testing.T) {
	ctx := context.Background()
	db := meDB(t)
	repo := NewUserRepository(db)
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	op := uint64(9)
	rows := []model.CreditLedger{
		{UserID: 1, Type: model.LedgerInitial, Amount: 50, CreatedAt: base},
		{UserID: 1, Type: model.LedgerFreeze, Amount: 10, TaskID: model.TaskIDPtr(1), CreatedAt: base.Add(time.Hour)},
		{UserID: 1, Type: model.LedgerSettle, Amount: 10, TaskID: model.TaskIDPtr(1), CreatedAt: base.Add(time.Hour)}, // 与上一条同一时间戳
		{UserID: 1, Type: model.LedgerAdminAdjust, Amount: -3, OperatorID: &op, Note: "国庆活动补发", CreatedAt: base.Add(2 * time.Hour)},
		{UserID: 2, Type: model.LedgerAdminAdjust, Amount: 100, OperatorID: &op, Note: "别人的", CreatedAt: base.Add(3 * time.Hour)},
	}
	for i := range rows {
		if err := db.Create(&rows[i]).Error; err != nil {
			t.Fatal(err)
		}
	}

	t.Run("全部：按 created_at DESC, id DESC，只看本人，带 note", func(t *testing.T) {
		items, err := repo.ListLedgerPage(ctx, LedgerPageFilter{UserID: 1, Offset: 0, Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 4 || items[0].Note != "国庆活动补发" || items[0].Amount != -3 ||
			items[1].ID != rows[2].ID || items[2].ID != rows[1].ID || items[3].Type != model.LedgerInitial {
			t.Fatalf("%+v", items)
		}
		if items[1].TaskID == nil || *items[1].TaskID != 1 {
			t.Fatalf("task_id 不对：%+v", items[1])
		}
		n, err := repo.CountLedger(ctx, 1, nil)
		if err != nil || n != 4 {
			t.Fatalf("count = %d %v", n, err)
		}
	})
	t.Run("分页不重复不遗漏（同一时间戳按 id 排）", func(t *testing.T) {
		seen := map[uint64]bool{}
		for off := 0; off < 4; off += 2 {
			items, _ := repo.ListLedgerPage(ctx, LedgerPageFilter{UserID: 1, Offset: off, Limit: 2})
			for _, it := range items {
				if seen[it.ID] {
					t.Fatalf("重复：%d", it.ID)
				}
				seen[it.ID] = true
			}
		}
		if len(seen) != 4 {
			t.Fatalf("遗漏：%v", seen)
		}
	})
	t.Run("按类型筛选 + 超出范围返回空", func(t *testing.T) {
		types := []string{model.LedgerFreeze, model.LedgerSettle, model.LedgerRefund}
		items, _ := repo.ListLedgerPage(ctx, LedgerPageFilter{UserID: 1, Types: types, Limit: 10})
		n, _ := repo.CountLedger(ctx, 1, types)
		if len(items) != 2 || n != 2 {
			t.Fatalf("%+v %d", items, n)
		}
		items, _ = repo.ListLedgerPage(ctx, LedgerPageFilter{UserID: 1, Offset: 100, Limit: 10})
		if len(items) != 0 {
			t.Fatalf("%+v", items)
		}
	})
}

func TestCanvasProjectRepository_CountByUser(t *testing.T) {
	ctx := context.Background()
	db := meDB(t)
	repo := NewCanvasProjectRepository(db)
	for _, uid := range []uint64{1, 1, 1, 2} {
		if err := repo.Create(ctx, &model.CanvasProject{UserID: uid, PayloadJSON: datatypes.JSON(`{}`)}); err != nil {
			t.Fatal(err)
		}
	}
	var first model.CanvasProject
	db.Where("user_id = 1").First(&first)
	if err := repo.Delete(ctx, 1, first.ID); err != nil {
		t.Fatal(err)
	}
	n, err := repo.CountByUser(ctx, 1)
	if err != nil || n != 2 {
		t.Fatalf("未删除的画布数 = %d %v", n, err)
	}
}

func TestUserRepository_AvatarStorageID(t *testing.T) {
	ctx := context.Background()
	db := meDB(t)
	repo := NewUserRepository(db)
	u := &model.User{Username: "a", Password: "x", AvatarKey: "avatars/1/aaaaaaaaaaaaaaaa.webp", AvatarStorageID: 7}
	if err := db.Create(u).Error; err != nil {
		t.Fatal(err)
	}
	id, err := repo.AvatarStorageID(ctx, "avatars/1/aaaaaaaaaaaaaaaa.webp")
	if err != nil || id != 7 {
		t.Fatalf("%d %v", id, err)
	}
	if _, err := repo.AvatarStorageID(ctx, "avatars/1/bbbbbbbbbbbbbbbb.webp"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("应返回 ErrNotFound：%v", err)
	}
	// 空 key 不能匹配到“没有头像”的用户
	_ = db.Create(&model.User{Username: "b", Password: "x"}).Error
	if _, err := repo.AvatarStorageID(ctx, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("空 key 应返回 ErrNotFound：%v", err)
	}
}

func TestMigrate_MeColumnsAndIndex(t *testing.T) {
	db := meDB(t)
	if !db.Migrator().HasColumn(&model.User{}, "avatar_key") {
		t.Fatal("users 缺 avatar_key")
	}
	if !db.Migrator().HasIndex(&model.GenerationTask{}, "idx_task_user_created") {
		t.Fatal("generation_tasks 缺 idx_task_user_created")
	}
}
