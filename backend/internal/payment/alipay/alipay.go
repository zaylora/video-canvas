// Package alipay 是支付宝电脑网站支付（alipay.trade.page.pay）渠道适配器，签名、验签、请求拼装交给官方推荐的
// github.com/smartwalle/alipay/v3，本包只做配置校验和协议结果到 payment 契约的转换。只支持公钥模式（RSA2），不支持证书模式。
package alipay

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	sdk "github.com/smartwalle/alipay/v3"

	"video-canvas/internal/payment"
	"video-canvas/internal/payment/payutil"
)

// Key 是本渠道的标识，和 model.ProviderAlipayPage 一致。
const Key = "alipay_page"

const (
	defaultGateway = "https://openapi.alipay.com/gateway.do"
	productCode    = "FAST_INSTANT_TRADE_PAY" // 电脑网站支付的销售产品码
	subCodeMissing = "ACQ.TRADE_NOT_EXIST"    // 交易不存在：用户还没打开付款页时支付宝就是这样回答
)

// Provider 是支付宝适配器，并发安全。
type Provider struct {
	client *http.Client
}

// New 创建适配器；client 应是带 SSRF 防护的出站客户端。
func New(client *http.Client) *Provider { return &Provider{client: client} }

var _ payment.Provider = (*Provider)(nil)

// Key 返回渠道标识。
func (*Provider) Key() string { return Key }

// Capabilities 返回渠道能力：能查单，能在渠道侧关单；RSA2 验签已足够，不需要再查单确认。
func (*Provider) Capabilities() payment.Capabilities {
	return payment.Capabilities{
		Methods: []string{payment.MethodAlipay}, Checkout: "redirect", CanQuery: true, CanCloseUpstream: true, ConfirmNotify: false,
	}
}

// Fields 返回配置字段定义。密钥可以粘贴 PEM（带 BEGIN/END 行），也可以粘贴支付宝开放平台给出的单行 base64。
func (*Provider) Fields() []payment.Field {
	return []payment.Field{
		{Key: "app_id", Label: "应用 AppID", Identity: true},
		{Key: "app_private_key", Label: "应用私钥（RSA2）", Secret: true},
		{Key: "alipay_public_key", Label: "支付宝公钥", Secret: true},
		{Key: "gateway", Label: "网关地址", Optional: true, Default: defaultGateway, Placeholder: "沙箱环境改成沙箱网关"},
	}
}

// Identity 返回商户身份：应用 AppID。
func (*Provider) Identity(cfg payment.Config, _ string) string {
	return strings.TrimSpace(cfg["app_id"])
}

// ValidateConfig 做本地校验：AppID 必填，两把密钥必须能解析，网关必须是 https。
func (*Provider) ValidateConfig(cfg payment.Config) error {
	if strings.TrimSpace(cfg["app_id"]) == "" {
		return errors.New("缺少应用 AppID")
	}
	if _, err := payutil.PrivateKeyPEM(cfg["app_private_key"]); err != nil {
		return fmt.Errorf("应用私钥无效：%w", err)
	}
	if _, err := payutil.PublicKeyPEM(cfg["alipay_public_key"]); err != nil {
		return fmt.Errorf("支付宝公钥无效：%w", err)
	}
	_, err := gateway(cfg)
	return err
}

// gateway 校验并返回网关地址（缺省官方正式网关）。
func gateway(cfg payment.Config) (string, error) {
	raw := strings.TrimSpace(cfg["gateway"])
	if raw == "" {
		return defaultGateway, nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("网关地址必须是 https:// 开头、不带用户名密码和参数的完整地址")
	}
	return raw, nil
}

// sdkClient 按配置创建 SDK 客户端并加载支付宝公钥。每次调用都新建：密钥解析很快，而且配置随时可能被管理员改。
func (p *Provider) sdkClient(cfg payment.Config) (*sdk.Client, error) {
	gw, err := gateway(cfg)
	if err != nil {
		return nil, err
	}
	priv, err := payutil.PrivateKeyPEM(cfg["app_private_key"])
	if err != nil {
		return nil, fmt.Errorf("应用私钥无效：%w", err)
	}
	pub, err := payutil.PublicKeyPEM(cfg["alipay_public_key"])
	if err != nil {
		return nil, fmt.Errorf("支付宝公钥无效：%w", err)
	}
	c, err := sdk.New(strings.TrimSpace(cfg["app_id"]), priv, true, sdk.WithHTTPClient(p.client), sdk.WithProductionGateway(gw))
	if err != nil {
		return nil, fmt.Errorf("初始化支付宝客户端失败：%w", err)
	}
	if err := c.LoadAliPayPublicKey(pub); err != nil {
		return nil, fmt.Errorf("加载支付宝公钥失败：%w", err)
	}
	return c, nil
}

// CreateOrder 生成电脑网站支付的跳转地址（签名后的 GET 链接），不需要请求支付宝。
func (p *Provider) CreateOrder(_ context.Context, cfg payment.Config, req payment.CreateReq) (payment.Checkout, error) {
	c, err := p.sdkClient(cfg)
	if err != nil {
		return payment.Checkout{}, err
	}
	var param sdk.TradePagePay
	param.NotifyURL, param.ReturnURL = req.NotifyURL, req.ReturnURL
	param.Subject, param.OutTradeNo, param.TotalAmount, param.ProductCode = req.Subject, req.OrderNo, payutil.YuanFromFen(req.AmountFen), productCode
	if !req.ExpiresAt.IsZero() {
		param.TimeExpire = payutil.FormatCNTime(req.ExpiresAt) // 绝对超时：用户过期后再付款，支付宝会直接拒绝
	}
	u, err := c.TradePagePay(param)
	if err != nil {
		return payment.Checkout{}, fmt.Errorf("生成支付宝付款链接失败：%w", err)
	}
	return payment.Checkout{Mode: payment.CheckoutRedirect, Value: u.String()}, nil
}

// bizError 提取支付宝的业务错误：有的错误随签名响应正常返回（rsp 的 code 非 10000），
// 有的没有签名、被 SDK 当作 error 返回。两种都归一成 *sdk.Error；其它错误（网络、验签失败）返回 nil。
func bizError(rspErr sdk.Error, err error) *sdk.Error {
	var ae *sdk.Error
	if errors.As(err, &ae) {
		return ae
	}
	if err == nil && !rspErr.IsSuccess() {
		return &rspErr
	}
	return nil
}

// QueryOrder 查单。支付宝回答交易不存在返回 StateNotExist；其它业务失败、验签失败、字段不一致都当作错误。
func (p *Provider) QueryOrder(ctx context.Context, cfg payment.Config, ref payment.OrderRef) (payment.TradeResult, error) {
	c, err := p.sdkClient(cfg)
	if err != nil {
		return payment.TradeResult{}, err
	}
	rsp, err := c.TradeQuery(ctx, sdk.TradeQuery{OutTradeNo: ref.OrderNo})
	var rspErr sdk.Error
	if rsp != nil {
		rspErr = rsp.Error
	}
	if be := bizError(rspErr, err); be != nil {
		if be.SubCode == subCodeMissing {
			return payment.TradeResult{OrderNo: ref.OrderNo, State: payment.StateNotExist}, nil
		}
		return payment.TradeResult{}, fmt.Errorf("支付宝查单失败：%s %s", be.SubCode, be.SubMsg)
	}
	if err != nil {
		return payment.TradeResult{}, fmt.Errorf("支付宝查单失败：%w", payutil.StripURLError(err))
	}
	return parseQuery(rsp, ref.OrderNo)
}

// parseQuery 把查单响应转成交易结果，并核对订单号；已付必须有交易号和合法金额。
func parseQuery(rsp *sdk.TradeQueryRsp, orderNo string) (payment.TradeResult, error) {
	if rsp.OutTradeNo != orderNo {
		return payment.TradeResult{}, errors.New("支付宝查单订单号不匹配")
	}
	res := payment.TradeResult{OrderNo: orderNo, TradeNo: rsp.TradeNo, PaidAt: payutil.ParseCNTime(rsp.SendPayDate)}
	switch rsp.TradeStatus {
	case sdk.TradeStatusWaitBuyerPay:
		res.State = payment.StatePending
		return res, nil
	case sdk.TradeStatusClosed:
		res.State = payment.StateClosed
		return res, nil
	case sdk.TradeStatusSuccess, sdk.TradeStatusFinished:
		res.State = payment.StatePaid
	default:
		return payment.TradeResult{}, fmt.Errorf("支付宝查单状态无法识别：%q", rsp.TradeStatus)
	}
	fen, err := payutil.FenFromYuan(rsp.TotalAmount)
	if err != nil {
		return payment.TradeResult{}, err
	}
	if res.TradeNo == "" {
		return payment.TradeResult{}, errors.New("支付宝已付订单缺少交易号")
	}
	res.PaidFen = fen
	return res, nil
}

// CloseOrder 在支付宝关单。交易不存在（用户从没打开过付款页）视为成功；交易已支付、状态不允许等其它失败都报错，
// 由 service 再查一次单决定是入账还是重试。
func (p *Provider) CloseOrder(ctx context.Context, cfg payment.Config, ref payment.OrderRef) error {
	c, err := p.sdkClient(cfg)
	if err != nil {
		return err
	}
	rsp, err := c.TradeClose(ctx, sdk.TradeClose{OutTradeNo: ref.OrderNo})
	var rspErr sdk.Error
	if rsp != nil {
		rspErr = rsp.Error
	}
	if be := bizError(rspErr, err); be != nil {
		if be.SubCode == subCodeMissing {
			return nil
		}
		return fmt.Errorf("支付宝关单失败：%s %s", be.SubCode, be.SubMsg)
	}
	if err != nil {
		return fmt.Errorf("支付宝关单失败：%w", payutil.StripURLError(err))
	}
	return nil
}

// VerifyNotify 验证异步通知：SDK 用支付宝公钥验 RSA2 签名，再核对 app_id 是我们的。
// 带 alipay_cert_sn 的通知一律拒绝：那是证书模式的标志，我们只支持公钥模式，
// 放行的话 SDK 会为了验签去访问支付宝下载证书，等于让陌生请求驱动我们发起出站请求。
func (p *Provider) VerifyNotify(ctx context.Context, cfg payment.Config, req payment.NotifyRequest) (payment.TradeResult, error) {
	vals, err := payutil.NotifyValues(req)
	if err != nil {
		return payment.TradeResult{}, fmt.Errorf("%w：%w", payment.ErrVerify, err)
	}
	if vals["alipay_cert_sn"] != "" {
		return payment.TradeResult{}, fmt.Errorf("%w：不支持证书模式的通知", payment.ErrVerify)
	}
	c, err := p.sdkClient(cfg)
	if err != nil {
		return payment.TradeResult{}, err
	}
	form := url.Values{}
	for k, v := range vals {
		form.Set(k, v)
	}
	n, err := c.DecodeNotification(ctx, form)
	if err != nil {
		return payment.TradeResult{}, fmt.Errorf("%w：%w", payment.ErrVerify, payutil.StripURLError(err))
	}
	if n.AppId != strings.TrimSpace(cfg["app_id"]) {
		return payment.TradeResult{}, fmt.Errorf("%w：app_id 不匹配", payment.ErrVerify)
	}
	fen, err := payutil.FenFromYuan(n.TotalAmount)
	if err != nil {
		return payment.TradeResult{}, fmt.Errorf("%w：%w", payment.ErrVerify, err)
	}
	res := payment.TradeResult{
		OrderNo: n.OutTradeNo, TradeNo: n.TradeNo, Identity: n.AppId, PaidFen: fen, State: payment.StatePending,
		PaidAt: payutil.ParseCNTime(n.GmtPayment), EventKey: "alipay:" + n.NotifyId,
	}
	switch n.TradeStatus {
	case sdk.TradeStatusSuccess, sdk.TradeStatusFinished:
		res.State = payment.StatePaid
	case sdk.TradeStatusClosed:
		res.State = payment.StateClosed
	}
	return res, nil
}

// NotifyAck 返回应答：支付宝要求成功时回复纯文本 success，否则按递增间隔重发。
func (*Provider) NotifyAck(ok bool) payment.Ack {
	if ok {
		return payment.Ack{Status: http.StatusOK, ContentType: "text/plain; charset=utf-8", Body: "success"}
	}
	return payment.Ack{Status: http.StatusBadRequest, ContentType: "text/plain; charset=utf-8", Body: "fail"}
}

// Probe 在线探测：查一个不存在的订单号。支付宝回答「交易不存在」说明 AppID、应用私钥签名、支付宝公钥验签都对得上；
// 回答验签出错、应用不存在等其它业务错误，则带上支付宝的原因返回。
func (p *Provider) Probe(ctx context.Context, cfg payment.Config) error {
	res, err := p.QueryOrder(ctx, cfg, payment.OrderRef{OrderNo: fmt.Sprintf("probe-%d", time.Now().UnixNano())})
	if err != nil {
		return err
	}
	if res.State != payment.StateNotExist {
		return errors.New("支付宝探测返回了意外的结果")
	}
	return nil
}
