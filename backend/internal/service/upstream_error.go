package service

import (
	"errors"

	"video-canvas/internal/llmgateway"
	"video-canvas/internal/provider"
)

// upstreamText 取文本模型调用错误里上游返回的错误原文，给用户看真实的失败原因；
// 原文由 llmgateway 遮掉渠道 Key 并截断过，这里再去掉上游自己的请求编号（对用户没用）。
// 不是上游返回的错误（网络断开、超时、宿主内部错误）或只剩编号时返回空串，调用方用自己的固定文案，不把内部错误透给前端。
func upstreamText(err error) string {
	var ue *llmgateway.UpstreamError
	if errors.As(err, &ue) {
		return provider.StripTraceIDs(ue.Message)
	}
	return ""
}
