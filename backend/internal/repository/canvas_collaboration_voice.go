package repository

import (
	"encoding/json"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"video-canvas/internal/model"
)

func finishPrimaryGenerationBinding(task *model.GenerationTask, fields *collaborationFields, patch map[string]any) {
	patch["data.generationRevision"] = nil
	if collaborationString(fields.get("data.kind")) != "character" {
		return
	}
	patch["data.characterImageTaskId"] = nil
	patch["data.characterImageAssetId"] = nil
	if task.Status == model.TaskSucceeded && patch["data.assetId"] != nil {
		patch["data.characterImageTaskId"] = strconv.FormatUint(task.ID, 10)
		patch["data.characterImageAssetId"] = patch["data.assetId"]
	}
}

func collaborationVoiceTask(task *model.GenerationTask) bool {
	return task.Kind == "audio" && strings.HasPrefix(task.NodeID, "voice:")
}

func collaborationTaskNode(task *model.GenerationTask) string {
	if collaborationVoiceTask(task) {
		return strings.TrimPrefix(task.NodeID, "voice:")
	}
	return task.NodeID
}

func collaborationVoiceChanged(before, after json.RawMessage) bool {
	read := func(raw json.RawMessage) json.RawMessage {
		voice, _ := decodeCollaborationObject(raw)["voice"].(collaborationObject)
		delete(voice, "src")
		encoded, _ := json.Marshal(voice)
		return encoded
	}
	return !equalCollaborationField(read(before), read(after))
}

func finalizeCollaborationVoice(tx *gorm.DB, task *model.GenerationTask, project *model.CanvasProject, record *model.CanvasCollaborationRecord, fields *collaborationFields) error {
	if collaborationString(fields.get("data.voiceTaskId")) != strconv.FormatUint(task.ID, 10) {
		return nil
	}
	patch := map[string]any{"data.voiceTaskId": nil, "data.voiceError": nil}
	allowed, err := canGenerateInCollaboration(tx, project, task.UserID)
	if err != nil {
		return err
	}
	if allowed && task.Status != model.TaskCanceled {
		if err := setCollaborationVoiceResult(tx, task, project, fields, patch); err != nil {
			return err
		}
	}
	return persistGenerationFields(tx, task, project.Revision, record, fields, patch)
}

func setCollaborationVoiceResult(tx *gorm.DB, task *model.GenerationTask, project *model.CanvasProject, fields *collaborationFields, patch map[string]any) error {
	if task.Status != model.TaskSucceeded {
		patch["data.voiceError"] = "音色生成未完成"
		return nil
	}
	character := decodeCollaborationObject(fields.get("data.character"))
	if character == nil || fields.versions["$voice"] > fields.versions["data.voiceTaskId"] {
		return nil
	}
	var outputs []model.TaskOutput
	var err error
	if task.UserID == project.UserID {
		err = json.Unmarshal(task.OutputJSON, &outputs)
	} else {
		outputs, err = attachCollaborationMedia(tx, project.UserID, task.CollaborationMedia)
	}
	if err != nil {
		return err
	}
	for _, output := range outputs {
		if output.MediaType == "audio" && output.AssetID != 0 {
			character["voice"] = map[string]any{"mode": "audio", "origin": "generated",
				"assetId": strconv.FormatUint(output.AssetID, 10), "src": output.URL}
			patch["data.character"] = character
			return nil
		}
	}
	patch["data.voiceError"] = "没有生成出可绑定的音频"
	return nil
}
