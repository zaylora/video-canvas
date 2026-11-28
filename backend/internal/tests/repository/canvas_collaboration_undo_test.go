package repository_test

import (
	"encoding/json"
	"errors"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/repository"
)

func TestCanvasCollaborationUndoExpectedRevision(t *testing.T) {
	_, repo, owner, _, _, project := newCollaborationFixture(t)
	first, err := repo.Commit(t.Context(), owner, project.ID, collaborationEdit(project, "first", "data.prompt", `"第一稿"`))
	if err != nil {
		t.Fatal(err)
	}
	project.Revision = first.Revision
	second, err := repo.Commit(t.Context(), owner, project.ID, collaborationEdit(project, "second", "data.prompt", `"第二稿"`))
	if err != nil {
		t.Fatal(err)
	}
	request := repository.CanvasCollaborationUndo{Epoch: project.CollaborationEpoch, OpID: "explicit-undo", ExpectedRevision: first.Revision}
	conflict, err := repo.Undo(t.Context(), owner, project.ID, request)
	if err != nil || len(conflict.Conflicts) != 1 || len(conflict.Changes) != 0 || conflict.Conflicts[0].Revision != second.Revision {
		t.Fatalf("过期的撤销目标必须在事务中拒绝: %+v, %v", conflict, err)
	}
	request.ExpectedRevision = second.Revision
	if _, err := repo.Undo(t.Context(), owner, project.ID, request); !errors.Is(err, repository.ErrCanvasCollaborationOpID) {
		t.Fatalf("同一编号不得替换撤销目标: %v", err)
	}
	request.OpID = "second-undo"
	applied, err := repo.Undo(t.Context(), owner, project.ID, request)
	if err != nil || len(applied.Changes) != 1 || applied.Undoes != second.Revision {
		t.Fatalf("应撤销当前确认目标: %+v, %v", applied, err)
	}
}

func TestCanvasCollaborationUndoRedoDeletion(t *testing.T) {
	fixture := `{"nodes":[{"id":"group","type":"group"},{"id":"node","type":"canvas","parentId":"group"},{"id":"other","type":"canvas"}],"edges":[{"id":"link","source":"node","target":"other"}]}`
	_, repo, owner, _, _, project := newCollaborationFixtureWithPayload(t, fixture)
	ctx := t.Context()
	batch := repository.CanvasCollaborationBatch{Epoch: project.CollaborationEpoch, BaseRevision: project.Revision, OpID: "delete-node",
		Changes: []repository.CanvasCollaborationChange{{Kind: "node", ID: "node", Action: "delete"}, {Kind: "group", ID: "group", Action: "delete"}}}
	if result, err := repo.Commit(ctx, owner, project.ID, batch); err != nil || len(result.Changes) != 3 {
		t.Fatalf("删除失败: %+v %v", result, err)
	}
	undo := repository.CanvasCollaborationUndo{Epoch: project.CollaborationEpoch, OpID: "restore-node"}
	result, err := repo.Undo(ctx, owner, project.ID, undo)
	if err != nil || len(result.Changes) != 3 {
		t.Fatalf("应恢复原分组、节点和级联删除的连线: %+v %v", result, err)
	}
	for index, kind := range []string{"group", "node", "edge"} {
		if result.Changes[index].Kind != kind || result.Changes[index].Action != "create" {
			t.Fatalf("恢复顺序错误: %+v", result.Changes)
		}
	}
	live, _, err := repo.Snapshot(ctx, owner, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	redo := repository.CanvasCollaborationUndo{Epoch: project.CollaborationEpoch, OpID: "redo-delete", Redo: true}
	result, err = repo.Undo(ctx, owner, project.ID, redo)
	if err != nil || len(result.Changes) != 3 || result.Changes[0].Action != "delete" {
		t.Fatalf("应再次删除组、节点和连线: %+v %v", result, err)
	}
	historical, err := repo.HistoricalSnapshot(ctx, owner, project.ID, live.CollaborationEpoch, live.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if !jsonEqual(live.PayloadJSON, string(historical.PayloadJSON)) {
		t.Fatalf("实时快照与历史回放应按同一顺序恢复节点:\n%s\n%s", live.PayloadJSON, historical.PayloadJSON)
	}
	undo.OpID = "restore-again"
	result, err = repo.Undo(ctx, owner, project.ID, undo)
	if err != nil || len(result.Changes) != 3 || result.Changes[0].Action != "create" {
		t.Fatalf("重做结果仍可撤销: %+v %v", result, err)
	}
}

func TestCanvasCollaborationRedoPreservesConcurrentEdits(t *testing.T) {
	db, repo, owner, editor, _, project := newCollaborationFixture(t)
	ctx := t.Context()
	if err := repository.NewCanvasShareRepository(db).GrantRole(ctx, owner, project.ID, editor, model.CanvasRoleEditor); err != nil {
		t.Fatal(err)
	}
	first := collaborationEdit(project, "first-edit", "data.prompt", `"初稿"`)
	first.Changes[0].Fields["data.label"] = json.RawMessage(`"第一版"`)
	started, err := repo.Commit(ctx, editor, project.ID, first)
	if err != nil {
		t.Fatal(err)
	}
	second := collaborationEdit(project, "second-edit", "data.prompt", `"第二稿"`)
	second.BaseRevision = started.Revision
	if _, err := repo.Commit(ctx, editor, project.ID, second); err != nil {
		t.Fatal(err)
	}
	var undone *repository.CanvasCollaborationCommit
	for _, op := range []string{"undo-second", "undo-first"} {
		undone, err = repo.Undo(ctx, editor, project.ID, repository.CanvasCollaborationUndo{Epoch: project.CollaborationEpoch, OpID: op})
		if err != nil || len(undone.Changes) != 1 {
			t.Fatalf("连续撤销失败: %+v %v", undone, err)
		}
	}
	other := collaborationEdit(project, "teammate-rename", "data.label", `"队友标题"`)
	other.BaseRevision = undone.Revision
	if _, err := repo.Commit(ctx, owner, project.ID, other); err != nil {
		t.Fatal(err)
	}
	for index, op := range []string{"redo-first", "redo-second"} {
		result, err := repo.Undo(ctx, editor, project.ID, repository.CanvasCollaborationUndo{Epoch: project.CollaborationEpoch, OpID: op, Redo: true})
		if err != nil || len(result.Changes) != 1 || len(result.Changes[0].Fields) != 1 {
			t.Fatalf("重做只应改提示词: %+v %v", result, err)
		}
		want := []string{`"初稿"`, `"第二稿"`}[index]
		if !jsonEqual(result.Changes[0].Fields["data.prompt"], want) {
			t.Fatalf("重做顺序错误: %+v", result)
		}
	}
}

func TestCanvasCollaborationPersonalUndo(t *testing.T) {
	db, repo, owner, editor, _, project := newCollaborationFixture(t)
	ctx := t.Context()
	shares := repository.NewCanvasShareRepository(db)
	if err := shares.GrantRole(ctx, owner, project.ID, editor, model.CanvasRoleEditor); err != nil {
		t.Fatal(err)
	}
	initial := collaborationEdit(project, "original", "data.label", `"起点"`)
	initial.Changes[0].Fields["data.prompt"] = json.RawMessage(`"初稿"`)
	commit := func(actor uint64, batch repository.CanvasCollaborationBatch) {
		t.Helper()
		if result, err := repo.Commit(ctx, actor, project.ID, batch); err != nil || len(result.Changes) != 1 {
			t.Fatalf("提交失败: %+v, %v", result, err)
		}
	}
	commit(owner, initial)
	edit := collaborationEdit(project, "editor-change", "data.label", `"编辑者标题"`)
	edit.BaseRevision = 9
	edit.Changes[0].Fields["data.prompt"] = json.RawMessage(`"第二稿"`)
	commit(editor, edit)
	newer := collaborationEdit(project, "changed-after-editor", "data.label", `"所有者新标题"`)
	newer.BaseRevision = 10
	commit(owner, newer)
	request := repository.CanvasCollaborationUndo{Epoch: project.CollaborationEpoch, OpID: "undo-editor"}
	result, err := repo.Undo(ctx, editor, project.ID, request)
	if err != nil || result.Revision != 12 || len(result.Changes) != 1 || len(result.Skipped) != 1 || result.Skipped[0].Field != "data.label" {
		t.Fatalf("撤销应仅回退仍匹配的提示词: %+v, %v", result, err)
	}
	if !jsonEqual(result.Changes[0].Fields["data.prompt"], `"初稿"`) {
		t.Fatalf("提示词应还原到编辑前: %+v", result)
	}
	repeat, err := repo.Undo(ctx, editor, project.ID, request)
	if err != nil || repeat.Revision != result.Revision || len(repeat.Skipped) != 1 {
		t.Fatalf("重复撤销应返回原收据: %+v, %v", repeat, err)
	}
	if err := shares.GrantRole(ctx, owner, project.ID, editor, model.CanvasRoleViewer); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.Undo(ctx, editor, project.ID, request); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("降级后应拒绝撤销: %v", err)
	}
}

func TestCanvasCollaborationStructuredPersonalUndo(t *testing.T) {
	db, repo, owner, editor, _, project := newCollaborationFixtureWithPayload(t, `{"nodes":[{"id":"board","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"storyboard","label":"分镜","storyboard":{"shots":[{"id":"first","scene":"外景","action":"走","dialogue":"初稿"}]}}}],"edges":[]}`)
	ctx := t.Context()
	if err := repository.NewCanvasShareRepository(db).GrantRole(ctx, owner, project.ID, editor, model.CanvasRoleEditor); err != nil {
		t.Fatal(err)
	}
	edit := func(actor, base uint64, op, value string) uint64 {
		t.Helper()
		result, err := repo.Commit(ctx, actor, project.ID, repository.CanvasCollaborationBatch{
			Epoch: project.CollaborationEpoch, BaseRevision: base, OpID: op,
			Changes: []repository.CanvasCollaborationChange{{Kind: "node", ID: "board", Action: "storyboard-shot", Value: json.RawMessage(value)}},
		})
		if err != nil || len(result.Changes) != 1 {
			t.Fatalf("提交 %s: %+v %v", op, result, err)
		}
		return result.Revision
	}
	change := edit(editor, project.Revision, "editor-shot", `{"action":"set","id":"first","fields":{"scene":"室内","dialogue":"编辑稿"}}`)
	edit(owner, change, "owner-shot", `{"action":"set","id":"first","fields":{"scene":"雨夜","action":"奔跑"}}`)
	undo, err := repo.Undo(ctx, editor, project.ID, repository.CanvasCollaborationUndo{Epoch: project.CollaborationEpoch, OpID: "undo-shot", ExpectedRevision: change})
	if err != nil || len(undo.Changes) != 1 || len(undo.Skipped) != 1 ||
		!jsonEqual(undo.Changes[0].Fields["data.storyboard"], `{"shots":[{"id":"first","scene":"雨夜","action":"奔跑","dialogue":"初稿"}]}`) {
		t.Fatalf("撤销只回退编辑者仍拥有的对白版本: %+v %v", undo, err)
	}
	redo, err := repo.Undo(ctx, editor, project.ID, repository.CanvasCollaborationUndo{Epoch: project.CollaborationEpoch, OpID: "redo-shot", ExpectedRevision: undo.Revision, Redo: true})
	if err != nil || len(redo.Changes) != 1 || len(redo.Skipped) != 0 ||
		!jsonEqual(redo.Changes[0].Fields["data.storyboard"], `{"shots":[{"id":"first","scene":"雨夜","action":"奔跑","dialogue":"编辑稿"}]}`) {
		t.Fatalf("重做只恢复实际撤销的对白: %+v %v", redo, err)
	}
}

func TestCanvasCollaborationDirectorPersonalUndo(t *testing.T) {
	const scene = `{"version":1,"duration":4,"activeCameraId":"","characters":[],"cameras":[],"props":[{"id":"wall","name":"墙","x":0}]}`
	db, repo, owner, editor, _, project := newCollaborationFixtureWithPayload(t, `{"nodes":[{"id":"stage","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"director","label":"导演台","scene":`+scene+`}}],"edges":[]}`)
	ctx := t.Context()
	if err := repository.NewCanvasShareRepository(db).GrantRole(ctx, owner, project.ID, editor, model.CanvasRoleEditor); err != nil {
		t.Fatal(err)
	}
	edit := func(actor, base uint64, op, value string) uint64 {
		t.Helper()
		result, err := repo.Commit(ctx, actor, project.ID, repository.CanvasCollaborationBatch{
			Epoch: project.CollaborationEpoch, BaseRevision: base, OpID: op,
			Changes: []repository.CanvasCollaborationChange{{Kind: "node", ID: "stage", Action: "director-object", Value: json.RawMessage(value)}},
		})
		if err != nil || len(result.Changes) != 1 {
			t.Fatalf("提交 %s: %+v %v", op, result, err)
		}
		return result.Revision
	}
	change := edit(editor, project.Revision, "editor-wall", `{"action":"set","collection":"props","id":"wall","fields":{"x":4,"locked":true}}`)
	edit(owner, change, "owner-wall", `{"action":"set","collection":"props","id":"wall","fields":{"name":"大门"}}`)
	want := `{"version":1,"duration":4,"activeCameraId":"","characters":[],"cameras":[],"props":[{"id":"wall","name":"大门","x":0}]}`
	undo, err := repo.Undo(ctx, editor, project.ID, repository.CanvasCollaborationUndo{Epoch: project.CollaborationEpoch, OpID: "undo-wall", ExpectedRevision: change})
	if err != nil || len(undo.Changes) != 1 || len(undo.Skipped) != 0 || !jsonEqual(undo.Changes[0].Fields["data.scene"], want) {
		t.Fatalf("撤销应保留名称并还原之前缺失的可选字段: %+v %v", undo, err)
	}
}

func TestCanvasCollaborationUndoGrouping(t *testing.T) {
	_, repo, owner, _, _, project := newCollaborationFixture(t)
	batch := collaborationEdit(project, "move-to-group", "parentId", `"new-group"`)
	batch.Changes[0].Fields["position"] = json.RawMessage(`{"x":30,"y":50}`)
	batch.Changes = append(batch.Changes, repository.CanvasCollaborationChange{
		Kind: "group", ID: "new-group", Action: "create",
		Value: json.RawMessage(`{"position":{"x":10,"y":20},"data":{"label":"新组"}}`),
	})
	if result, err := repo.Commit(t.Context(), owner, project.ID, batch); err != nil || len(result.Changes) != 2 {
		t.Fatalf("创建新组并移动节点失败: %+v %v", result, err)
	}
	result, err := repo.Undo(t.Context(), owner, project.ID, repository.CanvasCollaborationUndo{Epoch: project.CollaborationEpoch, OpID: "undo-grouping"})
	if err != nil || len(result.Changes) != 2 || len(result.Skipped) != 0 || !jsonEqual(result.Changes[0].Fields["parentId"], `"group"`) || result.Changes[1].Action != "delete" {
		t.Fatalf("应还原节点归属并删除无人使用的新组: %+v %v", result, err)
	}
}

func TestCanvasCollaborationConsecutiveUndoPreservesOtherEdits(t *testing.T) {
	for _, scenario := range []struct {
		name        string
		foreignEdit bool
	}{{"same-author", false}, {"intervening-editor", true}} {
		t.Run(scenario.name, func(t *testing.T) {
			db, repo, owner, editor, _, project := newCollaborationFixture(t)
			ctx := t.Context()
			if err := repository.NewCanvasShareRepository(db).GrantRole(ctx, owner, project.ID, editor, model.CanvasRoleEditor); err != nil {
				t.Fatal(err)
			}
			edit := func(actor uint64, id, prompt string) {
				t.Helper()
				result, err := repo.Commit(ctx, actor, project.ID, collaborationEdit(project, id, "data.prompt", prompt))
				if err != nil || len(result.Changes) != 1 {
					t.Fatalf("提示词应保存 (%+v, %v)", result, err)
				}
				project.Revision = result.Revision
			}
			edit(owner, "first-prompt", `"第一稿"`)
			previous := `"第一稿"`
			if scenario.foreignEdit {
				previous = `"另一成员的提示词"`
				edit(editor, "foreign-prompt", previous)
			}
			edit(owner, "second-prompt", `"第二稿"`)
			lastEdit := *project
			undo := func(id string) *repository.CanvasCollaborationCommit {
				t.Helper()
				result, err := repo.Undo(ctx, owner, project.ID, repository.CanvasCollaborationUndo{Epoch: project.CollaborationEpoch, OpID: id})
				if err != nil {
					t.Fatal(err)
				}
				return result
			}
			first := undo("undo-second-prompt")
			if len(first.Changes) != 1 || !jsonEqual(first.Changes[0].Fields["data.prompt"], previous) || len(first.Changes[0].FieldBases) > 0 {
				t.Fatalf("第一次撤销应还原前一稿 (%+v)", first)
			}
			second := undo("undo-first-prompt")
			if scenario.foreignEdit {
				if len(second.Changes) != 0 || len(second.Skipped) != 1 || second.Skipped[0].Field != "data.prompt" {
					t.Fatalf("第二次撤销应保留其他成员的编辑 (%+v)", second)
				}
			} else if len(second.Changes) != 1 || len(second.Skipped) != 0 || string(second.Changes[0].Fields["data.prompt"]) != "null" {
				t.Fatalf("两次个人撤销应还原到初始提示词 (%+v)", second)
			}
			stale, err := repo.Commit(ctx, editor, project.ID, collaborationEdit(&lastEdit, "stale-after-undo", "data.prompt", `"旧版本上的编辑"`))
			if err != nil || len(stale.Conflicts) != 1 || stale.Conflicts[0].Revision != second.Revision {
				t.Fatalf("断线编辑仍应按撤销后的确认版本检测冲突 (%+v, %v)", stale, err)
			}
		})
	}
}

func TestCanvasCollaborationUndoReceiptRejectsNewEpoch(t *testing.T) {
	_, repo, owner, _, _, project := newCollaborationFixture(t)
	ctx := t.Context()
	request := repository.CanvasCollaborationUndo{Epoch: project.CollaborationEpoch, OpID: "undo-old-epoch"}
	if _, err := repo.Undo(ctx, owner, project.ID, request); err != nil {
		t.Fatal(err)
	}
	closed, err := repo.SetActive(ctx, owner, project.ID, project.Revision, false)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := repo.SetActive(ctx, owner, project.ID, closed.Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	request.Epoch = opened.CollaborationEpoch
	if _, err := repo.Undo(ctx, owner, project.ID, request); !errors.Is(err, repository.ErrCanvasCollaborationOpID) {
		t.Fatalf("新会话的请求应拒绝沿用旧会话的撤销操作 ID (%v)", err)
	}
}

func TestCanvasCollaborationUndoCreationKeepsOthersReferences(t *testing.T) {
	db, repo, owner, editor, _, project := newCollaborationFixture(t)
	ctx := t.Context()
	if err := repository.NewCanvasShareRepository(db).GrantRole(ctx, owner, project.ID, editor, model.CanvasRoleEditor); err != nil {
		t.Fatal(err)
	}
	batch := repository.CanvasCollaborationBatch{OpID: "create-node", Epoch: project.CollaborationEpoch, BaseRevision: 8,
		Changes: []repository.CanvasCollaborationChange{{Kind: "node", ID: "another", Action: "create", Value: json.RawMessage(`{"position":{"x":0,"y":0},"data":{"kind":"image","label":"新节点"}}`)}}}
	if _, err := repo.Commit(ctx, owner, project.ID, batch); err != nil {
		t.Fatal(err)
	}
	batch.OpID, batch.BaseRevision = "connect-node", 9
	batch.Changes = []repository.CanvasCollaborationChange{{Kind: "edge", ID: "another-link", Action: "create", Value: json.RawMessage(`{"source":"node","target":"another"}`)}}
	if _, err := repo.Commit(ctx, editor, project.ID, batch); err != nil {
		t.Fatal(err)
	}
	result, err := repo.Undo(ctx, owner, project.ID, repository.CanvasCollaborationUndo{Epoch: project.CollaborationEpoch, OpID: "undo-created-node"})
	if err != nil || len(result.Changes) != 0 || len(result.Skipped) != 1 || result.Skipped[0].ID != "another" {
		t.Fatalf("不能撤销被别人接线的节点: %+v %v", result, err)
	}
}

func TestCanvasCollaborationUndoCreationAfterFieldUndo(t *testing.T) {
	for _, scenario := range []struct {
		name        string
		foreignEdit bool
	}{{"restored-creation", false}, {"surviving-editor-change", true}} {
		t.Run(scenario.name, func(t *testing.T) {
			db, repo, owner, editor, _, project := newCollaborationFixture(t)
			ctx := t.Context()
			if err := repository.NewCanvasShareRepository(db).GrantRole(ctx, owner, project.ID, editor, model.CanvasRoleEditor); err != nil {
				t.Fatal(err)
			}
			commit := func(actor uint64, id string, change repository.CanvasCollaborationChange) {
				t.Helper()
				result, err := repo.Commit(ctx, actor, project.ID, repository.CanvasCollaborationBatch{
					OpID: id, Epoch: project.CollaborationEpoch, BaseRevision: project.Revision,
					Changes: []repository.CanvasCollaborationChange{change},
				})
				if err != nil || len(result.Changes) != 1 {
					t.Fatalf("应确认提交 %s: %+v %v", id, result, err)
				}
				project.Revision = result.Revision
			}
			commit(owner, "create", repository.CanvasCollaborationChange{
				Kind: "node", ID: "another", Action: "create",
				Value: json.RawMessage(`{"position":{"x":0,"y":0},"data":{"kind":"image","label":"新节点"}}`),
			})
			if scenario.foreignEdit {
				commit(editor, "editor-prompt", repository.CanvasCollaborationChange{Kind: "node", ID: "another",
					Fields: map[string]json.RawMessage{"data.prompt": json.RawMessage(`"另一位成员的提示词"`)}})
			}
			commit(owner, "owner-edit", repository.CanvasCollaborationChange{Kind: "node", ID: "another",
				Fields: map[string]json.RawMessage{"data.prompt": json.RawMessage(`"我的提示词"`), "position": json.RawMessage(`{"x":40,"y":50}`)}})
			undo := func(opID string) *repository.CanvasCollaborationCommit {
				t.Helper()
				result, err := repo.Undo(ctx, owner, project.ID, repository.CanvasCollaborationUndo{Epoch: project.CollaborationEpoch, OpID: opID})
				if err != nil {
					t.Fatal(err)
				}
				return result
			}
			first := undo("undo-edit")
			if len(first.Changes) != 1 || len(first.Changes[0].Fields) != 2 || len(first.Skipped) != 0 {
				t.Fatalf("应先撤销自己的提示词和位置修改: %+v", first)
			}
			assertUndoCreationAfterFieldUndo(t, undo("undo-create"), scenario.foreignEdit)
		})
	}
}

func assertUndoCreationAfterFieldUndo(t *testing.T, result *repository.CanvasCollaborationCommit, foreignEdit bool) {
	t.Helper()
	if foreignEdit {
		if len(result.Changes) != 0 || len(result.Skipped) != 1 || result.Skipped[0].ID != "another" {
			t.Fatalf("不应删除仍保留他人提示词的节点: %+v", result)
		}
		return
	}
	if len(result.Changes) != 1 || result.Changes[0].ID != "another" || result.Changes[0].Action != "delete" || len(result.Skipped) != 0 {
		t.Fatalf("还原自己的字段修改后应能撤销创建: %+v", result)
	}
}
