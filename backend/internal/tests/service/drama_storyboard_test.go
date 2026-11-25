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

const storyboardValid = `{"segments":[{"seconds":15,"prompt":"@[村口](n_scene) @[陌生人](unknown)"}]}`

// storyboardDraft 是创作段的自由文本输出，不带任何栏名、秒数或 JSON。
const storyboardDraft = "村口，女子抬头说「你来了」，与陌生人争执。"

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
	over := strings.ReplaceAll(storyboardValid, `"seconds":15`, `"seconds":16`)
	// 每次调用计费 5；失败的尝试不计费，所以成功的只有创作段一次、格式段一次。格式段重试不重跑创作段
	for _, tc := range []struct {
		name                 string
		outputs              []string
		status               string
		code, calls, balance int
	}{
		{"成功", []string{storyboardDraft, storyboardValid}, "succeeded", 0, 2, 990},
		{"格式段非法JSON重试", []string{storyboardDraft, `{bad`, storyboardValid}, "succeeded", 0, 3, 990},
		{"创作稿空白重试", []string{" ", storyboardDraft, storyboardValid}, "succeeded", 0, 3, 990},
		{"创作稿连续为空退款", []string{" ", ""}, "failed", 67008, 2, 1000},
		{"格式段连续非法退款", []string{storyboardDraft, `{bad`, `{bad`}, "failed", 67008, 3, 1000},
		{"时长超限退款", []string{storyboardDraft, over, over}, "failed", 67008, 3, 1000},
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
				for _, prompt := range e.prompts {
					if strings.Contains(prompt, "n_other") || strings.Contains(prompt, "不应输入") {
						t.Fatal("混入其他集资产")
					}
					if strings.Contains(prompt, "n_orphan") {
						t.Fatal("混入未分类资产（出现集数为 null）")
					}
				}
				// 创作段拿剧本与风格；格式段拿创作稿，不再拿剧本全文
				if !strings.Contains(e.prompts[0], "女子进入村口") || strings.Contains(e.prompts[0], "segments") {
					t.Fatalf("创作段输入不对: %s", e.prompts[0])
				}
				// 格式段永远是最后一次调用（创作段重试时前面还有草稿）
				format := e.prompts[len(e.prompts)-1]
				if !strings.Contains(format, storyboardDraft) || !strings.Contains(format, "segments") || strings.Contains(format, "女子进入村口") {
					t.Fatalf("格式段应只拿创作稿: %s", format)
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

func TestDramaStoryboardLegacyAssetNameFallback(t *testing.T) {
	r := newDramaMemory()
	storyboardReady(r)
	// 旧画布角色资产把名字放在 label/character.name，drama.name 为空；分镜必须能把它当具名资产引用。
	r.canvas = []byte(`{"nodes":[{"id":"n_scene","data":{"drama":{"type":"scene","name":"村口","look":"夜","episodes":[]},"params":{"prompt":"夜间村口灯光"}}},{"id":"role_label","data":{"kind":"character","label":"林子若","character":{"name":""},"drama":{"type":"character","episodes":[]},"params":{"prompt":"栗色长发"}}},{"id":"role_character","data":{"kind":"character","label":"","character":{"name":"黄毛"},"drama":{"type":"character","episodes":[]},"params":{"prompt":"黄发"}}}],"edges":[]}`)
	draft := "村口，林子若与黄毛争执。"
	valid := `{"segments":[{"seconds":15,"prompt":"@[村口](n_scene) @[林子若](role_label) @[黄毛](role_character)"}]}`
	e := &dramaTextStreamer{outputs: []string{draft, valid}}
	s, _ := dramaTestService(r, e, nil)
	job, err := s.CreateJob(context.Background(), 7, 1, storyboardRequest(t, 0, []int{1}))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.HandleDrama(context.Background(), 7, 1, uint64(job.ID)); err != nil {
		t.Fatal(err)
	}
	if len(r.segments) != 1 || !strings.Contains(r.segments[0].Prompt, "@[林子若](role_label)") || !strings.Contains(r.segments[0].Prompt, "@[黄毛](role_character)") {
		t.Fatalf("旧资产引用未保留: %+v", r.segments)
	}
	for _, prompt := range e.prompts {
		for _, want := range []string{`"name":"林子若"`, `"name":"黄毛"`} {
			if !strings.Contains(prompt, want) {
				t.Fatalf("资产清单缺少%s: %s", want, prompt)
			}
		}
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
		e := &dramaTextStreamer{outputs: []string{storyboardDraft, storyboardValid}, onCall: func() { r.hasTakes = true }}
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
			e := &dramaTextStreamer{outputs: []string{storyboardDraft, strings.ReplaceAll(storyboardValid, `"seconds":15`, fmt.Sprintf(`"seconds":%d`, seconds))}}
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
	// 两次执行各跑创作段与格式段
	e := &dramaTextStreamer{outputs: []string{storyboardDraft, storyboardValid, storyboardDraft, storyboardValid}}
	s, _ := dramaTestService(r, e, nil)
	j, err := s.CreateJob(context.Background(), 7, 1, storyboardRequest(t, 15, []int{1}))
	if err != nil {
		t.Fatal(err)
	}
	frozen := r.credit.Frozen
	if err = s.HandleDrama(context.Background(), 7, 1, uint64(j.ID)); !errors.Is(err, failure) {
		t.Fatalf("want repository failure, got %v", err)
	}
	if len(r.segments) != 1 || r.segments[0].ID != 99 || r.episodes[0].StoryboardStatus != "generating" || r.credit.Balance != 1000 || r.credit.Frozen != frozen {
		t.Fatalf("非原子变更: segments=%+v ep=%+v credit=%+v", r.segments, r.episodes[0], r.credit)
	}
	r.replaceErr = nil
	if err = s.HandleDrama(context.Background(), 7, 1, uint64(j.ID)); err != nil {
		t.Fatal(err)
	}
	if len(r.segments) != 1 || r.segments[0].ID == 99 || r.episodes[0].StoryboardStatus != "ready" || r.credit.Balance != 990 || r.credit.Frozen != 0 {
		t.Fatalf("恢复未完成: segments=%+v ep=%+v credit=%+v", r.segments, r.episodes[0], r.credit)
	}
}

// 分镜成功落库后，把本集每个片段的故事板节点在画布上建好（空节点，进画布就能看到）；
// 画布没写成不影响分镜成功；分镜失败时不动画布。
func TestDramaStoryboardSyncsBoardNodes(t *testing.T) {
	for _, tc := range []struct {
		name       string
		outputs    []string
		syncErr    error
		wantStatus string
		wantSync   bool
	}{
		{"成功：同步本集故事板", []string{storyboardDraft, storyboardValid}, nil, "succeeded", true},
		{"画布写失败：只记日志，分镜仍然成功", []string{storyboardDraft, storyboardValid}, errors.New("画布冲突"), "succeeded", true},
		{"分镜失败：不动画布", []string{" ", ""}, nil, "failed", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newDramaMemory()
			storyboardReady(r)
			canvas := &dramaPipelineCanvas{mem: r, syncErr: tc.syncErr}
			s, _ := dramaTestService(r, &dramaTextStreamer{outputs: tc.outputs}, canvas)
			job, err := s.CreateJob(context.Background(), 7, 1, storyboardRequest(t, 0, []int{1}))
			if err != nil {
				t.Fatal(err)
			}
			if err = s.HandleDrama(context.Background(), 7, 1, uint64(job.ID)); err != nil {
				t.Fatal(err)
			}
			if r.jobs[0].Status != tc.wantStatus {
				t.Fatalf("job=%+v", r.jobs[0])
			}
			if !tc.wantSync {
				if len(canvas.synced) != 0 {
					t.Fatalf("不该同步：%+v", canvas.synced)
				}
				return
			}
			if len(canvas.synced) != 1 {
				t.Fatalf("应同步一次：%+v", canvas.synced)
			}
			got := canvas.synced[0]
			if got.episodeNo != 1 || got.canvasID != r.project.CanvasID || len(got.segments) != 1 {
				t.Fatalf("同步参数不对：%+v", got)
			}
			seg := got.segments[0]
			if seg.Seq != 0 || seg.Label != "故事板 · 第 1 集 · 01" || seg.ID == "" || seg.NewID == "" || seg.SourceHash == "" || seg.Layout != "2x2" || seg.ShotCount != 0 {
				t.Fatalf("片段信息不对：%+v", seg)
			}
		})
	}
}
