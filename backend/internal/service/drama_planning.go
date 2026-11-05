package service

import (
	"context"
	"encoding/json"
	"errors"

	"video-canvas/internal/drama"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/ws"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
)

// advancePlanningTx 只自动发起研读与规划；项目锁与任务终态在同一事务保证崩溃恢复。
func (s *DramaService) advancePlanningTx(ctx context.Context, tx repository.DramaTx, p *model.DramaProject, snap *provider.Snapshot) (*model.DramaJob, error) {
	if p.Phase != "running" {
		return nil, nil
	}
	jobs, err := tx.Jobs(ctx, p.UserID, p.ID, true)
	if err != nil {
		return nil, err
	}
	for _, j := range jobs {
		if pipelineJob(j) {
			return nil, nil
		}
	}
	step := "analyze"
	if p.SourceSummary != "" {
		if p.SourceKind == "script" {
			return nil, s.importScriptTx(ctx, tx, p)
		}
		if p.PlanCursor >= p.RangeEnd {
			return nil, setPlanningState(ctx, tx, p, "done", "", "")
		}
		step = "plan"
	}
	j, err := s.enqueue(ctx, tx, p, &model.DramaJobReq{Step: step}, snap, nil, 0, nil)
	if err == nil {
		return j, nil
	}
	var business *errcode.Error
	if !errors.As(err, &business) {
		return nil, err
	}
	return nil, setPlanningState(ctx, tx, p, "paused", planningPauseCode(business.Code), business.Msg)
}

// pipelineJob 对旧研读和大纲任务也按系统线处理，避免升级期间阻塞用户。
func pipelineJob(j model.DramaJob) bool {
	return j.Lane == "pipeline" || j.Step == "analyze" || j.Step == "plan" || j.Step == "outline"
}

// importScriptTx 原文即剧本，不调模型、不受规划范围限制，逐场完整导入。
func (s *DramaService) importScriptTx(ctx context.Context, tx repository.DramaTx, p *model.DramaProject) error {
	rows, err := tx.Units(ctx, p.UserID, p.ID, 1, 0)
	if err != nil {
		return err
	}
	existing, err := tx.Episodes(ctx, p.UserID, p.ID)
	if err != nil {
		return err
	}
	if len(existing) == 0 {
		for i, want := range drama.ScriptEpisodes(sourceUnits(rows), p.EpisodeSeconds) {
			ep := model.DramaEpisode{UserID: p.UserID, DramaID: p.ID, No: i + 1, Title: want.Title, ScriptMD: want.ScriptMD, ScriptStatus: "ready", UnitFrom: want.UnitFrom, UnitTo: want.UnitTo, Revision: 1}
			if err := tx.SaveEpisode(ctx, &ep, 0); err != nil {
				return err
			}
		}
	}
	fields := map[string]any{"phase": "done", "plan_cursor": p.UnitsTotal, "range_end": p.UnitsTotal, "status": "ready", "pause_code": "", "pause_reason": ""}
	if err := tx.UpdateProject(ctx, p.UserID, p.ID, p.Revision, fields); err != nil {
		return err
	}
	p.Phase = "done"
	p.PlanCursor = p.UnitsTotal
	p.RangeEnd = p.UnitsTotal
	p.Revision++
	return nil
}

// setPlanningState 更新规划阶段，不覆盖用户正在编辑的分集。
func setPlanningState(ctx context.Context, tx repository.DramaTx, p *model.DramaProject, phase, code, reason string) error {
	fields := map[string]any{"phase": phase, "pause_code": code, "pause_reason": drama.Clip(reason, 255)}
	if err := tx.UpdateProject(ctx, p.UserID, p.ID, p.Revision, fields); err != nil {
		return err
	}
	p.Phase = phase
	p.PauseCode = code
	p.PauseReason = drama.Clip(reason, 255)
	p.Revision++
	return nil
}

// planningPauseCode 为前端提供稳定的暂停分类，不泄露上游错误。
func planningPauseCode(code int) string {
	switch code {
	case errcode.ErrInsufficientCredits.Code:
		return "credits"
	case errcode.ErrDramaContext.Code:
		return "context"
	case errcode.ErrDramaCustomPrompt.Code:
		return "custom_prompt"
	}
	return "failed"
}

// Planning 改变规划控制状态；暂停只拦下一批，不取消已经入队的任务。
func (s *DramaService) Planning(ctx context.Context, userID, id uint64, action string, req *model.DramaPlanningReq) (*model.DramaView, error) {
	// 1. 模型解析放在项目事务外；暂停不应因模型停用而失败。
	p, err := s.repo.Project(ctx, userID, id, false)
	if err != nil {
		return nil, dramaError(err)
	}
	var snap *provider.Snapshot
	if action != "pause" {
		snap, err = s.resolvePlanningModel(ctx, p.TextModel)
		if err != nil {
			return nil, err
		}
	}
	var following *model.DramaJob
	// 2. 锁内检查阶段、范围和游标，重复操作返回明确的阶段冲突。
	err = s.repo.WithTx(ctx, func(tx repository.DramaTx) error {
		current, e := tx.Project(ctx, userID, id, true)
		if e != nil {
			return e
		}
		if e := s.changePlanningTx(ctx, tx, current, action, req); e != nil {
			return e
		}
		if action == "pause" {
			return nil
		}
		following, e = s.advancePlanningTx(ctx, tx, current, snap)
		return e
	})
	if err != nil {
		return nil, dramaError(err)
	}
	s.publish(ctx, following)
	s.publishDrama(ctx, userID, id)
	return s.Get(ctx, userID, id)
}

// EstimatePlanning 只报价尚未规划部分；研读完成后不重复计费。
func (s *DramaService) EstimatePlanning(ctx context.Context, userID, id uint64, req *model.DramaPlanningReq) (*model.DramaSourceEstimate, error) {
	// 1. 对继续范围按同一切分和模型预算报价。
	p, err := s.repo.Project(ctx, userID, id, false)
	if err != nil {
		return nil, dramaError(err)
	}
	end := req.RangeEnd
	if end == 0 {
		end = p.UnitsTotal
	}
	if end < 1 || end > p.UnitsTotal || end <= p.PlanCursor {
		return nil, errcode.ErrInvalidParams
	}
	rows, err := s.settings.Units(ctx, userID, id, p.PlanCursor+1, end)
	if err != nil {
		return nil, err
	}
	units := sourceUnits(rows)
	for i := range units {
		units[i].No = i + 1
	}
	return s.estimateSource(ctx, drama.PreflightResult{Text: drama.UnitSource(units), Units: units}, model.DramaSourceEstimateReq{SourceKind: p.SourceKind, EpisodeSeconds: p.EpisodeSeconds, TextModel: p.TextModel}, p.SourceSummary == "")
}

// publishDrama 提交后通知客户端重新读取阶段和新增分集。
func (s *DramaService) publishDrama(ctx context.Context, userID, id uint64) {
	if s.bc != nil {
		s.bc.Publish(ctx, ws.UserChannel(userID), ws.Message{Type: ws.TypeDrama, Data: map[string]any{"drama_id": idcodec.ID(id)}})
	}
}

// changePlanningTx 在持锁项目上应用控制动作，范围变更与快照切换都使用 revision。
func (s *DramaService) changePlanningTx(ctx context.Context, tx repository.DramaTx, p *model.DramaProject, action string, req *model.DramaPlanningReq) error {
	if action == "pause" {
		if p.Phase != "running" {
			return errcode.ErrDramaPhase
		}
		return setPlanningState(ctx, tx, p, "paused", "user", "用户暂停")
	}
	switch action {
	case "range":
		if err := expandPlanningRange(ctx, tx, p, req.RangeEnd); err != nil {
			return err
		}
	case "resume", "retry-builtin":
		if p.Phase != "paused" {
			return errcode.ErrDramaPhase
		}
	default:
		return errcode.ErrInvalidParams
	}
	if action == "retry-builtin" {
		if err := s.restorePlanningPrompt(ctx, tx, p, req.Step); err != nil {
			return err
		}
	}
	return setPlanningState(ctx, tx, p, "running", "", "")
}

// expandPlanningRange 只允许已完成小说扩大范围，零值归一到全部单元。
func expandPlanningRange(ctx context.Context, tx repository.DramaTx, p *model.DramaProject, end int) error {
	if end == 0 {
		end = p.UnitsTotal
	}
	if end < 1 || end > p.UnitsTotal {
		return errcode.ErrInvalidParams.WithMsg("规划范围超出原文单元")
	}
	if p.Phase != "done" || end <= p.RangeEnd || p.SourceKind == "script" {
		return errcode.ErrDramaPhase
	}
	if err := tx.UpdateProject(ctx, p.UserID, p.ID, p.Revision, map[string]any{"range_end": end}); err != nil {
		return err
	}
	p.RangeEnd = end
	p.Revision++
	return nil
}

// restorePlanningPrompt 仅在自定义改编正文失败后切换内置，不改动已入队任务。
func (s *DramaService) restorePlanningPrompt(ctx context.Context, tx repository.DramaTx, p *model.DramaProject, step string) error {
	if step != "adapt" || p.PauseCode != "custom_prompt" {
		return errcode.ErrDramaPhase
	}
	settings := promptSettings(p)
	builtin, err := s.snapshotPrompt(ctx, p.UserID, "adapt", model.DramaPromptChoice{Source: "builtin"})
	if err != nil {
		return err
	}
	settings["adapt"] = *builtin
	raw, _ := json.Marshal(settings)
	if err := tx.UpdateProject(ctx, p.UserID, p.ID, p.Revision, map[string]any{"prompt_settings": raw}); err != nil {
		return err
	}
	p.PromptSettings = raw
	p.Revision++
	return nil
}
