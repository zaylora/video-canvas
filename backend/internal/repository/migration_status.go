package repository

import (
	"context"

	"gorm.io/gorm"

	"video-canvas/internal/migration"
	"video-canvas/internal/model"
)

// MigrationStatusRepository 读取数据库迁移的执行状态（只读）。
type MigrationStatusRepository struct {
	db *gorm.DB
}

// NewMigrationStatusRepository 创建迁移状态仓储。
func NewMigrationStatusRepository(db *gorm.DB) *MigrationStatusRepository {
	return &MigrationStatusRepository{db: db}
}

// MigrationStatus 按文件名顺序返回内嵌迁移文件的执行状态，库里有、程序里没有的记录排在最后。
func (r *MigrationStatusRepository) MigrationStatus(ctx context.Context) ([]model.MigrationFileView, error) {
	files, err := migration.Status(ctx, r.db, nil)
	if err != nil {
		return nil, err
	}
	out := make([]model.MigrationFileView, 0, len(files))
	for _, f := range files {
		v := model.MigrationFileView{
			Filename: f.Filename, Applied: f.Applied, AppVersion: f.AppVersion,
			DurationMs: f.DurationMs, Modified: f.Modified, Unknown: f.Unknown,
		}
		if f.Applied {
			at := f.AppliedAt
			v.AppliedAt = &at
		}
		out = append(out, v)
	}
	return out, nil
}
