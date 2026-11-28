package repository

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
)

const collaborationAssetField = "data.assetId"

var collaborationImportFields = []string{collaborationAssetField, "data.src", "data.mediaType", "data.fileName", "data.uploaded", "data.status", "data.outputs", collaborationOutputField, "data.mediaAspect", "data.error"}

type collaborationImports struct {
	staged   map[uint64]CanvasCollaborationImport
	fileURL  func(string) string
	attached *model.Asset
	source   *CanvasCollaborationMediaSource
}

// CanvasCollaborationImport 绑定原素材存储身份与暂存的独立副本。
type CanvasCollaborationImport struct {
	SourceStorageID  uint64
	SourceStorageKey string
	Copy             model.CanvasGenerationMedia
}

func collaborationAssetID(raw json.RawMessage) uint64 {
	id, _ := strconv.ParseUint(collaborationString(raw), 10, 64)
	return id
}

func collaborationImportID(change CanvasCollaborationChange) uint64 {
	if change.Kind != "node" {
		return 0
	}
	if change.Action != "create" {
		return collaborationAssetID(change.Fields[collaborationAssetField])
	}
	fields, err := decodeCollaborationFields(change.Value, []byte(`{}`))
	if err != nil {
		return 0
	}
	return collaborationAssetID(fields.get(collaborationAssetField))
}

// collaborationStructuredValues 读取本次实际提交的复杂字段。
func collaborationStructuredValues(change CanvasCollaborationChange) map[string]json.RawMessage {
	if change.Action != "create" {
		return change.Fields
	}
	fields, err := decodeCollaborationFields(change.Value, []byte(`{}`))
	if err != nil {
		return nil
	}
	values := make(map[string]json.RawMessage, len(collaborationStructuredKinds))
	for name := range collaborationStructuredKinds {
		if name == collaborationManualReferences && fields.data["manualReferences"] == nil {
			continue
		}
		values[name] = fields.get(name)
	}
	return values
}

// ImportPlan 在复制对象前预检当前成员权限和幂等收据。
func (r *CanvasCollaborationRepository) ImportPlan(ctx context.Context, actor, canvas uint64, batch CanvasCollaborationBatch) (uint64, []uint64, error) {
	ids := make([]uint64, 0)
	seen := make(map[uint64]bool)
	add := func(id uint64) {
		if id != 0 && !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	for _, change := range batch.Changes {
		if change.Kind != "node" {
			continue
		}
		add(collaborationImportID(change))
		for field, raw := range collaborationStructuredValues(change) {
			if err := visitCollaborationReferences(field, decodeCollaborationObject(raw), func(ref collaborationObject, _, _ string) error {
				add(collaborationReferenceID(ref))
				return nil
			}); err != nil {
				return 0, nil, err
			}
		}
	}
	if len(ids) == 0 {
		return 0, nil, nil
	}
	if err := validateCollaborationBatch(batch); err != nil {
		return 0, nil, err
	}
	var project model.CanvasProject
	query := r.db.WithContext(ctx).Model(&model.CanvasProject{}).Select("user_id").Where("id = ? AND drama_id IS NULL AND collaboration_active AND collaboration_epoch = ?", canvas, batch.Epoch).
		Where("user_id = ? OR EXISTS (SELECT 1 FROM canvas_viewers v WHERE v.canvas_id = canvas_projects.id AND v.user_id = ? AND v.role = ?)", actor, actor, model.CanvasRoleEditor)
	if err := query.Take(&project).Error; err != nil {
		return 0, nil, translate(err)
	}
	var existing int64
	err := r.db.WithContext(ctx).Model(&model.CanvasCollaborationReceipt{}).
		Where("canvas_id = ? AND actor_id = ? AND op_id = ?", canvas, actor, batch.OpID).Count(&existing).Error
	if err != nil || existing > 0 {
		return 0, nil, err
	}
	return project.UserID, ids, nil
}

func (imports collaborationImports) apply(tx *gorm.DB, project *model.CanvasProject, actor uint64, records []model.CanvasCollaborationRecord, requested []CanvasCollaborationChange, result *CanvasCollaborationCommit) error {
	resolved := make(map[uint64]*model.Asset)
	resolve := func(id uint64) (*model.Asset, error) {
		if asset := resolved[id]; asset != nil {
			return asset, nil
		}
		asset, err := imports.resolve(tx, project, actor, id)
		if err == nil {
			resolved[id] = asset
		}
		return asset, err
	}
	for index := range result.Changes {
		change := &result.Changes[index]
		if change.Kind != "node" || change.Action == "delete" || slices.ContainsFunc(requested, func(input CanvasCollaborationChange) bool {
			return input.Kind == change.Kind && input.ID == change.ID && isCollaborationObjectAction(input.Action)
		}) {
			continue
		}
		values := collaborationStructuredValues(*change)
		id := collaborationImportID(*change)
		if id == 0 && !hasStructuredChange(values) {
			continue
		}
		row := slices.IndexFunc(records, func(record model.CanvasCollaborationRecord) bool {
			return record.Kind == change.Kind && record.RecordID == change.ID
		})
		if row < 0 {
			return ErrCanvasCollaborationInvalidBatch
		}
		if err := imports.writeChange(&records[row], change, id, values, resolve, result.Revision); err != nil {
			return err
		}
	}
	return nil
}

// hasStructuredChange 判断是否需要确认创作节点的内容。
func hasStructuredChange(values map[string]json.RawMessage) bool {
	for field := range collaborationStructuredKinds {
		if values[field] != nil {
			return true
		}
	}
	return false
}

// writeChange 确认创作节点的嵌套素材和主素材。
func (imports collaborationImports) writeChange(record *model.CanvasCollaborationRecord, change *CanvasCollaborationChange, id uint64, values map[string]json.RawMessage, resolve func(uint64) (*model.Asset, error), revision uint64) error {
	if hasStructuredChange(values) {
		if err := imports.writeStructured(record, change, values, resolve, revision); err != nil {
			return err
		}
	}
	if id == 0 {
		return nil
	}
	asset, err := resolve(id)
	if err != nil {
		return err
	}
	return imports.write(record, change, asset, revision)
}

func (imports collaborationImports) resolve(tx *gorm.DB, project *model.CanvasProject, actor, id uint64) (*model.Asset, error) {
	if imports.attached != nil && imports.attached.ID == id && imports.attached.UserID == project.UserID {
		return imports.attached, nil
	}
	if _, scoped := model.CanvasAutomationFromContext(tx.Statement.Context); scoped {
		return existingCollaborationAsset(tx, project, id)
	}
	var asset model.Asset
	query := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ?", id)
	err := query.Where("user_id = ? OR (user_id = 0 AND source = ?)", actor, model.AssetSourceOfficial).Take(&asset).Error
	if errors.Is(err, gorm.ErrRecordNotFound) && actor != project.UserID {
		return existingCollaborationAsset(tx, project, id)
	}
	if err != nil {
		return nil, translate(err)
	}
	if actor == project.UserID || asset.Source == model.AssetSourceOfficial {
		return &asset, nil
	}
	prepared, ok := imports.staged[id]
	stage := prepared.Copy
	if !ok || stage.Asset == nil || prepared.SourceStorageID != asset.StorageID || prepared.SourceStorageKey != asset.StorageKey || stage.Asset.Kind != asset.Kind {
		return nil, ErrNotFound
	}
	imported := *stage.Asset
	stage.Asset = &imported
	if _, err := attachCollaborationMedia(tx, project.UserID, []model.CanvasGenerationMedia{stage}); err != nil {
		return nil, err
	}
	return &imported, nil
}

func existingCollaborationAsset(tx *gorm.DB, project *model.CanvasProject, id uint64) (*model.Asset, error) {
	idString := strconv.FormatUint(id, 10)
	var referenced bool
	const path = `$.data ? (@.assetId == $id || @.outputs[*].assetId == $id || @.character.extras[*].assetId == $id || @.character.voice.assetId == $id || @.scene.thumb.assetId == $id || @.storyboard.shots[*].image.assetId == $id || @.params.images[*] == $id || @.params.videos[*] == $id || @.params.audios[*] == $id)`
	err := tx.Raw(`SELECT EXISTS (SELECT 1 FROM canvas_collaboration_records
		WHERE canvas_id = ? AND kind = 'node' AND NOT deleted AND
		jsonb_path_exists(body, ?::jsonpath, jsonb_build_object('id', ?::text)))`, project.ID, path, idString).Scan(&referenced).Error
	if err != nil {
		return nil, err
	}
	if !referenced {
		return nil, ErrNotFound
	}
	var asset model.Asset
	err = tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND user_id = ?", id, project.UserID).Take(&asset).Error
	return &asset, translate(err)
}

// writeStructured 解析实际接受的素材引用，再更新存档和确认增量。
func (imports collaborationImports) writeStructured(record *model.CanvasCollaborationRecord, change *CanvasCollaborationChange, values map[string]json.RawMessage, resolve func(uint64) (*model.Asset, error), revision uint64) error {
	fields, err := decodeCollaborationFields(record.Body, record.FieldVersions)
	if err != nil {
		return err
	}
	for field := range collaborationStructuredKinds {
		raw := values[field]
		if raw == nil {
			continue
		}
		updated, err := rewriteCollaborationReferences(field, raw, func(ref collaborationObject, address, kind string) error {
			asset, err := resolve(collaborationReferenceID(ref))
			if err != nil {
				return err
			}
			if asset.Kind != kind {
				return ErrCanvasCollaborationInvalidBatch
			}
			ref["assetId"] = strconv.FormatUint(asset.ID, 10)
			ref[address] = imports.assetURL(asset)
			return nil
		})
		if err != nil {
			return err
		}
		fields.set(field, updated)
		if change.Fields != nil {
			change.Fields[field] = updated
		}
	}
	if err := fields.save(record, revision); err != nil {
		return err
	}
	if change.Action == "create" {
		change.Value = json.RawMessage(record.Body)
	}
	return nil
}

// assetURL 生成保存到所有者画布中的素材地址。
func (imports collaborationImports) assetURL(asset *model.Asset) string {
	if strings.TrimSpace(asset.PublicURL) != "" {
		return asset.PublicURL
	}
	if imports.fileURL != nil {
		return imports.fileURL(asset.StorageKey)
	}
	return "/files/" + asset.StorageKey
}

func (imports collaborationImports) write(record *model.CanvasCollaborationRecord, change *CanvasCollaborationChange, asset *model.Asset, revision uint64) error {
	fields, err := decodeCollaborationFields(record.Body, record.FieldVersions)
	if err != nil {
		return err
	}
	kind := collaborationString(fields.data["kind"])
	imageContainer := kind == "character" || kind == "storyboard"
	if kind != asset.Kind && (!imageContainer || asset.Kind != "image") {
		return ErrCanvasCollaborationInvalidBatch
	}
	url := imports.assetURL(asset)
	media := importedCollaborationMedia(fields, asset, url, kind)
	for field, value := range media {
		encoded, _ := json.Marshal(value)
		fields.set(field, encoded)
		fields.versions[field] = revision
		if change.Fields != nil {
			change.Fields[field] = encoded
		}
	}
	if err := fields.save(record, revision); err != nil {
		return err
	}
	if change.Action == "create" {
		change.Value = json.RawMessage(record.Body)
	}
	return nil
}

// importedCollaborationMedia 保留历史结果，并让同次导入的媒体状态共同落库和撤销。
func importedCollaborationMedia(fields *collaborationFields, asset *model.Asset, url, kind string) map[string]any {
	id := strconv.FormatUint(asset.ID, 10)
	previous := collaborationString(fields.get(collaborationAssetField))
	uploaded := previous == "" || kind == "character" || string(fields.get("data.uploaded")) == "true"
	media := map[string]any{collaborationAssetField: id, "data.src": url, "data.mediaType": asset.Kind,
		"data.uploaded": uploaded, "data.status": "done", "data.error": nil, "data.mediaAspect": nil}
	media["data.fileName"] = nil
	if uploaded {
		media["data.fileName"] = asset.FileName
	}
	if asset.Width > 0 && asset.Height > 0 {
		media["data.mediaAspect"] = float64(asset.Width) / float64(asset.Height)
	}
	media["data.outputs"] = importedCollaborationHistory(fields, id, url, asset.Kind)
	media[collaborationOutputField] = id
	return media
}

// importedCollaborationHistory 把新素材加到结果历史，兼容早期只有主图的节点。
func importedCollaborationHistory(fields *collaborationFields, id, url, kind string) []map[string]any {
	var history []map[string]any
	_ = json.Unmarshal(fields.get("data.outputs"), &history)
	if previous, src := collaborationString(fields.get(collaborationAssetField)), collaborationString(fields.get("data.src")); len(history) == 0 && previous != "" && src != "" && previous != id {
		history = append(history, map[string]any{"id": previous, "assetId": previous, "src": src, "mediaType": kind, "createdAt": 0})
	}
	if !knownGenerationOutput(history, id) {
		history = append(history, map[string]any{"id": id, "assetId": id, "src": url, "mediaType": kind, "createdAt": time.Now().UnixMilli()})
	}
	return history[max(0, len(history)-50):]
}
