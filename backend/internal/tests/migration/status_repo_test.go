package migration_test

import (
	"context"
	"testing"

	"video-canvas/internal/migration"
	"video-canvas/internal/repository"
)

func TestMigrationStatusRepository(t *testing.T) {
	db := connect(t, newSchema(t))
	repo := repository.NewMigrationStatusRepository(db)

	before, err := repo.MigrationStatus(context.Background())
	if err != nil || len(before) == 0 || before[0].Applied || before[0].AppliedAt != nil {
		t.Fatalf("空库上应全部未执行：%+v %v", before, err)
	}

	runReal(t, db)
	after, err := repo.MigrationStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	first := after[0]
	if first.Filename != migration.BaselineFile || !first.Applied || first.AppliedAt == nil || first.AppVersion != "test" {
		t.Fatalf("执行后状态不对：%+v", first)
	}
}
