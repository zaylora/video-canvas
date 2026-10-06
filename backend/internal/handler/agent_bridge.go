package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/llmgateway"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/response"
	"video-canvas/internal/service"
)

// maxBridgeBody 是桥的请求体上限：对话历史可能带图片（data URL），所以比普通接口大。
const maxBridgeBody = 32 << 20

// AgentBridgeAPI 是桥 handler 依赖的业务，由 *service.AgentBridge 实现。
type AgentBridgeAPI interface {
	// ProxyModel 代理一次模型调用，write 收到上游的每一行 SSE；流开始之前出错时 write 一次都没调用。
	ProxyModel(ctx context.Context, token string, body []byte, write func([]byte)) error
	// ExecuteTool 执行 Node 回调的一次工具调用。
	ExecuteTool(ctx context.Context, token, toolCallID, name string, args json.RawMessage) (*service.ToolResult, error)
	// SaveState 保存 Node 回合结束时交回的对话历史。
	SaveState(ctx context.Context, token string, messages json.RawMessage) error
	// Finish 处理 Node 进程报告的片段结束。
	Finish(ctx context.Context, token, status, message string) error
}

// AgentBridgeHandler 是 Node 里的 pi 调用 Go 的接口。它只挂在一个绑定 127.0.0.1 的独立监听上，不在对外的主路由里。
type AgentBridgeHandler struct {
	svc AgentBridgeAPI
}

// NewAgentBridgeHandler 创建桥 handler。
func NewAgentBridgeHandler(svc AgentBridgeAPI) *AgentBridgeHandler {
	return &AgentBridgeHandler{svc: svc}
}

// bearerToken 取 Authorization: Bearer 里的令牌，没有或格式不对返回 false。
func bearerToken(c *gin.Context) (string, bool) {
	tok, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
	tok = strings.TrimSpace(tok)
	return tok, ok && tok != ""
}

// bridgeFail 返回标准业务错误：桥令牌无效按 401。
func bridgeFail(c *gin.Context, err error) {
	if errors.Is(err, service.ErrBridgeToken) {
		response.Fail(c, errcode.ErrUnauthorized.WithMsg("桥令牌无效或已过期"))
		return
	}
	response.Fail(c, err)
}

// ChatCompletions 是 OpenAI 兼容的对话端点：pi 把它当作模型提供方来调用。
// 响应是上游的 SSE 原样转出；流开始之前出错时返回 OpenAI 格式的 JSON 错误和对应的 HTTP 状态，pi 靠它显示报错、判断是否可重试。
func (h *AgentBridgeHandler) ChatCompletions(c *gin.Context) {
	tok, ok := bearerToken(c)
	if !ok {
		writeOpenAIError(c, http.StatusUnauthorized, "缺少桥令牌", "authentication_error")
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxBridgeBody+1))
	if err != nil || len(body) > maxBridgeBody {
		writeOpenAIError(c, http.StatusRequestEntityTooLarge, "请求体过大", "invalid_request_error")
		return
	}
	started := false
	write := func(line []byte) {
		if !started {
			c.Header("Content-Type", "text/event-stream")
			c.Header("Cache-Control", "no-cache")
			c.Header("X-Accel-Buffering", "no") // 反向代理不要缓冲，否则文字要等很久才出来
			c.Status(http.StatusOK)
			started = true
		}
		_, _ = c.Writer.Write(line) // 调用方已断开时写失败，下面的上下文取消会让代理结束
		c.Writer.Flush()
	}
	err = h.svc.ProxyModel(c.Request.Context(), tok, body, write)
	if err != nil && !started {
		status, msg, typ := openAIError(err)
		writeOpenAIError(c, status, msg, typ)
	}
	// 流已经开始：状态码改不了，只能结束流；调用方会把提前结束当作错误
}

// openAIError 把桥的错误转成 HTTP 状态和给调用方看的说明。内部错误只说「上游调用失败」，细节不外泄。
func openAIError(err error) (status int, msg, typ string) {
	var ue *llmgateway.UpstreamError
	var ec *errcode.Error
	switch {
	case errors.Is(err, service.ErrBridgeToken):
		return http.StatusUnauthorized, "桥令牌无效或已过期", "authentication_error"
	case errors.As(err, &ec):
		return ec.HTTPStatus(), ec.Msg, "agent_error"
	case errors.As(err, &ue):
		if ue.Status < 400 {
			return http.StatusBadGateway, ue.Message, "upstream_error"
		}
		return ue.Status, ue.Message, "upstream_error"
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

// toolReq 是工具回调的请求。
type toolReq struct {
	ToolCallID string          `json:"tool_call_id" binding:"required,max=128" label:"tool_call_id"` // pi 给这次工具调用的 id
	Name       string          `json:"name" binding:"required,max=64" label:"name"`                  // 工具名
	Args       json.RawMessage `json:"args"`                                                         // 工具参数，不传按空对象
}

// Tool 执行一次工具调用：Node 里每个工具的 execute 都只是回调这里。
func (h *AgentBridgeHandler) Tool(c *gin.Context) {
	tok, ok := bearerToken(c)
	if !ok {
		response.Fail(c, errcode.ErrUnauthorized.WithMsg("缺少桥令牌"))
		return
	}
	var req toolReq
	if !bindJSON(c, &req) {
		return
	}
	args := req.Args
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	res, err := h.svc.ExecuteTool(c.Request.Context(), tok, req.ToolCallID, req.Name, args)
	if err != nil {
		bridgeFail(c, err)
		return
	}
	response.OK(c, res)
}

// stateReq 是保存对话历史的请求。
type stateReq struct {
	Messages json.RawMessage `json:"messages" binding:"required" label:"messages"` // pi 的 state.messages
}

// State 保存 Node 在回合结束时交回的对话历史。
func (h *AgentBridgeHandler) State(c *gin.Context) {
	tok, ok := bearerToken(c)
	if !ok {
		response.Fail(c, errcode.ErrUnauthorized.WithMsg("缺少桥令牌"))
		return
	}
	var req stateReq
	if !bindJSON(c, &req) {
		return
	}
	if err := h.svc.SaveState(c.Request.Context(), tok, req.Messages); err != nil {
		bridgeFail(c, err)
		return
	}
	response.OK(c, nil)
}

// finishReq 是片段结束的请求。
type finishReq struct {
	Status  string `json:"status" binding:"required,oneof=done paused error" label:"status"` // done 跑完 / paused 因工具要求停下 / error 出错
	Message string `json:"message" binding:"max=500" label:"message"`                        // 出错时的说明
}

// Finish 报告运行片段结束。
func (h *AgentBridgeHandler) Finish(c *gin.Context) {
	tok, ok := bearerToken(c)
	if !ok {
		response.Fail(c, errcode.ErrUnauthorized.WithMsg("缺少桥令牌"))
		return
	}
	var req finishReq
	if !bindJSON(c, &req) {
		return
	}
	if err := h.svc.Finish(c.Request.Context(), tok, req.Status, req.Message); err != nil {
		bridgeFail(c, err)
		return
	}
	response.OK(c, nil)
}
