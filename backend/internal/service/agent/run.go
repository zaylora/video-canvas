package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
)

// StartRun 发起一轮运行：用户发了一条消息。运行创建后交给 runtime 异步执行，接口立刻返回。
func (s *AgentService) StartRun(ctx context.Context, userID, sessionID uint64, req *model.StartAgentRunReq) (*AgentRunView, error) {
	// 1. 会话必须是自己的；消息去掉首尾空白后不能为空
	sess, err := s.repo.GetSession(ctx, userID, sessionID)
	if err != nil {
		return nil, sessionErr(err)
	}
	msg := strings.TrimSpace(req.Message)
	if msg == "" {
		return nil, errcode.ErrInvalidParams.WithMsg("消息不能为空")
	}
	// 2. 消息里的引用（节点、模型、技能）必须有效；选中的节点里已不存在的去掉
	selection, err := s.checkRefs(ctx, userID, sess.CanvasID, msg, req.Selection)
	if err != nil {
		return nil, err
	}
	req.Selection = selection
	// 3. 补默认值：模式沿用会话的，预算默认 50
	mode := firstNonEmpty(req.Mode, sess.Mode, model.AgentModeAll)
	budget := defaultRunBudget
	if req.BudgetCredits != nil {
		budget = *req.BudgetCredits
	}
	// 4. 选 Agent 模型：请求里的 > 会话上次用的 > 清单第一个；必须是已发布的，否则 60002
	modelKey, err := s.pickModel(ctx, firstNonEmpty(req.AgentModelKey, sess.ModelKey))
	if err != nil {
		return nil, err
	}
	// 5. 创建运行。同一画布同时只能有一个活跃运行，由数据库的部分唯一索引保证，并发点两次发送也只有一个成功
	run := &model.AgentRun{SessionID: sess.ID, CanvasID: sess.CanvasID, UserID: userID, Status: model.RunQueued,
		Mode: mode, BudgetCredits: budget, MaxSteps: defaultRunMaxSteps}
	if err := s.repo.CreateRun(ctx, run); err != nil {
		if errors.Is(err, repository.ErrDuplicate) {
			return nil, errcode.ErrAgentRunActive
		}
		return nil, err
	}
	// 6. 记住这次的模式和模型；首条消息顺便当会话标题（会话还叫默认名时）
	fields := map[string]any{"mode": mode, "model_key": modelKey}
	if sess.Title == defaultAgentTitle {
		fields["title"] = cleanTitle(msg, defaultAgentTitle)
	}
	if err := s.repo.UpdateSession(ctx, userID, sessionID, fields); err != nil {
		return nil, sessionErr(err)
	}
	s.emit(ctx, userID, sess.ID, run.ID, sess.CanvasID, "message.user", map[string]any{"text": msg, "mode": mode, "selection": req.Selection})
	s.emit(ctx, userID, sess.ID, run.ID, sess.CanvasID, "run.status", map[string]any{"status": run.Status})
	// 7. 交给 runtime。启动失败说明运行时不可用：把运行标为失败，画布立刻空出来，返回 60005
	in := model.AgentRunInput{Message: msg, Mode: mode, Selection: req.Selection, Viewport: req.Viewport, ModelKey: modelKey}
	if err := s.runtime.Start(ctx, run, in); err != nil {
		s.failRun(ctx, run, errcode.ErrAgentUnavailable.Code, err)
		return nil, errcode.ErrAgentUnavailable
	}
	return runView(run), nil
}

// Interject 在运行中插话：补充要求，不打断当前步骤。
func (s *AgentService) Interject(ctx context.Context, userID, runID uint64, message string) error {
	// 1. 运行必须是自己的，并且正在跑；等用户确认时请先处理卡片
	run, err := s.repo.GetRun(ctx, userID, runID)
	if err != nil {
		return runErr(err)
	}
	msg := strings.TrimSpace(message)
	if msg == "" {
		return errcode.ErrInvalidParams.WithMsg("消息不能为空")
	}
	if run.Status != model.RunRunning {
		return errcode.ErrAgentState
	}
	if _, err := s.checkRefs(ctx, run.UserID, run.CanvasID, msg, nil); err != nil {
		return err
	}
	// 2. 先交给 runtime，成功了再记事件，免得记了一条没送达的插话
	if err := s.runtime.Interject(ctx, runID, msg); err != nil {
		return errcode.ErrAgentUnavailable
	}
	s.emit(ctx, run.UserID, run.SessionID, run.ID, run.CanvasID, "message.user", map[string]any{"text": msg, "steer": true})
	return nil
}

// Cancel 停止运行：已经完成的画布改动保留，等待中的审批失效。
func (s *AgentService) Cancel(ctx context.Context, userID, runID uint64) (*AgentRunView, error) {
	// 1. 运行必须是自己的，并且还在占用画布
	run, err := s.repo.GetRun(ctx, userID, runID)
	if err != nil {
		return nil, runErr(err)
	}
	// 2. CAS 迁移到 canceled：运行刚好自己结束时没命中，按「状态不允许」返回，而不是覆盖掉真实结果
	now := s.now()
	updated, err := s.repo.UpdateRunIf(ctx, runID, model.ActiveRunStatuses, map[string]any{"status": model.RunCanceled, "ended_at": now})
	if errors.Is(err, repository.ErrAgentStateConflict) {
		return nil, errcode.ErrAgentState
	}
	if err != nil {
		return nil, err
	}
	// 3. 待处理的审批一并失效；通知 runtime 中止（它已经不在也不是错误）
	if _, err := s.repo.ExpirePendingApprovals(ctx, runID); err != nil {
		return nil, err
	}
	_ = s.runtime.Cancel(ctx, runID) // 画布状态已经以数据库为准，runtime 没收到只会让它多跑一会儿，之后写画布会被状态校验挡住
	s.emit(ctx, run.UserID, run.SessionID, run.ID, run.CanvasID, "run.status", map[string]any{"status": model.RunCanceled})
	return runView(updated), nil
}

// Resume 让被中断、预算用尽或步数用尽的运行继续，可以追加预算。
func (s *AgentService) Resume(ctx context.Context, userID, runID uint64, addBudget int) (*AgentRunView, error) {
	// 1. 运行必须是自己的，并且处于可继续的状态
	run, err := s.repo.GetRun(ctx, userID, runID)
	if err != nil {
		return nil, runErr(err)
	}
	if !contains(model.ResumableRunStatuses, run.Status) {
		return nil, errcode.ErrAgentState
	}
	// 2. 预算用尽时必须追加预算，否则一继续又会立刻用尽
	if run.Status == model.RunBudgetExhausted && addBudget <= 0 {
		return nil, errcode.ErrInvalidParams.WithMsg("预算已用尽，请追加预算")
	}
	// 3. 回到 queued 重新占用画布。这期间别的运行可能已经占了画布，由唯一索引兜底，返回 60001；
	//    步数用尽时把上限也放宽一轮，否则继续后马上又到上限
	fields := map[string]any{"status": model.RunQueued, "error_code": "", "error_message": "", "ended_at": nil}
	if addBudget > 0 {
		fields["budget_credits"] = run.BudgetCredits + addBudget
	}
	if run.Status == model.RunStepLimit {
		fields["max_steps"] = run.MaxSteps + defaultRunMaxSteps
	}
	updated, err := s.repo.UpdateRunIf(ctx, runID, model.ResumableRunStatuses, fields)
	switch {
	case errors.Is(err, repository.ErrAgentStateConflict):
		return nil, errcode.ErrAgentState
	case errors.Is(err, repository.ErrDuplicate):
		return nil, errcode.ErrAgentRunActive
	case err != nil:
		return nil, err
	}
	s.emit(ctx, run.UserID, run.SessionID, run.ID, run.CanvasID, "run.status", map[string]any{"status": model.RunQueued, "resumed": true})
	if err := s.runtime.Resume(ctx, updated, ResumeInfo{Reason: "resume"}); err != nil {
		s.failRun(ctx, updated, errcode.ErrAgentUnavailable.Code, err)
		return nil, errcode.ErrAgentUnavailable
	}
	return runView(updated), nil
}

// failRun 把运行标为失败并释放画布。已经不在活跃状态的运行不动。
func (s *AgentService) failRun(ctx context.Context, run *model.AgentRun, code int, cause error) {
	msg := fmt.Sprintf("%v", cause)
	if utf8.RuneCountInString(msg) > 200 {
		msg = string([]rune(msg)[:200])
	}
	_, err := s.repo.UpdateRunIf(ctx, run.ID, model.ActiveRunStatuses, map[string]any{
		"status": model.RunFailed, "error_code": fmt.Sprint(code), "error_message": errcode.ErrAgentUnavailable.Msg, "ended_at": s.now(),
	})
	if err == nil {
		s.emit(ctx, run.UserID, run.SessionID, run.ID, run.CanvasID, "run.status", map[string]any{"status": model.RunFailed, "error": msg})
	}
}

// pickModel 选本轮用的 Agent 模型：优先 want，其次清单第一个；want 不在已发布清单里返回 60002。
func (s *AgentService) pickModel(ctx context.Context, want string) (string, error) {
	list, err := s.models.List(ctx)
	if err != nil {
		return "", err
	}
	if len(list) == 0 {
		return "", errcode.ErrAgentModelNA
	}
	if want == "" {
		return list[0].Key, nil
	}
	for _, m := range list {
		if m.Key == want {
			return want, nil
		}
	}
	return "", errcode.ErrAgentModelNA
}

// firstNonEmpty 返回第一个非空字符串。
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// contains 判断切片里有没有某个元素。
func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
