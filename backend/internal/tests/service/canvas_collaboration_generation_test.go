package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

func TestCanvasCollaborationGenerationSettlement(t *testing.T) {
	env := newGTIEnv(t, &fakeLimits{initial: 50, defMax: 4})
	ctx := t.Context()
	if err := env.db.AutoMigrate(&model.StoryboardSplitJob{}, &model.CanvasCollaborationRecord{}, &model.CanvasCollaborationReceipt{}, &model.CanvasCollaborationEvent{}, &model.CanvasCollaborationCheckpoint{}); err != nil {
		t.Fatal(err)
	}
	req := validCreateReq()
	canvasID := uint64(req.CanvasID)
	if err := env.db.Model(&model.CanvasProject{}).Where("id = ?", canvasID).Update("payload_json", json.RawMessage(`{"nodes":[{"id":"n1","type":"canvas","data":{"kind":"video","label":"初始镜头","model":"m1","prompt":"一只猫"}}],"edges":[]}`)).Error; err != nil {
		t.Fatal(err)
	}
	canvas := repository.NewCanvasCollaborationRepository(env.db)
	project, err := canvas.SetActive(ctx, env.user, canvasID, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	req.Collaboration = &model.CanvasGenerationBinding{Epoch: project.CollaborationEpoch, Revision: project.Revision}
	maxCredits := 9
	req.Collaboration.MaxCredits = &maxCredits
	_, overBudget := createSingle(ctx, env.svc, env.user, "over-budget", req)
	assertTaskCode(t, overBudget, errcode.ErrGenerationBudget.Code)
	var ledgerCount int64
	if err := env.db.Model(&model.CreditLedger{}).Where("user_id = ?", env.user).Count(&ledgerCount).Error; err != nil || ledgerCount != 0 {
		t.Fatalf("超过上限的请求不得写入积分流水: %d, %v", ledgerCount, err)
	}
	maxCredits = 10
	first, err := createSingle(ctx, env.svc, env.user, "same-collaboration-request", req)
	if err != nil {
		t.Fatal(err)
	}
	repeat, err := createSingle(ctx, env.svc, env.user, "same-collaboration-request", req)
	if err != nil || repeat.ID != first.ID || env.assertReconciled(t).FreezeSum != 10 {
		t.Fatalf("重试不得重复扣费或创建任务: %+v, %v", repeat, err)
	}
	modified := *req
	modified.Input = map[string]any{"prompt": "换了提示词的重试"}
	_, conflict := createSingle(ctx, env.svc, env.user, "same-collaboration-request", &modified)
	var biz *errcode.Error
	if !errors.As(conflict, &biz) || biz.Code != errcode.ErrCanvasConflict.Code {
		t.Fatalf("相同幂等键不能用于不同的协作生成正文: %v", conflict)
	}
	batch := repository.CanvasCollaborationBatch{OpID: "edit-during-generation", Epoch: project.CollaborationEpoch, BaseRevision: project.Revision + 1,
		Changes: []repository.CanvasCollaborationChange{{Kind: "node", ID: req.NodeID, Fields: map[string]json.RawMessage{"data.prompt": json.RawMessage(`"两只猫"`)}}}}
	if result, err := canvas.Commit(ctx, env.user, canvasID, batch); err != nil || len(result.Changes) != 1 {
		t.Fatalf("生成中仍应允许修改提示词: %+v, %v", result, err)
	}
	completeCollaborationTask(t, env, first.ID)
	confirmed, _, err := canvas.Snapshot(ctx, env.user, canvasID)
	if err != nil || !bytes.Contains(confirmed.PayloadJSON, []byte(`"两只猫"`)) || !bytes.Contains(confirmed.PayloadJSON, []byte(`"/files/result.mp4"`)) {
		t.Fatalf("终态应保留并发提示词并追加结果: %+v, %v", confirmed, err)
	}
	credits := env.assertReconciled(t)
	if credits.Balance != 40 || credits.Frozen != 0 || credits.SettleSum != 10 {
		t.Fatalf("生成结算不一致: %+v", credits)
	}
}

func TestCanvasCollaborationMemberBilling(t *testing.T) {
	env := newGTIEnv(t, &fakeLimits{initial: 50, defMax: 4})
	assertMemberGenerationAccounting(t, env, seedMemberGeneration(t, env))
}

func TestCanvasCollaborationCumulativeBudget(t *testing.T) {
	env := newGTIEnv(t, &fakeLimits{initial: 50, defMax: 4})
	ctx := t.Context()
	if err := env.db.AutoMigrate(&model.StoryboardSplitJob{}, &model.CanvasCollaborationRecord{}, &model.CanvasCollaborationReceipt{}, &model.CanvasCollaborationEvent{}, &model.CanvasCollaborationCheckpoint{}, &model.CanvasGenerationBudget{}); err != nil {
		t.Fatal(err)
	}
	req := validCreateReq()
	canvasID := uint64(req.CanvasID)
	payload := json.RawMessage(`{"nodes":[{"id":"n1","type":"canvas","data":{"kind":"video","model":"m1","prompt":"一只猫"}},{"id":"n2","type":"canvas","data":{"kind":"video","model":"m1","prompt":"一只猫"}}],"edges":[]}`)
	if err := env.db.Model(&model.CanvasProject{}).Where("id = ?", canvasID).Update("payload_json", payload).Error; err != nil {
		t.Fatal(err)
	}
	project, err := repository.NewCanvasCollaborationRepository(env.db).SetActive(ctx, env.user, canvasID, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	limit := 10
	req.Collaboration = &model.CanvasGenerationBinding{Epoch: project.CollaborationEpoch, Revision: project.Revision,
		MaxCredits: &limit, Budget: &model.CanvasGenerationBudgetRequest{Key: "episode-1", Limit: &limit}}
	clone := *req
	clone.NodeID = "n2"
	first, winner, loser := raceBudgetSubmissions(ctx, t, env, req, &clone)
	repeat, err := createSingle(ctx, env.svc, env.user, "budget-"+winner.NodeID, winner)
	if err != nil || repeat.ID != first.ID || env.assertReconciled(t).FreezeSum != 10 {
		t.Fatalf("预算已满时同编号重试应返回原任务: %+v %v", repeat, err)
	}
	modified := *loser
	binding := *loser.Collaboration
	higherLimit := 20
	binding.Budget = &model.CanvasGenerationBudgetRequest{Key: "episode-1", Limit: &higherLimit}
	modified.Collaboration = &binding
	_, err = createSingle(ctx, env.svc, env.user, "different-limit", &modified)
	assertTaskCode(t, err, errcode.ErrCanvasConflict.Code)
	if _, err := env.svc.Cancel(ctx, env.user, first.ID); err != nil {
		t.Fatal(err)
	}
	second, err := createSingle(ctx, env.svc, env.user, "budget-"+loser.NodeID, loser)
	if err != nil {
		t.Fatal(err)
	}
	completeCollaborationOutputs(t, env, second.ID, nil)
	_, err = createSingle(ctx, env.svc, env.user, "budget-third", winner)
	assertTaskCode(t, err, errcode.ErrGenerationBudget.Code)
}

func TestCanvasAutomationEnforcesIssuedBudget(t *testing.T) {
	env := newGTIEnv(t, &fakeLimits{initial: 50, defMax: 4})
	ctx := t.Context()
	if err := env.db.AutoMigrate(&model.StoryboardSplitJob{}, &model.CanvasCollaborationRecord{}, &model.CanvasCollaborationReceipt{}, &model.CanvasCollaborationEvent{}, &model.CanvasCollaborationCheckpoint{}, &model.CanvasGenerationBudget{}, &model.CanvasAutomationKey{}, &model.User{}); err != nil {
		t.Fatal(err)
	}
	if err := env.db.Create(&model.User{BaseModel: model.BaseModel{ID: env.user}, Username: "automation-owner", Status: model.UserStatusActive}).Error; err != nil {
		t.Fatal(err)
	}
	req := validCreateReq()
	canvasID := uint64(req.CanvasID)
	payload := json.RawMessage(`{"nodes":[{"id":"n1","type":"canvas","data":{"kind":"video","model":"m1","prompt":"一只猫"}},{"id":"n2","type":"canvas","data":{"kind":"video","model":"m1","prompt":"一只猫"}}],"edges":[]}`)
	if err := env.db.Model(&model.CanvasProject{}).Where("id = ?", canvasID).Update("payload_json", payload).Error; err != nil {
		t.Fatal(err)
	}
	canvas := repository.NewCanvasCollaborationRepository(env.db)
	project, err := canvas.SetActive(ctx, env.user, canvasID, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	limit := 10
	access := service.NewCanvasCollaborationService(canvas, repository.NewCanvasShareRepository(env.db), nil, true)
	issued, err := access.IssueAutomationKey(ctx, model.CanvasAutomationKey{CanvasID: canvasID, UserID: env.user, Epoch: project.CollaborationEpoch,
		Scope: model.CanvasAutomationGenerate, BudgetCredits: &limit}, 1)
	if err != nil {
		t.Fatal(err)
	}
	key, err := access.AuthenticateAutomationKey(ctx, canvasID, issued.Token)
	if err != nil {
		t.Fatal(err)
	}
	scoped := model.WithCanvasAutomation(ctx, key)
	req.Collaboration = &model.CanvasGenerationBinding{Epoch: project.CollaborationEpoch, Revision: project.Revision}
	second := *req
	second.NodeID = "n2"
	first, winner, loser := raceBudgetSubmissions(scoped, t, env, req, &second)
	if repeat, err := createSingle(scoped, env.svc, env.user, "budget-"+winner.NodeID, winner); err != nil || repeat.ID != first.ID {
		t.Fatalf("重试应返回原任务: %+v %v", repeat, err)
	}
	verifyPaidAutomationHTTP(t, env, access, issued, key, first.ID, loser)
	verifyAutomationBudgetScope(t, env, key, first.ID, loser)
}

func verifyAutomationBudgetScope(t *testing.T, env *gtiEnv, key model.CanvasAutomationKey, taskID uint64, request *model.CreateGenerationTaskReq) {
	t.Helper()
	ctx := t.Context()
	scoped := model.WithCanvasAutomation(ctx, key)
	if _, err := env.svc.Get(scoped, env.user, taskID); err != nil {
		t.Fatal(err)
	}
	other := key
	other.ID++
	_, err := env.svc.Get(model.WithCanvasAutomation(ctx, other), env.user, taskID)
	assertTaskCode(t, err, errcode.ErrTaskNotFound.Code)
	request.Collaboration.Budget = &model.CanvasGenerationBudgetRequest{Key: "larger-budget", Limit: key.BudgetCredits}
	_, err = createSingle(scoped, env.svc, env.user, "try-replace-budget", request)
	assertTaskCode(t, err, errcode.ErrInvalidParams.Code)
	canvas := repository.NewCanvasCollaborationRepository(env.db)
	if err := canvas.RevokeAutomationKey(ctx, key.UserID, key.CanvasID, key.ID); err != nil {
		t.Fatal(err)
	}
	if err := canvas.AuthorizeAutomationGeneration(scoped, key); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("吊销后不能授权读取或重试原任务: %v", err)
	}
}

func raceBudgetSubmissions(ctx context.Context, t *testing.T, env *gtiEnv, requests ...*model.CreateGenerationTaskReq) (*model.GenerationTaskView, *model.CreateGenerationTaskReq, *model.CreateGenerationTaskReq) {
	t.Helper()
	type result struct {
		task    *model.GenerationTaskView
		request *model.CreateGenerationTaskReq
		err     error
	}
	results := make(chan result, len(requests))
	for _, request := range requests {
		go func() {
			task, err := createSingle(ctx, env.svc, env.user, "budget-"+request.NodeID, request)
			results <- result{task: task, request: request, err: err}
		}()
	}
	var task *model.GenerationTaskView
	var winner, loser *model.CreateGenerationTaskReq
	for range requests {
		outcome := <-results
		if outcome.err != nil {
			assertTaskCode(t, outcome.err, errcode.ErrGenerationBudget.Code)
			loser = outcome.request
			continue
		}
		if winner != nil {
			t.Fatal("累计预算只够受理一笔任务")
		}
		task, winner = outcome.task, outcome.request
	}
	if winner == nil || loser == nil {
		t.Fatal("预期接受一笔并拒绝一笔")
	}
	return task, winner, loser
}

func seedMemberGeneration(t *testing.T, env *gtiEnv) *memberGenerationScenario {
	t.Helper()
	ctx := t.Context()
	if err := env.db.AutoMigrate(&model.User{}, &model.CanvasViewer{}, &model.StoryboardSplitJob{}, &model.CanvasCollaborationRecord{}, &model.CanvasCollaborationReceipt{}, &model.CanvasCollaborationEvent{}, &model.CanvasCollaborationCheckpoint{}); err != nil {
		t.Fatal(err)
	}
	member := &model.User{Username: "billing-member", Email: "billing-member@example.test"}
	if err := env.db.Create(member).Error; err != nil {
		t.Fatal(err)
	}
	req := validCreateReq()
	canvasID := uint64(req.CanvasID)
	if err := env.db.Model(&model.CanvasProject{}).Where("id = ?", canvasID).Update("payload_json", json.RawMessage(`{"nodes":[{"id":"n1","type":"canvas","data":{"kind":"video","label":"镜头","model":"m1","prompt":"一只猫"}}],"edges":[]}`)).Error; err != nil {
		t.Fatal(err)
	}
	canvas := repository.NewCanvasCollaborationRepository(env.db)
	project, err := canvas.SetActive(ctx, env.user, canvasID, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	share := repository.NewCanvasShareRepository(env.db)
	if err := share.GrantRole(ctx, env.user, canvasID, member.ID, model.CanvasRoleEditor); err != nil {
		t.Fatal(err)
	}
	if err := share.SetGenerationPermission(ctx, env.user, canvasID, member.ID, true); err != nil {
		t.Fatal(err)
	}
	req.Collaboration = &model.CanvasGenerationBinding{Epoch: project.CollaborationEpoch, Revision: project.Revision}
	const sameKey = "same-key-for-two-accounts"
	ownerTask, err := createSingle(ctx, env.svc, env.user, sameKey, req)
	if err != nil {
		t.Fatal(err)
	}
	batch := repository.CanvasCollaborationBatch{OpID: "member-confirm-input", Epoch: project.CollaborationEpoch, BaseRevision: project.Revision + 1,
		Changes: []repository.CanvasCollaborationChange{{Kind: "node", ID: req.NodeID, Fields: map[string]json.RawMessage{
			"data.generationInput": json.RawMessage(`{"model":"m1","input":{"prompt":"一只猫"}}`),
		}}}}
	confirmed, err := canvas.Commit(ctx, member.ID, canvasID, batch)
	if err != nil || len(confirmed.Changes) != 1 {
		t.Fatalf("成员确认生成输入失败: %+v, %v", confirmed, err)
	}
	req.Collaboration.Revision = confirmed.Revision
	memberTask, err := createSingle(ctx, env.svc, member.ID, sameKey, req)
	if err != nil || memberTask.ID == ownerTask.ID {
		t.Fatalf("两个账号相同的请求编号应产生独立任务: %+v, %v", memberTask, err)
	}
	return &memberGenerationScenario{
		canvas: canvas, share: share, req: req, member: member.ID, ownerTask: ownerTask.ID, memberTask: memberTask.ID,
	}
}

type memberGenerationScenario struct {
	canvas     *repository.CanvasCollaborationRepository
	share      *repository.CanvasShareRepository
	req        *model.CreateGenerationTaskReq
	member     uint64
	ownerTask  uint64
	memberTask uint64
}

func assertMemberGenerationAccounting(t *testing.T, env *gtiEnv, scenario *memberGenerationScenario) {
	t.Helper()
	ctx := t.Context()
	retry, err := createSingle(ctx, env.svc, scenario.member, "same-key-for-two-accounts", scenario.req)
	if err != nil || retry.ID != scenario.memberTask {
		t.Fatalf("成员重试必须命中自己的任务: %+v, %v", retry, err)
	}
	memberCredits, err := env.store.Reconcile(ctx, scenario.member)
	if err != nil || memberCredits.FreezeSum != 10 || memberCredits.Frozen != 10 || env.assertReconciled(t).FreezeSum != 10 {
		t.Fatalf("各发起人只冻结一次各自的积分: %+v, %v", memberCredits, err)
	}
	if _, err := env.svc.Get(ctx, env.user, scenario.memberTask); err == nil {
		t.Fatal("画布所有者不能查看成员的私有任务")
	}
	if _, err := env.svc.Cancel(ctx, env.user, scenario.ownerTask); err != nil {
		t.Fatal(err)
	}
	canvasID := uint64(scenario.req.CanvasID)
	if err := scenario.share.SetGenerationPermission(ctx, env.user, canvasID, scenario.member, false); err != nil {
		t.Fatal(err)
	}
	completeCollaborationTask(t, env, scenario.memberTask)
	memberCredits, err = env.store.Reconcile(ctx, scenario.member)
	if err != nil || memberCredits.SettleSum != 10 || memberCredits.Frozen != 0 || memberCredits.BalanceDiff() != 0 || memberCredits.ActiveDiff() != 0 {
		t.Fatalf("撤权后的任务仍归发起账号结算: %+v, %v", memberCredits, err)
	}
	if ownerCredits := env.assertReconciled(t); ownerCredits.RefundSum != 10 || ownerCredits.SettleSum != 0 {
		t.Fatalf("取消排队中的任务只退还所有者的积分: %+v", ownerCredits)
	}
	view, _, err := scenario.canvas.Snapshot(ctx, env.user, canvasID)
	if err != nil || bytes.Contains(view.PayloadJSON, []byte(`/files/result.mp4`)) || bytes.Contains(view.PayloadJSON, []byte(`"status":"running"`)) {
		t.Fatalf("撤权后的成员结果不得进入画布，运行状态应释放: %+v, %v", view, err)
	}
}

func completeCollaborationTask(t *testing.T, env *gtiEnv, id uint64) {
	t.Helper()
	completeCollaborationOutputs(t, env, id, []model.TaskOutput{{AssetID: 1, URL: "/files/result.mp4", MediaType: "video"}})
}

func completeCollaborationOutputs(t *testing.T, env *gtiEnv, id uint64, outputs []model.TaskOutput) {
	t.Helper()
	ctx := t.Context()
	task, err := env.store.GetByIDAny(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if applied, err := env.svc.MarkSubmitted(ctx, task, "collab-provider", nil, time.Now()); err != nil || !applied {
		t.Fatalf("提交模型任务失败: %v", err)
	}
	if applied, err := env.svc.MarkFinalizing(ctx, task); err != nil || !applied {
		t.Fatalf("进入转存失败: %v", err)
	}
	if applied, err := env.svc.Complete(ctx, task, outputs, nil); err != nil || !applied {
		t.Fatalf("回填失败: %v", err)
	}
}
