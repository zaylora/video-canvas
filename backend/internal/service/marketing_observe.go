// 本文件是出片任务的对账、观察者回调和创建任务时的守卫。
package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/repository"

	"go.uber.org/zap"
)

// reconcileScript 按出片任务对账脚本：状态、任务列表、最近失败原因；成功的成片带封面时更新项目封面。
func (s *MarketingService) reconcileScript(ctx context.Context, userID, id uint64) (*model.MarketingScript, error) {
	sc, err := s.repo.Script(ctx, userID, id, false)
	if err != nil {
		return nil, err
	}
	tasks, err := s.repo.ScriptTasks(ctx, userID, []string{marketingNode(id)})
	if err != nil {
		return nil, err
	}
	status, msg := deriveScriptStatus(sc.Status, tasks)
	ids := taskIDsJSON(tasks)
	if status != sc.Status || msg != sc.Error || !sameJSON(ids, sc.TaskIDs) {
		if err := s.repo.UpdateScript(ctx, id, map[string]any{"status": status, "error": msg, "task_ids": ids}); err != nil {
			return nil, err
		}
		sc.Status, sc.Error, sc.TaskIDs = status, msg, ids
	}
	return sc, nil
}

// OnTaskChanged 实现 GenerationTaskObserver：营销脚本的出片任务状态变化后对账脚本、更新项目封面并推送 marketing.script。
// 不是营销出片任务、脚本已删除的一律忽略。
func (s *MarketingService) OnTaskChanged(ctx context.Context, task *model.GenerationTask) error {
	if task == nil || task.IsTest || task.Kind != model.KindVideo || !strings.HasPrefix(task.NodeID, model.MarketingNodePrefix) {
		return nil
	}
	sid, ok := idcodec.Decode(strings.TrimPrefix(task.NodeID, model.MarketingNodePrefix))
	if !ok {
		return nil
	}
	sc, err := s.reconcileScript(ctx, task.UserID, sid)
	if mktNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if task.Status == model.TaskSucceeded {
		s.updateCover(ctx, sc, task)
	}
	v, err := s.scriptViewByRow(ctx, task.UserID, sc)
	if mktNotFound(err) || errors.Is(err, errcode.ErrMarketingNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	s.publishScript(ctx, task.UserID, sc, v)
	return nil
}

// updateCover 成片带封面时把项目封面换成最新成片的首帧；失败只记日志，不影响脚本状态。
func (s *MarketingService) updateCover(ctx context.Context, sc *model.MarketingScript, task *model.GenerationTask) {
	var outputs []model.TaskOutput
	if err := json.Unmarshal(task.OutputJSON, &outputs); err != nil {
		return
	}
	for _, o := range outputs {
		if o.MediaType == model.KindVideo && o.CoverAssetID > 0 {
			if err := s.repo.UpdateProject(ctx, sc.ProjectID, map[string]any{"cover_asset_id": o.CoverAssetID}); err != nil {
				logger.Warn("更新营销项目封面失败", zap.Uint64("project_id", sc.ProjectID), zap.Error(err))
			}
			return
		}
	}
}

// GuardMarketingGeneration 在生成任务自身事务里锁住营销项目和脚本，确认任务只能提给本人的、没有在出片或重写的脚本：
// 脚本不存在或不属于该用户返回 68001；正在出片 / 重写返回 68006。它由生成任务服务在创建 mkt: 节点任务时调用。
func GuardMarketingGeneration(ctx context.Context, tx repository.GenerationTaskTx, task *model.GenerationTask) error {
	if task.IsTest || !strings.HasPrefix(task.NodeID, model.MarketingNodePrefix) {
		return nil
	}
	adapter, ok := tx.(interface{ MarketingTx() repository.MarketingTx })
	if !ok {
		return errcode.ErrInternal
	}
	mtx := adapter.MarketingTx()
	sid, ok := idcodec.Decode(strings.TrimPrefix(task.NodeID, model.MarketingNodePrefix))
	if !ok {
		return errcode.ErrInvalidParams
	}
	probe, err := mtx.Script(ctx, task.UserID, sid, false)
	if mktNotFound(err) {
		return errcode.ErrMarketingNotFound
	}
	if err != nil {
		return err
	}
	// 锁顺序：项目、脚本；与删除项目、改脚本一致，防止并发穿透检查。
	if _, err := mtx.Project(ctx, task.UserID, probe.ProjectID, true); mktNotFound(err) {
		return errcode.ErrMarketingNotFound
	} else if err != nil {
		return err
	}
	sc, err := mtx.Script(ctx, task.UserID, sid, true)
	if err != nil {
		return err
	}
	if task.Kind != model.KindVideo || task.CanvasProjectID != nil {
		return errcode.ErrInvalidParams
	}
	if sc.Status == model.MarketingScriptRewriting {
		return errcode.ErrMarketingScriptBusy
	}
	if flags := flagList(sc); len(flags) > 0 {
		return errcode.ErrMarketingScriptBusy.WithMsg("旁白有问题（" + flagSummary(flags) + "），请先修改后再出片")
	}
	tasks, err := mtx.ScriptTasks(ctx, task.UserID, []string{task.NodeID})
	if err != nil {
		return err
	}
	for i := range tasks {
		if containsStatus(model.ActiveTaskStatuses, tasks[i].Status) {
			return errcode.ErrMarketingScriptBusy
		}
	}
	return nil
}

// containsStatus 判断状态是否在列表里。
func containsStatus(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
