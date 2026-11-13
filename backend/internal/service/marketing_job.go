package service

import (
	"context"
	"encoding/json"
	"errors"
	"runtime/debug"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"

	"video-canvas/internal/marketing"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
	"video-canvas/internal/stepkit"
)

// marketingErrRunes 是写进环节运行 error 列的原因最大字符数。
const marketingErrRunes = 300

// stepOutcome 是一个环节运行的结局，finishRun 据此落库、写脚本和结算。
type stepOutcome struct {
	status   string
	errMsg   string
	res      *stepkit.Result
	snap     *provider.Snapshot
	method   stepkit.MethodSnapshot
	model    string
	warnings []string
	basis    []byte // follow 据以写成的拆解（JSON）
	runID    uint64 // 产出脚本所属的环节运行
	apply    func(ctx context.Context, tx repository.MarketingTx, round *model.MarketingRound) error
	charge   bool // 上游确实被调用并产生了费用
}

// HandleMarketingRound 是 River job 的入口：按依赖分两层执行轮次里排队中的环节（第一层 facts、reverse，第二层 follow、create），
// 环节失败互不拖累；全部结束后定轮次终态。业务失败都落成环节运行的失败，只有基础设施错误才返回 error（队列重试）。
func (s *MarketingService) HandleMarketingRound(ctx context.Context, roundID uint64) error {
	for {
		round, err := s.repo.RoundByID(ctx, roundID, false)
		if mktNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if round.Status != model.MarketingRoundScripting {
			return nil
		}
		runs, err := s.repo.RoundRuns(ctx, roundID)
		if err != nil {
			return err
		}
		layer := pendingLayer(latestRuns(runs))
		if len(layer) == 0 {
			// 定终态后再回头看一眼：job 收尾期间若有新的重试入队被去重跳过，这里接着跑。
			if _, err := s.finalizeRound(ctx, roundID); err != nil {
				return err
			}
			continue
		}
		s.runLayer(ctx, round, layer)
		s.pushRound(ctx, round.UserID, roundID)
	}
}

// pendingLayer 返回下一层要执行的运行：先第一层（facts、reverse）的排队 / 进行中运行，没有再取第二层（follow、create）。
// 进行中（running）也算：job 被救回重试时接着跑。
func pendingLayer(latest []model.MarketingStepRun) []model.MarketingStepRun {
	var first, second []model.MarketingStepRun
	for _, r := range latest {
		if r.Status != model.MarketingRunQueued && r.Status != model.MarketingRunRunning {
			continue
		}
		if r.StepKey == marketing.StepFacts || r.StepKey == marketing.StepReverse {
			first = append(first, r)
		} else {
			second = append(second, r)
		}
	}
	if len(first) > 0 {
		return first
	}
	return second
}

// runLayer 并发执行一层运行，等全部结束。
func (s *MarketingService) runLayer(ctx context.Context, round *model.MarketingRound, layer []model.MarketingStepRun) {
	var wg sync.WaitGroup
	for i := range layer {
		wg.Add(1)
		go func(run model.MarketingStepRun) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					logger.Error("营销环节运行 panic", zap.Uint64("run_id", run.ID), zap.Any("panic", r), zap.ByteString("stack", debug.Stack()))
					s.failAfterPanic(ctx, round, &run)
				}
			}()
			s.execStep(ctx, round, &run)
		}(layer[i])
	}
	wg.Wait()
}

// failAfterPanic 环节运行出现未预期的 panic 时把它落成失败，免得队列无限重试同一个坏输入。
func (s *MarketingService) failAfterPanic(ctx context.Context, round *model.MarketingRound, run *model.MarketingStepRun) {
	out := &stepOutcome{status: model.MarketingRunFailed, errMsg: "写脚本时出现内部错误，请重试"}
	if err := s.finishRun(context.WithoutCancel(ctx), round, run, out); err != nil {
		logger.Error("营销环节运行失败落库失败", zap.Uint64("run_id", run.ID), zap.Error(err))
	}
}

// execStep 执行一个环节运行：置为 running → 准备输入并调模型 → 落库、写脚本、结算。
func (s *MarketingService) execStep(ctx context.Context, round *model.MarketingRound, run *model.MarketingStepRun) {
	err := s.repo.WithTx(ctx, func(tx repository.MarketingTx) error {
		return tx.UpdateRunIf(ctx, run.ID, []string{model.MarketingRunQueued, model.MarketingRunRunning},
			map[string]any{"status": model.MarketingRunRunning, "started_at": s.now()})
	})
	if err != nil {
		if !errorsIsConflict(err) {
			logger.Warn("营销环节置为进行中失败", zap.Uint64("run_id", run.ID), zap.Error(err))
		}
		return
	}
	s.pushRound(ctx, round.UserID, round.ID)
	out := s.produce(ctx, round, run)
	if err := s.finishRun(context.WithoutCancel(ctx), round, run, out); err != nil {
		logger.Error("营销环节运行落库失败", zap.Uint64("run_id", run.ID), zap.Error(err))
	}
}

// produce 跑一个环节，把各种结局归一成 stepOutcome。
func (s *MarketingService) produce(ctx context.Context, round *model.MarketingRound, run *model.MarketingStepRun) *stepOutcome {
	step, ok := s.steps.Get(run.StepKey)
	if !ok {
		return failedOutcome("环节已不存在")
	}
	settings, err := settingsOf(round)
	if err != nil {
		logger.Error("解码轮次设置失败", zap.Uint64("round_id", round.ID), zap.Error(err))
		return failedOutcome("写脚本失败，请重试")
	}
	project, err := s.repo.ProjectByID(ctx, round.ProjectID)
	if err != nil {
		return failedOutcome("项目已不存在")
	}
	// 依赖检查：缺上游就跳过，不调模型也不收费。
	if skip := s.dependencyGap(run.StepKey, project, round); skip != "" {
		return &stepOutcome{status: model.MarketingRunSkipped, errMsg: skip}
	}
	method, err := s.resolver.Method(ctx, step)
	if err != nil {
		logger.Error("读取环节方法失败", zap.String("step", run.StepKey), zap.Error(err))
		return failedOutcome("读取环节方法失败，请重试")
	}
	snap, err := s.pickTextModel(ctx, settings.TextModel, needOf(run.StepKey))
	if err != nil {
		return failedOutcome("没有可用的文本模型")
	}
	prep, err := s.prepareInput(ctx, round, project, settings, run.StepKey, snap)
	if err != nil {
		return &stepOutcome{status: model.MarketingRunFailed, errMsg: userMessage(err), model: snap.Model.Key, method: method.Snapshot()}
	}
	runCtx, cancel := context.WithTimeout(ctx, marketingStepTimeout)
	defer cancel()
	stopWatch := s.watchCancel(runCtx, cancel, round.ID)
	res, err := s.runner.Run(runCtx, step, method, snap, prep.input)
	stopWatch()
	out := s.classify(ctx, round, project, settings, run.StepKey, snap, res, err)
	out.model, out.warnings, out.basis, out.runID = snap.Model.Key, prep.warnings, prep.basis, run.ID
	if out.method.SHA256 == "" {
		out.method = method.Snapshot()
	}
	return out
}

// needOf 返回环节对文本模型的能力要求。
func needOf(stepKey string) textNeed {
	switch stepKey {
	case marketing.StepFacts:
		return needVision
	case marketing.StepReverse:
		return needVideo
	}
	return needAny
}

// failedOutcome 构造一个没有调用上游的失败结局（不收费）。
func failedOutcome(msg string) *stepOutcome {
	return &stepOutcome{status: model.MarketingRunFailed, errMsg: msg}
}

// dependencyGap 检查环节需要的上游：缺了返回跳过原因，齐了返回空串。
func (s *MarketingService) dependencyGap(stepKey string, p *model.MarketingProject, round *model.MarketingRound) string {
	switch stepKey {
	case marketing.StepFollow:
		if len(round.Reverse) == 0 {
			return "参考片拆解没有成功，跟拍版已跳过"
		}
		fallthrough
	case marketing.StepCreate:
		if len(p.Product) == 0 {
			return "商品事实卡没有成功，已跳过"
		}
	}
	return ""
}

// watchCancel 在环节运行期间每隔几秒查一次轮次是否被取消，取消了就中断这次调用。返回停止函数。
func (s *MarketingService) watchCancel(ctx context.Context, cancel context.CancelFunc, roundID uint64) func() {
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(marketingCancelPoll)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				r, err := s.repo.RoundByID(ctx, roundID, false)
				if err == nil && r.Status == model.MarketingRoundCanceled {
					cancel()
					return
				}
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(done) }) }
}

// classify 把 Runner 的结局转成环节结局：成功时解码输出并准备落库动作。
func (s *MarketingService) classify(ctx context.Context, round *model.MarketingRound, project *model.MarketingProject, st model.MarketingSettings,
	stepKey string, snap *provider.Snapshot, res *stepkit.Result, err error) *stepOutcome {
	out := &stepOutcome{res: res, snap: snap}
	switch {
	case err == nil:
		out.charge = true
		if applyErr := s.prepareApply(out, round, project, st, stepKey, res); applyErr != nil {
			logger.Error("解码环节输出失败", zap.String("step", stepKey), zap.Error(applyErr))
			out.status, out.errMsg = model.MarketingRunFailed, "模型输出无法解析，请重试"
			return out
		}
		out.status = model.MarketingRunSucceeded
	case ctx.Err() != nil || errors.Is(err, context.Canceled):
		out.status, out.charge = model.MarketingRunCanceled, res != nil && res.Usage.InputTokens+res.Usage.OutputTokens > 0
	case errors.Is(err, stepkit.ErrOutputInvalid):
		out.status, out.charge = model.MarketingRunFailed, true
		var verr *stepkit.ValidationError
		if errors.As(err, &verr) {
			out.errMsg = clipRunes("模型输出没有通过校验："+strings.Join(verr.Errors, "；"), marketingErrRunes)
		} else {
			out.errMsg = "模型输出没有通过校验，请重试"
		}
	case errors.Is(err, stepkit.ErrModelCapability):
		out.status, out.errMsg = model.MarketingRunFailed, "所选文本模型不能处理这个环节，请换一个模型"
	case errors.Is(err, stepkit.ErrCallFailed):
		logger.Warn("营销环节模型调用失败", zap.String("step", stepKey), zap.Error(err))
		out.status, out.errMsg = model.MarketingRunFailed, "文本模型暂时不可用，请稍后重试"
		out.charge = res != nil && res.Usage.InputTokens+res.Usage.OutputTokens > 0
	default:
		logger.Error("营销环节运行出错", zap.String("step", stepKey), zap.Error(err))
		out.status, out.errMsg = model.MarketingRunFailed, "写脚本失败，请重试"
	}
	return out
}

// clipRunes 按字符数截断。
func clipRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// userMessage 把准备输入时的错误转成给用户看的原因：业务错误原文，其他统一文案。
func userMessage(err error) string {
	var pe *prepareError
	if errors.As(err, &pe) {
		return pe.msg
	}
	logger.Warn("准备环节输入失败", zap.Error(err))
	return "读取素材失败，请重试"
}

// finishRun 在一个事务里落下环节结局：更新运行记录、写结果（事实卡 / 拆解 / 脚本）、结算积分。
// 轮次已被取消时成功的结果不写入，运行记为取消；已被别处结算的运行（状态迁移冲突）什么都不做。
func (s *MarketingService) finishRun(ctx context.Context, round *model.MarketingRound, run *model.MarketingStepRun, out *stepOutcome) error {
	charged := 0
	if out.charge && out.res != nil && out.snap != nil {
		charged = max(stepkit.Cost(out.snap, out.res), 0)
	}
	return s.repo.WithTx(ctx, func(tx repository.MarketingTx) error {
		cur, err := tx.RoundByID(ctx, round.ID, true)
		if err != nil {
			return err
		}
		status, msg := out.status, out.errMsg
		if cur.Status == model.MarketingRoundCanceled && status == model.MarketingRunSucceeded {
			status, msg = model.MarketingRunCanceled, ""
		}
		if err := s.closeOutcome(ctx, tx, run, out, status, msg); err != nil {
			if errorsIsConflict(err) {
				return nil
			}
			return err
		}
		if err := s.applyOutcome(ctx, tx, cur, run, out, status); err != nil {
			return err
		}
		return s.settleRun(ctx, tx, run, charged)
	})
}

// closeOutcome 把环节运行的终态写回运行记录；运行已被别处结算时返回 repository.ErrStateConflict。
func (s *MarketingService) closeOutcome(ctx context.Context, tx repository.MarketingTx, run *model.MarketingStepRun, out *stepOutcome, status, msg string) error {
	method, err := json.Marshal(out.method)
	if err != nil {
		return err
	}
	fields := map[string]any{"status": status, "error": msg, "finished_at": s.now(), "method": method}
	if out.model != "" {
		fields["text_model_id"] = out.model
	}
	if out.res != nil {
		fields["attempts"] = out.res.Attempts
	}
	return tx.UpdateRunIf(ctx, run.ID, []string{model.MarketingRunQueued, model.MarketingRunRunning}, fields)
}

// applyOutcome 成功时写结果（事实卡 / 拆解 / 脚本）和警告；跟拍没成功时把「重写中」的脚本放回草稿。
func (s *MarketingService) applyOutcome(ctx context.Context, tx repository.MarketingTx, cur *model.MarketingRound, run *model.MarketingStepRun, out *stepOutcome, status string) error {
	if status == model.MarketingRunSucceeded && out.apply != nil {
		if err := out.apply(ctx, tx, cur); err != nil {
			return err
		}
		return s.addWarnings(ctx, tx, cur, out.warnings)
	}
	if status != model.MarketingRunSucceeded && run.StepKey == marketing.StepFollow {
		return s.releaseRewriting(ctx, tx, cur.ID)
	}
	return nil
}

// addWarnings 把警告追加到轮次（去重）。
func (s *MarketingService) addWarnings(ctx context.Context, tx repository.MarketingTx, round *model.MarketingRound, add []string) error {
	if len(add) == 0 {
		return nil
	}
	var cur []string
	_ = json.Unmarshal(round.Warnings, &cur)
	for _, w := range add {
		found := false
		for _, c := range cur {
			found = found || c == w
		}
		if !found {
			cur = append(cur, w)
		}
	}
	raw, err := json.Marshal(cur)
	if err != nil {
		return err
	}
	return tx.UpdateRound(ctx, round.ID, map[string]any{"warnings": raw})
}

// finalizeRound 在所有环节都结束后定轮次终态：有脚本 ready，否则 failed 并带第一个失败环节的原因。
// 还有排队中或进行中的运行（并发重试新建的）时返回 false，调用方继续执行。
func (s *MarketingService) finalizeRound(ctx context.Context, roundID uint64) (bool, error) {
	final := false
	err := s.repo.WithTx(ctx, func(tx repository.MarketingTx) error {
		r, err := tx.RoundByID(ctx, roundID, true)
		if err != nil {
			return err
		}
		if r.Status != model.MarketingRoundScripting {
			final = true
			return nil
		}
		runs, err := tx.RoundRuns(ctx, roundID)
		if err != nil {
			return err
		}
		latest := latestRuns(runs)
		if len(pendingLayer(latest)) > 0 {
			return nil
		}
		scripts, err := tx.RoundScripts(ctx, roundID)
		if err != nil {
			return err
		}
		final = true
		fields := map[string]any{"status": model.MarketingRoundReady, "error": ""}
		if len(scripts) == 0 {
			fields["status"], fields["error"] = model.MarketingRoundFailed, roundFailure(latest)
		}
		return tx.UpdateRound(ctx, roundID, fields)
	})
	if err != nil {
		return false, err
	}
	if final {
		if r, err := s.repo.RoundByID(ctx, roundID, false); err == nil {
			s.pushRound(ctx, r.UserID, roundID)
		}
	}
	return final, nil
}

// roundFailure 取轮次失败的原因：事实卡优先，再按环节顺序取第一个失败的；一定带原因。
func roundFailure(latest []model.MarketingStepRun) string {
	order := []string{marketing.StepFacts, marketing.StepReverse, marketing.StepFollow, marketing.StepCreate}
	for _, k := range order {
		for _, r := range latest {
			if r.StepKey == k && r.Status == model.MarketingRunFailed && r.Error != "" {
				return r.Error
			}
		}
	}
	return "写脚本失败，请重试"
}

// prepareApply 解码成功的输出并准备落库动作（写事实卡 / 拆解 / 脚本）。
func (s *MarketingService) prepareApply(out *stepOutcome, round *model.MarketingRound, project *model.MarketingProject, st model.MarketingSettings,
	stepKey string, res *stepkit.Result) error {
	switch stepKey {
	case marketing.StepFacts:
		facts, err := marketing.DecodeFacts(res.Output)
		if err != nil {
			return err
		}
		out.apply = func(ctx context.Context, tx repository.MarketingTx, _ *model.MarketingRound) error {
			raw, err := json.Marshal(facts)
			if err != nil {
				return err
			}
			fields := map[string]any{"product": raw}
			if p, err := tx.ProjectByID(ctx, project.ID); err == nil && (p.Title == "" || p.Title == marketingProjectPlaceholder) && facts.Name != "" {
				fields["title"] = clipRunes(facts.Name, 120)
			}
			return tx.UpdateProject(ctx, project.ID, fields)
		}
	case marketing.StepReverse:
		rev, err := marketing.DecodeReverse(res.Output)
		if err != nil {
			return err
		}
		out.apply = func(ctx context.Context, tx repository.MarketingTx, cur *model.MarketingRound) error {
			raw, err := json.Marshal(rev)
			if err != nil {
				return err
			}
			return tx.UpdateRound(ctx, cur.ID, map[string]any{"reverse": raw, "reverse_original": raw, "reverse_revision": repository.Add("reverse_revision", 1)})
		}
	case marketing.StepFollow:
		fo, err := marketing.DecodeFollow(res.Output)
		if err != nil {
			return err
		}
		out.apply = s.applyFollow(fo, project, st, out)
	case marketing.StepCreate:
		co, err := marketing.DecodeCreate(res.Output)
		if err != nil {
			return err
		}
		out.apply = s.applyCreate(co, project, st, out)
	}
	return nil
}

// factsOf 解码项目的事实卡；没有时返回零值。
func factsOf(p *model.MarketingProject) marketing.ProductFacts {
	var f marketing.ProductFacts
	if len(p.Product) > 0 {
		_ = json.Unmarshal(p.Product, &f)
	}
	return f
}
