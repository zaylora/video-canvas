package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"unicode/utf8"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/provider/pluginmeta"
	"video-canvas/internal/repository"
)

// resolvedVersion 是解析出来的插件版本头信息与 meta。
type resolvedVersion struct {
	version *model.AIPluginVersion
	meta    *pluginmeta.Meta
}

// resolveVersion 解析 (pluginKey, version)：版本必须存在、meta 能解析；requireEnabled 时插件还必须启用。
// 用户填错造成的问题（不存在、已停用）作为 msg 返回，与其他字段的问题一起报；基础设施故障才返回 error。
// 有 msg 时返回的 *resolvedVersion 为 nil（调用方据此跳过 settings 校验）。
func (s *AIChannelService) resolveVersion(ctx context.Context, pluginKey, version string, requireEnabled bool) (*resolvedVersion, string, error) {
	if pluginKey == "" || version == "" {
		return nil, "plugin_key 与 plugin_version 都必须填写", nil
	}
	ver, err := s.plugins.FindVersion(ctx, pluginKey, version)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, fmt.Sprintf("插件版本 %s@%s 不存在", pluginKey, version), nil
	}
	if err != nil {
		return nil, "", err
	}
	if requireEnabled {
		p, err := s.plugins.GetPlugin(ctx, pluginKey)
		if errors.Is(err, repository.ErrNotFound) {
			return nil, fmt.Sprintf("插件 %s 不存在", pluginKey), nil
		}
		if err != nil {
			return nil, "", err
		}
		if !p.Enabled {
			return nil, fmt.Sprintf("插件 %s 已停用，不能选用", pluginKey), nil
		}
	}
	meta, err := pluginmeta.Parse(ver.MetaJSON)
	if err != nil {
		return nil, fmt.Sprintf("插件 %s@%s 的 meta 无法解析", pluginKey, version), nil
	}
	return &resolvedVersion{version: ver, meta: meta}, "", nil
}

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

// formatSettingIssues 把设置项问题拼成“前缀.名字：原因”的文案列表。
func formatSettingIssues(prefix string, issues []modelcfg.Issue) []string {
	out := make([]string, 0, len(issues))
	for _, is := range issues {
		out = append(out, prefix+"."+is.Path+"："+is.Message)
	}
	return out
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

// decodeObject 把 JSON 对象文本解成 map；空文本、损坏、null 都得到空 map（永远不是 nil）。
func decodeObject(raw model.JSONText) map[string]any {
	out := map[string]any{}
	if len(raw) > 0 {
		var m map[string]any
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
