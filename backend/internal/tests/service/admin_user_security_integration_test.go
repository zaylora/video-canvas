package service_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"video-canvas/internal/cache"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
	. "video-canvas/internal/service"
)

// 本文件用真实 PostgreSQL（与 Redis）验证角色调整、重置密码的并发安全与缓存失效。需要 TEST_DATABASE_DSN，未设置时跳过。

// slowRoleRepo 在“统计完 super_admin 数量之后”人为停顿一小会儿，把“统计 → 降级”的竞态窗口撑大。
// 有咨询锁时别的改角色事务卡在 LockRoleChange 上排队，不受影响；没有锁时两个事务都会读到同一个过期的数量，竞态必现，测试才有变异检测力。
type slowRoleRepo struct {
	*repository.UserRepository
	delay time.Duration
}

func (s slowRoleRepo) WithTx(ctx context.Context, fn func(tx repository.UserTx) error) error {
	return s.UserRepository.WithTx(ctx, func(tx repository.UserTx) error {
		return fn(slowRoleTx{UserTx: tx, delay: s.delay})
	})
}

type slowRoleTx struct {
	repository.UserTx
	delay time.Duration
}

func (s slowRoleTx) CountByRole(ctx context.Context, role string) (int64, error) {
	n, err := s.UserTx.CountByRole(ctx, role)
	time.Sleep(s.delay)
	return n, err
}

func mkDBUser(t *testing.T, db *gorm.DB, name, role string) *model.User {
	t.Helper()
	u := &model.User{Username: name, Password: "x", Role: role, Status: model.UserStatusActive}
	if err := db.Create(u).Error; err != nil {
		t.Fatal(err)
	}
	return u
}

func newSecuritySvc(t *testing.T, db *gorm.DB, repo AdminUserRepo, users UserInvalidator) *AdminUserService {
	t.Helper()
	if err := db.AutoMigrate(&model.User{}, &model.AdminAuditLog{}); err != nil {
		t.Fatal(err)
	}
	return NewAdminUserService(repo, repository.NewAdminAuditRepository(db), nil, WithAdminControls(AdminControls{Users: users}))
}

func countSupers(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var n int64
	if err := db.Model(&model.User{}).Where("role = ?", model.RoleSuperAdmin).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func TestAdminUser_SetRole_Integration_ConcurrentDemoteKeepsOneSuperAdmin(t *testing.T) {
	ctx := context.Background()
	db := gtiTestDB(t)
	svc := newSecuritySvc(t, db, slowRoleRepo{UserRepository: repository.NewUserRepository(db), delay: 50 * time.Millisecond}, nil)

	t.Run("两个超管同时互相降级：只能成功一个，至少剩一个", func(t *testing.T) {
		for round := range 5 {
			a := mkDBUser(t, db, fmt.Sprintf("a%d", round), model.RoleSuperAdmin)
			b := mkDBUser(t, db, fmt.Sprintf("b%d", round), model.RoleSuperAdmin)
			// 先把其他轮留下的超管降成 user，保证本轮库里恰好 2 个超管
			db.Model(&model.User{}).Where("role = ? AND id NOT IN ?", model.RoleSuperAdmin, []uint64{a.ID, b.ID}).Update("role", model.RoleUser)

			var wg sync.WaitGroup
			errs := make([]error, 2)
			start := make(chan struct{})
			for i, pair := range [][2]uint64{{a.ID, b.ID}, {b.ID, a.ID}} {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					errs[i] = svc.SetRole(ctx, pair[0], pair[1], model.RoleAdmin)
				}()
			}
			close(start)
			wg.Wait()

			if n := countSupers(t, db); n < 1 {
				t.Fatalf("第 %d 轮：超管被降光了（%d 个）；errs=%v", round, n, errs)
			}
			ok := 0
			for _, err := range errs {
				if err == nil {
					ok++
				} else {
					wantBizErr(t, err, errcode.ErrForbidden)
				}
			}
			if ok != 1 {
				t.Fatalf("第 %d 轮：应恰好成功 1 个：%v", round, errs)
			}
		}
	})

	t.Run("多个超管环形互相降级：至少剩一个", func(t *testing.T) {
		db.Model(&model.User{}).Where("role = ?", model.RoleSuperAdmin).Update("role", model.RoleUser)
		const n = 4
		ids := make([]uint64, n)
		for i := range ids {
			ids[i] = mkDBUser(t, db, fmt.Sprintf("ring%d", i), model.RoleSuperAdmin).ID
		}
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := range ids {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_ = svc.SetRole(ctx, ids[i], ids[(i+1)%n], model.RoleUser)
			}()
		}
		close(start)
		wg.Wait()
		if got := countSupers(t, db); got < 1 {
			t.Fatalf("超管被降光了：%d", got)
		}
	})
}

// redisForTest 连接测试 Redis；未设置 TEST_REDIS_ADDR 时跳过。
func redisForTest(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("未设置 TEST_REDIS_ADDR，跳过需要 Redis 的集成测试")
	}
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("连接测试 Redis 失败：%v", err)
	}
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb
}

// 真实 Redis 缓存 + 真实库：状态缓存被预热后，改角色 / 重置密码必须立刻让 State（RequireActive / RequireAdmin 用的）读到新值。
func TestAdminUser_Security_Integration_CacheInvalidated(t *testing.T) {
	ctx := context.Background()
	db := gtiTestDB(t)
	rdb := redisForTest(t)
	repo := repository.NewUserRepository(db)
	userSvc := NewUserService(UserDeps{Repo: repo, Cache: cache.NewUserCache(rdb)})
	svc := newSecuritySvc(t, db, repo, userSvc)

	root := mkDBUser(t, db, "root", model.RoleSuperAdmin)
	tom := mkDBUser(t, db, "tom", model.RoleUser)
	rdb.Del(ctx, fmt.Sprintf("user:%d", tom.ID))
	t.Cleanup(func() { rdb.Del(ctx, fmt.Sprintf("user:%d", tom.ID)) })

	warm, err := userSvc.State(ctx, tom.ID) // 预热缓存
	if err != nil || warm.Role != model.RoleUser || warm.TokenVersion != 0 {
		t.Fatalf("%v %+v", err, warm)
	}

	if err := svc.SetRole(ctx, root.ID, tom.ID, model.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if st, _ := userSvc.State(ctx, tom.ID); st.Role != model.RoleAdmin {
		t.Fatalf("改角色后缓存应已失效，读到 %q", st.Role)
	}

	if _, err := svc.ResetPassword(ctx, root.ID, tom.ID, ""); err != nil {
		t.Fatal(err)
	}
	if st, _ := userSvc.State(ctx, tom.ID); st.TokenVersion != 1 {
		t.Fatalf("重置密码后缓存应已失效，token_version = %d", st.TokenVersion)
	}
}
