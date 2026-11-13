package service

import (
	"context"
	"encoding/json"
	"slices"

	"go.uber.org/zap"

	"video-canvas/internal/marketing"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/pkg/ws"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/stepkit"
)

// viewData 是组装视图一次性取齐的数据，逐轮次、逐脚本只在内存里拼。
type viewData struct {
	project *model.MarketingProject
	rounds  []model.MarketingRound
	runs    map[uint64][]model.MarketingStepRun // 按轮次，创建顺序
	runByID map[uint64]model.MarketingStepRun
	scripts map[uint64][]model.MarketingScript // 按轮次，seq 顺序
	tasks   map[string][]model.GenerationTask  // 按节点 id
	credits map[uint64]int                     // 轮次 id → 单次出片预估积分
}

// loadViewData 取齐一个项目若干轮次的环节运行、脚本和出片任务。
func (s *MarketingService) loadViewData(ctx context.Context, p *model.MarketingProject, rounds []model.MarketingRound) (*viewData, error) {
	vd := &viewData{project: p, rounds: rounds, runs: map[uint64][]model.MarketingStepRun{}, runByID: map[uint64]model.MarketingStepRun{},
		scripts: map[uint64][]model.MarketingScript{}, tasks: map[string][]model.GenerationTask{}, credits: map[uint64]int{}}
	ids := make([]uint64, 0, len(rounds))
	for _, r := range rounds {
		ids = append(ids, r.ID)
	}
	runs, err := s.repo.RoundRuns(ctx, ids...)
	if err != nil {
		return nil, err
	}
	for _, r := range runs {
		vd.runs[r.RoundID] = append(vd.runs[r.RoundID], r)
		vd.runByID[r.ID] = r
	}
	scripts, err := s.repo.RoundScripts(ctx, ids...)
	if err != nil {
		return nil, err
	}
	var nodes []string
	for _, sc := range scripts {
		vd.scripts[sc.RoundID] = append(vd.scripts[sc.RoundID], sc)
		nodes = append(nodes, marketingNode(sc.ID))
	}
	tasks, err := s.repo.ScriptTasks(ctx, p.UserID, nodes)
	if err != nil {
		return nil, err
	}
	for _, t := range tasks {
		vd.tasks[t.NodeID] = append(vd.tasks[t.NodeID], t)
	}
	return vd, nil
}

// renderCredits 按轮次的视频模型和参数预估一次出片的积分；模型不可用或参数不合法时为 0。
func (s *MarketingService) renderCredits(ctx context.Context, st model.MarketingSettings, images int, hasRef bool) int {
	snap, err := s.models.Snapshot(ctx, st.VideoModel)
	if err != nil || snap == nil {
		return 0
	}
	input := map[string]any{"prompt": "预估", "op": modelcfg.OpOmni}
	for k, v := range st.Params {
		input[k] = v
	}
	if hasRef {
		input["videos"] = []any{"1"}
	}
	norm, errs := modelcfg.ValidateInput(modelcfg.KindVideo, snap.Model.Capabilities, input)
	if len(errs) > 0 {
		return 0
	}
	if images > 0 {
		norm["images"] = []any{"1"}
	}
	return max(modelcfg.Quote(snap.Model.Pricing, snap.Model.Capabilities, modelcfg.SpecFromInput(norm, "")), 0)
}

// roundViews 把若干轮次转成视图（输入顺序不变）。
func (s *MarketingService) roundViews(ctx context.Context, vd *viewData) ([]model.MarketingRoundView, error) {
	out := make([]model.MarketingRoundView, 0, len(vd.rounds))
	for i := range vd.rounds {
		v, err := s.roundView(ctx, vd, &vd.rounds[i])
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, nil
}

// roundView 组装一个轮次的视图。
func (s *MarketingService) roundView(ctx context.Context, vd *viewData, r *model.MarketingRound) (*model.MarketingRoundView, error) {
	st, err := settingsOf(r)
	if err != nil {
		return nil, err
	}
	v := &model.MarketingRoundView{ID: hexID(r.ID), No: r.No, Status: r.Status, Settings: st, PlanCount: r.PlanCount,
		ReverseRevision: r.ReverseRevision, Frozen: r.Frozen, Charged: r.Charged, CreatedAt: r.CreatedAt, Error: r.Error,
		Warnings: []string{}, Steps: []model.MarketingStepRunView{}, Scripts: []model.MarketingScriptView{}}
	if len(r.Warnings) > 0 {
		_ = json.Unmarshal(r.Warnings, &v.Warnings)
	}
	if v.Warnings == nil {
		v.Warnings = []string{}
	}
	if v.RefVideo, err = s.refVideoView(ctx, r); err != nil {
		return nil, err
	}
	if v.ModelAsset, err = s.roundModelAsset(ctx, vd.project.UserID, r); err != nil {
		return nil, err
	}
	latest := latestRuns(vd.runs[r.ID])
	for _, k := range []string{marketing.StepFacts, marketing.StepReverse, marketing.StepFollow, marketing.StepCreate} {
		for _, run := range latest {
			if run.StepKey == k {
				v.Steps = append(v.Steps, runView(run))
			}
		}
	}
	if r.Status == model.MarketingRoundScripting {
		v.Stage = stageOf(latest)
	}
	if err := fillReverse(v, r); err != nil {
		return nil, err
	}
	images := productCount(vd.project)
	if r.ModelAssetID != nil {
		images++
	}
	credits := s.renderCredits(ctx, st, images, r.RefVideoAssetID != nil)
	for i := range vd.scripts[r.ID] {
		sv, err := s.scriptView(vd, &vd.scripts[r.ID][i], credits)
		if err != nil {
			return nil, err
		}
		v.Scripts = append(v.Scripts, *sv)
	}
	return v, nil
}

// refVideoView 返回轮次的参考视频引用；没有或素材已删除为 nil。
func (s *MarketingService) refVideoView(ctx context.Context, r *model.MarketingRound) (*model.MarketingRefVideo, error) {
	if r.RefVideoAssetID == nil {
		return nil, nil //nolint:nilnil // 没有参考视频是正常结果
	}
	a, err := s.assetViewOrNil(ctx, r.UserID, *r.RefVideoAssetID)
	if err != nil || a == nil {
		return nil, err
	}
	return &model.MarketingRefVideo{ID: a.ID, URL: a.URL, Duration: float64(a.DurationMs) / 1000}, nil
}

// fillReverse 把当前拆解和「已编辑 / 与跟拍据以写成的拆解不同」写进视图。
func fillReverse(v *model.MarketingRoundView, r *model.MarketingRound) error {
	if len(r.Reverse) == 0 {
		return nil
	}
	var rev model.MarketingReverse
	if err := json.Unmarshal(r.Reverse, &rev); err != nil {
		return err
	}
	v.Reverse = normalizeReverse(&rev)
	v.ReverseEdited = !sameJSON(r.Reverse, r.ReverseOriginal)
	v.ReverseDirty = len(r.FollowBasis) > 0 && !sameJSON(r.Reverse, r.FollowBasis)
	return nil
}

// normalizeReverse 让拆解里的数组序列化成 [] 而不是 null。
func normalizeReverse(r *model.MarketingReverse) *model.MarketingReverse {
	r.Brands, r.Claims = nonNil(r.Brands), nonNil(r.Claims)
	if r.Shots == nil {
		r.Shots = []model.MarketingReverseShot{}
	}
	for i := range r.Shots {
		r.Shots[i].KeyColors = nonNil(r.Shots[i].KeyColors)
	}
	return r
}

// methodViewOf 把运行记录里的方法快照转成视图；没有快照（排队中、跳过）为 nil。
func methodViewOf(raw []byte) *model.MarketingMethodView {
	if len(raw) == 0 {
		return nil
	}
	var m stepkit.MethodSnapshot
	if err := json.Unmarshal(raw, &m); err != nil || m.Source == "" {
		return nil
	}
	return &model.MarketingMethodView{Source: m.Source, SkillName: m.SkillName, SkillTitle: m.SkillTitle, Version: m.Version, Fallback: m.Fallback}
}

// runView 把环节运行转成视图。
func runView(r model.MarketingStepRun) model.MarketingStepRunView {
	return model.MarketingStepRunView{ID: hexID(r.ID), Step: stepNameOf(r.StepKey), Status: r.Status, Method: methodViewOf(r.Method),
		TextModel: r.TextModelID, Error: r.Error, Attempts: r.Attempts, Charged: r.Charged}
}

// stageOf 按环节状态给出写脚本阶段文案（不落库，每次由状态算出）。
func stageOf(latest []model.MarketingStepRun) string {
	active := map[string]string{}
	for _, r := range latest {
		if r.Status == model.MarketingRunQueued || r.Status == model.MarketingRunRunning {
			active[r.StepKey] = r.Status
		}
	}
	_, facts := active[marketing.StepFacts]
	_, reverse := active[marketing.StepReverse]
	_, follow := active[marketing.StepFollow]
	_, create := active[marketing.StepCreate]
	switch {
	case facts && reverse:
		return "正在看商品图，拆解参考片"
	case facts:
		return "正在看商品图，写事实卡"
	case reverse:
		return "正在拆解参考片"
	case follow && create:
		return "正在写跟拍版和改编版"
	case follow:
		return "正在写跟拍版"
	case create:
		return "正在写创意脚本"
	}
	return "正在收尾"
}

// scriptView 组装一份脚本的视图：出片状态以生成任务为准。
func (s *MarketingService) scriptView(vd *viewData, sc *model.MarketingScript, renderCredits int) (*model.MarketingScriptView, error) {
	var shots []marketing.ScriptShot
	if err := json.Unmarshal(sc.Shots, &shots); err != nil {
		return nil, err
	}
	v := &model.MarketingScriptView{ID: hexID(sc.ID), RoundID: hexID(sc.RoundID), Kind: sc.Kind, Angle: sc.Angle, Title: sc.Title, Hook: sc.Hook,
		Music: sc.Music, Voice: sc.Voice, RenderCredits: renderCredits, Revision: sc.Revision, Shots: make([]model.MarketingScriptShotView, 0, len(shots)),
		Flags: []model.MarketingFlagView{}, Takes: []model.MarketingTaskLite{}}
	for _, sh := range shots {
		v.Shots = append(v.Shots, model.MarketingScriptShotView{Start: sh.Start, End: sh.End, Purpose: sh.Purpose, Shot: sh.Shot, Camera: sh.Camera,
			Scene: sh.Scene, Narration: sh.Narration, RefShot: sh.RefShot})
	}
	if len(sc.Flags) > 0 {
		_ = json.Unmarshal(sc.Flags, &v.Flags)
		if v.Flags == nil {
			v.Flags = []model.MarketingFlagView{}
		}
	}
	var orig marketing.Script
	if err := json.Unmarshal(sc.Original, &orig); err == nil {
		v.Edited = narrationsChanged(shots, orig.Shots)
	}
	if run, ok := vd.runByID[sc.StepRunID]; ok {
		v.Method = methodViewOf(run.Method)
	}
	tasks := vd.tasks[marketingNode(sc.ID)]
	for i := range tasks {
		v.Takes = append(v.Takes, taskLite(&tasks[i]))
	}
	v.Status, v.Error = deriveScriptStatus(sc.Status, tasks)
	return v, nil
}

// narrationsChanged 判断旁白和模型原稿是否不同。
func narrationsChanged(cur, orig []marketing.ScriptShot) bool {
	if len(cur) != len(orig) {
		return true
	}
	for i := range cur {
		if cur[i].Narration != orig[i].Narration {
			return true
		}
	}
	return false
}

// taskLite 把生成任务转成脚本视图里的精简形状。
func taskLite(t *model.GenerationTask) model.MarketingTaskLite {
	v := taskView(t)
	return model.MarketingTaskLite{ID: v.ID, TaskRef: v.TaskRef, Status: v.Status, Progress: v.Progress, Outputs: v.Outputs, ErrorMessage: v.ErrorMessage}
}

// deriveScriptStatus 由存的状态和出片任务算出脚本当前状态与最近一次失败原因：
// 重写中以存的为准；有任务时看最新一条：进行中是出片中，成功是已出片，失败 / 超时是失败；
// 最新一条被取消时退回上一个结果（有成功的 done，否则 draft）。
func deriveScriptStatus(stored string, tasks []model.GenerationTask) (string, string) {
	if stored == model.MarketingScriptRewriting {
		return stored, ""
	}
	if len(tasks) == 0 {
		return model.MarketingScriptDraft, ""
	}
	last := tasks[len(tasks)-1]
	switch {
	case slices.Contains(model.ActiveTaskStatuses, last.Status):
		return model.MarketingScriptRendering, ""
	case last.Status == model.TaskSucceeded:
		return model.MarketingScriptDone, ""
	case last.Status == model.TaskFailed || last.Status == model.TaskExpired:
		return model.MarketingScriptFailed, last.ErrorMessage
	}
	for i := range tasks {
		if tasks[i].Status == model.TaskSucceeded {
			return model.MarketingScriptDone, ""
		}
	}
	return model.MarketingScriptDraft, ""
}

// roundViewByID 取一个轮次的视图（按用户隔离）。
func (s *MarketingService) roundViewByID(ctx context.Context, userID, roundID uint64) (*model.MarketingRoundView, error) {
	r, err := s.repo.Round(ctx, userID, roundID, false)
	if mktNotFound(err) {
		return nil, errcode.ErrMarketingNotFound
	}
	if err != nil {
		return nil, err
	}
	p, err := s.repo.Project(ctx, userID, r.ProjectID, false)
	if mktNotFound(err) {
		return nil, errcode.ErrMarketingNotFound
	}
	if err != nil {
		return nil, err
	}
	vd, err := s.loadViewData(ctx, p, []model.MarketingRound{*r})
	if err != nil {
		return nil, err
	}
	return s.roundView(ctx, vd, r)
}

// pushRound 向用户推送轮次最新状态（marketing.round）；推送可以丢，客户端重连后会对账，所以失败只记日志。
func (s *MarketingService) pushRound(ctx context.Context, userID, roundID uint64) {
	if _, err := s.viewAndPush(ctx, userID, roundID); err != nil {
		logger.Warn("组装营销轮次推送失败", zap.Uint64("round_id", roundID), zap.Error(err))
	}
}

// viewAndPush 取轮次视图并推送 marketing.round，返回视图（用户操作改了轮次后，其他端靠推送同步）。
func (s *MarketingService) viewAndPush(ctx context.Context, userID, roundID uint64) (*model.MarketingRoundView, error) {
	v, err := s.roundViewByID(ctx, userID, roundID)
	if err != nil {
		return nil, err
	}
	r, err := s.repo.RoundByID(ctx, roundID, false)
	if err != nil {
		return nil, err
	}
	channel := ws.UserChannel(userID)
	s.bc.Publish(ctx, channel, ws.Message{Type: ws.TypeMarketingRound, Channel: channel, Data: map[string]any{"project_id": hexID(r.ProjectID), "round": v}})
	return v, nil
}

// publishScript 向用户推送单份脚本最新状态（marketing.script）。
func (s *MarketingService) publishScript(ctx context.Context, userID uint64, sc *model.MarketingScript, v *model.MarketingScriptView) {
	channel := ws.UserChannel(userID)
	s.bc.Publish(ctx, channel, ws.Message{Type: ws.TypeMarketingScript, Channel: channel,
		Data: map[string]any{"project_id": hexID(sc.ProjectID), "round_id": hexID(sc.RoundID), "script": v}})
}

// scriptViewByRow 取一份脚本的视图（带出片历史和预估积分）。
func (s *MarketingService) scriptViewByRow(ctx context.Context, userID uint64, sc *model.MarketingScript) (*model.MarketingScriptView, error) {
	r, err := s.repo.RoundByID(ctx, sc.RoundID, false)
	if err != nil {
		return nil, err
	}
	p, err := s.repo.Project(ctx, userID, sc.ProjectID, false)
	if err != nil {
		return nil, err
	}
	vd, err := s.loadViewData(ctx, p, []model.MarketingRound{*r})
	if err != nil {
		return nil, err
	}
	st, err := settingsOf(r)
	if err != nil {
		return nil, err
	}
	images := productCount(p)
	if r.ModelAssetID != nil {
		images++
	}
	return s.scriptView(vd, sc, s.renderCredits(ctx, st, images, r.RefVideoAssetID != nil))
}

// GetProject 返回项目详情：事实卡、素材、全部轮次（新的在前）。顺带把存的脚本状态按出片任务对账。
func (s *MarketingService) GetProject(ctx context.Context, userID, id uint64) (*model.MarketingProjectView, error) {
	p, err := s.repo.Project(ctx, userID, id, false)
	if mktNotFound(err) {
		return nil, errcode.ErrMarketingNotFound
	}
	if err != nil {
		return nil, err
	}
	rounds, err := s.repo.ProjectRounds(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	vd, err := s.loadViewData(ctx, p, rounds)
	if err != nil {
		return nil, err
	}
	s.healScripts(ctx, vd)
	v := &model.MarketingProjectView{ID: hexID(p.ID), Title: p.Title, Revision: p.Revision, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
		ProductAssets: []model.MarketingAssetRef{}}
	if len(p.Product) > 0 {
		var f model.MarketingFacts
		if err := json.Unmarshal(p.Product, &f); err != nil {
			return nil, err
		}
		v.Product = normalizeFacts(&f)
	}
	var ids []uint64
	if err := json.Unmarshal(p.ProductAssetIDs, &ids); err != nil {
		return nil, err
	}
	for _, aid := range ids {
		a, err := s.assetViewOrNil(ctx, userID, aid)
		if err != nil {
			return nil, err
		}
		if a != nil {
			v.ProductAssets = append(v.ProductAssets, model.MarketingAssetRef{ID: a.ID, URL: a.URL})
		}
	}
	if p.ModelAssetID != nil {
		if a, err := s.assetViewOrNil(ctx, userID, *p.ModelAssetID); err != nil {
			return nil, err
		} else if a != nil {
			v.ModelAsset = &model.MarketingAssetRef{ID: a.ID, URL: a.URL}
		}
	}
	if v.Rounds, err = s.roundViews(ctx, vd); err != nil {
		return nil, err
	}
	return v, nil
}

// normalizeFacts 让事实卡里的数组序列化成 [] 而不是 null。
func normalizeFacts(f *model.MarketingFacts) *model.MarketingFacts {
	f.Anchors, f.Prohibited = nonNil(f.Anchors), nonNil(f.Prohibited)
	if f.SellingPoints == nil {
		f.SellingPoints = []model.MarketingSellingPoint{}
	}
	return f
}

// healScripts 把存的脚本状态和任务列表按出片任务对账（任务回调丢了也能在读取时自愈），差异写回；失败只记日志。
func (s *MarketingService) healScripts(ctx context.Context, vd *viewData) {
	for _, list := range vd.scripts {
		for i := range list {
			sc := &list[i]
			tasks := vd.tasks[marketingNode(sc.ID)]
			status, msg := deriveScriptStatus(sc.Status, tasks)
			ids := taskIDsJSON(tasks)
			if status == sc.Status && msg == sc.Error && sameJSON(ids, sc.TaskIDs) {
				continue
			}
			if err := s.repo.UpdateScript(ctx, sc.ID, map[string]any{"status": status, "error": msg, "task_ids": ids}); err != nil {
				logger.Warn("对账营销脚本状态失败", zap.Uint64("script_id", sc.ID), zap.Error(err))
			}
		}
	}
}

// taskIDsJSON 返回任务 id 列表（按时间正序）的 JSON。
func taskIDsJSON(tasks []model.GenerationTask) []byte {
	ids := make([]uint64, 0, len(tasks))
	for i := range tasks {
		ids = append(ids, tasks[i].ID)
	}
	raw, _ := json.Marshal(ids)
	return raw
}

// roundModelAsset 返回轮次出片用的模特图；没有或素材已删为 nil。
func (s *MarketingService) roundModelAsset(ctx context.Context, userID uint64, r *model.MarketingRound) (*model.MarketingAssetRef, error) {
	if r.ModelAssetID == nil {
		return nil, nil //nolint:nilnil // 没有模特图是正常的「没有」
	}
	a, err := s.assetViewOrNil(ctx, userID, *r.ModelAssetID)
	if err != nil || a == nil {
		return nil, err
	}
	return &model.MarketingAssetRef{ID: a.ID, URL: a.URL}, nil
}
