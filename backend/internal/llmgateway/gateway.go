package llmgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"video-canvas/internal/provider/netguard"
)

const (
	defaultIdleTimeout = 90 * time.Second // 默认的空闲超时：推理模型首个 Token 可能要等一会儿
	maxErrBodyBytes    = 8 << 10          // 读错误响应体最多 8KB
	maxErrRunes        = 300              // 给用户看的上游报错最多 300 字
)

// Options 是网关的可选项。
type Options struct {
	// IdleTimeout 是上游多久没发数据就判定超时，含等第一个字节的时间；0 取 90 秒。
	IdleTimeout time.Duration
	// Dial 用于测试里替换拨号；生产不设。
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)
}

// Gateway 向渠道的 OpenAI 兼容接口发流式对话请求。可以并发使用。
type Gateway struct {
	opts       Options
	limiters   limiterSet
	transports [2]*http.Transport // 0 普通渠道，1 可信内网渠道
	tmu        sync.Mutex
}

// New 创建网关。
func New(opts Options) *Gateway {
	if opts.IdleTimeout <= 0 {
		opts.IdleTimeout = defaultIdleTimeout
	}
	return &Gateway{opts: opts}
}

// Stream 发一次流式对话请求，每个增量通过 emit 回调给调用方，返回完整结果。
//
// 失败时仍会返回已经收到的部分结果（取消、断流、空闲超时），调用方据此按已产生的用量结算；
// 上游 HTTP 错误返回 *UpstreamError，被 SSRF 防护拒绝的请求满足 netguard.IsGuardError。
// 同一渠道的并发和速率受渠道配置限制，排队期间取消 ctx 会立即返回。
func (g *Gateway) Stream(ctx context.Context, tg Target, req Request, emit func(Event)) (*Result, error) {
	// 1. 先校验：渠道地址、Key、上游模型名、消息
	base, err := validateTarget(tg)
	if err != nil {
		return nil, err
	}
	if len(req.Messages) == 0 {
		return nil, fmt.Errorf("%w: 没有消息", ErrInvalidRequest)
	}
	body, err := buildBody(tg, req)
	if err != nil {
		return nil, err
	}
	// 2. 渠道限流：同时在途数和速率；排队时可被取消
	release, err := g.limiters.get(tg.ChannelKey, tg.RPS, tg.MaxConcurrency).acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	// 3. 空闲看门狗：每收到数据重置计时，超时就取消请求
	ctx2, cancel := context.WithCancel(ctx)
	defer cancel()
	var idle atomic.Bool
	timer := time.AfterFunc(g.opts.IdleTimeout, func() { idle.Store(true); cancel() })
	defer timer.Stop()
	touch := func() { timer.Reset(g.opts.IdleTimeout) }

	res, err := g.do(ctx2, tg, base, body, emit, touch)
	// 4. 把「空闲超时」和「调用方取消」区分开：看门狗触发的取消不是调用方的意思
	switch {
	case err == nil:
		return res, nil
	case idle.Load():
		return res, ErrIdleTimeout
	case ctx.Err() != nil:
		return res, ctx.Err()
	}
	return res, err
}

// do 发请求并读响应。
func (g *Gateway) do(ctx context.Context, tg Target, base *url.URL, body []byte, emit func(Event), touch func()) (*Result, error) {
	endpoint := strings.TrimRight(base.String(), "/") + chatEndpoint
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidTarget, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Authorization", "Bearer "+tg.APIKey)

	// 白名单只有渠道自己的域名；重定向一律不跟随，否则带着 Key 的请求会被转到别处
	client := netguard.NewClient(g.transport(tg.TrustedInternal), []string{strings.ToLower(base.Hostname())}, 0)
	resp, err := client.Do(req)
	if err != nil {
		return nil, redactErr(err, tg.APIKey)
	}
	defer resp.Body.Close()
	touch()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, classifyHTTPError(resp, tg.APIKey)
	}
	if strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
		return readWhole(resp.Body, emit)
	}
	return readSSE(resp.Body, emit, touch)
}

// transport 懒创建并缓存带 SSRF 拨号校验的传输层。
func (g *Gateway) transport(trusted bool) *http.Transport {
	idx := 0
	if trusted {
		idx = 1
	}
	g.tmu.Lock()
	defer g.tmu.Unlock()
	if g.transports[idx] == nil {
		cfg := &netguard.Config{Dial: g.opts.Dial}
		if trusted {
			cfg.IPAllowed = func(net.IP) bool { return true }
		}
		cfg.ApplyDefaults()
		g.transports[idx] = netguard.NewTransport(cfg)
	}
	return g.transports[idx]
}

// validateTarget 校验渠道配置，返回解析后的地址：必须是 http(s)、有主机、不带用户名密码。
func validateTarget(tg Target) (*url.URL, error) {
	u, err := url.Parse(tg.BaseURL)
	switch {
	case err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "":
		return nil, fmt.Errorf("%w: 地址必须是带主机的 http(s) 地址", ErrInvalidTarget)
	case u.User != nil:
		return nil, fmt.Errorf("%w: 地址不能带用户名密码", ErrInvalidTarget)
	case tg.APIKey == "":
		return nil, fmt.Errorf("%w: 渠道 Key 尚未设置", ErrInvalidTarget)
	case tg.UpstreamModel == "":
		return nil, fmt.Errorf("%w: 没有上游模型名", ErrInvalidTarget)
	}
	return u, nil
}

// classifyHTTPError 把上游的非 2xx 响应转成 *UpstreamError：给用户的文案不含 Key，限流和上游故障可重试。
func classifyHTTPError(resp *http.Response, key string) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrBodyBytes)) // 读不全也没关系，只用来提取一句说明
	e := &UpstreamError{Status: resp.StatusCode}
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		e.Message = "渠道 Key 无效或已失效，请联系管理员"
	case resp.StatusCode == http.StatusForbidden:
		e.Message = "渠道没有权限访问该模型，请联系管理员"
	case resp.StatusCode == http.StatusTooManyRequests:
		e.Message, e.Retryable = "上游请求太频繁，请稍后重试", true
	case resp.StatusCode >= 500:
		e.Message, e.Retryable = fmt.Sprintf("上游服务暂时不可用（HTTP %d）", resp.StatusCode), true
	default:
		e.Message = upstreamMessage(raw, key, resp.StatusCode)
	}
	return e
}

// upstreamMessage 从上游的错误体里取 error.message，脱敏并截断；取不到给通用说明。
func upstreamMessage(raw []byte, key string, status int) string {
	var b struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &b) == nil && b.Error.Message != "" {
		return truncate(strings.ReplaceAll(b.Error.Message, key, "***"), maxErrRunes)
	}
	return fmt.Sprintf("上游拒绝了请求（HTTP %d）", status)
}

// redactErr 去掉错误里可能带出的 Key。
func redactErr(err error, key string) error {
	if key != "" && strings.Contains(err.Error(), key) {
		return errors.New(strings.ReplaceAll(err.Error(), key, "***"))
	}
	return err
}

// truncate 按字数截断，超出加省略号。
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
