package repository

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"unicode/utf8"

	"gorm.io/datatypes"

	"video-canvas/internal/model"
)

type collaborationFields struct {
	body             map[string]json.RawMessage
	data             map[string]json.RawMessage
	versions         map[string]uint64
	previousBoard    json.RawMessage
	previousBoardRev uint64
	previousScene    json.RawMessage
	previousSceneRev uint64
}

const collaborationOutputField = "data.activeOutputId"

func validateCollaborationBatch(batch CanvasCollaborationBatch) error {
	if !collaborationOpID.MatchString(batch.OpID) || batch.Epoch == 0 || batch.BaseRevision == 0 || len(batch.Changes) == 0 || len(batch.Changes) > model.CanvasCollaborationMaxBatchRecords {
		return ErrCanvasCollaborationInvalidBatch
	}
	seen := make(map[[2]string]bool, len(batch.Changes))
	for _, change := range batch.Changes {
		key := [2]string{change.Kind, change.ID}
		if seen[key] || !validCollaborationChange(change) {
			return ErrCanvasCollaborationInvalidBatch
		}
		seen[key] = true
	}
	return nil
}

func validCollaborationChange(change CanvasCollaborationChange) bool {
	if (change.Kind != "node" && change.Kind != "group" && change.Kind != "edge") || !validCollaborationRecordID(change.ID) {
		return false
	}
	if change.PreviousRevision != 0 || (change.RecordBase != 0 && change.Action != "delete") {
		return false
	}
	for field, version := range change.FieldBases {
		if version == 0 || change.Fields[field] == nil {
			return false
		}
	}
	switch change.Action {
	case "create":
		return len(change.Value) > 0 && len(change.Fields) == 0
	case "delete":
		return len(change.Value) == 0 && len(change.Fields) == 0
	case collaborationShotAction, collaborationDirectorAction:
		return validCollaborationObjectChange(change)
	case "", "update":
		if change.Kind == "edge" || len(change.Value) > 0 || len(change.Fields) == 0 || len(change.Fields) > 10 {
			return false
		}
	default:
		return false
	}
	for name, value := range change.Fields {
		if !validCollaborationField(change.Kind, name, value) {
			return false
		}
	}
	return true
}

func validCollaborationObjectChange(change CanvasCollaborationChange) bool {
	if change.Kind != "node" || len(change.Fields) != 0 {
		return false
	}
	if change.Action == collaborationShotAction {
		_, valid := readCollaborationShotEdit(change.Value)
		return valid
	}
	_, valid := readCollaborationDirectorEdit(change.Value)
	return valid
}

func validCollaborationRevision(batch CanvasCollaborationBatch, current uint64) bool {
	if batch.BaseRevision > current {
		return false
	}
	for _, change := range batch.Changes {
		if change.RecordBase > current {
			return false
		}
		for _, version := range change.FieldBases {
			if version > current {
				return false
			}
		}
	}
	return true
}

func validCollaborationRecordID(id string) bool {
	return id != "" && utf8.RuneCountInString(id) <= 128 && !strings.ContainsRune(id, 0)
}

func validCollaborationField(kind, name string, value json.RawMessage) bool {
	switch name {
	case "position":
		var point map[string]*float64
		return json.Unmarshal(value, &point) == nil && len(point) == 2 && validCollaborationCoordinate(point["x"]) && validCollaborationCoordinate(point["y"])
	case "data.label":
		return validCollaborationText(value, 512)
	case "data.prompt":
		return kind == "node" && validCollaborationText(value, 64000)
	case "data.text":
		return kind == "node" && validCollaborationText(value, 64000)
	case "data.model":
		return kind == "node" && validCollaborationText(value, 255)
	case "data.params":
		return kind == "node" && validCollaborationParams(value)
	case "data.character", "data.scene", "data.storyboard", collaborationManualReferences:
		return kind == "node" && validCollaborationStructured(name, value)
	case collaborationAssetField:
		return kind == "node" && collaborationAssetID(value) != 0
	case collaborationOutputField:
		return kind == "node" && validCollaborationRecordID(collaborationString(value))
	case collaborationGenerationInputField:
		_, valid := decodeGenerationInput(value)
		return kind == "node" && valid
	case "parentId":
		var parent *string
		return kind == "node" && json.Unmarshal(value, &parent) == nil && (parent == nil || validCollaborationRecordID(*parent))
	case "height", "width":
		var size *float64
		return kind == "group" && json.Unmarshal(value, &size) == nil && size != nil && *size >= 0 && *size <= 1e6
	case "data.color", "data.labelColor":
		return kind == "group" && validCollaborationColor(value)
	default:
		return false
	}
}

func validCollaborationParams(value json.RawMessage) bool {
	var params map[string]json.RawMessage
	if len(value) > 32*1024 || json.Unmarshal(value, &params) != nil || params == nil || len(params) > 96 {
		return false
	}
	for key, option := range params {
		if !editableCollaborationOption(key, option) {
			return false
		}
	}
	return true
}

func editableCollaborationOption(key string, value json.RawMessage) bool {
	name := strings.ToLower(key)
	switch name {
	case "prompt", "images", "videos", "audios", "src", "__proto__", "constructor", "prototype":
		return false
	}
	if !validCollaborationRecordID(key) || strings.HasSuffix(name, "url") || strings.Contains(name, "assetid") || strings.Contains(name, "storagekey") || strings.Contains(name, "secret") || strings.Contains(name, "token") {
		return false
	}
	var decoded any
	if json.Unmarshal(value, &decoded) != nil {
		return false
	}
	switch option := decoded.(type) {
	case bool, float64:
		return true
	case string:
		return !strings.ContainsRune(option, 0) && !strings.Contains(option, "/files/") && !strings.HasPrefix(option, "asset://") && !strings.HasPrefix(option, "blob:") && !strings.HasPrefix(option, "data:")
	default:
		return false
	}
}

func validCollaborationColor(value json.RawMessage) bool {
	var color string
	if json.Unmarshal(value, &color) != nil || len(color) == 0 || len(color) > 64 {
		return false
	}
	return strings.IndexFunc(color, func(char rune) bool {
		return !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789#(),. %/-", char)
	}) < 0
}

func validCollaborationCoordinate(value *float64) bool {
	return value != nil && math.Abs(*value) <= 1e9
}

func validCollaborationText(value json.RawMessage, limit int) bool {
	var text *string
	return len(value) <= limit*6+2 && json.Unmarshal(value, &text) == nil && text != nil &&
		len(*text) <= limit && !strings.ContainsRune(*text, 0)
}

func decodeCollaborationFields(body, versions []byte) (*collaborationFields, error) {
	fields := &collaborationFields{}
	if err := json.Unmarshal(body, &fields.body); err != nil {
		return nil, err
	}
	if data := fields.body["data"]; len(data) > 0 {
		if err := json.Unmarshal(data, &fields.data); err != nil {
			return nil, err
		}
	}
	if err := json.Unmarshal(versions, &fields.versions); err != nil {
		return nil, err
	}
	if fields.data == nil {
		fields.data = make(map[string]json.RawMessage)
	}
	fields.previousBoard = fields.get(collaborationStoryboardField)
	fields.previousBoardRev = fields.versions[collaborationStoryboardField]
	fields.previousScene = fields.get(collaborationSceneField)
	fields.previousSceneRev = fields.versions[collaborationSceneField]
	return fields, nil
}

func (fields *collaborationFields) get(name string) json.RawMessage {
	if name == collaborationManualReferences {
		return fields.manualReferences()
	}
	if name == collaborationOutputField {
		return fields.activeCollaborationOutput()
	}
	if name == "data.params" {
		params := fields.promptParams()
		assets := fields.paramAssets()
		for key, option := range params {
			if assets[key] != nil || !editableCollaborationOption(key, option) {
				delete(params, key)
			}
		}
		encoded, _ := json.Marshal(params)
		return encoded
	}
	if name == "data.prompt" {
		if prompt := fields.promptParams()["prompt"]; validCollaborationText(prompt, 64000) {
			return prompt
		}
	}
	if key, nested := strings.CutPrefix(name, "data."); nested {
		return fields.data[key]
	}
	return fields.body[name]
}

func (fields *collaborationFields) activeCollaborationOutput() json.RawMessage {
	if active := fields.data["activeOutputId"]; collaborationString(active) != "" {
		return active
	}
	outputs := fields.collaborationOutputs()
	if len(outputs) > 0 {
		return outputs[len(outputs)-1]["id"]
	}
	return fields.data["assetId"]
}

func (fields *collaborationFields) collaborationOutputs() []map[string]json.RawMessage {
	var outputs []map[string]json.RawMessage
	_ = json.Unmarshal(fields.data["outputs"], &outputs)
	return outputs
}

func (fields *collaborationFields) selectCollaborationOutput(value json.RawMessage) bool {
	output := fields.collaborationOutput(value)
	if output == nil {
		return false
	}
	fields.set(collaborationOutputField, value)
	for _, key := range []string{"assetId", "src", "mediaType"} {
		setCollaborationValue(fields.data, key, output[key])
	}
	return true
}

func (fields *collaborationFields) collaborationOutput(value json.RawMessage) map[string]json.RawMessage {
	id := collaborationString(value)
	if id == "" {
		return nil
	}
	for _, output := range fields.collaborationOutputs() {
		if collaborationString(output["id"]) == id && collaborationString(output["assetId"]) != "" && collaborationString(output["src"]) != "" {
			return output
		}
	}
	return nil
}

func (fields *collaborationFields) set(name string, value json.RawMessage) {
	if name == collaborationManualReferences {
		fields.setManualReferences(value)
		return
	}
	if name == "data.params" {
		params := make(map[string]json.RawMessage)
		_ = json.Unmarshal(value, &params)
		if params == nil {
			params = make(map[string]json.RawMessage)
		}
		assets := fields.paramAssets()
		for key := range assets {
			delete(params, key)
		}
		for key, option := range fields.promptParams() {
			if assets[key] != nil || !editableCollaborationOption(key, option) {
				params[key] = option
			}
		}
		fields.data["params"], _ = json.Marshal(params)
		return
	}
	if key, nested := strings.CutPrefix(name, "data."); nested {
		setCollaborationValue(fields.data, key, value)
		if key == "prompt" {
			params := fields.promptParams()
			setCollaborationValue(params, "prompt", value)
			fields.data["params"], _ = json.Marshal(params)
		}
	} else {
		setCollaborationValue(fields.body, name, value)
	}
}

func (fields *collaborationFields) paramAssets() map[string]json.RawMessage {
	var assets map[string]json.RawMessage
	_ = json.Unmarshal(fields.data["paramAssets"], &assets)
	return assets
}

func (fields *collaborationFields) allowsCollaborationParams(value json.RawMessage) bool {
	var params map[string]json.RawMessage
	_ = json.Unmarshal(value, &params)
	for key := range fields.paramAssets() {
		if params[key] != nil {
			return false
		}
	}
	return true
}

func setCollaborationValue(target map[string]json.RawMessage, key string, value json.RawMessage) {
	if len(value) == 0 || bytes.Equal(value, []byte("null")) {
		delete(target, key)
	} else {
		target[key] = value
	}
}

func (fields *collaborationFields) promptParams() map[string]json.RawMessage {
	var params map[string]json.RawMessage
	if json.Unmarshal(fields.data["params"], &params) != nil || params == nil {
		return make(map[string]json.RawMessage)
	}
	return params
}

func (fields *collaborationFields) equal(name string, value json.RawMessage) bool {
	if name == "data.prompt" {
		return equalCollaborationField(fields.data["prompt"], value) && equalCollaborationField(fields.promptParams()["prompt"], value)
	}
	if isCollaborationStructured(name) {
		current, err := rewriteCollaborationReferences(name, fields.get(name), func(ref collaborationObject, address, _ string) error {
			delete(ref, address)
			return nil
		})
		return err == nil && equalCollaborationField(current, value)
	}
	return equalCollaborationField(fields.get(name), value)
}

func (fields *collaborationFields) save(record *model.CanvasCollaborationRecord, revision uint64) error {
	fields.trackStoryboardVersions(fields.previousBoard, revision)
	fields.trackDirectorVersions(revision)
	if len(fields.data) > 0 || fields.body["data"] != nil {
		data, err := json.Marshal(fields.data)
		if err != nil {
			return err
		}
		fields.body["data"] = data
	}
	body, err := json.Marshal(fields.body)
	if err != nil {
		return err
	}
	versions, err := json.Marshal(fields.versions)
	if err != nil {
		return err
	}
	record.Body = datatypes.JSON(body)
	record.FieldVersions = datatypes.JSON(versions)
	record.Revision = revision
	return nil
}

func equalCollaborationField(current, proposed json.RawMessage) bool {
	if bytes.Equal(current, proposed) {
		return true
	}
	var confirmed, draft any
	return json.Unmarshal(current, &confirmed) == nil && json.Unmarshal(proposed, &draft) == nil && reflect.DeepEqual(confirmed, draft)
}
