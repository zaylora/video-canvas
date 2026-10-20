package service

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
)

// QueueAdminRepo 是后台「任务队列」页的统计数据来源（真实实现是 repository.QueueRepository）。
type QueueAdminRepo interface {
	// QueueCounts 返回各阶段（短名）的 job 计数。
	QueueCounts(ctx context.Context, now time.Time) (map[string]model.QueueCounts, error)
	// Throughput 返回 since 之后每分钟进入终态的任务统计（只含有任务的分钟，按时间升序）。
	Throughput(ctx context.Context, since time.Time) ([]repository.ThroughputPoint, error)
	// ChannelOccupancies 返回每个有未完成任务的渠道的名额占用。
	ChannelOccupancies(ctx context.Context) ([]repository.ChannelOccupancy, error)
	// ListInstances 返回全部实例记录（含失联的）。
	ListInstances(ctx context.Context) ([]model.WorkerInstance, error)
}

// QueueChannelLister 列出全部渠道（名称与 max_running 从渠道配置里读）。
type QueueChannelLister interface {
	// ListChannels 返回所有渠道，按 sort、key 升序。
	ListChannels(ctx context.Context) ([]model.AIChannel, error)
}

// QueueConcurrencyStore 读写后台设置的各阶段并发（真实实现是 SettingsService）。
type QueueConcurrencyStore interface {
	// SetQueueConcurrency 保存一个阶段的并发设置。
	SetQueueConcurrency(ctx context.Context, actorID uint64, stage string, n int) error
}

// QueueController 是任务队列运行时对后台开放的操作（真实实现是 queue.Runtime）。
type QueueController interface {
	// States 返回各阶段的暂停状态与本实例的并发。
	States(ctx context.Context) ([]model.QueueStageState, error)
	// Pause 暂停一个阶段（对所有实例生效，幂等）。
	Pause(ctx context.Context, stage string) error
	// Resume 恢复一个阶段（幂等）。
	Resume(ctx context.Context, stage string) error
	// RefreshSettings 立即按后台设置刷新本实例的并发。
	RefreshSettings(ctx context.Context)
	// Heartbeat 立即写一次本实例的心跳，让“已生效于几个实例”马上包含本实例。
	Heartbeat(ctx context.Context)
	// HardCap 返回阶段并发的硬上限，未知阶段返回 0。
	HardCap(stage string) int
}

// 吞吐统计的参数。
const (
	defaultThroughputMinutes = 60
	maxThroughputMinutes     = 1440
)

// QueueAdminService 是后台「任务队列」页的业务逻辑：总览、吞吐、调并发、暂停 / 恢复。读 = admin 或 super_admin，写 = 仅 super_admin（由路由保证）。
type QueueAdminService struct {
	repo     QueueAdminRepo
	channels QueueChannelLister
	settings QueueConcurrencyStore
	ctrl     QueueController
	audit    AdminAuditWriter
	now      func() time.Time
}

// NewQueueAdminService 创建任务队列后台服务；audit 可为 nil（不写审计）。
func NewQueueAdminService(repo QueueAdminRepo, channels QueueChannelLister, settings QueueConcurrencyStore, ctrl QueueController, audit AdminAuditWriter) *QueueAdminService {
	return &QueueAdminService{repo: repo, channels: channels, settings: settings, ctrl: ctrl, audit: audit, now: time.Now}
}

// SetClock 替换时间来源（测试用）。
func (s *QueueAdminService) SetClock(now func() time.Time) { s.now = now }

// Overview 返回后台页的总览：三张队列卡片、渠道名额占用、实例列表。
//
//  1. 卡片：暂停状态与并发来自运行时，job 计数来自 river_job（提交阶段等名额的任务算排队）；
//  2. 渠道：渠道配置里的“最大同时生成数”（max_running，0 不限）加上当前名额占用与等待情况；没有未完成任务的渠道也列出来，占用 0；
//  3. 实例：心跳不超过 30 秒算在线，stats 是实例自己上报的各阶段在途与生效并发。
func (s *QueueAdminService) Overview(ctx context.Context) (*model.QueueOverview, error) {
	now := s.now()
	states, err := s.ctrl.States(ctx)
	if err != nil {
		return nil, err
	}
	counts, err := s.repo.QueueCounts(ctx, now)
	if err != nil {
		return nil, err
	}
	out := &model.QueueOverview{Queues: make([]model.QueueCard, 0, len(states))}
	for _, st := range states {
		out.Queues = append(out.Queues, model.QueueCard{
			Name: st.Stage, Label: model.QueueStageLabel(st.Stage), Paused: st.Paused,
			Concurrency: st.Concurrency, HardCap: st.HardCap, Counts: counts[st.Stage],
		})
	}

	chs, err := s.channels.ListChannels(ctx)
	if err != nil {
		return nil, err
	}
	occ, err := s.repo.ChannelOccupancies(ctx)
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]repository.ChannelOccupancy, len(occ))
	for _, o := range occ {
		byKey[o.Channel] = o
	}
	out.Channels = make([]model.QueueChannelRow, 0, len(chs))
	for _, ch := range chs {
		o := byKey[ch.Key]
		row := model.QueueChannelRow{Key: ch.Key, Name: ch.Name, Occupied: o.Occupied, MaxRunning: channelMaxRunning(ch), Waiting: o.Waiting}
		if o.OldestWaitingAt != nil {
			row.OldestWaitSec = max(int64(now.Sub(*o.OldestWaitingAt).Seconds()), 0)
		}
		out.Channels = append(out.Channels, row)
	}

	out.Instances, err = s.instances(ctx, now)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// instances 把实例记录转成列表行，按启动时间升序。
func (s *QueueAdminService) instances(ctx context.Context, now time.Time) ([]model.QueueInstanceRow, error) {
	list, err := s.repo.ListInstances(ctx)
	if err != nil {
		return nil, err
	}
	rows := make([]model.QueueInstanceRow, 0, len(list))
	for _, in := range list {
		rows = append(rows, model.QueueInstanceRow{
			ID: in.ID, Hostname: in.Hostname, Version: in.Version, StartedAt: in.StartedAt, HeartbeatAt: in.HeartbeatAt,
			Online:   now.Sub(in.HeartbeatAt) <= model.WorkerInstanceOfflineAfter,
			IsLeader: in.IsLeader, Stats: decodeInstanceStats(in.StatsJSON),
		})
	}
	return rows, nil
}

// Throughput 返回最近 minutes 分钟（15..1440，缺省 60）每分钟的完成数、失败数与平均排队时长；没有任务的分钟补 0，按时间升序。
func (s *QueueAdminService) Throughput(ctx context.Context, minutes int) ([]model.QueueThroughputPoint, error) {
	if minutes == 0 {
		minutes = defaultThroughputMinutes
	}
	if minutes < 15 || minutes > maxThroughputMinutes {
		return nil, errcode.ErrInvalidParams.WithMsg("统计分钟数必须在 15 到 " + strconv.Itoa(maxThroughputMinutes) + " 之间")
	}
	end := s.now().Truncate(time.Minute)
	start := end.Add(-time.Duration(minutes-1) * time.Minute)
	pts, err := s.repo.Throughput(ctx, start)
	if err != nil {
		return nil, err
	}
	byMinute := make(map[int64]repository.ThroughputPoint, len(pts))
	for _, p := range pts {
		byMinute[p.Minute.Truncate(time.Minute).Unix()] = p
	}
	out := make([]model.QueueThroughputPoint, 0, minutes)
	for i := range minutes {
		m := start.Add(time.Duration(i) * time.Minute)
		p := byMinute[m.Unix()]
		out = append(out, model.QueueThroughputPoint{Minute: m, Succeeded: p.Succeeded, Failed: p.Failed, AvgWaitMs: p.AvgWaitMs})
	}
	return out, nil
}

// SetConcurrency 调整一个阶段的并发（每个实例），立即生效，写审计。
//
//  1. 阶段必须存在，并发必须在 1..硬上限之间（硬上限在配置文件里，后台不能把实例压垮）；
//  2. 保存到系统设置，然后让本实例立刻按新值运行并写一次心跳；其它实例由定时刷新在 5 秒内跟上；
//  3. 返回“已按新并发运行的在线实例数 / 在线实例总数”：刚保存时通常只有本实例，随后的心跳会让数字追上。
func (s *QueueAdminService) SetConcurrency(ctx context.Context, actorID uint64, stage string, n int) (*model.QueueConcurrencyResult, error) {
	if model.QueueStageLabel(stage) == "" {
		return nil, errcode.ErrNotFound.WithMsg("队列不存在")
	}
	if hardCap := s.ctrl.HardCap(stage); n < 1 || n > hardCap {
		return nil, errcode.ErrInvalidParams.WithMsg("并发必须在 1 到 " + strconv.Itoa(hardCap) + " 之间")
	}
	if err := s.settings.SetQueueConcurrency(ctx, actorID, stage, n); err != nil {
		return nil, err
	}
	s.ctrl.RefreshSettings(ctx)
	s.ctrl.Heartbeat(ctx)
	adminAudit(ctx, s.audit, actorID, model.AdminAuditQueueConcurrency, model.AdminAuditTargetQueue, 0, map[string]any{"queue": stage, "concurrency": n})

	rows, err := s.instances(ctx, s.now())
	if err != nil {
		return nil, err
	}
	res := &model.QueueConcurrencyResult{Name: stage, Concurrency: n}
	for _, r := range rows {
		if !r.Online {
			continue
		}
		res.TotalInstances++
		if r.Stats[stage].Limit == n {
			res.AppliedInstances++
		}
	}
	return res, nil
}

// SetPaused 暂停（paused=true）或恢复一个阶段，对所有实例生效，幂等，写审计。
// 暂停期间已入队的任务保留，但排队时间继续累计，超过排队超时的任务会过期并退款。
func (s *QueueAdminService) SetPaused(ctx context.Context, actorID uint64, stage string, paused bool) (*model.QueuePauseResult, error) {
	if model.QueueStageLabel(stage) == "" {
		return nil, errcode.ErrNotFound.WithMsg("队列不存在")
	}
	var err error
	action := model.AdminAuditQueueResume
	if paused {
		action = model.AdminAuditQueuePause
		err = s.ctrl.Pause(ctx, stage)
	} else {
		err = s.ctrl.Resume(ctx, stage)
	}
	if err != nil {
		return nil, err
	}
	adminAudit(ctx, s.audit, actorID, action, model.AdminAuditTargetQueue, 0, map[string]any{"queue": stage})
	return &model.QueuePauseResult{Name: stage, Paused: paused}, nil
}

// channelMaxRunning 读渠道配置里的“最大同时生成数”（rate_limit_json.max_running），缺省、非法或为负都按 0（不限）。
func channelMaxRunning(ch model.AIChannel) int {
	var rl provider.RateLimit
	if err := json.Unmarshal(ch.RateLimitJSON, &rl); err != nil {
		return 0
	}
	return max(rl.MaxRunning, 0)
}

// decodeInstanceStats 解码实例心跳里的 stats_json；为空或损坏时返回空 map（页面显示“–”），不让一行脏数据拖垮整页。
func decodeInstanceStats(raw []byte) model.QueueInstanceStats {
	st := model.QueueInstanceStats{}
	if len(raw) == 0 {
		return st
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		return model.QueueInstanceStats{}
	}
	return st
}
