package repository

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
)

// StorageConfigRepository 存储配置表的数据访问。
type StorageConfigRepository struct {
	db *gorm.DB
}

// NewStorageConfigRepository 创建存储配置仓储。
func NewStorageConfigRepository(db *gorm.DB) *StorageConfigRepository {
	return &StorageConfigRepository{db: db}
}

// Create 插入一套存储，成功后 s.ID 已回填；名称重复返回 ErrDuplicate。
func (r *StorageConfigRepository) Create(ctx context.Context, s *model.StorageConfig) error {
	if err := r.db.WithContext(ctx).Create(s).Error; err != nil {
		if isUniqueViolation(err) {
			return ErrDuplicate
		}
		return err
	}
	return nil
}

// GetByID 按 ID 查询，不存在返回 ErrNotFound。
func (r *StorageConfigRepository) GetByID(ctx context.Context, id uint64) (*model.StorageConfig, error) {
	var s model.StorageConfig
	if err := r.db.WithContext(ctx).First(&s, id).Error; err != nil {
		return nil, translate(err)
	}
	return &s, nil
}

// GetDefault 返回默认存储，没有默认存储返回 ErrNotFound。
func (r *StorageConfigRepository) GetDefault(ctx context.Context) (*model.StorageConfig, error) {
	var s model.StorageConfig
	if err := r.db.WithContext(ctx).Where("is_default").First(&s).Error; err != nil {
		return nil, translate(err)
	}
	return &s, nil
}

// List 返回全部存储，按 ID 升序（内置的本地磁盘最先建，所以排在最前）。
func (r *StorageConfigRepository) List(ctx context.Context) ([]model.StorageConfig, error) {
	var rows []model.StorageConfig
	if err := r.db.WithContext(ctx).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// Update 带乐观锁更新：只有 version 与数据库一致才更新，并把 version +1。
// 记录不存在返回 ErrNotFound，版本不一致返回 ErrRevisionConflict，名称与其他存储重复返回 ErrDuplicate。
func (r *StorageConfigRepository) Update(ctx context.Context, id uint64, version int, fields map[string]any) error {
	fields["version"] = gorm.Expr("version + 1")
	res := r.db.WithContext(ctx).Model(&model.StorageConfig{}).
		Where("id = ? AND version = ?", id, version).Updates(fields)
	if res.Error != nil {
		if isUniqueViolation(res.Error) {
			return ErrDuplicate
		}
		return res.Error
	}
	if res.RowsAffected > 0 {
		return nil
	}
	// 没更新到行：区分“记录不存在”和“版本已被别人改过”
	var n int64
	if err := r.db.WithContext(ctx).Model(&model.StorageConfig{}).Where("id = ?", id).Count(&n).Error; err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return ErrRevisionConflict
}

// RecordCheck 记录最近一次测试连接的结果。不改 version：测试结果不是配置变更，不应让各实例的客户端缓存失效。
// 记录不存在返回 ErrNotFound。
func (r *StorageConfigRepository) RecordCheck(ctx context.Context, id uint64, ok bool, errMsg string, at time.Time) error {
	res := r.db.WithContext(ctx).Model(&model.StorageConfig{}).Where("id = ?", id).
		UpdateColumns(map[string]any{"last_check_at": at, "last_check_ok": ok, "last_check_error": errMsg})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// SetDefault 把一套存储设为默认：同一事务里先清掉旧的默认再设置新的，全表始终最多一条默认。
// 存储不存在返回 ErrNotFound，此时旧的默认保持不变。
func (r *StorageConfigRepository) SetDefault(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var s model.StorageConfig
		if err := tx.First(&s, id).Error; err != nil {
			return translate(err)
		}
		if err := tx.Model(&model.StorageConfig{}).Where("is_default AND id <> ?", id).
			UpdateColumn("is_default", false).Error; err != nil {
			return err
		}
		return tx.Model(&model.StorageConfig{}).Where("id = ?", id).UpdateColumn("is_default", true).Error
	})
}

// AssetCounts 返回每套存储被多少素材引用（没有素材的存储不在结果里）。
func (r *StorageConfigRepository) AssetCounts(ctx context.Context) (map[uint64]int64, error) {
	var rows []struct {
		StorageID uint64
		N         int64
	}
	if err := r.db.WithContext(ctx).Model(&model.Asset{}).
		Select("storage_id, COUNT(*) AS n").Group("storage_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[uint64]int64, len(rows))
	for _, row := range rows {
		out[row.StorageID] = row.N
	}
	return out, nil
}

// CountRefs 返回一套存储当前的引用数：素材数，以及 now 之前还没过期的未完成上传意图数。
func (r *StorageConfigRepository) CountRefs(ctx context.Context, id uint64, now time.Time) (assets, intents int64, err error) {
	return countStorageRefs(r.db.WithContext(ctx), id, now)
}

// Delete 删除一套存储及它在 ai_secrets 里的密钥。素材引用或未过期的上传意图仍指向它时返回 ErrInUse，不存在返回 ErrNotFound。
// 检查与删除在同一事务里，并先锁住存储行，避免检查通过之后又有新引用写入。
func (r *StorageConfigRepository) Delete(ctx context.Context, id uint64, now time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var s model.StorageConfig
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&s, id).Error; err != nil {
			return translate(err)
		}
		assets, intents, err := countStorageRefs(tx, id, now)
		if err != nil {
			return err
		}
		if assets > 0 || intents > 0 {
			return ErrInUse
		}
		// 密钥和配置一起删：密钥行只在 ai_secrets 里，留着就成了没人管的孤儿凭证
		if err := tx.Where("name = ?", model.StorageSecretName(id)).Delete(&model.AISecret{}).Error; err != nil {
			return err
		}
		return tx.Delete(&model.StorageConfig{}, id).Error
	})
}

func countStorageRefs(db *gorm.DB, id uint64, now time.Time) (assets, intents int64, err error) {
	if err = db.Model(&model.Asset{}).Where("storage_id = ?", id).Count(&assets).Error; err != nil {
		return 0, 0, err
	}
	err = db.Model(&model.AssetUploadIntent{}).
		Where("storage_id = ? AND completed_at IS NULL AND expires_at > ?", id, now).Count(&intents).Error
	return assets, intents, err
}

// BackfillAssets 把还没记录存储的旧素材（storage_id = 0）全部指向 id，返回更新的行数。
func (r *StorageConfigRepository) BackfillAssets(ctx context.Context, id uint64) (int64, error) {
	res := r.db.WithContext(ctx).Model(&model.Asset{}).Where("storage_id = 0").UpdateColumn("storage_id", id)
	return res.RowsAffected, res.Error
}
