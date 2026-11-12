// 本文件是营销轮次的重试、重写跟拍和拆解编辑（建轮次、取消、结算在 marketing_round.go）。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"video-canvas/internal/marketing"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
	"video-canvas/internal/stepkit"
)

// 环节依赖：facts → follow、create；reverse → follow。
var marketingDependents = map[string][]string{
	marketing.StepFacts:   {marketing.StepFollow, marketing.StepCreate},
	marketing.StepReverse: {marketing.StepFollow},
}

// RetryStep 重试一个失败的环节：新建一条运行记录，连同依赖它、没有成功的后续环节一起重跑（重试事实卡等于整轮重试）。
// 方法用当前绑定，设置用该轮快照。轮次必须已结束（写脚本中返回 68007）；积分不足 40001。
func (s *MarketingService) RetryStep(ctx context.Context, userID, runID uint64) (*model.MarketingRoundView, error) {
	run, err := s.repo.Run(ctx, userID, runID, false)
	if mktNotFound(err) {
		return nil, errcode.ErrMarketingNotFound
	}
	if err != nil {
		return nil, err
	}
	round, err := s.repo.Round(ctx, userID, run.RoundID, false)
	if mktNotFound(err) {
		return nil, errcode.ErrMarketingNotFound
	}
	if err != nil {
		return nil, err
	}
	st, err := settingsOf(round)
	if err != nil {
		return nil, err
	}
	if run.Status != model.MarketingRunFailed && run.Status != model.MarketingRunSkipped && run.Status != model.MarketingRunCanceled {
		return nil, errcode.ErrInvalidParams.WithMsg("只有失败的环节可以重试")
	}
	err = s.repo.WithTx(ctx, func(tx repository.MarketingTx) error {
		if _, err := tx.Project(ctx, userID, round.ProjectID, true); mktNotFound(err) {
			return errcode.ErrMarketingNotFound
		} else if err != nil {
			return err
		}
		return s.requeue(ctx, tx, userID, round.ID, run.StepKey, st)
	})
	if err != nil {
		return nil, err
	}
	return s.viewAndPush(ctx, userID, round.ID)
}

// requeue 在已锁项目的事务里为一个环节及其未成功的下游新建运行记录、冻结积分、把轮次放回写脚本并入队。
func (s *MarketingService) requeue(ctx context.Context, tx repository.MarketingTx, userID, roundID uint64, stepKey string, st model.MarketingSettings) error {
	round, err := tx.Round(ctx, userID, roundID, true)
	if err != nil {
		return err
	}
	if round.Status == model.MarketingRoundScripting {
		return errcode.ErrMarketingRoundBusy
	}
	all, err := tx.RoundRuns(ctx, roundID)
	if err != nil {
		return err
	}
	latest := map[string]model.MarketingStepRun{}
	for _, r := range latestRuns(all) {
		latest[r.StepKey] = r
	}
	keys := []string{stepKey}
	for _, dep := range marketingDependents[stepKey] {
		if r, ok := latest[dep]; ok && slices.Contains([]string{model.MarketingRunFailed, model.MarketingRunSkipped, model.MarketingRunCanceled}, r.Status) {
			keys = append(keys, dep)
		}
	}
	if err := s.checkRequeueInputs(ctx, tx, round, stepKey); err != nil {
		return err
	}
	runs, total, err := s.newRuns(ctx, userID, round, st, keys)
	if err != nil {
		return err
	}
	if err := s.insertRuns(ctx, tx, userID, runs); err != nil {
		return err
	}
	if err := tx.UpdateRound(ctx, round.ID, map[string]any{"status": model.MarketingRoundScripting, "error": "", "frozen": repository.Add("frozen", total)}); err != nil {
		return err
	}
	if err := s.markRewriting(ctx, tx, round.ID, keys); err != nil {
		return err
	}
	return tx.EnqueueRound(ctx, round.ID)
}

// checkRequeueInputs 确认重试的环节有它需要的上游：跟拍要有拆解（68016），创意与跟拍要有事实卡。
func (s *MarketingService) checkRequeueInputs(ctx context.Context, tx repository.MarketingTx, round *model.MarketingRound, stepKey string) error {
	p, err := tx.ProjectByID(ctx, round.ProjectID)
	if err != nil {
		return err
	}
	switch stepKey {
	case marketing.StepFollow:
		if len(round.Reverse) == 0 {
			return errcode.ErrMarketingNoReverse
		}
		fallthrough
	case marketing.StepCreate:
		if len(p.Product) == 0 {
			return errcode.ErrInvalidParams.WithMsg("请先重试商品事实卡")
		}
	}
	return nil
}

// newRuns 为一组环节构造新的运行记录并预估各自的冻结积分。返回记录与冻结合计。
func (s *MarketingService) newRuns(ctx context.Context, userID uint64, round *model.MarketingRound, st model.MarketingSettings, keys []string) ([]*model.MarketingStepRun, int, error) {
	hasRef := round.RefVideoAssetID != nil
	createCount, err := planOf(st, hasRef)
	if err != nil {
		return nil, 0, err
	}
	costs, err := s.quoteSteps(ctx, st, hasRef, false, createCount)
	if err != nil {
		return nil, 0, err
	}
	frozen := map[string]int{marketing.StepFacts: costs.facts, marketing.StepReverse: costs.reverse, marketing.StepFollow: costs.follow, marketing.StepCreate: costs.create}
	need := map[string]textNeed{marketing.StepFacts: needVision, marketing.StepReverse: needVideo, marketing.StepFollow: needAny, marketing.StepCreate: needAny}
	var runs []*model.MarketingStepRun
	total := 0
	for _, k := range keys {
		snap, err := s.pickTextModel(ctx, st.TextModel, need[k])
		if err != nil {
			return nil, 0, err
		}
		runs = append(runs, &model.MarketingStepRun{UserID: userID, RoundID: round.ID, StepKey: k, Method: []byte("{}"),
			TextModelID: snap.Model.Key, Status: model.MarketingRunQueued, Frozen: frozen[k]})
		total += frozen[k]
	}
	return runs, total, nil
}

// markRewriting 重写跟拍（或重试跟拍）时，把已有的跟拍脚本标成重写中，界面据此显示并禁止修改和出片。
func (s *MarketingService) markRewriting(ctx context.Context, tx repository.MarketingTx, roundID uint64, keys []string) error {
	if !slices.Contains(keys, marketing.StepFollow) {
		return nil
	}
	scripts, err := tx.RoundScripts(ctx, roundID)
	if err != nil {
		return err
	}
	for i := range scripts {
		if scripts[i].Kind == marketing.KindFollow {
			return tx.UpdateScript(ctx, scripts[i].ID, map[string]any{"status": model.MarketingScriptRewriting})
		}
	}
	return nil
}

// Refollow 按当前拆解重写跟拍版：只重跑 marketing.follow，按文本模型计费。
// 没有拆解 68016；跟拍脚本正在出片或重写 68006；项目正在写脚本 68007；积分不足 40001。
func (s *MarketingService) Refollow(ctx context.Context, userID, roundID uint64) (*model.MarketingRoundView, error) {
	round, err := s.repo.Round(ctx, userID, roundID, false)
	if mktNotFound(err) {
		return nil, errcode.ErrMarketingNotFound
	}
	if err != nil {
		return nil, err
	}
	if len(round.Reverse) == 0 {
		return nil, errcode.ErrMarketingNoReverse
	}
	st, err := settingsOf(round)
	if err != nil {
		return nil, err
	}
	err = s.repo.WithTx(ctx, func(tx repository.MarketingTx) error {
		if _, err := tx.Project(ctx, userID, round.ProjectID, true); mktNotFound(err) {
			return errcode.ErrMarketingNotFound
		} else if err != nil {
			return err
		}
		if err := s.followIdle(ctx, tx, userID, round.ID); err != nil {
			return err
		}
		return s.requeue(ctx, tx, userID, round.ID, marketing.StepFollow, st)
	})
	if err != nil {
		return nil, err
	}
	return s.viewAndPush(ctx, userID, round.ID)
}

// followIdle 确认轮次的跟拍脚本没有在出片或重写；没有跟拍脚本也算空闲。
func (s *MarketingService) followIdle(ctx context.Context, tx repository.MarketingTx, userID, roundID uint64) error {
	scripts, err := tx.RoundScripts(ctx, roundID)
	if err != nil {
		return err
	}
	for i := range scripts {
		if scripts[i].Kind != marketing.KindFollow {
			continue
		}
		locked, err := tx.Script(ctx, userID, scripts[i].ID, true)
		if err != nil {
			return err
		}
		return s.scriptIdle(ctx, tx, locked)
	}
	return nil
}

// scriptIdle 确认脚本没有在出片或重写，否则返回 68006。出片状态以生成任务表为准。
func (s *MarketingService) scriptIdle(ctx context.Context, tx repository.MarketingTx, sc *model.MarketingScript) error {
	if sc.Status == model.MarketingScriptRewriting {
		return errcode.ErrMarketingScriptBusy
	}
	tasks, err := tx.ScriptTasks(ctx, sc.UserID, []string{marketingNode(sc.ID)})
	if err != nil {
		return err
	}
	for i := range tasks {
		if slices.Contains(model.ActiveTaskStatuses, tasks[i].Status) {
			return errcode.ErrMarketingScriptBusy
		}
	}
	return nil
}

// reverseOf 解码轮次的拆解。
func reverseOf(raw []byte) (marketing.Reverse, error) {
	var r marketing.Reverse
	if err := json.Unmarshal(raw, &r); err != nil {
		return r, fmt.Errorf("解码拆解失败：%w", err)
	}
	return r, nil
}

// UpdateReverse 保存用户对拆解的修改：只认镜头的 action / dialogue / product_role / key_colors，其余字段以服务端为准。
// reverse_revision 不一致 68009；合并后不过校验 68015；跟拍脚本正在出片或重写 68006。
func (s *MarketingService) UpdateReverse(ctx context.Context, userID, roundID uint64, req *model.UpdateMarketingReverseReq) (*model.MarketingRoundView, error) {
	err := s.repo.WithTx(ctx, func(tx repository.MarketingTx) error {
		r, err := tx.Round(ctx, userID, roundID, true)
		if mktNotFound(err) {
			return errcode.ErrMarketingNotFound
		}
		if err != nil {
			return err
		}
		if len(r.Reverse) == 0 {
			return errcode.ErrMarketingNoReverse
		}
		if r.ReverseRevision != req.ReverseRevision {
			return errcode.ErrMarketingConflict
		}
		if err := s.followIdle(ctx, tx, userID, r.ID); err != nil {
			return err
		}
		base, err := reverseOf(r.Reverse)
		if err != nil {
			return err
		}
		merged, err := mergeReverseEdit(base, req.Reverse)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(merged)
		if err != nil {
			return err
		}
		return tx.UpdateRound(ctx, r.ID, map[string]any{"reverse": raw, "reverse_revision": repository.Add("reverse_revision", 1)})
	})
	if err != nil {
		return nil, err
	}
	return s.viewAndPush(ctx, userID, roundID)
}

// mergeReverseEdit 把请求里的拆解合并进服务端的拆解：镜头按 id 一一对应（数量、id 必须一致），
// 只取 action / dialogue / product_role / key_colors，合并后按拆解的同一套校验检查。
func mergeReverseEdit(base marketing.Reverse, edit model.MarketingReverse) (marketing.Reverse, error) {
	if len(edit.Shots) != len(base.Shots) {
		return base, errcode.ErrMarketingReverseBad.WithMsg("镜头数量不能改")
	}
	out := base
	out.Shots = append([]marketing.ReverseShot(nil), base.Shots...)
	for i, es := range edit.Shots {
		if es.ID != base.Shots[i].ID {
			return base, errcode.ErrMarketingReverseBad.WithMsg("镜头顺序和编号不能改")
		}
		out.Shots[i].Action = strings.TrimSpace(es.Action)
		out.Shots[i].Dialogue = strings.TrimSpace(es.Dialogue)
		out.Shots[i].ProductRole = es.ProductRole
		out.Shots[i].KeyColors = nonNil(es.KeyColors)
	}
	duration := 0.0
	if n := len(base.Shots); n > 0 {
		duration = base.Shots[n-1].End
	}
	if err := marketing.ValidateReverse(out, duration); err != nil {
		return base, errcode.ErrMarketingReverseBad.WithMsg("拆解内容不合法：" + strings.Join(stepkit.ErrorList(err), "；"))
	}
	return out, nil
}

// RestoreReverse 把拆解恢复成模型原稿。没有拆解 68016；跟拍脚本正在出片或重写 68006。
func (s *MarketingService) RestoreReverse(ctx context.Context, userID, roundID uint64) (*model.MarketingRoundView, error) {
	err := s.repo.WithTx(ctx, func(tx repository.MarketingTx) error {
		r, err := tx.Round(ctx, userID, roundID, true)
		if mktNotFound(err) {
			return errcode.ErrMarketingNotFound
		}
		if err != nil {
			return err
		}
		if len(r.ReverseOriginal) == 0 {
			return errcode.ErrMarketingNoReverse
		}
		if err := s.followIdle(ctx, tx, userID, r.ID); err != nil {
			return err
		}
		return tx.UpdateRound(ctx, r.ID, map[string]any{"reverse": []byte(r.ReverseOriginal), "reverse_revision": repository.Add("reverse_revision", 1)})
	})
	if err != nil {
		return nil, err
	}
	return s.viewAndPush(ctx, userID, roundID)
}

// errorsIsConflict 判断仓储的状态迁移冲突（已被别处处理）。
func errorsIsConflict(err error) bool { return errors.Is(err, repository.ErrStateConflict) }
