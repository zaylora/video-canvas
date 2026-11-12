package stepkit

import (
	"context"
	"errors"
	"fmt"

	"go.uber.org/zap"

	"video-canvas/internal/llmgateway"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/provider"
)

// Streamer 向渠道发一次流式对话请求（*llmgateway.Gateway 实现它）。
type Streamer interface {
	// Stream 发起请求并返回完整结果；emit 收增量事件，环节运行不需要，传空函数。
	Stream(ctx context.Context, tg llmgateway.Target, req llmgateway.Request, emit func(llmgateway.Event)) (*llmgateway.Result, error)
}

// GatewayCaller 是生产用的 Caller：取渠道 Key，经 LLM 网关请求渠道。
type GatewayCaller struct {
	Streamer Streamer
	Secrets  provider.SecretResolver
}

// Complete 实现 Caller。Key 读取失败只记日志，不把内部原因透给调用方。
func (g *GatewayCaller) Complete(ctx context.Context, snap *provider.Snapshot, msgs []llmgateway.Message) (*llmgateway.Result, error) {
	if g.Streamer == nil || g.Secrets == nil {
		return nil, errors.New("文本模型网关未配置")
	}
	key, err := g.Secrets.Get(ctx, model.ChannelSecretName(snap.Channel.Key))
	if err != nil {
		logger.Warn("环节模型渠道 Key 读取失败", zap.String("channel", snap.Channel.Key), zap.Error(err))
		return nil, errors.New("模型渠道暂不可用")
	}
	target := llmgateway.Target{
		ChannelKey: snap.Channel.Key, BaseURL: snap.Channel.BaseURL, APIKey: key, Headers: snap.Channel.Headers,
		UpstreamModel: snap.Model.UpstreamModel, Format: snap.Model.APIFormat,
		RPS: snap.Channel.RateLimit.RPS, MaxConcurrency: snap.Channel.RateLimit.MaxConcurrency,
		IdleTimeout: snap.Model.IdleTimeout.D(),
	}
	req := llmgateway.Request{Messages: msgs}
	if c := snap.Model.Capabilities.Context; c != nil {
		req.MaxTokens = c.Output
	}
	res, err := g.Streamer.Stream(ctx, target, req, func(llmgateway.Event) {})
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, fmt.Errorf("模型没有返回结果")
	}
	return res, nil
}
