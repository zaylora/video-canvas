package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
)

// CanvasComposeEnqueuer enqueues a worker in the same transaction as its job row.
type CanvasComposeEnqueuer interface {
	EnqueueCanvasComposeTx(context.Context, *sql.Tx, uint64) error
}

// ErrCanvasComposeBusy indicates that the actor's composition slots are occupied.
var ErrCanvasComposeBusy = errors.New("当前画布最多同时提交两个成片任务")

// SubmitCompose saves the authorized ordered inputs and queues the renderer.
func (r *CanvasCollaborationRepository) SubmitCompose(ctx context.Context, actor, canvas uint64, req model.CanvasComposeReq, enqueue CanvasComposeEnqueuer) (*model.CanvasComposeJob, error) {
	if !collaborationOpID.MatchString(req.OpID) || req.Revision == 0 || len(req.Nodes) == 0 || len(req.Nodes) > 24 {
		return nil, ErrCanvasCollaborationInvalidBatch
	}
	encoded, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	job := &model.CanvasComposeJob{CanvasID: canvas, UserID: actor, Epoch: req.Epoch, Revision: req.Revision, OpID: req.OpID,
		RequestHash: fmt.Sprintf("%x", sha256.Sum256(encoded)), Status: model.TaskPending}
	err = r.WithTx(ctx, func(tx *gorm.DB) error {
		project, _, err := lockEditableCollaboration(tx, canvas, actor, req.Epoch)
		if err != nil {
			return err
		}
		previous, err := composeRequest(tx, job)
		if err != nil {
			return err
		}
		if previous != nil {
			job = previous
			return nil
		}
		if project.Revision != req.Revision {
			return ErrCanvasMediaDraftStale
		}
		return enqueueCanvasCompose(ctx, tx, project, job, req.Nodes, enqueue)
	})
	return job, err
}

func composeRequest(tx *gorm.DB, job *model.CanvasComposeJob) (*model.CanvasComposeJob, error) {
	var previous model.CanvasComposeJob
	err := tx.Where("canvas_id = ? AND user_id = ? AND op_id = ?", job.CanvasID, job.UserID, job.OpID).Take(&previous).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err == nil && previous.RequestHash != job.RequestHash {
		return nil, ErrCanvasCollaborationOpID
	}
	return &previous, err
}

func enqueueCanvasCompose(ctx context.Context, tx *gorm.DB, project *model.CanvasProject, job *model.CanvasComposeJob, nodes []string, enqueue CanvasComposeEnqueuer) error {
	var count int64
	if err := tx.Model(&model.CanvasComposeJob{}).Where("canvas_id = ? AND user_id = ? AND epoch = ? AND status IN ?", project.ID, job.UserID, job.Epoch, []string{model.TaskPending, model.TaskRunning}).Count(&count).Error; err != nil {
		return err
	}
	if count >= 2 {
		return ErrCanvasComposeBusy
	}
	clips, err := readComposeInputs(tx, project, nodes, job.Revision)
	if err != nil {
		return err
	}
	job.Inputs, err = json.Marshal(clips)
	if err != nil {
		return err
	}
	if err := tx.Create(job).Error; err != nil {
		return err
	}
	conn, ok := tx.Statement.ConnPool.(*sql.Tx)
	if !ok || enqueue == nil {
		return errors.New("成片队列未配置")
	}
	return enqueue.EnqueueCanvasComposeTx(ctx, conn, job.ID)
}

func readComposeInputs(tx *gorm.DB, project *model.CanvasProject, nodes []string, revision uint64) ([]model.CanvasComposeClip, error) {
	clips := make([]model.CanvasComposeClip, 0, len(nodes))
	for _, id := range nodes {
		if !validCollaborationRecordID(id) {
			return nil, ErrCanvasCollaborationInvalidBatch
		}
		_, fields, err := generationCanvasNode(tx, project.ID, id)
		if err != nil {
			return nil, err
		}
		asset := collaborationAssetID(fields.get(collaborationAssetField))
		if asset == 0 || collaborationString(fields.get("data.kind")) != "video" {
			return nil, ErrCanvasCollaborationInvalidBatch
		}
		if _, err := mediaDraftSource(tx, project, model.CanvasMediaDraftReq{Revision: revision, SourceNode: id, SourceAsset: asset}); err != nil {
			return nil, err
		}
		clips = append(clips, model.CanvasComposeClip{Node: id, Asset: asset})
	}
	return clips, nil
}

// ComposeJob reads or cancels an actor's own composition job under current canvas authorization.
func (r *CanvasCollaborationRepository) ComposeJob(ctx context.Context, actor, canvas, epoch, id uint64, cancel bool) (*model.CanvasComposeJob, error) {
	var job model.CanvasComposeJob
	err := r.WithTx(ctx, func(tx *gorm.DB) error {
		if err := authorizeComposeJob(tx, actor, canvas, epoch, cancel); err != nil {
			return err
		}
		query := tx.Where("id = ? AND canvas_id = ? AND user_id = ? AND epoch = ?", id, canvas, actor, epoch)
		if cancel {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.Take(&job).Error; err != nil {
			return translate(err)
		}
		if cancel && !model.IsTerminalStatus(job.Status) {
			job.Status = model.TaskCanceled
			return tx.Model(&job).Update("status", job.Status).Error
		}
		return nil
	})
	return &job, err
}

func authorizeComposeJob(tx *gorm.DB, actor, canvas, epoch uint64, cancel bool) error {
	if cancel {
		_, _, err := lockEditableCollaboration(tx, canvas, actor, epoch)
		return err
	}
	_, err := readCollaborationEditor(tx, actor, canvas, epoch)
	return err
}

// ClaimCompose starts an attempt or returns nil if the job is already settled or superseded.
func (r *CanvasCollaborationRepository) ClaimCompose(ctx context.Context, id uint64, attempt int) (*model.CanvasComposeJob, error) {
	var job model.CanvasComposeJob
	err := r.WithTx(ctx, func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).Take(&job).Error; err != nil {
			return translate(err)
		}
		if model.IsTerminalStatus(job.Status) || job.Attempt >= attempt {
			job.ID = 0
			return nil
		}
		job.Status, job.Attempt = model.TaskRunning, attempt
		return tx.Model(&job).Updates(map[string]any{"status": job.Status, "attempt": attempt}).Error
	})
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil || job.ID == 0 {
		return nil, err
	}
	return &job, nil
}

// CheckComposeSources rechecks edit access and all input bindings before reading any object.
func (r *CanvasCollaborationRepository) CheckComposeSources(ctx context.Context, job *model.CanvasComposeJob, clips []model.CanvasComposeClip) (uint64, error) {
	var owner uint64
	err := r.WithTx(ctx, func(tx *gorm.DB) error {
		project, _, err := lockEditableCollaboration(tx, job.CanvasID, job.UserID, job.Epoch)
		if err != nil {
			return err
		}
		owner = project.UserID
		return verifyComposeSources(tx, project, job, clips)
	})
	return owner, err
}

func verifyComposeSources(tx *gorm.DB, project *model.CanvasProject, job *model.CanvasComposeJob, clips []model.CanvasComposeClip) error {
	for _, clip := range clips {
		fields, err := mediaDraftSource(tx, project, model.CanvasMediaDraftReq{Revision: job.Revision, SourceNode: clip.Node, SourceAsset: clip.Asset})
		if err != nil {
			return err
		}
		if collaborationString(fields.get("data.kind")) != "video" {
			return ErrCanvasMediaDraftStale
		}
	}
	return nil
}

// FailCompose closes a worker attempt if cancellation or a newer attempt has not won.
func (r *CanvasCollaborationRepository) FailCompose(ctx context.Context, job *model.CanvasComposeJob, message string) error {
	return r.db.WithContext(ctx).Model(&model.CanvasComposeJob{}).Where("id = ? AND attempt = ? AND status = ?", job.ID, job.Attempt, model.TaskRunning).
		Updates(map[string]any{"status": model.TaskFailed, "error": message}).Error
}

// FinishCompose publishes the asset, provenance links, and terminal status atomically.
func (r *CanvasCollaborationRepository) FinishCompose(ctx context.Context, job *model.CanvasComposeJob, clips []model.CanvasComposeClip, stage model.CanvasGenerationMedia, fileURL func(string) string) (*CanvasCollaborationCommit, error) {
	var result *CanvasCollaborationCommit
	err := r.WithTx(ctx, func(tx *gorm.DB) error {
		project, _, err := lockEditableCollaboration(tx, job.CanvasID, job.UserID, job.Epoch)
		if err != nil {
			return err
		}
		var current model.CanvasComposeJob
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND attempt = ? AND status = ?", job.ID, job.Attempt, model.TaskRunning).Take(&current).Error; err != nil {
			return translate(err)
		}
		if err := verifyComposeSources(tx, project, job, clips); err != nil {
			return err
		}
		result, err = publishComposition(tx, project, job, clips, stage, fileURL)
		return err
	})
	return result, err
}

func publishComposition(tx *gorm.DB, project *model.CanvasProject, job *model.CanvasComposeJob, clips []model.CanvasComposeClip, stage model.CanvasGenerationMedia, fileURL func(string) string) (*CanvasCollaborationCommit, error) {
	if _, err := attachCollaborationMedia(tx, project.UserID, []model.CanvasGenerationMedia{stage}); err != nil {
		return nil, err
	}
	if stage.Asset == nil || stage.Asset.ID == 0 {
		return nil, ErrCanvasCollaborationInvalidBatch
	}
	suffix := "compose-" + stage.Attempt
	first := clips[0]
	_, fields, err := generationCanvasNode(tx, project.ID, first.Node)
	if err != nil {
		return nil, err
	}
	source := &CanvasCollaborationMediaSource{NodeID: first.Node, AssetID: strconv.FormatUint(first.Asset, 10), Revision: strconv.FormatUint(job.Revision, 10)}
	nodes := make([]string, 0, len(clips))
	for _, clip := range clips {
		nodes = append(nodes, clip.Node)
	}
	receipt := model.CanvasCollaborationReceipt{CanvasID: project.ID, ActorID: job.UserID, OpID: "compose:" + strconv.FormatUint(job.ID, 36), RequestHash: job.RequestHash}
	result, err := commitMediaDraft(tx, project, receipt, fields, stage.Asset, source, suffix, fileURL, nodes)
	if err != nil {
		return nil, err
	}
	err = tx.Model(job).Updates(map[string]any{"status": model.TaskSucceeded, "asset_id": stage.Asset.ID, "result_node": "media-" + suffix}).Error
	return result, err
}
