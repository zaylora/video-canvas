package repository

import (
	"encoding/json"
	"slices"
	"strings"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"video-canvas/internal/model"
)

type collaborationHistory struct {
	Direction string                      `json:"direction"`
	Origin    uint64                      `json:"origin"`
	Changes   []CanvasCollaborationChange `json:"changes"`
}

func personalCollaborationHistory(tx *gorm.DB, receipt model.CanvasCollaborationReceipt, epoch uint64) *gorm.DB {
	return tx.Model(&model.CanvasCollaborationEvent{}).Where("canvas_id = ? AND epoch = ? AND actor_id = ? AND undo IS NOT NULL", receipt.CanvasID, epoch, receipt.ActorID)
}

func clearCollaborationRedo(tx *gorm.DB, receipt model.CanvasCollaborationReceipt, epoch uint64) error {
	return personalCollaborationHistory(tx, receipt, epoch).Where("undo->>'direction' = 'redo'").UpdateColumn("undo", nil).Error
}

func decodeCollaborationHistory(event model.CanvasCollaborationEvent) (collaborationHistory, error) {
	history := collaborationHistory{Origin: event.Revision}
	if len(event.Undo) > 0 && event.Undo[0] == '[' {
		return history, json.Unmarshal(event.Undo, &history.Changes)
	}
	return history, json.Unmarshal(event.Undo, &history)
}

func persistCollaborationHistory(tx *gorm.DB, receipt model.CanvasCollaborationReceipt, result *CanvasCollaborationCommit, history collaborationHistory) error {
	encoded, err := json.Marshal(history)
	if err != nil {
		return err
	}
	return tx.Model(&model.CanvasCollaborationEvent{}).Where("canvas_id = ? AND revision = ?", receipt.CanvasID, result.Revision).UpdateColumn("undo", datatypes.JSON(encoded)).Error
}

func appendCollaborationDeletionUndo(tx *gorm.DB, canvasID uint64, undo, changes []CanvasCollaborationChange) ([]CanvasCollaborationChange, error) {
	if len(changes) == 0 {
		return undo, nil
	}
	index, err := collaborationRecordIndex(tx, canvasID, changes)
	if err != nil {
		return nil, err
	}
	for _, change := range changes {
		inverse, err := collaborationInverse(index[[2]string{change.Kind, change.ID}], change)
		if err != nil {
			return nil, err
		}
		undo = append(undo, inverse)
	}
	return undo, nil
}

func oppositeCollaborationHistory(before map[[2]string]model.CanvasCollaborationRecord, records []model.CanvasCollaborationRecord, source collaborationHistory, result *CanvasCollaborationCommit) (collaborationHistory, error) {
	next := collaborationHistory{Direction: "redo", Origin: source.Origin}
	if result.Redo {
		next.Direction = "undo"
	}
	after := make(map[[2]string]model.CanvasCollaborationRecord, len(records))
	for _, record := range records {
		after[[2]string{record.Kind, record.RecordID}] = record
	}
	for _, change := range result.Changes {
		key := [2]string{change.Kind, change.ID}
		if index := slices.IndexFunc(source.Changes, func(candidate CanvasCollaborationChange) bool {
			return candidate.Kind == change.Kind && candidate.ID == change.ID && isCollaborationObjectAction(candidate.Action)
		}); index != -1 {
			inverse, err := oppositeCollaborationObject(before[key], after[key], source.Changes[index], result)
			if err != nil {
				return next, err
			}
			next.Changes = append(next.Changes, inverse)
			continue
		}
		inverse, err := collaborationInverse(before[key], change)
		if err != nil {
			return next, err
		}
		if change.Action != "" {
			inverse.RecordBase = result.Revision + 1
		} else if !result.Redo {
			fields, err := decodeCollaborationFields(after[key].Body, after[key].FieldVersions)
			if err != nil {
				return next, err
			}
			for field := range inverse.Fields {
				inverse.FieldBases[field] = fields.undoFieldRevision(field)
			}
		}
		next.Changes = append(next.Changes, inverse)
	}
	return next, nil
}

func restoreCollaborationDeletions(index map[[2]string]model.CanvasCollaborationRecord, links map[[2]string]collaborationLink, inverses []CanvasCollaborationChange, result *CanvasCollaborationCommit) ([]model.CanvasCollaborationRecord, error) {
	var restored []model.CanvasCollaborationRecord
	ordinal := 0
	for _, link := range links {
		ordinal = max(ordinal, link.Ordinal)
	}
	for _, kind := range []string{"group", "node", "edge"} {
		for _, inverse := range inverses {
			if inverse.Kind != kind || inverse.Action != "delete" {
				continue
			}
			key := [2]string{kind, inverse.ID}
			record := index[key]
			ok, err := restoreCollaborationRecord(&record, inverse, links, result)
			if err != nil {
				return nil, err
			}
			if ok {
				ordinal++
				record.Ordinal = ordinal
				restored = append(restored, record)
				links[key] = collaborationRecordLink(record)
			}
		}
	}
	return restored, nil
}

func restoreCollaborationRecord(record *model.CanvasCollaborationRecord, inverse CanvasCollaborationChange, links map[[2]string]collaborationLink, result *CanvasCollaborationCommit) (bool, error) {
	candidate := *record
	candidate.Body = datatypes.JSON(inverse.Value)
	if !record.Deleted || record.Revision != max(result.Undoes, inverse.RecordBase) || len(inverse.Value) == 0 || !canRestoreCollaborationLink(collaborationRecordLink(candidate), links) {
		result.Skipped = append(result.Skipped, CanvasCollaborationConflict{Kind: inverse.Kind, ID: inverse.ID, Revision: record.Revision})
		return false, nil
	}
	versions, err := json.Marshal(inverse.FieldBases)
	if err != nil {
		return false, err
	}
	fields, err := decodeCollaborationFields(inverse.Value, versions)
	if err != nil {
		return false, err
	}
	if inverse.Kind == "node" {
		resetRestoredCollaborationTasks(fields)
	}
	if fields.versions == nil {
		fields.versions = make(map[string]uint64)
	}
	fields.versions["$restored"] = result.Revision + 1
	candidate.Deleted = false
	if err := fields.save(&candidate, result.Revision+1); err != nil {
		return false, err
	}
	*record = candidate
	result.Changes = append(result.Changes, CanvasCollaborationChange{Kind: record.Kind, ID: record.RecordID, Action: "create", Value: json.RawMessage(record.Body)})
	return true, nil
}

func canRestoreCollaborationLink(link collaborationLink, links map[[2]string]collaborationLink) bool {
	if link.Kind != "edge" {
		other := "group"
		if link.Kind == "group" {
			other = "node"
		}
		if links[[2]string{other, link.RecordID}].Kind != "" {
			return false
		}
		return link.Parent == "" || links[[2]string{"group", link.Parent}].Kind == "group"
	}
	if links[[2]string{"node", link.Source}].Kind != "node" || links[[2]string{"node", link.Target}].Kind != "node" {
		return false
	}
	for _, live := range links {
		if live.Kind == "edge" && live.Source == link.Source && live.Target == link.Target {
			return false
		}
	}
	return true
}

func resetRestoredCollaborationTasks(fields *collaborationFields) {
	for key := range fields.versions {
		if strings.HasPrefix(key, "$generation:") || strings.HasPrefix(key, "$candidate:") || strings.HasPrefix(key, "$candidateBatch:") {
			delete(fields.versions, key)
		}
	}
	for _, key := range []string{"taskId", "pendingTaskIds", "generationRevision", "generationInput", "voiceTaskId", "characterImageTaskId", "characterImageAssetId", "storyboardSplitRunning"} {
		delete(fields.data, key)
	}
	if collaborationString(fields.data["status"]) == "running" {
		fields.set("data.status", json.RawMessage(`"idle"`))
		if collaborationAssetID(fields.data["assetId"]) != 0 {
			fields.set("data.status", json.RawMessage(`"done"`))
		}
	}
}
