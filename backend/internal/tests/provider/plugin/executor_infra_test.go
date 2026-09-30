package plugin_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/provider/netguard"
	"video-canvas/internal/provider/plugin"
	"video-canvas/internal/provider/pluginmeta"
	"video-canvas/internal/provider/pluginrunner"
)

// 同步文本插件：向 /run 发一个请求，把响应里的 result 当正文。
const syncTextPlugin = `
module.exports = {
  buildSubmitRequest: function(ctx) { return {method: "POST", path: "/run", json: {x: 1}}; },
  parseSubmitResponse: function(ctx, resp) {
    return {immediate: {status: "succeeded", outputs: [{type: "text", text: "ok"}]}};
  }
};`

// execWith 用自定义宿主选项建执行器，runner 是进程内的真实 runner。
func execWith(code string, mod func(*plugin.Options)) *plugin.Executor {
	opts := plugin.Options{
		Runner:  plugin.NewInProcessRunnerClient(pluginrunner.NewServer(pluginrunner.Options{}).Handler()),
		Codes:   codeStore{"code": code},
		Secrets: secretResolver{value: "k"},
	}
	if mod != nil {
		mod(&opts)
	}
	return plugin.New(opts)
}

func syncSnapshot(baseURL, code, channelKey string, rl provider.RateLimit, trusted bool) *provider.Snapshot {
	snap := testSnapshot(baseURL, code, modelcfg.KindText, pluginmeta.Auth{Type: pluginmeta.AuthNone})
	snap.Channel.Key = channelKey
	snap.Channel.RateLimit = rl
	snap.Channel.TrustedInternal = trusted
	return snap
}

// concurrencyProbe 是记录并发峰值的上游。
type concurrencyProbe struct {
	cur, max atomic.Int32
	hold     time.Duration
}

func (p *concurrencyProbe) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n := p.cur.Add(1)
	for {
		m := p.max.Load()
		if n <= m || p.max.CompareAndSwap(m, n) {
			break
		}
	}
	time.Sleep(p.hold)
	p.cur.Add(-1)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{}`))
}

func submitAll(t *testing.T, exec *plugin.Executor, snaps ...*provider.Snapshot) {
	t.Helper()
	var wg sync.WaitGroup
	for i, s := range snaps {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := exec.Submit(context.Background(), s, provider.SubmitInput{Task: provider.TaskRef{ID: uint64(i + 1)}}); err != nil {
				t.Errorf("提交失败：%v", err)
			}
		}()
	}
	wg.Wait()
}

// 渠道并发上限来自快照里的 rate_limit.max_concurrency：同一渠道的请求最多同时 N 个。
func TestExecutorChannelMaxConcurrency(t *testing.T) {
	probe := &concurrencyProbe{hold: 80 * time.Millisecond}
	upstream := httptest.NewServer(probe)
	defer upstream.Close()
	exec := execWith(syncTextPlugin, nil)

	limited := syncSnapshot(upstream.URL, syncTextPlugin, "ch-limited", provider.RateLimit{MaxConcurrency: 2}, true)
	submitAll(t, exec, limited, limited, limited, limited, limited, limited)
	if got := probe.max.Load(); got != 2 {
		t.Fatalf("max_concurrency=2 时并发峰值应为 2，实际 %d", got)
	}
}

// 没有配置限流时不限并发（对照组，证明上面的限制来自 rate_limit 而不是别处）。
func TestExecutorNoRateLimitIsUnlimited(t *testing.T) {
	probe := &concurrencyProbe{hold: 80 * time.Millisecond}
	upstream := httptest.NewServer(probe)
	defer upstream.Close()
	exec := execWith(syncTextPlugin, nil)

	free := syncSnapshot(upstream.URL, syncTextPlugin, "ch-free", provider.RateLimit{}, true)
	submitAll(t, exec, free, free, free, free)
	if got := probe.max.Load(); got < 3 {
		t.Fatalf("不限流时应能并发，实际峰值 %d", got)
	}
}

// 限流以渠道 key 为单位：同一主机上的两个渠道互不占用对方的名额（原先按主机名，会互相挤占）。
func TestExecutorLimiterIsPerChannelNotPerHost(t *testing.T) {
	probe := &concurrencyProbe{hold: 150 * time.Millisecond}
	upstream := httptest.NewServer(probe)
	defer upstream.Close()
	exec := execWith(syncTextPlugin, nil)

	a := syncSnapshot(upstream.URL, syncTextPlugin, "ch-a", provider.RateLimit{MaxConcurrency: 1}, true)
	b := syncSnapshot(upstream.URL, syncTextPlugin, "ch-b", provider.RateLimit{MaxConcurrency: 1}, true)
	submitAll(t, exec, a, b)
	if got := probe.max.Load(); got != 2 {
		t.Fatalf("两个渠道各限 1 并发，同一主机上的峰值应为 2，实际 %d", got)
	}
}

// rps 令牌桶：限速渠道的请求被拉开，超过突发量的请求要排队等令牌。
func TestExecutorChannelRPS(t *testing.T) {
	upstream := httptest.NewServer(&concurrencyProbe{})
	defer upstream.Close()
	exec := execWith(syncTextPlugin, nil)
	snap := syncSnapshot(upstream.URL, syncTextPlugin, "ch-rps", provider.RateLimit{RPS: 10}, true)

	start := time.Now()
	// 突发量是 ceil(rps)=10，第 11~13 次请求各需等约 100ms
	for i := 0; i < 13; i++ {
		if _, err := exec.Submit(context.Background(), snap, provider.SubmitInput{Task: provider.TaskRef{ID: uint64(i + 1)}}); err != nil {
			t.Fatal(err)
		}
	}
	if elapsed := time.Since(start); elapsed < 200*time.Millisecond {
		t.Fatalf("rps=10 时 13 次请求应被限速拉开，实际只用了 %v", elapsed)
	}
}

// 限流等待受 ctx 约束：名额被占满时，ctx 结束就放弃等待，而不是一直挂着。
func TestExecutorLimiterWaitHonorsContext(t *testing.T) {
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()
	defer close(release)
	exec := execWith(syncTextPlugin, nil)
	snap := syncSnapshot(upstream.URL, syncTextPlugin, "ch-block", provider.RateLimit{MaxConcurrency: 1}, true)

	go func() {
		_, _ = exec.Submit(context.Background(), snap, provider.SubmitInput{Task: provider.TaskRef{ID: 1}})
	}()
	time.Sleep(100 * time.Millisecond) // 让第一个请求占住名额

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := exec.Submit(ctx, snap, provider.SubmitInput{Task: provider.TaskRef{ID: 2}})
	if err == nil || provider.ClassOf(err) != provider.ClassRetryable {
		t.Fatalf("等名额期间 ctx 超时应返回可重试错误：%v", err)
	}
}

// 连接池复用：同一执行器的连续请求共用一个 Transport，只建一条 TCP 连接（原先每次请求新建 Transport）。
func TestExecutorReusesConnections(t *testing.T) {
	var newConns atomic.Int32
	upstream := httptest.NewUnstartedServer(&concurrencyProbe{})
	upstream.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			newConns.Add(1)
		}
	}
	upstream.Start()
	defer upstream.Close()
	exec := execWith(syncTextPlugin, nil)
	snap := syncSnapshot(upstream.URL, syncTextPlugin, "ch-pool", provider.RateLimit{}, true)

	for i := 0; i < 6; i++ {
		if _, err := exec.Submit(context.Background(), snap, provider.SubmitInput{Task: provider.TaskRef{ID: uint64(i + 1)}}); err != nil {
			t.Fatal(err)
		}
	}
	if got := newConns.Load(); got != 1 {
		t.Fatalf("6 次顺序请求应复用同一条连接，实际新建 %d 条", got)
	}
}

// 缓存 Transport 后 SSRF 拨号校验仍然生效：非 trusted 渠道连回环地址被拒；
// trusted 渠道与非 trusted 渠道各有自己的 Transport，互不串味（信任不会泄漏给普通渠道）。
func TestExecutorSSRFStillEnforcedWithSharedTransport(t *testing.T) {
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close() // 监听在 127.0.0.1，属于内网地址

	exec := execWith(syncTextPlugin, nil)
	untrusted := syncSnapshot(upstream.URL, syncTextPlugin, "ch-plain", provider.RateLimit{}, false)
	trusted := syncSnapshot(upstream.URL, syncTextPlugin, "ch-trusted", provider.RateLimit{}, true)

	for i := 0; i < 2; i++ {
		before := hits.Load()
		_, err := exec.Submit(context.Background(), untrusted, provider.SubmitInput{Task: provider.TaskRef{ID: 1}})
		if !errors.Is(err, netguard.ErrBlockedAddress) {
			t.Fatalf("第 %d 次：非 trusted 渠道连内网地址应被拒：%v", i+1, err)
		}
		if hits.Load() != before {
			t.Fatal("被拒的请求不应到达上游")
		}
		if _, err := exec.Submit(context.Background(), trusted, provider.SubmitInput{Task: provider.TaskRef{ID: 2}}); err != nil {
			t.Fatalf("trusted 渠道应可访问内网：%v", err)
		}
	}
}

// 渠道 base_url 解析失败时，Download 返回错误而不是空指针崩溃。
func TestExecutorDownloadInvalidBaseURL(t *testing.T) {
	exec := execWith(syncTextPlugin, nil)
	for _, base := range []string{"http://[::1", "", "://bad"} {
		snap := syncSnapshot(base, syncTextPlugin, "ch-bad", provider.RateLimit{}, true)
		snap.Plugin.Meta.AllowedHosts = []string{"cdn.example.com"}
		dl, err := exec.Download(context.Background(), snap, "https://cdn.example.com/a.mp4")
		if err == nil || dl != nil {
			t.Fatalf("base_url=%q 应返回错误：%v", base, err)
		}
		if provider.ClassOf(err) != provider.ClassTerminal {
			t.Fatalf("非法 base_url 应是 terminal：%v", err)
		}
	}
}

// 非法的产物地址（url.Parse 失败）同样返回错误。
func TestExecutorDownloadInvalidURL(t *testing.T) {
	upstream := httptest.NewServer(http.NotFoundHandler())
	defer upstream.Close()
	exec := execWith(syncTextPlugin, nil)
	snap := syncSnapshot(upstream.URL, syncTextPlugin, "ch-dl", provider.RateLimit{}, true)
	for _, raw := range []string{"http://[::1", "%zz", "/relative", ""} {
		if _, err := exec.Download(context.Background(), snap, raw); err == nil {
			t.Fatalf("产物地址 %q 应被拒绝", raw)
		}
	}
}
