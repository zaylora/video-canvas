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
func (s *DramaService) callDrama(ctx context.Context, j *model.DramaJob, snap *provider.Snapshot, step string, vars map[string]any, validate func(any) error) (any, int, error) {
	var invalid error
	for range 2 {
		if e := s.ensureDramaActive(ctx, j); e != nil {
			return nil, 0, e
		}
		if invalid != nil {
			vars["validation_error"] = invalid.Error()
		}
		prompt, e := drama.Prompt(step, vars)
		if e != nil {
			return nil, 0, e
		}
		chars := utf8.RuneCountInString(prompt)
		if c := snap.Model.Capabilities.Context; c != nil && c.Window > 0 && chars > c.Window {
			return nil, 0, errcode.ErrDramaStepNotReady.WithMsg("模型上下文不足以执行当前步骤")
		}
		result, e := s.streamDramaText(ctx, j, snap, prompt)
		if e != nil {
			return nil, 0, e
		}
		decoded, e := drama.DecodeOutput(step, []byte(result.Text))
		if e == nil && validate != nil {
			e = validate(decoded)
		}
		if e != nil {
			invalid = e
			continue
		}
		var usage *modelcfg.Usage
		if !result.UsageMissing {
			usage = &modelcfg.Usage{InputTokens: max(0, result.Usage.InputTokens), OutputTokens: max(0, result.Usage.OutputTokens)}
		}
		quoted := modelcfg.Quote(snap.Model.Pricing, snap.Model.Capabilities, modelcfg.Spec{PromptChars: chars, Params: map[string]any{}})
		return decoded, modelcfg.Settle(snap.Model.Pricing, quoted, usage), nil
	}
	return nil, 0, errcode.ErrDramaOutputInvalid
}

// streamDramaText 经LLM网关向Agent模型的渠道发一次对话请求；请求期间任务被取消会中断上游调用。
func (s *DramaService) streamDramaText(ctx context.Context, j *model.DramaJob, snap *provider.Snapshot, prompt string) (*llmgateway.Result, error) {
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
				if s.ensureDramaActive(callCtx, j) != nil {
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
	if stop := s.ensureDramaActive(ctx, j); stop != nil {
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
