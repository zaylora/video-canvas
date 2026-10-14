package initialize

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"video-canvas/internal/config"
	"video-canvas/internal/migration"
	"video-canvas/internal/pkg/logger"
	"video-canvas/pkg/version"
)

// migrateTimeout 是启动时执行数据库迁移的总时限，包含等待其他实例释放迁移锁的时间。
const migrateTimeout = 15 * time.Minute

// NewDB 按配置连接 PostgreSQL，设置连接池，并按需执行数据库迁移（backend/migrations）。
// 迁移失败返回错误，调用方应中止启动：不能让新代码跑在没迁移完的库上。
func NewDB(cfg config.Database) (*gorm.DB, error) {
	db, err := OpenDB(cfg)
	if err != nil {
		return nil, err
	}
	if cfg.AutoMigrate {
		ctx, cancel := context.WithTimeout(context.Background(), migrateTimeout)
		defer cancel()
		if err := migration.Run(ctx, db, MigrationOptions(cfg)); err != nil {
			return nil, fmt.Errorf("数据库迁移失败: %w", err)
		}
	}
	return db, nil
}

// MigrationOptions 由数据库配置得到迁移参数。
func MigrationOptions(cfg config.Database) migration.Options {
	return migration.Options{
		AppVersion:       version.Version,
		AllowNewerSchema: cfg.AllowNewerSchema,
		Logger:           logger.L(),
	}
}

// OpenDB 按配置连接 PostgreSQL 并设置连接池，不执行迁移。
func OpenDB(cfg config.Database) (*gorm.DB, error) {
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
