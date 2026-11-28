package repository

import (
	"encoding/json"
	"maps"
	"slices"

	"video-canvas/internal/model"
)

type collaborationObjectUndo struct {
	ID         string `json:"id"`
	Collection string `json:"collection,omitempty"`
}

func collaborationObjectInverse(before model.CanvasCollaborationRecord, accepted, requested CanvasCollaborationChange) (CanvasCollaborationChange, error) {
	selector, props, ok := reversibleCollaborationProperties(requested)
	if !ok {
		return collaborationInverse(before, accepted)
	}
	fields, err := decodeCollaborationFields(before.Body, before.FieldVersions)
	if err != nil {
		return CanvasCollaborationChange{}, err
	}
	previous, _, _ := selector.read(fields.get(selector.field()))
	latest, _, _ := selector.read(accepted.Fields[selector.field()])
	inverse := CanvasCollaborationChange{Kind: accepted.Kind, ID: accepted.ID, Action: requested.Action,
		Value: requested.Value, Fields: map[string]json.RawMessage{}, FieldBases: map[string]uint64{}}
	inverse.Value, _ = json.Marshal(selector)
	for prop := range props {
		if !equalCollaborationField(previous[prop], latest[prop]) {
			inverse.Fields[prop] = previous[prop]
			inverse.FieldBases[prop] = fields.undoFieldRevision(selector.key(prop))
		}
	}
	return inverse, nil
}

func reversibleCollaborationProperties(change CanvasCollaborationChange) (collaborationObjectUndo, map[string]json.RawMessage, bool) {
	switch change.Action {
	case collaborationShotAction:
		edit, _ := readCollaborationShotEdit(change.Value)
		return collaborationObjectUndo{ID: edit.ID}, edit.Fields, edit.Action == "set" && edit.Fields["imageNodeId"] == nil && edit.Fields["videoNodeId"] == nil
	case collaborationDirectorAction:
		edit, _ := readCollaborationDirectorEdit(change.Value)
		return collaborationObjectUndo{ID: edit.ID, Collection: edit.Collection}, edit.Fields, edit.Action == "set" && edit.Fields["lookAt"] == nil
	default:
		return collaborationObjectUndo{}, nil, false
	}
}

func (selector collaborationObjectUndo) field() string {
	if selector.Collection == "" {
		return collaborationStoryboardField
	}
	return collaborationSceneField
}

func (selector collaborationObjectUndo) key(prop string) string {
	namespace := collaborationShotNamespace
	if selector.Collection != "" {
		namespace = selector.Collection + ":"
	}
	return collaborationShotKey(namespace+selector.ID, prop)
}

func (selector collaborationObjectUndo) read(raw json.RawMessage) (map[string]json.RawMessage, []map[string]json.RawMessage, map[string]json.RawMessage) {
	collection := selector.Collection
	if collection == "" {
		collection = "shots"
	}
	var content map[string]json.RawMessage
	if json.Unmarshal(raw, &content) != nil {
		return nil, nil, nil
	}
	rows, valid := directorRows(content, collection)
	if !valid {
		return nil, nil, nil
	}
	for _, row := range rows {
		if collaborationString(row["id"]) == selector.ID {
			return row, rows, content
		}
	}
	return nil, nil, nil
}

func reverseCollaborationObject(record *model.CanvasCollaborationRecord, inverse CanvasCollaborationChange, history collaborationHistory, result *CanvasCollaborationCommit) (bool, error) {
	var selector collaborationObjectUndo
	if err := json.Unmarshal(inverse.Value, &selector); err != nil {
		return false, err
	}
	fields, err := decodeCollaborationFields(record.Body, record.FieldVersions)
	if err != nil {
		return false, err
	}
	field := selector.field()
	row, rows, content := selector.read(fields.get(field))
	if row == nil || fields.objectLineage(selector) > history.Origin {
		result.Skipped = append(result.Skipped, CanvasCollaborationConflict{Kind: inverse.Kind, ID: inverse.ID, Field: field, Revision: record.Revision})
		return false, nil
	}
	revision := result.Revision + 1
	if !reverseCollaborationObjectProperties(fields, row, selector, inverse, history, result) {
		return false, nil
	}
	collection := selector.Collection
	if collection == "" {
		collection = "shots"
	}
	content[collection], _ = json.Marshal(rows)
	encoded, err := json.Marshal(content)
	if err != nil {
		return false, err
	}
	fields.set(field, encoded)
	fields.versions[field] = revision
	result.Changes = append(result.Changes, CanvasCollaborationChange{Kind: inverse.Kind, ID: inverse.ID,
		Fields: map[string]json.RawMessage{field: encoded}})
	return true, fields.save(record, revision)
}

func (fields *collaborationFields) objectLineage(selector collaborationObjectUndo) uint64 {
	if selector.Collection == "" {
		return max(fields.storyboardBaseline(), fields.versions[selector.key("$created")])
	}
	return fields.directorConflictRevision(collaborationDirectorEdit{ID: selector.ID, Collection: selector.Collection})
}

func reverseCollaborationObjectProperties(fields *collaborationFields, row map[string]json.RawMessage, selector collaborationObjectUndo, inverse CanvasCollaborationChange, history collaborationHistory, result *CanvasCollaborationCommit) bool {
	changed := false
	for _, prop := range slices.Sorted(maps.Keys(inverse.Fields)) {
		key := selector.key(prop)
		expected, origin := history.Origin, inverse.FieldBases[prop]
		if result.Redo {
			expected, origin = origin, expected
		}
		if fields.undoFieldRevision(key) != expected {
			result.Skipped = append(result.Skipped, CanvasCollaborationConflict{Kind: inverse.Kind, ID: inverse.ID,
				Field: selector.field() + "." + selector.ID + "." + prop, Revision: fields.versions[key]})
			continue
		}
		setCollaborationValue(row, prop, inverse.Fields[prop])
		fields.versions["$undoWrite:"+key] = result.Revision + 1
		fields.versions["$undoOrigin:"+key] = origin
		changed = true
	}
	return changed
}

func oppositeCollaborationObject(before, after model.CanvasCollaborationRecord, source CanvasCollaborationChange, result *CanvasCollaborationCommit) (CanvasCollaborationChange, error) {
	var selector collaborationObjectUndo
	if err := json.Unmarshal(source.Value, &selector); err != nil {
		return source, err
	}
	prior, err := decodeCollaborationFields(before.Body, before.FieldVersions)
	if err != nil {
		return source, err
	}
	latest, err := decodeCollaborationFields(after.Body, after.FieldVersions)
	if err != nil {
		return source, err
	}
	row, _, _ := selector.read(prior.get(selector.field()))
	opposite := source
	opposite.Fields = make(map[string]json.RawMessage)
	opposite.FieldBases = make(map[string]uint64)
	for prop := range source.Fields {
		key := selector.key(prop)
		if latest.versions[key] != result.Revision+1 {
			continue
		}
		opposite.Fields[prop] = row[prop]
		opposite.FieldBases[prop] = latest.undoFieldRevision(key)
		if result.Redo {
			opposite.FieldBases[prop] = prior.undoFieldRevision(key)
		}
	}
	return opposite, nil
}
