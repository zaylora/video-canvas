// Package xunhupay 是虎皮椒支付渠道适配器，只用标准库。
// 协议（api.xunhupay.com）：下单 POST /payment/do.html，查单 POST /payment/query.html，都是 JSON 请求体，
// 每个请求带 hash = MD5(参数按键升序拼成 k=v&k=v + AppSecret)；异步通知同样带 hash，状态 OD 表示已付。
// 虎皮椒的微信和支付宝是两组独立的 AppID / AppSecret，后台按支付方式分别配置。协议没有关单接口。
package xunhupay

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"video-canvas/internal/payment"
	"video-canvas/internal/payment/payutil"
)

// Key 是本渠道的标识，和 model.ProviderXunhupay 一致。
const Key = "xunhupay"

const (
	defaultGateway   = "https://api.xunhupay.com"
	apiVersion       = "1.1"
	pluginName       = "video-canvas"
	maxResponseBytes = 1 << 20
	maxTitleRunes    = 60
)

// Provider 是虎皮椒适配器，并发安全。
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
		Methods: []string{payment.MethodWechat, payment.MethodAlipay}, Checkout: "qrcode",
		CanQuery: true, CanCloseUpstream: false, ConfirmNotify: true,
	}
}

// Fields 返回配置字段定义。两组凭证都是可选的，但至少要完整填一组（ValidateConfig 校验）。
func (*Provider) Fields() []payment.Field {
	return []payment.Field{
		{Key: "gateway", Label: "网关地址", Optional: true, Default: defaultGateway},
		{Key: "wechat_app_id", Label: "微信 AppID", Identity: true, Optional: true},
		{Key: "wechat_app_secret", Label: "微信 AppSecret", Secret: true, Optional: true},
		{Key: "alipay_app_id", Label: "支付宝 AppID", Identity: true, Optional: true},
		{Key: "alipay_app_secret", Label: "支付宝 AppSecret", Secret: true, Optional: true},
	}
}

// cred 是一组凭证。
type cred struct{ appID, secret string }

// credFor 返回支付方式对应的凭证；没配置返回 ok=false。
func credFor(cfg payment.Config, method string) (cred, bool) {
	var c cred
	switch method {
	case payment.MethodWechat:
		c = cred{strings.TrimSpace(cfg["wechat_app_id"]), strings.TrimSpace(cfg["wechat_app_secret"])}
	case payment.MethodAlipay:
		c = cred{strings.TrimSpace(cfg["alipay_app_id"]), strings.TrimSpace(cfg["alipay_app_secret"])}
	}
	return c, c.appID != "" && c.secret != ""
}

// credByAppID 按 AppID 找凭证，通知里只带 AppID，据此知道该用哪组密钥验签。
func credByAppID(cfg payment.Config, appID string) (cred, bool) {
	for _, m := range []string{payment.MethodWechat, payment.MethodAlipay} {
		if c, ok := credFor(cfg, m); ok && c.appID == appID {
			return c, true
		}
	}
	return cred{}, false
}

// Identity 返回指定支付方式的商户身份（AppID）。
func (*Provider) Identity(cfg payment.Config, method string) string {
	c, _ := credFor(cfg, method)
	return c.appID
}

// ValidateConfig 做本地格式校验：网关必须是干净的 https 根地址；每组凭证要么都不填、要么填全；至少一组。
func (*Provider) ValidateConfig(cfg payment.Config) error {
	if _, err := gateway(cfg); err != nil {
		return err
	}
	configured := 0
	for _, m := range []string{payment.MethodWechat, payment.MethodAlipay} {
		prefix := m + "_"
		id, sec := strings.TrimSpace(cfg[prefix+"app_id"]), strings.TrimSpace(cfg[prefix+"app_secret"])
		if (id == "") != (sec == "") {
			return fmt.Errorf("%s 的 AppID 和 AppSecret 要同时填写", methodName(m))
		}
		if id != "" {
			configured++
		}
	}
	if configured == 0 {
		return errors.New("微信和支付宝至少要配置一组凭证")
	}
	return nil
}

func methodName(m string) string {
	if m == payment.MethodWechat {
		return "微信"
	}
	return "支付宝"
}

// gateway 校验并返回网关地址（缺省官方地址）。
func gateway(cfg payment.Config) (string, error) {
	raw := strings.TrimSpace(cfg["gateway"])
	if raw == "" {
		return defaultGateway, nil
	}
	u, err := url.Parse(raw)
	switch {
	case err != nil || u.Scheme != "https" || u.Host == "":
		return "", errors.New("网关地址必须以 https:// 开头")
	case u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "":
		return "", errors.New("网关地址只能是域名，不能带用户名密码、路径或参数")
	}
	return strings.TrimRight(raw, "/"), nil
}

// hash 按协议签名：参数按键升序拼成 k=v&k=v（不含 hash、空值），末尾直接拼 AppSecret，取 MD5。
func hash(vals map[string]string, secret string) string {
	return payutil.MD5Hex(payutil.Canonical(vals, "hash") + secret)
}

// flex 兼容渠道对同一字段时而返回字符串、时而返回数字的情况。
type flex string

// UnmarshalJSON 接受字符串、数字和 null。
func (f *flex) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	switch {
	case len(b) == 0 || string(b) == "null":
		*f = ""
	case b[0] == '"':
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*f = flex(strings.TrimSpace(s))
	default:
		*f = flex(string(b))
	}
	return nil
}

type envelope struct {
	ErrCode flex   `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

// check 业务错误码非 0 时返回错误。
func (e envelope) check() error {
	if e.ErrCode == "" || e.ErrCode == "0" {
		return nil
	}
	return &apiError{code: string(e.ErrCode), msg: strings.TrimSpace(e.ErrMsg)}
}

// apiError 是渠道返回的业务错误。
type apiError struct{ code, msg string }

func (e *apiError) Error() string {
	return fmt.Sprintf("虎皮椒返回错误 %s：%s", e.code, e.msg)
}

// notExist 判断错误是不是「订单不存在」。
func (e *apiError) notExist() bool {
	m := strings.ToLower(e.msg)
	return strings.Contains(m, "不存在") || strings.Contains(m, "not exist") || strings.Contains(m, "not found") || strings.Contains(m, "未找到")
}

type payResp struct {
	envelope
	URLQrcode string `json:"url_qrcode"`
	URL       string `json:"url"`
}

type queryResp struct {
	envelope
	Data struct {
		Status        flex   `json:"status"`
		TotalFee      flex   `json:"total_fee"`
		TotalAmount   flex   `json:"total_amount"`
		TransactionID flex   `json:"transaction_id"`
		OpenOrderID   flex   `json:"open_order_id"`
		OutTradeOrder flex   `json:"out_trade_order"`
		PaidDate      string `json:"paid_date"`
	} `json:"data"`
}

// CreateOrder 下单。优先返回渠道已经画好的二维码图片，没有则退到付款页跳转。
func (p *Provider) CreateOrder(ctx context.Context, cfg payment.Config, req payment.CreateReq) (payment.Checkout, error) {
	c, ok := credFor(cfg, req.Method)
	if !ok {
		return payment.Checkout{}, fmt.Errorf("未配置%s的 AppID 和 AppSecret", methodName(req.Method))
	}
	params := map[string]string{
		"version": apiVersion, "appid": c.appID, "trade_order_id": req.OrderNo, "total_fee": payutil.YuanFromFen(req.AmountFen),
		"title": title(req.Subject, req.OrderNo), "time": strconv.FormatInt(time.Now().Unix(), 10), "notify_url": req.NotifyURL,
		"plugins": pluginName, "nonce_str": nonce(),
	}
	if req.ReturnURL != "" {
		params["return_url"] = req.ReturnURL
		params["callback_url"] = req.ReturnURL
	}
	var out payResp
	if err := p.call(ctx, cfg, "/payment/do.html", params, c.secret, &out); err != nil {
		return payment.Checkout{}, err
	}
	switch {
	case strings.TrimSpace(out.URLQrcode) != "":
		return payment.Checkout{Mode: payment.CheckoutQRCodeImage, Value: strings.TrimSpace(out.URLQrcode)}, nil
	case strings.TrimSpace(out.URL) != "":
		return payment.Checkout{Mode: payment.CheckoutRedirect, Value: strings.TrimSpace(out.URL)}, nil
	}
	return payment.Checkout{}, errors.New("虎皮椒没有返回二维码或付款页")
}

// QueryOrder 查单：OD 已付，WP 待付，CD 已关闭。渠道回答订单不存在返回 StateNotExist。
func (p *Provider) QueryOrder(ctx context.Context, cfg payment.Config, ref payment.OrderRef) (payment.TradeResult, error) {
	c, ok := credFor(cfg, ref.Method)
	if !ok {
		return payment.TradeResult{}, fmt.Errorf("未配置%s的 AppID 和 AppSecret", methodName(ref.Method))
	}
	params := map[string]string{
		"appid": c.appID, "out_trade_order": ref.OrderNo, "time": strconv.FormatInt(time.Now().Unix(), 10), "nonce_str": nonce(),
	}
	var out queryResp
	if err := p.call(ctx, cfg, "/payment/query.html", params, c.secret, &out); err != nil {
		var ae *apiError
		if errors.As(err, &ae) && ae.notExist() {
			return payment.TradeResult{OrderNo: ref.OrderNo, State: payment.StateNotExist}, nil
		}
		return payment.TradeResult{}, err
	}
	return parseQuery(&out, c.appID, ref.OrderNo)
}

// parseQuery 解析查单响应并核对订单号；已付必须有交易号和合法金额。
func parseQuery(out *queryResp, appID, orderNo string) (payment.TradeResult, error) {
	d := out.Data
	if d.OutTradeOrder != "" && string(d.OutTradeOrder) != orderNo {
		return payment.TradeResult{}, errors.New("虎皮椒查单订单号不匹配")
	}
	res := payment.TradeResult{
		OrderNo: orderNo, TradeNo: firstNonEmpty(string(d.TransactionID), string(d.OpenOrderID)), Identity: appID,
		PaidAt: payutil.ParseCNTime(d.PaidDate),
	}
	switch d.Status {
	case "WP":
		res.State = payment.StatePending
		return res, nil
	case "CD":
		res.State = payment.StateClosed
		return res, nil
	case "OD":
		res.State = payment.StatePaid
	default:
		return payment.TradeResult{}, fmt.Errorf("虎皮椒查单状态无法识别：%q", string(d.Status))
	}
	fen, err := payutil.FenFromYuan(firstNonEmpty(string(d.TotalFee), string(d.TotalAmount)))
	if err != nil {
		return payment.TradeResult{}, err
	}
	if res.TradeNo == "" {
		return payment.TradeResult{}, errors.New("虎皮椒已付订单缺少交易号")
	}
	res.PaidFen = fen
	return res, nil
}

// CloseOrder 虎皮椒没有关单接口，调用即报错（service 不会对本渠道调用它）。
func (*Provider) CloseOrder(context.Context, payment.Config, payment.OrderRef) error {
	return errors.New("虎皮椒不支持渠道侧关单")
}

// VerifyNotify 验证异步通知：按通知里的 AppID 找到对应那组凭证，用它的 AppSecret 重算 hash（常量时间比较）。
// AppID 不是我们配置的、hash 不对、参数重复都返回 ErrVerify。状态 OD 才算已付。
func (*Provider) VerifyNotify(_ context.Context, cfg payment.Config, req payment.NotifyRequest) (payment.TradeResult, error) {
	vals, err := payutil.NotifyValues(req)
	if err != nil {
		return payment.TradeResult{}, fmt.Errorf("%w：%w", payment.ErrVerify, err)
	}
	c, ok := credByAppID(cfg, vals["appid"])
	if !ok {
		return payment.TradeResult{}, fmt.Errorf("%w：AppID 不是本站配置的", payment.ErrVerify)
	}
	if !payutil.SafeEqual(strings.ToLower(strings.TrimSpace(vals["hash"])), hash(vals, c.secret)) {
		return payment.TradeResult{}, payment.ErrVerify
	}
	fen, err := payutil.FenFromYuan(firstNonEmpty(vals["total_fee"], vals["total_amount"]))
	if err != nil {
		return payment.TradeResult{}, fmt.Errorf("%w：%w", payment.ErrVerify, err)
	}
	orderNo := firstNonEmpty(vals["trade_order_id"], vals["out_trade_order"])
	tradeNo := firstNonEmpty(vals["transaction_id"], vals["open_order_id"])
	res := payment.TradeResult{
		OrderNo: orderNo, TradeNo: tradeNo, Identity: c.appID, PaidFen: fen, State: payment.StatePending,
		EventKey: "xunhupay:" + firstNonEmpty(tradeNo, orderNo) + ":" + vals["status"] + ":" + vals["time"],
	}
	switch vals["status"] {
	case "OD":
		res.State = payment.StatePaid
	case "CD":
		res.State = payment.StateClosed
	}
	return res, nil
}

// NotifyAck 返回应答：收到成功通知后回复纯文本 success，否则渠道会反复重发。
func (*Provider) NotifyAck(ok bool) payment.Ack {
	if ok {
		return payment.Ack{Status: http.StatusOK, ContentType: "text/plain; charset=utf-8", Body: "success"}
	}
	return payment.Ack{Status: http.StatusBadRequest, ContentType: "text/plain; charset=utf-8", Body: "fail"}
}

// Probe 在线探测：对每组已配置的凭证查一个不存在的订单号，渠道回答「订单不存在」说明 AppID 和签名都被接受。
// （「订单不存在」的措辞需在真实网关实测。）
func (p *Provider) Probe(ctx context.Context, cfg payment.Config) error {
	for _, m := range []string{payment.MethodWechat, payment.MethodAlipay} {
		if _, ok := credFor(cfg, m); !ok {
			continue
		}
		res, err := p.QueryOrder(ctx, cfg, payment.OrderRef{OrderNo: "probe-" + nonce(), Method: m})
		if err != nil {
			return fmt.Errorf("%s：%w", methodName(m), err)
		}
		if res.State != payment.StateNotExist {
			return fmt.Errorf("%s：探测返回了意外的结果", methodName(m))
		}
	}
	return nil
}

// call 给参数签名后以 JSON 请求体 POST 到网关，把响应解析进 out，并检查业务错误码。
func (p *Provider) call(ctx context.Context, cfg payment.Config, path string, params map[string]string, secret string, out interface{ check() error }) error {
	base, err := gateway(cfg)
	if err != nil {
		return err
	}
	params["hash"] = hash(params, secret)
	body, err := json.Marshal(params)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("虎皮椒请求失败：%w", payutil.StripURLError(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("虎皮椒网关返回 HTTP %d", resp.StatusCode)
	}
	raw, err := payutil.ReadLimited(resp.Body, maxResponseBytes)
	if err != nil {
		return fmt.Errorf("读取虎皮椒响应失败：%w", err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("虎皮椒响应不是合法的 JSON：%w", err)
	}
	return out.check()
}

// title 整理商品标题：去掉控制字符，超长截断，空则用订单号。
func title(s, orderNo string) string {
	s = strings.NewReplacer("\n", " ", "\r", " ").Replace(strings.TrimSpace(s))
	if r := []rune(s); len(r) > maxTitleRunes {
		s = string(r[:maxTitleRunes])
	}
	if s == "" {
		return "订单 " + orderNo
	}
	return s
}

// nonce 返回 16 位随机十六进制串。
func nonce() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil { // crypto/rand 在受支持的平台上不会失败；失败时退回时间戳，仍然是唯一的
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}
