package repository

import (
	"context"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
)

// CanvasProjectRepository 是画布项目的数据访问层。
type CanvasProjectRepository struct {
	db *gorm.DB
}

// NewCanvasProjectRepository 创建画布项目仓储。
func NewCanvasProjectRepository(db *gorm.DB) *CanvasProjectRepository {
	return &CanvasProjectRepository{db: db}
}

// Create 新建画布项目。
func (r *CanvasProjectRepository) Create(ctx context.Context, p *model.CanvasProject) error {
	return r.db.WithContext(ctx).Create(p).Error
}

// GetByID 按 ID 查询，并限定所属用户，查不到或不属于该用户都返回 ErrNotFound。
func (r *CanvasProjectRepository) GetByID(ctx context.Context, userID, id uint64) (*model.CanvasProject, error) {
	var p model.CanvasProject
	if err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&p).Error; err != nil {
		return nil, translate(err)
	}
	return &p, nil
}

// List 分页查询用户不属于短剧的画布列表，不查 payload_json 大字段；左联 assets 带出封面素材的存储 key（素材没了就是空）。
// 计数不联表，联表后列名要带表名前缀（两张表都有 user_id）。
func (r *CanvasProjectRepository) List(ctx context.Context, userID uint64, keyword string, offset, limit int) ([]model.CanvasProjectItem, int64, error) {
	var (
		items []model.CanvasProjectItem
		total int64
	)
	db := r.db.WithContext(ctx).Model(&model.CanvasProject{}).Where("canvas_projects.user_id = ? AND canvas_projects.drama_id IS NULL", userID)
	if keyword != "" {
		db = db.Where("canvas_projects.title ILIKE ?", "%"+escapeLike(keyword)+"%")
	}
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := db.Joins("LEFT JOIN assets ON assets.id = canvas_projects.cover_asset_id AND canvas_projects.cover_asset_id <> 0").
		Select("canvas_projects.id", "canvas_projects.title", "canvas_projects.revision",
			"canvas_projects.created_at", "canvas_projects.updated_at", "COALESCE(assets.storage_key, '') AS cover_key").
		Order("canvas_projects.updated_at DESC").Offset(offset).Limit(limit).Find(&items).Error
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// SetCoverIfEmpty 在画布还没有封面时把素材设成封面：UPDATE … WHERE id=? AND user_id=? AND cover_asset_id=0。
// 画布不存在、不属于该用户或已经有封面都不算错，返回 nil。故意用 Exec 而不是 Model().Updates：
// 不能动 revision（会让用户正在编辑的画布保存时冲突）也不能动 updated_at（会让列表顺序跳动）。
func (r *CanvasProjectRepository) SetCoverIfEmpty(ctx context.Context, userID, id, assetID uint64) error {
	return r.db.WithContext(ctx).Exec(
		"UPDATE canvas_projects SET cover_asset_id = ? WHERE id = ? AND user_id = ? AND cover_asset_id = 0 AND deleted_at IS NULL",
		assetID, id, userID).Error
}

// Update 按 id + user_id + revision 更新并返回本次写入的完整快照，revision +1。
// 使用 RETURNING 避免更新后重查混入其它写入；记录不存在返回 ErrNotFound，版本不一致返回 ErrRevisionConflict。
func (r *CanvasProjectRepository) Update(ctx context.Context, userID, id, revision uint64, fields map[string]any) (*model.CanvasProject, error) {
	fields["revision"] = gorm.Expr("revision + 1")
	var saved model.CanvasProject
	res := r.db.WithContext(ctx).Model(&saved).Clauses(clause.Returning{}).
		Where("id = ? AND user_id = ? AND revision = ?", id, userID, revision).
		Updates(fields)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected > 0 {
		return &saved, nil
	}
	// 没有更新到任何行：区分是记录不存在还是版本冲突
	var count int64
	if err := r.db.WithContext(ctx).Model(&model.CanvasProject{}).
		Where("id = ? AND user_id = ?", id, userID).Count(&count).Error; err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, ErrNotFound
	}
	return nil, ErrRevisionConflict
}

// Delete 按 id + user_id 软删除画布；不存在或不属于该用户返回 ErrNotFound。
func (r *CanvasProjectRepository) Delete(ctx context.Context, userID, id uint64) error {
	res := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&model.CanvasProject{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// escapeLike 转义 LIKE 通配符，避免用户输入的 % 和 _ 被当作通配符。
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// CountByUser 统计用户未删除且不属于短剧的个人画布数（条件 user_id、drama_id IS NULL）。
func (r *CanvasProjectRepository) CountByUser(ctx context.Context, userID uint64) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.CanvasProject{}).Where("user_id = ? AND drama_id IS NULL", userID).Count(&n).Error
	return n, err
}

// BriefsByIDs 批量返回该用户这些画布的标题与是否已删除（含软删除的，生成历史要区分“已删除的画布”）；
// 查不到的 id（含不属于该用户的）直接缺席，ids 为空返回 nil。
func (r *CanvasProjectRepository) BriefsByIDs(ctx context.Context, userID uint64, ids []uint64) ([]model.CanvasBrief, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []model.CanvasBrief
	err := r.db.WithContext(ctx).Unscoped().Model(&model.CanvasProject{}).
		Select("id, title, (deleted_at IS NOT NULL) AS deleted").
		Where("user_id = ? AND id IN ?", userID, ids).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}
