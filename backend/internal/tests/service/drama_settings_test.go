package service_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"video-canvas/internal/drama"
	"video-canvas/internal/model"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

func seedDramaDraft(r *dramaMemory, text string) string {
	if r.drafts == nil {
		r.drafts = map[string]model.DramaSourceDraft{}
	}
	r.drafts["draft"] = model.DramaSourceDraft{ID: "draft", UserID: 7, Text: text, Filename: "原文.txt", ExpiresAt: time.Now().Add(time.Hour)}
	return "draft"
}
func (r *dramaMemory) CreateDraft(_ context.Context, d *model.DramaSourceDraft) error {
	if r.drafts == nil {
		r.drafts = map[string]model.DramaSourceDraft{}
	}
	r.drafts[d.ID] = *d
	return nil
}
func (r *dramaMemory) Draft(_ context.Context, user uint64, token string, _ bool) (*model.DramaSourceDraft, error) {
	d, ok := r.drafts[token]
	if !ok || d.UserID != user || !d.ExpiresAt.After(time.Now()) {
		return nil, repository.ErrNotFound
	}
	return &d, nil
}
func (r *dramaMemory) DeleteDraft(_ context.Context, _ uint64, token string) error {
	delete(r.drafts, token)
	return nil
}
func (r *dramaMemory) InsertUnits(_ context.Context, units []model.DramaUnit) error {
	r.units = append(r.units, units...)
	return nil
}
func (r *dramaMemory) Units(_ context.Context, user, id uint64, from, to int) ([]model.DramaUnit, error) {
	out := []model.DramaUnit{}
	for _, u := range r.units {
		if u.UserID == user && u.DramaID == id && u.No >= from && (to == 0 || u.No <= to) {
			out = append(out, u)
		}
	}
	return out, nil
}
func (r *dramaMemory) Canvas(ctx context.Context, user, id uint64) (*model.CanvasProject, error) {
	return r.GetByID(ctx, user, id)
}
func (r *dramaMemory) Prompts(_ context.Context, user uint64, step string) ([]model.DramaPrompt, error) {
	out := []model.DramaPrompt{}
	for _, p := range r.prompts {
		if p.UserID == user && (step == "" || p.Step == step) {
			out = append(out, p)
		}
	}
	return out, nil
}
func (r *dramaMemory) Prompt(_ context.Context, user, id uint64) (*model.DramaPrompt, error) {
	p, ok := r.prompts[id]
	if !ok || p.UserID != user {
		return nil, repository.ErrNotFound
	}
	return &p, nil
}
func (r *dramaMemory) SavePrompt(_ context.Context, p *model.DramaPrompt) error {
	if r.prompts == nil {
		r.prompts = map[uint64]model.DramaPrompt{}
	}
	if p.ID == 0 {
		p.ID = uint64(len(r.prompts) + 1)
	}
	r.prompts[p.ID] = *p
	return nil
}
func (r *dramaMemory) DeletePrompt(_ context.Context, _ uint64, id uint64) error {
	delete(r.prompts, id)
	return nil
}

func TestDramaPlanningReadsFullBatchAndAppends(t *testing.T) {
	r := newDramaMemory()
	source := "第1章 开始\n" + strings.Repeat("必须保留全文。", 2000) + "\n第2章 结尾\n最后一段原文。"
	seedDramaDraft(r, source)
	e := &dramaTextStreamer{outputs: []string{
		`{"source_kind":"script","summary":"梗概","characters_md":"人物","outline_md":"后文为抽样推断"}`,
		`{"episodes":[{"title":"开始","synopsis":"梗概","hook":"钩子","cliffhanger":"悬念","unit_from":1,"unit_to":3}]}`,
	}}
	s, _ := dramaTestService(r, e, nil)
	v, err := s.Create(context.Background(), 7, &model.CreateDramaReq{SourceToken: "draft", SourceKind: "novel", Ratio: "9:16", StyleKey: "custom", StyleCustom: "写实"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.HandleDrama(context.Background(), 7, uint64(v.ID), r.jobs[0].ID); err != nil {
		t.Fatal(err)
	}
	if r.project.SourceKind != "novel" || len(r.jobs) != 2 || r.jobs[1].Step != "plan" {
		t.Fatalf("%+v %+v", r.project, r.jobs)
	}
	var vars map[string]any
	if err = json.Unmarshal(r.jobs[1].InputJSON, &vars); err != nil {
		t.Fatal(err)
	}
	for _, u := range r.units {
		if !strings.Contains(vars["source"].(string), u.Text) {
			t.Fatal("规划缺少批内完整原文")
		}
	}
	if err = s.HandleDrama(context.Background(), 7, uint64(v.ID), r.jobs[1].ID); err != nil {
		t.Fatal(err)
	}
	if r.project.Phase != "done" || r.project.PlanCursor != 3 || len(r.episodes) != 1 || r.episodes[0].ScriptMD != "" {
		t.Fatalf("phase=%s cursor=%d units=%d episodes=%d jobs=%v", r.project.Phase, r.project.PlanCursor, r.project.UnitsTotal, len(r.episodes), func() []string {
			out := []string{}
			for _, j := range r.jobs {
				out = append(out, j.Step+":"+j.Status+":"+j.ErrorMessage)
			}
			return out
		}())
	}
}

func TestDramaPipelineAndUserCanOverlap(t *testing.T) {
	r := newDramaMemory()
	r.project.Phase = "running"
	r.jobs = []model.DramaJob{{BaseModel: model.BaseModel{ID: 2}, UserID: 7, DramaID: 1, Step: "plan", Lane: "pipeline", Status: "running"}}
	dramaAddEpisode(r, 1, "")
	s, _ := dramaTestService(r, &dramaTextStreamer{}, nil)
	if _, err := s.CreateJob(context.Background(), 7, 1, &model.DramaJobReq{Step: "scripts", Episodes: []int{1}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateJob(context.Background(), 7, 1, &model.DramaJobReq{Step: "breakdown", Episodes: []int{1}}); err == nil {
		t.Fatal("用户文本任务应互斥")
	}
	if _, err := s.CreateEpisode(context.Background(), 7, 1, &model.CreateDramaEpisodeReq{}); err == nil {
		t.Fatal("规划中不能新增集")
	}
	if err := s.DeleteEpisode(context.Background(), 7, 1, 1); err == nil {
		t.Fatal("规划中不能删集")
	}
}

func TestDramaScriptsUseUnitAndPreviousSynopsis(t *testing.T) {
	r := newDramaMemory()
	dramaAddEpisode(r, 1, "")
	dramaAddEpisode(r, 2, "")
	r.episodes[0].Synopsis = "上一集梗概"
	r.episodes[0].Cliffhanger = "上一集悬念"
	r.episodes[1].UnitFrom = 1
	r.episodes[1].UnitTo = 1
	r.episodes[1].Hook = "开场钩子"
	r.units = []model.DramaUnit{{UserID: 7, DramaID: 1, No: 1, Text: "本集必须出现的原文对白", Chars: 12}}
	e := &dramaTextStreamer{outputs: []string{`{"script_md":"本集剧本"}`}}
	s, _ := dramaTestService(r, e, nil)
	j, err := s.CreateJob(context.Background(), 7, 1, &model.DramaJobReq{Step: "scripts", Episodes: []int{2}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.HandleDrama(context.Background(), 7, 1, uint64(j.ID)); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"本集必须出现的原文对白", "上一集梗概", "上一集悬念", "开场钩子"} {
		if !strings.Contains(e.prompts[0], text) {
			t.Fatalf("缺少 %s", text)
		}
	}
}

func TestDramaPromptSnapshotAndCustomFailure(t *testing.T) {
	r := newDramaMemory()
	r.project.Phase = "paused"
	r.project.PlanCursor = 0
	r.project.RangeEnd = 1
	r.project.UnitsTotal = 1
	r.units = []model.DramaUnit{{UserID: 7, DramaID: 1, No: 1, Title: "第一章", Text: "正文", Chars: 2}}
	e := &dramaTextStreamer{outputs: []string{"Markdown", "Markdown"}}
	s, _ := dramaTestService(r, e, nil)
	prompt, err := s.SavePrompt(context.Background(), 7, 0, &model.SaveDramaPromptReq{Step: "adapt", Name: "我的", Bodies: map[string]string{"plan": "只输出 Markdown"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Update(context.Background(), 7, 1, &model.UpdateDramaReq{Revision: r.project.Revision, Prompts: map[string]model.DramaPromptChoice{"adapt": {Source: "mine", ID: prompt.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Planning(context.Background(), 7, 1, "resume", &model.DramaPlanningReq{})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DeletePrompt(context.Background(), 7, uint64(prompt.ID)); err != nil {
		t.Fatal(err)
	}
	// 项目更新不得影响已经排队的正文。
	_, err = s.Update(context.Background(), 7, 1, &model.UpdateDramaReq{Revision: r.project.Revision, Prompts: map[string]model.DramaPromptChoice{"adapt": {Source: "builtin"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.HandleDrama(context.Background(), 7, 1, r.jobs[0].ID); err != nil {
		t.Fatal(err)
	}
	if r.project.Phase != "paused" || r.project.PauseCode != "custom_prompt" || r.jobs[0].ErrorCode != 67015 || !strings.Contains(e.prompts[0], "只输出 Markdown") {
		t.Fatalf("%+v %+v", r.project, r.jobs)
	}
	// 恢复内置后重新入队同一批，已失败任务的历史快照保持不变。
	e.outputs = []string{`{"episodes":[{"title":"首集","synopsis":"梗概","hook":"开场","cliffhanger":"结尾","unit_from":1,"unit_to":1}]}`}
	if _, err = s.Planning(context.Background(), 7, 1, "retry-builtin", &model.DramaPlanningReq{Step: "adapt"}); err != nil {
		t.Fatal(err)
	}
	if len(r.jobs) != 2 {
		t.Fatalf("重试应产生新任务: %d", len(r.jobs))
	}
	if err = s.HandleDrama(context.Background(), 7, 1, r.jobs[1].ID); err != nil {
		t.Fatal(err)
	}
	if r.project.Phase != "done" || r.jobs[1].Status != "succeeded" {
		t.Fatal("内置重试没有完成")
	}
}

func TestDramaContextGate(t *testing.T) {
	r := newDramaMemory()
	seedDramaDraft(r, "原文")
	s, reg := dramaTestService(r, &dramaTextStreamer{}, nil)
	reg.snap.Model.Capabilities.Context.Window = 127999
	_, err := s.Create(context.Background(), 7, &model.CreateDramaReq{SourceToken: "draft", SourceKind: "novel", StyleKey: "custom", StyleCustom: "写实"})
	dramaCheckCode(t, err, 67022)
	key := "text"
	_, err = s.Update(context.Background(), 7, 1, &model.UpdateDramaReq{Revision: 1, TextModel: &key})
	dramaCheckCode(t, err, 67022)
	_, err = s.EstimateSource(context.Background(), 7, "draft", &model.DramaSourceEstimateReq{SourceKind: "novel"})
	dramaCheckCode(t, err, 67022)
}

func TestDramaPlanningPauseRangeAndCredits(t *testing.T) {
	r := newDramaMemory()
	r.project.Phase = "done"
	r.project.UnitsTotal = 2
	r.project.RangeEnd = 1
	r.project.PlanCursor = 1
	r.units = []model.DramaUnit{{UserID: 7, DramaID: 1, No: 1, Title: "第一章", Text: "正文", Chars: 2}, {UserID: 7, DramaID: 1, No: 2, Title: "第二章", Text: "后文", Chars: 2}}
	r.credit.Balance = 0
	s, _ := dramaTestService(r, &dramaTextStreamer{}, nil)
	v, err := s.Planning(context.Background(), 7, 1, "range", &model.DramaPlanningReq{RangeEnd: 2})
	if err != nil || v.Phase != "paused" || v.PauseCode != "credits" || v.RangeEnd != 2 {
		t.Fatalf("%+v %v", v, err)
	}
	r.credit.Balance = 100
	v, err = s.Planning(context.Background(), 7, 1, "resume", &model.DramaPlanningReq{})
	if err != nil || v.Phase != "running" || len(r.jobs) != 1 {
		t.Fatalf("%+v %v", v, err)
	}
	v, err = s.Planning(context.Background(), 7, 1, "pause", &model.DramaPlanningReq{})
	if err != nil || v.PauseCode != "user" || r.jobs[0].Status != "queued" {
		t.Fatalf("%+v %v", v, err)
	}
}

func TestDramaPlanningContinuesWhileUserJobQueued(t *testing.T) {
	r := newDramaMemory()
	r.project.Phase = "paused"
	r.project.UnitsTotal = 13
	r.project.RangeEnd = 13
	r.project.EpisodeSeconds = 60
	for i := 1; i <= 13; i++ {
		r.units = append(r.units, model.DramaUnit{UserID: 7, DramaID: 1, No: i, Title: "章节", Text: strings.Repeat("文", 2500), Chars: 2500})
	}
	e := &dramaTextStreamer{}
	s, _ := dramaTestService(r, e, nil)
	if _, err := s.Planning(context.Background(), 7, 1, "resume", &model.DramaPlanningReq{}); err != nil {
		t.Fatal(err)
	}
	runPlan := func(index int) {
		j := r.jobs[index]
		raw, _ := json.Marshal(drama.PlanOutput{Episodes: []drama.PlanEpisode{{Title: "本批", Synopsis: "梗概", Hook: "钩子", Cliffhanger: "悬念", UnitFrom: j.BatchFrom, UnitTo: j.BatchTo}}})
		e.outputs = []string{string(raw)}
		if err := s.HandleDrama(context.Background(), 7, 1, j.ID); err != nil {
			t.Fatal(err)
		}
	}
	runPlan(0)
	if len(r.jobs) != 2 {
		t.Fatalf("没有下一批: %d", len(r.jobs))
	}
	if _, err := s.CreateJob(context.Background(), 7, 1, &model.DramaJobReq{Step: "scripts", Episodes: []int{1}}); err != nil {
		t.Fatal(err)
	}
	runPlan(1)
	if r.project.Phase != "done" || r.project.PlanCursor != 13 || r.jobs[2].Status != "queued" || len(r.episodes) != 2 {
		t.Fatalf("phase=%s cursor=%d eps=%d", r.project.Phase, r.project.PlanCursor, len(r.episodes))
	}
}

func TestDramaEstimateScriptGaps(t *testing.T) {
	r := newDramaMemory()
	for i := 1; i <= 5; i++ {
		dramaAddEpisode(r, i, "")
	}
	s, _ := dramaTestService(r, &dramaTextStreamer{}, nil)
	gaps, err := s.ScriptGaps(context.Background(), 7, 1, &model.DramaJobReq{Step: "scripts", Episodes: []int{3, 5}})
	if err != nil || len(gaps) != 2 || gaps[0] != 2 || gaps[1] != 4 {
		t.Fatalf("%v %v", gaps, err)
	}
}

func TestDramaScriptImportSkipsPlanAndScripts(t *testing.T) {
	for _, source := range []string{"第1集 开始\n甲：走。\n第2集 结束\n乙：好。", "1-1 日 内\n甲：走。\n1-2 夜 外\n乙：好。\n1-3 日 内\n甲：来。"} {
		r := newDramaMemory()
		seedDramaDraft(r, source)
		e := &dramaTextStreamer{outputs: []string{`{"source_kind":"script","summary":"梗概","characters_md":"人物","outline_md":"主线"}`}}
		s, _ := dramaTestService(r, e, nil)
		v, err := s.Create(context.Background(), 7, &model.CreateDramaReq{SourceToken: "draft", SourceKind: "script", RangeEnd: 1, StyleKey: "custom", StyleCustom: "写实"})
		if err != nil {
			t.Fatal(err)
		}
		if err = s.HandleDrama(context.Background(), 7, uint64(v.ID), r.jobs[0].ID); err != nil {
			t.Fatal(err)
		}
		if len(r.jobs) != 1 || r.project.Phase != "done" || len(r.episodes) == 0 {
			t.Fatalf("%+v %+v", r.project, r.jobs)
		}
		var b strings.Builder
		for _, ep := range r.episodes {
			b.WriteString(ep.ScriptMD)
		}
		if !strings.Contains(b.String(), "乙：好。") {
			t.Fatal("导入丢失台词")
		}
		_, err = s.CreateJob(context.Background(), 7, uint64(v.ID), &model.DramaJobReq{Step: "scripts", Episodes: []int{1}})
		dramaCheckCode(t, err, 67006)
	}
}

// 编译期保证替身覆盖预检及原文仓储，不依赖内嵌 nil 接口。
var _ service.DramaSettingsRepo = (*dramaMemory)(nil)
var _ = drama.MaxSourceRunes

func (r *dramaMemory) CleanupDrafts(context.Context) error { return nil }
func (r *dramaMemory) RecentDramaDurations(context.Context, uint64, string) ([]int, error) {
	return nil, nil
}

func (r *dramaMemory) SegmentTasks(context.Context, uint64, uint64, string) ([]model.GenerationTask, error) {
	return nil, nil
}

func (r *dramaMemory) RecentDramaBatches(ctx context.Context, user, id uint64) ([]model.DramaJob, error) {
	return r.Jobs(ctx, user, id, false)
}
