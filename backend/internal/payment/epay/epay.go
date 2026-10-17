// Package epay 是易支付（彩虹易支付协议 V1，MD5 签名）渠道适配器，只用标准库。
// 协议：下单 POST {api_base}/mapi.php（表单），查单 GET {api_base}/api.php?act=order，异步通知 GET/POST 带 sign（MD5）。
// 协议没有关单接口，所以关单只能在本地做；通知验签后还要再查一次单确认（MD5 签名能被抓包重放）。
package epay

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"video-canvas/internal/payment"
	"video-canvas/internal/payment/payutil"
)

// Key 是本渠道的标识，和 model.ProviderEpay 一致。
const Key = "epay"

const (
	maxResponseBytes = 1 << 20 // 渠道响应最多读 1MB
	maxNameRunes     = 60      // 商品名最多的字数：部分站点对 name 长度敏感
)

// Provider 是易支付适配器，并发安全。
type Provider struct {
	client *http.Client
}

// New 创建适配器；client 应是带 SSRF 防护的出站客户端。
func New(client *http.Client) *Provider { return &Provider{client: client} }

var _ payment.Provider = (*Provider)(nil)

// Key 返回渠道标识。
func (*Provider) Key() string { return Key }

// Capabilities 返回渠道能力：能查单，不能在渠道侧关单，通知需查单确认。
func (*Provider) Capabilities() payment.Capabilities {
	return payment.Capabilities{
		Methods: []string{payment.MethodWechat, payment.MethodAlipay}, Checkout: "mixed",
		CanQuery: true, CanCloseUpstream: false, ConfirmNotify: true,
	}
}

// Fields 返回配置字段定义。
func (*Provider) Fields() []payment.Field {
	return []payment.Field{
		{Key: "api_base", Label: "接口地址", Placeholder: "https://pay.example.com"},
		{Key: "pid", Label: "商户 ID（pid）", Identity: true},
		{Key: "key", Label: "商户密钥", Secret: true},
		{Key: "wechat_type", Label: "微信支付通道名", Optional: true, Default: "wxpay", Placeholder: "不同站点的通道名可能不同"},
		{Key: "alipay_type", Label: "支付宝通道名", Optional: true, Default: "alipay", Placeholder: "不同站点的通道名可能不同"},
	}
}

// Identity 返回商户身份：易支付的 pid，微信和支付宝共用。
func (*Provider) Identity(cfg payment.Config, _ string) string { return strings.TrimSpace(cfg["pid"]) }

// ValidateConfig 做本地格式校验：必填项、接口地址必须是干净的 https 根地址。
// 要求 https 是因为查单接口把商户密钥放在 URL 的查询参数里，明文传输会泄露。
func (*Provider) ValidateConfig(cfg payment.Config) error {
	for _, k := range []string{"pid", "key"} {
		if strings.TrimSpace(cfg[k]) == "" {
			return fmt.Errorf("缺少 %s", k)
		}
	}
	_, err := apiBase(cfg)
	return err
}

// apiBase 校验并返回去掉末尾斜杠的接口地址。
func apiBase(cfg payment.Config) (string, error) {
	raw := strings.TrimSpace(cfg["api_base"])
	u, err := url.Parse(raw)
	switch {
	case raw == "" || err != nil:
		return "", errors.New("接口地址无效")
	case u.Scheme != "https":
		return "", errors.New("接口地址必须以 https:// 开头")
	case u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "":
		return "", errors.New("接口地址不能带用户名密码、查询参数或锚点")
	}
	return strings.TrimRight(raw, "/"), nil
}

// payType 返回支付方式对应的易支付通道名，配置里可以覆盖。
func payType(cfg payment.Config, method string) (string, error) {
	switch method {
	case payment.MethodWechat:
		return firstNonEmpty(cfg["wechat_type"], "wxpay"), nil
	case payment.MethodAlipay:
		return firstNonEmpty(cfg["alipay_type"], "alipay"), nil
	}
	return "", fmt.Errorf("易支付不支持的支付方式：%s", method)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

// sign 按协议签名：参数按键升序拼成 k=v&k=v（不含 sign、sign_type、空值），末尾直接拼商户密钥，取 MD5。
func sign(vals map[string]string, key string) string {
	return payutil.MD5Hex(payutil.Canonical(vals, "sign", "sign_type") + key)
}

// CreateOrder 向 mapi.php 下单。返回二维码内容或付款页地址。
func (p *Provider) CreateOrder(ctx context.Context, cfg payment.Config, req payment.CreateReq) (payment.Checkout, error) {
	base, err := apiBase(cfg)
	if err != nil {
		return payment.Checkout{}, err
	}
	typ, err := payType(cfg, req.Method)
	if err != nil {
		return payment.Checkout{}, err
	}
	vals := map[string]string{
		"pid": strings.TrimSpace(cfg["pid"]), "type": typ, "out_trade_no": req.OrderNo, "notify_url": req.NotifyURL,
		"return_url": req.ReturnURL, "name": subject(req.Subject, req.OrderNo), "money": payutil.YuanFromFen(req.AmountFen),
		"clientip": clientIP(req.ClientIP), "device": "pc",
	}
	vals["sign"] = sign(vals, strings.TrimSpace(cfg["key"]))
	vals["sign_type"] = "MD5"

	form := url.Values{}
	for k, v := range vals {
		form.Set(k, v)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/mapi.php", strings.NewReader(form.Encode()))
	if err != nil {
		return payment.Checkout{}, err
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.doJSON(httpReq)
	if err != nil {
		return payment.Checkout{}, err
	}
	if resp["code"] != "1" {
		return payment.Checkout{}, fmt.Errorf("易支付下单失败：%s", firstNonEmpty(resp["msg"], "code="+resp["code"]))
	}
	return pickCheckout(resp)
}

// pickCheckout 从下单响应里选收银方式。桌面端用户要的是「弹框里扫码」，所以能画成二维码的一律画二维码：
// qrcode 字段优先（两种支付方式都一样）；payurl 本身若是扫码链接（支付宝的 qr.alipay.com、微信的 weixin:// 与 wxp://）也当二维码；
// 剩下的 payurl 才是需要新开标签页的收银台页面。
func pickCheckout(resp map[string]string) (payment.Checkout, error) {
	qr, pay := strings.TrimSpace(resp["qrcode"]), firstNonEmpty(resp["payurl"], resp["urlscheme"])
	switch {
	case qr != "":
		return payment.Checkout{Mode: payment.CheckoutQRCode, Value: qr}, nil
	case pay != "" && isQRLink(pay):
		return payment.Checkout{Mode: payment.CheckoutQRCode, Value: pay}, nil
	case pay != "":
		return payment.Checkout{Mode: payment.CheckoutRedirect, Value: pay}, nil
	}
	return payment.Checkout{}, errors.New("易支付没有返回支付地址")
}

// isQRLink 判断一个地址是不是扫码支付链接：它本身就是二维码的内容，手机扫一下即可付款。
func isQRLink(link string) bool {
	if strings.HasPrefix(link, "weixin://") || strings.HasPrefix(link, "wxp://") {
		return true
	}
	u, err := url.Parse(link)
	return err == nil && u.Scheme == "https" && u.Hostname() == "qr.alipay.com"
}

// subject 整理商品名：去掉 % 和控制字符（部分站点会因此报错），超长截断，空则用订单号。
func subject(s, orderNo string) string {
	s = strings.NewReplacer("%", "", "\n", " ", "\r", " ").Replace(strings.TrimSpace(s))
	if r := []rune(s); len(r) > maxNameRunes {
		s = string(r[:maxNameRunes])
	}
	if s == "" {
		return "订单 " + orderNo
	}
	return s
}

// clientIP 返回合法的客户端 IP，否则用 0.0.0.0 占位：mapi 要求这个参数必填。
func clientIP(ip string) string {
	if net.ParseIP(strings.TrimSpace(ip)) == nil {
		return "0.0.0.0"
	}
	return strings.TrimSpace(ip)
}

// QueryOrder 查单。渠道回答「订单不存在」返回 StateNotExist；其它失败、字段缺失或不一致都当作错误，绝不猜测成已付。
func (p *Provider) QueryOrder(ctx context.Context, cfg payment.Config, ref payment.OrderRef) (payment.TradeResult, error) {
	base, err := apiBase(cfg)
	if err != nil {
		return payment.TradeResult{}, err
	}
	q := url.Values{"act": {"order"}, "pid": {strings.TrimSpace(cfg["pid"])}, "key": {strings.TrimSpace(cfg["key"])}, "out_trade_no": {ref.OrderNo}}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api.php?"+q.Encode(), nil)
	if err != nil {
		return payment.TradeResult{}, err
	}
	resp, err := p.doJSON(httpReq)
	if err != nil {
		return payment.TradeResult{}, err
	}
	if resp["code"] != "1" {
		if strings.Contains(resp["msg"], "不存在") && !strings.Contains(resp["msg"], "商户") {
			return payment.TradeResult{OrderNo: ref.OrderNo, State: payment.StateNotExist}, nil
		}
		return payment.TradeResult{}, fmt.Errorf("易支付查单失败：%s", firstNonEmpty(resp["msg"], "code="+resp["code"]))
	}
	return parseQuery(resp, cfg, ref.OrderNo)
}

// parseQuery 解析查单成功的响应，并核对商户号和订单号。
func parseQuery(resp map[string]string, cfg payment.Config, orderNo string) (payment.TradeResult, error) {
	if resp["pid"] != strings.TrimSpace(cfg["pid"]) {
		return payment.TradeResult{}, errors.New("易支付查单商户号不匹配")
	}
	if resp["out_trade_no"] != orderNo {
		return payment.TradeResult{}, errors.New("易支付查单订单号不匹配")
	}
	res := payment.TradeResult{OrderNo: orderNo, TradeNo: resp["trade_no"], Identity: resp["pid"], PaidAt: payutil.ParseCNTime(resp["endtime"])}
	switch resp["status"] {
	case "0":
		res.State = payment.StatePending
		return res, nil
	case "1":
		res.State = payment.StatePaid
	default:
		return payment.TradeResult{}, fmt.Errorf("易支付查单状态无法识别：%q", resp["status"])
	}
	fen, err := payutil.FenFromYuan(resp["money"])
	if err != nil {
		return payment.TradeResult{}, err
	}
	if res.TradeNo == "" {
		return payment.TradeResult{}, errors.New("易支付已付订单缺少支付流水号")
	}
	res.PaidFen = fen
	return res, nil
}

// CloseOrder 易支付没有关单接口，调用即报错（service 不会对本渠道调用它）。
func (*Provider) CloseOrder(context.Context, payment.Config, payment.OrderRef) error {
	return errors.New("易支付不支持渠道侧关单")
}

// VerifyNotify 验证异步通知：先核对签名类型和 MD5 签名（常量时间比较）和商户号，再解析金额。
// 任何一项不对都返回 ErrVerify；trade_status 不是 TRADE_SUCCESS 的通知验签通过但不算已付。
func (*Provider) VerifyNotify(_ context.Context, cfg payment.Config, req payment.NotifyRequest) (payment.TradeResult, error) {
	vals, err := payutil.NotifyValues(req)
	if err != nil {
		return payment.TradeResult{}, fmt.Errorf("%w：%w", payment.ErrVerify, err)
	}
	if st := strings.ToUpper(strings.TrimSpace(vals["sign_type"])); st != "" && st != "MD5" {
		return payment.TradeResult{}, fmt.Errorf("%w：签名类型 %s", payment.ErrVerify, st)
	}
	if !payutil.SafeEqual(strings.ToLower(strings.TrimSpace(vals["sign"])), sign(vals, strings.TrimSpace(cfg["key"]))) {
		return payment.TradeResult{}, payment.ErrVerify
	}
	if vals["pid"] != strings.TrimSpace(cfg["pid"]) {
		return payment.TradeResult{}, fmt.Errorf("%w：商户号不匹配", payment.ErrVerify)
	}
	fen, err := payutil.FenFromYuan(vals["money"])
	if err != nil {
		return payment.TradeResult{}, fmt.Errorf("%w：%w", payment.ErrVerify, err)
	}
	res := payment.TradeResult{
		OrderNo: vals["out_trade_no"], TradeNo: vals["trade_no"], Identity: vals["pid"], PaidFen: fen, State: payment.StatePending,
		EventKey: "epay:" + vals["out_trade_no"] + ":" + vals["trade_no"] + ":" + vals["trade_status"],
	}
	if vals["trade_status"] == "TRADE_SUCCESS" {
		res.State = payment.StatePaid
	}
	return res, nil
}

// NotifyAck 返回应答：易支付要求收到成功通知后回复纯文本 success，否则会反复重发。
func (*Provider) NotifyAck(ok bool) payment.Ack {
	if ok {
		return payment.Ack{Status: http.StatusOK, ContentType: "text/plain; charset=utf-8", Body: "success"}
	}
	return payment.Ack{Status: http.StatusBadRequest, ContentType: "text/plain; charset=utf-8", Body: "fail"}
}

// Probe 在线探测凭证：查一个不存在的订单号。渠道回答「订单不存在」说明商户号和密钥都被接受；
// 回答商户或密钥有问题、网关打不开都视为不可用，并带上渠道的提示。（不同站点的措辞可能不同，需在目标站点实测。）
func (p *Provider) Probe(ctx context.Context, cfg payment.Config) error {
	res, err := p.QueryOrder(ctx, cfg, payment.OrderRef{OrderNo: "probe-" + payutil.MD5Hex(cfg["pid"])[:12]})
	if err != nil {
		return err
	}
	if res.State != payment.StateNotExist {
		return errors.New("易支付探测返回了意外的结果")
	}
	return nil
}

// doJSON 发请求并把响应解析成扁平的字符串映射。HTTP 非 2xx、响应过大、不是 JSON 都报错；
// 网络错误去掉 URL（查单的 URL 里有密钥）。
func (p *Provider) doJSON(req *http.Request) (map[string]string, error) {
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("易支付请求失败：%w", payutil.StripURLError(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("易支付网关返回 HTTP %d", resp.StatusCode)
	}
	body, err := payutil.ReadLimited(resp.Body, maxResponseBytes)
	if err != nil {
		return nil, fmt.Errorf("读取易支付响应失败：%w", err)
	}
	return payutil.JSONStrings(body)
}
