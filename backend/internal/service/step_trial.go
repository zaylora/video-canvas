package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/repository"
	"video-canvas/internal/stepkit"
)

// trialDraft 是待写入的一条试跑：候选加按预估冻结的积分。
type trialDraft struct {
	cand   stepCandidate
	frozen int
}

// CreateTrials 发起试跑：用候选方法在所选样例上各跑一遍，对比时再加一条「当前生效方法」，两条 compare_of 互指。
// 试跑走 River 异步执行；费用从操作的管理员账户按预估冻结，结束按用量结算。
// 环节不存在 68012；技能或版本不可用 68014；该环节已有进行中的试跑 68017；积分不足 40001。
func (s *StepAdminService) CreateTrials(ctx context.Context, actorID uint64, key string, req *model.CreateStepTrialReq) (*model.StepTrialList, error) {
	// 1. 环节、样例、候选、模型都在事务外校验清楚，事务里只做写入。
	st, ok := s.reg.Get(key)
	if !ok {
		return nil, errcode.ErrStepNotFound
	}
	sample, ok := st.Sample(req.SampleKey)
	if !ok {
		return nil, errcode.ErrInvalidParams.WithMsg("试跑样例不存在")
	}
	cand, err := s.resolveCandidate(ctx, req.Candidate)
	if err != nil {
		return nil, err
	}
	snap, err := s.textModel(ctx, st, req.TextModel)
	if err != nil {
		return nil, err
	}
	ix, err := s.loadSkills(ctx)
	if err != nil {
		return nil, err
	}
	cands := []stepCandidate{cand}
	if req.Compare {
		// 对比的是「用户端此刻真正会用的方法」：绑定的技能不可用时就是兜底后的内置。
		cur, err := s.currentCandidate(ctx, key, ix)
		if err != nil {
			return nil, err
		}
		cands = append(cands, cur)
	}
	// 2. 按预估冻结：用样例输入拼一次提示词，乘以最多调用次数（首次加重写）。
	input, err := sample.Build(ctx)
	if err != nil {
		return nil, fmt.Errorf("构造试跑样例 %s 失败：%w", sample.Key, err)
	}
	drafts := make([]trialDraft, 0, len(cands))
	for _, c := range cands {
		method, err := s.methodOf(ctx, st, c)
		if err != nil {
			return nil, err
		}
		est, err := stepkit.Estimate(snap, st, method, input)
		if err != nil {
			return nil, err
		}
		drafts = append(drafts, trialDraft{cand: c, frozen: est})
	}
	// 3. 同一事务内：环节锁 → 进行中检查 → 冻结积分 → 写试跑 → 入队。
	rows, err := s.insertTrials(ctx, actorID, st.Key, sample.Key, req.TextModel, drafts)
	if err != nil {
		return nil, err
	}
	adminAudit(ctx, s.audit, actorID, model.AdminAuditStepTrial, model.AdminAuditTargetStep, rows[0].ID,
		map[string]any{"step": key, "sample": sample.Key, "model": req.TextModel, "source": cand.Source, "skill": cand.SkillName, "version": cand.Version, "compare": req.Compare})
	out := &model.StepTrialList{Trials: make([]model.StepTrialView, 0, len(rows))}
	for i := range rows {
		out.Trials = append(out.Trials, s.trialView(&rows[i], ix))
	}
	return out, nil
}

// insertTrials 在一个事务里写入试跑并冻结积分、入队。
func (s *StepAdminService) insertTrials(ctx context.Context, actorID uint64, stepKey, sampleKey, modelKey string, drafts []trialDraft) ([]model.StepTrial, error) {
	initial, err := s.credit.InitialCredits(ctx)
	if err != nil {
		return nil, err
	}
	total := 0
	for _, d := range drafts {
		total += d.frozen
	}
	now := s.now()
	rows := make([]model.StepTrial, len(drafts))
	err = s.repo.WithTx(ctx, func(tx repository.StepTx) error {
		// 环节锁串行化同一环节的发起与生效：并发提交时只有一个能通过进行中检查。
		if err := tx.LockStep(ctx, stepKey); err != nil {
			return err
		}
		busy, err := tx.ActiveTrials(ctx, stepKey, now.Add(-stepTrialStale))
		if err != nil {
			return err
		}
		if busy > 0 {
			return errcode.ErrStepTrialBusy
		}
		if err := freezeStepCredits(ctx, tx, actorID, initial, total); err != nil {
			return err
		}
		for i, d := range drafts {
			if err := insertOneTrial(ctx, tx, actorID, rows, i, newTrialRow(stepKey, sampleKey, modelKey, actorID, d, now)); err != nil {
				return err
			}
		}
		if len(rows) > 1 {
			// 第一条也指向第二条，两条互指，界面按 compare_of 并排展示。
			second := rows[1].ID
			rows[0].CompareOf = &second
			return tx.UpdateTrial(ctx, rows[0].ID, map[string]any{"compare_of": second})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// insertOneTrial 写入第 i 条试跑：第二条起 compare_of 指向第一条；随后写冻结流水并入队。结果放进 rows[i]。
func insertOneTrial(ctx context.Context, tx repository.StepTx, actorID uint64, rows []model.StepTrial, i int, row model.StepTrial) error {
	rows[i] = row
	if i > 0 {
		id := rows[0].ID
		rows[i].CompareOf = &id
	}
	if err := tx.InsertTrial(ctx, &rows[i]); err != nil {
		return err
	}
	if err := recordFreeze(ctx, tx, actorID, &rows[i]); err != nil {
		return err
	}
	return tx.EnqueueTrial(ctx, rows[i].ID)
}

// newTrialRow 构造一条 queued 的试跑行。
func newTrialRow(stepKey, sampleKey, modelKey string, actorID uint64, d trialDraft, now time.Time) model.StepTrial {
	t := model.StepTrial{StepKey: stepKey, Source: d.cand.Source, SampleKey: sampleKey, TextModelID: modelKey,
		Status: model.StepTrialQueued, Errors: []byte("[]"), Flags: []byte("[]"), Frozen: d.frozen, CreatedBy: actorID, CreatedAt: now}
	if d.cand.Source == stepkit.SourceSkill {
		id, v := d.cand.SkillID, d.cand.Version
		t.SkillID, t.SkillVersion = &id, &v
	}
	return t
}

// freezeStepCredits 在账户锁内检查可用积分够不够冻结，不够返回 40001。冻结额为 0（免费模型）时不动账户。
func freezeStepCredits(ctx context.Context, tx repository.StepTx, userID uint64, initial, total int) error {
	if total <= 0 {
		return nil
	}
	if err := tx.EnsureCredit(ctx, userID, initial); err != nil {
		return err
	}
	credit, err := tx.LockCredit(ctx, userID)
	if err != nil {
		return err
	}
	if credit.Balance-credit.Frozen < total {
		return errcode.ErrInsufficientCredits
	}
	return nil
}

// recordFreeze 为一条试跑写冻结流水并增加账户冻结额；流水关联试跑，(step_trial_id, type) 唯一保证幂等。
func recordFreeze(ctx context.Context, tx repository.StepTx, userID uint64, t *model.StepTrial) error {
	if t.Frozen <= 0 {
		return nil
	}
	if _, err := tx.InsertLedger(ctx, &model.CreditLedger{UserID: userID, StepTrialID: &t.ID, Type: model.LedgerFreeze, Amount: t.Frozen}); err != nil {
		return err
	}
	return tx.AddCredit(ctx, userID, 0, t.Frozen)
}

// textModel 取试跑用的文本模型快照：必须是可用的 agent 类模型，并且满足环节的媒体需求（看图 / 看视频）。
func (s *StepAdminService) textModel(ctx context.Context, st stepkit.Step, key string) (*provider.Snapshot, error) {
	snap, err := s.models.Snapshot(ctx, key)
	if errors.Is(err, provider.ErrModelUnavailable) || (err == nil && (snap == nil || snap.Model.Kind != modelcfg.KindAgent)) {
		return nil, errcode.ErrInvalidParams.WithMsg("所选文本模型不可用")
	}
	if err != nil {
		return nil, err
	}
	caps := snap.Model.Capabilities
	if st.Media == stepkit.MediaImages && !caps.Vision {
		return nil, errcode.ErrInvalidParams.WithMsg("该环节需要看图，所选文本模型不支持")
	}
	if st.Media == stepkit.MediaVideo && !caps.Video && !caps.Vision {
		return nil, errcode.ErrInvalidParams.WithMsg("该环节需要看视频，所选文本模型不支持")
	}
	return snap, nil
}

// currentCandidate 返回环节此刻真正生效的候选：绑定的技能可用就是该技能该版本，否则（含兜底）是内置。
func (s *StepAdminService) currentCandidate(ctx context.Context, key string, ix *skillIndex) (stepCandidate, error) {
	row, err := s.repo.GetBinding(ctx, key)
	if errors.Is(err, repository.ErrNotFound) {
		return stepCandidate{Source: stepkit.SourceBuiltin}, nil
	}
	if err != nil {
		return stepCandidate{}, err
	}
	b := toBinding(key, row)
	if b.Source != stepkit.SourceSkill {
		return stepCandidate{Source: stepkit.SourceBuiltin}, nil
	}
	if _, reason := ix.usable(b.SkillName, b.SkillVersion); reason != "" {
		return stepCandidate{Source: stepkit.SourceBuiltin}, nil
	}
	return stepCandidate{Source: stepkit.SourceSkill, SkillID: b.SkillID, SkillName: b.SkillName, Version: b.SkillVersion}, nil
}

// methodOf 读取候选的方法文本（估价和执行共用），不做兜底：技能不可用返回 68014。
func (s *StepAdminService) methodOf(ctx context.Context, st stepkit.Step, c stepCandidate) (stepkit.Method, error) {
	if c.Source != stepkit.SourceSkill {
		return stepkit.BuiltinMethod(st), nil
	}
	m, err := stepkit.SkillMethod(ctx, s.skills, st, c.SkillName, c.Version)
	if errors.Is(err, stepkit.ErrSkillUnavailable) {
		return stepkit.Method{}, errcode.ErrStepSkillUnavail
	}
	return m, err
}
