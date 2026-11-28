package repository

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	"video-canvas/internal/pkg/idcodec"
)

var collaborationStructuredKinds = map[string]string{
	"data.character": "character", "data.scene": "director", "data.storyboard": "storyboard",
	collaborationManualReferences: "",
}

func isCollaborationStructured(field string) bool {
	_, exists := collaborationStructuredKinds[field]
	return exists
}

type collaborationObject = map[string]any
type collaborationReference func(object collaborationObject, address, kind string) error

// decodeCollaborationObject 保留素材编号等数字的精度。
func decodeCollaborationObject(raw json.RawMessage) collaborationObject {
	var result collaborationObject
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&result) != nil {
		return nil
	}
	return result
}

// collaborationObjectFields 检查扁平属性类型与允许的嵌套字段。
func collaborationObjectFields(object collaborationObject, text, numbers, toggles, nested string) bool {
	if object == nil {
		return false
	}
	for key, value := range object {
		switch typed := value.(type) {
		case string:
			if collaborationHasKey(text, key) && !strings.ContainsRune(typed, 0) {
				continue
			}
		case json.Number:
			if numeric, err := typed.Float64(); err == nil && numeric >= -1e6 && numeric <= 1e6 && collaborationHasKey(numbers, key) {
				continue
			}
		case bool:
			if collaborationHasKey(toggles, key) {
				continue
			}
		}
		if !collaborationHasKey(nested, key) {
			return false
		}
	}
	return true
}

// collaborationHasKey 在以空格隔开的模式中匹配完整字段名。
func collaborationHasKey(names, key string) bool {
	return key != "" && !strings.ContainsRune(key, ' ') && strings.Contains(" "+names+" ", " "+key+" ")
}

// collaborationObjects 对有限长度的对象数组逐项检查。
func collaborationObjects(value any, limit int, check func(collaborationObject) bool) bool {
	items, ok := value.([]any)
	if !ok || len(items) > limit {
		return false
	}
	for _, item := range items {
		object, _ := item.(collaborationObject)
		if !check(object) {
			return false
		}
	}
	return true
}

// validCollaborationStructured 检查结构、大小及客户端可提交的素材字段。
func validCollaborationStructured(field string, raw json.RawMessage) bool {
	if !json.Valid(raw) || len(raw) > 512*1024 {
		return false
	}
	value := decodeCollaborationObject(raw)
	if !validCollaborationStructuredShape(field, value) {
		return false
	}
	return visitCollaborationReferences(field, value, func(ref collaborationObject, address, kind string) error {
		keys := "assetId"
		if address == "src" && kind == "audio" {
			keys += " mode origin name"
			if ref["mode"] != "audio" || (ref["origin"] != "upload" && ref["origin"] != "generated" && ref["origin"] != "library") {
				return ErrCanvasCollaborationInvalidBatch
			}
		}
		if !collaborationObjectFields(ref, keys, "", "", "") || collaborationReferenceID(ref) == 0 {
			return ErrCanvasCollaborationInvalidBatch
		}
		return nil
	}) == nil
}

// validCollaborationStructuredShape 只接受编辑器定义的创作节点数据结构。
func validCollaborationStructuredShape(field string, value collaborationObject) bool {
	switch field {
	case collaborationManualReferences:
		return collaborationObjectFields(value, "", "", "", "images videos audios")
	case "data.character":
		return collaborationObjectFields(value, "name look description voicePrompt voiceModel", "", "", "extras voice") &&
			collaborationObjects(value["extras"], 4, func(item collaborationObject) bool { return item != nil })
	case "data.storyboard":
		return collaborationObjectFields(value, "", "", "", "shots") && collaborationObjects(value["shots"], 2000, func(shot collaborationObject) bool {
			id, _ := shot["id"].(string)
			return validCollaborationRecordID(id) && collaborationObjectFields(shot, "id scene shot camera action dialogue imageNodeId videoNodeId", "duration", "", "image")
		})
	case "data.scene":
		return collaborationObjectFields(value, "activeCameraId", "version duration", "", "props cameras characters thumb") &&
			value["version"] == json.Number("1") && validCollaborationScene(value)
	default:
		return false
	}
}

// validCollaborationScene 检查导演台中的人物、道具、机位和关键帧。
func validCollaborationScene(scene collaborationObject) bool {
	return collaborationObjects(scene["characters"], 1000, func(item collaborationObject) bool {
		return collaborationObjectFields(item, "id name color model pose", "x z yaw scale", "hidden locked", "keys") &&
			collaborationObjects(item["keys"], 2000, func(key collaborationObject) bool {
				return collaborationObjectFields(key, "ease", "x z yaw t", "", "")
			})
	}) && collaborationObjects(scene["props"], 1000, func(item collaborationObject) bool {
		return collaborationObjectFields(item, "id name shape", "x z yaw w h d", "hidden locked", "")
	}) && collaborationObjects(scene["cameras"], 1000, func(item collaborationObject) bool {
		return collaborationObjectFields(item, "id name aspect lookAt", "x y z yaw pitch fov", "", "keys") &&
			collaborationObjects(item["keys"], 2000, func(key collaborationObject) bool {
				return collaborationObjectFields(key, "ease", "x y z yaw pitch fov t", "", "")
			})
	})
}

// collaborationReferenceID 读取创作节点里的素材编号。
func collaborationReferenceID(ref collaborationObject) uint64 {
	raw, _ := json.Marshal(ref["assetId"])
	return collaborationAssetID(raw)
}

// visitCollaborationReferences 按当前节点数据契约遍历嵌套媒体引用。
func visitCollaborationReferences(field string, value collaborationObject, visit collaborationReference) error {
	switch field {
	case collaborationManualReferences:
		return visitManualCollaborationReferences(value, visit)
	case "data.character":
		extras, _ := value["extras"].([]any)
		for _, extra := range extras {
			if err := visitCollaborationReference(extra, "src", "image", visit); err != nil {
				return err
			}
		}
		return visitCollaborationReference(value["voice"], "src", "audio", visit)
	case "data.storyboard":
		shots, _ := value["shots"].([]any)
		for _, item := range shots {
			shot, _ := item.(collaborationObject)
			if err := visitCollaborationReference(shot["image"], "src", "image", visit); err != nil {
				return err
			}
		}
	case "data.scene":
		return visitCollaborationReference(value["thumb"], "url", "image", visit)
	}
	return nil
}

func visitManualCollaborationReferences(value collaborationObject, visit collaborationReference) error {
	for key, raw := range value {
		kind := collaborationReferenceKinds[key]
		items, ok := raw.([]any)
		if kind == "" || !ok || len(items) > 64 {
			return ErrCanvasCollaborationInvalidBatch
		}
		for _, item := range items {
			if item == nil {
				return ErrCanvasCollaborationInvalidBatch
			}
			if err := visitCollaborationReference(item, "url", kind, visit); err != nil {
				return err
			}
		}
	}
	return nil
}

// visitCollaborationReference 跳过未绑定的媒体位置。
func visitCollaborationReference(value any, address, kind string, visit collaborationReference) error {
	if value == nil {
		return nil
	}
	ref, ok := value.(collaborationObject)
	if !ok {
		return ErrCanvasCollaborationInvalidBatch
	}
	return visit(ref, address, kind)
}

// rewriteCollaborationReferences 投影或替换嵌套素材而保留其余编辑字段。
func rewriteCollaborationReferences(field string, raw json.RawMessage, visit collaborationReference) (json.RawMessage, error) {
	value := decodeCollaborationObject(raw)
	if value == nil {
		return raw, nil
	}
	if err := visitCollaborationReferences(field, value, visit); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

// scopedStructuredValue 仅向成员返回通过画布授权读取的素材地址。
func scopedStructuredValue(canvasID uint64, field string, raw json.RawMessage) (json.RawMessage, error) {
	prefix := "/api/v1/canvas-media/" + idcodec.Encode(canvasID) + "/"
	return rewriteCollaborationReferences(field, raw, func(ref collaborationObject, address, _ string) error {
		id := collaborationReferenceID(ref)
		delete(ref, address)
		if id != 0 {
			ref["assetId"] = strconv.FormatUint(id, 10)
			ref[address] = prefix + idcodec.Encode(id)
		}
		return nil
	})
}

// projectStructuredFields 改写增量中复杂字段的素材地址。
func projectStructuredFields(canvasID uint64, fields map[string]json.RawMessage) error {
	for field := range collaborationStructuredKinds {
		if raw := fields[field]; raw != nil {
			projected, err := scopedStructuredValue(canvasID, field, raw)
			if err != nil {
				return err
			}
			fields[field] = projected
		}
	}
	return nil
}

// projectStructuredConflicts 改写冲突提示中当前版本的素材地址。
func projectStructuredConflicts(canvasID uint64, conflicts []CanvasCollaborationConflict) error {
	for index := range conflicts {
		conflict := &conflicts[index]
		if isCollaborationStructured(conflict.Field) {
			projected, err := scopedStructuredValue(canvasID, conflict.Field, conflict.Value)
			if err != nil {
				return err
			}
			conflict.Value = projected
		}
	}
	return nil
}

// projectCollaborationCommit 按画布授权返回成员可见的操作结果。
func projectCollaborationCommit(canvasID uint64, result *CanvasCollaborationCommit) error {
	if err := projectGenerationEvent(canvasID, result.Changes); err != nil {
		return err
	}
	return projectStructuredConflicts(canvasID, result.Conflicts)
}
