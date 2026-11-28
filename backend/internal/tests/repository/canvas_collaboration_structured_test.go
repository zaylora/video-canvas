package repository_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/repository"
)

func TestCanvasCollaborationManualReferences(t *testing.T) {
	db, repo, owner, editor, _, project := newCollaborationFixture(t)
	ctx := t.Context()
	if err := db.AutoMigrate(&model.Asset{}); err != nil {
		t.Fatal(err)
	}
	if err := repository.NewCanvasShareRepository(db).GrantRole(ctx, owner, project.ID, editor, model.CanvasRoleEditor); err != nil {
		t.Fatal(err)
	}
	asset := model.Asset{UserID: owner, Kind: "image", StorageKey: "owner/ref.png"}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	field := "data.manualReferences"
	set := collaborationEdit(project, "manual-ref", field, fmt.Sprintf(`{"images":[{"assetId":"%d"}]}`, asset.ID))
	confirm, err := repo.Commit(ctx, owner, project.ID, set)
	if err != nil || len(confirm.Changes) != 1 {
		t.Fatalf("应确认素材绑定: %+v %v", confirm, err)
	}
	replay, err := repo.Replay(ctx, editor, project.ID, project.CollaborationEpoch, project.Revision, 10)
	if err != nil || len(replay.Events) != 1 {
		t.Fatalf("成员应收到素材增量: %+v %v", replay, err)
	}
	ref := string(replay.Events[0].Changes[0].Fields[field])
	if !strings.Contains(ref, "/api/v1/canvas-media/") || strings.Contains(ref, "owner/ref.png") {
		t.Fatalf("应返回画布授权地址: %s", ref)
	}
	snapshot, _, err := repo.Snapshot(ctx, editor, project.ID)
	if err != nil || !strings.Contains(string(snapshot.PayloadJSON), `"paramAssets"`) || strings.Contains(string(snapshot.PayloadJSON), `"manualReferences"`) {
		t.Fatalf("重开后应读到标准画布素材字段: %+v %v", snapshot, err)
	}
	invalid := collaborationEdit(project, "manual-ref-invalid", field, `{"images":[{"assetId":"999999"}]}`)
	invalid.BaseRevision = confirm.Revision
	if _, err := repo.Commit(ctx, editor, project.ID, invalid); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("应拒绝画布外素材: %v", err)
	}
	clearRefs := collaborationEdit(project, "manual-ref-clear", field, `{}`)
	clearRefs.BaseRevision = confirm.Revision
	if result, err := repo.Commit(ctx, editor, project.ID, clearRefs); err != nil || len(result.Changes) != 1 {
		t.Fatalf("成员应能移除共享引用: %+v %v", result, err)
	}
}

func TestCanvasCollaborationStructuredNodes(t *testing.T) {
	db, repo, owner, editor, _, project := newCollaborationFixture(t)
	ctx := t.Context()
	if err := db.AutoMigrate(&model.Asset{}); err != nil {
		t.Fatal(err)
	}
	if err := repository.NewCanvasShareRepository(db).GrantRole(ctx, owner, project.ID, editor, model.CanvasRoleEditor); err != nil {
		t.Fatal(err)
	}
	asset := model.Asset{UserID: owner, Kind: "image", StorageKey: "owner/private.png"}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	commit := func(actor, base uint64, op string, change repository.CanvasCollaborationChange) (*repository.CanvasCollaborationCommit, error) {
		return repo.Commit(ctx, actor, project.ID, repository.CanvasCollaborationBatch{Epoch: project.CollaborationEpoch,
			BaseRevision: base, OpID: op, Changes: []repository.CanvasCollaborationChange{change}})
	}
	created, err := commit(owner, project.Revision, "create-role", repository.CanvasCollaborationChange{Kind: "node", ID: "role", Action: "create",
		Value: json.RawMessage(fmt.Sprintf(`{"position":{"x":0,"y":0},"data":{"kind":"character","label":"角色","character":{"name":"甲","look":"基础形象","description":"","extras":[{"assetId":"%d"}],"voice":null}}}`, asset.ID))})
	if err != nil || len(created.Changes) != 1 || !strings.Contains(string(created.Changes[0].Value), "/files/owner/private.png") {
		t.Fatalf("创建时应确认角色素材: %+v, %v", created, err)
	}
	role := func(name, id string) repository.CanvasCollaborationChange {
		return repository.CanvasCollaborationChange{Kind: "node", ID: "role", Fields: map[string]json.RawMessage{
			"data.character": json.RawMessage(fmt.Sprintf(`{"name":%q,"look":"基础形象","description":"","extras":[{"assetId":%q}],"voice":null}`, name, id)),
		}}
	}
	updated, err := commit(editor, created.Revision, "rename-role", role("乙", fmt.Sprint(asset.ID)))
	if err != nil || len(updated.Changes) != 1 || !strings.Contains(string(updated.Changes[0].Fields["data.character"]), "/api/v1/canvas-media/") {
		t.Fatalf("成员编辑应保留现有画布素材并投影地址: %+v, %v", updated, err)
	}
	conflict, err := commit(owner, created.Revision, "conflict-role", role("丙", fmt.Sprint(asset.ID)))
	if err != nil || len(conflict.Conflicts) != 1 || len(conflict.Changes) != 0 {
		t.Fatalf("角色字段过期应进入冲突处理: %+v, %v", conflict, err)
	}
	if _, err := commit(editor, updated.Revision, "missing-asset", role("丙", "999999")); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("画布外素材应被拒绝: %v", err)
	}
	if _, err := commit(editor, updated.Revision, "wrong-type", repository.CanvasCollaborationChange{Kind: "node", ID: "role",
		Fields: map[string]json.RawMessage{"data.storyboard": json.RawMessage(`{"shots":[]}`)}}); !errors.Is(err, repository.ErrCanvasCollaborationInvalidBatch) {
		t.Fatalf("结构化字段必须匹配节点类型: %v", err)
	}
	undo, err := repo.Undo(ctx, editor, project.ID, repository.CanvasCollaborationUndo{Epoch: project.CollaborationEpoch, OpID: "undo-role-rename"})
	if err != nil || len(undo.Changes) != 1 || !strings.Contains(string(undo.Changes[0].Fields["data.character"]), `"name":"甲"`) {
		t.Fatalf("成员应能撤销角色编辑: %+v, %v", undo, err)
	}
	replay, err := repo.Replay(ctx, editor, project.ID, project.CollaborationEpoch, project.Revision, 20)
	if err != nil || len(replay.Events) != 3 {
		t.Fatalf("成员应能追赶创建、编辑和撤销版本: %+v, %v", replay, err)
	}
	encoded, err := json.Marshal(replay.Events)
	if err != nil || strings.Contains(string(encoded), "owner/private.png") || !strings.Contains(string(encoded), "/api/v1/canvas-media/") {
		t.Fatalf("所有回放事件应使用画布素材地址: %s, %v", encoded, err)
	}
}

func TestCanvasCollaborationStructuredSceneAndShots(t *testing.T) {
	_, repo, owner, _, _, project := newCollaborationFixture(t)
	commit := func(base uint64, op string, changes ...repository.CanvasCollaborationChange) (*repository.CanvasCollaborationCommit, error) {
		return repo.Commit(t.Context(), owner, project.ID, repository.CanvasCollaborationBatch{Epoch: project.CollaborationEpoch,
			BaseRevision: base, OpID: op, Changes: changes})
	}
	created, err := commit(project.Revision, "create-scene-and-shots",
		repository.CanvasCollaborationChange{Kind: "node", ID: "stage", Action: "create", Value: json.RawMessage(`{"position":{"x":0,"y":0},"data":{"kind":"director","label":"导演台","scene":{"version":1,"duration":4,"activeCameraId":"","characters":[],"cameras":[],"props":[]}}}`)},
		repository.CanvasCollaborationChange{Kind: "node", ID: "board", Action: "create", Value: json.RawMessage(`{"position":{"x":10,"y":20},"data":{"kind":"storyboard","label":"分镜表","storyboard":{"shots":[]}}}`)},
	)
	if err != nil || len(created.Changes) != 2 {
		t.Fatalf("应能创建分镜表和导演台: %+v, %v", created, err)
	}
	moved, err := commit(created.Revision, "move-stage", repository.CanvasCollaborationChange{Kind: "node", ID: "stage", Fields: map[string]json.RawMessage{
		"position": json.RawMessage(`{"x":30,"y":40}`),
	}})
	if err != nil || len(moved.Changes) != 1 {
		t.Fatalf("应能移动导演台: %+v, %v", moved, err)
	}
	updated, err := commit(created.Revision, "edit-scene-and-shots",
		repository.CanvasCollaborationChange{Kind: "node", ID: "stage", Fields: map[string]json.RawMessage{
			"data.scene": json.RawMessage(`{"version":1,"duration":6,"activeCameraId":"","characters":[],"cameras":[],"props":[{"id":"wall","name":"墙","shape":"wall","x":0,"z":0,"yaw":0,"w":2,"h":2,"d":0.1}]}`),
		}},
		repository.CanvasCollaborationChange{Kind: "node", ID: "board", Fields: map[string]json.RawMessage{
			"data.storyboard": json.RawMessage(`{"shots":[{"id":"shot","scene":"开场","shot":"","camera":"","action":"","dialogue":"","duration":3}]}`),
		}},
	)
	if err != nil || len(updated.Changes) != 2 || updated.Revision != moved.Revision+1 {
		t.Fatalf("场景编辑应与并发移动合并，分镜应同批写入: %+v, %v", updated, err)
	}
}

func TestCanvasCollaborationShotConflicts(t *testing.T) {
	_, repo, owner, _, _, project := newCollaborationFixture(t)
	commit := func(base uint64, op string, change repository.CanvasCollaborationChange) *repository.CanvasCollaborationCommit {
		t.Helper()
		result, err := repo.Commit(t.Context(), owner, project.ID, repository.CanvasCollaborationBatch{
			Epoch: project.CollaborationEpoch, BaseRevision: base, OpID: op, Changes: []repository.CanvasCollaborationChange{change},
		})
		if err != nil {
			t.Fatalf("提交 %s 失败: %v", op, err)
		}
		return result
	}
	created := commit(project.Revision, "create-board", repository.CanvasCollaborationChange{Kind: "node", ID: "board", Action: "create",
		Value: json.RawMessage(`{"position":{"x":0,"y":0},"data":{"kind":"storyboard","label":"分镜","storyboard":{"shots":[{"id":"first","scene":"外景"},{"id":"second","scene":"室内"}]}}}`)})
	base := created.Revision
	edit := func(op, value string) *repository.CanvasCollaborationCommit {
		t.Helper()
		return commit(base, op, repository.CanvasCollaborationChange{Kind: "node", ID: "board", Action: "storyboard-shot", Value: json.RawMessage(value)})
	}
	web := commit(base, "web-scene", repository.CanvasCollaborationChange{Kind: "node", ID: "board", Fields: map[string]json.RawMessage{
		"data.storyboard": json.RawMessage(`{"shots":[{"id":"first","scene":"雨夜"},{"id":"second","scene":"室内"}]}`),
	}})
	otherField := edit("cli-dialogue", `{"action":"set","id":"first","fields":{"dialogue":"来了"}}`)
	otherShot := edit("cli-second", `{"action":"set","id":"second","fields":{"scene":"车内"}}`)
	if len(otherField.Changes) != 1 || len(otherShot.Changes) != 1 || otherShot.Revision != web.Revision+2 {
		t.Fatalf("同镜头的不同字段和不同镜头应合并: %+v %+v", otherField, otherShot)
	}
	if fields := otherShot.Changes[0].Fields; !jsonEqual(fields["data.storyboard"], `{"shots":[{"id":"first","scene":"雨夜","dialogue":"来了"},{"id":"second","scene":"车内"}]}`) {
		t.Fatalf("回放应保留全部已确认编辑: %s", fields["data.storyboard"])
	}
	for _, attempt := range []struct{ op, value string }{
		{"stale-field", `{"action":"set","id":"first","fields":{"scene":"清晨"}}`},
		{"stale-delete", `{"action":"rm","id":"first"}`},
	} {
		if result := edit(attempt.op, attempt.value); len(result.Conflicts) != 1 || result.Revision != otherShot.Revision {
			t.Fatalf("%s 应返回冲突并保留当前版本: %+v", attempt.op, result)
		}
	}
	moved := edit("cli-move", `{"action":"move","id":"first","index":2}`)
	if len(moved.Changes) != 1 {
		t.Fatalf("字段编辑不应阻止重排: %+v", moved)
	}
	if result := edit("stale-move", `{"action":"move","id":"second","index":2}`); len(result.Conflicts) != 1 {
		t.Fatalf("过期顺序应冲突: %+v", result)
	}
}

func TestCanvasCollaborationDirectorObjectConflicts(t *testing.T) {
	_, repo, owner, _, _, project := newCollaborationFixture(t)
	commit := func(base uint64, op, action, value string) *repository.CanvasCollaborationCommit {
		t.Helper()
		change := repository.CanvasCollaborationChange{Kind: "node", ID: "stage", Action: action, Value: json.RawMessage(value)}
		if action == "" {
			change.Value = nil
			change.Fields = map[string]json.RawMessage{"data.scene": json.RawMessage(value)}
		}
		result, err := repo.Commit(t.Context(), owner, project.ID, repository.CanvasCollaborationBatch{
			Epoch: project.CollaborationEpoch, BaseRevision: base, OpID: op, Changes: []repository.CanvasCollaborationChange{change},
		})
		if err != nil {
			t.Fatalf("提交 %s: %v", op, err)
		}
		return result
	}
	const scene = `{"version":1,"duration":4,"activeCameraId":"","characters":[],"cameras":[],"props":[{"id":"wall","name":"墙","x":0},{"id":"table","name":"桌"}]}`
	const webScene = `{"version":1,"duration":4,"activeCameraId":"","characters":[],"cameras":[],"props":[{"id":"wall","name":"门","x":0},{"id":"table","name":"桌"}]}`
	created := commit(project.Revision, "create-stage", "create", `{"position":{"x":0,"y":0},"data":{"kind":"director","label":"导演台","scene":`+scene+`}}`)
	base := created.Revision
	web := commit(base, "web-edit-stage", "", webScene)
	otherField := commit(base, "cli-coordinate", "director-object", `{"action":"set","collection":"props","id":"wall","fields":{"x":3}}`)
	otherObject := commit(base, "cli-prop", "director-object", `{"action":"set","collection":"props","id":"table","fields":{"name":"椅子"}}`)
	want := `{"version":1,"duration":4,"activeCameraId":"","characters":[],"cameras":[],"props":[{"id":"wall","name":"门","x":3},{"id":"table","name":"椅子"}]}`
	if len(otherField.Changes) != 1 || len(otherObject.Changes) != 1 || otherObject.Revision != web.Revision+2 || !jsonEqual(otherObject.Changes[0].Fields["data.scene"], want) {
		t.Fatalf("不同对象或属性应保留网页和 CLI 的编辑: %+v %+v", otherField, otherObject)
	}
	for _, attempt := range []struct{ op, value string }{
		{"stale-name", `{"action":"set","collection":"props","id":"wall","fields":{"name":"新墙"}}`},
		{"stale-remove", `{"action":"rm","collection":"props","id":"table"}`},
	} {
		if result := commit(base, attempt.op, "director-object", attempt.value); len(result.Conflicts) != 1 || result.Revision != otherObject.Revision {
			t.Fatalf("%s 应返回冲突: %+v", attempt.op, result)
		}
	}
}
