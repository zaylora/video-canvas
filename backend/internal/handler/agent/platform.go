package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/handler"
	"video-canvas/internal/model"
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
	// ProxyModelPlatform 代理一次模型调用（上下文模式）；onCall 在流开始之前收到这次调用的 id。
	ProxyModelPlatform(ctx context.Context, rc agentsvc.RunContext, body []byte, onCall func(callID uint64), write func([]byte)) error
	// PlatformRunUsage 查一个运行的用量；callID 不为 0 时一并给出这次调用的结算情况。
	PlatformRunUsage(ctx context.Context, rc agentsvc.RunContext, callID uint64) (*agentsvc.PlatformUsage, error)
	// ExecutePlatformTool 在上下文模式下执行一次工具调用；幂等键放在 ctx 里。
	ExecutePlatformTool(ctx context.Context, rc agentsvc.RunContext, toolCallID, name string, args json.RawMessage) (*agentsvc.ToolResult, error)
	// EndPlatformRun 释放一个运行在内存里的状态。
	EndPlatformRun(runID uint64)
	// ProposeApproval 为需要用户决定的工具调用（生成、删除、提问）算出审批内容和预估积分，不落库。
	ProposeApproval(ctx context.Context, rc agentsvc.RunContext, kind string, args json.RawMessage) (*agentsvc.ApprovalProposal, error)
	// ResolveApproval 把用户的决定解析成实际批准了什么，纯逻辑。
	ResolveApproval(ctx context.Context, rc agentsvc.RunContext, kind string, payload json.RawMessage, req model.DecideAgentApprovalReq) (*agentsvc.ApprovalOutcome, error)
	// ExecuteApproval 执行批准的内容（幂等，键是审批 id），并写出给模型看的结果文字。
	ExecuteApproval(ctx context.Context, rc agentsvc.RunContext, kind string, approvalID uint64, toolCallID string, outcome agentsvc.ApprovalOutcome) (*agentsvc.ApprovalExecution, error)
}

// PlatformHandler 是 Agent 服务调用 Go 平台能力的接口。它和桥挂在同一个内网监听上，必须配了服务令牌才挂载。
type PlatformHandler struct {
	svc PlatformAPI
}

// NewPlatformHandler 创建平台 handler。
func NewPlatformHandler(svc PlatformAPI) *PlatformHandler { return &PlatformHandler{svc: svc} }

// runContextFrom 读请求头里的运行上下文并校验格式；业务上的校验（身份字段是否齐、模式是否认识）在业务层。
func runContextFrom(c *gin.Context) (agentsvc.RunContext, error) {
	var rc agentsvc.RunContext
	raw := c.GetHeader(RunContextHeader)
	if raw == "" || len(raw) > maxRunContextHeader {
		return rc, errors.New("缺少或过大的 " + RunContextHeader)
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || json.Unmarshal(b, &rc) != nil {
		return rc, errors.New(RunContextHeader + " 必须是运行上下文 JSON 的 base64url")
	}
	return rc, nil
}

// parseRunContext 同 runContextFrom，失败时直接写标准错误响应。
func parseRunContext(c *gin.Context) (agentsvc.RunContext, bool) {
	rc, err := runContextFrom(c)
	if err != nil {
		response.Fail(c, errcode.ErrInvalidParams.WithMsg(err.Error()))
		return rc, false
	}
	return rc, true
}

// CallIDHeader 是响应头：上下文模式的模型代理把这次调用的 id（十进制字符串）放在这里，调用方事后据此查这次调用的积分。
const CallIDHeader = "X-Agent-Call-Id"

// ChatCompletions 是上下文模式的 OpenAI 兼容对话端点：Agent 服务里的 pi 把它当作模型提供方。
// 流开始之前出错（含上下文不合法）一律返回 OpenAI 格式的错误：调用方的 OpenAI 客户端只认这个格式。
func (h *PlatformHandler) ChatCompletions(c *gin.Context) {
	rc, err := runContextFrom(c)
	if err != nil {
		writeOpenAIError(c, http.StatusBadRequest, err.Error(), "invalid_request_error")
		return
	}
	serveModelStream(c, func(ctx context.Context, body []byte, headers http.Header, write func([]byte)) error {
		if c.GetHeader("X-Agent-Purpose") == agentsvc.ModelPurposeSummary { // 只认已知的用途：别的值一律当普通对话
			ctx = agentsvc.WithModelPurpose(ctx, agentsvc.ModelPurposeSummary)
		}
		return h.svc.ProxyModelPlatform(ctx, rc, body, func(id uint64) { headers.Set(CallIDHeader, strconv.FormatUint(id, 10)) }, write)
	})
}

// Usage 查一个运行的用量（本轮模型调用已实扣的积分、上下文大小）；带 call_id 时一并给出这次调用的结算情况。
// 路径里的运行必须和上下文里声明的一致：用户身份来自上下文，不能让一个运行去查另一个运行。
func (h *PlatformHandler) Usage(c *gin.Context) {
	rc, ok := parseRunContext(c)
	if !ok {
		return
	}
	runID, err := strconv.ParseUint(c.Param("run_id"), 10, 64)
	if err != nil || runID == 0 || runID != rc.RunID {
		response.Fail(c, errcode.ErrInvalidParams.WithMsg("路径里的 run_id 必须是正整数，并且和 "+RunContextHeader+" 里的一致"))
		return
	}
	var callID uint64
	if raw := c.Query("call_id"); raw != "" {
		if callID, err = strconv.ParseUint(raw, 10, 64); err != nil || callID == 0 {
			response.Fail(c, errcode.ErrInvalidParams.WithMsg("call_id 必须是正整数"))
			return
		}
	}
	out, err := h.svc.PlatformRunUsage(c.Request.Context(), rc, callID)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, out)
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

// proposeBody 是审批预估的请求体：kind 是审批种类（generate、delete、ask），args 是模型给那个工具的参数。
type proposeBody struct {
	Kind string          `json:"kind" binding:"required,max=32" label:"kind"`
	Args json.RawMessage `json:"args"` // 不传按空对象
}

// ProposeApproval 为一次需要用户决定的工具调用算出审批内容和预估积分。工具层的失败是 200 加 is_error，不是 HTTP 错误。
func (h *PlatformHandler) ProposeApproval(c *gin.Context) {
	rc, ok := parseRunContext(c)
	if !ok {
		return
	}
	var req proposeBody
	if !handler.BindJSON(c, &req) {
		return
	}
	args := req.Args
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	out, err := h.svc.ProposeApproval(c.Request.Context(), rc, req.Kind, args)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, out)
}

// resolveBody 是解析用户决定的请求体：payload 是预估时给出的审批内容，原样传回来。
type resolveBody struct {
	Kind     string                       `json:"kind" binding:"required,max=32" label:"kind"`
	Payload  json.RawMessage              `json:"payload" binding:"required" label:"payload"`
	Decision model.DecideAgentApprovalReq `json:"decision" binding:"required" label:"decision"`
}

// ResolveApproval 把用户的决定解析成实际批准了什么。超预算、一项都没批准这类业务错误原样返回标准错误。
func (h *PlatformHandler) ResolveApproval(c *gin.Context) {
	rc, ok := parseRunContext(c)
	if !ok {
		return
	}
	var req resolveBody
	if !handler.BindJSON(c, &req) {
		return
	}
	if string(req.Payload) == "null" {
		response.Fail(c, errcode.ErrInvalidParams.WithMsg("payload 不能是 null"))
		return
	}
	out, err := h.svc.ResolveApproval(c.Request.Context(), rc, req.Kind, req.Payload, req.Decision)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, out)
}

// executeBody 是执行审批的请求体。approval_id 是十进制字符串：雪花 ID 超过 2^53，用数字会在 JavaScript 里丢精度。
type executeBody struct {
	Kind       string                    `json:"kind" binding:"required,max=32" label:"kind"`
	ApprovalID string                    `json:"approval_id" binding:"required,max=20" label:"approval_id"`
	ToolCallID string                    `json:"tool_call_id" binding:"max=128" label:"tool_call_id"`
	Outcome    *agentsvc.ApprovalOutcome `json:"outcome" binding:"required" label:"outcome"`
}

// ExecuteApproval 执行用户批准的内容。执行是幂等的（键是审批 id）：调用方在记录结果前崩溃，重试得到的是第一次的结果。
func (h *PlatformHandler) ExecuteApproval(c *gin.Context) {
	rc, ok := parseRunContext(c)
	if !ok {
		return
	}
	var req executeBody
	if !handler.BindJSON(c, &req) {
		return
	}
	id, err := strconv.ParseUint(req.ApprovalID, 10, 64)
	if err != nil || id == 0 {
		response.Fail(c, errcode.ErrInvalidParams.WithMsg("approval_id 必须是正整数的十进制字符串"))
		return
	}
	out, err := h.svc.ExecuteApproval(c.Request.Context(), rc, req.Kind, id, req.ToolCallID, *req.Outcome)
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, out)
}
