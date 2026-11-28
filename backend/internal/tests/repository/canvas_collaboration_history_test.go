package repository_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"video-canvas/internal/canvasdoc"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

func TestCanvasCollaborationSnapshotReviewCapabilities(t *testing.T) {
	db, repo, owner, editor, viewer, project := newCollaborationFixtureWithPayload(t, imageCollaborationFixture)
	if err := db.AutoMigrate(&model.CanvasCollaborationComment{}); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	shares := repository.NewCanvasShareRepository(db)
	if err := shares.GrantRole(ctx, owner, project.ID, editor, model.CanvasRoleEditor); err != nil {
		t.Fatal(err)
	}
	if err := shares.GrantRole(ctx, owner, project.ID, viewer, model.CanvasRoleViewer); err != nil {
		t.Fatal(err)
	}
	svc := service.NewCanvasCollaborationService(repo, shares, nil, true)
	for _, actor := range []uint64{owner, editor, viewer} {
		snapshot, err := svc.Snapshot(ctx, actor, project.ID)
		if err != nil {
			t.Fatal(err)
		}
		_, historyErr := svc.History(ctx, actor, project.ID, project.CollaborationEpoch, 0)
		_, commentsErr := svc.Comments(ctx, actor, project.ID, project.CollaborationEpoch, "node", 0)
		for action, routeErr := range map[string]error{"history": historyErr, "comments-read": commentsErr} {
			if actor == viewer && !errors.Is(routeErr, errcode.ErrCanvasNotFound) || actor != viewer && routeErr != nil {
				t.Fatalf("%s %s: %v", snapshot.Role, action, routeErr)
			}
			if advertised := slices.Contains(snapshot.Capabilities.Actions, action); advertised != (routeErr == nil) {
				t.Errorf("%s %s: 能力声明 %t 与实际权限 %v 不一致", snapshot.Role, action, advertised, routeErr)
			}
		}
	}
}

func TestCanvasCollaborationExportConfirmedVersion(t *testing.T) {
	db, repo, owner, editor, viewer, project := newCollaborationFixture(t)
	ctx := t.Context()
	shares := repository.NewCanvasShareRepository(db)
	if err := shares.GrantRole(ctx, owner, project.ID, editor, model.CanvasRoleEditor); err != nil {
		t.Fatal(err)
	}
	if err := shares.GrantRole(ctx, owner, project.ID, viewer, model.CanvasRoleViewer); err != nil {
		t.Fatal(err)
	}
	transfer := service.NewCanvasTransferService(service.CanvasTransferDeps{
		Canvases: repository.NewCanvasProjectRepository(db), History: repo, Limits: canvasdoc.DefaultLimits(),
	})
	first, err := repo.Commit(ctx, editor, project.ID, collaborationEdit(project, "export-first", "data.prompt", `"确认提示词"`))
	if err != nil || len(first.Changes) != 1 {
		t.Fatalf("保存导出版本失败：%+v %v", first, err)
	}
	project.Revision = first.Revision
	second, err := repo.Commit(ctx, owner, project.ID, collaborationEdit(project, "export-later", "data.prompt", `"后来修改"`))
	if err != nil || len(second.Changes) != 1 {
		t.Fatalf("保存更新版本失败：%+v %v", second, err)
	}
	result, err := transfer.ExportVersion(ctx, owner, project.ID, project.CollaborationEpoch, first.Revision)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range result.Document.Canvas.Nodes {
		if node["id"] == "node" {
			if prompt := node["data"].(map[string]any)["prompt"]; prompt != "确认提示词" {
				t.Fatalf("期望导出指定版本的提示词，得到 %q", prompt)
			}
			return
		}
	}
	t.Fatal("导出文件缺少确认版本的节点")
}

func TestCanvasCollaborationExportAuthorization(t *testing.T) {
	db, repo, owner, editor, viewer, project := newCollaborationFixture(t)
	ctx := t.Context()
	shares := repository.NewCanvasShareRepository(db)
	if err := shares.GrantRole(ctx, owner, project.ID, editor, model.CanvasRoleEditor); err != nil {
		t.Fatal(err)
	}
	if err := shares.GrantRole(ctx, owner, project.ID, viewer, model.CanvasRoleViewer); err != nil {
		t.Fatal(err)
	}
	transfer := service.NewCanvasTransferService(service.CanvasTransferDeps{
		Canvases: repository.NewCanvasProjectRepository(db), History: repo, Limits: canvasdoc.DefaultLimits(),
	})
	for _, test := range []struct {
		name                   string
		actor, epoch, revision uint64
		code                   int
	}{
		{"editor", editor, project.CollaborationEpoch, project.Revision, errcode.ErrCanvasNotFound.Code},
		{"viewer", viewer, project.CollaborationEpoch, project.Revision, errcode.ErrCanvasNotFound.Code},
		{"wrong epoch", owner, project.CollaborationEpoch + 1, project.Revision, errcode.ErrCanvasCollaborationSession.Code},
		{"future revision", owner, project.CollaborationEpoch, project.Revision + 1, errcode.ErrCanvasConflict.Code},
		{"unpinned", owner, 0, 0, errcode.ErrCanvasCollaborationActive.Code},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := exportCollaborationError(t, transfer, project.ID, test.actor, test.epoch, test.revision)
			var failure *errcode.Error
			if !errors.As(err, &failure) || failure.Code != test.code {
				t.Fatalf("期望错误码 %d，得到 %v", test.code, err)
			}
		})
	}
}

func exportCollaborationError(t *testing.T, transfer *service.CanvasTransferService, canvas, actor, epoch, revision uint64) error {
	t.Helper()
	if epoch == 0 {
		_, err := transfer.Export(t.Context(), actor, canvas)
		return err
	}
	_, err := transfer.ExportVersion(t.Context(), actor, canvas, epoch, revision)
	return err
}

func TestCanvasCollaborationHistoryCheckpoints(t *testing.T) {
	db, repo, owner, _, _, project := newCollaborationFixture(t)
	ctx := t.Context()
	var confirmedAtCheckpoint []byte
	for index := range 27 {
		batch := collaborationEdit(project, fmt.Sprintf("move-%d", index), "position", fmt.Sprintf(`{"x":%d,"y":7}`, index+100))
		result, err := repo.Commit(ctx, owner, project.ID, batch)
		if err != nil || len(result.Changes) != 1 {
			t.Fatalf("移动节点失败: %+v, %v", result, err)
		}
		project.Revision = result.Revision
		if project.Revision == 24 {
			confirmed, _, err := repo.Snapshot(ctx, owner, project.ID)
			if err != nil {
				t.Fatal(err)
			}
			confirmedAtCheckpoint = confirmed.PayloadJSON
		}
	}
	var checkpoint model.CanvasCollaborationCheckpoint
	if err := db.Where("canvas_id = ? AND NOT baseline", project.ID).Order("revision DESC").Take(&checkpoint).Error; err != nil {
		t.Fatalf("连续提交后应有可用于恢复的新检查点: %v", err)
	}
	if checkpoint.Revision != 24 || !jsonEqual(checkpoint.PayloadJSON, string(confirmedAtCheckpoint)) {
		t.Fatalf("检查点应与同一版本的确认图谱一致: %s", checkpoint.PayloadJSON)
	}
	if err := db.Where("canvas_id = ? AND revision <= ?", project.ID, checkpoint.Revision).Delete(&model.CanvasCollaborationEvent{}).Error; err != nil {
		t.Fatal(err)
	}
	view, err := repo.HistoricalSnapshot(ctx, owner, project.ID, project.CollaborationEpoch, project.Revision-1)
	if err != nil || view.Revision != project.Revision-1 || !strings.Contains(string(view.PayloadJSON), `"x":125`) {
		t.Fatalf("应从新检查点恢复并叠加剩余变更: %+v, %v", view, err)
	}
}

func TestCanvasCollaborationHistoryRetention(t *testing.T) {
	db, repo, owner, _, _, project := newCollaborationFixture(t)
	ctx := t.Context()
	base := project.Revision
	first := collaborationEdit(project, "first", "position", `{"x":24,"y":7}`)
	for revision := base + 1; revision <= 25; revision++ {
		batch := collaborationEdit(project, fmt.Sprintf("move-%d", revision), "position", fmt.Sprintf(`{"x":%d,"y":7}`, revision))
		if revision == base+1 {
			batch = first
		}
		result, err := repo.Commit(ctx, owner, project.ID, batch)
		if err != nil || result.Revision != revision {
			t.Fatalf("提交失败: %+v, %v", result, err)
		}
		project.Revision = revision
	}
	cutoff := time.Now().Add(-30 * 24 * time.Hour)
	old := cutoff.Add(-time.Hour)
	if err := db.Model(&model.CanvasCollaborationCheckpoint{}).Where("canvas_id = ?", project.ID).Update("created_at", old).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CanvasCollaborationEvent{}).Where("canvas_id = ? AND revision < 24", project.ID).Updates(map[string]any{"created_at": old, "published_at": old}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CanvasCollaborationReceipt{}).Where("canvas_id = ?", project.ID).Update("created_at", old).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.PruneHistory(ctx, cutoff, 100); err != nil {
		t.Fatal(err)
	}
	replay, err := repo.Replay(ctx, owner, project.ID, project.CollaborationEpoch, base, 100)
	if err != nil || !replay.SnapshotRequired || len(replay.Events) != 0 {
		t.Fatalf("清理后应以快照恢复: %+v, %v", replay, err)
	}
	if _, err := repo.HistoricalSnapshot(ctx, owner, project.ID, project.CollaborationEpoch, base); !errors.Is(err, repository.ErrCanvasVersionUnavailable) {
		t.Fatalf("已清理历史版本不能恢复: %v", err)
	}
	view, err := repo.HistoricalSnapshot(ctx, owner, project.ID, project.CollaborationEpoch, 24)
	if err != nil || !strings.Contains(string(view.PayloadJSON), `"x":24`) {
		t.Fatalf("最近检查点应可恢复: %+v, %v", view, err)
	}
	ack, err := repo.Commit(ctx, owner, project.ID, first)
	if err != nil || ack.Revision != base+1 {
		t.Fatalf("活跃代数的原提交收据仍须精确对账: %+v, %v", ack, err)
	}
}
