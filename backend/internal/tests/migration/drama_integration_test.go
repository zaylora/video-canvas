package migration_test

import (
	"context"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"video-canvas/internal/migration"
	"video-canvas/migrations"
)

// migrationsExcept 返回内嵌迁移里去掉指定编号前缀之后的文件集合。
func migrationsExcept(t *testing.T, prefixes ...string) fstest.MapFS {
	t.Helper()
	out := fstest.MapFS{}
	names, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
next:
	for _, name := range names {
		for _, prefix := range prefixes {
			if strings.HasPrefix(name, prefix) {
				continue next
			}
		}
		data, readErr := migrations.FS.ReadFile(name)
		if readErr != nil {
			t.Fatal(readErr)
		}
		out[name] = &fstest.MapFile{Data: data}
	}
	return out
}

// TestDramaMigration_Upgrade 验证 0017–0019 升级时已有角色数据保留，并允许同名角色和无主图角色；
// 角色卡表本身在 0020 才下线，所以这里只跑到 0019。
func TestDramaMigration_Upgrade(t *testing.T) {
	db := isolatedDB(t)
	// 短剧迁移（0017–0019）都留给升级步骤；0020 删表更晚，也不能提前跑
	before := migrationsExcept(t, "0017_", "0018_", "0019_", "0020_")
	if err := migration.Run(context.Background(), db, migration.Options{FS: before, Logger: testLogger()}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO character_cards (user_id, name, avatar_asset_id) VALUES (7, '母亲', 41)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := migration.Run(context.Background(), db, migration.Options{FS: migrationsExcept(t, "0020_"), Logger: testLogger()}); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"drama_projects", "drama_episodes", "drama_jobs"} {
		if !hasTable(t, db, table) {
			t.Fatalf("缺少短剧表 %s", table)
		}
	}
	var avatar uint64
	if err := db.Raw(`SELECT avatar_asset_id FROM character_cards WHERE user_id = 7`).Scan(&avatar).Error; err != nil || avatar != 41 {
		t.Fatalf("升级应保留主图：avatar=%d err=%v", avatar, err)
	}
	if err := db.Exec(`INSERT INTO character_cards (user_id, name, avatar_asset_id, drama_id, look_name, voice_prompt)
		VALUES (7, '母亲', NULL, 11, '青年', '温柔低声'), (7, '母亲', NULL, 12, '', '')`).Error; err != nil {
		t.Fatalf("同名且无主图的短剧角色应能共存：%v", err)
	}
	if err := db.Exec(`INSERT INTO drama_jobs (drama_id, user_id, step, status) VALUES (11, 7, 'analyze', 'queued')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO drama_jobs (drama_id, user_id, step, status) VALUES (11, 7, 'outline', 'running')`).Error; err == nil {
		t.Fatal("同一短剧不能同时存在两个活跃任务")
	}
	if err := db.Exec(`UPDATE drama_jobs SET status = 'succeeded' WHERE drama_id = 11`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO drama_jobs (drama_id, user_id, step, status) VALUES (11, 7, 'outline', 'queued')`).Error; err != nil {
		t.Fatalf("结束后应可排下一个任务：%v", err)
	}
}

// TestCharacterCardsDropMigration 验证 0020：旧角色卡数据不迁移、直接清掉，表连同它的索引一起删除；
// 画布 payload 里的角色数据不在数据库表里，不受影响。
func TestCharacterCardsDropMigration(t *testing.T) {
	db := isolatedDB(t)
	if err := migration.Run(context.Background(), db, migration.Options{FS: migrationsExcept(t, "0020_"), Logger: testLogger()}); err != nil {
		t.Fatal(err)
	}
	if !hasTable(t, db, "character_cards") {
		t.Fatal("0020 之前角色卡表应存在")
	}
	if err := db.Exec(`INSERT INTO character_cards (user_id, name, avatar_asset_id) VALUES (7, '母亲', 41), (7, '母亲', NULL)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO canvas_projects (user_id, title, payload_json, revision) VALUES (7, '画布', '{"nodes":[{"id":"n1","data":{"kind":"character","character":{"name":"母亲"}}}]}', 1)`).Error; err != nil {
		t.Fatal(err)
	}
	runReal(t, db)
	if hasTable(t, db, "character_cards") {
		t.Fatal("0020 之后角色卡表应已删除")
	}
	var indexes int64
	if err := db.Raw(`SELECT count(*) FROM pg_indexes WHERE schemaname = current_schema() AND tablename = 'character_cards'`).Scan(&indexes).Error; err != nil || indexes != 0 {
		t.Fatalf("角色卡表的索引应随表删除：n=%d err=%v", indexes, err)
	}
	var payload string
	if err := db.Raw(`SELECT payload_json::text FROM canvas_projects WHERE user_id = 7`).Scan(&payload).Error; err != nil || !strings.Contains(payload, `"character"`) {
		t.Fatalf("画布里内嵌的角色数据不应受影响：%s %v", payload, err)
	}
	// 重复执行迁移幂等：已记录的文件不会再跑
	runReal(t, db)
}
