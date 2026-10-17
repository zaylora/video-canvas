// Package wechat 是微信支付 Native（扫码，APIv3）渠道适配器。请求签名、响应验签、回调验签与解密交给微信官方
// github.com/wechatpay-apiv3/wechatpay-go，本包只做配置校验和协议结果到 payment 契约的转换。
// 验签用「微信支付公钥」模式（商户平台下载的公钥 + 公钥 ID），不用平台证书，所以运行时不会为了更新证书去访问微信。
package wechat

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/wechatpay-apiv3/wechatpay-go/core"
	"github.com/wechatpay-apiv3/wechatpay-go/core/auth/verifiers"
	"github.com/wechatpay-apiv3/wechatpay-go/core/notify"
	"github.com/wechatpay-apiv3/wechatpay-go/core/option"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments/native"

	"video-canvas/internal/payment"
	"video-canvas/internal/payment/payutil"
)

// Key 是本渠道的标识，和 model.ProviderWechatNative 一致。
const Key = "wechat_native"

const (
	apiV3KeyLen    = 32 // APIv3 密钥固定 32 字节
	maxDescRunes   = 40 // 商品描述最多的字数：微信限制 127 字节，按中文每字 3 字节留足余量
	errOrderAbsent = "ORDER_NOT_EXIST"
)

// Provider 是微信支付 Native 适配器，并发安全。
type Provider struct {
	client *http.Client
}

// New 创建适配器；client 应是带 SSRF 防护的出站客户端。
func New(client *http.Client) *Provider { return &Provider{client: client} }

var _ payment.Provider = (*Provider)(nil)

// Key 返回渠道标识。
func (*Provider) Key() string { return Key }

// Capabilities 返回渠道能力：能查单，能在渠道侧关单；签名加解密已足够，不需要再查单确认。
func (*Provider) Capabilities() payment.Capabilities {
	return payment.Capabilities{
		Methods: []string{payment.MethodWechat}, Checkout: "qrcode", CanQuery: true, CanCloseUpstream: true, ConfirmNotify: false,
	}
}

// Fields 返回配置字段定义。
func (*Provider) Fields() []payment.Field {
	return []payment.Field{
		{Key: "app_id", Label: "AppID", Identity: true},
		{Key: "mch_id", Label: "商户号", Identity: true},
		{Key: "merchant_serial_no", Label: "商户 API 证书序列号"},
		{Key: "wechatpay_public_key_id", Label: "微信支付公钥 ID", Placeholder: "PUB_KEY_ID_ 开头"},
		{Key: "merchant_private_key", Label: "商户 API 私钥", Secret: true},
		{Key: "api_v3_key", Label: "APIv3 密钥", Secret: true},
		{Key: "wechatpay_public_key", Label: "微信支付公钥", Secret: true},
	}
}

// Identity 返回商户身份：商户号。
func (*Provider) Identity(cfg payment.Config, _ string) string {
	return strings.TrimSpace(cfg["mch_id"])
}

// ValidateConfig 做本地校验：必填项、APIv3 密钥长度、两把密钥必须能解析。
func (*Provider) ValidateConfig(cfg payment.Config) error {
	for k, label := range map[string]string{
		"app_id": "AppID", "mch_id": "商户号", "merchant_serial_no": "商户 API 证书序列号", "wechatpay_public_key_id": "微信支付公钥 ID",
	} {
		if strings.TrimSpace(cfg[k]) == "" {
			return fmt.Errorf("缺少%s", label)
		}
	}
	if len(strings.TrimSpace(cfg["api_v3_key"])) != apiV3KeyLen {
		return fmt.Errorf("APIv3 密钥必须是 %d 位", apiV3KeyLen)
	}
	if _, err := payutil.RSAPrivateKey(cfg["merchant_private_key"]); err != nil {
		return fmt.Errorf("商户 API 私钥无效：%w", err)
	}
	if _, err := payutil.RSAPublicKey(cfg["wechatpay_public_key"]); err != nil {
		return fmt.Errorf("微信支付公钥无效：%w", err)
	}
	return nil
}

// sdkClient 按配置创建 SDK 客户端：商户私钥签名，微信支付公钥验签。
func (p *Provider) sdkClient(ctx context.Context, cfg payment.Config) (*core.Client, error) {
	priv, err := payutil.RSAPrivateKey(cfg["merchant_private_key"])
	if err != nil {
		return nil, fmt.Errorf("商户 API 私钥无效：%w", err)
	}
	pub, err := payutil.RSAPublicKey(cfg["wechatpay_public_key"])
	if err != nil {
		return nil, fmt.Errorf("微信支付公钥无效：%w", err)
	}
	c, err := core.NewClient(ctx,
		option.WithWechatPayPublicKeyAuthCipher(strings.TrimSpace(cfg["mch_id"]), strings.TrimSpace(cfg["merchant_serial_no"]), priv, strings.TrimSpace(cfg["wechatpay_public_key_id"]), pub),
		option.WithHTTPClient(p.client),
	)
	if err != nil {
		return nil, fmt.Errorf("初始化微信支付客户端失败：%w", err)
	}
	return c, nil
}

// wxError 把 SDK 的错误整理成可读的错误：业务错误带上微信的错误码和说明；其它错误去掉 URL。
func wxError(action string, err error) error {
	var ae *core.APIError
	if errors.As(err, &ae) {
		return fmt.Errorf("微信支付%s失败：%s %s", action, ae.Code, ae.Message)
	}
	return fmt.Errorf("微信支付%s失败：%w", action, payutil.StripURLError(err))
}

// isAbsent 判断错误是不是「订单不存在」。
func isAbsent(err error) bool {
	var ae *core.APIError
	return errors.As(err, &ae) && ae.Code == errOrderAbsent
}

// CreateOrder 调用 Native 下单接口，返回二维码内容（code_url）。
func (p *Provider) CreateOrder(ctx context.Context, cfg payment.Config, req payment.CreateReq) (payment.Checkout, error) {
	c, err := p.sdkClient(ctx, cfg)
	if err != nil {
		return payment.Checkout{}, err
	}
	pr := native.PrepayRequest{
		Appid: core.String(strings.TrimSpace(cfg["app_id"])), Mchid: core.String(strings.TrimSpace(cfg["mch_id"])),
		Description: core.String(description(req.Subject, req.OrderNo)), OutTradeNo: core.String(req.OrderNo),
		NotifyUrl: core.String(req.NotifyURL), Amount: &native.Amount{Total: core.Int64(int64(req.AmountFen)), Currency: core.String("CNY")},
	}
	if !req.ExpiresAt.IsZero() {
		pr.TimeExpire = core.Time(payutil.InCN(req.ExpiresAt))
	}
	resp, _, err := (&native.NativeApiService{Client: c}).Prepay(ctx, pr)
	if err != nil {
		return payment.Checkout{}, wxError("下单", err)
	}
	if resp == nil || resp.CodeUrl == nil || strings.TrimSpace(*resp.CodeUrl) == "" {
		return payment.Checkout{}, errors.New("微信支付没有返回二维码")
	}
	return payment.Checkout{Mode: payment.CheckoutQRCode, Value: strings.TrimSpace(*resp.CodeUrl)}, nil
}

// description 整理商品描述：超长截断，空则用订单号。
func description(s, orderNo string) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > maxDescRunes {
		s = string(r[:maxDescRunes])
	}
	if s == "" {
		return "订单 " + orderNo
	}
	return s
}

// QueryOrder 查单。微信回答订单不存在返回 StateNotExist；已付后又退款（REFUND）、字段不一致等无法自动处理的情况返回错误，
// 绝不猜测成已付：REFUND 的订单入账等于给已退款的订单发积分。
func (p *Provider) QueryOrder(ctx context.Context, cfg payment.Config, ref payment.OrderRef) (payment.TradeResult, error) {
	c, err := p.sdkClient(ctx, cfg)
	if err != nil {
		return payment.TradeResult{}, err
	}
	tx, _, err := (&native.NativeApiService{Client: c}).QueryOrderByOutTradeNo(ctx, native.QueryOrderByOutTradeNoRequest{
		OutTradeNo: core.String(ref.OrderNo), Mchid: core.String(strings.TrimSpace(cfg["mch_id"])),
	})
	if isAbsent(err) {
		return payment.TradeResult{OrderNo: ref.OrderNo, State: payment.StateNotExist}, nil
	}
	if err != nil {
		return payment.TradeResult{}, wxError("查单", err)
	}
	return parseTransaction(tx, cfg, ref.OrderNo)
}

// parseTransaction 把微信的交易对象转成交易结果，并核对订单号和商户号；已付必须有交易号和金额。
func parseTransaction(tx *payments.Transaction, cfg payment.Config, orderNo string) (payment.TradeResult, error) {
	if tx == nil || deref(tx.OutTradeNo) != orderNo {
		return payment.TradeResult{}, errors.New("微信支付查单订单号不匹配")
	}
	mch := strings.TrimSpace(cfg["mch_id"])
	if deref(tx.Mchid) != mch {
		return payment.TradeResult{}, errors.New("微信支付查单商户号不匹配")
	}
	res := payment.TradeResult{OrderNo: orderNo, TradeNo: deref(tx.TransactionId), Identity: mch}
	if t, err := time.Parse(time.RFC3339, deref(tx.SuccessTime)); err == nil {
		res.PaidAt = t
	}
	switch deref(tx.TradeState) {
	case "NOTPAY", "USERPAYING":
		res.State = payment.StatePending
		return res, nil
	case "CLOSED", "REVOKED", "PAYERROR":
		res.State = payment.StateClosed
		return res, nil
	case "SUCCESS":
		res.State = payment.StatePaid
	case "REFUND":
		return payment.TradeResult{}, errors.New("微信支付订单已转入退款，请在商户平台人工核实")
	default:
		return payment.TradeResult{}, fmt.Errorf("微信支付订单状态无法识别：%q", deref(tx.TradeState))
	}
	if tx.Amount == nil || tx.Amount.Total == nil || res.TradeNo == "" {
		return payment.TradeResult{}, errors.New("微信支付已付订单缺少金额或交易号")
	}
	res.PaidFen = int(*tx.Amount.Total)
	return res, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// CloseOrder 在微信关单。订单不存在（用户没扫过码）和订单已关闭都视为成功；订单已支付等其它失败报错，
// 由 service 再查一次单决定是入账还是重试。
func (p *Provider) CloseOrder(ctx context.Context, cfg payment.Config, ref payment.OrderRef) error {
	c, err := p.sdkClient(ctx, cfg)
	if err != nil {
		return err
	}
	_, err = (&native.NativeApiService{Client: c}).CloseOrder(ctx, native.CloseOrderRequest{
		OutTradeNo: core.String(ref.OrderNo), Mchid: core.String(strings.TrimSpace(cfg["mch_id"])),
	})
	var ae *core.APIError
	if err != nil && errors.As(err, &ae) && (ae.Code == errOrderAbsent || ae.Code == "ORDERCLOSED") {
		return nil
	}
	if err != nil {
		return wxError("关单", err)
	}
	return nil
}

// VerifyNotify 验证异步通知：SDK 用微信支付公钥验请求头里的签名（含时间戳新鲜度），再用 APIv3 密钥解密 resource。
// 验签或解密失败、商户号或 AppID 不是我们的，都返回 ErrVerify。SDK 的错误文本会带上整个请求，这里只保留前面的原因。
func (*Provider) VerifyNotify(ctx context.Context, cfg payment.Config, req payment.NotifyRequest) (payment.TradeResult, error) {
	pub, err := payutil.RSAPublicKey(cfg["wechatpay_public_key"])
	if err != nil {
		return payment.TradeResult{}, err
	}
	handler, err := notify.NewRSANotifyHandler(strings.TrimSpace(cfg["api_v3_key"]),
		verifiers.NewSHA256WithRSAPubkeyVerifier(strings.TrimSpace(cfg["wechatpay_public_key_id"]), *pub))
	if err != nil {
		return payment.TradeResult{}, err
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, "/", bytes.NewReader(req.Body))
	if err != nil {
		return payment.TradeResult{}, err
	}
	hr.Header = req.Header.Clone()
	var tx payments.Transaction
	nr, err := handler.ParseNotifyRequest(ctx, hr, &tx)
	if err != nil {
		return payment.TradeResult{}, fmt.Errorf("%w：%s", payment.ErrVerify, brief(err))
	}
	if deref(tx.Mchid) != strings.TrimSpace(cfg["mch_id"]) || deref(tx.Appid) != strings.TrimSpace(cfg["app_id"]) {
		return payment.TradeResult{}, fmt.Errorf("%w：商户号或 AppID 不匹配", payment.ErrVerify)
	}
	res := payment.TradeResult{
		OrderNo: deref(tx.OutTradeNo), TradeNo: deref(tx.TransactionId), Identity: deref(tx.Mchid), State: payment.StatePending,
		EventKey: "wxpay:" + nr.ID,
	}
	if t, perr := time.Parse(time.RFC3339, deref(tx.SuccessTime)); perr == nil {
		res.PaidAt = t
	}
	if tx.Amount != nil && tx.Amount.Total != nil {
		res.PaidFen = int(*tx.Amount.Total)
	}
	if deref(tx.TradeState) == "SUCCESS" {
		res.State = payment.StatePaid
	}
	return res, nil
}

// brief 取错误文本里「request:」之前的原因，并限制长度：SDK 会把整个 HTTP 请求打印进错误文本。
func brief(err error) string {
	msg := err.Error()
	if i := strings.Index(msg, ", request:"); i > 0 {
		msg = msg[:i]
	}
	if r := []rune(msg); len(r) > 160 {
		msg = string(r[:160])
	}
	return msg
}

// NotifyAck 返回应答：微信要求成功时 HTTP 200 加 JSON {"code":"SUCCESS"}；失败必须是 4xx/5xx，微信才会按间隔重发。
func (*Provider) NotifyAck(ok bool) payment.Ack {
	if ok {
		return payment.Ack{Status: http.StatusOK, ContentType: "application/json", Body: `{"code":"SUCCESS","message":"成功"}`}
	}
	return payment.Ack{Status: http.StatusInternalServerError, ContentType: "application/json", Body: `{"code":"FAIL","message":"失败"}`}
}

// Probe 在线探测：查一个不存在的订单号。微信回答「订单不存在」说明商户号、证书序列号、商户私钥签名和微信支付公钥验签都对得上；
// 回答签名错误、商户号不存在等其它错误，则带上微信的原因返回。
func (p *Provider) Probe(ctx context.Context, cfg payment.Config) error {
	res, err := p.QueryOrder(ctx, cfg, payment.OrderRef{OrderNo: fmt.Sprintf("probe-%d", time.Now().UnixNano())})
	if err != nil {
		return err
	}
	if res.State != payment.StateNotExist {
		return errors.New("微信支付探测返回了意外的结果")
	}
	return nil
}
