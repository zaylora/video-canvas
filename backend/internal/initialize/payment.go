package initialize

import (
	"context"
	"net"
	"net/http"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"video-canvas/internal/handler"
	"video-canvas/internal/payment"
	"video-canvas/internal/payment/alipay"
	"video-canvas/internal/payment/epay"
	"video-canvas/internal/payment/wechat"
	"video-canvas/internal/payment/xunhupay"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/pkg/ws"
	"video-canvas/internal/provider/netguard"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

const (
	paymentMaintenanceInterval = 30 * time.Second // 补单与超时处理的间隔
	paymentRequestTimeout      = 20 * time.Second // 向支付渠道发请求的整体超时（单次请求，含重定向）
)

// paymentWiring 是支付模块装配出来的东西：两个 handler，以及后台补单任务要用的 service。
type paymentWiring struct {
	user  *handler.PaymentHandler
	admin *handler.PaymentAdminHandler
	svc   *service.PaymentService
}

// wirePayment 组装支付模块。向渠道发的请求统一走 netguard 的出站客户端：渠道网关地址是管理员填的，
// 必须挡住指向内网的地址；密钥复用 AI 配置的加密存储（ai_secrets）。
func wirePayment(db *gorm.DB, hub ws.Broadcaster, secrets service.PaymentSecretStore, initial service.InitialCreditsPolicy, audit service.AdminAuditWriter, outboundPolicy func(net.IP) bool) *paymentWiring {
	client := paymentHTTPClient(outboundPolicy)
	registry := payment.NewRegistry(
		alipay.New(client), wechat.New(client), epay.New(client), xunhupay.New(client),
	)
	repo := repository.NewPaymentRepository(db)
	settings := repository.NewSystemSettingRepository(db)
	svc := service.NewPaymentService(service.PaymentDeps{
		Orders: repo, Channels: repo, Products: repo, Settings: settings, Secrets: secrets, Registry: registry,
		Credits: initial, Broadcaster: hub, Audit: audit,
	})
	admin := service.NewPaymentAdminService(service.PaymentAdminDeps{
		Channels: repo, Products: repo, Orders: repo, Settings: settings, Secrets: secrets, Registry: registry, Audit: audit,
	})
	return &paymentWiring{user: handler.NewPaymentHandler(svc), admin: handler.NewPaymentAdminHandler(admin, svc), svc: svc}
}

// paymentHTTPClient 创建向支付渠道发请求的客户端：只允许 http(s)、拒绝内网地址、最多跟随 3 次重定向、整体超时 20 秒。
func paymentHTTPClient(policy func(net.IP) bool) *http.Client {
	cfg := &netguard.Config{IPAllowed: policy}
	cfg.ApplyDefaults()
	c := netguard.NewClient(netguard.NewTransport(cfg), nil, cfg.MaxRedirects)
	c.Timeout = paymentRequestTimeout
	return c
}

// runPayment 周期性地补单和处理超时订单，直到 ctx 结束。第一轮在启动 30 秒后：给数据库和渠道配置留出就绪时间。
// 每一轮处理的都是库里的状态，进程重启后自然接着做；多个实例同时跑也安全（见 PaymentService.RunMaintenance）。
func (a *App) runPayment(ctx context.Context) {
	ticker := time.NewTicker(paymentMaintenanceInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.runPaymentOnce(ctx)
		}
	}
}

// runPaymentOnce 跑一轮补单；任何一轮的异常都不能让整个循环退出，所以 recover 后记日志。
func (a *App) runPaymentOnce(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("支付补单任务异常", zap.Any("panic", r))
		}
	}()
	a.payment.RunMaintenance(ctx)
}
