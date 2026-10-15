package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/netguard"
)

// pullModelsTimeout 是拉取上游模型清单的总超时。
const pullModelsTimeout = 30 * time.Second

// pullErrSnippetLen 是错误信息里引用的上游响应片段的最大字符数。
const pullErrSnippetLen = 200

// ListUpstreamModels 实现 provider.ModelLister：GET {base_url}/models，渠道 API Key 作 Bearer，读 OpenAI 形状的 data[].id。
// 与其他出站请求一样走 SSRF 防护的客户端和渠道限流；渠道自定义请求头照常带上（目标就是 base_url 本身）。
// 失败都返回 *provider.PullError，信息里不含 Key 明文。
func (e *Executor) ListUpstreamModels(ctx context.Context, ch provider.ChannelSnapshot, apiKey string) ([]string, error) {
	// 1. 目标地址：base_url + /models
	target, err := pullModelsURL(ch.BaseURL)
	if err != nil {
		return nil, &provider.PullError{Reason: provider.PullUnreachable, Message: "渠道的 Base URL 不合法"}
	}
	redact := func(s string) string { return strings.ReplaceAll(s, apiKey, "***") }
	// 2. 渠道限流（与插件请求共享同一份配额），再带超时发请求
	release, err := e.limit(ctx, ch)
	if err != nil {
		return nil, &provider.PullError{Reason: provider.PullUnreachable, Message: "渠道请求过于频繁，请稍后重试"}
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, pullModelsTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, &provider.PullError{Reason: provider.PullUnreachable, Message: "渠道的 Base URL 不合法"}
	}
	// 请求头优先级与插件请求一致：默认 UA < 渠道自定义头 < 鉴权
	req.Header.Set("User-Agent", defaultUserAgent)
	req.Header.Set("Accept", "application/json")
	for k, v := range ch.Headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := e.client().Do(req)
	if err != nil {
		return nil, pullTransportError(err, target, redact)
	}
	defer func() { _ = resp.Body.Close() }()

	// 3. 状态码：401 / 403 是 Key 问题，其他非 2xx 带上响应片段帮助判断
	body, err := io.ReadAll(io.LimitReader(resp.Body, provider.MaxPullBodyBytes+1))
	if err != nil {
		return nil, &provider.PullError{Reason: provider.PullUnreachable, Message: "读取上游响应失败：" + redact(err.Error())}
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, &provider.PullError{Reason: provider.PullUnauthorized, Status: resp.StatusCode,
			Message: fmt.Sprintf("上游拒绝了请求（HTTP %d），请检查 API Key 是否有效、有无权限", resp.StatusCode)}
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return nil, &provider.PullError{Reason: provider.PullStatus, Status: resp.StatusCode,
			Message: fmt.Sprintf("上游返回 HTTP %d，它可能不提供 /models：%s", resp.StatusCode, redact(snippet(body)))}
	case len(body) > provider.MaxPullBodyBytes:
		return nil, &provider.PullError{Reason: provider.PullTooLarge, Message: "上游返回的模型清单过大"}
	}

	// 4. 解析 OpenAI 形状：{"data":[{"id":"..."}]}；去重保序，没有 id 的项忽略
	return parsePulledModels(body)
}

// parsePulledModels 解析 OpenAI 形状的模型列表，去重保序；不是该形状返回 PullFormat，超过上限返回 PullTooLarge。
func parsePulledModels(body []byte) ([]string, error) {
	var doc struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &doc); err != nil || doc.Data == nil {
		return nil, &provider.PullError{Reason: provider.PullFormat, Message: "上游返回的不是 OpenAI 形状的模型列表（缺少 data 数组）"}
	}
	if len(doc.Data) > provider.MaxPullModels {
		return nil, &provider.PullError{Reason: provider.PullTooLarge, Message: fmt.Sprintf("上游返回了 %d 个模型，超过上限 %d", len(doc.Data), provider.MaxPullModels)}
	}
	seen := make(map[string]bool, len(doc.Data))
	out := make([]string, 0, len(doc.Data))
	for _, m := range doc.Data {
		id := strings.TrimSpace(m.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out, nil
}

// pullModelsURL 由 base_url 拼出模型清单地址：只在末尾补 /models，不自动补 /v1（上游需要 /v1 就写进 base_url）。
func pullModelsURL(base string) (string, error) {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return "", errors.New("base_url 不合法")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/models"
	u.RawQuery, u.Fragment = "", ""
	return u.String(), nil
}

// pullTransportError 把传输层错误翻译成对运营有用的说明：被 SSRF 防护拦截、超时、其他连接失败。
func pullTransportError(err error, target string, redact func(string) string) error {
	host := target
	if u, perr := url.Parse(target); perr == nil {
		host = u.Host
	}
	switch {
	case netguard.IsGuardError(err):
		return &provider.PullError{Reason: provider.PullUnreachable, Message: fmt.Sprintf("%s 是不允许访问的地址（内网 / 回环地址会被拦截）", host)}
	case errors.Is(err, context.DeadlineExceeded):
		return &provider.PullError{Reason: provider.PullUnreachable, Message: fmt.Sprintf("连接 %s 超时（%d 秒内没有响应）", host, int(pullModelsTimeout/time.Second))}
	default:
		return &provider.PullError{Reason: provider.PullUnreachable, Message: fmt.Sprintf("无法连接 %s：%s", host, redact(err.Error()))}
	}
}

// snippet 取响应体开头一小段，折叠空白，用于错误信息。
func snippet(body []byte) string {
	s := strings.Join(strings.Fields(string(body)), " ")
	if r := []rune(s); len(r) > pullErrSnippetLen {
		s = string(r[:pullErrSnippetLen]) + "…"
	}
	return s
}
