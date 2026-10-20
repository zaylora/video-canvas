package provider

import (
	"context"
	"encoding/json"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/provider/modelcfg"
)

// SlotOutcome 是一次名额申请的结果。
type SlotOutcome string

// 名额申请的各种结果。
const (
	SlotAcquired    SlotOutcome = "acquired"     // 已拿到（或本来就持有）两个名额
	SlotWaitUser    SlotOutcome = "wait_user"    // 用户并发名额已满
	SlotWaitChannel SlotOutcome = "wait_channel" // 渠道“最大同时生成数”已满，或前面还有更早的等待者
	SlotGone        SlotOutcome = "gone"         // 任务已不是 pending，不用再申请
)

// TaskStore 是 worker 需要的任务存储与状态迁移能力，由 service.GenerationTaskService 实现（worker.Store 是它的别名）。
// 接口放在 provider 包里，是为了让 service 与 worker 互不依赖：两边都只认这里的契约。
//
// 除 GetTask / AcquireSlot / SaveProviderState / SaveTrace 外，迁移方法的第一个返回值 applied=false 表示任务已被其它流程
// （用户取消、超时、别的实例）迁移走了，worker 应放弃后续处理，这不是错误。
type TaskStore interface {
	// GetTask 按 id 读取任务（系统内部用，不按用户过滤）。任务不存在返回 (nil, nil)。
	GetTask(ctx context.Context, id uint64) (*model.GenerationTask, error)
	// AcquireSlot 为 pending 任务申请“用户名额 + 渠道名额”，拿到后写 slot_acquired_at 并把生成截止时间从此刻重新计算。
	// 没拿到不改任何数据，调用方稍后重试；任务已被取消 / 超时返回 SlotGone。
	AcquireSlot(ctx context.Context, t *model.GenerationTask) (SlotOutcome, error)
	// SaveProviderState 只更新 provider_state，不改状态、不 bump version、不推送（准备阶段落库时用）。
	// 任务已终态或不存在时静默忽略（返回 nil）。
	SaveProviderState(ctx context.Context, t *model.GenerationTask, st ProviderState) error
	// MarkSubmitting 记录“开始调用上游”：状态仍是 pending，写入 submitted_at 并推送，界面据此从“排队中”变成“生成中”。
	// 同步接口（生图等）在上游返回前一直是 pending，不记这一步用户会一直看到排队中。
	MarkSubmitting(ctx context.Context, t *model.GenerationTask) (bool, error)
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
	// Complete finalizing → succeeded，写产物并结算积分。usage 是文本任务的实际 Token 用量（插件没回传为 nil），
	// Token 计费据此按实际用量扣费并退回多冻结的部分；其他计费方式忽略它。
	Complete(ctx context.Context, t *model.GenerationTask, outputs []model.TaskOutput, usage *modelcfg.Usage) (bool, error)
	// Fail 非终态 → failed 并退回积分。errorCode 是统一错误码，message 是给用户看的文案。
	Fail(ctx context.Context, t *model.GenerationTask, errorCode, message string) (bool, error)
	// FailWithUpstream 与 Fail 相同，并把上游错误原文（宿主已遮掉渠道 Key）记进任务事件，只给后台排查用，不返回给用户。
	FailWithUpstream(ctx context.Context, t *model.GenerationTask, errorCode, message, upstream string) (bool, error)
	// Expire 非终态 → expired 并退回积分。
	Expire(ctx context.Context, t *model.GenerationTask) (bool, error)
	// SaveTrace 写入试跑任务（is_test）的执行追踪；非试跑任务忽略。追踪已由宿主脱敏。
	SaveTrace(ctx context.Context, t *model.GenerationTask, steps []TraceStep) error
}
