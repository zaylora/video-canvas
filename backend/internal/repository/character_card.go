package repository

import (
	"context"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"video-canvas/internal/model"
)

// CharacterCardRepository 是角色卡表的数据访问层。
type CharacterCardRepository struct {
	db *gorm.DB
}

// NewCharacterCardRepository 创建角色卡仓储。
func NewCharacterCardRepository(db *gorm.DB) *CharacterCardRepository {
	return &CharacterCardRepository{db: db}
}

// List 按用户查询不属于短剧的角色卡，可按名称做字面量 ILIKE 包含搜索；结果按更新时间、id 倒序。
func (r *CharacterCardRepository) List(ctx context.Context, userID uint64, q string) ([]model.CharacterCard, error) {
	db := r.db.WithContext(ctx).Where("user_id = ? AND drama_id IS NULL", userID)
	if q != "" {
		db = db.Where("name ILIKE ?", "%"+escapeLike(q)+"%")
	}
	var cards []model.CharacterCard
	if err := db.Order("updated_at DESC, id DESC").Find(&cards).Error; err != nil {
		return nil, err
	}
	return cards, nil
}

// ListByIDs 批量查询用户自己的角色卡；不存在、不属于用户的 id 直接缺席，空 ids 返回 nil。
func (r *CharacterCardRepository) ListByIDs(ctx context.Context, userID uint64, ids []uint64) ([]model.CharacterCard, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var cards []model.CharacterCard
	if err := r.db.WithContext(ctx).Where("user_id = ? AND id IN ?", userID, ids).
		Order("updated_at DESC, id DESC").Find(&cards).Error; err != nil {
		return nil, err
	}
	return cards, nil
}

// GetByID 按 id + user_id 查询角色卡，不存在或不属于用户返回 ErrNotFound。
func (r *CharacterCardRepository) GetByID(ctx context.Context, userID, id uint64) (*model.CharacterCard, error) {
	var card model.CharacterCard
	if err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&card).Error; err != nil {
		return nil, translate(err)
	}
	return &card, nil
}

// Count 统计用户不属于短剧的角色卡数量，用于个人角色库的数量上限。
func (r *CharacterCardRepository) Count(ctx context.Context, userID uint64) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&model.CharacterCard{}).Where("user_id = ? AND drama_id IS NULL", userID).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// Create 插入角色卡，成功后回填 id 与时间；名称允许重复。
func (r *CharacterCardRepository) Create(ctx context.Context, card *model.CharacterCard) error {
	if card.ExtraAssetIDs == nil {
		card.ExtraAssetIDs = datatypes.JSONSlice[uint64]{}
	}
	if err := r.db.WithContext(ctx).Create(card).Error; err != nil {
		return err
	}
	return nil
}

// Update 按 id + user_id 更新角色卡全部可变字段；不存在、不属于用户返回 ErrNotFound。
func (r *CharacterCardRepository) Update(ctx context.Context, userID uint64, card *model.CharacterCard) error {
	extra := card.ExtraAssetIDs
	if extra == nil {
		extra = datatypes.JSONSlice[uint64]{}
	}
	now := time.Now()
	res := r.db.WithContext(ctx).Model(&model.CharacterCard{}).
		Where("id = ? AND user_id = ?", card.ID, userID).
		Updates(map[string]any{
			"name":            card.Name,
			"description":     card.Description,
			"look_name":       card.LookName,
			"voice_prompt":    card.VoicePrompt,
			"avatar_asset_id": card.AvatarAssetID,
			"extra_asset_ids": extra,
			"voice_asset_id":  card.VoiceAssetID,
			"updated_at":      now,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	card.UserID = userID
	card.ExtraAssetIDs = extra
	card.UpdatedAt = now
	return nil
}

// Delete 按 id + user_id 删除角色卡，不存在或不属于用户返回 ErrNotFound。
func (r *CharacterCardRepository) Delete(ctx context.Context, userID, id uint64) error {
	res := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&model.CharacterCard{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
