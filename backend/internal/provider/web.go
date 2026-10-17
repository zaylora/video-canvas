package provider

import "context"

// WebAccess 是 Agent 联网工具用的出站能力：搜索和抓取网页。实现在 provider/webaccess，
// 内网拦截、大小和超时限制都在实现里；service 只依赖这份契约。
type WebAccess interface {
	// Search 在互联网上搜索。没有结果返回空列表（不是错误）；所有搜索源都不可用返回 *WebError。
	Search(ctx context.Context, query string, limit int) (*WebSearchResponse, error)
	// Fetch 抓取一个 http(s) 网页并抽取正文。maxChars 是返回的正文字数上限。出错一律返回 *WebError。
	Fetch(ctx context.Context, rawURL string, maxChars int) (*WebPage, error)
}

// WebSearchResult 是一条搜索结果。
type WebSearchResult struct {
	Title   string
	URL     string
	Snippet string
}

// WebSearchResponse 是一次搜索的结果，Provider 是实际出结果的搜索源。
type WebSearchResponse struct {
	Provider string
	Results  []WebSearchResult
}

// WebPage 是抓回来的网页：正文已抽成 Markdown。
type WebPage struct {
	URL       string // 最终地址（跟随重定向之后）
	Title     string
	Text      string
	Truncated bool // 正文被字数或响应体大小的上限截断了
}

// WebError 是联网失败的原因。Message 是可以直接告诉模型的话，不含内部细节。
type WebError struct {
	Message string
	Blocked bool // 目标是本机、内网等被防护拦下的地址
}

func (e *WebError) Error() string { return e.Message }
