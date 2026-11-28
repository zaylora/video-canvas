package repository

import (
	"encoding/json"

	"video-canvas/internal/model"
	"video-canvas/internal/provider/modelcfg"
)

func matchesVocalSeparationSource(task *model.GenerationTask, fields *collaborationFields) bool {
	source, separatesVocals := vocalSeparationSource(task)
	if !separatesVocals {
		return true
	}
	return source != "" && collaborationString(fields.get("data.kind")) == model.KindAudio &&
		collaborationString(fields.get("data.assetId")) == source
}

func vocalSeparationSource(task *model.GenerationTask) (string, bool) {
	var input struct {
		Operation string   `json:"audio_operation"`
		Audios    []string `json:"audios"`
	}
	if err := json.Unmarshal(task.InputJSON, &input); err != nil || input.Operation != modelcfg.AudioOperationVocalSeparation {
		return "", false
	}
	if len(input.Audios) != 1 {
		return "", true
	}
	return input.Audios[0], true
}

func preserveVocalSeparationSource(task *model.GenerationTask, fields *collaborationFields, patch map[string]any) {
	id, separatesVocals := vocalSeparationSource(task)
	if !separatesVocals || id == "" {
		return
	}
	src := collaborationString(fields.get("data.src"))
	if src == "" {
		return
	}
	var history []map[string]any
	_ = json.Unmarshal(fields.get("data.outputs"), &history)
	if !knownGenerationOutput(history, id) {
		patch["data.outputs"] = append(history, map[string]any{
			"id": id, "assetId": id, "src": src, "mediaType": model.KindAudio, "createdAt": task.CreatedAt.UnixMilli(),
		})
	}
}
