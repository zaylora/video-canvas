package agent_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"video-canvas/internal/agent/prompts"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	. "video-canvas/internal/service/agent"
)

type dramaFake struct {
	view     model.DramaView
	ep       model.DramaEpisodeView
	segments []model.DramaSegmentView
	writes   int
	busy     bool
	jobs     map[uint64]*model.DramaJobView
}

func (f *dramaFake) Get(_ context.Context, user, id uint64) (*model.DramaView, error) {
	if user != 1 || id != 10 {
		return nil, errcode.ErrDramaNotFound
	}
	v := f.view
	return &v, nil
}
func (f *dramaFake) GetEpisode(ctx context.Context, user, id uint64, no int) (*model.DramaEpisodeView, error) {
	if _, err := f.Get(ctx, user, id); err != nil {
		return nil, err
	}
	if no != 1 {
		return nil, errcode.ErrDramaNotFound
	}
	e := f.ep
	return &e, nil
}
func (f *dramaFake) Update(ctx context.Context, user, id uint64, req *model.UpdateDramaReq) (*model.DramaView, error) {
	if _, err := f.Get(ctx, user, id); err != nil {
		return nil, err
	}
	if req.Revision != f.view.Revision {
		return nil, errcode.ErrCanvasConflict
	}
	if req.OutlineMD != nil {
		f.view.OutlineMD = *req.OutlineMD
	}
	if req.CharactersMD != nil {
		f.view.CharactersMD = *req.CharactersMD
	}
	f.writes++
	f.view.Revision++
	return f.Get(ctx, user, id)
}
func (f *dramaFake) UpdateEpisode(ctx context.Context, user, id uint64, no int, req *model.UpdateDramaEpisodeReq) (*model.DramaEpisodeView, error) {
	if _, err := f.GetEpisode(ctx, user, id, no); err != nil {
		return nil, err
	}
	if req.Revision != f.ep.Revision {
		return nil, errcode.ErrCanvasConflict
	}
	if f.ep.Locked && (req.ScriptMD != nil || req.Synopsis != nil) {
		return nil, errcode.ErrDramaEpisodeLocked
	}
	if req.Title != nil {
		f.ep.Title = *req.Title
	}
	if req.Synopsis != nil {
		f.ep.Synopsis = *req.Synopsis
	}
	if req.ScriptMD != nil {
		f.ep.ScriptMD = *req.ScriptMD
	}
	f.writes++
	f.ep.Revision++
	return f.GetEpisode(ctx, user, id, no)
}
func (f *dramaFake) Estimate(ctx context.Context, user, id uint64, _ *model.DramaJobReq) (int, error) {
	_, err := f.Get(ctx, user, id)
	return 17, err
}
func (f *dramaFake) CreateAgentJob(ctx context.Context, user, id, approval uint64, req *model.DramaJobReq) (*model.DramaJobView, error) {
	if _, err := f.Get(ctx, user, id); err != nil {
		return nil, err
	}
	if j := f.jobs[approval]; j != nil {
		return j, nil
	}
	if f.busy {
		return nil, errcode.ErrDramaJobBusy
	}
	j := &model.DramaJobView{ID: 30, Step: req.Step, Status: "queued"}
	f.jobs[approval] = j
	f.writes++
	return j, nil
}
func (f *dramaFake) List(ctx context.Context, user, id uint64, no int) ([]model.DramaSegmentView, error) {
	if _, err := f.GetEpisode(ctx, user, id, no); err != nil {
		return nil, err
	}
	return slices.Clone(f.segments), nil
}
func (f *dramaFake) UpdateSegment(_ context.Context, user, id uint64, req *model.UpdateDramaSegmentReq) (*model.DramaSegmentView, error) {
	if user != 1 || id != 20 {
		return nil, errcode.ErrDramaSegmentNotFound
	}
	v := &f.segments[0]
	if v.Revision != req.Revision {
		return nil, errcode.ErrCanvasConflict
	}
	if req.Prompt != nil {
		v.Prompt = *req.Prompt
	}
	if req.Seconds != nil {
		v.Seconds = *req.Seconds
	}
	v.Revision++
	f.writes++
	return v, nil
}

type dramaSegmentFake struct{ f *dramaFake }

func (f dramaSegmentFake) List(ctx context.Context, user, id uint64, no int) ([]model.DramaSegmentView, error) {
	return f.f.List(ctx, user, id, no)
}
func (f dramaSegmentFake) Update(ctx context.Context, user, id uint64, req *model.UpdateDramaSegmentReq) (*model.DramaSegmentView, error) {
	return f.f.UpdateSegment(ctx, user, id, req)
}

func newDramaBridge(t *testing.T) (*AgentBridge, *dramaFake, *bridgeEnv) {
	t.Helper()
	e := newBridgeEnv(t)
	id := uint64(10)
	e.repo.canvas.DramaID = &id
	f := &dramaFake{view: model.DramaView{ID: idcodec.ID(id), CanvasID: 7, Title: "短剧", OutlineMD: "原大纲", Revision: 1, Episodes: []model.DramaEpisodeSummary{{No: 1}}}, ep: model.DramaEpisodeView{DramaEpisodeSummary: model.DramaEpisodeSummary{No: 1, Revision: 1}, ScriptMD: "原剧本"}, segments: []model.DramaSegmentView{{ID: 20, EpisodeNo: 1, Prompt: "原提示词", Seconds: 5, Revision: 1}}, jobs: map[uint64]*model.DramaJobView{}}
	br := NewAgentBridge(BridgeDeps{Repo: e.repo, Agent: e.svc, Canvas: NewAgentCanvasService(e.repo, e.bc), Registry: e.reg, Secrets: fakeSecrets{"channel:ch1": "sk-test"}, Streamer: e.str, RunUsage: &fakeRunUsage{bill: e.bill}, Drama: f, DramaSegments: dramaSegmentFake{f}, DramaApprovalKey: []byte("test-drama-signing-secret")})
	return br, f, e
}

func TestDrama_ModeAndPrompt(t *testing.T) {
	if !model.ValidAgentMode(model.AgentModeDrama) {
		t.Fatal("drama 模式不合法")
	}
	for _, mode := range []string{"all", "script", "storyboard", "prompt", "drama"} {
		for _, tool := range []string{"drama_read", "drama_update_outline", "drama_update_episode", "drama_update_segment", "drama_run_job"} {
			if slices.Contains(AgentToolsForMode(mode), tool) != (mode == "drama") {
				t.Errorf("mode=%s tool=%s", mode, tool)
			}
		}
	}
	for _, tool := range []string{"canvas_get_state", "canvas_apply_ops", "canvas_arrange", "canvas_delete", "generate_media"} {
		if !slices.Contains(AgentToolsForMode("drama"), tool) {
			t.Errorf("短剧画布工具缺少 %s", tool)
		}
	}
	for _, word := range []string{"drama_read", "锁定", "审批", "67005", "revision"} {
		if !strings.Contains(prompts.System("drama"), word) {
			t.Errorf("提示词缺少 %s", word)
		}
		if strings.Contains(prompts.System("all"), "drama_read") {
			t.Fatal("普通模式提示词发生变化")
		}
	}
}

func TestDrama_CheckCanvasAndPrepare(t *testing.T) {
	br, _, e := newDramaBridge(t)
	ctx := context.Background()
	if err := br.CheckCanvasMode(ctx, 1, 7, "drama"); err != nil {
		t.Fatal(err)
	}
	setup, err := br.PrepareRun(ctx, platformCtx(991, "drama"), PrepareRequest{})
	if err != nil || !strings.Contains(setup.SystemPrompt, "当前模式：短剧助手") {
		t.Fatalf("准备短剧运行 %+v %v", setup, err)
	}
	if !slices.Contains(toolNames(setup.Tools), "drama_read") {
		t.Fatal("运行没有短剧工具")
	}
	wantAgentCode(t, br.CheckCanvasMode(ctx, 2, 7, "drama"), errcode.ErrCanvasNotFound)
	wantAgentCode(t, br.CheckCanvasMode(ctx, 1, 7, "bad"), errcode.ErrInvalidParams)
	e.repo.canvas.DramaID = nil
	wantAgentCode(t, br.CheckCanvasMode(ctx, 1, 7, "drama"), errcode.ErrInvalidParams)
	_, err = br.PrepareRun(ctx, platformCtx(991, "drama"), PrepareRequest{})
	wantAgentCode(t, err, errcode.ErrInvalidParams)
}

func TestDrama_ReadAndApprovalWrites(t *testing.T) {
	cases := []struct{ kind, args string }{
		{"drama_update_outline", `{"outline_md":"新大纲","reason":"补全设定"}`},
		{"drama_update_episode", `{"episode_no":1,"script_md":"新剧本"}`},
		{"drama_update_segment", `{"segment_id":"` + idcodec.Encode(20) + `","prompt":"新提示词","seconds":8}`},
		{"drama_run_job", `{"step":"scripts","episodes":[1],"reason":"补全设定"}`},
	}
	for _, c := range cases {
		t.Run(c.kind, func(t *testing.T) {
			br, f, _ := newDramaBridge(t)
			ctx, rc := context.Background(), platformCtx(990, "drama")
			for _, scope := range []string{"overview", "episode", "segments"} {
				r, err := br.ExecutePlatformTool(ctx, rc, "r", "drama_read", []byte(`{"scope":"`+scope+`","episode_no":1}`))
				if err != nil || r.IsError {
					t.Fatalf("读取 %s: %+v %v", scope, r, err)
				}
			}
			p, err := br.ProposeApproval(ctx, rc, c.kind, []byte(c.args))
			if err != nil || p.IsError || !p.Terminate {
				t.Fatalf("审批: %+v %v", p, err)
			}
			if f.writes != 0 {
				t.Fatal("审批前发生写入")
			}
			var data map[string]any
			if json.Unmarshal(p.Payload, &data) != nil || data["before"] == "" || data["after"] == "" || data["title"] == "" {
				t.Fatalf("缺少前后摘要: %s", p.Payload)
			}
			if (c.kind == "drama_update_outline" || c.kind == "drama_run_job") && data["reason"] != "补全设定" {
				t.Fatalf("理由未带入审批: %s", p.Payload)
			}
			if c.kind == "drama_run_job" && (p.QuoteCredits != 17 || !strings.Contains(p.Summary, "17")) {
				t.Fatalf("缺少估价 %+v", p)
			}
			o, err := br.ResolveApproval(ctx, rc, c.kind, p.Payload, model.DecideAgentApprovalReq{Decision: "approve"})
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				r, err := br.ExecuteApproval(ctx, rc, c.kind, 123, "write", *o)
				if err != nil || r.Status != ExecExecuted {
					t.Fatalf("执行 %+v %v", r, err)
				}
			}
			if f.writes != 1 {
				t.Fatalf("审批重试写入 %d 次", f.writes)
			}
			var tampered map[string]any
			_ = json.Unmarshal(p.Payload, &tampered)
			tampered["after"] = "绕过审批"
			raw, _ := json.Marshal(tampered)
			if _, err := br.ResolveApproval(ctx, rc, c.kind, raw, model.DecideAgentApprovalReq{Decision: "approve"}); err == nil {
				t.Fatal("篡改审批成功")
			}
		})
	}
}

func TestDrama_BoundariesAndServiceErrors(t *testing.T) {
	br, f, e := newDramaBridge(t)
	ctx, rc := context.Background(), platformCtx(990, "drama")
	f.ep.Locked = true
	p, err := br.ProposeApproval(ctx, rc, "drama_update_episode", []byte(`{"episode_no":1,"synopsis":"新梗概"}`))
	if err != nil || !p.IsError || !strings.Contains(p.Content, errcode.ErrDramaEpisodeLocked.Msg) {
		t.Fatalf("锁定 %+v %v", p, err)
	}
	p, err = br.ProposeApproval(ctx, rc, "drama_update_segment", []byte(`{"segment_id":"`+idcodec.Encode(99)+`","prompt":"x"}`))
	if err != nil || !p.IsError {
		t.Fatalf("其他项目片段 %+v %v", p, err)
	}
	other := rc
	other.UserID = 2
	r, err := br.ExecutePlatformTool(ctx, other, "r", "drama_read", []byte(`{"scope":"overview"}`))
	if err != nil || !r.IsError {
		t.Fatalf("其他用户 %+v %v", r, err)
	}
	normal := rc
	normal.Mode = "all"
	r, err = br.ExecutePlatformTool(ctx, normal, "r", "drama_read", []byte(`{"scope":"overview"}`))
	if err != nil || !r.IsError {
		t.Fatalf("普通模式 %+v %v", r, err)
	}
	p, _ = br.ProposeApproval(ctx, rc, "drama_run_job", []byte(`{"step":"scripts","episodes":[1],"reason":"补全设定"}`))
	o, err := br.ResolveApproval(ctx, rc, "drama_run_job", p.Payload, model.DecideAgentApprovalReq{Decision: "approve"})
	if err != nil {
		t.Fatal(err)
	}
	f.busy = true
	result, err := br.ExecuteApproval(ctx, rc, "drama_run_job", 44, "job", *o)
	if err != nil || result.Status != ExecFailed || !strings.Contains(result.OutcomeText, errcode.ErrDramaJobBusy.Msg) {
		t.Fatalf("67004 %+v %v", result, err)
	}
	e.repo.canvas.DramaID = nil
	if _, err := br.ExecuteApproval(ctx, rc, "drama_run_job", 44, "job", *o); err == nil {
		t.Fatal("普通画布执行短剧审批")
	}
}
