//go:build legacy

package engine_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
	. "video-canvas/internal/provider/engine"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/dsl"
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
		{"::1", true}, {"::", true},
		{"fc00::1", true}, {"fd12:3456::1", true},
		{"fe80::1", true},
		{"::ffff:127.0.0.1", true}, {"::ffff:10.0.0.1", true}, {"::ffff:8.8.8.8", false},
		{"64:ff9b::7f00:1", true}, {"64:ff9b::808:808", true}, // NAT64 前缀本身被禁
		{"2002:7f00:1::", true}, {"2002:0a00:1::", true}, // 6to4 内嵌 127.0.0.1 / 10.0.0.1
		{"8.8.8.8", false}, {"93.184.216.34", false}, {"1.1.1.1", false},
		{"2606:4700:4700::1111", false},
	}
	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			if got := IsBlockedIP(net.ParseIP(tt.ip)); got != tt.want {
				t.Fatalf("IsBlockedIP(%s) = %v，期望 %v", tt.ip, got, tt.want)
			}
		})
	}
	if !IsBlockedIP(nil) {
		t.Fatal("nil 应被拒绝")
	}
}

// engProdEngine 创建“生产配置”的引擎：不放行任何内网地址。
func engProdEngine(mut func(*Options)) provider.Executor {
	o := Options{Secrets: engDefaultSecrets(), Assets: &engAssets{url: "https://s.example/a"}}
	if mut != nil {
		mut(&o)
	}
	return New(o)
}

func TestEngine_生产默认拒绝内网_Submit(t *testing.T) {
	var hits int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		engJSON(w, 200, map[string]any{"taskId": "T"})
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	port := u.Port()

	tests := []struct {
		name    string
		baseURL string
		allowed []string
	}{
		{"127.0.0.1（白名单里写了也不行）", "http://127.0.0.1:" + port, []string{"127.0.0.1"}},
		{"10.x", "http://10.1.2.3:" + port, []string{"10.1.2.3"}},
		{"169.254 元数据地址", "http://169.254.169.254", []string{"169.254.169.254"}},
		{"192.168.x", "http://192.168.0.10:8080", []string{"192.168.0.10"}},
		{"172.16.x", "http://172.16.0.1", []string{"172.16.0.1"}},
		{"0.0.0.0", "http://0.0.0.0:" + port, []string{"0.0.0.0"}},
		{"IPv6 回环", "http://[::1]:" + port, []string{"::1"}},
		{"IPv6 ULA", "http://[fd00::1]:" + port, []string{"fd00::1"}},
		{"IPv6 链路本地", "http://[fe80::1]:" + port, []string{"fe80::1"}},
		{"IPv4 映射的回环", "http://[::ffff:127.0.0.1]:" + port, []string{"::ffff:127.0.0.1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snap := engSnapshot(t, "http://example.invalid")
			snap.Provider.BaseURL = tt.baseURL
			snap.Provider.AllowedHosts = tt.allowed
			snap.Provider.Operations.Upload = nil
			ex := engProdEngine(nil)
			start := time.Now()
			_, err := ex.Submit(context.Background(), snap, engSubmitInput())
			if engClass(t, err) != provider.ClassTerminal || !strings.Contains(err.Error(), "安全策略") {
				t.Fatalf("应被安全策略拒绝为 terminal：%v", err)
			}
			if time.Since(start) > 2*time.Second {
				t.Fatal("拦截应发生在拨号之前，不应等待连接超时")
			}
		})
	}
	mu.Lock()
	defer mu.Unlock()
	if hits != 0 {
		t.Fatalf("被拦截的请求不应到达服务器，实际命中 %d 次", hits)
	}
}

func TestEngine_域名不在白名单(t *testing.T) {
	srv := engNewRH(t)
	snap := engSnapshot(t, srv.srv.URL)
	snap.Provider.Operations.Upload = nil
	snap.Provider.AllowedHosts = []string{"www.runninghub.cn"} // base_url 是 127.0.0.1，不在白名单
	ex := engNew(&engAssets{url: "https://s.example/a"}, engDefaultSecrets(), nil)

	_, err := ex.Submit(context.Background(), snap, engSubmitInput())
	if engClass(t, err) != provider.ClassTerminal || !strings.Contains(err.Error(), "白名单") {
		t.Fatalf("应因白名单拒绝：%v", err)
	}
	if srv.submitURI != "" {
		t.Fatal("请求不应到达服务器")
	}
	// 查询、取消同样受限
	_, err = ex.Query(context.Background(), snap, provider.TaskRef{ProviderTaskID: "T"})
	if engClass(t, err) != provider.ClassTerminal {
		t.Fatalf("Query 应被拒绝：%v", err)
	}
}

func TestEngine_通配符白名单(t *testing.T) {
	rh := engNewRH(t)
	u, _ := url.Parse(rh.srv.URL)
	// 把 api.runninghub.test 解析到测试服务器，白名单用通配符
	res := engMapResolver{"api.runninghub.test": {net.ParseIP("127.0.0.1")}}
	snap := engSnapshot(t, "http://api.runninghub.test:"+u.Port())
	snap.Provider.AllowedHosts = []string{"*.runninghub.test"}
	snap.Provider.Operations.Upload = nil
	ex := engNew(&engAssets{url: "https://s.example/a"}, engDefaultSecrets(), func(o *Options) { o.Resolver = res })
	if _, err := ex.Submit(context.Background(), snap, engSubmitInput()); err != nil {
		t.Fatalf("通配符白名单应放行：%v", err)
	}
	// 通配符不匹配根域名本身
	snap.Provider.AllowedHosts = []string{"*.other.test"}
	if _, err := ex.Submit(context.Background(), snap, engSubmitInput()); err == nil {
		t.Fatal("不匹配的通配符应拒绝")
	}
}

// engMapResolver 是固定映射的解析器。
type engMapResolver map[string][]net.IP

func (m engMapResolver) LookupIP(ctx context.Context, host string) ([]net.IP, error) {
	if ips, ok := m[host]; ok {
		return ips, nil
	}
	return nil, errors.New("no such host: " + host)
}

func TestEngine_DNS解析到内网被拒绝(t *testing.T) {
	// “允许 IP 的白名单域名”解析到内网：DNS rebinding 的静态形态
	res := engMapResolver{
		"evil.allowed.test":  {net.ParseIP("10.0.0.5")},
		"mixed.allowed.test": {net.ParseIP("93.184.216.34"), net.ParseIP("127.0.0.1")}, // 公网 + 内网混合应答
		"meta.allowed.test":  {net.ParseIP("169.254.169.254")},
	}
	for host := range res {
		t.Run(host, func(t *testing.T) {
			var dialed []string
			ex := engProdEngine(func(o *Options) {
				o.Resolver = res
				o.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
					dialed = append(dialed, addr)
					return nil, errors.New("不应拨号")
				}
			})
			snap := engSnapshot(t, "http://"+host)
			snap.Provider.AllowedHosts = []string{"*.allowed.test"}
			snap.Provider.Operations.Upload = nil
			_, err := ex.Submit(context.Background(), snap, engSubmitInput())
			if engClass(t, err) != provider.ClassTerminal || !strings.Contains(err.Error(), "安全策略") {
				t.Fatalf("应被拒绝：%v", err)
			}
			if len(dialed) != 0 {
				t.Fatalf("不应拨号，实际 %v", dialed)
			}
		})
	}
}

// engFlipResolver 模拟 DNS rebinding：第一次解析返回公网地址，之后返回内网地址。
type engFlipResolver struct {
	mu    sync.Mutex
	calls int
}

func (r *engFlipResolver) LookupIP(ctx context.Context, host string) ([]net.IP, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if r.calls == 1 {
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}
	return []net.IP{net.ParseIP("127.0.0.1")}, nil
}

func TestEngine_DNSRebinding直接拨校验过的IP(t *testing.T) {
	res := &engFlipResolver{}
	var mu sync.Mutex
	var dialed []string
	ex := engProdEngine(func(o *Options) {
		o.Resolver = res
		o.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			mu.Lock()
			dialed = append(dialed, addr)
			mu.Unlock()
			return nil, errors.New("模拟连接失败")
		}
	})
	snap := engSnapshot(t, "http://rebind.allowed.test")
	snap.Provider.AllowedHosts = []string{"rebind.allowed.test"}
	snap.Provider.Operations.Upload = nil
	_, err := ex.Submit(context.Background(), snap, engSubmitInput())
	if engClass(t, err) != provider.ClassRetryable {
		t.Fatalf("公网 IP 校验通过后拨号失败应为 retryable：%v", err)
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

func TestEngine_自定义解析器让白名单域名落在回环(t *testing.T) {
	// 证明拨号确实使用注入的解析结果（而不是系统 DNS）：域名根本不存在，却能连上测试服务器
	rh := engNewRH(t)
	u, _ := url.Parse(rh.srv.URL)
	ex := engNew(&engAssets{url: "https://s.example/a"}, engDefaultSecrets(), func(o *Options) {
		o.Resolver = engMapResolver{"platform.allowed.test": {net.ParseIP("127.0.0.1")}}
	})
	snap := engSnapshot(t, "http://platform.allowed.test:"+u.Port())
	snap.Provider.AllowedHosts = []string{"platform.allowed.test"}
	snap.Provider.Operations.Upload = nil
	if _, err := ex.Submit(context.Background(), snap, engSubmitInput()); err != nil {
		t.Fatalf("提交失败：%v", err)
	}
}

// engRedirectEngine 构造：白名单域名 pub.allowed.test -> 127.0.0.1（视为“公网”，只有它被 IPCheck 放行），
// priv.allowed.test -> 10.0.0.5（内网）。
func engRedirectEngine() provider.Executor {
	return engProdEngine(func(o *Options) {
		o.Resolver = engMapResolver{
			"pub.allowed.test":  {net.ParseIP("127.0.0.1")},
			"priv.allowed.test": {net.ParseIP("10.0.0.5")},
		}
		o.IPCheck = func(ip net.IP) bool { return ip.Equal(net.ParseIP("127.0.0.1")) }
	})
}

func TestEngine_重定向逐跳校验(t *testing.T) {
	var target atomicString
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			_, _ = w.Write([]byte("OK"))
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		default:
			http.Redirect(w, r, target.Get(), http.StatusFound)
		}
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	port := u.Port()
	snap := engSnapshot(t, "http://pub.allowed.test:"+port)
	snap.Provider.AllowedHosts = []string{"*.allowed.test"}
	ex := engRedirectEngine()
	start := "http://pub.allowed.test:" + port

	t.Run("同域名内的重定向放行", func(t *testing.T) {
		target.Set(start + "/ok")
		dl, err := ex.Download(context.Background(), snap, start+"/go")
		if err != nil {
			t.Fatalf("应放行：%v", err)
		}
		b, _ := io.ReadAll(dl.Body)
		dl.Body.Close()
		if string(b) != "OK" {
			t.Fatalf("内容 = %q", b)
		}
	})
	tests := []struct {
		name   string
		target string
	}{
		{"重定向到白名单外的域名", "http://other.example.com/x"},
		{"重定向到 127.0.0.1 字面量", "http://127.0.0.1:" + port + "/ok"},
		{"重定向到内网 IP 字面量", "http://10.0.0.5/x"},
		{"重定向到元数据地址", "http://169.254.169.254/latest/meta-data/"},
		{"重定向到白名单域名但解析到内网", "http://priv.allowed.test:" + port + "/ok"},
		{"重定向到非 http 协议", "file:///etc/passwd"},
		{"重定向到带用户名密码的 URL", "http://user:pass@pub.allowed.test:" + port + "/ok"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target.Set(tt.target)
			_, err := ex.Download(context.Background(), snap, start+"/go")
			if err == nil {
				t.Fatal("应被拒绝")
			}
			if engClass(t, err) != provider.ClassTerminal {
				t.Fatalf("应为 terminal：%v", err)
			}
		})
	}
	t.Run("重定向次数过多", func(t *testing.T) {
		_, err := ex.Download(context.Background(), snap, start+"/loop")
		if engClass(t, err) != provider.ClassTerminal || !strings.Contains(err.Error(), "重定向") {
			t.Fatalf("应因重定向过多拒绝：%v", err)
		}
	})
	t.Run("API 调用的重定向同样受限", func(t *testing.T) {
		target.Set("http://10.0.0.5/steal")
		snap2 := engSnapshot(t, "http://pub.allowed.test:"+port)
		snap2.Provider.AllowedHosts = []string{"*.allowed.test"}
		snap2.Provider.Operations.Upload = nil
		_, err := ex.Submit(context.Background(), snap2, engSubmitInput())
		if engClass(t, err) != provider.ClassTerminal {
			t.Fatalf("Submit 重定向到内网应为 terminal：%v", err)
		}
	})
}

type atomicString struct {
	mu sync.Mutex
	s  string
}

func (a *atomicString) Set(s string) { a.mu.Lock(); a.s = s; a.mu.Unlock() }
func (a *atomicString) Get() string  { a.mu.Lock(); defer a.mu.Unlock(); return a.s }

func TestEngine_Download安全与上限(t *testing.T) {
	rh := engNewRH(t)
	snap := engSnapshot(t, rh.srv.URL)

	t.Run("非白名单域名", func(t *testing.T) {
		ex := engNew(nil, engDefaultSecrets(), nil)
		_, err := ex.Download(context.Background(), snap, "http://evil.example.com/x.mp4")
		if engClass(t, err) != provider.ClassTerminal || !strings.Contains(err.Error(), "安全策略") {
			t.Fatalf("应被拒绝：%v", err)
		}
	})
	t.Run("非法地址", func(t *testing.T) {
		ex := engNew(nil, engDefaultSecrets(), nil)
		for _, u := range []string{"", "not a url", "/relative", "ftp://x/y", "file:///etc/passwd", "http://user:p@" + strings.TrimPrefix(rh.srv.URL, "http://") + "/x"} {
			if _, err := ex.Download(context.Background(), snap, u); err == nil {
				t.Errorf("%q 应被拒绝", u)
			}
		}
	})
	t.Run("生产默认拒绝回环下载", func(t *testing.T) {
		_, err := engProdEngine(nil).Download(context.Background(), snap, rh.srv.URL+"/files/x.mp4")
		if engClass(t, err) != provider.ClassTerminal || !strings.Contains(err.Error(), "安全策略") {
			t.Fatalf("应被拒绝：%v", err)
		}
	})
	t.Run("声明长度超限", func(t *testing.T) {
		rh.setExtra("/files/big.bin", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "2048")
			_, _ = w.Write(make([]byte, 2048))
		})
		ex := engNew(nil, engDefaultSecrets(), func(o *Options) { o.MaxDownloadBytes = 1024 })
		_, err := ex.Download(context.Background(), snap, rh.srv.URL+"/files/big.bin")
		if engClass(t, err) != provider.ClassTerminal || !errors.Is(err, ErrResponseTooLarge) {
			t.Fatalf("应因过大拒绝：%v", err)
		}
	})
	t.Run("未声明长度的流式超限", func(t *testing.T) {
		rh.setExtra("/files/stream.bin", func(w http.ResponseWriter, r *http.Request) {
			f := w.(http.Flusher)
			for i := 0; i < 8; i++ {
				_, _ = w.Write(make([]byte, 512))
				f.Flush()
			}
		})
		ex := engNew(nil, engDefaultSecrets(), func(o *Options) { o.MaxDownloadBytes = 1024 })
		dl, err := ex.Download(context.Background(), snap, rh.srv.URL+"/files/stream.bin")
		if err != nil {
			t.Fatalf("下载应能开始：%v", err)
		}
		defer dl.Body.Close()
		n, err := io.Copy(io.Discard, dl.Body)
		if !errors.Is(err, ErrResponseTooLarge) || n != 1024 {
			t.Fatalf("应读到 1024 字节后报超限，实际 n=%d err=%v", n, err)
		}
	})
	t.Run("恰好等于上限不算超限", func(t *testing.T) {
		rh.setExtra("/files/exact.bin", func(w http.ResponseWriter, r *http.Request) {
			f := w.(http.Flusher)
			_, _ = w.Write(make([]byte, 1024))
			f.Flush()
		})
		ex := engNew(nil, engDefaultSecrets(), func(o *Options) { o.MaxDownloadBytes = 1024 })
		dl, err := ex.Download(context.Background(), snap, rh.srv.URL+"/files/exact.bin")
		if err != nil {
			t.Fatal(err)
		}
		defer dl.Body.Close()
		if n, err := io.Copy(io.Discard, dl.Body); err != nil || n != 1024 {
			t.Fatalf("n=%d err=%v", n, err)
		}
	})
	t.Run("HTTP 状态码分类", func(t *testing.T) {
		ex := engNew(nil, engDefaultSecrets(), nil)
		for status, want := range map[int]provider.ErrorClass{404: provider.ClassTerminal, 403: provider.ClassTerminal, 429: provider.ClassRetryable, 502: provider.ClassRetryable} {
			status := status
			rh.setExtra("/files/s.bin", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) })
			_, err := ex.Download(context.Background(), snap, rh.srv.URL+"/files/s.bin")
			if got := engClass(t, err); got != want {
				t.Errorf("HTTP %d 分类 = %s，期望 %s", status, got, want)
			}
		}
	})
	t.Run("文件名来自 Content-Disposition", func(t *testing.T) {
		rh.setExtra("/files/dl", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Disposition", `attachment; filename="../../result.mp4"`)
			_, _ = w.Write([]byte("x"))
		})
		ex := engNew(nil, engDefaultSecrets(), nil)
		dl, err := ex.Download(context.Background(), snap, rh.srv.URL+"/files/dl")
		if err != nil {
			t.Fatal(err)
		}
		defer dl.Body.Close()
		if dl.FileName != "result.mp4" {
			t.Fatalf("文件名 = %q（应去掉目录部分）", dl.FileName)
		}
	})
}

func TestEngine_响应体超限(t *testing.T) {
	rh := engNewRH(t)
	rh.onSubmit = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"taskId":"T","pad":"` + strings.Repeat("a", 4096) + `"}`))
	}
	snap := engSnapshot(t, rh.srv.URL)
	snap.Provider.Operations.Upload = nil
	ex := engNew(&engAssets{url: "https://s.example/a"}, engDefaultSecrets(), func(o *Options) { o.MaxResponseBytes = 1024 })
	_, err := ex.Submit(context.Background(), snap, engSubmitInput())
	if engClass(t, err) != provider.ClassTerminal || !errors.Is(err, ErrResponseTooLarge) && !strings.Contains(err.Error(), "上限") {
		t.Fatalf("应因响应过大拒绝：%v", err)
	}
	// 默认上限是 5MB
	rh.onSubmit = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"taskId":"T","pad":"` + strings.Repeat("a", 6<<20) + `"}`))
	}
	ex = engNew(&engAssets{url: "https://s.example/a"}, engDefaultSecrets(), nil)
	if _, err = ex.Submit(context.Background(), snap, engSubmitInput()); engClass(t, err) != provider.ClassTerminal {
		t.Fatalf("超过默认 5MB 应拒绝：%v", err)
	}
}

func TestNewGuardedClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("hi")) }))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)

	t.Run("回环地址被拒绝（即使白名单写了）", func(t *testing.T) {
		c := NewGuardedClient([]string{"127.0.0.1"}, time.Second)
		_, err := c.Get(srv.URL)
		if !errors.Is(err, ErrBlockedAddress) {
			t.Fatalf("期望 ErrBlockedAddress，实际 %v", err)
		}
	})
	t.Run("不在白名单", func(t *testing.T) {
		c := NewGuardedClient([]string{"www.runninghub.cn"}, time.Second)
		_, err := c.Get("http://" + net.JoinHostPort("127.0.0.1", u.Port()))
		if !errors.Is(err, ErrHostNotAllowed) {
			t.Fatalf("期望 ErrHostNotAllowed，实际 %v", err)
		}
	})
	t.Run("内网 IP 与元数据地址", func(t *testing.T) {
		c := NewGuardedClient([]string{"10.0.0.1", "169.254.169.254"}, time.Second)
		for _, target := range []string{"http://10.0.0.1/", "http://169.254.169.254/latest/meta-data/"} {
			if _, err := c.Get(target); !errors.Is(err, ErrBlockedAddress) {
				t.Errorf("%s 期望 ErrBlockedAddress，实际 %v", target, err)
			}
		}
	})
	t.Run("不走环境代理", func(t *testing.T) {
		t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
		t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
		c := NewGuardedClient([]string{"127.0.0.1"}, time.Second)
		_, err := c.Get(srv.URL)
		// 若走了代理会得到代理连接错误；不走代理时才是我们的 IP 拦截
		if !errors.Is(err, ErrBlockedAddress) {
			t.Fatalf("应由防护层拒绝而不是走代理：%v", err)
		}
	})
	t.Run("超时默认值", func(t *testing.T) {
		if c := NewGuardedClient([]string{"a.example.com"}, 0); c.Timeout != 30*time.Second {
			t.Fatalf("默认超时 = %v", c.Timeout)
		}
	})
}

func TestEngine_IPCheck优先于AllowPrivate(t *testing.T) {
	rh := engNewRH(t)
	snap := engSnapshot(t, rh.srv.URL)
	snap.Provider.Operations.Upload = nil
	ex := engNew(&engAssets{url: "https://s.example/a"}, engDefaultSecrets(), func(o *Options) {
		o.IPCheck = func(net.IP) bool { return false }
	})
	_, err := ex.Submit(context.Background(), snap, engSubmitInput())
	if engClass(t, err) != provider.ClassTerminal || !strings.Contains(err.Error(), "安全策略") {
		t.Fatalf("IPCheck 应生效：%v", err)
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
		{"https", "runninghub.ai", false, true},
		{"https", "evil.com", false, true},
		{"https", "www.runninghub.cn.evil.com", false, true},
		{"ftp", "www.runninghub.cn", false, true},
		{"file", "", false, true},
		{"https", "www.runninghub.cn", true, true},
	}
	for _, tt := range tests {
		err := CheckURLAllowed(allowed, tt.scheme, tt.host, tt.userinfo)
		if (err != nil) != tt.wantErr {
			t.Errorf("%+v: err=%v", tt, err)
		}
	}
}

// 引擎的 Download 不应带任何平台凭证，即使目标域名在白名单内（前面的完整流程测试已覆盖头部）；
// 这里补充：querystring 鉴权的凭证不会出现在下载请求里。
func TestEngine_Download不携带query鉴权(t *testing.T) {
	rh := engNewRH(t)
	snap := engSnapshot(t, rh.srv.URL)
	snap.Provider.Auth = dsl.AuthConfig{Type: dsl.AuthQuery, Secret: "runninghub_api_key", Name: "key"}
	var rawQuery string
	rh.setExtra("/files/q.bin", func(w http.ResponseWriter, r *http.Request) { rawQuery = r.URL.RawQuery; _, _ = w.Write([]byte("x")) })
	ex := engNew(nil, engDefaultSecrets(), nil)
	dl, err := ex.Download(context.Background(), snap, rh.srv.URL+"/files/q.bin")
	if err != nil {
		t.Fatal(err)
	}
	dl.Body.Close()
	if rawQuery != "" {
		t.Fatalf("下载请求带了查询参数：%q", rawQuery)
	}
}
