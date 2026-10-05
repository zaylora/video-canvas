// Package netguard 是出网的 SSRF 防护传输层：域名白名单、内网 IP 拦截、DNS 解析后校验并直接拨号（防 DNS rebinding）、
// 重定向校验、不走环境代理。插件宿主的请求和结果下载都走它。
package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"video-canvas/internal/provider/modelcfg"
)

// 本文件是 SSRF 防护：白名单校验、内网 IP 拦截、DNS 解析后校验并直接拨号（防 DNS rebinding）。
//
// 三道关口：
//  1. hostGuardRT（RoundTripper）：每次请求和每次重定向都校验域名在 allowed_hosts 内；
//  2. guardDialer（DialContext）：先自己解析 DNS，校验所有解析结果不在内网段，再直接拨 IP，
//     这样“校验时解析到公网、连接时解析到内网”的 rebinding 攻击没有可乘之机；
//  3. CheckRedirect：限制重定向次数。

// ErrBlockedAddress 目标地址落在被禁止的网段（内网、回环、链路本地等）。
var ErrBlockedAddress = errors.New("目标地址不允许访问")

// ErrHostNotAllowed 目标域名不在 allowed_hosts 白名单内。
var ErrHostNotAllowed = errors.New("目标域名不在白名单内")

// ErrTooManyRedirects 重定向次数超过上限。
var ErrTooManyRedirects = errors.New("重定向次数过多")

// ErrResponseTooLarge 响应体超过大小上限。
var ErrResponseTooLarge = errors.New("响应体超过大小上限")

// Resolver 解析域名，测试里可以注入以模拟 DNS rebinding。
type Resolver interface {
	LookupIP(ctx context.Context, host string) ([]net.IP, error)
}

// netResolver 是基于标准库的默认解析器。
type netResolver struct{}

func (netResolver) LookupIP(ctx context.Context, host string) ([]net.IP, error) {
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	out := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, a.IP)
	}
	return out, nil
}

// blockedNets 是默认禁止访问的网段（除 net.IP 自带的 Is* 判断之外的补充）。
var blockedNets = mustCIDRs(
	"0.0.0.0/8",       // “本网络”
	"100.64.0.0/10",   // 运营商级 NAT，云厂商元数据地址（如 100.100.100.200）常在这里
	"192.0.0.0/24",    // IETF 协议分配
	"192.0.2.0/24",    // 文档保留
	"198.18.0.0/15",   // 基准测试
	"198.51.100.0/24", // 文档保留
	"203.0.113.0/24",  // 文档保留
	"240.0.0.0/4",     // 保留（含 255.255.255.255 广播）
	"64:ff9b::/96",    // NAT64：内嵌的 IPv4 另行检查
	"2001:db8::/32",   // 文档保留
	"::/96",           // IPv4 兼容地址（已废弃，如 ::7f00:1 等价于 127.0.0.1 的另一种写法），不会有合法用途
	"100::/64",        // 丢弃专用前缀
	"fec0::/10",       // 站点本地地址（已废弃的 IPv6 私网段，net.IP.IsPrivate 不覆盖）
)

func mustCIDRs(list ...string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(list))
	for _, c := range list {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic(err)
		}
		out = append(out, n)
	}
	return out
}

// IsBlockedIP 判断 IP 是否属于默认禁止访问的范围：回环、未指定（0.0.0.0 / ::）、私有网段
// （10/8、172.16/12、192.168/16、fc00::/7）、链路本地（169.254/16、fe80::/10）、多播及其他保留段。
// IPv4 映射的 IPv6 地址、NAT64 与 6to4 内嵌的 IPv4 地址会还原后再判断。
func IsBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	if ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return true
	}
	for _, n := range blockedNets {
		if n.Contains(ip) {
			return true
		}
	}
	if len(ip) == net.IPv6len {
		// NAT64（64:ff9b::/96）：后 4 字节是 IPv4
		if ip[0] == 0x00 && ip[1] == 0x64 && ip[2] == 0xff && ip[3] == 0x9b {
			return IsBlockedIP(net.IP(ip[12:16]))
		}
		// 6to4（2002::/16）：第 3~6 字节是 IPv4
		if ip[0] == 0x20 && ip[1] == 0x02 {
			return IsBlockedIP(net.IP(ip[2:6]))
		}
	}
	return false
}

// Config 是防护的运行参数。
type Config struct {
	IPAllowed    func(net.IP) bool // 返回 true 表示允许连接该 IP
	Resolver     Resolver
	Dial         func(ctx context.Context, network, addr string) (net.Conn, error)
	MaxRedirects int
}

// ApplyDefaults 补默认值：IP 校验默认拒绝内网 / 回环 / 链路本地，DNS 用系统解析，拨号超时 10 秒，最多重定向 3 次。
func (c *Config) ApplyDefaults() {
	if c.IPAllowed == nil {
		c.IPAllowed = func(ip net.IP) bool { return !IsBlockedIP(ip) }
	}
	if c.Resolver == nil {
		c.Resolver = netResolver{}
	}
	if c.Dial == nil {
		d := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
		c.Dial = d.DialContext
	}
	if c.MaxRedirects <= 0 {
		c.MaxRedirects = 3
	}
}

// NewTransport 创建带 IP 校验的传输层。它不使用环境代理：代理会让实际连接的目标绕过我们的校验。
func NewTransport(cfg *Config) *http.Transport {
	return &http.Transport{
		Proxy:                 nil,
		DialContext:           cfg.dialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
}

// dialContext 先解析域名并校验所有结果，再直接拨号到校验过的 IP。
// 不能把域名交给标准库再解析一次，否则两次解析之间 DNS 可以被换成内网地址。
func (c *Config) dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	var ips []net.IP
	if ip := net.ParseIP(strings.TrimSuffix(host, ".")); ip != nil {
		ips = []net.IP{ip}
	} else {
		ips, err = c.Resolver.LookupIP(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("解析域名 %s 失败：%w", host, err)
		}
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("域名 %s 没有解析结果", host)
	}
	// 只要有一个解析结果落在禁止范围就整体拒绝：攻击者可以让域名同时返回公网和内网地址，赌我们选到内网那个
	for _, ip := range ips {
		if !c.IPAllowed(ip) {
			return nil, fmt.Errorf("%w：%s 解析到 %s", ErrBlockedAddress, host, ip)
		}
	}
	var lastErr error
	for _, ip := range ips {
		conn, err := c.Dial(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// hostGuardRT 在每次 RoundTrip（含重定向）前校验域名白名单与协议。
type hostGuardRT struct {
	allowed []string
	base    http.RoundTripper
}

func (g *hostGuardRT) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := CheckURLAllowed(g.allowed, req.URL.Scheme, req.URL.Hostname(), req.URL.User != nil); err != nil {
		if req.Body != nil {
			_ = req.Body.Close()
		}
		return nil, err
	}
	return g.base.RoundTrip(req)
}

// CheckURLAllowed 校验协议、域名白名单、不带用户名密码。
func CheckURLAllowed(allowed []string, scheme, host string, hasUserinfo bool) error {
	if scheme != "https" && scheme != "http" {
		return fmt.Errorf("%w：不支持的协议 %q", ErrHostNotAllowed, scheme)
	}
	if hasUserinfo {
		return fmt.Errorf("%w：URL 不能包含用户名密码", ErrHostNotAllowed)
	}
	if !modelcfg.MatchHost(allowed, host) {
		return fmt.Errorf("%w：%s", ErrHostNotAllowed, host)
	}
	return nil
}

// NewClient 创建一个绑定白名单的客户端。共享 base 传输层以复用连接池；白名单只在 RoundTripper 里检查，
// 所以不同渠道可以共用同一个连接池。maxRedirects 是最多跟随的重定向次数（<=0 表示不跟随任何重定向，
// 与 Config.ApplyDefaults 把 <=0 补成 3 不同：直接调用 NewClient 时由调用方决定）。
func NewClient(base http.RoundTripper, allowed []string, maxRedirects int) *http.Client {
	return &http.Client{
		Transport: &hostGuardRT{allowed: allowed, base: base},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > maxRedirects {
				return ErrTooManyRedirects
			}
			return nil // 域名白名单由 hostGuardRT 在跟随重定向时再次校验
		},
	}
}

// IsGuardError 判断错误是不是 SSRF 防护主动拒绝的（白名单、内网地址、重定向过多）。
func IsGuardError(err error) bool {
	return errors.Is(err, ErrBlockedAddress) || errors.Is(err, ErrHostNotAllowed) || errors.Is(err, ErrTooManyRedirects)
}

// CheckHost 校验一个主机名（或字面量 IP）能不能作为出站目标：字面量 IP 直接判断，域名先解析，
// 任何一个解析结果落在禁止范围（内网、回环、链路本地等）、解析失败或没有结果都返回错误。
// 用于保存配置时的快速拒绝（如 SMTP 主机）；真正连接时仍要走 DialContext，避免“校验后 DNS 被换成内网”。
func (c *Config) CheckHost(ctx context.Context, host string) error {
	host = strings.TrimSuffix(strings.TrimSpace(host), ".")
	var ips []net.IP
	if ip := net.ParseIP(host); ip != nil {
		ips = []net.IP{ip}
	} else {
		var err error
		if ips, err = c.Resolver.LookupIP(ctx, host); err != nil {
			return fmt.Errorf("解析域名 %s 失败：%w", host, err)
		}
	}
	if len(ips) == 0 {
		return fmt.Errorf("域名 %s 没有解析结果", host)
	}
	for _, ip := range ips {
		if !c.IPAllowed(ip) {
			return fmt.Errorf("%w：%s 解析到 %s", ErrBlockedAddress, host, ip)
		}
	}
	return nil
}

// DialContext 先解析并校验再直接拨号到校验过的 IP（见 dialContext），供非 HTTP 的出站连接（如 SMTP）使用。
func (c *Config) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return c.dialContext(ctx, network, addr)
}
