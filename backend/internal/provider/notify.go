package provider

import (
	"context"
	"time"
)

// TaskSettledNotice 是“一个 Agent 发起的生成任务进入了终态”的通知内容。
// 它只是门铃：不带产物，Agent 醒来后再向平台读取任务的最新状态。
type TaskSettledNotice struct {
	TaskID     uint64 // 生成任务 id
	AgentRunID uint64 // 发起它的 Agent 运行 id
	Status     string // 任务终态：succeeded / failed / expired / canceled
}

// AgentNotifier 把任务结束的消息送给 Agent 服务（真实实现在 service/agent，经 HTTP 调 Agent）。
// 队列在后台调用它，失败时按退避重试。
type AgentNotifier interface {
	// TaskSettled 通知 Agent。返回 nil 表示已送达，或 Agent 明确说没有对应的等待者（不需要再试）；
	// 返回错误表示这次没送到（连不上、Agent 报错），队列稍后重试。
	TaskSettled(ctx context.Context, n TaskSettledNotice) error
}

// AgentNotifyRetryCap 是通知重试的最长间隔：Agent 长时间不可用时，不会越退越久。
const AgentNotifyRetryCap = 10 * time.Minute
