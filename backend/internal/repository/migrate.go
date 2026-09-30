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

// MigrateLegacyAIConfig 幂等地清理旧版平台协议配置遗留的库结构与数据：
// 删除 ai_providers 表、删除 ai_models.provider_key 列、删除 ai_config_revisions 里 target='provider' 的版本。
// 应在 AutoMigrate 之前调用，全部语句在一个事务里执行；重复执行、或全新空库上执行都不会报错。
// 表不存在时（全新库）跳过 revision 清理，由随后的 AutoMigrate 建表。
func MigrateLegacyAIConfig(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		for _, stmt := range legacyAIConfigSQL {
			if err := tx.Exec(stmt).Error; err != nil {
				return fmt.Errorf("执行旧版清理语句失败（%s）：%w", stmt, err)
			}
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
