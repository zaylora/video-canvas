package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"video-canvas/internal/drama"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
)

// dramaRun 汇集一个步骤的持久化输入，不持有数据库事务跨网络请求。
type dramaRun struct {
	project  *model.DramaProject
	job      *model.DramaJob
	snapshot provider.Snapshot
	episodes []model.DramaEpisode
}

// dramaResult 保存步骤输出与实际费用，画布提交后可从checkpoint恢复。
type dramaResult struct {
	output    any
	cost      int
	next      string
	newAssets []DramaCanvasAsset
}

// dramaCheckpoint 是画布写入与后续业务结算之间的持久化交接。
type dramaCheckpoint struct {
	Result          json.RawMessage    `json:"result"`
	Cost            int                `json:"cost"`
	CanvasCommitted bool               `json:"canvas_committed"`
	NewAssets       []DramaCanvasAsset `json:"new_assets"`
}

// HandleDrama 执行一个短剧步骤；模型业务失败退款，数据库错误交给River恢复。
func (s *DramaService) HandleDrama(ctx context.Context, userID, dramaID, jobID uint64) error {
	// 1. 在项目锁内领取任务；终态重复投递不会再调用模型。
	run, err := s.startDrama(ctx, userID, dramaID, jobID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if run == nil {
		return nil
	}
	s.publish(ctx, run.job)
	// 2. 网络请求在事务外执行；有效输出写入持久checkpoint以支持崩溃恢复。
	if deadline := run.snapshot.Model.Deadline.D(); deadline > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, deadline)
		defer cancel()
	}
	result, err := s.executeDrama(ctx, run)
	if err != nil {
		return s.failDrama(ctx, run.job, err)
	}
	// 3. 输出落库、结算和自动续步一起提交。
	return s.commitDrama(ctx, run, result)
}

// startDrama 按project→job顺序加锁，保留首次开始时间。
func (s *DramaService) startDrama(ctx context.Context, userID, dramaID, jobID uint64) (*dramaRun, error) {
	run := &dramaRun{}
	err := s.repo.WithTx(ctx, func(tx repository.DramaTx) error {
		var e error
		run.project, e = tx.Project(ctx, userID, dramaID, true)
		if e != nil {
			return e
		}
		run.job, e = tx.Job(ctx, userID, dramaID, jobID, true)
		if e != nil {
			return e
		}
		if !dramaActive(run.job.Status) {
			return nil
		}
		if e := json.Unmarshal(run.job.ConfigSnapshot, &run.snapshot); e != nil {
			return e
		}
		run.episodes, e = tx.Episodes(ctx, userID, dramaID)
		if e != nil {
			return e
		}
		if run.job.StartedAt == nil {
			now := time.Now()
			run.job.StartedAt = &now
		}
		run.job.Status = "running"
		return tx.UpdateJob(ctx, userID, jobID, map[string]any{"status": "running", "started_at": *run.job.StartedAt})
	})
	if err != nil {
		return nil, err
	}
	if !dramaActive(run.job.Status) {
		return nil, nil
	}
	return run, nil
}

// executeDrama 按步骤运行固定流水线；画布已提交时只恢复结算数据。
func (s *DramaService) executeDrama(ctx context.Context, run *dramaRun) (*dramaResult, error) {
	if saved, ok, err := restoreDramaCheckpoint(run.job); ok || err != nil {
		return saved, err
	}
	vars := s.dramaVariables(run.project)
	selected := selectedEpisodes(run.episodes, run.job.Episodes)
	result := &dramaResult{}
	var err error
	switch run.job.Step {
	case "analyze":
		result.output, result.cost, err = s.runAnalyze(ctx, run.project, run.job, &run.snapshot)
		result.next = "outline"
	case "outline":
		result.output, result.cost, err = s.runOutline(ctx, run, vars)
	case "scripts":
		result.output, result.cost, err = s.runScripts(ctx, run.project, run.job, &run.snapshot, run.episodes, selected)
	case "breakdown":
		return s.runBreakdown(ctx, run, vars, selected)
	case "asset_prompts":
		return s.runAssetPrompts(ctx, run, vars)
	default:
		err = errcode.ErrInvalidParams.WithMsg("不支持的流水线步骤")
	}
	return result, err
}

// runOutline 有分集标记则直接整理原文，小说只把研读摘要交给大纲模型。
func (s *DramaService) runOutline(ctx context.Context, run *dramaRun, vars map[string]any) (any, int, error) {
	marked := dramaMarkedEpisodes(run.project.SourceText)
	if len(marked) > 0 {
		return drama.OutlineOutput{OutlineMD: run.project.SourceSummary, CharactersMD: run.project.CharactersMD, Episodes: marked}, 0, nil
	}
	return s.callDrama(ctx, run.job, &run.snapshot, "outline", vars, nil)
}

// runBreakdown 校验拆解范围，先持久化有效结果，再由画布事务锁集和创建资产。
func (s *DramaService) runBreakdown(ctx context.Context, run *dramaRun, vars map[string]any, selected []model.DramaEpisode) (*dramaResult, error) {
	vars["episodes"] = dramaScriptInputs(selected)
	out, cost, err := s.callDrama(ctx, run.job, &run.snapshot, "breakdown", vars, func(v any) error { return validateBreakdownScope(v.(drama.BreakdownOutput), run.job.Episodes) })
	if err != nil {
		return nil, err
	}
	result := &dramaResult{output: out, cost: cost}
	if err = s.prepareCanvasCheckpoint(ctx, run.job, result); err != nil {
		return nil, err
	}
	if s.canvas == nil {
		return nil, errcode.ErrInternal
	}
	result.newAssets, err = s.canvas.ApplyBreakdown(ctx, run.job.UserID, run.job.DramaID, run.project.CanvasID, run.job.ID, run.job.Episodes, out.(drama.BreakdownOutput).Assets)
	if err != nil {
		return nil, err
	}
	if len(result.newAssets) > 0 {
		result.next = "asset_prompts"
	}
	return result, nil
}

// runAssetPrompts 只为本次新资产写提示词，输出必须精确覆盖节点集合。
func (s *DramaService) runAssetPrompts(ctx context.Context, run *dramaRun, vars map[string]any) (*dramaResult, error) {
	var assets []DramaCanvasAsset
	if e := json.Unmarshal(run.job.InputJSON, &assets); e != nil || len(assets) == 0 {
		return nil, errcode.ErrDramaStepNotReady.WithMsg("没有待生成提示词的新资产")
	}
	vars["assets"] = assets
	out, cost, err := s.callDrama(ctx, run.job, &run.snapshot, "asset_prompts", vars, func(v any) error { return validatePromptCoverage(v.(drama.AssetPromptsOutput), assets) })
	if err != nil {
		return nil, err
	}
	result := &dramaResult{output: out, cost: cost}
	if err = s.prepareCanvasCheckpoint(ctx, run.job, result); err != nil {
		return nil, err
	}
	if s.canvas == nil {
		return nil, errcode.ErrInternal
	}
	if err = s.canvas.ApplyPrompts(ctx, run.job.UserID, run.project.CanvasID, run.job.ID, out.(drama.AssetPromptsOutput).Prompts); err != nil {
		return nil, err
	}
	return result, nil
}

// prepareCanvasCheckpoint 在网络结束后保存费用和结果，画布事务只追加提交标记。
func (s *DramaService) prepareCanvasCheckpoint(ctx context.Context, j *model.DramaJob, result *dramaResult) error {
	raw, err := json.Marshal(result.output)
	if err != nil {
		return err
	}
	checkpoint, err := json.Marshal(dramaCheckpoint{Result: raw, Cost: result.cost})
	if err != nil {
		return err
	}
	return s.repo.WithTx(ctx, func(tx repository.DramaTx) error {
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
		return tx.UpdateJob(ctx, j.UserID, j.ID, map[string]any{"output": checkpoint})
	})
}

// restoreDramaCheckpoint 已提交画布不可重复写入；恢复费用、新资产和下一步。
func restoreDramaCheckpoint(j *model.DramaJob) (*dramaResult, bool, error) {
	var checkpoint dramaCheckpoint
	if err := json.Unmarshal(j.Output, &checkpoint); err != nil || !checkpoint.CanvasCommitted {
		return nil, false, nil
	}
	out, err := drama.DecodeOutput(j.Step, checkpoint.Result)
	if err != nil {
		return nil, true, errcode.ErrDramaOutputInvalid
	}
	result := &dramaResult{output: out, cost: checkpoint.Cost, newAssets: checkpoint.NewAssets}
	if j.Step == "breakdown" && len(result.newAssets) > 0 {
		result.next = "asset_prompts"
	}
	return result, true, nil
}

// dramaCanvasCommitted 判断取消是否还来得及，画布提交后必须完成结算。
func dramaCanvasCommitted(j *model.DramaJob) bool {
	var checkpoint dramaCheckpoint
	return json.Unmarshal(j.Output, &checkpoint) == nil && checkpoint.CanvasCommitted
}

// commitDrama 输出、结算和自动后继任务在同一事务提交。
func (s *DramaService) commitDrama(ctx context.Context, run *dramaRun, result *dramaResult) error {
	var completed, following *model.DramaJob
	err := s.repo.WithTx(ctx, func(tx repository.DramaTx) error {
		p, e := tx.Project(ctx, run.job.UserID, run.job.DramaID, true)
		if e != nil {
			return e
		}
		current, e := tx.Job(ctx, run.job.UserID, run.job.DramaID, run.job.ID, true)
		if e != nil {
			return e
		}
		completed = current
		if !dramaActive(current.Status) {
			return nil
		}
		if e = s.applyDramaOutput(ctx, tx, p, current, result.output); e != nil {
			return e
		}
		audit := result.output
		if current.Step == "breakdown" || current.Step == "asset_prompts" {
			raw, e := json.Marshal(result.output)
			if e != nil {
				return e
			}
			audit = dramaCheckpoint{Result: raw, Cost: result.cost, CanvasCommitted: true, NewAssets: result.newAssets}
		}
		if e = s.finishTx(ctx, tx, current, "succeeded", 0, "", result.cost, audit); e != nil {
			return e
		}
		following, e = s.continueDrama(ctx, tx, p, current, result, &run.snapshot)
		return e
	})
	if err != nil {
		return err
	}
	s.publish(ctx, completed)
	s.publish(ctx, following)
	return nil
}

// continueDrama 自动接续大纲或新资产提示词，复用本步骤模型快照。
func (s *DramaService) continueDrama(ctx context.Context, tx repository.DramaTx, p *model.DramaProject, current *model.DramaJob, result *dramaResult, snap *provider.Snapshot) (*model.DramaJob, error) {
	if result.next == "" {
		return nil, nil
	}
	req := &model.DramaJobReq{Step: result.next}
	var selected []model.DramaEpisode
	if result.next == "asset_prompts" {
		eps, e := tx.Episodes(ctx, p.UserID, p.ID)
		if e != nil {
			return nil, e
		}
		selected = selectedEpisodes(eps, current.Episodes)
	}
	following, e := s.enqueue(ctx, tx, p, req, snap, selected, 0)
	if e != nil {
		return nil, e
	}
	return following, nil
}
