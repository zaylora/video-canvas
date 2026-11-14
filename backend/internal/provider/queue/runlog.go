package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"go.uber.org/zap"

	"video-canvas/internal/pkg/logger"
)

// RunKindSystem 是不属于任何用户的后台任务（清理、通知等）的运行类型；运行编号取 River 任务 ID。
const RunKindSystem = "system"

// RunArgs 是所有队列任务参数必须实现的接口：告诉日志这次任务属于哪个运行。
// 用 AddWorker 注册 worker，没实现它编译不过（设计见 docs/design/统一服务器日志 6.12）。
type RunArgs interface {
	river.JobArgs
	// RunRef 返回运行标识：ID 和界面上显示的编号相同；系统任务返回 Kind=RunKindSystem、ID 留空。
	RunRef() logger.RunRef
}

// runRefs 登记每种任务类型怎么从原始参数里取运行标识：River 的中间件只拿得到 JSON，拿不到带类型的参数。
var (
	runRefsMu sync.RWMutex
	runRefs   = map[string]func([]byte) (logger.RunRef, error){}
)

// AddWorker 替代 river.AddWorker：注册 worker 的同时登记该任务类型的运行标识解码。
// 架构测试禁止在别处直接调用 river.AddWorker。
func AddWorker[T RunArgs](workers *river.Workers, w river.Worker[T]) {
	river.AddWorker(workers, w)
	var zero T
	runRefsMu.Lock()
	runRefs[zero.Kind()] = func(raw []byte) (logger.RunRef, error) {
		var args T
		if err := json.Unmarshal(raw, &args); err != nil {
			return logger.RunRef{}, err
		}
		return args.RunRef(), nil
	}
	runRefsMu.Unlock()
}

// refOf 取任务的运行标识。没登记或解不出来时按任务类型记，不影响执行；ID 为空时用 River 任务 ID。
func refOf(job *rivertype.JobRow) logger.RunRef {
	runRefsMu.RLock()
	decode := runRefs[job.Kind]
	runRefsMu.RUnlock()
	var ref logger.RunRef
	if decode != nil {
		ref, _ = decode(job.EncodedArgs)
	}
	if ref.Kind == "" {
		ref.Kind = job.Kind
	}
	if ref.ID == "" {
		ref.ID = strconv.FormatInt(job.ID, 10)
	}
	if ref.RequestID == "" {
		var meta struct {
			RequestID string `json:"request_id"`
		}
		_ = json.Unmarshal(job.Metadata, &meta) // 没有 metadata 或格式不对时只是少一个字段
		ref.RequestID = meta.RequestID
	}
	return ref
}

// withRequestMeta 把入队请求的编号放进任务 metadata，执行时由中间件还原。不放进任务参数：参数参与去重（UniqueOpts.ByArgs）。
// opts 为 nil 时其余选项沿用参数自带的 InsertOpts()。
func withRequestMeta(ctx context.Context, opts *river.InsertOpts) *river.InsertOpts {
	rid := logger.RequestIDOf(ctx)
	if rid == "" {
		return opts
	}
	out := river.InsertOpts{}
	if opts != nil {
		out = *opts
	}
	out.Metadata, _ = json.Marshal(map[string]string{"request_id": rid})
	return &out
}

// runLogMiddleware 给每次任务执行挂上运行上下文，并在业务没管到的情况下补日志：
//   - 被延后（JobSnooze）：不打任何日志——生成任务轮询、闸门已满时每秒都会延后；
//   - 返回错误、还会重试：打 run.retry；
//   - 返回错误、不再重试（重试用完或 JobCancel）：业务没收尾就兜底打 failed 的 run.finish；
//   - 正常返回：本次执行里开始了运行（Run.Start）却没收尾，兜底打 unknown 的 run.finish；没开始则不打（如任务已是终态）；
//   - panic：兜底打 Error 级 run.finish，再把 panic 抛回给 River。
//
// 系统任务没有业务收尾，成功时只打 Debug。
type runLogMiddleware struct {
	river.MiddlewareDefaults
}

// NewRunLogMiddleware 返回挂到 river.Config.WorkerMiddleware 上的运行日志中间件。
func NewRunLogMiddleware() rivertype.WorkerMiddleware { return &runLogMiddleware{} }

// Work 实现 rivertype.WorkerMiddleware。
func (m *runLogMiddleware) Work(ctx context.Context, job *rivertype.JobRow, doInner func(context.Context) error) (err error) {
	ref := refOf(job)
	ctx = logger.WithAttempt(ctx, job.Attempt)
	ctx = logger.WithFields(ctx, zap.String("job_kind", job.Kind))
	ctx, run := logger.BeginRun(ctx, ref)

	defer func() {
		if r := recover(); r != nil {
			run.Fallback(logger.StatusFailed, zap.String("error_type", logger.ErrorTypeInternal), zap.String("error", fmt.Sprint("panic: ", r)), zap.Stack("stack"))
			panic(r)
		}
		var snooze *river.JobSnoozeError
		var cancel *river.JobCancelError
		switch {
		case errors.As(err, &snooze):
		case err != nil && !errors.As(err, &cancel) && job.Attempt < job.MaxAttempts:
			logger.Ctx(ctx).Warn("任务出错，队列将重试", logger.Event(logger.EventRunRetry), zap.Int("max_attempts", job.MaxAttempts), zap.String("error", err.Error()))
		case err != nil:
			run.Fallback(logger.StatusFailed, zap.String("error_type", logger.ErrorTypeInternal), zap.String("error", err.Error()))
		case ref.Kind == RunKindSystem:
			if !run.Ended() {
				logger.Ctx(ctx).Debug("系统任务完成", logger.Event(logger.EventRunFinish), zap.String("status", logger.StatusSucceeded))
			}
		case run.Started():
			run.Fallback(logger.StatusUnknown)
		}
	}()
	return doInner(ctx)
}
