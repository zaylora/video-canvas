package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"
	"gorm.io/datatypes"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/repository"
)

// ClaimDue 领取到期任务并写入租约。
func (s *GenerationTaskService) ClaimDue(ctx context.Context, limit int, lease time.Duration) ([]model.GenerationTask, error) {
	return s.repo.ClaimDue(ctx, s.now(), lease, limit)
}

// ExtendLease 续租，防止长时间操作期间任务被其它 worker 重复领取。
func (s *GenerationTaskService) ExtendLease(ctx context.Context, id uint64, lease time.Duration) (bool, error) {
	return s.repo.ExtendLease(ctx, id, s.now().Add(lease))
}

// SaveProviderState 保存插件的准备结果与私有状态，不改变任务状态。
func (s *GenerationTaskService) SaveProviderState(ctx context.Context, t *model.GenerationTask, st provider.ProviderState) error {
	raw, err := json.Marshal(st)
	if err != nil {
		return fmt.Errorf("编码 provider state 失败: %w", err)
	}
	updated, err := s.repo.UpdateIf(ctx, t.ID, model.ActiveTaskStatuses, map[string]any{
		"provider_state": datatypes.JSON(raw),
	}, false)
	if errors.Is(err, repository.ErrStateConflict) {
		return nil
	}
	if err != nil {
		return err
	}
	if updated == nil {
		return nil
	}
	return nil
}

// SaveTrace 保存试跑任务的追踪步骤，正式任务与空追踪直接忽略。
func (s *GenerationTaskService) SaveTrace(ctx context.Context, t *model.GenerationTask, steps []provider.TraceStep) error {
	if t == nil || !t.IsTest || len(steps) == 0 {
		return nil
	}
	raw, err := json.Marshal(steps)
	if err != nil {
		return fmt.Errorf("编码试跑追踪失败: %w", err)
	}
	return s.repo.SaveTrace(ctx, t.ID, datatypes.JSON(raw))
}

// restartDeadline 把任务的超时时间改成“从现在起一个完整的模型超时”。
// 创建任务时 deadline_at = 创建时间 + 超时，pending 阶段它就是排队的上限；任务真正交给上游后，
// 生成时长应该从这一刻重新算，不能让排队的时间吃掉生成的时间。快照解析失败时保持原值，不影响提交结果。
func (s *GenerationTaskService) restartDeadline(t *model.GenerationTask, now time.Time, fields map[string]any) {
	var snap provider.Snapshot
	if err := json.Unmarshal(t.ConfigSnapshot, &snap); err != nil {
		logger.Warn("解析任务快照失败，保持原有截止时间", zap.Uint64("task_id", t.ID), zap.Error(err))
		return
	}
	fields["deadline_at"] = now.Add(taskDeadline(&snap))
}

// MarkSubmitted 记录异步任务已被上游受理，并从受理时刻起重新计算生成超时。
func (s *GenerationTaskService) MarkSubmitted(ctx context.Context, t *model.GenerationTask, providerTaskID string, pluginState json.RawMessage, nextPollAt time.Time) (bool, error) {
	now := s.now()
	fields := map[string]any{
		"status":           model.TaskQueued,
		"provider_task_id": providerTaskID,
		"submitted_at":     now,
		"next_poll_at":     nextPollAt,
		"poll_attempts":    0,
		"lease_until":      nil,
	}
	s.restartDeadline(t, now, fields)
	if len(pluginState) > 0 {
		raw, err := mergeProviderState(t.ProviderState, provider.ProviderState{Plugin: pluginState})
		if err != nil {
			return false, err
		}
		fields["provider_state"] = datatypes.JSON(raw)
	}
	updated, applied, err := s.transit(ctx, t.ID, []string{model.TaskPending}, fields, true)
	if err != nil || !applied {
		return false, err
	}
	s.publish(ctx, updated)
	return true, nil
}

// MarkImmediate 记录同步接口的即时结果并进入转存阶段。
func (s *GenerationTaskService) MarkImmediate(ctx context.Context, t *model.GenerationTask, providerTaskID string, pluginState json.RawMessage, outputs json.RawMessage) (bool, error) {
	now := s.now()
	fields := map[string]any{
		"status":           model.TaskFinalizing,
		"provider_task_id": providerTaskID,
		"provider_result":  datatypes.JSON(outputs),
		"next_poll_at":     now,
		"poll_attempts":    0,
		"lease_until":      nil,
	}
	// 转存窗口是 deadline_at + TransferGrace：同样从上游出结果的时刻重新算，别让排队时间挤掉转存的余量
	s.restartDeadline(t, now, fields)
	if len(pluginState) > 0 {
		raw, err := mergeProviderState(t.ProviderState, provider.ProviderState{Plugin: pluginState})
		if err != nil {
			return false, err
		}
		fields["provider_state"] = datatypes.JSON(raw)
	}
	updated, applied, err := s.transit(ctx, t.ID, []string{model.TaskPending}, fields, true)
	if err != nil || !applied {
		return false, err
	}
	s.publish(ctx, updated)
	s.Kick()
	return true, nil
}

// MarkPolled 记录上游仍在处理的状态与插件状态。
func (s *GenerationTaskService) MarkPolled(ctx context.Context, t *model.GenerationTask, status string, progress *int, pluginState json.RawMessage, attempts int, nextPollAt time.Time) (bool, error) {
	if status != model.TaskQueued && status != model.TaskRunning {
		return false, fmt.Errorf("MarkPolled 不支持的状态：%s", status)
	}
	fields := map[string]any{
		"status":        status,
		"poll_attempts": attempts,
		"next_poll_at":  nextPollAt,
		"lease_until":   nil,
	}
	changed := status != t.Status
	if progress != nil {
		fields["progress"] = *progress
		changed = changed || t.Progress == nil || *t.Progress != *progress
	}
	if len(pluginState) > 0 {
		raw, err := mergeProviderState(t.ProviderState, provider.ProviderState{Plugin: pluginState})
		if err != nil {
			return false, err
		}
		fields["provider_state"] = datatypes.JSON(raw)
	}
	updated, applied, err := s.transit(ctx, t.ID, []string{model.TaskQueued, model.TaskRunning}, fields, changed)
	if err != nil || !applied {
		return false, err
	}
	if changed {
		s.publish(ctx, updated)
	}
	return true, nil
}

// MarkFinalizing 把已成功的上游任务转入产物转存阶段。
func (s *GenerationTaskService) MarkFinalizing(ctx context.Context, t *model.GenerationTask) (bool, error) {
	updated, applied, err := s.transit(ctx, t.ID, []string{model.TaskQueued, model.TaskRunning}, map[string]any{
		"status":        model.TaskFinalizing,
		"progress":      100,
		"poll_attempts": 0,
		"next_poll_at":  s.now(),
		"lease_until":   nil,
	}, true)
	if err != nil || !applied {
		return false, err
	}
	s.publish(ctx, updated)
	s.Kick()
	return true, nil
}

// Retry 安排一次可重试的任务处理。
func (s *GenerationTaskService) Retry(ctx context.Context, t *model.GenerationTask, attempts int, nextPollAt time.Time) (bool, error) {
	_, applied, err := s.transit(ctx, t.ID, []string{t.Status}, map[string]any{
		"poll_attempts": attempts,
		"next_poll_at":  nextPollAt,
		"lease_until":   nil,
	}, false)
	return applied, err
}

// Complete 完成转存并结算积分。Token 计费按 usage 结算：扣 min(实际, 冻结)，差额退回；没有 usage 按冻结额扣并记日志。
func (s *GenerationTaskService) Complete(ctx context.Context, t *model.GenerationTask, outputs []model.TaskOutput, usage *modelcfg.Usage) (bool, error) {
	if outputs == nil {
		outputs = []model.TaskOutput{}
	}
	raw, err := json.Marshal(outputs)
	if err != nil {
		return false, err
	}
	charge := s.settleAmount(t, usage)
	updated, applied, err := s.finish(ctx, t.ID, []string{model.TaskFinalizing}, model.TaskSucceeded, map[string]any{
		"output_json":     datatypes.JSON(raw),
		"progress":        100,
		"error_code":      "",
		"error_message":   "",
		"charged_credits": charge,
	})
	if err != nil || !applied {
		return false, err
	}
	s.publish(ctx, updated)
	return true, nil
}

// settleAmount 按任务快照里的定价算成功时实际扣的积分（不超过冻结额）。快照解析失败时按冻结额扣，不让用户少付也不多付。
func (s *GenerationTaskService) settleAmount(t *model.GenerationTask, usage *modelcfg.Usage) int {
	var snap provider.Snapshot
	if err := json.Unmarshal(t.ConfigSnapshot, &snap); err != nil {
		logger.Warn("解析任务快照失败，按冻结额结算", zap.Uint64("task_id", t.ID), zap.Error(err))
		return t.Credits
	}
	if snap.Model.Pricing.Billing == modelcfg.BillingToken && usage == nil {
		logger.Warn("Token 计费的任务没有回传用量，按冻结额结算", zap.Uint64("task_id", t.ID), zap.String("model", t.ModelKey))
	}
	return max(modelcfg.Settle(snap.Model.Pricing, t.Credits, usage), 0)
}

// Fail 把非终态任务置为失败并退回积分。
func (s *GenerationTaskService) Fail(ctx context.Context, t *model.GenerationTask, errorCode, message string) (bool, error) {
	updated, applied, err := s.finish(ctx, t.ID, model.ActiveTaskStatuses, model.TaskFailed, map[string]any{
		"error_code":    errorCode,
		"error_message": message,
	})
	if err != nil || !applied {
		return false, err
	}
	s.publish(ctx, updated)
	return true, nil
}

// Expire 把超时任务置为 expired 并退回积分。
func (s *GenerationTaskService) Expire(ctx context.Context, t *model.GenerationTask) (bool, error) {
	// 从没调用过上游（pending 且没有过提交重试）就超时，说明是在排队：文案要让用户知道不是生成失败
	msg := taskMsgTimeout
	if t.Status == model.TaskPending && t.PollAttempts == 0 {
		msg = taskMsgQueueTimeout
	}
	updated, applied, err := s.finish(ctx, t.ID, model.ActiveTaskStatuses, model.TaskExpired, map[string]any{
		"error_code":    TaskErrTimeout,
		"error_message": msg,
	})
	if err != nil || !applied {
		return false, err
	}
	s.publish(ctx, updated)
	return true, nil
}

// Cancel 软取消任务；取消成功后尽力通知插件执行器取消上游任务。
func (s *GenerationTaskService) Cancel(ctx context.Context, userID, id uint64) (*model.GenerationTaskView, error) {
	t, err := s.repo.GetByID(ctx, userID, id)
	if errors.Is(err, repository.ErrNotFound) || (err == nil && t.IsTest) {
		return nil, errcode.ErrTaskNotFound
	}
	if err != nil {
		return nil, err
	}
	if model.IsTerminalStatus(t.Status) {
		return nil, errcode.ErrTaskNotCancelable
	}
	updated, applied, err := s.finish(ctx, id, model.ActiveTaskStatuses, model.TaskCanceled, map[string]any{
		"error_code":    TaskErrCanceled,
		"error_message": taskMsgCanceled,
	})
	if err != nil {
		return nil, err
	}
	if !applied {
		return nil, errcode.ErrTaskNotCancelable
	}
	s.cancelProvider(ctx, updated)
	s.publish(ctx, updated)
	return taskView(updated), nil
}

// CancelActiveByUser 取消用户全部进行中的正式任务并退还冻结（后台封禁并取消任务用）。
// 每个任务复用 Cancel 的完整流程（状态迁移 + 结算 / 退款流水 + 退还冻结在同一事务里、通知上游取消、推送），不另写积分逻辑。
// 单个任务失败不影响其他任务：失败的 id 放进结果，已成功的不回滚；只有查询进行中任务失败才返回 error。
func (s *GenerationTaskService) CancelActiveByUser(ctx context.Context, userID uint64) (*CancelActiveResult, error) {
	// 1. 取该用户全部非终态的正式任务（试跑任务不占积分，不在其中）
	tasks, err := s.repo.ListActive(ctx, userID)
	if err != nil {
		return nil, err
	}
	// 2. 逐个取消。任务在查询之后被别的流程结束（返回“不可取消”）说明目标已达成，既不算取消也不算失败
	res := &CancelActiveResult{}
	for i := range tasks {
		_, err := s.Cancel(ctx, userID, tasks[i].ID)
		var ec *errcode.Error
		switch {
		case err == nil:
			res.Canceled++
		case errors.As(err, &ec) && ec.Code == errcode.ErrTaskNotCancelable.Code:
		default:
			logger.Warn("取消进行中任务失败", zap.Error(err), zap.Uint64("task_id", tasks[i].ID), zap.Uint64("user_id", userID))
			res.Failed = append(res.Failed, tasks[i].ID)
		}
	}
	return res, nil
}

func (s *GenerationTaskService) cancelProvider(ctx context.Context, t *model.GenerationTask) {
	if t == nil || t.ProviderTaskID == "" || s.executor == nil {
		return
	}
	var snap provider.Snapshot
	if err := json.Unmarshal(t.ConfigSnapshot, &snap); err != nil {
		logger.Warn("解析任务快照失败，跳过上游取消", zap.Uint64("task_id", t.ID), zap.Error(err))
		return
	}
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), executorCancelTimeout)
	defer cancel()
	err := s.executor.Cancel(cctx, &snap, provider.TaskRef{
		ID: t.ID, UserID: t.UserID, ProviderTaskID: t.ProviderTaskID,
	})
	if err != nil && !errors.Is(err, provider.ErrCancelUnsupported) {
		logger.Warn("通知上游取消任务失败", zap.Uint64("task_id", t.ID), zap.String("channel", t.Provider), zap.Error(err))
	}
}

func (s *GenerationTaskService) transit(ctx context.Context, id uint64, from []string, fields map[string]any, bumpVersion bool) (*model.GenerationTask, bool, error) {
	t, err := s.repo.UpdateIf(ctx, id, from, fields, bumpVersion)
	if errors.Is(err, repository.ErrStateConflict) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return t, true, nil
}

func (s *GenerationTaskService) finish(ctx context.Context, id uint64, from []string, target string, extra map[string]any) (*model.GenerationTask, bool, error) {
	fields := map[string]any{"status": target, "finished_at": s.now(), "lease_until": nil}
	for k, v := range extra {
		fields[k] = v
	}
	var updated *model.GenerationTask
	err := s.repo.WithTx(ctx, func(tx repository.GenerationTaskTx) error {
		t, err := tx.UpdateIf(ctx, id, from, fields, true)
		if err != nil {
			return err
		}
		updated = t
		if t.IsTest {
			return nil
		}
		return settleLedger(ctx, tx, t, target)
	})
	if errors.Is(err, repository.ErrStateConflict) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return updated, true, nil
}

// settleLedger 写结算 / 退款流水并解冻：成功时扣 charged_credits（没有则扣冻结额），多冻结的部分再写一笔 refund；
// 失败 / 取消 / 超时全额退回。流水 UNIQUE(task_id, type) 保证重复结算只生效一次。
func settleLedger(ctx context.Context, tx repository.GenerationTaskTx, t *model.GenerationTask, target string) error {
	frozen, charge := t.Credits, 0
	if target == model.TaskSucceeded {
		charge = frozen
		if t.ChargedCredits != nil {
			charge = min(max(*t.ChargedCredits, 0), frozen)
		}
	}
	entries := []model.CreditLedger{}
	if charge > 0 || target == model.TaskSucceeded {
		entries = append(entries, model.CreditLedger{UserID: t.UserID, TaskID: model.TaskIDPtr(t.ID), Type: model.LedgerSettle, Amount: charge})
	}
	if refund := frozen - charge; refund > 0 || target != model.TaskSucceeded {
		entries = append(entries, model.CreditLedger{UserID: t.UserID, TaskID: model.TaskIDPtr(t.ID), Type: model.LedgerRefund, Amount: refund})
	}
	for i := range entries {
		inserted, err := tx.InsertLedger(ctx, &entries[i])
		if err != nil {
			return err
		}
		if !inserted {
			logger.Warn("积分流水已存在，跳过重复结算", zap.Uint64("task_id", t.ID), zap.String("type", entries[i].Type))
			return nil
		}
	}
	return tx.AddCredit(ctx, t.UserID, -charge, -frozen)
}

func mergeProviderState(raw []byte, next provider.ProviderState) ([]byte, error) {
	current, ok := provider.DecodeProviderState(raw)
	if !ok {
		current = provider.ProviderState{}
	}
	if len(next.Prepared) > 0 {
		current.Prepared = next.Prepared
	}
	if len(next.Plugin) > 0 {
		current.Plugin = next.Plugin
	}
	out, err := json.Marshal(current)
	if err != nil {
		return nil, fmt.Errorf("编码 provider state 失败: %w", err)
	}
	return out, nil
}
