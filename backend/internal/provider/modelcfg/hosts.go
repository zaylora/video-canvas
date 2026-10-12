// 本文件：域名白名单匹配。插件宿主不再使用白名单（去掉了 allowedHosts），这里只留给还需要按域名放行的调用方，
// 如 netguard.CheckURLAllowed。

package modelcfg

import "strings"

// MatchHost 判断 host 是否命中白名单。"*.example.com" 匹配任意层级的子域名，不匹配 example.com 本身。
// 比较不区分大小写，并忽略末尾的点。
func MatchHost(patterns []string, host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "" {
		return false
	}
	for _, p := range patterns {
		p = strings.ToLower(p)
		if strings.HasPrefix(p, "*.") {
			if strings.HasSuffix(host, p[1:]) && len(host) > len(p)-1 {
				return true
			}
			continue
		}
		if host == p {
			return true
		}
	}
	return false
}
