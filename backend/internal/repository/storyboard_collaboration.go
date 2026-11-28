package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"video-canvas/internal/model"
	"video-canvas/internal/storyboard"
)

func guardSplitCanvas(tx *gorm.DB, job *model.StoryboardSplitJob) (*model.CanvasProject, error) {
	if job.CollaborationEpoch == 0 {
		return nil, lockTaskCanvas(tx, job.UserID, job.CanvasID)
	}
	project, _, err := lockEditableCollaboration(tx, job.CanvasID, job.UserID, job.CollaborationEpoch)
	if err != nil {
		return nil, err
	}
	allowed, err := canGenerateInCollaboration(tx, project, job.UserID)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrNotFound
	}
	record, fields, err := generationCanvasNode(tx, project.ID, job.NodeID)
	if err != nil {
		return nil, err
	}
	input := confirmedGenerationInput{Actor: job.UserID, Model: "storyboard-split", Input: map[string]any{"script": strings.TrimSpace(job.Script), "mode": job.Mode}}
	if (job.Mode != "replace" && job.Mode != "append") || collaborationString(fields.get("data.kind")) != "storyboard" ||
		record.Revision > job.CollaborationRevision || job.CollaborationRevision > project.Revision ||
		collaborationString(fields.get(collaborationGenerationInputField)) != generationInputDigest(input) {
		return nil, ErrCanvasCollaborationInput
	}
	return project, nil
}

func bindCollaborationSplit(tx *gorm.DB, project *model.CanvasProject, job *model.StoryboardSplitJob) error {
	record, fields, err := generationCanvasNode(tx, project.ID, job.NodeID)
	if err != nil {
		return err
	}
	fields.versions["$storyboardSplit"] = job.ID
	return persistCanvasTaskFields(tx, project.CollaborationEpoch, project.Revision, &record, fields, map[string]any{"data.storyboardSplitRunning": true})
}

// CheckCollaborationSplit 校验排队任务仍属于当前协作代数，并且发起者仍可生成。
func (r *StoryboardRepository) CheckCollaborationSplit(ctx context.Context, job *model.StoryboardSplitJob) error {
	if job.CollaborationEpoch == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		project, _, err := lockEditableCollaboration(tx, job.CanvasID, job.UserID, job.CollaborationEpoch)
		if err != nil {
			return err
		}
		return requireCanvasGeneration(tx, project, job.UserID)
	})
}

// FinishCollaborationSplit 写入确认版本，或在任务失败时清除进行中状态。
func (r *StoryboardRepository) FinishCollaborationSplit(ctx context.Context, job *model.StoryboardSplitJob, shots []model.StoryboardShot) error {
	tx := r.db.WithContext(ctx)
	project, err := lockGenerationCollaboration(tx, &model.GenerationTask{CanvasProjectID: &job.CanvasID, CollaborationEpoch: job.CollaborationEpoch})
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrCanvasCollaborationSession) {
		if shots != nil {
			return ErrCanvasCollaborationSession
		}
		return nil
	}
	if err != nil {
		return err
	}
	record, fields, err := generationCanvasNode(tx, project.ID, job.NodeID)
	if errors.Is(err, ErrNotFound) || (err == nil && fields.versions["$storyboardSplit"] != job.ID) {
		if shots != nil {
			return ErrCanvasCollaborationInput
		}
		return nil
	}
	if err != nil {
		return err
	}
	patch := map[string]any{"data.storyboardSplitRunning": nil}
	if shots != nil {
		if err := collaborationSplitPatch(tx, project, job, fields, shots, patch); err != nil {
			return err
		}
	}
	delete(fields.versions, "$storyboardSplit")
	return persistCanvasTaskFields(tx, project.CollaborationEpoch, project.Revision, &record, fields, patch)
}

func collaborationSplitPatch(tx *gorm.DB, project *model.CanvasProject, job *model.StoryboardSplitJob, fields *collaborationFields, shots []model.StoryboardShot, patch map[string]any) error {
	allowed, err := canGenerateInCollaboration(tx, project, job.UserID)
	if err != nil {
		return err
	}
	if !allowed || (job.Mode == "replace" && fields.versions["data.storyboard"] > job.CollaborationRevision) {
		return ErrCanvasCollaborationInput
	}
	var merged []json.RawMessage
	if job.Mode == "append" {
		var existing struct{ Shots []json.RawMessage }
		if raw := fields.get("data.storyboard"); len(raw) > 0 && json.Unmarshal(raw, &existing) != nil {
			return ErrCanvasCollaborationInvalidDocument
		}
		merged = existing.Shots
	}
	if len(merged)+len(shots) > storyboard.MaxShots {
		return ErrCanvasCollaborationInput
	}
	for index, shot := range shots {
		encoded, err := json.Marshal(struct {
			ID string `json:"id"`
			model.StoryboardShot
		}{"split-" + strconv.FormatUint(job.ID, 10) + "-" + strconv.Itoa(index+1), shot})
		if err != nil {
			return err
		}
		merged = append(merged, encoded)
	}
	patch["data.storyboard"] = map[string]any{"shots": merged}
	return nil
}
