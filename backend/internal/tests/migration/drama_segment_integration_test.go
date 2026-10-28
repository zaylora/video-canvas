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

func TestDramaSegmentMigration_Upgrade(t *testing.T) {
	db := isolatedDB(t)
	before := fstest.MapFS{}
	names, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if strings.HasPrefix(name, "0018_") {
			continue
		}
		data, err := migrations.FS.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		before[name] = &fstest.MapFile{Data: data}
	}
	if err := migration.Run(context.Background(), db, migration.Options{FS: before, Logger: testLogger()}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO drama_episodes (user_id, drama_id, no, script_md, storyboard_status) VALUES (7, 11, 1, '保留剧本', 'ready')`).Error; err != nil {
		t.Fatal(err)
	}
	runReal(t, db)
	if !hasTable(t, db, "drama_segments") {
		t.Fatal("缺少片段表")
	}
	if err := db.Exec(`INSERT INTO drama_segments (user_id, episode_id, seq, seconds) VALUES (7, 1, 0, 12)`).Error; err != nil {
		t.Fatal(err)
	}
	var result struct {
		Revision         int64
		Shots            string
		ScriptMD         string
		StoryboardStatus string
	}
	if err := db.Raw(`SELECT s.revision, s.shots::text, e.script_md, e.storyboard_status FROM drama_segments s JOIN drama_episodes e ON e.id = s.episode_id`).Scan(&result).Error; err != nil {
		t.Fatal(err)
	}
	if result.Revision != 1 || result.Shots != "[]" || result.ScriptMD != "保留剧本" || result.StoryboardStatus != "ready" {
		t.Fatalf("迁移默认值与存量数据：%+v", result)
	}
}
