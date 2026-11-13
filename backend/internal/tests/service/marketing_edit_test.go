package service_test

import (
	"context"
	"strings"
	"testing"

	"video-canvas/internal/marketing"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/videoframe"
	"video-canvas/internal/service/stepfake"
)

// refRound 建带参考片的项目并跑完第一轮（跟拍 + 1 份创意），返回项目 id、轮次视图。
func (e *mktEnv) refRound() (*model.MarketingProjectView, *model.MarketingRoundView) {
	e.t.Helper()
	e.caller.on(marketing.StepCreate, func(int) (string, error) { return createReply(10, "pain"), nil })
	v := e.createRefProject()
	e.runRound(&v.Rounds[0])
	return v, e.round(v.ID, v.Rounds[0].ID)
}

// editedReverse 复制拆解并改第 i 个镜头的动作。
func editedReverse(r *model.MarketingReverse, i int, action string) model.MarketingReverse {
	out := *r
	out.Shots = append([]model.MarketingReverseShot(nil), r.Shots...)
	out.Shots[i].Action = action
	return out
}

func TestMarketing_UpdateReverse_SavesEditsAndMarksDirty(t *testing.T) {
	env := newMktEnv(t, 300)
	v, r := env.refRound()
	ctx := context.Background()
	edited := editedReverse(r.Reverse, 1, "左手挤出一粒豌豆大的乳霜，指腹在手背推开")
	// 请求里夹带的其他字段改动（景别、时间）必须被忽略，以服务端为准。
	edited.Shots[1].ShotSize = "远景"
	edited.Shots[1].End = 99
	got, err := env.svc.UpdateReverse(ctx, env.user, uint64(r.ID), &model.UpdateMarketingReverseReq{Reverse: edited, ReverseRevision: r.ReverseRevision})
	if err != nil {
		t.Fatal(err)
	}
	if got.ReverseRevision != r.ReverseRevision+1 || !got.ReverseEdited || !got.ReverseDirty {
		t.Fatalf("改后应 revision+1、已编辑、与跟拍据以写成的拆解不同：%+v", got)
	}
	s1 := got.Reverse.Shots[1]
	if s1.Action != "左手挤出一粒豌豆大的乳霜，指腹在手背推开" || s1.ShotSize != "特写" || s1.End != r.Reverse.Shots[1].End {
		t.Fatalf("只认 action 等四个字段：%+v", s1)
	}
	// 旧 revision 提交：冲突。
	_, err = env.svc.UpdateReverse(ctx, env.user, uint64(r.ID), &model.UpdateMarketingReverseReq{Reverse: edited, ReverseRevision: r.ReverseRevision})
	wantMktCode(t, err, errcode.ErrMarketingConflict.Code)
	// 恢复原稿：不再 edited 也不再 dirty。
	restored, err := env.svc.RestoreReverse(ctx, env.user, uint64(r.ID))
	if err != nil {
		t.Fatal(err)
	}
	if restored.ReverseEdited || restored.ReverseDirty || restored.Reverse.Shots[1].Action != r.Reverse.Shots[1].Action {
		t.Fatalf("恢复原稿：%+v", restored)
	}
	_ = v
}

func TestMarketing_UpdateReverse_RejectsInvalid(t *testing.T) {
	env := newMktEnv(t, 300)
	_, r := env.refRound()
	ctx := context.Background()
	rev := r.Reverse.Shots
	badRole := editedReverse(r.Reverse, 0, rev[0].Action)
	badRole.Shots[0].ProductRole = "weird"
	fewer := *r.Reverse
	fewer.Shots = rev[:3]
	renumbered := editedReverse(r.Reverse, 0, rev[0].Action)
	renumbered.Shots[0].ID = 9
	cases := map[string]model.MarketingReverse{
		"动作为空":    editedReverse(r.Reverse, 0, ""),
		"动作只是名词":  editedReverse(r.Reverse, 0, "护手霜"),
		"商品角色不合法": badRole,
		"镜头数量不能改": fewer,
		"镜头编号不能改": renumbered,
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := env.svc.UpdateReverse(ctx, env.user, uint64(r.ID), &model.UpdateMarketingReverseReq{Reverse: edit, ReverseRevision: r.ReverseRevision})
			wantMktCode(t, err, errcode.ErrMarketingReverseBad.Code)
		})
	}
}

func TestMarketing_UpdateReverse_NoReverse(t *testing.T) {
	env := newMktEnv(t, 300)
	v := env.createProject(mktSettings())
	env.runRound(&v.Rounds[0])
	_, err := env.svc.UpdateReverse(context.Background(), env.user, uint64(v.Rounds[0].ID), &model.UpdateMarketingReverseReq{})
	wantMktCode(t, err, errcode.ErrMarketingNoReverse.Code)
	_, err = env.svc.Refollow(context.Background(), env.user, uint64(v.Rounds[0].ID))
	wantMktCode(t, err, errcode.ErrMarketingNoReverse.Code)
}

func TestMarketing_Refollow_OnlyRerunsFollowInPlace(t *testing.T) {
	env := newMktEnv(t, 400)
	env.caller.on(marketing.StepFollow, func(n int) (string, error) {
		if n == 1 {
			return followReply(10, "一抹就润"), nil
		}
		return followReply(10, "改后的旁白"), nil
	})
	v, r := env.refRound()
	ctx := context.Background()
	before := r.Scripts[0]
	chargedBefore := r.Charged
	edited := editedReverse(r.Reverse, 2, "左手把护手霜涂满右手手背，来回揉匀")
	if _, err := env.svc.UpdateReverse(ctx, env.user, uint64(r.ID), &model.UpdateMarketingReverseReq{Reverse: edited, ReverseRevision: r.ReverseRevision}); err != nil {
		t.Fatal(err)
	}
	re, err := env.svc.Refollow(ctx, env.user, uint64(r.ID))
	if err != nil {
		t.Fatal(err)
	}
	if re.Status != model.MarketingRoundScripting || stepOf(re, "follow").Status != model.MarketingRunQueued || stepOf(re, "create").Status != model.MarketingRunSucceeded {
		t.Fatalf("只重跑跟拍：%+v", re.Steps)
	}
	if re.Scripts[0].Status != model.MarketingScriptRewriting {
		t.Fatalf("跟拍脚本应变为 rewriting：%s", re.Scripts[0].Status)
	}
	// 重写中不能改拆解或脚本。
	_, err = env.svc.UpdateReverse(ctx, env.user, uint64(r.ID), &model.UpdateMarketingReverseReq{Reverse: edited, ReverseRevision: r.ReverseRevision + 1})
	wantMktCode(t, err, errcode.ErrMarketingScriptBusy.Code)
	env.runRound(re)
	done := env.round(v.ID, r.ID)
	if env.caller.count(marketing.StepFollow) != 2 || env.caller.count(marketing.StepCreate) != 1 || env.caller.count(marketing.StepReverse) != 1 {
		t.Fatalf("只该多调一次 follow：follow=%d create=%d reverse=%d", env.caller.count(marketing.StepFollow), env.caller.count(marketing.StepCreate), env.caller.count(marketing.StepReverse))
	}
	after := done.Scripts[0]
	if after.ID != before.ID || after.Status != model.MarketingScriptDraft || after.Shots[0].Narration != "改后的旁白" || after.Revision <= before.Revision {
		t.Fatalf("应原地更新同一份跟拍脚本：before=%+v after=%+v", before.Revision, after)
	}
	if done.ReverseDirty || !done.ReverseEdited || done.Status != model.MarketingRoundReady {
		t.Fatalf("重写后不再 dirty：%+v", done)
	}
	if done.Charged <= chargedBefore {
		t.Fatalf("重写按文本模型再扣一次：%d -> %d", chargedBefore, done.Charged)
	}
	if env.credit().Frozen != 0 {
		t.Fatalf("结算后无冻结：%+v", env.credit())
	}
}

func TestMarketing_Refollow_FailureKeepsOldScript(t *testing.T) {
	env := newMktEnv(t, 400)
	calls := 0
	env.caller.on(marketing.StepFollow, func(int) (string, error) {
		calls++
		if calls == 1 {
			return followReply(10, "一抹就润"), nil
		}
		return "", context.DeadlineExceeded
	})
	v, r := env.refRound()
	ctx := context.Background()
	re, err := env.svc.Refollow(ctx, env.user, uint64(r.ID))
	if err != nil {
		t.Fatal(err)
	}
	env.runRound(re)
	done := env.round(v.ID, r.ID)
	if st := stepOf(done, "follow"); st.Status != model.MarketingRunFailed {
		t.Fatalf("重写失败：%+v", st)
	}
	if done.Status != model.MarketingRoundReady || done.Scripts[0].Status != model.MarketingScriptDraft || done.Scripts[0].Shots[0].Narration != "一抹就润" {
		t.Fatalf("重写失败时保留上一版跟拍并回到草稿：%+v", done.Scripts[0])
	}
}

func TestMarketing_UpdateScript_RescansFlagsAndConflicts(t *testing.T) {
	env := newMktEnv(t, 300)
	v := env.createProject(mktSettings())
	env.runRound(&v.Rounds[0])
	sc := env.round(v.ID, v.Rounds[0].ID).Scripts[0]
	ctx := context.Background()
	if sc.Edited || len(sc.Flags) != 0 {
		t.Fatalf("原稿不应有问题：%+v", sc)
	}
	dirty := []string{"全网第一的香水", "留香很久", "早上喷一下"}
	got, err := env.svc.UpdateScript(ctx, env.user, uint64(sc.ID), &model.UpdateMarketingScriptReq{Narrations: dirty, Revision: sc.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Edited || got.Revision != sc.Revision+1 || got.Shots[0].Narration != "全网第一的香水" {
		t.Fatalf("保存后：%+v", got)
	}
	if len(got.Flags) == 0 || got.Flags[0].Type != "forbidden" || got.Flags[0].Shot != 0 {
		t.Fatalf("极限词应被标出（镜头下标从 0 起）：%+v", got.Flags)
	}
	// 改掉后问题消失。
	clean, err := env.svc.UpdateScript(ctx, env.user, uint64(sc.ID), &model.UpdateMarketingScriptReq{Narrations: []string{"早上喷一下", "留香很久", "下班还在"}, Revision: got.Revision})
	if err != nil || len(clean.Flags) != 0 {
		t.Fatalf("改掉后不应再有问题：%v %+v", err, clean.Flags)
	}
	// 旧版本提交：冲突；镜头数不符：参数错误。
	_, err = env.svc.UpdateScript(ctx, env.user, uint64(sc.ID), &model.UpdateMarketingScriptReq{Narrations: dirty, Revision: sc.Revision})
	wantMktCode(t, err, errcode.ErrMarketingConflict.Code)
	_, err = env.svc.UpdateScript(ctx, env.user, uint64(sc.ID), &model.UpdateMarketingScriptReq{Narrations: []string{"一句"}, Revision: clean.Revision})
	wantMktCode(t, err, errcode.ErrInvalidParams.Code)
	// 恢复原稿。
	restored, err := env.svc.RestoreScript(ctx, env.user, uint64(sc.ID))
	if err != nil || restored.Edited || restored.Shots[0].Narration != sc.Shots[0].Narration {
		t.Fatalf("恢复原稿：%v %+v", err, restored)
	}
	// 别人的脚本。
	_, err = env.svc.UpdateScript(ctx, env.user+1, uint64(sc.ID), &model.UpdateMarketingScriptReq{Narrations: dirty, Revision: restored.Revision})
	wantMktCode(t, err, errcode.ErrMarketingNotFound.Code)
}

func TestMarketing_UpdateScript_FollowRescansBrandAndClaim(t *testing.T) {
	env := newMktEnv(t, 300)
	_, r := env.refRound()
	f := r.Scripts[0]
	if len(f.Flags) != 0 {
		t.Fatalf("原稿干净：%+v", f.Flags)
	}
	got, err := env.svc.UpdateScript(context.Background(), env.user, uint64(f.ID), &model.UpdateMarketingScriptReq{
		Narrations: []string{"HERALIFE 护手霜", "销量第一", "一抹就润", "手更软"}, Revision: f.Revision})
	if err != nil {
		t.Fatal(err)
	}
	types := map[string]bool{}
	for _, fl := range got.Flags {
		types[fl.Type] = true
	}
	if !types["brand"] || !types["claim"] {
		t.Fatalf("跟拍改旁白后应重扫品牌与背书：%+v", got.Flags)
	}
}

// mktFrames 是内存截帧：可用，返回固定帧数。
type mktFrames struct{ ok bool }

func (f mktFrames) Available() bool                                   { return f.ok }
func (f mktFrames) Duration(context.Context, string) (float64, error) { return 10, nil }
func (f mktFrames) Cuts(context.Context, string, float64) ([]float64, error) {
	return []float64{2.5, 5, 7.5}, nil
}
func (f mktFrames) FramesAt(_ context.Context, _ string, times []float64, _ videoframe.FrameOpts) ([]videoframe.Frame, error) {
	out := make([]videoframe.Frame, 0, len(times))
	for _, at := range times {
		out = append(out, videoframe.Frame{At: at, Data: []byte("jpeg")})
	}
	return out, nil
}

// useFrames 把文本模型换成「能看图不能看视频」，并接上截帧。
func (e *mktEnv) useFrames(ok bool) {
	e.models.Snapshots["text-a"] = stepfake.TextModel("text-a", true, false, 2)
	e.rebuild(mktFrames{ok: ok})
}

func TestMarketing_Reverse_FallsBackToFramesWithWarning(t *testing.T) {
	env := newMktEnv(t, 500)
	env.useFrames(true)
	_, r := env.refRound()
	if rev := stepOf(r, "reverse"); rev.Status != model.MarketingRunSucceeded {
		t.Fatalf("模型不能看视频但能看图且能截帧时拆解应成功：%+v", rev)
	}
	if len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], "拆解未包含声音") {
		t.Fatalf("退回截帧要给轮次加警告：%v", r.Warnings)
	}
}

func TestMarketing_Reverse_FailsWhenNoWayToReadVideo(t *testing.T) {
	env := newMktEnv(t, 500)
	env.useFrames(false)
	_, err := env.svc.CreateProject(context.Background(), env.user, &model.CreateMarketingProjectReq{ProductAssetIDs: []uint64{1}, RefVideoAssetID: idp(4), Settings: mktRefSettings()})
	wantMktCode(t, err, errcode.ErrMarketingNoTextModel.Code)
}

func TestMarketing_Reverse_NativeVideoHasNoWarning(t *testing.T) {
	env := newMktEnv(t, 500)
	_, r := env.refRound()
	if len(r.Warnings) != 0 {
		t.Fatalf("模型能直接看视频不应有警告：%v", r.Warnings)
	}
}
