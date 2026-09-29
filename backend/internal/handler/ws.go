package handler

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/pkg/response"
	"video-canvas/internal/pkg/ws"
)

// wsHub 是 WSHandler 依赖的连接管理器（由 *ws.Hub 实现），声明成接口便于测试替换。
type wsHub interface {
	// Serve 接管已 Upgrade 的连接，连接结束才返回。
	Serve(ws *websocket.Conn, userID uint64)
}

// WSHandler 提供用户级 WebSocket 的两个入口：签发 ticket 与建立连接。
type WSHandler struct {
	tickets  ws.TicketStore
	hub      wsHub
	upgrader websocket.Upgrader
}

// WSTicketResp 是 ticket 接口的响应体。
type WSTicketResp struct {
	Ticket    string `json:"ticket"`
	ExpiresIn int    `json:"expires_in"` // 秒
}

// NewWSHandler 创建 WSHandler。allowedOrigins 来自 cfg.Server.AllowedOrigins，
// 是 WebSocket 升级时允许的跨源 Origin 白名单（同源请求和无 Origin 的非浏览器客户端不受此限）。
func NewWSHandler(tickets ws.TicketStore, hub wsHub, allowedOrigins []string) *WSHandler {
	return &WSHandler{
		tickets: tickets,
		hub:     hub,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin:     newOriginChecker(allowedOrigins),
		},
	}
}

// newOriginChecker 构造 CheckOrigin，防止跨站 WebSocket 劫持（浏览器建 WS 不受同源策略限制，
// 恶意页面可以带着用户的登录态去连）。规则：
//  1. Origin 为空：不是浏览器发起的（curl、原生客户端），不存在被劫持的风险，允许
//  2. Origin 的 host 与请求 Host 相同：同源，允许
//  3. 否则必须精确命中白名单（忽略大小写和末尾斜杠），不支持通配符
func newOriginChecker(allowed []string) func(r *http.Request) bool {
	whitelist := make(map[string]struct{}, len(allowed))
	for _, o := range allowed {
		if n := normalizeOrigin(o); n != "" {
			whitelist[n] = struct{}{}
		}
	}
	return func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		u, err := url.Parse(origin)
		if err != nil || u.Host == "" {
			return false
		}
		if strings.EqualFold(u.Host, r.Host) {
			return true
		}
		_, ok := whitelist[normalizeOrigin(origin)]
		return ok
	}
}

// normalizeOrigin 统一大小写并去掉末尾斜杠，便于和白名单比较。
func normalizeOrigin(o string) string {
	return strings.ToLower(strings.TrimRight(strings.TrimSpace(o), "/"))
}

// IssueTicket 签发一次性 WebSocket ticket（需要登录，30 秒内有效，只能用一次）。
func (h *WSHandler) IssueTicket(c *gin.Context) {
	ticket, err := h.tickets.Issue(c.Request.Context(), currentUserID(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, WSTicketResp{Ticket: ticket, ExpiresIn: int(ws.TicketTTL.Seconds())})
}

// Connect 用 ticket 建立 WebSocket 连接（不走 JWT，身份由 ticket 决定）。
func (h *WSHandler) Connect(c *gin.Context) {
	// 1. 校验并消费 ticket：无效、过期、已用过统一返回 401
	userID, ok := h.tickets.Consume(c.Request.Context(), c.Query("ticket"))
	if !ok {
		response.Fail(c, errcode.ErrWSTicketInvalid)
		return
	}

	// 2. 升级协议；失败时 Upgrader 已经自己写了 HTTP 错误响应（如 Origin 不允许返回 403），这里不能再写
	ws, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.Warn("WebSocket 升级失败", zap.Uint64("user_id", userID), zap.Error(err))
		return
	}

	// 3. 连接已被接管，之后不能再用 gin 的 response 写入；Serve 会一直阻塞到连接结束
	h.hub.Serve(ws, userID)
}
