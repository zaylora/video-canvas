package service_test

import (
	"context"
	"testing"

	"video-canvas/internal/marketing"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
)

// readyRound 建无参考项目并跑完第一轮，返回项目与轮次视图。
func (e *mktEnv) readyRound() (*model.MarketingProjectView, *model.MarketingRoundView) {
	e.t.Helper()
	v := e.createProject(mktSettings())
	e.runRound(&v.Rounds[0])
	return v, e.round(v.ID, v.Rounds[0].ID)
}

// addVideoModel 再登记一个能力相同的视频模型。
func (e *mktEnv) addVideoModel(key string) {
	snap := mktVideoSnapshot()
	snap.Model.Key = key
	e.models.Snapshots[key] = snap
}

func TestMarketing_UpdateRoundSettings_SwitchesModelKeepsLockedParams(t *testing.T) {
	env := newMktEnv(t, 300)
	env.addVideoModel("video-b")
	_, r := env.readyRound()
	ctx := context.Background()
	got, err := env.svc.UpdateRoundSettings(ctx, env.user, uint64(r.ID), &model.UpdateMarketingRoundSettingsReq{
		VideoModel: "video-b", Params: map[string]any{"duration": float64(15), "aspect_ratio": "9:16"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Settings.VideoModel != "video-b" || got.Settings.Platform != r.Settings.Platform || len(got.Scripts) != len(r.Scripts) {
		t.Fatalf("应只换模型，其余设置和脚本不动：%+v", got.Settings)
	}
	// 画幅、时长决定分镜：改了要拒绝。
	for _, params := range []map[string]any{
		{"duration": float64(10), "aspect_ratio": "9:16"},
		{"duration": float64(15), "aspect_ratio": "16:9"},
		{"aspect_ratio": "9:16"},
	} {
		_, err := env.svc.UpdateRoundSettings(ctx, env.user, uint64(r.ID), &model.UpdateMarketingRoundSettingsReq{VideoModel: "video-b", Params: params})
		wantMktCode(t, err, errcode.ErrInvalidParams.Code)
	}
	// 没有这一轮的模特图，不能在这里加。
	_, err = env.svc.UpdateRoundSettings(ctx, env.user, uint64(r.ID), &model.UpdateMarketingRoundSettingsReq{
		VideoModel: mktVideoKey, Params: r.Settings.Params, ModelAssetID: idp(3)})
	wantMktCode(t, err, errcode.ErrInvalidParams.Code)
}

func TestMarketing_UpdateRoundSettings_SwapsModelImage(t *testing.T) {
	env := newMktEnv(t, 300)
	v, r := env.refRound()
	got, err := env.svc.UpdateRoundSettings(context.Background(), env.user, uint64(r.ID), &model.UpdateMarketingRoundSettingsReq{
		VideoModel: mktVideoKey, Params: r.Settings.Params, ModelAssetID: idp(2)})
	if err != nil {
		t.Fatal(err)
	}
	if got.ModelAsset == nil || got.ModelAsset.ID != 2 {
		t.Fatalf("本轮模特图应换成 2：%+v", got.ModelAsset)
	}
	if p := env.project(v.ID); p.ModelAsset == nil || p.ModelAsset.ID != 2 {
		t.Fatalf("最新一轮换了模特，项目默认模特应跟着换：%+v", p.ModelAsset)
	}
	// 不是图片：素材无效。
	_, err = env.svc.UpdateRoundSettings(context.Background(), env.user, uint64(r.ID), &model.UpdateMarketingRoundSettingsReq{
		VideoModel: mktVideoKey, Params: r.Settings.Params, ModelAssetID: idp(4)})
	wantMktCode(t, err, errcode.ErrMarketingAssets.Code)
}

func TestMarketing_UpdateProject_SwapsProductImages(t *testing.T) {
	env := newMktEnv(t, 300)
	v, _ := env.readyRound()
	p := env.project(v.ID)
	got, err := env.svc.UpdateProject(context.Background(), env.user, uint64(v.ID), &model.UpdateMarketingProjectReq{
		ProductAssetIDs: []uint64{3, 1}, Revision: p.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ProductAssets) != 2 || got.ProductAssets[0].ID != 3 || got.ProductAssets[1].ID != 1 {
		t.Fatalf("商品图应换成 [3 1]：%+v", got.ProductAssets)
	}
	// 重复、视频素材：无效。
	for _, ids := range [][]uint64{{1, 1}, {4}} {
		_, err := env.svc.UpdateProject(context.Background(), env.user, uint64(v.ID), &model.UpdateMarketingProjectReq{ProductAssetIDs: ids, Revision: got.Revision})
		wantMktCode(t, err, errcode.ErrMarketingAssets.Code)
	}
}

func TestMarketing_UpdateProject_ProductSwapBlockedWhileScripting(t *testing.T) {
	env := newMktEnv(t, 300)
	v := env.createProject(mktSettings())
	_, err := env.svc.UpdateProject(context.Background(), env.user, uint64(v.ID), &model.UpdateMarketingProjectReq{
		ProductAssetIDs: []uint64{3}, Revision: v.Revision})
	wantMktCode(t, err, errcode.ErrMarketingRoundBusy.Code)
}

func TestMarketing_RefreshFacts_ReplacesOnlyAnchors(t *testing.T) {
	env := newMktEnv(t, 300)
	v, _ := env.readyRound()
	ctx := context.Background()
	p := env.project(v.ID)
	edited := *p.Product
	edited.Name = "用户改过的名字"
	edited.CTA = "用户改过的引导"
	p, err := env.svc.UpdateProject(ctx, env.user, uint64(v.ID), &model.UpdateMarketingProjectReq{Product: &edited, Revision: p.Revision})
	if err != nil {
		t.Fatal(err)
	}
	env.caller.on(marketing.StepFacts, func(int) (string, error) {
		return mustJSONText(map[string]any{"name": "模型新名字", "category": "香水", "anchors": []string{"圆柱透明瓶身", "黑色瓶盖"},
			"selling_points": []map[string]any{{"text": "新卖点", "proof": "画面", "source": "image"}},
			"prohibited":     []string{"新禁用"}, "audience": "新人群", "cta": "新引导"}), nil
	})
	before := env.caller.count(marketing.StepFacts)
	round, err := env.svc.RefreshFacts(ctx, env.user, uint64(v.ID))
	if err != nil {
		t.Fatal(err)
	}
	if round.Status != model.MarketingRoundScripting {
		t.Fatalf("重新识别应把最新一轮放回写脚本：%s", round.Status)
	}
	env.runRound(round)
	if n := env.caller.count(marketing.StepFacts); n != before+1 {
		t.Fatalf("只应重跑一次事实卡：%d → %d", before, n)
	}
	after := env.project(v.ID)
	if got := after.Product.Anchors; len(got) != 2 || got[0] != "圆柱透明瓶身" {
		t.Fatalf("外观锚点应换成新识别的：%v", got)
	}
	if after.Product.Name != "用户改过的名字" || after.Product.CTA != "用户改过的引导" || after.Title != p.Title {
		t.Fatalf("商品名、引导下单、标题应保留：%+v / %s", after.Product, after.Title)
	}
	r := env.round(v.ID, round.ID)
	if r.Status != model.MarketingRoundReady || len(r.Scripts) != len(env.round(v.ID, v.Rounds[0].ID).Scripts) {
		t.Fatalf("跑完回到 ready，脚本不动：%s", r.Status)
	}
	// 写脚本中再点：68007。
	if _, err := env.svc.RefreshFacts(ctx, env.user, uint64(v.ID)); err != nil {
		t.Fatal(err)
	}
	_, err = env.svc.RefreshFacts(ctx, env.user, uint64(v.ID))
	wantMktCode(t, err, errcode.ErrMarketingRoundBusy.Code)
}
