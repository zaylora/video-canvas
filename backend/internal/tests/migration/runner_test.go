package migration_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"video-canvas/internal/migration"
)

// baselineSQL 故意不写 IF NOT EXISTS：老库路径如果误执行了基线，会因为表已存在而失败。
const baselineSQL = `CREATE TABLE users (id BIGSERIAL PRIMARY KEY, name TEXT);
CREATE TABLE notes (id BIGSERIAL PRIMARY KEY, body TEXT);`

func files(m map[string]string) fstest.MapFS {
	fsys := fstest.MapFS{}
	for name, body := range m {
		fsys[name] = &fstest.MapFile{Data: []byte(body)}
	}
	return fsys
}

func opts(fsys fstest.MapFS) migration.Options {
	return migration.Options{
		FS:         fsys,
		AppVersion: "9.9.9",
		LockWait:   20 * time.Second,
		Logger:     testLogger(),
		Legacy:     func(*gorm.DB, *zap.Logger) error { return errors.New("全新库不应走老库补齐") },
	}
}

func run(t *testing.T, db *gorm.DB, o migration.Options) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	return migration.Run(ctx, db, o)
}

func TestRunFreshDatabase(t *testing.T) {
	db := connect(t, newSchema(t))
	fsys := files(map[string]string{
		"0001_baseline.sql":  baselineSQL,
		"0002_add_age.sql":   "ALTER TABLE users ADD COLUMN age INT;\nALTER TABLE notes ADD COLUMN pinned BOOLEAN NOT NULL DEFAULT false;",
		"0003_add_email.sql": "ALTER TABLE users ADD COLUMN email TEXT",
	})
	if err := run(t, db, opts(fsys)); err != nil {
		t.Fatalf("迁移失败：%v", err)
	}

	if !hasColumn(t, db, "users", "age") || !hasColumn(t, db, "notes", "pinned") || !hasColumn(t, db, "users", "email") {
		t.Fatal("多语句文件里的每条语句都应执行")
	}
	if got := applied(t, db); strings.Join(got, ",") != "0001_baseline.sql,0002_add_age.sql,0003_add_email.sql" {
		t.Fatalf("迁移记录不对：%v", got)
	}
	var version string
	db.Raw("SELECT app_version FROM schema_migrations WHERE filename = '0002_add_age.sql'").Scan(&version)
	if version != "9.9.9" {
		t.Fatalf("应记录执行迁移的程序版本，得到 %q", version)
	}

	t.Run("再次启动什么都不执行", func(t *testing.T) {
		if err := run(t, db, opts(fsys)); err != nil {
			t.Fatalf("重复执行不应报错：%v", err)
		}
		if got := applied(t, db); len(got) != 3 {
			t.Fatalf("记录数应保持 3 条，得到 %v", got)
		}
	})

	t.Run("新版本只执行新增的文件", func(t *testing.T) {
		fsys["0004_add_notes_title.sql"] = &fstest.MapFile{Data: []byte("ALTER TABLE notes ADD COLUMN title TEXT")}
		if err := run(t, db, opts(fsys)); err != nil {
			t.Fatalf("迁移失败：%v", err)
		}
		if !hasColumn(t, db, "notes", "title") || len(applied(t, db)) != 4 {
			t.Fatal("新增的 0004 应执行并记录")
		}
	})
}

func TestRunRejectsModifiedMigration(t *testing.T) {
	db := connect(t, newSchema(t))
	fsys := files(map[string]string{"0001_baseline.sql": baselineSQL, "0002_add_age.sql": "ALTER TABLE users ADD COLUMN age INT"})
	if err := run(t, db, opts(fsys)); err != nil {
		t.Fatal(err)
	}
	fsys["0002_add_age.sql"] = &fstest.MapFile{Data: []byte("ALTER TABLE users ADD COLUMN age BIGINT")}
	err := run(t, db, opts(fsys))
	if err == nil || !strings.Contains(err.Error(), "0002_add_age.sql") {
		t.Fatalf("已执行的文件被改过应报错并指出文件名，得到 %v", err)
	}
}

func TestRunRejectsNewerSchema(t *testing.T) {
	db := connect(t, newSchema(t))
	newer := files(map[string]string{"0001_baseline.sql": baselineSQL, "0002_add_age.sql": "ALTER TABLE users ADD COLUMN age INT"})
	if err := run(t, db, opts(newer)); err != nil {
		t.Fatal(err)
	}
	older := files(map[string]string{"0001_baseline.sql": baselineSQL})

	err := run(t, db, opts(older))
	if err == nil || !strings.Contains(err.Error(), "0002_add_age.sql") || !strings.Contains(err.Error(), "9.9.9") {
		t.Fatalf("旧程序遇到更新的库应拒绝启动，并指出文件和执行它的版本，得到 %v", err)
	}

	o := opts(older)
	o.AllowNewerSchema = true
	if err := run(t, db, o); err != nil {
		t.Fatalf("显式允许后应放行：%v", err)
	}
}

func TestRunFailedMigrationLeavesNoTrace(t *testing.T) {
	db := connect(t, newSchema(t))
	fsys := files(map[string]string{
		"0001_baseline.sql": baselineSQL,
		"0002_broken.sql":   "ALTER TABLE users ADD COLUMN age INT;\nSELECT no_such_function();",
		"0003_after.sql":    "ALTER TABLE users ADD COLUMN email TEXT",
	})
	err := run(t, db, opts(fsys))
	if err == nil || !strings.Contains(err.Error(), "0002_broken.sql") {
		t.Fatalf("应报出失败的文件名，得到 %v", err)
	}
	if hasColumn(t, db, "users", "age") {
		t.Fatal("失败文件里已执行的语句应随事务回滚")
	}
	if hasColumn(t, db, "users", "email") {
		t.Fatal("失败之后的文件不应继续执行")
	}
	if got := applied(t, db); strings.Join(got, ",") != "0001_baseline.sql" {
		t.Fatalf("只有成功的文件留下记录，得到 %v", got)
	}
}

func TestRunNoTransactionMigration(t *testing.T) {
	db := connect(t, newSchema(t))
	fsys := files(map[string]string{
		"0001_baseline.sql":            baselineSQL,
		"0002_idx_users_name.notx.sql": "CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_users_name ON users (name)",
	})
	if err := run(t, db, opts(fsys)); err != nil {
		t.Fatalf("CONCURRENTLY 索引应在事务外执行成功：%v", err)
	}
	var n int64
	db.Raw("SELECT count(*) FROM pg_indexes WHERE schemaname = current_schema() AND indexname = 'idx_users_name'").Scan(&n)
	if n != 1 || len(applied(t, db)) != 2 {
		t.Fatal("索引应建好并记录")
	}
}

func TestRunRejectsBadFileName(t *testing.T) {
	db := connect(t, newSchema(t))
	for _, name := range []string{"2_short.sql", "0002-dash.sql", "0002_Upper.sql", "0002_x.tx.sql"} {
		fsys := files(map[string]string{"0001_baseline.sql": baselineSQL, name: "SELECT 1"})
		if err := run(t, db, opts(fsys)); err == nil || !strings.Contains(err.Error(), name) {
			t.Fatalf("文件名 %s 不合规应报错，得到 %v", name, err)
		}
	}
}

func TestRunUpgradesLegacyDatabase(t *testing.T) {
	db := connect(t, newSchema(t))
	// 接入迁移器之前的老库：表是 AutoMigrate 建的，没有 schema_migrations，还缺新版本的列
	if err := db.Exec("CREATE TABLE users (id BIGSERIAL PRIMARY KEY, legacy_flag BOOLEAN)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO users (legacy_flag) VALUES (true)").Error; err != nil {
		t.Fatal(err)
	}

	legacyCalls := 0
	o := opts(files(map[string]string{"0001_baseline.sql": baselineSQL, "0002_add_age.sql": "ALTER TABLE users ADD COLUMN age INT"}))
	o.Legacy = func(tx *gorm.DB, _ *zap.Logger) error {
		legacyCalls++
		// 补齐到基线：删掉老字段，建出基线里有的表与列
		return tx.Exec("ALTER TABLE users DROP COLUMN legacy_flag, ADD COLUMN name TEXT; CREATE TABLE notes (id BIGSERIAL PRIMARY KEY, body TEXT)").Error
	}
	if err := run(t, db, o); err != nil {
		t.Fatalf("老库升级失败：%v", err)
	}
	if legacyCalls != 1 {
		t.Fatalf("老库补齐应执行一次，实际 %d 次", legacyCalls)
	}
	if hasColumn(t, db, "users", "legacy_flag") || !hasColumn(t, db, "users", "age") || !hasTable(t, db, "notes") {
		t.Fatal("老库应先补齐到基线，再执行 0002")
	}
	var n int64
	db.Raw("SELECT count(*) FROM users").Scan(&n)
	if n != 1 {
		t.Fatal("老库的数据应保留")
	}
	if got := applied(t, db); strings.Join(got, ",") != "0001_baseline.sql,0002_add_age.sql" {
		t.Fatalf("基线应记为已执行，得到 %v", got)
	}

	if err := run(t, db, o); err != nil || legacyCalls != 1 {
		t.Fatalf("接入后再启动不应再走老库补齐：err=%v calls=%d", err, legacyCalls)
	}
}

func TestRunConcurrentInstances(t *testing.T) {
	schema := newSchema(t)
	// 0002 执行两次会因为表已存在而失败，借此证明只有一个实例真正执行
	fsys := files(map[string]string{"0001_baseline.sql": baselineSQL, "0002_once.sql": "CREATE TABLE once_only (id INT); SELECT pg_sleep(0.3);"})

	var wg sync.WaitGroup
	errs := make([]error, 3)
	for i := range errs {
		db := connect(t, schema)
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = run(t, db, opts(fsys))
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("实例 %d 迁移失败：%v", i, err)
		}
	}
	if got := applied(t, connect(t, schema)); len(got) != 2 {
		t.Fatalf("记录应只有 2 条，得到 %v", got)
	}
}

func TestStatus(t *testing.T) {
	db := connect(t, newSchema(t))
	fsys := files(map[string]string{"0001_baseline.sql": baselineSQL, "0002_add_age.sql": "ALTER TABLE users ADD COLUMN age INT"})
	if err := run(t, db, opts(files(map[string]string{"0001_baseline.sql": baselineSQL}))); err != nil {
		t.Fatal(err)
	}

	st, err := migration.Status(context.Background(), db, fsys)
	if err != nil {
		t.Fatal(err)
	}
	if len(st) != 2 || !st[0].Applied || st[0].AppVersion != "9.9.9" || st[1].Applied || st[1].Filename != "0002_add_age.sql" {
		t.Fatalf("状态不对：%+v", st)
	}
}

func TestStatusOnEmptyDatabase(t *testing.T) {
	db := connect(t, newSchema(t))
	st, err := migration.Status(context.Background(), db, files(map[string]string{"0001_baseline.sql": baselineSQL}))
	if err != nil {
		t.Fatalf("还没有 schema_migrations 时也应能查看状态：%v", err)
	}
	if len(st) != 1 || st[0].Applied {
		t.Fatalf("状态不对：%+v", st)
	}
	if hasTable(t, db, "schema_migrations") {
		t.Fatal("查看状态不应改动数据库")
	}
}
