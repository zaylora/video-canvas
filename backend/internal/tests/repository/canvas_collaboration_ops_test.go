package repository_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"gorm.io/gorm"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
)

func newCollaborationFixture(t *testing.T) (*gorm.DB, *repository.CanvasCollaborationRepository, uint64, uint64, uint64, *model.CanvasProject) {
	t.Helper()
	return newCollaborationFixtureWithPayload(t, collaborationFixture)
}

func newCollaborationFixtureWithPayload(t *testing.T, payload string) (*gorm.DB, *repository.CanvasCollaborationRepository, uint64, uint64, uint64, *model.CanvasProject) {
	t.Helper()
	db := isolatedDB(t, &model.CanvasProject{}, &model.CanvasViewer{}, &model.CanvasCollaborationRecord{},
		&model.CanvasCollaborationReceipt{}, &model.CanvasCollaborationEvent{}, &model.CanvasCollaborationCheckpoint{}, &model.GenerationTask{}, &model.StoryboardSplitJob{}, &model.CanvasComposeJob{})
	users := make([]model.User, 3)
	for index := range users {
		name := fmt.Sprintf("collaborator-%d", index)
		users[index] = model.User{Username: name, Email: name + "@example.test", Password: "unused", Status: model.UserStatusActive}
		if err := db.Create(&users[index]).Error; err != nil {
			t.Fatal(err)
		}
	}
	project := &model.CanvasProject{UserID: users[0].ID, Revision: 7, Title: "协作画布", PayloadJSON: []byte(payload)}
	if err := db.Create(project).Error; err != nil {
		t.Fatal(err)
	}
	repo := repository.NewCanvasCollaborationRepository(db)
	active, err := repo.SetActive(t.Context(), project.UserID, project.ID, 7, true)
	if err != nil {
		t.Fatal(err)
	}
	return db, repo, users[0].ID, users[1].ID, users[2].ID, active
}

func TestCanvasCollaborationFieldBases(t *testing.T) {
	_, repo, owner, _, _, project := newCollaborationFixture(t)
	ctx := t.Context()
	prompt := collaborationEdit(project, "prompt", "data.prompt", `"原提示词"`)
	position := collaborationEdit(project, "move", "position", `{"x":42,"y":9}`)
	if _, err := repo.Commit(ctx, owner, project.ID, position); err != nil {
		t.Fatal(err)
	}
	first, err := repo.Commit(ctx, owner, project.ID, prompt)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Changes) != 1 || first.Revision != 10 {
		t.Fatalf("提示词未确认: %+v", first)
	}
	next := collaborationEdit(project, "second-prompt", "data.prompt", `"新提示词"`)
	next.Changes[0].FieldBases = map[string]uint64{"data.prompt": first.Revision}
	next.Changes[0].Fields["position"] = json.RawMessage(`{"x":7,"y":8}`)
	conflict, err := repo.Commit(ctx, owner, project.ID, next)
	if err != nil || len(conflict.Conflicts) != 1 || conflict.Conflicts[0].Field != "position" {
		t.Fatalf("位置字段的旧前置版本应触发原子冲突: %+v, %v", conflict, err)
	}
	delete(next.Changes[0].Fields, "position")
	next.OpID = "prompt-only"
	second, err := repo.Commit(ctx, owner, project.ID, next)
	if err != nil || len(second.Changes) != 1 || second.Revision != 11 || len(second.Changes[0].FieldBases) != 0 {
		t.Fatalf("提示词应被确认，确认内容不携带前置条件: %+v, %v", second, err)
	}
	next.OpID = "future-base"
	next.Changes[0].FieldBases["data.prompt"] = second.Revision + 1
	if _, err := repo.Commit(ctx, owner, project.ID, next); !errors.Is(err, repository.ErrCanvasCollaborationSession) {
		t.Fatalf("应拒绝未来字段版本: %v", err)
	}
}

func TestCanvasCollaborationDragUsesGestureRevision(t *testing.T) {
	_, repo, owner, _, _, project := newCollaborationFixture(t)
	move := collaborationEdit(project, "first-move", "position", `{"x":42,"y":9}`)
	first, err := repo.Commit(t.Context(), owner, project.ID, move)
	if err != nil || len(first.Changes) != 1 {
		t.Fatalf("首次移动失败: %+v %v", first, err)
	}
	draft := collaborationEdit(project, "drag-ended", "position", `{"x":13,"y":15}`)
	draft.BaseRevision = first.Revision
	draft.Changes[0].FieldBases = map[string]uint64{"position": move.BaseRevision}
	result, err := repo.Commit(t.Context(), owner, project.ID, draft)
	if err != nil || len(result.Conflicts) != 1 || result.Conflicts[0].Field != "position" {
		t.Fatalf("接收远端移动后松手，仍应检测手势起点以来的位置变更: %+v %v", result, err)
	}
}

func TestCanvasCollaborationParamsKeepConcurrentPrompt(t *testing.T) {
	_, repo, owner, _, _, project := newCollaborationFixture(t)
	ctx := t.Context()
	prompt := collaborationEdit(project, "updated-prompt", "data.prompt", `"并行编辑的提示词"`)
	options := collaborationEdit(project, "updated-options", "data.params", `{"ratio":"9:16","duration":5}`)
	for _, batch := range []repository.CanvasCollaborationBatch{prompt, options} {
		result, err := repo.Commit(ctx, owner, project.ID, batch)
		if err != nil || len(result.Changes) != 1 || len(result.Conflicts) > 0 {
			t.Fatalf("模型参数与提示词应独立确认: %+v %v", result, err)
		}
	}
	conflicting := collaborationEdit(project, "conflicting-options", "data.params", `{"ratio":"4:3"}`)
	result, err := repo.Commit(ctx, owner, project.ID, conflicting)
	if err != nil || len(result.Conflicts) != 1 || result.Conflicts[0].Field != "data.params" {
		t.Fatalf("同时修改同一参数组应产生冲突: %+v %v", result, err)
	}
	for _, invalidValue := range []string{`{"prompt":"另一个提示词"}`, `{"images":["123"]}`, `{"assetId":"123"}`, `{"mode":{"enabled":true}}`} {
		invalid := collaborationEdit(project, "invalid-options", "data.params", invalidValue)
		if _, err := repo.Commit(ctx, owner, project.ID, invalid); !errors.Is(err, repository.ErrCanvasCollaborationInvalidBatch) {
			t.Fatalf("参数组只接受普通模型参数: %v", err)
		}
	}
	snapshot, err := repo.HistoricalSnapshot(ctx, owner, project.ID, project.CollaborationEpoch, result.Revision)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Nodes []struct {
			ID   string
			Data struct{ Params map[string]any }
		}
	}
	if err := json.Unmarshal(snapshot.PayloadJSON, &payload); err != nil {
		t.Fatal(err)
	}
	for _, node := range payload.Nodes {
		if node.ID == "node" && node.Data.Params["prompt"] == "并行编辑的提示词" && node.Data.Params["ratio"] == "9:16" {
			return
		}
	}
	t.Fatalf("确认快照应同时保留提示词与模型参数: %s", snapshot.PayloadJSON)
}

func collaborationEdit(project *model.CanvasProject, id, field, value string) repository.CanvasCollaborationBatch {
	return repository.CanvasCollaborationBatch{Epoch: project.CollaborationEpoch, BaseRevision: project.Revision, OpID: id,
		Changes: []repository.CanvasCollaborationChange{{Kind: "node", ID: "node", Fields: map[string]json.RawMessage{field: json.RawMessage(value)}}}}
}

func TestCanvasCollaborationAccess(t *testing.T) {
	db, repo, owner, editor, viewer, project := newCollaborationFixture(t)
	ctx := t.Context()
	share := repository.NewCanvasShareRepository(db)
	if err := share.GrantRole(ctx, owner, project.ID, editor, model.CanvasRoleEditor); err != nil {
		t.Fatal(err)
	}
	if err := share.Grant(ctx, owner, project.ID, viewer); err != nil {
		t.Fatal(err)
	}
	batch := collaborationEdit(project, "editor-edit", "data.label", `"编辑者"`)
	if _, err := repo.Commit(ctx, viewer, project.ID, batch); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("查看者不能编辑: %v", err)
	}
	if _, err := repo.Commit(ctx, editor, project.ID, batch); err != nil {
		t.Fatal(err)
	}
	if err := share.GrantRole(ctx, owner, project.ID, editor, model.CanvasRoleViewer); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Commit(ctx, editor, project.ID, batch); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("降级后不能取回编辑收据: %v", err)
	}
	if err := share.Remove(ctx, owner, project.ID, editor); err != nil {
		t.Fatal(err)
	}
	allowed := repo.RoomReaders(ctx, []uint64{owner, editor, viewer}, project.ID)
	if !allowed[owner] || allowed[editor] || !allowed[viewer] {
		t.Fatalf("批量鉴权必须使用当前成员授权: %v", allowed)
	}
	if _, err := share.SharedCanvas(ctx, editor, project.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("撤权后不能读取画布: %v", err)
	}
}

func TestCanvasCollaborationImportMedia(t *testing.T) {
	db, repo, owner, editor, _, project := newCollaborationFixtureWithPayload(t, imageCollaborationFixture)
	if err := db.AutoMigrate(&model.Asset{}, &model.CanvasDeliveryStage{}); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	if err := repository.NewCanvasShareRepository(db).GrantRole(ctx, owner, project.ID, editor, model.CanvasRoleEditor); err != nil {
		t.Fatal(err)
	}
	asset := model.Asset{UserID: owner, Kind: "image", StorageKey: "owner/image.png", FileName: "image.png"}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	assetID := fmt.Sprintf(`"%d"`, asset.ID)
	batch := collaborationEdit(project, "import-owner-image", "data.assetId", assetID)
	result, err := repo.Commit(ctx, owner, project.ID, batch)
	if err != nil || len(result.Changes) != 1 || !jsonEqual(result.Changes[0].Fields["data.src"], `"/files/owner/image.png"`) {
		t.Fatalf("素材应从授权记录解析并随节点字段确认: %+v, %v", result, err)
	}
	replay, err := repo.Replay(ctx, editor, project.ID, project.CollaborationEpoch, project.Revision, 10)
	if err != nil || len(replay.Events) != 1 || !jsonEqual(replay.Events[0].Changes[0].Fields["data.src"], fmt.Sprintf(`"/api/v1/canvas-media/%s/%s"`, idcodec.Encode(project.ID), idcodec.Encode(asset.ID))) {
		t.Fatalf("成员应收到共享媒体地址: %+v, %v", replay, err)
	}
	undo, err := repo.Undo(ctx, owner, project.ID, repository.CanvasCollaborationUndo{Epoch: project.CollaborationEpoch, OpID: "undo-import"})
	if err != nil || len(undo.Changes) != 1 || !jsonEqual(undo.Changes[0].Fields["data.assetId"], "null") || !jsonEqual(undo.Changes[0].Fields["data.src"], "null") {
		t.Fatalf("个人撤销应清除该次导入的媒体字段: %+v, %v", undo, err)
	}
}

func TestCanvasCollaborationRevokeWinsQueuedWrite(t *testing.T) {
	db, repo, owner, editor, _, project := newCollaborationFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	share := repository.NewCanvasShareRepository(db)
	if err := share.GrantRole(ctx, owner, project.ID, editor, model.CanvasRoleEditor); err != nil {
		t.Fatal(err)
	}
	tx := db.WithContext(ctx).Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer func() { _ = tx.Rollback() }()
	var revokerPID int
	if err := tx.Raw("SELECT pg_backend_pid()").Scan(&revokerPID).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.NewCanvasShareRepository(tx).Remove(ctx, owner, project.ID, editor); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	batch := collaborationEdit(project, "queued-after-revoke", "data.label", `"排队的写入"`)
	go func() {
		_, err := repo.Commit(ctx, editor, project.ID, batch)
		result <- err
	}()
	waitForCollaborationLock(ctx, t, db, revokerPID)
	if err := tx.Commit().Error; err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("已排队的写入应按撤权后的权限处理: %v", err)
	}
	var count int64
	if err := db.Model(&model.CanvasCollaborationEvent{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("撤权后不会留下写入事件: %d, %v", count, err)
	}
}

func waitForCollaborationLock(ctx context.Context, t *testing.T, db *gorm.DB, blocker int) {
	t.Helper()
	ticker := time.NewTicker(15 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		err := db.WithContext(ctx).Raw("SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE ? = ANY(pg_blocking_pids(pid)))", blocker).Scan(&waiting).Error
		if err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("写入未进入画布事务锁等待队列")
		case <-ticker.C:
		}
	}
}

func TestCanvasCollaborationConcurrentOps(t *testing.T) {
	db, repo, owner, editor, _, project := newCollaborationFixture(t)
	ctx := t.Context()
	if err := repository.NewCanvasShareRepository(db).GrantRole(ctx, owner, project.ID, editor, model.CanvasRoleEditor); err != nil {
		t.Fatal(err)
	}
	label := collaborationEdit(project, "label", "data.label", `"甲"`)
	position := collaborationEdit(project, "position", "position", `{"x":42,"y":9}`)
	first, err := repo.Commit(ctx, owner, project.ID, label)
	if err != nil || first.Revision != 9 || len(first.Changes) != 1 {
		t.Fatalf("初次编辑没有确认: %+v, %v", first, err)
	}
	second, err := repo.Commit(ctx, editor, project.ID, position)
	if err != nil || second.Revision != 10 || len(second.Changes) != 1 {
		t.Fatalf("基于同版本修改独立字段应合并: %+v, %v", second, err)
	}
	conflict := collaborationEdit(project, "conflict", "data.label", `"乙"`)
	conflict.Changes[0].Fields["data.prompt"] = json.RawMessage(`"不应落库"`)
	result, err := repo.Commit(ctx, editor, project.ID, conflict)
	if err != nil || result.Revision != 10 || len(result.Conflicts) != 1 || !jsonEqual(result.Conflicts[0].Value, `"甲"`) {
		t.Fatalf("同字段应返回冲突值: %+v, %v", result, err)
	}
	repeat, err := repo.Commit(ctx, editor, project.ID, position)
	if err != nil || repeat.Revision != second.Revision || len(repeat.Changes) != 1 {
		t.Fatalf("重试应取回原收据: %+v, %v", repeat, err)
	}
	position.Changes[0].Fields["position"] = json.RawMessage(`{"x":11,"y":12}`)
	if _, err := repo.Commit(ctx, editor, project.ID, position); !errors.Is(err, repository.ErrCanvasCollaborationOpID) {
		t.Fatalf("相同 ID 不能提交不同请求: %v", err)
	}
	var count int64
	if err := db.Model(&model.CanvasCollaborationEvent{}).Count(&count).Error; err != nil || count != 2 {
		t.Fatalf("事件只记录实际提交: %d, %v", count, err)
	}
	snapshot, err := repository.NewCanvasShareRepository(db).SharedCanvas(ctx, editor, project.ID)
	want := `{"nodes":[{"id":"group","type":"group"},{"id":"node","type":"canvas","parentId":"group","data":{"label":"甲"},"position":{"x":42,"y":9}}],"edges":[],"appearance":{"background":"grid"},"viewport":{"x":5,"y":8,"zoom":0.6}}`
	if err != nil || snapshot.Revision != 10 || !jsonEqual(snapshot.PayloadJSON, want) {
		t.Fatalf("共享快照应包含两个确认字段且无冲突批次的写入: %+v, %v", snapshot, err)
	}
}
