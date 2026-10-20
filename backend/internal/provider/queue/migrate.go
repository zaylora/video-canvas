package queue

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/riverqueue/river/riverdriver/riverdatabasesql"
	"github.com/riverqueue/river/rivermigrate"
)

// Migrate 把 River 自己的表（river_job、river_queue、river_leader 等）迁移到最新版本。
// 项目自己的迁移（migrations/*.sql）不包含它们：River 的表结构由 River 版本决定，升级 River 时由它的迁移器负责。
// 必须在项目迁移之后、启动队列之前调用；已经是最新版本时什么都不做。schema 为空表示用连接的默认 schema。
func Migrate(ctx context.Context, db *sql.DB, schema string) error {
	m, err := rivermigrate.New(riverdatabasesql.New(db), &rivermigrate.Config{Schema: schema})
	if err != nil {
		return fmt.Errorf("创建 River 迁移器失败：%w", err)
	}
	if _, err := m.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return fmt.Errorf("迁移 River 表失败：%w", err)
	}
	return nil
}
