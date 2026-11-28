package repository

import (
	"encoding/json"
	"fmt"
	"slices"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"video-canvas/internal/model"
)

type collaborationLink struct {
	Kind     string
	RecordID string
	Parent   string
	Source   string
	Target   string
	Revision uint64
	Ordinal  int
}

func hasCollaborationStructure(changes []CanvasCollaborationChange) bool {
	return slices.ContainsFunc(changes, func(change CanvasCollaborationChange) bool {
		return change.Action != "" || change.Fields["parentId"] != nil
	})
}

func createCollaborationRecord(canvasID, revision uint64, change CanvasCollaborationChange) (model.CanvasCollaborationRecord, error) {
	body, err := collaborationMetadata(change.Value)
	if err != nil || !validCollaborationCreation(change.Kind, body) {
		return model.CanvasCollaborationRecord{}, ErrCanvasCollaborationInvalidBatch
	}
	if change.Kind != "edge" {
		typeName := change.Kind
		if typeName == "node" {
			typeName = "canvas"
		}
		body["type"], _ = json.Marshal(typeName)
	}
	body["id"], _ = json.Marshal(change.ID)
	encoded, err := json.Marshal(body)
	return model.CanvasCollaborationRecord{CanvasID: canvasID, Kind: change.Kind, RecordID: change.ID,
		Body: datatypes.JSON(encoded), Revision: revision, FieldVersions: datatypes.JSON(fmt.Appendf(nil, `{"$created":%d}`, revision))}, err
}

func validCollaborationCreation(kind string, body map[string]json.RawMessage) bool {
	if kind == "edge" {
		return validCollaborationEdgeBody(body)
	}
	data, err := collaborationMetadata(body["data"])
	if err != nil || !validCollaborationField(kind, "position", body["position"]) || !validCollaborationField(kind, "data.label", data["label"]) {
		return false
	}
	for key, value := range body {
		if key != "data" && !validCollaborationField(kind, key, value) {
			return false
		}
	}
	return validCollaborationCreationData(kind, data)
}

func validCollaborationCreationData(kind string, data map[string]json.RawMessage) bool {
	nodeKind := collaborationString(data["kind"])
	if kind == "node" && !slices.Contains([]string{"script", "image", "video", "audio", "character", "director", "storyboard"}, nodeKind) {
		return false
	}
	for key, value := range data {
		if key == "kind" && kind == "node" {
			continue
		}
		name := "data." + key
		if key == "outpaintFrom" {
			if kind != "node" || nodeKind != "image" || !validCollaborationRecordID(collaborationString(value)) {
				return false
			}
			continue
		}
		if expected := collaborationStructuredKinds[name]; expected != "" && nodeKind != expected {
			return false
		}
		if name == collaborationGenerationInputField || name == collaborationOutputField || !validCollaborationField(kind, name, value) {
			return false
		}
	}
	return true
}

func validCollaborationEdgeBody(body map[string]json.RawMessage) bool {
	if !validCollaborationRecordID(collaborationString(body["source"])) || !validCollaborationRecordID(collaborationString(body["target"])) {
		return false
	}
	for key, value := range body {
		switch key {
		case "source", "target":
		case "sourceHandle", "targetHandle":
			var handle *string
			if json.Unmarshal(value, &handle) != nil || (handle != nil && !validCollaborationRecordID(*handle)) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func markCollaborationDeleted(record *model.CanvasCollaborationRecord, revision uint64) {
	record.Deleted = true
	record.Body = datatypes.JSON(`{}`)
	record.FieldVersions = datatypes.JSON(`{}`)
	record.Revision = revision
}

func applyCollaborationStructure(tx *gorm.DB, canvasID, base uint64, records []model.CanvasCollaborationRecord, result *CanvasCollaborationCommit) ([]model.CanvasCollaborationRecord, error) {
	links, err := liveCollaborationLinks(tx, canvasID)
	if err != nil {
		return nil, err
	}
	edges := make(map[string]collaborationLink)
	vertices := make(map[string]collaborationLink)
	ordinal := 0
	for _, link := range links {
		ordinal = max(ordinal, link.Ordinal)
		if link.Kind == "edge" {
			edges[link.RecordID] = link
		} else {
			vertices[link.RecordID] = link
		}
	}
	removed := make(map[string]bool)
	for index := range records {
		record := &records[index]
		linksByID := vertices
		if record.Kind == "edge" {
			linksByID = edges
		}
		if record.Deleted {
			delete(linksByID, record.RecordID)
			if record.Kind == "node" {
				removed[record.RecordID] = true
			}
			continue
		}
		if previous := linksByID[record.RecordID]; previous.Kind != "" && previous.Kind != record.Kind {
			return nil, ErrCanvasCollaborationInvalidBatch
		}
		if result.Changes[index].Action == "create" {
			ordinal++
			record.Ordinal = ordinal
		}
		linksByID[record.RecordID] = collaborationRecordLink(*record)
	}
	return closeCollaborationStructure(canvasID, base, records, result, vertices, edges, removed)
}

func liveCollaborationLinks(tx *gorm.DB, canvasID uint64) ([]collaborationLink, error) {
	var links []collaborationLink
	const linkFields = `kind, record_id, ordinal, revision, body->>'parentId' AS parent, body->>'source' AS source, body->>'target' AS target`
	err := tx.Model(&model.CanvasCollaborationRecord{}).Select(linkFields).
		Where("canvas_id = ? AND NOT deleted", canvasID).Order("kind, record_id").Scan(&links).Error
	return links, err
}

func validateCollaborationBaseline(canvasID, revision uint64, vertexRecords, edgeRecords []model.CanvasCollaborationRecord) error {
	vertices := make(map[string]collaborationLink, len(vertexRecords))
	for _, record := range vertexRecords {
		vertices[record.RecordID] = collaborationRecordLink(record)
	}
	edges := make(map[string]collaborationLink, len(edgeRecords))
	for _, record := range edgeRecords {
		edges[record.RecordID] = collaborationRecordLink(record)
	}
	baseline := &CanvasCollaborationCommit{Revision: revision}
	if _, err := closeCollaborationStructure(canvasID, revision, nil, baseline, vertices, edges, nil); err != nil {
		return ErrCanvasCollaborationInvalidDocument
	}
	return nil
}

func closeCollaborationStructure(canvasID, base uint64, records []model.CanvasCollaborationRecord, result *CanvasCollaborationCommit,
	vertices, edges map[string]collaborationLink, removed map[string]bool,
) ([]model.CanvasCollaborationRecord, error) {
	for _, vertex := range vertices {
		if vertex.Parent != "" && (vertex.Kind != "node" || vertices[vertex.Parent].Kind != "group") {
			return nil, ErrCanvasCollaborationInvalidBatch
		}
	}
	endpoints := make(map[[2]string]collaborationLink, len(edges))
	edgeIDs := make([]string, 0, len(edges))
	for id := range edges {
		edgeIDs = append(edgeIDs, id)
	}
	slices.Sort(edgeIDs)
	for _, id := range edgeIDs {
		edge := edges[id]
		if removed[edge.Source] || removed[edge.Target] {
			if edge.Revision > base {
				result.Conflicts = append(result.Conflicts, CanvasCollaborationConflict{Kind: "edge", ID: edge.RecordID, Revision: edge.Revision})
				continue
			}
			if len(result.Changes) >= 500 {
				return nil, ErrCanvasCollaborationInvalidBatch
			}
			record := model.CanvasCollaborationRecord{CanvasID: canvasID, Kind: "edge", RecordID: edge.RecordID}
			markCollaborationDeleted(&record, result.Revision+1)
			records = append(records, record)
			result.Changes = append(result.Changes, CanvasCollaborationChange{Kind: "edge", ID: edge.RecordID, Action: "delete"})
			continue
		}
		if err := checkCollaborationEdgePair(vertices, endpoints, edge, base, result); err != nil {
			return nil, err
		}
	}
	return records, nil
}

func checkCollaborationEdgePair(vertices map[string]collaborationLink, endpoints map[[2]string]collaborationLink, edge collaborationLink, base uint64, result *CanvasCollaborationCommit) error {
	if vertices[edge.Source].Kind != "node" || vertices[edge.Target].Kind != "node" || edge.Source == edge.Target {
		return ErrCanvasCollaborationInvalidBatch
	}
	pair := [2]string{edge.Source, edge.Target}
	existing, duplicate := endpoints[pair]
	if !duplicate {
		endpoints[pair] = edge
		return nil
	}
	if existing.Revision > edge.Revision {
		existing, edge = edge, existing
	}
	if existing.Revision <= base || existing.Revision > result.Revision || edge.Revision <= result.Revision {
		return ErrCanvasCollaborationInvalidBatch
	}
	endpoints[pair] = existing
	result.Conflicts = append(result.Conflicts, CanvasCollaborationConflict{Kind: "edge", ID: existing.RecordID, Revision: existing.Revision})
	return nil
}

func collaborationRecordLink(record model.CanvasCollaborationRecord) collaborationLink {
	var body struct {
		Parent string `json:"parentId"`
		Source string `json:"source"`
		Target string `json:"target"`
	}
	_ = json.Unmarshal(record.Body, &body)
	return collaborationLink{Kind: record.Kind, RecordID: record.RecordID, Parent: body.Parent, Source: body.Source, Target: body.Target,
		Revision: record.Revision, Ordinal: record.Ordinal}
}

func collaborationString(value json.RawMessage) string {
	var text string
	_ = json.Unmarshal(value, &text)
	return text
}
