package repository_test

import (
	"errors"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/repository"
)

func TestStorageHybridGuardsAndRestoresDefault(t *testing.T) {
	db := isolatedDB(t, storageModels()...)
	repo := repository.NewStorageConfigRepository(db)
	ctx := t.Context()
	cloud, other := newStorageRow("cloud"), newStorageRow("other")
	local := &model.StorageConfig{Name: "disk", Provider: "local", Builtin: true, Version: 1}
	for _, row := range []*model.StorageConfig{cloud, other, local} {
		if err := repo.Create(ctx, row); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.SetDefault(ctx, cloud.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.BeginHybrid(ctx, 0); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if state, err := repo.ClaimHybrid(ctx, "worker", now); err != nil || state == nil {
		t.Fatalf("应认领启用任务: %v, %v", state, err)
	}
	if err := repo.PinHybrid(ctx, "worker", cloud.ID, local.ID, cloud.Version); err != nil {
		t.Fatal(err)
	}
	if err := repo.FinishHybrid(ctx, "worker", ""); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetDefault(ctx, other.ID); !errors.Is(err, repository.ErrStorageHybridLocked) {
		t.Fatalf("应在仓储冻结默认操作: %v", err)
	}
	if err := repo.Delete(ctx, cloud.ID, now); !errors.Is(err, repository.ErrStorageHybridLocked) {
		t.Fatalf("应在仓储冻结入选配置的删除: %v", err)
	}
	if err := repo.Update(ctx, cloud.ID, cloud.Version, map[string]any{"bucket": "changed"}); !errors.Is(err, repository.ErrStorageHybridLocked) {
		t.Fatalf("策略期间不能重定向已选配置: %v", err)
	}
	if err := repo.Delete(ctx, other.ID, now); err != nil {
		t.Fatalf("仍可删除无引用的未入选配置: %v", err)
	}
	if _, err := repo.DisableHybrid(ctx); err != nil {
		t.Fatal(err)
	}
	if row, err := repo.GetDefault(ctx); err != nil || row.ID != cloud.ID {
		t.Fatalf("应恢复原默认且不搬资源: %+v, %v", row, err)
	}
	if err := repo.SetDefault(ctx, local.ID); err != nil {
		t.Fatalf("应解冻默认操作: %v", err)
	}
}
