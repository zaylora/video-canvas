package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"video-canvas/internal/drama"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
)

// applyDramaOutput 写回已经通过结构校验的项目和集数据。
func (s *DramaService) applyDramaOutput(ctx context.Context, tx repository.DramaTx, p *model.DramaProject, j *model.DramaJob, out any) error {
	fields := map[string]any{}
	switch result := out.(type) {
	case drama.StoryboardOutput:
		if err := saveStoryboardOutput(ctx, tx, p, j, result); err != nil {
			return err
		}
		p.Status = "ready"
		fields["status"] = "ready"
	case drama.AnalyzeOutput:
		p.SourceKind = result.SourceKind
		p.SourceSummary = result.Summary
		p.CharactersMD = result.CharactersMD
		fields = map[string]any{"source_kind": result.SourceKind, "source_summary": result.Summary, "characters_md": result.CharactersMD}
	case drama.OutlineOutput:
		ready, e := saveDramaOutline(ctx, tx, p, result)
		if e != nil {
			return e
		}
		p.OutlineMD = result.OutlineMD
		p.CharactersMD = result.CharactersMD
		p.Status = "outlined"
		if ready {
			p.Status = "ready"
		}
		fields = map[string]any{"outline_md": result.OutlineMD, "characters_md": result.CharactersMD, "status": p.Status}
	}
	if j.Step == "scripts" {
		p.Status = "ready"
		fields["status"] = "ready"
	}
	if len(fields) == 0 {
		return nil
	}
	if e := tx.UpdateProject(ctx, p.UserID, p.ID, p.Revision, fields); e != nil {
		return e
	}
	p.Revision++
	return nil
}

// saveDramaOutline 将分集大纲映射到已有集，保留已锁定剧本。
func saveDramaOutline(ctx context.Context, tx repository.DramaTx, p *model.DramaProject, result drama.OutlineOutput) (bool, error) {
	existing, e := tx.Episodes(ctx, p.UserID, p.ID)
	if e != nil {
		return false, e
	}
	ready := true
	for _, want := range result.Episodes {
		ep := model.DramaEpisode{UserID: p.UserID, DramaID: p.ID, No: want.No, Revision: 1, ScriptStatus: "empty"}
		for _, old := range existing {
			if old.No == want.No {
				ep = old
				break
			}
		}
		if ep.LockedAt != nil {
			continue
		}
		revision := ep.Revision
		ep.Title = want.Title
		ep.Synopsis = want.Synopsis
		if want.ScriptMD != "" {
			ep.ScriptMD = want.ScriptMD
			ep.ScriptStatus = "ready"
		}
		if ep.ScriptStatus != "ready" {
			ready = false
		}
		if e := tx.SaveEpisode(ctx, &ep, revision); e != nil {
			return false, e
		}
	}
	return ready, nil
}

// finishTx 任务终态与积分结算同事务，失败释放冻结额且不扣费。
func (s *DramaService) finishTx(ctx context.Context, tx repository.DramaTx, j *model.DramaJob, status string, code int, message string, cost int, output any) error {
	if !dramaActive(j.Status) {
		return nil
	}
	if status != "succeeded" {
		cost = 0
	}
	cost = min(max(cost, 0), j.FrozenCredits)
	if e := finishStoryboardStatus(ctx, tx, j, status); e != nil {
		return e
	}
	if e := settleDramaCredit(ctx, tx, j, status, cost); e != nil {
		return e
	}
	now := time.Now()
	fields := map[string]any{"status": status, "credits": cost, "error_code": code, "error_message": message, "finished_at": now}
	if status == "succeeded" {
		fields["progress"] = 100
		j.Progress = 100
	}
	if output != nil {
		raw, e := json.Marshal(output)
		if e != nil {
			return e
		}
		fields["output"] = raw
		j.Output = raw
	}
	if e := tx.UpdateJob(ctx, j.UserID, j.ID, fields); e != nil {
		return e
	}
	j.Status = status
	j.Credits = cost
	j.ErrorCode = code
	j.ErrorMessage = message
	j.FinishedAt = &now
	return nil
}

// settleDramaCredit 保持freeze = settle + refund，和现有积分对账语义一致。
func settleDramaCredit(ctx context.Context, tx repository.DramaTx, j *model.DramaJob, status string, cost int) error {
	if j.Step == "compose" {
		return nil
	}
	if _, e := tx.LockCredit(ctx, j.UserID); e != nil {
		return e
	}
	kind, amount := model.LedgerRefund, j.FrozenCredits
	if status == "succeeded" {
		kind, amount = model.LedgerSettle, cost
	}
	inserted, e := tx.InsertLedger(ctx, &model.CreditLedger{UserID: j.UserID, DramaJobID: &j.ID, Type: kind, Amount: amount})
	if e != nil {
		return e
	}
	if status == "succeeded" && j.FrozenCredits > cost {
		if _, e := tx.InsertLedger(ctx, &model.CreditLedger{UserID: j.UserID, DramaJobID: &j.ID, Type: model.LedgerRefund, Amount: j.FrozenCredits - cost}); e != nil {
			return e
		}
	}
	if !inserted {
		return nil
	}
	return tx.AddCredit(ctx, j.UserID, -cost, -j.FrozenCredits)
}

// failDrama 业务失败原子退款；画布已提交则留给队列恢复结算。
func (s *DramaService) failDrama(ctx context.Context, j *model.DramaJob, cause error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	code, message := dramaFailure(cause)
	err := s.repo.WithTx(ctx, func(tx repository.DramaTx) error {
		p, e := tx.Project(ctx, j.UserID, j.DramaID, true)
		if e != nil {
			return e
		}
		current, e := tx.Job(ctx, j.UserID, j.DramaID, j.ID, true)
		if e != nil {
			return e
		}
		if !dramaActive(current.Status) {
			j = current
			return nil
		}
		if dramaCanvasCommitted(current) {
			return cause
		}
		if e := s.finishTx(ctx, tx, current, "failed", code, message, 0, nil); e != nil {
			return e
		}
		j = current
		return tx.UpdateProject(ctx, p.UserID, p.ID, p.Revision, map[string]any{"status": "failed"})
	})
	if errors.Is(err, repository.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	s.publish(ctx, j)
	return nil
}

// dramaProgress 每增加10%持久化并推送，取消后不再变更状态。
func (s *DramaService) dramaProgress(ctx context.Context, j *model.DramaJob, progress int) error {
	progress = min(90, progress/10*10)
	if progress <= j.Progress {
		return nil
	}
	err := s.repo.WithTx(ctx, func(tx repository.DramaTx) error {
		if _, e := tx.Project(ctx, j.UserID, j.DramaID, true); e != nil {
			return e
		}
		current, e := tx.Job(ctx, j.UserID, j.DramaID, j.ID, true)
		if e != nil {
			return e
		}
		if !dramaActive(current.Status) {
			return errcode.ErrTaskNotCancelable
		}
		return tx.UpdateJob(ctx, j.UserID, j.ID, map[string]any{"progress": progress})
	})
	if err != nil {
		return err
	}
	j.Progress = progress
	s.publish(ctx, j)
	return nil
}
