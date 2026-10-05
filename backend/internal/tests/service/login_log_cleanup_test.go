package service_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"video-canvas/internal/model"
	"video-canvas/internal/repository"
	. "video-canvas/internal/service"
)

// 本文件测试登录记录清理：分批、保留天数、错误处理（fake），以及真实 PostgreSQL 上只删旧的、>1000 行分批正确、可重复执行。

// fakePurger 记录每次调用的截止时间与批量大小；每次删 min(remaining, limit) 行。
type fakePurger struct {
	mu        sync.Mutex
	remaining int64
	cutoffs   []time.Time
	limits    []int
	failAt    int // 第几次调用返回错误（从 1 开始），0 不失败
	calls     int
}

func (f *fakePurger) DeleteLoginLogsBefore(_ context.Context, before time.Time, limit int) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.cutoffs = append(f.cutoffs, before)
	f.limits = append(f.limits, limit)
	if f.failAt != 0 && f.calls == f.failAt {
		return 0, errBoom
	}
	n := min(f.remaining, int64(limit))
	f.remaining -= n
	return n, nil
}

func TestLoginLogCleaner_Cleanup_Fake(t *testing.T) {
	ctx := context.Background()

	t.Run("分批：每批 <= 1000，删到不足一批为止", func(t *testing.T) {
		p := &fakePurger{remaining: 2500}
		n, err := NewLoginLogCleaner(p).Cleanup(ctx)
		if err != nil || n != 2500 {
			t.Fatalf("%d %v", n, err)
		}
		if len(p.limits) != 3 {
			t.Fatalf("2500 行应分 3 批：%v", p.limits)
		}
		for _, l := range p.limits {
			if l != LoginLogCleanupBatch || l > 1000 {
				t.Fatalf("每批不能超过 1000：%v", p.limits)
			}
		}
	})

	t.Run("恰好整批：多查一次确认没有剩余", func(t *testing.T) {
		p := &fakePurger{remaining: 1000}
		n, err := NewLoginLogCleaner(p).Cleanup(ctx)
		if err != nil || n != 1000 || len(p.limits) != 2 {
			t.Fatalf("%d %v %v", n, err, p.limits)
		}
	})

	t.Run("没有可删记录：只查一次", func(t *testing.T) {
		p := &fakePurger{}
		n, err := NewLoginLogCleaner(p).Cleanup(ctx)
		if err != nil || n != 0 || len(p.limits) != 1 {
			t.Fatalf("%d %v %v", n, err, p.limits)
		}
	})

	t.Run("保留天数固定为 180 天：截止时间 = 现在 - 180 天", func(t *testing.T) {
		if DefaultLoginLogRetentionDays != 180 {
			t.Fatalf("保留天数常量应为 180：%d", DefaultLoginLogRetentionDays)
		}
		p := &fakePurger{}
		if _, err := NewLoginLogCleaner(p).Cleanup(ctx); err != nil {
			t.Fatal(err)
		}
		want := time.Now().AddDate(0, 0, -180)
		if d := p.cutoffs[0].Sub(want); d < -time.Minute || d > time.Minute {
			t.Fatalf("截止时间 %v 与期望 %v 差太多", p.cutoffs[0], want)
		}
	})

	t.Run("某批失败：返回已删数量与错误，不 panic", func(t *testing.T) {
		p := &fakePurger{remaining: 2500, failAt: 2}
		n, err := NewLoginLogCleaner(p).Cleanup(ctx)
		if err == nil || n != 1000 {
			t.Fatalf("第 2 批失败时应已删 1000 行：%d %v", n, err)
		}
	})

	t.Run("ctx 取消：立即停止", func(t *testing.T) {
		p := &fakePurger{remaining: 5000}
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := NewLoginLogCleaner(p).Cleanup(cctx); err == nil {
			t.Fatal("取消后应返回错误")
		}
		if p.calls != 0 {
			t.Fatalf("取消后不应再删：%d", p.calls)
		}
	})
}

// countingPurger 包装真实仓储，记录每批的 limit。
type countingPurger struct {
	*repository.UserRepository
	limits []int
}

func (c *countingPurger) DeleteLoginLogsBefore(ctx context.Context, before time.Time, limit int) (int64, error) {
	c.limits = append(c.limits, limit)
	return c.UserRepository.DeleteLoginLogsBefore(ctx, before, limit)
}

func insertLoginLogs(t *testing.T, db *gorm.DB, n int, at time.Time) {
	t.Helper()
	logs := make([]model.UserLoginLog, n)
	for i := range logs {
		logs[i] = model.UserLoginLog{UserID: 1, Kind: model.LoginKindLogin, Result: model.LoginResultOK, CreatedAt: at}
	}
	if err := db.CreateInBatches(&logs, 500).Error; err != nil {
		t.Fatal(err)
	}
}

func TestLoginLogCleaner_Cleanup_Integration(t *testing.T) {
	ctx := context.Background()
	db := gtiTestDB(t)
	if err := db.AutoMigrate(&model.UserLoginLog{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	insertLoginLogs(t, db, 2300, now.AddDate(0, 0, -181)) // 过期：> 2 批
	insertLoginLogs(t, db, 7, now.AddDate(0, 0, -179))    // 未过期
	insertLoginLogs(t, db, 3, now)                        // 刚产生

	p := &countingPurger{UserRepository: repository.NewUserRepository(db)}
	c := NewLoginLogCleaner(p)
	n, err := c.Cleanup(ctx)
	if err != nil || n != 2300 {
		t.Fatalf("应删 2300 行：%d %v", n, err)
	}
	if len(p.limits) != 3 {
		t.Fatalf("2300 行应分 3 批（1000+1000+300）：%v", p.limits)
	}
	var left, old int64
	db.Model(&model.UserLoginLog{}).Count(&left)
	db.Model(&model.UserLoginLog{}).Where("created_at < ?", now.AddDate(0, 0, -180)).Count(&old)
	if left != 10 || old != 0 {
		t.Fatalf("应只剩 10 条未过期记录：left=%d old=%d", left, old)
	}
	// 可重复执行
	if n, err := c.Cleanup(ctx); err != nil || n != 0 {
		t.Fatalf("重复执行应无事发生：%d %v", n, err)
	}
}
