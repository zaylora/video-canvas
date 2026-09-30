package worker

import (
	"context"
	"encoding/json"
	"time"

	"go.uber.org/zap"

	"video-canvas/internal/model"
	"video-canvas/internal/provider"
)

const (
	// maxTraceSteps 是一个试跑任务最多保留的追踪步骤数：长时间轮询的任务每次查询都会追加步骤，
	// 超出后丢弃最早的，保留最近的现场（管理端排查时最关心最后发生了什么）
	maxTraceSteps = 200
	// traceSaveTimeout 是保存追踪的超时。用不随 worker 取消的 ctx：退出前最后一次处理的追踪也要尽量留下
	traceSaveTimeout = 5 * time.Second
)

// saveTrace 把本次处理记录下的追踪步骤追加到试跑任务已有的追踪后面并落库。
// 追踪只是给运营看的辅助信息：任何失败（含 panic）都只记警告，不影响任务本身。
func (w *Worker) saveTrace(ctx context.Context, t *model.GenerationTask, tr *provider.Trace) {
	log := w.taskLog(t)
	defer func() {
		if r := recover(); r != nil {
			log.Warn("保存试跑追踪时 panic，已忽略", zap.Any("panic", r))
		}
	}()
	steps := tr.Steps()
	if len(steps) == 0 {
		return
	}
	all, dropped := mergeTrace(t.TraceJSON, steps)
	if dropped > 0 {
		log.Debug("试跑追踪超过上限，已丢弃最早的步骤", zap.Int("limit", maxTraceSteps), zap.Int("dropped", dropped))
	}
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), traceSaveTimeout)
	defer cancel()
	if err := w.store.SaveTrace(sctx, t, all); err != nil {
		log.Warn("保存试跑追踪失败，本次步骤丢失", zap.Int("steps", len(steps)), zap.Error(err))
	}
}

// mergeTrace 解码已有的追踪（损坏当空：追踪只是辅助信息，不值得为它让任务出错），追加本次步骤，
// 总数超过 maxTraceSteps 时丢弃最早的，dropped 是丢弃的步数。
func mergeTrace(existing []byte, steps []provider.TraceStep) (all []provider.TraceStep, dropped int) {
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &all); err != nil {
			all = nil
		}
	}
	all = append(all, steps...)
	if over := len(all) - maxTraceSteps; over > 0 {
		all = append([]provider.TraceStep(nil), all[over:]...)
		dropped = over
	}
	return all, dropped
}
