package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/repository"
)

// Estimate 使用现有模型定价及相同请求范围估价，不冻结积分。
func (s *DramaService) Estimate(ctx context.Context, userID, id uint64, req *model.DramaJobReq) (int, error) {
	if req.Step == "compose" {
		return s.estimateCompose(ctx, userID, id, req)
	}
	if err := checkStoryboardRequest(req); err != nil {
		return 0, err
	}
	if req.Step == "storyboard" {
		return s.estimateStoryboard(ctx, userID, id, req)
	}
	// 1. 估价执行与提交相同的前置检查。
	p, err := s.repo.Project(ctx, userID, id, false)
	if err != nil {
		return 0, dramaError(err)
	}
	eps, err := s.repo.Episodes(ctx, userID, id)
	if err != nil {
		return 0, err
	}
	selected, err := selectDramaEpisodes(p, eps, req)
	if err != nil {
		return 0, err
	}
	if req.Step == "asset_prompts" {
		jobs, e := s.repo.Jobs(ctx, userID, id, false)
		if e != nil {
			return 0, e
		}
		scope := dramaEpisodeNumbers(selected)
		if _, e = dramaRetryAssets(jobs, scope); e != nil {
			return 0, e
		}
	}
	snap, err := s.resolveModel(ctx, p.TextModel)
	if err != nil {
		return 0, err
	}
	return dramaQuote(p, req.Step, selected, snap), nil
}

// CreateJob 同项目任务串行提交，模型任务冻结积分，合成任务不访问模型或账户。
func (s *DramaService) CreateJob(ctx context.Context, userID, id uint64, req *model.DramaJobReq) (*model.DramaJobView, error) {
	return s.createDramaJob(ctx, userID, id, nil, req)
}

// createDramaJob 统一普通提交和审批提交，审批ID与任务在同一事务登记。
func (s *DramaService) createDramaJob(ctx context.Context, userID, id uint64, approvalID *uint64, req *model.DramaJobReq) (*model.DramaJobView, error) {
	if err := checkStoryboardRequest(req); err != nil {
		return nil, err
	}
	// 1. 审批重试先恢复原任务，即使模型配置后来失效也不能创建第二个任务。
	if approvalID != nil {
		previous, err := s.approvedDramaJob(ctx, userID, id, *approvalID)
		if err != nil || previous != nil {
			return previous, err
		}
	}
	p, err := s.repo.Project(ctx, userID, id, false)
	if err != nil {
		return nil, dramaError(err)
	}
	snap, initial, err := s.jobFreeze(ctx, p, req)
	if err != nil {
		return nil, err
	}
	var job *model.DramaJob
	err = s.repo.WithTx(ctx, func(tx repository.DramaTx) error {
		var e error
		job, e = s.insertDramaJob(ctx, tx, userID, id, approvalID, req, snap, initial)
		return e
	})
	if err != nil {
		return nil, dramaError(err)
	}
	s.publish(ctx, job)
	v := jobView(*job)
	return &v, nil
}

// jobFreeze 事务外冻结模型配置和起始积分；合成不调模型，两者为空。
func (s *DramaService) jobFreeze(ctx context.Context, p *model.DramaProject, req *model.DramaJobReq) (*provider.Snapshot, int, error) {
	if req.Step == "compose" {
		return nil, 0, nil
	}
	snap, err := s.resolveModel(ctx, p.TextModel)
	if err != nil {
		return nil, 0, err
	}
	initial, err := s.dramaInitialCredits(ctx)
	return snap, initial, err
}

// insertDramaJob 在项目锁内恢复审批已建任务，否则校验并入队。
func (s *DramaService) insertDramaJob(ctx context.Context, tx repository.DramaTx, userID, id uint64, approvalID *uint64, req *model.DramaJobReq, snap *provider.Snapshot, initial int) (*model.DramaJob, error) {
	current, err := tx.Project(ctx, userID, id, true)
	if err != nil {
		return nil, err
	}
	if approvalID != nil {
		job, err := tx.JobByApproval(ctx, userID, *approvalID)
		if err == nil {
			if job.DramaID != id {
				return nil, errcode.ErrDramaNotFound
			}
			return job, nil
		}
		if !errors.Is(err, repository.ErrNotFound) {
			return nil, err
		}
	}
	selected, err := s.prepareDramaJob(ctx, tx, current, req)
	if err != nil {
		return nil, err
	}
	if req.Step == "compose" {
		return enqueueCompose(ctx, tx, current, req, selected, approvalID)
	}
	return s.enqueue(ctx, tx, current, req, snap, selected, initial, approvalID)
}

// prepareDramaJob 在项目锁内校验并发、前置条件与比例。
func (s *DramaService) prepareDramaJob(ctx context.Context, tx repository.DramaTx, p *model.DramaProject, req *model.DramaJobReq) ([]model.DramaEpisode, error) {
	jobs, e := tx.Jobs(ctx, p.UserID, p.ID, true)
	if e != nil {
		return nil, e
	}
	if len(jobs) > 0 {
		return nil, errcode.ErrDramaJobBusy
	}
	eps, e := tx.Episodes(ctx, p.UserID, p.ID)
	if e != nil {
		return nil, e
	}
	selected, e := selectDramaEpisodes(p, eps, req)
	if e != nil {
		return nil, e
	}
	if req.Step == "storyboard" {
		if e := checkStoryboardTakes(ctx, tx, p.UserID, selected); e != nil {
			return nil, e
		}
	}
	if req.Step != "breakdown" || p.Ratio != "" {
		return selected, nil
	}
	if req.Ratio == "" {
		return nil, errcode.ErrDramaStepNotReady.WithMsg("拆解前请选择画面比例")
	}
	if e := tx.UpdateProject(ctx, p.UserID, p.ID, p.Revision, map[string]any{"ratio": req.Ratio}); e != nil {
		return nil, e
	}
	p.Ratio = req.Ratio
	p.Revision++
	return selected, nil
}

// ListJobs 返回用户项目任务，active仅返回排队或执行中的任务。
func (s *DramaService) ListJobs(ctx context.Context, userID, id uint64, active bool) ([]model.DramaJobView, error) {
	// 1. 查询前确认项目归属，空结果序列化为[]。
	if _, e := s.repo.Project(ctx, userID, id, false); e != nil {
		return nil, dramaError(e)
	}
	jobs, e := s.repo.Jobs(ctx, userID, id, active)
	if e != nil {
		return nil, e
	}
	out := make([]model.DramaJobView, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, jobView(j))
	}
	return out, nil
}

// CancelJob 幂等取消任务并释放冻结积分。
func (s *DramaService) CancelJob(ctx context.Context, userID, dramaID, jobID uint64) (*model.DramaJobView, error) {
	// 1. 按project→job→credit顺序加锁，取消和完成只有一个能结算。
	var job *model.DramaJob
	err := s.repo.WithTx(ctx, func(tx repository.DramaTx) error {
		if _, e := tx.Project(ctx, userID, dramaID, true); e != nil {
			return e
		}
		var e error
		job, e = tx.Job(ctx, userID, dramaID, jobID, true)
		if e != nil {
			return e
		}
		if !dramaActive(job.Status) {
			return nil
		}
		if dramaCanvasCommitted(job) {
			return errcode.ErrTaskNotCancelable.WithMsg("画布已写入，任务正在完成结算")
		}
		return s.finishTx(ctx, tx, job, "canceled", 0, "", 0, nil)
	})
	if err != nil {
		return nil, dramaError(err)
	}
	s.publish(ctx, job)
	v := jobView(*job)
	return &v, nil
}

// resolveModel 跟随后台 Agent 模型排序；失效的选择回落至第一个仍启用的模型。
func (s *DramaService) resolveModel(ctx context.Context, key string) (*provider.Snapshot, error) {
	if s.registry == nil {
		return nil, errcode.ErrDramaNoTextModel
	}
	models, e := s.registry.ListModels(ctx, modelcfg.KindAgent)
	if e != nil {
		return nil, e
	}
	keys := make([]string, 0, len(models))
	for _, m := range models {
		if m.Key == key {
			keys = append(keys, key)
		}
	}
	for _, m := range models {
		if m.Key != key {
			keys = append(keys, m.Key)
		}
	}
	for _, k := range keys {
		snap, e := s.registry.Snapshot(ctx, k)
		if errors.Is(e, provider.ErrModelUnavailable) {
			continue
		}
		if e != nil {
			return nil, e
		}
		if snap != nil && snap.Model.Kind == modelcfg.KindAgent {
			return snap, nil
		}
	}
	return nil, errcode.ErrDramaNoTextModel
}

// enqueue 冻结费用并同事务创建业务任务与River job。
func (s *DramaService) enqueue(ctx context.Context, tx repository.DramaTx, p *model.DramaProject, req *model.DramaJobReq, snap *provider.Snapshot, eps []model.DramaEpisode, initial int, approvalID *uint64) (*model.DramaJob, error) {
	raw, e := json.Marshal(snap)
	if e != nil {
		return nil, e
	}
	scope := []int{}
	for _, ep := range eps {
		scope = append(scope, ep.No)
	}
	j := &model.DramaJob{UserID: p.UserID, DramaID: p.ID, Step: req.Step, Episodes: scope, Status: "queued", FrozenCredits: dramaQuote(p, req.Step, eps, snap), ConfigSnapshot: raw, InputJSON: []byte(`{}`), Output: []byte(`{}`), ModelID: snap.ModelRevisionID}
	j.ApprovalID = approvalID
	if req.Step == "storyboard" {
		if e := enqueueStoryboard(ctx, tx, j, req, eps); e != nil {
			return nil, e
		}
	}
	if req.Step == "asset_prompts" {
		if e := prepareDramaAssetInput(ctx, tx, j); e != nil {
			return nil, e
		}
	}
	if e := s.freezeDrama(ctx, tx, j, initial); e != nil {
		return nil, e
	}
	status := p.Status
	switch req.Step {
	case "analyze":
		status = "analyzing"
	case "scripts":
		status = "scripting"
	}
	if e := tx.UpdateProject(ctx, p.UserID, p.ID, p.Revision, map[string]any{"status": status, "text_model_id": snap.ModelRevisionID}); e != nil {
		return nil, e
	}
	return j, nil
}

// prepareDramaAssetInput 在任务创建前恢复上次同范围的新资产集合。
func prepareDramaAssetInput(ctx context.Context, tx repository.DramaTx, j *model.DramaJob) error {
	jobs, e := tx.Jobs(ctx, j.UserID, j.DramaID, false)
	if e != nil {
		return e
	}
	assets, e := dramaRetryAssets(jobs, j.Episodes)
	if e != nil {
		return e
	}
	raw, e := json.Marshal(assets)
	if e != nil {
		return e
	}
	j.InputJSON = raw
	return nil
}

// dramaEpisodeNumbers 提取集号供范围比较，保持自然顺序。
func dramaEpisodeNumbers(eps []model.DramaEpisode) []int {
	scope := make([]int, 0, len(eps))
	for _, ep := range eps {
		scope = append(scope, ep.No)
	}
	return scope
}

// freezeDrama 在账户锁内检查余额并创建冻结流水。
func (s *DramaService) freezeDrama(ctx context.Context, tx repository.DramaTx, j *model.DramaJob, initial int) error {
	if e := tx.EnsureCredit(ctx, j.UserID, initial); e != nil {
		return e
	}
	credit, e := tx.LockCredit(ctx, j.UserID)
	if e != nil {
		return e
	}
	if credit.Balance-credit.Frozen < j.FrozenCredits {
		return errcode.ErrInsufficientCredits
	}
	if e := tx.InsertJob(ctx, j); e != nil {
		return e
	}
	if e := tx.AddCredit(ctx, j.UserID, 0, j.FrozenCredits); e != nil {
		return e
	}
	_, e = tx.InsertLedger(ctx, &model.CreditLedger{UserID: j.UserID, DramaJobID: &j.ID, Type: model.LedgerFreeze, Amount: j.FrozenCredits})
	return e
}

// selectDramaEpisodes 统一估价与提交的步骤前置条件。
func selectDramaEpisodes(p *model.DramaProject, eps []model.DramaEpisode, req *model.DramaJobReq) ([]model.DramaEpisode, error) {
	if err := checkStoryboardRequest(req); err != nil {
		return nil, err
	}
	if req.Step == "compose" && len(req.Episodes) != 1 {
		return nil, errcode.ErrInvalidParams.WithMsg("合成须且只能选择一集")
	}
	switch req.Step {
	case "analyze":
		return nil, nil
	case "outline":
		if p.SourceSummary == "" {
			return nil, errcode.ErrDramaStepNotReady.WithMsg("请先完成原文研读")
		}
		return nil, nil
	case "scripts", "breakdown", "asset_prompts", "storyboard", "compose":
	default:
		return nil, errcode.ErrInvalidParams.WithMsg("不支持的流水线步骤")
	}
	if len(eps) == 0 {
		return nil, errcode.ErrDramaStepNotReady.WithMsg("请先生成分集大纲")
	}
	return filterDramaEpisodes(eps, req)
}

// filterDramaEpisodes 校验集号和剧本状态，保持集号自然顺序。
func filterDramaEpisodes(eps []model.DramaEpisode, req *model.DramaJobReq) ([]model.DramaEpisode, error) {
	seen := map[int]bool{}
	selected := []model.DramaEpisode{}
	for _, no := range req.Episodes {
		if no < 1 || no > 100 || seen[no] {
			return nil, errcode.ErrInvalidParams.WithMsg("集号须为1至100且不能重复")
		}
		seen[no] = true
	}
	for _, ep := range eps {
		if len(seen) > 0 && !seen[ep.No] {
			continue
		}
		if e := checkDramaEpisodeStep(ep, req.Step); e != nil {
			return nil, e
		}
		selected = append(selected, ep)
	}
	if len(selected) == 0 || len(seen) > 0 && len(selected) != len(seen) {
		return nil, errcode.ErrDramaStepNotReady.WithMsg("所选集不存在")
	}
	return selected, nil
}

// checkDramaEpisodeStep 锁定集不能重新生成正文或重新拆解。
func checkDramaEpisodeStep(ep model.DramaEpisode, step string) error {
	if step == "scripts" && ep.LockedAt != nil {
		return errcode.ErrDramaEpisodeLocked
	}
	if step == "breakdown" && (ep.ScriptStatus != "ready" || ep.LockedAt != nil) {
		return errcode.ErrDramaStepNotReady.WithMsg("所选集须已有剧本且尚未拆解")
	}
	if step == "storyboard" && (ep.ScriptStatus != "ready" || strings.TrimSpace(ep.ScriptMD) == "" || ep.LockedAt == nil) {
		return errcode.ErrDramaStepNotReady.WithMsg("所选集须已拆解资产且剧本已就绪")
	}
	return nil
}

// dramaQuote 复用现有定价，脚本逐集计费、研读按上下文分块计费。
func dramaQuote(p *model.DramaProject, step string, eps []model.DramaEpisode, snap *provider.Snapshot) int {
	if step == "outline" && len(dramaMarkedEpisodes(p.SourceText)) > 0 {
		return 0
	}
	requests := 1
	chars := utf8.RuneCountInString(p.SourceSummary) + utf8.RuneCountInString(p.OutlineMD) + 4096
	switch step {
	case "scripts":
		requests = max(1, len(eps))
	case "analyze":
		requests = len(dramaSourceChunks(p.SourceText, snap))
		if requests > 1 {
			requests++
		}
		chars = min(utf8.RuneCountInString(p.SourceText)+2048, dramaContextLimit(snap))
	case "breakdown", "storyboard":
		for _, ep := range eps {
			chars += utf8.RuneCountInString(ep.ScriptMD)
		}
	}
	return requests * modelcfg.Quote(snap.Model.Pricing, snap.Model.Capabilities, modelcfg.Spec{PromptChars: chars, Params: map[string]any{}})
}

// dramaRequestInputs 取脚本目录，不把其他集的全文重复送入模型。
func dramaRequestInputs(eps []model.DramaEpisode) []model.DramaEpisodeSummary {
	out := make([]model.DramaEpisodeSummary, 0, len(eps))
	for _, ep := range eps {
		v := episodeView(ep).DramaEpisodeSummary
		v.Synopsis = strings.TrimSpace(v.Synopsis)
		out = append(out, v)
	}
	return out
}
