package migration_test

import (
	"testing"

	"video-canvas/internal/migration/legacymodel"
)

// 0002_channel_protocol_on_model：渠道不再绑协议，升级时渠道、模型、渠道 Key 一起清空，
// 插件、其他凭证与用户数据保留；新列有值可写，旧列有默认值（新建渠道不用写）。
func TestChannelProtocolOnModelMigration(t *testing.T) {
	old := isolatedDB(t)
	if err := old.AutoMigrate(legacymodel.All()...); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`INSERT INTO ai_plugins (key, name, source, enabled, updated_at) VALUES ('openai', 'OpenAI', 'builtin', true, now())`,
		`INSERT INTO ai_channels (key, name, plugin_key, plugin_version_id, base_url, settings_json, rate_limit_json, created_at, updated_at)
			VALUES ('old-ch', '旧渠道', 'openai', 1, 'https://api.example.com', '{"api":"responses"}', '{}', now(), now())`,
		`INSERT INTO ai_models (key, kind, enabled, sort, updated_at) VALUES ('old-model', 'text', true, 10, now())`,
		`INSERT INTO ai_config_revisions (target, target_key, revision_no, body_json, status, created_at) VALUES ('model', 'old-model', 1, '{}', 'published', now())`,
		`INSERT INTO ai_secrets (name, ciphertext, nonce, key_version, updated_at) VALUES ('channel:old-ch', '\x01', '\x02', 1, now()), ('other-secret', '\x01', '\x02', 1, now())`,
		`INSERT INTO users (username, password, role, status, created_at, updated_at) VALUES ('keep-me', 'x', 'user', 'active', now(), now())`,
	} {
		if err := old.Exec(stmt).Error; err != nil {
			t.Fatalf("准备旧库失败（%s）：%v", stmt, err)
		}
	}
	runReal(t, old)

	count := func(sql string) int64 {
		var n int64
		if err := old.Raw(sql).Scan(&n).Error; err != nil {
			t.Fatalf("查询失败（%s）：%v", sql, err)
		}
		return n
	}
	for table, want := range map[string]int64{
		"ai_channels": 0, "ai_models": 0, "ai_config_revisions": 0,
	} {
		if got := count("SELECT count(*) FROM " + table); got != want {
			t.Errorf("%s 应已清空，还剩 %d 行", table, got)
		}
	}
	if got := count("SELECT count(*) FROM ai_secrets WHERE name LIKE 'channel:%'"); got != 0 {
		t.Errorf("渠道 Key 应已清空，还剩 %d 行", got)
	}
	if got := count("SELECT count(*) FROM ai_secrets WHERE name = 'other-secret'"); got != 1 {
		t.Error("非渠道凭证应保留")
	}
	if got := count("SELECT count(*) FROM ai_plugins WHERE key = 'openai'"); got != 1 {
		t.Error("插件应保留")
	}
	if got := count("SELECT count(*) FROM users WHERE username = 'keep-me'"); got != 1 {
		t.Error("用户数据应保留")
	}

	// 新结构的渠道：不写旧四列也能插入，新列有值
	if err := old.Exec(`INSERT INTO ai_channels (key, name, base_url, headers_json, rate_limit_json, created_at, updated_at)
		VALUES ('new-ch', '新渠道', 'https://api.example.com', '{"X-A":"1"}', '{}', now(), now())`).Error; err != nil {
		t.Fatalf("新结构的渠道应能不写旧列直接插入：%v", err)
	}
	if got := count("SELECT sort FROM ai_channels WHERE key = 'new-ch'"); got != 100 {
		t.Errorf("渠道 sort 默认应为 100，得到 %d", got)
	}
}
