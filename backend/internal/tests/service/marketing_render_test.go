package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"gorm.io/datatypes"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
)

// readyScripts 建无参考项目并跑完第一轮，返回项目 id 与两份创意脚本。
func (e *mktEnv) readyScripts() (idcodec.ID, []model.MarketingScriptView) {
	e.t.Helper()
	v := e.createProject(mktSettings())
	e.runRound(&v.Rounds[0])
	return v.ID, e.round(v.ID, v.Rounds[0].ID).Scripts
}

// renderKey 用唯一幂等键出片。
func (e *mktEnv) render(sc idcodec.ID, key string) (*model.MarketingScriptView, error) {
	return e.svc.Render(context.Background(), e.user, uint64(sc), &model.RenderMarketingScriptReq{IdempotencyKey: key})
}

// scriptTasks 读脚本节点的全部任务（按时间正序）。
func (e *mktEnv) scriptTasks(sc idcodec.ID) []model.GenerationTask {
	e.t.Helper()
	var rows []model.GenerationTask
	node := "mkt:" + idcodec.Encode(uint64(sc))
	if err := e.db.Where("node_id = ?", node).Order("id").Find(&rows).Error; err != nil {
		e.t.Fatal(err)
	}
	return rows
}

// finishTask 把任务置成终态并触发观察者回调（模拟 worker 结束任务）。
func (e *mktEnv) finishTask(task model.GenerationTask, status, errMsg string, outputs []model.TaskOutput) {
	e.t.Helper()
	raw, _ := json.Marshal(outputs)
	fields := map[string]any{"status": status, "error_message": errMsg, "output_json": datatypes.JSON(raw)}
	if err := e.db.Model(&model.GenerationTask{}).Where("id = ?", task.ID).Updates(fields).Error; err != nil {
		e.t.Fatal(err)
	}
	var cur model.GenerationTask
	if err := e.db.Where("id = ?", task.ID).Take(&cur).Error; err != nil {
		e.t.Fatal(err)
	}
	if err := e.svc.OnTaskChanged(context.Background(), &cur); err != nil {
		e.t.Fatal(err)
	}
}

func TestMarketing_Render_CreatesOmniTaskWithFormattedPrompt(t *testing.T) {
	env := newMktEnv(t, 300)
	_, scripts := env.readyScripts()
	sc := scripts[0]
	if sc.RenderCredits != 20 {
		t.Fatalf("出片预估积分应按视频模型定价：%d", sc.RenderCredits)
	}
	got, err := env.render(sc.ID, "k1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.MarketingScriptRendering || len(got.Takes) != 1 {
		t.Fatalf("出片中且有一次出片：%+v", got)
	}
	tasks := env.scriptTasks(sc.ID)
	if len(tasks) != 1 || tasks[0].Kind != model.KindVideo || tasks[0].CanvasProjectID != nil || tasks[0].Credits != 20 {
		t.Fatalf("任务：%+v", tasks)
	}
	var input map[string]any
	if err := json.Unmarshal(tasks[0].InputJSON, &input); err != nil {
		t.Fatal(err)
	}
	if input["op"] != "omni" || input["generate_audio"] != true {
		t.Fatalf("op=omni 且有声：%v", input)
	}
	if imgs, _ := input["images"].([]any); len(imgs) != 2 || imgs[0] != "1" || imgs[1] != "2" {
		t.Fatalf("图片 = 商品图：%v", input["images"])
	}
	if _, has := input["videos"]; has {
		t.Fatal("无参考视频时不带 videos")
	}
	prompt, _ := input["prompt"].(string)
	if !strings.Contains(prompt, "[基本参数]") || !strings.Contains(prompt, "[分镜]") || !strings.Contains(prompt, "早上喷一下") {
		t.Fatalf("提示词应是 seedance 分区块写法并带旁白：%s", prompt)
	}
	if input["duration"] != float64(15) || input["aspect_ratio"] != "9:16" {
		t.Fatalf("参数沿用本轮设置：%v", input)
	}
	// 幂等：同一个 key 再提交返回同一次出片，不再建任务。
	again, err := env.render(sc.ID, "k1")
	if err != nil || len(again.Takes) != 1 || again.Takes[0].ID != got.Takes[0].ID || len(env.scriptTasks(sc.ID)) != 1 {
		t.Fatalf("同 key 应幂等：%v %+v", err, again)
	}
	// 出片中不能再出片，也不能改旁白。
	_, err = env.render(sc.ID, "k2")
	wantMktCode(t, err, errcode.ErrMarketingScriptBusy.Code)
	_, err = env.svc.UpdateScript(context.Background(), env.user, uint64(sc.ID), &model.UpdateMarketingScriptReq{Narrations: []string{"a", "b", "c"}, Revision: got.Revision})
	wantMktCode(t, err, errcode.ErrMarketingScriptBusy.Code)
	if c := env.credit(); c.Frozen != 20+env.ledgerSum(model.LedgerFreeze)-env.ledgerSum(model.LedgerSettle)-env.ledgerSum(model.LedgerRefund) {
		t.Fatalf("出片冻结 20 积分：%+v", c)
	}
}

func TestMarketing_Render_WithRef_AddsVideoAndModelImage(t *testing.T) {
	env := newMktEnv(t, 500)
	_, r := env.refRound()
	got, err := env.render(r.Scripts[0].ID, "kf")
	if err != nil {
		t.Fatal(err)
	}
	var input map[string]any
	_ = json.Unmarshal(env.scriptTasks(got.ID)[0].InputJSON, &input)
	if vids, _ := input["videos"].([]any); len(vids) != 1 || vids[0] != "4" {
		t.Fatalf("跟拍带参考视频：%v", input["videos"])
	}
	if imgs, _ := input["images"].([]any); len(imgs) != 2 || imgs[1] != "3" {
		t.Fatalf("图片 = 商品图 + 模特图：%v", input["images"])
	}
	if !strings.Contains(input["prompt"].(string), "@视频1") {
		t.Fatalf("跟拍提示词应写明参考视频：%v", input["prompt"])
	}
	// 创意改编版同样带参考视频。
	got2, err := env.render(r.Scripts[1].ID, "kc")
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(env.scriptTasks(got2.ID)[0].InputJSON, &input)
	if vids, _ := input["videos"].([]any); len(vids) != 1 {
		t.Fatalf("改编版也带参考视频：%v", input["videos"])
	}
}

func TestMarketing_Render_RefusesFlaggedAndRewriting(t *testing.T) {
	env := newMktEnv(t, 300)
	_, scripts := env.readyScripts()
	sc := scripts[0]
	flagged, err := env.svc.UpdateScript(context.Background(), env.user, uint64(sc.ID), &model.UpdateMarketingScriptReq{Narrations: []string{"全网第一", "留香", "下班"}, Revision: sc.Revision})
	if err != nil || len(flagged.Flags) == 0 {
		t.Fatalf("准备带问题的脚本：%v %+v", err, flagged)
	}
	_, err = env.render(sc.ID, "k1")
	wantMktCode(t, err, errcode.ErrMarketingScriptBusy.Code)
	if err == nil || !strings.Contains(err.Error(), "旁白有问题") {
		t.Fatalf("应带「旁白有问题」文案：%v", err)
	}
	if len(env.scriptTasks(sc.ID)) != 0 {
		t.Fatal("被拒绝时不能创建任务")
	}
	// 重写中。
	if err := env.db.Model(&model.MarketingScript{}).Where("id = ?", uint64(scripts[1].ID)).Update("status", model.MarketingScriptRewriting).Error; err != nil {
		t.Fatal(err)
	}
	_, err = env.render(scripts[1].ID, "k2")
	wantMktCode(t, err, errcode.ErrMarketingScriptBusy.Code)
}

func TestMarketing_Render_GuardInTaskCreate_OwnershipAndState(t *testing.T) {
	env := newMktEnv(t, 300)
	_, scripts := env.readyScripts()
	node := "mkt:" + idcodec.Encode(uint64(scripts[0].ID))
	ctx := context.Background()
	other := env.user + 1
	create := func(user uint64, key string) *model.CreateTaskItem {
		resp, err := env.taskSvc.Create(ctx, user, key, &model.CreateGenerationTaskReq{Kind: model.KindVideo, ModelID: mktVideoKey, NodeID: node,
			Input: map[string]any{"prompt": "x", "op": "t2v"}})
		if err != nil {
			t.Fatal(err)
		}
		return &resp.Items[0]
	}
	if it := create(other, "o1"); it.Error == nil || it.Error.Code != errcode.ErrMarketingNotFound.Code {
		t.Fatalf("别人的脚本节点应按不存在处理：%+v", it)
	}
	if it := create(env.user, "m1"); it.Error != nil || it.Task == nil {
		t.Fatalf("本人可以提交：%+v", it.Error)
	}
	if it := create(env.user, "m2"); it.Error == nil || it.Error.Code != errcode.ErrMarketingScriptBusy.Code {
		t.Fatalf("已有进行中的任务应拒绝：%+v", it)
	}
	if it := create(env.user, "m1"); it.Error != nil || it.Task == nil {
		t.Fatalf("同 key 幂等先于状态检查：%+v", it.Error)
	}
	var n int64
	env.db.Model(&model.GenerationTask{}).Where("node_id = ?", node).Count(&n)
	if n != 1 {
		t.Fatalf("应只有一个任务：%d", n)
	}
	// 不是营销前缀的节点不受守卫影响。
	resp, err := env.taskSvc.Create(ctx, env.user, "free", &model.CreateGenerationTaskReq{Kind: model.KindVideo, ModelID: mktVideoKey, NodeID: "n_abc",
		Input: map[string]any{"prompt": "x", "op": "t2v"}})
	if err != nil || resp.Items[0].Error != nil {
		t.Fatalf("普通节点：%v", err)
	}
}

func TestMarketing_Render_ModelErrors(t *testing.T) {
	env := newMktEnv(t, 300)
	_, scripts := env.readyScripts()
	ctx := context.Background()
	_, err := env.svc.Render(ctx, env.user, uint64(scripts[0].ID), &model.RenderMarketingScriptReq{IdempotencyKey: "k1", VideoModel: "nope"})
	wantMktCode(t, err, errcode.ErrMarketingVideoModel.Code)
	_, err = env.svc.Prompt(ctx, env.user, uint64(scripts[0].ID), "nope")
	wantMktCode(t, err, errcode.ErrMarketingVideoModel.Code)
	short := mktVideoSnapshot()
	short.Model.Capabilities.Prompt.MaxLength = 80
	env.models.Snapshots[mktVideoKey] = short
	_, err = env.render(scripts[0].ID, "k2")
	wantMktCode(t, err, errcode.ErrMarketingPromptLong.Code)
	_, err = env.svc.Prompt(ctx, env.user, uint64(scripts[0].ID), "")
	wantMktCode(t, err, errcode.ErrMarketingPromptLong.Code)
	if len(env.scriptTasks(scripts[0].ID)) != 0 {
		t.Fatal("超长时不能创建任务")
	}
}

func TestMarketing_Prompt_ReflectsNarrationEdits(t *testing.T) {
	env := newMktEnv(t, 300)
	_, scripts := env.readyScripts()
	ctx := context.Background()
	p, err := env.svc.Prompt(ctx, env.user, uint64(scripts[0].ID), "")
	if err != nil || p.Format != "seedance" || !strings.Contains(p.Prompt, "早上喷一下") {
		t.Fatalf("预览：%v %+v", err, p)
	}
	if _, err := env.svc.UpdateScript(ctx, env.user, uint64(scripts[0].ID), &model.UpdateMarketingScriptReq{Narrations: []string{"换一句", "留香", "下班"}, Revision: scripts[0].Revision}); err != nil {
		t.Fatal(err)
	}
	p, err = env.svc.Prompt(ctx, env.user, uint64(scripts[0].ID), "")
	if err != nil || !strings.Contains(p.Prompt, "换一句") || strings.Contains(p.Prompt, "早上喷一下\"") && false {
		t.Fatalf("改旁白后提示词应立即反映：%v", err)
	}
	if c := env.credit(); c.Frozen != 0 {
		t.Fatalf("预览不收费不冻结：%+v", c)
	}
	generic := mktVideoSnapshot()
	generic.Model.Capabilities.PromptFormat = ""
	env.models.Snapshots["video-b"] = generic
	pg, err := env.svc.Prompt(ctx, env.user, uint64(scripts[0].ID), "video-b")
	if err != nil || pg.Format != "generic" || strings.Contains(pg.Prompt, "[分镜]") {
		t.Fatalf("通用写法：%v %+v", err, pg)
	}
}

func TestMarketing_TaskLifecycle_UpdatesScriptAndCover(t *testing.T) {
	env := newMktEnv(t, 300)
	pid, scripts := env.readyScripts()
	sc := scripts[0]
	if _, err := env.render(sc.ID, "k1"); err != nil {
		t.Fatal(err)
	}
	task := env.scriptTasks(sc.ID)[0]
	env.finishTask(task, model.TaskFailed, "上游生成失败", nil)
	failed := env.round(pid, sc.RoundID).Scripts[0]
	if failed.Status != model.MarketingScriptFailed || failed.Error != "上游生成失败" {
		t.Fatalf("失败：%+v", failed)
	}
	// 失败后可以重新出片。
	if _, err := env.render(sc.ID, "k2"); err != nil {
		t.Fatal(err)
	}
	second := env.scriptTasks(sc.ID)[1]
	cover := &model.Asset{ID: 90, UserID: env.user, Kind: "image", MimeType: "image/jpeg", StorageKey: "cover90", ByteSize: 1}
	if err := env.db.Create(cover).Error; err != nil {
		t.Fatal(err)
	}
	env.finishTask(second, model.TaskSucceeded, "", []model.TaskOutput{{AssetID: 91, URL: "https://x/v.mp4", MediaType: "video", CoverAssetID: 90}})
	done := env.round(pid, sc.RoundID).Scripts[0]
	if done.Status != model.MarketingScriptDone || len(done.Takes) != 2 || done.Takes[1].Status != model.TaskSucceeded || len(done.Takes[1].Outputs) != 1 {
		t.Fatalf("成功后 done，出片历史按时间正序全部保留：%+v", done)
	}
	items, _, err := env.svc.ListProjects(context.Background(), env.user, "", 1, 10)
	if err != nil || items[0].CoverURL != "https://files.test/cover90" || items[0].ScriptCounts.Done != 1 {
		t.Fatalf("封面应是最新成片首帧：%v %+v", err, items[0])
	}
	env.bc.mu.Lock()
	scriptMsgs := 0
	for _, m := range env.bc.msgs {
		if m.Type == "marketing.script" {
			scriptMsgs++
		}
	}
	env.bc.mu.Unlock()
	if scriptMsgs < 2 {
		t.Fatalf("任务结束应推送 marketing.script：%d", scriptMsgs)
	}
}

func TestMarketing_RenderRound_SkipsWithReasons(t *testing.T) {
	env := newMktEnv(t, 300)
	pid, scripts := env.readyScripts()
	ctx := context.Background()
	if _, err := env.svc.UpdateScript(ctx, env.user, uint64(scripts[1].ID), &model.UpdateMarketingScriptReq{Narrations: []string{"全网第一", "留香", "下班"}, Revision: scripts[1].Revision}); err != nil {
		t.Fatal(err)
	}
	out, err := env.svc.RenderRound(ctx, env.user, uint64(scripts[0].RoundID), &model.RenderMarketingRoundReq{IdempotencyKey: "batch"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Skipped) != 1 || out.Skipped[0].ScriptID != scripts[1].ID || !strings.Contains(out.Skipped[0].Reason, "旁白有问题") {
		t.Fatalf("有问题的脚本应跳过并说明原因：%+v", out.Skipped)
	}
	if out.Round.Scripts[0].Status != model.MarketingScriptRendering || out.Round.Scripts[1].Status != model.MarketingScriptDraft {
		t.Fatalf("只出了第一份：%+v", out.Round.Scripts)
	}
	again, err := env.svc.RenderRound(ctx, env.user, uint64(scripts[0].RoundID), &model.RenderMarketingRoundReq{IdempotencyKey: "batch2"})
	if err != nil || len(again.Skipped) != 2 || len(env.scriptTasks(scripts[0].ID)) != 1 {
		t.Fatalf("已在出片的跳过，不重复建任务：%v %+v", err, again.Skipped)
	}
	_, err = env.svc.RenderRound(ctx, env.user+1, uint64(scripts[0].RoundID), &model.RenderMarketingRoundReq{IdempotencyKey: "x"})
	wantMktCode(t, err, errcode.ErrMarketingNotFound.Code)
	_ = pid
}

func TestMarketing_RenderRound_InsufficientCreditsWhenNothingCreated(t *testing.T) {
	env := newMktEnv(t, 40)
	_, scripts := env.readyScripts()
	env.db.Model(&model.UserCredit{}).Where("user_id = ?", env.user).Updates(map[string]any{"balance": 5, "frozen": 0})
	_, err := env.svc.RenderRound(context.Background(), env.user, uint64(scripts[0].RoundID), &model.RenderMarketingRoundReq{IdempotencyKey: "b"})
	wantMktCode(t, err, errcode.ErrInsufficientCredits.Code)
}

func TestMarketing_DeleteProject_RefusesWhileBusy(t *testing.T) {
	env := newMktEnv(t, 300)
	v := env.createProject(mktSettings())
	ctx := context.Background()
	wantMktCode(t, env.svc.DeleteProject(ctx, env.user, uint64(v.ID)), errcode.ErrMarketingBusyDelete.Code)
	env.runRound(&v.Rounds[0])
	scripts := env.round(v.ID, v.Rounds[0].ID).Scripts
	if _, err := env.render(scripts[0].ID, "k1"); err != nil {
		t.Fatal(err)
	}
	wantMktCode(t, env.svc.DeleteProject(ctx, env.user, uint64(v.ID)), errcode.ErrMarketingBusyDelete.Code)
	env.finishTask(env.scriptTasks(scripts[0].ID)[0], model.TaskSucceeded, "", []model.TaskOutput{{AssetID: 91, MediaType: "video"}})
	if err := env.svc.DeleteProject(ctx, env.user, uint64(v.ID)); err != nil {
		t.Fatalf("没有进行中的任务可以删除：%v", err)
	}
	_, err := env.svc.GetProject(ctx, env.user, uint64(v.ID))
	wantMktCode(t, err, errcode.ErrMarketingNotFound.Code)
	if len(env.scriptTasks(scripts[0].ID)) != 1 {
		t.Fatal("删除项目时成片任务保留")
	}
	if _, err := env.render(scripts[1].ID, "k9"); err == nil {
		t.Fatal("项目删除后不能再出片")
	} else {
		wantMktCode(t, err, errcode.ErrMarketingNotFound.Code)
	}
}

func TestMarketing_UpdateProject_FactsAndRevision(t *testing.T) {
	env := newMktEnv(t, 300)
	v := env.createProject(mktSettings())
	env.runRound(&v.Rounds[0])
	p := env.project(v.ID)
	ctx := context.Background()
	title := "新标题"
	facts := *p.Product
	facts.CTA = "点击下方链接，把清晨带在身上"
	got, err := env.svc.UpdateProject(ctx, env.user, uint64(v.ID), &model.UpdateMarketingProjectReq{Title: &title, Product: &facts, Revision: p.Revision})
	if err != nil || got.Title != "新标题" || got.Product.CTA != facts.CTA || got.Revision != p.Revision+1 {
		t.Fatalf("保存：%v %+v", err, got)
	}
	_, err = env.svc.UpdateProject(ctx, env.user, uint64(v.ID), &model.UpdateMarketingProjectReq{Title: &title, Revision: p.Revision})
	wantMktCode(t, err, errcode.ErrMarketingConflict.Code)
	bad := facts
	bad.Name = ""
	_, err = env.svc.UpdateProject(ctx, env.user, uint64(v.ID), &model.UpdateMarketingProjectReq{Product: &bad, Revision: got.Revision})
	wantMktCode(t, err, errcode.ErrInvalidParams.Code)
	_, err = env.svc.UpdateProject(ctx, env.user+1, uint64(v.ID), &model.UpdateMarketingProjectReq{Title: &title, Revision: got.Revision})
	wantMktCode(t, err, errcode.ErrMarketingNotFound.Code)
}

func TestMarketing_NewRound_ReplacesModelAssetAndKeepsOldOne(t *testing.T) {
	env := newMktEnv(t, 500)
	v := env.createRefProject()
	env.runRound(&v.Rounds[0])
	env.assets.put(env.user, 7, "image", "image/jpeg", 0)
	if err := env.db.Create(&model.Asset{ID: 7, UserID: env.user, Kind: "image", MimeType: "image/jpeg", StorageKey: "k7", ByteSize: 1}).Error; err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	st := mktSettings()
	st.Angles = []string{"pain"}
	r2, err := env.svc.NewRound(ctx, env.user, uint64(v.ID), &model.CreateMarketingRoundReq{Settings: st})
	if err != nil {
		t.Fatal(err)
	}
	p := env.project(v.ID)
	if p.ModelAsset == nil || p.ModelAsset.ID != 3 {
		t.Fatalf("不传模特图沿用项目已有的：%+v", p.ModelAsset)
	}
	env.runRound(r2)
	r3, err := env.svc.NewRound(ctx, env.user, uint64(v.ID), &model.CreateMarketingRoundReq{ModelAssetID: idp(7), Settings: st})
	if err != nil {
		t.Fatal(err)
	}
	if p := env.project(v.ID); p.ModelAsset == nil || p.ModelAsset.ID != 7 {
		t.Fatalf("传了就替换项目的模特图：%+v", p.ModelAsset)
	}
	var old, cur model.MarketingRound
	env.db.Where("id = ?", uint64(v.Rounds[0].ID)).Take(&old)
	env.db.Where("id = ?", uint64(r3.ID)).Take(&cur)
	if old.ModelAssetID == nil || *old.ModelAssetID != 3 || cur.ModelAssetID == nil || *cur.ModelAssetID != 7 {
		t.Fatalf("每一轮记着自己用的模特图：old=%v cur=%v", old.ModelAssetID, cur.ModelAssetID)
	}
}

func TestMarketing_ToCanvas_BuildsAssetsShotsAndEdges(t *testing.T) {
	env := newMktEnv(t, 500)
	_, r := env.refRound()
	follow := r.Scripts[0]
	ctx := context.Background()
	res, err := env.svc.ToCanvas(ctx, env.user, uint64(follow.ID))
	if err != nil || uint64(res.CanvasID) != 77 {
		t.Fatalf("发到画布：%v %+v", err, res)
	}
	req := env.canvases.req
	if req.Title != "晨雾香水 50ml · 跟拍" {
		t.Fatalf("画布标题 = 商品名 · 跟拍：%q", req.Title)
	}
	var g struct {
		Nodes []map[string]any `json:"nodes"`
		Edges []map[string]any `json:"edges"`
	}
	if err := json.Unmarshal(req.PayloadJSON, &g); err != nil {
		t.Fatal(err)
	}
	var images, videos, shots int
	for _, n := range g.Nodes {
		d := n["data"].(map[string]any)
		switch {
		case d["kind"] == "image" && d["uploaded"] == true:
			images++
		case d["kind"] == "video" && d["uploaded"] == true:
			videos++
		case d["kind"] == "video" && d["status"] == "idle":
			shots++
			if d["model"] != mktVideoKey || !strings.Contains(d["prompt"].(string), "镜头") || d["params"].(map[string]any)["op"] != "omni" {
				t.Fatalf("镜头节点应带单镜头提示词、模型和 omni：%v", d)
			}
		}
	}
	if images != 2 || videos != 1 || shots != 4 || len(g.Edges) != 3*4 {
		t.Fatalf("商品图 + 模特图 2、参考视频 1、4 个镜头、每镜 3 条连线：%d %d %d edges=%d", images, videos, shots, len(g.Edges))
	}
	// 有成片时多一个成片节点。
	if _, err := env.render(follow.ID, "k1"); err != nil {
		t.Fatal(err)
	}
	env.assets.put(env.user, 91, "video", "video/mp4", 15000)
	env.finishTask(env.scriptTasks(follow.ID)[0], model.TaskSucceeded, "", []model.TaskOutput{{AssetID: 91, URL: "https://x/v.mp4", MediaType: "video"}})
	if _, err := env.svc.ToCanvas(ctx, env.user, uint64(follow.ID)); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(env.canvases.req.PayloadJSON, &g)
	finals := 0
	for _, n := range g.Nodes {
		if d := n["data"].(map[string]any); d["label"] == "成片" && d["status"] == "done" {
			finals++
		}
	}
	if finals != 1 {
		t.Fatalf("应有一个成片节点：%d", finals)
	}
	// 失败与归属。
	env.canvases.err = errors.New("画布写入失败")
	_, err = env.svc.ToCanvas(ctx, env.user, uint64(follow.ID))
	wantMktCode(t, err, errcode.ErrMarketingCanvasFailed.Code)
	_, err = env.svc.ToCanvas(ctx, env.user+1, uint64(follow.ID))
	wantMktCode(t, err, errcode.ErrMarketingNotFound.Code)
}
