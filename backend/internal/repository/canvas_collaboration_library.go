package repository

import (
	"context"
	"errors"
	"strconv"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
)

// 画布收藏操作的权限与资源错误。
var (
	ErrCanvasLibraryPermission = errors.New("canvas library copy permission required")
	ErrCanvasLibraryAsset      = errors.New("canvas library source asset not found")
	ErrCanvasLibraryFolder     = errors.New("canvas library destination folder not found")
	errCanvasLibraryDuplicate  = errors.New("canvas library copy already exists")
)

func canCopyToLibrary(tx *gorm.DB, project *model.CanvasProject, actor uint64) (bool, error) {
	if actor == project.UserID {
		return true, nil
	}
	var count int64
	err := tx.Model(&model.CanvasViewer{}).Where("canvas_id = ? AND user_id = ? AND role = ? AND can_copy_to_library", project.ID, actor, model.CanvasRoleEditor).Count(&count).Error
	return count != 0, err
}

// CanvasLibrarySource 查找可收藏的素材。已经入库时返回空素材。
func (r *CanvasCollaborationRepository) CanvasLibrarySource(ctx context.Context, actor, canvas uint64, req model.CanvasLibraryCopyReq) (*model.Asset, error) {
	var source *model.Asset
	err := r.withLibraryTx(ctx, func(tx *gorm.DB) error {
		var err error
		source, err = canvasLibrarySource(tx, actor, canvas, req)
		return err
	})
	return source, err
}

// CommitLibraryCopy 原子确认独立素材和个人库条目。
func (r *CanvasCollaborationRepository) CommitLibraryCopy(ctx context.Context, actor, canvas uint64, req model.CanvasLibraryCopyReq, source *model.Asset, staged model.CanvasGenerationMedia) error {
	err := r.withLibraryTx(ctx, func(tx *gorm.DB) error {
		current, err := canvasLibrarySource(tx, actor, canvas, req)
		if err != nil || current == nil {
			return err
		}
		if current.UserID != source.UserID || current.StorageID != source.StorageID || current.StorageKey != source.StorageKey {
			return ErrCanvasLibraryAsset
		}
		if _, err := attachCollaborationMedia(tx, actor, []model.CanvasGenerationMedia{staged}); err != nil {
			return err
		}
		item := &model.AssetLibraryItem{UserID: actor, AssetID: staged.Asset.ID, CanvasSourceAssetID: &req.AssetID, Name: req.Name}
		if req.FolderID != 0 {
			folder := uint64(req.FolderID)
			item.FolderID = &folder
		}
		result := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "canvas_source_asset_id"}}, DoNothing: true}).Create(item)
		if result.Error == nil && result.RowsAffected == 0 {
			return errCanvasLibraryDuplicate
		}
		return result.Error
	})
	if errors.Is(err, errCanvasLibraryDuplicate) {
		return nil
	}
	return err
}

func (r *CanvasCollaborationRepository) withLibraryTx(ctx context.Context, fn func(*gorm.DB) error) error {
	return r.db.WithContext(ctx).Transaction(fn)
}

func canvasLibrarySource(tx *gorm.DB, actor, canvas uint64, req model.CanvasLibraryCopyReq) (*model.Asset, error) {
	project, err := lockCollaborationProject(tx, canvas)
	if err != nil {
		return nil, err
	}
	if _, err := collaborationRole(tx, project, actor); err != nil {
		return nil, err
	}
	if !project.CollaborationActive || project.CollaborationEpoch != req.Epoch {
		return nil, ErrCanvasCollaborationSession
	}
	allowed, err := canCopyToLibrary(tx, project, actor)
	if err != nil {
		return nil, err
	}
	if !allowed || actor == project.UserID {
		return nil, ErrCanvasLibraryPermission
	}
	if err := validateCanvasLibraryDestination(tx, actor, uint64(req.FolderID)); err != nil {
		return nil, err
	}
	if err := validateCanvasLibraryNode(tx, canvas, req); err != nil {
		return nil, err
	}
	var existing int64
	if err := tx.Model(&model.AssetLibraryItem{}).Where("user_id = ? AND canvas_source_asset_id = ?", actor, req.AssetID).Count(&existing).Error; err != nil || existing > 0 {
		return nil, err
	}
	var asset model.Asset
	err = tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND user_id = ?", req.AssetID, project.UserID).Take(&asset).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrCanvasLibraryAsset
	}
	return &asset, err
}

func validateCanvasLibraryDestination(tx *gorm.DB, actor uint64, folderID uint64) error {
	if folderID == 0 {
		return nil
	}
	err := lockAssetFolder(tx, actor, folderID)
	if errors.Is(err, ErrNotFound) {
		return ErrCanvasLibraryFolder
	}
	return err
}

func validateCanvasLibraryNode(tx *gorm.DB, canvas uint64, req model.CanvasLibraryCopyReq) error {
	var node model.CanvasCollaborationRecord
	err := tx.Select("record_id").Where("canvas_id = ? AND kind = 'node' AND record_id = ? AND NOT deleted", canvas, req.NodeID).
		Where("body #>> '{data,kind}' IN ('image', 'video', 'audio') AND body #>> '{data,assetId}' = ?", strconv.FormatUint(req.AssetID, 10)).Take(&node).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrCanvasLibraryAsset
	}
	return err
}
