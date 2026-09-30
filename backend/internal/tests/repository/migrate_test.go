package repository_test

import (
	"context"
	"testing"

	"gorm.io/gorm"

	"video-canvas/internal/model"
	. "video-canvas/internal/repository"
)

// legacySchemaDB 在独立 schema 里手工建出旧版（平台协议配置）的表结构并塞入遗留数据：
// ai_providers 表、带 NOT NULL provider_key 列的 ai_models、target=provider 的 revision、旧凭证。
func legacySchemaDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := isolatedDB(t, &model.AIConfigRevision{}, &model.AISecret{})
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
	ctx := context.Background()

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
		db.Model(&model.AIConfigRevision{}).Where("target = ?", "provider").Count(&provRevs)
		db.Model(&model.AIConfigRevision{}).Where("target = ?", "model").Count(&modelRevs)
		if provRevs != 0 || modelRevs != 1 {
			t.Fatalf("应只删除平台 revision：provider=%d model=%d", provRevs, modelRevs)
		}
		var m model.AIModel
		if err := db.First(&m, "key = ?", "old-m").Error; err != nil || m.Kind != "video" {
			t.Fatalf("模型指针行应保留：%+v %v", m, err)
		}
	})

	t.Run("旧凭证保留不动", func(t *testing.T) {
		db := legacySchemaDB(t)
		if err := MigrateLegacyAIConfig(db); err != nil {
			t.Fatal(err)
		}
		list, err := NewAIConfigRepository(db).ListSecrets(ctx)
		if err != nil || len(list) != 2 || list[0].Name != "channel:new" || list[1].Name != "old-p" {
			t.Fatalf("凭证应全部保留：%+v %v", list, err)
		}
	})

	t.Run("清理后 AutoMigrate 可正常建表，模型可无 provider_key 写入", func(t *testing.T) {
		db := legacySchemaDB(t)
		if err := MigrateLegacyAIConfig(db); err != nil {
			t.Fatal(err)
		}
		if err := db.AutoMigrate(model.All()...); err != nil {
			t.Fatalf("AutoMigrate 失败：%v", err)
		}
		r := NewAIConfigRepository(db)
		_, err := r.SaveDraft(ctx, SaveDraftInput{
			Pointer: ConfigPointer{Target: model.ConfigTargetModel, Key: "new-m", Kind: "video"}, Body: []byte(`{}`),
		})
		if err != nil {
			t.Fatalf("清理后保存模型草稿失败：%v", err)
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
		db := isolatedDB(t, &model.AIModel{}, &model.AIConfigRevision{}, &model.AISecret{})
		r := NewAIConfigRepository(db)
		rev := aicRepoSaveModel(t, r, "keep-m", `{}`)
		if err := MigrateLegacyAIConfig(db); err != nil {
			t.Fatal(err)
		}
		if got, err := r.GetRevision(ctx, rev.ID); err != nil || got.TargetKey != "keep-m" {
			t.Fatalf("模型 revision 应保留：%+v %v", got, err)
		}
	})
}
