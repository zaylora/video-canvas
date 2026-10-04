package repository

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
)

// ImageProcessorRepository 图片处理服务（image_processors / image_processor_versions）的数据访问。
type ImageProcessorRepository struct {
	db *gorm.DB
}

// NewImageProcessorRepository 创建图片处理服务仓储。
func NewImageProcessorRepository(db *gorm.DB) *ImageProcessorRepository {
	return &ImageProcessorRepository{db: db}
}

// PublishParams 是发布一个处理服务所需的参数。
type PublishParams struct {
	ID          uint64         // 要发布的处理服务
	Version     int            // 调用方读到的乐观锁版本
	TrialResult model.JSONText // 发布前的校验与试跑结果，写进发布历史
	By          uint64         // 发布人
	At          time.Time      // 发布时间
}

// Create 插入一个处理服务，成功后 p.ID 已回填；名称重复返回 ErrDuplicate。
func (r *ImageProcessorRepository) Create(ctx context.Context, p *model.ImageProcessor) error {
	if err := r.db.WithContext(ctx).Create(p).Error; err != nil {
		if isUniqueViolation(err) {
			return ErrDuplicate
		}
		return err
	}
	return nil
}

// GetByID 按 ID 查询，不存在返回 ErrNotFound。
func (r *ImageProcessorRepository) GetByID(ctx context.Context, id uint64) (*model.ImageProcessor, error) {
	var p model.ImageProcessor
	if err := r.db.WithContext(ctx).First(&p, id).Error; err != nil {
		return nil, translate(err)
	}
	return &p, nil
}

// List 返回全部处理服务，按 ID 升序。
func (r *ImageProcessorRepository) List(ctx context.Context) ([]model.ImageProcessor, error) {
	var rows []model.ImageProcessor
	if err := r.db.WithContext(ctx).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// GetPublishedByStorage 返回绑定该存储的已发布处理服务，没有返回 ErrNotFound。
func (r *ImageProcessorRepository) GetPublishedByStorage(ctx context.Context, storageID uint64) (*model.ImageProcessor, error) {
	var p model.ImageProcessor
	if err := r.db.WithContext(ctx).Where("storage_id = ? AND status = ?", storageID, model.ProcessorPublished).First(&p).Error; err != nil {
		return nil, translate(err)
	}
	return &p, nil
}

// Update 带乐观锁更新：只有 version 与数据库一致才更新，并把 version +1。
// 记录不存在返回 ErrNotFound，版本不一致返回 ErrRevisionConflict，名称与其他处理服务重复返回 ErrDuplicate。
func (r *ImageProcessorRepository) Update(ctx context.Context, id uint64, version int, fields map[string]any) error {
	fields["version"] = gorm.Expr("version + 1")
	res := r.db.WithContext(ctx).Model(&model.ImageProcessor{}).
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
	if err := r.db.WithContext(ctx).Model(&model.ImageProcessor{}).Where("id = ?", id).Count(&n).Error; err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return ErrRevisionConflict
}

// SetCheck 记录最近一次校验与试跑的结果，不改 version。处理服务不存在返回 ErrNotFound。
func (r *ImageProcessorRepository) SetCheck(ctx context.Context, id uint64, result model.JSONText) error {
	res := r.db.WithContext(ctx).Model(&model.ImageProcessor{}).Where("id = ?", id).Update("check_result", result)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// Publish 在一个事务里发布处理服务：锁行并核对 version（不一致返回 ErrRevisionConflict）；
// 把同一存储上已发布的其他处理服务置为已停用；在发布历史里新增一行（版本号 = 历史最大值 + 1，
// 回滚后再发布也不会复用旧版本号）；把当前配置作为线上配置。返回新的发布版本号，处理服务不存在返回 ErrNotFound。
func (r *ImageProcessorRepository) Publish(ctx context.Context, in PublishParams) (int, error) {
	var published int
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var p model.ImageProcessor
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&p, in.ID).Error; err != nil {
			return translate(err)
		}
		if p.Version != in.Version {
			return ErrRevisionConflict
		}
		// 先停用同存储上的旧服务，再发布自己，部分唯一索引才不会冲突
		if err := tx.Model(&model.ImageProcessor{}).
			Where("storage_id = ? AND status = ? AND id <> ?", p.StorageID, model.ProcessorPublished, p.ID).
			Update("status", model.ProcessorDisabled).Error; err != nil {
			return err
		}
		var maxVer int
		if err := tx.Model(&model.ImageProcessorVersion{}).Where("processor_id = ?", p.ID).
			Select("COALESCE(MAX(version), 0)").Scan(&maxVer).Error; err != nil {
			return err
		}
		published = maxVer + 1
		if err := tx.Create(&model.ImageProcessorVersion{
			ProcessorID: p.ID, Version: published, Config: p.Config, TrialResult: in.TrialResult, PublishedBy: in.By, PublishedAt: in.At,
		}).Error; err != nil {
			return err
		}
		return tx.Model(&model.ImageProcessor{}).Where("id = ?", p.ID).Updates(map[string]any{
			"status": model.ProcessorPublished, "published_config": p.Config, "published_version": published, "updated_by": in.By,
		}).Error
	})
	if err != nil {
		return 0, err
	}
	return published, nil
}

// Rollback 把已发布的处理服务回滚到更早的一个发布版本（比当前线上版本小的最大版本），返回回滚到的版本号。
// 处理服务不存在、或没有更早的版本都返回 ErrNotFound。
func (r *ImageProcessorRepository) Rollback(ctx context.Context, id uint64) (int, error) {
	var to int
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var p model.ImageProcessor
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&p, id).Error; err != nil {
			return translate(err)
		}
		var v model.ImageProcessorVersion
		if err := tx.Where("processor_id = ? AND version < ?", id, p.PublishedVersion).Order("version DESC").First(&v).Error; err != nil {
			return translate(err)
		}
		to = v.Version
		return tx.Model(&model.ImageProcessor{}).Where("id = ?", id).
			Updates(map[string]any{"published_config": v.Config, "published_version": v.Version}).Error
	})
	if err != nil {
		return 0, err
	}
	return to, nil
}

// Disable 停用已发布的处理服务；不存在或不是已发布状态返回 ErrNotFound。
func (r *ImageProcessorRepository) Disable(ctx context.Context, id uint64) error {
	res := r.db.WithContext(ctx).Model(&model.ImageProcessor{}).
		Where("id = ? AND status = ?", id, model.ProcessorPublished).Update("status", model.ProcessorDisabled)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete 删除处理服务与它的发布历史；不存在返回 ErrNotFound。
func (r *ImageProcessorRepository) Delete(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("processor_id = ?", id).Delete(&model.ImageProcessorVersion{}).Error; err != nil {
			return err
		}
		res := tx.Delete(&model.ImageProcessor{}, id)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// PreviousVersions 返回已发布处理服务各自“可回滚到的上一个版本号”（比线上版本小的最大版本）。
// ids 为空表示全部；没有更早版本的处理服务不在结果里。
func (r *ImageProcessorRepository) PreviousVersions(ctx context.Context, ids ...uint64) (map[uint64]int, error) {
	q := r.db.WithContext(ctx).Table("image_processors AS p").
		Select("p.id AS id, MAX(v.version) AS prev").
		Joins("JOIN image_processor_versions AS v ON v.processor_id = p.id AND v.version < p.published_version").
		Where("p.status = ?", model.ProcessorPublished).Group("p.id")
	if len(ids) > 0 {
		q = q.Where("p.id IN ?", ids)
	}
	var rows []struct {
		ID   uint64
		Prev int
	}
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[uint64]int, len(rows))
	for _, row := range rows {
		out[row.ID] = row.Prev
	}
	return out, nil
}

// SampleAsset 返回该存储里最新的一个指定种类（image / video）素材，用来试跑；没有返回 ErrNotFound。
func (r *ImageProcessorRepository) SampleAsset(ctx context.Context, storageID uint64, kind string) (*model.Asset, error) {
	var a model.Asset
	if err := r.db.WithContext(ctx).Where("storage_id = ? AND kind = ?", storageID, kind).Order("id DESC").First(&a).Error; err != nil {
		return nil, translate(err)
	}
	return &a, nil
}
