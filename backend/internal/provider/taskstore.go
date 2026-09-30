package provider

import (
	"context"
	"encoding/json"
	"time"

	"video-canvas/internal/model"
)

// TaskStore 是 worker 需要的任务存储与状态迁移能力，由 service.GenerationTaskService 实现（worker.Store 是它的别名）。
// 接口放在 provider 包里，是为了让 service 与 worker 互不依赖：两边都只认这里的契约。
//
// 除 ClaimDue / ExtendLease / SaveProviderState / SaveTrace 外，迁移方法的第一个返回值 applied=false 表示任务已被其它流程
// （用户取消、超时、别的实例）迁移走了，worker 应放弃后续处理，这不是错误。
type TaskStore interface {
	// ClaimDue 领取最多 limit 个到期且租约已过期的非终态任务，并写入 lease 时长的租约。
	ClaimDue(ctx context.Context, limit int, lease time.Duration) ([]model.GenerationTask, error)
	// ExtendLease 续租，任务已终态返回 false。
	ExtendLease(ctx context.Context, id uint64, lease time.Duration) (bool, error)
	// SaveProviderState 只更新 provider_state，不改状态、不 bump version、不推送（准备阶段落库时用）。
	// 任务已终态或不存在时静默忽略（返回 nil）。
	SaveProviderState(ctx context.Context, t *model.GenerationTask, st ProviderState) error
	// MarkSubmitted pending → queued，记录上游任务 id 与插件 state（pluginState 为 nil 表示不变），安排第一次查询。
	MarkSubmitted(ctx context.Context, t *model.GenerationTask, providerTaskID string, pluginState json.RawMessage, nextPollAt time.Time) (bool, error)
	// MarkImmediate pending → finalizing：提交即成功（同步接口）。outputs（[]Output 的 JSON）写进 provider_result，
	// 之后 finalizing 直接用它转存，不再调用上游；pluginState 为 nil 表示不变。立即安排转存（next_poll_at = 现在并唤醒 worker）。
	MarkImmediate(ctx context.Context, t *model.GenerationTask, providerTaskID string, pluginState json.RawMessage, outputs json.RawMessage) (bool, error)
	// MarkPolled 记录一次查询结果（queued / running）与新的插件 state（nil 表示不变），安排下一次查询。
	MarkPolled(ctx context.Context, t *model.GenerationTask, status string, progress *int, pluginState json.RawMessage, attempts int, nextPollAt time.Time) (bool, error)
	// MarkFinalizing queued / running → finalizing，立即转存。
	MarkFinalizing(ctx context.Context, t *model.GenerationTask) (bool, error)
	// Retry 可重试的失败：状态不变，记录重试次数与下次处理时间。
	Retry(ctx context.Context, t *model.GenerationTask, attempts int, nextPollAt time.Time) (bool, error)
	// Complete finalizing → succeeded，写产物并结算积分。
	Complete(ctx context.Context, t *model.GenerationTask, outputs []model.TaskOutput) (bool, error)
	// Fail 非终态 → failed 并退回积分。errorCode 是统一错误码，message 是给用户看的文案。
	Fail(ctx context.Context, t *model.GenerationTask, errorCode, message string) (bool, error)
	// Expire 非终态 → expired 并退回积分。
	Expire(ctx context.Context, t *model.GenerationTask) (bool, error)
	// SaveTrace 写入试跑任务（is_test）的执行追踪；非试跑任务忽略。追踪已由宿主脱敏。
	SaveTrace(ctx context.Context, t *model.GenerationTask, steps []TraceStep) error
}
