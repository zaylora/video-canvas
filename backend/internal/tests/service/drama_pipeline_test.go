package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"video-canvas/internal/drama"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

type dramaMemory struct {
	repository.DramaTx
	project  model.DramaProject
	episodes []model.DramaEpisode
	jobs     []model.DramaJob
	credit   model.UserCredit
	ledger   []model.CreditLedger
	nextID   uint64
}

func newDramaMemory() *dramaMemory {
	return &dramaMemory{project: model.DramaProject{BaseModel: model.BaseModel{ID: 1}, UserID: 7, CanvasID: 2, Title: "测试", SourceText: "第一章\n小说正文", SourceKind: "novel", SourceSummary: "原文摘要", StyleKey: "custom", StyleCustom: "写实", Revision: 1}, credit: model.UserCredit{UserID: 7, Balance: 1000}, nextID: 10}
}
func (r *dramaMemory) WithTx(ctx context.Context, fn func(repository.DramaTx) error) error {
	before := *r
	before.episodes = append([]model.DramaEpisode{}, r.episodes...)
	before.jobs = append([]model.DramaJob{}, r.jobs...)
	before.ledger = append([]model.CreditLedger{}, r.ledger...)
	if err := fn(r); err != nil {
		*r = before
		return err
	}
	return nil
}
func (r *dramaMemory) List(_ context.Context, userID uint64, _ string, _, _ int) ([]model.DramaProject, int64, error) {
	if userID != r.project.UserID {
		return []model.DramaProject{}, 0, nil
	}
	return []model.DramaProject{r.project}, 1, nil
}
func (r *dramaMemory) Project(_ context.Context, userID, id uint64, _ bool) (*model.DramaProject, error) {
	if r.project.UserID != userID || r.project.ID != id || r.project.DeletedAt.Valid {
		return nil, repository.ErrNotFound
	}
	p := r.project
	return &p, nil
}
func (r *dramaMemory) InsertProject(_ context.Context, p *model.DramaProject) error {
	p.ID = r.nextID
	r.nextID++
	p.CanvasID = r.nextID
	r.nextID++
	p.CreatedAt = time.Now()
	p.UpdatedAt = p.CreatedAt
	r.project = *p
	return nil
}
func (r *dramaMemory) UpdateProject(_ context.Context, userID, id uint64, revision int64, fields map[string]any) error {
	if userID != r.project.UserID || id != r.project.ID {
		return repository.ErrNotFound
	}
	if revision != r.project.Revision {
		return repository.ErrStateConflict
	}
	for k, v := range fields {
		switch k {
		case "source_kind":
			r.project.SourceKind = v.(string)
		case "source_summary":
			r.project.SourceSummary = v.(string)
		case "characters_md":
			r.project.CharactersMD = v.(string)
		case "outline_md":
			r.project.OutlineMD = v.(string)
		case "status":
			r.project.Status = v.(string)
		case "ratio":
			r.project.Ratio = v.(string)
		case "title":
			r.project.Title = v.(string)
		case "text_model":
			r.project.TextModel = v.(string)
		}
	}
	r.project.Revision++
	return nil
}
func (r *dramaMemory) DeleteProject(_ context.Context, _, _ uint64) error {
	r.project.DeletedAt.Valid = true
	return nil
}
func (r *dramaMemory) Episodes(_ context.Context, userID, id uint64) ([]model.DramaEpisode, error) {
	if userID != r.project.UserID || id != r.project.ID {
		return nil, repository.ErrNotFound
	}
	return append([]model.DramaEpisode{}, r.episodes...), nil
}
func (r *dramaMemory) SaveEpisode(_ context.Context, e *model.DramaEpisode, revision int64) error {
	for i := range r.episodes {
		if r.episodes[i].ID == e.ID && e.ID != 0 {
			if r.episodes[i].Revision != revision {
				return repository.ErrStateConflict
			}
			e.Revision++
			r.episodes[i] = *e
			return nil
		}
	}
	r.nextID++
	e.ID = r.nextID
	r.episodes = append(r.episodes, *e)
	return nil
}
func (r *dramaMemory) Jobs(_ context.Context, userID, id uint64, active bool) ([]model.DramaJob, error) {
	if userID != r.project.UserID || id != r.project.ID {
		return nil, repository.ErrNotFound
	}
	out := []model.DramaJob{}
	for _, j := range r.jobs {
		if !active || j.Status == "queued" || j.Status == "running" {
			out = append(out, j)
		}
	}
	return out, nil
}
func (r *dramaMemory) Job(_ context.Context, userID, id, jobID uint64, _ bool) (*model.DramaJob, error) {
	if userID != r.project.UserID || id != r.project.ID {
		return nil, repository.ErrNotFound
	}
	for _, j := range r.jobs {
		if j.ID == jobID {
			return &j, nil
		}
	}
	return nil, repository.ErrNotFound
}
func (r *dramaMemory) InsertJob(_ context.Context, j *model.DramaJob) error {
	for _, old := range r.jobs {
		if old.Status == "queued" || old.Status == "running" {
			return repository.ErrDuplicate
		}
	}
	r.nextID++
	j.ID = r.nextID
	j.CreatedAt = time.Now()
	r.jobs = append(r.jobs, *j)
	return nil
}
func (r *dramaMemory) UpdateJob(_ context.Context, userID, id uint64, fields map[string]any) error {
	for i := range r.jobs {
		j := &r.jobs[i]
		if j.ID != id || j.UserID != userID {
			continue
		}
		for k, v := range fields {
			switch k {
			case "status":
				j.Status = v.(string)
			case "progress":
				j.Progress = v.(int)
			case "credits":
				j.Credits = v.(int)
			case "error_code":
				j.ErrorCode = v.(int)
			case "error_message":
				j.ErrorMessage = v.(string)
			case "finished_at":
				now := v.(time.Time)
				j.FinishedAt = &now
			case "started_at":
				now := v.(time.Time)
				j.StartedAt = &now
			case "output":
				j.Output = v.([]byte)
			case "input_json":
				j.InputJSON = v.([]byte)
			}
		}
		return nil
	}
	return repository.ErrNotFound
}
func (r *dramaMemory) EnsureCredit(context.Context, uint64, int) error { return nil }
func (r *dramaMemory) LockCredit(context.Context, uint64) (*model.UserCredit, error) {
	c := r.credit
	return &c, nil
}
func (r *dramaMemory) AddCredit(_ context.Context, _ uint64, balance, frozen int) error {
	r.credit.Balance += balance
	r.credit.Frozen += frozen
	return nil
}
func (r *dramaMemory) InsertLedger(_ context.Context, e *model.CreditLedger) (bool, error) {
	for _, old := range r.ledger {
		if old.DramaJobID != nil && e.DramaJobID != nil && *old.DramaJobID == *e.DramaJobID && old.Type == e.Type {
			return false, nil
		}
	}
	r.ledger = append(r.ledger, *e)
	return true, nil
}

type dramaRegistry struct {
	snap        *provider.Snapshot
	models      []provider.ModelInfo
	unavailable map[string]bool
	requested   []string
}

func (r *dramaRegistry) ListModels(context.Context, string) ([]provider.ModelInfo, error) {
	return r.models, nil
}
func (r *dramaRegistry) Snapshot(_ context.Context, key string) (*provider.Snapshot, error) {
	r.requested = append(r.requested, key)
	if r.unavailable[key] {
		return nil, provider.ErrModelUnavailable
	}
	return r.snap, nil
}

type dramaTextExecutor struct {
	provider.Executor
	outputs []string
	prompts []string
	calls   int
	failAt  int
}

func (e *dramaTextExecutor) Submit(_ context.Context, _ *provider.Snapshot, in provider.SubmitInput) (*provider.SubmitResult, error) {
	e.calls++
	e.prompts = append(e.prompts, in.Input["prompt"].(string))
	if e.calls == e.failAt {
		return nil, errors.New("上游失败")
	}
	if len(e.outputs) == 0 {
		return nil, errors.New("测试未提供输出")
	}
	text := e.outputs[0]
	e.outputs = e.outputs[1:]
	return &provider.SubmitResult{Immediate: &provider.QueryResult{Status: provider.StatusSucceeded, Outputs: []provider.Output{{Type: provider.OutputText, Text: text}}}}, nil
}

type dramaPipelineCanvas struct {
	applied  int
	prompted int
	created  []service.DramaCanvasAsset
}

func (c *dramaPipelineCanvas) ApplyBreakdown(context.Context, uint64, uint64, uint64, uint64, []int, []drama.Asset) ([]service.DramaCanvasAsset, error) {
	c.applied++
	return c.created, nil
}
func (c *dramaPipelineCanvas) ApplyPrompts(context.Context, uint64, uint64, uint64, []drama.AssetPrompt) error {
	c.prompted++
	return nil
}
func dramaTestService(r *dramaMemory, e *dramaTextExecutor, c *dramaPipelineCanvas) (*service.DramaService, *dramaRegistry) {
	snap := &provider.Snapshot{ModelRevisionID: 9, Model: provider.ModelSnapshot{Key: "text", Kind: "text", Pricing: modelcfg.Pricing{Billing: modelcfg.BillingPerCall, Unit: 5}, Capabilities: modelcfg.Capabilities{Prompt: modelcfg.PromptSpec{MaxLength: 100000}, Context: &modelcfg.ContextSpec{Window: 100000, Output: 5000}}}}
	registry := &dramaRegistry{snap: snap, models: []provider.ModelInfo{{Key: "text", Kind: "text"}}}
	return service.NewDramaService(service.DramaDeps{Repo: r, Registry: registry, Executor: e, Canvas: c}), registry
}
func dramaCheckCode(t *testing.T, err error, code int) {
	t.Helper()
	var business *errcode.Error
	if !errors.As(err, &business) || business.Code != code {
		t.Fatalf("want code %d, got %v", code, err)
	}
}
func dramaAddEpisode(r *dramaMemory, no int, script string) {
	r.nextID++
	status := "empty"
	if script != "" {
		status = "ready"
	}
	r.episodes = append(r.episodes, model.DramaEpisode{BaseModel: model.BaseModel{ID: r.nextID}, UserID: 7, DramaID: 1, No: no, Title: fmt.Sprintf("第%d集", no), ScriptMD: script, ScriptStatus: status, Revision: 1})
}

func TestDramaPipelineJSONRetryAndNoCharge(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		outputs              []string
		status               string
		code, calls, balance int
	}{{"成功", []string{`{"script_md":"剧本正文"}`}, "succeeded", 0, 1, 995}, {"JSON重试成功", []string{`{bad`, `{"script_md":"修正后的正文"}`}, "succeeded", 0, 2, 995}, {"JSON两次失败", []string{`{bad`, `{bad`}, "failed", 67008, 2, 1000}} {
		t.Run(tc.name, func(t *testing.T) {
			r := newDramaMemory()
			dramaAddEpisode(r, 1, "")
			e := &dramaTextExecutor{outputs: tc.outputs}
			s, _ := dramaTestService(r, e, nil)
			j, err := s.CreateJob(context.Background(), 7, 1, &model.DramaJobReq{Step: "scripts", Episodes: []int{1}})
			if err != nil {
				t.Fatal(err)
			}
			if err := s.HandleDrama(context.Background(), 7, 1, uint64(j.ID)); err != nil {
				t.Fatal(err)
			}
			got := r.jobs[0]
			if got.Status != tc.status || got.ErrorCode != tc.code || e.calls != tc.calls {
				t.Fatalf("job=%+v calls=%d", got, e.calls)
			}
			if r.credit.Balance != tc.balance || r.credit.Frozen != 0 {
				t.Fatalf("account=%+v", r.credit)
			}
			if tc.calls == 2 && !strings.Contains(e.prompts[1], "validation_error") {
				t.Fatal("重试缺少校验反馈")
			}
		})
	}
}
func TestDramaJobPrerequisitesBusyAndIsolation(t *testing.T) {
	r := newDramaMemory()
	s, registry := dramaTestService(r, &dramaTextExecutor{}, nil)
	_, err := s.CreateJob(context.Background(), 7, 1, &model.DramaJobReq{Step: "scripts"})
	dramaCheckCode(t, err, 67006)
	_, err = s.Get(context.Background(), 8, 1)
	dramaCheckCode(t, err, 67001)
	dramaAddEpisode(r, 1, "")
	_, err = s.CreateJob(context.Background(), 7, 1, &model.DramaJobReq{Step: "breakdown", Episodes: []int{1}})
	dramaCheckCode(t, err, 67006)
	j, err := s.CreateJob(context.Background(), 7, 1, &model.DramaJobReq{Step: "scripts"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateJob(context.Background(), 7, 1, &model.DramaJobReq{Step: "scripts"})
	dramaCheckCode(t, err, 67004)
	_, err = s.CancelJob(context.Background(), 8, 1, uint64(j.ID))
	dramaCheckCode(t, err, 67001)
	if _, err = s.CancelJob(context.Background(), 7, 1, uint64(j.ID)); err != nil {
		t.Fatal(err)
	}
	if r.credit.Frozen != 0 || r.credit.Balance != 1000 {
		t.Fatalf("cancel account=%+v", r.credit)
	}
	registry.models = nil
	_, err = s.Estimate(context.Background(), 7, 1, &model.DramaJobReq{Step: "scripts"})
	dramaCheckCode(t, err, 67007)
}
func TestDramaEpisodeLockedAndRevision(t *testing.T) {
	r := newDramaMemory()
	dramaAddEpisode(r, 1, "正文")
	now := time.Now()
	r.episodes[0].LockedAt = &now
	s, _ := dramaTestService(r, &dramaTextExecutor{}, nil)
	text := "修改"
	for _, req := range []*model.UpdateDramaEpisodeReq{{Revision: 1, ScriptMD: &text}, {Revision: 1, Synopsis: &text}} {
		_, err := s.UpdateEpisode(context.Background(), 7, 1, 1, req)
		dramaCheckCode(t, err, 67005)
	}
	_, err := s.UpdateEpisode(context.Background(), 7, 1, 1, &model.UpdateDramaEpisodeReq{Revision: 2, Title: &text})
	dramaCheckCode(t, err, 30002)
	view, err := s.UpdateEpisode(context.Background(), 7, 1, 1, &model.UpdateDramaEpisodeReq{Revision: 1, Title: &text})
	if err != nil {
		t.Fatal(err)
	}
	if view.Title != text || view.Revision != 2 || !view.Locked {
		t.Fatalf("view=%+v", view)
	}
	_, err = s.UpdateEpisode(context.Background(), 8, 1, 1, &model.UpdateDramaEpisodeReq{Revision: 2, Title: &text})
	dramaCheckCode(t, err, 67001)
}
func TestDramaScriptsSerialContinuationAndPartialSuccess(t *testing.T) {
	r := newDramaMemory()
	dramaAddEpisode(r, 1, "")
	dramaAddEpisode(r, 2, "")
	dramaAddEpisode(r, 3, "")
	ending := strings.Repeat("甲", 900) + "最后一句"
	raw, _ := json.Marshal(drama.ScriptOutput{ScriptMD: ending})
	e := &dramaTextExecutor{outputs: []string{string(raw)}, failAt: 2}
	s, _ := dramaTestService(r, e, nil)
	j, err := s.CreateJob(context.Background(), 7, 1, &model.DramaJobReq{Step: "scripts"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.HandleDrama(context.Background(), 7, 1, uint64(j.ID)); err != nil {
		t.Fatal(err)
	}
	if r.episodes[0].ScriptStatus != "ready" || r.episodes[0].ScriptMD != ending || r.episodes[1].ScriptStatus != "failed" || r.episodes[2].ScriptStatus != "empty" {
		t.Fatalf("episodes=%+v", r.episodes)
	}
	if !strings.Contains(e.prompts[1], "最后一句") || strings.Contains(e.prompts[1], strings.Repeat("甲", 900)) {
		t.Fatal("衔接没有限定为上一集结尾约800字")
	}
	if r.credit.Balance != 1000 || r.credit.Frozen != 0 {
		t.Fatalf("failed job charged %+v", r.credit)
	}
}
func TestDramaAnalyzeAutomaticallyOutlinesMarkedSource(t *testing.T) {
	r := newDramaMemory()
	r.project.SourceText = "第一集 开端\n原文甲\n第2集 继续\n原文乙"
	r.project.SourceSummary = ""
	e := &dramaTextExecutor{outputs: []string{`{"source_kind":"script","summary":"故事摘要","characters_md":"人物设定"}`}}
	s, _ := dramaTestService(r, e, nil)
	j, err := s.CreateJob(context.Background(), 7, 1, &model.DramaJobReq{Step: "analyze"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.HandleDrama(context.Background(), 7, 1, uint64(j.ID)); err != nil {
		t.Fatal(err)
	}
	if len(r.jobs) != 2 || r.jobs[1].Step != "outline" {
		t.Fatalf("jobs=%+v", r.jobs)
	}
	if err = s.HandleDrama(context.Background(), 7, 1, r.jobs[1].ID); err != nil {
		t.Fatal(err)
	}
	if e.calls != 1 || len(r.episodes) != 2 || r.episodes[1].ScriptStatus != "ready" || !strings.Contains(r.episodes[1].ScriptMD, "原文乙") {
		t.Fatalf("calls=%d eps=%+v", e.calls, r.episodes)
	}
	if r.jobs[1].Credits != 0 {
		t.Fatal("原文捷径不应扣文本模型费用")
	}
}
func TestDramaModelFallbackAndEstimate(t *testing.T) {
	r := newDramaMemory()
	r.project.TextModel = "disabled"
	dramaAddEpisode(r, 1, "")
	dramaAddEpisode(r, 2, "")
	s, registry := dramaTestService(r, &dramaTextExecutor{}, nil)
	registry.models = []provider.ModelInfo{{Key: "unavailable", Kind: "text"}, {Key: "text", Kind: "text"}}
	registry.unavailable = map[string]bool{"unavailable": true}
	credits, err := s.Estimate(context.Background(), 7, 1, &model.DramaJobReq{Step: "scripts"})
	if err != nil {
		t.Fatal(err)
	}
	if credits != 10 || len(registry.requested) != 2 || registry.requested[1] != "text" {
		t.Fatalf("estimate=%d requested=%v", credits, registry.requested)
	}
	r.credit.Balance = 0
	_, err = s.CreateJob(context.Background(), 7, 1, &model.DramaJobReq{Step: "scripts"})
	dramaCheckCode(t, err, 40001)
	if len(r.jobs) != 0 {
		t.Fatal("余额不足不得入队")
	}
}
func TestDramaBreakdownAutomaticallyPromptsNewAssets(t *testing.T) {
	r := newDramaMemory()
	r.project.Ratio = "9:16"
	dramaAddEpisode(r, 1, "正文")
	asset := drama.Asset{Type: "character", Name: "林川", Aliases: []string{}, Episodes: []int{1}, Description: "人物", VoicePrompt: "低沉"}
	raw, _ := json.Marshal(drama.BreakdownOutput{Assets: []drama.Asset{asset}})
	prompts, _ := json.Marshal(drama.AssetPromptsOutput{Prompts: []drama.AssetPrompt{{NodeID: "new", Prompt: "角色正向描述"}}})
	e := &dramaTextExecutor{outputs: []string{string(raw), string(prompts)}}
	c := &dramaPipelineCanvas{created: []service.DramaCanvasAsset{{NodeID: "new", Asset: asset}}}
	s, _ := dramaTestService(r, e, c)
	j, err := s.CreateJob(context.Background(), 7, 1, &model.DramaJobReq{Step: "breakdown", Episodes: []int{1}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.HandleDrama(context.Background(), 7, 1, uint64(j.ID)); err != nil {
		t.Fatal(err)
	}
	if c.applied != 1 || len(r.jobs) != 2 || r.jobs[1].Step != "asset_prompts" {
		t.Fatalf("canvas=%+v jobs=%+v", c, r.jobs)
	}
	if err = s.HandleDrama(context.Background(), 7, 1, r.jobs[1].ID); err != nil {
		t.Fatal(err)
	}
	if c.prompted != 1 || r.jobs[1].Status != "succeeded" {
		t.Fatalf("canvas=%+v jobs=%+v", c, r.jobs)
	}
}
