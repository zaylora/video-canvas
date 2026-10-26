package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"video-canvas/internal/provider"
)

// fakeWeb 扮演联网能力：记下调用，按脚本回放。
type fakeWeb struct {
	searchResp  *provider.WebSearchResponse
	searchErr   error
	page        *provider.WebPage
	fetchErr    error
	searches    []string
	searchLimit int
	fetches     []string
	fetchChars  int
}

func (f *fakeWeb) Search(_ context.Context, q string, limit int) (*provider.WebSearchResponse, error) {
	f.searches, f.searchLimit = append(f.searches, q), limit
	if f.searchErr != nil {
		return nil, f.searchErr
	}
	if f.searchResp != nil {
		return f.searchResp, nil
	}
	return &provider.WebSearchResponse{Provider: "bing", Results: []provider.WebSearchResult{
		{Title: "港口纪录片", URL: "https://a.example/1", Snippet: "清晨的港口"},
		{Title: "渔船", URL: "https://b.example/2", Snippet: ""},
	}}, nil
}

func (f *fakeWeb) Fetch(_ context.Context, u string, maxChars int) (*provider.WebPage, error) {
	f.fetches, f.fetchChars = append(f.fetches, u), maxChars
	if f.fetchErr != nil {
		return nil, f.fetchErr
	}
	if f.page != nil {
		return f.page, nil
	}
	return &provider.WebPage{URL: u, Title: "示例页面", Text: "# 示例\n正文内容"}, nil
}

const untrusted = "不可信资料"

func TestWebSearchTool(t *testing.T) {
	b := newBridgeEnv(t)
	tok, _ := b.token(t, "all")
	res := b.tool(t, tok, "web_search", map[string]any{"query": "港口 纪录片", "limit": 3})
	if res.IsError || res.Terminate {
		t.Fatalf("res=%+v", res)
	}
	for _, want := range []string{untrusted, "港口 纪录片", "bing", "1. 港口纪录片", "https://a.example/1", "清晨的港口", "2. 渔船", "https://b.example/2"} {
		if !strings.Contains(res.Content, want) {
			t.Errorf("结果应包含 %q:\n%s", want, res.Content)
		}
	}
	w := b.web()
	if len(w.searches) != 1 || w.searches[0] != "港口 纪录片" || w.searchLimit != 3 {
		t.Errorf("searches=%v limit=%d", w.searches, w.searchLimit)
	}
	if got := res.ToolEnd["summary"]; got != "搜索「港口 纪录片」：2 条结果" {
		t.Errorf("时间线摘要: %v", got)
	}
}

func TestWebSearchTool_EmptyIsNotAnError(t *testing.T) {
	b := newBridgeEnv(t)
	b.web().searchResp = &provider.WebSearchResponse{Provider: "bing"}
	tok, _ := b.token(t, "all")
	res := b.tool(t, tok, "web_search", map[string]any{"query": "zzzz"})
	if res.IsError || !strings.Contains(res.Content, "没有找到") {
		t.Errorf("没结果是正常结果，告诉模型换个说法: %+v", res)
	}
}

func TestWebSearchTool_ArgsAndErrors(t *testing.T) {
	b := newBridgeEnv(t)
	tok, _ := b.token(t, "all")
	if res := b.tool(t, tok, "web_search", map[string]any{"query": "  "}); !res.IsError {
		t.Error("空关键词应报错")
	}
	if res := b.tool(t, tok, "web_search", map[string]any{"query": strings.Repeat("词", 301)}); !res.IsError {
		t.Error("关键词太长应报错")
	}
	b.tool(t, tok, "web_search", map[string]any{"query": "x", "limit": 99})
	if b.web().searchLimit != 10 {
		t.Errorf("limit 最多 10: %d", b.web().searchLimit)
	}
	b.tool(t, tok, "web_search", map[string]any{"query": "x"})
	if b.web().searchLimit != 5 {
		t.Errorf("limit 默认 5: %d", b.web().searchLimit)
	}
	b.web().searchErr = &provider.WebError{Message: "搜索服务暂时不可用，请稍后再试。"}
	res := b.tool(t, tok, "web_search", map[string]any{"query": "x"})
	if !res.IsError || !strings.Contains(res.Content, "搜索服务暂时不可用") {
		t.Errorf("联网失败是工具错误，原因告诉模型: %+v", res)
	}
	b.web().searchErr = errors.New("dial tcp 10.0.0.5: 内部细节")
	res = b.tool(t, tok, "web_search", map[string]any{"query": "x"})
	if !res.IsError || strings.Contains(res.Content, "10.0.0.5") {
		t.Errorf("其他错误只说内部错误，细节不外泄: %+v", res)
	}
}

func TestWebFetchTool(t *testing.T) {
	b := newBridgeEnv(t)
	tok, _ := b.token(t, "all")
	res := b.tool(t, tok, "web_fetch", map[string]any{"url": "https://site.example/a", "maxChars": 3000})
	if res.IsError {
		t.Fatalf("res=%+v", res)
	}
	for _, want := range []string{untrusted, "https://site.example/a", "示例页面", "正文内容"} {
		if !strings.Contains(res.Content, want) {
			t.Errorf("结果应包含 %q:\n%s", want, res.Content)
		}
	}
	if b.web().fetchChars != 3000 {
		t.Errorf("maxChars 传给实现: %d", b.web().fetchChars)
	}
	if got := res.ToolEnd["summary"]; got != "读取 https://site.example/a" {
		t.Errorf("时间线摘要: %v", got)
	}
	b.tool(t, tok, "web_fetch", map[string]any{"url": "https://site.example/a"})
	if b.web().fetchChars != 20000 {
		t.Errorf("maxChars 默认 2 万: %d", b.web().fetchChars)
	}
	b.tool(t, tok, "web_fetch", map[string]any{"url": "https://site.example/a", "maxChars": 999999})
	if b.web().fetchChars != 20000 {
		t.Errorf("maxChars 最多 2 万: %d", b.web().fetchChars)
	}
}

func TestWebFetchTool_TruncatedAndErrors(t *testing.T) {
	b := newBridgeEnv(t)
	tok, _ := b.token(t, "all")
	b.web().page = &provider.WebPage{URL: "https://s.example", Title: "长文", Text: "前半部分", Truncated: true}
	if res := b.tool(t, tok, "web_fetch", map[string]any{"url": "https://s.example"}); !strings.Contains(res.Content, "已截断") {
		t.Errorf("被截断要告诉模型: %s", res.Content)
	}
	if res := b.tool(t, tok, "web_fetch", map[string]any{"url": ""}); !res.IsError {
		t.Error("空网址应报错")
	}
	b.web().fetchErr = &provider.WebError{Message: "不能访问这个地址：它指向本机或内网，联网工具只能访问公网网页。", Blocked: true}
	res := b.tool(t, tok, "web_fetch", map[string]any{"url": "http://127.0.0.1/"})
	if !res.IsError || !strings.Contains(res.Content, "本机或内网") {
		t.Errorf("被拦下的地址：工具错误，原因告诉模型: %+v", res)
	}
}

func TestWebTools_PerSegmentCallLimit(t *testing.T) {
	b := newBridgeEnv(t)
	tok, _ := b.token(t, "all")
	// 联网工具计入这个运行片段的联网次数：搜索和抓取共用上限，失败的也算
	for i := 0; i < 20; i++ {
		name := "web_search"
		args := map[string]any{"query": "x"}
		if i%2 == 1 {
			name, args = "web_fetch", map[string]any{"url": "https://s.example"}
		}
		if res := b.tool(t, tok, name, args); res.IsError {
			t.Fatalf("第 %d 次不该被拦: %+v", i+1, res)
		}
	}
	res := b.tool(t, tok, "web_search", map[string]any{"query": "x"})
	if !res.IsError || !strings.Contains(res.Content, "上限") {
		t.Errorf("第 21 次应被挡住并说明上限: %+v", res)
	}
	if len(b.web().searches) != 10 {
		t.Errorf("被挡住的调用不该真的去搜索: %d", len(b.web().searches))
	}
}

func TestWebTools_DisabledWithoutWebAccess(t *testing.T) {
	b := newBridgeEnvWith(t, nil, nil)
	tok, _ := b.token(t, "all")
	for _, name := range []string{"web_search", "web_fetch"} {
		raw, _ := json.Marshal(map[string]any{"query": "x", "url": "https://s.example"})
		res, err := b.br.ExecutePlatformTool(context.Background(), b.rcs[tok], "tc-"+name, name, raw)
		if err != nil {
			t.Fatal(err)
		}
		if !res.IsError || !strings.Contains(res.Content, "未启用") {
			t.Errorf("%s 没有联网能力时要说明未启用: %+v", name, res)
		}
	}
}

func TestWebTools_AvailableInEveryMode(t *testing.T) {
	for _, mode := range []string{"all", "script", "storyboard", "prompt"} {
		b := newBridgeEnv(t)
		tok, _ := b.token(t, mode)
		if res := b.tool(t, tok, "web_search", map[string]any{"query": "x"}); res.IsError {
			t.Errorf("模式 %s 应能用联网工具: %+v", mode, res)
		}
	}
}
