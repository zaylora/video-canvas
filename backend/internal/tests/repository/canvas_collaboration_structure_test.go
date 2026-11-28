package repository_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/repository"
)

func TestCanvasCollaborationStructure(t *testing.T) {
	_, repo, owner, _, _, project := newCollaborationFixture(t)
	ctx := t.Context()
	add := repository.CanvasCollaborationBatch{Epoch: project.CollaborationEpoch, BaseRevision: project.Revision, OpID: "create",
		Changes: []repository.CanvasCollaborationChange{
			{Kind: "edge", ID: "line", Action: "create", Value: json.RawMessage(`{"source":"node","target":"result"}`)},
			{Kind: "node", ID: "result", Action: "create", Value: json.RawMessage(`{"position":{"x":4,"y":6},"data":{"kind":"image","label":"结果"}}`)},
		}}
	if result, err := repo.Commit(ctx, owner, project.ID, add); err != nil || result.Revision != 9 || len(result.Changes) != 2 {
		t.Fatalf("同批连线应可引用同批节点: %+v %v", result, err)
	}
	deleteBatch := repository.CanvasCollaborationBatch{Epoch: project.CollaborationEpoch, BaseRevision: 8, OpID: "stale-delete",
		Changes: []repository.CanvasCollaborationChange{{Kind: "node", ID: "node", Action: "delete"}}}
	if result, err := repo.Commit(ctx, owner, project.ID, deleteBatch); err != nil || len(result.Conflicts) != 1 || result.Revision != 9 {
		t.Fatalf("已新增相关连线时旧版本不能删除节点: %+v %v", result, err)
	}
	deleteBatch.BaseRevision, deleteBatch.OpID = 9, "delete"
	if result, err := repo.Commit(ctx, owner, project.ID, deleteBatch); err != nil || len(result.Changes) != 2 || result.Revision != 10 {
		t.Fatalf("删除应原子移除节点及相连边: %+v %v", result, err)
	}
	add.BaseRevision, add.OpID = 10, "recreate"
	add.Changes = []repository.CanvasCollaborationChange{{Kind: "node", ID: "node", Action: "create", Value: add.Changes[1].Value}}
	if result, err := repo.Commit(ctx, owner, project.ID, add); err != nil || len(result.Conflicts) != 1 {
		t.Fatalf("删除后的稳定 ID 应保留墓碑: %+v %v", result, err)
	}
	add.OpID = "create-missing-endpoint"
	add.Changes = []repository.CanvasCollaborationChange{{Kind: "edge", ID: "other", Action: "create", Value: json.RawMessage(`{"source":"node","target":"result"}`)}}
	if _, err := repo.Commit(ctx, owner, project.ID, add); !errors.Is(err, repository.ErrCanvasCollaborationInvalidBatch) {
		t.Fatalf("连线的端点必须存在: %v", err)
	}
}

func TestCanvasCollaborationDeleteAfterOwnEdit(t *testing.T) {
	for _, relatedEdge := range []bool{false, true} {
		t.Run(fmt.Sprintf("relatedEdge=%t", relatedEdge), func(t *testing.T) {
			db, repo, owner, editor, _, project := newCollaborationFixtureWithPayload(t,
				`{"nodes":[{"id":"node","type":"canvas"},{"id":"other","type":"canvas"}],"edges":[]}`)
			ctx := t.Context()
			if err := repository.NewCanvasShareRepository(db).GrantRole(ctx, owner, project.ID, editor, model.CanvasRoleEditor); err != nil {
				t.Fatal(err)
			}
			if relatedEdge {
				link := repository.CanvasCollaborationBatch{Epoch: project.CollaborationEpoch, BaseRevision: project.Revision, OpID: "other-link",
					Changes: []repository.CanvasCollaborationChange{{Kind: "edge", ID: "line", Action: "create", Value: json.RawMessage(`{"source":"node","target":"other"}`)}}}
				if _, err := repo.Commit(ctx, editor, project.ID, link); err != nil {
					t.Fatal(err)
				}
			}
			edit, err := repo.Commit(ctx, owner, project.ID, collaborationEdit(project, "local-edit", "data.label", `"编辑后删除"`))
			if err != nil || len(edit.Changes) != 1 {
				t.Fatalf("应确认前序编辑: %+v %v", edit, err)
			}
			batch := repository.CanvasCollaborationBatch{Epoch: project.CollaborationEpoch, BaseRevision: project.Revision, OpID: "local-delete",
				Changes: []repository.CanvasCollaborationChange{{Kind: "node", ID: "node", Action: "delete", RecordBase: edit.Revision + 1}}}
			if _, err := repo.Commit(ctx, owner, project.ID, batch); !errors.Is(err, repository.ErrCanvasCollaborationSession) {
				t.Fatalf("删除不能使用未来的记录版本: %v", err)
			}
			batch.Changes[0].RecordBase = edit.Revision
			result, err := repo.Commit(ctx, owner, project.ID, batch)
			if err != nil {
				t.Fatal(err)
			}
			if relatedEdge {
				if len(result.Conflicts) != 1 || result.Conflicts[0].ID != "line" || len(result.Changes) != 0 || result.Revision != edit.Revision {
					t.Fatalf("其他成员的连线应阻止节点删除: %+v", result)
				}
			} else if len(result.Conflicts) != 0 || len(result.Changes) != 1 || result.Changes[0].Action != "delete" || result.Changes[0].RecordBase != 0 {
				t.Fatalf("应能删除已确认前序编辑的节点: %+v", result)
			}
		})
	}
}

func TestCanvasCollaborationConcurrentDuplicateEdge(t *testing.T) {
	for _, winnerID := range []string{"a-edge", "z-edge"} {
		t.Run(winnerID, func(t *testing.T) {
			db, repo, owner, editor, _, project := newCollaborationFixtureWithPayload(t,
				`{"nodes":[{"id":"node","type":"canvas"},{"id":"other","type":"canvas"}],"edges":[]}`)
			ctx := t.Context()
			if err := repository.NewCanvasShareRepository(db).GrantRole(ctx, owner, project.ID, editor, model.CanvasRoleEditor); err != nil {
				t.Fatal(err)
			}
			winner := collaborationEdgeBatch(project, "winning-link", winnerID)
			confirmed, err := repo.Commit(ctx, owner, project.ID, winner)
			if err != nil || len(confirmed.Changes) != 1 {
				t.Fatalf("应确认第一条连线: %+v %v", confirmed, err)
			}
			loser := collaborationEdgeBatch(project, "concurrent-link", "m-edge")
			loser.Changes = append(loser.Changes, collaborationEdit(project, "label", "data.label", `"同批标题"`).Changes...)
			for range 2 {
				result, err := repo.Commit(ctx, editor, project.ID, loser)
				if err != nil || result.Revision != confirmed.Revision || len(result.Changes) != 0 || len(result.Conflicts) != 1 ||
					result.Conflicts[0].Kind != "edge" || result.Conflicts[0].ID != winnerID || result.Conflicts[0].Revision != confirmed.Revision {
					t.Fatalf("并发重复连线应返回可重试的冲突收据，整批保持原子性: %+v %v", result, err)
				}
			}
			loser.BaseRevision, loser.OpID = confirmed.Revision, "known-duplicate"
			if _, err := repo.Commit(ctx, editor, project.ID, loser); !errors.Is(err, repository.ErrCanvasCollaborationInvalidBatch) {
				t.Fatalf("基于当前版本仍重复创建连线属于无效操作: %v", err)
			}
		})
	}
}

func collaborationEdgeBatch(project *model.CanvasProject, opID, id string) repository.CanvasCollaborationBatch {
	return repository.CanvasCollaborationBatch{Epoch: project.CollaborationEpoch, BaseRevision: project.Revision, OpID: opID,
		Changes: []repository.CanvasCollaborationChange{{Kind: "edge", ID: id, Action: "create", Value: json.RawMessage(`{"source":"node","target":"other"}`)}}}
}
