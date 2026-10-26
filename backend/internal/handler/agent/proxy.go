package agent

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"video-canvas/internal/middleware"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/pkg/response"
)

// Agent 服务认前端请求的两个头：服务令牌证明请求来自 Go，用户头是 Go 校验过登录态后的结论。
const (
	agentServiceTokenHeader = "X-Agent-Service-Token"
	agentUserHeader         = "X-Agent-User"
)

// agentProxyHeaderTimeout 是等 Agent 服务回响应头的上限：REST 接口都是秒级的（发起运行只回 202）。
// apiPrefix 是对外 API 的路径前缀，转发时去掉。
const apiPrefix = "/api/v1"

const agentProxyHeaderTimeout = 30 * time.Second

// AgentProxyHandler 把前端的 Agent 接口（会话、运行、审批、评价）转给独立的 Agent 服务（agent.runtime=durable）。
// Go 只做两件事：校验登录态（路由上的 JWT 中间件），和把"这是谁"用服务令牌 + 用户头告诉 Agent；
// 业务和响应信封都由 Agent 服务负责，和 Go 自己的实现逐字一致（见黄金文件）。
type AgentProxyHandler struct {
	proxy *httputil.ReverseProxy
	token string
}

// NewAgentProxy 创建代理。target 是 Agent 服务的根地址（http(s)://host:port，不带路径），token 是服务令牌。
func NewAgentProxy(target, token string) (*AgentProxyHandler, error) {
	u, err := url.Parse(target)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("agent.url 必须是 http(s)://host:port 形式的地址")
	}
	if token == "" {
		return nil, errors.New("反向代理需要服务令牌")
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.ResponseHeaderTimeout = agentProxyHeaderTimeout
	h := &AgentProxyHandler{token: token}
	h.proxy = &httputil.ReverseProxy{
		Transport: tr,
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(u)
			// Agent 服务的路由不带 /api/v1 前缀（它不是对外的 API 网关）
			pr.Out.URL.Path = strings.TrimPrefix(pr.Out.URL.Path, apiPrefix)
			pr.Out.URL.RawPath = strings.TrimPrefix(pr.Out.URL.RawPath, apiPrefix)
			pr.Out.Host = u.Host
			// 用户的登录凭据到此为止（身份头由 Forward 覆盖成可信的值）
			pr.Out.Header.Del("Authorization")
			pr.Out.Header.Del("Cookie")
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			// 连不上、超时、被对端断开：只记日志，不把内部地址和错误细节给前端
			logger.Warn("转发 Agent 服务失败", zap.String("path", r.URL.Path), zap.Error(err))
			e := errcode.ErrAgentUnavailable
			writeJSONError(w, e.HTTPStatus(), e.Code, e.Msg)
		},
	}
	return h, nil
}

// Forward 是挂在各个 Agent 路由上的处理函数：必须放在 JWT 中间件之后。
func (h *AgentProxyHandler) Forward(c *gin.Context) {
	uid := middleware.GetUserID(c)
	if uid == 0 {
		response.Fail(c, errcode.ErrUnauthorized)
		return
	}
	c.Request.Header.Set(agentServiceTokenHeader, h.token)
	c.Request.Header.Set(agentUserHeader, strconv.FormatUint(uint64(uid), 10))
	h.proxy.ServeHTTP(plainWriter{c.Writer}, c.Request)
}

// plainWriter 只露出 ResponseWriter 本身：gin 的写入器声明自己实现 CloseNotifier，
// 但底层写入器（比如测试用的记录器）不一定有，ReverseProxy 一调就 panic。刷新靠 Unwrap 找到底层的 Flush。
type plainWriter struct{ http.ResponseWriter }

func (w plainWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// writeJSONError 按统一信封写一个错误（代理层出错时没有 gin 上下文可用）。
func writeJSONError(w http.ResponseWriter, status, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	b, _ := json.Marshal(response.Body{Code: code, Msg: msg})
	_, _ = w.Write(b)
}
