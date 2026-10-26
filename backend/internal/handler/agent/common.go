package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/llmgateway"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/response"
	agentsvc "video-canvas/internal/service/agent"
)

// maxBridgeBody 是模型代理的请求体上限：对话历史可能带图片（data URL），所以比普通接口大。
const maxBridgeBody = 32 << 20

// encodedParam 解析路径里的编码 id（运行和画布一样用十六进制串）；格式不对按参数错误返回。
func encodedParam(c *gin.Context, name string) (uint64, bool) {
	id, ok := idcodec.Decode(c.Param(name))
	if !ok {
		response.Fail(c, errcode.ErrInvalidParams.WithMsg(name+" 格式错误"))
		return 0, false
	}
	return id, true
}

// serveModelStream 是模型代理端点的外壳：读请求体、把上游的 SSE 原样转出；
// 流开始之前出错时返回 OpenAI 格式的 JSON 错误和对应的 HTTP 状态。call 做真正的代理；
// headers 里的头会在流开始（第一行写出）时加到响应上，比如这次调用的 id。
func serveModelStream(c *gin.Context, call func(ctx context.Context, body []byte, headers http.Header, write func([]byte)) error) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxBridgeBody+1))
	if err != nil || len(body) > maxBridgeBody {
		writeOpenAIError(c, http.StatusRequestEntityTooLarge, "请求体过大", "invalid_request_error")
		return
	}
	started := false
	extra := http.Header{}
	write := func(line []byte) {
		if !started {
			for k, v := range extra {
				c.Header(k, v[0])
			}
			c.Header("Content-Type", "text/event-stream")
			c.Header("Cache-Control", "no-cache")
			c.Header("X-Accel-Buffering", "no") // 反向代理不要缓冲，否则文字要等很久才出来
			c.Status(http.StatusOK)
			started = true
		}
		_, _ = c.Writer.Write(line) // 调用方已断开时写失败，下面的上下文取消会让代理结束
		c.Writer.Flush()
	}
	ctx := c.Request.Context()
	if purpose := agentsvc.ModelPurposeOf(c.GetHeader("X-Agent-Purpose")); purpose != "" { // 只认已知的用途：别的值一律当普通对话
		ctx = agentsvc.WithModelPurpose(ctx, purpose)
	}
	err = call(ctx, body, extra, write)
	if err != nil && !started {
		status, msg, typ := openAIError(err)
		// 业务错误把错误码放进响应头：调用方（Agent 服务）据此区分「本轮额度用完」等情况，不用比对文案
		var ec *errcode.Error
		if errors.As(err, &ec) {
			c.Header(ErrorCodeHeader, strconv.Itoa(ec.Code))
		}
		writeOpenAIError(c, status, msg, typ)
	}
	// 流已经开始：状态码改不了，只能结束流；调用方会把提前结束当作错误
}

// openAIError 把桥的错误转成 HTTP 状态和给调用方看的说明。内部错误只说「上游调用失败」，细节不外泄。
func openAIError(err error) (status int, msg, typ string) {
	var ue *llmgateway.UpstreamError
	var ec *errcode.Error
	switch {
	case errors.As(err, &ec):
		return ec.HTTPStatus(), ec.Msg, "agent_error"
	case errors.As(err, &ue):
		if ue.Status < 400 {
			return http.StatusBadGateway, ue.Message, "upstream_error"
		}
		return ue.Status, ue.Message, "upstream_error"
	case errors.Is(err, llmgateway.ErrIdleTimeout):
		return http.StatusGatewayTimeout, "模型响应超时，请稍后重试", "upstream_timeout"
	case errors.Is(err, llmgateway.ErrInvalidRequest):
		return http.StatusBadRequest, "请求不合法", "invalid_request_error"
	case errors.Is(err, context.Canceled):
		return 499, "调用方已取消", "canceled"
	}
	return http.StatusBadGateway, "上游调用失败，请稍后重试", "upstream_error"
}

// writeOpenAIError 写 OpenAI 格式的错误响应 {"error":{"message","type"}}。
func writeOpenAIError(c *gin.Context, status int, msg, typ string) {
	b, _ := json.Marshal(map[string]any{"error": map[string]any{"message": msg, "type": typ}}) // 只含字符串，序列化不会失败
	c.Data(status, "application/json", b)
}
