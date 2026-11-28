package repository

import (
	"bytes"
	"encoding/json"
	"slices"

	"video-canvas/internal/model"
)

const collaborationDirectorAction = "director-object"
const collaborationSceneField = "data.scene"
const collaborationSceneBaseline = "$scene:baseline"

var collaborationSceneCollections = []string{"characters", "props", "cameras"}

type collaborationDirectorEdit struct {
	Action     string                     `json:"action"`
	Collection string                     `json:"collection"`
	ID         string                     `json:"id"`
	Fields     map[string]json.RawMessage `json:"fields"`
}

func readCollaborationDirectorEdit(raw json.RawMessage) (collaborationDirectorEdit, bool) {
	var edit collaborationDirectorEdit
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if !json.Valid(raw) || decoder.Decode(&edit) != nil || !validCollaborationRecordID(edit.ID) || !slices.Contains(collaborationSceneCollections, edit.Collection) {
		return edit, false
	}
	if edit.Action == "rm" {
		return edit, len(edit.Fields) == 0
	}
	if (edit.Action != "add" && edit.Action != "set") || len(edit.Fields) == 0 || edit.Fields["id"] != nil {
		return edit, false
	}
	return edit, true
}

func directorRows(scene map[string]json.RawMessage, collection string) ([]map[string]json.RawMessage, bool) {
	var rows []map[string]json.RawMessage
	if json.Unmarshal(scene[collection], &rows) != nil || rows == nil {
		return nil, false
	}
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		id := collaborationString(row["id"])
		if !validCollaborationRecordID(id) || seen[id] {
			return nil, false
		}
		seen[id] = true
	}
	return rows, true
}

func (fields *collaborationFields) trackDirectorVersions(revision uint64) {
	current := fields.get(collaborationSceneField)
	if bytes.Equal(fields.previousScene, current) {
		return
	}
	var before, after map[string]json.RawMessage
	_ = json.Unmarshal(fields.previousScene, &before)
	_ = json.Unmarshal(current, &after)
	for _, collection := range collaborationSceneCollections {
		namespace := collection + ":"
		previous, validBefore := directorRows(before, collection)
		latest, validAfter := directorRows(after, collection)
		if !validBefore || !validAfter {
			clearCollaborationRowVersions(fields.versions, namespace)
			fields.versions[collaborationSceneBaseline] = revision
			continue
		}
		trackCollaborationRows(fields.versions, namespace, previous, latest, revision)
	}
	if _, tracked := fields.versions[collaborationSceneBaseline]; !tracked {
		fields.versions[collaborationSceneBaseline] = fields.previousSceneRev
	}
}

func (fields *collaborationFields) directorConflictRevision(edit collaborationDirectorEdit) uint64 {
	baseline, tracked := fields.versions[collaborationSceneBaseline]
	if !tracked {
		baseline = fields.versions[collaborationSceneField]
	}
	id := edit.Collection + ":" + edit.ID
	version := max(baseline, fields.versions["$created"], fields.versions["$restored"], fields.versions[collaborationShotKey(id, "$created")])
	if edit.Action == "rm" {
		version = max(version, fields.versions[collaborationShotKey(id, "$updated")])
	}
	for field := range edit.Fields {
		version = max(version, fields.versions[collaborationShotKey(id, field)])
	}
	return version
}

func resolveCollaborationDirectorEdit(record model.CanvasCollaborationRecord, change CanvasCollaborationChange, base uint64, result *CanvasCollaborationCommit) (CanvasCollaborationChange, bool, error) {
	fields, err := decodeCollaborationFields(record.Body, record.FieldVersions)
	if err != nil {
		return change, false, err
	}
	current := fields.get(collaborationSceneField)
	var scene map[string]json.RawMessage
	if collaborationString(fields.data["kind"]) != "director" || json.Unmarshal(current, &scene) != nil {
		return change, false, ErrCanvasCollaborationInvalidBatch
	}
	edit, _ := readCollaborationDirectorEdit(change.Value)
	rows, valid := directorRows(scene, edit.Collection)
	if !valid {
		return change, false, ErrCanvasCollaborationInvalidBatch
	}
	index := slices.IndexFunc(rows, func(row map[string]json.RawMessage) bool {
		return collaborationString(row["id"]) == edit.ID
	})
	version := fields.directorConflictRevision(edit)
	if version > base || (index < 0) != (edit.Action == "add") {
		result.Conflicts = append(result.Conflicts, CanvasCollaborationConflict{
			Kind: change.Kind, ID: change.ID, Field: collaborationSceneField, Revision: max(version, record.Revision), Value: current,
		})
		return change, false, nil
	}
	return mergeCollaborationDirectorEdit(change, scene, rows, edit, index, record.Revision)
}

func mergeCollaborationDirectorEdit(change CanvasCollaborationChange, scene map[string]json.RawMessage, rows []map[string]json.RawMessage, edit collaborationDirectorEdit, index int, revision uint64) (CanvasCollaborationChange, bool, error) {
	switch edit.Action {
	case "add":
		edit.Fields["id"], _ = json.Marshal(edit.ID)
		rows = append(rows, edit.Fields)
	case "set":
		for field, value := range edit.Fields {
			setCollaborationValue(rows[index], field, value)
		}
	case "rm":
		if edit.Collection == "cameras" && collaborationString(scene["activeCameraId"]) == edit.ID {
			return change, false, ErrCanvasCollaborationInvalidBatch
		}
		if edit.Collection == "characters" && directorCharacterReferenced(scene, edit.ID) {
			return change, false, ErrCanvasCollaborationInvalidBatch
		}
		rows = slices.Delete(rows, index, index+1)
	}
	updated, _ := json.Marshal(rows)
	if equalCollaborationField(scene[edit.Collection], updated) {
		return change, false, nil
	}
	scene[edit.Collection] = updated
	encoded, err := json.Marshal(scene)
	if err != nil {
		return change, false, err
	}
	if len(encoded) > 512*1024 || !validCollaborationStructuredShape(collaborationSceneField, decodeCollaborationObject(encoded)) || !validDirectorLinks(scene) {
		return change, false, ErrCanvasCollaborationInvalidBatch
	}
	change.Action, change.Value = "", nil
	change.Fields = map[string]json.RawMessage{collaborationSceneField: encoded}
	change.FieldBases = map[string]uint64{collaborationSceneField: revision}
	return change, true, nil
}

func directorCharacterReferenced(scene map[string]json.RawMessage, id string) bool {
	rows, _ := directorRows(scene, "cameras")
	return slices.ContainsFunc(rows, func(row map[string]json.RawMessage) bool {
		return collaborationString(row["lookAt"]) == id
	})
}

func validDirectorLinks(scene map[string]json.RawMessage) bool {
	cameras, validCameras := directorRows(scene, "cameras")
	characters, validCharacters := directorRows(scene, "characters")
	if !validCameras || !validCharacters {
		return false
	}
	if active := collaborationString(scene["activeCameraId"]); active != "" && !slices.ContainsFunc(cameras, func(row map[string]json.RawMessage) bool {
		return collaborationString(row["id"]) == active
	}) {
		return false
	}
	ids := make(map[string]bool, len(characters))
	for _, character := range characters {
		ids[collaborationString(character["id"])] = true
	}
	return !slices.ContainsFunc(cameras, func(camera map[string]json.RawMessage) bool {
		id := collaborationString(camera["lookAt"])
		return id != "" && !ids[id]
	})
}
