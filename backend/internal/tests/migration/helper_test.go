package migration_test

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

var schemaSeq atomic.Int64

// testDSN 返回测试库连接串；未设置 TEST_DATABASE_DSN 时跳过用例。
func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("未设置 TEST_DATABASE_DSN，跳过迁移集成测试")
	}
	return dsn
}

// newSchema 在测试库里建一个独立的空 schema，用例结束时删除，返回 schema 名。
func newSchema(t *testing.T) string {
	t.Helper()
	dsn := testDSN(t)
	admin := openDB(t, dsn)
	schema := fmt.Sprintf("mg_%d_%d", time.Now().UnixNano()%1_000_000_000, schemaSeq.Add(1))
	if err := admin.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatalf("创建 schema 失败：%v", err)
	}
	t.Cleanup(func() {
		_ = admin.Exec("DROP SCHEMA " + schema + " CASCADE").Error
	})
	return schema
}

// connect 打开一个 search_path 指向 schema 的连接池；同一 schema 可以开多个，模拟多个实例。
func connect(t *testing.T, schema string) *gorm.DB {
	t.Helper()
	return openDB(t, testDSN(t)+" search_path="+schema)
}

// isolatedDB 建一个独立 schema 并连上它，按需 AutoMigrate 给定的模型（老库补齐测试里用冻结模型模拟老库）。
func isolatedDB(t *testing.T, models ...any) *gorm.DB {
	t.Helper()
	db := connect(t, newSchema(t))
	if len(models) > 0 {
		if err := db.AutoMigrate(models...); err != nil {
			t.Fatalf("迁移失败：%v", err)
		}
	}
	return db
}

func openDB(t *testing.T, dsn string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		t.Fatalf("连接测试库失败：%v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(10)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func testLogger() *zap.Logger { return zap.NewNop() }

// applied 返回 schema_migrations 里的文件名，按名字排序。
func applied(t *testing.T, db *gorm.DB) []string {
	t.Helper()
	var names []string
	if err := db.Raw("SELECT filename FROM schema_migrations ORDER BY filename").Scan(&names).Error; err != nil {
		t.Fatalf("读取迁移记录失败：%v", err)
	}
	return names
}

func hasColumn(t *testing.T, db *gorm.DB, table, column string) bool {
	t.Helper()
	var n int64
	err := db.Raw(`SELECT count(*) FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = ? AND column_name = ?`, table, column).Scan(&n).Error
	if err != nil {
		t.Fatal(err)
	}
	return n > 0
}

func hasTable(t *testing.T, db *gorm.DB, table string) bool {
	t.Helper()
	var ok bool
	if err := db.Raw("SELECT to_regclass(?) IS NOT NULL", table).Scan(&ok).Error; err != nil {
		t.Fatal(err)
	}
	return ok
}

// schemaSnapshot 把当前 schema 的结构整理成可比较的文本行：列、索引、约束。
// 定义里出现的 schema 名会被抹掉，方便比较两个不同 schema。
func schemaSnapshot(t *testing.T, db *gorm.DB, schema string) map[string]string {
	t.Helper()
	snap := map[string]string{}
	type row struct{ K, V string }
	queries := []string{
		`SELECT 'column ' || table_name || '.' || column_name AS k,
			concat_ws(' | ', data_type, character_maximum_length::text, numeric_precision::text, numeric_scale::text, is_nullable, column_default) AS v
		FROM information_schema.columns WHERE table_schema = current_schema()`,
		`SELECT 'index ' || indexname AS k, indexdef AS v FROM pg_indexes WHERE schemaname = current_schema()`,
		`SELECT 'constraint ' || c.relname || '.' || con.conname AS k, con.contype::text || ' ' || pg_get_constraintdef(con.oid) AS v
		FROM pg_constraint con JOIN pg_class c ON c.oid = con.conrelid JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = current_schema()`,
	}
	for _, q := range queries {
		var rows []row
		if err := db.Raw(q).Scan(&rows).Error; err != nil {
			t.Fatalf("读取结构失败：%v", err)
		}
		for _, r := range rows {
			snap[r.K] = strings.ReplaceAll(r.V, schema+".", "")
		}
	}
	delete(snap, "")
	return snap
}

// diffSnapshots 返回两个快照的差异描述，没有差异时返回空切片。ignore 里的键不参与比较。
func diffSnapshots(a, b map[string]string, ignore map[string]bool) []string {
	var out []string
	for k, va := range a {
		if ignore[k] {
			continue
		}
		vb, ok := b[k]
		switch {
		case !ok:
			out = append(out, "只在左侧："+k+" = "+va)
		case va != vb:
			out = append(out, "不一致："+k+"\n    左："+va+"\n    右："+vb)
		}
	}
	for k, vb := range b {
		if ignore[k] {
			continue
		}
		if _, ok := a[k]; !ok {
			out = append(out, "只在右侧："+k+" = "+vb)
		}
	}
	sort.Strings(out)
	return out
}
