package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/response"
	"video-canvas/internal/pkg/ws"
)

// River UI 的会话 cookie 参数。
const (
	riverCookieName = "vc_river"
	riverSessionTTL = 30 * time.Minute
	// riverRenewBelow：会话剩余有效期低于它时，下一次请求顺带续期（滑动过期），连续使用时不会被踢出
	riverRenewBelow = 15 * time.Minute
)

// RiverUIHandler 把 River UI（任务级监控：查看、重试、取消、删除 job）嵌进后台，并负责它的鉴权。
//
// River UI 是一个独立的网页应用，浏览器在新标签页里直接打开它，带不上后台接口用的 Authorization 头，所以走“一次性 ticket 换会话 cookie”：
//  1. 后台页调 POST /admin/queue/river-ticket（Bearer 鉴权，仅 super_admin），得到带 ticket 的地址（ticket 一次性、30 秒有效）；
//  2. 新标签页打开该地址：校验并消费 ticket，下发 HttpOnly、SameSite=Strict、路径限定在 River UI 下的会话 cookie，再重定向到干净的地址；
//  3. 之后每个请求只认这个 cookie，并且每次都重新查用户角色：被降级、被封禁立即失效。
//
// River UI 能对 job 做重试和删除，所以只允许 super_admin 打开；admin 只能看自建页面。
type RiverUIHandler struct {
	ui      http.Handler
	tickets ws.TicketStore
	role    middleware.RoleLookup
	secret  []byte
	prefix  string
	now     func() time.Time
}

// NewRiverUIHandler 创建 River UI 的鉴权与托管。prefix 是挂载路径（如 /api/v1/admin/river），secret 用来给会话 cookie 签名。
func NewRiverUIHandler(ui http.Handler, tickets ws.TicketStore, role middleware.RoleLookup, secret, prefix string) *RiverUIHandler {
	return &RiverUIHandler{ui: ui, tickets: tickets, role: role, secret: []byte(secret), prefix: strings.TrimRight(prefix, "/"), now: time.Now}
}

// SetClock 替换时间来源（测试用）。
func (h *RiverUIHandler) SetClock(now func() time.Time) { h.now = now }

// IssueTicket 签发打开 River UI 的一次性地址（POST /admin/queue/river-ticket，仅 super_admin）。
func (h *RiverUIHandler) IssueTicket(c *gin.Context) {
	ticket, err := h.tickets.Issue(c.Request.Context(), currentUserID(c))
	if err != nil {
		response.Fail(c, err)
		return
	}
	response.OK(c, model.RiverTicketResult{URL: h.prefix + "/?ticket=" + url.QueryEscape(ticket)})
}

// Redirect 把没有结尾斜杠的入口地址补上斜杠：River UI 的静态资源用相对路径，缺斜杠会找不到。
func (h *RiverUIHandler) Redirect(c *gin.Context) {
	target := h.prefix + "/"
	if c.Request.URL.RawQuery != "" {
		target += "?" + c.Request.URL.RawQuery
	}
	c.Redirect(http.StatusFound, target)
}

// Serve 处理 /admin/river 下的全部请求（不经过 JWTAuth，身份由 ticket / 会话 cookie 决定）。
//
//  1. 带 ticket：消费它（一次性），确认用户当前仍是 super_admin，下发会话 cookie 并重定向到干净的地址；
//  2. 否则读会话 cookie：签名、有效期、角色任一不满足就拒绝；
//  3. 通过后交给 River UI；会话快过期时顺带续期。
func (h *RiverUIHandler) Serve(c *gin.Context) {
	ctx := c.Request.Context()
	if ticket := c.Query("ticket"); ticket != "" {
		userID, ok := h.tickets.Consume(ctx, ticket)
		if !ok {
			response.Fail(c, errcode.ErrUnauthorized.WithMsg("链接已失效，请回到后台重新打开"))
			return
		}
		if !h.isSuperAdmin(c, userID) {
			return
		}
		h.setSession(c, userID)
		c.Redirect(http.StatusFound, h.prefix+"/")
		return
	}

	userID, remain, ok := h.session(c)
	if !ok {
		response.Fail(c, errcode.ErrUnauthorized.WithMsg("会话已过期，请回到后台重新打开"))
		return
	}
	if !h.isSuperAdmin(c, userID) {
		return
	}
	if remain < riverRenewBelow {
		h.setSession(c, userID)
	}
	h.ui.ServeHTTP(c.Writer, c.Request)
}

// isSuperAdmin 实时查角色，不是 super_admin 就写 403 并返回 false（查询失败按服务端错误返回）。
func (h *RiverUIHandler) isSuperAdmin(c *gin.Context, userID uint64) bool {
	role, err := h.role(c.Request.Context(), userID)
	if err != nil {
		response.Fail(c, err)
		return false
	}
	if role != model.RoleSuperAdmin {
		response.Fail(c, errcode.ErrForbidden)
		return false
	}
	return true
}

// sign 返回 payload 的 HMAC-SHA256（十六进制）。
func (h *RiverUIHandler) sign(payload string) string {
	m := hmac.New(sha256.New, h.secret)
	m.Write([]byte("river-ui:" + payload))
	return hex.EncodeToString(m.Sum(nil))
}

// setSession 下发会话 cookie：值是 <用户id>.<过期时间戳>.<签名>。
func (h *RiverUIHandler) setSession(c *gin.Context, userID uint64) {
	exp := h.now().Add(riverSessionTTL)
	payload := strconv.FormatUint(userID, 10) + "." + strconv.FormatInt(exp.Unix(), 10)
	secure := c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
	//nolint:gosec // G124: Secure 取决于请求是否经过 TLS（含反向代理的 X-Forwarded-Proto）：纯 http 的内网部署下强制 Secure 会让浏览器直接丢掉 cookie，River UI 就打不开了
	http.SetCookie(c.Writer, &http.Cookie{
		Name: riverCookieName, Value: payload + "." + h.sign(payload), Path: h.prefix, Expires: exp, MaxAge: int(riverSessionTTL.Seconds()),
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode,
	})
}

// session 解析并校验会话 cookie，返回用户 id 与剩余有效期；缺失、格式不对、签名不符、已过期都返回 ok=false。
func (h *RiverUIHandler) session(c *gin.Context) (userID uint64, remain time.Duration, ok bool) {
	raw, err := c.Cookie(riverCookieName)
	if err != nil {
		return 0, 0, false
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return 0, 0, false
	}
	payload := parts[0] + "." + parts[1]
	if !hmac.Equal([]byte(h.sign(payload)), []byte(parts[2])) {
		return 0, 0, false
	}
	uid, err1 := strconv.ParseUint(parts[0], 10, 64)
	exp, err2 := strconv.ParseInt(parts[1], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	remain = time.Unix(exp, 0).Sub(h.now())
	if remain <= 0 {
		return 0, 0, false
	}
	return uid, remain, true
}
