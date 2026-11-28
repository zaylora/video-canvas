package repository

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
)

// ErrCanvasMediaDraftStale 表示当前加工产物无法确认。
var ErrCanvasMediaDraftStale = errors.New("加工产物已过期、尚未就绪或源素材已变化")

// CanvasCollaborationMediaSource 保留挂载收据中的来源素材和观察版本。
type CanvasCollaborationMediaSource struct {
	NodeID   string `json:"nodeId"`
	AssetID  string `json:"assetId"`
	Revision string `json:"revision"`
}

const (
	mediaDraftWriting  = "writing"
	mediaDraftReady    = "ready"
	mediaDraftDeleting = "deleting"
	mediaDraftTTL      = time.Hour
)

// WithTx 在同一数据库事务内执行画布协作操作。
func (r *CanvasCollaborationRepository) WithTx(ctx context.Context, run func(*gorm.DB) error) error {
	return r.db.WithContext(ctx).Transaction(run)
}

// MediaDraftOwner 在读取上传体之前验证写权限和来源版本。
func (r *CanvasCollaborationRepository) MediaDraftOwner(ctx context.Context, actor, canvas uint64, req model.CanvasMediaDraftReq) (uint64, error) {
	if !collaborationOpID.MatchString(req.ID) || !validCollaborationRecordID(req.SourceNode) || req.SourceAsset == 0 {
		return 0, ErrCanvasCollaborationInvalidBatch
	}
	var owner uint64
	err := r.WithTx(ctx, func(tx *gorm.DB) error {
		project, _, err := lockEditableCollaboration(tx, canvas, actor, req.Epoch)
		if err != nil {
			return err
		}
		owner = project.UserID
		_, err = mediaDraftSource(tx, project, req)
		return err
	})
	return owner, err
}

func mediaDraftSource(tx *gorm.DB, project *model.CanvasProject, req model.CanvasMediaDraftReq) (*collaborationFields, error) {
	var node model.CanvasCollaborationRecord
	if req.Revision > project.Revision {
		return nil, ErrCanvasMediaDraftStale
	}
	err := tx.Where("canvas_id = ? AND kind = 'node' AND record_id = ? AND NOT deleted", project.ID, req.SourceNode).Take(&node).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrCanvasMediaDraftStale
	}
	if err != nil {
		return nil, err
	}
	fields, err := decodeCollaborationFields(node.Body, node.FieldVersions)
	if err != nil {
		return nil, err
	}
	if !isCanvasDraftSourceKind(collaborationString(fields.get("data.kind"))) || collaborationAssetID(fields.get(collaborationAssetField)) != req.SourceAsset ||
		max(fields.versions[collaborationAssetField], fields.versions["data.src"], fields.versions["$created"], fields.versions["$restored"]) > req.Revision {
		return nil, ErrCanvasMediaDraftStale
	}
	var asset model.Asset
	err = tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("id").Where("id = ? AND user_id = ?", req.SourceAsset, project.UserID).Take(&asset).Error
	return fields, translate(err)
}

func isCanvasDraftSourceKind(kind string) bool {
	return kind == "image" || kind == "video" || kind == "audio"
}

func mediaDraftKey(db *gorm.DB, canvas, actor uint64, id string) *gorm.DB {
	return db.Where("canvas_id = ? AND actor_id = ? AND draft_id = ?", canvas, actor, id)
}

// ReserveMediaDraft 在对象落盘前登记回收信息，并在重试时比对内容哈希。
func (r *CanvasCollaborationRepository) ReserveMediaDraft(ctx context.Context, actor, canvas uint64, req model.CanvasMediaDraftReq, asset *model.Asset) (bool, error) {
	metadata, err := json.Marshal(asset)
	if err != nil {
		return false, err
	}
	draft := model.CanvasMediaDraft{CanvasID: canvas, ActorID: actor, DraftID: req.ID, Epoch: req.Epoch, Revision: req.Revision,
		SourceNode: req.SourceNode, SourceAsset: req.SourceAsset, State: mediaDraftWriting, StorageID: asset.StorageID,
		StorageKey: asset.StorageKey, SHA256: asset.Sha256, Metadata: datatypes.JSON(metadata), ExpiresAt: time.Now().Add(mediaDraftTTL)}
	var reserved bool
	err = r.WithTx(ctx, func(tx *gorm.DB) error {
		project, _, err := lockEditableCollaboration(tx, canvas, actor, req.Epoch)
		if err != nil {
			return err
		}
		if _, err := mediaDraftSource(tx, project, req); err != nil {
			return err
		}
		var used int64
		if err := tx.Model(&model.CanvasCollaborationReceipt{}).Where("canvas_id = ? AND actor_id = ? AND op_id = ?", canvas, actor, req.ID).Count(&used).Error; err != nil {
			return err
		}
		if used != 0 {
			return ErrCanvasCollaborationOpID
		}
		reserved, err = reserveMediaDraft(tx, req, draft)
		return err
	})
	return reserved, err
}

func reserveMediaDraft(db *gorm.DB, req model.CanvasMediaDraftReq, draft model.CanvasMediaDraft) (bool, error) {
	created := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&draft)
	if created.Error != nil || created.RowsAffected == 1 {
		return created.RowsAffected == 1, created.Error
	}
	var saved model.CanvasMediaDraft
	if err := mediaDraftKey(db, draft.CanvasID, draft.ActorID, req.ID).Take(&saved).Error; err != nil {
		return false, translate(err)
	}
	if mediaDraftRequest(saved) != req || saved.SHA256 != draft.SHA256 {
		return false, ErrCanvasCollaborationOpID
	}
	if saved.State != mediaDraftReady || !time.Now().Before(saved.ExpiresAt) {
		return false, ErrCanvasMediaDraftStale
	}
	return false, nil
}

// FinishMediaDraft 仅允许成功写入且尚未过期的对象进入可挂载状态。
func (r *CanvasCollaborationRepository) FinishMediaDraft(ctx context.Context, actor, canvas uint64, id, key string) error {
	updated := mediaDraftKey(r.db.WithContext(ctx).Model(&model.CanvasMediaDraft{}), canvas, actor, id).
		Where("state = ? AND storage_key = ? AND expires_at > ?", mediaDraftWriting, key, time.Now()).Update("state", mediaDraftReady)
	if updated.Error == nil && updated.RowsAffected != 1 {
		return ErrCanvasMediaDraftStale
	}
	return updated.Error
}

// AttachMediaDraft 在画布事务内确认产物或恢复同一请求的收据。
func (r *CanvasCollaborationRepository) AttachMediaDraft(ctx context.Context, actor, canvas, epoch uint64, id string, fileURL func(string) string) (*CanvasCollaborationCommit, error) {
	if !collaborationOpID.MatchString(id) {
		return nil, ErrCanvasCollaborationInvalidBatch
	}
	var result *CanvasCollaborationCommit
	var owner uint64
	err := r.WithTx(ctx, func(tx *gorm.DB) error {
		project, _, err := lockEditableCollaboration(tx, canvas, actor, epoch)
		if err != nil {
			return err
		}
		owner = project.UserID
		digest := sha256.Sum256(fmt.Appendf(nil, "media-draft:%d:%d:%d:%s", canvas, actor, epoch, id))
		receipt := model.CanvasCollaborationReceipt{CanvasID: canvas, ActorID: actor, OpID: id, RequestHash: fmt.Sprintf("%x", digest)}
		result, err = attachMediaDraft(tx, project, receipt, fmt.Sprintf("%x", digest[:16]), fileURL)
		return err
	})
	if err == nil && actor != owner {
		err = projectCollaborationCommit(canvas, result)
	}
	return result, err
}

func attachMediaDraft(tx *gorm.DB, project *model.CanvasProject, receipt model.CanvasCollaborationReceipt, suffix string, fileURL func(string) string) (*CanvasCollaborationCommit, error) {
	previous, err := readCollaborationReceipt(tx, receipt)
	if err != nil || previous != nil {
		return previous, err
	}
	var draft model.CanvasMediaDraft
	query := mediaDraftKey(tx, project.ID, receipt.ActorID, receipt.OpID).Clauses(clause.Locking{Strength: "UPDATE"})
	if err := query.Take(&draft).Error; err != nil {
		return nil, translate(err)
	}
	if draft.State != mediaDraftReady || draft.Epoch != project.CollaborationEpoch || !time.Now().Before(draft.ExpiresAt) {
		return nil, ErrCanvasMediaDraftStale
	}
	fields, err := mediaDraftSource(tx, project, mediaDraftRequest(draft))
	if err != nil {
		return nil, err
	}
	var asset model.Asset
	if err := json.Unmarshal(draft.Metadata, &asset); err != nil {
		return nil, err
	}
	asset.StorageID, asset.StorageKey, asset.Sha256, asset.UserID = draft.StorageID, draft.StorageKey, draft.SHA256, project.UserID
	if err := tx.Create(&asset).Error; err != nil {
		return nil, err
	}
	source := &CanvasCollaborationMediaSource{NodeID: draft.SourceNode, AssetID: strconv.FormatUint(draft.SourceAsset, 10), Revision: strconv.FormatUint(draft.Revision, 10)}
	result, err := commitMediaDraft(tx, project, receipt, fields, &asset, source, suffix, fileURL, []string{source.NodeID})
	if err != nil {
		return nil, err
	}
	return result, tx.Delete(&draft).Error
}

func mediaDraftRequest(draft model.CanvasMediaDraft) model.CanvasMediaDraftReq {
	return model.CanvasMediaDraftReq{ID: draft.DraftID, Epoch: draft.Epoch, Revision: draft.Revision, SourceNode: draft.SourceNode, SourceAsset: draft.SourceAsset}
}

func commitMediaDraft(tx *gorm.DB, project *model.CanvasProject, receipt model.CanvasCollaborationReceipt, fields *collaborationFields, asset *model.Asset, source *CanvasCollaborationMediaSource, suffix string, fileURL func(string) string, nodes []string) (*CanvasCollaborationCommit, error) {
	var pos struct{ X, Y float64 }
	_ = json.Unmarshal(fields.get("position"), &pos)
	nodeID := "media-" + suffix
	node, err := json.Marshal(map[string]any{"position": map[string]float64{"x": math.Max(-1e9, math.Min(1e9, pos.X+320)), "y": math.Max(-1e9, math.Min(1e9, pos.Y))},
		"data": map[string]string{"kind": asset.Kind, "label": asset.FileName, "assetId": strconv.FormatUint(asset.ID, 10)}})
	if err != nil {
		return nil, err
	}
	batch := CanvasCollaborationBatch{Epoch: project.CollaborationEpoch, BaseRevision: project.Revision, OpID: suffix,
		Changes: []CanvasCollaborationChange{{Kind: "node", ID: nodeID, Action: "create", Value: node}}}
	seen := make(map[string]bool, len(nodes))
	for index, id := range nodes {
		if seen[id] {
			continue
		}
		seen[id] = true
		edge, err := json.Marshal(map[string]string{"source": id, "target": nodeID})
		if err != nil {
			return nil, err
		}
		batch.Changes = append(batch.Changes, CanvasCollaborationChange{Kind: "edge", ID: fmt.Sprintf("source-%s-%d", suffix, index), Action: "create", Value: edge})
	}
	if err := validateCollaborationBatch(batch); err != nil {
		return nil, err
	}
	batch.OpID = receipt.OpID
	result, err := applyCollaborationBatch(tx, project, batch, receipt, collaborationImports{fileURL: fileURL, attached: asset, source: source})
	if err == nil && len(result.Changes) != len(batch.Changes) {
		return nil, ErrCanvasMediaDraftStale
	}
	return result, err
}

// SweepMediaDrafts 回收过期对象，清理前按行条件排除已确认的产物。
func (r *CanvasCollaborationRepository) SweepMediaDrafts(ctx context.Context, remove func(context.Context, uint64, string) error) error {
	db := r.db.WithContext(ctx)
	var expired []model.CanvasMediaDraft
	if err := db.Where("expires_at <= ?", time.Now()).Order("expires_at").Limit(100).Find(&expired).Error; err != nil {
		return err
	}
	var sweepErr error
	for _, draft := range expired {
		sweepErr = errors.Join(sweepErr, deleteMediaDraft(ctx, db, draft, remove))
	}
	return sweepErr
}

func deleteMediaDraft(ctx context.Context, db *gorm.DB, draft model.CanvasMediaDraft, remove func(context.Context, uint64, string) error) error {
	query := mediaDraftKey(db.Model(&model.CanvasMediaDraft{}), draft.CanvasID, draft.ActorID, draft.DraftID).
		Where("storage_key = ? AND expires_at <= ?", draft.StorageKey, time.Now())
	claimed := query.Session(&gorm.Session{}).Update("state", mediaDraftDeleting)
	if claimed.Error != nil || claimed.RowsAffected == 0 {
		return claimed.Error
	}
	if err := remove(ctx, draft.StorageID, draft.StorageKey); err != nil {
		return err
	}
	return query.Where("state = ?", mediaDraftDeleting).Delete(&model.CanvasMediaDraft{}).Error
}
