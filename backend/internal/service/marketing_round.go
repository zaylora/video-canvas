package service

import (
	"context"
	"encoding/json"
	"fmt"

	"video-canvas/internal/marketing"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
)

// roundSpec 是开一轮写脚本需要的全部已校验输入。
type roundSpec struct {
	settings    model.MarketingSettings
	refID       *uint64
	modelID     *uint64 // 本轮生效的模特图
	costs       stepCosts
	createCount int
	hasFacts    bool              // 项目已有事实卡：facts 环节沿用
	textModels  map[string]string // 环节 key → 预选的文本模型（运行时按能力再确认）
}

// CreateProject 建营销项目并开第一轮：校验素材、视频模型和文本模型 → 同事务建项目、轮次、环节运行、冻结积分并入队。
// 错误：商品图 / 素材无效 68002、没有可用文本模型 68003、视频模型不满足 68004、参考视频超限 68005、积分不足 40001。
func (s *MarketingService) CreateProject(ctx context.Context, userID uint64, req *model.CreateMarketingProjectReq) (*model.MarketingProjectView, error) {
	st := normalizeSettings(req.Settings)
	refID := optionalID(req.RefVideoAssetID)
	modelID := optionalID(req.ModelAssetID)
	hasRef := refID != nil
	if err := checkNewPlatform(st); err != nil {
		return nil, err
	}
	createCount, err := planOf(st, hasRef)
	if err != nil {
		return nil, err
	}
	images := len(req.ProductAssetIDs)
	if modelID != nil {
		images++
	}
	snap, err := s.checkVideoModel(ctx, st.VideoModel, st.Params, images, hasRef)
	if err != nil {
		return nil, err
	}
	if _, err := s.checkAssets(ctx, userID, req.ProductAssetIDs, modelID, refID, snap.Model.Capabilities); err != nil {
		return nil, err
	}
	spec, err := s.prepareRound(ctx, st, refID, modelID, false, createCount)
	if err != nil {
		return nil, err
	}
	idsJSON, err := json.Marshal(req.ProductAssetIDs)
	if err != nil {
		return nil, err
	}
	p := &model.MarketingProject{UserID: userID, Title: marketingProjectPlaceholder, ProductAssetIDs: idsJSON, ModelAssetID: modelID}
	var round *model.MarketingRound
	err = s.repo.WithTx(ctx, func(tx repository.MarketingTx) error {
		if err := tx.InsertProject(ctx, p); err != nil {
			return err
		}
		round, err = s.insertRound(ctx, tx, p, spec)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.pushRound(ctx, userID, round.ID)
	return s.GetProject(ctx, userID, p.ID)
}

// prepareRound 在事务外算好一轮的预估积分和预选文本模型。
func (s *MarketingService) prepareRound(ctx context.Context, st model.MarketingSettings, refID, modelID *uint64, hasFacts bool, createCount int) (roundSpec, error) {
	hasRef := refID != nil
	costs, err := s.quoteSteps(ctx, st, hasRef, hasFacts, createCount)
	if err != nil {
		return roundSpec{}, err
	}
	spec := roundSpec{settings: st, refID: refID, modelID: modelID, costs: costs, createCount: createCount, hasFacts: hasFacts, textModels: map[string]string{}}
	for key, need := range map[string]textNeed{marketing.StepFacts: needVision, marketing.StepReverse: needVideo, marketing.StepFollow: needAny, marketing.StepCreate: needAny} {
		if key == marketing.StepFacts && hasFacts || (key == marketing.StepReverse || key == marketing.StepFollow) && !hasRef {
			continue
		}
		snap, err := s.pickTextModel(ctx, st.TextModel, need)
		if err != nil {
			return roundSpec{}, err
		}
		spec.textModels[key] = snap.Model.Key
	}
	return spec, nil
}

// insertRound 在项目已加锁（或刚创建）的事务里写入一轮：轮次、环节运行、冻结积分、入队 River job。
// 项目已有写脚本中的轮次返回 68007。
func (s *MarketingService) insertRound(ctx context.Context, tx repository.MarketingTx, p *model.MarketingProject, sp roundSpec) (*model.MarketingRound, error) {
	if n, err := tx.ScriptingRounds(ctx, p.ID); err != nil {
		return nil, err
	} else if n > 0 {
		return nil, errcode.ErrMarketingRoundBusy
	}
	hasRef := sp.refID != nil
	settingsJSON, err := json.Marshal(sp.settings)
	if err != nil {
		return nil, err
	}
	plan := sp.createCount
	if hasRef {
		plan++
	}
	round := &model.MarketingRound{UserID: p.UserID, ProjectID: p.ID, Status: model.MarketingRoundScripting, Warnings: []byte("[]"),
		Settings: settingsJSON, RefVideoAssetID: sp.refID, ModelAssetID: sp.modelID, PlanCount: plan, Frozen: sp.costs.total()}
	if err := tx.InsertRound(ctx, round); err != nil {
		return nil, err
	}
	var runs []*model.MarketingStepRun
	add := func(key string, frozen int, status string) {
		runs = append(runs, &model.MarketingStepRun{UserID: p.UserID, RoundID: round.ID, StepKey: key, Method: []byte("{}"),
			TextModelID: sp.textModels[key], Status: status, Frozen: frozen})
	}
	if sp.hasFacts {
		add(marketing.StepFacts, 0, model.MarketingRunReused)
	} else {
		add(marketing.StepFacts, sp.costs.facts, model.MarketingRunQueued)
	}
	if hasRef {
		add(marketing.StepReverse, sp.costs.reverse, model.MarketingRunQueued)
		add(marketing.StepFollow, sp.costs.follow, model.MarketingRunQueued)
	}
	add(marketing.StepCreate, sp.costs.create, model.MarketingRunQueued)
	if err := s.insertRuns(ctx, tx, p.UserID, runs); err != nil {
		return nil, err
	}
	if sp.modelID != nil && (p.ModelAssetID == nil || *p.ModelAssetID != *sp.modelID) {
		if err := tx.UpdateProject(ctx, p.ID, map[string]any{"model_asset_id": *sp.modelID}); err != nil {
			return nil, err
		}
	}
	if err := tx.EnqueueRound(ctx, round.ID); err != nil {
		return nil, err
	}
	return round, nil
}

// insertRuns 写入环节运行并冻结它们的积分：账户锁内检查可用积分够不够（40001），每个环节一条冻结流水。
func (s *MarketingService) insertRuns(ctx context.Context, tx repository.MarketingTx, userID uint64, runs []*model.MarketingStepRun) error {
	total := 0
	for _, r := range runs {
		total += r.Frozen
	}
	if total > 0 {
		initial, err := s.initialCredits(ctx)
		if err != nil {
			return err
		}
		if err := tx.EnsureCredit(ctx, userID, initial); err != nil {
			return err
		}
		acc, err := tx.LockCredit(ctx, userID)
		if err != nil {
			return err
		}
		if acc.Balance-acc.Frozen < total {
			return errcode.ErrInsufficientCredits
		}
	}
	for _, r := range runs {
		r.CreatedAt = s.now()
		if err := tx.InsertRun(ctx, r); err != nil {
			return err
		}
		if r.Frozen <= 0 {
			continue
		}
		if _, err := tx.InsertLedger(ctx, &model.CreditLedger{UserID: userID, MarketingRunID: &r.ID, Type: model.LedgerFreeze, Amount: r.Frozen}); err != nil {
			return err
		}
	}
	if total > 0 {
		return tx.AddCredit(ctx, userID, 0, total)
	}
	return nil
}

// initialCredits 返回新积分账户的初始积分。
func (s *MarketingService) initialCredits(ctx context.Context) (int, error) {
	if s.credits == nil {
		return DefaultInitialCredits, nil
	}
	return s.credits.InitialCredits(ctx)
}

// NewRound 给已有项目开新一轮。不传模特图沿用项目已有的，传了就替换项目的模特图。
// 错误：项目不存在 68001、项目正在写脚本 68007，其余同创建项目。
func (s *MarketingService) NewRound(ctx context.Context, userID, projectID uint64, req *model.CreateMarketingRoundReq) (*model.MarketingRoundView, error) {
	p, err := s.repo.Project(ctx, userID, projectID, false)
	if mktNotFound(err) {
		return nil, errcode.ErrMarketingNotFound
	}
	if err != nil {
		return nil, err
	}
	st := normalizeSettings(req.Settings)
	refID := optionalID(req.RefVideoAssetID)
	modelID := optionalID(req.ModelAssetID)
	if modelID == nil {
		modelID = p.ModelAssetID
	}
	hasRef := refID != nil
	if err := checkNewPlatform(st); err != nil {
		return nil, err
	}
	createCount, err := planOf(st, hasRef)
	if err != nil {
		return nil, err
	}
	var productIDs []uint64
	if err := json.Unmarshal(p.ProductAssetIDs, &productIDs); err != nil {
		return nil, fmt.Errorf("解码项目商品图失败：%w", err)
	}
	images := len(productIDs)
	if modelID != nil {
		images++
	}
	snap, err := s.checkVideoModel(ctx, st.VideoModel, st.Params, images, hasRef)
	if err != nil {
		return nil, err
	}
	if _, err := s.checkAssets(ctx, userID, productIDs, req.ModelAssetID, refID, snap.Model.Capabilities); err != nil {
		return nil, err
	}
	spec, err := s.prepareRound(ctx, st, refID, modelID, len(p.Product) > 0, createCount)
	if err != nil {
		return nil, err
	}
	var round *model.MarketingRound
	err = s.repo.WithTx(ctx, func(tx repository.MarketingTx) error {
		locked, err := tx.Project(ctx, userID, projectID, true)
		if mktNotFound(err) {
			return errcode.ErrMarketingNotFound
		}
		if err != nil {
			return err
		}
		spec.hasFacts = len(locked.Product) > 0
		round, err = s.insertRound(ctx, tx, locked, spec)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.viewAndPush(ctx, userID, round.ID)
}

// CancelRound 取消写脚本：排队中的环节关闭并解冻，进行中的环节会被中断（用量照实结算）；轮次已结束时原样返回。
func (s *MarketingService) CancelRound(ctx context.Context, userID, roundID uint64) (*model.MarketingRoundView, error) {
	err := s.repo.WithTx(ctx, func(tx repository.MarketingTx) error {
		r, err := tx.Round(ctx, userID, roundID, true)
		if mktNotFound(err) {
			return errcode.ErrMarketingNotFound
		}
		if err != nil || r.Status != model.MarketingRoundScripting {
			return err
		}
		runs, err := tx.RoundRuns(ctx, r.ID)
		if err != nil {
			return err
		}
		for _, run := range latestRuns(runs) {
			stale := run.Status == model.MarketingRunRunning && run.StartedAt != nil && s.now().Sub(*run.StartedAt) > marketingStepTimeout+marketingCancelPoll*5
			if run.Status != model.MarketingRunQueued && !stale {
				continue
			}
			if err := s.closeRun(ctx, tx, &run, model.MarketingRunCanceled, ""); err != nil {
				return err
			}
		}
		if err := s.releaseRewriting(ctx, tx, r.ID); err != nil {
			return err
		}
		return tx.UpdateRound(ctx, r.ID, map[string]any{"status": model.MarketingRoundCanceled, "error": ""})
	})
	if err != nil {
		return nil, err
	}
	return s.viewAndPush(ctx, userID, roundID)
}

// closeRun 把一个还没结算的环节运行关掉（跳过 / 取消）并全额解冻。已被别处结算的运行不处理。
func (s *MarketingService) closeRun(ctx context.Context, tx repository.MarketingTx, run *model.MarketingStepRun, status, msg string) error {
	now := s.now()
	err := tx.UpdateRunIf(ctx, run.ID, []string{model.MarketingRunQueued, model.MarketingRunRunning},
		map[string]any{"status": status, "error": msg, "finished_at": now})
	if errorsIsConflict(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.settleRun(ctx, tx, run, 0)
}

// settleRun 结算一个环节运行的冻结：扣 charged，其余退回，写 settle / refund 流水，更新账户和轮次累计。
// 调用方保证同一运行只结算一次（UpdateRunIf 的状态迁移）；流水唯一索引是最后一道防线。
func (s *MarketingService) settleRun(ctx context.Context, tx repository.MarketingTx, run *model.MarketingStepRun, charged int) error {
	charged = min(max(charged, 0), run.Frozen)
	if run.Frozen > 0 {
		if _, err := tx.LockCredit(ctx, run.UserID); err != nil {
			return err
		}
		inserted, err := tx.InsertLedger(ctx, &model.CreditLedger{UserID: run.UserID, MarketingRunID: &run.ID, Type: model.LedgerSettle, Amount: charged})
		if err != nil {
			return err
		}
		if rest := run.Frozen - charged; rest > 0 {
			if _, err := tx.InsertLedger(ctx, &model.CreditLedger{UserID: run.UserID, MarketingRunID: &run.ID, Type: model.LedgerRefund, Amount: rest}); err != nil {
				return err
			}
		}
		if inserted {
			if err := tx.AddCredit(ctx, run.UserID, -charged, -run.Frozen); err != nil {
				return err
			}
		}
	}
	if charged > 0 {
		if err := tx.UpdateRun(ctx, run.ID, map[string]any{"charged": charged}); err != nil {
			return err
		}
		return tx.UpdateRound(ctx, run.RoundID, map[string]any{"charged": repository.Add("charged", charged)})
	}
	return nil
}

// releaseRewriting 把轮次里「重写中」的脚本放回草稿（取消或重写失败后，脚本内容还是上一版）。
func (s *MarketingService) releaseRewriting(ctx context.Context, tx repository.MarketingTx, roundID uint64) error {
	scripts, err := tx.RoundScripts(ctx, roundID)
	if err != nil {
		return err
	}
	for i := range scripts {
		if scripts[i].Status == model.MarketingScriptRewriting {
			if err := tx.UpdateScript(ctx, scripts[i].ID, map[string]any{"status": model.MarketingScriptDraft}); err != nil {
				return err
			}
		}
	}
	return nil
}

// latestRuns 取每个环节最新的一条运行记录（输入按创建顺序）。
func latestRuns(runs []model.MarketingStepRun) []model.MarketingStepRun {
	byKey := map[string]model.MarketingStepRun{}
	var order []string
	for _, r := range runs {
		if _, ok := byKey[r.StepKey]; !ok {
			order = append(order, r.StepKey)
		}
		byKey[r.StepKey] = r
	}
	out := make([]model.MarketingStepRun, 0, len(order))
	for _, k := range order {
		out = append(out, byKey[k])
	}
	return out
}
