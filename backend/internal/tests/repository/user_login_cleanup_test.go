package repository_test

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"video-canvas/internal/model"
	. "video-canvas/internal/repository"
)

// mkLoginLogs 批量插入 n 条登录记录，created_at 统一设为 at，返回它们的 id。
func mkLoginLogs(t *testing.T, db *gorm.DB, userID uint64, n int, at time.Time) []uint64 {
	t.Helper()
	logs := make([]model.UserLoginLog, n)
	for i := range logs {
		logs[i] = model.UserLoginLog{UserID: userID, Kind: model.LoginKindLogin, Result: model.LoginResultOK, CreatedAt: at}
	}
	if err := db.CreateInBatches(&logs, 500).Error; err != nil {
		t.Fatalf("造登录记录失败：%v", err)
	}
	ids := make([]uint64, n)
	for i, l := range logs {
		ids[i] = l.ID
	}
	return ids
}

func countLoginLogs(t *testing.T, db *gorm.DB, where string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := db.Model(&model.UserLoginLog{}).Where(where, args...).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func TestUserRepository_DeleteLoginLogsBefore(t *testing.T) {
	ctx := context.Background()
	db := userDB(t)
	repo := NewUserRepository(db)
	now := time.Now()
	cutoff := now.AddDate(0, 0, -180)

	mkLoginLogs(t, db, 1, 5, now.AddDate(0, 0, -181)) // 过期
	mkLoginLogs(t, db, 2, 4, now.AddDate(0, 0, -179)) // 未过期
	mkLoginLogs(t, db, 0, 2, now.AddDate(0, 0, -400)) // 过期（用户不存在的失败记录也要清）

	t.Run("每批最多删 limit 行，只删早于截止时间的", func(t *testing.T) {
		n, err := repo.DeleteLoginLogsBefore(ctx, cutoff, 3)
		if err != nil || n != 3 {
			t.Fatalf("第一批应删 3 行：%d %v", n, err)
		}
		if left := countLoginLogs(t, db, "created_at < ?", cutoff); left != 4 {
			t.Fatalf("还应剩 4 行过期记录：%d", left)
		}
	})
	t.Run("再删到没有为止；新记录一条不动；可重复执行", func(t *testing.T) {
		n, err := repo.DeleteLoginLogsBefore(ctx, cutoff, 1000)
		if err != nil || n != 4 {
			t.Fatalf("应删光剩下的 4 行：%d %v", n, err)
		}
		if n, err := repo.DeleteLoginLogsBefore(ctx, cutoff, 1000); err != nil || n != 0 {
			t.Fatalf("重复执行应无事发生：%d %v", n, err)
		}
		if kept := countLoginLogs(t, db, "created_at >= ?", cutoff); kept != 4 {
			t.Fatalf("179 天前的 4 条必须保留：%d", kept)
		}
	})
}
