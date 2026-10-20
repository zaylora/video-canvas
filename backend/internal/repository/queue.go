package repository

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
)

// QueueRepository 是任务队列的数据访问：实例心跳、对账救回，以及后台「任务队列」页的统计。
// 统计会直接查 River 的表（river_job、river_leader）：这些表由 River 的迁移器维护，只读，且只用它稳定的列。
type QueueRepository struct {
	db *gorm.DB
}

// NewQueueRepository 创建任务队列仓储。
func NewQueueRepository(db *gorm.DB) *QueueRepository { return &QueueRepository{db: db} }

// 三个阶段 job 的 kind 与活着的 job 状态，对账的 SQL 用。要和 provider/queue 里的定义保持一致。
const (
	queueJobKinds      = `('gen.submit','gen.poll','gen.finalize')`
	queueLiveJobStates = `('available','pending','retryable','running','scheduled')`
	queueNames         = `('gen_submit','gen_poll','gen_finalize')`
)

// UpsertInstance 写入或刷新实例的心跳记录：已存在时只更新主机名、版本、心跳时间、leader 标记与统计（启动时间不变）。
func (r *QueueRepository) UpsertInstance(ctx context.Context, inst model.WorkerInstance) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"hostname", "version", "heartbeat_at", "is_leader", "stats_json"}),
	}).Create(&inst).Error
}

// DeleteInstance 删除一个实例的记录（优雅退出时调用）；不存在也不报错。
func (r *QueueRepository) DeleteInstance(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&model.WorkerInstance{}, "id = ?", id).Error
}

// DeleteStaleInstances 删除心跳早于 before 的实例记录。
func (r *QueueRepository) DeleteStaleInstances(ctx context.Context, before time.Time) error {
	return r.db.WithContext(ctx).Where("heartbeat_at < ?", before).Delete(&model.WorkerInstance{}).Error
}

// ListInstances 返回全部实例记录（含失联的，按启动时间升序），由调用方按心跳判断是否在线。
func (r *QueueRepository) ListInstances(ctx context.Context) ([]model.WorkerInstance, error) {
	var list []model.WorkerInstance
	err := r.db.WithContext(ctx).Order("started_at ASC, id ASC").Find(&list).Error
	return list, err
}

// LiveInstanceIDs 返回心跳不早于 since 的实例 id。
func (r *QueueRepository) LiveInstanceIDs(ctx context.Context, since time.Time) ([]string, error) {
	var ids []string
	err := r.db.WithContext(ctx).Model(&model.WorkerInstance{}).Where("heartbeat_at >= ?", since).Pluck("id", &ids).Error
	return ids, err
}

// IsLeader 判断 clientID 是否是 River 当前的 leader（river_leader 里有一行未过期的记录指向它）。
func (r *QueueRepository) IsLeader(ctx context.Context, clientID string) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Raw(`SELECT COUNT(*) FROM river_leader WHERE leader_id = ? AND expires_at > now()`, clientID).Scan(&n).Error
	return n > 0, err
}

// unscheduledSQL 找“任务还没终态、队列里没有活着的 job”的任务。先把活着的 job 的任务 id 收集成一个集合再做反连接，
// 这样耗时随（任务数 + job 数）线性增长；写成对每个任务各查一遍 river_job 会随两者的乘积增长。
const unscheduledSQL = `
WITH live AS (
	SELECT (args->>'task_id')::bigint AS task_id FROM river_job
	WHERE kind IN ` + queueJobKinds + ` AND state IN ` + queueLiveJobStates + `
)
SELECT t.id, t.status, t.next_poll_at
FROM generation_tasks t
LEFT JOIN live ON live.task_id = t.id
WHERE t.status IN ('pending','queued','running','finalizing') AND live.task_id IS NULL
ORDER BY t.id
LIMIT ?`

// UnscheduledTasks 返回非终态、但队列里没有存活 job 的任务（最多 limit 个，按 id 升序）。
func (r *QueueRepository) UnscheduledTasks(ctx context.Context, limit int) ([]model.UnscheduledTask, error) {
	var list []model.UnscheduledTask
	err := r.db.WithContext(ctx).Raw(unscheduledSQL, limit).Scan(&list).Error
	return list, err
}

// RecoverOrphanedJobs 把“正由不在 liveIDs 里的实例执行”的 running job 立即放回队列（retryable，马上可取），返回数量。
// River 记录在 attempted_by 数组里的最后一个元素就是当前这次尝试的实例 id。liveIDs 为空时什么都不做，
// 避免调用方出错时把所有 running 的 job 都放回去。
func (r *QueueRepository) RecoverOrphanedJobs(ctx context.Context, liveIDs []string) (int64, error) {
	if len(liveIDs) == 0 {
		return 0, nil
	}
	res := r.db.WithContext(ctx).Exec(`
UPDATE river_job SET state = 'retryable', scheduled_at = now()
WHERE kind IN `+queueJobKinds+` AND state = 'running'
  AND attempted_by[array_length(attempted_by, 1)] NOT IN ?`, liveIDs)
	return res.RowsAffected, res.Error
}

type queueCountRow struct {
	Queue       string
	Available   int
	Scheduled   int
	Running     int
	Retryable   int
	Discarded1h int `gorm:"column:discarded_1h"`
}

// QueueCounts 返回各阶段（短名）的 job 计数。
//
// 提交阶段的特殊处理：任务在等用户名额 / 渠道名额时，它的 job 是“睡着”（scheduled）的，但从用户角度这就是在排队，
// 所以把这部分从“延迟中”挪到“排队”里，让“积压主要看排队这一列”成立。
func (r *QueueRepository) QueueCounts(ctx context.Context, now time.Time) (map[string]model.QueueCounts, error) {
	db := r.db.WithContext(ctx)
	var rows []queueCountRow
	err := db.Raw(`
SELECT queue,
       COUNT(*) FILTER (WHERE state = 'available') AS available,
       COUNT(*) FILTER (WHERE state = 'scheduled') AS scheduled,
       COUNT(*) FILTER (WHERE state = 'running') AS running,
       COUNT(*) FILTER (WHERE state = 'retryable') AS retryable,
       COUNT(*) FILTER (WHERE state = 'discarded' AND finalized_at > ?) AS discarded_1h
FROM river_job WHERE queue IN `+queueNames+` GROUP BY queue`, now.Add(-time.Hour)).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := map[string]model.QueueCounts{}
	for _, row := range rows {
		stage := row.Queue[len("gen_"):]
		out[stage] = model.QueueCounts{Available: row.Available, Scheduled: row.Scheduled, Running: row.Running, Retryable: row.Retryable, Discarded1h: row.Discarded1h}
	}

	var waiting int
	err = db.Raw(`
SELECT COUNT(*) FROM river_job j JOIN generation_tasks t ON t.id = (j.args->>'task_id')::bigint
WHERE j.queue = 'gen_submit' AND j.state = 'scheduled' AND t.status = 'pending' AND t.slot_acquired_at IS NULL`).Scan(&waiting).Error
	if err != nil {
		return nil, err
	}
	if c, ok := out[model.QueueStageSubmit]; ok && waiting > 0 {
		waiting = min(waiting, c.Scheduled)
		c.Available += waiting
		c.Scheduled -= waiting
		out[model.QueueStageSubmit] = c
	}
	return out, nil
}

// ThroughputPoint 是一分钟的吞吐：这一分钟内进入终态的任务。
type ThroughputPoint struct {
	Minute    time.Time `json:"minute"`      // 整分钟
	Succeeded int       `json:"succeeded"`   // 成功数
	Failed    int       `json:"failed"`      // 失败数（failed + expired，取消不算）
	AvgWaitMs int64     `json:"avg_wait_ms"` // 平均排队时长（创建 → 拿到名额），没有名额记录的任务不参与
}

// Throughput 返回 since 之后每分钟进入终态的任务统计（只统计有任务的分钟，缺的分钟由调用方补 0），按时间升序。不含试跑任务。
func (r *QueueRepository) Throughput(ctx context.Context, since time.Time) ([]ThroughputPoint, error) {
	var list []ThroughputPoint
	err := r.db.WithContext(ctx).Raw(`
SELECT date_trunc('minute', finished_at) AS minute,
       COUNT(*) FILTER (WHERE status = 'succeeded') AS succeeded,
       COUNT(*) FILTER (WHERE status IN ('failed','expired')) AS failed,
       COALESCE(AVG(EXTRACT(EPOCH FROM (slot_acquired_at - created_at)) * 1000) FILTER (WHERE slot_acquired_at IS NOT NULL), 0)::bigint AS avg_wait_ms
FROM generation_tasks
WHERE is_test = FALSE AND status IN ('succeeded','failed','expired') AND finished_at >= ?
GROUP BY 1 ORDER BY 1`, since).Scan(&list).Error
	return list, err
}

// ChannelOccupancy 是一个渠道当前的名额占用。
type ChannelOccupancy struct {
	Channel         string     `json:"channel"`           // 渠道 key
	Occupied        int        `json:"occupied"`          // 已占名额：提交中、queued、running
	Waiting         int        `json:"waiting"`           // 在等名额的 pending
	OldestWaitingAt *time.Time `json:"oldest_waiting_at"` // 最早一个在等名额的任务的创建时间
}

// ChannelOccupancies 返回每个有未完成任务的渠道的名额占用（没有任务的渠道不在结果里，调用方按 0 处理），按渠道 key 升序。
func (r *QueueRepository) ChannelOccupancies(ctx context.Context) ([]ChannelOccupancy, error) {
	var list []ChannelOccupancy
	err := r.db.WithContext(ctx).Raw(`
SELECT provider AS channel,
       COUNT(*) FILTER (WHERE slot_acquired_at IS NOT NULL) AS occupied,
       COUNT(*) FILTER (WHERE status = 'pending' AND slot_acquired_at IS NULL) AS waiting,
       MIN(created_at) FILTER (WHERE status = 'pending' AND slot_acquired_at IS NULL) AS oldest_waiting_at
FROM generation_tasks
WHERE status IN ('pending','queued','running')
GROUP BY provider ORDER BY provider`).Scan(&list).Error
	return list, err
}
