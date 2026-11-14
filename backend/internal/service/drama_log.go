// 本文件是短剧运行的日志：运行标识、公共字段、失败分类，以及保留底层原因的错误包装（设计见 docs/design/短剧运行日志）。
package service

import (
	"context"
	"errors"
	"fmt"

	"go.uber.org/zap"

	"video-canvas/internal/llmgateway"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/logger"
)

// dramaRef 返回短剧任务的运行标识：run_id 和任务条上显示的编号相同。
func dramaRef(j *model.DramaJob) logger.RunRef {
	return logger.RunRef{Kind: "drama", ID: idcodec.Encode(j.ID), UserID: j.UserID}
}

// dramaJobRun 挂上（或复用队列中间件挂好的）短剧运行。
func dramaJobRun(ctx context.Context, j *model.DramaJob) (context.Context, *logger.Run) {
	return logger.EnsureRun(ctx, dramaRef(j))
}

// dramaFields 是短剧运行的业务字段：步骤、来源、集数、自动规划的批次。
func dramaFields(j *model.DramaJob) []zap.Field {
	fields := []zap.Field{zap.String("step", j.Step), zap.String("lane", j.Lane), zap.Ints("episodes", j.Episodes)}
	if j.BatchTo > 0 {
		fields = append(fields, zap.String("batch", fmt.Sprintf("%d-%d", j.BatchFrom, j.BatchTo)))
	}
	return fields
}

// logDramaCreated 打 run.create：任务入库并入队之后。
func logDramaCreated(ctx context.Context, j *model.DramaJob, parent *model.DramaJob) {
	if j == nil {
		return
	}
	ref := dramaRef(j)
	logger.NoteRun(ctx, ref.Kind, ref.ID)
	fields := append([]zap.Field{logger.Event(logger.EventRunCreate), logger.RunField(ref.Kind, ref.ID),
		zap.Uint64("user_id", j.UserID), zap.Int("frozen_credits", j.FrozenCredits)}, dramaFields(j)...)
	if parent != nil {
		fields = append(fields, zap.String("parent_job_id", idcodec.Encode(parent.ID)))
	}
	logger.L().Info("短剧任务已创建", fields...)
}

// dramaCallFailure 标记「模型调用本身失败」（上游、网络、超时）：保留原始错误，日志按 upstream / timeout / network 分类。
type dramaCallFailure struct{ err error }

func (e *dramaCallFailure) Error() string { return "模型调用失败：" + e.err.Error() }
func (e *dramaCallFailure) Unwrap() error { return e.err }

// dramaCause 让失败对用户显示固定文案，同时把底层原因留给日志（如合成的下载、ffmpeg、存储错误）。
type dramaCause struct {
	user  *errcode.Error
	cause error
	typ   string // 日志里的 error_type
}

func (e *dramaCause) Error() string   { return e.user.Msg + "：" + e.cause.Error() }
func (e *dramaCause) Unwrap() []error { return []error{e.user, e.cause} }

// withCause 包装一个对用户显示 user 文案、日志记 cause 的错误；cause 为空时直接返回 user。
func withCause(user *errcode.Error, cause error, typ string) error {
	if cause == nil {
		return user
	}
	return &dramaCause{user: user, cause: cause, typ: typ}
}

// dramaErrorType 把失败原因归成低基数的 error_type，供运行汇总行和巡检计数。
func dramaErrorType(err error) string {
	var dc *dramaCause
	if errors.As(err, &dc) && dc.typ != "" {
		return dc.typ
	}
	var ue *llmgateway.UpstreamError
	switch {
	case errors.As(err, &ue):
		return "upstream"
	case errors.Is(err, llmgateway.ErrIdleTimeout), errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	}
	var e *errcode.Error
	if errors.As(err, &e) {
		switch e.Code {
		case errcode.ErrDramaOutputInvalid.Code:
			return "invalid_output"
		case errcode.ErrDramaCustomPrompt.Code:
			return "custom_prompt"
		case errcode.ErrDramaContext.Code, errcode.ErrDramaEpisodeSourceTooLong.Code:
			return "context_too_long"
		case errcode.ErrDramaNoTextModel.Code:
			return "no_model"
		case errcode.ErrTaskNotCancelable.Code:
			return "canceled"
		case errcode.ErrInternal.Code:
			return logger.ErrorTypeInternal
		}
		return "business"
	}
	var cf *dramaCallFailure
	if errors.As(err, &cf) {
		return "network"
	}
	return logger.ErrorTypeInternal
}

// episodeRange 把集号列表写成日志里的批次范围：连续时 1-5，否则逗号分隔。
func episodeRange(nos []int) string {
	if len(nos) == 0 {
		return ""
	}
	contiguous := true
	for i := 1; i < len(nos); i++ {
		contiguous = contiguous && nos[i] == nos[i-1]+1
	}
	if contiguous {
		return fmt.Sprintf("%d-%d", nos[0], nos[len(nos)-1])
	}
	out := ""
	for i, n := range nos {
		if i > 0 {
			out += ","
		}
		out += fmt.Sprint(n)
	}
	return out
}
