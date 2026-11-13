package queue

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/riverqueue/river"
	"go.uber.org/zap"
)

// 营销写脚本的队列名与 job 类型。
const (
	marketingQueue = "marketing"
	kindMarketing  = "marketing_round"
)

// marketingRoundTimeout 是单个写脚本 job 一次执行的时限：两层环节各自最长 8 分钟，再留出落库和重试新增环节的余量。
const marketingRoundTimeout = 30 * time.Minute

// MarketingHandler 执行一轮营销写脚本，业务终态由服务持久化。
type MarketingHandler interface {
	// HandleMarketingRound 执行轮次里排队中的环节；业务失败由服务落成环节失败并返回 nil，只有基础设施错误才返回 error（队列重试）。
	HandleMarketingRound(ctx context.Context, roundID uint64) error
}

// MarketingRoundArgs 是写脚本 job 的参数：一个轮次对应一个 job（重试环节时新轮次状态下再入队一个）。
type MarketingRoundArgs struct {
	RoundID uint64 `json:"round_id"`
}

// Kind 实现 river.JobArgs。
func (MarketingRoundArgs) Kind() string { return kindMarketing }

// InsertOpts 实现 river.JobArgsWithInsertOpts：同一轮次同时只存在一个存活 job。
func (MarketingRoundArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: marketingQueue, MaxAttempts: jobMaxAttempts, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: liveStates}}
}

// marketingWorker 与生成队列共用实例并发闸门，不占用生成 TaskStore 状态机。
type marketingWorker struct {
	river.WorkerDefaults[MarketingRoundArgs]
	rt *Runtime
}

// Timeout 实现 river.Worker。
func (w *marketingWorker) Timeout(*river.Job[MarketingRoundArgs]) time.Duration {
	return marketingRoundTimeout
}

// Work 抢到并发闸门后执行；闸门满了睡一秒再取，不消耗重试次数。
func (w *marketingWorker) Work(ctx context.Context, j *river.Job[MarketingRoundArgs]) error {
	if !w.rt.gate.tryAcquire() {
		return river.JobSnooze(time.Second)
	}
	defer w.rt.gate.release()
	if err := w.rt.cfg.Marketing.HandleMarketingRound(ctx, j.Args.RoundID); err != nil {
		w.rt.log.Warn("执行营销写脚本失败，等待队列重试", zap.Uint64("round_id", j.Args.RoundID), zap.Error(err))
		return err
	}
	return nil
}

// EnqueueMarketingRoundTx 实现 repository.MarketingEnqueuer：轮次行与 River job 在同一个数据库事务里提交。
func (rt *Runtime) EnqueueMarketingRoundTx(ctx context.Context, tx *sql.Tx, roundID uint64) error {
	if rt.cfg.Marketing == nil {
		return fmt.Errorf("营销写脚本执行器未配置")
	}
	if _, err := rt.client.InsertTx(ctx, tx, MarketingRoundArgs{RoundID: roundID}, nil); err != nil {
		return fmt.Errorf("入队营销写脚本失败：%w", err)
	}
	return nil
}
