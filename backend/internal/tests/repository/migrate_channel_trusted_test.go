package repository_test

import (
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"video-canvas/internal/model"
	. "video-canvas/internal/repository"
)

// 旧库里 ai_channels 带 trusted_internal 列：迁移后列没有了，开着“允许访问内网”的渠道各打一条 WARN，
// 提醒运维升级后这些渠道不能再访问内网；可重复执行，全新库什么都不做。
func TestMigrateChannelTrustedInternal(t *testing.T) {
	t.Run("旧表升级：先对开着开关的渠道打 WARN，再删列，数据保留且可重复执行", func(t *testing.T) {
		db := isolatedDB(t)
		stmts := []string{
			// 旧表结构照真实的 model.AIChannel 写：主键是 key（varchar），没有 id 列
			`CREATE TABLE ai_channels (key varchar(64) PRIMARY KEY, name varchar(128) NOT NULL DEFAULT '', plugin_key varchar(30) NOT NULL DEFAULT '',
				plugin_version_id bigint NOT NULL DEFAULT 0, base_url varchar(512) NOT NULL DEFAULT '',
				trusted_internal boolean NOT NULL DEFAULT false, allow_credentials boolean NOT NULL DEFAULT false,
				enabled boolean NOT NULL DEFAULT true, created_at timestamptz, updated_at timestamptz)`,
			`INSERT INTO ai_channels (key, name, base_url, trusted_internal) VALUES
				('self-hosted', '自建网关', 'http://192.168.1.10:3000/v1', true),
				('public-gw', '公网网关', 'https://api.example.com', false),
				('also-internal', '另一个内网', 'http://newapi.corp.local', true)`,
		}
		for _, s := range stmts {
			if err := db.Exec(s).Error; err != nil {
				t.Fatalf("准备旧版渠道表失败（%s）：%v", s, err)
			}
		}

		core, logs := observer.New(zap.WarnLevel)
		for round := range 2 {
			if err := MigrateChannelTrustedInternal(db, zap.New(core)); err != nil {
				t.Fatalf("第 %d 次 MigrateChannelTrustedInternal 失败：%v", round+1, err)
			}
		}

		if db.Migrator().HasColumn("ai_channels", "trusted_internal") {
			t.Error("trusted_internal 列应已删除")
		}
		var keys []string
		db.Table("ai_channels").Order("key").Pluck("key", &keys)
		if len(keys) != 3 {
			t.Errorf("渠道行应全部保留：%v", keys)
		}

		warned := map[string]string{}
		for _, e := range logs.All() {
			ctx := e.ContextMap()
			key, _ := ctx["channel"].(string)
			host, _ := ctx["base_url_host"].(string)
			warned[key] = host
		}
		if len(warned) != 2 || warned["self-hosted"] != "192.168.1.10:3000" || warned["also-internal"] != "newapi.corp.local" {
			t.Errorf("应只对开着开关的两个渠道各打一条 WARN（第二次执行不再重复）：%v", warned)
		}
		if logs.Len() != 2 {
			t.Errorf("WARN 总数应为 2，实际 %d", logs.Len())
		}
		for _, e := range logs.All() {
			if !strings.Contains(e.Message, "内网") {
				t.Errorf("WARN 应说明升级后不能再访问内网：%q", e.Message)
			}
			if strings.Contains(e.Message, "192.168") {
				t.Errorf("消息正文不应带地址，地址只放在字段里：%q", e.Message)
			}
		}
	})

	t.Run("全新库什么都不做，之后 AutoMigrate 的新表也没有这一列", func(t *testing.T) {
		db := isolatedDB(t)
		core, logs := observer.New(zap.WarnLevel)
		for range 2 {
			if err := MigrateChannelTrustedInternal(db, zap.New(core)); err != nil {
				t.Fatalf("全新库应无报错：%v", err)
			}
			if err := db.AutoMigrate(&model.AIChannel{}); err != nil {
				t.Fatal(err)
			}
		}
		if db.Migrator().HasColumn("ai_channels", "trusted_internal") {
			t.Error("新库不应有 trusted_internal 列")
		}
		if logs.Len() != 0 {
			t.Errorf("全新库不应有 WARN：%d", logs.Len())
		}
	})
}

// 用 GORM 按当前模型真实建出来的表，再补回旧版的 trusted_internal 列：迁移里的 SQL 必须和真实列名一致。
// （只靠手写的旧表测，列名写错了也发现不了：这次启动就是因为查了不存在的 channel_key 列而退出。）
func TestMigrateChannelTrustedInternal_RealSchema(t *testing.T) {
	db := isolatedDB(t, &model.AIChannel{})
	if err := db.Exec(`ALTER TABLE ai_channels ADD COLUMN trusted_internal boolean NOT NULL DEFAULT false`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO ai_channels (key, name, plugin_key, plugin_version_id, base_url, settings_json, rate_limit_json, trusted_internal)
		VALUES ('lan-gw', '内网网关', 'newapi', 1, 'http://10.0.0.8:3000', '{}', '{}', true)`).Error; err != nil {
		t.Fatalf("插入渠道失败：%v", err)
	}
	core, logs := observer.New(zap.WarnLevel)
	if err := MigrateChannelTrustedInternal(db, zap.New(core)); err != nil {
		t.Fatalf("在真实表结构上迁移失败：%v", err)
	}
	if db.Migrator().HasColumn("ai_channels", "trusted_internal") {
		t.Error("trusted_internal 列应已删除")
	}
	if logs.Len() != 1 || logs.All()[0].ContextMap()["channel"] != "lan-gw" {
		t.Errorf("应对 lan-gw 打一条 WARN：%+v", logs.All())
	}
}
