package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
)

// CanvasCommentThread 包含同一权限视图的评论和当前可提及成员。
type CanvasCommentThread struct {
	Rows    []model.CanvasCollaborationComment
	Members []uint64
}

// CanvasCommentEvent 包含一条评论及其节点在当前视图中是否可见。
type CanvasCommentEvent struct {
	model.CanvasCollaborationComment
	Visible bool
}

// CanvasCommentReplay 包含一批按评论主键排序的记录和起始水位。
type CanvasCommentReplay struct {
	Cursor uint64
	Events []CanvasCommentEvent
}

// ReplayComments 从已授权的协作代数读取评论。省略游标时只返回当前水位。
func (r *CanvasCollaborationRepository) ReplayComments(ctx context.Context, user, canvas, epoch uint64, after *uint64, limit int) (CanvasCommentReplay, error) {
	var result CanvasCommentReplay
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := readCollaborationEditor(tx, user, canvas, epoch); err != nil {
			return err
		}
		query := tx.Table("canvas_collaboration_comments AS comment").Where("comment.canvas_id = ? AND comment.epoch = ?", canvas, epoch)
		if after == nil {
			return query.Select("COALESCE(MAX(comment.id), 0)").Scan(&result.Cursor).Error
		}
		result.Cursor = *after
		return query.Select("comment.*, node.record_id IS NOT NULL AS visible").
			Joins("LEFT JOIN canvas_collaboration_records AS node ON node.canvas_id = comment.canvas_id AND node.record_id = comment.node_id AND node.kind = 'node' AND NOT node.deleted").
			Where("comment.id > ?", *after).Order("comment.id ASC").Limit(limit).Scan(&result.Events).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, err
}

// CanvasMention identifies a discussion the current collaborator was invited to read.
type CanvasMention struct {
	ID     uint64
	NodeID string
}

// Mentions reads unread candidates from live discussions in the current collaboration epoch.
func (r *CanvasCollaborationRepository) Mentions(ctx context.Context, user, canvas, epoch, after uint64) ([]CanvasMention, error) {
	items := make([]CanvasMention, 0)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := readCollaborationEditor(tx, user, canvas, epoch); err != nil {
			return err
		}
		return tx.Table("canvas_collaboration_comments AS comment").Select("comment.id, comment.node_id").
			Joins("JOIN canvas_collaboration_records AS node ON node.canvas_id = comment.canvas_id AND node.record_id = comment.node_id AND node.kind = 'node' AND NOT node.deleted").
			Where("comment.canvas_id = ? AND comment.epoch = ? AND comment.id > ? AND comment.actor_id <> ?", canvas, epoch, after, user).
			Where("comment.mentions @> jsonb_build_array(?::bigint)", user).Order("comment.id ASC").Limit(20).Scan(&items).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return items, err
}

// CommentThread 按当前协作代数和节点读取编辑者可见的评论。
func (r *CanvasCollaborationRepository) CommentThread(ctx context.Context, user, canvas, epoch uint64, node string, before uint64) (CanvasCommentThread, error) {
	result := CanvasCommentThread{Rows: make([]model.CanvasCollaborationComment, 0)}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		project, err := readCollaborationEditor(tx, user, canvas, epoch)
		if err != nil {
			return err
		}
		if err := commentNodeExists(tx, canvas, node); err != nil {
			return err
		}
		members, err := collaborationCommentMembers(tx, project)
		if err != nil {
			return err
		}
		result.Members = members
		page := tx.Where("canvas_id = ? AND epoch = ? AND node_id = ?", canvas, epoch, node)
		if before != 0 {
			page = page.Where("id < ?", before)
		}
		return page.Order("id DESC").Limit(51).Find(&result.Rows).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, err
}

func readCollaborationEditor(tx *gorm.DB, user, canvas, epoch uint64) (*model.CanvasProject, error) {
	var project model.CanvasProject
	if err := tx.Select("id", "user_id", "collaboration_active", "collaboration_epoch", "revision").Where("id = ? AND drama_id IS NULL", canvas).Take(&project).Error; err != nil {
		return nil, translate(err)
	}
	role, err := collaborationRole(tx, &project, user)
	if err != nil {
		return nil, err
	}
	if role != model.CanvasRoleOwner && role != model.CanvasRoleEditor {
		return nil, ErrNotFound
	}
	if !project.CollaborationActive || project.CollaborationEpoch != epoch {
		return nil, ErrCanvasCollaborationSession
	}
	return &project, nil
}

func commentNodeExists(tx *gorm.DB, canvas uint64, node string) error {
	if !validCollaborationRecordID(node) {
		return ErrCanvasCollaborationInvalidBatch
	}
	var record model.CanvasCollaborationRecord
	return translate(tx.Select("record_id").Where("canvas_id = ? AND kind = 'node' AND record_id = ? AND NOT deleted", canvas, node).Take(&record).Error)
}

func collaborationCommentMembers(tx *gorm.DB, project *model.CanvasProject) ([]uint64, error) {
	ids := []uint64{project.UserID}
	var editors []uint64
	err := tx.Table("canvas_viewers v").Joins("JOIN users u ON u.id = v.user_id AND u.deleted_at IS NULL AND u.status = ?", model.UserStatusActive).
		Where("v.canvas_id = ? AND v.role = ?", project.ID, model.CanvasRoleEditor).Order("v.user_id").Pluck("v.user_id", &editors).Error
	return append(ids, editors...), err
}

// AddComment 保存一条评论，并在提交边界核对节点与当前成员角色。
func (r *CanvasCollaborationRepository) AddComment(ctx context.Context, user, canvas uint64, request model.CanvasCommentRequest) (model.CanvasCollaborationComment, error) {
	row, err := collaborationCommentInput(user, canvas, request)
	if err != nil {
		return row, err
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		project, _, err := lockEditableCollaboration(tx, canvas, user, request.Epoch)
		if err != nil {
			return err
		}
		if err := commentNodeExists(tx, canvas, row.NodeID); err != nil {
			return err
		}
		found, err := findCollaborationComment(tx, &row)
		if found || err != nil {
			return err
		}
		if err := validateCommentMentions(tx, project, row.Mentions); err != nil {
			return err
		}
		return tx.Create(&row).Error
	})
	return row, err
}

func collaborationCommentInput(user, canvas uint64, request model.CanvasCommentRequest) (model.CanvasCollaborationComment, error) {
	body := strings.TrimSpace(request.Body)
	if body == "" || utf8.RuneCountInString(body) > 2000 || strings.ContainsRune(body, 0) || !collaborationOpID.MatchString(request.OpID) || len(request.Mentions) > 10 {
		return model.CanvasCollaborationComment{}, ErrCanvasCollaborationInvalidBatch
	}
	mentions := make([]uint64, 0, len(request.Mentions))
	for _, mention := range request.Mentions {
		id, ok := idcodec.Decode(mention)
		if !ok || id == 0 || idcodec.Encode(id) != mention {
			return model.CanvasCollaborationComment{}, ErrCanvasCollaborationInvalidBatch
		}
		mentions = append(mentions, id)
	}
	slices.Sort(mentions)
	encoded, err := json.Marshal(slices.Compact(mentions))
	return model.CanvasCollaborationComment{CanvasID: canvas, Epoch: request.Epoch, NodeID: request.NodeID,
		ActorID: user, OpID: request.OpID, Body: body, Mentions: encoded}, err
}

func findCollaborationComment(tx *gorm.DB, comment *model.CanvasCollaborationComment) (bool, error) {
	var existing model.CanvasCollaborationComment
	err := tx.Where("canvas_id = ? AND epoch = ? AND actor_id = ? AND op_id = ?", comment.CanvasID, comment.Epoch, comment.ActorID, comment.OpID).Take(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if existing.NodeID != comment.NodeID || existing.Body != comment.Body || !equalCollaborationField(json.RawMessage(existing.Mentions), json.RawMessage(comment.Mentions)) {
		return true, ErrCanvasCollaborationOpID
	}
	*comment = existing
	return true, nil
}

func validateCommentMentions(tx *gorm.DB, project *model.CanvasProject, encoded []byte) error {
	var ids []uint64
	if err := json.Unmarshal(encoded, &ids); err != nil || len(ids) == 0 {
		return err
	}
	allowed, err := collaborationCommentMembers(tx, project)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if !slices.Contains(allowed, id) {
			return ErrCanvasCollaborationInvalidBatch
		}
	}
	return nil
}
