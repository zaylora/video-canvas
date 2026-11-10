package service_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"video-canvas/internal/drama"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/service"
	"video-canvas/internal/service/dramasegmentfake"
)

const adaptOrigin = "@[林阳](n_lin) 0-5秒：中景，按住石碑。"

// adaptSetup 建一个带原稿的片段，返回服务、仓储、转写模拟器和片段视图。
func adaptSetup(t *testing.T, prompt string) (*service.DramaSegmentService, *dramasegmentfake.Repo, *dramasegmentfake.Adapter, *model.DramaSegmentView) {
	t.Helper()
	r := dramasegmentfake.New()
	a := &dramasegmentfake.Adapter{Out: "integrated_multimodal_description: @[林阳](n_lin)"}
	s := service.NewDramaSegmentService(service.DramaSegmentDeps{Repo: r, Tasks: r, Assets: r, Broadcaster: r, Adapter: a})
	seconds := 8
	v, err := s.Create(context.Background(), 1, 7, 1, &model.CreateDramaSegmentReq{Prompt: prompt, Seconds: &seconds})
	if err != nil {
		t.Fatal(err)
	}
	return s, r, a, v
}

func TestDramaSegmentAdapt_SavesAdaptation(t *testing.T) {
	ctx := context.Background()
	s, r, a, v := adaptSetup(t, adaptOrigin)
	r.Messages = nil
	got, err := s.Adapt(ctx, 1, uint64(v.ID), &model.AdaptDramaSegmentReq{Format: "h3", Revision: v.Revision})
	if err != nil {
		t.Fatal(err)
	}
	ad, ok := got.Adaptations["h3"]
	if !ok || ad.Prompt != a.Out || ad.SourceHash != drama.SourceHash(adaptOrigin) || ad.Edited || ad.UpdatedAt.IsZero() {
		t.Fatalf("转写稿：%+v", got.Adaptations)
	}
	if got.Prompt != adaptOrigin || got.Revision != v.Revision+1 {
		t.Fatalf("原稿不能变、revision 加一：%+v", got)
	}
	if len(a.Calls) != 1 || a.Format[0] != "h3" || a.Calls[0].Source != adaptOrigin || a.Calls[0].Seconds != 8 {
		t.Fatalf("转写输入：%+v %v", a.Calls, a.Format)
	}
	if len(r.Messages) != 1 {
		t.Fatalf("转写完成应推送一次片段事件，实际 %d", len(r.Messages))
	}
	// 再转写另一种格式，两份并存
	got, err = s.Adapt(ctx, 1, uint64(v.ID), &model.AdaptDramaSegmentReq{Format: "generic", Revision: got.Revision})
	if err != nil || len(got.Adaptations) != 2 {
		t.Fatalf("两种格式应并存：%+v %v", got.Adaptations, err)
	}
	// 列表也带转写稿；没转写过的片段是空对象而不是 null
	empty, err := s.Create(ctx, 1, 7, 1, &model.CreateDramaSegmentReq{})
	if err != nil || empty.Adaptations == nil {
		t.Fatalf("新片段 adaptations 应为空对象：%+v %v", empty, err)
	}
}

func TestDramaSegmentAdapt_Errors(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		prompt string
		req    func(v *model.DramaSegmentView) *model.AdaptDramaSegmentReq
		user   uint64
		setup  func(a *dramasegmentfake.Adapter)
		code   int
	}{
		{"seedance 不需要转写", adaptOrigin, func(v *model.DramaSegmentView) *model.AdaptDramaSegmentReq {
			return &model.AdaptDramaSegmentReq{Format: "seedance", Revision: v.Revision}
		}, 1, nil, errcode.ErrDramaAdaptFormat.Code},
		{"原稿为空", "  ", func(v *model.DramaSegmentView) *model.AdaptDramaSegmentReq {
			return &model.AdaptDramaSegmentReq{Format: "h3", Revision: v.Revision}
		}, 1, nil, errcode.ErrDramaAdaptEmpty.Code},
		{"版本过期", adaptOrigin, func(v *model.DramaSegmentView) *model.AdaptDramaSegmentReq {
			return &model.AdaptDramaSegmentReq{Format: "h3", Revision: v.Revision + 5}
		}, 1, nil, errcode.ErrCanvasConflict.Code},
		{"别人的片段", adaptOrigin, func(v *model.DramaSegmentView) *model.AdaptDramaSegmentReq {
			return &model.AdaptDramaSegmentReq{Format: "h3", Revision: v.Revision}
		}, 2, nil, errcode.ErrDramaSegmentNotFound.Code},
		{"转写不合格", adaptOrigin, func(v *model.DramaSegmentView) *model.AdaptDramaSegmentReq {
			return &model.AdaptDramaSegmentReq{Format: "h3", Revision: v.Revision}
		}, 1, func(a *dramasegmentfake.Adapter) { a.Err = errcode.ErrDramaAdaptFailed }, errcode.ErrDramaAdaptFailed.Code},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, r, a, v := adaptSetup(t, tc.prompt)
			if tc.setup != nil {
				tc.setup(a)
			}
			before := r.Rows[0]
			_, err := s.Adapt(ctx, tc.user, uint64(v.ID), tc.req(v))
			segmentCode(t, err, tc.code)
			if r.Rows[0].Revision != before.Revision || string(r.Rows[0].Adaptations) != string(before.Adaptations) {
				t.Fatal("失败时不能写入片段")
			}
		})
	}
}

func TestDramaSegmentAdapt_FailureKeepsOldAdaptation(t *testing.T) {
	ctx := context.Background()
	s, _, a, v := adaptSetup(t, adaptOrigin)
	first, err := s.Adapt(ctx, 1, uint64(v.ID), &model.AdaptDramaSegmentReq{Format: "h3", Revision: v.Revision})
	if err != nil {
		t.Fatal(err)
	}
	a.Err = errcode.ErrDramaAdaptFailed
	if _, err = s.Adapt(ctx, 1, uint64(v.ID), &model.AdaptDramaSegmentReq{Format: "h3", Revision: first.Revision}); err == nil {
		t.Fatal("应失败")
	}
	views, err := s.List(ctx, 1, 7, 1)
	if err != nil || views[0].Adaptations["h3"].Prompt != a.Out {
		t.Fatalf("旧转写稿应保留：%+v %v", views, err)
	}
}

// 转写期间用户又改了原稿：结果照写，但指纹记的是转写时的原稿，前端会显示已过期。
func TestDramaSegmentAdapt_SourceChangedDuringAdapt(t *testing.T) {
	ctx := context.Background()
	s, r, a, v := adaptSetup(t, adaptOrigin)
	a.Mutate = func() {
		r.Rows[0].Prompt = adaptOrigin + "改"
		r.Rows[0].Revision++
	}
	got, err := s.Adapt(ctx, 1, uint64(v.ID), &model.AdaptDramaSegmentReq{Format: "h3", Revision: v.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if got.Prompt != adaptOrigin+"改" || got.Adaptations["h3"].SourceHash != drama.SourceHash(adaptOrigin) {
		t.Fatalf("应保留新原稿且指纹为转写时的原稿：%+v", got)
	}
}

func TestDramaSegmentAdapt_BusyWhileAdapting(t *testing.T) {
	ctx := context.Background()
	s, _, a, v := adaptSetup(t, adaptOrigin)
	a.Gate, a.Entered = make(chan struct{}), make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		_, err := s.Adapt(ctx, 1, uint64(v.ID), &model.AdaptDramaSegmentReq{Format: "h3", Revision: v.Revision})
		done <- err
	}()
	select {
	case <-a.Entered:
	case <-time.After(2 * time.Second):
		t.Fatal("第一次转写没有开始")
	}
	_, err := s.Adapt(ctx, 1, uint64(v.ID), &model.AdaptDramaSegmentReq{Format: "h3", Revision: v.Revision})
	segmentCode(t, err, errcode.ErrDramaAdaptBusy.Code)
	close(a.Gate)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestDramaSegmentUpdate_EditAdaptation(t *testing.T) {
	ctx := context.Background()
	s, _, _, v := adaptSetup(t, adaptOrigin)
	_, err := s.Update(ctx, 1, uint64(v.ID), &model.UpdateDramaSegmentReq{Revision: v.Revision, Adaptation: &model.DramaSegmentAdaptationEdit{Format: "h3", Prompt: "手改"}})
	segmentCode(t, err, errcode.ErrDramaAdaptMissing.Code)
	got, err := s.Adapt(ctx, 1, uint64(v.ID), &model.AdaptDramaSegmentReq{Format: "h3", Revision: v.Revision})
	if err != nil {
		t.Fatal(err)
	}
	hash := got.Adaptations["h3"].SourceHash
	got, err = s.Update(ctx, 1, uint64(v.ID), &model.UpdateDramaSegmentReq{Revision: got.Revision, Adaptation: &model.DramaSegmentAdaptationEdit{Format: "h3", Prompt: "手改"}})
	if err != nil {
		t.Fatal(err)
	}
	ad := got.Adaptations["h3"]
	if ad.Prompt != "手改" || !ad.Edited || ad.SourceHash != hash || got.Prompt != adaptOrigin {
		t.Fatalf("手改转写稿：%+v", got)
	}
	// 重新转写清掉手改标记
	got, err = s.Adapt(ctx, 1, uint64(v.ID), &model.AdaptDramaSegmentReq{Format: "h3", Revision: got.Revision})
	if err != nil || got.Adaptations["h3"].Edited {
		t.Fatalf("重新转写后 edited 应为 false：%+v %v", got, err)
	}
}

func TestDramaServiceAdaptSegment(t *testing.T) {
	ctx := context.Background()
	source := "@[林阳](n_lin) 0-5秒：中景，按住石碑。"
	good := `{"prompt":"integrated_multimodal_description: [Shot 1] @[林阳](n_lin) presses the stele.\n\noverall_soundscape: hum.\n\nnon_diegetic_music: No non-diegetic music."}`
	lostRef := `{"prompt":"integrated_multimodal_description: Lin presses the stele.\n\noverall_soundscape: hum.\n\nnon_diegetic_music: none."}`
	in := drama.AdaptInput{Source: source, Seconds: 5, Ratio: "9:16"}

	t.Run("第一次不合格带反馈重写一次", func(t *testing.T) {
		r := newDramaMemory()
		e := &dramaTextStreamer{outputs: []string{lostRef, good}}
		s, _ := dramaTestService(r, e, nil)
		out, err := s.AdaptSegment(ctx, &r.project, "h3", in)
		if err != nil || len(e.prompts) != 2 {
			t.Fatalf("out=%q err=%v calls=%d", out, err, len(e.prompts))
		}
		if !strings.Contains(e.prompts[1], "丢了原稿里的引用") || !strings.Contains(e.prompts[0], `"style":"写实"`) {
			t.Fatalf("第二次应带校验反馈、提示词应带画风：\n%s", e.prompts[1])
		}
		if !strings.Contains(out, "@[林阳](n_lin)") {
			t.Fatalf("out=%q", out)
		}
	})
	t.Run("两次都不合格", func(t *testing.T) {
		r := newDramaMemory()
		s, _ := dramaTestService(r, &dramaTextStreamer{outputs: []string{lostRef, lostRef}}, nil)
		_, err := s.AdaptSegment(ctx, &r.project, "h3", in)
		segmentCode(t, err, errcode.ErrDramaAdaptFailed.Code)
	})
	t.Run("上游失败", func(t *testing.T) {
		r := newDramaMemory()
		s, _ := dramaTestService(r, &dramaTextStreamer{failAt: 1}, nil)
		_, err := s.AdaptSegment(ctx, &r.project, "generic", in)
		segmentCode(t, err, errcode.ErrDramaAdaptFailed.Code)
	})
	t.Run("没有文本模型", func(t *testing.T) {
		r := newDramaMemory()
		s, registry := dramaTestService(r, &dramaTextStreamer{}, nil)
		registry.models = nil
		_, err := s.AdaptSegment(ctx, &r.project, "h3", in)
		segmentCode(t, err, errcode.ErrDramaNoTextModel.Code)
	})
	t.Run("不扣积分", func(t *testing.T) {
		r := newDramaMemory()
		s, _ := dramaTestService(r, &dramaTextStreamer{outputs: []string{good}}, nil)
		if _, err := s.AdaptSegment(ctx, &r.project, "h3", in); err != nil {
			t.Fatal(err)
		}
		if r.credit.Balance != 1000 || len(r.ledger) != 0 {
			t.Fatalf("转写不应扣积分：%+v %+v", r.credit, r.ledger)
		}
	})
}
