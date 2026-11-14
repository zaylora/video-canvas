package service

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"slices"
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
)

// callDrama JSON校验失败只重试一次，失败请求不计入扣费。
// call 是这次调用在运行里的位置（如 episode=3、batch=1-5），单次步骤传空串；写进日志的调用行。
func (s *DramaService) callDrama(ctx context.Context, j *model.DramaJob, snap *provider.Snapshot, step, call string, vars map[string]any, validate func(any) error) (any, int, error) {
	var invalid error
	for try := 1; try <= 2; try++ {
		if e := s.ensureDramaActive(ctx, j); e != nil {
			return nil, 0, e
		}
		if invalid != nil {
			vars["validation_error"] = invalid.Error()
		}
		decoded, cost, bad, e := s.dramaAttempt(ctx, j, snap, step, call, try, vars, validate)
		if e != nil {
			return nil, 0, e
		}
		if bad == nil {
			return decoded, cost, nil
		}
		invalid = bad
	}
	if cfg := jobConfig(j); cfg.Prompt.Source != "" && cfg.Prompt.Source != "builtin" {
		return nil, 0, errcode.ErrDramaCustomPrompt.WithMsg("自定义提示词或技能导致输出不合格（" + dramaStepGroup(step) + "）")
	}
	return nil, 0, errcode.ErrDramaOutputInvalid
}

// dramaAttempt 是 callDrama 的一次尝试：拼提示词、调模型、解码并校验。输出不合格时返回 invalid（调用方带着原因重写），
// 其他失败返回 err。每次尝试声明一次调用：网关的 llm.call 和 payload.log 的全文用同一个 call_id，不合格时 run.step 指向它。
func (s *DramaService) dramaAttempt(ctx context.Context, j *model.DramaJob, snap *provider.Snapshot, step, call string, try int, vars map[string]any, validate func(any) error) (decoded any, cost int, invalid, err error) {
	cfg := jobConfig(j)
	if step == "breakdown" {
		vars["assets"] = cfg.KnownCast
		delete(vars, "known_cast")
	} else {
		vars["known_cast"] = cfg.KnownCast
	}
	template, body := dramaTemplate(step, cfg.Prompt)
	prompt, err := drama.PromptWithBody(template, vars, body)
	if err != nil {
		return nil, 0, nil, err
	}
	chars := utf8.RuneCountInString(prompt)
	if chars > dramaContextLimit(snap) {
		return nil, 0, nil, errcode.ErrDramaContext
	}
	callFields := []zap.Field{zap.Int("try", try), zap.String("model_key", snap.Model.Key)}
	if call != "" {
		callFields = append(callFields, zap.String("call", call))
	}
	callCtx, callID := logger.WithCall(ctx, "drama."+step, callFields...)
	logger.RunOf(ctx).NoteCall(callID)
	result, err := s.streamDramaText(callCtx, j, snap, prompt)
	if err != nil {
		var business *errcode.Error
		if !errors.As(err, &business) {
			err = &dramaCallFailure{err: err} // 上游、网络、超时：保留原始错误给日志，给用户的文案仍由 dramaFailure 决定
		}
		return nil, 0, nil, err
	}
	decoded, invalid = drama.DecodeOutput(step, []byte(result.Text))
	if invalid == nil && validate != nil {
		invalid = validate(decoded)
	}
	if invalid != nil {
		logger.Ctx(ctx).Warn("短剧模型输出不合格", append([]zap.Field{logger.Event(logger.EventRunStep), zap.String("call_id", callID),
			zap.String("error_type", "invalid_output"), zap.String("validation_error", invalid.Error()), zap.String("finish_reason", result.FinishReason)}, callFields...)...)
		return nil, 0, invalid, nil
	}
	var usage *modelcfg.Usage
	if !result.UsageMissing {
		usage = &modelcfg.Usage{InputTokens: max(0, result.Usage.InputTokens), OutputTokens: max(0, result.Usage.OutputTokens)}
	}
	quoted := modelcfg.Quote(snap.Model.Pricing, snap.Model.Capabilities, modelcfg.Spec{PromptChars: chars, Params: map[string]any{}})
	return decoded, modelcfg.Settle(snap.Model.Pricing, quoted, usage), nil, nil
}

// dramaTemplate 选择本次调用的模板与正文：用户自写或技能提示词是「替换正文、Schema 固定」，始终用用户正文；
// 内置来源时始终用当前内置模板，已有项目也随发版拿到新提示词。
func dramaTemplate(step string, setting model.DramaPromptSetting) (template, body string) {
	if setting.Source == "" || setting.Source == "builtin" {
		// 内置来源不读项目快照里的正文副本：那是建项目时复制的，发版更新内置提示词后会过期。
		// 空正文让 PromptWithBody 直接用当前内置模板。
		return step, ""
	}
	return step, setting.Bodies[step]
}

// streamDramaText 经LLM网关向Agent模型的渠道发一次对话请求；请求期间任务被取消会中断上游调用。
func (s *DramaService) streamDramaText(ctx context.Context, j *model.DramaJob, snap *provider.Snapshot, prompt string) (*llmgateway.Result, error) {
	return s.streamText(ctx, snap, prompt, func(c context.Context) error { return s.ensureDramaActive(c, j) })
}

// streamText 是 streamDramaText 的核心：active 每秒调用一次，返回错误就中断上游请求并把该错误交给调用方。
// 预检这类还没有任务的调用传一个只看 ctx 的 active。
func (s *DramaService) streamText(ctx context.Context, snap *provider.Snapshot, prompt string, active func(context.Context) error) (*llmgateway.Result, error) {
	if s.streamer == nil || s.secrets == nil {
		return nil, errcode.ErrInternal
	}
	key, e := s.secrets.Get(ctx, model.ChannelSecretName(snap.Channel.Key))
	if e != nil {
		logger.Warn("短剧模型渠道Key读取失败", zap.String("channel", snap.Channel.Key), zap.Error(e))
		return nil, errcode.ErrDramaNoTextModel.WithMsg("模型渠道暂不可用")
	}
	tg := llmgateway.Target{ChannelKey: snap.Channel.Key, BaseURL: snap.Channel.BaseURL, APIKey: key, Headers: snap.Channel.Headers, UpstreamModel: snap.Model.UpstreamModel,
		Format: snap.Model.APIFormat, RPS: snap.Channel.RateLimit.RPS, MaxConcurrency: snap.Channel.RateLimit.MaxConcurrency, IdleTimeout: snap.Model.IdleTimeout.D()}
	req := llmgateway.Request{Messages: []llmgateway.Message{{Role: "user", Text: prompt}}}
	if c := snap.Model.Capabilities.Context; c != nil {
		req.MaxTokens = c.Output
	}

	// 1. 每秒确认任务仍有效，被取消就中断上游请求，不再等它出完。
	callCtx, cancel := context.WithCancel(ctx)
	watched := make(chan struct{})
	go func() {
		defer close(watched)
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-callCtx.Done():
				return
			case <-tick.C:
				if active(callCtx) != nil {
					cancel()
					return
				}
			}
		}
	}()
	res, e := s.streamer.Stream(callCtx, tg, req, func(llmgateway.Event) {})
	cancel()
	<-watched

	// 2. 取消造成的上游中断按取消处理，其余按上游失败。
	if stop := active(ctx); stop != nil {
		return nil, stop
	}
	if e != nil {
		return nil, e
	}
	if res == nil {
		return nil, errcode.ErrInternal
	}
	return res, nil
}

// ensureDramaActive 取消后停止后续网络调用与写入。
func (s *DramaService) ensureDramaActive(ctx context.Context, j *model.DramaJob) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	current, e := s.repo.Job(ctx, j.UserID, j.DramaID, j.ID, false)
	if e != nil {
		return e
	}
	if !dramaActive(current.Status) {
		return errcode.ErrTaskNotCancelable
	}
	return nil
}

// dramaRetryAssets 从最近同范围任务恢复本次新建节点，不扩展到旧资产。
func dramaRetryAssets(jobs []model.DramaJob, scope []int) ([]DramaCanvasAsset, error) {
	slices.SortFunc(jobs, func(a, b model.DramaJob) int { return cmp.Compare(b.ID, a.ID) })
	for _, j := range jobs {
		if !dramaSameScope(j.Episodes, scope) {
			continue
		}
		var assets []DramaCanvasAsset
		if j.Step == "asset_prompts" {
			if jsonErr := decodeDramaAssets(j.InputJSON, &assets); jsonErr == nil {
				return assets, nil
			}
		}
		if j.Step == "breakdown" {
			var checkpoint dramaCheckpoint
			if json.Unmarshal(j.Output, &checkpoint) == nil && checkpoint.CanvasCommitted && len(checkpoint.NewAssets) > 0 {
				return checkpoint.NewAssets, nil
			}
		}
	}
	return nil, errcode.ErrDramaStepNotReady.WithMsg("没有待生成提示词的新资产")
}

// dramaSameScope 比较范围集合，顺序差异不影响重试。
func dramaSameScope(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[int]bool{}
	for _, n := range a {
		seen[n] = true
	}
	for _, n := range b {
		if !seen[n] {
			return false
		}
	}
	return true
}

// decodeDramaAssets 拒绝空批次，防止重试请求变成无作用的扣费。
func decodeDramaAssets(raw []byte, out *[]DramaCanvasAsset) error {
	if e := json.Unmarshal(raw, out); e != nil {
		return e
	}
	if len(*out) == 0 {
		return errors.New("新资产列表为空")
	}
	return nil
}
