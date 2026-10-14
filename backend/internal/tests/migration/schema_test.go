package migration_test

import (
	"context"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"video-canvas/internal/migration"
	"video-canvas/internal/migration/legacymodel"
	"video-canvas/internal/model"
	"video-canvas/migrations"
)

// autoMigrateOnly 是 GORM 标签表达不了、只在 SQL 迁移里存在的结构，结构一致性比较时跳过。
// 每一项都要写明原因。
var autoMigrateOnly = map[string]bool{
	// worker 扫描非终态任务用的部分索引，GORM 的 where 标签写不了 IN 列表
	"index idx_task_poll": true,
}

// TestMigrationFileNames 检查内嵌迁移文件的命名：符合规则、从 0001 开始连续编号、不重复。
func TestMigrationFileNames(t *testing.T) {
	names, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	if len(names) == 0 || names[0] != migration.BaselineFile {
		t.Fatalf("第一个迁移必须是 %s，得到 %v", migration.BaselineFile, names)
	}
	sort.Strings(names)
	pattern := regexp.MustCompile(`^([0-9]{4})_[a-z0-9_]+(\.notx)?\.sql$`)
	for i, name := range names {
		m := pattern.FindStringSubmatch(name)
		if m == nil {
			t.Fatalf("文件名 %s 不合规：应为 NNNN_小写下划线描述.sql 或 .notx.sql", name)
		}
		if n, _ := strconv.Atoi(m[1]); n != i+1 {
			t.Fatalf("迁移编号应从 0001 起连续：第 %d 个文件是 %s", i+1, name)
		}
	}
}

func runReal(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := migration.Run(ctx, db, migration.Options{AppVersion: "test", Logger: testLogger()}); err != nil {
		t.Fatalf("执行迁移失败：%v", err)
	}
}

// TestMigrationsMatchModels 保证模型与 SQL 迁移不会各走各的：
// 一个 schema 只跑 SQL 迁移，另一个只跑 AutoMigrate(model.All())，两边的列、索引、约束必须一致。
// 失败说明改了 internal/model 却没新增迁移文件（或反过来）。
func TestMigrationsMatchModels(t *testing.T) {
	sqlSchema := newSchema(t)
	sqlDB := connect(t, sqlSchema)
	runReal(t, sqlDB)

	gormSchema := newSchema(t)
	gormDB := connect(t, gormSchema)
	if err := gormDB.AutoMigrate(model.All()...); err != nil {
		t.Fatal(err)
	}

	left := schemaSnapshot(t, sqlDB, sqlSchema)
	delete(left, "index schema_migrations_pkey")
	delete(left, "constraint schema_migrations.schema_migrations_pkey")
	for k := range left {
		if strings.HasPrefix(k, "column schema_migrations.") {
			delete(left, k)
		}
	}
	if diff := diffSnapshots(left, schemaSnapshot(t, gormDB, gormSchema), autoMigrateOnly); len(diff) > 0 {
		t.Fatalf("SQL 迁移（左）与 internal/model（右）的结构不一致，改模型时要同时新增迁移文件：\n%s", strings.Join(diff, "\n"))
	}
}

// TestLegacyDatabaseUpgrade 模拟接入迁移器之前的老库（比 v0.3.2 更早，还留着已经删掉的旧结构），
// 升级后结构必须和全新库完全一致，数据保留，基线只记录不执行。
func TestLegacyDatabaseUpgrade(t *testing.T) {
	oldSchema := newSchema(t)
	old := connect(t, oldSchema)
	if err := old.AutoMigrate(legacymodel.All()...); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		// 更早版本留下的结构：旧迁移链会删掉它们
		`ALTER TABLE ai_channels ADD COLUMN trusted_internal boolean NOT NULL DEFAULT false`,
		`CREATE TABLE ai_providers (key varchar(64) PRIMARY KEY)`,
		`INSERT INTO users (username, password, role, status, created_at, updated_at) VALUES ('old-user', 'x', 'user', 'active', now(), now())`,
	} {
		if err := old.Exec(stmt).Error; err != nil {
			t.Fatalf("准备老库失败（%s）：%v", stmt, err)
		}
	}
	runReal(t, old)

	freshSchema := newSchema(t)
	fresh := connect(t, freshSchema)
	runReal(t, fresh)

	if diff := diffSnapshots(schemaSnapshot(t, old, oldSchema), schemaSnapshot(t, fresh, freshSchema), nil); len(diff) > 0 {
		t.Fatalf("老库升级后（左）与全新库（右）的结构不一致：\n%s", strings.Join(diff, "\n"))
	}
	var n int64
	old.Raw("SELECT count(*) FROM users WHERE username = 'old-user'").Scan(&n)
	if n != 1 {
		t.Fatal("老库的数据应保留")
	}
	if got := applied(t, old); len(got) == 0 || got[0] != migration.BaselineFile {
		t.Fatalf("老库应记录基线：%v", got)
	}
	runReal(t, old) // 接入后再次启动，什么都不做
}
