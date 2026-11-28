package repository_test

import (
	"errors"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/repository"
)

func TestCanvasCollaborationModePreviewBindsImpact(t *testing.T) {
	db, repo, owner, editor, viewer, project := newCollaborationFixture(t)
	ctx := t.Context()
	shares := repository.NewCanvasShareRepository(db)
	if err := shares.GrantRole(ctx, owner, project.ID, editor, model.CanvasRoleEditor); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ModePreview(ctx, editor, project.ID, false); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("编辑者不能预览切换协作模式: %v", err)
	}
	before, err := repo.ModePreview(ctx, owner, project.ID, false)
	if err != nil || before.Editors != 1 || before.Viewers != 0 || before.Revision != project.Revision {
		t.Fatalf("预览应包含当前版本及受影响成员: %+v, %v", before, err)
	}
	if err := shares.GrantRole(ctx, owner, project.ID, viewer, model.CanvasRoleViewer); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SetActiveConfirmed(ctx, owner, project.ID, before.Revision, false, before.Preview); !errors.Is(err, repository.ErrRevisionConflict) {
		t.Fatalf("成员范围变化后应拒绝旧确认: %v", err)
	}
	current, err := repo.ModePreview(ctx, owner, project.ID, false)
	if err != nil || current.Viewers != 1 || current.Revision != before.Revision {
		t.Fatalf("预览应更新成员范围而不改变画布版本: %+v, %v", current, err)
	}
	if _, err := repo.SetActiveConfirmed(ctx, owner, project.ID, current.Revision, true, current.Preview); !errors.Is(err, repository.ErrRevisionConflict) {
		t.Fatalf("关闭预览不能用于开启: %v", err)
	}
	closed, err := repo.SetActiveConfirmed(ctx, owner, project.ID, current.Revision, false, current.Preview)
	if err != nil || closed.CollaborationActive || closed.Revision != current.Revision+1 || !jsonEqual(closed.PayloadJSON, collaborationFixture) {
		t.Fatalf("确认关闭应保留已提交画布: %+v, %v", closed, err)
	}
	verifyReopenModePreview(t, repo, owner, project.ID)
}

func verifyReopenModePreview(t *testing.T, repo *repository.CanvasCollaborationRepository, owner, canvas uint64) {
	t.Helper()
	ctx := t.Context()
	preview, err := repo.ModePreview(ctx, owner, canvas, true)
	if err != nil || preview.Editors != 1 || preview.Viewers != 1 || !preview.Active {
		t.Fatalf("重新开启应保留原成员的授权: %+v, %v", preview, err)
	}
	opened, err := repo.SetActiveConfirmed(ctx, owner, canvas, preview.Revision, true, preview.Preview)
	if err != nil || !opened.CollaborationActive || opened.Revision != preview.Revision+1 {
		t.Fatalf("确认开启后应恢复协作: %+v, %v", opened, err)
	}
}
