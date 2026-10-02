package repository

import (
	"fmt"

	"gorm.io/gorm"

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
