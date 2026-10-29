package service

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"video-canvas/internal/drama"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
)

// dramaStoryboardInput 持久化视频模型时长上限，任务执行不能依赖后续请求配置。
type dramaStoryboardInput struct {
	MaxSeconds int `json:"max_seconds"`
}

// dramaStoryboardAsset 仅暴露分镜所需的画布资产信息，不发送其他集资产或图片数据。
type dramaStoryboardAsset struct {
	NodeID        string `json:"node_id"`
	Type          string `json:"type"`
	Name          string `json:"name"`
	Look          string `json:"look"`
	PromptSummary string `json:"prompt_summary"`
}

// checkStoryboardRequest 单集拆分必须显式选择范围；缺省时长遵循契约15秒。
func checkStoryboardRequest(req *model.DramaJobReq) error {
	if req.Step != "storyboard" {
		return nil
	}
	if len(req.Episodes) != 1 {
		return errcode.ErrInvalidParams.WithMsg("分镜须且只能选择一集")
	}
	if req.MaxSeconds != 0 && (req.MaxSeconds < 4 || req.MaxSeconds > 60) {
		return errcode.ErrInvalidParams.WithMsg("片段时长上限须为4至60秒")
	}
	return nil
}

// checkStoryboardTakes 与片段生成共用project行锁，阻止提交与最终替换覆盖并发产生的take。
func checkStoryboardTakes(ctx context.Context, tx repository.DramaTx, userID uint64, selected []model.DramaEpisode) error {
	for _, ep := range selected {
		has, err := tx.HasSegmentTakes(ctx, userID, ep.ID)
		if err != nil {
			return err
		}
		if has {
			return errcode.ErrDramaStoryboardHasTakes
		}
	}
	return nil
}

// estimateStoryboard 在项目锁下使用与提交一致的前置与take校验，不冻结积分。
func (s *DramaService) estimateStoryboard(ctx context.Context, userID, id uint64, req *model.DramaJobReq) (int, error) {
	var project *model.DramaProject
	var selected []model.DramaEpisode
	err := s.repo.WithTx(ctx, func(tx repository.DramaTx) error {
		var err error
		project, err = tx.Project(ctx, userID, id, true)
		if err != nil {
			return err
		}
		eps, err := tx.Episodes(ctx, userID, id)
		if err != nil {
			return err
		}
		selected, err = selectDramaEpisodes(project, eps, req)
		if err != nil {
			return err
		}
		return checkStoryboardTakes(ctx, tx, userID, selected)
	})
	if err != nil {
		return 0, dramaError(err)
	}
	snap, err := s.resolveModel(ctx, project.TextModel)
	if err != nil {
		return 0, err
	}
	return dramaQuote(project, req.Step, selected, snap), nil
}

// enqueueStoryboard 保存不可变输入并标记generating，随冻结和队列插入一起回滚。
func enqueueStoryboard(ctx context.Context, tx repository.DramaTx, j *model.DramaJob, req *model.DramaJobReq, eps []model.DramaEpisode) error {
	limit := req.MaxSeconds
	if limit == 0 {
		limit = 15
	}
	input, err := json.Marshal(dramaStoryboardInput{MaxSeconds: limit})
	if err != nil {
		return err
	}
	j.InputJSON = input
	for _, ep := range eps {
		ep.StoryboardStatus = "generating"
		if err = tx.SaveEpisode(ctx, &ep, ep.Revision); err != nil {
			return err
		}
	}
	return nil
}

// runStoryboard 读取本集可用资产后复用一期JSON重试，仅在全部输出合法时准备替换。
func (s *DramaService) runStoryboard(ctx context.Context, run *dramaRun, vars map[string]any, selected []model.DramaEpisode) (*dramaResult, error) {
	if len(selected) != 1 {
		return nil, errcode.ErrDramaStepNotReady
	}
	ep := selected[0]
	if err := checkDramaEpisodeStep(ep, "storyboard"); err != nil {
		return nil, err
	}
	var input dramaStoryboardInput
	if err := json.Unmarshal(run.job.InputJSON, &input); err != nil {
		return nil, err
	}
	if input.MaxSeconds < 4 || input.MaxSeconds > 60 {
		return nil, errcode.ErrDramaStepNotReady
	}
	assets, err := s.storyboardAssets(ctx, run.project, ep.No)
	if err != nil {
		return nil, err
	}
	vars["script_md"], vars["episode_no"], vars["assets"], vars["max_seconds"] = ep.ScriptMD, ep.No, assets, input.MaxSeconds
	output, cost, err := s.callDrama(ctx, run.job, &run.snapshot, "storyboard", vars, func(value any) error {
		return value.(drama.StoryboardOutput).ValidateMaxSeconds(input.MaxSeconds)
	})
	if err != nil {
		return nil, err
	}
	result := output.(drama.StoryboardOutput)
	ids := make(map[string]bool, len(assets))
	for _, asset := range assets {
		ids[asset.NodeID] = true
	}
	result.SanitizeReferences(ids)
	return &dramaResult{output: result, cost: cost}, nil
}

// storyboardAssets 过滤本集或全集节点，并截取提示词摘要以避免把完整画布送进文本模型。
func (s *DramaService) storyboardAssets(ctx context.Context, p *model.DramaProject, no int) ([]dramaStoryboardAsset, error) {
	if s.canvasAssets == nil {
		return nil, errcode.ErrInternal
	}
	canvas, err := s.canvasAssets.GetByID(ctx, p.UserID, p.CanvasID)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Nodes []struct {
			ID   string `json:"id"`
			Data struct {
				Drama *struct {
					Type     string `json:"type"`
					Name     string `json:"name"`
					Look     string `json:"look"`
					Episodes []int  `json:"episodes"`
				} `json:"drama"`
				Params struct {
					Prompt string `json:"prompt"`
				} `json:"params"`
			} `json:"data"`
		} `json:"nodes"`
	}
	if err = json.Unmarshal(canvas.PayloadJSON, &payload); err != nil {
		return nil, err
	}
	assets := make([]dramaStoryboardAsset, 0, len(payload.Nodes))
	for _, node := range payload.Nodes {
		d := node.Data.Drama
		// Episodes 为 nil（JSON null）是未分类：所在的集被删了还没重新归类，不属于任何一集；[] 才是全集
		if node.ID == "" || d == nil || d.Episodes == nil || !slices.Contains([]string{"character", "scene", "prop"}, d.Type) || len(d.Episodes) > 0 && !slices.Contains(d.Episodes, no) {
			continue
		}
		summary := []rune(strings.TrimSpace(node.Data.Params.Prompt))
		if len(summary) > 500 {
			summary = summary[:500]
		}
		assets = append(assets, dramaStoryboardAsset{NodeID: node.ID, Type: d.Type, Name: d.Name, Look: d.Look, PromptSummary: string(summary)})
	}
	return assets, nil
}

// saveStoryboardOutput 在project锁内再次校验take，再原子替换全部片段和集状态。
func saveStoryboardOutput(ctx context.Context, tx repository.DramaTx, p *model.DramaProject, j *model.DramaJob, output drama.StoryboardOutput) error {
	eps, err := tx.Episodes(ctx, p.UserID, p.ID)
	if err != nil {
		return err
	}
	selected := selectedEpisodes(eps, j.Episodes)
	if len(selected) != 1 {
		return errcode.ErrDramaStepNotReady
	}
	ep := selected[0]
	if err = checkDramaEpisodeStep(ep, "storyboard"); err != nil {
		return err
	}
	if err = checkStoryboardTakes(ctx, tx, p.UserID, selected); err != nil {
		return err
	}
	segments := make([]model.DramaSegment, 0, len(output.Segments))
	for seq, segment := range output.Segments {
		segments = append(segments, model.DramaSegment{UserID: p.UserID, EpisodeID: ep.ID, Seq: seq, Seconds: segment.Seconds, Prompt: segment.Prompt, Revision: 1})
	}
	if err = tx.ReplaceSegments(ctx, p.UserID, ep.ID, segments); err != nil {
		return err
	}
	ep.StoryboardStatus = "ready"
	return tx.SaveEpisode(ctx, &ep, ep.Revision)
}

// finishStoryboardStatus 失败保留旧片段；取消恢复已有ready或empty，避免集卡片永久显示generating。
func finishStoryboardStatus(ctx context.Context, tx repository.DramaTx, j *model.DramaJob, status string) error {
	if j.Step != "storyboard" || status == "succeeded" {
		return nil
	}
	eps, err := tx.Episodes(ctx, j.UserID, j.DramaID)
	if err != nil {
		return err
	}
	for _, ep := range selectedEpisodes(eps, j.Episodes) {
		ep.StoryboardStatus = "failed"
		if status == "canceled" {
			segments, err := tx.Segments(ctx, j.UserID, ep.ID)
			if err != nil {
				return err
			}
			ep.StoryboardStatus = "empty"
			if len(segments) > 0 {
				ep.StoryboardStatus = "ready"
			}
		}
		if err = tx.SaveEpisode(ctx, &ep, ep.Revision); err != nil {
			return fmt.Errorf("更新分镜状态失败: %w", err)
		}
	}
	return nil
}
