package repository

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"video-canvas/internal/model"
)

// ErrCanvasVersionUnavailable 表示历史版本没有可用的检查点或可回放日志。
var ErrCanvasVersionUnavailable = errors.New("此版本的历史快照暂不可用")

func checkpointCollaborationEvent(tx *gorm.DB, event model.CanvasCollaborationEvent) error {
	const interval = 24
	const largeEvent = 128 * 1024
	if event.Revision%interval != 0 && len(event.Changes) < largeEvent {
		return nil
	}
	const query = `INSERT INTO canvas_collaboration_checkpoints (canvas_id, epoch, revision, payload_json)
		SELECT project.id, ?, ?, project.payload_json || jsonb_build_object(
			'nodes', COALESCE(graph.nodes, '[]'::jsonb), 'edges', COALESCE(graph.edges, '[]'::jsonb))
		FROM canvas_projects AS project
		CROSS JOIN LATERAL (
			SELECT jsonb_agg(body ORDER BY ordinal, kind, record_id) FILTER (WHERE kind <> 'edge') AS nodes,
				jsonb_agg(body ORDER BY ordinal, kind, record_id) FILTER (WHERE kind = 'edge') AS edges
			FROM canvas_collaboration_records WHERE canvas_id = project.id AND NOT deleted
		) AS graph
		WHERE project.id = ? AND project.deleted_at IS NULL`
	created := tx.Exec(query, event.Epoch, event.Revision, event.CanvasID)
	if created.Error == nil && created.RowsAffected == 0 {
		return ErrNotFound
	}
	return created.Error
}

// CanvasHistoryEntry 是一次已确认版本的变更摘要。
type CanvasHistoryEntry struct {
	Revision  uint64
	ActorID   uint64
	Source    string
	CreatedAt time.Time
	Targets   datatypes.JSON
}

// History 读取当前代数的编辑活动。查询只抽取记录标识和动作，不返回历史正文。
func (r *CanvasCollaborationRepository) History(ctx context.Context, user, canvas, epoch, before uint64) ([]CanvasHistoryEntry, error) {
	rows := make([]CanvasHistoryEntry, 0)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := readCollaborationEditor(tx, user, canvas, epoch); err != nil {
			return err
		}
		const query = `WITH selected AS (
			SELECT revision, actor_id, source, created_at, changes FROM canvas_collaboration_events
			WHERE canvas_id = ? AND epoch = ? AND (? = 0 OR revision < ?)
			UNION ALL
			SELECT revision, 0::bigint, '', created_at, '[]'::jsonb FROM canvas_collaboration_checkpoints
			WHERE canvas_id = ? AND epoch = ? AND (? = 0 OR revision < ?) AND baseline
			ORDER BY revision DESC LIMIT 31
		)
		SELECT revision, actor_id, source, created_at,
			COALESCE((SELECT jsonb_agg(jsonb_build_object('kind', change->>'kind', 'id', change->>'id', 'action', COALESCE(change->>'action','update')))
			 FROM jsonb_array_elements(changes) AS change), '[]'::jsonb) AS targets
		FROM selected ORDER BY revision DESC`
		return tx.Raw(query, canvas, epoch, before, before, canvas, epoch, before, before).Scan(&rows).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return rows, err
}

// HistoricalSnapshot 仅向当前所有者返回指定协作代数的已确认图谱。
func (r *CanvasCollaborationRepository) HistoricalSnapshot(ctx context.Context, owner, canvas, epoch, revision uint64) (*model.CanvasProject, error) {
	var project model.CanvasProject
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ? AND user_id = ? AND drama_id IS NULL", canvas, owner).Take(&project).Error; err != nil {
			return translate(err)
		}
		return loadHistoricalGraph(tx, &project, epoch, revision)
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return &project, err
}

func loadHistoricalGraph(tx *gorm.DB, project *model.CanvasProject, epoch, revision uint64) error {
	if !project.CollaborationActive || project.CollaborationEpoch != epoch {
		return ErrCanvasCollaborationSession
	}
	if revision == project.Revision {
		return hydrateCollaborationProject(tx, project)
	}
	if revision > project.Revision || revision == 0 {
		return ErrCanvasVersionUnavailable
	}
	var checkpoint model.CanvasCollaborationCheckpoint
	err := tx.Where("canvas_id = ? AND epoch = ? AND revision <= ?", project.ID, epoch, revision).Order("revision DESC").Take(&checkpoint).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrCanvasVersionUnavailable
	}
	if err != nil {
		return err
	}
	payload, err := replayHistoricalGraph(tx, checkpoint, revision)
	if err != nil {
		return err
	}
	project.Revision, project.PayloadJSON = revision, payload
	return nil
}

func replayHistoricalGraph(tx *gorm.DB, checkpoint model.CanvasCollaborationCheckpoint, revision uint64) ([]byte, error) {
	const replayByteLimit = 8 * 1024 * 1024
	if revision-checkpoint.Revision > 2000 {
		return nil, ErrCanvasVersionUnavailable
	}
	var events []model.CanvasCollaborationEvent
	err := tx.Raw(`WITH replay_rows AS (
		SELECT revision, changes, SUM(octet_length(changes::text)) OVER (ORDER BY revision) AS byte_count
		FROM canvas_collaboration_events WHERE canvas_id = ? AND epoch = ? AND revision > ? AND revision <= ?
	)
SELECT revision, changes FROM replay_rows WHERE byte_count <= ? ORDER BY revision`,
		checkpoint.CanvasID, checkpoint.Epoch, checkpoint.Revision, revision, replayByteLimit).Scan(&events).Error
	if err != nil {
		return nil, err
	}
	if uint64(len(events)) != revision-checkpoint.Revision {
		return nil, ErrCanvasVersionUnavailable
	}
	return assembleHistoricalGraph(checkpoint, events)
}

func assembleHistoricalGraph(checkpoint model.CanvasCollaborationCheckpoint, events []model.CanvasCollaborationEvent) ([]byte, error) {
	metadata, err := collaborationMetadata(checkpoint.PayloadJSON)
	if err != nil {
		return nil, err
	}
	graph := historicalCanvasGraph{records: make(map[[2]string]model.CanvasCollaborationRecord)}
	for key, kind := range map[string]string{"nodes": "node", "edges": "edge"} {
		project := &model.CanvasProject{BaseModel: model.BaseModel{ID: checkpoint.CanvasID}, Revision: checkpoint.Revision - 1}
		records, err := splitCollaborationRecords(metadata[key], project, kind)
		if err != nil {
			return nil, err
		}
		for _, record := range records {
			graph.records[[2]string{record.Kind, record.RecordID}] = record
			graph.ordinal = max(graph.ordinal, record.Ordinal)
		}
	}
	for index, event := range events {
		if event.Revision != checkpoint.Revision+uint64(index)+1 {
			return nil, ErrCanvasVersionUnavailable
		}
		if err := graph.apply(event); err != nil {
			return nil, err
		}
	}
	records := slices.Collect(maps.Values(graph.records))
	slices.SortFunc(records, compareHistoricalCanvasRecords)
	return assembleCollaborationPayload(metadata, records)
}

type historicalCanvasGraph struct {
	records map[[2]string]model.CanvasCollaborationRecord
	ordinal int
}

func (graph *historicalCanvasGraph) apply(event model.CanvasCollaborationEvent) error {
	var changes []CanvasCollaborationChange
	if err := json.Unmarshal(event.Changes, &changes); err != nil {
		return err
	}
	for _, change := range changes {
		key := [2]string{change.Kind, change.ID}
		switch change.Action {
		case "create":
			graph.ordinal++
			graph.records[key] = model.CanvasCollaborationRecord{Kind: change.Kind, RecordID: change.ID, Ordinal: graph.ordinal, Body: datatypes.JSON(change.Value)}
		case "delete":
			delete(graph.records, key)
		default:
			record, err := applyHistoricalFields(graph.records[key], change.Fields, event.Revision)
			if err != nil {
				return err
			}
			graph.records[key] = record
		}
	}
	return nil
}

func applyHistoricalFields(record model.CanvasCollaborationRecord, changes map[string]json.RawMessage, revision uint64) (model.CanvasCollaborationRecord, error) {
	if record.RecordID == "" {
		return record, ErrCanvasVersionUnavailable
	}
	fields, err := decodeCollaborationFields(record.Body, []byte("{}"))
	if err != nil {
		return record, err
	}
	if output := changes[collaborationOutputField]; output != nil && changes["data.src"] == nil && !fields.selectCollaborationOutput(output) {
		return record, ErrCanvasVersionUnavailable
	}
	for field, value := range changes {
		fields.set(field, value)
	}
	if err := fields.save(&record, revision); err != nil {
		return record, err
	}
	return record, nil
}

func compareHistoricalCanvasRecords(first, second model.CanvasCollaborationRecord) int {
	if order := cmp.Compare(first.Ordinal, second.Ordinal); order != 0 {
		return order
	}
	if order := cmp.Compare(first.Kind, second.Kind); order != 0 {
		return order
	}
	return cmp.Compare(first.RecordID, second.RecordID)
}
