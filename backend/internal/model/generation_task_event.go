package model

import (
	"time"

	"gorm.io/datatypes"
)

// 任务事件名：每个事件对应任务状态的一次真正变化，进度变化与 queued → running 不记事件。
const (
	TaskEventCreated    = "created"    // 任务入库（pending）
	TaskEventSubmitted  = "submitted"  // 上游已受理：pending → queued / running
	TaskEventFinalizing = "finalizing" // 上游已出结果，进入转存
	TaskEventSucceeded  = "succeeded"  // 成功
	TaskEventFailed     = "failed"     // 失败
	TaskEventCanceled   = "canceled"   // 已取消
	TaskEventExpired    = "expired"    // 已超时
)

// GenerationTaskEvent 是任务的状态时间线，只增不改不删，随任务保留（任务本身不删）。
// 事件与状态迁移在同一事务里写入，所以时间线不会缺事件；上线前的老任务没有事件。
// detail_json 里的上游错误原文由宿主在构造时遮掉渠道 Key，其余内容原样保存。
type GenerationTaskEvent struct {
	ID         uint64         `gorm:"primaryKey;index:idx_task_events_task,priority:2" json:"id"`    // 事件 ID，倒序即时间倒序
	TaskID     uint64         `gorm:"not null;index:idx_task_events_task,priority:1" json:"task_id"` // 所属任务
	Event      string         `gorm:"size:24;not null" json:"event"`                                 // 见 TaskEvent* 常量
	FromStatus string         `gorm:"size:16;not null;default:''" json:"from_status"`                // 迁移前状态，created 为空
	ToStatus   string         `gorm:"size:16;not null" json:"to_status"`                             // 迁移后状态
	DetailJSON datatypes.JSON `gorm:"type:jsonb" json:"detail_json"`                                 // 失败类事件：{upstream, reason}；submitted：{provider_task_id_set}
	CreatedAt  time.Time      `json:"created_at"`                                                    // 事件发生时间
}

// TableName 返回表名。
func (GenerationTaskEvent) TableName() string { return "generation_task_events" }
