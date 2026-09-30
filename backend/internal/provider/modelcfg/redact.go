// 本文件：密钥脱敏（Redact）：把文本里出现的凭证明文（含 URL 转义形式）替换成 ***。

package modelcfg

import (
	"net/url"
	"reflect"
	"sort"
	"strings"
)

// Redact 把 v 里出现的 secrets 明文替换成 "***"，用于 dry-run / 试跑 / 日志输出脱敏。
func Redact(v any, secrets ...string) any { return redact(v, secrets) }

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
			out[redactString(k, needles)] = redactStrings(xs, needles)
		}
		return out
	case []string:
		return redactStrings(t, needles)
	}
	return redactReflect(v, needles)
}

// redactStrings 复制并脱敏字符串切片。
func redactStrings(xs []string, needles []string) []string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = redactString(x, needles)
	}
	return out
}

// redactReflect 处理其他 map / slice 类型（如 []map[string]any），统一复制成 []any / map[string]any。
func redactReflect(v any, needles []string) any {
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
