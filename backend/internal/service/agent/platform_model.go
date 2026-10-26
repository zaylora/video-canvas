package agent

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf8"

	"go.uber.org/zap"

	"video-canvas/internal/llmgateway"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/repository"
)

// RunUsageSource 按调用记录汇总一个运行的用量，由 *repository.AgentRepository 实现。
type RunUsageSource interface {
	// RunModelUsage 汇总该用户、该运行已结算的调用：实扣积分之和，以及最近一次对话调用的输入 Token 数。没有调用时全为 0。
	RunModelUsage(ctx context.Context, userID, runID uint64) (model.AgentRunModelUsage, error)
	// ModelCall 取该用户、该运行的一次调用，不存在或不属于他们返回 repository.ErrNotFound。
	ModelCall(ctx context.Context, userID, runID, callID uint64) (*model.AgentModelCall, error)
}

// ProxyModelPlatform 是上下文模式下的模型代理：和令牌模式的 ProxyModel 同样做模型快照、渠道 Key、预检、透传流式响应、按用量结算，
// 区别是运行记录在 Agent 服务里：
//   - 本轮已花的积分 = 调用方声明的 SpentExtra（已批准的生成）+ Go 按调用记录累加的模型调用实扣；
//   - 不发事件（message.delta、message.done、run.usage 都由 Agent 服务发），预算用尽也不改运行状态，只返回错误，由 Agent 服务决定怎么办；
//   - 额度检查只能放在这里：Agent 服务的「请求前钩子」抛异常拦不住请求，而所有模型请求本来就经 Go。
//
// onCall 在流开始之前收到这次调用的 id，调用方据此事后查这次调用的积分（响应头 X-Agent-Call-Id）。
// 结算用不会被取消的上下文：调用方断开（用户停止）时，已经产生的部分也要照样收费。
func (b *AgentBridge) ProxyModelPlatform(ctx context.Context, rc RunContext, body []byte, onCall func(callID uint64), write func([]byte)) error {
	if err := rc.validate(); err != nil {
		return err
	}
	if rc.ModelKey == "" {
		return errcode.ErrInvalidParams.WithMsg("运行上下文缺少 model_key")
	}
	if b.d.RunUsage == nil {
		return errors.New("平台模型代理没有配置 RunUsage")
	}
	snap, key, err := b.resolveModel(ctx, rc.ModelKey)
	if err != nil {
		return err
	}
	run, err := b.platformRunState(ctx, rc)
	if err != nil {
		return err
	}
	inputChars := utf8.RuneCount(body)
	if err := b.d.Billing.Preflight(ctx, run, snap.Model.Pricing, snap.Model.Capabilities, inputChars); err != nil {
		return err
	}
	call, err := b.d.Billing.Begin(ctx, run, rc.ModelKey)
	if err != nil {
		return err
	}
	if onCall != nil {
		onCall(call.ID)
	}
	tg := llmgateway.Target{ChannelKey: snap.Channel.Key, BaseURL: snap.Channel.BaseURL, APIKey: key, Headers: snap.Channel.Headers, UpstreamModel: snap.Model.UpstreamModel,
		RPS: snap.Channel.RateLimit.RPS, MaxConcurrency: snap.Channel.RateLimit.MaxConcurrency, IdleTimeout: snap.Model.IdleTimeout.D()}
	lim := llmgateway.RawLimits{}
	if c := snap.Model.Capabilities.Context; c != nil {
		lim.MaxTokens = c.Output
	}
	// 模型请求体的留存是 Go 的观测数据（Go 代理时产生的），上下文模式下照旧
	if b.reqRec != nil {
		if sent, err := llmgateway.SanitizeRaw(body, tg, lim); err == nil {
			go b.recordModelRequest(ctx, rc.RunID, call.ID, sent)
		}
	}

	began := b.d.Now()
	var firstByte = began
	first := true
	res, serr := b.d.Streamer.StreamRaw(ctx, tg, body, lim, func(llmgateway.Event) {}, func(line []byte) {
		if first {
			first, firstByte = false, b.d.Now()
		}
		write(line)
	})
	meta := CallMeta{Purpose: firstNonEmpty(ModelPurposeFrom(ctx), "main"), ChannelKey: snap.Channel.Key, UpstreamModel: snap.Model.UpstreamModel, Latency: b.d.Now().Sub(began)}
	if !first {
		meta.TTFT = firstByte.Sub(began)
	}
	if _, berr := b.d.Billing.SettleExternal(context.WithoutCancel(ctx), call, snap.Model.Pricing, CallOutcome{Result: res, InputChars: inputChars, Err: serr, Meta: meta}); berr != nil {
		// 结算失败意味着这次调用没收到钱，只能留下线索供对账补扣；不能因此让用户的对话失败
		logger.Error("Agent 对话结算失败", zap.Error(berr), zap.Uint64("call_id", call.ID), zap.Uint64("run_id", rc.RunID))
	}
	return serr
}

// platformRunState 构造带「本轮已花积分」的运行：调用方声明的 spent_extra 加上 Go 按调用记录累加的模型调用实扣。
// 预算检查（模型代理的预检、批准生成时的预算）都用它。
func (b *AgentBridge) platformRunState(ctx context.Context, rc RunContext) (*model.AgentRun, error) {
	if b.d.RunUsage == nil {
		return nil, errors.New("平台接口没有配置 RunUsage")
	}
	used, err := b.d.RunUsage.RunModelUsage(ctx, rc.UserID, rc.RunID)
	if err != nil {
		return nil, err
	}
	run := rc.run()
	run.SpentCredits = rc.SpentExtra + used.Credits
	return run, nil
}

// PlatformUsage 是一个运行的用量：Agent 服务据此发 run.usage（已花积分、上下文大小）和 message.done 里的积分。
type PlatformUsage struct {
	SpentCredits  int                `json:"spent_credits"`  // 模型调用已实扣的积分（不含调用方自己记的已批准的生成）
	ContextTokens int                `json:"context_tokens"` // 最近一次对话调用送进模型的输入 Token 数
	Call          *PlatformCallUsage `json:"call,omitempty"` // 查询时带了调用 id 才有
}

// PlatformCallUsage 是一次模型调用的结算情况。
type PlatformCallUsage struct {
	ID           uint64 `json:"id,string"`
	Status       string `json:"status"` // pending 还没结算，settled 已结算
	Credits      int    `json:"credits"`
	Charged      int    `json:"charged"`
	Estimated    bool   `json:"estimated"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	Error        string `json:"error,omitempty"`
}

// PlatformRunUsage 查一个运行的用量；callID 不为 0 时一并给出这次调用的结算情况（调用必须属于这个运行和用户，否则返回「资源不存在」）。
func (b *AgentBridge) PlatformRunUsage(ctx context.Context, rc RunContext, callID uint64) (*PlatformUsage, error) {
	if err := rc.validate(); err != nil {
		return nil, err
	}
	if b.d.RunUsage == nil {
		return nil, errors.New("平台用量查询没有配置 RunUsage")
	}
	used, err := b.d.RunUsage.RunModelUsage(ctx, rc.UserID, rc.RunID)
	if err != nil {
		return nil, err
	}
	out := &PlatformUsage{SpentCredits: used.Credits, ContextTokens: used.ContextTokens}
	if callID == 0 {
		return out, nil
	}
	c, err := b.d.RunUsage.ModelCall(ctx, rc.UserID, rc.RunID, callID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("读取模型调用失败: %w", err)
	}
	out.Call = &PlatformCallUsage{ID: c.ID, Status: c.Status, Credits: c.Credits, Charged: c.Charged, Estimated: c.UsageEstimated,
		InputTokens: c.InputTokens, OutputTokens: c.OutputTokens, Error: c.Error}
	return out, nil
}
