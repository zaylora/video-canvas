package repository

import (
	"errors"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"video-canvas/internal/model"
)

func collaborationCandidatePrefix(task *model.GenerationTask) string {
	return "$candidate:" + strconv.FormatUint(task.UserID, 10) + ":"
}

func collaborationCandidateKey(task *model.GenerationTask) string {
	return collaborationCandidatePrefix(task) + strconv.FormatUint(task.ID, 10)
}

func collaborationCandidateAnchor(task *model.GenerationTask) string {
	return "$candidateBatch:" + strconv.FormatUint(task.UserID, 10)
}

func guardConfirmedCandidateInput(tx *gorm.DB, task *model.GenerationTask, recordRevision uint64, fields *collaborationFields, owner bool) error {
	if !matchesGenerationInput(task, fields, owner) {
		return ErrCanvasCollaborationInput
	}
	if recordRevision <= task.CollaborationInputRevision {
		return nil
	}
	same, err := sameCollaborationCandidateBatch(tx, task, fields)
	if err != nil {
		return err
	}
	if !same || !candidateInputsUnchanged(fields, task.CollaborationInputRevision) {
		return ErrCanvasCollaborationInput
	}
	return nil
}

func advancePrimaryCandidate(task *model.GenerationTask, fields *collaborationFields, patch map[string]any, revision uint64) {
	if patch["data.src"] == nil && collaborationString(fields.get("data.assetId")) == "" {
		promoteCanvasGenerationFallback(fields, patch)
	}
	finishPrimaryGenerationBinding(task, fields, patch)
	patch["data.taskId"] = nil
	if next := nextCollaborationCandidate(task, fields); next != "" {
		patch["data.status"], patch["data.taskId"], patch["data.generationRevision"] = "running", next, revision
	}
}

func candidateHistoryPatch(fields *collaborationFields, patch map[string]any, bound bool) map[string]any {
	if outputs, exists := patch["data.outputs"]; exists {
		if collaborationString(fields.get("data.status")) == "running" || collaborationString(fields.get("data.assetId")) != "" {
			return map[string]any{"data.outputs": outputs}
		}
		return patch
	}
	if bound {
		return map[string]any{}
	}
	return nil
}

func sameCollaborationCandidateBatch(tx *gorm.DB, task *model.GenerationTask, fields *collaborationFields) (bool, error) {
	anchor := fields.versions[collaborationCandidateAnchor(task)]
	if anchor == 0 || task.CollaborationRequestHash == "" {
		return false, nil
	}
	var first model.GenerationTask
	err := tx.Select("collaboration_request_hash", "collaboration_input_revision").Where("id = ? AND user_id = ?", anchor, task.UserID).Take(&first).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return first.CollaborationInputRevision == task.CollaborationInputRevision && first.CollaborationRequestHash == task.CollaborationRequestHash, err
}

func candidateInputsUnchanged(fields *collaborationFields, revision uint64) bool {
	for _, field := range []string{"$created", "$restored", "data.model", "data.prompt", "data.params", collaborationManualReferences, collaborationGenerationInputField, collaborationAssetField, "data.character"} {
		if fields.versions[field] > revision {
			if field == collaborationAssetField && fields.versions["data.outputs"] == fields.versions[field] {
				continue
			}
			return false
		}
	}
	return true
}

func bindCollaborationCandidate(tx *gorm.DB, task *model.GenerationTask, fields *collaborationFields) error {
	same, err := sameCollaborationCandidateBatch(tx, task, fields)
	if err != nil {
		return err
	}
	if !same {
		for key := range fields.versions {
			if strings.HasPrefix(key, collaborationCandidatePrefix(task)) {
				delete(fields.versions, key)
			}
		}
		delete(fields.versions, collaborationCandidateAnchor(task))
	}
	if task.CollaborationRequestHash != "" {
		fields.versions[collaborationCandidateAnchor(task)] = task.ID
		fields.versions[collaborationCandidateKey(task)] = task.CollaborationInputRevision
	}
	return nil
}

func nextCollaborationCandidate(task *model.GenerationTask, fields *collaborationFields) string {
	prefix := collaborationCandidatePrefix(task)
	var next uint64
	for key := range fields.versions {
		if suffix, found := strings.CutPrefix(key, prefix); found {
			id, _ := strconv.ParseUint(suffix, 10, 64)
			if id != 0 && (next == 0 || id < next) {
				next = id
			}
		}
	}
	if next == 0 {
		return ""
	}
	return strconv.FormatUint(next, 10)
}
