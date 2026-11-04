package migration_test

import (
	"context"
	"strings"
	"testing"

	"video-canvas/internal/migration"
	"video-canvas/internal/model"
)

func TestDramaSettingsMigration_BackfillsAndRetainsSource(t *testing.T) {
	db := isolatedDB(t)
	if err := migration.Run(context.Background(), db, migration.Options{FS: migrationsExcept(t, "0025_"), Logger: testLogger()}); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"序言\n第1集 开始\n甲\n第2集 结束\n乙", "没有标记的旧原文"} {
		if err := db.Exec(`INSERT INTO drama_projects (user_id,canvas_project_id,title,source_text,style_key) VALUES (1,(SELECT count(*)+1 FROM drama_projects),'旧项目',?,'custom')`, source).Error; err != nil {
			t.Fatal(err)
		}
	}
	runReal(t, db)
	var projects []model.DramaProject
	if err := db.Order("id").Find(&projects).Error; err != nil {
		t.Fatal(err)
	}
	for _, p := range projects {
		if p.Phase != "done" || p.SourceText == "" || p.SourceChars == 0 || p.UnitsTotal == 0 {
			t.Fatalf("旧项目缺少回填: id=%d phase=%s chars=%d units=%d", p.ID, p.Phase, p.SourceChars, p.UnitsTotal)
		}
		var units []model.DramaUnit
		if err := db.Where("drama_id = ?", p.ID).Order("no").Find(&units).Error; err != nil {
			t.Fatal(err)
		}
		var text strings.Builder
		for i, u := range units {
			if u.No != i+1 {
				t.Fatal("回填单元不连续")
			}
			text.WriteString(u.Text)
			text.WriteByte('\n')
		}
		for _, line := range strings.Split(p.SourceText, "\n") {
			if !strings.Contains(text.String(), line) {
				t.Fatalf("回填丢失 %q", line)
			}
		}
	}
	if projects[0].UnitsTotal != 3 || projects[1].UnitsTotal != 1 {
		t.Fatalf("切分错误: %d %d", projects[0].UnitsTotal, projects[1].UnitsTotal)
	}
}
