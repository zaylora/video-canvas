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

// 画布成片合成的队列名与 job 类型。
const (
	canvasComposeQueue = "canvas_compose"
	kindCanvasCompose  = "canvas_compose"
)

// canvasComposeJobTimeout 是单个合成 job 一次执行的时限：ffmpeg 自身受 10 分钟限制，
// 这里再留出下载、上传对象和落库的余量。
const canvasComposeJobTimeout = 20 * time.Minute

// CanvasComposeHandler 执行一条画布成片合成任务，业务终态由服务持久化。
type CanvasComposeHandler interface {
	// HandleCanvasCompose 执行任务；业务失败由服务落成任务失败并返回 nil，只有基础设施错误才返回 error（队列重试）。
	HandleCanvasCompose(ctx context.Context, jobID uint64) error
}

// CanvasComposeArgs 是合成 job 的参数：一条 canvas_compose_jobs 行对应一个 job。
type CanvasComposeArgs struct {
	JobID uint64 `json:"job_id"`
}

// Kind 实现 river.JobArgs。
func (CanvasComposeArgs) Kind() string { return kindCanvasCompose }

// RunRef 实现 RunArgs：画布成片合成。
func (a CanvasComposeArgs) RunRef() logger.RunRef {
	return logger.RunRef{Kind: "canvas_compose", ID: idcodec.Encode(a.JobID)}
}

// InsertOpts 实现 river.JobArgsWithInsertOpts：同一任务同时只存在一个存活 job。
func (CanvasComposeArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: canvasComposeQueue, MaxAttempts: jobMaxAttempts, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: liveStates}}
}

// canvasComposeWorker 与生成队列共用实例并发闸门，不占用生成 TaskStore 状态机。
type canvasComposeWorker struct {
	river.WorkerDefaults[CanvasComposeArgs]
	rt *Runtime
}

// Timeout 实现 river.Worker。
func (w *canvasComposeWorker) Timeout(*river.Job[CanvasComposeArgs]) time.Duration {
	return canvasComposeJobTimeout
}

// Work 抢到并发闸门后执行；闸门满了睡一秒再取，不消耗重试次数。
func (w *canvasComposeWorker) Work(ctx context.Context, j *river.Job[CanvasComposeArgs]) error {
	if !w.rt.gate.tryAcquire() {
		return river.JobSnooze(time.Second)
	}
	defer w.rt.gate.release()
	if err := w.rt.cfg.CanvasCompose.HandleCanvasCompose(ctx, j.Args.JobID); err != nil {
		w.rt.log.Warn("执行画布合成失败，等待队列重试", zap.Uint64("job_id", j.Args.JobID), zap.Error(err))
		return err
	}
	return nil
}

// EnqueueCanvasComposeTx 实现 repository.CanvasComposeEnqueuer：任务行与 River job 在同一个数据库事务里提交。
func (rt *Runtime) EnqueueCanvasComposeTx(ctx context.Context, tx *sql.Tx, jobID uint64) error {
	if rt.cfg.CanvasCompose == nil {
		return fmt.Errorf("合成执行器未配置")
	}
	if _, err := rt.client.InsertTx(ctx, tx, CanvasComposeArgs{JobID: jobID}, withRequestMeta(ctx, nil)); err != nil {
		return fmt.Errorf("入队合成任务失败：%w", err)
	}
	return nil
}
