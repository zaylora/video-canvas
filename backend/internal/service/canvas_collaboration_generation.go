package service

import (
	"context"
	"crypto/rand"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/pkg/ws"

	"go.uber.org/zap"
)

type canvasGenerationProgressSource interface {
	// CanvasProgressRevision 核对可共享任务的版本，绑定或授权失效时返回零。
	CanvasProgressRevision(context.Context, *model.GenerationTask) (uint64, error)
}

// publishCanvasGenerationProgress 只向协作房间发送节点进度与运行起始版本。
func (s *GenerationTaskService) publishCanvasGenerationProgress(ctx context.Context, task *model.GenerationTask) {
	if s.canvasBus == nil || task.CanvasProjectID == nil || task.CollaborationEpoch == 0 || task.Progress == nil || model.IsTerminalStatus(task.Status) {
		return
	}
	source, ok := s.repo.(canvasGenerationProgressSource)
	if !ok {
		return
	}
	deadline, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	revision, err := source.CanvasProgressRevision(deadline, task)
	if err != nil || revision == 0 {
		return
	}
	s.canvasBus.Notice(deadline, ws.CanvasCollaborationNotice{
		CanvasID: idcodec.Encode(*task.CanvasProjectID), Epoch: task.CollaborationEpoch,
		Progress: &ws.CanvasGenerationProgress{NodeID: task.NodeID, RunRevision: revision, Percent: min(100, max(0, *task.Progress))},
	})
}

type canvasGenerationMediaRepo interface {
	// CanvasGenerationOwner 查询生成发起者在当前会话下是否可交付结果。
	CanvasGenerationOwner(context.Context, *model.GenerationTask) (uint64, error)
	// StageCanvasGenerationMedia 持久记录要复制的对象，便于失败后回收。
	StageCanvasGenerationMedia(context.Context, *model.CanvasDeliveryStage) error
}

type canvasGenerationMediaStore interface {
	// Get 读取发起人账户拥有的原始产物。
	Get(context.Context, uint64, uint64) (*model.Asset, error)
	// StageCanvasAsset 创建项目所有者持有的独立素材对象。
	StageCanvasAsset(context.Context, *model.Asset, uint64, func(uint64, string) error) (*model.Asset, error)
	// FileURL 返回该存储对象的站内读取地址。
	FileURL(string) string
}

// stageCollaborationOutputs 为已授权成员的产物准备独立副本，失败时保留发起人的个人产物与正常结算。
func (s *GenerationTaskService) stageCollaborationOutputs(ctx context.Context, task *model.GenerationTask, outputs []model.TaskOutput) []model.CanvasGenerationMedia {
	if task.CollaborationEpoch == 0 || len(outputs) == 0 {
		return nil
	}
	repo, repoOK := s.repo.(canvasGenerationMediaRepo)
	store, storeOK := s.assets.(canvasGenerationMediaStore)
	if !repoOK || !storeOK {
		return nil
	}
	work, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	owner, err := repo.CanvasGenerationOwner(work, task)
	if err != nil {
		logger.Warn("协作生成产物授权查询失败", logger.GenRun(task.ID), zap.Error(err))
		return nil
	}
	if owner == 0 || owner == task.UserID {
		return nil
	}
	return stageMemberOutputs(work, repo, store, task, owner, outputs)
}

// stageMemberOutputs 按同一尝试编号登记产物对象，由结算事务择机确认。
func stageMemberOutputs(ctx context.Context, repo canvasGenerationMediaRepo, store canvasGenerationMediaStore, task *model.GenerationTask, owner uint64, outputs []model.TaskOutput) []model.CanvasGenerationMedia {
	attempt := rand.Text()
	copies := make([]model.CanvasGenerationMedia, 0, len(outputs))
	for _, output := range outputs {
		if output.AssetID == 0 {
			continue
		}
		source, err := store.Get(ctx, task.UserID, output.AssetID)
		if err != nil {
			logger.Warn("协作生成无法读取原始产物", logger.GenRun(task.ID), zap.Error(err))
			return nil
		}
		stagedAsset, err := store.StageCanvasAsset(ctx, source, owner, func(storage uint64, key string) error {
			return repo.StageCanvasGenerationMedia(ctx, &model.CanvasDeliveryStage{Attempt: attempt, StorageID: storage, StorageKey: key})
		})
		if err != nil {
			logger.Warn("协作生成产物暂存失败", logger.GenRun(task.ID), zap.Error(err))
			return nil
		}
		output.URL = store.FileURL(stagedAsset.StorageKey)
		copies = append(copies, model.CanvasGenerationMedia{Asset: stagedAsset, Attempt: attempt, Output: output})
	}
	return copies
}
