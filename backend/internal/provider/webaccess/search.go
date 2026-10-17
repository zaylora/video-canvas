package webaccess

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"video-canvas/internal/provider"
)

// ErrSearchBlocked 表示搜索引擎把请求当成机器人拦下了（人机验证页、异常页）。
// 它和「没有结果」不同：要换下一个搜索源，而不是告诉模型没搜到。
var ErrSearchBlocked = errors.New("搜索引擎拦截了请求（人机验证）")

// Searcher 是一个搜索源。没有结果返回空列表；被拦、出错返回 error。
type Searcher interface {
	Name() string
	Search(ctx context.Context, query string, limit int) ([]provider.WebSearchResult, error)
}

const (
	maxSearchBody = 2 << 20
	// 免 key 抓搜索引擎网页要带浏览器样式的请求头，否则会直接被当成机器人
	browserUA   = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"
	acceptLang  = "zh-CN,zh;q=0.9,en;q=0.8"
	acceptPages = "text/html,application/xhtml+xml;q=0.9,*/*;q=0.8"
)

// doRequest 发请求（payload 非空时带请求体），返回状态码和（最多 maxSearchBody 字节的）响应体；响应体在这里关闭。
func doRequest(ctx context.Context, hc *http.Client, method, rawURL string, headers map[string]string, payload []byte) (status int, body []byte, err error) {
	var rd io.Reader
	if payload != nil {
		rd = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, rd)
	if err != nil {
		return 0, nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err = io.ReadAll(io.LimitReader(resp.Body, maxSearchBody))
	return resp.StatusCode, body, err
}

func withQuery(base string, kv ...string) string {
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	q := u.Query()
	for i := 0; i+1 < len(kv); i += 2 {
		q.Set(kv[i], kv[i+1])
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// ---- SearXNG：自己部署的元搜索，开源、无 key、自带 JSON 接口 ----

type searxng struct {
	base string
	hc   *http.Client
}

// NewSearXNG 创建 SearXNG 搜索源。base 是它的地址（如 http://searxng:8080），由管理员配置，不来自模型。
func NewSearXNG(base string, hc *http.Client) Searcher {
	return &searxng{base: strings.TrimRight(base, "/"), hc: hc}
}

func (s *searxng) Name() string { return "searxng" }

func (s *searxng) Search(ctx context.Context, query string, limit int) ([]provider.WebSearchResult, error) {
	status, body, err := doRequest(ctx, s.hc, http.MethodGet, withQuery(s.base+"/search", "q", query, "format", "json"), map[string]string{"Accept": "application/json"}, nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("SearXNG 返回 %d", status)
	}
	var out struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("SearXNG 返回的不是 JSON（是否在配置里开启了 json 格式）：%w", err)
	}
	var res []provider.WebSearchResult
	for _, r := range out.Results {
		if r.URL == "" {
			continue
		}
		res = append(res, provider.WebSearchResult{Title: collapse(r.Title), URL: r.URL, Snippet: collapse(r.Content)})
		if len(res) >= limit {
			break
		}
	}
	return res, nil
}

// ---- Bing：抓网页结果，零配置 ----

type bing struct {
	base string
	hc   *http.Client
}

// DefaultBingURL 是 Bing 的网页搜索地址。
const DefaultBingURL = "https://www.bing.com/search"

// NewBing 创建 Bing 搜索源（抓网页结果）。
func NewBing(base string, hc *http.Client) Searcher { return &bing{base: base, hc: hc} }

func (b *bing) Name() string { return "bing" }

func (b *bing) Search(ctx context.Context, query string, limit int) ([]provider.WebSearchResult, error) {
	status, body, err := doRequest(ctx, b.hc, http.MethodGet, withQuery(b.base, "q", query), map[string]string{"User-Agent": browserUA, "Accept-Language": acceptLang, "Accept": acceptPages}, nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("搜索源 Bing 返回 %d：%w", status, ErrSearchBlocked)
	}
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	var res []provider.WebSearchResult
	for _, li := range findAll(doc, func(n *html.Node) bool { return n.DataAtom == atom.Li && hasClass(n, "b_algo") }) {
		h2 := findFirst(li, func(n *html.Node) bool { return n.DataAtom == atom.H2 })
		if h2 == nil {
			continue
		}
		a := findFirst(h2, func(n *html.Node) bool { return n.DataAtom == atom.A })
		if a == nil {
			continue
		}
		target := decodeBingURL(attr(a, "href"))
		if target == "" {
			continue
		}
		snippet := ""
		if p := findFirst(li, func(n *html.Node) bool { return n.DataAtom == atom.P }); p != nil {
			snippet = collapse(nodeText(p))
		}
		res = append(res, provider.WebSearchResult{Title: collapse(nodeText(a)), URL: target, Snippet: snippet})
		if len(res) >= limit {
			break
		}
	}
	if len(res) == 0 && !strings.Contains(string(body), `id="b_results"`) {
		return nil, fmt.Errorf("搜索源 Bing 返回了不认识的页面：%w", ErrSearchBlocked) // 既没有结果也没有结果区：多半是验证页
	}
	return res, nil
}

// decodeBingURL 解出 Bing 跳转链接里的真实网址：https://www.bing.com/ck/a?...&u=a1<base64url>。
// 不是跳转链接就原样返回（必须是 http(s)）。
func decodeBingURL(href string) string {
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if strings.HasSuffix(u.Hostname(), "bing.com") && u.Path == "/ck/a" {
		enc := u.Query().Get("u")
		if !strings.HasPrefix(enc, "a1") {
			return ""
		}
		raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(enc[2:], "="))
		if err != nil {
			return ""
		}
		href = string(raw)
		if u, err = url.Parse(href); err != nil {
			return ""
		}
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	return href
}

// ---- DuckDuckGo：抓网页结果，零配置；数据中心的出口常被要求人机验证 ----

type duckduckgo struct {
	base string
	hc   *http.Client
}

// DefaultDuckDuckGoURL 是 DuckDuckGo 无脚本版的搜索地址。
const DefaultDuckDuckGoURL = "https://html.duckduckgo.com/html/"

// NewDuckDuckGo 创建 DuckDuckGo 搜索源（抓网页结果）。
func NewDuckDuckGo(base string, hc *http.Client) Searcher { return &duckduckgo{base: base, hc: hc} }

func (d *duckduckgo) Name() string { return "duckduckgo" }

func (d *duckduckgo) Search(ctx context.Context, query string, limit int) ([]provider.WebSearchResult, error) {
	status, body, err := doRequest(ctx, d.hc, http.MethodGet, withQuery(d.base, "q", query), map[string]string{"User-Agent": browserUA, "Accept-Language": acceptLang, "Accept": acceptPages}, nil)
	if err != nil {
		return nil, err
	}
	if status == http.StatusAccepted || strings.Contains(string(body), "anomaly-modal") {
		return nil, ErrSearchBlocked
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("DuckDuckGo 返回 %d", status)
	}
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	var res []provider.WebSearchResult
	for _, item := range findAll(doc, func(n *html.Node) bool {
		return n.DataAtom == atom.Div && hasClass(n, "result") && !hasClass(n, "result--ad")
	}) {
		a := findFirst(item, func(n *html.Node) bool { return n.DataAtom == atom.A && hasClass(n, "result__a") })
		if a == nil {
			continue
		}
		target := decodeDDGURL(attr(a, "href"))
		if target == "" {
			continue
		}
		snippet := ""
		if s := findFirst(item, func(n *html.Node) bool { return hasClass(n, "result__snippet") }); s != nil {
			snippet = collapse(nodeText(s))
		}
		res = append(res, provider.WebSearchResult{Title: collapse(nodeText(a)), URL: target, Snippet: snippet})
		if len(res) >= limit {
			break
		}
	}
	return res, nil
}

// decodeDDGURL 解出 DuckDuckGo 跳转链接 //duckduckgo.com/l/?uddg=<编码后的真实网址> 里的真实网址。
func decodeDDGURL(href string) string {
	if strings.HasPrefix(href, "//") {
		href = "https:" + href
	}
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if target := u.Query().Get("uddg"); target != "" {
		href = target
		if u, err = url.Parse(href); err != nil {
			return ""
		}
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	return href
}

// ---- Exa：免 key 的 MCP 接口，语义检索，结果比抓网页的搜索引擎稳定；是 Exa 托管的免费额度，会限流，搜索词会发给 Exa ----

type exa struct {
	endpoint string
	hc       *http.Client
}

// DefaultExaURL 是 Exa 公开的 MCP 端点（不需要 API Key）。
const DefaultExaURL = "https://mcp.exa.ai/mcp"

const exaSnippetRunes = 300 // 每条结果的摘要最多留多少字，免得一条结果占掉太多上下文

// NewExa 创建 Exa 搜索源：用 MCP 的 tools/call 调 web_search_exa，不需要先 initialize。
func NewExa(endpoint string, hc *http.Client) Searcher { return &exa{endpoint: endpoint, hc: hc} }

func (e *exa) Name() string { return "exa" }

func (e *exa) Search(ctx context.Context, query string, limit int) ([]provider.WebSearchResult, error) {
	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": "web_search_exa", "arguments": map[string]any{"query": query, "numResults": limit}},
	})
	status, body, err := doRequest(ctx, e.hc, http.MethodPost, e.endpoint,
		map[string]string{"Content-Type": "application/json", "Accept": "application/json, text/event-stream"}, payload)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("搜索源 Exa 返回 %d", status)
	}
	return parseExa(body, limit)
}

// parseExa 解析 MCP 的响应：SSE（data: 行里是 JSON-RPC 结果）或直接是 JSON。结果是一段文字，
// 条目之间用 --- 隔开，每条以 Title: 开头，有 URL: 行，Highlights: 之后是摘要。
func parseExa(body []byte, limit int) ([]provider.WebSearchResult, error) {
	payload := strings.TrimSpace(string(body))
	for _, line := range strings.Split(payload, "\n") {
		if rest, ok := strings.CutPrefix(line, "data:"); ok {
			payload = strings.TrimSpace(rest)
			break
		}
	}
	var msg struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(payload), &msg); err != nil {
		return nil, fmt.Errorf("搜索源 Exa 返回的不是 MCP 响应：%w", err)
	}
	if msg.Error != nil {
		return nil, fmt.Errorf("搜索源 Exa 返回错误：%s", msg.Error.Message)
	}
	var sb strings.Builder
	for _, c := range msg.Result.Content {
		sb.WriteString(c.Text)
		sb.WriteString("\n\n---\n\n")
	}
	if msg.Result.IsError {
		return nil, fmt.Errorf("搜索源 Exa 搜索失败：%s", truncateForLog(sb.String()))
	}
	var res []provider.WebSearchResult
	for _, entry := range strings.Split(sb.String(), "\n\n---\n\n") {
		r := parseExaEntry(entry)
		if r.URL == "" {
			continue
		}
		res = append(res, r)
		if len(res) >= limit {
			break
		}
	}
	return res, nil
}

func parseExaEntry(entry string) provider.WebSearchResult {
	var r provider.WebSearchResult
	head, highlights, _ := strings.Cut(entry, "Highlights:")
	for _, line := range strings.Split(head, "\n") {
		if v, ok := strings.CutPrefix(line, "Title:"); ok {
			r.Title = collapse(v)
		} else if v, ok := strings.CutPrefix(line, "URL:"); ok {
			if u := strings.TrimSpace(v); strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
				r.URL = u
			}
		}
	}
	if sn := []rune(collapse(highlights)); len(sn) > exaSnippetRunes {
		r.Snippet = string(sn[:exaSnippetRunes]) + "…"
	} else {
		r.Snippet = string(sn)
	}
	return r
}

func truncateForLog(s string) string {
	s = collapse(s)
	if utf8.RuneCountInString(s) > 120 {
		return string([]rune(s)[:120]) + "…"
	}
	return s
}
