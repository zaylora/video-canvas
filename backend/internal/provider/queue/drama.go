package queue

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/riverqueue/river"
	"go.uber.org/zap"
)

// DramaHandler 执行一个完整短剧步骤，业务终态由服务持久化。
type DramaHandler interface {
	HandleDrama(ctx context.Context, userID, dramaID, jobID uint64) error
}

// DramaArgs 一个drama_jobs行对应一个River任务。
type DramaArgs struct {
	UserID  uint64 `json:"user_id"`
	DramaID uint64 `json:"drama_id"`
	JobID   uint64 `json:"job_id"`
}

// Kind 实现River短剧任务类型。
func (DramaArgs) Kind() string { return "drama_step" }

// InsertOpts 同一业务任务同时只存在一个存活job。
func (DramaArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: "drama", MaxAttempts: jobMaxAttempts, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: liveStates}}
}

// dramaWorker 与生成队列共用实例并发闸门，不占用生成TaskStore状态机。
type dramaWorker struct {
	river.WorkerDefaults[DramaArgs]
	rt *Runtime
}

func (w *dramaWorker) Timeout(*river.Job[DramaArgs]) time.Duration { return jobTimeout }
func (w *dramaWorker) Work(ctx context.Context, j *river.Job[DramaArgs]) error {
	if !w.rt.gate.tryAcquire() {
		return river.JobSnooze(time.Second)
	}
	defer w.rt.gate.release()
	if err := w.rt.cfg.Drama.HandleDrama(ctx, j.Args.UserID, j.Args.DramaID, j.Args.JobID); err != nil {
		w.rt.log.Warn("执行短剧流水线失败，等待队列恢复", zap.Uint64("drama_job_id", j.Args.JobID), zap.Error(err))
		return err
	}
	return nil
}

// EnqueueDramaTx 将短剧业务行与River任务放在同一数据库事务提交。
func (rt *Runtime) EnqueueDramaTx(ctx context.Context, tx *sql.Tx, userID, dramaID, jobID uint64) error {
	if rt.cfg.Drama == nil {
		return fmt.Errorf("短剧任务执行器未配置")
	}
	_, err := rt.client.InsertTx(ctx, tx, DramaArgs{UserID: userID, DramaID: dramaID, JobID: jobID}, nil)
	if err != nil {
		return fmt.Errorf("入队短剧任务失败：%w", err)
	}
	return nil
}
