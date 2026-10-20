package model

import "time"

// SettingQueueConcurrency 是任务队列并发的设置键。值是每个实例同时处理的任务数，范围 1..配置文件里的硬上限；
// 库里没有或非法时用 DefaultQueueConcurrency。一个生成任务从提交、查询进度到转存产物，全程占一个并发名额。
const SettingQueueConcurrency = "queue_concurrency"

// DefaultQueueConcurrency 是默认并发（每个实例）。名额主要由用户并发上限与渠道“最大同时生成数”约束，
// 这个值是实例自己的保护线，所以给得比较宽。
const DefaultQueueConcurrency = 16

// QueueInstanceStats 是实例心跳里的 stats_json：本实例的在途任务数与生效并发。Limit 为 0 表示没有上报（旧版本或损坏）。
type QueueInstanceStats struct {
	Inflight int `json:"inflight"` // 正在处理的任务数
	Limit    int `json:"limit"`    // 当前生效的并发上限
}

// 任务队列的时间参数。
const (
	// QueueSettingsRefreshEvery 是各实例重读并发设置的间隔：后台改完并发，所有实例最迟这么久之后生效。
	QueueSettingsRefreshEvery = 5 * time.Second
)

// UnscheduledTask 是对账找到的“任务还没终态、队列里却没有活着的 job”的任务，对账为它补一个 job。
type UnscheduledTask struct {
	ID         uint64    // 任务 id
	NextPollAt time.Time // 下一次处理时间，补入队时据此排期
}

// QueueState 是队列当前的运行状态：暂停状态存在数据库里（对所有实例生效），并发与硬上限是本实例的值。
type QueueState struct {
	Paused      bool // 是否被暂停
	Concurrency int  // 本实例当前生效的并发
	HardCap     int  // 硬上限（配置文件）
}

// QueueCounts 是队列的 job 计数。一个任务一个 job，所以也是任务数。
type QueueCounts struct {
	Waiting     int `json:"waiting"`      // 排队：等名额、等并发、等到点的 job
	Running     int `json:"running"`      // 正在处理（占着并发名额）
	Retryable   int `json:"retryable"`    // 重试中
	Discarded1h int `json:"discarded_1h"` // 近 1 小时被丢弃（重试用尽）
}

// QueueCard 是后台页上的队列卡片：GET /admin/queue/overview 的 queue。
type QueueCard struct {
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
	Stats       QueueInstanceStats `json:"stats"`     // 在途任务数与生效并发
}

// QueueOverview 是 GET /admin/queue/overview 的响应。
type QueueOverview struct {
	Queue     QueueCard          `json:"queue"`
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

// QueueConcurrencyReq 是 PUT /admin/queue/concurrency 的请求体。
type QueueConcurrencyReq struct {
	Concurrency int `json:"concurrency" binding:"required,min=1" label:"并发"` // 范围 1..硬上限，上限在 service 里校验
}

// QueueConcurrencyResult 是 PUT /admin/queue/concurrency 的响应。
type QueueConcurrencyResult struct {
	Concurrency      int `json:"concurrency"`       // 保存后的并发
	AppliedInstances int `json:"applied_instances"` // 已经按新并发运行的在线实例数
	TotalInstances   int `json:"total_instances"`   // 在线实例总数
}

// QueuePauseResult 是暂停 / 恢复接口的响应。
type QueuePauseResult struct {
	Paused bool `json:"paused"` // 操作后是否处于暂停
}

// RiverTicketResult 是 POST /admin/queue/river-ticket 的响应。
type RiverTicketResult struct {
	URL string `json:"url"` // 同源相对路径，带一次性 ticket，前端直接在新标签页打开
}
