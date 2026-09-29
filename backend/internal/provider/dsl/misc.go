// 本文件：零散的辅助函数：平台状态映射（status_map）与密钥脱敏（Redact）。

package dsl

import (
	"net/url"
	"reflect"
	"sort"
	"strings"
)

// DefaultStatusKey 是 status_map 里的兜底键。
const DefaultStatusKey = "_default"

// mapStatus 用 status_map 映射平台状态。先精确匹配，再忽略大小写匹配；都没有则用 "_default"。
// 连 "_default" 都没配置时按 running 处理（继续轮询，由任务 deadline 兜底），同样返回 usedDefault=true。
func mapStatus(p *ProviderConfig, providerStatus string) (string, bool) {
	if p != nil {
		if v, ok := p.StatusMap[providerStatus]; ok && providerStatus != DefaultStatusKey {
			return v, false
		}
		for k, v := range p.StatusMap {
			if k != DefaultStatusKey && strings.EqualFold(k, providerStatus) {
				return v, false
			}
		}
		if v, ok := p.StatusMap[DefaultStatusKey]; ok {
			return v, true
		}
	}
	return "running", true
}

// redact 递归复制 v，并把字符串里出现的 secrets 明文（以及它的 URL 转义形式）替换成 "***"。
// 处理 string / map / slice；其他类型（数字、bool、nil）原样返回。空 secret 会被忽略，避免把所有内容都替换掉。
func redact(v any, secrets []string) any {
	var needles []string
	seen := map[string]bool{}
	for _, s := range secrets {
		if s == "" {
			continue
		}
		for _, n := range []string{s, url.QueryEscape(s), url.PathEscape(s)} {
			if !seen[n] {
				seen[n] = true
				needles = append(needles, n)
			}
		}
	}
	if len(needles) == 0 {
		return v
	}
	// 长的先替换，避免短 secret 是长 secret 的子串时留下残片
	sort.Slice(needles, func(i, j int) bool { return len(needles[i]) > len(needles[j]) })
	return redactValue(v, needles)
}

func redactString(s string, needles []string) string {
	for _, n := range needles {
		if strings.Contains(s, n) {
			s = strings.ReplaceAll(s, n, "***")
		}
	}
	return s
}

func redactValue(v any, needles []string) any {
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		return redactString(t, needles)
	case []byte:
		return []byte(redactString(string(t), needles))
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, x := range t {
			out[redactString(k, needles)] = redactValue(x, needles)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = redactValue(x, needles)
		}
		return out
	case map[string]string:
		out := make(map[string]string, len(t))
		for k, x := range t {
			out[redactString(k, needles)] = redactString(x, needles)
		}
		return out
	case map[string][]string: // http.Header
		out := make(map[string][]string, len(t))
		for k, xs := range t {
			cp := make([]string, len(xs))
			for i, x := range xs {
				cp[i] = redactString(x, needles)
			}
			out[k] = cp
		}
		return out
	case []string:
		out := make([]string, len(t))
		for i, x := range t {
			out[i] = redactString(x, needles)
		}
		return out
	}
	// 其他 map / slice 类型（如 []map[string]any）走反射
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice:
		out := make([]any, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			out[i] = redactValue(rv.Index(i).Interface(), needles)
		}
		return out
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return v
		}
		out := make(map[string]any, rv.Len())
		for _, k := range rv.MapKeys() {
			out[redactString(k.String(), needles)] = redactValue(rv.MapIndex(k).Interface(), needles)
		}
		return out
	}
	return v
}
