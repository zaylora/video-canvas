package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
)

// ErrReferenceCacheCapacity 表示已登记对象与预留容量之和超过临时素材上限。
var ErrReferenceCacheCapacity = errors.New("临时参考素材容量不足")

// ReferenceCacheRepository 协调多 worker 的容量、任务保护与清理认领。
type ReferenceCacheRepository struct{ db *gorm.DB }

// NewReferenceCacheRepository 创建临时素材仓储。
func NewReferenceCacheRepository(db *gorm.DB) *ReferenceCacheRepository {
	return &ReferenceCacheRepository{db: db}
}

const referenceCacheLockID int64 = 0x5643524546434143

// withTx 对所有写入同一个数据库的实例串行化容量与租用决策；事务内没有对象存储请求。
func (r *ReferenceCacheRepository) withTx(ctx context.Context, fn func(*gorm.DB) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", referenceCacheLockID).Error; err != nil {
			return err
		}
		return fn(tx)
	})
}

// Reserve 复用仍有寿命的对象，或者先检查总容量再创建一条预留，同时登记任务保护。
func (r *ReferenceCacheRepository) Reserve(ctx context.Context, req model.ReferenceCacheReservation) (model.ReferenceCacheObject, error) {
	var object model.ReferenceCacheObject
	err := r.withTx(ctx, func(tx *gorm.DB) error {
		in := req.Object
		err := tx.Where("namespace = ? AND identity = ? AND size = ? AND digest = ? AND deleting = FALSE AND created_at > ?",
			in.Namespace, in.Identity, in.Size, in.Digest, req.ReuseAfter).
			Order("created_at DESC").Take(&object).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			object = in
			if err := reserveReferenceBytes(tx, &object, req.MaxBytes); err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			if err := tx.Model(&object).Updates(map[string]any{
				"last_used_at": gorm.Expr("GREATEST(last_used_at, ?)", in.LastUsedAt),
				"busy_until":   gorm.Expr("GREATEST(busy_until, ?)", in.BusyUntil),
			}).Error; err != nil {
				return err
			}
		}
		return referenceCacheUse(tx, object.Key, req.TaskID, in.BusyUntil)
	})
	return object, err
}

func reserveReferenceBytes(tx *gorm.DB, object *model.ReferenceCacheObject, limit int64) error {
	if limit == 0 {
		return tx.Create(object).Error
	}
	used, err := referenceCacheBytes(tx)
	if err != nil {
		return err
	}
	if object.Size > limit-used {
		return ErrReferenceCacheCapacity
	}
	return tx.Create(object).Error
}

func referenceCacheUse(tx *gorm.DB, key string, taskID uint64, until time.Time) error {
	use := model.ReferenceCacheUse{ObjectKey: key, TaskID: taskID, ProtectUntil: until}
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "object_key"}, {Name: "task_id"}},
		DoUpdates: clause.Assignments(map[string]any{"protect_until": gorm.Expr("GREATEST(reference_cache_uses.protect_until, EXCLUDED.protect_until)")}),
	}).Create(&use).Error
}

// MarkReady 在 HEAD 复核后确认容量并保护已签发给任务的 URL。
func (r *ReferenceCacheRepository) MarkReady(ctx context.Context, key string, taskID uint64, until time.Time) error {
	return r.withTx(ctx, func(tx *gorm.DB) error {
		res := tx.Model(&model.ReferenceCacheObject{}).
			Where("key = ? AND deleting = FALSE", key).Update("ready", true)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return referenceCacheUse(tx, key, taskID, until)
	})
}

// ClaimCleanup 认领一条可回收记录。已认领的 key 永远不再用于新请求，删除失败后可再次认领。
func (r *ReferenceCacheRepository) ClaimCleanup(ctx context.Context, req model.ReferenceCacheSweep) (*model.ReferenceCacheObject, error) {
	var object model.ReferenceCacheObject
	err := r.withTx(ctx, func(tx *gorm.DB) error {
		err := cleanupReferenceCandidates(tx, req).Order("last_used_at, key").Take(&object).Error
		if err != nil {
			return err
		}
		return tx.Model(&object).Updates(map[string]any{"deleting": true, "busy_until": req.ClaimUntil}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("认领临时对象失败: %w", err)
	}
	return &object, nil
}

func cleanupReferenceCandidates(tx *gorm.DB, req model.ReferenceCacheSweep) *gorm.DB {
	return tx.Model(&model.ReferenceCacheObject{}).
		Where("namespace = ? AND busy_until <= ?", req.Namespace, req.Now).
		Where("(deleting OR NOT ready OR last_used_at <= ?)", req.UnusedSince).
		Where(
			"NOT EXISTS (SELECT 1 FROM reference_cache_uses AS u LEFT JOIN generation_tasks AS t ON t.id = u.task_id"+
				" WHERE u.object_key = reference_cache_objects.key AND (u.protect_until > ? OR t.status IN ? OR"+
				" ((t.error_code = 'submit_unknown' OR t.status IN ?) AND t.updated_at > ?)))",
			req.Now, model.ActiveTaskStatuses, []string{model.TaskCanceled, model.TaskExpired}, req.UncertainSince,
		)
}

// TrackOrphan 补记前缀枚举发现的未入账对象。已有对象即使有活跃任务也保持原记录。
func (r *ReferenceCacheRepository) TrackOrphan(ctx context.Context, object model.ReferenceCacheObject) error {
	var key string
	err := r.db.WithContext(ctx).Model(&model.ReferenceCacheObject{}).Select("key").Where("key = ?", object.Key).Take(&key).Error
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return r.withTx(ctx, func(tx *gorm.DB) error {
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&object).Error
	})
}

// FinishCleanup 在确认 R2 不再有该对象后释放额度和任务关联。
func (r *ReferenceCacheRepository) FinishCleanup(ctx context.Context, key string) error {
	return r.withTx(ctx, func(tx *gorm.DB) error {
		deleted := tx.Where("key = ? AND deleting", key).Delete(&model.ReferenceCacheObject{})
		if deleted.Error != nil {
			return deleted.Error
		}
		if deleted.RowsAffected == 0 {
			return nil
		}
		return tx.Where("object_key = ?", key).Delete(&model.ReferenceCacheUse{}).Error
	})
}

// Bytes 返回临时对象与上传预留占用的总字节。
func (r *ReferenceCacheRepository) Bytes(ctx context.Context) (int64, error) {
	return referenceCacheBytes(r.db.WithContext(ctx))
}

func referenceCacheBytes(db *gorm.DB) (int64, error) {
	var out int64
	err := db.Model(&model.ReferenceCacheObject{}).Select("COALESCE(SUM(size), 0)").Scan(&out).Error
	return out, err
}
