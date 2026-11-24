package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/pkg/response"
	agentsvc "video-canvas/internal/service/agent"
	"video-canvas/pkg/version"
)

// SessionHeader 是 MCP 标准的会话标识头：一期只用于把同一个宿主的多次调用归到一条会话上，
// 不用来存服务端状态（多实例没有粘性时会丢）。
const SessionHeader = "Mcp-Session-Id"

// 支持的协议版本，新的在前；客户端请求的版本认识就照它回，不认识就给最新的。
var supportedProtocols = []string{"2025-06-18", "2025-03-26", "2024-11-05"}

// JSON-RPC 错误码。工具自身的失败不走这里，而是按 MCP 约定回 isError 的结果，让模型看到错误文本自己纠正。
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
)

// ToolExecutor 执行一次工具调用；真实实现是 *agentsvc.AgentBridge。
type ToolExecutor interface {
	ExecuteExternalTool(ctx context.Context, rc agentsvc.RunContext, toolCallID, name string, args json.RawMessage) (*agentsvc.ToolResult, error)
}

// CanvasDirectory 是 MCP 自己的画布读接口：
// GetByID 按 userID 过滤，本身就完成了「这块画布属于这个用户」的校验；List 给 canvas_list 用。
type CanvasDirectory interface {
	GetByID(ctx context.Context, userID, id uint64) (*model.CanvasProject, error)
	List(ctx context.Context, userID uint64, keyword string, offset, limit int) ([]model.CanvasProjectItem, int64, error)
}

// DramaDirectory 是短剧项目的只读目录，给 drama_list 用。
//
// 单独一个接口而不是复用 CanvasDirectory：网页端画布列表有意排除短剧（drama_id IS NULL），
// 而外部工具必须先能发现短剧画布，才能把 code 填进 drama_* 的 canvas 参数。
type DramaDirectory interface {
	List(ctx context.Context, userID uint64, keyword string, page, size int) ([]model.DramaProject, int64, error)
}

// Server 是对外 MCP 端点：一个 JSON-RPC 2.0 单端点，工具面来自画布 Agent 的工具声明。
type Server struct {
	tools    ToolExecutor
	canvases CanvasDirectory
	dramas   DramaDirectory
	// callTimeout 返回单次调用的服务端上限；为空时用 DefaultCallTimeout。
	callTimeout func(context.Context) time.Duration
}

// DefaultCallTimeout 是没配置时的单次调用上限。
// 一次调用要是挂上几分钟，客户端只能干等，用户也分不清是「慢」还是「卡死」——宁可早断早报。
const DefaultCallTimeout = 60 * time.Second

// NewServer 创建 MCP 端点。
func NewServer(tools ToolExecutor, canvases CanvasDirectory) *Server {
	return &Server{tools: tools, canvases: canvases}
}

// SetCallTimeout 注入单次调用的服务端上限（后台「注册设置」里的 MCP 调用超时）。
func (s *Server) SetCallTimeout(fn func(context.Context) time.Duration) { s.callTimeout = fn }

// SetDramaDirectory 注入短剧目录查询（drama_list 用）。
func (s *Server) SetDramaDirectory(d DramaDirectory) { s.dramas = d }

// timeoutFor 取这次调用的上限；取不到就用默认值。
func (s *Server) timeoutFor(ctx context.Context) time.Duration {
	if s.callTimeout == nil {
		return DefaultCallTimeout
	}
	if d := s.callTimeout(ctx); d > 0 {
		return d
	}
	return DefaultCallTimeout
}

// rpcRequest 是 JSON-RPC 2.0 请求。ID 用 RawMessage 原样回显（可能是数字、字符串或 null）。
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// rpcError 是 JSON-RPC 错误体。
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// rpcResponse 是 JSON-RPC 响应体。
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// Handle 处理一次 JSON-RPC 调用。鉴权已由 RequireMCPToken 完成，这里只做协议与权限内的事。
func (s *Server) Handle(c *gin.Context) {
	id := middleware.GetMCPIdentity(c)
	if id == nil {
		response.Fail(c, errcode.ErrMCPTokenInvalid)
		return
	}
	var req rpcRequest
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
		writeRPC(c, rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: codeParseError, Message: "请求不是合法 JSON"}})
		return
	}
	// 通知（没有 id）不需要回包。initialize 之外的 init 通知也走这里。
	if len(req.ID) == 0 {
		c.Status(http.StatusAccepted)
		return
	}
	switch req.Method {
	case "initialize":
		s.initialize(c, id, req)
	case "tools/list":
		writeRPC(c, rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"tools": catalog(id.Scopes)}})
	case "tools/call":
		s.callTool(c, id, req)
	case "ping":
		writeRPC(c, rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}})
	default:
		writeRPC(c, rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: codeMethodNotFound, Message: "不支持的方法：" + req.Method}})
	}
}

// initialize 回协议版本、能力与身份信息，并下发会话标识。
func (s *Server) initialize(c *gin.Context, id *middleware.MCPIdentity, req rpcRequest) {
	var params struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	_ = json.Unmarshal(req.Params, &params)
	if c.GetHeader(SessionHeader) == "" {
		c.Header(SessionHeader, newSessionID())
	}
	writeRPC(c, rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
		"protocolVersion": pickProtocol(params.ProtocolVersion),
		"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
		// 标题带 Beta：工具名、权限档位还会随反馈调整，接入方别把它当稳定契约。
		// version 用服务端构建版本，客户端只用来展示与排查。
		"serverInfo": map[string]any{
			"name":    "video-canvas",
			"title":   "画布 MCP 接入（Beta）",
			"version": version.Version,
		},
	}})
}

// callTool 执行一次工具调用：先查这个工具开不开放、身份有没有对应权限，再解析画布，最后交给工具层。
//
// 权限单位是用户：能访问哪些画布由「画布属不属于这个归属用户」决定，
// 调用方通过 arguments.canvas 指定本次操作哪一块，不指定就用项目配的默认画布。
func (s *Server) callTool(c *gin.Context, id *middleware.MCPIdentity, req rpcRequest) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil || params.Name == "" {
		writeRPC(c, rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: codeInvalidParams, Message: "缺少工具名"}})
		return
	}
	scope, open := ScopeFor(params.Name)
	if !open {
		writeRPC(c, rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: codeInvalidParams, Message: errcode.ErrMCPToolDenied.Msg}})
		return
	}
	if !scopeAllowed(id.Scopes, params.Name, scope) {
		writeRPC(c, rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: codeInvalidParams, Message: errcode.ErrMCPScopeDenied.Msg}})
		return
	}

	args, canvasCode, rpcErr := parseArgs(params.Arguments)
	if rpcErr != nil {
		writeRPC(c, rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: rpcErr})
		return
	}
	// canvas 是网关层的字段，不进工具：工具登记表里的画布工具作用在「会话绑定的画布」上，
	// 外部接入改成把它翻译成 RunContext.CanvasID。
	if params.Name == CanvasListToolName {
		s.listCanvases(c, id, req, args)
		return
	}
	if params.Name == DramaListToolName {
		s.listDramas(c, id, req, args)
		return
	}
	// 单次调用上限：从这里开始算，画布解析与工具执行都在这条 ctx 上
	ctx, cancel := context.WithTimeout(c.Request.Context(), s.timeoutFor(c.Request.Context()))
	defer cancel()
	canvasID, err := s.resolveCanvas(ctx, id, params.Name, canvasCode)
	if err != nil {
		// 画布问题（没指定 / 不属于自己）是模型能自己纠正的：按 MCP 约定回 isError 结果，
		// 让它在内容里读到「先去 canvas_list 查」，而不是撞一个协议层错误就没法继续。
		writeRPC(c, rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: toolResult(&agentsvc.ToolResult{Content: err.Error(), IsError: true})})
		return
	}
	body, err := json.Marshal(args)
	if err != nil {
		writeRPC(c, rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: codeInvalidParams, Message: "参数无法序列化"}})
		return
	}

	rc := agentsvc.RunContext{
		RunID:     runIDFor(id.ProjectID, c.GetHeader(SessionHeader)),
		SessionID: runIDFor(id.ProjectID, c.GetHeader(SessionHeader)),
		UserID:    id.UserID,
		CanvasID:  canvasID,
		// 短剧工具只登记在 drama 模式下，模式不对会被工具层直接拒；这里按工具名给对应的模式，
		// 不按画布类型猜——普通画布上误调 drama_read 应该由短剧服务回「只有短剧画布能用」。
		Mode: agentsvc.ExternalMode(params.Name),
		// 模型 / 技能 / 任务类工具不落在画布上：项目没配默认画布、这次也没指定 canvas 时
		// canvasID 是 0，声明一下让上下文校验放行，否则这四类工具会被「缺少 canvas_id」挡住。
		NoCanvas: !CanvasNeeded(params.Name),
	}
	res, err := s.tools.ExecuteExternalTool(ctx, rc, callID(id.ProjectID), params.Name, body)
	if ctx.Err() != nil {
		// 超时不是工具的错：按 isError 结果回，模型读到的是「这次太大了，拆小重试」
		msg := fmt.Sprintf("这次调用超过 %d 秒还没完成，已中止。把范围拆小重试（比如一次少改几个节点、改用 task_get 查进度），"+
			"或者让管理员在「注册设置」里调大 MCP 调用超时。", int(s.timeoutFor(c.Request.Context()).Seconds()))
		writeRPC(c, rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: toolResult(&agentsvc.ToolResult{Content: msg, IsError: true})})
		return
	}
	if err != nil {
		// 基础设施类失败（上下文不合法、工具被排除表拦下、库出错）按 RPC 错误回；
		// 工具自身的业务失败在 res.IsError 里，按 MCP 约定回结果。
		var bizErr *errcode.Error
		if errors.As(err, &bizErr) {
			writeRPC(c, rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: codeInternalError, Message: bizErr.Msg}})
			return
		}
		writeRPC(c, rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: codeInternalError, Message: "工具执行失败"}})
		return
	}
	writeRPC(c, rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: toolResult(res)})
}

// parseArgs 解出 arguments 并把 canvas 摘出来：canvas 只用于定画布，不传给工具。
// 只接受对象；不是对象就按参数错误回，避免工具收到没法解析的东西。
func parseArgs(raw json.RawMessage) (map[string]any, string, *rpcError) {
	args := map[string]any{}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, "", &rpcError{Code: codeInvalidParams, Message: "arguments 必须是对象"}
		}
		if args == nil {
			args = map[string]any{}
		}
	}
	code, _ := args["canvas"].(string)
	delete(args, "canvas")
	return args, strings.TrimSpace(code), nil
}

// scopeAllowed 判断这个身份能不能调这个工具。canvas_list 放宽到「任一画布权限」：
// 有写权限却查不到画布 code，等于给了钥匙没给门牌号。
func scopeAllowed(scopes map[string]bool, name, scope string) bool {
	if name == CanvasListToolName {
		return CanvasListVisible(scopes)
	}
	return scopes[scope]
}

// resolveCanvas 定出这次调用落在哪块画布：显式指定的优先，其次项目的默认画布。
// 无论走哪条，都按归属用户校验一次——这是外部接入唯一的资源边界。
// 不需要画布的工具（模型、技能、任务、列画布）返回 0 且不校验。
func (s *Server) resolveCanvas(ctx context.Context, id *middleware.MCPIdentity, toolName, code string) (uint64, error) {
	needed := CanvasNeeded(toolName)
	var canvasID uint64
	switch {
	case code != "":
		v, ok := idcodec.Decode(code)
		if !ok || v == 0 {
			return 0, errors.New("canvas 参数不是有效的画布 code（32 位十六进制），先用 canvas_list 查一下再试")
		}
		canvasID = v
	case id.DefaultCanvasID != 0:
		canvasID = id.DefaultCanvasID
	case needed:
		return 0, errors.New("这次调用没有指定画布，项目也没有配默认画布：先调 canvas_list 拿到画布 code，再填进 canvas 参数")
	}
	if canvasID == 0 {
		return 0, nil
	}
	if s.canvases == nil {
		return 0, errors.New("服务端没有装配画布查询，暂时不能操作画布")
	}
	if _, err := s.canvases.GetByID(ctx, id.UserID, canvasID); err != nil {
		// 不区分「不存在」与「不属于你」：对调用方是同一件事，细节只进日志。
		logger.Warn("MCP 调用的画布不属于归属用户",
			zap.Uint64("project_id", id.ProjectID), zap.Uint64("user_id", id.UserID), zap.Uint64("canvas_id", canvasID))
		return 0, errors.New("这块画布不在这个账号名下或已被删除：用 canvas_list 确认后再试")
	}
	return canvasID, nil
}

// listCanvases 实现 canvas_list：列归属用户名下的画布，条目自带 code，模型据此挑一块再用。
func (s *Server) listCanvases(c *gin.Context, id *middleware.MCPIdentity, req rpcRequest, args map[string]any) {
	if s.canvases == nil {
		writeRPC(c, rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: codeInternalError, Message: "服务端没有装配画布查询"}})
		return
	}
	keyword, _ := args["keyword"].(string)
	limit := 50
	if v, ok := args["limit"].(float64); ok && v > 0 {
		limit = int(v)
	}
	if limit > 200 {
		limit = 200
	}
	items, total, err := s.canvases.List(c.Request.Context(), id.UserID, strings.TrimSpace(keyword), 0, limit)
	if err != nil {
		logger.Error("MCP 列画布失败", zap.Error(err), zap.Uint64("user_id", id.UserID))
		writeRPC(c, rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: codeInternalError, Message: "查询画布失败"}})
		return
	}
	if items == nil {
		items = []model.CanvasProjectItem{}
	}
	// 条目用 CanvasProjectItem 自带的 MarshalJSON：id 已经是地址里那串 code，不用另做转换。
	body, err := json.Marshal(map[string]any{"canvases": items, "total": total, "returned": len(items)})
	if err != nil {
		writeRPC(c, rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: codeInternalError, Message: "画布列表无法序列化"}})
		return
	}
	writeRPC(c, rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: toolResult(&agentsvc.ToolResult{Content: string(body)})})
}

// listDramas 实现 drama_list：列归属用户名下的短剧，条目带短剧画布 code，
// 外部工具据此把 code 填进 drama_* 的 canvas 参数。
func (s *Server) listDramas(c *gin.Context, id *middleware.MCPIdentity, req rpcRequest, args map[string]any) {
	if s.dramas == nil {
		writeRPC(c, rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: codeInternalError, Message: "服务端没有装配短剧查询"}})
		return
	}
	keyword, _ := args["keyword"].(string)
	limit := 50
	if v, ok := args["limit"].(float64); ok && v > 0 {
		limit = int(v)
	}
	if limit > 200 {
		limit = 200
	}
	rows, total, err := s.dramas.List(c.Request.Context(), id.UserID, strings.TrimSpace(keyword), 1, limit)
	if err != nil {
		logger.Error("MCP 列短剧失败", zap.Error(err), zap.Uint64("user_id", id.UserID))
		writeRPC(c, rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: codeInternalError, Message: "查询短剧失败"}})
		return
	}
	items := make([]dramaListItem, 0, len(rows))
	for _, p := range rows {
		// 只放外部工具用得上的字段：原文、提示词设置这些既没必要传，也不该出现在外部工具上下文里。
		phase := p.Phase
		if phase == "" {
			phase = "done" // 早于自动规划的老项目
		}
		items = append(items, dramaListItem{
			Canvas:         idcodec.Encode(p.CanvasID),
			Title:          p.Title,
			Status:         p.Status,
			Phase:          phase,
			TargetEpisodes: p.TargetEpisodes,
			UpdatedAt:      p.UpdatedAt,
		})
	}
	body, err := json.Marshal(map[string]any{"dramas": items, "total": total, "returned": len(items)})
	if err != nil {
		writeRPC(c, rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: codeInternalError, Message: "短剧列表无法序列化"}})
		return
	}
	writeRPC(c, rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: toolResult(&agentsvc.ToolResult{Content: string(body)})})
}

// dramaListItem 是 drama_list 的一条：canvas 是短剧画布的 code，不是短剧项目 id。
type dramaListItem struct {
	Canvas         string    `json:"canvas"`
	Title          string    `json:"title"`
	Status         string    `json:"status"`
	Phase          string    `json:"phase"`
	TargetEpisodes int       `json:"target_episodes"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// toolResult 把工具结果转成 MCP 的 content 数组：文字一块，图片按 image 块附上（看图工具需要）。
func toolResult(res *agentsvc.ToolResult) map[string]any {
	content := make([]map[string]any, 0, 1+len(res.Images))
	text := res.Content
	if text == "" {
		text = "（没有内容）"
	}
	content = append(content, map[string]any{"type": "text", "text": text})
	for _, img := range res.Images {
		if img.Data == "" {
			continue
		}
		content = append(content, map[string]any{"type": "image", "data": img.Data, "mimeType": img.MimeType})
	}
	return map[string]any{"content": content, "isError": res.IsError}
}

// writeRPC 统一回包：MCP 的报文格式由协议规定，不能套平台统一的 code/msg/data 信封。
func writeRPC(c *gin.Context, resp rpcResponse) {
	resp.JSONRPC = "2.0"
	response.Raw(c, http.StatusOK, resp)
}

// pickProtocol 选协议版本：客户端说的认识就照它，否则给最新的。
func pickProtocol(want string) string {
	for _, p := range supportedProtocols {
		if want == p {
			return p
		}
	}
	return supportedProtocols[0]
}

// runIDFor 由项目和会话派生出稳定的运行号：同一会话的多次调用共用一份内存状态（读前写门禁靠它），
// 又不会和真实 Agent 运行撞号。
func runIDFor(projectID uint64, session string) uint64 {
	h := fnv.New64a()
	_, _ = fmt.Fprintf(h, "mcp:%d:%s", projectID, session)
	return h.Sum64()
}

// callID 生成本次调用的标识，只用于日志与工具内部的区分。
func callID(projectID uint64) string {
	raw := make([]byte, 6)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Sprintf("mcp-%d", projectID)
	}
	return "mcp-" + hex.EncodeToString(raw)
}

// newSessionID 生成会话标识。
func newSessionID() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "mcp-session"
	}
	return hex.EncodeToString(raw)
}
