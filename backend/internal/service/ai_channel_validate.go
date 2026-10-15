package service

import (
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"unicode/utf8"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
)

// checkChannelName 校验并规整渠道显示名。
func checkChannelName(raw string) (string, string) {
	name := strings.TrimSpace(raw)
	if name == "" || utf8.RuneCountInString(name) > channelNameMaxLen {
		return "", fmt.Sprintf("name 不能为空，且不超过 %d 个字符", channelNameMaxLen)
	}
	return name, ""
}

// checkBaseURL 校验渠道地址：http / https、有主机、不带用户名密码（凭证只能走 Key）、不带查询参数与片段（插件的 path 要拼在它后面）。
func checkBaseURL(raw string) (string, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > channelBaseURLMaxLen {
		return "", fmt.Sprintf("base_url 不能为空，且不超过 %d 个字符", channelBaseURLMaxLen)
	}
	u, err := url.Parse(raw)
	switch {
	case err != nil || u.Host == "" || u.Hostname() == "":
		return "", "base_url 不是合法的地址"
	case u.Scheme != "http" && u.Scheme != "https":
		return "", "base_url 必须以 http:// 或 https:// 开头"
	case u.User != nil:
		return "", "base_url 不能包含用户名或密码，凭证请通过渠道 Key 设置"
	case u.RawQuery != "" || u.Fragment != "" || strings.Contains(raw, "?") || strings.Contains(raw, "#"):
		return "", "base_url 不能包含查询参数或片段"
	}
	return raw, ""
}

// checkRateLimit 限流参数不能为负；0 表示不限。
func checkRateLimit(r provider.RateLimit) string {
	if r.RPS < 0 || r.MaxConcurrency < 0 || r.MaxRunning < 0 {
		return "rate_limit 的 rps、max_concurrency 与 max_running 不能为负数（0 表示不限）"
	}
	return ""
}

// 自定义请求头的限制。
const (
	channelHeadersMax     = 32   // 最多条数
	channelHeaderValueMax = 1024 // 单个值的最大字符数
)

// forbiddenChannelHeaders 是渠道不能自定义的请求头（小写）：由宿主独占（传输层 / 鉴权）或属于敏感头，
// 允许设置会让请求走私或把凭证带去不该去的地方。与插件请求描述里的禁用头（plugin 包 forbiddenHeaders）同一口径。
var forbiddenChannelHeaders = map[string]bool{
	"host": true, "content-length": true, "transfer-encoding": true, "connection": true, "upgrade": true,
	"authorization": true, "proxy-authorization": true, "cookie": true,
}

// checkChannelHeaders 校验并规整渠道自定义请求头：最多 32 条；名称是合法的 HTTP token，大小写不同的同名视为重复；
// 禁用头被拒绝；值不超过 1024 字符、不含换行或控制字符（防请求头注入）。返回规整后的 map（永不为 nil）和问题说明。
// 值明文存储，不要放密钥：密钥走渠道 Key。
func checkChannelHeaders(in map[string]string) (map[string]string, string) {
	if len(in) > channelHeadersMax {
		return map[string]string{}, fmt.Sprintf("headers 最多 %d 条（当前 %d 条）", channelHeadersMax, len(in))
	}
	names := make([]string, 0, len(in))
	for k := range in {
		names = append(names, k)
	}
	sort.Strings(names) // 报错顺序稳定
	out := make(map[string]string, len(in))
	seen := map[string]bool{}
	var problems []string
	for _, name := range names {
		v := in[name]
		lower := strings.ToLower(name)
		switch {
		case !isHeaderToken(name):
			problems = append(problems, fmt.Sprintf("请求头名称 %q 不合法", name))
			continue
		case forbiddenChannelHeaders[lower]:
			problems = append(problems, fmt.Sprintf("请求头 %s 由宿主独占或属于敏感头，不允许设置", name))
			continue
		case seen[lower]:
			problems = append(problems, fmt.Sprintf("请求头 %s 重复（名称不区分大小写）", name))
			continue
		case utf8.RuneCountInString(v) > channelHeaderValueMax:
			problems = append(problems, fmt.Sprintf("请求头 %s 的值不能超过 %d 个字符", name, channelHeaderValueMax))
			continue
		case hasControlChar(v):
			problems = append(problems, fmt.Sprintf("请求头 %s 的值不能含换行或控制字符", name))
			continue
		}
		seen[lower] = true
		out[name] = v
	}
	return out, strings.Join(problems, "；")
}

// hasControlChar 判断是否含换行或其他控制字符（制表符除外）。
func hasControlChar(s string) bool {
	for _, r := range s {
		if r < 0x20 && r != '\t' || r == 0x7f {
			return true
		}
	}
	return false
}

// isHeaderToken 判断名字是不是合法的 HTTP 请求头名称（RFC 7230 token）。
func isHeaderToken(s string) bool {
	for _, r := range s {
		if r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || strings.ContainsRune("!#$%&'*+-.^_|~", r) || r == '`' {
			continue
		}
		return false
	}
	return s != ""
}

// appendIf 非空才追加。
func appendIf(list []string, s string) []string {
	if s != "" {
		return append(list, s)
	}
	return list
}

// invalidChannel 把问题列表合成一个 ErrChannelInvalid，原因写进 msg。
func invalidChannel(problems []string) error {
	return errcode.ErrChannelInvalid.WithMsg(strings.Join(problems, "；"))
}

// decodeStringMap 把 JSON 对象文本解成 map[string]string；空文本、损坏、null 都得到空 map（永远不是 nil）。
func decodeStringMap(raw model.JSONText) map[string]string {
	out := map[string]string{}
	if len(raw) > 0 {
		var m map[string]string
		if err := json.Unmarshal(raw, &m); err == nil && m != nil {
			out = m
		}
	}
	return out
}

// jsonEqual 比较两份 JSON 文本的语义是否相同（忽略键序与空白）。
func jsonEqual(a, b model.JSONText) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return string(a) == string(b)
	}
	return reflect.DeepEqual(x, y)
}
