package service

import (
	"context"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"

	"video-canvas/internal/drama"
	"video-canvas/internal/llmgateway"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/repository"
)

// adaptTimeout 转写一个片段（含重写一次）的最长等待；须小于服务端 write_timeout（默认 60s），否则响应会被连接超时截断。
// 超时按失败处理，用户可以重试。
const adaptTimeout = 50 * time.Second

// AdaptSegment 用项目文本模型把片段原稿转写成 format 写法，输出不合格时带校验反馈重写一次；
// 返回转写稿和应收积分。计费口径与其他短剧文本步骤相同：按模型定价和实际用量，只算通过校验的那一次。
// 这里只算钱不动账户：扣费和写回转写稿在片段服务的同一个事务里完成，转写失败就不扣。
func (s *DramaService) AdaptSegment(ctx context.Context, p *model.DramaProject, format string, in drama.AdaptInput) (string, int, error) {
	// 1. 跟随项目文本模型；选的模型停用时回落到第一个可用的 Agent 模型（与流水线一致）
	snap, err := s.resolveModel(ctx, p.TextModel)
	if err != nil {
		return "", 0, err
	}
	in.Style = s.dramaVariables(p)["style"].(string)
	// 2. 调模型前按预估费用检查可用积分：不够就不调，免得做完了扣不到钱
	if err = s.checkAdaptCredits(ctx, p.UserID, snap, format, in); err != nil {
		return "", 0, err
	}
	callCtx, cancel := context.WithTimeout(ctx, adaptTimeout)
	defer cancel()
	// 3. 最多两次：第一次不合格时把校验原因交给模型修正，仍不合格才报错；不合格的那次不收费
	for range 2 {
		prompt, err := drama.AdaptPrompt(format, in)
		if err != nil {
			return "", 0, errcode.ErrDramaAdaptFormat
		}
		chars := utf8.RuneCountInString(prompt)
		if chars > dramaContextLimit(snap) {
			return "", 0, errcode.ErrDramaContext
		}
		res, err := s.streamText(callCtx, snap, prompt, func(c context.Context) error { return c.Err() })
		if err != nil {
			// 上游失败和超时不把内部错误透给前端，记一条日志后按转写失败返回
			logger.Warn("短剧片段转写调用失败", zap.String("format", format), zap.Error(err))
			return "", 0, errcode.ErrDramaAdaptFailed.WithMsg("转写失败：文本模型没有返回结果，请稍后重试")
		}
		decoded, err := drama.DecodeOutput("adapt", []byte(res.Text))
		if err == nil {
			out := decoded.(drama.AdaptOutput).Prompt
			if err = drama.ValidateAdapt(format, in.Source, out, in.Seconds); err == nil {
				return out, adaptCost(snap, chars, res), nil
			}
		}
		in.ValidationError = err.Error()
	}
	return "", 0, errcode.ErrDramaAdaptFailed
}

// checkAdaptCredits 按提示词字数预估一次调用的费用，可用积分（余额减冻结）不够返回积分不足；
// 没有账户时按新账户初始积分惰性建立，和提交短剧任务一致。
func (s *DramaService) checkAdaptCredits(ctx context.Context, userID uint64, snap *provider.Snapshot, format string, in drama.AdaptInput) error {
	prompt, err := drama.AdaptPrompt(format, in)
	if err != nil {
		return errcode.ErrDramaAdaptFormat
	}
	estimate := modelcfg.Quote(snap.Model.Pricing, snap.Model.Capabilities, modelcfg.Spec{PromptChars: utf8.RuneCountInString(prompt), Params: map[string]any{}})
	if estimate <= 0 {
		return nil
	}
	initial, err := s.dramaInitialCredits(ctx)
	if err != nil {
		return err
	}
	// 只读一眼余额，不冻结：真正扣费在写回时加锁做，并封顶在当时的可用积分
	return s.repo.WithTx(ctx, func(tx repository.DramaTx) error {
		if err := tx.EnsureCredit(ctx, userID, initial); err != nil {
			return err
		}
		credit, err := tx.LockCredit(ctx, userID)
		if err != nil {
			return err
		}
		if credit.Balance-credit.Frozen < estimate {
			return errcode.ErrInsufficientCredits
		}
		return nil
	})
}

// adaptCost 按实际用量结算一次调用；上游没给用量时按预估收。
func adaptCost(snap *provider.Snapshot, chars int, res *llmgateway.Result) int {
	var usage *modelcfg.Usage
	if !res.UsageMissing {
		usage = &modelcfg.Usage{InputTokens: max(0, res.Usage.InputTokens), OutputTokens: max(0, res.Usage.OutputTokens)}
	}
	quoted := modelcfg.Quote(snap.Model.Pricing, snap.Model.Capabilities, modelcfg.Spec{PromptChars: chars, Params: map[string]any{}})
	return modelcfg.Settle(snap.Model.Pricing, quoted, usage)
}
