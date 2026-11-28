package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"

	"gorm.io/gorm"

	"video-canvas/internal/model"
)

const collaborationReplayBytes = 512 * 1024

// CanJoin checks current canvas membership and account state for a room subscription.
func (r *CanvasCollaborationRepository) CanJoin(ctx context.Context, user, canvas uint64) bool {
	return r.canAccessRoom(ctx, user, canvas, false)
}

// CanEditRoom 校验账号与画布状态以及房间内编辑者的授权。
func (r *CanvasCollaborationRepository) CanEditRoom(ctx context.Context, user, canvas uint64) bool {
	return r.canAccessRoom(ctx, user, canvas, true)
}

// RoomReaders 一次查询当前仍有权接收画布版本通知的账号。
func (r *CanvasCollaborationRepository) RoomReaders(ctx context.Context, users []uint64, canvas uint64) map[uint64]bool {
	allowed := make(map[uint64]bool)
	if len(users) == 0 {
		return allowed
	}
	var ids []uint64
	err := r.db.WithContext(ctx).Raw(`SELECT u.id FROM users u CROSS JOIN canvas_projects p
		WHERE u.id IN ? AND u.deleted_at IS NULL AND u.status = ?
		AND p.id = ? AND p.drama_id IS NULL AND p.deleted_at IS NULL AND p.collaboration_active
		AND (p.user_id = u.id OR EXISTS (SELECT 1 FROM canvas_viewers v WHERE v.canvas_id = p.id AND v.user_id = u.id))`,
		users, model.UserStatusActive, canvas).Scan(&ids).Error
	if err == nil {
		for _, id := range ids {
			allowed[id] = true
		}
	}
	return allowed
}

func (r *CanvasCollaborationRepository) canAccessRoom(ctx context.Context, user, canvas uint64, edit bool) bool {
	var allowed bool
	err := r.db.WithContext(ctx).Raw(`SELECT EXISTS (
		SELECT 1 FROM canvas_projects p JOIN users u ON u.id = ? AND u.deleted_at IS NULL AND u.status = ?
		WHERE p.id = ? AND p.drama_id IS NULL AND p.deleted_at IS NULL AND p.collaboration_active
		AND (p.user_id = u.id OR EXISTS (SELECT 1 FROM canvas_viewers v WHERE v.canvas_id = p.id AND v.user_id = u.id AND (NOT ? OR v.role = 'editor')))
	)`, user, model.UserStatusActive, canvas, edit).Scan(&allowed).Error
	return err == nil && allowed
}

// CanvasCollaborationReplay 返回同一授权视图下的当前版本和连续增量。
type CanvasCollaborationReplay struct {
	CanGenerate      bool                        `json:"canGenerate"`
	CanCopyToLibrary bool                        `json:"canCopyToLibrary"`
	Epoch            uint64                      `json:"epoch"`
	Revision         uint64                      `json:"revision"`
	Cursor           uint64                      `json:"cursor"`
	Title            string                      `json:"title"`
	Role             string                      `json:"role"`
	Events           []CanvasCollaborationCommit `json:"events"`
	SnapshotRequired bool                        `json:"snapshotRequired"`
}

// Snapshot returns a canvas and the caller's current role from the same committed view.
func (r *CanvasCollaborationRepository) Snapshot(ctx context.Context, actorID, canvasID uint64) (*model.CanvasProject, string, error) {
	var project model.CanvasProject
	var role string
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ? AND drama_id IS NULL", canvasID).Take(&project).Error; err != nil {
			return translate(err)
		}
		var err error
		role, project.CollaborationCanGenerate, project.CollaborationCanCopyToLibrary, err = collaborationReadGrant(tx, &project, actorID)
		if err != nil {
			return err
		}
		if !project.CollaborationActive {
			return ErrCanvasCollaborationSession
		}
		if err := checkCanvasAutomation(tx, &project, actorID, role, false); err != nil {
			return err
		}
		return hydrateCollaborationProject(tx, &project)
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, "", err
	}
	return &project, role, nil
}

// Replay 根据当前权限和会话代数，从连续版本分页读取已确认操作。
func (r *CanvasCollaborationRepository) Replay(ctx context.Context, actorID, canvasID, epoch, after uint64, limit int) (*CanvasCollaborationReplay, error) {
	result := &CanvasCollaborationReplay{Cursor: after, Events: make([]CanvasCollaborationCommit, 0)}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var project model.CanvasProject
		if err := tx.Select("id", "user_id", "title", "revision", "collaboration_epoch", "collaboration_active").
			Where("id = ? AND drama_id IS NULL", canvasID).Take(&project).Error; err != nil {
			return translate(err)
		}
		role, canGenerate, canCopy, err := collaborationReadGrant(tx, &project, actorID)
		if err != nil {
			return err
		}
		if !project.CollaborationActive || project.CollaborationEpoch != epoch {
			return ErrCanvasCollaborationSession
		}
		if err := checkCanvasAutomation(tx, &project, actorID, role, false); err != nil {
			return err
		}
		result.Epoch, result.Revision, result.Role = project.CollaborationEpoch, project.Revision, role
		result.Title = project.Title
		_, scoped := model.CanvasAutomationFromContext(ctx)
		result.CanGenerate, result.CanCopyToLibrary = !scoped && canGenerate, !scoped && canCopy
		if after == project.Revision {
			return nil
		}
		if after > project.Revision {
			result.SnapshotRequired = true
			return nil
		}
		return replayCollaborationEvents(tx, actorID, canvasID, min(max(limit, 1), 100), result)
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func collaborationReadGrant(tx *gorm.DB, project *model.CanvasProject, actor uint64) (string, bool, bool, error) {
	if actor == project.UserID {
		return model.CanvasRoleOwner, true, true, nil
	}
	var grant model.CanvasViewer
	err := tx.Select("role", "can_generate", "can_copy_to_library").
		Where("canvas_id = ? AND user_id = ?", project.ID, actor).Take(&grant).Error
	if err != nil {
		return "", false, false, translate(err)
	}
	editor := grant.Role == model.CanvasRoleEditor
	return grant.Role, editor && grant.CanGenerate, editor && grant.CanCopyToLibrary, nil
}

func replayCollaborationEvents(tx *gorm.DB, actorID, canvasID uint64, limit int, result *CanvasCollaborationReplay) error {
	var events []model.CanvasCollaborationEvent
	const pageQuery = `WITH candidates AS (
		SELECT revision, actor_id, op_id, changes
		FROM canvas_collaboration_events
		WHERE canvas_id = ? AND revision > ? AND revision <= ? AND epoch = ?
		ORDER BY revision LIMIT ?
	)
	SELECT revision, actor_id, op_id, changes
	FROM (SELECT candidates.*, SUM(octet_length(changes::text)) OVER (ORDER BY revision) AS bytes FROM candidates) AS page
	WHERE bytes <= ? ORDER BY revision`
	if err := tx.Raw(pageQuery, canvasID, result.Cursor, result.Revision, result.Epoch, limit, collaborationReplayBytes).Scan(&events).Error; err != nil {
		return err
	}
	result.SnapshotRequired = len(events) == 0
	_, scoped := model.CanvasAutomationFromContext(tx.Statement.Context)
	for _, event := range events {
		if event.Revision != result.Cursor+1 {
			result.SnapshotRequired = true
			break
		}
		commit := CanvasCollaborationCommit{Epoch: result.Epoch, Revision: event.Revision}
		if event.ActorID == actorID {
			commit.OpID = event.OpID
		}
		if err := json.Unmarshal(event.Changes, &commit.Changes); err != nil {
			return err
		}
		if event.ActorID != actorID {
			for index := range commit.Changes {
				commit.Changes[index].PreviousRevision = 0
			}
		}
		if result.Role != model.CanvasRoleOwner || scoped {
			if err := projectGenerationEvent(canvasID, commit.Changes); err != nil {
				return err
			}
			commit.Changes = slices.DeleteFunc(commit.Changes, func(change CanvasCollaborationChange) bool {
				return change.Action == "" && len(change.Fields) == 0
			})
		}
		result.Events = append(result.Events, commit)
		result.Cursor = event.Revision
	}
	return nil
}
