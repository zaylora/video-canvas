package initialize

import (
	"net"

	"video-canvas/internal/config"
	"video-canvas/internal/provider/netguard"
)

// outboundIPPolicy 返回出站请求（插件宿主、Agent 大模型网关）的 IP 策略：默认 nil，沿用 netguard 的默认策略（拒绝全部内网）；
// 配置了 ai.allow_fake_ip（开发机）时放行 198.18.0.0/15，其余内网照拦。
func outboundIPPolicy(cfg *config.Config) func(net.IP) bool {
	if cfg.AI.AllowFakeIP {
		return netguard.IPAllowedWithFakeIP
	}
	return nil
}
