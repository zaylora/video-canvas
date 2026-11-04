package repository

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
)

// RecentDramaBatches 按用户、短剧查询最近三次成功批次的范围，不加载输入或输出。
func (r *DramaRepository) RecentDramaBatches(ctx context.Context, userID, dramaID uint64) ([]model.DramaJob, error) {
	var jobs []model.DramaJob
	err := r.db.WithContext(ctx).Select("id", "step", "status", "batch_from", "batch_to", "episodes").Where("user_id = ? AND drama_id = ? AND status = 'succeeded' AND step IN ?", userID, dramaID, []string{"plan", "breakdown"}).Order("id DESC").Limit(3).Find(&jobs).Error
	return jobs, err
}

// CleanupDrafts 物理删除过期草稿，不读取任何原文到应用进程。
func (r *DramaRepository) CleanupDrafts(ctx context.Context) error {
	return r.db.WithContext(ctx).Where("expires_at <= ?", time.Now()).Delete(&model.DramaSourceDraft{}).Error
}

// RecentDramaDurations 查询模型最近五十个同步骤成功任务的耗时，不读取输出及快照。
func (r *DramaRepository) RecentDramaDurations(ctx context.Context, modelID uint64, step string) ([]int, error) {
	var seconds []int
	err := r.db.WithContext(ctx).Model(&model.DramaJob{}).Where("model_id = ? AND step = ? AND status = 'succeeded' AND started_at IS NOT NULL AND finished_at IS NOT NULL", modelID, step).
		Order("finished_at DESC, id DESC").Limit(50).Select("GREATEST(1, EXTRACT(EPOCH FROM (finished_at - started_at))::bigint)").Scan(&seconds).Error
	return seconds, err
}

// Canvas 按用户和画布查询，不存在返回 ErrNotFound。
func (r *DramaRepository) Canvas(ctx context.Context, userID, id uint64) (*model.CanvasProject, error) {
	var row model.CanvasProject
	err := r.db.WithContext(ctx).Where("user_id = ? AND id = ?", userID, id).First(&row).Error
	return &row, translate(err)
}

// CreateDraft 锁用户行限制每人三份有效草稿，同时回收过期正文。
func (r *DramaRepository) CreateDraft(ctx context.Context, draft *model.DramaSourceDraft) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user model.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, draft.UserID).Error; err != nil {
			return translate(err)
		}
		if err := tx.Where("expires_at <= ?", time.Now()).Delete(&model.DramaSourceDraft{}).Error; err != nil {
			return err
		}
		if err := tx.Create(draft).Error; err != nil {
			return err
		}
		var ids []string
		if err := tx.Model(&model.DramaSourceDraft{}).Where("user_id = ?", draft.UserID).Order("created_at DESC, id DESC").Offset(3).Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		return tx.Where("user_id = ? AND id IN ?", draft.UserID, ids).Delete(&model.DramaSourceDraft{}).Error
	})
}

// Draft 按用户和 token 查询有效草稿，过期或不属于本人返回 ErrNotFound。
func (r *DramaRepository) Draft(ctx context.Context, userID uint64, token string, lock bool) (*model.DramaSourceDraft, error) {
	q := r.db.WithContext(ctx)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var draft model.DramaSourceDraft
	err := q.Where("user_id = ? AND id = ? AND expires_at > ?", userID, token, time.Now()).First(&draft).Error
	return &draft, translate(err)
}

// DeleteDraft 仅删除本人草稿，不存在返回 ErrNotFound。
func (r *DramaRepository) DeleteDraft(ctx context.Context, userID uint64, token string) error {
	res := r.db.WithContext(ctx).Where("user_id = ? AND id = ?", userID, token).Delete(&model.DramaSourceDraft{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// InsertUnits 批量写入项目单元，调用者保证已锁定本人项目。
func (r *DramaRepository) InsertUnits(ctx context.Context, units []model.DramaUnit) error {
	if len(units) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).CreateInBatches(units, 100).Error
}

// Units 按用户、项目与闭区间查询完整原文，to 为零时不限末尾。
func (r *DramaRepository) Units(ctx context.Context, userID, dramaID uint64, from, to int) ([]model.DramaUnit, error) {
	q := r.db.WithContext(ctx).Where("user_id = ? AND drama_id = ? AND no >= ?", userID, dramaID, max(1, from))
	if to > 0 {
		q = q.Where("no <= ?", to)
	}
	var units []model.DramaUnit
	err := q.Order("no ASC").Find(&units).Error
	return units, err
}

// Prompts 按用户和可选环节查询，更新时间倒序。
func (r *DramaRepository) Prompts(ctx context.Context, userID uint64, step string) ([]model.DramaPrompt, error) {
	q := r.db.WithContext(ctx).Where("user_id = ?", userID)
	if step != "" {
		q = q.Where("step = ?", step)
	}
	var rows []model.DramaPrompt
	err := q.Order("updated_at DESC, id DESC").Find(&rows).Error
	return rows, err
}

// Prompt 按用户和 ID 查询正文，不存在返回 ErrNotFound。
func (r *DramaRepository) Prompt(ctx context.Context, userID, id uint64) (*model.DramaPrompt, error) {
	var row model.DramaPrompt
	err := r.db.WithContext(ctx).Where("user_id = ? AND id = ?", userID, id).First(&row).Error
	return &row, translate(err)
}

// SavePrompt 创建或按本人 revision 更新提示词，重名返回 ErrDuplicate，版本冲突返回 ErrStateConflict。
func (r *DramaRepository) SavePrompt(ctx context.Context, row *model.DramaPrompt) error {
	if row.ID == 0 {
		err := r.db.WithContext(ctx).Create(row).Error
		if isUniqueViolation(err) {
			return ErrDuplicate
		}
		return err
	}
	res := r.db.WithContext(ctx).Model(&model.DramaPrompt{}).Where("user_id = ? AND id = ? AND revision = ?", row.UserID, row.ID, row.Revision).
		Updates(map[string]any{"step": row.Step, "name": row.Name, "bodies": row.Bodies, "revision": gorm.Expr("revision + 1"), "updated_at": time.Now()})
	if isUniqueViolation(res.Error) {
		return ErrDuplicate
	}
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrStateConflict
	}
	row.Revision++
	row.UpdatedAt = time.Now()
	return nil
}

// DeletePrompt 硬删本人提示词，不修改项目和任务快照；不存在返回 ErrNotFound。
func (r *DramaRepository) DeletePrompt(ctx context.Context, userID, id uint64) error {
	res := r.db.WithContext(ctx).Where("user_id = ? AND id = ?", userID, id).Delete(&model.DramaPrompt{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
