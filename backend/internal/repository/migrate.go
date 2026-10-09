package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"video-canvas/internal/model"
)

// legacyAIConfigSQL 是清理旧版（平台协议配置化）遗留的语句，全部幂等，顺序无关。
// ai_secrets 里不带 channel: 前缀的旧凭证故意不动：凭证是只写不读的，删掉就无法再找回。
var legacyAIConfigSQL = []string{
	// 平台指针表已被插件 + 渠道取代
	"DROP TABLE IF EXISTS ai_providers",
	// 模型不再绑定平台，改为通过渠道路由
	"ALTER TABLE IF EXISTS ai_models DROP COLUMN IF EXISTS provider_key",
}

// MigrateLegacyAIConfig 幂等地清理旧版遗留的库结构与数据：
// 删除 ai_providers 表、删除 ai_models.provider_key 列、删除 ai_config_revisions 里 target='provider' 的版本；
// 以及模型软删除方案的遗留（见 dropModelSoftDelete）。
// 应在 AutoMigrate 之前调用，全部语句在一个事务里执行；重复执行、或全新空库上执行都不会报错。
// 表不存在时（全新库）跳过 revision 清理，由随后的 AutoMigrate 建表。
func MigrateLegacyAIConfig(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		for _, stmt := range legacyAIConfigSQL {
			if err := tx.Exec(stmt).Error; err != nil {
				return fmt.Errorf("执行旧版清理语句失败（%s）：%w", stmt, err)
			}
		}
		if err := dropModelSoftDelete(tx); err != nil {
			return err
		}
		if !tx.Migrator().HasTable(&model.AIConfigRevision{}) {
			return nil
		}
		err := tx.Exec("DELETE FROM ai_config_revisions WHERE target = ?", "provider").Error
		if err != nil {
			return fmt.Errorf("清理旧版平台配置版本失败：%w", err)
		}
		return nil
	})
}

// dropModelSoftDelete 清理模型软删除方案的遗留（模型删除已改为硬删除）：ai_models 有 deleted_at 列时，
// 先把 deleted_at 非空的模型连同它们的全部 revision 删掉（否则去掉这列后它们会重新出现在列表里），再删掉这列与它的索引。
// 没有这列（全新库或已清理过）时什么都不做，所以可以重复执行。
func dropModelSoftDelete(tx *gorm.DB) error {
	if !tx.Migrator().HasColumn("ai_models", "deleted_at") {
		return nil
	}
	stmts := []string{
		"DELETE FROM ai_models WHERE deleted_at IS NOT NULL",
		"DROP INDEX IF EXISTS idx_ai_models_deleted_at",
		"ALTER TABLE ai_models DROP COLUMN IF EXISTS deleted_at",
	}
	if tx.Migrator().HasTable("ai_config_revisions") {
		stmts = append([]string{
			"DELETE FROM ai_config_revisions WHERE target = 'model' AND target_key IN " +
				"(SELECT key FROM ai_models WHERE deleted_at IS NOT NULL)",
		}, stmts...)
	}
	for _, stmt := range stmts {
		if err := tx.Exec(stmt).Error; err != nil {
			return fmt.Errorf("清理模型软删除遗留失败（%s）：%w", stmt, err)
		}
	}
	return nil
}

// MigrateConversationDefault 幂等地清理「默认创作」的遗留：conversations 有 is_default 列时，
// 先删掉只服务于它的部分唯一索引 uk_conversations_default，再删这一列。
// 原来的「默认创作」行保留，变成一段普通对话（标题仍叫「默认创作」，用户可以改名或删除）。
// 应在 AutoMigrate 之前调用；表或列不存在（全新库、已清理过）时什么都不做，可以重复执行。
func MigrateConversationDefault(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if !tx.Migrator().HasColumn("conversations", "is_default") {
			return nil
		}
		stmts := []string{
			"DROP INDEX IF EXISTS uk_conversations_default",
			"ALTER TABLE conversations DROP COLUMN IF EXISTS is_default",
		}
		for _, stmt := range stmts {
			if err := tx.Exec(stmt).Error; err != nil {
				return fmt.Errorf("清理默认创作遗留失败（%s）：%w", stmt, err)
			}
		}
		return nil
	})
}

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

// MigrateUserManagement 在 AutoMigrate 之前幂等地处理 AutoMigrate 做不了的积分流水表变更：
//  1. credit_ledger.task_id 改为可空（管理员调整与初始积分流水没有任务）；
//  2. 旧的全量唯一索引 uk_ledger_task_type 换成仅对 task_id IS NOT NULL 生效的部分唯一索引。
//
// 原因：GORM 的 AutoMigrate 看到同名索引就跳过，不会把旧的全量索引改成部分索引，所以要先删掉旧索引，让随后的 AutoMigrate 按新模型重建。
// 表不存在（全新库）时什么都不做，由 AutoMigrate 直接建出新结构；可重复执行。
func MigrateUserManagement(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if !tx.Migrator().HasTable("credit_ledger") {
			return nil
		}
		if err := tx.Exec("ALTER TABLE credit_ledger ALTER COLUMN task_id DROP NOT NULL").Error; err != nil {
			return fmt.Errorf("放开 credit_ledger.task_id 非空约束失败：%w", err)
		}
		// 只删非部分索引：已经是部分索引（indexdef 含 WHERE）说明迁移过了，保留即可
		var def string
		err := tx.Raw(`SELECT indexdef FROM pg_indexes WHERE schemaname = current_schema() AND tablename = 'credit_ledger' AND indexname = 'uk_ledger_task_type'`).
			Scan(&def).Error
		if err != nil {
			return fmt.Errorf("查询 credit_ledger 索引失败：%w", err)
		}
		if def != "" && !strings.Contains(strings.ToUpper(def), " WHERE ") {
			if err := tx.Exec("DROP INDEX uk_ledger_task_type").Error; err != nil {
				return fmt.Errorf("删除旧的流水唯一索引失败：%w", err)
			}
		}
		return nil
	})
}
