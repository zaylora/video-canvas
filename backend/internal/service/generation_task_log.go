// 本文件是生成任务的运行日志：创建打 run.create，终态（成功 / 失败 / 超时 / 取消）打 run.finish（设计见 docs/design/统一服务器日志）。
package service

import (
	"context"

	"go.uber.org/zap"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/repository"
)

// genRunRef 返回生成任务的运行标识：run_id 是界面上的任务编号；由 Agent、营销脚本、短剧片段发起的任务指向发起者。
func genRunRef(t *model.GenerationTask) logger.RunRef {
	ref := logger.RunRef{Kind: "gen", ID: logger.GenRunID(t.ID), UserID: t.UserID}
	ref.ParentKind, ref.ParentID = t.RunParent()
	return ref
}

// logGenCreated 打 run.create：任务和队列 job 在同一个事务里提交之后。
func logGenCreated(ctx context.Context, t *model.GenerationTask) {
	ref := genRunRef(t)
	logger.NoteRun(ctx, ref.Kind, ref.ID)
	fields := []zap.Field{logger.Event(logger.EventRunCreate), logger.RunField(ref.Kind, ref.ID),
		zap.Uint64("user_id", t.UserID), zap.String("kind", t.Kind), zap.String("model", t.ModelKey), zap.String("channel", t.Provider),
		zap.Int("frozen_credits", t.Credits), zap.String("node_id", t.NodeID)}
	if ref.ParentKind != "" {
		fields = append(fields, zap.String("parent_kind", ref.ParentKind), zap.String("parent_id", ref.ParentID))
	}
	if t.IsTest {
		fields = append(fields, zap.Bool("test", true))
	}
	logger.L().Info("生成任务已创建", fields...)
}

// logGenFinished 打 run.finish：所有终态迁移都经过 finish，在这里统一收尾。ctx 是 worker 的（复用队列中间件挂的运行）
// 或用户取消的 HTTP 请求（新开一个运行）。生成任务的一生跨多次队列执行，所以另带 total_ms（创建到结束）。
func logGenFinished(ctx context.Context, t *model.GenerationTask, target string, extra map[string]any) {
	_, run := logger.EnsureRun(ctx, genRunRef(t))
	fields := []zap.Field{zap.String("kind", t.Kind), zap.String("model", t.ModelKey), zap.String("channel", t.Provider),
		zap.Int("frozen_credits", t.Credits), zap.Int("charged_credits", chargeOf(t, target))}
	if t.FinishedAt != nil {
		fields = append(fields, zap.Int64("total_ms", t.FinishedAt.Sub(t.CreatedAt).Milliseconds()))
	}
	// 失败类型：失败取 error_code（插件宿主的错误分类，取值不多）；超时、取消固定
	errType := ""
	switch target {
	case model.TaskFailed:
		errType = t.ErrorCode
	case model.TaskExpired:
		errType = "timeout"
	case model.TaskCanceled:
		errType = "canceled"
	}
	if errType != "" {
		fields = append(fields, zap.String("error_type", errType))
	}
	if t.ErrorCode != "" {
		fields = append(fields, zap.String("error_code", t.ErrorCode), zap.String("error_message", t.ErrorMessage))
	}
	if detail, ok := extra[repository.EventDetailKey].(map[string]any); ok {
		if up, ok := detail["upstream"].(string); ok && up != "" {
			fields = append(fields, zap.String("upstream", up))
		}
	}
	if t.IsTest {
		fields = append(fields, zap.Bool("test", true))
	}
	run.End(target, fields...)
}
