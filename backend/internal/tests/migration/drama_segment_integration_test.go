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
		ScriptMD         string
		StoryboardStatus string
	}
	if err := db.Raw(`SELECT s.revision, e.script_md, e.storyboard_status FROM drama_segments s JOIN drama_episodes e ON e.id = s.episode_id`).Scan(&result).Error; err != nil {
		t.Fatal(err)
	}
	if result.Revision != 1 || result.ScriptMD != "保留剧本" || result.StoryboardStatus != "ready" {
		t.Fatalf("迁移默认值与存量数据：%+v", result)
	}
}

// 已有分镜表和首帧数据的库升级到 0021、0022：shots、keyframe_asset_id 两列被删掉，片段的其余数据原样保留。
func TestDropSegmentShotsMigration_KeepsSegments(t *testing.T) {
	db := isolatedDB(t)
	before := fstest.MapFS{}
	names, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if name >= "0021_" {
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
	if err := db.Exec(`INSERT INTO drama_segments (user_id, episode_id, seq, seconds, prompt, shots, keyframe_asset_id) VALUES (7, 1, 0, 12, '村口争执', '[{"seconds":12}]', 99)`).Error; err != nil {
		t.Fatal(err)
	}
	runReal(t, db)
	for _, column := range []string{"shots", "keyframe_asset_id"} {
		if hasColumn(t, db, "drama_segments", column) {
			t.Fatalf("drama_segments.%s 应已删除", column)
		}
	}
	var prompt string
	if err := db.Raw(`SELECT prompt FROM drama_segments WHERE user_id = 7`).Scan(&prompt).Error; err != nil {
		t.Fatal(err)
	}
	if prompt != "村口争执" {
		t.Fatalf("片段提示词应保留，得到 %q", prompt)
	}
}
