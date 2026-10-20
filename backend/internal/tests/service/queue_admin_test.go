package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
	. "video-canvas/internal/service"
)

// ---------- 假依赖 ----------

type fakeQueueRepo struct {
	counts    model.QueueCounts
	points    []repository.ThroughputPoint
	occ       []repository.ChannelOccupancy
	instances []model.WorkerInstance
	errs      map[string]error
	since     time.Time // Throughput 收到的起点
}

func (f *fakeQueueRepo) QueueCounts(context.Context, time.Time) (model.QueueCounts, error) {
	return f.counts, f.errs["QueueCounts"]
}
func (f *fakeQueueRepo) Throughput(_ context.Context, since time.Time) ([]repository.ThroughputPoint, error) {
	f.since = since
	return f.points, f.errs["Throughput"]
}
func (f *fakeQueueRepo) ChannelOccupancies(context.Context) ([]repository.ChannelOccupancy, error) {
	return f.occ, f.errs["ChannelOccupancies"]
}
func (f *fakeQueueRepo) ListInstances(context.Context) ([]model.WorkerInstance, error) {
	return f.instances, f.errs["ListInstances"]
}

type fakeQueueChannels struct {
	list []model.AIChannel
	err  error
}

func (f *fakeQueueChannels) ListChannels(context.Context) ([]model.AIChannel, error) {
	return f.list, f.err
}

type fakeConcStore struct {
	saved int
	by    uint64
	err   error
}

func (f *fakeConcStore) SetQueueConcurrency(_ context.Context, actor uint64, n int) error {
	if f.err != nil {
		return f.err
	}
	f.saved, f.by = n, actor
	return nil
}

type fakeQueueCtrl struct {
	state     model.QueueState
	paused    int
	resumed   int
	refreshed int
	beats     int
	errs      map[string]error
}

func (f *fakeQueueCtrl) State(context.Context) (model.QueueState, error) {
	return f.state, f.errs["State"]
}
func (f *fakeQueueCtrl) Pause(context.Context) error {
	if err := f.errs["Pause"]; err != nil {
		return err
	}
	f.paused++
	return nil
}
func (f *fakeQueueCtrl) Resume(context.Context) error {
	if err := f.errs["Resume"]; err != nil {
		return err
	}
	f.resumed++
	return nil
}
func (f *fakeQueueCtrl) RefreshSettings(context.Context) { f.refreshed++ }
func (f *fakeQueueCtrl) Heartbeat(context.Context)       { f.beats++ }
func (f *fakeQueueCtrl) HardCap() int                    { return f.state.HardCap }

type queueEnv struct {
	svc      *QueueAdminService
	repo     *fakeQueueRepo
	channels *fakeQueueChannels
	conc     *fakeConcStore
	ctrl     *fakeQueueCtrl
	audit    *fakeAudit
	now      time.Time
}

func newQueueEnv() *queueEnv {
	e := &queueEnv{
		repo:     &fakeQueueRepo{errs: map[string]error{}},
		channels: &fakeQueueChannels{},
		conc:     &fakeConcStore{},
		ctrl:     &fakeQueueCtrl{state: model.QueueState{Concurrency: 8, HardCap: 64}, errs: map[string]error{}},
		audit:    &fakeAudit{},
		now:      time.Date(2026, 10, 20, 12, 30, 45, 0, time.UTC),
	}
	e.svc = NewQueueAdminService(e.repo, e.channels, e.conc, e.ctrl, e.audit)
	e.svc.SetClock(func() time.Time { return e.now })
	return e
}

func channel(key, name, rateLimit string) model.AIChannel {
	return model.AIChannel{Key: key, Name: name, RateLimitJSON: model.JSONText(rateLimit)}
}

// ---------- 总览 ----------

func TestQueueAdmin_Overview(t *testing.T) {
	ctx := context.Background()

	t.Run("队列卡片：暂停状态、并发、硬上限、计数", func(t *testing.T) {
		e := newQueueEnv()
		e.repo.counts = model.QueueCounts{Waiting: 64, Running: 8, Retryable: 2, Discarded1h: 1}
		e.ctrl.state = model.QueueState{Paused: true, Concurrency: 8, HardCap: 64}
		ov, err := e.svc.Overview(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if q := ov.Queue; !q.Paused || q.Concurrency != 8 || q.HardCap != 64 || q.Counts != e.repo.counts {
			t.Fatalf("队列卡片不对：%+v", q)
		}
	})

	t.Run("渠道：没有任务的渠道也列出来；max_running 取渠道配置，缺省 / 损坏 / 为负都按不限；最早等待按秒算", func(t *testing.T) {
		e := newQueueEnv()
		e.channels.list = []model.AIChannel{
			channel("a", "可灵", `{"max_running":5}`),
			channel("b", "Vidu", `{}`),
			channel("c", "坏配置", `not json`),
			channel("d", "负数", `{"max_running":-3}`),
		}
		wait := e.now.Add(-6*time.Minute - 12*time.Second)
		e.repo.occ = []repository.ChannelOccupancy{{Channel: "a", Occupied: 5, Waiting: 14, OldestWaitingAt: &wait}, {Channel: "b", Occupied: 2}}
		ov, err := e.svc.Overview(ctx)
		if err != nil || len(ov.Channels) != 4 {
			t.Fatalf("%+v %v", ov, err)
		}
		a, b, c, d := ov.Channels[0], ov.Channels[1], ov.Channels[2], ov.Channels[3]
		if a.Name != "可灵" || a.Occupied != 5 || a.MaxRunning != 5 || a.Waiting != 14 || a.OldestWaitSec != 372 {
			t.Fatalf("a 不对：%+v", a)
		}
		if b.Occupied != 2 || b.MaxRunning != 0 || b.OldestWaitSec != 0 {
			t.Fatalf("b 不对：%+v", b)
		}
		if c.MaxRunning != 0 || d.MaxRunning != 0 || c.Occupied != 0 {
			t.Fatalf("损坏 / 为负的配置应按不限、占用 0：%+v %+v", c, d)
		}
	})

	t.Run("实例：心跳不超过 30 秒算在线；stats 解码；损坏、旧格式或缺失的 stats 变成零值", func(t *testing.T) {
		e := newQueueEnv()
		e.repo.instances = []model.WorkerInstance{
			{ID: "i1", Hostname: "vc-1", Version: "0.4.6", HeartbeatAt: e.now.Add(-2 * time.Second), IsLeader: true,
				StatsJSON: []byte(`{"inflight":3,"limit":8}`)},
			{ID: "i2", HeartbeatAt: e.now.Add(-model.WorkerInstanceOfflineAfter), StatsJSON: []byte("{")},
			{ID: "i4", HeartbeatAt: e.now, StatsJSON: []byte(`{"submit":{"inflight":3,"limit":8}}`)},
			{ID: "i3", HeartbeatAt: e.now.Add(-47 * time.Second)},
		}
		ov, err := e.svc.Overview(ctx)
		if err != nil || len(ov.Instances) != 4 {
			t.Fatalf("%+v %v", ov, err)
		}
		i1, i2, i4, i3 := ov.Instances[0], ov.Instances[1], ov.Instances[2], ov.Instances[3]
		if !i1.Online || !i1.IsLeader || i1.Stats != (model.QueueInstanceStats{Inflight: 3, Limit: 8}) || i1.Hostname != "vc-1" {
			t.Fatalf("i1 不对：%+v", i1)
		}
		if !i2.Online || i2.Stats != (model.QueueInstanceStats{}) {
			t.Fatalf("刚好 30 秒仍算在线，损坏的 stats 为零值：%+v", i2)
		}
		if i4.Stats != (model.QueueInstanceStats{}) {
			t.Fatalf("旧版本按阶段上报的 stats 不认，按零值处理（页面显示“–”）：%+v", i4)
		}
		if i3.Online || i3.Stats != (model.QueueInstanceStats{}) {
			t.Fatalf("47 秒前失联；没有 stats 是零值：%+v", i3)
		}
	})

	t.Run("任何一个依赖出错都透传，不返回半截数据", func(t *testing.T) {
		for _, name := range []string{"State", "QueueCounts", "ChannelOccupancies", "ListInstances"} {
			e := newQueueEnv()
			if name == "State" {
				e.ctrl.errs[name] = errBoom
			} else {
				e.repo.errs[name] = errBoom
			}
			if ov, err := e.svc.Overview(ctx); !errors.Is(err, errBoom) || ov != nil {
				t.Fatalf("%s：%+v %v", name, ov, err)
			}
		}
		e := newQueueEnv()
		e.channels.err = errBoom
		if _, err := e.svc.Overview(ctx); !errors.Is(err, errBoom) {
			t.Fatalf("渠道列表出错应透传：%v", err)
		}
	})
}

// ---------- 吞吐 ----------

func TestQueueAdmin_Throughput(t *testing.T) {
	ctx := context.Background()

	t.Run("缺省 60 分钟：没有任务的分钟补 0，按时间升序，最后一个点是当前分钟", func(t *testing.T) {
		e := newQueueEnv()
		cur := e.now.Truncate(time.Minute)
		e.repo.points = []repository.ThroughputPoint{
			{Minute: cur, Succeeded: 3, Failed: 1, AvgWaitMs: 1500},
			{Minute: cur.Add(-10 * time.Minute), Succeeded: 7},
		}
		got, err := e.svc.Throughput(ctx, 0)
		if err != nil || len(got) != 60 {
			t.Fatalf("应有 60 个点：%d %v", len(got), err)
		}
		if !got[59].Minute.Equal(cur) || got[59].Succeeded != 3 || got[59].Failed != 1 || got[59].AvgWaitMs != 1500 {
			t.Fatalf("最后一个点应是当前分钟：%+v", got[59])
		}
		if got[49].Succeeded != 7 || got[48].Succeeded != 0 || got[0].Succeeded != 0 {
			t.Fatalf("10 分钟前应有 7 个，其余补 0：%+v", got[49])
		}
		if !got[0].Minute.Equal(cur.Add(-59 * time.Minute)) {
			t.Fatalf("起点不对：%v", got[0].Minute)
		}
		if !e.repo.since.Equal(got[0].Minute) {
			t.Fatalf("查询起点应等于第一个点：%v", e.repo.since)
		}
	})

	t.Run("分钟数范围 15..1440，越界是参数错误，不查库", func(t *testing.T) {
		for _, n := range []int{-1, 1, 14, 1441, 99999} {
			e := newQueueEnv()
			_, err := e.svc.Throughput(ctx, n)
			if codeOf(err) != errcode.ErrInvalidParams.Code || !e.repo.since.IsZero() {
				t.Fatalf("minutes=%d 应 10001 且不查库：%v", n, err)
			}
		}
		e := newQueueEnv()
		if got, err := e.svc.Throughput(ctx, 1440); err != nil || len(got) != 1440 {
			t.Fatalf("1440 应放行：%d %v", len(got), err)
		}
		if got, err := e.svc.Throughput(ctx, 15); err != nil || len(got) != 15 {
			t.Fatalf("15 应放行：%d %v", len(got), err)
		}
	})

	t.Run("查库出错透传", func(t *testing.T) {
		e := newQueueEnv()
		e.repo.errs["Throughput"] = errBoom
		if _, err := e.svc.Throughput(ctx, 60); !errors.Is(err, errBoom) {
			t.Fatalf("%v", err)
		}
	})
}

// ---------- 调并发 ----------

func TestQueueAdmin_SetConcurrency(t *testing.T) {
	ctx := context.Background()
	const actor = uint64(9)

	t.Run("成功：写设置、本实例立即刷新并写心跳、写审计；已生效实例数按在线实例上报的并发统计", func(t *testing.T) {
		e := newQueueEnv()
		e.repo.instances = []model.WorkerInstance{
			{ID: "me", HeartbeatAt: e.now, StatsJSON: []byte(`{"inflight":0,"limit":20}`)},
			{ID: "other", HeartbeatAt: e.now.Add(-5 * time.Second), StatsJSON: []byte(`{"inflight":0,"limit":8}`)},
			{ID: "dead", HeartbeatAt: e.now.Add(-time.Minute), StatsJSON: []byte(`{"inflight":0,"limit":20}`)},
		}
		res, err := e.svc.SetConcurrency(ctx, actor, 20)
		if err != nil {
			t.Fatal(err)
		}
		if res.Concurrency != 20 || res.AppliedInstances != 1 || res.TotalInstances != 2 {
			t.Fatalf("结果不对（失联的不计入）：%+v", res)
		}
		if e.conc.saved != 20 || e.conc.by != actor || e.ctrl.refreshed != 1 || e.ctrl.beats != 1 {
			t.Fatalf("应保存、刷新、写心跳：%+v refreshed=%d beats=%d", e.conc, e.ctrl.refreshed, e.ctrl.beats)
		}
		if got := e.audit.actions(); len(got) != 1 || got[0] != model.AdminAuditQueueConcurrency || e.audit.logs[0].ActorID != actor {
			t.Fatalf("审计不对：%+v", e.audit.logs)
		}
	})

	t.Run("范围 1..硬上限：越界是参数错误，什么都不改", func(t *testing.T) {
		for _, n := range []int{-1, 0, 65} {
			e := newQueueEnv()
			_, err := e.svc.SetConcurrency(ctx, actor, n)
			if codeOf(err) != errcode.ErrInvalidParams.Code || e.conc.saved != 0 || e.ctrl.refreshed != 0 || len(e.audit.logs) != 0 {
				t.Fatalf("%d 应 10001 且不改任何东西：%v", n, err)
			}
		}
		e := newQueueEnv()
		if _, err := e.svc.SetConcurrency(ctx, actor, 64); err != nil {
			t.Fatalf("恰好等于硬上限应放行：%v", err)
		}
		if _, err := e.svc.SetConcurrency(ctx, actor, 1); err != nil {
			t.Fatalf("1 应放行：%v", err)
		}
	})

	t.Run("保存失败透传，不刷新、不写审计", func(t *testing.T) {
		e := newQueueEnv()
		e.conc.err = errBoom
		if _, err := e.svc.SetConcurrency(ctx, actor, 5); !errors.Is(err, errBoom) || e.ctrl.refreshed != 0 || len(e.audit.logs) != 0 {
			t.Fatalf("%v refreshed=%d audit=%d", err, e.ctrl.refreshed, len(e.audit.logs))
		}
	})

	t.Run("读实例列表失败透传（设置已保存）", func(t *testing.T) {
		e := newQueueEnv()
		e.repo.errs["ListInstances"] = errBoom
		if _, err := e.svc.SetConcurrency(ctx, actor, 5); !errors.Is(err, errBoom) {
			t.Fatalf("%v", err)
		}
		if e.conc.saved != 5 {
			t.Fatal("设置应已保存，只是统计实例失败")
		}
	})
}

// ---------- 暂停 / 恢复 ----------

func TestQueueAdmin_SetPaused(t *testing.T) {
	ctx := context.Background()
	const actor = uint64(9)

	t.Run("暂停：调用运行时、写审计，返回暂停状态", func(t *testing.T) {
		e := newQueueEnv()
		res, err := e.svc.SetPaused(ctx, actor, true)
		if err != nil || !res.Paused {
			t.Fatalf("%+v %v", res, err)
		}
		if e.ctrl.paused != 1 || e.ctrl.resumed != 0 {
			t.Fatalf("应只调用 Pause：%+v", e.ctrl)
		}
		if got := e.audit.actions(); len(got) != 1 || got[0] != model.AdminAuditQueuePause {
			t.Fatalf("审计不对：%v", got)
		}
	})

	t.Run("恢复：调用运行时、写审计", func(t *testing.T) {
		e := newQueueEnv()
		res, err := e.svc.SetPaused(ctx, actor, false)
		if err != nil || res.Paused {
			t.Fatalf("%+v %v", res, err)
		}
		if e.ctrl.resumed != 1 || e.ctrl.paused != 0 {
			t.Fatalf("应只调用 Resume：%+v", e.ctrl)
		}
		if got := e.audit.actions(); len(got) != 1 || got[0] != model.AdminAuditQueueResume {
			t.Fatalf("审计不对：%v", got)
		}
	})

	t.Run("运行时出错透传，不写审计", func(t *testing.T) {
		for _, paused := range []bool{true, false} {
			e := newQueueEnv()
			e.ctrl.errs["Pause"], e.ctrl.errs["Resume"] = errBoom, errBoom
			if _, err := e.svc.SetPaused(ctx, actor, paused); !errors.Is(err, errBoom) || len(e.audit.logs) != 0 {
				t.Fatalf("paused=%v %v audit=%d", paused, err, len(e.audit.logs))
			}
		}
	})
}

// 没有审计依赖（nil）时不报错，只是不写审计。
func TestQueueAdmin_NilAuditIsAllowed(t *testing.T) {
	e := newQueueEnv()
	svc := NewQueueAdminService(e.repo, e.channels, e.conc, e.ctrl, nil)
	if _, err := svc.SetPaused(context.Background(), 1, true); err != nil {
		t.Fatalf("%v", err)
	}
	if _, err := svc.SetConcurrency(context.Background(), 1, 3); err != nil {
		t.Fatalf("%v", err)
	}
}
