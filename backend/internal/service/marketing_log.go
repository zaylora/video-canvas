// 本文件是营销写脚本的运行日志：环节运行（mkt）和轮次（mkt_round）的收尾行与失败分类。
package service

import (
	"context"
	"errors"

	"video-canvas/internal/llmgateway"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/stepkit"

	"go.uber.org/zap"
)

// endStepRun 打环节运行的收尾行：状态、模型、给用户的原因、原始错误和分类。
func endStepRun(run *logger.Run, stepKey string, out *stepOutcome) {
	fields := []zap.Field{zap.String("step", stepKey), zap.String("model", out.model), zap.Bool("charged", out.charge)}
	if out.res != nil {
		fields = append(fields, zap.Int("attempts", out.res.Attempts))
	}
	if out.errMsg != "" {
		fields = append(fields, zap.String("error_message", out.errMsg))
	}
	status := out.status
	switch status {
	case model.MarketingRunReused:
		status = logger.StatusSucceeded
	case model.MarketingRunFailed:
		fields = append(fields, zap.String("error_type", stepErrorType(out.err)))
	}
	if out.err != nil {
		fields = append(fields, zap.String("error", out.err.Error()))
	}
	run.End(status, fields...)
}

// stepErrorType 把环节运行的失败归成 error_type。
func stepErrorType(err error) string {
	var ue *llmgateway.UpstreamError
	switch {
	case err == nil:
		return "business"
	case errors.As(err, &ue):
		return "upstream"
	case errors.Is(err, stepkit.ErrOutputInvalid):
		return "invalid_output"
	case errors.Is(err, stepkit.ErrModelCapability):
		return "no_model"
	case errors.Is(err, llmgateway.ErrIdleTimeout), errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, stepkit.ErrCallFailed):
		return "network"
	}
	return logger.ErrorTypeInternal
}

// roundRunStatus 把轮次终态映射成运行终态：至少有一份脚本可用算成功。
func roundRunStatus(status string) string {
	switch status {
	case model.MarketingRoundReady:
		return logger.StatusSucceeded
	case model.MarketingRoundCanceled:
		return logger.StatusCanceled
	}
	return logger.StatusFailed
}
