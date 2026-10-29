package repository

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
)

// liveSegmentEpisodes 限定同一用户且项目与分集都未软删，避免父项目删除后仍读到片段。
func (r *DramaRepository) liveSegmentEpisodes(ctx context.Context, userID uint64) *gorm.DB {
	projects := r.db.WithContext(ctx).Model(&model.DramaProject{}).Select("id").Where("user_id = ?", userID)
	return r.db.WithContext(ctx).Model(&model.DramaEpisode{}).Select("id").Where("user_id = ? AND drama_id IN (?)", userID, projects)
}

// SegmentEpisode 按用户和集ID读取存活项目下的集，缺失返回ErrNotFound。
func (r *DramaRepository) SegmentEpisode(ctx context.Context, userID, episodeID uint64) (*model.DramaEpisode, error) {
	var ep model.DramaEpisode
	err := r.db.WithContext(ctx).Where("user_id = ? AND id = ? AND id IN (?)", userID, episodeID, r.liveSegmentEpisodes(ctx, userID)).First(&ep).Error
	return &ep, translate(err)
}

// Segments 按用户和集ID读取存活项目下的片段，按seq、ID排序。
func (r *DramaRepository) Segments(ctx context.Context, userID, episodeID uint64) ([]model.DramaSegment, error) {
	rows := []model.DramaSegment{}
	err := r.db.WithContext(ctx).Where("user_id = ? AND episode_id = ? AND episode_id IN (?)", userID, episodeID, r.liveSegmentEpisodes(ctx, userID)).Order("seq ASC, id ASC").Find(&rows).Error
	return rows, err
}

// Segment 按用户和ID读取存活项目下的片段，缺失返回ErrNotFound；写入前须先锁项目。
func (r *DramaRepository) Segment(ctx context.Context, userID, id uint64, lock bool) (*model.DramaSegment, error) {
	q := r.db.WithContext(ctx).Where("user_id = ? AND id = ? AND episode_id IN (?)", userID, id, r.liveSegmentEpisodes(ctx, userID))
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var row model.DramaSegment
	err := q.First(&row).Error
	return &row, translate(err)
}

// SaveSegment 按用户、集与revision更新；旧版本返回ErrStateConflict，父资源不存在返回ErrNotFound。
func (r *DramaRepository) SaveSegment(ctx context.Context, row *model.DramaSegment, revision int64) error {
	if _, err := r.SegmentEpisode(ctx, row.UserID, row.EpisodeID); err != nil {
		return err
	}
	if row.ID == 0 {
		if err := r.db.WithContext(ctx).Create(row).Error; err != nil {
			return err
		}
		return r.bumpSegmentVersion(ctx, row.UserID, row.EpisodeID)
	}
	previous, err := r.Segment(ctx, row.UserID, row.ID, false)
	if err != nil {
		return err
	}
	now := time.Now()
	res := r.db.WithContext(ctx).Model(&model.DramaSegment{}).Where("user_id = ? AND episode_id = ? AND id = ? AND revision = ?", row.UserID, row.EpisodeID, row.ID, revision).Updates(map[string]any{
		"seq": row.Seq, "seconds": row.Seconds, "prompt": row.Prompt,
		"chosen_task_id": row.ChosenTaskID,
		"revision":       gorm.Expr("revision + 1"), "updated_at": now,
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrStateConflict
	}
	row.Revision, row.UpdatedAt = revision+1, now
	if previous.Seq != row.Seq || !sameChosenTask(previous.ChosenTaskID, row.ChosenTaskID) {
		return r.bumpSegmentVersion(ctx, row.UserID, row.EpisodeID)
	}
	return nil
}

// DeleteSegment 只软删当前用户存活项目下的片段，缺失返回ErrNotFound；任务和素材不受影响。
func (r *DramaRepository) DeleteSegment(ctx context.Context, userID, id uint64) error {
	row, err := r.Segment(ctx, userID, id, false)
	if err != nil {
		return err
	}
	res := r.db.WithContext(ctx).Where("user_id = ? AND id = ? AND episode_id IN (?)", userID, id, r.liveSegmentEpisodes(ctx, userID)).Delete(&model.DramaSegment{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return r.bumpSegmentVersion(ctx, userID, row.EpisodeID)
}

// ReplaceSegments 在调用方项目锁保护的事务内替换全集片段，不删除历史任务和素材。
func (r *DramaRepository) ReplaceSegments(ctx context.Context, userID, episodeID uint64, rows []model.DramaSegment) error {
	if _, err := r.SegmentEpisode(ctx, userID, episodeID); err != nil {
		return err
	}
	if err := r.db.WithContext(ctx).Where("user_id = ? AND episode_id = ?", userID, episodeID).Delete(&model.DramaSegment{}).Error; err != nil {
		return err
	}
	for i := range rows {
		rows[i].BaseModel = model.BaseModel{}
		rows[i].UserID, rows[i].EpisodeID, rows[i].Seq, rows[i].Revision = userID, episodeID, i, 1
	}
	if len(rows) > 0 {
		if err := r.db.WithContext(ctx).Create(&rows).Error; err != nil {
			return err
		}
	}
	return r.bumpSegmentVersion(ctx, userID, episodeID)
}

// SegmentTasks 严格匹配用户、画布、节点且排除试跑，按创建时间、ID正序返回。
func (r *DramaRepository) SegmentTasks(ctx context.Context, userID, canvasID uint64, nodeID string) ([]model.GenerationTask, error) {
	rows := []model.GenerationTask{}
	err := r.db.WithContext(ctx).Where("user_id = ? AND canvas_project_id = ? AND node_id = ? AND is_test = false", userID, canvasID, nodeID).Order("created_at ASC, id ASC").Find(&rows).Error
	return rows, err
}

// HasSegmentTakes 判断同用户存活项目下该集现有片段是否有任何视频任务，包括失败或进行中的任务。
func (r *DramaRepository) HasSegmentTakes(ctx context.Context, userID, episodeID uint64) (bool, error) {
	ep, err := r.SegmentEpisode(ctx, userID, episodeID)
	if err != nil {
		return false, err
	}
	project, err := r.Project(ctx, userID, ep.DramaID, false)
	if err != nil {
		return false, err
	}
	segments, err := r.Segments(ctx, userID, episodeID)
	if err != nil || len(segments) == 0 {
		return false, err
	}
	nodes := make([]string, len(segments))
	for i := range segments {
		nodes[i] = "seg:" + idcodec.Encode(segments[i].ID)
	}
	var count int64
	err = r.db.WithContext(ctx).Model(&model.GenerationTask{}).Where("user_id = ? AND canvas_project_id = ? AND node_id IN ? AND kind = ? AND is_test = false", userID, project.CanvasID, nodes, model.KindVideo).Count(&count).Error
	return count > 0, err
}

// bumpSegmentVersion 记录采用、顺序和增删发生过变化；即使之后改回也不能误报成片仍新鲜。
func (r *DramaRepository) bumpSegmentVersion(ctx context.Context, userID, episodeID uint64) error {
	res := r.db.WithContext(ctx).Model(&model.DramaEpisode{}).Where("user_id = ? AND id = ?", userID, episodeID).
		Updates(map[string]any{"segment_version": gorm.Expr("segment_version + 1"), "updated_at": time.Now()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// sameChosenTask 比较可空采用ID，提示词、首帧等编辑不影响既有成片。
func sameChosenTask(a, b *uint64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
