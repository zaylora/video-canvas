// 本文件：allowed_hosts 的校验与匹配：只接受小写域名或 *.example.com 通配，拒绝 IP、端口和覆盖公共后缀的通配。

package modelcfg

import (
	"fmt"
	"net"
	"regexp"
	"strings"

	"golang.org/x/net/publicsuffix"
)

// hostLabelRe 是单个域名标签的格式。
var hostPatternRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$`)

// ValidateHostPattern 校验 allowed_hosts 里的一项：小写域名，或 "*.example.com" 通配。
// 不接受 IP 字面量、端口、协议、路径，也不接受 "*.com"、"*.com.cn" 这种覆盖整个公共后缀的通配。
func ValidateHostPattern(p string) error {
	if p == "" {
		return fmt.Errorf("不能为空")
	}
	if p != strings.ToLower(p) {
		return fmt.Errorf("请使用小写域名")
	}
	if strings.ContainsAny(p, "/:@? #") {
		return fmt.Errorf("只能填写域名，不要带协议、端口或路径")
	}
	base := p
	if strings.HasPrefix(p, "*.") {
		base = p[2:]
		if strings.Count(base, ".") < 1 {
			return fmt.Errorf("通配范围过宽，至少需要 *.example.com 这样的形式")
		}
		// "*.com.cn" / "*.co.uk" 这类通配等于放行整个注册域后缀下的所有站点。
		// 只按 ICANN 部分判断：cloudfront.net、s3.amazonaws.com 这类云厂商的私有后缀
		// 常被平台用来托管结果文件，管理员需要能写 "*.cloudfront.net"，所以不拦。
		if suffix, icann := publicsuffix.PublicSuffix(base); icann && suffix == base {
			return fmt.Errorf("通配范围过宽，%s 是公共后缀，请写成 *.example.%s 这样的形式", base, base)
		}
	}
	if net.ParseIP(base) != nil {
		return fmt.Errorf("不能使用 IP 地址，请使用域名")
	}
	if !hostPatternRe.MatchString(base) {
		return fmt.Errorf("域名格式不合法")
	}
	return nil
}

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
