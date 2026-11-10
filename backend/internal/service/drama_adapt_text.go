package service

import (
	"context"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"

	"video-canvas/internal/drama"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
)

// adaptTimeout 转写一个片段（含重写一次）的最长等待；须小于服务端 write_timeout（默认 60s），否则响应会被连接超时截断。
// 超时按失败处理，用户可以重试。
const adaptTimeout = 50 * time.Second

// AdaptSegment 用项目文本模型把片段原稿转写成 format 写法，输出不合格时带校验反馈重写一次。
// 转写只发一段原稿，用量很小，和预检里 AI 判断体裁一样由平台承担，不向用户扣积分，也不建短剧任务。
func (s *DramaService) AdaptSegment(ctx context.Context, p *model.DramaProject, format string, in drama.AdaptInput) (string, error) {
	// 1. 跟随项目文本模型；选的模型停用时回落到第一个可用的 Agent 模型（与流水线一致）
	snap, err := s.resolveModel(ctx, p.TextModel)
	if err != nil {
		return "", err
	}
	in.Style = s.dramaVariables(p)["style"].(string)
	callCtx, cancel := context.WithTimeout(ctx, adaptTimeout)
	defer cancel()
	// 2. 最多两次：第一次不合格时把校验原因交给模型修正，仍不合格才报错
	for range 2 {
		prompt, err := drama.AdaptPrompt(format, in)
		if err != nil {
			return "", errcode.ErrDramaAdaptFormat
		}
		if utf8.RuneCountInString(prompt) > dramaContextLimit(snap) {
			return "", errcode.ErrDramaContext
		}
		res, err := s.streamText(callCtx, snap, prompt, func(c context.Context) error { return c.Err() })
		if err != nil {
			// 上游失败和超时不把内部错误透给前端，记一条日志后按转写失败返回
			logger.Warn("短剧片段转写调用失败", zap.String("format", format), zap.Error(err))
			return "", errcode.ErrDramaAdaptFailed.WithMsg("转写失败：文本模型没有返回结果，请稍后重试")
		}
		decoded, err := drama.DecodeOutput("adapt", []byte(res.Text))
		if err == nil {
			out := decoded.(drama.AdaptOutput).Prompt
			if err = drama.ValidateAdapt(format, in.Source, out, in.Seconds); err == nil {
				return out, nil
			}
		}
		in.ValidationError = err.Error()
	}
	return "", errcode.ErrDramaAdaptFailed
}
