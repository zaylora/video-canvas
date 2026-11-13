package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"video-canvas/internal/llmgateway"
	"video-canvas/internal/model"
	"video-canvas/internal/service/stepfake"
	"video-canvas/internal/stepkit"
)

// 本文件测试环节后台：列表与详情、发起试跑与冻结积分、River 任务执行与结算。设计见 营销工作室.md §6.9、§6.11。

func TestStepListAndDetail(t *testing.T) {
	e := newStepEnv(t)
	e.lib.Add("scripty", &stepfake.Skill{ID: 8, Title: "带脚本", Enabled: false, Active: 2, HasScripts: true, Versions: map[int]string{1: "a", 2: "bbbb"}})
	e.stats.rates[stepKeyName] = 0.25

	list, err := e.svc.List(context.Background())
	if err != nil || len(list) != 1 {
		t.Fatalf("%v %v", list, err)
	}
	row := list[0]
	if row.Key != stepKeyName || row.Studio != "test" || row.Media != "none" || row.Binding.Source != "builtin" || row.Effective.Source != "builtin" || row.Effective.Fallback {
		t.Fatalf("没有绑定记录应是内置：%+v", row)
	}
	if row.LastTrial != nil || row.NewerVersion != nil || row.FailRate7d != 0.25 {
		t.Fatalf("列表行不对：%+v", row)
	}

	d, err := e.svc.Get(context.Background(), stepKeyName)
	if err != nil {
		t.Fatal(err)
	}
	if d.Duty != "写镜头。" || len(d.Inputs) != 1 || d.Inputs[0].Name != "product" || d.Schema != stepSchema || len(d.Rules.Hard) != 1 || len(d.Rules.Soft) != 0 {
		t.Fatalf("协议详情不对：%+v", d)
	}
	if len(d.Samples) != 1 || d.Samples[0].Key != "s1" || d.Samples[0].NeedsRef || len(d.History) != 0 {
		t.Fatalf("样例或历史不对：%+v", d)
	}
	if len(d.Candidates) != 3 || d.Candidates[0].Source != "builtin" {
		t.Fatalf("候选应是内置加两个技能：%+v", d.Candidates)
	}
	sc := d.Candidates[1] // 按名字排序：scripty 在 tvc 前
	if sc.SkillName != "scripty" || *sc.Enabled || !*sc.HasScripts || *sc.Chars != 4 || sc.ActiveVersion != 2 || len(sc.Versions) != 2 {
		t.Fatalf("技能候选字段不对：%+v", sc)
	}
}

func TestStepGetUnknownStep(t *testing.T) {
	e := newStepEnv(t)
	_, err := e.svc.Get(context.Background(), "nope.step")
	wantStepCode(t, err, 68012)
	_, err = e.svc.CreateTrials(context.Background(), stepActor, "nope.step", builtinTrialReq())
	wantStepCode(t, err, 68012)
	_, err = e.svc.SetBinding(context.Background(), stepActor, "nope.step", &model.SetStepBindingReq{Source: "builtin", TrialID: encodeTrialID(1)})
	wantStepCode(t, err, 68012)
}

func TestStepCreateTrialFreezesCreditsAndEnqueues(t *testing.T) {
	e := newStepEnv(t)
	list := e.create(t, builtinTrialReq())
	if len(list.Trials) != 1 || list.Trials[0].Status != "queued" || list.Trials[0].Candidate.Source != "builtin" || list.Trials[0].CompareOf != nil {
		t.Fatalf("应返回 1 条排队中的试跑：%+v", list.Trials)
	}
	if len(e.repo.Enqueued) != 1 {
		t.Fatalf("应入队 1 个 job：%v", e.repo.Enqueued)
	}
	trial := e.repo.Trials[e.repo.Enqueued[0]]
	// 按次计费 2 积分，预估按最多调用 2 次冻结
	if trial.Frozen != 4 || trial.CreatedBy != stepActor || trial.StepKey != stepKeyName || trial.TextModelID != "text-a" {
		t.Fatalf("试跑行不对：%+v", trial)
	}
	if c := e.repo.Credits[stepActor]; c.Frozen != 4 || c.Balance != 100 {
		t.Fatalf("应冻结在操作管理员账户：%+v", c)
	}
	ledger := e.repo.LedgerOf(trial.ID)
	if len(ledger) != 1 || ledger[0].Type != model.LedgerFreeze || ledger[0].Amount != 4 || ledger[0].UserID != stepActor {
		t.Fatalf("冻结流水应关联试跑：%+v", ledger)
	}
	if got := e.audit.Actions(); len(got) != 1 || got[0] != model.AdminAuditStepTrial {
		t.Fatalf("发起试跑应写审计：%v", got)
	}
}

func TestStepCreateTrialCompareUsesCurrentEffective(t *testing.T) {
	e := newStepEnv(t)
	list := e.create(t, skillTrialReq("tvc", 0)) // 版本省略：取生效版本 v1
	if len(list.Trials) != 1 || list.Trials[0].Candidate.SkillName != "tvc" || list.Trials[0].Candidate.Version != 1 {
		t.Fatalf("省略版本应取生效版本：%+v", list.Trials)
	}

	e2 := newStepEnv(t)
	req := skillTrialReq("tvc", 1)
	req.Compare = true
	list = e2.create(t, req)
	if len(list.Trials) != 2 {
		t.Fatalf("对比应有两条：%+v", list.Trials)
	}
	a, b := list.Trials[0], list.Trials[1]
	if a.CompareOf == nil || b.CompareOf == nil || *a.CompareOf != b.ID || *b.CompareOf != a.ID {
		t.Fatalf("两条应 compare_of 互指：%+v %+v", a, b)
	}
	if a.Candidate.Source != "skill" || b.Candidate.Source != "builtin" {
		t.Fatalf("第二条应是当前生效方法（没有绑定即内置）：%+v", b.Candidate)
	}
	if len(e2.repo.Enqueued) != 2 {
		t.Fatal("两条都要入队")
	}
	if c := e2.repo.Credits[stepActor]; c.Frozen != 8 {
		t.Fatalf("两条各冻结 4：%+v", c)
	}
}

// 对比的是用户端此刻真正会用的方法：绑定的技能被停用后，第二条是兜底后的内置，而不是那个停用的技能。
func TestStepCompareAfterBoundSkillDisabledUsesBuiltin(t *testing.T) {
	e := newStepEnv(t)
	trial := e.passedTrialFor(t, skillTrialReq("tvc", 1))
	if _, err := e.svc.SetBinding(context.Background(), stepActor, stepKeyName, &model.SetStepBindingReq{Source: "skill", SkillName: "tvc", Version: 1, TrialID: trial}); err != nil {
		t.Fatal(err)
	}
	e.lib.Skills["tvc"].Enabled = false
	e.repo.Enqueued = nil
	req := builtinTrialReq()
	req.Compare = true
	list := e.create(t, req)
	if list.Trials[1].Candidate.Source != "builtin" {
		t.Fatalf("绑定技能已停用，对比应取内置：%+v", list.Trials[1].Candidate)
	}
}

func TestStepCreateTrialRejections(t *testing.T) {
	cases := map[string]struct {
		mutate func(e *stepEnv, req *model.CreateStepTrialReq)
		code   int
	}{
		"技能不存在": {func(_ *stepEnv, r *model.CreateStepTrialReq) { *r = *skillTrialReq("ghost", 1) }, 68014},
		"技能停用": {func(e *stepEnv, r *model.CreateStepTrialReq) {
			e.lib.Skills["tvc"].Enabled = false
			*r = *skillTrialReq("tvc", 1)
		}, 68014},
		"版本不存在": {func(_ *stepEnv, r *model.CreateStepTrialReq) { *r = *skillTrialReq("tvc", 9) }, 68014},
		"没有生效版本": {func(e *stepEnv, r *model.CreateStepTrialReq) {
			e.lib.Skills["tvc"].Active = 0
			*r = *skillTrialReq("tvc", 0)
		}, 68014},
		"样例不存在": {func(_ *stepEnv, r *model.CreateStepTrialReq) { r.SampleKey = "ghost" }, 10001},
		"模型不存在": {func(_ *stepEnv, r *model.CreateStepTrialReq) { r.TextModel = "ghost" }, 10001},
		"模型不是文本模型": {func(e *stepEnv, r *model.CreateStepTrialReq) {
			e.models.Snapshots["img"] = stepfake.TextModel("img", false, false, 1)
			e.models.Snapshots["img"].Model.Kind = "image"
			r.TextModel = "img"
		}, 10001},
		"积分不足": {func(e *stepEnv, _ *model.CreateStepTrialReq) { e.repo.Credits[stepActor].Balance = 3 }, 40001},
	}
	for name, c := range cases {
		e := newStepEnv(t)
		req := builtinTrialReq()
		c.mutate(e, req)
		_, err := e.svc.CreateTrials(context.Background(), stepActor, stepKeyName, req)
		if stepCodeOf(err) != c.code {
			t.Fatalf("%s：应返回 %d，实际 %v", name, c.code, err)
		}
		if len(e.repo.Trials) != 0 || len(e.repo.Enqueued) != 0 || e.repo.Credits[stepActor].Frozen != 0 {
			t.Fatalf("%s：拒绝时不应留下试跑、job 或冻结", name)
		}
	}
}

// 看图 / 看视频的环节，所选模型能力不够时直接拒绝，不入队。
func TestStepCreateTrialRequiresModelCapability(t *testing.T) {
	e := newStepEnv(t)
	vid := newStepStep("test.video")
	vid.Media = stepkit.MediaVideo
	e.reg.MustRegister(vid)
	e.models.Snapshots["blind"] = stepfake.TextModel("blind", false, false, 1)
	req := builtinTrialReq()
	req.TextModel = "blind"
	_, err := e.svc.CreateTrials(context.Background(), stepActor, "test.video", req)
	wantStepCode(t, err, 10001)
	req.TextModel = "text-a" // 能看图，可用截帧
	if _, err := e.svc.CreateTrials(context.Background(), stepActor, "test.video", req); err != nil {
		t.Fatalf("能看图的模型应能试跑视频环节（用截帧）：%v", err)
	}
}

// 同一环节已有进行中的试跑返回 68017；结束后可再发；卡死超过 30 分钟的不挡；别的环节不受影响。
func TestStepCreateTrialBusy(t *testing.T) {
	e := newStepEnv(t)
	e.reg.MustRegister(newStepStep("test.other"))
	e.create(t, builtinTrialReq())
	_, err := e.svc.CreateTrials(context.Background(), stepActor, stepKeyName, builtinTrialReq())
	wantStepCode(t, err, 68017)
	if _, err := e.svc.CreateTrials(context.Background(), stepActor, "test.other", builtinTrialReq()); err != nil {
		t.Fatalf("别的环节不受影响：%v", err)
	}
	if len(e.repo.Trials) != 2 {
		t.Fatalf("被拒绝的请求不应写试跑：%d", len(e.repo.Trials))
	}
	// 卡死：创建时间早于 30 分钟前
	e.now = e.now.Add(31 * time.Minute)
	if _, err := e.svc.CreateTrials(context.Background(), stepActor, stepKeyName, builtinTrialReq()); err != nil {
		t.Fatalf("卡死的试跑不应永远挡住环节：%v", err)
	}
}

func TestStepCreateTrialBusyClearsAfterFinish(t *testing.T) {
	e := newStepEnv(t)
	trial := e.runTrial(t, builtinTrialReq(), okReply())
	if trial.Status != model.StepTrialSucceeded {
		t.Fatalf("%+v", trial)
	}
	e.repo.Enqueued = nil
	if _, err := e.svc.CreateTrials(context.Background(), stepActor, stepKeyName, builtinTrialReq()); err != nil {
		t.Fatalf("试跑结束后应能再发：%v", err)
	}
}

func TestStepTrialSucceedsAndSettlesCredits(t *testing.T) {
	e := newStepEnv(t)
	trial := e.runTrial(t, builtinTrialReq(), okReply())
	if trial.Status != model.StepTrialSucceeded || !trial.Passed || trial.Attempts != 1 || trial.FinishedAt == nil {
		t.Fatalf("应成功通过：%+v", trial)
	}
	if trial.InputTokens != 100 || trial.OutputTokens != 20 || trial.Credits != 2 || len(trial.Output) == 0 || len(trial.Method) == 0 {
		t.Fatalf("用量、积分、输出、方法快照应落库：%+v", trial)
	}
	c := e.repo.Credits[stepActor]
	if c.Balance != 98 || c.Frozen != 0 {
		t.Fatalf("应扣 2 积分并释放冻结：%+v", c)
	}
	types := map[string]int{}
	for _, l := range e.repo.LedgerOf(trial.ID) {
		types[l.Type] += l.Amount
	}
	if types[model.LedgerFreeze] != 4 || types[model.LedgerSettle] != 2 || types[model.LedgerRefund] != 2 {
		t.Fatalf("流水应是冻结 4 = 结算 2 + 退回 2：%v", types)
	}
	// 视图：预览、候选
	v, err := e.svc.GetTrial(context.Background(), encodeTrialID(trial.ID))
	if err != nil || v.Status != "succeeded" || !v.Passed || v.Preview == "" || len(v.Errors) != 0 || v.Credits != 2 || v.Output == nil {
		t.Fatalf("试跑视图不对：%+v %v", v, err)
	}
	// 列表的最近试跑
	list, _ := e.svc.List(context.Background())
	if list[0].LastTrial == nil || !list[0].LastTrial.Passed || list[0].LastTrial.ID != encodeTrialID(trial.ID) {
		t.Fatalf("最近试跑不对：%+v", list[0].LastTrial)
	}
}

// 输出不合格（重写一次仍不过）：试跑「成功」但 passed=false，带原因；上游确实被调用，照常收费。
func TestStepTrialValidationFailureIsNotCallFailure(t *testing.T) {
	e := newStepEnv(t)
	bad := stepfake.Reply{Text: stepBad, Usage: llmgateway.Usage{InputTokens: 10, OutputTokens: 5}}
	trial := e.runTrial(t, builtinTrialReq(), bad, bad)
	if trial.Status != model.StepTrialSucceeded || trial.Passed || trial.Attempts != 2 {
		t.Fatalf("应是成功但未通过：%+v", trial)
	}
	v, _ := e.svc.GetTrial(context.Background(), encodeTrialID(trial.ID))
	if len(v.Errors) != 1 || v.Errors[0] != "标题含禁用词" || v.Error != "" {
		t.Fatalf("应带格式校验原因，没有调用失败原因：%+v", v)
	}
	if e.repo.Credits[stepActor].Balance == 100 {
		t.Fatal("输出不合格也用了上游，应收费")
	}
	// 未通过的试跑不会让「最近试跑」显示通过
	list, _ := e.svc.List(context.Background())
	if list[0].LastTrial == nil || list[0].LastTrial.Passed {
		t.Fatalf("%+v", list[0].LastTrial)
	}
}

// 重写一次后成功：试跑通过，attempts=2。
func TestStepTrialRewritesOnce(t *testing.T) {
	e := newStepEnv(t)
	trial := e.runTrial(t, builtinTrialReq(), stepfake.Reply{Text: stepBad}, okReply())
	if trial.Status != model.StepTrialSucceeded || !trial.Passed || trial.Attempts != 2 {
		t.Fatalf("%+v", trial)
	}
}

// 调用本身失败：状态 failed，记原因，全额退回冻结，不收费。
func TestStepTrialCallFailureRefunds(t *testing.T) {
	e := newStepEnv(t)
	trial := e.runTrial(t, builtinTrialReq(), stepfake.Reply{Err: errors.New("上游 502")})
	if trial.Status != model.StepTrialFailed || trial.Passed || trial.Error == "" || trial.Credits != 0 {
		t.Fatalf("应是调用失败且不收费：%+v", trial)
	}
	c := e.repo.Credits[stepActor]
	if c.Balance != 100 || c.Frozen != 0 {
		t.Fatalf("应全额退回：%+v", c)
	}
	types := map[string]int{}
	for _, l := range e.repo.LedgerOf(trial.ID) {
		types[l.Type] += l.Amount
	}
	if types[model.LedgerRefund] != 4 || types[model.LedgerSettle] != 0 {
		t.Fatalf("应退回 4，结算 0：%v", types)
	}
	v, _ := e.svc.GetTrial(context.Background(), encodeTrialID(trial.ID))
	if v.Status != "failed" || v.Error == "" || v.Output != nil {
		t.Fatalf("failed 的视图应带原因、没有输出：%+v", v)
	}
}

// 候选技能在试跑执行前被删除或停用：failed，不调用模型，不收费。
func TestStepTrialCandidateGoneBeforeRun(t *testing.T) {
	e := newStepEnv(t)
	e.create(t, skillTrialReq("tvc", 1))
	e.lib.Skills["tvc"].Enabled = false
	e.handleAll(t)
	trial := e.repo.Trials[e.repo.Enqueued[0]]
	if trial.Status != model.StepTrialFailed || e.caller.Calls != 0 || trial.Credits != 0 {
		t.Fatalf("%+v calls=%d", trial, e.caller.Calls)
	}
	if c := e.repo.Credits[stepActor]; c.Balance != 100 || c.Frozen != 0 {
		t.Fatalf("应退回冻结：%+v", c)
	}
}

// 模型在执行前下线：failed，退回冻结。
func TestStepTrialModelGoneBeforeRun(t *testing.T) {
	e := newStepEnv(t)
	e.create(t, builtinTrialReq())
	delete(e.models.Snapshots, "text-a")
	e.handleAll(t)
	trial := e.repo.Trials[e.repo.Enqueued[0]]
	if trial.Status != model.StepTrialFailed || trial.Error == "" {
		t.Fatalf("%+v", trial)
	}
	if c := e.repo.Credits[stepActor]; c.Frozen != 0 || c.Balance != 100 {
		t.Fatalf("%+v", c)
	}
}

// 重复投递同一个 job 不会二次结算；不存在的试跑直接忽略。
func TestStepTrialHandleIsIdempotent(t *testing.T) {
	e := newStepEnv(t)
	trial := e.runTrial(t, builtinTrialReq(), okReply())
	for range 3 {
		if err := e.svc.HandleStepTrial(context.Background(), trial.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.svc.HandleStepTrial(context.Background(), 99999); err != nil {
		t.Fatalf("不存在的试跑应忽略：%v", err)
	}
	if c := e.repo.Credits[stepActor]; c.Balance != 98 || c.Frozen != 0 || e.caller.Calls != 1 {
		t.Fatalf("不应重复扣费或重复调用：%+v calls=%d", c, e.caller.Calls)
	}
}

func TestStepGetTrialNotFound(t *testing.T) {
	e := newStepEnv(t)
	for _, id := range []string{"zzz", encodeTrialID(404)} {
		if _, err := e.svc.GetTrial(context.Background(), id); stepCodeOf(err) != 10002 {
			t.Fatalf("%s：应返回 10002：%v", id, err)
		}
	}
}
