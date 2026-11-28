package migration_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"gorm.io/gorm"

	"video-canvas/internal/migration"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
	"video-canvas/internal/service/agent"
)

func TestCanvasCollaborationMigrationUpgradeAndModeRollback(t *testing.T) {
	db := isolatedDB(t)
	before := migrationsExcept(t)
	for name := range before {
		if name >= "0055_" {
			delete(before, name)
		}
	}
	if err := migration.Run(t.Context(), db, migration.Options{FS: before, Logger: testLogger()}); err != nil {
		t.Fatal(err)
	}
	seedCanvasBeforeCollaboration(t, db)
	runReal(t, db)
	runReal(t, db)

	ctx := t.Context()
	const owner, viewer, canvas = 701, 702, 703
	shares := repository.NewCanvasShareRepository(db)
	shared, err := shares.SharedCanvas(ctx, viewer, canvas)
	if err != nil || shared.CollaborationActive || shared.CollaborationEpoch != 0 || shared.Revision != 7 || !bytes.Contains(shared.PayloadJSON, []byte("升级前内容")) {
		t.Fatalf("升级后原只读共享应保留原画布数据 (%+v, %v)", shared, err)
	}
	projects := repository.NewCanvasProjectRepository(db)
	if _, err := projects.Update(ctx, owner, canvas, 7, map[string]any{"title": "升级后编辑"}); err != nil {
		t.Fatal(err)
	}
	collaboration := repository.NewCanvasCollaborationRepository(db)
	active, err := collaboration.SetActive(ctx, owner, canvas, 8, true)
	if err != nil || active.Revision != 9 || active.CollaborationEpoch != 1 || !bytes.Contains(active.PayloadJSON, []byte("升级前内容")) {
		t.Fatalf("主动开启后应保留原图并初始化会话 (%+v, %v)", active, err)
	}
	_, role, err := collaboration.Snapshot(ctx, viewer, canvas)
	if role != model.CanvasRoleViewer || err != nil {
		t.Fatalf("已有仅查看授权应继续为仅查看 (%s, %v)", role, err)
	}
	if _, err := projects.Update(ctx, owner, canvas, active.Revision, map[string]any{"title": "旧客户端编辑"}); !errors.Is(err, repository.ErrCanvasCollaborationActive) {
		t.Fatalf("协作期间应拒绝旧写入口 (%v)", err)
	}
	assertMigratedCanvasToolWritesBlocked(t, db, active)
	batch := repository.CanvasCollaborationBatch{Epoch: active.CollaborationEpoch, BaseRevision: active.Revision, OpID: "migration-first-edit",
		Changes: []repository.CanvasCollaborationChange{{Kind: "node", ID: "shot", Fields: map[string]json.RawMessage{"data.label": json.RawMessage(`"协作确认内容"`)}}},
	}
	confirmed, err := collaboration.Commit(ctx, owner, canvas, batch)
	if err != nil || confirmed.Revision != 10 || len(confirmed.Changes) != 1 {
		t.Fatalf("SQL 迁移后的图谱应能确认新变更 (%+v, %v)", confirmed, err)
	}
	assertMigratedCanvasRollback(t, collaboration, projects, shares, owner, viewer, canvas, batch, confirmed.Revision)
}

func assertMigratedCanvasToolWritesBlocked(t *testing.T, db *gorm.DB, active *model.CanvasProject) {
	t.Helper()
	const ops = `[{"op":"create_node","kind":"image","label":"旧入口新节点"}]`
	projects := service.NewCanvasProjectService(repository.NewCanvasProjectRepository(db))
	_, cliErr := projects.ApplyOps(t.Context(), active.UserID, active.ID, &service.CanvasOpsReq{
		Revision: active.Revision, Ops: json.RawMessage(ops),
	})
	agentCanvas := agent.NewAgentCanvasService(repository.NewAgentRepository(db), nil)
	_, agentErr := agentCanvas.ApplyOps(t.Context(), &model.AgentRun{UserID: active.UserID, CanvasID: active.ID}, "migration-agent-edit", []byte(ops))
	for name, err := range map[string]error{"CLI": cliErr, "Agent": agentErr} {
		var failure *errcode.Error
		if !errors.As(err, &failure) || failure.Code != errcode.ErrCanvasCollaborationActive.Code {
			t.Errorf("%s 写入口应提示切换到实时协作 (%v)", name, err)
		}
	}
}

func seedCanvasBeforeCollaboration(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, statement := range []string{
		`INSERT INTO users (id, username, email, password) VALUES (701, 'canvas-owner', 'owner@example.test', 'fixture'), (702, 'canvas-viewer', 'viewer@example.test', 'fixture')`,
		`INSERT INTO canvas_projects (id, user_id, title, payload_json, revision) VALUES (703, 701, '既有画布', '{"nodes":[{"id":"shot","type":"canvas","data":{"label":"升级前内容"}}],"edges":[]}', 7)`,
		`INSERT INTO canvas_viewers (canvas_id, user_id) VALUES (703, 702)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func assertMigratedCanvasRollback(t *testing.T, collaboration *repository.CanvasCollaborationRepository, projects *repository.CanvasProjectRepository, shares *repository.CanvasShareRepository, owner, viewer, canvas uint64, batch repository.CanvasCollaborationBatch, revision uint64) {
	t.Helper()
	ctx := t.Context()
	restored, err := collaboration.SetActive(ctx, owner, canvas, revision, false)
	if err != nil || restored.CollaborationActive || restored.Revision != revision+1 || !bytes.Contains(restored.PayloadJSON, []byte("协作确认内容")) {
		t.Fatalf("回退应物化已确认协作内容 (%+v, %v)", restored, err)
	}
	if _, err := collaboration.Replay(ctx, viewer, canvas, batch.Epoch, batch.BaseRevision, 10); !errors.Is(err, repository.ErrCanvasCollaborationSession) {
		t.Fatalf("回退后旧会话应停止重放 (%v)", err)
	}
	if _, err := projects.Update(ctx, owner, canvas, restored.Revision, map[string]any{"title": "回退后编辑"}); err != nil {
		t.Fatal(err)
	}
	shared, err := shares.SharedCanvas(ctx, viewer, canvas)
	if err != nil || shared.Title != "回退后编辑" || !bytes.Contains(shared.PayloadJSON, []byte("协作确认内容")) {
		t.Fatalf("回退后应恢复个人写入及原只读共享 (%+v, %v)", shared, err)
	}
	if opened, err := collaboration.SetActive(ctx, owner, canvas, shared.Revision, true); err != nil || opened.CollaborationEpoch <= batch.Epoch {
		t.Fatalf("重开协作应使用新会话 (%+v, %v)", opened, err)
	}
	if _, err := collaboration.Replay(ctx, viewer, canvas, batch.Epoch, batch.BaseRevision, 10); !errors.Is(err, repository.ErrCanvasCollaborationSession) {
		t.Fatalf("重开后旧会话仍应拒绝重放 (%v)", err)
	}
	if _, err := collaboration.Commit(ctx, owner, canvas, batch); !errors.Is(err, repository.ErrCanvasCollaborationSession) {
		t.Fatalf("重开后旧会话批次和收据不应有效 (%v)", err)
	}
}
