package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"video-canvas/internal/marketing"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	. "video-canvas/internal/service"
)

// ledgerSum 汇总用户营销环节某类流水的金额。
func (e *mktEnv) ledgerSum(typ string) int {
	e.t.Helper()
	var n int
	if err := e.db.Model(&model.CreditLedger{}).Where("user_id = ? AND type = ? AND marketing_step_run_id IS NOT NULL", e.user, typ).
		Select("COALESCE(SUM(amount), 0)").Scan(&n).Error; err != nil {
		e.t.Fatal(err)
	}
	return n
}

func TestMarketing_CreateProject_FreezesPerStepAndEnqueues(t *testing.T) {
	env := newMktEnv(t, 100)
	v := env.createProject(mktSettings())
	if len(v.Rounds) != 1 || v.Rounds[0].Status != model.MarketingRoundScripting || v.Rounds[0].No != 1 {
		t.Fatalf("应有一个写脚本中的第一轮：%+v", v.Rounds)
	}
	r := v.Rounds[0]
	if r.PlanCount != 2 || r.Frozen <= 0 {
		t.Fatalf("计划份数和冻结积分不对：plan=%d frozen=%d", r.PlanCount, r.Frozen)
	}
	if env.queue.count() != 1 {
		t.Fatalf("应入队一次，实际 %d", env.queue.count())
	}
	if got := env.credit().Frozen; got != r.Frozen {
		t.Fatalf("账户冻结 %d 应等于轮次冻结 %d", got, r.Frozen)
	}
	if env.ledgerSum(model.LedgerFreeze) != r.Frozen {
		t.Fatalf("每个环节一条冻结流水，合计应为 %d", r.Frozen)
	}
	if facts, create := stepOf(&r, "facts"), stepOf(&r, "create"); facts == nil || create == nil || facts.Status != model.MarketingRunQueued {
		t.Fatalf("环节运行不对：%+v", r.Steps)
	}
	if stepOf(&r, "reverse") != nil || stepOf(&r, "follow") != nil {
		t.Fatal("无参考时不应有 reverse / follow 环节")
	}
	if v.Title != "未命名商品" {
		t.Fatalf("事实卡出来之前的标题：%q", v.Title)
	}
}

func TestMarketing_Round_NoRef_ProducesScriptsAndSettles(t *testing.T) {
	env := newMktEnv(t, 100)
	v := env.createProject(mktSettings())
	env.runRound(&v.Rounds[0])
	got := env.project(v.ID)
	r := got.Rounds[0]
	if r.Status != model.MarketingRoundReady || len(r.Scripts) != 2 {
		t.Fatalf("应 ready 且有 2 份脚本：status=%s scripts=%d err=%s", r.Status, len(r.Scripts), r.Error)
	}
	if got.Product == nil || got.Product.Name != "晨雾香水 50ml" || got.Title != "晨雾香水 50ml" {
		t.Fatalf("事实卡应写进项目并带出标题：%+v title=%q", got.Product, got.Title)
	}
	for _, s := range r.Scripts {
		if s.Kind != "create" || s.Status != "draft" || len(s.Shots) != 3 || s.Method == nil || s.Method.Source != "builtin" {
			t.Fatalf("脚本不对：%+v", s)
		}
	}
	if r.Scripts[0].Angle != "pain" || r.Scripts[1].Angle != "lifestyle" {
		t.Fatalf("脚本按角度顺序：%s %s", r.Scripts[0].Angle, r.Scripts[1].Angle)
	}
	for _, name := range []string{"facts", "create"} {
		if st := stepOf(&r, name); st == nil || st.Status != model.MarketingRunSucceeded || st.Charged <= 0 || st.Attempts != 1 {
			t.Fatalf("环节 %s 应成功并扣费：%+v", name, st)
		}
	}
	c := env.credit()
	if c.Frozen != 0 || c.Balance != 100-r.Charged || r.Charged <= 0 {
		t.Fatalf("结算后应无冻结、余额扣掉实际用量：%+v charged=%d", c, r.Charged)
	}
	if env.ledgerSum(model.LedgerSettle) != r.Charged {
		t.Fatalf("结算流水合计应等于已扣积分")
	}
	if env.ledgerSum(model.LedgerSettle)+env.ledgerSum(model.LedgerRefund) != env.ledgerSum(model.LedgerFreeze) {
		t.Fatal("扣费加退款应等于冻结")
	}
}

func TestMarketing_Round_ReusesProjectFacts(t *testing.T) {
	env := newMktEnv(t, 100)
	v := env.createProject(mktSettings())
	env.runRound(&v.Rounds[0])
	round, err := env.svc.NewRound(context.Background(), env.user, uint64(v.ID), &model.CreateMarketingRoundReq{Settings: mktSettings()})
	if err != nil {
		t.Fatal(err)
	}
	if facts := stepOf(round, "facts"); facts == nil || facts.Status != model.MarketingRunReused {
		t.Fatalf("已有事实卡应沿用：%+v", facts)
	}
	env.runRound(round)
	if env.caller.count(marketing.StepFacts) != 1 {
		t.Fatalf("事实卡只该调一次模型，实际 %d", env.caller.count(marketing.StepFacts))
	}
	got := env.round(v.ID, round.ID)
	if got.No != 2 || got.Status != model.MarketingRoundReady || len(got.Scripts) != 2 {
		t.Fatalf("第二轮应 ready：%+v", got)
	}
}

func TestMarketing_NewRound_BusyWhileScripting(t *testing.T) {
	env := newMktEnv(t, 100)
	v := env.createProject(mktSettings())
	_, err := env.svc.NewRound(context.Background(), env.user, uint64(v.ID), &model.CreateMarketingRoundReq{Settings: mktSettings()})
	wantMktCode(t, err, errcode.ErrMarketingRoundBusy.Code)
}

func TestMarketing_Round_WithRef_FollowAndCreate(t *testing.T) {
	env := newMktEnv(t, 200)
	env.caller.on(marketing.StepFollow, func(int) (string, error) { return followReply(10, "HERALIFE 销量第一，一抹就润"), nil })
	env.caller.on(marketing.StepCreate, func(int) (string, error) { return createReply(10, "pain"), nil })
	v := env.createRefProject()
	if v.Rounds[0].RefVideo == nil || v.Rounds[0].RefVideo.Duration != 10 || v.Rounds[0].PlanCount != 2 {
		t.Fatalf("参考视频与计划份数：%+v plan=%d", v.Rounds[0].RefVideo, v.Rounds[0].PlanCount)
	}
	env.runRound(&v.Rounds[0])
	r := env.round(v.ID, v.Rounds[0].ID)
	if r.Status != model.MarketingRoundReady || len(r.Scripts) != 2 || r.Scripts[0].Kind != "follow" || r.Scripts[0].Angle != "follow" {
		t.Fatalf("跟拍在前、共 2 份：%+v", r.Scripts)
	}
	if r.Reverse == nil || r.ReverseRevision != 1 || r.ReverseEdited || r.ReverseDirty || len(r.Reverse.Shots) != 4 {
		t.Fatalf("拆解应落库且未改动：rev=%v revision=%d edited=%v dirty=%v", r.Reverse != nil, r.ReverseRevision, r.ReverseEdited, r.ReverseDirty)
	}
	follow := r.Scripts[0]
	for i, sh := range follow.Shots {
		if sh.RefShot == nil || *sh.RefShot != i+1 {
			t.Fatalf("跟拍每镜应回填对应参考镜头：第 %d 镜 %+v", i, sh.RefShot)
		}
	}
	types := map[string]bool{}
	for _, f := range follow.Flags {
		types[f.Type] = true
	}
	if !types["brand"] || !types["claim"] {
		t.Fatalf("跟拍应标出第三方品牌和无依据的背书：%+v", follow.Flags)
	}
	raw, _ := json.Marshal(follow.Flags)
	if !strings.Contains(string(raw), `"shot":0`) {
		t.Fatalf("flags[].shot 从 0 起且不省略：%s", raw)
	}
	if r.Scripts[1].Kind != "create" || len(r.Scripts[1].Flags) != 0 {
		t.Fatalf("创意脚本：%+v", r.Scripts[1])
	}
	var row model.MarketingRound
	if err := env.db.Where("id = ?", uint64(r.ID)).Take(&row).Error; err != nil || len(row.FollowBasis) == 0 {
		t.Fatalf("跟拍成功后应记下据以写成的拆解：%v", err)
	}
}

func TestMarketing_Round_ReverseFails_FollowSkipped_CreateContinues(t *testing.T) {
	env := newMktEnv(t, 200)
	env.caller.on(marketing.StepReverse, func(int) (string, error) { return "不是 JSON", nil })
	env.caller.on(marketing.StepCreate, func(int) (string, error) { return createReply(10, "pain"), nil })
	v := env.createRefProject()
	env.runRound(&v.Rounds[0])
	r := env.round(v.ID, v.Rounds[0].ID)
	if r.Status != model.MarketingRoundReady || len(r.Scripts) != 1 || r.Scripts[0].Kind != "create" {
		t.Fatalf("拆解失败时创意脚本照常产出：%+v", r)
	}
	if rev := stepOf(r, "reverse"); rev.Status != model.MarketingRunFailed || rev.Error == "" || rev.Attempts != 2 {
		t.Fatalf("拆解应失败（重写过一次）并带原因：%+v", rev)
	}
	if f := stepOf(r, "follow"); f.Status != model.MarketingRunSkipped || f.Error == "" {
		t.Fatalf("跟拍应被跳过：%+v", f)
	}
	if env.caller.count(marketing.StepFollow) != 0 {
		t.Fatal("跳过的跟拍不能调模型")
	}
	if env.credit().Frozen != 0 {
		t.Fatalf("全部结束后不应还有冻结：%+v", env.credit())
	}
}

func TestMarketing_Round_FactsFail_WholeRoundFailsAndUnfreezes(t *testing.T) {
	env := newMktEnv(t, 200)
	env.caller.on(marketing.StepFacts, func(int) (string, error) { return "", errors.New("上游 503") })
	v := env.createRefProject()
	env.runRound(&v.Rounds[0])
	r := env.round(v.ID, v.Rounds[0].ID)
	if r.Status != model.MarketingRoundFailed || r.Error == "" || len(r.Scripts) != 0 {
		t.Fatalf("事实卡失败应整轮失败并带原因：%+v", r)
	}
	if facts := stepOf(r, "facts"); facts.Status != model.MarketingRunFailed || facts.Error == "" {
		t.Fatalf("事实卡运行：%+v", facts)
	}
	for _, name := range []string{"follow", "create"} {
		if st := stepOf(r, name); st.Status != model.MarketingRunSkipped {
			t.Fatalf("%s 应被跳过：%+v", name, st)
		}
	}
	if c := env.credit(); c.Frozen != 0 {
		t.Fatalf("整轮失败应解冻：%+v", c)
	}
	if env.caller.count(marketing.StepCreate) != 0 {
		t.Fatal("事实卡失败时不能写创意脚本")
	}
}

func TestMarketing_RetryFacts_RerunsDependents(t *testing.T) {
	env := newMktEnv(t, 200)
	env.caller.on(marketing.StepFacts, func(n int) (string, error) {
		if n == 1 {
			return "", errors.New("上游 503")
		}
		return factsReply(), nil
	})
	v := env.createProject(mktSettings())
	env.runRound(&v.Rounds[0])
	failed := env.round(v.ID, v.Rounds[0].ID)
	if failed.Status != model.MarketingRoundFailed {
		t.Fatalf("第一次应失败：%s", failed.Status)
	}
	retried, err := env.svc.RetryStep(context.Background(), env.user, uint64(stepOf(failed, "facts").ID))
	if err != nil {
		t.Fatal(err)
	}
	if retried.Status != model.MarketingRoundScripting || retried.Error != "" {
		t.Fatalf("重试后轮次回到写脚本中：%+v", retried)
	}
	if stepOf(retried, "facts").Status != model.MarketingRunQueued || stepOf(retried, "create").Status != model.MarketingRunQueued {
		t.Fatalf("事实卡与依赖它的创意脚本一起重跑：%+v", retried.Steps)
	}
	if env.queue.count() != 2 {
		t.Fatalf("重试应再入队一次，实际 %d", env.queue.count())
	}
	env.runRound(retried)
	done := env.round(v.ID, v.Rounds[0].ID)
	if done.Status != model.MarketingRoundReady || len(done.Scripts) != 2 {
		t.Fatalf("重试后应 ready：%+v", done)
	}
	if env.credit().Frozen != 0 {
		t.Fatalf("重试结算后不应有冻结：%+v", env.credit())
	}
}

func TestMarketing_Retry_OnlyFailedSteps(t *testing.T) {
	env := newMktEnv(t, 200)
	v := env.createProject(mktSettings())
	env.runRound(&v.Rounds[0])
	ready := env.round(v.ID, v.Rounds[0].ID)
	_, err := env.svc.RetryStep(context.Background(), env.user, uint64(stepOf(ready, "create").ID))
	wantMktCode(t, err, errcode.ErrInvalidParams.Code)
}

func TestMarketing_CreateProject_InsufficientCredits(t *testing.T) {
	env := newMktEnv(t, 1)
	_, err := env.svc.CreateProject(context.Background(), env.user, &model.CreateMarketingProjectReq{ProductAssetIDs: []uint64{1}, Settings: mktSettings()})
	wantMktCode(t, err, errcode.ErrInsufficientCredits.Code)
	var n int64
	env.db.Model(&model.MarketingProject{}).Count(&n)
	if n != 0 {
		t.Fatal("积分不足时不能留下项目（同一事务回滚）")
	}
}

func TestMarketing_CreateProject_ValidationErrors(t *testing.T) {
	env := newMktEnv(t, 200)
	ctx := context.Background()
	st := mktSettings()
	badModel := st
	badModel.VideoModel = "nope"
	manyAngles := mktRefSettings()
	manyAngles.Angles = []string{"pain", "feature", "mood", "unbox"}
	retired := st
	retired.Platform = "amazon"
	cases := []struct {
		name string
		req  model.CreateMarketingProjectReq
		code int
	}{
		{"商品图是别人的", model.CreateMarketingProjectReq{ProductAssetIDs: []uint64{999}, Settings: st}, errcode.ErrMarketingAssets.Code},
		{"商品图用了视频素材", model.CreateMarketingProjectReq{ProductAssetIDs: []uint64{4}, Settings: st}, errcode.ErrMarketingAssets.Code},
		{"商品图太多", model.CreateMarketingProjectReq{ProductAssetIDs: []uint64{1, 2, 3, 1, 2}, Settings: st}, errcode.ErrMarketingAssets.Code},
		{"视频模型不存在", model.CreateMarketingProjectReq{ProductAssetIDs: []uint64{1}, Settings: badModel}, errcode.ErrMarketingVideoModel.Code},
		{"参考视频不是视频", model.CreateMarketingProjectReq{ProductAssetIDs: []uint64{1}, RefVideoAssetID: idp(2), Settings: mktRefSettings()}, errcode.ErrMarketingAssets.Code},
		{"改编份数超限", model.CreateMarketingProjectReq{ProductAssetIDs: []uint64{1}, RefVideoAssetID: idp(4), Settings: manyAngles}, errcode.ErrInvalidParams.Code},
		{"投放平台已下线", model.CreateMarketingProjectReq{ProductAssetIDs: []uint64{1}, Settings: retired}, errcode.ErrInvalidParams.Code},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := env.svc.CreateProject(ctx, env.user, &c.req)
			wantMktCode(t, err, c.code)
		})
	}
}

func TestMarketing_RefVideoTooLong(t *testing.T) {
	env := newMktEnv(t, 200)
	env.assets.put(env.user, 5, "video", "video/mp4", 16000)
	_, err := env.svc.CreateProject(context.Background(), env.user, &model.CreateMarketingProjectReq{ProductAssetIDs: []uint64{1}, RefVideoAssetID: idp(5), Settings: mktRefSettings()})
	wantMktCode(t, err, errcode.ErrMarketingRefTooLong.Code)
}

func TestMarketing_NoTextModel(t *testing.T) {
	env := newMktEnv(t, 200)
	delete(env.models.Snapshots, "text-a")
	_, err := env.svc.CreateProject(context.Background(), env.user, &model.CreateMarketingProjectReq{ProductAssetIDs: []uint64{1}, Settings: mktSettings()})
	wantMktCode(t, err, errcode.ErrMarketingNoTextModel.Code)
}

func TestMarketing_Cancel_StopsRunningAndRefunds(t *testing.T) {
	env := newMktEnv(t, 200)
	env.caller.block[marketing.StepFacts] = make(chan struct{})
	env.caller.started[marketing.StepFacts] = make(chan struct{})
	v := env.createProject(mktSettings())
	done := make(chan error, 1)
	go func() { done <- env.svc.HandleMarketingRound(context.Background(), uint64(v.Rounds[0].ID)) }()
	select {
	case <-env.caller.started[marketing.StepFacts]:
	case <-time.After(10 * time.Second):
		t.Fatal("事实卡没有开始运行")
	}
	canceled, err := env.svc.CancelRound(context.Background(), env.user, uint64(v.Rounds[0].ID))
	if err != nil {
		t.Fatal(err)
	}
	if canceled.Status != model.MarketingRoundCanceled {
		t.Fatalf("轮次应为 canceled：%s", canceled.Status)
	}
	if st := stepOf(canceled, "create"); st.Status != model.MarketingRunCanceled {
		t.Fatalf("排队中的环节应立即取消：%+v", st)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("取消后进行中的环节没有被中断")
	}
	r := env.round(v.ID, v.Rounds[0].ID)
	if r.Status != model.MarketingRoundCanceled || len(r.Scripts) != 0 {
		t.Fatalf("取消后不应有脚本：%+v", r)
	}
	if st := stepOf(r, "facts"); st.Status != model.MarketingRunCanceled {
		t.Fatalf("进行中的环节应记为取消：%+v", st)
	}
	if env.credit().Frozen != 0 {
		t.Fatalf("取消后应全部解冻：%+v", env.credit())
	}
	var proj model.MarketingProject
	if err := env.db.Where("id = ?", uint64(v.ID)).Take(&proj).Error; err != nil || len(proj.Product) != 0 {
		t.Fatalf("取消后不应写入事实卡：%v", err)
	}
}

func TestMarketing_Cancel_AlreadyFinishedIsNoop(t *testing.T) {
	env := newMktEnv(t, 200)
	v := env.createProject(mktSettings())
	env.runRound(&v.Rounds[0])
	r, err := env.svc.CancelRound(context.Background(), env.user, uint64(v.Rounds[0].ID))
	if err != nil || r.Status != model.MarketingRoundReady {
		t.Fatalf("已结束的轮次取消应原样返回：%v %+v", err, r)
	}
}

func TestMarketing_IsolationBetweenUsers(t *testing.T) {
	env := newMktEnv(t, 200)
	v := env.createProject(mktSettings())
	other := env.user + 1000
	ctx := context.Background()
	_, err := env.svc.GetProject(ctx, other, uint64(v.ID))
	wantMktCode(t, err, errcode.ErrMarketingNotFound.Code)
	_, err = env.svc.CancelRound(ctx, other, uint64(v.Rounds[0].ID))
	wantMktCode(t, err, errcode.ErrMarketingNotFound.Code)
	wantMktCode(t, env.svc.DeleteProject(ctx, other, uint64(v.ID)), errcode.ErrMarketingNotFound.Code)
	_, err = env.svc.RetryStep(ctx, other, uint64(v.Rounds[0].Steps[0].ID))
	wantMktCode(t, err, errcode.ErrMarketingNotFound.Code)
}

func TestMarketing_PushesRoundUpdates(t *testing.T) {
	env := newMktEnv(t, 200)
	v := env.createProject(mktSettings())
	env.runRound(&v.Rounds[0])
	env.bc.mu.Lock()
	defer env.bc.mu.Unlock()
	n := 0
	for _, m := range env.bc.msgs {
		if m.Type == "marketing.round" {
			n++
		}
	}
	if n < 3 {
		t.Fatalf("环节状态变化应推送 marketing.round，实际 %d 条", n)
	}
}

func TestMarketing_ListProjects_Summary(t *testing.T) {
	env := newMktEnv(t, 200)
	v := env.createProject(mktSettings())
	items, total, err := env.svc.ListProjects(context.Background(), env.user, "", 1, 20)
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("列表：%v total=%d", err, total)
	}
	it := items[0]
	if !it.Scripting || len(it.Bars) != 2 || it.Bars[0] != "rendering" || it.CoverURL == "" {
		t.Fatalf("写脚本中的轮次按计划份数填 rendering，封面取第一张商品图：%+v", it)
	}
	env.runRound(&v.Rounds[0])
	items, _, _ = env.svc.ListProjects(context.Background(), env.user, "", 1, 20)
	it = items[0]
	if it.Scripting || it.ScriptCounts.Draft != 2 || len(it.Bars) != 2 || it.Bars[0] != "draft" || it.LastRoundFailed {
		t.Fatalf("完成后：%+v", it)
	}
	if it.Title != "晨雾香水 50ml" {
		t.Fatalf("标题：%q", it.Title)
	}
}

func TestMarketing_ListProjects_Keyword(t *testing.T) {
	env := newMktEnv(t, 200)
	v := env.createProject(mktSettings())
	env.runRound(&v.Rounds[0])
	ctx := context.Background()
	items, total, err := env.svc.ListProjects(ctx, env.user, "香水", 1, 20)
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("标题含关键词应命中：%v total=%d", err, total)
	}
	items, total, err = env.svc.ListProjects(ctx, env.user, "咖啡", 1, 20)
	if err != nil || total != 0 || len(items) != 0 {
		t.Fatalf("标题不含关键词应为空：%v total=%d", err, total)
	}
	if _, total, _ = env.svc.ListProjects(ctx, env.user, "%", 1, 20); total != 0 {
		t.Fatalf("通配符按字面匹配：total=%d", total)
	}
}

func TestMarketing_Quote(t *testing.T) {
	env := newMktEnv(t, 200)
	ctx := context.Background()
	q, err := env.svc.Quote(ctx, env.user, &model.MarketingQuoteReq{Settings: mktSettings()})
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Steps) != 2 || q.Steps[0].Step != "facts" || q.Steps[1].Step != "create" || q.Credits != q.Steps[0].Credits+q.Steps[1].Credits || q.Credits <= 0 {
		t.Fatalf("无参考只列 facts、create：%+v", q)
	}
	qr, err := env.svc.Quote(ctx, env.user, &model.MarketingQuoteReq{HasRef: true, Settings: mktRefSettings()})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range qr.Steps {
		names = append(names, s.Step)
	}
	if strings.Join(names, ",") != "facts,reverse,follow,create" {
		t.Fatalf("有参考应列全部 4 个环节：%v", names)
	}
	v := env.createProject(mktSettings())
	env.runRound(&v.Rounds[0])
	pid := v.ID
	qp, err := env.svc.Quote(ctx, env.user, &model.MarketingQuoteReq{ProjectID: &pid, HasRef: true, Settings: mktRefSettings()})
	if err != nil {
		t.Fatal(err)
	}
	if qp.Steps[0].Step != "facts" || qp.Steps[0].Credits != 0 || qp.Credits != qr.Credits-qr.Steps[0].Credits {
		t.Fatalf("项目已有事实卡时 facts 为 0 但仍列出：%+v", qp)
	}
	_, err = env.svc.Quote(ctx, env.user+1, &model.MarketingQuoteReq{ProjectID: &pid, Settings: mktSettings()})
	wantMktCode(t, err, errcode.ErrMarketingNotFound.Code)
}

func TestMarketing_FailRate7d(t *testing.T) {
	env := newMktEnv(t, 200)
	env.caller.on(marketing.StepFacts, func(n int) (string, error) {
		if n == 2 {
			return "", errors.New("上游 503")
		}
		return factsReply(), nil
	})
	stats := NewMarketingStats(env.repo, nil)
	rate, err := stats.FailRate7d(context.Background(), marketing.StepFacts)
	if err != nil || rate != 0 {
		t.Fatalf("没有记录失败率为 0：%v %v", rate, err)
	}
	// 第一个项目事实卡成功；第二个项目事实卡失败：成功 1 失败 1。
	v1 := env.createProject(mktSettings())
	env.runRound(&v1.Rounds[0])
	v2 := env.createProject(mktSettings())
	env.runRound(&v2.Rounds[0])
	rate, err = stats.FailRate7d(context.Background(), marketing.StepFacts)
	if err != nil || rate != 0.5 {
		t.Fatalf("失败率应为 0.5：%v %v", rate, err)
	}
	if r, _ := stats.FailRate7d(context.Background(), "drama.plan"); r != 0 {
		t.Fatalf("非营销环节按 0：%v", r)
	}
	old, err := env.repo.FailRate7d(context.Background(), marketing.StepFacts, time.Now().Add(time.Hour))
	if err != nil || old != 0 {
		t.Fatalf("窗口之外的记录不算：%v %v", old, err)
	}
}

func TestMarketing_Options_HidesRetiredPlatforms(t *testing.T) {
	env := newMktEnv(t, 200)
	for _, p := range env.svc.Options().Platforms {
		if p.Key == "amazon" {
			t.Fatalf("选项里不应再有已下线的亚马逊：%+v", p)
		}
	}
}
