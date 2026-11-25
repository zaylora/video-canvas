package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
)

// ErrStorageHybridLocked 表示混合策略暂时冻结了此操作。
var ErrStorageHybridLocked = errors.New("混合存储策略锁定")

const hybridPolicyLock int64 = 0x5643535448594252

// Hybrid 返回当前策略；首次使用前为关闭。
func (r *StorageConfigRepository) Hybrid(ctx context.Context) (model.StorageHybridPolicy, error) {
	return readHybrid(r.db.WithContext(ctx))
}

func readHybrid(db *gorm.DB) (model.StorageHybridPolicy, error) {
	var policy model.StorageHybridPolicy
	err := db.First(&policy, 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.StorageHybridPolicy{ID: 1, State: model.StorageHybridOff}, nil
	}
	return policy, err
}

func lockHybrid(tx *gorm.DB) (model.StorageHybridPolicy, error) {
	if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", hybridPolicyLock).Error; err != nil {
		return model.StorageHybridPolicy{}, err
	}
	return readHybrid(tx)
}

func guardHybrid(tx *gorm.DB, id uint64, defaultChange bool) error {
	policy, err := lockHybrid(tx)
	if err != nil {
		return err
	}
	if policy.State == model.StorageHybridActive || policy.State == model.StorageHybridPreparing {
		if defaultChange || policy.CloudID == id {
			return ErrStorageHybridLocked
		}
	}
	return nil
}

// guardHybridLocation 固定选中存储的定位与访问方式，正常编辑名称等设置仍可使用。
func guardHybridLocation(tx *gorm.DB, id uint64, fields map[string]any) error {
	policy, err := lockHybrid(tx)
	if err != nil {
		return err
	}
	if policy.CloudID != id || (policy.State != model.StorageHybridActive && policy.State != model.StorageHybridPreparing) {
		return nil
	}
	var row model.StorageConfig
	if err := tx.First(&row, id).Error; err != nil {
		return translate(err)
	}
	current := map[string]any{
		"account_id": row.AccountID, "endpoint": row.Endpoint, "region": row.Region,
		"bucket": row.Bucket, "path_prefix": row.PathPrefix, "addressing": row.Addressing,
		"use_ssl": row.UseSSL, "public_base_url": row.PublicBaseURL,
	}
	for name, value := range current {
		if next, ok := fields[name]; ok && next != value {
			return ErrStorageHybridLocked
		}
	}
	return nil
}

// BeginHybrid 记录一个待启用任务；同样限额的重复请求复用当前启用任务。
func (r *StorageConfigRepository) BeginHybrid(ctx context.Context, limit int64) (model.StorageHybridPolicy, error) {
	var policy model.StorageHybridPolicy
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		current, err := lockHybrid(tx)
		if err != nil {
			return err
		}
		if current.State == model.StorageHybridActive || current.State == model.StorageHybridPreparing {
			policy = current
			if current.LimitBytes != limit {
				return ErrStorageHybridLocked
			}
			return nil
		}
		policy = model.StorageHybridPolicy{ID: 1, State: model.StorageHybridPreparing, LimitBytes: limit}
		return tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&policy).Error
	})
	return policy, err
}

// DisableHybrid 释放配置锁，让原默认设置重新生效；不动文件或素材记录。
func (r *StorageConfigRepository) DisableHybrid(ctx context.Context) (model.StorageHybridPolicy, error) {
	off := model.StorageHybridPolicy{ID: 1, State: model.StorageHybridOff}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		current, err := lockHybrid(tx)
		if err != nil {
			return err
		}
		if current.State == model.StorageHybridPreparing {
			return ErrStorageHybridLocked
		}
		if current.State == model.StorageHybridOff {
			return nil
		}
		return tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&off).Error
	})
	return off, err
}

const hybridLease = 2 * time.Minute

// ClaimHybrid 认领待执行或上一个 worker 已失联的启用任务。
func (r *StorageConfigRepository) ClaimHybrid(ctx context.Context, token string, now time.Time) (*model.StorageHybridPolicy, error) {
	var policy model.StorageHybridPolicy
	res := r.db.WithContext(ctx).Model(&policy).Clauses(clause.Returning{}).
		Where("id = 1 AND state = ? AND lease_until <= ?", model.StorageHybridPreparing, now).
		Updates(map[string]any{"lease_token": token, "lease_until": now.Add(hybridLease)})
	if res.Error != nil || res.RowsAffected == 0 {
		return nil, res.Error
	}
	return &policy, nil
}

// RenewHybrid 延长当前认领；认领失效返回 ErrRevisionConflict。
func (r *StorageConfigRepository) RenewHybrid(ctx context.Context, token string, now time.Time) error {
	return updateClaimedHybrid(r.db.WithContext(ctx), token, map[string]any{"lease_until": now.Add(hybridLease)})
}

// PinHybrid 锁定已经过实测的配置版本，避免同步期间被改桶或删除。
func (r *StorageConfigRepository) PinHybrid(ctx context.Context, token string, cloudID, localID uint64, version int) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := lockHybrid(tx); err != nil {
			return err
		}
		var row model.StorageConfig
		if err := tx.Where("id = ? AND version = ?", cloudID, version).Take(&row).Error; err != nil {
			return translate(err)
		}
		return updateClaimedHybrid(tx, token, map[string]any{"cloud_id": cloudID, "local_id": localID})
	})
}

// FinishHybrid 发布成功状态或把检测与同步错误保存给管理端。
func (r *StorageConfigRepository) FinishHybrid(ctx context.Context, token, reason string) error {
	fields := map[string]any{"state": model.StorageHybridActive, "error": reason, "lease_token": ""}
	if reason != "" {
		fields["state"], fields["cloud_id"], fields["local_id"] = model.StorageHybridFailed, 0, 0
	}
	return updateClaimedHybrid(r.db.WithContext(ctx), token, fields)
}

// ReleaseHybrid 让关停中的 worker 立即交还未完成的启用任务。
func (r *StorageConfigRepository) ReleaseHybrid(ctx context.Context, token string) error {
	return updateClaimedHybrid(r.db.WithContext(ctx), token, map[string]any{"lease_until": time.Time{}, "lease_token": ""})
}

func updateClaimedHybrid(db *gorm.DB, token string, fields map[string]any) error {
	res := db.Model(&model.StorageHybridPolicy{}).
		Where("id = 1 AND state = ? AND lease_token = ?", model.StorageHybridPreparing, token).Updates(fields)
	if res.Error == nil && res.RowsAffected == 0 {
		return ErrRevisionConflict
	}
	return res.Error
}
