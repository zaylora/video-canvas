package initialize

import (
	"fmt"
	"log"
	"os"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"video-canvas/internal/config"
	"video-canvas/internal/model"
	"video-canvas/internal/repository"
)

// NewDB 按配置连接 PostgreSQL，设置连接池，并按需自动迁移表结构。
func NewDB(cfg config.Database) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(cfg.DSN), &gorm.Config{
		Logger: gormlogger.New(log.New(os.Stdout, "\r\n", log.LstdFlags), gormlogger.Config{
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  parseGormLogLevel(cfg.LogLevel),
			IgnoreRecordNotFoundError: true, // 查不到记录是正常业务分支，不当作错误打印
			Colorful:                  true,
		}),
	})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	if cfg.AutoMigrate {
		// 先清旧版遗留的结构（ai_providers、ai_models.provider_key、模型软删除的 deleted_at 及已软删除的模型），幂等；
		// 不清的话旧库上新建模型会因 NOT NULL 报 500，已软删除的模型也会在去掉 deleted_at 字段后重新出现
		if err := repository.MigrateLegacyAIConfig(db); err != nil {
			return nil, fmt.Errorf("migrate legacy ai config: %w", err)
		}
		// 再处理积分流水表的变更（task_id 可空、唯一索引改部分索引）：AutoMigrate 做不了，必须在它之前；幂等
		if err := repository.MigrateUserManagement(db); err != nil {
			return nil, fmt.Errorf("migrate user management: %w", err)
		}
		// 对话不再有「默认创作」：删掉 is_default 列和它的部分唯一索引；幂等
		if err := repository.MigrateConversationDefault(db); err != nil {
			return nil, fmt.Errorf("migrate conversation default: %w", err)
		}
		if err := db.AutoMigrate(model.All()...); err != nil {
			return nil, fmt.Errorf("auto migrate: %w", err)
		}
		// worker 每秒扫描非终态任务：只给非终态建部分索引，终态任务（绝大多数）不占索引空间。
		// GORM 的 where 标签不方便写 IN 列表，所以在迁移后补建。
		if err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_task_poll ON generation_tasks (next_poll_at) ` +
			`WHERE status IN ('pending','queued','running','finalizing')`).Error; err != nil {
			return nil, fmt.Errorf("create idx_task_poll: %w", err)
		}
	}
	return db, nil
}

func parseGormLogLevel(level string) gormlogger.LogLevel {
	switch level {
	case "silent":
		return gormlogger.Silent
	case "error":
		return gormlogger.Error
	case "info":
		return gormlogger.Info
	default:
		return gormlogger.Warn
	}
}
