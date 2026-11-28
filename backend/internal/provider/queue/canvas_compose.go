package queue

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/riverqueue/river"

	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/logger"
)

const canvasComposeQueue = "canvas_compose"

// CanvasComposeHandler handles one durable rendering attempt.
type CanvasComposeHandler interface {
	HandleCanvasComposeAttempt(context.Context, uint64, int, bool) error
}

// CanvasComposeArgs identifies the composition to render.
type CanvasComposeArgs struct {
	JobID uint64 `json:"job_id"`
}

// Kind implements river.JobArgs.
func (CanvasComposeArgs) Kind() string { return canvasComposeQueue }

// RunRef identifies the composition in queue logs.
func (args CanvasComposeArgs) RunRef() logger.RunRef {
	return logger.RunRef{Kind: canvasComposeQueue, ID: idcodec.Encode(args.JobID)}
}

// InsertOpts routes composition jobs to their dedicated worker pool.
func (CanvasComposeArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: canvasComposeQueue, MaxAttempts: jobMaxAttempts, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: liveStates}}
}

type canvasComposeWorker struct {
	river.WorkerDefaults[CanvasComposeArgs]
	rt *Runtime
}

func (worker *canvasComposeWorker) Timeout(*river.Job[CanvasComposeArgs]) time.Duration {
	return 11 * time.Minute
}

func (worker *canvasComposeWorker) Work(ctx context.Context, job *river.Job[CanvasComposeArgs]) error {
	if !worker.rt.gate.tryAcquire() {
		return river.JobSnooze(time.Second)
	}
	defer worker.rt.gate.release()
	return worker.rt.cfg.CanvasCompose.HandleCanvasComposeAttempt(ctx, job.Args.JobID, job.Attempt, job.Attempt >= job.MaxAttempts)
}

// EnqueueCanvasComposeTx inserts the render attempt in the composition transaction.
func (rt *Runtime) EnqueueCanvasComposeTx(ctx context.Context, tx *sql.Tx, id uint64) error {
	if rt.cfg.CanvasCompose == nil {
		return errors.New("成片执行器未配置")
	}
	if _, err := rt.client.InsertTx(ctx, tx, CanvasComposeArgs{JobID: id}, withRequestMeta(ctx, nil)); err != nil {
		return fmt.Errorf("入队成片失败：%w", err)
	}
	return nil
}
