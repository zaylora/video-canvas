package service

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
)

// SetLibraryPermission 更新编辑成员保存共享素材的授权。
func (s *CanvasCollaborationService) SetLibraryPermission(ctx context.Context, owner, canvas, member uint64, enabled bool) error {
	if err := s.shares.SetLibraryPermission(ctx, owner, canvas, member, enabled); err != nil {
		return collaborationError(err)
	}
	s.notice(ctx, canvas, 0, 0)
	return nil
}

// CopyToLibrary 将确认过的画布素材保存为编辑者个人库的独立副本。
func (s *CanvasCollaborationService) CopyToLibrary(ctx context.Context, actor, canvas uint64, req model.CanvasLibraryCopyReq) error {
	if s.media == nil {
		return errcode.ErrCanvasCollaborationDisabled
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name != "" {
		if err := validateLibraryItemName(req.Name); err != nil {
			return err
		}
	}
	source, err := s.repo.CanvasLibrarySource(ctx, actor, canvas, req)
	if err != nil || source == nil {
		return canvasLibraryError(err)
	}
	work, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	attempt := rand.Text()
	asset, err := s.media.StageCanvasAsset(work, source, actor, func(storageID uint64, key string) error {
		return s.shares.Stage(work, &model.CanvasDeliveryStage{Attempt: attempt, StorageID: storageID, StorageKey: key})
	})
	if err != nil {
		return canvasLibraryError(err)
	}
	return canvasLibraryError(s.repo.CommitLibraryCopy(work, actor, canvas, req, source, model.CanvasGenerationMedia{Attempt: attempt, Asset: asset}))
}

func canvasLibraryError(err error) error {
	switch {
	case errors.Is(err, repository.ErrCanvasLibraryPermission):
		return errcode.ErrForbidden.WithMsg("加入资产库需画布所有者授权")
	case errors.Is(err, repository.ErrCanvasLibraryAsset):
		return errcode.ErrAssetNotFound.WithMsg("画布中已找不到这份素材，请刷新后重试")
	case errors.Is(err, repository.ErrCanvasLibraryFolder):
		return errcode.ErrAssetFolderNotFound
	default:
		return collaborationError(err)
	}
}
