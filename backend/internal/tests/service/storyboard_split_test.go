package service_test

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"video-canvas/internal/llmgateway"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/ws"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
	"video-canvas/internal/service/stepfake"
	"video-canvas/internal/stepkit"
)

// sbRepo 是拆分镜用的内存仓储：积分原语复用 stepfake.Repo，画布归属按 owned 判断，任务存在内存里。
type sbRepo struct {
	*stepfake.Repo
	owned map[uint64]uint64 // 画布 id → 用户 id
	jobs  map[uint64]*model.StoryboardSplitJob
	next  uint64
	// enqueued 记下入队的任务 id（真实实现里和任务行同一个事务提交）
	enqueued []uint64
	// failCreate 非空时 CreateSplitJob 返回它，用来验证建任务失败时不入队
	failCreate error
	accessErr  error
}

func newSBRepo() *sbRepo {
	return &sbRepo{Repo: stepfake.NewRepo(), owned: map[uint64]uint64{sbCanvas: sbUser}, jobs: map[uint64]*model.StoryboardSplitJob{}}
}

// sbTx 让 stepfake.Repo 的积分原语加上任务读写，满足 repository.StoryboardTx。
type sbTx struct {
	*stepfake.Repo
	r *sbRepo
}

func (t *sbTx) CreateSplitJob(_ context.Context, job *model.StoryboardSplitJob) error {
	if t.r.failCreate != nil {
		return t.r.failCreate
	}
	for _, j := range t.r.jobs {
		if j.CanvasID == job.CanvasID && j.NodeID == job.NodeID && (j.Status == model.SplitJobPending || j.Status == model.SplitJobRunning) {
			return repository.ErrSplitJobBusy
		}
	}
	t.r.next++
	job.ID = t.r.next
	cp := *job
	t.r.jobs[job.ID] = &cp
	t.r.enqueued = append(t.r.enqueued, job.ID)
	return nil
}

func (t *sbTx) UpdateSplitJob(_ context.Context, id uint64, patch map[string]any) error {
	j, ok := t.r.jobs[id]
	if !ok {
		return repository.ErrNotFound
	}
	for k, v := range patch {
		switch k {
		case "status":
			j.Status = v.(string)
		case "error":
			j.Error = v.(string)
		case "credits":
			j.Credits = v.(int)
		case "attempts":
			j.Attempts = v.(int)
		case "script":
			j.Script = v.(string)
		case "shots":
			j.Shots = v.([]byte)
		}
	}
	return nil
}

func (t *sbTx) SplitJobForUpdate(_ context.Context, id uint64) (*model.StoryboardSplitJob, error) {
	j, ok := t.r.jobs[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	cp := *j
	return &cp, nil
}

func (r *sbRepo) WithTx(_ context.Context, fn func(repository.StoryboardTx) error) error {
	return fn(&sbTx{Repo: r.Repo, r: r})
}

func (r *sbRepo) CanvasOwned(_ context.Context, userID, canvasID uint64) error {
	if r.owned[canvasID] != userID {
		return repository.ErrNotFound
	}
	return nil
}

func (r *sbRepo) SplitJobByID(_ context.Context, userID, id uint64) (*model.StoryboardSplitJob, error) {
	j, ok := r.jobs[id]
	if !ok || j.UserID != userID {
		return nil, repository.ErrNotFound
	}
	cp := *j
	return &cp, nil
}

func (r *sbRepo) SplitJobByRequest(_ context.Context, userID uint64, key string) (*model.StoryboardSplitJob, error) {
	for _, job := range r.jobs {
		if job.UserID == userID && job.RequestKey != nil && *job.RequestKey == key {
			return job, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (r *sbRepo) CheckCollaborationSplit(context.Context, *model.StoryboardSplitJob) error {
	return r.accessErr
}

func (r *sbRepo) ActiveSplitJob(_ context.Context, userID, canvasID uint64, nodeID string) (*model.StoryboardSplitJob, error) {
	for _, j := range r.jobs {
		if j.UserID == userID && j.CanvasID == canvasID && j.NodeID == nodeID && (j.Status == model.SplitJobPending || j.Status == model.SplitJobRunning) {
			cp := *j
			return &cp, nil
		}
	}
	return nil, repository.ErrNotFound
}

// sbBroadcaster 记下推送，按顺序。
type sbBroadcaster struct {
	mu   sync.Mutex
	msgs []ws.Message
}

func (b *sbBroadcaster) Publish(_ context.Context, _ string, m ws.Message) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.msgs = append(b.msgs, m)
}

func (b *sbBroadcaster) statuses() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, len(b.msgs))
	for i, m := range b.msgs {
		out[i] = m.Data.(*model.StoryboardSplitResp).Status
	}
	return out
}

const (
	sbUser   = uint64(7)
	sbCanvas = uint64(70)
)

const sbValid = `{"shots":[
  {"scene":"雨夜便利店外景","shot":"远景","camera":"固定","action":"","dialogue":"","duration":4},
  {"scene":"林晚推门进来","shot":"中景","camera":"跟","action":"走到收银台","dialogue":"林晚：还有关东煮吗？","duration":75}
]}`

// sbEnv 组装拆分镜服务：一个 Agent 模型（按次 5 积分）、账户 100 积分，带一个记录推送的 broadcaster。
type sbEnv struct {
	svc    *service.StoryboardService
	repo   *sbRepo
	caller *stepfake.Caller
	models *stepfake.Models
	bc     *sbBroadcaster
}

func newSBEnv(replies ...stepfake.Reply) *sbEnv {
	repo := newSBRepo()
	repo.Credits[sbUser] = &model.UserCredit{UserID: sbUser, Balance: 100}
	models := stepfake.NewModels()
	models.Snapshots = map[string]*provider.Snapshot{"agent-a": stepfake.TextModel("agent-a", false, false, 5)}
	caller := &stepfake.Caller{Replies: replies}
	bc := &sbBroadcaster{}
	svc := service.NewStoryboardService(service.StoryboardDeps{Repo: repo, Models: models, Caller: caller, Credits: stepfake.Policy{Initial: 100}, Broadcaster: bc})
	return &sbEnv{svc: svc, repo: repo, caller: caller, models: models, bc: bc}
}

func sbReq(script string) *model.StoryboardSplitReq {
	return &model.StoryboardSplitReq{NodeID: "n_sb", Script: script, Model: "agent-a"}
}

// submit 提交一次拆分镜并返回它的内存任务 id。
func submit(t *testing.T, e *sbEnv, script string) uint64 {
	t.Helper()
	resp, err := e.svc.Split(context.Background(), sbUser, sbCanvas, sbReq(script))
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if resp.Status != model.SplitJobPending {
		t.Fatalf("提交后应是 pending：%+v", resp)
	}
	if len(e.repo.enqueued) == 0 {
		t.Fatal("建任务没有入队")
	}
	return e.repo.enqueued[len(e.repo.enqueued)-1]
}

// run 把 worker 调一次。
func run(t *testing.T, e *sbEnv, jobID uint64) error {
	t.Helper()
	return e.svc.HandleStoryboardSplit(context.Background(), jobID)
}

func TestStoryboardSplit_QueueRetryReclaimsRunningJob(t *testing.T) {
	e := newSBEnv(stepfake.Reply{Text: sbValid})
	jobID := submit(t, e, "剧本")
	e.repo.jobs[jobID].Status = model.SplitJobRunning
	e.repo.jobs[jobID].Attempts = 1
	if err := run(t, e, jobID); err != nil {
		t.Fatal(err)
	}
	if e.caller.Calls != 0 {
		t.Fatal("同一次队列执行不得重复调用模型")
	}
	if err := e.svc.HandleStoryboardSplitAttempt(t.Context(), jobID, 2, false); err != nil {
		t.Fatal(err)
	}
	if job := e.repo.jobs[jobID]; job.Status != model.SplitJobSucceeded || job.Attempts != 2 || e.repo.Credits[sbUser].Balance != 95 {
		t.Fatalf("队列重试应接管并只结算一次: %+v", job)
	}
}

// TestStoryboardSplit_SubmitReturnsPendingAndEnqueues 提交后立刻返回 pending、入队、不扣费、不调模型。
func TestStoryboardSplit_SubmitReturnsPendingAndEnqueues(t *testing.T) {
	e := newSBEnv(stepfake.Reply{Text: sbValid})
	resp, err := e.svc.Split(context.Background(), sbUser, sbCanvas, sbReq("雨夜，林晚推门进便利店。"))
	if err != nil {
		t.Fatalf("Split: %v", err)
	}
	if resp.Status != model.SplitJobPending || resp.ID == "" || resp.Model != "agent-a" {
		t.Fatalf("提交快照不对：%+v", resp)
	}
	if e.caller.Calls != 0 {
		t.Error("提交不调模型")
	}
	if e.repo.Credits[sbUser].Balance != 100 || len(e.repo.Ledger) != 0 {
		t.Error("提交不扣费")
	}
	if len(e.repo.enqueued) != 1 {
		t.Fatalf("应入队 1 个 job：%v", e.repo.enqueued)
	}
}

// TestStoryboardSplit_Succeeds worker 跑完后：拆出镜头、时长截断，按实际用量扣费、推送 running 和 succeeded。
func TestStoryboardSplit_Succeeds(t *testing.T) {
	e := newSBEnv(stepfake.Reply{Text: sbValid, Usage: llmgateway.Usage{InputTokens: 10, OutputTokens: 10}})
	id := submit(t, e, "雨夜，林晚推门进便利店。")
	if err := run(t, e, id); err != nil {
		t.Fatalf("worker: %v", err)
	}
	snap, err := e.svc.Job(context.Background(), sbUser, sbCanvas, id)
	if err != nil {
		t.Fatalf("Job: %v", err)
	}
	if snap.Status != model.SplitJobSucceeded || len(snap.Shots) != 2 || snap.Shots[1].Duration != 60 {
		t.Fatalf("终态不对：%+v", snap)
	}
	if snap.Credits != 5 || e.repo.Credits[sbUser].Balance != 95 || len(e.repo.Ledger) != 2 {
		t.Fatalf("扣费不对：%+v %d", snap, e.repo.Credits[sbUser].Balance)
	}
	if got := e.bc.statuses(); !reflect.DeepEqual(got, []string{model.SplitJobRunning, model.SplitJobSucceeded}) {
		t.Fatalf("推送顺序：%v", got)
	}
}

// jobOf 读任务终态快照。
func jobOf(t *testing.T, e *sbEnv, id uint64) *model.StoryboardSplitResp {
	t.Helper()
	snap, err := e.svc.Job(context.Background(), sbUser, sbCanvas, id)
	if err != nil {
		t.Fatalf("Job: %v", err)
	}
	return snap
}

// TestStoryboardSplit_InvalidTwiceFailsWithoutCharge 两次输出都不合格：任务失败，不扣费，worker 不要求重试，推送了 failed。
func TestStoryboardSplit_InvalidTwiceFailsWithoutCharge(t *testing.T) {
	bad := stepfake.Reply{Text: `{"shots":[{"scene":"x","shot":"超大特写","camera":"推","action":"","dialogue":"","duration":3}]}`}
	e := newSBEnv(bad, bad)
	id := submit(t, e, "剧本")
	if err := run(t, e, id); err != nil {
		t.Fatalf("业务失败 worker 应返回 nil，实际 %v", err)
	}
	snap := jobOf(t, e, id)
	if snap.Status != model.SplitJobFailed || !strings.Contains(snap.Error, "不合格") {
		t.Fatalf("应是失败并说明原因：%+v", snap)
	}
	if e.repo.Credits[sbUser].Balance != 100 || len(e.repo.Ledger) != 0 {
		t.Error("失败不扣费")
	}
	if got := e.bc.statuses(); !reflect.DeepEqual(got, []string{model.SplitJobRunning, model.SplitJobFailed}) {
		t.Fatalf("推送顺序：%v", got)
	}
}

// TestStoryboardSplit_ModelCallFailure 上游报错：任务失败并带出上游原文，不扣费。
func TestStoryboardSplit_ModelCallFailure(t *testing.T) {
	e := newSBEnv(stepfake.Reply{Err: &llmgateway.UpstreamError{Status: 429, Message: "rate limited"}})
	id := submit(t, e, "剧本")
	if err := run(t, e, id); err != nil {
		t.Fatalf("worker: %v", err)
	}
	snap := jobOf(t, e, id)
	if snap.Status != model.SplitJobFailed || !strings.Contains(snap.Error, "rate limited") {
		t.Fatalf("应失败并带出原因：%+v", snap)
	}
	if len(e.repo.Ledger) != 0 {
		t.Error("失败不扣费")
	}
}

// TestStoryboardSplit_RejectsBeforeEnqueue 空剧本、超长剧本、别人的画布、积分不够、没有模型：提交就拒绝，不建任务、不入队。
func TestStoryboardSplit_RejectsBeforeEnqueue(t *testing.T) {
	cases := []struct {
		name  string
		setup func(e *sbEnv) (canvas uint64, req *model.StoryboardSplitReq)
		want  error
	}{
		{"空剧本", func(*sbEnv) (uint64, *model.StoryboardSplitReq) { return sbCanvas, sbReq("  \n ") }, errcode.ErrStoryboardScriptEmpty},
		{"超长剧本", func(*sbEnv) (uint64, *model.StoryboardSplitReq) {
			return sbCanvas, sbReq(strings.Repeat("字", 20001))
		}, errcode.ErrStoryboardScriptTooLong},
		{"别人的画布", func(*sbEnv) (uint64, *model.StoryboardSplitReq) { return 99, sbReq("剧本") }, errcode.ErrCanvasNotFound},
		{"积分不够", func(e *sbEnv) (uint64, *model.StoryboardSplitReq) {
			e.repo.Credits[sbUser].Balance = 1
			return sbCanvas, sbReq("剧本")
		}, errcode.ErrInsufficientCredits},
		{"没有模型", func(e *sbEnv) (uint64, *model.StoryboardSplitReq) {
			delete(e.models.Snapshots, "agent-a")
			return sbCanvas, sbReq("剧本")
		}, errcode.ErrStoryboardNoModel},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newSBEnv(stepfake.Reply{Text: sbValid})
			canvas, req := c.setup(e)
			_, err := e.svc.Split(context.Background(), sbUser, canvas, req)
			if !errors.Is(err, c.want) {
				t.Fatalf("应报 %v，实际 %v", c.want, err)
			}
			if len(e.repo.jobs) != 0 || len(e.repo.enqueued) != 0 || e.caller.Calls != 0 {
				t.Error("不该建任务、入队或调模型")
			}
		})
	}
}

// TestStoryboardSplit_FallsBackToFirstModel 选的模型下架了：用第一个可用的 Agent 模型，任务里记的是实际用的那个。
func TestStoryboardSplit_FallsBackToFirstModel(t *testing.T) {
	e := newSBEnv(stepfake.Reply{Text: sbValid})
	req := sbReq("剧本")
	req.Model = "gone"
	resp, err := e.svc.Split(context.Background(), sbUser, sbCanvas, req)
	if err != nil || resp.Model != "agent-a" {
		t.Fatalf("应回落到 agent-a：%+v %v", resp, err)
	}
}

// TestStoryboardSplit_BusyWhileActive 同一张分镜表已有进行中的任务：再点一次直接报正在拆分镜，不另建任务。
func TestStoryboardSplit_BusyWhileActive(t *testing.T) {
	e := newSBEnv(stepfake.Reply{Text: sbValid})
	submit(t, e, "剧本")
	if _, err := e.svc.Split(context.Background(), sbUser, sbCanvas, sbReq("剧本")); !errors.Is(err, errcode.ErrStoryboardSplitBusy) {
		t.Fatalf("应报正在拆分镜，实际 %v", err)
	}
	if len(e.repo.enqueued) != 1 {
		t.Errorf("不该再入队：%v", e.repo.enqueued)
	}
}

// TestStoryboardSplit_AfterFinishCanSplitAgain 上一次结束（成功或失败）后同一张表能再拆。
func TestStoryboardSplit_AfterFinishCanSplitAgain(t *testing.T) {
	e := newSBEnv(stepfake.Reply{Text: sbValid}, stepfake.Reply{Text: sbValid})
	id := submit(t, e, "剧本")
	if err := run(t, e, id); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Split(context.Background(), sbUser, sbCanvas, sbReq("剧本")); err != nil {
		t.Fatalf("结束后应能再拆：%v", err)
	}
}

// TestStoryboardSplit_WorkerIsIdempotent 同一个 job 被再执行一次（队列重试、两个实例抢到）：终态的任务直接跳过，不重复调模型、不重复扣费。
func TestStoryboardSplit_WorkerIsIdempotent(t *testing.T) {
	e := newSBEnv(stepfake.Reply{Text: sbValid}, stepfake.Reply{Text: sbValid})
	id := submit(t, e, "剧本")
	for i := 0; i < 3; i++ {
		if err := run(t, e, id); err != nil {
			t.Fatalf("第 %d 次执行：%v", i+1, err)
		}
	}
	if e.caller.Calls != 1 {
		t.Errorf("只该调一次模型，实际 %d", e.caller.Calls)
	}
	if e.repo.Credits[sbUser].Balance != 95 || len(e.repo.Ledger) != 2 {
		t.Errorf("只该扣一次费：余额 %d，流水 %d", e.repo.Credits[sbUser].Balance, len(e.repo.Ledger))
	}
}

// TestStoryboardSplit_WorkerSkipsMissingJob 任务行没了（被清理）：worker 当没事发生，不让队列无限重试。
func TestStoryboardSplit_WorkerSkipsMissingJob(t *testing.T) {
	e := newSBEnv()
	if err := run(t, e, 12345); err != nil {
		t.Fatalf("任务不存在应返回 nil：%v", err)
	}
}

// TestStoryboardSplit_JobIsPrivate 别人的任务按不存在处理。
func TestStoryboardSplit_JobIsPrivate(t *testing.T) {
	e := newSBEnv(stepfake.Reply{Text: sbValid})
	id := submit(t, e, "剧本")
	if _, err := e.svc.Job(context.Background(), sbUser+1, sbCanvas, id); !errors.Is(err, errcode.ErrStoryboardJobNotFound) {
		t.Fatalf("别人的任务应按不存在：%v", err)
	}
	if _, err := e.svc.Job(t.Context(), sbUser, sbCanvas+1, id); !errors.Is(err, errcode.ErrStoryboardJobNotFound) {
		t.Fatalf("其他画布不能读取任务: %v", err)
	}
}

func TestStoryboardSplit_RequestRecoversTerminalJob(t *testing.T) {
	e := newSBEnv()
	ctx := t.Context()
	budget := 0
	request := sbReq(" 剧本 ")
	request.Mode = "append"
	request.RequestKey = "split-1"
	request.Collaboration = &model.CanvasGenerationBinding{Epoch: 3, Revision: 7, MaxCredits: &budget}
	if _, err := e.svc.Split(ctx, sbUser, sbCanvas, request); !errors.Is(err, errcode.ErrGenerationBudget) || len(e.repo.enqueued) != 0 || len(e.repo.Ledger) != 0 {
		t.Fatalf("预算不足不得入队或计费: %v", err)
	}
	budget = 100
	prior, err := e.svc.Split(ctx, sbUser, sbCanvas, request)
	if err != nil || len(e.repo.enqueued) != 1 {
		t.Fatalf("提交任务: %+v %v", prior, err)
	}
	job := e.repo.jobs[e.repo.enqueued[0]]
	job.Status, job.Script = model.SplitJobSucceeded, ""
	e.models.Snapshots = nil
	request.Script = "剧本"
	resumed, err := e.svc.Split(ctx, sbUser, sbCanvas, request)
	if err != nil || resumed.ID != prior.ID || resumed.Status != model.SplitJobSucceeded || len(e.repo.enqueued) != 1 {
		t.Fatalf("模型配置变化后仍应返回原任务: %+v %v", resumed, err)
	}
	request.Script = "另一版"
	var conflict *errcode.Error
	if _, err := e.svc.Split(ctx, sbUser, sbCanvas, request); !errors.As(err, &conflict) || conflict.Code != errcode.ErrCanvasConflict.Code {
		t.Fatalf("同编号不同正文应冲突: %v", err)
	}
	request.Script = "剧本"
	e.repo.accessErr = repository.ErrNotFound
	if _, err := e.svc.Split(ctx, sbUser, sbCanvas, request); err == nil {
		t.Fatal("撤权后不可重试读取原任务")
	}
	if _, err := e.svc.Job(ctx, sbUser, sbCanvas, job.ID); !errors.Is(err, errcode.ErrStoryboardJobNotFound) {
		t.Fatalf("撤权后不可通过任务轮询获取分镜结果: %v", err)
	}
}

// TestStoryboardSplit_ModelGoneAtRunTime 排队期间模型被下架：任务失败说明原因，不扣费。
func TestStoryboardSplit_ModelGoneAtRunTime(t *testing.T) {
	e := newSBEnv(stepfake.Reply{Text: sbValid})
	id := submit(t, e, "剧本")
	delete(e.models.Snapshots, "agent-a")
	if err := run(t, e, id); err != nil {
		t.Fatal(err)
	}
	snap := jobOf(t, e, id)
	if snap.Status != model.SplitJobFailed || e.caller.Calls != 0 || len(e.repo.Ledger) != 0 {
		t.Fatalf("应失败且没调模型没扣费：%+v", snap)
	}
}

// TestStoryboardSplit_ClearsScriptAfterFinish 结束后清掉剧本原文，不长期占库。
func TestStoryboardSplit_ClearsScriptAfterFinish(t *testing.T) {
	e := newSBEnv(stepfake.Reply{Text: sbValid})
	id := submit(t, e, "一段很长的剧本")
	if e.repo.jobs[id].Script == "" {
		t.Fatal("提交时应存着剧本给 worker 用")
	}
	if err := run(t, e, id); err != nil {
		t.Fatal(err)
	}
	if e.repo.jobs[id].Script != "" {
		t.Error("结束后应清掉剧本原文")
	}
}

// ctxCaller 阻塞到 ctx 结束，模拟一个一直不出字的模型；记下它拿到的截止时间。
type ctxCaller struct {
	deadline atomic.Pointer[time.Time]
}

func (c *ctxCaller) Complete(ctx context.Context, _ *provider.Snapshot, _ []llmgateway.Message) (*llmgateway.Result, error) {
	if d, ok := ctx.Deadline(); ok {
		c.deadline.Store(&d)
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

// splitEnvWithCaller 用给定的调用方和模型截止时间搭一个服务，账户 100 积分。
func splitEnvWithCaller(deadline time.Duration, caller stepkit.Caller) *sbEnv {
	e := newSBEnv()
	e.models.Snapshots["agent-a"].Model.Deadline = modelcfg.Duration(deadline)
	e.svc = service.NewStoryboardService(service.StoryboardDeps{Repo: e.repo, Models: e.models, Caller: caller, Credits: stepfake.Policy{Initial: 100}, Broadcaster: e.bc})
	return e
}

// TestStoryboardSplit_TimesOutByModelDeadline 超时按后台给这个模型配的截止时间来：到点任务就按超时失败，不扣积分。
func TestStoryboardSplit_TimesOutByModelDeadline(t *testing.T) {
	e := splitEnvWithCaller(150*time.Millisecond, &ctxCaller{})
	id := submit(t, e, "剧本")
	start := time.Now()
	if err := run(t, e, id); err != nil {
		t.Fatal(err)
	}
	snap := jobOf(t, e, id)
	if snap.Status != model.SplitJobFailed || !strings.Contains(snap.Error, "超时") {
		t.Fatalf("应报模型响应超时：%+v", snap)
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Errorf("应在配置的截止时间附近失败，实际等了 %v", took)
	}
	if e.repo.Credits[sbUser].Balance != 100 || len(e.repo.Ledger) != 0 {
		t.Error("超时失败不扣费")
	}
}

// TestStoryboardSplit_NoFixedCeiling 不再有写死的上限：模型配了多长的截止时间，调用就能等多长。
func TestStoryboardSplit_NoFixedCeiling(t *testing.T) {
	c := &ctxCaller{}
	e := splitEnvWithCaller(10*time.Minute, c)
	id := submit(t, e, "剧本")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = e.svc.HandleStoryboardSplit(ctx, id)
	d := c.deadline.Load()
	if d == nil {
		t.Fatal("调用方没有拿到截止时间")
	}
	if time.Until(*d) > time.Second {
		t.Fatalf("截止时间应来自外层 ctx 或模型配置，不该多出别的上限：还剩 %v", time.Until(*d))
	}
	src, err := os.ReadFile("../../service/storyboard_split.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(src), "splitTimeout") || strings.Contains(string(src), "50 * time.Second") {
		t.Error("不该再写死拆分镜的超时时间")
	}
}

// TestStoryboardSplit_SlowStreamOver60sSucceeds 3000 多字的剧本、模型慢慢吐字超过 60 秒（比同步接口的 60 秒写超时还久）：
// 异步任务照样成功。用模拟时间：调用方"耗时"90 秒，截止时间配 5 分钟。
func TestStoryboardSplit_SlowStreamOver60sSucceeds(t *testing.T) {
	long := strings.Repeat("闰土在月光下的瓜田里守着瓜，手里捏着一柄钢叉。", 160) // 约 3500 字
	if n := len([]rune(long)); n < 3000 {
		t.Fatalf("样例剧本应有 3000 多字，实际 %d", n)
	}
	e := newSBEnv()
	e.models.Snapshots["agent-a"].Model.Deadline = modelcfg.Duration(5 * time.Minute)
	slow := &simulatedSlowCaller{took: 90 * time.Second, reply: sbValid}
	e.svc = service.NewStoryboardService(service.StoryboardDeps{Repo: e.repo, Models: e.models, Caller: slow, Credits: stepfake.Policy{Initial: 100}, Broadcaster: e.bc})
	id := submit(t, e, long)
	if err := run(t, e, id); err != nil {
		t.Fatal(err)
	}
	snap := jobOf(t, e, id)
	if snap.Status != model.SplitJobSucceeded || len(snap.Shots) == 0 {
		t.Fatalf("慢速流式输出的长剧本也应成功：%+v", snap)
	}
	if !slow.sawDeadlineAtLeast(90 * time.Second) {
		t.Errorf("模型调用的截止时间应至少 90 秒，实际 %v", slow.deadline)
	}
}

// simulatedSlowCaller 假装花了 took 这么久：不真等，只检查它拿到的 ctx 允许等这么久，然后给出结果。
type simulatedSlowCaller struct {
	took     time.Duration
	reply    string
	deadline time.Duration
}

func (c *simulatedSlowCaller) Complete(ctx context.Context, _ *provider.Snapshot, _ []llmgateway.Message) (*llmgateway.Result, error) {
	if d, ok := ctx.Deadline(); ok {
		c.deadline = time.Until(d)
	}
	return &llmgateway.Result{Text: c.reply, Usage: llmgateway.Usage{InputTokens: 5000, OutputTokens: 9000}}, nil
}

func (c *simulatedSlowCaller) sawDeadlineAtLeast(d time.Duration) bool {
	return c.deadline >= d
}
