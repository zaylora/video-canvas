package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"strings"

	"gorm.io/gorm"

	"video-canvas/internal/agent/canvasgraph"
	"video-canvas/internal/model"
)

// CheckAgentCanvasEpoch 校验执行时的会话代数和生成授权。
func (r *AgentRepository) CheckAgentCanvasEpoch(ctx context.Context, userID, canvasID, epoch uint64) error {
	db := r.db.WithContext(ctx)
	var project model.CanvasProject
	if err := db.Select("id", "user_id", "collaboration_active", "collaboration_epoch").Where("id = ?", canvasID).Take(&project).Error; err != nil {
		return translate(err)
	}
	if !project.CollaborationActive {
		if project.UserID != userID {
			return ErrNotFound
		}
		if epoch != 0 {
			return ErrCanvasCollaborationSession
		}
		return nil
	}
	if err := requireCanvasGeneration(db, &project, userID); err != nil {
		return err
	}
	if epoch == 0 || epoch != project.CollaborationEpoch {
		return ErrCanvasCollaborationSession
	}
	return nil
}

func commitAgentCollaboration(tx *gorm.DB, in CommitInput) error {
	project, _, err := lockEditableCollaboration(tx, in.CanvasID, in.UserID, in.CollaborationEpoch)
	if err != nil {
		return err
	}
	if err := requireCanvasGeneration(tx, project, in.UserID); err != nil {
		return err
	}
	if project.Revision != in.BaseRevision {
		return ErrRevisionConflict
	}
	batch, err := AgentCollaborationBatch(in)
	if err != nil {
		return err
	}
	if err := resolveAgentCollaborationKinds(tx, in.CanvasID, batch.Changes); err != nil {
		return err
	}
	if err := validateCollaborationBatch(batch); err != nil {
		return err
	}
	result, err := applyCollaborationBatch(tx, project, batch, model.CanvasCollaborationReceipt{CanvasID: in.CanvasID,
		ActorID: in.UserID, OpID: batch.OpID, RequestHash: batch.OpID}, collaborationImports{staged: in.MediaImports})
	if err != nil {
		return err
	}
	if result.Revision != in.BaseRevision+1 || len(result.Conflicts) > 0 {
		return ErrRevisionConflict
	}
	return recordConfirmedAgentCreations(in.Mutation, result.Changes)
}

func recordConfirmedAgentCreations(mutation *model.AgentMutation, confirmed []CanvasCollaborationChange) error {
	var graph canvasgraph.Graph
	for _, change := range confirmed {
		if change.Action != "create" || change.Kind == "edge" {
			continue
		}
		var node canvasgraph.Node
		if err := json.Unmarshal(change.Value, &node); err != nil {
			return err
		}
		graph.Nodes = append(graph.Nodes, node)
	}
	if len(graph.Nodes) == 0 {
		return nil
	}
	created := make(map[string]map[string]any, len(graph.Nodes))
	for _, change := range canvasgraph.Diff(&canvasgraph.Graph{}, &graph) {
		created[change.ID] = change.After
	}
	var logged []canvasgraph.Change
	if err := json.Unmarshal(mutation.ChangesJSON, &logged); err != nil {
		return err
	}
	for index := range logged {
		change := &logged[index]
		if after := created[change.ID]; change.Kind == "node" && change.Op == canvasgraph.ChangeCreate && after != nil {
			change.After = after
		}
	}
	encoded, err := json.Marshal(logged)
	if err == nil {
		mutation.ChangesJSON = encoded
	}
	return err
}

// AgentCollaborationBatch 将小镜变更转成可校验的协作字段，供事务和素材暂存使用。
func AgentCollaborationBatch(in CommitInput) (CanvasCollaborationBatch, error) {
	changes, err := agentCollaborationChanges(in.Mutation.ChangesJSON)
	digest := sha256.Sum256([]byte(fmt.Sprintf("%d/%d/%s/%s", in.Mutation.RunID, in.BaseRevision, in.Mutation.ToolCallID, in.Mutation.IdemKey)))
	return CanvasCollaborationBatch{OpID: hex.EncodeToString(digest[:]), Epoch: in.CollaborationEpoch, BaseRevision: in.BaseRevision, Changes: changes}, err
}

func resolveAgentCollaborationKinds(tx *gorm.DB, canvas uint64, changes []CanvasCollaborationChange) error {
	for index := range changes {
		change := &changes[index]
		if change.Kind != "node" || change.Action == "create" {
			continue
		}
		var record model.CanvasCollaborationRecord
		if err := tx.Select("kind").Where("canvas_id = ? AND record_id = ? AND kind IN ('node','group') AND NOT deleted", canvas, change.ID).Take(&record).Error; err != nil {
			return translate(err)
		}
		change.Kind = record.Kind
	}
	return nil
}

func requireCanvasGeneration(tx *gorm.DB, project *model.CanvasProject, userID uint64) error {
	allowed, err := canGenerateInCollaboration(tx, project, userID)
	if err == nil && !allowed {
		return ErrNotFound
	}
	return err
}

func agentCollaborationChanges(raw []byte) ([]CanvasCollaborationChange, error) {
	var changes []canvasgraph.Change
	if err := json.Unmarshal(raw, &changes); err != nil {
		return nil, err
	}
	out := make([]CanvasCollaborationChange, 0, len(changes))
	for _, change := range changes {
		converted, err := agentCollaborationChange(change)
		if err != nil {
			return nil, err
		}
		if converted.Action != "" || len(converted.Fields) > 0 {
			out = append(out, converted)
		}
	}
	return out, nil
}

func agentCollaborationChange(change canvasgraph.Change) (CanvasCollaborationChange, error) {
	kind := change.Kind
	if change.After["type"] == "group" || change.Before["type"] == "group" {
		kind = "group"
	}
	out := CanvasCollaborationChange{ID: change.ID, Kind: kind}
	switch change.Op {
	case canvasgraph.ChangeDelete:
		out.Action = "delete"
	case canvasgraph.ChangeCreate:
		out.Action = "create"
		out.Value, _ = json.Marshal(agentCollaborationNodeBody(kind, change.After))
	case canvasgraph.ChangeUpdate:
		out.Fields = make(map[string]json.RawMessage)
		fields := maps.Clone(change.After)
		for field := range change.Before {
			if _, exists := fields[field]; !exists {
				fields[field] = agentClearedField(field)
			}
		}
		for field, value := range fields {
			if _, changesText := fields["data.text"]; changesText && (field == "data.status" || field == "data.error" || field == "data.errorTaskRef") {
				continue
			}
			raw, _ := json.Marshal(agentCollaborationOptions(field, value))
			out.Fields[field] = raw
		}
	default:
		return out, ErrCanvasCollaborationInvalidBatch
	}
	return out, nil
}

func agentClearedField(field string) any {
	switch field {
	case "data.prompt", "data.text", "data.model":
		return ""
	case "data.params":
		return map[string]any{}
	default:
		return nil
	}
}

func agentCollaborationNodeBody(kind string, fields map[string]any) map[string]any {
	body := make(map[string]any)
	data := make(map[string]any)
	for field, value := range fields {
		if field == "id" || field == "type" || agentDerivedCreationField(field) {
			continue
		}
		if key, nested := strings.CutPrefix(field, "data."); nested && kind != "edge" {
			data[key] = agentCollaborationOptions(field, value)
		} else {
			body[field] = value
		}
	}
	if kind != "edge" {
		body["data"] = data
	}
	return body
}

func agentDerivedCreationField(field string) bool {
	switch field {
	case "data.status", "data.src", "data.mediaType", "data.fileName", "data.uploaded", "data.mediaAspect", "data.outputs", "data.activeOutputId", "data.error":
		return true
	default:
		return false
	}
}

func agentCollaborationOptions(field string, value any) any {
	if params, ok := value.(map[string]any); field == "data.params" && ok {
		options := make(map[string]any)
		for key, item := range params {
			raw, _ := json.Marshal(item)
			if editableCollaborationOption(key, raw) {
				options[key] = item
			}
		}
		return options
	}
	return value
}
