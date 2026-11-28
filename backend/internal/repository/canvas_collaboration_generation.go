package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
)

// ErrCanvasCollaborationInput 表示生成输入与已确认的节点版本不一致。
var ErrCanvasCollaborationInput = errors.New("节点生成输入已变更，请核对后重新发起")

const collaborationGenerationInputField = "data.generationInput"

func collaborationGenerationActorKey(actor uint64) string {
	return "$generation:" + strconv.FormatUint(actor, 10)
}

func canGenerateInCollaboration(tx *gorm.DB, project *model.CanvasProject, actor uint64) (bool, error) {
	if actor == project.UserID {
		return true, nil
	}
	var count int64
	err := tx.Model(&model.CanvasViewer{}).Where("canvas_id = ? AND user_id = ? AND role = ? AND can_generate", project.ID, actor, model.CanvasRoleEditor).Count(&count).Error
	return count != 0, err
}

// CanvasGenerationOwner 查询当前授权任务的画布所有者，失效会话或撤权时返回零。
func (r *GenerationTaskRepository) CanvasGenerationOwner(ctx context.Context, task *model.GenerationTask) (uint64, error) {
	if task.CanvasProjectID == nil || task.CollaborationEpoch == 0 {
		return 0, nil
	}
	var project model.CanvasProject
	db := r.db.WithContext(ctx)
	err := db.Where("id = ? AND drama_id IS NULL AND collaboration_active AND collaboration_epoch = ?", *task.CanvasProjectID, task.CollaborationEpoch).Take(&project).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	allowed, err := canGenerateInCollaboration(db, &project, task.UserID)
	if !allowed || err != nil {
		return 0, err
	}
	return project.UserID, nil
}

// StageCanvasGenerationMedia 在复制前登记可回收的目标存储对象。
func (r *GenerationTaskRepository) StageCanvasGenerationMedia(ctx context.Context, stage *model.CanvasDeliveryStage) error {
	return NewCanvasShareRepository(r.db).Stage(ctx, stage)
}

// CanvasProgressRevision 读取当前可公开的节点任务版本，绑定或权限失效时返回零。
func (r *GenerationTaskRepository) CanvasProgressRevision(ctx context.Context, task *model.GenerationTask) (uint64, error) {
	if task.CanvasProjectID == nil || task.CollaborationEpoch == 0 || model.IsTerminalStatus(task.Status) {
		return 0, nil
	}
	var revision uint64
	err := r.db.WithContext(ctx).Table("canvas_collaboration_records AS node").
		Select("(node.field_versions ->> 'data.taskId')::bigint").
		Joins("JOIN canvas_projects AS canvas ON canvas.id = node.canvas_id").
		Where("canvas.id = ? AND canvas.collaboration_active AND canvas.collaboration_epoch = ?", *task.CanvasProjectID, task.CollaborationEpoch).
		Where("canvas.user_id = ? OR EXISTS (SELECT 1 FROM canvas_viewers WHERE canvas_id = canvas.id AND user_id = ? AND role = ? AND can_generate)", task.UserID, task.UserID, model.CanvasRoleEditor).
		Where("node.kind = 'node' AND node.record_id = ? AND NOT node.deleted", task.NodeID).
		Where("node.body #>> '{data,taskId}' = ? AND node.body #>> '{data,status}' = 'running' AND node.field_versions ->> 'data.taskId' IS NOT NULL", strconv.FormatUint(task.ID, 10)).
		Scan(&revision).Error
	return revision, err
}

type confirmedGenerationInput struct {
	Model string         `json:"model"`
	Input map[string]any `json:"input"`
	Actor uint64         `json:"actor,omitempty"`
}

func decodeGenerationInput(value json.RawMessage) (confirmedGenerationInput, bool) {
	var input confirmedGenerationInput
	err := json.Unmarshal(value, &input)
	valid := err == nil && len(value) <= 160*1024 && input.Input != nil && len(input.Input) <= 100 &&
		input.Model != "" && len(input.Model) <= 128 && !strings.ContainsRune(input.Model, 0)
	return input, valid
}

func generationInputDigest(input confirmedGenerationInput) string {
	encoded, err := json.Marshal(input)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func guardGenerationCanvas(tx *gorm.DB, task *model.GenerationTask) error {
	if task.CollaborationEpoch == 0 {
		if task.CanvasProjectID == nil {
			return nil
		}
		return lockTaskCanvas(tx, task.UserID, *task.CanvasProjectID)
	}
	project, err := lockGenerationCollaboration(tx, task)
	if err != nil {
		return err
	}
	if allowed, err := canGenerateInCollaboration(tx, project, task.UserID); err != nil || !allowed {
		if err != nil {
			return err
		}
		return ErrNotFound
	}
	if task.CollaborationInputRevision == 0 || task.CollaborationInputRevision > project.Revision || task.IsTest {
		return ErrCanvasCollaborationInput
	}
	record, fields, err := generationCanvasNode(tx, project.ID, collaborationTaskNode(task))
	if err != nil {
		return err
	}
	if !matchesVocalSeparationSource(task, fields) {
		return ErrCanvasCollaborationInput
	}
	if task.AgentRunID != 0 {
		if project.Revision != task.CollaborationInputRevision || collaborationString(fields.get("data.status")) == "running" ||
			collaborationString(fields.get("data.assetId")) != "" || collaborationString(fields.get("data.kind")) != task.Kind ||
			collaborationString(fields.get("data.model")) != task.ModelKey {
			return ErrCanvasCollaborationInput
		}
		return nil
	}
	return guardConfirmedCandidateInput(tx, task, record.Revision, fields, project.UserID == task.UserID)
}

func lockGenerationCollaboration(tx *gorm.DB, task *model.GenerationTask) (*model.CanvasProject, error) {
	if task.CanvasProjectID == nil {
		return nil, ErrNotFound
	}
	project, err := lockCollaborationProject(tx, *task.CanvasProjectID)
	if err != nil {
		return nil, err
	}
	if !project.CollaborationActive || project.CollaborationEpoch != task.CollaborationEpoch {
		return nil, ErrCanvasCollaborationSession
	}
	return project, nil
}

func generationCanvasNode(tx *gorm.DB, canvasID uint64, nodeID string) (model.CanvasCollaborationRecord, *collaborationFields, error) {
	var record model.CanvasCollaborationRecord
	if err := tx.Where("canvas_id = ? AND kind = 'node' AND record_id = ? AND NOT deleted", canvasID, nodeID).Take(&record).Error; err != nil {
		return record, nil, translate(err)
	}
	fields, err := decodeCollaborationFields(record.Body, record.FieldVersions)
	return record, fields, err
}

func matchesGenerationInput(task *model.GenerationTask, fields *collaborationFields, owner bool) bool {
	kind := collaborationString(fields.data["kind"])
	if kind == "character" {
		kind = "image"
		if collaborationVoiceTask(task) {
			kind = "audio"
		}
	} else if collaborationVoiceTask(task) {
		return false
	}
	if kind != task.Kind || (kind != "image" && kind != "video" && kind != "audio") {
		return false
	}
	input := task.CollaborationRawInput
	if input == nil && json.Unmarshal(task.InputJSON, &input) != nil {
		return false
	}
	if digest := collaborationString(fields.get(collaborationGenerationInputField)); digest != "" {
		confirmed := confirmedGenerationInput{Model: task.ModelKey, Input: input, Actor: task.UserID}
		if generationInputDigest(confirmed) == digest {
			return true
		}
		confirmed.Actor = 0
		return owner && generationInputDigest(confirmed) == digest
	}
	if !owner || collaborationVoiceTask(task) || collaborationString(fields.get("data.model")) != task.ModelKey {
		return false
	}
	confirmed := fields.promptParams()
	confirmed["prompt"] = fields.get("data.prompt")
	want, err := json.Marshal(confirmed)
	got, inputErr := json.Marshal(input)
	return err == nil && inputErr == nil && equalCollaborationField(want, got)
}

func writeGenerationStart(tx *gorm.DB, task *model.GenerationTask) error {
	project, err := lockGenerationCollaboration(tx, task)
	if err != nil {
		return err
	}
	record, fields, err := generationCanvasNode(tx, project.ID, collaborationTaskNode(task))
	if err != nil {
		return err
	}
	if collaborationVoiceTask(task) {
		if collaborationString(fields.get("data.voiceTaskId")) != "" {
			return ErrCanvasCollaborationInput
		}
		return persistGenerationFields(tx, task, project.Revision, &record, fields, map[string]any{
			"data.voiceTaskId": strconv.FormatUint(task.ID, 10), "data.voiceError": nil,
		})
	}
	if err := bindCollaborationCandidate(tx, task, fields); err != nil {
		return err
	}
	fields.versions[collaborationGenerationActorKey(task.UserID)] = task.ID
	patch := map[string]any{
		"data.status": "running", "data.taskId": strconv.FormatUint(task.ID, 10), "data.error": nil,
		"data.generationRevision": project.Revision + 1,
	}
	preserveVocalSeparationSource(task, fields, patch)
	return persistGenerationFields(tx, task, project.Revision, &record, fields, patch)
}

// FinalizeCanvasCollaboration 把匹配节点任务与画布代数的结算结果写入协作版本。
func (r *GenerationTaskRepository) FinalizeCanvasCollaboration(ctx context.Context, task *model.GenerationTask) error {
	task.CollaborationAppliedRevision = 0
	if task.CollaborationEpoch == 0 || !model.IsTerminalStatus(task.Status) {
		return nil
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		project, err := lockGenerationCollaboration(tx, task)
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrCanvasCollaborationSession) {
			return nil
		}
		if err != nil {
			return err
		}
		record, fields, err := generationCanvasNode(tx, project.ID, collaborationTaskNode(task))
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if collaborationVoiceTask(task) {
			return finalizeCollaborationVoice(tx, task, project, &record, fields)
		}
		return finalizeGenerationBinding(tx, task, project, &record, fields)
	})
}

func finalizeGenerationBinding(tx *gorm.DB, task *model.GenerationTask, project *model.CanvasProject, record *model.CanvasCollaborationRecord, fields *collaborationFields) error {
	key := collaborationGenerationActorKey(task.UserID)
	bound := fields.versions[key]
	candidate := collaborationCandidateKey(task)
	isCandidate := fields.versions[candidate] != 0
	if (task.CollaborationRequestHash != "" && !isCandidate && task.AgentRunID == 0) || (!isCandidate && bound != 0 && bound != task.ID) {
		return nil
	}
	primary := collaborationString(fields.get("data.taskId")) == strconv.FormatUint(task.ID, 10) && collaborationString(fields.get("data.status")) == "running"
	if !primary && !isCandidate && (bound == 0 || task.Status != model.TaskSucceeded) {
		return nil
	}
	patch, err := collaborationTaskPatch(tx, task, project, fields)
	if err != nil {
		return err
	}
	delete(fields.versions, candidate)
	if primary {
		advancePrimaryCandidate(task, fields, patch, project.Revision+1)
	} else {
		patch = candidateHistoryPatch(fields, patch, isCandidate)
		if patch == nil {
			return nil
		}
	}
	if bound == task.ID {
		delete(fields.versions, key)
	}
	return persistGenerationFields(tx, task, project.Revision, record, fields, patch)
}

func promoteCanvasGenerationFallback(fields *collaborationFields, patch map[string]any) {
	result := fields.collaborationOutput(fields.activeCollaborationOutput())
	if result == nil {
		return
	}
	patch[collaborationOutputField] = result["id"]
	for _, key := range []string{"assetId", "src", "mediaType"} {
		if result[key] != nil {
			patch["data."+key] = result[key]
		}
	}
	patch["data.status"], patch["data.error"] = "done", nil
}

func collaborationTaskPatch(tx *gorm.DB, task *model.GenerationTask, project *model.CanvasProject, fields *collaborationFields) (map[string]any, error) {
	if task.UserID == project.UserID {
		return terminalGenerationPatch(task, fields)
	}
	allowed, err := canGenerateInCollaboration(tx, project, task.UserID)
	if err != nil {
		return nil, err
	}
	if !allowed || task.Status == model.TaskCanceled {
		return collaborationIdlePatch(fields), nil
	}
	if task.Status != model.TaskSucceeded {
		return terminalGenerationPatch(task, fields)
	}
	if len(task.CollaborationMedia) == 0 {
		return collaborationIdlePatch(fields), nil
	}
	outputs, err := attachCollaborationMedia(tx, project.UserID, task.CollaborationMedia)
	if err != nil {
		return nil, err
	}
	return mergeGenerationOutputs(task, fields, map[string]any{"data.status": "idle", "data.error": nil, "data.taskId": nil}, outputs), nil
}

func attachCollaborationMedia(tx *gorm.DB, owner uint64, copies []model.CanvasGenerationMedia) ([]model.TaskOutput, error) {
	outputs := make([]model.TaskOutput, 0, len(copies))
	for _, copy := range copies {
		if copy.Asset == nil || copy.Asset.UserID != owner || copy.Asset.ID != 0 {
			return nil, ErrCanvasCollaborationInput
		}
		stage := tx.Where("storage_key = ? AND storage_id = ? AND attempt = ?", copy.Asset.StorageKey, copy.Asset.StorageID, copy.Attempt).Delete(&model.CanvasDeliveryStage{})
		if stage.Error != nil {
			return nil, stage.Error
		}
		if stage.RowsAffected != 1 {
			return nil, ErrCanvasCollaborationInput
		}
		if err := tx.Create(copy.Asset).Error; err != nil {
			return nil, err
		}
		output := copy.Output
		output.AssetID, output.CoverAssetID = copy.Asset.ID, 0
		outputs = append(outputs, output)
	}
	return outputs, nil
}

func collaborationIdlePatch(fields *collaborationFields) map[string]any {
	status := "idle"
	if collaborationString(fields.get("data.assetId")) != "" {
		status = "done"
	}
	return map[string]any{"data.status": status, "data.error": nil, "data.taskId": nil}
}

func terminalGenerationPatch(task *model.GenerationTask, fields *collaborationFields) (map[string]any, error) {
	patch := map[string]any{"data.status": "idle", "data.error": nil}
	if task.Status == model.TaskSucceeded {
		return newGenerationOutputs(task, fields, patch)
	}
	if collaborationString(fields.get("data.assetId")) != "" {
		patch["data.status"] = "done"
	} else if task.Status != model.TaskCanceled {
		patch["data.status"] = "error"
		patch["data.error"] = "生成未完成"
	}
	return patch, nil
}

func newGenerationOutputs(task *model.GenerationTask, fields *collaborationFields, patch map[string]any) (map[string]any, error) {
	var raw []model.TaskOutput
	if err := json.Unmarshal(task.OutputJSON, &raw); err != nil {
		return nil, err
	}
	return mergeGenerationOutputs(task, fields, patch, raw), nil
}

func mergeGenerationOutputs(task *model.GenerationTask, fields *collaborationFields, patch map[string]any, raw []model.TaskOutput) map[string]any {
	var history []map[string]any
	_ = json.Unmarshal(fields.get("data.outputs"), &history)
	for _, output := range raw {
		if output.AssetID == 0 || output.URL == "" {
			continue
		}
		id := strconv.FormatUint(output.AssetID, 10)
		if knownGenerationOutput(history, id) {
			continue
		}
		if patch["data.assetId"] == nil {
			patch["data.assetId"], patch["data.activeOutputId"] = id, id
			patch["data.src"], patch["data.mediaType"] = output.URL, output.MediaType
			if output.Width > 0 && output.Height > 0 {
				patch["data.mediaAspect"] = float64(output.Width) / float64(output.Height)
			}
		}
		entry := map[string]any{"id": id, "assetId": id, "src": output.URL, "mediaType": output.MediaType,
			"model": task.ModelKey, "createdAt": task.UpdatedAt.UnixMilli()}
		if output.Stem != "" {
			entry["stem"] = output.Stem
		}
		history = append(history, entry)
	}
	if len(history) > 0 {
		patch["data.outputs"] = history
		patch["data.status"] = "done"
	}
	return patch
}

func knownGenerationOutput(history []map[string]any, assetID string) bool {
	for _, output := range history {
		if output["id"] == assetID {
			return true
		}
	}
	return false
}

func persistGenerationFields(tx *gorm.DB, task *model.GenerationTask, revision uint64, record *model.CanvasCollaborationRecord, fields *collaborationFields, patch map[string]any) error {
	if err := persistCanvasTaskFields(tx, task.CollaborationEpoch, revision, record, fields, patch); err != nil {
		return err
	}
	task.CollaborationAppliedRevision = revision + 1
	return nil
}

func persistCanvasTaskFields(tx *gorm.DB, epoch, revision uint64, record *model.CanvasCollaborationRecord, fields *collaborationFields, patch map[string]any) error {
	changes := make(map[string]json.RawMessage, len(patch))
	for field, value := range patch {
		encoded, err := json.Marshal(value)
		if err != nil {
			return err
		}
		fields.set(field, encoded)
		fields.versions[field] = revision + 1
		changes[field] = encoded
	}
	if err := fields.save(record, revision+1); err != nil {
		return err
	}
	event := &CanvasCollaborationCommit{Epoch: epoch, Revision: revision + 1,
		Changes: []CanvasCollaborationChange{{Kind: "node", ID: record.RecordID, Fields: changes}}}
	return persistCollaborationBatch(tx, []model.CanvasCollaborationRecord{*record}, model.CanvasCollaborationReceipt{CanvasID: record.CanvasID}, event, nil)
}

func scopedGenerationURL(canvasID uint64, assetID json.RawMessage) json.RawMessage {
	asset, err := strconv.ParseUint(collaborationString(assetID), 10, 64)
	if err != nil || asset == 0 {
		return json.RawMessage("null")
	}
	url, _ := json.Marshal("/api/v1/canvas-media/" + idcodec.Encode(canvasID) + "/" + idcodec.Encode(asset))
	return url
}

func projectGenerationEvent(canvasID uint64, changes []CanvasCollaborationChange) error {
	for index := range changes {
		change := &changes[index]
		if change.Kind == "node" && change.Action == "create" {
			if err := projectCollaborationCreation(canvasID, change); err != nil {
				return err
			}
		}
		fields := change.Fields
		if err := projectStructuredFields(canvasID, fields); err != nil {
			return err
		}
		delete(fields, collaborationGenerationInputField)
		delete(fields, "data.taskId")
		delete(fields, "data.error")
		if fields["data.src"] != nil {
			fields["data.src"] = scopedGenerationURL(canvasID, fields["data.assetId"])
		}
		if raw := fields["data.outputs"]; raw != nil {
			outputs, err := scopedGenerationHistory(canvasID, raw)
			if err != nil {
				return err
			}
			fields["data.outputs"] = outputs
		}
	}
	return nil
}

func projectCollaborationCreation(canvasID uint64, change *CanvasCollaborationChange) error {
	body, err := decodeCollaborationFields(change.Value, []byte(`{}`))
	if err != nil {
		return err
	}
	if body.data["src"] != nil {
		body.data["src"] = scopedGenerationURL(canvasID, body.data["assetId"])
	}
	if raw := body.data["outputs"]; raw != nil {
		outputs, err := scopedGenerationHistory(canvasID, raw)
		if err != nil {
			return err
		}
		body.data["outputs"] = outputs
	}
	for field := range collaborationStructuredKinds {
		if raw := body.get(field); raw != nil {
			projected, err := scopedStructuredValue(canvasID, field, raw)
			if err != nil {
				return err
			}
			body.set(field, projected)
		}
	}
	body.body["data"], err = json.Marshal(body.data)
	if err != nil {
		return err
	}
	change.Value, err = json.Marshal(body.body)
	return err
}

func scopedGenerationHistory(canvasID uint64, raw json.RawMessage) (json.RawMessage, error) {
	var items []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	projected := make([]map[string]json.RawMessage, 0, len(items))
	for _, item := range items {
		asset := item["assetId"]
		if asset == nil {
			asset = item["id"]
		}
		url := scopedGenerationURL(canvasID, asset)
		if string(url) == "null" {
			continue
		}
		safe := map[string]json.RawMessage{"src": url}
		for _, key := range []string{"id", "assetId", "mediaType", "model", "createdAt", "mediaAspect", "width", "height", "durationMs"} {
			if item[key] != nil {
				safe[key] = item[key]
			}
		}
		projected = append(projected, safe)
	}
	return json.Marshal(projected)
}
