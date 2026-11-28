package repository

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"video-canvas/internal/model"
)

// CanvasCollaborationUndo 标识一次可重试的个人撤销。
type CanvasCollaborationUndo = model.CanvasCollaborationUndo

// Undo 撤销指定画布代数中当前成员的最近一笔编辑，检查每个字段的版本。
func (r *CanvasCollaborationRepository) Undo(ctx context.Context, actorID, canvasID uint64, req CanvasCollaborationUndo) (*CanvasCollaborationCommit, error) {
	if !collaborationOpID.MatchString(req.OpID) || req.Epoch == 0 {
		return nil, ErrCanvasCollaborationInvalidBatch
	}
	receipt := model.CanvasCollaborationReceipt{CanvasID: canvasID, ActorID: actorID, OpID: req.OpID, RequestHash: "undo"}
	if req.Redo {
		receipt.RequestHash = "redo"
	}
	if req.ExpectedRevision != 0 {
		receipt.RequestHash += ":" + strconv.FormatUint(req.ExpectedRevision, 10)
	}
	var result *CanvasCollaborationCommit
	var owner uint64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		project, _, err := lockEditableCollaboration(tx, canvasID, actorID, req.Epoch)
		if err != nil {
			return err
		}
		owner = project.UserID
		result, err = readCollaborationReceipt(tx, receipt)
		if err == nil && result != nil && result.Epoch != req.Epoch {
			result = nil
			return ErrCanvasCollaborationOpID
		}
		if err != nil || result != nil {
			return err
		}
		result = &CanvasCollaborationCommit{Epoch: req.Epoch, Revision: project.Revision, OpID: req.OpID, Redo: req.Redo}
		if err := applyPersonalUndo(tx, receipt, req.ExpectedRevision, result); err != nil {
			return err
		}
		return storeCollaborationReceipt(tx, receipt, result)
	})
	if err == nil && owner != actorID {
		err = projectGenerationEvent(canvasID, result.Changes)
	}
	return result, err
}

func applyPersonalUndo(tx *gorm.DB, receipt model.CanvasCollaborationReceipt, expectedRevision uint64, result *CanvasCollaborationCommit) error {
	var source model.CanvasCollaborationEvent
	query := personalCollaborationHistory(tx, receipt, result.Epoch)
	if result.Redo {
		query = query.Where("undo->>'direction' = 'redo'")
	} else {
		query = query.Where("jsonb_typeof(undo) = 'array' OR undo->>'direction' = 'undo'")
	}
	err := query.Select("revision", "undo").Order("revision DESC").Take(&source).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if expectedRevision != 0 && source.Revision != expectedRevision {
		result.Conflicts = []CanvasCollaborationConflict{{Field: "expectedRevision", Revision: source.Revision}}
		return nil
	}
	if source.Revision == 0 {
		return nil
	}
	result.Undoes = source.Revision
	history, err := decodeCollaborationHistory(source)
	if err != nil {
		return err
	}
	updated, opposite, err := reverseCollaborationChanges(tx, receipt.CanvasID, history, result)
	if err != nil {
		return err
	}
	if len(updated) > 0 {
		result.Revision++
		if err := persistCollaborationBatch(tx, updated, receipt, result, nil); err != nil {
			return err
		}
		if err := persistCollaborationHistory(tx, receipt, result, opposite); err != nil {
			return err
		}
	}
	return personalCollaborationHistory(tx, receipt, result.Epoch).Where("revision = ?", source.Revision).UpdateColumn("undo", nil).Error
}

func collaborationRecordIndex(tx *gorm.DB, canvasID uint64, changes []CanvasCollaborationChange) (map[[2]string]model.CanvasCollaborationRecord, error) {
	keys := make([][]any, 0, len(changes))
	for _, change := range changes {
		keys = append(keys, []any{change.Kind, change.ID})
	}
	var records []model.CanvasCollaborationRecord
	if err := tx.Where("canvas_id = ? AND (kind, record_id) IN ?", canvasID, keys).Find(&records).Error; err != nil {
		return nil, err
	}
	index := make(map[[2]string]model.CanvasCollaborationRecord, len(records))
	for _, record := range records {
		index[[2]string{record.Kind, record.RecordID}] = record
	}
	return index, nil
}

func collaborationInverse(before model.CanvasCollaborationRecord, change CanvasCollaborationChange) (CanvasCollaborationChange, error) {
	inverse := CanvasCollaborationChange{Kind: change.Kind, ID: change.ID, Action: change.Action}
	if change.Action == "delete" {
		inverse.Value = json.RawMessage(before.Body)
		return inverse, json.Unmarshal(before.FieldVersions, &inverse.FieldBases)
	}
	if change.Action != "" {
		return inverse, nil
	}
	fields, err := decodeCollaborationFields(before.Body, before.FieldVersions)
	if err != nil {
		return inverse, err
	}
	inverse.Fields = make(map[string]json.RawMessage, len(change.Fields))
	for field := range change.Fields {
		if field != collaborationGenerationInputField {
			inverse.Fields[field] = fields.get(field)
		}
	}
	if change.Fields[collaborationAssetField] != nil {
		for _, field := range collaborationImportFields {
			value := fields.get(field)
			if value == nil {
				value = json.RawMessage("null")
			}
			inverse.Fields[field] = value
		}
	}
	inverse.FieldBases = make(map[string]uint64, len(inverse.Fields))
	for field := range inverse.Fields {
		inverse.FieldBases[field] = fields.undoFieldRevision(field)
	}
	return inverse, nil
}

func reverseCollaborationChanges(tx *gorm.DB, canvasID uint64, history collaborationHistory, result *CanvasCollaborationCommit) ([]model.CanvasCollaborationRecord, collaborationHistory, error) {
	index, err := collaborationRecordIndex(tx, canvasID, history.Changes)
	if err != nil {
		return nil, history, err
	}
	links, err := undoCollaborationLinks(tx, canvasID, history.Changes)
	if err != nil {
		return nil, history, err
	}
	updated, err := reverseCollaborationRecords(index, links, history, result)
	if err != nil {
		return nil, history, err
	}
	opposite, err := oppositeCollaborationHistory(index, updated, history, result)
	return updated, opposite, err
}

func reverseCollaborationRecords(index map[[2]string]model.CanvasCollaborationRecord, links map[[2]string]collaborationLink, history collaborationHistory, result *CanvasCollaborationCommit) ([]model.CanvasCollaborationRecord, error) {
	updated, err := restoreCollaborationDeletions(index, links, history.Changes, result)
	if err != nil {
		return nil, err
	}
	var creations []model.CanvasCollaborationRecord
	for _, inverse := range history.Changes {
		if inverse.Action == "delete" {
			continue
		}
		key := [2]string{inverse.Kind, inverse.ID}
		record := index[key]
		if inverse.Action == "create" {
			eligible, err := canReverseCollaborationCreation(record, max(inverse.RecordBase, history.Origin))
			if err != nil {
				return nil, err
			}
			if eligible {
				creations = append(creations, record)
				continue
			}
		}
		applied, err := reverseCollaborationFields(&record, inverse, links, history, result)
		if err != nil {
			return nil, err
		}
		if applied {
			updated = append(updated, record)
			if links != nil {
				links[key] = collaborationRecordLink(record)
			}
		}
	}
	return append(updated, reverseCollaborationCreations(creations, links, result)...), nil
}

func undoCollaborationLinks(tx *gorm.DB, canvasID uint64, inverses []CanvasCollaborationChange) (map[[2]string]collaborationLink, error) {
	if !hasCollaborationStructure(inverses) {
		return nil, nil
	}
	live, err := liveCollaborationLinks(tx, canvasID)
	if err != nil {
		return nil, err
	}
	links := make(map[[2]string]collaborationLink, len(live))
	for _, link := range live {
		links[[2]string{link.Kind, link.RecordID}] = link
	}
	return links, nil
}

func canReverseCollaborationCreation(record model.CanvasCollaborationRecord, createdAt uint64) (bool, error) {
	if record.Deleted || record.RecordID == "" {
		return false, nil
	}
	if record.Revision == createdAt {
		return true, nil
	}
	fields, err := decodeCollaborationFields(record.Body, record.FieldVersions)
	if err != nil || fields.versions["$created"] != createdAt {
		return false, err
	}
	for field := range fields.versions {
		if !strings.HasPrefix(field, "$") && fields.undoFieldRevision(field) > createdAt {
			return false, nil
		}
	}
	return true, nil
}

func reverseCollaborationCreations(creations []model.CanvasCollaborationRecord, links map[[2]string]collaborationLink, result *CanvasCollaborationCommit) []model.CanvasCollaborationRecord {
	var removed []model.CanvasCollaborationRecord
	for _, kind := range []string{"edge", "node", "group"} {
		for _, record := range creations {
			if record.Kind != kind {
				continue
			}
			if isCollaborativeCreationReferenced(record, links) {
				result.Skipped = append(result.Skipped, CanvasCollaborationConflict{Kind: kind, ID: record.RecordID, Revision: record.Revision})
				continue
			}
			delete(links, [2]string{kind, record.RecordID})
			markCollaborationDeleted(&record, result.Revision+1)
			removed = append(removed, record)
			result.Changes = append(result.Changes, CanvasCollaborationChange{Kind: kind, ID: record.RecordID, Action: "delete"})
		}
	}
	return removed
}

func isCollaborativeCreationReferenced(record model.CanvasCollaborationRecord, links map[[2]string]collaborationLink) bool {
	for _, link := range links {
		switch record.Kind {
		case "node":
			if link.Source == record.RecordID || link.Target == record.RecordID {
				return true
			}
		case "group":
			if link.Parent == record.RecordID {
				return true
			}
		}
	}
	return false
}

func reverseCollaborationFields(record *model.CanvasCollaborationRecord, inverse CanvasCollaborationChange, links map[[2]string]collaborationLink, history collaborationHistory, result *CanvasCollaborationCommit) (bool, error) {
	if isCollaborationObjectAction(inverse.Action) && !record.Deleted && record.RecordID != "" {
		return reverseCollaborationObject(record, inverse, history, result)
	}
	if inverse.Action != "" || record.Deleted || record.RecordID == "" {
		result.Skipped = append(result.Skipped, CanvasCollaborationConflict{Kind: inverse.Kind, ID: inverse.ID, Revision: record.Revision})
		return false, nil
	}
	fields, err := decodeCollaborationFields(record.Body, record.FieldVersions)
	if err != nil {
		return false, err
	}
	accepted := make(map[string]json.RawMessage)
	expected := func(field string) uint64 {
		if result.Redo {
			return inverse.FieldBases[field]
		}
		return history.Origin
	}
	blockedMove := undoCollaborationMoveBlocked(fields, inverse, links, expected("parentId"), expected("position"))
	isImport := inverse.Fields[collaborationAssetField] != nil
	blockedImport := isImport && slices.ContainsFunc(collaborationImportFields, func(field string) bool {
		return fields.undoFieldRevision(field) != expected(field)
	})
	for _, field := range slices.Sorted(maps.Keys(inverse.Fields)) {
		value := inverse.Fields[field]
		if fields.undoFieldRevision(field) != expected(field) || (blockedMove && (field == "parentId" || field == "position")) ||
			(blockedImport && slices.Contains(collaborationImportFields, field)) ||
			(field == collaborationOutputField && !isImport && !fields.selectCollaborationOutput(value)) {
			result.Skipped = append(result.Skipped, CanvasCollaborationConflict{Kind: inverse.Kind, ID: inverse.ID, Field: field, Revision: fields.versions[field]})
			continue
		}
		origin, known := inverse.FieldBases[field]
		if result.Redo {
			origin, known = history.Origin, true
		}
		fields.reverseField(field, value, result.Revision+1, origin, known)
		accepted[field] = value
	}
	if len(accepted) == 0 {
		return false, nil
	}
	inverse.Fields = accepted
	inverse.FieldBases = nil
	result.Changes = append(result.Changes, inverse)
	return true, fields.save(record, result.Revision+1)
}

func (fields *collaborationFields) reverseField(field string, value json.RawMessage, revision, origin uint64, known bool) {
	fields.set(field, value)
	fields.versions[field] = revision
	if known {
		fields.versions["$undoWrite:"+field] = revision
		fields.versions["$undoOrigin:"+field] = origin
	}
}

func (fields *collaborationFields) undoFieldRevision(field string) uint64 {
	version := fields.versions[field]
	if write, known := fields.versions["$undoWrite:"+field]; known && write == version {
		return fields.versions["$undoOrigin:"+field]
	}
	return version
}

func undoCollaborationMoveBlocked(fields *collaborationFields, inverse CanvasCollaborationChange, links map[[2]string]collaborationLink, parentVersion, positionVersion uint64) bool {
	value, moved := inverse.Fields["parentId"]
	if !moved {
		return fields.undoFieldRevision("parentId") > positionVersion
	}
	parent := collaborationString(value)
	return fields.undoFieldRevision("parentId") != parentVersion || fields.undoFieldRevision("position") > positionVersion ||
		(parent != "" && links[[2]string{"group", parent}].Kind != "group")
}
