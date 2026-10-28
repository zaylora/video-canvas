package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
)

// segmentNode 与对外片段ID共用编码，避免将数据库主键直接暴露给节点。
func segmentNode(sid uint64, keyframe bool) string {
	if keyframe {
		return "segkf:" + idcodec.Encode(sid)
	}
	return "seg:" + idcodec.Encode(sid)
}

// readView 在统一锁顺序下恢复任务写回；素材读取在事务提交之后进行。
func (s *DramaSegmentService) readView(ctx context.Context, userID, sid uint64) (*model.DramaSegmentView, *model.DramaProject, *model.DramaEpisode, bool, error) {
	var p *model.DramaProject
	var ep *model.DramaEpisode
	var row *model.DramaSegment
	var takes []model.GenerationTask
	var changed bool
	err := s.repo.WithTx(ctx, func(tx repository.DramaTx) error {
		var err error
		p, ep, row, err = lockSegment(ctx, tx, userID, sid)
		if err != nil {
			return err
		}
		takes, err = tx.SegmentTasks(ctx, userID, p.CanvasID, segmentNode(sid, false))
		if err != nil {
			return err
		}
		frames, err := tx.SegmentTasks(ctx, userID, p.CanvasID, segmentNode(sid, true))
		if err != nil {
			return err
		}
		changed, err = reconcileSegment(row, takes, frames)
		if err != nil || !changed {
			return err
		}
		return tx.SaveSegment(ctx, row, row.Revision)
	})
	if err != nil {
		return nil, nil, nil, false, segmentError(err)
	}
	v, err := s.segmentView(ctx, row, ep.No, takes)
	return v, p, ep, changed, err
}

// reconcileSegment 首次成功自动采用；首帧始终取创建时间和ID排序后的最新成功产物。
func reconcileSegment(row *model.DramaSegment, takes, frames []model.GenerationTask) (bool, error) {
	changed := false
	if row.ChosenTaskID == nil {
		for i := range takes {
			if takes[i].Kind == model.KindVideo && takes[i].Status == model.TaskSucceeded {
				id := takes[i].ID
				row.ChosenTaskID, changed = &id, true
				break
			}
		}
	}
	for i := len(frames) - 1; i >= 0; i-- {
		if frames[i].Kind != model.KindImage || frames[i].Status != model.TaskSucceeded {
			continue
		}
		id, err := segmentOutputAsset(&frames[i], model.KindImage)
		if err != nil {
			return false, err
		}
		if id == 0 {
			continue
		}
		if row.KeyframeAssetID == nil || *row.KeyframeAssetID != id {
			row.KeyframeAssetID, changed = &id, true
		}
		break
	}
	return changed, nil
}

// segmentOutputAsset 从已转存产物读取指定媒体类型的第一个素材；非法持久化JSON按内部错误处理。
func segmentOutputAsset(task *model.GenerationTask, kind string) (uint64, error) {
	if len(task.OutputJSON) == 0 {
		return 0, nil
	}
	var outputs []model.TaskOutput
	if err := json.Unmarshal(task.OutputJSON, &outputs); err != nil {
		return 0, fmt.Errorf("解析片段任务产物失败：%w", err)
	}
	for _, output := range outputs {
		if output.MediaType == kind && output.AssetID > 0 {
			return output.AssetID, nil
		}
	}
	return 0, nil
}

// segmentView 状态优先反映最新take，已有采用不掩盖新一轮生成或失败。
func (s *DramaSegmentService) segmentView(ctx context.Context, row *model.DramaSegment, no int, tasks []model.GenerationTask) (*model.DramaSegmentView, error) {
	v := &model.DramaSegmentView{ID: idcodec.ID(row.ID), EpisodeNo: no, Seq: row.Seq, Seconds: row.Seconds, Prompt: row.Prompt, Shots: []model.DramaSegmentShot{}, Takes: []model.DramaSegmentTakeView{}, Status: "idle", Revision: row.Revision, UpdatedAt: row.UpdatedAt}
	if len(row.Shots) > 0 {
		if err := json.Unmarshal(row.Shots, &v.Shots); err != nil {
			return nil, fmt.Errorf("解析片段分镜失败：%w", err)
		}
	}
	if row.ChosenTaskID != nil {
		v.ChosenTaskID, v.Status = idcodec.Encode(*row.ChosenTaskID), "done"
	}
	if row.KeyframeAssetID != nil {
		var err error
		v.Keyframe, err = s.segmentAsset(ctx, row.UserID, *row.KeyframeAssetID)
		if err != nil {
			return nil, err
		}
	}
	for i := range tasks {
		if tasks[i].Kind != model.KindVideo {
			continue
		}
		take, err := s.segmentTake(ctx, row.UserID, &tasks[i])
		if err != nil {
			return nil, err
		}
		v.Takes = append(v.Takes, take)
		if tasks[i].Status == model.TaskSucceeded {
			v.Status = "done"
		}
	}
	if len(v.Takes) > 0 {
		switch v.Takes[len(v.Takes)-1].Status {
		case model.TaskPending, model.TaskQueued, model.TaskRunning, model.TaskFinalizing:
			v.Status = "running"
		case model.TaskFailed:
			v.Status = "error"
		}
	}
	return v, nil
}

// segmentTake 仅对成功任务读取视频素材，状态和用户错误沿用真实生成任务。
func (s *DramaSegmentService) segmentTake(ctx context.Context, userID uint64, task *model.GenerationTask) (model.DramaSegmentTakeView, error) {
	v := model.DramaSegmentTakeView{TaskID: idcodec.ID(task.ID), Status: task.Status, ErrorMessage: task.ErrorMessage, CreatedAt: task.CreatedAt}
	if task.Status != model.TaskSucceeded {
		return v, nil
	}
	id, err := segmentOutputAsset(task, model.KindVideo)
	if err != nil || id == 0 {
		return v, err
	}
	v.Asset, err = s.segmentAsset(ctx, userID, id)
	return v, err
}

// segmentAsset 已删除素材显示为空；数据库等未知故障仍上报，不能伪装为没有素材。
func (s *DramaSegmentService) segmentAsset(ctx context.Context, userID, id uint64) (*model.AssetView, error) {
	v, err := s.assets.View(ctx, userID, id)
	var business *errcode.Error
	if errors.Is(err, repository.ErrNotFound) || (errors.As(err, &business) && business.Code == errcode.ErrAssetNotFound.Code) {
		return nil, nil
	}
	return v, err
}

// OnTaskChanged 处理片段任务状态变化，自动采用、首帧写回和通知在提交后完成；已删除片段忽略。
func (s *DramaSegmentService) OnTaskChanged(ctx context.Context, task *model.GenerationTask) error {
	// 1. 只接受正式片段任务；画布、对话和已删除片段不改变既有行为。
	if task == nil || task.IsTest || task.CanvasProjectID == nil {
		return nil
	}
	prefix, kind := "seg:", model.KindVideo
	if strings.HasPrefix(task.NodeID, "segkf:") {
		prefix, kind = "segkf:", model.KindImage
	}
	if !strings.HasPrefix(task.NodeID, prefix) || task.Kind != kind {
		return nil
	}
	sid, ok := idcodec.Decode(strings.TrimPrefix(task.NodeID, prefix))
	if !ok {
		return nil
	}
	row, err := s.repo.Segment(ctx, task.UserID, sid, false)
	if errors.Is(err, repository.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	ep, err := s.repo.SegmentEpisode(ctx, task.UserID, row.EpisodeID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	p, err := s.repo.Project(ctx, task.UserID, ep.DramaID, false)
	if errors.Is(err, repository.ErrNotFound) {
		return nil
	}
	if err != nil || p.CanvasID != *task.CanvasProjectID {
		return err
	}
	// 2. 从任务表重读而非信任回调快照，迟到事件不会覆盖较新的首帧或采用。
	_, err = s.changedView(ctx, task.UserID, sid)
	var business *errcode.Error
	if errors.As(err, &business) && business.Code == errcode.ErrDramaSegmentNotFound.Code {
		return nil
	}
	return err
}
