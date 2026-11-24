package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"video-canvas/internal/model"
)

// MCPRepository 是「画布 MCP 接入」用到的存储：项目、项目令牌。
// 令牌只按 sha256 查，库里没有明文；项目停用与令牌注销都是即时生效的字段，不设缓存。
type MCPRepository struct {
	db *gorm.DB
}

// NewMCPRepository 创建 MCP 接入仓储。
func NewMCPRepository(db *gorm.DB) *MCPRepository { return &MCPRepository{db: db} }

// CreateProject 插入一个项目。
func (r *MCPRepository) CreateProject(ctx context.Context, p *model.MCPProject) error {
	return r.db.WithContext(ctx).Create(p).Error
}

// UpdateProject 保存项目的可改字段（名称、归属用户、权限、默认画布、启停）。
// canvas_id 是 NULL 可空的：传 nil 会把默认画布清掉。
func (r *MCPRepository) UpdateProject(ctx context.Context, p *model.MCPProject) error {
	res := r.db.WithContext(ctx).Model(&model.MCPProject{}).Where("id = ?", p.ID).
		Updates(map[string]any{
			"name":          p.Name,
			"owner_user_id": p.OwnerUserID,
			"canvas_id":     p.CanvasID,
			"scopes":        p.Scopes,
			"enabled":       p.Enabled,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteProject 删除项目连同它的令牌（一个事务）。
func (r *MCPRepository) DeleteProject(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("project_id = ?", id).Delete(&model.MCPProjectToken{}).Error; err != nil {
			return err
		}
		res := tx.Where("id = ?", id).Delete(&model.MCPProject{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// ListProjects 按创建时间倒序列出项目（新的在前）。
// ownerID 为 0 表示全部（后台运维视角），否则只看这个用户名下的项目（用户自助视角）。
func (r *MCPRepository) ListProjects(ctx context.Context, ownerID uint64) ([]model.MCPProject, error) {
	var out []model.MCPProject
	db := r.db.WithContext(ctx)
	if ownerID != 0 {
		db = db.Where("owner_user_id = ?", ownerID)
	}
	err := db.Order("id DESC").Find(&out).Error
	return out, err
}

// CountProjectsByOwner 数一个用户名下有几个项目，用于自助开通时的数量上限。
func (r *MCPRepository) CountProjectsByOwner(ctx context.Context, ownerID uint64) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.MCPProject{}).Where("owner_user_id = ?", ownerID).Count(&n).Error
	return n, err
}

// ProjectByID 读一个项目；不存在返回 ErrNotFound。
func (r *MCPRepository) ProjectByID(ctx context.Context, id uint64) (*model.MCPProject, error) {
	var p model.MCPProject
	err := r.db.WithContext(ctx).First(&p, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// CreateToken 插入一条令牌。
func (r *MCPRepository) CreateToken(ctx context.Context, t *model.MCPProjectToken) error {
	return r.db.WithContext(ctx).Create(t).Error
}

// ListTokens 列出一个项目的令牌（含已注销的，后台要看到历史；不含明文）。
func (r *MCPRepository) ListTokens(ctx context.Context, projectID uint64) ([]model.MCPProjectToken, error) {
	var out []model.MCPProjectToken
	err := r.db.WithContext(ctx).Where("project_id = ?", projectID).Order("id DESC").Find(&out).Error
	return out, err
}

// RevokeToken 注销一条令牌（幂等：已注销的再注销不报错，也不改原时间）。
func (r *MCPRepository) RevokeToken(ctx context.Context, projectID, tokenID uint64, at time.Time) error {
	res := r.db.WithContext(ctx).Model(&model.MCPProjectToken{}).
		Where("id = ? AND project_id = ? AND revoked_at IS NULL", tokenID, projectID).
		Update("revoked_at", at)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		// 已经注销过就不算错；但要区分「根本不存在」，后台才好回 10002。
		var n int64
		if err := r.db.WithContext(ctx).Model(&model.MCPProjectToken{}).
			Where("id = ? AND project_id = ?", tokenID, projectID).Count(&n).Error; err != nil {
			return err
		}
		if n == 0 {
			return ErrNotFound
		}
	}
	return nil
}

// TokenByID 按项目 + 令牌 id 查一条令牌；不存在返回 ErrNotFound。
func (r *MCPRepository) TokenByID(ctx context.Context, projectID, tokenID uint64) (*model.MCPProjectToken, error) {
	var t model.MCPProjectToken
	err := r.db.WithContext(ctx).Where("id = ? AND project_id = ?", tokenID, projectID).First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// DeleteOtherTokens 删掉项目里除 keepID 之外的全部令牌。
// 每个项目只留一枚：重新签发就是轮换，旧行直接删掉，列表里不会堆一串失效的历史。
func (r *MCPRepository) DeleteOtherTokens(ctx context.Context, projectID, keepID uint64) error {
	return r.db.WithContext(ctx).Where("project_id = ? AND id <> ?", projectID, keepID).
		Delete(&model.MCPProjectToken{}).Error
}

// TokenByHash 按 sha256 查令牌；不存在返回 ErrNotFound。
func (r *MCPRepository) TokenByHash(ctx context.Context, hash string) (*model.MCPProjectToken, error) {
	var t model.MCPProjectToken
	err := r.db.WithContext(ctx).Where("token_hash = ?", hash).First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// TouchToken 更新令牌的最近使用时间。
func (r *MCPRepository) TouchToken(ctx context.Context, id uint64, at time.Time) error {
	return r.db.WithContext(ctx).Model(&model.MCPProjectToken{}).Where("id = ?", id).
		Update("last_used_at", at).Error
}

// MCPTokenStat 是一个项目的令牌统计，用于后台列表（不逐个项目发查询）。
type MCPTokenStat struct {
	ProjectID  uint64
	Count      int
	LastUsedAt *time.Time
}

// TokenStats 一次查出项目的令牌数量与最近使用时间。ownerID 为 0 表示全部项目。
// 用户自助视角必须按归属过滤，否则会看到别人的令牌数量。
func (r *MCPRepository) TokenStats(ctx context.Context, ownerID uint64) (map[uint64]MCPTokenStat, error) {
	var rows []struct {
		ProjectID  uint64
		Count      int
		LastUsedAt *time.Time
	}
	db := r.db.WithContext(ctx).Model(&model.MCPProjectToken{}).
		Select("mcp_project_tokens.project_id, COUNT(*) AS count, MAX(mcp_project_tokens.last_used_at) AS last_used_at").
		Where("mcp_project_tokens.revoked_at IS NULL")
	if ownerID != 0 {
		db = db.Joins("JOIN mcp_projects ON mcp_projects.id = mcp_project_tokens.project_id").
			Where("mcp_projects.owner_user_id = ?", ownerID)
	}
	err := db.Group("mcp_project_tokens.project_id").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make(map[uint64]MCPTokenStat, len(rows))
	for _, row := range rows {
		out[row.ProjectID] = MCPTokenStat{ProjectID: row.ProjectID, Count: row.Count, LastUsedAt: row.LastUsedAt}
	}
	return out, nil
}

// CanvasTitles 批量取画布标题，给后台列表回显用（默认画布在写入时已校验过归属，这里只按 id 取标题）。
func (r *MCPRepository) CanvasTitles(ctx context.Context, ids []uint64) (map[uint64]string, error) {
	if len(ids) == 0 {
		return map[uint64]string{}, nil
	}
	var rows []struct {
		ID    uint64
		Title string
	}
	err := r.db.WithContext(ctx).Table("canvas_projects").Select("id, title").Where("id IN ?", ids).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make(map[uint64]string, len(rows))
	for _, row := range rows {
		out[row.ID] = row.Title
	}
	return out, nil
}

// TokensByProjects 一次取多个项目的令牌，每个项目只取最新一枚。
//
// 一个项目就是一个接入，列表页把令牌状态直接挂在项目上，不再单独请求「令牌列表」。
func (r *MCPRepository) TokensByProjects(ctx context.Context, ids []uint64) (map[uint64]model.MCPProjectToken, error) {
	out := make(map[uint64]model.MCPProjectToken, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var rows []model.MCPProjectToken
	err := r.db.WithContext(ctx).
		Raw(`SELECT DISTINCT ON (project_id) * FROM mcp_project_tokens
			WHERE project_id IN ? ORDER BY project_id, id DESC`, ids).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, t := range rows {
		out[t.ProjectID] = t
	}
	return out, nil
}
