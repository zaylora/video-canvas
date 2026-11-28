package repository_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"gorm.io/datatypes"

	"video-canvas/internal/model"
	"video-canvas/internal/repository"
	agentsvc "video-canvas/internal/service/agent"
)

func TestCanvasCollaborationSplitSettlement(t *testing.T) {
	for _, scenario := range []struct {
		name, mode string
		revoke     bool
	}{
		{name: "追加保留并行新增", mode: "append"},
		{name: "覆盖遇到并行编辑即拒绝", mode: "replace"},
		{name: "撤权后拒绝写入", mode: "append", revoke: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			testCollaborationSplitSettlement(t, scenario.mode, scenario.revoke)
		})
	}
}

func TestCanvasCollaborationSplitPreviewVersion(t *testing.T) {
	_, canvas, owner, _, _, project := newCollaborationFixtureWithPayload(t, `{"nodes":[{"id":"board","type":"canvas","data":{"kind":"storyboard","storyboard":{"shots":[]}}}],"edges":[]}`)
	ctx := t.Context()
	edited, err := canvas.Commit(ctx, owner, project.ID, repository.CanvasCollaborationBatch{
		OpID: "board-update", Epoch: project.CollaborationEpoch, BaseRevision: project.Revision,
		Changes: []repository.CanvasCollaborationChange{{Kind: "node", ID: "board", Fields: map[string]json.RawMessage{"data.storyboard": json.RawMessage(`{"shots":[{"id":"new","duration":4}]}`)}}},
	})
	if err != nil || len(edited.Conflicts) != 0 {
		t.Fatalf("并行修改: %+v %v", edited, err)
	}
	var request repository.CanvasCollaborationBatch
	if err := json.Unmarshal([]byte(`{"opId":"confirm-replace","requireBaseRevision":true,"changes":[{"kind":"node","id":"board","fields":{"data.generationInput":{"model":"storyboard-split","input":{"script":"剧本","mode":"replace"}}}}]}`), &request); err != nil {
		t.Fatal(err)
	}
	request.Epoch, request.BaseRevision = project.CollaborationEpoch, project.Revision
	for range 2 {
		result, err := canvas.Commit(ctx, owner, project.ID, request)
		if err != nil || len(result.Conflicts) != 1 || result.Revision != edited.Revision || len(result.Changes) != 0 {
			t.Fatalf("预览失效应返回可重试对账的冲突: %+v %v", result, err)
		}
	}
	request.OpID, request.BaseRevision = "confirm-after-preview", edited.Revision
	for range 2 {
		result, err := canvas.Commit(ctx, owner, project.ID, request)
		if err != nil || len(result.Conflicts) != 0 || result.Revision != edited.Revision+1 {
			t.Fatalf("已受理的确认应保留原收据: %+v %v", result, err)
		}
	}
}

func testCollaborationSplitSettlement(t *testing.T, mode string, revoke bool) {
	t.Helper()
	db, canvas, owner, editor, _, project := newCollaborationFixtureWithPayload(t, `{"nodes":[{"id":"board","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"storyboard","label":"分镜表","storyboard":{"shots":[]}}}],"edges":[]}`)
	ctx := t.Context()
	repo := repository.NewStoryboardRepository(db)
	repo.SetEnqueuer(&splitEnqueuer{})
	shares := repository.NewCanvasShareRepository(db)
	if err := shares.GrantRole(ctx, owner, project.ID, editor, model.CanvasRoleEditor); err != nil {
		t.Fatal(err)
	}
	if err := shares.SetGenerationPermission(ctx, owner, project.ID, editor, true); err != nil {
		t.Fatal(err)
	}
	commit := func(actor, base uint64, op, field, value string) uint64 {
		t.Helper()
		result, err := canvas.Commit(ctx, actor, project.ID, repository.CanvasCollaborationBatch{
			OpID: op, Epoch: project.CollaborationEpoch, BaseRevision: base,
			Changes: []repository.CanvasCollaborationChange{{Kind: "node", ID: "board", Fields: map[string]json.RawMessage{field: json.RawMessage(value)}}},
		})
		if err != nil || len(result.Conflicts) != 0 {
			t.Fatalf("提交 %s: %+v, %v", op, result, err)
		}
		return result.Revision
	}
	base := commit(editor, project.Revision, "input", "data.generationInput", `{"model":"storyboard-split","input":{"script":"剧本","mode":"`+mode+`"}}`)
	key := "split-intent"
	job := &model.StoryboardSplitJob{UserID: editor, CanvasID: project.ID, NodeID: "board", Script: "剧本", Status: model.SplitJobPending, Mode: mode, CollaborationEpoch: project.CollaborationEpoch, CollaborationRevision: base, RequestKey: &key, RequestHash: "input-hash"}
	if err := createSplit(t, repo, job); err != nil {
		t.Fatal(err)
	}
	prior, err := repo.SplitJobByRequest(ctx, editor, key)
	if err != nil || prior.ID != job.ID || prior.RequestHash != job.RequestHash {
		t.Fatalf("原编号应关联已受理的任务: %+v %v", prior, err)
	}
	if _, err := repo.SplitJobByRequest(ctx, owner, key); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("其他账号不得查询这笔任务: %v", err)
	}
	base = commit(owner, base+1, "manual", "data.storyboard", `{"shots":[{"id":"manual","scene":"手动","duration":4}]}`)
	if revoke {
		if err := shares.SetGenerationPermission(ctx, owner, project.ID, editor, false); err != nil {
			t.Fatal(err)
		}
		if err := repo.CheckCollaborationSplit(ctx, job); !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("撤权后排队任务应在调用模型前停止: %v", err)
		}
	}
	finish := func(shots []model.StoryboardShot) error {
		return repo.WithTx(ctx, func(tx repository.StoryboardTx) error {
			return tx.(interface {
				FinishCollaborationSplit(context.Context, *model.StoryboardSplitJob, []model.StoryboardShot) error
			}).FinishCollaborationSplit(ctx, job, shots)
		})
	}
	err = finish([]model.StoryboardShot{{Scene: "自动", Duration: 4}})
	if mode == "replace" || revoke {
		if !errors.Is(err, repository.ErrCanvasCollaborationInput) {
			t.Fatalf("拒绝覆盖或撤权写入: %v", err)
		}
		err = finish(nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	page, err := canvas.Replay(ctx, owner, project.ID, project.CollaborationEpoch, base, 10)
	if err != nil || len(page.Events) != 1 || string(page.Events[0].Changes[0].Fields["data.storyboardSplitRunning"]) != "null" {
		t.Fatalf("结束事件应清除运行标记: %+v, %v", page, err)
	}
	confirmed, _, err := canvas.Snapshot(ctx, owner, project.ID)
	if err != nil || !strings.Contains(string(confirmed.PayloadJSON), `"manual"`) || strings.Contains(string(confirmed.PayloadJSON), "自动") != (mode == "append" && !revoke) {
		t.Fatalf("分镜内容应符合结算模式: %+v, %v", confirmed, err)
	}
}

func TestCanvasCollaborationSplitRequiresGenerationPermission(t *testing.T) {
	db, canvas, owner, editor, _, project := newCollaborationFixtureWithPayload(t, `{"nodes":[{"id":"board","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"storyboard","label":"分镜表","storyboard":{"shots":[]}}}],"edges":[]}`)
	repo := repository.NewStoryboardRepository(db)
	repo.SetEnqueuer(&splitEnqueuer{})
	shares := repository.NewCanvasShareRepository(db)
	if err := shares.GrantRole(t.Context(), owner, project.ID, editor, model.CanvasRoleEditor); err != nil {
		t.Fatal(err)
	}
	job := &model.StoryboardSplitJob{UserID: editor, CanvasID: project.ID, NodeID: "board", Script: "剧本", Status: model.SplitJobPending, Mode: "append", CollaborationEpoch: project.CollaborationEpoch}
	if err := createSplit(t, repo, job); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("编辑成员未授权: %v", err)
	}
	if err := shares.SetGenerationPermission(t.Context(), owner, project.ID, editor, true); err != nil {
		t.Fatal(err)
	}
	confirmed, err := canvas.Commit(t.Context(), editor, project.ID, repository.CanvasCollaborationBatch{
		OpID: "split-input", Epoch: project.CollaborationEpoch, BaseRevision: project.Revision,
		Changes: []repository.CanvasCollaborationChange{{Kind: "node", ID: "board", Fields: map[string]json.RawMessage{
			"data.generationInput": json.RawMessage(`{"model":"storyboard-split","input":{"script":"剧本","mode":"append"}}`),
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	job.CollaborationRevision = confirmed.Revision
	if err := createSplit(t, repo, job); err != nil {
		t.Fatalf("已授权成员应能提交协作拆分: %v", err)
	}
}

func TestCanvasCollaborationBudgetSharedWithSplit(t *testing.T) {
	db, canvas, owner, _, _, project := newCollaborationFixtureWithPayload(t, `{"nodes":[{"id":"board","type":"canvas","data":{"kind":"storyboard","storyboard":{"shots":[]}}},{"id":"image","type":"canvas","data":{"kind":"image","model":"m","prompt":"画面"}}],"edges":[]}`)
	ctx := t.Context()
	if err := db.AutoMigrate(&model.CanvasGenerationBudget{}, &model.GenerationTaskEvent{}, &model.CreditLedger{}); err != nil {
		t.Fatal(err)
	}
	confirmed, err := canvas.Commit(ctx, owner, project.ID, repository.CanvasCollaborationBatch{
		OpID: "confirm-split", Epoch: project.CollaborationEpoch, BaseRevision: project.Revision,
		Changes: []repository.CanvasCollaborationChange{{Kind: "node", ID: "board", Fields: map[string]json.RawMessage{
			"data.generationInput": json.RawMessage(`{"model":"storyboard-split","input":{"script":"剧本","mode":"append"}}`),
		}}},
	})
	if err != nil || len(confirmed.Changes) != 1 {
		t.Fatalf("确认拆分输入失败: %+v %v", confirmed, err)
	}
	budgetLimit, splitLimit := 10, 6
	budget := &model.CanvasGenerationBudgetRequest{Key: "production", Limit: &budgetLimit}
	split := &model.StoryboardSplitJob{UserID: owner, CanvasID: project.ID, NodeID: "board", Script: "剧本", Status: model.SplitJobPending,
		Mode: "append", CollaborationEpoch: project.CollaborationEpoch, CollaborationRevision: confirmed.Revision, MaxCredits: &splitLimit, BudgetRequest: budget}
	splits := repository.NewStoryboardRepository(db)
	splits.SetEnqueuer(&splitEnqueuer{})
	if err := createSplit(t, splits, split); err != nil {
		t.Fatal(err)
	}
	tasks := repository.NewGenerationTaskRepository(db)
	image := &model.GenerationTask{UserID: owner, CanvasProjectID: &project.ID, NodeID: "image", Kind: "image", ModelKey: "m", Provider: "test", Status: model.TaskPending,
		InputJSON: datatypes.JSON(`{"prompt":"画面"}`), ConfigSnapshot: datatypes.JSON(`{}`), IdempotencyKey: "image", Credits: 5,
		CollaborationEpoch: project.CollaborationEpoch, CollaborationInputRevision: confirmed.Revision, BudgetRequest: budget}
	if _, err := tasks.InsertTask(ctx, image); !errors.Is(err, repository.ErrCanvasGenerationBudget) {
		t.Fatalf("拆分镜预留 6 点时不能受理 5 点生成: %v", err)
	}
	if err := splits.UpdateSplitJob(ctx, split.ID, map[string]any{"status": model.SplitJobSucceeded, "credits": 5}); err != nil {
		t.Fatal(err)
	}
	if inserted, err := tasks.InsertTask(ctx, image); err != nil || !inserted || image.BudgetGroupID == nil || *image.BudgetGroupID != *split.BudgetGroupID {
		t.Fatalf("拆分镜实际结算后应允许生成使用剩余预算: %v, %v", inserted, err)
	}
}

func TestCanvasCollaborationAgentMemberRead(t *testing.T) {
	db, canvas, owner, editor, _, project := newCollaborationFixture(t)
	ctx := t.Context()
	repo := repository.NewAgentRepository(db)
	shares := repository.NewCanvasShareRepository(db)
	if err := shares.GrantRole(ctx, owner, project.ID, editor, model.CanvasRoleEditor); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetCanvas(ctx, editor, project.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("缺少生成授权时不能读取小镜画布: %v", err)
	}
	if err := shares.SetGenerationPermission(ctx, owner, project.ID, editor, true); err != nil {
		t.Fatal(err)
	}
	if snapshot, err := repo.GetCanvas(ctx, editor, project.ID); err != nil || !snapshot.CollaborationActive {
		t.Fatalf("已授权成员应能读取协作数据: %+v, %v", snapshot, err)
	}
	if err := db.AutoMigrate(&model.AgentMutation{}, &model.AgentCanvasRead{}); err != nil {
		t.Fatal(err)
	}
	run := &model.AgentRun{ID: 101, UserID: editor, CanvasID: project.ID, CollaborationEpoch: project.CollaborationEpoch}
	svc := agentsvc.NewAgentCanvasService(repo, nil)
	result, err := svc.ApplyOps(ctx, run, "agent-collab-create", []byte(`[{"op":"create_node","tempId":"script","kind":"script","label":"剧本","prompt":"夜晚"},{"op":"create_node","tempId":"image","kind":"image","label":"主图","prompt":"夜晚的街景"},{"op":"connect","source":"script","target":"image"}]`))
	if err != nil || result.Revision != project.Revision+1 {
		t.Fatalf("小镜应提交记录级变更: %+v, %v", result, err)
	}
	page, err := canvas.Replay(ctx, owner, project.ID, project.CollaborationEpoch, project.Revision, 10)
	if err != nil || len(page.Events) != 1 || len(page.Events[0].Changes) != 3 {
		t.Fatalf("其他成员应收到同一笔版号: %+v, %v", page, err)
	}
	undo, err := svc.UndoRun(ctx, run)
	if err != nil || undo.Reverted != 2 || len(undo.Skipped) != 0 || undo.Revision != result.Revision+1 {
		t.Fatalf("应能撤销小镜创建的节点和连线: %+v, %v", undo, err)
	}
	undoPage, err := canvas.Replay(ctx, owner, project.ID, project.CollaborationEpoch, result.Revision, 10)
	if err != nil || len(undoPage.Events) != 1 || len(undoPage.Events[0].Changes) != 3 {
		t.Fatalf("撤销应向其他成员回放两节点及一连线的删除: %+v, %v", undoPage, err)
	}
	if err := shares.SetGenerationPermission(ctx, owner, project.ID, editor, false); err != nil {
		t.Fatal(err)
	}
	if err := repo.CheckAgentCanvasEpoch(ctx, editor, project.ID, run.CollaborationEpoch); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("撤权应立即限制小镜: %v", err)
	}
}

func TestCanvasCollaborationCharacterVoice(t *testing.T) {
	db, canvas, owner, _, _, project := newCollaborationFixture(t)
	ctx := t.Context()
	if err := db.AutoMigrate(&model.GenerationTaskEvent{}); err != nil {
		t.Fatal(err)
	}
	commit := func(op string, revision uint64, change repository.CanvasCollaborationChange) uint64 {
		t.Helper()
		result, err := canvas.Commit(ctx, owner, project.ID, repository.CanvasCollaborationBatch{
			OpID: op, Epoch: project.CollaborationEpoch, BaseRevision: revision, Changes: []repository.CanvasCollaborationChange{change},
		})
		if err != nil || len(result.Conflicts) > 0 {
			t.Fatalf("%s: %+v, %v", op, result, err)
		}
		return result.Revision
	}
	revision := commit("create-character", project.Revision, repository.CanvasCollaborationChange{Kind: "node", ID: "character", Action: "create", Value: json.RawMessage(`{"position":{"x":0,"y":0},"data":{"kind":"character","label":"角色","character":{"name":"阿青","look":"","description":"","extras":[],"voice":null}}}`)})
	revision = commit("confirm-voice", revision, repository.CanvasCollaborationChange{Kind: "node", ID: "character", Fields: map[string]json.RawMessage{
		"data.generationInput": json.RawMessage(`{"model":"audio-v1","input":{"prompt":"试音"}}`),
	}})
	task := &model.GenerationTask{UserID: owner, CanvasProjectID: &project.ID, NodeID: "voice:character", Kind: "audio", ModelKey: "audio-v1", Provider: "test", Status: model.TaskPending,
		InputJSON: datatypes.JSON(`{"prompt":"试音"}`), ConfigSnapshot: datatypes.JSON(`{}`), IdempotencyKey: "voice", CollaborationEpoch: project.CollaborationEpoch, CollaborationInputRevision: revision}
	tasks := repository.NewGenerationTaskRepository(db)
	if inserted, err := tasks.InsertTask(ctx, task); err != nil || !inserted {
		t.Fatalf("绑定音色任务: %v", err)
	}
	commit("rename-character", task.CollaborationAppliedRevision, repository.CanvasCollaborationChange{Kind: "node", ID: "character", Fields: map[string]json.RawMessage{
		"data.character": json.RawMessage(`{"name":"新名字","look":"","description":"","extras":[],"voice":null}`),
	}})
	task.Status = model.TaskSucceeded
	task.OutputJSON = datatypes.JSON(`[{"asset_id":512,"url":"/files/voice.wav","media_type":"audio"}]`)
	if err := tasks.FinalizeCanvasCollaboration(ctx, task); err != nil || task.CollaborationAppliedRevision == 0 {
		t.Fatalf("结算音色任务: %v", err)
	}
	page, err := canvas.Replay(ctx, owner, project.ID, project.CollaborationEpoch, task.CollaborationAppliedRevision-1, 10)
	if err != nil || len(page.Events) != 1 {
		t.Fatalf("回放音色: %+v, %v", page, err)
	}
	fields := page.Events[0].Changes[0].Fields
	var character struct {
		Name  string
		Voice struct{ AssetID string }
	}
	if err := json.Unmarshal(fields["data.character"], &character); err != nil || character.Name != "新名字" || character.Voice.AssetID != "512" {
		t.Fatalf("音色应保留并行的角色修改: %s", fields["data.character"])
	}
	if fields["data.status"] != nil || fields["data.assetId"] != nil {
		t.Fatalf("音色不得修改主图: %+v", fields)
	}
}
