package repository_test

import (
	"testing"

	"video-canvas/internal/model"
	. "video-canvas/internal/repository"
)

// 旧库里 conversations 带 is_default 列和部分唯一索引：迁移后列与索引都没有，原来的「默认创作」变成普通对话。
func TestMigrateConversationDefault(t *testing.T) {
	t.Run("旧表升级：删掉列和索引、数据保留，且可重复执行", func(t *testing.T) {
		db := isolatedDB(t)
		stmts := []string{
			`CREATE TABLE conversations (id bigserial PRIMARY KEY, created_at timestamptz, updated_at timestamptz, deleted_at timestamptz,
				user_id bigint NOT NULL, title varchar(50) NOT NULL DEFAULT '', is_default boolean NOT NULL DEFAULT false,
				record_count bigint NOT NULL DEFAULT 0, last_record_at timestamptz)`,
			`CREATE UNIQUE INDEX uk_conversations_default ON conversations (user_id) WHERE is_default AND deleted_at IS NULL`,
			`INSERT INTO conversations (user_id, title, is_default) VALUES (1, '默认创作', true), (1, '雨夜霓虹', false)`,
		}
		for _, s := range stmts {
			if err := db.Exec(s).Error; err != nil {
				t.Fatalf("准备旧版对话表失败（%s）：%v", s, err)
			}
		}

		for range 2 {
			if err := MigrateConversationDefault(db); err != nil {
				t.Fatalf("MigrateConversationDefault 失败：%v", err)
			}
			if err := db.AutoMigrate(&model.Conversation{}); err != nil {
				t.Fatalf("AutoMigrate 失败：%v", err)
			}
		}

		if db.Migrator().HasColumn("conversations", "is_default") {
			t.Error("is_default 列应已删除")
		}
		var idx int64
		db.Raw(`SELECT count(*) FROM pg_indexes WHERE schemaname = current_schema() AND indexname = 'uk_conversations_default'`).Scan(&idx)
		if idx != 0 {
			t.Error("uk_conversations_default 应已删除")
		}
		var titles []string
		db.Model(&model.Conversation{}).Where("user_id = 1").Order("id").Pluck("title", &titles)
		if len(titles) != 2 || titles[0] != "默认创作" {
			t.Errorf("原有对话应保留，原「默认创作」成为普通对话：%v", titles)
		}
		// 同一用户可以有任意多段同名对话，也都能删除
		for range 2 {
			if err := db.Create(&model.Conversation{UserID: 1, Title: "默认创作"}).Error; err != nil {
				t.Fatalf("不应再有每人一段的唯一限制：%v", err)
			}
		}
	})

	t.Run("全新库什么都不做", func(t *testing.T) {
		db := isolatedDB(t)
		for range 2 {
			if err := MigrateConversationDefault(db); err != nil {
				t.Fatalf("全新库应无报错：%v", err)
			}
			if err := db.AutoMigrate(&model.Conversation{}); err != nil {
				t.Fatal(err)
			}
		}
		if db.Migrator().HasColumn("conversations", "is_default") {
			t.Error("新库不应有 is_default 列")
		}
	})
}
