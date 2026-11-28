package repository_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"video-canvas/internal/model"
	"video-canvas/internal/repository"
)

func TestCanvasCollaborationCandidateBatch(t *testing.T) {
	db, canvas, owner, _, _, project := newCollaborationFixture(t)
	ctx := t.Context()
	if err := db.AutoMigrate(&model.GenerationTaskEvent{}); err != nil {
		t.Fatal(err)
	}
	batch := repository.CanvasCollaborationBatch{OpID: "candidate-node", Epoch: project.CollaborationEpoch, BaseRevision: project.Revision,
		Changes: []repository.CanvasCollaborationChange{{Kind: "node", ID: "image", Action: "create",
			Value: json.RawMessage(`{"position":{"x":0,"y":0},"data":{"kind":"image","label":"图片","model":"image-v1"}}`)}}}
	created, err := canvas.Commit(ctx, owner, project.ID, batch)
	if err != nil {
		t.Fatal(err)
	}
	batch = collaborationEdit(project, "confirm-input", "data.generationInput", `{"model":"image-v1","input":{"prompt":"候选"}}`)
	batch.BaseRevision, batch.Changes[0].ID = created.Revision, "image"
	confirmed, err := canvas.Commit(ctx, owner, project.ID, batch)
	if err != nil {
		t.Fatal(err)
	}
	tasks := repository.NewGenerationTaskRepository(db)
	candidates := make([]*model.GenerationTask, 3)
	for index := range candidates {
		task := &model.GenerationTask{UserID: owner, CanvasProjectID: &project.ID, NodeID: "image", Kind: "image", ModelKey: "image-v1", Provider: "test", Status: model.TaskPending,
			InputJSON: datatypes.JSON(`{"prompt":"候选"}`), ConfigSnapshot: datatypes.JSON(`{}`), IdempotencyKey: fmt.Sprintf("candidate-%d", index),
			CollaborationEpoch: confirmed.Epoch, CollaborationInputRevision: confirmed.Revision, CollaborationRequestHash: strings.Repeat("a", 64)}
		if inserted, err := tasks.InsertTask(ctx, task); err != nil || !inserted {
			t.Fatalf("提交候选 %d 失败: %v", index, err)
		}
		candidates[index] = task
	}
	candidates[1].Status = model.TaskFailed
	if err := tasks.FinalizeCanvasCollaboration(ctx, candidates[1]); err != nil {
		t.Fatalf("失败候选应独立结算: %v", err)
	}
	for _, index := range []int{2, 0} {
		task := candidates[index]
		task.Status = model.TaskSucceeded
		task.OutputJSON = datatypes.JSON(fmt.Sprintf(`[{"asset_id":%d,"url":"/files/candidate-%d.png","media_type":"image"}]`, 500+index, index))
		if err := tasks.FinalizeCanvasCollaboration(ctx, task); err != nil || task.CollaborationAppliedRevision == 0 {
			t.Fatalf("候选 %d 应独立回填: %v", index, err)
		}
	}
	snapshot, err := canvas.HistoricalSnapshot(ctx, owner, project.ID, confirmed.Epoch, candidates[0].CollaborationAppliedRevision)
	if err != nil || !strings.Contains(string(snapshot.PayloadJSON), "candidate-0.png") || !strings.Contains(string(snapshot.PayloadJSON), "candidate-2.png") || strings.Contains(string(snapshot.PayloadJSON), `"status":"running"`) {
		t.Fatalf("两个候选都应出现在节点历史: %+v %v", snapshot, err)
	}
}

func TestCanvasCollaborationGenerationBinding(t *testing.T) {
	db, canvas, owner, editor, _, project := newCollaborationFixture(t)
	ctx := t.Context()
	if err := db.AutoMigrate(&model.GenerationTaskEvent{}); err != nil {
		t.Fatal(err)
	}
	shares := repository.NewCanvasShareRepository(db)
	if err := shares.GrantRole(ctx, owner, project.ID, editor, model.CanvasRoleEditor); err != nil {
		t.Fatal(err)
	}
	create := repository.CanvasCollaborationBatch{OpID: "generation-node", Epoch: project.CollaborationEpoch, BaseRevision: project.Revision,
		Changes: []repository.CanvasCollaborationChange{{Kind: "node", ID: "image", Action: "create", Value: json.RawMessage(`{"position":{"x":0,"y":0},"data":{"kind":"image","label":"图片","model":"image-v1","prompt":"确认的提示词"}}`)}}}
	created, err := canvas.Commit(ctx, owner, project.ID, create)
	if err != nil {
		t.Fatal(err)
	}
	newTask := func(user uint64, input string, revision uint64, key string) *model.GenerationTask {
		return &model.GenerationTask{UserID: user, CanvasProjectID: &project.ID, NodeID: "image", Kind: "image", ModelKey: "image-v1", Provider: "test", Status: model.TaskPending,
			InputJSON: datatypes.JSON(input), ConfigSnapshot: datatypes.JSON(`{}`), IdempotencyKey: key, CollaborationEpoch: created.Epoch, CollaborationInputRevision: revision}
	}
	tasks := repository.NewGenerationTaskRepository(db)
	if _, err := tasks.InsertTask(ctx, newTask(editor, `{"prompt":"确认的提示词"}`, created.Revision, "editor")); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("编辑者尚未获得生成授权: %v", err)
	}
	if _, err := tasks.InsertTask(ctx, newTask(owner, `{"prompt":"草稿"}`, created.Revision, "draft")); !errors.Is(err, repository.ErrCanvasCollaborationInput) {
		t.Fatalf("生成必须使用已确认输入: %v", err)
	}
	if _, err := tasks.InsertTask(ctx, newTask(owner, `{"prompt":"确认的提示词","images":["512"]}`, created.Revision, "extra-input")); !errors.Is(err, repository.ErrCanvasCollaborationInput) {
		t.Fatalf("不能发起含未确认素材的生成: %v", err)
	}
	first := newTask(owner, `{"prompt":"确认的提示词"}`, created.Revision, "first")
	if inserted, err := tasks.InsertTask(ctx, first); err != nil || !inserted {
		t.Fatalf("所有者生成没有绑定: %v", err)
	}
	second := newTask(owner, `{"prompt":"确认的提示词"}`, first.CollaborationAppliedRevision, "second")
	if inserted, err := tasks.InsertTask(ctx, second); err != nil || !inserted {
		t.Fatalf("再次生成没有绑定: %v", err)
	}
	first.Status = model.TaskFailed
	if err := tasks.FinalizeCanvasCollaboration(ctx, first); err != nil || first.CollaborationAppliedRevision != 0 {
		t.Fatalf("先发任务的结果不应覆盖后发任务: %d, %v", first.CollaborationAppliedRevision, err)
	}
	second.Status = model.TaskSucceeded
	second.OutputJSON = datatypes.JSON(`[{"asset_id":512,"url":"/files/private-original.png","media_type":"image"},{"asset_id":513,"url":"/files/private-second.png","media_type":"image"}]`)
	if err := tasks.FinalizeCanvasCollaboration(ctx, second); err != nil || second.CollaborationAppliedRevision == 0 {
		t.Fatalf("当前任务的结果应更新节点: %d, %v", second.CollaborationAppliedRevision, err)
	}
	page, err := canvas.Replay(ctx, editor, project.ID, created.Epoch, created.Revision, 20)
	if err != nil || len(page.Events) != 3 || page.Events[2].Changes[0].Fields["data.src"] == nil {
		t.Fatalf("生成状态和结果应参与同一版本序列: %+v, %v", page, err)
	}
	fields := page.Events[2].Changes[0].Fields
	if string(fields["data.src"]) == `"/files/private-original.png"` || fields["data.taskId"] != nil {
		t.Fatalf("编辑成员只能得到授权的结果引用: %s", fields["data.src"])
	}
	assertCanvasPrimaryOutput(t, canvas, project, owner, editor, page.Events[2].Revision)
}

func assertCanvasPrimaryOutput(t *testing.T, canvas *repository.CanvasCollaborationRepository, project *model.CanvasProject, owner, editor, base uint64) {
	t.Helper()
	ctx := t.Context()
	selectResult := func(actor uint64, op, asset string) (*repository.CanvasCollaborationCommit, error) {
		return canvas.Commit(ctx, actor, project.ID, repository.CanvasCollaborationBatch{
			OpID: op, Epoch: project.CollaborationEpoch, BaseRevision: base,
			Changes: []repository.CanvasCollaborationChange{{Kind: "node", ID: "image", Fields: map[string]json.RawMessage{
				"data.activeOutputId": json.RawMessage(`"` + asset + `"`),
			}}},
		})
	}
	if _, err := selectResult(editor, "unknown-output", "999"); !errors.Is(err, repository.ErrCanvasCollaborationInvalidBatch) {
		t.Fatalf("主结果只能引用节点自己的历史: %v", err)
	}
	selected, err := selectResult(editor, "select-output", "513")
	if err != nil || selected.Revision != base+1 || len(selected.Changes) != 1 {
		t.Fatalf("主结果应作为一笔确认操作: %+v, %v", selected, err)
	}
	if fields := selected.Changes[0].Fields; len(fields) != 1 || string(fields["data.activeOutputId"]) != `"513"` {
		t.Fatalf("确认只广播结果标识: %+v", fields)
	}
	assertOutput := func(snapshot *model.CanvasProject, err error, fragments ...string) {
		t.Helper()
		if err != nil || snapshot == nil {
			t.Fatalf("读取主结果失败: %v", err)
		}
		for _, fragment := range fragments {
			if !strings.Contains(string(snapshot.PayloadJSON), fragment) {
				t.Fatalf("主结果应派生正确的素材字段 %s: %s", fragment, snapshot.PayloadJSON)
			}
		}
	}
	live, _, err := canvas.Snapshot(ctx, owner, project.ID)
	assertOutput(live, err, `"activeOutputId":"513"`, `"assetId":"513"`, `"src":"/files/private-second.png"`)
	historical, err := canvas.HistoricalSnapshot(ctx, owner, project.ID, project.CollaborationEpoch, selected.Revision)
	assertOutput(historical, err, `"activeOutputId":"513"`, `"assetId":"513"`, `"src":"/files/private-second.png"`)
	conflict, err := selectResult(owner, "old-selection", "512")
	if err != nil || len(conflict.Conflicts) != 1 || conflict.Revision != selected.Revision {
		t.Fatalf("不同的主结果选择应检测字段冲突: %+v, %v", conflict, err)
	}
	undone, err := canvas.Undo(ctx, editor, project.ID, repository.CanvasCollaborationUndo{OpID: "undo-selection", Epoch: project.CollaborationEpoch})
	if err != nil || undone.Undoes != selected.Revision || len(undone.Changes) != 1 {
		t.Fatalf("主结果应支持个人撤销: %+v, %v", undone, err)
	}
	live, _, err = canvas.Snapshot(ctx, owner, project.ID)
	assertOutput(live, err, `"activeOutputId":"512"`, `"assetId":"512"`, `"src":"/files/private-original.png"`)
}

func TestCanvasCollaborationGenerationConfirmedRequest(t *testing.T) {
	db, canvas, owner, editor, _, project := newCollaborationFixture(t)
	ctx := t.Context()
	if err := db.AutoMigrate(&model.GenerationTaskEvent{}); err != nil {
		t.Fatal(err)
	}
	if err := repository.NewCanvasShareRepository(db).GrantRole(ctx, owner, project.ID, editor, model.CanvasRoleEditor); err != nil {
		t.Fatal(err)
	}
	seed := repository.CanvasCollaborationBatch{OpID: "model-default", Epoch: project.CollaborationEpoch, BaseRevision: project.Revision,
		Changes: []repository.CanvasCollaborationChange{{Kind: "node", ID: "image", Action: "create", Value: json.RawMessage(`{"position":{"x":0,"y":0},"data":{"kind":"image","label":"图片","prompt":"确认的提示词"}}`)}}}
	created, err := canvas.Commit(ctx, owner, project.ID, seed)
	if err != nil {
		t.Fatal(err)
	}
	confirm := repository.CanvasCollaborationBatch{OpID: "confirm-generation", Epoch: project.CollaborationEpoch, BaseRevision: created.Revision,
		Changes: []repository.CanvasCollaborationChange{{Kind: "node", ID: "image", Fields: map[string]json.RawMessage{
			"data.generationInput": json.RawMessage(`{"model":"image-v1","input":{"prompt":"拓展后的提示词","images":["512"],"ratio":"9:16"}}`),
		}}}}
	if _, err := canvas.Commit(ctx, editor, project.ID, confirm); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("编辑者不能确认生成输入: %v", err)
	}
	confirmed, err := canvas.Commit(ctx, owner, project.ID, confirm)
	if err != nil || len(confirmed.Changes) != 1 || len(confirmed.Changes[0].Fields["data.generationInput"]) != 66 {
		t.Fatalf("生成确认只持久化摘要: %+v %v", confirmed, err)
	}
	page, err := canvas.Replay(ctx, editor, project.ID, project.CollaborationEpoch, created.Revision, 20)
	if err != nil || page.Cursor != confirmed.Revision || page.SnapshotRequired || len(page.Events) != 1 || len(page.Events[0].Changes) != 0 || page.Events[0].OpID != "" {
		t.Fatalf("成员应推进版本且不收到私人输入确认信息: %+v %v", page, err)
	}
	tasks := repository.NewGenerationTaskRepository(db)
	newTask := func(ratio string) *model.GenerationTask {
		return &model.GenerationTask{UserID: owner, CanvasProjectID: &project.ID, NodeID: "image", Kind: "image", ModelKey: "image-v1", Provider: "test", Status: model.TaskPending,
			InputJSON: datatypes.JSON(`{}`), ConfigSnapshot: datatypes.JSON(`{}`), CollaborationEpoch: confirmed.Epoch, CollaborationInputRevision: confirmed.Revision,
			CollaborationRawInput: map[string]any{"prompt": "拓展后的提示词", "ratio": ratio, "images": []string{"512"}}}
	}
	if _, err := tasks.InsertTask(ctx, newTask("16:9")); !errors.Is(err, repository.ErrCanvasCollaborationInput) {
		t.Fatalf("更改确认的生成参数应被拒绝: %v", err)
	}
	if inserted, err := tasks.InsertTask(ctx, newTask("9:16")); err != nil || !inserted {
		t.Fatalf("确认的模型、提示词、参数和素材应原子绑定: %v", err)
	}
}

func TestCanvasCollaborationGenerationGrantLifecycle(t *testing.T) {
	db, canvas, owner, editor, viewer, project := newCollaborationFixture(t)
	ctx := t.Context()
	share := repository.NewCanvasShareRepository(db)
	if err := share.GrantRole(ctx, owner, project.ID, editor, model.CanvasRoleEditor); err != nil {
		t.Fatal(err)
	}
	if err := share.GrantRole(ctx, owner, project.ID, viewer, model.CanvasRoleViewer); err != nil {
		t.Fatal(err)
	}
	seed := repository.CanvasCollaborationBatch{OpID: "seed-image", Epoch: project.CollaborationEpoch, BaseRevision: project.Revision,
		Changes: []repository.CanvasCollaborationChange{{Kind: "node", ID: "image", Action: "create", Value: json.RawMessage(`{"position":{"x":0,"y":0},"data":{"kind":"image","label":"图片"}}`)}}}
	created, err := canvas.Commit(ctx, owner, project.ID, seed)
	if err != nil {
		t.Fatal(err)
	}
	confirm := repository.CanvasCollaborationBatch{OpID: "grant-confirm", Epoch: project.CollaborationEpoch, BaseRevision: created.Revision,
		Changes: []repository.CanvasCollaborationChange{{Kind: "node", ID: "image", Fields: map[string]json.RawMessage{
			"data.generationInput": json.RawMessage(`{"model":"test","input":{"prompt":"共同生成"}}`),
		}}}}
	if err := share.SetGenerationPermission(ctx, editor, project.ID, editor, true); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("仅所有者可授权成员生成: %v", err)
	}
	if err := share.SetGenerationPermission(ctx, owner, project.ID, viewer, true); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("只读成员不能获得生成授权: %v", err)
	}
	if err := share.SetGenerationPermission(ctx, owner, project.ID, editor, true); err != nil {
		t.Fatal(err)
	}
	confirmed, err := canvas.Commit(ctx, editor, project.ID, confirm)
	if err != nil {
		t.Fatalf("具有生成授权的编辑成员可确认生成输入: %v", err)
	}
	if err := db.AutoMigrate(&model.GenerationTaskEvent{}); err != nil {
		t.Fatal(err)
	}
	tasks := repository.NewGenerationTaskRepository(db)
	task := &model.GenerationTask{UserID: editor, CanvasProjectID: &project.ID, NodeID: "image", Kind: "image", ModelKey: "test", Provider: "test",
		Status: model.TaskPending, InputJSON: datatypes.JSON(`{"prompt":"共同生成"}`), ConfigSnapshot: datatypes.JSON(`{}`),
		CollaborationEpoch: confirmed.Epoch, CollaborationInputRevision: confirmed.Revision}
	if ok, err := tasks.InsertTask(ctx, task); err != nil || !ok {
		t.Fatalf("任务应归发起成员: %v", err)
	}
	if revision, err := tasks.CanvasProgressRevision(ctx, task); err != nil || revision != task.CollaborationAppliedRevision {
		t.Fatalf("运行中的授权任务应以确认版本发送进度: %d, %v", revision, err)
	}
	started, _, err := canvas.Snapshot(ctx, editor, project.ID)
	if err != nil || !strings.Contains(string(started.PayloadJSON), fmt.Sprintf(`"generationRevision":%d`, task.CollaborationAppliedRevision)) {
		t.Fatalf("运行节点需要公开且稳定的版本用来匹配进度: %+v, %v", started, err)
	}
	for _, role := range []string{model.CanvasRoleViewer, model.CanvasRoleEditor} {
		if err := share.GrantRole(ctx, owner, project.ID, editor, role); err != nil {
			t.Fatal(err)
		}
	}
	if revision, err := tasks.CanvasProgressRevision(ctx, task); err != nil || revision != 0 {
		t.Fatalf("撤回生成授权后不再发送进度: %d, %v", revision, err)
	}
	confirm.OpID = "grant-confirm-revoked"
	if _, err := canvas.Commit(ctx, editor, project.ID, confirm); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("降级再升级应重新授予生成权限: %v", err)
	}
	task.Status = model.TaskSucceeded
	task.OutputJSON = datatypes.JSON(`[{"asset_id":512,"url":"/files/actor.png","media_type":"image"}]`)
	if err := tasks.FinalizeCanvasCollaboration(ctx, task); err != nil || task.CollaborationAppliedRevision == 0 {
		t.Fatalf("撤权后释放对应节点的运行状态: %v", err)
	}
	view, _, err := canvas.Snapshot(ctx, owner, project.ID)
	if err != nil || strings.Contains(string(view.PayloadJSON), "actor.png") || strings.Contains(string(view.PayloadJSON), `"status":"running"`) {
		t.Fatalf("撤权后应跳过成员私有结果写回: %+v, %v", view, err)
	}
}

func TestCanvasCollaborationGenerationPublishesOwnedCopies(t *testing.T) {
	for _, outcome := range []string{"owner-first", "member-first", "member-canceled"} {
		t.Run(outcome, func(t *testing.T) { testCanvasParallelGeneration(t, outcome) })
	}
}

func testCanvasParallelGeneration(t *testing.T, outcome string) {
	t.Helper()
	db, canvas, owner, editor, viewer, project := newCollaborationFixture(t)
	if err := db.AutoMigrate(&model.GenerationTaskEvent{}, &model.Asset{}, &model.CanvasDeliveryStage{}); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	share := repository.NewCanvasShareRepository(db)
	if err := share.GrantRole(ctx, owner, project.ID, editor, model.CanvasRoleEditor); err != nil {
		t.Fatal(err)
	}
	if err := share.Grant(ctx, owner, project.ID, viewer); err != nil {
		t.Fatal(err)
	}
	if err := share.SetGenerationPermission(ctx, owner, project.ID, editor, true); err != nil {
		t.Fatal(err)
	}
	digest := repository.CanvasCollaborationBatch{OpID: "member-input", Epoch: project.CollaborationEpoch, BaseRevision: project.Revision,
		Changes: []repository.CanvasCollaborationChange{{Kind: "node", ID: "image", Fields: map[string]json.RawMessage{
			"data.generationInput": json.RawMessage(`{"model":"test","input":{"prompt":"生成"}}`),
		}}}}
	seed := repository.CanvasCollaborationBatch{OpID: "asset-node", Epoch: project.CollaborationEpoch, BaseRevision: project.Revision,
		Changes: []repository.CanvasCollaborationChange{{Kind: "node", ID: "image", Action: "create", Value: json.RawMessage(`{"position":{"x":0,"y":0},"data":{"kind":"image","label":"图片","model":"test","prompt":"生成"}}`)}}}
	created, err := canvas.Commit(ctx, owner, project.ID, seed)
	if err != nil {
		t.Fatal(err)
	}
	tasks := repository.NewGenerationTaskRepository(db)
	ownTask := &model.GenerationTask{UserID: owner, CanvasProjectID: &project.ID, NodeID: "image", Kind: "image", ModelKey: "test", Provider: "test",
		Status: model.TaskPending, InputJSON: datatypes.JSON(`{"prompt":"生成"}`), ConfigSnapshot: datatypes.JSON(`{}`),
		CollaborationEpoch: created.Epoch, CollaborationInputRevision: created.Revision}
	if ok, err := tasks.InsertTask(ctx, ownTask); err != nil || !ok {
		t.Fatalf("所有者任务应先进入队列: %v", err)
	}
	digest.BaseRevision = ownTask.CollaborationAppliedRevision
	confirmed, err := canvas.Commit(ctx, editor, project.ID, digest)
	if err != nil {
		t.Fatal(err)
	}
	task := &model.GenerationTask{UserID: editor, CanvasProjectID: &project.ID, NodeID: "image", Kind: "image", ModelKey: "test", Provider: "test",
		Status: model.TaskPending, InputJSON: datatypes.JSON(`{"prompt":"生成"}`), ConfigSnapshot: datatypes.JSON(`{}`),
		CollaborationEpoch: confirmed.Epoch, CollaborationInputRevision: confirmed.Revision}
	if ok, err := tasks.InsertTask(ctx, task); err != nil || !ok {
		t.Fatalf("成员任务应绑定节点: %v", err)
	}
	if err := share.Ready(ctx, owner, project.ID); !errors.Is(err, repository.ErrInUse) {
		t.Fatalf("成员任务运行期间应阻止转移: %v", err)
	}
	if outcome == "member-first" {
		assertCanvasMemberMedia(t, db, share, canvas, tasks, owner, viewer, project, task, confirmed.Revision, 2)
	}
	memberStart := task.CollaborationAppliedRevision
	ownTask.Status = model.TaskSucceeded
	ownTask.OutputJSON = datatypes.JSON(`[{"asset_id":512,"url":"/files/owner-first.png","media_type":"image"}]`)
	if err := tasks.FinalizeCanvasCollaboration(ctx, ownTask); err != nil || ownTask.CollaborationAppliedRevision != memberStart+1 {
		t.Fatalf("不同发起人的较早结果也应保留: %d, %v", ownTask.CollaborationAppliedRevision, err)
	}
	page, err := canvas.Replay(ctx, viewer, project.ID, project.CollaborationEpoch, memberStart, 20)
	if err != nil || len(page.Events) != 1 || len(page.Events[0].Changes) != 1 || len(page.Events[0].Changes[0].Fields) != 1 || page.Events[0].Changes[0].Fields["data.outputs"] == nil {
		t.Fatalf("较早任务只能更新结果历史: %+v, %v", page, err)
	}
	if err := tasks.FinalizeCanvasCollaboration(ctx, ownTask); err != nil || ownTask.CollaborationAppliedRevision != 0 {
		t.Fatalf("同一个较早任务不得重复追加: %d, %v", ownTask.CollaborationAppliedRevision, err)
	}
	switch outcome {
	case "owner-first":
		assertCanvasMemberMedia(t, db, share, canvas, tasks, owner, viewer, project, task, confirmed.Revision, 3)
	case "member-canceled":
		task.Status = model.TaskCanceled
		if err := tasks.FinalizeCanvasCollaboration(ctx, task); err != nil {
			t.Fatal(err)
		}
		snapshot, _, err := canvas.Snapshot(ctx, owner, project.ID)
		if err != nil || !strings.Contains(string(snapshot.PayloadJSON), `"activeOutputId":"512"`) || !strings.Contains(string(snapshot.PayloadJSON), `"status":"done"`) {
			t.Fatalf("首选任务取消时已有结果应成为主结果: %+v, %v", snapshot, err)
		}
	}
}

func assertCanvasMemberMedia(t *testing.T, db *gorm.DB, share *repository.CanvasShareRepository, canvas *repository.CanvasCollaborationRepository,
	tasks *repository.GenerationTaskRepository, owner, viewer uint64, project *model.CanvasProject, task *model.GenerationTask, revision uint64, eventCount int) {
	t.Helper()
	ctx := t.Context()
	asset := &model.Asset{UserID: owner, Kind: "image", MimeType: "image/png", Source: model.AssetSourceUpload, StorageKey: "owner/project-result.png"}
	if err := share.Stage(ctx, &model.CanvasDeliveryStage{Attempt: "result", StorageKey: asset.StorageKey}); err != nil {
		t.Fatal(err)
	}
	task.Status = model.TaskSucceeded
	task.OutputJSON = datatypes.JSON(`[{"asset_id":512,"url":"/files/actor-private.png","media_type":"image"}]`)
	task.CollaborationMedia = []model.CanvasGenerationMedia{{Asset: asset, Attempt: "result", Output: model.TaskOutput{URL: "/files/owner/project-result.png", MediaType: "image"}}}
	if err := tasks.FinalizeCanvasCollaboration(ctx, task); err != nil || asset.ID == 0 {
		t.Fatalf("只确认独立的画布副本: %v", err)
	}
	var saved model.Asset
	if err := db.Take(&saved, asset.ID).Error; err != nil || saved.UserID != owner || saved.TaskID != nil {
		t.Fatalf("画布副本不得关联发起者的任务: %+v，%v", saved, err)
	}
	page, err := canvas.Replay(ctx, viewer, project.ID, project.CollaborationEpoch, revision, 20)
	if err != nil || len(page.Events) != eventCount {
		t.Fatalf("查看者应接到状态与产物确认: %+v，%v", page, err)
	}
	fields := page.Events[eventCount-1].Changes[0].Fields
	if eventCount == 3 && !strings.Contains(string(fields["data.outputs"]), `"id":"512"`) {
		t.Fatalf("确认后仍须保留较早任务的产物: %+v", fields)
	}
	if !strings.Contains(string(fields["data.src"]), "/api/v1/canvas-media/") || strings.Contains(string(fields["data.outputs"]), "actor-private") || fields["data.taskId"] != nil {
		t.Fatalf("成员只看到受保护的画布产物: %+v", fields)
	}
}
