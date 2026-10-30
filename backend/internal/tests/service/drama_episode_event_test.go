package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/ws"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

type dramaEpisodeEventRepo struct {
	*dramaMemory
	inTx      bool
	failReady bool
}

func (r *dramaEpisodeEventRepo) WithTx(ctx context.Context, fn func(repository.DramaTx) error) error {
	r.inTx = true
	defer func() { r.inTx = false }()
	return r.dramaMemory.WithTx(ctx, func(repository.DramaTx) error { return fn(r) })
}

func (r *dramaEpisodeEventRepo) SaveEpisode(ctx context.Context, ep *model.DramaEpisode, revision int64) error {
	if r.failReady && ep.ScriptStatus == "ready" {
		return errors.New("模拟保存失败")
	}
	return r.dramaMemory.SaveEpisode(ctx, ep, revision)
}

type dramaEpisodeBroadcast struct {
	publish func(string, ws.Message)
}

func (b dramaEpisodeBroadcast) Publish(_ context.Context, channel string, msg ws.Message) {
	b.publish(channel, msg)
}

func TestDramaService_ScriptEpisodeEvents(t *testing.T) {
	for _, tc := range []struct {
		name       string
		count      int
		failAt     int
		failSave   bool
		wantEvents int
	}{
		{name: "每集保存后在下一集请求前通知", count: 2, wantEvents: 2},
		{name: "二十集不受百分比合并限制", count: 20, wantEvents: 20},
		{name: "后续失败保留成功集并通知失败状态", count: 2, failAt: 2, wantEvents: 2},
		{name: "事务失败不通知", count: 1, failSave: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &dramaEpisodeEventRepo{dramaMemory: newDramaMemory(), failReady: tc.failSave}
			e := &dramaTextStreamer{failAt: tc.failAt}
			for no := 1; no <= tc.count; no++ {
				dramaAddEpisode(r.dramaMemory, no, "")
				e.outputs = append(e.outputs, `{"script_md":"这一集的正文"}`)
			}
			_, registry := dramaTestService(r.dramaMemory, e, nil)
			var notified []int
			e.onCall = func() {
				if len(notified) != e.calls-1 {
					t.Fatalf("第 %d 次请求开始前仅收到 %d 集通知", e.calls, len(notified))
				}
			}
			bc := dramaEpisodeBroadcast{publish: func(channel string, msg ws.Message) {
				if msg.Type != "drama.episode" {
					return
				}
				if r.inTx {
					t.Fatal("事务提交前发送通知")
				}
				data, err := json.Marshal(msg.Data)
				if err != nil {
					t.Fatal(err)
				}
				var event struct {
					DramaID   string `json:"drama_id"`
					EpisodeNo int    `json:"episode_no"`
				}
				if err := json.Unmarshal(data, &event); err != nil {
					t.Fatal(err)
				}
				if channel != ws.UserChannel(7) || event.DramaID != idcodec.Encode(1) {
					t.Fatalf("事件归属错误：%s %s", channel, data)
				}
				if event.EpisodeNo != len(notified)+1 || r.episodes[event.EpisodeNo-1].ScriptStatus == "empty" {
					t.Fatalf("通知时集尚未保存或集号错误：%s", data)
				}
				notified = append(notified, event.EpisodeNo)
			}}
			s := service.NewDramaService(service.DramaDeps{
				Repo: r, Registry: registry, Streamer: e,
				Secrets:     dramaSecrets{model.ChannelSecretName("chan"): "sk-test"},
				Broadcaster: bc,
			})
			ctx := context.Background()
			job, err := s.CreateJob(ctx, 7, 1, &model.DramaJobReq{Step: "scripts"})
			if err != nil {
				t.Fatal(err)
			}
			if err := s.HandleDrama(ctx, 7, 1, uint64(job.ID)); err != nil {
				t.Fatal(err)
			}
			if len(notified) != tc.wantEvents {
				t.Fatalf("期望 %d 次分集通知，实际 %v", tc.wantEvents, notified)
			}
		})
	}
}
