package repository

import (
	"context"
	"database/sql"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
)

func lockTaskCanvas(tx *gorm.DB, user, canvas uint64) error {
	var project model.CanvasProject
	err := tx.Model(&project).Select("id", "collaboration_active").Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND user_id = ?", canvas, user).Take(&project).Error
	if err != nil {
		return translate(err)
	}
	if project.CollaborationActive {
		return ErrCanvasCollaborationActive
	}
	return nil
}

// CanvasShareRepository 持久化画布授权与交付收据。
type CanvasShareRepository struct{ db *gorm.DB }

// NewCanvasShareRepository 构造使用指定事务或连接池的仓储。
func NewCanvasShareRepository(db *gorm.DB) *CanvasShareRepository {
	return &CanvasShareRepository{db: db}
}

// Recipient 按规范化邮箱查找启用的接收账号。
func (r *CanvasShareRepository) Recipient(ctx context.Context, email string) (uint64, error) {
	var user model.User
	err := r.db.WithContext(ctx).Select("id").Where("lower(email) = ? AND status = ?", email, model.UserStatusActive).Take(&user).Error
	return user.ID, translate(err)
}

// Grant 为普通画布添加幂等的只读授权。
func (r *CanvasShareRepository) Grant(ctx context.Context, owner, canvas, viewer uint64) error {
	return r.grantRole(ctx, owner, canvas, viewer, model.CanvasRoleViewer, false)
}

// GrantRole 由所有者设置成员权限；编辑授权仅适用于协作画布。
func (r *CanvasShareRepository) GrantRole(ctx context.Context, owner, canvas, member uint64, role string) error {
	if owner == member || (role != model.CanvasRoleViewer && role != model.CanvasRoleEditor) {
		return ErrCanvasCollaborationInvalidBatch
	}
	return r.grantRole(ctx, owner, canvas, member, role, true)
}

func (r *CanvasShareRepository) grantRole(ctx context.Context, owner, canvas, member uint64, role string, replace bool) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var project model.CanvasProject
		if err := NewCanvasShareRepository(tx).owned(ctx, owner, canvas).Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id", "collaboration_active").Take(&project).Error; err != nil {
			return translate(err)
		}
		if role == model.CanvasRoleEditor && !project.CollaborationActive {
			return ErrCanvasCollaborationSession
		}
		var recipient model.User
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("id").Where("id = ? AND status = ?", member, model.UserStatusActive).Take(&recipient).Error; err != nil {
			return translate(err)
		}
		conflict := clause.OnConflict{DoNothing: true}
		if replace {
			conflict = clause.OnConflict{Columns: []clause.Column{{Name: "canvas_id"}, {Name: "user_id"}}, DoUpdates: clause.Assignments(map[string]any{
				"role": role, "can_generate": gorm.Expr("canvas_viewers.can_generate AND ? = ?", role, model.CanvasRoleEditor),
				"can_copy_to_library": gorm.Expr("canvas_viewers.can_copy_to_library AND ? = ?", role, model.CanvasRoleEditor),
			})}
		}
		return tx.Clauses(conflict).Create(&model.CanvasViewer{CanvasID: canvas, UserID: member, Role: role}).Error
	})
}

// SetGenerationPermission 修改编辑成员的独立生成授权。画布或编辑成员不存在时返回 ErrNotFound。
func (r *CanvasShareRepository) SetGenerationPermission(ctx context.Context, owner, canvas, member uint64, enabled bool) error {
	return r.setEditorPermission(ctx, owner, canvas, member, "can_generate", enabled)
}

// SetLibraryPermission 修改编辑成员收藏画布素材的授权。
func (r *CanvasShareRepository) SetLibraryPermission(ctx context.Context, owner, canvas, member uint64, enabled bool) error {
	return r.setEditorPermission(ctx, owner, canvas, member, "can_copy_to_library", enabled)
}

func (r *CanvasShareRepository) setEditorPermission(ctx context.Context, owner, canvas, member uint64, column string, enabled bool) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		project, err := lockCollaborationProject(tx, canvas)
		if err != nil {
			return err
		}
		if project.UserID != owner || !project.CollaborationActive {
			return ErrNotFound
		}
		result := tx.Model(&model.CanvasViewer{}).Where("canvas_id = ? AND user_id = ? AND role = ?", canvas, member, model.CanvasRoleEditor).
			Update(column, enabled)
		if result.Error == nil && result.RowsAffected != 1 {
			return ErrNotFound
		}
		return result.Error
	})
}

// Remove 撤销单个成员的授权或退出共享。
func (r *CanvasShareRepository) Remove(ctx context.Context, owner, canvas, viewer uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Model(&model.CanvasProject{}).Where("id = ? AND drama_id IS NULL", canvas)
		if owner != viewer {
			query = query.Where("user_id = ?", owner)
		}
		if err := query.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Take(&model.CanvasProject{}).Error; err != nil {
			if owner == viewer && errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return translate(err)
		}
		return tx.Where("canvas_id = ? AND user_id = ?", canvas, viewer).Delete(&model.CanvasViewer{}).Error
	})
}

// Members 列出当前所有者授权的查看成员。
func (r *CanvasShareRepository) Members(ctx context.Context, owner, canvas uint64) ([]model.CanvasShareMember, error) {
	if err := r.owned(ctx, owner, canvas).Select("id").Take(&model.CanvasProject{}).Error; err != nil {
		return nil, translate(err)
	}
	var users []struct {
		model.User
		CanGenerate      bool
		CanCopyToLibrary bool
	}
	err := r.db.WithContext(ctx).Model(&model.User{}).Select("users.id, users.email, users.username, v.role, v.can_generate, v.can_copy_to_library").Joins("JOIN canvas_viewers v ON v.user_id = users.id").
		Where("v.canvas_id = ?", canvas).Order("users.email").Find(&users).Error
	items := make([]model.CanvasShareMember, 0, len(users))
	for _, user := range users {
		items = append(items, model.CanvasShareMember{ID: idcodec.Encode(user.ID), Email: user.Email, Username: user.Username, Role: user.Role,
			CanGenerate: user.CanGenerate && user.Role == model.CanvasRoleEditor, CanCopyToLibrary: user.CanCopyToLibrary && user.Role == model.CanvasRoleEditor})
	}
	return items, err
}

// Shared 按标题分页列出共享给用户的普通画布。
func (r *CanvasShareRepository) Shared(ctx context.Context, user uint64, keyword string, offset, limit int) ([]model.CanvasShareItem, int64, error) {
	query := r.shared(ctx, user)
	if keyword != "" {
		query = query.Where("canvas_projects.title ILIKE ?", "%"+escapeLike(keyword)+"%")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []struct {
		model.CanvasProject
		Role string
	}
	err := query.Select("canvas_projects.id, canvas_projects.title, canvas_projects.cover_asset_id, canvas_projects.revision, canvas_projects.updated_at, CASE WHEN canvas_projects.collaboration_active THEN v.role ELSE 'viewer' END AS role").
		Order("canvas_projects.updated_at DESC, canvas_projects.id DESC").Offset(offset).Limit(limit).Scan(&rows).Error
	items := make([]model.CanvasShareItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, model.CanvasShareItem{ID: idcodec.Encode(row.ID), Title: row.Title, CoverAsset: readCoverID(row.CoverAssetID), Revision: row.Revision, UpdatedAt: row.UpdatedAt, Role: row.Role})
	}
	return items, total, err
}

// SharedCanvas 仅按当前有效的查看授权读取原画布。
func (r *CanvasShareRepository) SharedCanvas(ctx context.Context, user, canvas uint64) (*model.CanvasProject, error) {
	return readCanvasWithCollaboration(r.db.WithContext(ctx), func(db *gorm.DB, project *model.CanvasProject) error {
		return NewCanvasShareRepository(db).shared(ctx, user).Where("canvas_projects.id = ?", canvas).Take(project).Error
	})
}

// SharedCanvasRevision 在同一套查看授权下读取版本号。
func (r *CanvasShareRepository) SharedCanvasRevision(ctx context.Context, user, canvas uint64) (uint64, error) {
	var row model.CanvasProject
	err := r.shared(ctx, user).Select("canvas_projects.revision").Where("canvas_projects.id = ?", canvas).Take(&row).Error
	return row.Revision, translate(err)
}

// Source 读取调用者拥有的普通画布。
func (r *CanvasShareRepository) Source(ctx context.Context, owner, id uint64) (*model.CanvasProject, error) {
	return readCanvasWithCollaboration(r.db.WithContext(ctx), func(db *gorm.DB, project *model.CanvasProject) error {
		return NewCanvasShareRepository(db).owned(ctx, owner, id).Take(project).Error
	})
}

// DeliverySource 为协作副本读取当前代数中指定的确认版本，其他交付要求源版本仍是最新。
func (r *CanvasShareRepository) DeliverySource(ctx context.Context, request model.CanvasDelivery) (*model.CanvasProject, error) {
	var source model.CanvasProject
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := NewCanvasShareRepository(tx).owned(ctx, request.UserID, request.CanvasID).Take(&source).Error; err != nil {
			return translate(err)
		}
		if request.Mode == "copy" && source.CollaborationActive {
			return loadHistoricalGraph(tx, &source, source.CollaborationEpoch, request.Revision)
		}
		if source.Revision != request.Revision {
			return ErrRevisionConflict
		}
		return hydrateCollaborationProject(tx, &source)
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if errors.Is(err, ErrCanvasVersionUnavailable) || errors.Is(err, ErrCanvasCollaborationSession) {
		return nil, ErrRevisionConflict
	}
	return &source, err
}

// Claim 认领交付请求，已成功请求返回原结果。
func (r *CanvasShareRepository) Claim(ctx context.Context, request model.CanvasDelivery) (bool, uint64, error) {
	result := r.db.WithContext(ctx).Exec(`INSERT INTO canvas_deliveries(user_id,request_key,canvas_id,recipient,revision,mode,attempt,status,result_id,updated_at)
 SELECT ?,?,?,?,?,?,?,'running',0,now() FROM canvas_projects WHERE id=? AND user_id=? AND drama_id IS NULL AND deleted_at IS NULL
 ON CONFLICT (user_id,request_key) DO NOTHING`, request.UserID, request.RequestKey, request.CanvasID, request.Recipient, request.Revision, request.Mode, request.Attempt, request.CanvasID, request.UserID)
	if result.Error != nil {
		return false, 0, result.Error
	}
	if result.RowsAffected == 1 {
		return true, 0, nil
	}
	var saved model.CanvasDelivery
	if err := r.db.WithContext(ctx).Where("user_id = ? AND request_key = ?", request.UserID, request.RequestKey).Take(&saved).Error; err != nil {
		return false, 0, translate(err)
	}
	if saved.CanvasID != request.CanvasID || saved.Mode != request.Mode || saved.Recipient != request.Recipient || saved.Revision != request.Revision {
		return false, 0, ErrRevisionConflict
	}
	if saved.Status == "complete" {
		return false, saved.ResultID, nil
	}

	res := r.db.WithContext(ctx).Model(&saved).Where("user_id = ? AND request_key = ? AND (status = 'failed' OR (status = 'running' AND updated_at < now()-interval '7 minutes'))", request.UserID, request.RequestKey).
		Updates(map[string]any{"attempt": request.Attempt, "status": "running", "updated_at": gorm.Expr("now()")})
	if res.Error == nil && res.RowsAffected == 0 {
		return false, 0, ErrInUse
	}
	return res.RowsAffected == 1, 0, res.Error
}

// Release 仅释放当前尝试的失败请求。
func (r *CanvasShareRepository) Release(ctx context.Context, request model.CanvasDelivery) error {
	return r.db.WithContext(ctx).Model(&model.CanvasDelivery{}).Where("user_id = ? AND request_key = ? AND attempt = ? AND status = ?", request.UserID, request.RequestKey, request.Attempt, "running").Update("status", "failed").Error
}

// CanvasShareTx 约束最终交付事务内允许的写入。
type CanvasShareTx interface {
	CreateAsset(context.Context, *model.Asset) error
	CreateCanvas(context.Context, *model.CanvasProject) error
}

// WithDelivery 锁定源画布并复核交付条件，在同一事务内写入目标和交付收据。
func (r *CanvasShareRepository) WithDelivery(ctx context.Context, request model.CanvasDelivery, cover, epoch uint64, create func(CanvasShareTx) (uint64, error)) error {
	return r.db.WithContext(ctx).Transaction(func(dbtx *gorm.DB) error {
		tx := NewCanvasShareRepository(dbtx)
		if err := tx.lockDelivery(ctx, request, cover, epoch); err != nil {
			return err
		}
		id, err := create(tx)
		if err != nil {
			return err
		}
		if request.Mode == "transfer" {
			if err := tx.retire(ctx, request.UserID, request.CanvasID); err != nil {
				return err
			}
		}
		result := dbtx.Model(&model.CanvasDelivery{}).Where("user_id = ? AND request_key = ? AND attempt = ? AND status = ?", request.UserID, request.RequestKey, request.Attempt, "running").Updates(map[string]any{"result_id": id, "status": "complete"})
		if result.Error == nil && result.RowsAffected != 1 {
			return ErrRevisionConflict
		}
		if result.Error != nil {
			return result.Error
		}
		return dbtx.Where("attempt = ?", request.Attempt).Delete(&model.CanvasDeliveryStage{}).Error
	})
}

// Receipt reads the sender's delivery status even if the source has retired.
func (r *CanvasShareRepository) Receipt(ctx context.Context, user, canvas uint64, key string) (string, error) {
	var row model.CanvasDelivery
	err := r.db.WithContext(ctx).Select("status").Where("user_id = ? AND canvas_id = ? AND request_key = ?", user, canvas, key).Take(&row).Error
	return row.Status, translate(err)
}

// Result 复核提交结果，避免清理已交付的素材。
func (r *CanvasShareRepository) Result(ctx context.Context, request model.CanvasDelivery) (uint64, error) {
	var row model.CanvasDelivery
	err := r.db.WithContext(ctx).Select("result_id").Where("user_id = ? AND request_key = ?", request.UserID, request.RequestKey).Take(&row).Error
	return row.ResultID, err
}

// Ready 检查可能回写画布的生成、拆分镜和成片任务。
func (r *CanvasShareRepository) Ready(ctx context.Context, user, canvas uint64) error {
	checks := []struct {
		model any
		query string
		args  []any
	}{
		{&model.GenerationTask{}, "canvas_project_id = ? AND (user_id = ? OR collaboration_epoch > 0) AND status IN ?", []any{canvas, user, model.ActiveTaskStatuses}},
		{&model.StoryboardSplitJob{}, "canvas_id = ? AND (user_id = ? OR collaboration_epoch > 0) AND status IN ?", []any{canvas, user, []string{model.SplitJobPending, model.SplitJobRunning}}},
		{&model.CanvasComposeJob{}, "canvas_id = ? AND status IN ? AND EXISTS (SELECT 1 FROM canvas_projects WHERE id = canvas_compose_jobs.canvas_id AND collaboration_active AND collaboration_epoch = canvas_compose_jobs.epoch)",
			[]any{canvas, []string{model.TaskPending, model.TaskRunning}}},
	}
	for _, check := range checks {
		var count int64
		if err := r.db.WithContext(ctx).Model(check.model).Where(check.query, check.args...).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return ErrInUse
		}
	}
	return nil
}

// CreateAsset 在交付事务中登记独立的接收方素材。
func (r *CanvasShareRepository) CreateAsset(ctx context.Context, asset *model.Asset) error {
	return r.db.WithContext(ctx).Create(asset).Error
}

// CreateCanvas 写入已改写引用的新画布。
func (r *CanvasShareRepository) CreateCanvas(ctx context.Context, canvas *model.CanvasProject) error {
	return r.db.WithContext(ctx).Create(canvas).Error
}

func (r *CanvasShareRepository) retire(ctx context.Context, owner, canvas uint64) error {
	if err := r.db.WithContext(ctx).Where("canvas_id = ?", canvas).Delete(&model.CanvasViewer{}).Error; err != nil {
		return err
	}
	if err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", canvas, owner).Delete(&model.CanvasProject{}).Error; err != nil {
		return err
	}
	return nil
}

func (r *CanvasShareRepository) owned(ctx context.Context, owner, canvas uint64) *gorm.DB {
	return r.db.WithContext(ctx).Model(&model.CanvasProject{}).Where("user_id = ? AND id = ? AND drama_id IS NULL", owner, canvas)
}

func (r *CanvasShareRepository) shared(ctx context.Context, user uint64) *gorm.DB {
	return r.db.WithContext(ctx).Model(&model.CanvasProject{}).Joins("JOIN canvas_viewers v ON v.canvas_id = canvas_projects.id AND v.user_id = ?", user).
		Where("canvas_projects.drama_id IS NULL")
}

// Stage 在对象复制前记录其回收信息。
func (r *CanvasShareRepository) Stage(ctx context.Context, stage *model.CanvasDeliveryStage) error {
	return r.db.WithContext(ctx).Create(stage).Error
}

// SweepStages 回收过期暂存对象，失败的记录保留用于重试。
func (r *CanvasShareRepository) SweepStages(ctx context.Context, remove func(context.Context, uint64, string) error) error {
	var stages []model.CanvasDeliveryStage
	if err := r.db.WithContext(ctx).Where("created_at < now() - interval '10 minutes'").Order("created_at").Limit(200).Find(&stages).Error; err != nil {
		return err
	}
	var errs []error
	for _, stage := range stages {
		if err := r.sweepStage(ctx, stage, remove); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (r *CanvasShareRepository) sweepStage(ctx context.Context, stage model.CanvasDeliveryStage, remove func(context.Context, uint64, string) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("storage_key = ? AND created_at < now() - interval '10 minutes'", stage.StorageKey).Find(&stage)
		if result.Error != nil || result.RowsAffected == 0 {
			return result.Error
		}
		if err := remove(ctx, stage.StorageID, stage.StorageKey); err != nil {
			return err
		}
		return tx.Delete(&stage).Error
	})
}

func (r *CanvasShareRepository) lockDelivery(ctx context.Context, request model.CanvasDelivery, cover, epoch uint64) error {
	var source model.CanvasProject
	if err := r.owned(ctx, request.UserID, request.CanvasID).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&source).Error; err != nil {
		return translate(err)
	}
	confirmedCopy := request.Mode == "copy" && source.CollaborationActive && source.Revision >= request.Revision
	if (!confirmedCopy && source.Revision != request.Revision) || source.CoverAssetID != cover || source.CollaborationEpoch != epoch {
		return ErrRevisionConflict
	}
	var user model.User
	if err := r.db.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND status = ?", request.Recipient, model.UserStatusActive).Select("id").Take(&user).Error; err != nil {
		return translate(err)
	}
	if err := r.Ready(ctx, request.UserID, request.CanvasID); err != nil {
		return err
	}
	return nil
}

func readCoverID(id uint64) string {
	if id == 0 {
		return ""
	}
	return idcodec.Encode(id)
}
