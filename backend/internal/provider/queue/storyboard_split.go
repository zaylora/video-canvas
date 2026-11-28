package queue

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/logger"

	"github.com/riverqueue/river"
	"go.uber.org/zap"
)

// 分镜表拆分镜的队列名与 job 类型。
const (
	storyboardSplitQueue = "storyboard_split"
	kindStoryboardSplit  = "storyboard_split"
)

// storyboardSplitJobTimeout 是单个拆分镜 job 一次执行的时限。模型自己的截止时间由服务按后台配置设，
// 这里只是 job 级的兜底：比能配的最大截止时间（见 modelcfg）再留出落库和重试的余量。
const storyboardSplitJobTimeout = 40 * time.Minute

// StoryboardSplitHandler 执行一条拆分镜任务，业务终态由服务持久化。
type StoryboardSplitHandler interface {
	// HandleStoryboardSplitAttempt 执行当前队列尝试，只有基础设施错误才交给队列重试。
	HandleStoryboardSplitAttempt(ctx context.Context, jobID uint64, attempt int, final bool) error
}

// StoryboardSplitArgs 是拆分镜 job 的参数：一条 storyboard_split_jobs 行对应一个 job。
type StoryboardSplitArgs struct {
	JobID uint64 `json:"job_id"`
}

// Kind 实现 river.JobArgs。
func (StoryboardSplitArgs) Kind() string { return kindStoryboardSplit }

// RunRef 实现 RunArgs：分镜表拆分镜。
func (a StoryboardSplitArgs) RunRef() logger.RunRef {
	return logger.RunRef{Kind: "storyboard_split", ID: idcodec.Encode(a.JobID)}
}

// InsertOpts 实现 river.JobArgsWithInsertOpts：同一任务同时只存在一个存活 job。
func (StoryboardSplitArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: storyboardSplitQueue, MaxAttempts: jobMaxAttempts, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: liveStates}}
}

// storyboardSplitWorker 与生成队列共用实例并发闸门，不占用生成 TaskStore 状态机。
type storyboardSplitWorker struct {
	river.WorkerDefaults[StoryboardSplitArgs]
	rt *Runtime
}

// Timeout 实现 river.Worker。
func (w *storyboardSplitWorker) Timeout(*river.Job[StoryboardSplitArgs]) time.Duration {
	return storyboardSplitJobTimeout
}

// Work 抢到并发闸门后执行；闸门满了睡一秒再取，不消耗重试次数。
func (w *storyboardSplitWorker) Work(ctx context.Context, j *river.Job[StoryboardSplitArgs]) error {
	if !w.rt.gate.tryAcquire() {
		return river.JobSnooze(time.Second)
	}
	defer w.rt.gate.release()
	if err := w.rt.cfg.StoryboardSplit.HandleStoryboardSplitAttempt(ctx, j.Args.JobID, j.Attempt, j.Attempt >= j.MaxAttempts); err != nil {
		w.rt.log.Warn("执行拆分镜失败，等待队列重试", zap.Uint64("job_id", j.Args.JobID), zap.Error(err))
		return err
	}
	return nil
}

// EnqueueStoryboardSplitTx 实现 repository.StoryboardSplitEnqueuer：任务行与 River job 在同一个数据库事务里提交。
func (rt *Runtime) EnqueueStoryboardSplitTx(ctx context.Context, tx *sql.Tx, jobID uint64) error {
	if rt.cfg.StoryboardSplit == nil {
		return fmt.Errorf("拆分镜执行器未配置")
	}
	if _, err := rt.client.InsertTx(ctx, tx, StoryboardSplitArgs{JobID: jobID}, withRequestMeta(ctx, nil)); err != nil {
		return fmt.Errorf("入队拆分镜失败：%w", err)
	}
	return nil
}
