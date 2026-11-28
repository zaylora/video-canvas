package repository

import (
	"bytes"
	"encoding/json"
	"math"
	"slices"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"video-canvas/internal/model"
)

const (
	collaborationStoryboardField = "data.storyboard"
	collaborationShotAction      = "storyboard-shot"
	collaborationShotBaseline    = "$shots:baseline"
	collaborationShotOrder       = "$shots:order"
	collaborationShotNamespace   = "shots:"
)

type collaborationShotEdit struct {
	Action string                     `json:"action"`
	ID     string                     `json:"id"`
	Fields map[string]json.RawMessage `json:"fields"`
	Index  int                        `json:"index"`
}

type collaborationBoard struct {
	Shots []map[string]json.RawMessage `json:"shots"`
}

func readCollaborationShotEdit(raw json.RawMessage) (collaborationShotEdit, bool) {
	var edit collaborationShotEdit
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if !json.Valid(raw) || decoder.Decode(&edit) != nil || !validCollaborationRecordID(edit.ID) {
		return edit, false
	}
	switch edit.Action {
	case "add", "set":
		if edit.Index != 0 || (edit.Action == "set" && len(edit.Fields) == 0) {
			return edit, false
		}
		for field, value := range edit.Fields {
			if !validCollaborationShotField(field, value) {
				return edit, false
			}
		}
		return edit, true
	case "move":
		return edit, len(edit.Fields) == 0 && edit.Index > 0 && edit.Index <= 2000
	case "rm":
		return edit, edit.Index == 0 && len(edit.Fields) == 0
	default:
		return edit, false
	}
}

func validCollaborationShotField(field string, raw json.RawMessage) bool {
	switch field {
	case "scene", "shot", "camera", "action", "dialogue":
		return validCollaborationText(raw, 64000)
	case "duration":
		var duration float64
		return json.Unmarshal(raw, &duration) == nil && duration >= 1 && duration <= 60 && math.Round(duration*10) == duration*10
	case "imageNodeId", "videoNodeId":
		return bytes.Equal(raw, []byte("null")) || validCollaborationRecordID(collaborationString(raw))
	default:
		return false
	}
}

func readCollaborationBoard(raw json.RawMessage) (collaborationBoard, bool) {
	board := collaborationBoard{Shots: []map[string]json.RawMessage{}}
	if len(raw) != 0 && json.Unmarshal(raw, &board) != nil {
		return board, false
	}
	seen := make(map[string]bool, len(board.Shots))
	for _, shot := range board.Shots {
		id := collaborationString(shot["id"])
		if seen[id] || !validCollaborationRecordID(id) {
			return board, false
		}
		seen[id] = true
	}
	return board, true
}

func collaborationShotKey(id, field string) string {
	return "$shot:" + strconv.Itoa(len(id)) + ":" + id + ":" + field
}

func (fields *collaborationFields) trackStoryboardVersions(previous json.RawMessage, revision uint64) {
	current := fields.get(collaborationStoryboardField)
	if bytes.Equal(previous, current) {
		return
	}
	before, validBefore := readCollaborationBoard(previous)
	after, validAfter := readCollaborationBoard(current)
	if !validBefore || !validAfter {
		clearCollaborationRowVersions(fields.versions, collaborationShotNamespace)
		fields.versions[collaborationShotBaseline] = revision
		return
	}
	if _, tracked := fields.versions[collaborationShotBaseline]; !tracked {
		fields.versions[collaborationShotBaseline] = fields.previousBoardRev
	}
	if !slices.EqualFunc(before.Shots, after.Shots, func(first, second map[string]json.RawMessage) bool {
		return bytes.Equal(first["id"], second["id"])
	}) {
		fields.versions[collaborationShotOrder] = revision
	}
	trackCollaborationRows(fields.versions, collaborationShotNamespace, before.Shots, after.Shots, revision)
}

func trackCollaborationRows(versions map[string]uint64, namespace string, before, after []map[string]json.RawMessage, revision uint64) {
	old := make(map[string]map[string]json.RawMessage, len(before))
	for _, shot := range before {
		old[collaborationString(shot["id"])] = shot
	}
	for _, shot := range after {
		id := collaborationString(shot["id"])
		trackCollaborationShot(versions, namespace+id, old[id], shot, revision)
		delete(old, id)
	}
	if len(old) > 0 {
		for key := range versions {
			if id, ok := collaborationShotVersionID(key); ok && strings.HasPrefix(id, namespace) && old[strings.TrimPrefix(id, namespace)] != nil {
				delete(versions, key)
			}
		}
	}
}

func collaborationShotVersionID(key string) (string, bool) {
	key = strings.TrimPrefix(strings.TrimPrefix(key, "$undoWrite:"), "$undoOrigin:")
	tail, prefixed := strings.CutPrefix(key, "$shot:")
	length, content, found := strings.Cut(tail, ":")
	size, err := strconv.Atoi(length)
	if !prefixed || !found || err != nil || size <= 0 || size >= len(content) {
		return "", false
	}
	return content[:size], true
}

func clearCollaborationRowVersions(versions map[string]uint64, namespace string) {
	for key := range versions {
		if id, ok := collaborationShotVersionID(key); ok && strings.HasPrefix(id, namespace) {
			delete(versions, key)
		}
	}
}

func trackCollaborationShot(versions map[string]uint64, id string, before, after map[string]json.RawMessage, revision uint64) {
	if before == nil {
		versions[collaborationShotKey(id, "$created")] = revision
		return
	}
	track := func(field string) {
		if field != "id" && !equalCollaborationField(before[field], after[field]) {
			versions[collaborationShotKey(id, field)] = revision
			versions[collaborationShotKey(id, "$updated")] = revision
		}
	}
	for field := range before {
		track(field)
	}
	for field := range after {
		if before[field] == nil {
			track(field)
		}
	}
}

func (fields *collaborationFields) storyboardBaseline() uint64 {
	baseline, tracked := fields.versions[collaborationShotBaseline]
	if !tracked {
		baseline = fields.versions[collaborationStoryboardField]
	}
	return max(baseline, fields.versions["$restored"], fields.versions["$created"])
}

func (fields *collaborationFields) shotConflictRevision(edit collaborationShotEdit) uint64 {
	if edit.Action == "add" {
		return fields.storyboardBaseline()
	}
	id := collaborationShotNamespace + edit.ID
	version := max(fields.storyboardBaseline(), fields.versions[collaborationShotKey(id, "$created")])
	if edit.Action == "move" || edit.Action == "rm" {
		version = max(version, fields.versions[collaborationShotOrder])
	}
	if edit.Action == "rm" {
		version = max(version, fields.versions[collaborationShotKey(id, "$updated")])
	}
	for field := range edit.Fields {
		version = max(version, fields.versions[collaborationShotKey(id, field)])
		if field == "imageNodeId" {
			version = max(version, fields.versions[collaborationShotKey(id, "image")])
		}
	}
	return version
}

func resolveCollaborationShotEdit(tx *gorm.DB, record model.CanvasCollaborationRecord, change CanvasCollaborationChange, batch CanvasCollaborationBatch, result *CanvasCollaborationCommit) (CanvasCollaborationChange, bool, error) {
	fields, err := decodeCollaborationFields(record.Body, record.FieldVersions)
	if err != nil {
		return change, false, err
	}
	current := fields.get(collaborationStoryboardField)
	board, valid := readCollaborationBoard(current)
	if collaborationString(fields.data["kind"]) != "storyboard" || !valid {
		return change, false, ErrCanvasCollaborationInvalidBatch
	}
	edit, _ := readCollaborationShotEdit(change.Value)
	index := slices.IndexFunc(board.Shots, func(shot map[string]json.RawMessage) bool {
		return collaborationString(shot["id"]) == edit.ID
	})
	version := fields.shotConflictRevision(edit)
	if version > batch.BaseRevision || (index < 0) != (edit.Action == "add") || edit.Index > len(board.Shots) {
		result.Conflicts = append(result.Conflicts, CanvasCollaborationConflict{
			Kind: change.Kind, ID: change.ID, Field: collaborationStoryboardField, Revision: max(version, record.Revision), Value: current,
		})
		return change, false, nil
	}
	if err := validateCollaborationShotLinks(tx, record.CanvasID, edit.Fields, batch.Changes); err != nil {
		return change, false, err
	}
	resolved, accepted, err := mergeCollaborationShotEdit(change, board, edit, index, record.Revision)
	return resolved, accepted && !equalCollaborationField(current, resolved.Fields[collaborationStoryboardField]), err
}

func validateCollaborationShotLinks(tx *gorm.DB, canvasID uint64, fields map[string]json.RawMessage, changes []CanvasCollaborationChange) error {
	for _, kind := range []string{"image", "video"} {
		if id := collaborationString(fields[kind+"NodeId"]); id != "" {
			if slices.ContainsFunc(changes, func(change CanvasCollaborationChange) bool {
				return change.Kind == "node" && change.ID == id && change.Action == "delete"
			}) {
				return ErrCanvasCollaborationInvalidBatch
			}
			var count int64
			query := tx.Model(&model.CanvasCollaborationRecord{}).Where("canvas_id = ? AND kind = 'node' AND record_id = ? AND NOT deleted AND body #>> '{data,kind}' = ?", canvasID, id, kind)
			if err := query.Count(&count).Error; err != nil {
				return err
			}
			if count != 1 {
				return ErrCanvasCollaborationInvalidBatch
			}
		}
	}
	return nil
}

func mergeCollaborationShotEdit(change CanvasCollaborationChange, board collaborationBoard, edit collaborationShotEdit, index int, revision uint64) (CanvasCollaborationChange, bool, error) {
	switch edit.Action {
	case "add":
		var shot map[string]json.RawMessage
		_ = json.Unmarshal([]byte(`{"scene":"","shot":"中景","camera":"固定","action":"","dialogue":"","duration":3}`), &shot)
		shot["id"], _ = json.Marshal(edit.ID)
		patchCollaborationShot(shot, edit.Fields)
		board.Shots = append(board.Shots, shot)
	case "set":
		patchCollaborationShot(board.Shots[index], edit.Fields)
	case "rm":
		board.Shots = slices.Delete(board.Shots, index, index+1)
	case "move":
		shot := board.Shots[index]
		board.Shots = slices.Insert(slices.Delete(board.Shots, index, index+1), edit.Index-1, shot)
	}
	encoded, err := json.Marshal(board)
	if err != nil {
		return change, false, err
	}
	if len(encoded) > 512*1024 || len(board.Shots) > 2000 {
		return change, false, ErrCanvasCollaborationInvalidBatch
	}
	change.Action, change.Value = "", nil
	change.Fields = map[string]json.RawMessage{collaborationStoryboardField: encoded}
	change.FieldBases = map[string]uint64{collaborationStoryboardField: revision}
	return change, true, nil
}

func patchCollaborationShot(shot, patch map[string]json.RawMessage) {
	for field, value := range patch {
		setCollaborationValue(shot, field, value)
	}
	if collaborationString(patch["imageNodeId"]) != "" {
		delete(shot, "image")
	}
}
