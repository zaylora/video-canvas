// Package queue 把生成任务的调度交给 River（基于 PostgreSQL 的开源任务队列）：
// 一个任务对应一个 job，job 随任务状态在三个阶段队列（提交上游 / 查询进度 / 转存产物）之间交接，直到任务进入终态。
//
// 分工：
//   - 任务行（generation_tasks）仍是唯一的业务事实来源：状态、积分、名额、下一次处理时间（next_poll_at）都在它上面；
//   - River 只负责“什么时候执行、哪个实例执行、同时执行几个、实例挂了怎么救回、怎么监控”；
//   - 真正的业务逻辑（提交、轮询、转存、失败、退款）仍在 provider/worker 里，队列只在合适的时候调用它的 Handle。
//
// 一个 job 的一生：创建任务的事务里入队（任务、积分冻结、job 一起提交）→ 队列取出后按任务行里的 next_poll_at 睡到点
// → 申请用户名额与渠道名额（拿不到就再睡一会，不消耗重试次数）→ 调用 worker 处理一步 → 读任务行：
// 终态就结束，状态进入下一阶段就把 job 交给对应阶段的队列，否则按新的 next_poll_at 继续睡。
//
// 文件分工：queue.go 参数类型与常量；gate.go 可调并发闸门；work.go 单个 job 的处理流程；
// runtime.go 运行时（River 客户端、设置刷新、实例心跳、对账救回、后台操作）；slog.go 日志桥接；migrate.go River 自己的表迁移。
package queue

import (
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"video-canvas/internal/model"
)

// River 里的队列名。对外（接口、界面、设置键）只用 model.QueueStage* 的短名。
const (
	riverQueuePrefix = "gen_"

	kindSubmit   = "gen.submit"
	kindPoll     = "gen.poll"
	kindFinalize = "gen.finalize"
)

// riverQueue 返回阶段对应的 River 队列名。
func riverQueue(stage string) string { return riverQueuePrefix + stage }

// 各阶段单个 job 的执行时限。必须覆盖 worker 内部最长的一次操作：
// 提交可能是同步接口（等上游把结果生成完，时限取模型的任务超时，缺省 30 分钟）；转存单次最长 30 分钟；查询只是一次短请求。
// River 的 RescueStuckJobsAfter 要大于它们的最大值，见 Runtime。
const (
	submitJobTimeout   = 45 * time.Minute
	pollJobTimeout     = 5 * time.Minute
	finalizeJobTimeout = 45 * time.Minute
)

// jobMaxAttempts 是 job 的最大尝试次数。job 的“重试”只在基础设施故障（读库失败等）时发生，业务上的重试
// 和退避都记在任务行里（poll_attempts / next_poll_at），所以这里给得很宽，避免 job 因为偶发故障被丢弃而变成孤儿。
const jobMaxAttempts = 100

// liveStates 是“job 还活着”的状态：唯一约束只在这些状态下生效，已完成 / 取消 / 丢弃的 job 不挡后来的同任务 job。
var liveStates = []rivertype.JobState{
	rivertype.JobStateAvailable,
	rivertype.JobStatePending,
	rivertype.JobStateRetryable,
	rivertype.JobStateRunning,
	rivertype.JobStateScheduled,
}

// insertOpts 是三个阶段 job 共用的入队选项：同一任务同一阶段同时只存在一个活着的 job。
func insertOpts(stage string) river.InsertOpts {
	return river.InsertOpts{
		Queue:       riverQueue(stage),
		MaxAttempts: jobMaxAttempts,
		UniqueOpts:  river.UniqueOpts{ByArgs: true, ByState: liveStates},
	}
}

// SubmitArgs 是“提交上游”阶段的 job 参数（任务处于 pending）。
type SubmitArgs struct {
	TaskID uint64 `json:"task_id"`
}

// Kind 实现 river.JobArgs。
func (SubmitArgs) Kind() string { return kindSubmit }

// InsertOpts 实现 river.JobArgsWithInsertOpts。
func (SubmitArgs) InsertOpts() river.InsertOpts { return insertOpts(model.QueueStageSubmit) }

// PollArgs 是“查询进度”阶段的 job 参数（任务处于 queued / running）。
type PollArgs struct {
	TaskID uint64 `json:"task_id"`
}

// Kind 实现 river.JobArgs。
func (PollArgs) Kind() string { return kindPoll }

// InsertOpts 实现 river.JobArgsWithInsertOpts。
func (PollArgs) InsertOpts() river.InsertOpts { return insertOpts(model.QueueStagePoll) }

// FinalizeArgs 是“转存产物”阶段的 job 参数（任务处于 finalizing）。
type FinalizeArgs struct {
	TaskID uint64 `json:"task_id"`
}

// Kind 实现 river.JobArgs。
func (FinalizeArgs) Kind() string { return kindFinalize }

// InsertOpts 实现 river.JobArgsWithInsertOpts。
func (FinalizeArgs) InsertOpts() river.InsertOpts { return insertOpts(model.QueueStageFinalize) }

// argsFor 返回阶段对应的 job 参数。
func argsFor(stage string, taskID uint64) river.JobArgs {
	switch stage {
	case model.QueueStagePoll:
		return PollArgs{TaskID: taskID}
	case model.QueueStageFinalize:
		return FinalizeArgs{TaskID: taskID}
	default:
		return SubmitArgs{TaskID: taskID}
	}
}
