package service

import (
	"context"
	"errors"
	"net/http"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
)

const canvasMediaStageTimeout = 10 * time.Minute

var errCanvasMediaAlreadyStaged = errors.New("加工产物已暂存")

type canvasMediaDraftStore interface {
	StageCanvasMedia(context.Context, uint64, UploadInput, func(*model.Asset) error) (*model.Asset, error)
	FileURL(string) string
	MaxUpload() int64
}

// WithMediaDrafts 接入加工产物的暂存存储。
func (s *CanvasCollaborationService) WithMediaDrafts(store canvasMediaDraftStore) *CanvasCollaborationService {
	s.drafts = store
	return s
}

// MediaDraftUploadLimit 返回协作媒体暂存的文件上限。
func (s *CanvasCollaborationService) MediaDraftUploadLimit() int64 {
	if s.drafts == nil {
		return 0
	}
	return s.drafts.MaxUpload()
}

// StageCanvasMedia 写入临时对象，由调用者在写入前登记用于故障回收的元数据。
func (s *AssetService) StageCanvasMedia(ctx context.Context, owner uint64, in UploadInput, reserve func(*model.Asset) error) (*model.Asset, error) {
	if in.Body == nil {
		return nil, errcode.ErrAssetInvalid
	}
	if in.Size > s.MaxUpload() {
		return nil, assetTooLarge(s.MaxUpload())
	}
	asset, _, err := s.ingest(ctx, assetIngest{UserID: owner, Source: model.AssetSourceUpload, KeyPrefix: "u",
		FileName: sanitizeAssetFileName(in.FileName), MaxBytes: s.MaxUpload(), Body: in.Body, Stage: func(asset *model.Asset) error {
			if asset.Kind != "image" && asset.Kind != "video" && asset.Kind != "audio" {
				return errcode.ErrAssetInvalid
			}
			return reserve(asset)
		}})
	var tooLarge *http.MaxBytesError
	var read *assetReadError
	if errors.As(err, &tooLarge) {
		return nil, assetTooLarge(s.MaxUpload())
	}
	if errors.As(err, &read) {
		return nil, errcode.ErrAssetInvalid.WithMsg("上传中断或读取文件失败，请重试")
	}
	return asset, err
}

// StageMediaDraft 暂存当前编辑者加工的文件，暂存期间只保存对象和回收记录。
func (s *CanvasCollaborationService) StageMediaDraft(ctx context.Context, actor, canvas uint64, req model.CanvasMediaDraftReq, in UploadInput) error {
	if s.drafts == nil {
		return collaborationError(repository.ErrNotFound)
	}
	work, cancel := context.WithTimeout(ctx, canvasMediaStageTimeout)
	defer cancel()
	owner, err := s.repo.MediaDraftOwner(work, actor, canvas, req)
	if err != nil {
		return collaborationError(err)
	}
	asset, err := s.drafts.StageCanvasMedia(work, owner, in, func(asset *model.Asset) error {
		reserved, err := s.repo.ReserveMediaDraft(work, actor, canvas, req, asset)
		if err == nil && !reserved {
			return errCanvasMediaAlreadyStaged
		}
		return err
	})
	if errors.Is(err, errCanvasMediaAlreadyStaged) {
		return nil
	}
	if err != nil {
		return collaborationError(err)
	}
	finish, cancelFinish := context.WithTimeout(context.WithoutCancel(work), 5*time.Second)
	defer cancelFinish()
	return collaborationError(s.repo.FinishMediaDraft(finish, actor, canvas, req.ID, asset.StorageKey))
}

// AttachMediaDraft 复核当前权限和源素材，确认后广播画布版本供其他客户端追赶。
func (s *CanvasCollaborationService) AttachMediaDraft(ctx context.Context, actor, canvas, epoch uint64, id string) (*repository.CanvasCollaborationCommit, error) {
	if s.drafts == nil {
		return nil, collaborationError(repository.ErrNotFound)
	}
	result, err := s.repo.AttachMediaDraft(ctx, actor, canvas, epoch, id, s.drafts.FileURL)
	if err == nil && len(result.Changes) > 0 {
		s.notice(ctx, canvas, result.Epoch, result.Revision)
	}
	return result, collaborationError(err)
}
