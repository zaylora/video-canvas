// 本文件：netguard 的单元测试：IsBlockedIP 各网段、域名白名单、内网拦截、DNS 解析后校验并直接拨号（防 rebinding）、
// 重定向逐跳校验、不走环境代理、CheckURLAllowed、IsGuardError、Config 默认值。

package netguard_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"video-canvas/internal/provider/netguard"
)

func TestIsBlockedIP(t *testing.T) {
	tests := []struct {
		ip   string
		want bool
	}{
		{"127.0.0.1", true}, {"127.255.255.254", true},
		{"10.0.0.1", true}, {"10.255.255.255", true},
		{"172.16.0.1", true}, {"172.31.255.255", true}, {"172.15.255.255", false}, {"172.32.0.1", false},
		{"192.168.1.1", true},
		{"169.254.169.254", true}, {"169.254.0.1", true},
		{"0.0.0.0", true}, {"0.1.2.3", true},
		{"100.100.100.200", true}, {"100.64.0.1", true},
		{"255.255.255.255", true}, {"240.0.0.1", true},
		{"224.0.0.1", true},
		{"192.0.0.8", true}, {"192.0.2.1", true}, {"198.18.0.1", true}, {"198.51.100.1", true}, {"203.0.113.1", true},
		{"::1", true}, {"::", true},
		{"fc00::1", true}, {"fd12:3456::1", true},
		{"fe80::1", true},
		{"ff02::1", true},
		{"2001:db8::1", true},
		{"::7f00:1", true}, {"::808:808", true}, // IPv4 兼容地址（已废弃的写法），整段拒绝
		{"fec0::1", true}, // 站点本地（已废弃的 IPv6 私网段）
		{"100::1", true},  // 丢弃专用前缀
		{"::ffff:127.0.0.1", true}, {"::ffff:10.0.0.1", true}, {"::ffff:8.8.8.8", false},
		{"64:ff9b::7f00:1", true}, {"64:ff9b::808:808", true}, // NAT64 前缀本身被禁
		{"2002:7f00:1::", true}, {"2002:0a00:1::", true}, // 6to4 内嵌 127.0.0.1 / 10.0.0.1
		{"2002:0808:0808::", false}, // 6to4 内嵌公网 8.8.8.8
		{"8.8.8.8", false}, {"93.184.216.34", false}, {"1.1.1.1", false},
		{"2606:4700:4700::1111", false},
	}
	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			if got := netguard.IsBlockedIP(net.ParseIP(tt.ip)); got != tt.want {
				t.Fatalf("IsBlockedIP(%s) = %v，期望 %v", tt.ip, got, tt.want)
			}
		})
	}
	if !netguard.IsBlockedIP(nil) {
		t.Fatal("nil 应被拒绝")
	}
}

// mapResolver 是固定映射的解析器；没有映射的域名返回错误。
type mapResolver map[string][]net.IP

func (m mapResolver) LookupIP(_ context.Context, host string) ([]net.IP, error) {
	if ips, ok := m[host]; ok {
		return ips, nil
	}
	return nil, errors.New("no such host: " + host)
}

// flipResolver 模拟 DNS rebinding：第一次解析返回公网地址，之后返回内网地址。
type flipResolver struct {
	mu    sync.Mutex
	calls int
}

func (r *flipResolver) LookupIP(_ context.Context, _ string) ([]net.IP, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if r.calls == 1 {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}
	return []net.IP{net.ParseIP("127.0.0.1")}, nil
}

func ip(s string) net.IP { return net.ParseIP(s) }

// onlyLoopback 把 127.0.0.1 当作“公网”，其余一律拒绝：让测试服务器（监听在回环）能被连上，同时保留拦截能力。
func onlyLoopback(p net.IP) bool { return p.Equal(ip("127.0.0.1")) }

// newClient 用给定的 Config 与白名单构造受防护的客户端。
func newClient(cfg *netguard.Config, allowed []string) *http.Client {
	cfg.ApplyDefaults()
	c := netguard.NewClient(netguard.NewTransport(cfg), allowed, cfg.MaxRedirects)
	c.Timeout = 5 * time.Second
	return c
}

// get 发一个 GET 请求并关闭响应体。
func get(c *http.Client, target string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(req)
	if err == nil {
		_ = resp.Body.Close()
	}
	return resp, err
}

// countingServer 启动一个记录命中次数的测试服务器。
func countingServer(t *testing.T) (srv *httptest.Server, hits *atomic.Int32) {
	t.Helper()
	hits = &atomic.Int32{}
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)
	return srv, hits
}

func TestClient_默认拒绝内网地址(t *testing.T) {
	srv, hits := countingServer(t)
	u, _ := url.Parse(srv.URL)
	port := u.Port()

	tests := []struct {
		name string
		host string // 同时写进白名单：白名单放行也不能绕过 IP 拦截
		url  string
	}{
		{"127.0.0.1", "127.0.0.1", "http://127.0.0.1:" + port},
		{"10.x", "10.1.2.3", "http://10.1.2.3:" + port},
		{"169.254 元数据地址", "169.254.169.254", "http://169.254.169.254"},
		{"192.168.x", "192.168.0.10", "http://192.168.0.10:8080"},
		{"172.16.x", "172.16.0.1", "http://172.16.0.1"},
		{"0.0.0.0", "0.0.0.0", "http://0.0.0.0:" + port},
		{"云厂商元数据 100.100.100.200", "100.100.100.200", "http://100.100.100.200"},
		{"IPv6 回环", "::1", "http://[::1]:" + port},
		{"IPv6 ULA", "fd00::1", "http://[fd00::1]:" + port},
		{"IPv6 链路本地", "fe80::1", "http://[fe80::1]:" + port},
		{"IPv4 映射的回环", "::ffff:127.0.0.1", "http://[::ffff:127.0.0.1]:" + port},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newClient(&netguard.Config{}, []string{tt.host})
			start := time.Now()
			_, err := get(c, tt.url)
			if !errors.Is(err, netguard.ErrBlockedAddress) || !netguard.IsGuardError(err) {
				t.Fatalf("应被拒绝为 ErrBlockedAddress：%v", err)
			}
			if time.Since(start) > 2*time.Second {
				t.Fatal("拦截应发生在拨号之前，不应等待连接超时")
			}
		})
	}
	if hits.Load() != 0 {
		t.Fatalf("被拦截的请求不应到达服务器，实际命中 %d 次", hits.Load())
	}
}

func TestClient_域名不在白名单(t *testing.T) {
	srv, hits := countingServer(t)
	u, _ := url.Parse(srv.URL)
	c := newClient(&netguard.Config{IPAllowed: onlyLoopback}, []string{"www.runninghub.cn"})

	_, err := get(c, "http://"+net.JoinHostPort("127.0.0.1", u.Port()))
	if !errors.Is(err, netguard.ErrHostNotAllowed) || !netguard.IsGuardError(err) {
		t.Fatalf("期望 ErrHostNotAllowed，实际 %v", err)
	}
	if hits.Load() != 0 {
		t.Fatal("请求不应到达服务器")
	}
}

func TestClient_通配符白名单(t *testing.T) {
	srv, _ := countingServer(t)
	u, _ := url.Parse(srv.URL)
	cfg := &netguard.Config{
		Resolver:  mapResolver{"api.runninghub.test": {ip("127.0.0.1")}},
		IPAllowed: onlyLoopback,
	}
	target := "http://api.runninghub.test:" + u.Port()

	if _, err := get(newClient(cfg, []string{"*.runninghub.test"}), target); err != nil {
		t.Fatalf("通配符白名单应放行：%v", err)
	}
	// 通配符不匹配根域名本身，也不匹配别的域名
	for _, allowed := range []string{"*.other.test", "runninghub.test", "*.api.runninghub.test"} {
		_, err := get(newClient(cfg, []string{allowed}), target)
		if !errors.Is(err, netguard.ErrHostNotAllowed) {
			t.Errorf("白名单 %q 不应放行：%v", allowed, err)
		}
	}
}

func TestClient_DNS解析到内网被拒绝(t *testing.T) {
	// “白名单里的域名”解析到内网：DNS rebinding 的静态形态
	res := mapResolver{
		"evil.allowed.test":  {ip("10.0.0.5")},
		"mixed.allowed.test": {ip("93.184.216.34"), ip("127.0.0.1")}, // 公网 + 内网混合应答，整体拒绝
		"meta.allowed.test":  {ip("169.254.169.254")},
	}
	for host := range res {
		t.Run(host, func(t *testing.T) {
			var dialed atomic.Int32
			cfg := &netguard.Config{
				Resolver: res,
				Dial: func(context.Context, string, string) (net.Conn, error) {
					dialed.Add(1)
					return nil, errors.New("不应拨号")
				},
			}
			_, err := get(newClient(cfg, []string{"*.allowed.test"}), "http://"+host)
			if !errors.Is(err, netguard.ErrBlockedAddress) {
				t.Fatalf("应被拒绝：%v", err)
			}
			if dialed.Load() != 0 {
				t.Fatalf("不应拨号，实际 %d 次", dialed.Load())
			}
		})
	}
}

func TestClient_DNSRebinding直接拨校验过的IP(t *testing.T) {
	res := &flipResolver{}
	var mu sync.Mutex
	var dialed []string
	cfg := &netguard.Config{
		Resolver: res,
		Dial: func(_ context.Context, _, addr string) (net.Conn, error) {
			mu.Lock()
			dialed = append(dialed, addr)
			mu.Unlock()
			return nil, errors.New("模拟连接失败")
		},
	}
	_, err := get(newClient(cfg, []string{"rebind.allowed.test"}), "http://rebind.allowed.test")
	if err == nil || netguard.IsGuardError(err) {
		t.Fatalf("公网 IP 校验通过后拨号失败，不应是防护错误：%v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	// 只解析一次，并且拨的是校验过的 IP 而不是域名——第二次解析（内网）没有机会发生
	if res.calls != 1 {
		t.Fatalf("每次拨号只应解析一次，实际 %d 次", res.calls)
	}
	if len(dialed) != 1 || dialed[0] != "93.184.216.34:80" {
		t.Fatalf("应直接拨校验过的 IP，实际 %v", dialed)
	}
}

func TestClient_多个解析结果依次尝试(t *testing.T) {
	var mu sync.Mutex
	var dialed []string
	srv, _ := countingServer(t)
	u, _ := url.Parse(srv.URL)
	realDial := (&net.Dialer{Timeout: time.Second}).DialContext
	cfg := &netguard.Config{
		Resolver:  mapResolver{"multi.allowed.test": {ip("127.0.0.2"), ip("127.0.0.1")}},
		IPAllowed: func(p net.IP) bool { return p.IsLoopback() },
		Dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			mu.Lock()
			dialed = append(dialed, addr)
			mu.Unlock()
			if strings.HasPrefix(addr, "127.0.0.2:") {
				return nil, errors.New("模拟第一个地址不可达")
			}
			return realDial(ctx, network, addr)
		},
	}
	if _, err := get(newClient(cfg, []string{"multi.allowed.test"}), "http://multi.allowed.test:"+u.Port()); err != nil {
		t.Fatalf("第一个地址失败后应换下一个：%v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(dialed) != 2 || !strings.HasPrefix(dialed[0], "127.0.0.2:") || !strings.HasPrefix(dialed[1], "127.0.0.1:") {
		t.Fatalf("拨号顺序不符：%v", dialed)
	}
}

func TestClient_解析失败与无结果(t *testing.T) {
	t.Run("解析出错", func(t *testing.T) {
		_, err := get(newClient(&netguard.Config{Resolver: mapResolver{}}, []string{"nx.allowed.test"}), "http://nx.allowed.test")
		if err == nil || !strings.Contains(err.Error(), "解析域名") || netguard.IsGuardError(err) {
			t.Fatalf("应报解析失败且不是防护错误：%v", err)
		}
	})
	t.Run("没有解析结果", func(t *testing.T) {
		cfg := &netguard.Config{Resolver: mapResolver{"empty.allowed.test": {}}}
		_, err := get(newClient(cfg, []string{"empty.allowed.test"}), "http://empty.allowed.test")
		if err == nil || !strings.Contains(err.Error(), "没有解析结果") {
			t.Fatalf("应报没有解析结果：%v", err)
		}
	})
}

func TestClient_自定义IPAllowed让白名单域名落在回环(t *testing.T) {
	// 证明拨号确实使用注入的解析结果（而不是系统 DNS）：域名根本不存在，却能连上测试服务器
	// （这也是“渠道级内网例外”的用法：trusted_internal 的渠道传入放行内网的 IPAllowed）
	srv, hits := countingServer(t)
	u, _ := url.Parse(srv.URL)
	cfg := &netguard.Config{
		Resolver:  mapResolver{"platform.allowed.test": {ip("127.0.0.1")}},
		IPAllowed: func(net.IP) bool { return true },
	}
	resp, err := get(newClient(cfg, []string{"platform.allowed.test"}), "http://platform.allowed.test:"+u.Port())
	if err != nil || resp.StatusCode != http.StatusOK || hits.Load() != 1 {
		t.Fatalf("应能连上：err=%v hits=%d", err, hits.Load())
	}
}

// redirectFixture 构造重定向测试环境：pub.allowed.test -> 127.0.0.1（唯一被放行的“公网”），
// priv.allowed.test -> 10.0.0.5（内网）。/go 会跳到 target，/loop 无限自跳，/hop/N 连续跳 N 次后返回 200。
type redirectFixture struct {
	client *http.Client
	start  string // 入口前缀，形如 http://pub.allowed.test:port
	port   string
	target atomic.Value // string
}

func newRedirectFixture(t *testing.T, maxRedirects int) *redirectFixture {
	t.Helper()
	f := &redirectFixture{}
	f.target.Store("")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/ok":
			_, _ = w.Write([]byte("OK"))
		case r.URL.Path == "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		case strings.HasPrefix(r.URL.Path, "/hop/"):
			var n int
			_, _ = fmt.Sscanf(strings.TrimPrefix(r.URL.Path, "/hop/"), "%d", &n)
			if n <= 0 {
				_, _ = w.Write([]byte("OK"))
				return
			}
			http.Redirect(w, r, fmt.Sprintf("/hop/%d", n-1), http.StatusFound)
		default:
			http.Redirect(w, r, f.target.Load().(string), http.StatusFound)
		}
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	f.port = u.Port()
	f.start = "http://pub.allowed.test:" + f.port
	cfg := &netguard.Config{
		Resolver: mapResolver{
			"pub.allowed.test":  {ip("127.0.0.1")},
			"priv.allowed.test": {ip("10.0.0.5")},
		},
		IPAllowed:    onlyLoopback,
		MaxRedirects: maxRedirects,
	}
	f.client = newClient(cfg, []string{"*.allowed.test"})
	return f
}

func TestClient_重定向逐跳校验(t *testing.T) {
	f := newRedirectFixture(t, 0)

	t.Run("同域名内的重定向放行", func(t *testing.T) {
		f.target.Store(f.start + "/ok")
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, f.start+"/go", nil)
		resp, err := f.client.Do(req)
		if err != nil {
			t.Fatalf("应放行：%v", err)
		}
		defer resp.Body.Close()
		if b, _ := io.ReadAll(resp.Body); string(b) != "OK" {
			t.Fatalf("内容 = %q", b)
		}
	})

	tests := []struct {
		name   string
		target string
		want   error
	}{
		{"重定向到白名单外的域名", "http://other.example.com/x", netguard.ErrHostNotAllowed},
		{"重定向到 127.0.0.1 字面量", "http://127.0.0.1:" + f.port + "/ok", netguard.ErrHostNotAllowed},
		{"重定向到内网 IP 字面量", "http://10.0.0.5/x", netguard.ErrHostNotAllowed},
		{"重定向到元数据地址", "http://169.254.169.254/latest/meta-data/", netguard.ErrHostNotAllowed},
		{"重定向到白名单域名但解析到内网", "http://priv.allowed.test:" + f.port + "/ok", netguard.ErrBlockedAddress},
		{"重定向到非 http 协议", "file:///etc/passwd", netguard.ErrHostNotAllowed},
		{"重定向到带用户名密码的 URL", "http://user:pass@pub.allowed.test:" + f.port + "/ok", netguard.ErrHostNotAllowed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f.target.Store(tt.target)
			_, err := get(f.client, f.start+"/go")
			if !errors.Is(err, tt.want) || !netguard.IsGuardError(err) {
				t.Fatalf("期望 %v，实际 %v", tt.want, err)
			}
		})
	}

	t.Run("重定向次数过多", func(t *testing.T) {
		_, err := get(f.client, f.start+"/loop")
		if !errors.Is(err, netguard.ErrTooManyRedirects) || !netguard.IsGuardError(err) {
			t.Fatalf("应因重定向过多拒绝：%v", err)
		}
	})
}

func TestNewClient_重定向次数上限(t *testing.T) {
	f := newRedirectFixture(t, 3) // 默认值 3：最多跟随 3 次
	if _, err := get(f.client, f.start+"/hop/3"); err != nil {
		t.Fatalf("恰好 3 次重定向应放行：%v", err)
	}
	if _, err := get(f.client, f.start+"/hop/4"); !errors.Is(err, netguard.ErrTooManyRedirects) {
		t.Fatalf("第 4 次重定向应拒绝：%v", err)
	}

	f2 := newRedirectFixture(t, 1)
	if _, err := get(f2.client, f2.start+"/hop/1"); err != nil {
		t.Fatalf("上限 1 时一次重定向应放行：%v", err)
	}
	if _, err := get(f2.client, f2.start+"/hop/2"); !errors.Is(err, netguard.ErrTooManyRedirects) {
		t.Fatalf("上限 1 时两次重定向应拒绝：%v", err)
	}
}

func TestNewClient_上限为0不跟随重定向(t *testing.T) {
	f := newRedirectFixture(t, 3)
	// 复用夹具的服务器与解析，但直接调 NewClient 传 0
	cfg := &netguard.Config{
		Resolver:  mapResolver{"pub.allowed.test": {ip("127.0.0.1")}},
		IPAllowed: onlyLoopback,
	}
	cfg.ApplyDefaults()
	c := netguard.NewClient(netguard.NewTransport(cfg), []string{"*.allowed.test"}, 0)
	if _, err := get(c, f.start+"/hop/1"); !errors.Is(err, netguard.ErrTooManyRedirects) {
		t.Fatalf("上限 0 应拒绝任何重定向：%v", err)
	}
	if _, err := get(c, f.start+"/hop/0"); err != nil {
		t.Fatalf("没有重定向的请求应正常：%v", err)
	}
}

// closeTracker 记录请求体是否被关闭。
type closeTracker struct {
	io.Reader
	closed atomic.Bool
}

func (c *closeTracker) Close() error { c.closed.Store(true); return nil }

func TestClient_拒绝时关闭请求体(t *testing.T) {
	body := &closeTracker{Reader: strings.NewReader("payload")}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://evil.example.com/x", body)
	c := newClient(&netguard.Config{}, []string{"www.runninghub.cn"})
	resp, err := c.Do(req)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("应被拒绝")
	}
	if !body.closed.Load() {
		t.Fatal("被白名单拒绝时应关闭请求体，避免泄漏")
	}
}

func TestTransport_不走环境代理(t *testing.T) {
	srv, _ := countingServer(t)
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("http_proxy", "http://127.0.0.1:1")
	c := newClient(&netguard.Config{}, []string{"127.0.0.1"})
	_, err := get(c, srv.URL)
	// 若走了代理会得到代理连接错误；不走代理时才是我们的 IP 拦截
	if !errors.Is(err, netguard.ErrBlockedAddress) {
		t.Fatalf("应由防护层拒绝而不是走代理：%v", err)
	}
	if tr := netguard.NewTransport(&netguard.Config{}); tr.Proxy != nil {
		t.Fatal("传输层不应设置代理")
	}
}

func TestCheckURLAllowed(t *testing.T) {
	allowed := []string{"www.runninghub.cn", "*.runninghub.ai"}
	tests := []struct {
		scheme, host string
		userinfo     bool
		wantErr      bool
	}{
		{"https", "www.runninghub.cn", false, false},
		{"http", "a.runninghub.ai", false, false},
		{"https", "WWW.RunningHub.CN", false, false},
		{"https", "www.runninghub.cn.", false, false},
		{"https", "runninghub.ai", false, true},
		{"https", "evil.com", false, true},
		{"https", "www.runninghub.cn.evil.com", false, true},
		{"https", "", false, true},
		{"ftp", "www.runninghub.cn", false, true},
		{"file", "", false, true},
		{"", "www.runninghub.cn", false, true},
		{"https", "www.runninghub.cn", true, true},
	}
	for _, tt := range tests {
		err := netguard.CheckURLAllowed(allowed, tt.scheme, tt.host, tt.userinfo)
		if (err != nil) != tt.wantErr {
			t.Errorf("%+v: err=%v", tt, err)
		}
		if err != nil && !errors.Is(err, netguard.ErrHostNotAllowed) {
			t.Errorf("%+v: 应是 ErrHostNotAllowed：%v", tt, err)
		}
	}
	if err := netguard.CheckURLAllowed(nil, "https", "a.com", false); err == nil {
		t.Error("空白名单应拒绝一切")
	}
}

func TestIsGuardError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"ErrBlockedAddress", netguard.ErrBlockedAddress, true},
		{"ErrHostNotAllowed", netguard.ErrHostNotAllowed, true},
		{"ErrTooManyRedirects", netguard.ErrTooManyRedirects, true},
		{"被包装的防护错误", fmt.Errorf("外层：%w", netguard.ErrBlockedAddress), true},
		{"url.Error 里的防护错误", &url.Error{Op: "Get", URL: "http://x", Err: netguard.ErrHostNotAllowed}, true},
		{"响应过大不是防护错误", netguard.ErrResponseTooLarge, false},
		{"普通错误", errors.New("连接被拒绝"), false},
		{"超时", context.DeadlineExceeded, false},
		{"nil", nil, false},
	}
	for _, tt := range tests {
		if got := netguard.IsGuardError(tt.err); got != tt.want {
			t.Errorf("%s: IsGuardError = %v，期望 %v", tt.name, got, tt.want)
		}
	}
}

func TestConfig_ApplyDefaults(t *testing.T) {
	t.Run("全部取默认值", func(t *testing.T) {
		var cfg netguard.Config
		cfg.ApplyDefaults()
		if cfg.Resolver == nil || cfg.Dial == nil || cfg.MaxRedirects != 3 {
			t.Fatalf("默认值不全：%+v", cfg)
		}
		if cfg.IPAllowed(ip("127.0.0.1")) || cfg.IPAllowed(ip("10.0.0.1")) || !cfg.IPAllowed(ip("8.8.8.8")) {
			t.Fatal("默认 IPAllowed 应拒绝内网、放行公网")
		}
	})
	t.Run("负数上限取默认值", func(t *testing.T) {
		cfg := netguard.Config{MaxRedirects: -1}
		cfg.ApplyDefaults()
		if cfg.MaxRedirects != 3 {
			t.Fatalf("MaxRedirects = %d", cfg.MaxRedirects)
		}
	})
	t.Run("不覆盖已设置的值", func(t *testing.T) {
		res := mapResolver{}
		cfg := netguard.Config{
			IPAllowed:    func(net.IP) bool { return true },
			Resolver:     res,
			MaxRedirects: 7,
		}
		cfg.ApplyDefaults()
		if !cfg.IPAllowed(ip("127.0.0.1")) || cfg.MaxRedirects != 7 {
			t.Fatalf("自定义值被覆盖：%+v", cfg)
		}
		if _, ok := cfg.Resolver.(mapResolver); !ok {
			t.Fatal("自定义 Resolver 被覆盖")
		}
	})
}
