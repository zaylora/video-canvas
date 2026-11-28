package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"maps"
	"unicode/utf8"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
)

// 画布模式和迁移产生的协作错误。
var (
	ErrCanvasCollaborationActive          = errors.New("画布正在协作，请使用实时协作入口")
	ErrCanvasCollaborationInvalidDocument = errors.New("画布包含无法迁移到协作模式的记录")
)

// CanvasCollaborationRepository 负责协作模式、提交与恢复的事务边界。
type CanvasCollaborationRepository struct{ db *gorm.DB }

// NewCanvasCollaborationRepository 创建协作仓储。
func NewCanvasCollaborationRepository(db *gorm.DB) *CanvasCollaborationRepository {
	return &CanvasCollaborationRepository{db: db}
}

// SetActive 按确认版本切换普通画布的权威存储，并使旧协作会话失效。
func (r *CanvasCollaborationRepository) SetActive(ctx context.Context, ownerID, canvasID, revision uint64, active bool) (*model.CanvasProject, error) {
	return r.setActive(ctx, ownerID, canvasID, revision, active, "")
}

// SetActiveConfirmed 复核模式切换预览后提交。旧客户端可仅提供确认版本。
func (r *CanvasCollaborationRepository) SetActiveConfirmed(ctx context.Context, ownerID, canvasID, revision uint64, active bool, preview string) (*model.CanvasProject, error) {
	if preview != "" && len(preview) != 64 {
		return nil, ErrCanvasCollaborationInvalidBatch
	}
	return r.setActive(ctx, ownerID, canvasID, revision, active, preview)
}

func (r *CanvasCollaborationRepository) setActive(ctx context.Context, ownerID, canvasID, revision uint64, active bool, expectedPreview string) (*model.CanvasProject, error) {
	var project model.CanvasProject
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND user_id = ? AND drama_id IS NULL", canvasID, ownerID).Take(&project).Error; err != nil {
			return translate(err)
		}
		if project.Revision != revision {
			return ErrRevisionConflict
		}
		if expectedPreview != "" {
			preview, err := canvasModePreview(tx, &project, active)
			if err != nil {
				return err
			}
			if preview.Preview != expectedPreview {
				return ErrRevisionConflict
			}
		}
		if project.CollaborationActive == active {
			return hydrateCollaborationProject(tx, &project)
		}
		return switchCanvasCollaboration(tx, &project, active)
	})
	if err != nil {
		return nil, err
	}
	return &project, nil
}

func switchCanvasCollaboration(tx *gorm.DB, project *model.CanvasProject, active bool) error {
	if active {
		if err := NewCanvasShareRepository(tx).Ready(tx.Statement.Context, project.UserID, project.ID); err != nil {
			return err
		}
		checkpoint := model.CanvasCollaborationCheckpoint{
			CanvasID: project.ID, Epoch: project.CollaborationEpoch + 1, Revision: project.Revision + 1, PayloadJSON: project.PayloadJSON, Baseline: true,
		}
		if err := splitCollaborationProject(tx, project); err != nil {
			return err
		}
		if err := tx.Create(&checkpoint).Error; err != nil {
			return err
		}
	} else if err := materializeRetiredCollaboration(tx, project); err != nil {
		return err
	}
	project.CollaborationActive = active
	project.CollaborationEpoch++
	project.Revision++
	if err := tx.Model(project).Select("PayloadJSON", "CollaborationActive", "CollaborationEpoch", "Revision", "UpdatedAt").Updates(project).Error; err != nil {
		return err
	}
	return hydrateCollaborationProject(tx, project)
}

func materializeRetiredCollaboration(tx *gorm.DB, project *model.CanvasProject) error {
	if err := hydrateCollaborationProject(tx, project); err != nil {
		return err
	}
	metadata, err := collaborationMetadata(project.PayloadJSON)
	if err != nil {
		return err
	}
	var nodes []map[string]json.RawMessage
	if err := json.Unmarshal(metadata["nodes"], &nodes); err != nil {
		return err
	}
	for _, node := range nodes {
		if err := clearRetiredCollaborationNode(node); err != nil {
			return err
		}
	}
	metadata["nodes"], err = json.Marshal(nodes)
	if err != nil {
		return err
	}
	project.PayloadJSON, err = json.Marshal(metadata)
	return err
}

func clearRetiredCollaborationNode(node map[string]json.RawMessage) error {
	var data map[string]json.RawMessage
	if raw := node["data"]; len(raw) > 0 && json.Unmarshal(raw, &data) != nil {
		return ErrCanvasCollaborationInvalidDocument
	}
	if data == nil {
		return nil
	}
	if collaborationString(data["status"]) == "running" {
		data["status"] = json.RawMessage(`"idle"`)
	}
	for _, key := range []string{"taskId", "pendingTaskIds", "generationInput", "generationRevision", "voiceTaskId", "characterImageTaskId", "characterImageAssetId", "storyboardSplitRunning"} {
		delete(data, key)
	}
	encoded, err := json.Marshal(data)
	node["data"] = encoded
	return err
}

func splitCollaborationProject(tx *gorm.DB, project *model.CanvasProject) error {
	metadata, err := collaborationMetadata(project.PayloadJSON)
	if err != nil {
		return err
	}
	vertices, err := splitCollaborationRecords(metadata["nodes"], project, "node")
	if err != nil {
		return err
	}
	edges, err := splitCollaborationRecords(metadata["edges"], project, "edge")
	if err != nil {
		return err
	}
	if err := validateCollaborationBaseline(project.ID, project.Revision+1, vertices, edges); err != nil {
		return err
	}
	if err := tx.Where("canvas_id = ?", project.ID).Delete(&model.CanvasCollaborationRecord{}).Error; err != nil {
		return err
	}
	if records := append(vertices, edges...); len(records) > 0 {
		if err := tx.CreateInBatches(records, 200).Error; err != nil {
			return err
		}
	}
	delete(metadata, "nodes")
	delete(metadata, "edges")
	project.PayloadJSON, err = json.Marshal(metadata)
	return err
}

func splitCollaborationRecords(raw json.RawMessage, project *model.CanvasProject, kind string) ([]model.CanvasCollaborationRecord, error) {
	var bodies []json.RawMessage
	if len(raw) > 0 && json.Unmarshal(raw, &bodies) != nil {
		return nil, ErrCanvasCollaborationInvalidDocument
	}
	seen := make(map[string]bool, len(bodies))
	records := make([]model.CanvasCollaborationRecord, 0, len(bodies))
	for ordinal, body := range bodies {
		var identity struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		}
		if json.Unmarshal(body, &identity) != nil || identity.ID == "" || utf8.RuneCountInString(identity.ID) > 128 || seen[identity.ID] {
			return nil, ErrCanvasCollaborationInvalidDocument
		}
		seen[identity.ID] = true
		recordKind := kind
		if kind == "node" && identity.Type == "group" {
			recordKind = "group"
		}
		records = append(records, model.CanvasCollaborationRecord{CanvasID: project.ID, Kind: recordKind,
			RecordID: identity.ID, Ordinal: ordinal, Body: datatypes.JSON(body), Revision: project.Revision + 1})
	}
	return records, nil
}

func collaborationMetadata(payload []byte) (map[string]json.RawMessage, error) {
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal(payload, &metadata); err != nil || metadata == nil {
		return nil, ErrCanvasCollaborationInvalidDocument
	}
	return metadata, nil
}

func hydrateCollaborationProject(tx *gorm.DB, project *model.CanvasProject) error {
	if !project.CollaborationActive {
		return nil
	}
	metadata, err := collaborationMetadata(project.PayloadJSON)
	if err != nil {
		return err
	}
	var records []model.CanvasCollaborationRecord
	if err := tx.Where("canvas_id = ? AND deleted = false", project.ID).Order("ordinal, kind, record_id").Find(&records).Error; err != nil {
		return err
	}
	project.PayloadJSON, err = assembleCollaborationPayload(metadata, records)
	return err
}

func assembleCollaborationPayload(metadata map[string]json.RawMessage, records []model.CanvasCollaborationRecord) ([]byte, error) {
	graph := maps.Clone(metadata)
	nodes := make([]json.RawMessage, 0, len(records))
	edges := make([]json.RawMessage, 0)
	for _, record := range records {
		if record.Kind == "edge" {
			edges = append(edges, json.RawMessage(record.Body))
		} else {
			nodes = append(nodes, json.RawMessage(record.Body))
		}
	}
	var err error
	if graph["nodes"], err = json.Marshal(nodes); err != nil {
		return nil, err
	}
	if graph["edges"], err = json.Marshal(edges); err != nil {
		return nil, err
	}
	return json.Marshal(graph)
}

func readCanvasWithCollaboration(db *gorm.DB, read func(*gorm.DB, *model.CanvasProject) error) (*model.CanvasProject, error) {
	var project model.CanvasProject
	if err := read(db, &project); err != nil {
		return nil, translate(err)
	}
	if !project.CollaborationActive {
		return &project, nil
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := read(tx, &project); err != nil {
			return translate(err)
		}
		return hydrateCollaborationProject(tx, &project)
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	return &project, nil
}
