package model

import (
	"time"

	"gorm.io/datatypes"
)

// WorkerInstance 是任务队列实例的心跳记录：每个运行中的实例每 10 秒更新一行，后台“任务队列”页据此展示实例。
// 心跳超过 WorkerInstanceOfflineAfter 视为失联；失联实例手上的任务会被其它实例救回（见 queue 包）。
// 实例重启会换新的 id，旧行由清理循环删除（WorkerInstanceKeepFor 之后）。
type WorkerInstance struct {
	ID          string         `gorm:"primaryKey;size:64" json:"id"`                                      // River client id，每次启动生成一个新的
	Hostname    string         `gorm:"size:128;not null;default:''" json:"hostname"`                      // 主机名
	Version     string         `gorm:"size:64;not null;default:''" json:"version"`                        // 应用版本
	StartedAt   time.Time      `gorm:"not null" json:"started_at"`                                        // 启动时间
	HeartbeatAt time.Time      `gorm:"not null;index:idx_worker_instances_heartbeat" json:"heartbeat_at"` // 最近一次心跳
	IsLeader    bool           `gorm:"not null;default:false" json:"is_leader"`                           // 是否是 River 的 leader（跑清理、救回等维护任务）
	StatsJSON   datatypes.JSON `gorm:"column:stats_json;type:jsonb" json:"-"`                             // 各队列在途数与生效并发：{"submit":{"inflight":3,"limit":8},...}
}

// TableName 返回表名。
func (WorkerInstance) TableName() string { return "worker_instances" }

// 实例心跳的时间参数。
const (
	WorkerHeartbeatEvery       = 10 * time.Second // 心跳间隔
	WorkerInstanceOfflineAfter = 30 * time.Second // 超过这个时间没有心跳，视为失联
	WorkerInstanceKeepFor      = 24 * time.Hour   // 失联实例的记录保留时间
)
