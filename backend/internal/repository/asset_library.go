package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"video-canvas/internal/model"
)

// TimeCursor 是按 (created_at, id) 翻页的位置：上一页最后一条的创建时间与主键。
type TimeCursor struct {
	At time.Time // 创建时间
	ID uint64    // 主键
}

// AssetFolderTx 是删除文件夹事务里要用到的 SQL 原语；AssetFolderRepository 自身实现它，WithTx 把它绑定到事务上传给回调。
type AssetFolderTx interface {
	// GetByID 按 id + user_id 查询文件夹，不存在或不属于该用户返回 ErrNotFound。
	GetByID(ctx context.Context, userID, id uint64) (*model.AssetFolder, error)
	// DeleteItems 删除该用户在这个文件夹里的全部库归属行，返回删除的行数；不动 assets 与存储文件。
	DeleteItems(ctx context.Context, userID, folderID uint64) (int64, error)
	// Delete 删除文件夹本身，不存在或不属于该用户返回 ErrNotFound。
	Delete(ctx context.Context, userID, id uint64) error
}

// AssetFolderRepository 资产库文件夹表（asset_folders）的数据访问。
type AssetFolderRepository struct {
	db *gorm.DB
}

// NewAssetFolderRepository 创建文件夹仓储。
func NewAssetFolderRepository(db *gorm.DB) *AssetFolderRepository {
	return &AssetFolderRepository{db: db}
}

var _ AssetFolderTx = (*AssetFolderRepository)(nil)

// WithTx 在一个事务里执行 fn：fn 返回错误整体回滚，否则提交。回调里只能用传入的 tx。
func (r *AssetFolderRepository) WithTx(ctx context.Context, fn func(tx AssetFolderTx) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(&AssetFolderRepository{db: tx})
	})
}

// ListWithCount 返回用户全部文件夹及各自的素材数，按创建时间、id 升序（新建的排在后面）。
func (r *AssetFolderRepository) ListWithCount(ctx context.Context, userID uint64) ([]model.AssetFolderCount, error) {
	var rows []model.AssetFolderCount
	err := r.db.WithContext(ctx).Table("asset_folders AS f").
		Select("f.*, (SELECT count(*) FROM asset_library_items i WHERE i.user_id = f.user_id AND i.folder_id = f.id) AS item_count").
		Where("f.user_id = ?", userID).Order("f.created_at ASC, f.id ASC").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// Count 统计用户的文件夹数。
func (r *AssetFolderRepository) Count(ctx context.Context, userID uint64) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.AssetFolder{}).Where("user_id = ?", userID).Count(&n).Error
	return n, err
}

// NameExists 判断用户名下是否已有同名（忽略大小写）的文件夹；excludeID 非 0 时不把这个文件夹本身算进去（重命名用）。
func (r *AssetFolderRepository) NameExists(ctx context.Context, userID uint64, name string, excludeID uint64) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.AssetFolder{}).
		Where("user_id = ? AND lower(name) = lower(?) AND id <> ?", userID, name, excludeID).Count(&n).Error
	return n > 0, err
}

// Create 插入文件夹，成功后 f.ID 已回填；同一用户下名称（忽略大小写）重复返回 ErrDuplicate。
func (r *AssetFolderRepository) Create(ctx context.Context, f *model.AssetFolder) error {
	if err := r.db.WithContext(ctx).Create(f).Error; err != nil {
		if isUniqueViolation(err) {
			return ErrDuplicate
		}
		return err
	}
	return nil
}

// GetByID 按 id + user_id 查询文件夹，不存在或不属于该用户返回 ErrNotFound。
func (r *AssetFolderRepository) GetByID(ctx context.Context, userID, id uint64) (*model.AssetFolder, error) {
	var f model.AssetFolder
	if err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&f).Error; err != nil {
		return nil, translate(err)
	}
	return &f, nil
}

// Rename 改名并刷新 updated_at，不存在或不属于该用户返回 ErrNotFound，重名返回 ErrDuplicate。
func (r *AssetFolderRepository) Rename(ctx context.Context, userID, id uint64, name string) error {
	res := r.db.WithContext(ctx).Model(&model.AssetFolder{}).Where("id = ? AND user_id = ?", id, userID).Update("name", name)
	if res.Error != nil {
		if isUniqueViolation(res.Error) {
			return ErrDuplicate
		}
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// CountItems 统计这个文件夹里该用户的素材数。
func (r *AssetFolderRepository) CountItems(ctx context.Context, userID, folderID uint64) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.AssetLibraryItem{}).Where("user_id = ? AND folder_id = ?", userID, folderID).Count(&n).Error
	return n, err
}

// DeleteItems 删除该用户在这个文件夹里的全部库归属行，返回删除的行数。
func (r *AssetFolderRepository) DeleteItems(ctx context.Context, userID, folderID uint64) (int64, error) {
	res := r.db.WithContext(ctx).Where("user_id = ? AND folder_id = ?", userID, folderID).Delete(&model.AssetLibraryItem{})
	return res.RowsAffected, res.Error
}

// Delete 删除文件夹本身，不存在或不属于该用户返回 ErrNotFound。
func (r *AssetFolderRepository) Delete(ctx context.Context, userID, id uint64) error {
	res := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&model.AssetFolder{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// LibraryFolderScope 是库条目列表的文件夹范围。
type LibraryFolderScope int

// 文件夹范围取值。
const (
	LibraryScopeRoot   LibraryFolderScope = iota // 根目录（folder_id 为空）
	LibraryScopeFolder                           // 指定文件夹
	LibraryScopeAll                              // 全部（跨文件夹平铺）
)

// LibraryItemFilter 是库条目列表的过滤条件。
type LibraryItemFilter struct {
	Scope    LibraryFolderScope // 文件夹范围
	FolderID uint64             // Scope 为 LibraryScopeFolder 时的文件夹
	Kind     string             // 素材类型，空不过滤
	Q        string             // 文件名包含匹配（忽略大小写），空不过滤
	After    *TimeCursor        // 上一页最后一条的位置，nil 从头开始
	Limit    int                // 最多返回多少条
}

// AssetLibraryRepository 资产库归属表（asset_library_items）的数据访问。
type AssetLibraryRepository struct {
	db *gorm.DB
}

// NewAssetLibraryRepository 创建资产库归属仓储。
func NewAssetLibraryRepository(db *gorm.DB) *AssetLibraryRepository {
	return &AssetLibraryRepository{db: db}
}

// Create 插入归属行，成功后 it.ID 已回填；该用户已有这个素材的归属返回 ErrDuplicate。
func (r *AssetLibraryRepository) Create(ctx context.Context, it *model.AssetLibraryItem) error {
	if err := r.db.WithContext(ctx).Create(it).Error; err != nil {
		if isUniqueViolation(err) {
			return ErrDuplicate
		}
		return err
	}
	return nil
}

// GetByID 按 id + user_id 查询归属，不存在或不属于该用户返回 ErrNotFound。
func (r *AssetLibraryRepository) GetByID(ctx context.Context, userID, id uint64) (*model.AssetLibraryItem, error) {
	var it model.AssetLibraryItem
	if err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&it).Error; err != nil {
		return nil, translate(err)
	}
	return &it, nil
}

// GetByAssetID 按 user_id + asset_id 查询归属，不存在返回 ErrNotFound。
func (r *AssetLibraryRepository) GetByAssetID(ctx context.Context, userID, assetID uint64) (*model.AssetLibraryItem, error) {
	var it model.AssetLibraryItem
	if err := r.db.WithContext(ctx).Where("user_id = ? AND asset_id = ?", userID, assetID).First(&it).Error; err != nil {
		return nil, translate(err)
	}
	return &it, nil
}

// ListByAssetIDs 一次查询返回该用户这些素材的归属；不在库里的素材直接缺席，assetIDs 为空返回 nil。
func (r *AssetLibraryRepository) ListByAssetIDs(ctx context.Context, userID uint64, assetIDs []uint64) ([]model.AssetLibraryItem, error) {
	if len(assetIDs) == 0 {
		return nil, nil
	}
	var rows []model.AssetLibraryItem
	if err := r.db.WithContext(ctx).Where("user_id = ? AND asset_id IN ?", userID, assetIDs).Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// SetFolder 把归属移到 folderID（nil 表示根目录），不存在或不属于该用户返回 ErrNotFound。
func (r *AssetLibraryRepository) SetFolder(ctx context.Context, userID, id uint64, folderID *uint64) error {
	res := r.db.WithContext(ctx).Model(&model.AssetLibraryItem{}).Where("id = ? AND user_id = ?", id, userID).Update("folder_id", folderID)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete 移出资产库（只删归属行，不动 assets），不存在或不属于该用户返回 ErrNotFound。
func (r *AssetLibraryRepository) Delete(ctx context.Context, userID, id uint64) error {
	res := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&model.AssetLibraryItem{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// List 按加入时间倒序（同一时刻按 id 倒序）返回该用户的库条目，最多 f.Limit 条。
// 类型与文件名过滤要看素材，所以联 assets（只取归属行的列，素材由调用方再按 id 批量取）。
func (r *AssetLibraryRepository) List(ctx context.Context, userID uint64, f LibraryItemFilter) ([]model.AssetLibraryItem, error) {
	db := r.db.WithContext(ctx).Table("asset_library_items AS i").
		Joins("JOIN assets a ON a.id = i.asset_id").
		Select("i.*").Where("i.user_id = ?", userID)
	switch f.Scope {
	case LibraryScopeRoot:
		db = db.Where("i.folder_id IS NULL")
	case LibraryScopeFolder:
		db = db.Where("i.folder_id = ?", f.FolderID)
	case LibraryScopeAll:
		// 不限文件夹
	}
	if f.Kind != "" {
		db = db.Where("a.kind = ?", f.Kind)
	}
	if f.Q != "" {
		db = db.Where("a.file_name ILIKE ?", "%"+escapeLike(f.Q)+"%")
	}
	if f.After != nil {
		db = db.Where("(i.created_at, i.id) < (?, ?)", f.After.At, f.After.ID)
	}
	var rows []model.AssetLibraryItem
	if err := db.Order("i.created_at DESC, i.id DESC").Limit(f.Limit).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}
