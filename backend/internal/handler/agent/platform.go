package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"regexp"
	"strconv"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/handler"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/response"
	agentsvc "video-canvas/internal/service/agent"
)

// 平台 API 的请求头。上下文模式下没有桥令牌：调用方（Agent 服务）用服务令牌认证，运行信息放在 RunContextHeader 里。
const (
	// RunContextHeader 的值是运行上下文的 JSON 再做 base64url（无填充）：一个头装下所有字段，也不用操心非 ASCII 字符。
	// JSON 里的 ID 是十进制字符串（雪花 ID 超过 2^53，用数字会在 JavaScript 里丢精度）。
	RunContextHeader = "X-Agent-Context"
	// IdempotencyKeyHeader 是调用方给的幂等键（Pi 的工具任务 id）；带了键的画布写入重放时不会再写一遍。
	IdempotencyKeyHeader = "X-Idempotency-Key"

	maxRunContextHeader = 16 << 10
)

// idemKeyPattern 限制幂等键的字符和长度：它要存进 varchar(64) 的列。
var idemKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,64}$`)

// PlatformAPI 是平台 handler 依赖的业务，由 *agentsvc.AgentBridge 实现。
type PlatformAPI interface {
	// ExecutePlatformTool 在上下文模式下执行一次工具调用；幂等键放在 ctx 里。
	ExecutePlatformTool(ctx context.Context, rc agentsvc.RunContext, toolCallID, name string, args json.RawMessage) (*agentsvc.ToolResult, error)
	// EndPlatformRun 释放一个运行在内存里的状态。
	EndPlatformRun(runID uint64)
}

// PlatformHandler 是 Agent 服务调用 Go 平台能力的接口。它和桥挂在同一个内网监听上，必须配了服务令牌才挂载。
type PlatformHandler struct {
	svc PlatformAPI
}

// NewPlatformHandler 创建平台 handler。
func NewPlatformHandler(svc PlatformAPI) *PlatformHandler { return &PlatformHandler{svc: svc} }

// parseRunContext 读请求头里的运行上下文并校验格式；业务上的校验（身份字段是否齐、模式是否认识）在业务层。
func parseRunContext(c *gin.Context) (agentsvc.RunContext, bool) {
	var rc agentsvc.RunContext
	raw := c.GetHeader(RunContextHeader)
	if raw == "" || len(raw) > maxRunContextHeader {
		response.Fail(c, errcode.ErrInvalidParams.WithMsg("缺少或过大的 "+RunContextHeader))
		return rc, false
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || json.Unmarshal(b, &rc) != nil {
		response.Fail(c, errcode.ErrInvalidParams.WithMsg(RunContextHeader+" 必须是运行上下文 JSON 的 base64url"))
		return rc, false
	}
	return rc, true
}

// toolBody 是工具调用的请求体。
type toolBody struct {
	ToolCallID string          `json:"tool_call_id" binding:"max=128" label:"tool_call_id"` // 模型给这次调用的 id，只用于记录和展示；不是幂等键，它在不同回合里可能重复
	Args       json.RawMessage `json:"args"`                                                // 工具参数，不传按空对象
}

// Tool 执行一次工具调用。工具名在路径里；工具层的失败是 200 加 is_error，不是 HTTP 错误。
func (h *PlatformHandler) Tool(c *gin.Context) {
	rc, ok := parseRunContext(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	if key := c.GetHeader(IdempotencyKeyHeader); key != "" {
		if !idemKeyPattern.MatchString(key) {
			response.Fail(c, errcode.ErrInvalidParams.WithMsg(IdempotencyKeyHeader+" 只能是 1 到 64 个字母、数字或 . _ : -"))
			return
		}
		ctx = agentsvc.WithIdempotencyKey(ctx, key)
	}
	var req toolBody
	if !handler.BindJSON(c, &req) {
		return
	}
	args := req.Args
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	res, err := h.svc.ExecutePlatformTool(ctx, rc, req.ToolCallID, c.Param("name"), args)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, res)
}

// EndRun 释放一个运行在内存里的状态。运行结束时 Agent 服务调用；不调用也会在闲置过期后清掉。
func (h *PlatformHandler) EndRun(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("run_id"), 10, 64)
	if err != nil || id == 0 {
		response.Fail(c, errcode.ErrInvalidParams.WithMsg("run_id 必须是正整数"))
		return
	}
	h.svc.EndPlatformRun(id)
	response.OK(c, nil)
}
