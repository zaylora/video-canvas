package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"

	"video-canvas/internal/model"
)

// CanvasModePreview 描述切换到目标模式时受影响的现有成员范围。
type CanvasModePreview struct {
	model.CanvasArchivePreview
	Active bool `json:"active"`
}

// ModePreview 从同一事务视图读取当前画布版本和成员授权。
func (r *CanvasCollaborationRepository) ModePreview(ctx context.Context, owner, canvas uint64, active bool) (*CanvasModePreview, error) {
	var preview *CanvasModePreview
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var project model.CanvasProject
		if err := tx.Select("id", "user_id", "title", "revision", "collaboration_epoch", "collaboration_active").
			Where("id = ? AND user_id = ? AND drama_id IS NULL", canvas, owner).Take(&project).Error; err != nil {
			return translate(err)
		}
		var err error
		preview, err = canvasModePreview(tx, &project, active)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return preview, err
}

func canvasModePreview(tx *gorm.DB, project *model.CanvasProject, active bool) (*CanvasModePreview, error) {
	impact, err := canvasArchivePreview(tx, project)
	if err != nil {
		return nil, err
	}
	key := fmt.Appendf(nil, "canvas-mode/%s/%d/%t/%t", impact.Preview, project.CollaborationEpoch, project.CollaborationActive, active)
	impact.Preview = fmt.Sprintf("%x", sha256.Sum256(key))
	return &CanvasModePreview{CanvasArchivePreview: *impact, Active: active}, nil
}

// ArchivePreview 读取当前画布版本及归档时会撤销的成员范围。
func (r *CanvasCollaborationRepository) ArchivePreview(ctx context.Context, owner, canvas uint64) (*model.CanvasArchivePreview, error) {
	var preview *model.CanvasArchivePreview
	err := r.WithTx(ctx, func(tx *gorm.DB) error {
		project, err := lockCollaborationProject(tx, canvas)
		if err != nil {
			return err
		}
		if project.UserID != owner {
			return ErrNotFound
		}
		if err := NewCanvasShareRepository(tx).Ready(ctx, owner, canvas); err != nil {
			return err
		}
		preview, err = canvasArchivePreview(tx, project)
		return err
	})
	return preview, err
}

// Manage renames or retires an owned canvas at a confirmed revision.
func (r *CanvasCollaborationRepository) Manage(ctx context.Context, owner, canvas uint64, req model.CanvasManagementReq) (*model.CanvasProject, error) {
	title := strings.TrimSpace(req.Title)
	if !validCanvasManagement(req, title) {
		return nil, ErrCanvasCollaborationInvalidBatch
	}
	var updated *model.CanvasProject
	err := r.WithTx(ctx, func(tx *gorm.DB) error {
		project, err := lockCollaborationProject(tx, canvas)
		if err != nil {
			return err
		}
		if project.UserID != owner {
			return ErrNotFound
		}
		if project.Revision != req.Revision {
			return ErrRevisionConflict
		}
		updated = project
		if !req.Archive {
			return renameCanvasProject(tx, project, title)
		}
		return archiveCanvasProject(ctx, tx, project, req.Preview)
	})
	return updated, err
}

func validCanvasManagement(req model.CanvasManagementReq, title string) bool {
	if req.Archive {
		return req.Title == "" && len(req.Preview) == 64
	}
	return req.Preview == "" && title != "" && !strings.ContainsRune(title, 0) && utf8.RuneCountInString(title) <= 255
}

func archiveCanvasProject(ctx context.Context, tx *gorm.DB, project *model.CanvasProject, expected string) error {
	preview, err := canvasArchivePreview(tx, project)
	if err != nil {
		return err
	}
	if preview.Preview != expected {
		return ErrRevisionConflict
	}
	shares := NewCanvasShareRepository(tx)
	if err := shares.Ready(ctx, project.UserID, project.ID); err != nil {
		return err
	}
	return shares.retire(ctx, project.UserID, project.ID)
}

func canvasArchivePreview(tx *gorm.DB, project *model.CanvasProject) (*model.CanvasArchivePreview, error) {
	var grants []struct {
		UserID uint64
		Role   string
	}
	if err := tx.Model(&model.CanvasViewer{}).Select("user_id", "role").Where("canvas_id = ?", project.ID).
		Order("user_id").Find(&grants).Error; err != nil {
		return nil, err
	}
	data, err := json.Marshal(grants)
	if err != nil {
		return nil, err
	}
	data = fmt.Appendf(data, "/%d/%d/%d", project.ID, project.UserID, project.Revision)
	preview := &model.CanvasArchivePreview{Revision: project.Revision, Title: project.Title, Preview: fmt.Sprintf("%x", sha256.Sum256(data))}
	for _, grant := range grants {
		if grant.Role == model.CanvasRoleEditor {
			preview.Editors++
		} else {
			preview.Viewers++
		}
	}
	return preview, nil
}

func renameCanvasProject(tx *gorm.DB, project *model.CanvasProject, title string) error {
	project.Revision++
	project.Title = title
	if err := tx.Model(project).Select("Title", "Revision", "UpdatedAt").Updates(project).Error; err != nil || !project.CollaborationActive {
		return err
	}
	event := model.CanvasCollaborationEvent{
		CanvasID: project.ID, Revision: project.Revision, Epoch: project.CollaborationEpoch,
		ActorID: project.UserID, OpID: "rename-" + strconv.FormatUint(project.Revision, 10),
		Changes: []byte(`[]`), Source: "rename",
	}
	if err := tx.Create(&event).Error; err != nil {
		return err
	}
	return checkpointCollaborationEvent(tx, event)
}
