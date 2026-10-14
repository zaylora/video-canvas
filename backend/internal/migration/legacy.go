package migration

import (
	"fmt"
	"net/url"
	"strings"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"video-canvas/internal/migration/legacymodel"
)

// UpgradeLegacy 把接入版本化迁移之前、由 AutoMigrate 建出的老库（任意 ≤ v0.3.2 的版本）补齐到基线结构，
// 顺序与 v0.3.2 的启动流程完全一致。它和 legacymodel 一样是冻结的：只服务老库的首次接入，永久保留，不再修改。
// 每一步都幂等，失败后下次启动可以重来。
func UpgradeLegacy(db *gorm.DB, log *zap.Logger) error {
	// 先清旧版遗留的结构（ai_providers、ai_models.provider_key、模型软删除的 deleted_at 及已软删除的模型）
	if err := MigrateLegacyAIConfig(db); err != nil {
		return fmt.Errorf("migrate legacy ai config: %w", err)
	}
	// 积分流水表的变更（task_id 可空、唯一索引改部分索引）：AutoMigrate 做不了，必须在它之前
	if err := MigrateUserManagement(db); err != nil {
		return fmt.Errorf("migrate user management: %w", err)
	}
	// 对话不再有「默认创作」：删掉 is_default 列和它的部分唯一索引
	if err := MigrateConversationDefault(db); err != nil {
		return fmt.Errorf("migrate conversation default: %w", err)
	}
	// 渠道不再有“允许访问内网”开关：删 trusted_internal 列（对仍开着的渠道打 WARN）
	if err := MigrateChannelTrustedInternal(db, log); err != nil {
		return fmt.Errorf("migrate channel trusted internal: %w", err)
	}
	// 用冻结的 v0.3.2 模型补齐表、列和索引；不能用 model.All()，否则老库会被建成以后版本的结构
	if err := db.AutoMigrate(legacymodel.All()...); err != nil {
		return fmt.Errorf("auto migrate legacy models: %w", err)
	}
	// 模型不再有版本管理：把老库里的多版本收敛成每个模型一份配置（草稿优先），必须在 AutoMigrate 之后
	if err := MigrateModelSingleConfig(db); err != nil {
		return fmt.Errorf("migrate model single config: %w", err)
	}
	// worker 扫描非终态任务用的部分索引，GORM 标签表达不了
	if err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_task_poll ON generation_tasks (next_poll_at) ` +
		`WHERE status IN ('pending','queued','running','finalizing')`).Error; err != nil {
		return fmt.Errorf("create idx_task_poll: %w", err)
	}
	return nil
}

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
		if !tx.Migrator().HasTable(&legacymodel.AIConfigRevision{}) {
			return nil
		}
		err := tx.Exec("DELETE FROM ai_config_revisions WHERE target = ?", "provider").Error
		if err != nil {
			return fmt.Errorf("清理旧版平台配置版本失败：%w", err)
		}
		return nil
	})
}

// MigrateModelSingleConfig 幂等地把模型的多版本数据收敛成“每个模型一份配置”（模型不再有版本管理）：
// 有草稿的模型以最新草稿为准（运营最后改的内容），没有草稿就用已发布版本；其余历史版本全部删除，
// 留下的那一行记为 revision_no=1 / published，并让指针指向它。上下架与排序不动。
// 应在 AutoMigrate 之后调用；表不存在、或已经收敛过时什么都不做。
func MigrateModelSingleConfig(db *gorm.DB) error {
	if !db.Migrator().HasTable(&legacymodel.AIConfigRevision{}) || !db.Migrator().HasTable(&legacymodel.AIModel{}) {
		return nil
	}
	stmts := []string{
		// 1. 有草稿的模型：指针改指最新草稿
		`UPDATE ai_models m SET published_revision_id = d.id
			FROM (SELECT DISTINCT ON (target_key) id, target_key FROM ai_config_revisions
				WHERE target = 'model' AND status = 'draft' ORDER BY target_key, revision_no DESC) d
			WHERE m.key = d.target_key`,
		// 2. 指针没指到的 revision 全部删除
		`DELETE FROM ai_config_revisions r WHERE r.target = 'model'
			AND NOT EXISTS (SELECT 1 FROM ai_models m WHERE m.published_revision_id = r.id)`,
		// 3. 留下的统一成 published / 1
		`UPDATE ai_config_revisions SET status = 'published', revision_no = 1
			WHERE target = 'model' AND (status <> 'published' OR revision_no <> 1)`,
	}
	return db.Transaction(func(tx *gorm.DB) error {
		for _, stmt := range stmts {
			if err := tx.Exec(stmt).Error; err != nil {
				return fmt.Errorf("收敛模型配置版本失败（%s）：%w", stmt, err)
			}
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

// MigrateChannelTrustedInternal 幂等地删除 ai_channels.trusted_internal 列（“允许访问内网”开关已取消，所有渠道一律拦截内网）。
// 删列前对仍然开着开关的渠道各打一条 WARN（渠道 key 和 base_url 的主机放在字段里），提醒运维：
// 升级后这些渠道不能再访问内网，base_url 指向内网的需要换成公网可访问的地址。
// 应在 AutoMigrate 之前调用；表或列不存在（全新库、已清理过）时什么都不做，可以重复执行。
func MigrateChannelTrustedInternal(db *gorm.DB, log *zap.Logger) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if !tx.Migrator().HasColumn("ai_channels", "trusted_internal") {
			return nil
		}
		var rows []struct {
			Key     string
			BaseURL string
		}
		// 渠道的主键列就叫 key（legacymodel.AIChannel.Key），没有 id 列，按 key 排序保证日志顺序稳定
		if err := tx.Table("ai_channels").Select(`"key", base_url`).Where("trusted_internal").Order(`"key"`).Find(&rows).Error; err != nil {
			return fmt.Errorf("查询开着“允许访问内网”的渠道失败：%w", err)
		}
		for _, r := range rows {
			host := r.BaseURL
			if u, err := url.Parse(r.BaseURL); err == nil && u.Host != "" {
				host = u.Host
			}
			log.Warn("渠道开着“允许访问内网”，该开关已取消：升级后这个渠道不能再访问内网地址，base_url 指向内网的需要换成公网可访问的地址",
				zap.String("channel", r.Key), zap.String("base_url_host", host))
		}
		if err := tx.Exec("ALTER TABLE ai_channels DROP COLUMN IF EXISTS trusted_internal").Error; err != nil {
			return fmt.Errorf("删除 ai_channels.trusted_internal 失败：%w", err)
		}
		return nil
	})
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
