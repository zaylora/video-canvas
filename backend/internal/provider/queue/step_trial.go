package queue

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/riverqueue/river"
	"go.uber.org/zap"
)

// 环节试跑的队列名与 job 类型。
const (
	stepTrialQueue = "step_trial"
	kindStepTrial  = "step_trial"
)

// stepTrialTimeout 是单个试跑 job 一次执行的时限：模型调用最长 10 分钟（含重写），留出落库的余量。
const stepTrialTimeout = 15 * time.Minute

// StepTrialHandler 执行一条环节试跑，业务终态由服务持久化。
type StepTrialHandler interface {
	// HandleStepTrial 执行试跑；业务失败由服务落成试跑结果并返回 nil，只有基础设施错误才返回 error（队列重试）。
	HandleStepTrial(ctx context.Context, trialID uint64) error
}

// StepTrialArgs 是试跑 job 的参数：一条 step_trials 行对应一个 job。
type StepTrialArgs struct {
	TrialID uint64 `json:"trial_id"`
}

// Kind 实现 river.JobArgs。
func (StepTrialArgs) Kind() string { return kindStepTrial }

// InsertOpts 实现 river.JobArgsWithInsertOpts：同一试跑同时只存在一个存活 job。
func (StepTrialArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: stepTrialQueue, MaxAttempts: jobMaxAttempts, UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: liveStates}}
}

// stepTrialWorker 与生成队列共用实例并发闸门，不占用生成 TaskStore 状态机。
type stepTrialWorker struct {
	river.WorkerDefaults[StepTrialArgs]
	rt *Runtime
}

// Timeout 实现 river.Worker。
func (w *stepTrialWorker) Timeout(*river.Job[StepTrialArgs]) time.Duration { return stepTrialTimeout }

// Work 抢到并发闸门后执行试跑；闸门满了睡一秒再取，不消耗重试次数。
func (w *stepTrialWorker) Work(ctx context.Context, j *river.Job[StepTrialArgs]) error {
	if !w.rt.gate.tryAcquire() {
		return river.JobSnooze(time.Second)
	}
	defer w.rt.gate.release()
	if err := w.rt.cfg.StepTrial.HandleStepTrial(ctx, j.Args.TrialID); err != nil {
		w.rt.log.Warn("执行环节试跑失败，等待队列重试", zap.Uint64("trial_id", j.Args.TrialID), zap.Error(err))
		return err
	}
	return nil
}

// EnqueueStepTrialTx 实现 repository.StepEnqueuer：试跑行与 River job 在同一个数据库事务里提交。
func (rt *Runtime) EnqueueStepTrialTx(ctx context.Context, tx *sql.Tx, trialID uint64) error {
	if rt.cfg.StepTrial == nil {
		return fmt.Errorf("环节试跑执行器未配置")
	}
	if _, err := rt.client.InsertTx(ctx, tx, StepTrialArgs{TrialID: trialID}, nil); err != nil {
		return fmt.Errorf("入队环节试跑失败：%w", err)
	}
	return nil
}
