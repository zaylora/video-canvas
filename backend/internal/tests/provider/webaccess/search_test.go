package webaccess_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/webaccess"
)

func serve(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return s
}

func TestSearXNG(t *testing.T) {
	var gotQuery, gotFormat string
	s := serve(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery, gotFormat = r.URL.Query().Get("q"), r.URL.Query().Get("format")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[
			{"title":"港口纪录片","url":"https://a.example/1","content":"清晨的港口"},
			{"title":"渔船","url":"https://b.example/2","content":""},
			{"title":"没有网址","url":"","content":"丢弃"},
			{"title":"第三条","url":"https://c.example/3","content":"c"}]}`))
	})
	got, err := webaccess.NewSearXNG(s.URL, s.Client()).Search(context.Background(), "港口 纪录片", 2)
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery != "港口 纪录片" || gotFormat != "json" {
		t.Errorf("请求参数: q=%q format=%q", gotQuery, gotFormat)
	}
	if len(got) != 2 || got[0].Title != "港口纪录片" || got[0].URL != "https://a.example/1" || got[0].Snippet != "清晨的港口" || got[1].URL != "https://b.example/2" {
		t.Errorf("按 limit 截断，丢掉没有网址的: %+v", got)
	}
}

func TestSearXNG_Errors(t *testing.T) {
	bad := serve(t, func(w http.ResponseWriter, r *http.Request) { http.Error(w, "boom", 500) })
	if _, err := webaccess.NewSearXNG(bad.URL, bad.Client()).Search(context.Background(), "x", 5); err == nil {
		t.Error("上游 500 应报错")
	}
	notJSON := serve(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("<html>forbidden</html>")) })
	if _, err := webaccess.NewSearXNG(notJSON.URL, notJSON.Client()).Search(context.Background(), "x", 5); err == nil {
		t.Error("没开 JSON 格式时返回的是网页，应报错而不是当成空结果")
	}
}

func bingLink(target string) string {
	return "https://www.bing.com/ck/a?!&amp;&amp;p=abc123&amp;u=a1" + base64.RawURLEncoding.EncodeToString([]byte(target)) + "&amp;ntb=1"
}

func bingPage() string {
	item := func(title, target, snippet string) string {
		return `<li class="b_algo" data-id><h2><a href="` + bingLink(target) + `" h="ID=SERP,5">` + title + `</a></h2>` +
			`<div class="b_caption"><p class="b_lineclamp2">` + snippet + `</p></div></li>`
	}
	return `<html><body><ol id="b_results">` +
		item("Go <strong>readability</strong>", "https://github.com/go-shiori/go-readability", "正文 <b>抽取</b>库") +
		item("第二条", "https://example.com/two?a=1&b=2", "摘要二") +
		`<li class="b_ad"><h2><a href="https://ad.example">广告</a></h2></li>` +
		`</ol></body></html>`
}

func TestBing(t *testing.T) {
	var ua, lang string
	s := serve(t, func(w http.ResponseWriter, r *http.Request) {
		ua, lang = r.Header.Get("User-Agent"), r.Header.Get("Accept-Language")
		if r.URL.Query().Get("q") != "go readability" {
			t.Errorf("q=%q", r.URL.Query().Get("q"))
		}
		_, _ = w.Write([]byte(bingPage()))
	})
	got, err := webaccess.NewBing(s.URL, s.Client()).Search(context.Background(), "go readability", 5)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ua, "Mozilla") || lang == "" {
		t.Errorf("要带浏览器样式的请求头，否则会被当成机器人: ua=%q lang=%q", ua, lang)
	}
	if len(got) != 2 {
		t.Fatalf("只取自然结果，不要广告: %+v", got)
	}
	if got[0].URL != "https://github.com/go-shiori/go-readability" || got[0].Title != "Go readability" || got[0].Snippet != "正文 抽取库" {
		t.Errorf("要解出真实网址、去掉标签: %+v", got[0])
	}
	if got[1].URL != "https://example.com/two?a=1&b=2" {
		t.Errorf("真实网址: %q", got[1].URL)
	}
}

func TestBing_NoResultsIsNotAnError(t *testing.T) {
	s := serve(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><ol id="b_results"><li class="b_no">没有找到结果</li></ol></body></html>`))
	})
	got, err := webaccess.NewBing(s.URL, s.Client()).Search(context.Background(), "zzzzqqqq", 5)
	if err != nil || len(got) != 0 {
		t.Errorf("没结果是空列表: %+v %v", got, err)
	}
}

func ddgPage() string {
	item := func(title, target, snippet string) string {
		return `<div class="result results_links results_links_deep web-result"><div class="links_main links_deep result__body">` +
			`<h2 class="result__title"><a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=` + strings.ReplaceAll(strings.ReplaceAll(target, ":", "%3A"), "/", "%2F") + `&amp;rut=zz">` + title + `</a></h2>` +
			`<a class="result__snippet" href="//duckduckgo.com/l/?uddg=x">` + snippet + `</a></div></div>`
	}
	return `<html><body><div id="links" class="results">` +
		item("DDG <b>一</b>", "https://one.example/a", "摘要<b>一</b>") + item("DDG 二", "https://two.example/b", "摘要二") +
		`</div></body></html>`
}

func TestDuckDuckGo(t *testing.T) {
	s := serve(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(ddgPage())) })
	got, err := webaccess.NewDuckDuckGo(s.URL, s.Client()).Search(context.Background(), "x", 5)
	if err != nil || len(got) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	if got[0].URL != "https://one.example/a" || got[0].Title != "DDG 一" || got[0].Snippet != "摘要一" {
		t.Errorf("要解出 uddg 里的真实网址: %+v", got[0])
	}
}

func TestDuckDuckGo_ChallengePageIsReportedAsBlocked(t *testing.T) {
	// DuckDuckGo 对数据中心的出口会返回人机验证页（HTTP 202，没有结果）：要报「被拦了」，不能当成「没有结果」
	s := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(202)
		_, _ = w.Write([]byte(`<html><body><div class="anomaly-modal__modal">Unfortunately, bots use DuckDuckGo too.</div></body></html>`))
	})
	_, err := webaccess.NewDuckDuckGo(s.URL, s.Client()).Search(context.Background(), "x", 5)
	if !errors.Is(err, webaccess.ErrSearchBlocked) {
		t.Errorf("应返回 ErrSearchBlocked: %v", err)
	}
}

type fakeSearcher struct {
	name  string
	res   []provider.WebSearchResult
	err   error
	calls int
}

func (f *fakeSearcher) Name() string { return f.name }
func (f *fakeSearcher) Search(context.Context, string, int) ([]provider.WebSearchResult, error) {
	f.calls++
	return f.res, f.err
}

func TestClientSearch_FallsBackInOrder(t *testing.T) {
	a := &fakeSearcher{name: "searxng", err: errors.New("连不上")}
	b := &fakeSearcher{name: "bing", res: []provider.WebSearchResult{{Title: "B", URL: "https://b.example"}}}
	c := &fakeSearcher{name: "duckduckgo", res: []provider.WebSearchResult{{Title: "C", URL: "https://c.example"}}}
	cl := webaccess.New(webaccess.Config{Searchers: []webaccess.Searcher{a, b, c}})
	got, err := cl.Search(context.Background(), "x", 5)
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != "bing" || len(got.Results) != 1 || a.calls != 1 || b.calls != 1 || c.calls != 0 {
		t.Errorf("前一个失败换下一个，成功就不再往后: %+v a=%d b=%d c=%d", got, a.calls, b.calls, c.calls)
	}
}

func TestClientSearch_EmptyResultsTriesNext(t *testing.T) {
	a := &fakeSearcher{name: "searxng"}
	b := &fakeSearcher{name: "bing", res: []provider.WebSearchResult{{Title: "B", URL: "https://b.example"}}}
	got, err := webaccess.New(webaccess.Config{Searchers: []webaccess.Searcher{a, b}}).Search(context.Background(), "x", 5)
	if err != nil || got.Provider != "bing" {
		t.Errorf("空结果也可能是上游引擎被限流，换下一个: %+v %v", got, err)
	}
}

func TestClientSearch_AllFail(t *testing.T) {
	a := &fakeSearcher{name: "searxng", err: errors.New("超时")}
	b := &fakeSearcher{name: "bing", err: webaccess.ErrSearchBlocked}
	_, err := webaccess.New(webaccess.Config{Searchers: []webaccess.Searcher{a, b}}).Search(context.Background(), "x", 5)
	var we *provider.WebError
	if !errors.As(err, &we) || we.Message == "" {
		t.Fatalf("全部失败应返回给模型看的说明: %v", err)
	}
	if strings.Contains(we.Message, "超时") || strings.Contains(we.Message, "searxng") {
		t.Errorf("说明里不放内部细节: %q", we.Message)
	}
}

func TestClientSearch_AllEmptyIsNotAnError(t *testing.T) {
	a := &fakeSearcher{name: "searxng"}
	b := &fakeSearcher{name: "bing"}
	got, err := webaccess.New(webaccess.Config{Searchers: []webaccess.Searcher{a, b}}).Search(context.Background(), "x", 5)
	if err != nil || len(got.Results) != 0 {
		t.Errorf("都没结果就是没结果，不是错误: %+v %v", got, err)
	}
}

func TestClientSearch_DefaultSourcesFollowConfig(t *testing.T) {
	// 没配 SearXNG 时不尝试它；配了就排在第一位
	if got := webaccess.New(webaccess.Config{}).SearchSources(); len(got) != 3 || got[0] != "exa" || got[1] != "bing" || got[2] != "duckduckgo" {
		t.Errorf("默认源（Exa 的结果最切题排前面，抓网页的两个兜底）: %v", got)
	}
	if got := webaccess.New(webaccess.Config{SearXNGURL: "http://searxng:8080"}).SearchSources(); len(got) != 4 || got[0] != "searxng" || got[1] != "exa" {
		t.Errorf("配了 SearXNG 排第一: %v", got)
	}
}

// ---- Exa：免 key 的 MCP 接口，返回的是语义检索的结果，质量比抓网页的搜索引擎稳定 ----

func exaText() string {
	return "Title: 《中国港口》_CCTV节目官网\nURL: http://tv.cctv.com/special/CCTV4zggk/index.shtml\nPublished: N/A\nAuthor: N/A\nHighlights:\n《中国港口》运用文献与记录相结合的叙述方式。\n\n### 视频点播\n\n---\n\n" +
		"Title: 港口晨雾素材\nURL: https://b.example/mist\nPublished: 2024-01-02\nAuthor: someone\nHighlights:\n" + strings.Repeat("长", 600) + "\n\n---\n\n" +
		"Title: 没有网址的条目\nPublished: N/A\nHighlights:\n丢弃\n\n---\n\n" +
		"Title: 第三条\nURL: https://c.example/3\nHighlights:\n摘要三"
}

func exaSSE(text string) string {
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"content": []map[string]string{{"type": "text", "text": text}}}})
	return "event: message\ndata: " + string(b) + "\n\n"
}

func TestExa(t *testing.T) {
	var req struct {
		Method string `json:"method"`
		Params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		} `json:"params"`
	}
	var accept, ctype, httpMethod string
	s := serve(t, func(w http.ResponseWriter, r *http.Request) {
		httpMethod, accept, ctype = r.Method, r.Header.Get("Accept"), r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &req)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(exaSSE(exaText())))
	})
	got, err := webaccess.NewExa(s.URL, s.Client()).Search(context.Background(), "港口 清晨 纪录片 拍摄", 3)
	if err != nil {
		t.Fatal(err)
	}
	if httpMethod != http.MethodPost || !strings.Contains(accept, "text/event-stream") || !strings.Contains(ctype, "application/json") {
		t.Errorf("请求: %s accept=%q content-type=%q", httpMethod, accept, ctype)
	}
	if req.Method != "tools/call" || req.Params.Name != "web_search_exa" || req.Params.Arguments["query"] != "港口 清晨 纪录片 拍摄" || req.Params.Arguments["numResults"] != float64(3) {
		t.Errorf("JSON-RPC 请求不对: %+v", req)
	}
	if len(got) != 3 {
		t.Fatalf("丢掉没有网址的条目: %+v", got)
	}
	if got[0].Title != "《中国港口》_CCTV节目官网" || got[0].URL != "http://tv.cctv.com/special/CCTV4zggk/index.shtml" || !strings.HasPrefix(got[0].Snippet, "《中国港口》运用文献与记录相结合") {
		t.Errorf("第一条: %+v", got[0])
	}
	if n := len([]rune(got[1].Snippet)); n > 301 {
		t.Errorf("摘要要截短，免得一条结果占掉太多上下文: %d 字", n)
	}
	if got[2].URL != "https://c.example/3" {
		t.Errorf("第三条: %+v", got[2])
	}
}

func TestExa_LimitAndPlainJSON(t *testing.T) {
	// 不是 SSE、直接返回 JSON 也要能解析；条数按 limit 截断
	s := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(strings.TrimPrefix(strings.SplitN(exaSSE(exaText()), "\n", 3)[1], "data: ")))
	})
	got, err := webaccess.NewExa(s.URL, s.Client()).Search(context.Background(), "x", 1)
	if err != nil || len(got) != 1 {
		t.Errorf("%+v %v", got, err)
	}
}

func TestExa_Errors(t *testing.T) {
	limited := serve(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	})
	if _, err := webaccess.NewExa(limited.URL, limited.Client()).Search(context.Background(), "x", 3); err == nil {
		t.Error("429 应报错，由调用方换下一个搜索源")
	}
	rpcErr := serve(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`event: message` + "\n" + `data: {"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"quota exceeded"}}` + "\n\n"))
	})
	if _, err := webaccess.NewExa(rpcErr.URL, rpcErr.Client()).Search(context.Background(), "x", 3); err == nil || !strings.Contains(err.Error(), "quota exceeded") {
		t.Errorf("JSON-RPC 错误应报出来: %v", err)
	}
	toolErr := serve(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`data: {"result":{"isError":true,"content":[{"type":"text","text":"search failed"}]}}` + "\n\n"))
	})
	if _, err := webaccess.NewExa(toolErr.URL, toolErr.Client()).Search(context.Background(), "x", 3); err == nil {
		t.Error("工具返回 isError 应报错")
	}
	garbage := serve(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("<html>not mcp</html>")) })
	if _, err := webaccess.NewExa(garbage.URL, garbage.Client()).Search(context.Background(), "x", 3); err == nil {
		t.Error("不是 MCP 响应应报错")
	}
}
