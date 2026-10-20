package model

import "time"

// 任务队列的三个阶段。一个生成任务依次经过它们，每个阶段在 River 里是一个独立的队列（river 队列名见 queue 包），
// 后台分别调整并发。对外（接口、界面、设置键）只用这里的短名，不暴露 river 的队列名。
const (
	QueueStageSubmit   = "submit"   // 提交上游：拿名额、调用上游提交
	QueueStagePoll     = "poll"     // 查询进度：定时查询上游任务状态
	QueueStageFinalize = "finalize" // 转存产物：把产物下载到自有存储并结算
)

// QueueStages 是全部阶段，顺序即页面上的展示顺序。
var QueueStages = []string{QueueStageSubmit, QueueStagePoll, QueueStageFinalize}

// QueueStageLabel 返回阶段的中文名，后台界面只显示它。未知阶段返回空串。
func QueueStageLabel(stage string) string {
	switch stage {
	case QueueStageSubmit:
		return "提交上游"
	case QueueStagePoll:
		return "查询进度"
	case QueueStageFinalize:
		return "转存产物"
	}
	return ""
}

// QueueStageOfStatus 把非终态任务状态映射到负责它的阶段；终态返回 false。
func QueueStageOfStatus(status string) (string, bool) {
	switch status {
	case TaskPending:
		return QueueStageSubmit, true
	case TaskQueued, TaskRunning:
		return QueueStagePoll, true
	case TaskFinalizing:
		return QueueStageFinalize, true
	}
	return "", false
}

// SettingQueueConcurrencyPrefix 是各阶段并发的设置键前缀：queue_concurrency_submit / _poll / _finalize。
// 值是每个实例的并发数，范围 1..配置文件里的硬上限；库里没有或非法时用 DefaultQueueConcurrency。
const SettingQueueConcurrencyPrefix = "queue_concurrency_"

// DefaultQueueConcurrency 是各阶段的默认并发（每个实例）。提交与旧的全局并发 8 持平；
// 查询只发一次很短的请求、占用时间短，所以更宽；转存要下载大文件，更窄。
var DefaultQueueConcurrency = map[string]int{
	QueueStageSubmit:   8,
	QueueStagePoll:     16,
	QueueStageFinalize: 4,
}

// QueueStageStats 是一个实例上某个阶段的在途数与生效并发。
type QueueStageStats struct {
	Inflight int `json:"inflight"` // 正在执行的任务数
	Limit    int `json:"limit"`    // 当前生效的并发上限
}

// QueueInstanceStats 是实例心跳里的 stats_json：阶段短名 → 在途与并发。
type QueueInstanceStats map[string]QueueStageStats

// 任务队列的时间参数。
const (
	// QueueSettingsRefreshEvery 是各实例重读并发设置的间隔：后台改完并发，所有实例最迟这么久之后生效。
	QueueSettingsRefreshEvery = 5 * time.Second
)

// UnscheduledTask 是对账找到的“任务还没终态、队列里却没有活着的 job”的任务，对账按它的状态补一个对应阶段的 job。
type UnscheduledTask struct {
	ID         uint64    // 任务 id
	Status     string    // 任务状态，决定补进哪个阶段
	NextPollAt time.Time // 下一次处理时间，补入队时据此排期
}

// QueueStageState 是一个阶段当前的运行状态：暂停状态存在数据库里（对所有实例生效），并发与硬上限是本实例的值。
type QueueStageState struct {
	Stage       string // 阶段短名，见 QueueStage*
	Paused      bool   // 是否被暂停
	Concurrency int    // 本实例当前生效的并发
	HardCap     int    // 硬上限（配置文件）
}

// QueueCounts 是一个队列的 job 计数。
type QueueCounts struct {
	Available   int `json:"available"`    // 排队：可以取、等执行的；提交阶段还在等名额的任务也算在内
	Scheduled   int `json:"scheduled"`    // 延迟中：等到点再处理（下一次查询、重试退避）
	Running     int `json:"running"`      // 运行中
	Retryable   int `json:"retryable"`    // 重试中
	Discarded1h int `json:"discarded_1h"` // 近 1 小时被丢弃（重试用尽）
}

// QueueCard 是后台页上的一张队列卡片：GET /admin/queue/overview 的 queues 项。
type QueueCard struct {
	Name        string      `json:"name"`        // 阶段短名：submit / poll / finalize
	Label       string      `json:"label"`       // 中文名：提交上游 / 查询进度 / 转存产物
	Paused      bool        `json:"paused"`      // 是否被暂停
	Concurrency int         `json:"concurrency"` // 每个实例当前生效的并发
	HardCap     int         `json:"hard_cap"`    // 并发的硬上限（配置文件），后台只能在 1..硬上限 之间调
	Counts      QueueCounts `json:"counts"`      // job 计数
}

// QueueChannelRow 是渠道名额占用的一行。
type QueueChannelRow struct {
	Key           string `json:"key"`             // 渠道 key
	Name          string `json:"name"`            // 渠道显示名
	Occupied      int    `json:"occupied"`        // 已占名额（提交中、queued、running）
	MaxRunning    int    `json:"max_running"`     // 渠道的“最大同时生成数”，0 表示不限
	Waiting       int    `json:"waiting"`         // 在等名额的任务数
	OldestWaitSec int64  `json:"oldest_wait_sec"` // 最早一个在等名额的任务已经等了多少秒，没有为 0
}

// QueueInstanceRow 是实例列表的一行。
type QueueInstanceRow struct {
	ID          string             `json:"id"`
	Hostname    string             `json:"hostname"`
	Version     string             `json:"version"`
	StartedAt   time.Time          `json:"started_at"`
	HeartbeatAt time.Time          `json:"heartbeat_at"`
	Online      bool               `json:"online"`    // 心跳不超过 WorkerInstanceOfflineAfter
	IsLeader    bool               `json:"is_leader"` // 是否是 River 的 leader
	Stats       QueueInstanceStats `json:"stats"`     // 各阶段在途数与生效并发
}

// QueueOverview 是 GET /admin/queue/overview 的响应。
type QueueOverview struct {
	Queues    []QueueCard        `json:"queues"`
	Channels  []QueueChannelRow  `json:"channels"`
	Instances []QueueInstanceRow `json:"instances"`
}

// QueueThroughputPoint 是 GET /admin/queue/throughput 的一项：一分钟内进入终态的任务。
type QueueThroughputPoint struct {
	Minute    time.Time `json:"minute"`      // 整分钟
	Succeeded int       `json:"succeeded"`   // 成功数
	Failed    int       `json:"failed"`      // 失败数（failed + expired）
	AvgWaitMs int64     `json:"avg_wait_ms"` // 平均排队时长（毫秒）
}

// QueueThroughputReq 是 GET /admin/queue/throughput 的查询参数。
type QueueThroughputReq struct {
	Minutes int `form:"minutes" binding:"omitempty,min=15,max=1440" label:"统计分钟数"` // 缺省 60
}

// QueueConcurrencyReq 是 PUT /admin/queue/queues/:name 的请求体。
type QueueConcurrencyReq struct {
	Concurrency int `json:"concurrency" binding:"required,min=1" label:"并发"` // 范围 1..该阶段的硬上限，上限在 service 里校验
}

// QueueConcurrencyResult 是 PUT /admin/queue/queues/:name 的响应。
type QueueConcurrencyResult struct {
	Name             string `json:"name"`              // 阶段短名
	Concurrency      int    `json:"concurrency"`       // 保存后的并发
	AppliedInstances int    `json:"applied_instances"` // 已经按新并发运行的在线实例数
	TotalInstances   int    `json:"total_instances"`   // 在线实例总数
}

// QueuePauseResult 是暂停 / 恢复接口的响应。
type QueuePauseResult struct {
	Name   string `json:"name"`   // 阶段短名
	Paused bool   `json:"paused"` // 操作后是否处于暂停
}

// RiverTicketResult 是 POST /admin/queue/river-ticket 的响应。
type RiverTicketResult struct {
	URL string `json:"url"` // 同源相对路径，带一次性 ticket，前端直接在新标签页打开
}
