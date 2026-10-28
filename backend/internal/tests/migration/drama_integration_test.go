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

// TestDramaMigration_Upgrade 验证已有角色数据保留，并允许同名角色和无主图角色。
func TestDramaMigration_Upgrade(t *testing.T) {
	db := isolatedDB(t)
	before := fstest.MapFS{}
	names, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if strings.HasPrefix(name, "0017_") {
			continue
		}
		data, readErr := migrations.FS.ReadFile(name)
		if readErr != nil {
			t.Fatal(readErr)
		}
		before[name] = &fstest.MapFile{Data: data}
	}
	if err := migration.Run(context.Background(), db, migration.Options{FS: before, Logger: testLogger()}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO character_cards (user_id, name, avatar_asset_id) VALUES (7, '母亲', 41)`).Error; err != nil {
		t.Fatal(err)
	}
	runReal(t, db)
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
