package repository

import (
	"context"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"video-canvas/internal/model"
)

// CanvasTransferTx 是画布导入事务里要用到的 SQL 原语；CanvasTransferRepository 自身实现它，WithTx 把它绑定到事务上传给回调。
type CanvasTransferTx interface {
	// CreateAsset 插入一条素材记录，成功后回填 id；storage_key 冲突返回 ErrDuplicate。
	CreateAsset(ctx context.Context, a *model.Asset) error
	// CountCharacters 返回该用户已有的角色卡数量，用于检查角色卡数量上限。
	CountCharacters(ctx context.Context, userID uint64) (int64, error)
	// CharacterNameExists 判断该用户是否已有同名（不区分大小写）的角色卡。
	CharacterNameExists(ctx context.Context, userID uint64, name string) (bool, error)
	// CreateCharacter 插入一张角色卡，成功后回填 id；同一用户重名返回 ErrDuplicate，且不会让所在事务失效。
	CreateCharacter(ctx context.Context, c *model.CharacterCard) error
	// CreateCanvas 插入一张画布，成功后回填 id。
	CreateCanvas(ctx context.Context, p *model.CanvasProject) error
}

// CanvasTransferRepository 是画布导入的事务仓储：素材、角色卡、画布在同一个事务里写入。
type CanvasTransferRepository struct {
	db *gorm.DB
}

// NewCanvasTransferRepository 创建画布导入事务仓储。
func NewCanvasTransferRepository(db *gorm.DB) *CanvasTransferRepository {
	return &CanvasTransferRepository{db: db}
}

var _ CanvasTransferTx = (*CanvasTransferRepository)(nil)

// WithTx 在一个事务里执行 fn：fn 返回错误整体回滚，否则提交。回调里只能用传入的 tx。
func (r *CanvasTransferRepository) WithTx(ctx context.Context, fn func(tx CanvasTransferTx) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(&CanvasTransferRepository{db: tx})
	})
}

// CreateAsset 插入一条素材记录并回填 id；storage_key 唯一索引冲突返回 ErrDuplicate。
func (r *CanvasTransferRepository) CreateAsset(ctx context.Context, a *model.Asset) error {
	if err := r.db.WithContext(ctx).Create(a).Error; err != nil {
		if isUniqueViolation(err) {
			return ErrDuplicate
		}
		return err
	}
	return nil
}

// CountCharacters 按 user_id 统计角色卡数量。
func (r *CanvasTransferRepository) CountCharacters(ctx context.Context, userID uint64) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.CharacterCard{}).Where("user_id = ?", userID).Count(&n).Error
	return n, err
}

// CharacterNameExists 按 user_id + lower(name) 判断是否已有同名角色卡。
func (r *CanvasTransferRepository) CharacterNameExists(ctx context.Context, userID uint64, name string) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.CharacterCard{}).
		Where("user_id = ? AND lower(name) = lower(?)", userID, name).Count(&n).Error
	return n > 0, err
}

// CreateCharacter 插入角色卡并回填 id；同一用户重名（唯一索引 user_id + lower(name)）返回 ErrDuplicate。
// 插入包在保存点里：PostgreSQL 的事务一旦有语句失败就整体作废，用保存点只回退这一条，调用方才能换个名字重试。
func (r *CanvasTransferRepository) CreateCharacter(ctx context.Context, c *model.CharacterCard) error {
	if c.ExtraAssetIDs == nil {
		c.ExtraAssetIDs = datatypes.JSONSlice[uint64]{}
	}
	err := r.db.WithContext(ctx).Transaction(func(sp *gorm.DB) error {
		return sp.Create(c).Error
	})
	if err != nil {
		if isUniqueViolation(err) {
			return ErrDuplicate
		}
		return err
	}
	return nil
}

// CreateCanvas 插入一张画布并回填 id。
func (r *CanvasTransferRepository) CreateCanvas(ctx context.Context, p *model.CanvasProject) error {
	return r.db.WithContext(ctx).Create(p).Error
}
