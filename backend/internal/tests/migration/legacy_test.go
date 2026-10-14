package migration_test

import (
	"testing"

	"gorm.io/gorm"

	. "video-canvas/internal/migration"
	"video-canvas/internal/migration/legacymodel"
)

// legacySchemaDB 在独立 schema 里手工建出旧版（平台协议配置）的表结构并塞入遗留数据：
// ai_providers 表、带 NOT NULL provider_key 列的 ai_models、target=provider 的 revision、旧凭证。
func legacySchemaDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := isolatedDB(t, &legacymodel.AIConfigRevision{}, &legacymodel.AISecret{})
	stmts := []string{
		`CREATE TABLE ai_providers (key varchar(64) PRIMARY KEY, name varchar(128), published_revision_id bigint)`,
		`CREATE TABLE ai_models (key varchar(128) PRIMARY KEY, kind varchar(16) NOT NULL, provider_key varchar(64) NOT NULL,
			enabled boolean NOT NULL DEFAULT false, sort bigint NOT NULL DEFAULT 100, published_revision_id bigint, updated_at timestamptz)`,
		`INSERT INTO ai_providers (key, name) VALUES ('old-p', '旧平台')`,
		`INSERT INTO ai_models (key, kind, provider_key) VALUES ('old-m', 'video', 'old-p')`,
		`INSERT INTO ai_config_revisions (target, target_key, revision_no, body_json, status, created_at)
			VALUES ('provider', 'old-p', 1, '{}', 'published', now()), ('model', 'old-m', 1, '{}', 'published', now())`,
		`INSERT INTO ai_secrets (name, ciphertext, nonce, key_version, updated_at)
			VALUES ('old-p', 'c', 'n', 1, now()), ('channel:new', 'c', 'n', 1, now())`,
	}
	for _, s := range stmts {
		if err := db.Exec(s).Error; err != nil {
			t.Fatalf("准备旧版数据失败（%s）：%v", s, err)
		}
	}
	return db
}

func TestMigrateLegacyAIConfig(t *testing.T) {
	t.Run("清理旧表、旧列与平台 revision，且可重复执行", func(t *testing.T) {
		db := legacySchemaDB(t)
		for i := range 2 {
			if err := MigrateLegacyAIConfig(db); err != nil {
				t.Fatalf("第 %d 次执行失败：%v", i+1, err)
			}
		}
		if db.Migrator().HasTable("ai_providers") {
			t.Fatal("ai_providers 应已删除")
		}
		if db.Migrator().HasColumn("ai_models", "provider_key") {
			t.Fatal("ai_models.provider_key 应已删除")
		}
		var provRevs, modelRevs int64
		db.Model(&legacymodel.AIConfigRevision{}).Where("target = ?", "provider").Count(&provRevs)
		db.Model(&legacymodel.AIConfigRevision{}).Where("target = ?", "model").Count(&modelRevs)
		if provRevs != 0 || modelRevs != 1 {
			t.Fatalf("应只删除平台 revision：provider=%d model=%d", provRevs, modelRevs)
		}
		var m legacymodel.AIModel
		if err := db.First(&m, "key = ?", "old-m").Error; err != nil || m.Kind != "video" {
			t.Fatalf("模型指针行应保留：%+v %v", m, err)
		}
	})

	t.Run("旧凭证保留不动", func(t *testing.T) {
		db := legacySchemaDB(t)
		if err := MigrateLegacyAIConfig(db); err != nil {
			t.Fatal(err)
		}
		var list []legacymodel.AISecret
		err := db.Order("name").Find(&list).Error
		if err != nil || len(list) != 2 || list[0].Name != "channel:new" || list[1].Name != "old-p" {
			t.Fatalf("凭证应全部保留：%+v %v", list, err)
		}
	})

	t.Run("清理后 AutoMigrate 可正常建表，模型可无 provider_key 写入", func(t *testing.T) {
		db := legacySchemaDB(t)
		if err := MigrateLegacyAIConfig(db); err != nil {
			t.Fatal(err)
		}
		if err := db.AutoMigrate(legacymodel.All()...); err != nil {
			t.Fatalf("AutoMigrate 失败：%v", err)
		}
		if err := db.Create(&legacymodel.AIModel{Key: "new-m", Kind: "video"}).Error; err != nil {
			t.Fatalf("清理后保存模型失败：%v", err)
		}
	})

	t.Run("全新空库上执行不报错", func(t *testing.T) {
		db := isolatedDB(t)
		if err := MigrateLegacyAIConfig(db); err != nil {
			t.Fatalf("空库执行失败：%v", err)
		}
		if err := MigrateLegacyAIConfig(db); err != nil {
			t.Fatalf("空库第二次执行失败：%v", err)
		}
	})

	t.Run("已是新结构的库上执行不改动数据", func(t *testing.T) {
		db := isolatedDB(t, &legacymodel.AIModel{}, &legacymodel.AIConfigRevision{}, &legacymodel.AISecret{})
		seedModel(t, db, "keep-m")
		if err := MigrateLegacyAIConfig(db); err != nil {
			t.Fatal(err)
		}
		var n int64
		db.Model(&legacymodel.AIConfigRevision{}).Where("target = 'model' AND target_key = ?", "keep-m").Count(&n)
		if n != 1 {
			t.Fatalf("模型 revision 应保留：%d", n)
		}
	})
}

// softDeleteSchemaDB 模拟模型软删除方案留下的库：ai_models 带 deleted_at 列与索引，
// soft-a 已软删除（2 个 revision），live-b 未删除（1 个 revision）。
func softDeleteSchemaDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := isolatedDB(t, &legacymodel.AIModel{}, &legacymodel.AIConfigRevision{}, &legacymodel.AISecret{})
	stmts := []string{
		`ALTER TABLE ai_models ADD COLUMN deleted_at timestamptz`,
		`CREATE INDEX idx_ai_models_deleted_at ON ai_models (deleted_at)`,
		`INSERT INTO ai_models (key, kind, enabled, sort, deleted_at) VALUES ('soft-a', 'video', false, 1, now()), ('live-b', 'video', false, 2, NULL)`,
		`INSERT INTO ai_config_revisions (target, target_key, revision_no, body_json, status, created_at)
			VALUES ('model', 'soft-a', 1, '{}', 'archived', now()), ('model', 'soft-a', 2, '{}', 'draft', now()),
			       ('model', 'live-b', 1, '{}', 'draft', now())`,
	}
	for _, s := range stmts {
		if err := db.Exec(s).Error; err != nil {
			t.Fatalf("准备软删除遗留数据失败（%s）：%v", s, err)
		}
	}
	return db
}

func TestMigrateLegacyAIConfig_DropModelSoftDelete(t *testing.T) {
	t.Run("删掉已软删除的模型及其 revision，再删掉 deleted_at 列与索引，可重复执行", func(t *testing.T) {
		db := softDeleteSchemaDB(t)
		for i := range 2 {
			if err := MigrateLegacyAIConfig(db); err != nil {
				t.Fatalf("第 %d 次执行失败：%v", i+1, err)
			}
		}
		if db.Migrator().HasColumn("ai_models", "deleted_at") {
			t.Fatal("deleted_at 列应已删除")
		}
		if db.Migrator().HasIndex("ai_models", "idx_ai_models_deleted_at") {
			t.Fatal("deleted_at 的索引应已删除")
		}
		var list []legacymodel.AIModel
		err := db.Find(&list).Error
		if err != nil || len(list) != 1 || list[0].Key != "live-b" {
			t.Fatalf("只应保留未删除的模型：%+v %v", list, err)
		}
		var softN, liveN int64
		db.Model(&legacymodel.AIConfigRevision{}).Where("target_key = ?", "soft-a").Count(&softN)
		db.Model(&legacymodel.AIConfigRevision{}).Where("target_key = ?", "live-b").Count(&liveN)
		if softN != 0 {
			t.Fatalf("已软删除模型的 revision 应一并删除：%d", softN)
		}
		if liveN != 1 {
			t.Fatalf("未删除模型的 revision 应保留：%d", liveN)
		}
	})

	t.Run("清理后同名 key 可以重新新建，revision_no 从 1 开始", func(t *testing.T) {
		db := softDeleteSchemaDB(t)
		if err := MigrateLegacyAIConfig(db); err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&legacymodel.AIModel{Key: "soft-a", Kind: "video"}).Error; err != nil {
			t.Fatalf("同名 key 应能重新新建：%v", err)
		}
		var n int64
		db.Model(&legacymodel.AIConfigRevision{}).Where("target_key = ?", "soft-a").Count(&n)
		if n != 0 {
			t.Fatalf("旧 revision 应已清空，新建后 revision_no 才能从 1 开始：%d", n)
		}
	})
}

// seedModel 写入一个已发布的模型：指针行 + 一个 revision，指针指向它。
func seedModel(t *testing.T, db *gorm.DB, key string) {
	t.Helper()
	rev := legacymodel.AIConfigRevision{Target: legacymodel.ConfigTargetModel, TargetKey: key, RevisionNo: 1, BodyJSON: []byte(`{}`), Status: legacymodel.RevisionPublished}
	if err := db.Create(&rev).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&legacymodel.AIModel{Key: key, Kind: "video", PublishedRevisionID: &rev.ID}).Error; err != nil {
		t.Fatal(err)
	}
}
