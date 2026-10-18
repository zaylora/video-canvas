package webaccess_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/webaccess"
)

func allowAll(net.IP) bool { return true }

// fetchClient 的出站策略放行本机：测试服务器在 127.0.0.1 上。安全用例不用它，用默认策略。
func fetchClient(cfg webaccess.Config) *webaccess.Client {
	if cfg.IPAllowed == nil {
		cfg.IPAllowed = allowAll
	}
	return webaccess.New(cfg)
}

func htmlHandler(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(body))
	}
}

func wantWebError(t *testing.T, err error, blocked bool) *provider.WebError {
	t.Helper()
	var we *provider.WebError
	if !errors.As(err, &we) {
		t.Fatalf("应返回 *provider.WebError（给模型看的说明），实际 %T: %v", err, err)
	}
	if we.Message == "" {
		t.Error("说明不能为空")
	}
	if we.Blocked != blocked {
		t.Errorf("Blocked=%v，应为 %v（%s）", we.Blocked, blocked, we.Message)
	}
	return we
}

func TestFetch_ExtractsReadableText(t *testing.T) {
	s := serve(t, htmlHandler(articleHTML))
	page, err := fetchClient(webaccess.Config{}).Fetch(context.Background(), s.URL+"/posts/1", 20000)
	if err != nil {
		t.Fatal(err)
	}
	if page.Title != "港口的清晨 - 示例站" || !strings.Contains(page.Text, "# 港口的清晨") || strings.Contains(page.Text, "不该出现") {
		t.Errorf("page=%+v", page)
	}
	if page.URL != s.URL+"/posts/1" || page.Truncated {
		t.Errorf("URL 应是最终地址，没截断: %+v", page)
	}
}

func TestFetch_TruncatesToMaxChars(t *testing.T) {
	s := serve(t, htmlHandler("<body><p>"+strings.Repeat("字", 500)+"</p></body>"))
	page, err := fetchClient(webaccess.Config{}).Fetch(context.Background(), s.URL, 100)
	if err != nil {
		t.Fatal(err)
	}
	if n := utf8.RuneCountInString(page.Text); n != 100 || !page.Truncated {
		t.Errorf("按字数截断到 100，实际 %d truncated=%v", n, page.Truncated)
	}
}

func TestFetch_PlainTextAndGBK(t *testing.T) {
	plain := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("纯文本内容"))
	})
	p, err := fetchClient(webaccess.Config{}).Fetch(context.Background(), plain.URL, 1000)
	if err != nil || !strings.Contains(p.Text, "纯文本内容") {
		t.Errorf("纯文本: %+v %v", p, err)
	}
	gbk := serve(t, func(w http.ResponseWriter, r *http.Request) {
		enc, _ := simplifiedchinese.GBK.NewEncoder().String("<html><head><title>老站点</title></head><body><p>这是 GBK 编码的页面</p></body></html>")
		w.Header().Set("Content-Type", "text/html; charset=gbk")
		_, _ = w.Write([]byte(enc))
	})
	p, err = fetchClient(webaccess.Config{}).Fetch(context.Background(), gbk.URL, 1000)
	if err != nil || p.Title != "老站点" || !strings.Contains(p.Text, "这是 GBK 编码的页面") {
		t.Errorf("要按页面声明的编码解码: %+v %v", p, err)
	}
}

func TestFetch_UnsupportedContentAndHTTPErrors(t *testing.T) {
	pdf := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte("%PDF-1.4"))
	})
	_, err := fetchClient(webaccess.Config{}).Fetch(context.Background(), pdf.URL, 1000)
	if we := wantWebError(t, err, false); !strings.Contains(we.Message, "application/pdf") {
		t.Errorf("要告诉模型是什么类型不支持: %q", we.Message)
	}
	nf := serve(t, func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	_, err = fetchClient(webaccess.Config{}).Fetch(context.Background(), nf.URL, 1000)
	if we := wantWebError(t, err, false); !strings.Contains(we.Message, "404") {
		t.Errorf("要带上状态码: %q", we.Message)
	}
}

func TestFetch_BodyCapAndTimeout(t *testing.T) {
	big := serve(t, htmlHandler("<body><p>"+strings.Repeat("a", 50000)+"</p></body>"))
	page, err := fetchClient(webaccess.Config{MaxBodyBytes: 2000}).Fetch(context.Background(), big.URL, 100000)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Text) > 2100 || !page.Truncated {
		t.Errorf("响应体只读到上限，并标明被截断: len=%d truncated=%v", len(page.Text), page.Truncated)
	}
	slow := serve(t, func(w http.ResponseWriter, r *http.Request) { time.Sleep(600 * time.Millisecond) })
	start := time.Now()
	_, err = fetchClient(webaccess.Config{Timeout: 100 * time.Millisecond}).Fetch(context.Background(), slow.URL, 1000)
	_ = wantWebError(t, err, false)
	if time.Since(start) > 2*time.Second {
		t.Error("超时没有生效")
	}
}

func TestFetch_RejectsBadURLs(t *testing.T) {
	c := fetchClient(webaccess.Config{})
	for _, u := range []string{"", "not a url", "file:///etc/passwd", "ftp://example.com/x", "gopher://example.com", "http://user:pw@example.com/", "http:///nohost"} {
		if _, err := c.Fetch(context.Background(), u, 1000); err == nil {
			t.Errorf("%q 应被拒绝", u)
		} else {
			_ = wantWebError(t, err, false)
		}
	}
}

// ---- 安全：默认策略，拦本机和内网 ----

func TestFetch_BlocksLocalAndPrivateAddresses(t *testing.T) {
	s := serve(t, htmlHandler("<body>secret</body>"))
	port := s.Listener.Addr().(*net.TCPAddr).Port
	c := webaccess.New(webaccess.Config{}) // 默认策略
	for _, u := range []string{
		s.URL, "http://localhost:" + itoa(port) + "/", "http://127.1:" + itoa(port) + "/",
		"http://169.254.169.254/latest/meta-data/", "http://10.0.0.5/", "http://192.168.1.1/", "http://172.16.0.1/", "http://[::1]/", "http://0.0.0.0/",
		"http://[::ffff:127.0.0.1]/",
		// 非标准写法的 IPv4：纯 Go 解析器不认，系统解析器认，必须由守卫自己还原后拦下，不能指望解析失败
		"http://2130706433/", "http://0x7f.0.0.1/", "http://0177.0.0.1/", "http://0x7f000001/", "http://10.1/", "http://169.254.43518/",
	} {
		_, err := c.Fetch(context.Background(), u, 1000)
		if err == nil {
			t.Errorf("%s 应被拦下", u)
			continue
		}
		_ = wantWebError(t, err, true)
	}
}

type mapResolver map[string][]net.IP

func (m mapResolver) LookupIP(_ context.Context, host string) ([]net.IP, error) {
	if ips, ok := m[host]; ok {
		return ips, nil
	}
	return nil, errors.New("no such host")
}

func TestFetch_BlocksHostnameResolvingToPrivate(t *testing.T) {
	c := webaccess.New(webaccess.Config{Resolver: mapResolver{
		"evil.test":  {net.ParseIP("10.0.0.5")},
		"mixed.test": {net.ParseIP("93.184.216.34"), net.ParseIP("127.0.0.1")}, // 同时返回公网和内网：整体拒绝
	}})
	for _, u := range []string{"http://evil.test/", "http://mixed.test/"} {
		_, err := c.Fetch(context.Background(), u, 1000)
		_ = wantWebError(t, err, true)
	}
}

func TestFetch_BlocksRedirectToPrivate(t *testing.T) {
	// 公网域名（解析到公网地址，连接被导到测试服务器）跳转到本机：每一跳都要重新校验
	s := serve(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.1:1/secret", http.StatusFound)
	})
	c := webaccess.New(webaccess.Config{
		Resolver: mapResolver{"pub.test": {net.ParseIP("93.184.216.34")}},
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, s.Listener.Addr().String())
		},
	})
	_, err := c.Fetch(context.Background(), "http://pub.test/start", 1000)
	_ = wantWebError(t, err, true)
}

func TestFetch_TooManyRedirects(t *testing.T) {
	s := serve(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, r.URL.Path+"x", http.StatusFound) })
	_, err := fetchClient(webaccess.Config{MaxRedirects: 3}).Fetch(context.Background(), s.URL+"/a", 1000)
	_ = wantWebError(t, err, false)
}

func TestFetch_BusyWhenTooManyConcurrent(t *testing.T) {
	release := make(chan struct{})
	s := serve(t, func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
	})
	c := fetchClient(webaccess.Config{MaxConcurrent: 1})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); _, _ = c.Fetch(context.Background(), s.URL, 100) }()
	time.Sleep(100 * time.Millisecond)
	_, err := c.Fetch(context.Background(), s.URL, 100)
	if we := wantWebError(t, err, false); !strings.Contains(we.Message, "稍后") {
		t.Errorf("满了应让模型稍后再试: %q", we.Message)
	}
	close(release)
	wg.Wait()
}

func itoa(n int) string { return strconv.Itoa(n) }
