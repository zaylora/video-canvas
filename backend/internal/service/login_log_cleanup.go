package service

import (
	"context"
	"time"
)

// 登录记录（user_login_logs）只用于运营排查，保留有限天数后清理，避免表无限增长。
const (
	// DefaultLoginLogRetentionDays 是登录记录的固定保留天数（契约 §5），超过的由后台任务清理。
	// 它不再可配置：保留期是运营排查与存储体积之间的固定取舍，改动需要改代码并走评审。
	DefaultLoginLogRetentionDays = 180
	// LoginLogCleanupBatch 是每条 DELETE 最多删除的行数：批量太大会长时间占用事务与锁。
	LoginLogCleanupBatch = 1000
)

// LoginLogPurger 是清理登录记录的数据访问接口，由 repository.UserRepository 实现。
type LoginLogPurger interface {
	// DeleteLoginLogsBefore 删除早于 before 的登录记录，一次最多 limit 行，返回实际删除的行数。
	DeleteLoginLogsBefore(ctx context.Context, before time.Time, limit int) (int64, error)
}

// LoginLogCleaner 清理过期的登录记录。
type LoginLogCleaner struct {
	repo LoginLogPurger
}

// NewLoginLogCleaner 创建清理器，保留天数固定为 DefaultLoginLogRetentionDays。
func NewLoginLogCleaner(repo LoginLogPurger) *LoginLogCleaner {
	return &LoginLogCleaner{repo: repo}
}

// Cleanup 删除超过保留天数的登录记录，返回删除总数。
// 每批最多 1000 行，删到某批不足一批为止；某批失败立即返回已删数量与错误（下一轮定时任务会继续），ctx 取消同理。
// 多实例同时跑也安全：DELETE 本身幂等，互相只会让对方少删几行。
func (c *LoginLogCleaner) Cleanup(ctx context.Context) (int64, error) {
	// 1. 截止时间 = 现在 - 保留天数
	cutoff := time.Now().AddDate(0, 0, -DefaultLoginLogRetentionDays)

	// 2. 分批删除：每批前先看 ctx，退出信号来了就别再开新的事务
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, err := c.repo.DeleteLoginLogsBefore(ctx, cutoff, LoginLogCleanupBatch)
		total += n
		if err != nil {
			return total, err
		}
		if n < LoginLogCleanupBatch {
			return total, nil
		}
	}
}
