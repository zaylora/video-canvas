package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync/atomic"
	"time"

	"video-canvas/internal/provider/pluginproto"
)

// RunnerClient 是宿主访问 plugin-runner 的客户端。实现方把传输层失败归成两类哨兵：
// 连不上（请求没能送达）返回 ErrRunnerUnavailable；请求已送达、响应读取中途连接断开（runner 被杀）返回 ErrRunnerCrashed。
// 钩子本身的失败不是 error，而是 CallResponse.Error。
type RunnerClient interface {
	// Call 执行一个钩子。timeout 为 0 用 runner 的默认时限。
	Call(ctx context.Context, sha string, hook string, args []json.RawMessage, timeout time.Duration) (*pluginproto.CallResponse, error)
	// Load 把插件代码装进 runner（按 sha 缓存）。
	Load(ctx context.Context, sha, code string) (*pluginproto.LoadResponse, error)
	// Precheck 做上传预检，不登记。
	Precheck(ctx context.Context, code string) (*pluginproto.PrecheckResponse, error)
	// Ready 探活：runner 可用返回 nil，否则返回 ErrRunnerUnavailable。
	Ready(ctx context.Context) error
}

// ErrRunnerUnavailable 表示连不上 plugin-runner（未启动、正在重启、地址错误），请求没有送达。
var ErrRunnerUnavailable = errors.New("plugin-runner 不可用")

// ErrRunnerCrashed 表示请求已送达 plugin-runner，但在拿到完整响应前连接断开（通常是 runner 进程被杀，如内存超限）。
var ErrRunnerCrashed = errors.New("plugin-runner 在调用中崩溃")

const (
	// runnerBaseURL 是发给 runner 的请求的 URL 前缀；主机名只是占位，实际连接由 DialContext 决定。
	runnerBaseURL = "http://plugin-runner"
	// callGrace 是钩子时限之外给 runner 往返留的余量：runner 自己按钩子时限中断 JS，
	// 宿主的截止时间只用来防止 runner 卡死（如卡在不可中断的 Go 函数里）时调用方永远等下去。
	callGrace = 5 * time.Second
	// defaultRunnerOpTimeout 是 Load / Precheck 的默认时限（编译并执行到导出）。
	defaultRunnerOpTimeout = 30 * time.Second
	// maxRunnerResponseBytes 是 runner 响应体的读取上限：返回值 ≤1MB，另留日志与错误信息的余量。
	maxRunnerResponseBytes = 4 << 20
	// runnerErrorSnippet 是 runner 非 200 响应写进错误信息的最大字节数。
	runnerErrorSnippet = 300
)

// httpRunnerClient 用 HTTP + JSON 访问 runner。
type httpRunnerClient struct {
	hc *http.Client
}

// NewHTTPRunnerClient 创建经 unix socket 或本机 TCP 访问 runner 的客户端。
// runner 在本机，所以不走环境代理、不用 netguard；关闭连接复用，让“连不上”与“调用中断开”的区分没有歧义
// （复用的空闲连接被 runner 重启关掉时，标准库可能把它报成读失败，从而被误判为崩溃）。
func NewHTTPRunnerClient(network, address string) RunnerClient {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	tr := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, address)
		},
		DisableKeepAlives:  true,
		DisableCompression: true,
	}
	return &httpRunnerClient{hc: &http.Client{Transport: tr}}
}

// NewInProcessRunnerClient 创建直接调用 handler 的客户端（无网络），用于测试与 inprocess 模式。
// handler panic 被当作 runner 崩溃（ErrRunnerCrashed）。
func NewInProcessRunnerClient(h http.Handler) RunnerClient {
	return &httpRunnerClient{hc: &http.Client{Transport: inProcessTransport{h: h}}}
}

func (c *httpRunnerClient) Call(ctx context.Context, sha string, hook string, args []json.RawMessage, timeout time.Duration) (*pluginproto.CallResponse, error) {
	req := pluginproto.CallRequest{SHA256: sha, Hook: hook, Args: args, TimeoutMs: int(timeout.Milliseconds())}
	if timeout <= 0 {
		timeout = time.Duration(pluginproto.DefaultHookTimeoutMs) * time.Millisecond
	}
	var out pluginproto.CallResponse
	if err := c.post(ctx, pluginproto.PathCall, req, &out, timeout+callGrace); err != nil {
		var re *runnerStatusError
		if errors.As(err, &re) {
			// runner 对 /call 约定“成功失败都是 200”，非 200 说明请求本身被拒（如超过大小上限），按钩子失败处理
			return &pluginproto.CallResponse{Error: &pluginproto.CallError{Code: pluginproto.CodeInternal, Message: re.Error()}}, nil
		}
		return nil, err
	}
	return &out, nil
}

func (c *httpRunnerClient) Load(ctx context.Context, sha, code string) (*pluginproto.LoadResponse, error) {
	var out pluginproto.LoadResponse
	if err := c.post(ctx, pluginproto.PathLoad, pluginproto.LoadRequest{SHA256: sha, Code: code}, &out, defaultRunnerOpTimeout); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *httpRunnerClient) Precheck(ctx context.Context, code string) (*pluginproto.PrecheckResponse, error) {
	var out pluginproto.PrecheckResponse
	if err := c.post(ctx, pluginproto.PathPrecheck, pluginproto.PrecheckRequest{Code: code}, &out, defaultRunnerOpTimeout); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *httpRunnerClient) Ready(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, runnerBaseURL+pluginproto.PathHealth, nil)
	if err != nil {
		return fmt.Errorf("构造探活请求失败：%w", err)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("%w：%w", ErrRunnerUnavailable, err)
	}
	defer resp.Body.Close()
	// 探活响应体没有内容，读完只是为了让连接正常结束，读失败不影响结论
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, runnerErrorSnippet))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w：探活返回 HTTP %d", ErrRunnerUnavailable, resp.StatusCode)
	}
	return nil
}

// runnerStatusError 是 runner 返回了非 200 的响应。
type runnerStatusError struct {
	status int
	body   string
}

func (e *runnerStatusError) Error() string {
	return fmt.Sprintf("plugin-runner 返回 HTTP %d：%s", e.status, e.body)
}

// post 发一个 JSON 请求并解码 200 响应。传输层错误按“是否已建立连接”归成 ErrRunnerUnavailable / ErrRunnerCrashed；
// 调用方 ctx 结束时返回 ctx 的错误（不归类，调用方自己知道为什么结束）。
func (c *httpRunnerClient) post(ctx context.Context, path string, in, out any, timeout time.Duration) error {
	body, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("编码 runner 请求失败：%w", err)
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var connected atomic.Bool
	callCtx = httptrace.WithClientTrace(callCtx, &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) { connected.Store(true) }})
	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, runnerBaseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("构造 runner 请求失败：%w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return classifyRunnerErr(ctx, err, connected.Load())
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxRunnerResponseBytes+1))
	if err != nil {
		return classifyRunnerErr(ctx, err, true)
	}
	if resp.StatusCode != http.StatusOK {
		return &runnerStatusError{status: resp.StatusCode, body: truncate(strings.TrimSpace(string(raw)), runnerErrorSnippet)}
	}
	if len(raw) > maxRunnerResponseBytes {
		return &runnerStatusError{status: resp.StatusCode, body: "响应体超过上限"}
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return &runnerStatusError{status: resp.StatusCode, body: "响应不是合法的 JSON：" + err.Error()}
	}
	return nil
}

// classifyRunnerErr 把传输层错误归类：调用方 ctx 已结束 → 原样返回 ctx 错误；连接建立前失败 → 不可用；之后失败 → 崩溃。
func classifyRunnerErr(ctx context.Context, err error, connected bool) error {
	if ctx.Err() != nil {
		return fmt.Errorf("调用 plugin-runner 被取消：%w", ctx.Err())
	}
	if connected {
		return fmt.Errorf("%w：%w", ErrRunnerCrashed, err)
	}
	return fmt.Errorf("%w：%w", ErrRunnerUnavailable, err)
}

// inProcessTransport 把请求直接交给 handler 处理，不经过网络。
type inProcessTransport struct {
	h http.Handler
}

func (t inProcessTransport) RoundTrip(req *http.Request) (resp *http.Response, err error) {
	// 进程内没有“建立连接”这一步：一进来就算已连接，这样 handler panic 会被归为崩溃而不是不可用
	if tr := httptrace.ContextClientTrace(req.Context()); tr != nil && tr.GotConn != nil {
		tr.GotConn(httptrace.GotConnInfo{})
	}
	rec := &memResponseWriter{header: http.Header{}, status: http.StatusOK}
	defer func() {
		if r := recover(); r != nil {
			resp, err = nil, fmt.Errorf("进程内 runner panic：%v", r)
		}
	}()
	t.h.ServeHTTP(rec, req)
	if req.Body != nil {
		// 请求体已被 handler 消费或不再需要，关闭失败没有后果
		_ = req.Body.Close()
	}
	return &http.Response{
		StatusCode:    rec.status,
		Header:        rec.header,
		Body:          io.NopCloser(&rec.body),
		ContentLength: int64(rec.body.Len()),
		Request:       req,
	}, nil
}

// memResponseWriter 是最小的内存 ResponseWriter。
type memResponseWriter struct {
	header      http.Header
	body        bytes.Buffer
	status      int
	wroteHeader bool
}

func (w *memResponseWriter) Header() http.Header { return w.header }

func (w *memResponseWriter) Write(p []byte) (int, error) {
	w.wroteHeader = true
	return w.body.Write(p)
}

func (w *memResponseWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.status = status
}
