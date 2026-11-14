package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"go.uber.org/zap"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
	"video-canvas/internal/stepkit"
)

// stepTrialErrorRunes 是写进试跑 error 列的失败原因的最大字符数。
const stepTrialErrorRunes = 500

// trialOutcome 是一次试跑执行的结果，finishTrial 据此落库和结算。
type trialOutcome struct {
	status string // succeeded / failed
	passed bool   // 格式校验是否通过（仅 succeeded 有意义）
	errors []string
	flags  stepkit.Flags
	errMsg string // status=failed 时的原因
	res    *stepkit.Result
	snap   *provider.Snapshot
	charge bool // 上游确实被调用并产生了费用
}

// HandleStepTrial 执行一条试跑（River job 的入口）：读样例和候选方法 → 调模型 → 校验 → 落库并结算积分。
// 业务失败（模型不可用、技能已删、调用失败、输出不合格）都落成试跑结果并返回 nil，不让队列重试；
// 只有读库等基础设施错误才返回 error，由队列重试。试跑已结束或不存在时直接返回 nil（幂等）。
func (s *StepAdminService) HandleStepTrial(ctx context.Context, trialID uint64) error {
	// 1. 抢到 running 状态；已结束的任务（重复投递）直接忽略。
	t, err := s.repo.StartTrial(ctx, trialID)
	if errors.Is(err, repository.ErrNotFound) || errors.Is(err, repository.ErrStateConflict) {
		return nil
	}
	if err != nil {
		return err
	}
	ctx, logRun := logger.EnsureRun(ctx, logger.RunRef{Kind: "step_trial", ID: idcodec.Encode(t.ID), UserID: t.CreatedBy})
	logRun.Start(zap.String("step", t.StepKey), zap.String("sample", t.SampleKey), zap.String("model", t.TextModelID))
	ctx = logger.WithPurpose(ctx, "step_trial."+t.StepKey)
	// 2. 执行，得到结果；基础设施错误交给队列重试。
	out, err := s.executeTrial(ctx, t)
	if err != nil {
		return err
	}
	// 3. 落库并结算。
	if err := s.finishTrial(ctx, t, out); err != nil {
		return err
	}
	fields := []zap.Field{zap.String("step", t.StepKey), zap.Bool("passed", out.passed), zap.Bool("charged", out.charge)}
	if out.errMsg != "" {
		fields = append(fields, zap.String("error_message", out.errMsg))
	}
	if len(out.errors) > 0 {
		fields = append(fields, zap.Strings("validation_errors", out.errors))
	}
	logRun.End(out.status, fields...)
	return nil
}

// executeTrial 准备输入、方法和模型并运行环节，把各种结局归一成 trialOutcome。
func (s *StepAdminService) executeTrial(ctx context.Context, t *model.StepTrial) (*trialOutcome, error) {
	st, ok := s.reg.Get(t.StepKey)
	if !ok {
		return failedTrial("环节已不存在"), nil
	}
	sample, ok := st.Sample(t.SampleKey)
	if !ok {
		return failedTrial("试跑样例已不存在"), nil
	}
	snap, err := s.models.Snapshot(ctx, t.TextModelID)
	if errors.Is(err, provider.ErrModelUnavailable) {
		return failedTrial("所选文本模型已不可用"), nil
	}
	if err != nil {
		return nil, err
	}
	method, out, err := s.trialMethod(ctx, st, t)
	if err != nil || out != nil {
		return out, err
	}
	input, err := sample.Build(ctx)
	if err != nil {
		logger.Warn("构造试跑样例失败", zap.String("step", t.StepKey), zap.String("sample", t.SampleKey), zap.Error(err))
		return failedTrial("构造试跑样例失败"), nil
	}
	runCtx, cancel := context.WithTimeout(ctx, stepTrialTimeout)
	defer cancel()
	res, err := s.runner.Run(runCtx, st, method, snap, input)
	return classifyRun(res, snap, err), nil
}

// trialMethod 读取试跑候选的方法文本。技能已被停用或删除属于业务失败（outcome 非空），读库失败返回 error。
func (s *StepAdminService) trialMethod(ctx context.Context, st stepkit.Step, t *model.StepTrial) (stepkit.Method, *trialOutcome, error) {
	if t.Source != stepkit.SourceSkill || t.SkillID == nil || t.SkillVersion == nil {
		return stepkit.BuiltinMethod(st), nil, nil
	}
	ix, err := s.loadSkills(ctx)
	if err != nil {
		return stepkit.Method{}, nil, err
	}
	info, ok := ix.byID[*t.SkillID]
	if !ok {
		return stepkit.Method{}, failedTrial("候选技能已被删除"), nil
	}
	m, err := stepkit.SkillMethod(ctx, s.skills, st, info.Name, *t.SkillVersion)
	if errors.Is(err, stepkit.ErrSkillUnavailable) {
		return stepkit.Method{}, failedTrial("候选技能已停用或版本已被删除"), nil
	}
	return m, nil, err
}

// failedTrial 构造一个没有调用上游的失败结果（不收费）。
func failedTrial(msg string) *trialOutcome {
	return &trialOutcome{status: model.StepTrialFailed, errMsg: msg}
}

// classifyRun 把 Runner 的结局分成三类：通过；输出不合格（调用成功但格式不过，试跑成功 + passed=false）；
// 调用失败（状态 failed）。输出不合格时上游确实消耗了用量，要收费；调用失败只在已有用量时收费。
func classifyRun(res *stepkit.Result, snap *provider.Snapshot, err error) *trialOutcome {
	out := &trialOutcome{res: res, snap: snap}
	switch {
	case err == nil:
		out.status, out.passed, out.flags, out.charge = model.StepTrialSucceeded, true, res.Flags, true
	case errors.Is(err, stepkit.ErrOutputInvalid):
		var verr *stepkit.ValidationError
		if errors.As(err, &verr) {
			out.errors = verr.Errors
		}
		out.status, out.charge = model.StepTrialSucceeded, true
	default:
		out.status, out.errMsg = model.StepTrialFailed, clipTrialError(err)
		out.charge = res != nil && res.Usage.InputTokens+res.Usage.OutputTokens > 0
	}
	return out
}

// clipTrialError 取失败原因给管理员看，截断过长的上游错误文本。
func clipTrialError(err error) string {
	r := []rune(err.Error())
	if len(r) > stepTrialErrorRunes {
		return string(r[:stepTrialErrorRunes])
	}
	return string(r)
}

// finishTrial 在一个事务里写回结果并结算积分：成功按用量扣费、多冻结的退回，失败全额退回。
// 事务内重新加锁读试跑，已经结束就什么都不做，保证重复投递不会二次结算。
func (s *StepAdminService) finishTrial(ctx context.Context, t *model.StepTrial, out *trialOutcome) error {
	credits := 0
	if out.charge && out.res != nil && out.snap != nil {
		credits = min(max(stepkit.Cost(out.snap, out.res), 0), t.Frozen)
	}
	fields, err := trialFields(out, credits, s.now())
	if err != nil {
		return err
	}
	return s.repo.WithTx(ctx, func(tx repository.StepTx) error {
		cur, err := tx.Trial(ctx, t.ID, true)
		if err != nil {
			return err
		}
		if cur.Status == model.StepTrialSucceeded || cur.Status == model.StepTrialFailed {
			return nil
		}
		if err := settleTrialCredit(ctx, tx, cur, credits); err != nil {
			return err
		}
		return tx.UpdateTrial(ctx, t.ID, fields)
	})
}

// trialFields 构造要写回试跑行的列。
func trialFields(out *trialOutcome, credits int, now time.Time) (map[string]any, error) {
	errs, err := json.Marshal(nonNil(out.errors))
	if err != nil {
		return nil, err
	}
	flags, err := json.Marshal(nonNilFlags(out.flags))
	if err != nil {
		return nil, err
	}
	fields := map[string]any{"status": out.status, "passed": out.passed, "errors": errs, "flags": flags,
		"error": out.errMsg, "credits": credits, "finished_at": now}
	if out.res != nil {
		fields["attempts"] = out.res.Attempts
		fields["duration_ms"] = int(out.res.Duration.Milliseconds())
		fields["input_tokens"] = out.res.Usage.InputTokens
		fields["output_tokens"] = out.res.Usage.OutputTokens
		method, err := json.Marshal(out.res.Method)
		if err != nil {
			return nil, err
		}
		fields["method"] = method
		if len(out.res.Output) > 0 {
			fields["output"] = []byte(out.res.Output)
		}
	}
	return fields, nil
}

// nonNilFlags 让空的软问题序列化成 [] 。
func nonNilFlags(f stepkit.Flags) stepkit.Flags {
	if f == nil {
		return stepkit.Flags{}
	}
	return f
}

// settleTrialCredit 结算一条试跑的冻结：扣 credits，其余退回；两条流水（settle / refund）各最多一条，
// 以 settle 的 inserted 判断是否第一次结算，只有第一次才动账户，重复调用是空操作。
func settleTrialCredit(ctx context.Context, tx repository.StepTx, t *model.StepTrial, credits int) error {
	if t.Frozen <= 0 {
		return nil
	}
	if _, err := tx.LockCredit(ctx, t.CreatedBy); err != nil {
		return err
	}
	inserted, err := tx.InsertLedger(ctx, &model.CreditLedger{UserID: t.CreatedBy, StepTrialID: &t.ID, Type: model.LedgerSettle, Amount: credits})
	if err != nil {
		return err
	}
	if rest := t.Frozen - credits; rest > 0 {
		if _, err := tx.InsertLedger(ctx, &model.CreditLedger{UserID: t.CreatedBy, StepTrialID: &t.ID, Type: model.LedgerRefund, Amount: rest}); err != nil {
			return err
		}
	}
	if !inserted {
		return nil
	}
	return tx.AddCredit(ctx, t.CreatedBy, -credits, -t.Frozen)
}
