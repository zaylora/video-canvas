// Package webaccess 是 Agent 联网工具的出站实现：抓取网页并抽成正文、在多个搜索源之间依次尝试。
// 联网的风险都在这里挡：目标地址是本机或内网时拒绝（解析后校验并直接拨号，每次跳转都重新校验，见 provider/netguard），
// 响应体、正文字数、耗时和并发都有上限。搜索和抓回来的内容只是资料，由调用方标注为「不可信」。
package webaccess

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"
	"golang.org/x/net/html/charset"

	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/netguard"
)

// 默认值。
const (
	DefaultTimeout       = 15 * time.Second
	DefaultMaxBodyBytes  = 2 << 20 // 响应体最多读 2MB，超出的部分丢掉并标明被截断
	DefaultMaxRedirects  = 3
	DefaultMaxConcurrent = 8
	// MaxChars 是返回给模型的正文字数上限，也是 maxChars 参数的上限。
	MaxChars  = 20000
	maxSearch = 10
)

// Config 是联网能力的参数。零值可用。
type Config struct {
	SearXNGURL    string                                                            // 自己部署的 SearXNG 地址，为空不用；由管理员配置，不来自模型
	ExaURL        string                                                            // 默认 DefaultExaURL，测试里换成本地服务
	BingURL       string                                                            // 默认 DefaultBingURL，测试里换成本地服务
	DuckDuckGoURL string                                                            // 默认 DefaultDuckDuckGoURL
	IPAllowed     func(net.IP) bool                                                 // 出站 IP 策略，nil 用 netguard 的默认策略（拒绝内网、回环、链路本地）
	Resolver      netguard.Resolver                                                 // 测试里注入
	Dial          func(ctx context.Context, network, addr string) (net.Conn, error) // 测试里注入
	Timeout       time.Duration                                                     // 单次请求的超时
	MaxBodyBytes  int64
	MaxRedirects  int
	MaxConcurrent int        // 同时进行的联网请求数，满了就让模型稍后再试
	Searchers     []Searcher // 非空时替换默认的搜索源（测试用）
}

// Client 实现 provider.WebAccess。
type Client struct {
	cfg       Config
	guarded   *http.Client // 抓网页和抓搜索结果页：受 netguard 防护
	searchers []Searcher
	sem       chan struct{}
}

var _ provider.WebAccess = (*Client)(nil)

// New 创建 Client。
func New(cfg Config) *Client {
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.MaxBodyBytes <= 0 {
		cfg.MaxBodyBytes = DefaultMaxBodyBytes
	}
	if cfg.MaxRedirects <= 0 {
		cfg.MaxRedirects = DefaultMaxRedirects
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = DefaultMaxConcurrent
	}
	if cfg.ExaURL == "" {
		cfg.ExaURL = DefaultExaURL
	}
	if cfg.BingURL == "" {
		cfg.BingURL = DefaultBingURL
	}
	if cfg.DuckDuckGoURL == "" {
		cfg.DuckDuckGoURL = DefaultDuckDuckGoURL
	}
	ng := &netguard.Config{IPAllowed: cfg.IPAllowed, Resolver: cfg.Resolver, Dial: cfg.Dial, MaxRedirects: cfg.MaxRedirects}
	ng.ApplyDefaults()
	c := &Client{cfg: cfg, guarded: netguard.NewClient(netguard.NewTransport(ng), nil, ng.MaxRedirects), sem: make(chan struct{}, cfg.MaxConcurrent)}
	c.searchers = cfg.Searchers
	if len(c.searchers) == 0 {
		if cfg.SearXNGURL != "" {
			// SearXNG 在内网，地址是私网 IP，过不了 netguard；它的地址只来自管理员配置，所以用不拦内网的普通客户端
			c.searchers = append(c.searchers, NewSearXNG(cfg.SearXNGURL, &http.Client{Transport: &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext}}))
		}
		// Exa 的结果最切题排前面；Bing、DuckDuckGo 是抓网页的兜底（对自动化请求常给降级的结果，或直接要求人机验证）
		c.searchers = append(c.searchers, NewExa(cfg.ExaURL, c.guarded), NewBing(cfg.BingURL, c.guarded), NewDuckDuckGo(cfg.DuckDuckGoURL, c.guarded))
	}
	return c
}

// SearchSources 返回搜索源的名字，按尝试的顺序。
func (c *Client) SearchSources() []string {
	out := make([]string, len(c.searchers))
	for i, s := range c.searchers {
		out[i] = s.Name()
	}
	return out
}

var errBusy = &provider.WebError{Message: "当前联网请求较多，请稍后再试。"}

// acquire 占一个并发名额，满了立刻返回 false，不排队：排队会让一次运行卡在联网上。
func (c *Client) acquire() (release func(), ok bool) {
	select {
	case c.sem <- struct{}{}:
		return func() { <-c.sem }, true
	default:
		return nil, false
	}
}

// Search 依次尝试各搜索源：失败或没有结果就换下一个，有结果就停。都失败返回 *provider.WebError；
// 都没有结果（至少一个正常返回）是空结果，不是错误。
func (c *Client) Search(ctx context.Context, query string, limit int) (*provider.WebSearchResponse, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, &provider.WebError{Message: "搜索关键词不能为空。"}
	}
	if limit <= 0 {
		limit = 5
	}
	limit = min(limit, maxSearch)
	release, ok := c.acquire()
	if !ok {
		return nil, errBusy
	}
	defer release()

	answered := ""
	for _, s := range c.searchers {
		sctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
		res, err := s.Search(sctx, query, limit)
		cancel()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err != nil {
			logger.Warn("联网搜索源失败，换下一个", zap.String("source", s.Name()), zap.Error(err))
			continue
		}
		if len(res) == 0 {
			answered = s.Name() // 没结果也可能是上游引擎被限流了，再试下一个
			continue
		}
		return &provider.WebSearchResponse{Provider: s.Name(), Results: res[:min(len(res), limit)]}, nil
	}
	if answered != "" {
		return &provider.WebSearchResponse{Provider: answered}, nil
	}
	return nil, &provider.WebError{Message: "搜索服务暂时不可用，请稍后再试，或换个说法重试。"}
}

// Fetch 抓取网页并抽取正文。
func (c *Client) Fetch(ctx context.Context, rawURL string, maxChars int) (*provider.WebPage, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return nil, &provider.WebError{Message: "网址不合法：只支持 http 和 https 网页，且不能带用户名密码。"}
	}
	if maxChars <= 0 || maxChars > MaxChars {
		maxChars = MaxChars
	}
	release, ok := c.acquire()
	if !ok {
		return nil, errBusy
	}
	defer release()

	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, &provider.WebError{Message: "网址不合法。"}
	}
	req.Header.Set("User-Agent", browserUA)
	req.Header.Set("Accept", acceptPages)
	req.Header.Set("Accept-Language", acceptLang)
	resp, err := c.guarded.Do(req)
	if err != nil {
		return nil, c.fetchError(ctx, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &provider.WebError{Message: fmt.Sprintf("对方网站返回了 %d，没能取到内容。", resp.StatusCode)}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, c.cfg.MaxBodyBytes+1))
	if err != nil {
		return nil, c.fetchError(ctx, err)
	}
	truncated := int64(len(raw)) > c.cfg.MaxBodyBytes
	if truncated {
		raw = raw[:c.cfg.MaxBodyBytes]
	}

	ctype := resp.Header.Get("Content-Type")
	media, _, _ := mime.ParseMediaType(ctype)
	if media == "" {
		media = http.DetectContentType(raw[:min(len(raw), 512)])
		media, _, _ = mime.ParseMediaType(media)
	}
	if media != "text/html" && media != "application/xhtml+xml" && media != "text/plain" {
		return nil, &provider.WebError{Message: fmt.Sprintf("这个地址返回的内容类型是 %s，只支持网页和纯文本，不支持 PDF、图片等。", media)}
	}
	rd, err := charset.NewReader(bytes.NewReader(raw), ctype) // 按响应头或页面声明的编码转成 UTF-8
	if err != nil {
		return nil, &provider.WebError{Message: "页面的编码无法识别。"}
	}
	final := resp.Request.URL
	var title, text string
	if media == "text/plain" {
		b, _ := io.ReadAll(rd)
		text = strings.TrimSpace(string(b))
	} else if title, text, err = ExtractPage(rd, final); err != nil {
		return nil, &provider.WebError{Message: "页面解析失败。"}
	}
	if utf8.RuneCountInString(text) > maxChars {
		text = string([]rune(text)[:maxChars])
		truncated = true
	}
	return &provider.WebPage{URL: final.String(), Title: title, Text: text, Truncated: truncated}, nil
}

// fetchError 把抓取时的错误翻成给模型看的话：不放内部细节（解析到哪个 IP、底层报错）。
func (c *Client) fetchError(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, netguard.ErrTooManyRedirects):
		return &provider.WebError{Message: "这个地址重定向的次数太多。"}
	case netguard.IsGuardError(err):
		return &provider.WebError{Message: "不能访问这个地址：它指向本机或内网，联网工具只能访问公网网页。", Blocked: true}
	case errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil:
		return &provider.WebError{Message: "访问这个网址超时了。"}
	}
	logger.Warn("联网抓取失败", zap.Error(err))
	return &provider.WebError{Message: "无法打开这个网址（连接失败或域名无法解析）。"}
}
