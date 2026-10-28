package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"video-canvas/internal/model"
)

const storyboardValid = `{"segments":[{"seconds":15,"prompt":"@[村口](n_scene) @[陌生人](unknown)","shots":[{"seconds":15,"shot_size":"全景","camera":"平视固定","action":"女子走入村口","dialogue":""}]}]}`

func storyboardRequest(t *testing.T, maxSeconds int, episodes []int) *model.DramaJobReq {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"step": "storyboard", "episodes": episodes, "max_seconds": maxSeconds})
	var req model.DramaJobReq
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatal(err)
	}
	return &req
}

func storyboardReady(r *dramaMemory) {
	dramaAddEpisode(r, 1, "女子进入村口，与陌生人争执。")
	now := time.Now()
	r.episodes[0].LockedAt = &now
	r.project.Ratio = "9:16"
}

func TestDramaStoryboardPipeline(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		outputs              []string
		status               string
		code, calls, balance int
	}{
		{"成功", []string{storyboardValid}, "succeeded", 0, 1, 995},
		{"非法JSON重试", []string{`{bad`, storyboardValid}, "succeeded", 0, 2, 995},
		{"连续非法退款", []string{`{bad`, `{bad`}, "failed", 67008, 2, 1000},
		{"时长超限退款", []string{strings.ReplaceAll(storyboardValid, `"seconds":15`, `"seconds":16`), strings.ReplaceAll(storyboardValid, `"seconds":15`, `"seconds":16`)}, "failed", 67008, 2, 1000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newDramaMemory()
			storyboardReady(r)
			e := &dramaTextStreamer{outputs: tc.outputs}
			s, _ := dramaTestService(r, e, nil)
			job, err := s.CreateJob(context.Background(), 7, 1, storyboardRequest(t, 0, []int{1}))
			if err != nil {
				t.Fatal(err)
			}
			if r.episodes[0].StoryboardStatus != "generating" {
				t.Fatalf("queued status=%s", r.episodes[0].StoryboardStatus)
			}
			if err = s.HandleDrama(context.Background(), 7, 1, uint64(job.ID)); err != nil {
				t.Fatal(err)
			}
			if r.jobs[0].Status != tc.status || r.jobs[0].ErrorCode != tc.code || e.calls != tc.calls {
				t.Fatalf("job=%+v calls=%d", r.jobs[0], e.calls)
			}
			if r.credit.Balance != tc.balance || r.credit.Frozen != 0 {
				t.Fatalf("credit=%+v", r.credit)
			}
			wantStatus := "failed"
			if tc.status == "succeeded" {
				wantStatus = "ready"
			}
			if r.episodes[0].StoryboardStatus != wantStatus {
				t.Fatalf("episode=%+v", r.episodes[0])
			}
			if tc.status == "succeeded" {
				if len(r.segments) != 1 || r.segments[0].Seconds != 15 || r.segments[0].Seq != 0 || r.segments[0].Revision != 1 {
					t.Fatalf("segments=%+v", r.segments)
				}
				if strings.Contains(r.segments[0].Prompt, "unknown") || !strings.Contains(r.segments[0].Prompt, "@[村口](n_scene)") {
					t.Fatalf("saved prompt=%s", r.segments[0].Prompt)
				}
				if strings.Contains(e.prompts[0], "n_other") || strings.Contains(e.prompts[0], "不应输入") {
					t.Fatal("混入其他集资产")
				}
				if !strings.Contains(string(r.jobs[0].Output), "陌生人") || strings.Contains(string(r.jobs[0].Output), "unknown") {
					t.Fatalf("引用未降级: %s", r.jobs[0].Output)
				}
				for _, required := range []string{"n_scene", "村口", "9:16", "写实", "max_seconds"} {
					if !strings.Contains(e.prompts[0], required) {
						t.Fatalf("输入缺少%s: %s", required, e.prompts[0])
					}
				}
			}
		})
	}
}

func TestDramaStoryboardPrerequisitesAndBounds(t *testing.T) {
	for _, tc := range []struct {
		name          string
		max           int
		scope         []int
		locked, ready bool
		code          int
	}{
		{"未拆解", 15, []int{1}, false, true, 67006},
		{"剧本未就绪", 15, []int{1}, true, false, 67006},
		{"未指定单集", 15, nil, true, true, 10001},
		{"多集", 15, []int{1, 2}, true, true, 10001},
		{"低于上限边界", 3, []int{1}, true, true, 10001},
		{"超过上限边界", 61, []int{1}, true, true, 10001},
		{"默认上限", 0, []int{1}, true, true, 0},
		{"最小上限", 4, []int{1}, true, true, 0},
		{"最大上限", 60, []int{1}, true, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newDramaMemory()
			storyboardReady(r)
			if !tc.locked {
				r.episodes[0].LockedAt = nil
			}
			if !tc.ready {
				r.episodes[0].ScriptStatus = "empty"
			}
			s, _ := dramaTestService(r, &dramaTextStreamer{}, nil)
			req := storyboardRequest(t, tc.max, tc.scope)
			_, err := s.Estimate(context.Background(), 7, 1, req)
			if tc.code == 0 {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				dramaCheckCode(t, err, tc.code)
			}
			_, err = s.CreateJob(context.Background(), 7, 1, req)
			if tc.code == 0 {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				dramaCheckCode(t, err, tc.code)
			}
		})
	}
}

func TestDramaStoryboardHasTakes(t *testing.T) {
	t.Run("估价及提交", func(t *testing.T) {
		r := newDramaMemory()
		storyboardReady(r)
		r.hasTakes = true
		s, _ := dramaTestService(r, &dramaTextStreamer{}, nil)
		req := storyboardRequest(t, 15, []int{1})
		_, err := s.Estimate(context.Background(), 7, 1, req)
		dramaCheckCode(t, err, 67013)
		_, err = s.CreateJob(context.Background(), 7, 1, req)
		dramaCheckCode(t, err, 67013)
		if len(r.jobs) != 0 || r.credit.Frozen != 0 {
			t.Fatalf("拒绝仍冻结: %+v", r.credit)
		}
	})
	t.Run("模型执行期间新建take", func(t *testing.T) {
		r := newDramaMemory()
		storyboardReady(r)
		r.segments = []model.DramaSegment{{BaseModel: model.BaseModel{ID: 99}, UserID: 7, EpisodeID: r.episodes[0].ID, Prompt: "旧片段", Seconds: 5, Revision: 1}}
		e := &dramaTextStreamer{outputs: []string{storyboardValid}, onCall: func() { r.hasTakes = true }}
		s, _ := dramaTestService(r, e, nil)
		j, err := s.CreateJob(context.Background(), 7, 1, storyboardRequest(t, 15, []int{1}))
		if err != nil {
			t.Fatal(err)
		}
		if err = s.HandleDrama(context.Background(), 7, 1, uint64(j.ID)); err != nil {
			t.Fatal(err)
		}
		if r.jobs[0].Status != "failed" || r.jobs[0].ErrorCode != 67013 || r.credit.Balance != 1000 || r.credit.Frozen != 0 {
			t.Fatalf("job=%+v credit=%+v", r.jobs[0], r.credit)
		}
		if len(r.segments) != 1 || r.segments[0].ID != 99 || r.segments[0].Prompt != "旧片段" {
			t.Fatalf("已有片段被覆盖: %+v", r.segments)
		}
	})
}

func TestDramaStoryboardCancelAndUpstreamFailure(t *testing.T) {
	for _, tc := range []struct {
		name        string
		cancel, old bool
		want        string
	}{{"取消空分镜", true, false, "empty"}, {"取消已有分镜", true, true, "ready"}, {"上游失败退款", false, false, "failed"}} {
		t.Run(tc.name, func(t *testing.T) {
			r := newDramaMemory()
			storyboardReady(r)
			if tc.old {
				r.segments = []model.DramaSegment{{BaseModel: model.BaseModel{ID: 99}, UserID: 7, EpisodeID: r.episodes[0].ID, Prompt: "旧片段", Seconds: 5, Revision: 1}}
			}
			e := &dramaTextStreamer{outputs: []string{storyboardValid}}
			if !tc.cancel {
				e.failAt = 1
			}
			s, _ := dramaTestService(r, e, nil)
			j, err := s.CreateJob(context.Background(), 7, 1, storyboardRequest(t, 15, []int{1}))
			if err != nil {
				t.Fatal(err)
			}
			if tc.cancel {
				e.onCall = func() {
					if _, err := s.CancelJob(context.Background(), 7, 1, uint64(j.ID)); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err = s.HandleDrama(context.Background(), 7, 1, uint64(j.ID)); err != nil {
				t.Fatal(err)
			}
			if r.credit.Balance != 1000 || r.credit.Frozen != 0 || r.episodes[0].StoryboardStatus != tc.want {
				t.Fatalf("credit=%+v ep=%+v", r.credit, r.episodes[0])
			}
			if tc.cancel && r.jobs[0].Status != "canceled" {
				t.Fatalf("job=%+v", r.jobs[0])
			}
			if tc.old && (len(r.segments) != 1 || r.segments[0].ID != 99) {
				t.Fatalf("取消改了片段: %+v", r.segments)
			}
		})
	}
}

func TestDramaStoryboardExactDurationAndSummary(t *testing.T) {
	for _, seconds := range []int{4, 15, 60} {
		t.Run(fmt.Sprintf("%d秒", seconds), func(t *testing.T) {
			r := newDramaMemory()
			storyboardReady(r)
			e := &dramaTextStreamer{outputs: []string{strings.ReplaceAll(storyboardValid, `"seconds":15`, fmt.Sprintf(`"seconds":%d`, seconds))}}
			s, _ := dramaTestService(r, e, nil)
			j, err := s.CreateJob(context.Background(), 7, 1, storyboardRequest(t, seconds, []int{1}))
			if err != nil {
				t.Fatal(err)
			}
			if err = s.HandleDrama(context.Background(), 7, 1, uint64(j.ID)); err != nil {
				t.Fatal(err)
			}
			if len(r.segments) != 1 {
				t.Fatalf("segments=%+v", r.segments)
			}
			task := uint64(77)
			r.segments[0].ChosenTaskID = &task
			view, err := s.Get(context.Background(), 7, 1)
			if err != nil {
				t.Fatal(err)
			}
			ep, err := s.GetEpisode(context.Background(), 7, 1, 1)
			if err != nil {
				t.Fatal(err)
			}
			for _, summary := range []model.DramaEpisodeSummary{view.Episodes[0], ep.DramaEpisodeSummary} {
				if summary.StoryboardStatus != "ready" || summary.SegmentCount != 1 || summary.SegmentSeconds != seconds || summary.DoneSegmentCount != 1 {
					t.Fatalf("summary=%+v", summary)
				}
			}
		})
	}
}

func TestDramaStoryboardAtomicReplacementRetry(t *testing.T) {
	r := newDramaMemory()
	storyboardReady(r)
	r.segments = []model.DramaSegment{{BaseModel: model.BaseModel{ID: 99}, UserID: 7, EpisodeID: r.episodes[0].ID, Prompt: "旧片段", Seconds: 5, Revision: 1}}
	failure := errors.New("写片段失败")
	r.replaceErr = failure
	e := &dramaTextStreamer{outputs: []string{storyboardValid, storyboardValid}}
	s, _ := dramaTestService(r, e, nil)
	j, err := s.CreateJob(context.Background(), 7, 1, storyboardRequest(t, 15, []int{1}))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.HandleDrama(context.Background(), 7, 1, uint64(j.ID)); !errors.Is(err, failure) {
		t.Fatalf("want repository failure, got %v", err)
	}
	if len(r.segments) != 1 || r.segments[0].ID != 99 || r.episodes[0].StoryboardStatus != "generating" || r.credit.Balance != 1000 || r.credit.Frozen != 5 {
		t.Fatalf("非原子变更: segments=%+v ep=%+v credit=%+v", r.segments, r.episodes[0], r.credit)
	}
	r.replaceErr = nil
	if err = s.HandleDrama(context.Background(), 7, 1, uint64(j.ID)); err != nil {
		t.Fatal(err)
	}
	if len(r.segments) != 1 || r.segments[0].ID == 99 || r.episodes[0].StoryboardStatus != "ready" || r.credit.Balance != 995 || r.credit.Frozen != 0 {
		t.Fatalf("恢复未完成: segments=%+v ep=%+v credit=%+v", r.segments, r.episodes[0], r.credit)
	}
}
