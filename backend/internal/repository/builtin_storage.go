package repository

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
)

// builtinStorageName 是内置本地磁盘存储的显示名。
const builtinStorageName = "本地磁盘"

// EnsureBuiltinStorage 幂等地确保内置本地磁盘存储存在，返回它的 id：
//  1. 没有内置存储就创建一条（配置来自 YAML，这里只占一行，让它能像其他存储一样被选为默认、被素材引用）；
//  2. 全表没有默认存储时，把它设为默认（已有别的默认就不抢）；
//  3. 把升级前留下的素材（storage_id = 0）回填到它。
//
// 应在 AutoMigrate 之后调用；多个实例同时启动也安全（名称唯一索引保证只有一条内置存储）。
func EnsureBuiltinStorage(ctx context.Context, db *gorm.DB) (uint64, error) {
	var id uint64
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var s model.StorageConfig
		err := tx.Where("builtin").First(&s).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			s = model.StorageConfig{Name: builtinStorageName, Provider: "local", Builtin: true, Addressing: "auto", UseSSL: true, SignedTTLSec: 3600, Version: 1}
			// 并发启动时另一个实例可能刚建好：冲突就忽略，再读一次
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&s).Error; err != nil {
				return err
			}
			err = tx.Where("builtin").First(&s).Error
		}
		if err != nil {
			return err
		}
		id = s.ID

		var defaults int64
		if err := tx.Model(&model.StorageConfig{}).Where("is_default").Count(&defaults).Error; err != nil {
			return err
		}
		if defaults == 0 {
			if err := tx.Model(&model.StorageConfig{}).Where("id = ?", id).UpdateColumn("is_default", true).Error; err != nil {
				return err
			}
		}
		return tx.Model(&model.Asset{}).Where("storage_id = 0").UpdateColumn("storage_id", id).Error
	})
	if err != nil {
		return 0, fmt.Errorf("初始化内置存储失败：%w", err)
	}
	return id, nil
}
