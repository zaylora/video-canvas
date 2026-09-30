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

// MarkSubmitted 记录异步任务已被上游受理。
func (s *GenerationTaskService) MarkSubmitted(ctx context.Context, t *model.GenerationTask, providerTaskID string, pluginState json.RawMessage, nextPollAt time.Time) (bool, error) {
	fields := map[string]any{
		"status":           model.TaskQueued,
		"provider_task_id": providerTaskID,
		"submitted_at":     s.now(),
		"next_poll_at":     nextPollAt,
		"poll_attempts":    0,
		"lease_until":      nil,
	}
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
	fields := map[string]any{
		"status":           model.TaskFinalizing,
		"provider_task_id": providerTaskID,
		"provider_result":  datatypes.JSON(outputs),
		"next_poll_at":     s.now(),
		"poll_attempts":    0,
		"lease_until":      nil,
	}
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

// Complete 完成转存并结算积分。
func (s *GenerationTaskService) Complete(ctx context.Context, t *model.GenerationTask, outputs []model.TaskOutput) (bool, error) {
	if outputs == nil {
		outputs = []model.TaskOutput{}
	}
	raw, err := json.Marshal(outputs)
	if err != nil {
		return false, err
	}
	updated, applied, err := s.finish(ctx, t.ID, []string{model.TaskFinalizing}, model.TaskSucceeded, map[string]any{
		"output_json":   datatypes.JSON(raw),
		"progress":      100,
		"error_code":    "",
		"error_message": "",
	})
	if err != nil || !applied {
		return false, err
	}
	s.publish(ctx, updated)
	return true, nil
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
	updated, applied, err := s.finish(ctx, t.ID, model.ActiveTaskStatuses, model.TaskExpired, map[string]any{
		"error_code":    TaskErrTimeout,
		"error_message": taskMsgTimeout,
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
		ledgerType, dBalance := model.LedgerRefund, 0
		if target == model.TaskSucceeded {
			ledgerType, dBalance = model.LedgerSettle, -t.Credits
		}
		inserted, err := tx.InsertLedger(ctx, &model.CreditLedger{
			UserID: t.UserID, TaskID: t.ID, Type: ledgerType, Amount: t.Credits,
		})
		if err != nil {
			return err
		}
		if !inserted {
			logger.Warn("积分流水已存在，跳过重复结算", zap.Uint64("task_id", t.ID), zap.String("type", ledgerType))
			return nil
		}
		return tx.AddCredit(ctx, t.UserID, dBalance, -t.Credits)
	})
	if errors.Is(err, repository.ErrStateConflict) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return updated, true, nil
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
