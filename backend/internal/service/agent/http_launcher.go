package agent

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// HTTPLauncher 让 Agent 作为独立的 HTTP 服务（见 agent/src/server.ts）时，运行时的接口不用变，
// 只是把「起一个子进程」换成「向服务发一个请求」：
//
//   - 第一次 Send 是启动参数：POST /segments，响应是一条 NDJSON 流，一直开到片段结束。
//     这条连接就是租约：Kill 关掉它，服务就中止这个片段；服务崩了，流断开，Done 立刻给出异常退出。
//   - 之后的 Send 是插话和中止，分别 POST /segments/{id}/steer、/abort。
//   - 流里除了 accepted 和最后的 exit，还有定时的 heartbeat；超过 HeartbeatTimeout 没收到任何一行，说明服务假死，
//     当作异常退出并关掉连接，服务那边也会放弃这个片段。
//
// 服务令牌随每个请求发出；这个启动器不碰子进程，LaunchSpec 里的可执行文件、参数、目录、环境变量都用不上。
type HTTPLauncher struct {
	BaseURL      string // Agent 服务的地址，如 http://agent:8090
	ServiceToken string
	// Client 为空用默认的：不走代理环境变量，连接超时 5 秒，不设整体超时（片段可能跑好几分钟）
	Client *http.Client
	// HeartbeatTimeout 是流里最长允许多久没有任何一行；0 取 60 秒（服务每 15 秒发一次心跳）
	HeartbeatTimeout time.Duration
}

const (
	defaultHeartbeatTimeout = 60 * time.Second
	controlTimeout          = 10 * time.Second
	maxNDJSONLine           = 4 << 20 // exit 行里带 stderr 末尾，远小于这个值
)

var (
	errHeartbeat = errors.New("heartbeat timeout")
	errKilled    = errors.New("killed")
)

// 内网服务之间的调用：不走 HTTP_PROXY 等环境变量，连不上要快速失败
var defaultAgentClient = &http.Client{Transport: &http.Transport{
	Proxy:                 nil,
	DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
	ResponseHeaderTimeout: 15 * time.Second,
	MaxIdleConnsPerHost:   8,
	IdleConnTimeout:       60 * time.Second,
}}

// Launch 为一个片段准备好连接，不发任何请求：启动参数在第一次 Send 里。
func (l *HTTPLauncher) Launch(ctx context.Context, _ LaunchSpec) (Proc, error) {
	raw := make([]byte, 16)
	_, _ = rand.Read(raw) // crypto/rand 在受支持的平台上不会失败
	// 片段要比发起它的 HTTP 请求活得久，所以不随请求取消；只有 Kill 或心跳超时会取消它
	runCtx, cancel := context.WithCancelCause(context.WithoutCancel(ctx))
	return &httpProc{l: l, id: hex.EncodeToString(raw), ctx: runCtx, cancel: cancel, done: make(chan ExitInfo, 1)}, nil
}

func (l *HTTPLauncher) client() *http.Client {
	if l.Client != nil {
		return l.Client
	}
	return defaultAgentClient
}

func (l *HTTPLauncher) heartbeatTimeout() time.Duration {
	if l.HeartbeatTimeout > 0 {
		return l.HeartbeatTimeout
	}
	return defaultHeartbeatTimeout
}

type httpProc struct {
	l      *HTTPLauncher
	id     string
	ctx    context.Context
	cancel context.CancelCauseFunc
	done   chan ExitInfo

	mu      sync.Mutex
	started bool
}

// Send 第一次是启动参数，之后是控制消息（插话、中止）。
func (p *httpProc) Send(line []byte) error {
	p.mu.Lock()
	first := !p.started
	p.started = true
	p.mu.Unlock()
	if first {
		return p.start(line)
	}
	return p.control(line)
}

// Kill 关掉连接：服务看到连接断开，就中止这个片段。
func (p *httpProc) Kill() { p.cancel(errKilled) }

// Done 在片段结束、连接断开或心跳超时后收到一次退出信息。
func (p *httpProc) Done() <-chan ExitInfo { return p.done }

func (p *httpProc) request(ctx context.Context, path string, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.l.BaseURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-Service-Token", p.l.ServiceToken)
	return req, nil
}

// start 发启动请求，等到响应头回来（服务收下了）才返回；被拒绝（令牌、重复、并发满）或连不上都是启动失败。
func (p *httpProc) start(input []byte) error {
	req, err := p.request(p.ctx, "/segments", input)
	if err != nil {
		return err
	}
	req.Header.Set("X-Segment-Id", p.id)
	resp, err := p.l.client().Do(req)
	if err != nil {
		return fmt.Errorf("连接 Agent 服务失败：%w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return fmt.Errorf("启动被拒绝：Agent 服务返回 HTTP %d：%s", resp.StatusCode, errorMessage(resp))
	}
	go p.read(resp.Body)
	return nil
}

// read 读 NDJSON 流直到 exit 行；没有 exit 行就结束说明服务崩了、被杀或假死，都是异常退出。
func (p *httpProc) read(body io.ReadCloser) {
	defer body.Close()
	defer p.cancel(nil) // 不管怎么结束都关掉连接

	timeout := p.l.heartbeatTimeout()
	watchdog := time.AfterFunc(timeout, func() { p.cancel(errHeartbeat) })
	defer watchdog.Stop()

	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 0, 64<<10), maxNDJSONLine)
	for sc.Scan() {
		watchdog.Reset(timeout) // accepted、heartbeat、exit 都算活着
		var m struct {
			Type       string `json:"type"`
			Code       int    `json:"code"`
			StderrTail string `json:"stderr_tail"`
		}
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			continue // 看不懂的行忽略，以后服务加新类型的消息不会把旧的 Go 弄坏
		}
		if m.Type == "exit" {
			p.done <- ExitInfo{Code: m.Code, StderrTail: m.StderrTail}
			return
		}
	}
	p.done <- ExitInfo{Code: -1, Err: p.abnormal(sc.Err(), timeout)}
}

// abnormal 说明连接为什么在没有 exit 行时结束。
func (p *httpProc) abnormal(scanErr error, timeout time.Duration) error {
	switch cause := context.Cause(p.ctx); {
	case errors.Is(cause, errHeartbeat):
		return fmt.Errorf("心跳超时：Agent 服务 %s 内没有发来任何消息", timeout)
	case errors.Is(cause, errKilled):
		return errors.New("片段被强制结束")
	case scanErr != nil:
		return fmt.Errorf("连接在片段结束前断开：%w", scanErr)
	}
	return errors.New("连接在片段结束前断开：Agent 服务关闭了连接")
}

// control 把插话、中止转给服务。服务回非 2xx（比如片段刚好结束了）就把原因交给调用方。
func (p *httpProc) control(line []byte) error {
	var m struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(line, &m); err != nil {
		return fmt.Errorf("控制消息不是合法的 JSON：%w", err)
	}
	var path string
	var body []byte
	switch m.Type {
	case "steer":
		path, body = "/segments/"+p.id+"/steer", controlBody(map[string]string{"text": m.Text})
	case "abort":
		path, body = "/segments/"+p.id+"/abort", []byte("{}")
	default:
		return fmt.Errorf("未知的控制消息：%q", m.Type)
	}
	ctx, cancel := context.WithTimeout(p.ctx, controlTimeout)
	defer cancel()
	req, err := p.request(ctx, path, body)
	if err != nil {
		return err
	}
	resp, err := p.l.client().Do(req)
	if err != nil {
		return fmt.Errorf("连接 Agent 服务失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("控制消息没有送达：Agent 服务返回 HTTP %d：%s", resp.StatusCode, errorMessage(resp))
	}
	return nil
}

// errorMessage 取服务标准错误响应里的 msg；不是 JSON（比如反向代理的 502 页面）就只说状态码。
func errorMessage(resp *http.Response) string {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	var e struct {
		Msg string `json:"msg"`
	}
	if json.Unmarshal(b, &e) == nil && e.Msg != "" {
		return e.Msg
	}
	return fmt.Sprintf("HTTP %d", resp.StatusCode)
}

func controlBody(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err) // 只含字符串的 map，序列化不会失败
	}
	return b
}
