package service

import (
	"context"
	"encoding/json"
	"errors"

	"go.uber.org/zap"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/repository"
	"video-canvas/internal/stepkit"
)

// SetBinding 把环节的方法设为候选：候选必须有一次同环节、同候选（来源 + 技能 + 版本）、通过格式校验、
// 24 小时内的试跑作依据，否则返回 68013；技能或版本不可用返回 68014；环节不存在 68012。
// 绑定钉死版本号，技能之后发新版本不会自动跟随。成功后写绑定历史和审计，返回最新详情。
func (s *StepAdminService) SetBinding(ctx context.Context, actorID uint64, key string, req *model.SetStepBindingReq) (*model.StepDetail, error) {
	// 1. 环节和候选先校验：不存在的环节、不可用的技能不必去查试跑。
	if _, ok := s.reg.Get(key); !ok {
		return nil, errcode.ErrStepNotFound
	}
	cand, err := s.resolveCandidate(ctx, model.StepCandidateReq{Source: req.Source, SkillName: req.SkillName, Version: req.Version})
	if err != nil {
		return nil, err
	}
	trialID, ok := idcodec.Decode(req.TrialID)
	if !ok {
		return nil, errcode.ErrStepTrialNeeded
	}
	// 2. 环节锁内核对试跑并改绑定：检查与写入之间绑定和试跑都不会被别的请求改掉。
	var from model.StepMethodRef
	err = s.repo.WithTx(ctx, func(tx repository.StepTx) error {
		if err := tx.LockStep(ctx, key); err != nil {
			return err
		}
		trial, err := tx.Trial(ctx, trialID, true)
		if errors.Is(err, repository.ErrNotFound) {
			return errcode.ErrStepTrialNeeded
		}
		if err != nil {
			return err
		}
		if !s.trialQualifies(trial, key, cand) {
			return errcode.ErrStepTrialNeeded
		}
		from, err = s.currentRef(ctx, tx, key)
		if err != nil {
			return err
		}
		return saveBinding(ctx, tx, key, cand, from, trial.ID, actorID)
	})
	if err != nil {
		return nil, err
	}
	adminAudit(ctx, s.audit, actorID, model.AdminAuditStepBinding, model.AdminAuditTargetStep, trialID,
		map[string]any{"step": key, "from_source": from.Source, "from_skill": from.SkillName, "from_version": from.SkillVersion,
			"to_source": cand.Source, "to_skill": cand.SkillName, "to_version": cand.Version})
	return s.Get(ctx, key)
}

// trialQualifies 判断一次试跑能不能作为「设为生效」的依据：同一环节、已成功结束、格式校验通过、
// 候选与要生效的完全一致，并且结束在 24 小时之内。
func (s *StepAdminService) trialQualifies(t *model.StepTrial, key string, c stepCandidate) bool {
	if t.StepKey != key || t.Status != model.StepTrialSucceeded || !t.Passed || !c.sameAs(t) {
		return false
	}
	finished := t.CreatedAt
	if t.FinishedAt != nil {
		finished = *t.FinishedAt
	}
	return s.now().Sub(finished) <= stepBindingTrialTTL
}

// currentRef 读取事务内环节当前的绑定，没有记录是内置。
func (s *StepAdminService) currentRef(ctx context.Context, tx repository.StepTx, key string) (model.StepMethodRef, error) {
	row, err := tx.GetBinding(ctx, key)
	if errors.Is(err, repository.ErrNotFound) {
		return model.StepMethodRef{Source: stepkit.SourceBuiltin}, nil
	}
	if err != nil {
		return model.StepMethodRef{}, err
	}
	ref := model.StepMethodRef{Source: row.Source, SkillName: row.SkillName}
	if row.SkillID != nil {
		ref.SkillID = *row.SkillID
	}
	if row.SkillVersion != nil {
		ref.SkillVersion = *row.SkillVersion
	}
	if row.Source != stepkit.SourceSkill {
		ref = model.StepMethodRef{Source: stepkit.SourceBuiltin}
	}
	return ref, nil
}

// saveBinding 写入新绑定和一条历史（from → to）。
func saveBinding(ctx context.Context, tx repository.StepTx, key string, cand stepCandidate, from model.StepMethodRef, trialID, actorID uint64) error {
	b := &model.StepBinding{StepKey: key, Source: cand.Source, TrialID: &trialID, UpdatedBy: actorID}
	if cand.Source == stepkit.SourceSkill {
		id, v := cand.SkillID, cand.Version
		b.SkillID, b.SkillVersion = &id, &v
	}
	if err := tx.SaveBinding(ctx, b); err != nil {
		return err
	}
	fromJSON, err := json.Marshal(from)
	if err != nil {
		return err
	}
	toJSON, err := json.Marshal(cand.ref())
	if err != nil {
		return err
	}
	return tx.InsertHistory(ctx, &model.StepBindingHistory{StepKey: key, FromBinding: fromJSON, ToBinding: toJSON, TrialID: &trialID, ActorID: actorID})
}

// stepBindingStore 把仓储适配成 stepkit.BindingStore。
type stepBindingStore struct{ repo StepRepo }

// NewStepBindingStore 返回读绑定的 stepkit.BindingStore：没有记录等于内置，技能被删时 SkillName 为空。
func NewStepBindingStore(repo StepRepo) stepkit.BindingStore { return stepBindingStore{repo: repo} }

// GetBinding 实现 stepkit.BindingStore。
func (b stepBindingStore) GetBinding(ctx context.Context, stepKey string) (stepkit.Binding, bool, error) {
	row, err := b.repo.GetBinding(ctx, stepKey)
	if errors.Is(err, repository.ErrNotFound) {
		return stepkit.Binding{}, false, nil
	}
	if err != nil {
		return stepkit.Binding{}, false, err
	}
	return toBinding(stepKey, row), true, nil
}

// Resolver 返回给业务包用的方法解析器：读绑定、读技能、必要时兜底内置，并在首次兜底时写日志和审计。
// 营销等业务每次运行环节前调用 Resolver().Method(ctx, step)，再交给 stepkit.Runner 运行。
func (s *StepAdminService) Resolver() *stepkit.Resolver {
	r := stepkit.NewResolver(NewStepBindingStore(s.repo), s.skills)
	r.OnFallback = s.noteFallback
	return r
}

// noteFallback 记录环节退回内置方法：每种「环节 + 原因」在进程内只记一次，免得每次运行都刷审计。
// 审计的操作人为 0（系统）；后台页顶部的提示由列表里的 effective.fallback 驱动，不依赖审计。
func (s *StepAdminService) noteFallback(ctx context.Context, st stepkit.Step, b stepkit.Binding, reason string) {
	if _, seen := s.fallbackSeen.LoadOrStore(st.Key+"|"+reason, true); seen {
		return
	}
	logger.Warn("环节绑定的技能不可用，已退回内置方法", zap.String("step", st.Key), zap.String("skill", b.SkillName), zap.Int("version", b.SkillVersion), zap.String("reason", reason))
	adminAudit(ctx, s.audit, 0, model.AdminAuditStepFallback, model.AdminAuditTargetStep, 0,
		map[string]any{"step": st.Key, "skill": b.SkillName, "version": b.SkillVersion, "reason": reason})
}
