package service

import (
	"context"
	"crypto/rand"
	"errors"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/provider"
	"video-canvas/internal/repository"
)

// StageMediaImports 暂存编辑者的素材副本，画布确认事务负责接收副本。
func (s *CanvasCollaborationService) StageMediaImports(ctx context.Context, actor, canvas uint64, batch repository.CanvasCollaborationBatch) (map[uint64]repository.CanvasCollaborationImport, error) {
	_, scoped := model.CanvasAutomationFromContext(ctx)
	if s.media == nil || scoped {
		return nil, nil
	}
	owner, ids, err := s.repo.ImportPlan(ctx, actor, canvas, batch)
	if err != nil || len(ids) == 0 || owner == actor {
		return nil, err
	}
	work, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	attempt := rand.Text()
	imports := make(map[uint64]repository.CanvasCollaborationImport)
	for _, id := range ids {
		source, err := s.media.Get(work, actor, id)
		if errors.Is(err, provider.ErrAssetNotFound) || errors.Is(err, repository.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if source.Source == model.AssetSourceOfficial || source.UserID != actor {
			continue
		}
		copyAsset, err := s.media.StageCanvasAsset(work, source, owner, func(storage uint64, key string) error {
			return s.shares.Stage(work, &model.CanvasDeliveryStage{Attempt: attempt, StorageID: storage, StorageKey: key})
		})
		if err != nil {
			return nil, err
		}
		imports[id] = repository.CanvasCollaborationImport{SourceStorageID: source.StorageID, SourceStorageKey: source.StorageKey,
			Copy: model.CanvasGenerationMedia{Attempt: attempt, Asset: copyAsset}}
	}
	return imports, nil
}
