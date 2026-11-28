package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"regexp"
	"slices"
	"time"

	"go.uber.org/zap"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/logger"
)

// 协作提交可能返回的错误及操作编号格式。
var (
	ErrCanvasCollaborationSession      = errors.New("协作会话已失效")
	ErrCanvasCollaborationInvalidBatch = errors.New("协作操作不合法")
	ErrCanvasCollaborationOpID         = errors.New("操作编号已用于其他请求")
	collaborationOpID                  = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
)

// CanvasCollaborationBatch 是可幂等重试的原子编辑请求。
type CanvasCollaborationBatch = model.CanvasCollaborationBatch

// CanvasCollaborationChange 描述一个记录上成组提交的字段。
type CanvasCollaborationChange = model.CanvasCollaborationChange

// CanvasCollaborationConflict 记录客户端需要对比的当前值与版本。
type CanvasCollaborationConflict struct {
	Kind     string          `json:"kind"`
	ID       string          `json:"id"`
	Field    string          `json:"field"`
	Revision uint64          `json:"revision"`
	Value    json.RawMessage `json:"value"`
}

// CanvasCollaborationCommit 是一次提交的持久化收据。
type CanvasCollaborationCommit struct {
	Epoch     uint64                          `json:"epoch"`
	Revision  uint64                          `json:"revision"`
	OpID      string                          `json:"opId"`
	Changes   []CanvasCollaborationChange     `json:"changes,omitempty"`
	Conflicts []CanvasCollaborationConflict   `json:"conflicts,omitempty"`
	Skipped   []CanvasCollaborationConflict   `json:"skipped,omitempty"`
	Undoes    uint64                          `json:"undoes,omitempty"`
	Redo      bool                            `json:"redo,omitempty"`
	Source    *CanvasCollaborationMediaSource `json:"source,omitempty"`
}

// Commit 在同一事务内检查授权、校验并提交编辑。
func (r *CanvasCollaborationRepository) Commit(ctx context.Context, actorID, canvasID uint64, batch CanvasCollaborationBatch) (*CanvasCollaborationCommit, error) {
	return r.CommitWithImports(ctx, actorID, canvasID, batch, nil, nil)
}

// CommitWithImports 确认编辑及对应的独立素材副本。
func (r *CanvasCollaborationRepository) CommitWithImports(ctx context.Context, actorID, canvasID uint64, batch CanvasCollaborationBatch, staged map[uint64]CanvasCollaborationImport, fileURL func(string) string) (*CanvasCollaborationCommit, error) {
	if err := validateCollaborationBatch(batch); err != nil {
		return nil, err
	}
	key, scoped := model.CanvasAutomationFromContext(ctx)
	if scoped && slices.ContainsFunc(batch.Changes, func(change CanvasCollaborationChange) bool {
		return automationForbiddenChange(change, key)
	}) {
		return nil, ErrNotFound
	}
	encoded, err := json.Marshal(batch)
	if err != nil || len(encoded) > model.CanvasCollaborationMaxBatchBytes {
		return nil, ErrCanvasCollaborationInvalidBatch
	}
	digest := sha256.Sum256(encoded)
	receipt := model.CanvasCollaborationReceipt{CanvasID: canvasID, ActorID: actorID, OpID: batch.OpID, RequestHash: hex.EncodeToString(digest[:])}
	var result *CanvasCollaborationCommit
	var projectOwner uint64
	var lockQuery, operationDuration time.Duration
	transactionStart := time.Now()
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		project, elapsed, err := lockEditableCollaboration(tx, canvasID, actorID, batch.Epoch)
		lockQuery = elapsed
		if err != nil {
			return err
		}
		projectOwner = project.UserID
		if !validCollaborationRevision(batch, project.Revision) {
			return ErrCanvasCollaborationSession
		}
		if err := authorizeGenerationChanges(tx, project, actorID, batch.Changes); err != nil {
			return err
		}
		imports := collaborationImports{staged: staged, fileURL: fileURL}
		operationStart := time.Now()
		result, err = applyCollaborationBatch(tx, project, batch, receipt, imports)
		operationDuration = time.Since(operationStart)
		return err
	})
	logger.Ctx(ctx).Debug("画布协作事务耗时", zap.Uint64("canvas_id", canvasID),
		zap.Duration("lock_query", lockQuery), zap.Duration("apply_operation", operationDuration),
		zap.Duration("transaction", time.Since(transactionStart)), zap.Bool("committed", err == nil))
	if err == nil && (actorID != projectOwner || scoped) {
		err = projectCollaborationCommit(canvasID, result)
	}
	return result, err
}

func authorizeGenerationChanges(tx *gorm.DB, project *model.CanvasProject, actor uint64, changes []CanvasCollaborationChange) error {
	if actor == project.UserID || !slices.ContainsFunc(changes, func(change CanvasCollaborationChange) bool {
		return change.Fields[collaborationGenerationInputField] != nil
	}) {
		return nil
	}
	allowed, err := canGenerateInCollaboration(tx, project, actor)
	if err == nil && !allowed {
		return ErrNotFound
	}
	return err
}

func lockEditableCollaboration(tx *gorm.DB, canvasID, actorID, epoch uint64) (*model.CanvasProject, time.Duration, error) {
	start := time.Now()
	project, err := lockCollaborationProject(tx, canvasID)
	elapsed := time.Since(start)
	if err != nil {
		return nil, elapsed, err
	}
	role, err := collaborationRole(tx, project, actorID)
	if err != nil {
		return nil, elapsed, err
	}
	if role != model.CanvasRoleOwner && role != model.CanvasRoleEditor {
		return nil, elapsed, ErrNotFound
	}
	if !project.CollaborationActive || project.CollaborationEpoch != epoch {
		return nil, elapsed, ErrCanvasCollaborationSession
	}
	if err := checkCanvasAutomation(tx, project, actorID, role, true); err != nil {
		return nil, elapsed, err
	}
	return project, elapsed, nil
}

func lockCollaborationProject(tx *gorm.DB, canvasID uint64) (*model.CanvasProject, error) {
	var project model.CanvasProject
	err := tx.Select("id", "user_id", "title", "revision", "collaboration_epoch", "collaboration_active").
		Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND drama_id IS NULL", canvasID).Take(&project).Error
	return &project, translate(err)
}

func collaborationRole(tx *gorm.DB, project *model.CanvasProject, actorID uint64) (string, error) {
	if actorID == project.UserID {
		return model.CanvasRoleOwner, nil
	}
	var grant model.CanvasViewer
	err := tx.Select("role").Where("canvas_id = ? AND user_id = ?", project.ID, actorID).Take(&grant).Error
	return grant.Role, translate(err)
}

func applyCollaborationBatch(tx *gorm.DB, project *model.CanvasProject, batch CanvasCollaborationBatch, receipt model.CanvasCollaborationReceipt, imports collaborationImports) (*CanvasCollaborationCommit, error) {
	previous, err := readCollaborationReceipt(tx, receipt)
	if err != nil || previous != nil {
		return previous, err
	}
	result := &CanvasCollaborationCommit{Epoch: batch.Epoch, Revision: project.Revision, OpID: batch.OpID, Source: imports.source}
	if batch.RequireBaseRevision && batch.BaseRevision != project.Revision {
		result.Conflicts = []CanvasCollaborationConflict{{Kind: "canvas", ID: "revision", Revision: project.Revision}}
		return result, storeCollaborationReceipt(tx, receipt, result)
	}
	return commitCollaborationRecords(tx, project, batch, receipt, imports, result)
}

func commitCollaborationRecords(tx *gorm.DB, project *model.CanvasProject, batch CanvasCollaborationBatch, receipt model.CanvasCollaborationReceipt, imports collaborationImports, result *CanvasCollaborationCommit) (*CanvasCollaborationCommit, error) {
	var undo []CanvasCollaborationChange
	records, err := editCollaborationRecords(tx, project, receipt.ActorID, batch, result, &undo)
	if err != nil {
		return nil, err
	}
	if len(result.Conflicts) == 0 && hasCollaborationStructure(result.Changes) {
		explicit := len(result.Changes)
		records, err = applyCollaborationStructure(tx, project.ID, batch.BaseRevision, records, result)
		if err != nil {
			return nil, err
		}
		undo, err = appendCollaborationDeletionUndo(tx, project.ID, undo, result.Changes[explicit:])
		if err != nil {
			return nil, err
		}
	}
	if len(result.Conflicts) > 0 {
		result.Changes = nil
	} else if len(records) > 0 {
		result.Revision++
		if err := imports.apply(tx, project, receipt.ActorID, records, batch.Changes, result); err != nil {
			return nil, err
		}
		if err := persistCollaborationBatch(tx, records, receipt, result, undo); err != nil {
			return nil, err
		}
		if len(undo) > 0 {
			if err := clearCollaborationRedo(tx, receipt, result.Epoch); err != nil {
				return nil, err
			}
		}
	}
	return result, storeCollaborationReceipt(tx, receipt, result)
}

func storeCollaborationReceipt(tx *gorm.DB, receipt model.CanvasCollaborationReceipt, result *CanvasCollaborationCommit) error {
	encoded, err := json.Marshal(result)
	if err != nil {
		return err
	}
	receipt.Result = datatypes.JSON(encoded)
	return tx.Create(&receipt).Error
}

func readCollaborationReceipt(tx *gorm.DB, request model.CanvasCollaborationReceipt) (*CanvasCollaborationCommit, error) {
	var saved model.CanvasCollaborationReceipt
	err := tx.Where("canvas_id = ? AND actor_id = ? AND op_id = ?", request.CanvasID, request.ActorID, request.OpID).Take(&saved).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if saved.RequestHash != request.RequestHash {
		return nil, ErrCanvasCollaborationOpID
	}
	var result CanvasCollaborationCommit
	return &result, json.Unmarshal(saved.Result, &result)
}

func persistCollaborationBatch(tx *gorm.DB, records []model.CanvasCollaborationRecord, receipt model.CanvasCollaborationReceipt, result *CanvasCollaborationCommit, undo []CanvasCollaborationChange) error {
	if err := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "canvas_id"}, {Name: "kind"}, {Name: "record_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"body", "field_versions", "revision", "deleted", "ordinal"}),
	}).CreateInBatches(records, 200).Error; err != nil {
		return err
	}
	changes, err := json.Marshal(result.Changes)
	if err != nil {
		return err
	}
	if err := tx.Model(&model.CanvasProject{}).Where("id = ?", receipt.CanvasID).Update("revision", result.Revision).Error; err != nil {
		return err
	}
	event := model.CanvasCollaborationEvent{CanvasID: receipt.CanvasID, Revision: result.Revision, Epoch: result.Epoch,
		ActorID: receipt.ActorID, OpID: result.OpID, Changes: datatypes.JSON(changes)}
	if _, scoped := model.CanvasAutomationFromContext(tx.Statement.Context); scoped {
		event.Source = "automation"
	}
	if len(undo) > 0 {
		encoded, err := json.Marshal(undo)
		if err != nil {
			return err
		}
		event.Undo = datatypes.JSON(encoded)
	}
	if err := tx.Create(&event).Error; err != nil {
		return err
	}
	return checkpointCollaborationEvent(tx, event)
}

func editCollaborationRecords(tx *gorm.DB, project *model.CanvasProject, actor uint64, batch CanvasCollaborationBatch, result *CanvasCollaborationCommit, undo *[]CanvasCollaborationChange) ([]model.CanvasCollaborationRecord, error) {
	index, err := collaborationRecordIndex(tx, project.ID, batch.Changes)
	if err != nil {
		return nil, err
	}
	edited := make([]model.CanvasCollaborationRecord, 0, len(batch.Changes))
	for _, change := range batch.Changes {
		record := index[[2]string{change.Kind, change.ID}]
		before := record
		accepted, err := applyRequestedCollaborationChange(tx, project.ID, actor, batch, &record, change, result)
		if err != nil {
			return nil, err
		}
		if accepted != nil {
			inverse, err := collaborationObjectInverse(before, *accepted, change)
			if err != nil {
				return nil, err
			}
			if inverse.Action != "" || len(inverse.Fields) > 0 {
				*undo = append(*undo, inverse)
			}
			edited = append(edited, record)
			result.Changes = append(result.Changes, *accepted)
		}
	}
	return edited, nil
}

func applyRequestedCollaborationChange(tx *gorm.DB, canvasID, actor uint64, batch CanvasCollaborationBatch, record *model.CanvasCollaborationRecord, change CanvasCollaborationChange, result *CanvasCollaborationCommit) (*CanvasCollaborationChange, error) {
	if record.RecordID != "" && !record.Deleted && isCollaborationObjectAction(change.Action) {
		var resolved CanvasCollaborationChange
		var ok bool
		var err error
		if change.Action == collaborationShotAction {
			resolved, ok, err = resolveCollaborationShotEdit(tx, *record, change, batch, result)
		} else {
			resolved, ok, err = resolveCollaborationDirectorEdit(*record, change, batch.BaseRevision, result)
		}
		if err != nil || !ok {
			return nil, err
		}
		change = resolved
	}
	return applyCollaborationChange(canvasID, actor, batch.BaseRevision, record, change, result)
}

func isCollaborationObjectAction(action string) bool {
	return action == collaborationShotAction || action == collaborationDirectorAction
}

func applyCollaborationChange(canvasID, actor, base uint64, record *model.CanvasCollaborationRecord, change CanvasCollaborationChange, result *CanvasCollaborationCommit) (*CanvasCollaborationChange, error) {
	if change.Action == "create" && record.RecordID == "" {
		created, err := createCollaborationRecord(canvasID, result.Revision+1, change)
		if err != nil {
			return nil, err
		}
		*record = created
		change.Value = json.RawMessage(created.Body)
		return &change, nil
	}
	if change.Action == "create" || record.RecordID == "" || record.Deleted || (change.Action == "delete" && record.Revision > max(base, change.RecordBase)) {
		result.Conflicts = append(result.Conflicts, CanvasCollaborationConflict{Kind: change.Kind, ID: change.ID, Revision: record.Revision})
		return nil, nil
	}
	if change.Action == "delete" {
		markCollaborationDeleted(record, result.Revision+1)
		change.RecordBase = 0
		return &change, nil
	}
	previousRevision := record.Revision
	fields, err := editCollaborationRecord(record, change, actor, base, result)
	if len(fields) == 0 || err != nil {
		return nil, err
	}
	change.Action = ""
	change.Fields = fields
	change.FieldBases = nil
	change.PreviousRevision = previousRevision
	return &change, nil
}

func editCollaborationRecord(record *model.CanvasCollaborationRecord, change CanvasCollaborationChange, actor, base uint64, result *CanvasCollaborationCommit) (map[string]json.RawMessage, error) {
	fields, err := decodeCollaborationFields(record.Body, record.FieldVersions)
	if err != nil {
		return nil, err
	}
	accepted := make(map[string]json.RawMessage)
	for _, field := range slices.Sorted(maps.Keys(change.Fields)) {
		value := change.Fields[field]
		if expected := collaborationStructuredKinds[field]; expected != "" && collaborationString(fields.data["kind"]) != expected {
			return nil, ErrCanvasCollaborationInvalidBatch
		}
		if field == "data.params" && !fields.allowsCollaborationParams(value) {
			return nil, ErrCanvasCollaborationInvalidBatch
		}
		if field == collaborationGenerationInputField {
			input, _ := decodeGenerationInput(value)
			input.Actor = actor
			value, _ = json.Marshal(generationInputDigest(input))
		}
		if fields.equal(field, value) {
			continue
		}
		if conflict := fields.editConflict(change, field, base); conflict != nil {
			result.Conflicts = append(result.Conflicts, *conflict)
			continue
		}
		if field == "data.character" && collaborationVoiceChanged(fields.get(field), value) {
			fields.versions["$voice"] = result.Revision + 1
		}
		if !fields.applyClientField(change, field, value) {
			return nil, ErrCanvasCollaborationInvalidBatch
		}
		fields.versions[field] = result.Revision + 1
		accepted[field] = value
	}
	if len(accepted) == 0 {
		return nil, nil
	}
	return accepted, fields.save(record, result.Revision+1)
}

func (fields *collaborationFields) editConflict(change CanvasCollaborationChange, field string, base uint64) *CanvasCollaborationConflict {
	if supplied := change.FieldBases[field]; supplied != 0 {
		base = supplied
	}
	version := max(fields.versions[field], fields.versions["$created"], fields.versions["$restored"])
	if field == collaborationAssetField {
		version = max(version, fields.versions["data.status"], fields.versions[collaborationOutputField])
	}
	if version <= base {
		return nil
	}
	return &CanvasCollaborationConflict{Kind: change.Kind, ID: change.ID, Field: field, Revision: version, Value: fields.get(field)}
}

func (fields *collaborationFields) applyClientField(change CanvasCollaborationChange, field string, value json.RawMessage) bool {
	if field == collaborationOutputField {
		return fields.selectCollaborationOutput(value)
	}
	if field == collaborationAssetField {
		kind := collaborationString(fields.data["kind"])
		return collaborationString(fields.get("data.status")) != "running" && change.Fields[collaborationOutputField] == nil &&
			(collaborationString(fields.get(field)) == "" || kind == "image" || kind == "character")
	}
	fields.set(field, value)
	return true
}
