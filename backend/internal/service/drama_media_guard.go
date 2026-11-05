package service

import (
	"context"
	"slices"
	"strings"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
)

// checkDramaMediaTasks 同一集的片段视频和分镜、合成共享互斥，不影响其他集与文本任务。
func checkDramaMediaTasks(ctx context.Context, tx repository.DramaTx, p *model.DramaProject, eps []model.DramaEpisode, key string) error {
	for _, ep := range eps {
		if ep.LockedAt == nil {
			return errcode.ErrDramaStepNotReady.WithMsg("媒体任务只允许已锁定的集")
		}
		rows, err := tx.Segments(ctx, p.UserID, ep.ID)
		if err != nil {
			return err
		}
		for _, row := range rows {
			tasks, err := tx.SegmentTasks(ctx, p.UserID, p.CanvasID, segmentNode(row.ID))
			if err != nil {
				return err
			}
			for _, task := range tasks {
				if slices.Contains(model.ActiveTaskStatuses, task.Status) && (key == "" || task.IdempotencyKey != key) {
					return errcode.ErrDramaJobBusy
				}
			}
		}
	}
	return nil
}

// guardDramaGeneration 在生成任务自身事务里锁短剧项目，防止两个片段同时穿透预检查。
func guardDramaGeneration(ctx context.Context, tx repository.GenerationTaskTx, task *model.GenerationTask) error {
	if task.IsTest || !strings.HasPrefix(task.NodeID, "seg:") {
		return nil
	}
	adapter, ok := tx.(interface{ DramaTx() repository.DramaTx })
	if !ok {
		return errcode.ErrInternal
	}
	dtx := adapter.DramaTx()
	sid, ok := idcodec.Decode(strings.TrimPrefix(task.NodeID, "seg:"))
	if !ok {
		return errcode.ErrInvalidParams
	}
	p, ep, _, err := lockSegment(ctx, dtx, task.UserID, sid)
	if err != nil {
		return segmentError(err)
	}
	if task.CanvasProjectID == nil || *task.CanvasProjectID != p.CanvasID || task.Kind != model.KindVideo {
		return errcode.ErrInvalidParams
	}
	jobs, err := dtx.Jobs(ctx, p.UserID, p.ID, true)
	if err != nil {
		return err
	}
	if dramaJobsConflict(jobs, &model.DramaJobReq{Step: "segment", Episodes: []int{ep.No}}) {
		return errcode.ErrDramaJobBusy
	}
	return checkDramaMediaTasks(ctx, dtx, p, []model.DramaEpisode{*ep}, task.IdempotencyKey)
}
