package repository_test

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

var schemaSeq atomic.Int64

// isolatedDB 连接专用测试库，为本用例创建独立 schema（search_path 指向它）并迁移 models，用例结束后整体删除。
// 独立 schema 让“全表扫描 / 全表计数”类的查询不会被其它用例（或其它包并行跑的测试）的数据干扰，
// 也避开测试库 public schema 里可能残留的旧版表结构。未设置 TEST_DATABASE_DSN 时跳过。
func isolatedDB(t *testing.T, models ...any) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("未设置 TEST_DATABASE_DSN，跳过仓储集成测试")
	}
	quiet := &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)}
	admin, err := gorm.Open(postgres.Open(dsn), quiet)
	if err != nil {
		t.Fatalf("连接测试库失败：%v", err)
	}
	schema := fmt.Sprintf("rt_%d_%d", time.Now().UnixNano()%1_000_000_000, schemaSeq.Add(1))
	if err := admin.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatalf("创建 schema 失败：%v", err)
	}
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schema), quiet)
	if err != nil {
		t.Fatalf("连接测试 schema 失败：%v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(20)
	t.Cleanup(func() {
		_ = sqlDB.Close()
		_ = admin.Exec("DROP SCHEMA " + schema + " CASCADE").Error
		if a, err := admin.DB(); err == nil {
			_ = a.Close()
		}
	})
	if len(models) > 0 {
		if err := db.AutoMigrate(models...); err != nil {
			t.Fatalf("迁移失败：%v", err)
		}
	}
	return db
}

// jsonEqual 按 JSON 语义（忽略空白与 jsonb 的键重排）比较库里读出的值与期望文本。
func jsonEqual(got []byte, want string) bool {
	var g, w any
	if json.Unmarshal(got, &g) != nil || json.Unmarshal([]byte(want), &w) != nil {
		return false
	}
	return reflect.DeepEqual(g, w)
}
