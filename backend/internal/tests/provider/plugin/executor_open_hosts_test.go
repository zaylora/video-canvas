package plugin_test

// 本文件：去掉 allowedHosts 白名单后的宿主行为（设计文档 docs/design/插件简化/插件简化.md §6.1）：
//   - url 产物与结果下载不再限制域名，仍然拦截内网；
//   - 绝对地址请求可以发往任意公网域名，但只有和渠道 base_url 同源（协议、域名、端口一致）才注入 Key；
//   - 旧插件 meta 里残留 allowedHosts 不影响运行。

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

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/provider/netguard"
	"video-canvas/internal/provider/plugin"
	"video-canvas/internal/provider/pluginmeta"
)

// 把任意域名都解析到本机回环，让“第三方 CDN”域名能连到 httptest 服务；其余地址仍按默认规则拦截内网。
type fakeDNS map[string]net.IP

func (f fakeDNS) LookupIP(_ context.Context, host string) ([]net.IP, error) {
	if ip, ok := f[host]; ok {
		return []net.IP{ip}, nil
	}
	return nil, errors.New("no such host: " + host)
}

// publicLoopback 只放行 127.0.0.1，模拟“这个地址在公网上”；10.x 等其他内网地址继续被拒。
func publicLoopback(ip net.IP) bool { return ip.Equal(net.ParseIP("127.0.0.1")) }

// openHostsExec 建一个宿主：cdn.third-party.test 解析到回环，回环算“公网”，10.0.0.5 仍是内网。
func openHostsExec(code string) *plugin.Executor {
	return execWith(code, func(o *plugin.Options) {
		o.Resolver = fakeDNS{"cdn.third-party.test": net.ParseIP("127.0.0.1"), "evil.internal.test": net.ParseIP("10.0.0.5")}
		o.IPAllowed = publicLoopback
	})
}

func portOf(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u.Port()
}

// url 产物放在 meta 里没声明过的第三方域名：不再被拒绝，也能下载。
func TestOpenHostsURLOutputOnAnyPublicHost(t *testing.T) {
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("png-bytes"))
	}))
	defer cdn.Close()
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) }))
	defer gateway.Close()

	resultURL := "http://cdn.third-party.test:" + portOf(t, cdn.URL) + "/r/1.png"
	code := `module.exports = {
  buildSubmitRequest: function() { return {method: "GET", path: "/x"}; },
  parseSubmitResponse: function() {
    return {immediate: {status: "succeeded", outputs: [{type: "url", url: "` + resultURL + `", media_type: "image"}]}};
  }
};`
	exec := openHostsExec(code)
	snap := testSnapshot(gateway.URL, code, modelcfg.KindImage, pluginmeta.Auth{Type: pluginmeta.AuthNone})
	snap.Plugin.Meta.Endpoints = map[string]pluginmeta.Endpoint{modelcfg.KindImage: {Mode: pluginmeta.ModeSync}}

	res, err := submit(exec, snap)
	if err != nil {
		t.Fatalf("第三方域名的 url 产物应该通过：%v", err)
	}
	if res.Immediate == nil || len(res.Immediate.Outputs) != 1 || res.Immediate.Outputs[0].URL != resultURL {
		t.Fatalf("产物不符合预期：%+v", res.Immediate)
	}
	dl, err := exec.Download(context.Background(), snap, resultURL)
	if err != nil {
		t.Fatalf("第三方域名应能下载：%v", err)
	}
	defer dl.Body.Close()
	if body, _ := io.ReadAll(dl.Body); string(body) != "png-bytes" {
		t.Fatalf("下载内容不对：%q", body)
	}
}

// 去掉白名单不等于放开内网：产物地址解析到内网、字面量内网 IP 都要被拦截。
func TestOpenHostsStillBlocksPrivateAddresses(t *testing.T) {
	var hits int
	var mu sync.Mutex
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		_, _ = w.Write([]byte("secret"))
	}))
	defer internal.Close()
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) }))
	defer gateway.Close()

	exec := openHostsExec(syncTextPlugin)
	snap := textSnap(gateway.URL, syncTextPlugin)
	for _, target := range []string{
		"http://evil.internal.test:" + portOf(t, internal.URL) + "/a.png", // 域名解析到 10.0.0.5
		"http://169.254.169.254/latest/meta-data/",                        // 云元数据地址
		"http://192.168.1.10:3000/v1/models",
	} {
		dl, err := exec.Download(context.Background(), snap, target)
		if err == nil {
			dl.Body.Close()
			t.Fatalf("%s 应被拦截", target)
		}
		if !errors.Is(err, netguard.ErrBlockedAddress) {
			t.Fatalf("%s 应因内网地址被拦截，实际：%v", target, err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if hits != 0 {
		t.Fatalf("被拦截的请求不应到达目标，实际 %d 次", hits)
	}
}

// 绝对地址请求：同源带 Key，非同源（别的域名、同域名不同端口）不带 Key，请求本身照常发出。
func TestOpenHostsAbsoluteURLInjectsKeyOnlyForSameOrigin(t *testing.T) {
	var mu sync.Mutex
	got := map[string]string{} // 路径 -> 收到的 Authorization
	gotQuery := map[string]string{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got[r.URL.Path] = r.Header.Get("Authorization")
		gotQuery[r.URL.Path] = r.URL.Query().Get("key")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	gateway := httptest.NewServer(handler) // base_url
	defer gateway.Close()
	other := httptest.NewServer(handler) // 另一个端口，也就是另一个源
	defer other.Close()
	cdn := httptest.NewServer(handler) // 第三方域名
	defer cdn.Close()

	sameOrigin := gateway.URL + "/same"
	otherPort := "http://127.0.0.1:" + portOf(t, other.URL) + "/otherport"
	otherHost := "http://cdn.third-party.test:" + portOf(t, cdn.URL) + "/otherhost"

	for _, tc := range []struct {
		name, target, path string
		wantKey            bool
	}{
		{"同源绝对地址", sameOrigin, "/same", true},
		{"同域名不同端口", otherPort, "/otherport", false},
		{"别的域名", otherHost, "/otherhost", false},
	} {
		for _, auth := range []string{"bearer", "query"} {
			t.Run(tc.name+"/"+auth, func(t *testing.T) {
				authDesc := `auth: {type: "` + auth + `", name: "key"}`
				code := `module.exports = {
  buildSubmitRequest: function() { return {method: "GET", url: "` + tc.target + `", ` + authDesc + `}; },
  parseSubmitResponse: function() { return {immediate: {status: "succeeded", outputs: [{type: "text", text: "ok"}]}}; }
};`
				mu.Lock()
				delete(got, tc.path)
				delete(gotQuery, tc.path)
				mu.Unlock()

				exec := execWith(code, func(o *plugin.Options) {
					o.Resolver = fakeDNS{"cdn.third-party.test": net.ParseIP("127.0.0.1")}
					o.IPAllowed = publicLoopback
					o.Secrets = secretResolver{value: "SECRET-KEY"}
				})
				snap := testSnapshot(gateway.URL, code, modelcfg.KindText, pluginmeta.Auth{Type: pluginmeta.AuthBearer})
				if _, err := submit(exec, snap); err != nil {
					t.Fatalf("请求应能发出：%v", err)
				}
				mu.Lock()
				defer mu.Unlock()
				hasHeader := strings.Contains(got[tc.path], "SECRET-KEY")
				hasQuery := gotQuery[tc.path] == "SECRET-KEY"
				if tc.wantKey && !hasHeader && !hasQuery {
					t.Fatalf("同源请求应注入 Key：header=%q query=%q", got[tc.path], gotQuery[tc.path])
				}
				if !tc.wantKey && (hasHeader || hasQuery) {
					t.Fatalf("非同源请求不能带 Key：header=%q query=%q", got[tc.path], gotQuery[tc.path])
				}
			})
		}
	}
}

// 非同源请求跳过注入时，在追踪里留一条说明，方便定位“上游 401 是因为没带 Key”。
func TestOpenHostsTraceNotesSkippedKey(t *testing.T) {
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) }))
	defer gateway.Close()
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) }))
	defer cdn.Close()

	target := "http://cdn.third-party.test:" + portOf(t, cdn.URL) + "/upload"
	code := `module.exports = {
  buildSubmitRequest: function() { return {method: "GET", url: "` + target + `"}; },
  parseSubmitResponse: function() { return {immediate: {status: "succeeded", outputs: [{type: "text", text: "ok"}]}}; }
};`
	exec := execWith(code, func(o *plugin.Options) {
		o.Resolver = fakeDNS{"cdn.third-party.test": net.ParseIP("127.0.0.1")}
		o.IPAllowed = publicLoopback
	})
	snap := testSnapshot(gateway.URL, code, modelcfg.KindText, pluginmeta.Auth{Type: pluginmeta.AuthBearer})
	ctx, trace := provider.WithTrace(context.Background())
	if _, err := exec.Submit(ctx, snap, provider.SubmitInput{Task: provider.TaskRef{ID: 1, UserID: 9}, Input: map[string]any{"prompt": "hi"}}); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, s := range trace.Steps() {
		if s.Kind == "http" && s.Request != nil && strings.Contains(s.Request.Body, "未注入 Key") {
			found = true
			if !strings.Contains(s.Request.URL, "/upload") {
				t.Fatalf("追踪步骤应带请求地址：%+v", s.Request)
			}
			if s.Error != "" {
				t.Fatalf("这不是失败，不能写 Error（前端会标红）：%q", s.Error)
			}
		}
	}
	if !found {
		t.Fatalf("应有一条 http 步骤说明“未注入 Key”：%+v", trace.Steps())
	}
}

// 旧插件 meta 里残留 allowedHosts（快照来自老数据）：结构体里已经没有这个字段，反序列化直接忽略。
func TestOpenHostsLegacyAllowedHostsIgnored(t *testing.T) {
	var m pluginmeta.Meta
	raw := `{"apiVersion":1,"key":"old","name":"旧插件","version":"1.0.0","auth":{"type":"bearer"},
	  "allowedHosts":["cdn.example.com","*.deepwl.cn"],"endpoints":{"image":{"mode":"async"}}}`
	got, err := pluginmeta.Parse([]byte(raw))
	if err != nil {
		t.Fatalf("旧 meta 应能解析：%v", err)
	}
	m = *got
	if m.Key != "old" || m.Endpoints["image"].Mode != pluginmeta.ModeAsync {
		t.Fatalf("其余字段应照常解析：%+v", m)
	}
}

// 开发机的代理工具开着 Fake-IP 模式：域名被解析到 198.18.0.0/15。默认策略拒绝；
// 宿主选项配成 netguard.IPAllowedWithFakeIP 后放行，但解析到真正内网（10.x）的域名仍被拦。
func TestFakeIPPolicyOnExecutor(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) }))
	defer upstream.Close()
	port := portOf(t, upstream.URL)
	dns := fakeDNS{"api.fake.test": net.ParseIP("198.18.0.35"), "lan.fake.test": net.ParseIP("10.0.0.5")}
	// 把连接改道到本机的 httptest 服务，只验证 IP 策略（真实拨号会去 198.18.0.35）
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, "127.0.0.1:"+port)
	}
	mk := func(allowed func(net.IP) bool) *plugin.Executor {
		return execWith(syncTextPlugin, func(o *plugin.Options) { o.Resolver, o.IPAllowed, o.Dial = dns, allowed, dial })
	}
	snap := func(host string) *provider.Snapshot {
		return syncSnapshot("http://"+host+":"+port, syncTextPlugin, "ch-fake", provider.RateLimit{})
	}
	in := provider.SubmitInput{Task: provider.TaskRef{ID: 1}, Input: map[string]any{"prompt": "hi"}}

	if _, err := mk(nil).Submit(context.Background(), snap("api.fake.test"), in); !errors.Is(err, netguard.ErrBlockedAddress) {
		t.Fatalf("默认策略应拦截 198.18.x：%v", err)
	}
	if _, err := mk(netguard.IPAllowedWithFakeIP).Submit(context.Background(), snap("api.fake.test"), in); err != nil {
		t.Fatalf("放行 Fake-IP 后应能连上：%v", err)
	}
	if _, err := mk(netguard.IPAllowedWithFakeIP).Submit(context.Background(), snap("lan.fake.test"), in); !errors.Is(err, netguard.ErrBlockedAddress) {
		t.Fatalf("放行 Fake-IP 不等于放行内网，10.x 仍应被拦：%v", err)
	}
}
