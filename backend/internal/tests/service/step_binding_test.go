package service_test

import (
	"context"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/service/stepfake"
	"video-canvas/internal/stepkit"
)

// 本文件测试「设为生效」：必须有同环节、同候选、通过、24 小时内的试跑；绑定钉死版本；兜底退回内置。

func skillBinding(trial string) *model.SetStepBindingReq {
	return &model.SetStepBindingReq{Source: "skill", SkillName: "tvc", Version: 1, TrialID: trial}
}

func TestStepSetBindingRequiresPassedSameCandidateTrial(t *testing.T) {
	ctx := context.Background()
	e := newStepEnv(t)
	passed := e.passedTrialFor(t, skillTrialReq("tvc", 1)) // tvc v1 通过
	builtinPassed := func() string {
		e.repo.Enqueued = nil
		return e.passedTrialFor(t, builtinTrialReq())
	}()
	e.repo.Enqueued = nil
	// 一条没通过的试跑
	failedID := func() string {
		trial := e.runTrial(t, skillTrialReq("tvc", 1), stepfake.Reply{Text: stepBad}, stepfake.Reply{Text: stepBad})
		return encodeTrialID(trial.ID)
	}()
	// 一条调用失败的试跑
	e.repo.Enqueued = nil
	callFailed := func() string {
		trial := e.runTrial(t, skillTrialReq("tvc", 1), stepfake.Reply{Err: context.DeadlineExceeded})
		return encodeTrialID(trial.ID)
	}()
	// 另一个环节的通过试跑
	e.reg.MustRegister(newStepStep("test.other"))
	otherID := func() string {
		e.caller.Replies, e.caller.Calls = []stepfake.Reply{okReply()}, 0
		list, err := e.svc.CreateTrials(ctx, stepActor, "test.other", builtinTrialReq())
		if err != nil {
			t.Fatal(err)
		}
		e.handleAll(t)
		return list.Trials[0].ID
	}()

	cases := map[string]*model.SetStepBindingReq{
		"没有试跑":    skillBinding(encodeTrialID(9999)),
		"id 格式错误": skillBinding("not-hex"),
		"试跑未通过":   skillBinding(failedID),
		"试跑调用失败":  skillBinding(callFailed),
		"候选不同：来源": {Source: "builtin", TrialID: passed},                                    // 试跑是技能候选，要生效的是内置
		"候选不同：技能": {Source: "skill", SkillName: "tvc", Version: 1, TrialID: builtinPassed}, // 试跑是内置候选
		"候选不同：版本": {Source: "skill", SkillName: "tvc", Version: 2, TrialID: passed},
		"别的环节的试跑": skillBinding(otherID),
	}
	e.lib.Skills["tvc"].Versions[2] = "新方法" // 让 v2 可用，确保「候选不同：版本」拒绝的原因是试跑而不是技能不可用
	for name, req := range cases {
		_, err := e.svc.SetBinding(ctx, stepActor, stepKeyName, req)
		if stepCodeOf(err) != 68013 {
			t.Fatalf("%s：应返回 68013，实际 %v", name, err)
		}
	}
	if _, ok := e.repo.Bindings[stepKeyName]; ok || len(e.repo.Histories) != 0 {
		t.Fatal("被拒绝时不应改绑定或写历史")
	}
}

func TestStepSetBindingTrialExpires(t *testing.T) {
	ctx := context.Background()
	e := newStepEnv(t)
	id := e.passedTrialFor(t, skillTrialReq("tvc", 1))
	// 23 小时 59 分仍有效
	e.now = e.now.Add(24*time.Hour - time.Minute)
	if _, err := e.svc.SetBinding(ctx, stepActor, stepKeyName, skillBinding(id)); err != nil {
		t.Fatalf("24 小时内应有效：%v", err)
	}
	// 再过两分钟就超过 24 小时
	e.now = e.now.Add(2 * time.Minute)
	_, err := e.svc.SetBinding(ctx, stepActor, stepKeyName, skillBinding(id))
	wantStepCode(t, err, 68013)
}

func TestStepSetBindingSkillUnavailable(t *testing.T) {
	ctx := context.Background()
	cases := map[string]func(e *stepEnv){
		"技能被停用": func(e *stepEnv) { e.lib.Skills["tvc"].Enabled = false },
		"版本被删除": func(e *stepEnv) { delete(e.lib.Skills["tvc"].Versions, 1) },
		"技能被删除": func(e *stepEnv) { delete(e.lib.Skills, "tvc") },
	}
	for name, breakIt := range cases {
		e := newStepEnv(t)
		id := e.passedTrialFor(t, skillTrialReq("tvc", 1))
		breakIt(e)
		_, err := e.svc.SetBinding(ctx, stepActor, stepKeyName, skillBinding(id))
		if stepCodeOf(err) != 68014 {
			t.Fatalf("%s：应返回 68014，实际 %v", name, err)
		}
	}
}

func TestStepSetBindingSucceedsAndPinsVersion(t *testing.T) {
	ctx := context.Background()
	e := newStepEnv(t)
	id := e.passedTrialFor(t, skillTrialReq("tvc", 1))
	d, err := e.svc.SetBinding(ctx, stepActor, stepKeyName, skillBinding(id))
	if err != nil {
		t.Fatal(err)
	}
	if d.Binding.Source != "skill" || d.Binding.SkillName != "tvc" || d.Binding.SkillTitle != "TVC 导演" || d.Binding.Version != 1 || d.Effective.Source != "skill" || d.Effective.Fallback {
		t.Fatalf("绑定与生效方法不对：%+v %+v", d.Binding, d.Effective)
	}
	if len(d.History) != 1 || d.History[0].From.Source != "builtin" || d.History[0].To.SkillName != "tvc" || d.History[0].To.Version != 1 ||
		d.History[0].Actor != "admin-nine" || d.History[0].TrialID == nil || *d.History[0].TrialID != id {
		t.Fatalf("历史应记录从内置换成 tvc v1、操作人和依据试跑：%+v", d.History)
	}
	acts := e.audit.Actions()
	if acts[len(acts)-1] != model.AdminAuditStepBinding {
		t.Fatalf("生效应写审计：%v", acts)
	}
	b := e.repo.Bindings[stepKeyName]
	if b.Source != "skill" || *b.SkillID != 7 || *b.SkillVersion != 1 || b.UpdatedBy != stepActor {
		t.Fatalf("绑定行不对：%+v", b)
	}

	// 版本钉死：技能发了新版本 v2 并设为生效，绑定仍是 v1，列表给出「有新版本 v2」；运行时读的也是 v1
	e.lib.Skills["tvc"].Versions[2] = "新方法"
	e.lib.Skills["tvc"].Active = 2
	list, _ := e.svc.List(ctx)
	if list[0].Binding.Version != 1 || list[0].NewerVersion == nil || *list[0].NewerVersion != 2 {
		t.Fatalf("绑定应不变并提示新版本：%+v", list[0])
	}
	m, err := e.svc.Resolver().Method(ctx, mustStep(t, e))
	if err != nil || m.Version != 1 || m.Body != "旧方法" {
		t.Fatalf("运行时应读钉死的 v1：%+v %v", m, err)
	}
}

func mustStep(t *testing.T, e *stepEnv) stepkit.Step {
	t.Helper()
	st, ok := e.reg.Get(stepKeyName)
	if !ok {
		t.Fatal("环节未注册")
	}
	return st
}

// 换回内置也要试跑：恢复（切回内置）同样需要内置候选的通过试跑。
func TestStepSetBindingBackToBuiltinNeedsTrial(t *testing.T) {
	ctx := context.Background()
	e := newStepEnv(t)
	skillTrial := e.passedTrialFor(t, skillTrialReq("tvc", 1))
	if _, err := e.svc.SetBinding(ctx, stepActor, stepKeyName, skillBinding(skillTrial)); err != nil {
		t.Fatal(err)
	}
	_, err := e.svc.SetBinding(ctx, stepActor, stepKeyName, &model.SetStepBindingReq{Source: "builtin", TrialID: skillTrial})
	wantStepCode(t, err, 68013)

	e.repo.Enqueued = nil
	builtinTrial := e.passedTrialFor(t, builtinTrialReq())
	d, err := e.svc.SetBinding(ctx, stepActor, stepKeyName, &model.SetStepBindingReq{Source: "builtin", TrialID: builtinTrial})
	if err != nil {
		t.Fatal(err)
	}
	if d.Binding.Source != "builtin" || len(d.History) != 2 || d.History[0].From.Source != "skill" || d.History[0].To.Source != "builtin" {
		t.Fatalf("应记录 tvc → 内置：%+v", d.History)
	}
}

// 绑定的技能被停用 / 版本被删 / 技能被删：列表标记兜底原因，运行时退回内置并写一次审计。
func TestStepFallbackWhenBoundSkillGone(t *testing.T) {
	ctx := context.Background()
	cases := map[string]func(e *stepEnv){
		"停用":   func(e *stepEnv) { e.lib.Skills["tvc"].Enabled = false },
		"版本删除": func(e *stepEnv) { delete(e.lib.Skills["tvc"].Versions, 1) },
		"技能删除": func(e *stepEnv) { delete(e.lib.Skills, "tvc"); delete(e.repo.SkillNames, 7) },
	}
	for name, breakIt := range cases {
		e := newStepEnv(t)
		id := e.passedTrialFor(t, skillTrialReq("tvc", 1))
		if _, err := e.svc.SetBinding(ctx, stepActor, stepKeyName, skillBinding(id)); err != nil {
			t.Fatal(err)
		}
		breakIt(e)

		list, err := e.svc.List(ctx)
		if err != nil || list[0].Effective.Source != "builtin" || !list[0].Effective.Fallback || list[0].Effective.FallbackReason == "" {
			t.Fatalf("%s：列表应标记兜底：%+v %v", name, list, err)
		}
		before := len(e.audit.Actions())
		for range 3 {
			m, err := e.svc.Resolver().Method(ctx, mustStep(t, e))
			if err != nil || m.Source != "builtin" || !m.Fallback {
				t.Fatalf("%s：运行时应退回内置：%+v %v", name, m, err)
			}
		}
		acts := e.audit.Actions()[before:]
		if len(acts) != 1 || acts[0] != model.AdminAuditStepFallback {
			// Resolver() 每次返回新对象但去重表在服务上：同一原因整个进程只审计一次
			t.Fatalf("%s：兜底应只审计一次：%v", name, acts)
		}
	}
}

// 技能库读失败（不是技能不可用）时，运行时把错误交给调用方，不悄悄退回内置。
func TestStepResolverPropagatesLibraryError(t *testing.T) {
	ctx := context.Background()
	e := newStepEnv(t)
	id := e.passedTrialFor(t, skillTrialReq("tvc", 1))
	if _, err := e.svc.SetBinding(ctx, stepActor, stepKeyName, skillBinding(id)); err != nil {
		t.Fatal(err)
	}
	e.lib.Err = stepfake.ErrInjected
	if _, err := e.svc.Resolver().Method(ctx, mustStep(t, e)); err == nil {
		t.Fatal("应透传技能库错误")
	}
}

func TestStepResolverDefaultsToBuiltin(t *testing.T) {
	e := newStepEnv(t)
	m, err := e.svc.Resolver().Method(context.Background(), mustStep(t, e))
	if err != nil || m.Source != "builtin" || m.Fallback || m.Body != "内置方法" {
		t.Fatalf("%+v %v", m, err)
	}
}
