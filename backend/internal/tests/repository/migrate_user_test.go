package repository_test

import (
	"strings"
	"testing"

	"gorm.io/gorm"

	"video-canvas/internal/model"
	. "video-canvas/internal/repository"
)

// legacyLedgerDB 建出升级前的 credit_ledger（task_id NOT NULL + 全量唯一索引）与 users 表，并塞入一条旧流水。
func legacyLedgerDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := isolatedDB(t)
	stmts := []string{
		`CREATE TABLE credit_ledger (id bigserial PRIMARY KEY, user_id bigint NOT NULL, task_id bigint NOT NULL,
			type varchar(16) NOT NULL, amount bigint NOT NULL, created_at timestamptz)`,
		`CREATE INDEX idx_credit_ledger_user_id ON credit_ledger (user_id)`,
		`CREATE UNIQUE INDEX uk_ledger_task_type ON credit_ledger (task_id, type)`,
		`INSERT INTO credit_ledger (user_id, task_id, type, amount, created_at) VALUES (1, 100, 'freeze', 5, now())`,
	}
	for _, s := range stmts {
		if err := db.Exec(s).Error; err != nil {
			t.Fatalf("准备旧版流水表失败（%s）：%v", s, err)
		}
	}
	return db
}

// migrateAll 按启动顺序执行用户管理迁移 + AutoMigrate。
func migrateAll(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := MigrateUserManagement(db); err != nil {
		t.Fatalf("MigrateUserManagement 失败：%v", err)
	}
	if err := db.AutoMigrate(model.All()...); err != nil {
		t.Fatalf("AutoMigrate 失败：%v", err)
	}
}

func TestMigrateUserManagement(t *testing.T) {
	t.Run("旧流水表升级：task_id 可空、唯一索引变部分索引、旧数据保留，且可重复执行", func(t *testing.T) {
		db := legacyLedgerDB(t)
		for range 2 {
			migrateAll(t, db)
		}
		// 管理员调整 / 初始积分流水没有 task_id：同类型可以有多条
		for range 2 {
			err := db.Create(&model.CreditLedger{UserID: 1, Type: model.LedgerAdminAdjust, Amount: 3, Note: "补偿"}).Error
			if err != nil {
				t.Fatalf("task_id 为空的流水应可重复写入：%v", err)
			}
		}
		// 任务流水仍然按 (task_id, type) 幂等
		res := db.Exec(`INSERT INTO credit_ledger (user_id, task_id, type, amount) VALUES (1, 100, 'freeze', 5) ON CONFLICT DO NOTHING`)
		if res.Error != nil || res.RowsAffected != 0 {
			t.Fatalf("同一任务同类型流水应被唯一索引挡住：%v affected=%d", res.Error, res.RowsAffected)
		}
		var n int64
		db.Model(&model.CreditLedger{}).Where("task_id = 100").Count(&n)
		if n != 1 {
			t.Fatalf("旧流水应保留：%d", n)
		}
		var def string
		db.Raw(`SELECT indexdef FROM pg_indexes WHERE schemaname = current_schema() AND indexname = 'uk_ledger_task_type'`).Scan(&def)
		if def == "" || !strings.Contains(def, "task_id IS NOT NULL") {
			t.Fatalf("唯一索引应为部分索引：%s", def)
		}
	})

	t.Run("全新库直接建表", func(t *testing.T) {
		db := isolatedDB(t)
		migrateAll(t, db)
		migrateAll(t, db)
		for _, tbl := range []string{"users", "credit_ledger", "user_login_logs", "admin_audit_logs", "system_settings", "smtp_settings"} {
			if !db.Migrator().HasTable(tbl) {
				t.Fatalf("缺少表 %s", tbl)
			}
		}
		for _, col := range []string{"status", "max_active_tasks", "email_verified_at", "last_login_at", "token_version"} {
			if !db.Migrator().HasColumn("users", col) {
				t.Fatalf("users 缺少列 %s", col)
			}
		}
		for _, col := range []string{"operator_id", "note"} {
			if !db.Migrator().HasColumn("credit_ledger", col) {
				t.Fatalf("credit_ledger 缺少列 %s", col)
			}
		}
	})

	t.Run("已有用户升级后默认 active，邮箱部分唯一索引放行空邮箱", func(t *testing.T) {
		db := isolatedDB(t)
		if err := db.Exec(`CREATE TABLE users (id bigserial PRIMARY KEY, created_at timestamptz, updated_at timestamptz, deleted_at timestamptz,
			username varchar(64) NOT NULL, password varchar(128) NOT NULL, nickname varchar(64), email varchar(128), role varchar(16) NOT NULL DEFAULT 'user')`).Error; err != nil {
			t.Fatal(err)
		}
		db.Exec(`CREATE UNIQUE INDEX idx_users_username ON users (username)`)
		db.Exec(`INSERT INTO users (username, password, email) VALUES ('a', 'x', ''), ('b', 'x', '')`)
		migrateAll(t, db)
		var u model.User
		if err := db.First(&u, "username = ?", "a").Error; err != nil || u.Status != model.UserStatusActive || u.TokenVersion != 0 {
			t.Fatalf("旧用户应为 active：%+v %v", u, err)
		}
		if err := db.Create(&model.User{Username: "c", Password: "x", Email: "x@y.com"}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.User{Username: "d", Password: "x", Email: "x@y.com"}).Error; err == nil {
			t.Fatal("相同邮箱应被唯一索引拒绝")
		}
	})
}
