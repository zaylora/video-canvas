package service

import (
	"context"
	"errors"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
)

// Generate 固定视频种类并在短剧画布的seg节点创建任务，幂等键沿用HTTP请求头。
func (s *DramaSegmentService) Generate(ctx context.Context, userID, sid uint64, key string, req *model.GenerateDramaSegmentReq) (*model.GenerationTaskView, error) {
	// 1. 生成任务统一由既有服务校验模型、输入、积分和并发。
	return s.generateSegment(ctx, userID, sid, key, req, false)
}

// Keyframe 固定图片种类在segkf节点创建首帧任务，成功写回由任务观察者负责。
func (s *DramaSegmentService) Keyframe(ctx context.Context, userID, sid uint64, key string, req *model.GenerateDramaSegmentReq) (*model.GenerationTaskView, error) {
	// 1. 首帧和视频共用提交行为，只改变kind与节点前缀。
	return s.generateSegment(ctx, userID, sid, key, req, true)
}

// generateSegment 归属检查事务先结束，再提交任务，避免同步观察者再次锁项目造成死锁。
func (s *DramaSegmentService) generateSegment(ctx context.Context, userID, sid uint64, key string, req *model.GenerateDramaSegmentReq, keyframe bool) (*model.GenerationTaskView, error) {
	if req.ModelID == "" || req.Input == nil {
		return nil, errcode.ErrInvalidParams
	}
	var project *model.DramaProject
	err := s.repo.WithTx(ctx, func(tx repository.DramaTx) error {
		p, _, _, err := lockSegment(ctx, tx, userID, sid)
		project = p
		return err
	})
	if err != nil {
		return nil, segmentError(err)
	}
	kind := model.KindVideo
	if keyframe {
		kind = model.KindImage
	}
	result, err := s.tasks.Create(ctx, userID, key, &model.CreateGenerationTaskReq{Kind: kind, ModelID: req.ModelID, CanvasID: idcodec.ID(project.CanvasID), NodeID: segmentNode(sid, keyframe), Input: req.Input})
	if err != nil {
		return nil, err
	}
	if result == nil || len(result.Items) != 1 {
		return nil, errors.New("片段任务创建未返回唯一结果")
	}
	item := result.Items[0]
	if item.Error != nil {
		// 单节点接口把生成服务的逐项错误恢复为业务错误，保留HTTP状态与业务码。
		return nil, errcode.New(item.Error.Code, item.Error.Message, item.Error.Status)
	}
	if item.Task == nil {
		return nil, errors.New("片段任务创建结果缺少任务")
	}
	// 同一用户的幂等键可能已用于别的资源，不能把别段任务当成本段生成结果返回。
	if item.Task.NodeID != segmentNode(sid, keyframe) || item.Task.CanvasProjectID == nil || uint64(*item.Task.CanvasProjectID) != project.CanvasID || item.Task.Kind != kind {
		return nil, errcode.ErrInvalidParams.WithMsg("Idempotency-Key 已用于其他生成请求")
	}
	return item.Task, nil
}
