// Package queue 把生成任务的调度交给 River（基于 PostgreSQL 的开源任务队列）：
// 一个生成任务对应一个 job，从提交上游、查询进度到转存产物，全程在这一个 job 里跑完，直到任务进入终态。
//
// 分工：
//   - 任务行（generation_tasks）仍是唯一的业务事实来源：状态、积分、名额、下一次处理时间（next_poll_at）都在它上面；
//   - River 只负责“什么时候执行、哪个实例执行、同时执行几个、实例挂了怎么救回、怎么监控”；
//   - 真正的业务逻辑（提交、轮询、转存、失败、退款）仍在 provider/worker 里，队列只在合适的时候调用它的 Handle。
//
// 一个 job 的一生：创建任务的事务里入队（任务、积分冻结、job 一起提交）→ 队列取出后抢并发闸门 → 申请用户名额与渠道名额
// （拿不到就把 job 睡一会、让出闸门，不消耗重试次数；这段时间任务在界面上是“排队中”）→ 拿到名额后在 job 里循环：
// 调用 worker 处理一步，按任务行里的 next_poll_at 等到点，再处理下一步，直到任务终态。
// 实例崩溃时 job 由对账放回队列，重新取出后从任务行记录的状态接着往下走，不会重复提交。
//
// 文件分工：queue.go 参数类型与常量；gate.go 可调并发闸门；work.go 单个 job 的处理流程；
// runtime.go 运行时（River 客户端、设置刷新、实例心跳、对账救回、后台操作）；slog.go 日志桥接；migrate.go River 自己的表迁移。
package queue

import (
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

// River 里的队列名与 job 类型。对外（接口、界面）不暴露它们。
const (
	riverQueue = "generation"
	kindTask   = "gen.task"

	// 通知 Agent 的 job 单独一个队列：它只发一个很快的 HTTP 请求，不占生成并发的名额，也不被生成任务的排队拖慢
	notifyQueue = "agent_notify"
	kindNotify  = "agent.task_settled"
)

// notifyWorkers 是通知队列的并发：每个 job 只是一次 HTTP 调用，4 个足够。
const notifyWorkers = 4

// notifyMaxAttempts 是通知的最大尝试次数。退避从 NotifyBackoff 起翻倍、封顶 provider.AgentNotifyRetryCap，
// 默认起点 2 秒时 25 次约覆盖 3 小时：Agent 服务重启、发版期间不会丢通知，更久的缺口由 Agent 自己的兜底查询补上。
const notifyMaxAttempts = 25

// defaultNotifyBackoff 是通知重试退避的起点。
const defaultNotifyBackoff = 2 * time.Second

// jobTimeout 是单个 job 一次执行的时限：覆盖任务从拿到名额到终态的全程（模型的任务超时 + 转存宽限期）。
// 超时只会让 job 被取消后重试，重试时从任务行记录的状态接着走，所以取一个宽松的值即可。
// River 的 RescueStuckJobsAfter 要大于它，见 Runtime。
const jobTimeout = 6 * time.Hour

// jobMaxAttempts 是 job 的最大尝试次数。job 的“重试”只在基础设施故障（读库失败等）和被救回时发生，业务上的重试
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

// TaskArgs 是生成任务 job 的参数。
type TaskArgs struct {
	TaskID uint64 `json:"task_id"`
}

// Kind 实现 river.JobArgs。
func (TaskArgs) Kind() string { return kindTask }

// InsertOpts 实现 river.JobArgsWithInsertOpts：同一任务同时只存在一个活着的 job。
func (TaskArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       riverQueue,
		MaxAttempts: jobMaxAttempts,
		UniqueOpts:  river.UniqueOpts{ByArgs: true, ByState: liveStates},
	}
}

// NotifyArgs 是“通知 Agent 任务结束了”的 job 参数。
type NotifyArgs struct {
	TaskID     uint64 `json:"task_id"`
	AgentRunID uint64 `json:"agent_run_id"`
}

// Kind 实现 river.JobArgs。
func (NotifyArgs) Kind() string { return kindNotify }

// InsertOpts 实现 river.JobArgsWithInsertOpts：同一任务同时只存在一个活着的通知。
func (NotifyArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       notifyQueue,
		MaxAttempts: notifyMaxAttempts,
		UniqueOpts:  river.UniqueOpts{ByArgs: true, ByState: liveStates},
	}
}
