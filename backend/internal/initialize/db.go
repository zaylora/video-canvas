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
		if err := db.AutoMigrate(model.All()...); err != nil {
			return nil, fmt.Errorf("auto migrate: %w", err)
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
