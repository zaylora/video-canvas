package payment_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"

	"video-canvas/internal/payment"
	"video-canvas/internal/payment/alipay"
)

// 测试里扮演两方：merchant 是我们的应用私钥（SDK 用它给请求签名），gateway 是「支付宝」（测试用它给响应和通知签名）。
// 响应和通知的签名在测试里按支付宝文档的规则独立实现，不借助 SDK：这样才能证明 SDK 和协议规则是对得上的。
var (
	aliKeysOnce sync.Once
	aliMerchant *rsa.PrivateKey
	aliGateway  *rsa.PrivateKey
)

func aliKeys(t *testing.T) (merchant, gateway *rsa.PrivateKey) {
	t.Helper()
	aliKeysOnce.Do(func() {
		var err error
		if aliMerchant, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
			panic(err)
		}
		if aliGateway, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
			panic(err)
		}
	})
	return aliMerchant, aliGateway
}

func pemPKCS8(t *testing.T, k *rsa.PrivateKey) string {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

func pemPKIX(t *testing.T, k *rsa.PublicKey) string {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(k)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

func rsaSign(t *testing.T, k *rsa.PrivateKey, content string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(content))
	sig, err := rsa.SignPKCS1v15(rand.Reader, k, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(sig)
}

// aliCanonical 是支付宝的待签名串：参数按键升序拼成 k=v&k=v，跳过空值和 exclude 里的键。
// 注意两个方向规则不同：验通知和响应时 sign 与 sign_type 都不参与；而我们发出的请求签名里 sign_type 参与、只有 sign 不参与。
func aliCanonical(vals url.Values, exclude ...string) string {
	skip := map[string]bool{}
	for _, k := range exclude {
		skip[k] = true
	}
	var keys []string
	for k := range vals {
		if !skip[k] && vals.Get(k) != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + vals.Get(k)
	}
	return strings.Join(parts, "&")
}

func aliConfig(t *testing.T, gatewayURL string) payment.Config {
	merchant, gateway := aliKeys(t)
	return payment.Config{
		"app_id": "2021004123456789", "app_private_key": pemPKCS8(t, merchant), "alipay_public_key": pemPKIX(t, &gateway.PublicKey),
		"gateway": gatewayURL,
	}
}

// signedResponse 按支付宝网关的响应格式拼出带签名的响应：签名覆盖业务对象的原始文本。
func signedResponse(t *testing.T, api, biz string) string {
	_, gateway := aliKeys(t)
	return `{"` + api + `_response":` + biz + `,"sign":"` + rsaSign(t, gateway, biz) + `"}`
}

func newAliServer(t *testing.T, h http.HandlerFunc) (*alipay.Provider, payment.Config) {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	return alipay.New(srv.Client()), aliConfig(t, srv.URL+"/gateway.do")
}

func TestAlipay_MetaAndValidate(t *testing.T) {
	p := alipay.New(http.DefaultClient)
	caps := p.Capabilities()
	if p.Key() != "alipay_page" || !caps.CanQuery || !caps.CanCloseUpstream || caps.ConfirmNotify || len(caps.Methods) != 1 || caps.Methods[0] != payment.MethodAlipay {
		t.Fatalf("%+v", caps)
	}
	cfg := aliConfig(t, "")
	if p.Identity(cfg, payment.MethodAlipay) != "2021004123456789" {
		t.Fatal("商户身份应是 app_id")
	}
	if err := p.ValidateConfig(cfg); err != nil {
		t.Fatalf("合法配置不应报错：%v", err)
	}
	bad := map[string]func(c payment.Config){
		"缺 app_id":   func(c payment.Config) { delete(c, "app_id") },
		"应用私钥无效":     func(c payment.Config) { c["app_private_key"] = "not a key" },
		"支付宝公钥无效":    func(c payment.Config) { c["alipay_public_key"] = "not a key" },
		"缺应用私钥":      func(c payment.Config) { delete(c, "app_private_key") },
		"缺支付宝公钥":     func(c payment.Config) { delete(c, "alipay_public_key") },
		"网关不是 https": func(c payment.Config) { c["gateway"] = "http://openapi.alipay.com/gateway.do" },
		"网关带用户名密码":   func(c payment.Config) { c["gateway"] = "https://u:p@openapi.alipay.com/gateway.do" },
	}
	for name, mutate := range bad {
		c := aliConfig(t, "")
		mutate(c)
		if err := p.ValidateConfig(c); err == nil {
			t.Errorf("%s：应报错", name)
		}
	}
}

func TestAlipay_RawBase64Keys(t *testing.T) {
	// 支付宝开放平台给出的密钥是没有 PEM 头尾的单行 base64，运营会直接粘贴，必须能用；
	// 应用私钥还常见 PKCS#1（RSA PRIVATE KEY）格式。
	merchant, gateway := aliKeys(t)
	oneLine := func(pemText string) string {
		var b strings.Builder
		for _, line := range strings.Split(pemText, "\n") {
			if !strings.HasPrefix(line, "-----") {
				b.WriteString(strings.TrimSpace(line))
			}
		}
		return b.String()
	}
	p := alipay.New(http.DefaultClient)
	cfg := aliConfig(t, "")
	cfg["app_private_key"] = oneLine(pemPKCS8(t, merchant))
	cfg["alipay_public_key"] = oneLine(pemPKIX(t, &gateway.PublicKey))
	if err := p.ValidateConfig(cfg); err != nil {
		t.Fatalf("单行 base64 密钥应被接受：%v", err)
	}
	pkcs1 := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(merchant)}))
	cfg["app_private_key"] = pkcs1
	if err := p.ValidateConfig(cfg); err != nil {
		t.Fatalf("PKCS#1 私钥应被接受：%v", err)
	}
	if _, err := p.CreateOrder(context.Background(), cfg, payment.CreateReq{OrderNo: "VC1", Method: payment.MethodAlipay, Subject: "x", AmountFen: 100}); err != nil {
		t.Fatalf("规范化后的密钥应能正常下单：%v", err)
	}
}

func TestAlipay_CreateOrder(t *testing.T) {
	merchant, _ := aliKeys(t)
	p := alipay.New(http.DefaultClient)
	cfg := aliConfig(t, "https://openapi.alipay.com/gateway.do")
	req := payment.CreateReq{
		OrderNo: "VC2610160930124821330", Method: payment.MethodAlipay, Subject: "积分充值 10,000 积分", AmountFen: 10000,
		NotifyURL: "https://site.example.com/api/v1/payment/notify/alipay_page", ReturnURL: "https://site.example.com/pay/return?order_no=VC2610160930124821330",
	}
	co, err := p.CreateOrder(context.Background(), cfg, req)
	if err != nil || co.Mode != payment.CheckoutRedirect {
		t.Fatalf("%+v %v", co, err)
	}
	u, err := url.Parse(co.Value)
	if err != nil || u.Host != "openapi.alipay.com" || u.Path != "/gateway.do" {
		t.Fatalf("跳转地址不对：%s %v", co.Value, err)
	}
	q := u.Query()
	if q.Get("method") != "alipay.trade.page.pay" || q.Get("app_id") != "2021004123456789" || q.Get("notify_url") != req.NotifyURL || q.Get("return_url") != req.ReturnURL || q.Get("sign_type") != "RSA2" {
		t.Fatalf("公共参数不对：%v", q)
	}
	biz := q.Get("biz_content")
	for _, want := range []string{`"out_trade_no":"VC2610160930124821330"`, `"total_amount":"100.00"`, `"product_code":"FAST_INSTANT_TRADE_PAY"`, `"subject":"积分充值 10,000 积分"`} {
		if !strings.Contains(biz, want) {
			t.Errorf("biz_content 缺少 %s：%s", want, biz)
		}
	}
	// 独立验证 SDK 的签名：用应用公钥按支付宝文档的规则验
	sig, err := base64.StdEncoding.DecodeString(q.Get("sign"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(aliCanonical(q, "sign")))
	if err := rsa.VerifyPKCS1v15(&merchant.PublicKey, crypto.SHA256, sum[:], sig); err != nil {
		t.Fatalf("请求签名不符合支付宝规则：%v", err)
	}
}

func TestAlipay_QueryOrder(t *testing.T) {
	ref := payment.OrderRef{OrderNo: "VC2610160930124821330", Method: payment.MethodAlipay}
	respond := func(t *testing.T, biz string) (*alipay.Provider, payment.Config) {
		return newAliServer(t, func(w http.ResponseWriter, r *http.Request) {
			_ = r.ParseForm()
			if r.Form.Get("method") != "alipay.trade.query" || !strings.Contains(r.Form.Get("biz_content"), ref.OrderNo) {
				t.Errorf("查单请求不对：%v", r.Form)
			}
			_, _ = w.Write([]byte(signedResponse(t, "alipay_trade_query", biz)))
		})
	}
	t.Run("交易成功：paid，金额和交易号取自响应", func(t *testing.T) {
		p, cfg := respond(t, `{"code":"10000","msg":"Success","trade_no":"2026101622001400001","out_trade_no":"VC2610160930124821330","trade_status":"TRADE_SUCCESS","total_amount":"100.00","send_pay_date":"2026-10-16 10:01:02"}`)
		res, err := p.QueryOrder(context.Background(), cfg, ref)
		if err != nil || res.State != payment.StatePaid || res.PaidFen != 10000 || res.TradeNo != "2026101622001400001" || res.OrderNo != ref.OrderNo {
			t.Fatalf("%+v %v", res, err)
		}
	})
	t.Run("交易结束（TRADE_FINISHED）同样算已付", func(t *testing.T) {
		p, cfg := respond(t, `{"code":"10000","msg":"Success","trade_no":"T1","out_trade_no":"VC2610160930124821330","trade_status":"TRADE_FINISHED","total_amount":"100.00"}`)
		if res, err := p.QueryOrder(context.Background(), cfg, ref); err != nil || res.State != payment.StatePaid {
			t.Fatalf("%+v %v", res, err)
		}
	})
	t.Run("等待付款：pending；已关闭：closed", func(t *testing.T) {
		for status, want := range map[string]string{"WAIT_BUYER_PAY": payment.StatePending, "TRADE_CLOSED": payment.StateClosed} {
			p, cfg := respond(t, `{"code":"10000","msg":"Success","trade_no":"T1","out_trade_no":"VC2610160930124821330","trade_status":"`+status+`","total_amount":"100.00"}`)
			if res, err := p.QueryOrder(context.Background(), cfg, ref); err != nil || res.State != want {
				t.Fatalf("%s：%+v %v", status, res, err)
			}
		}
	})
	t.Run("用户还没打开付款页：支付宝回答 ACQ.TRADE_NOT_EXIST，不是错误", func(t *testing.T) {
		p, cfg := respond(t, `{"code":"40004","msg":"Business Failed","sub_code":"ACQ.TRADE_NOT_EXIST","sub_msg":"交易不存在"}`)
		res, err := p.QueryOrder(context.Background(), cfg, ref)
		if err != nil || res.State != payment.StateNotExist {
			t.Fatalf("%+v %v", res, err)
		}
	})
	t.Run("其它业务失败：报错", func(t *testing.T) {
		p, cfg := respond(t, `{"code":"40004","msg":"Business Failed","sub_code":"ACQ.SYSTEM_ERROR","sub_msg":"系统繁忙"}`)
		if _, err := p.QueryOrder(context.Background(), cfg, ref); err == nil {
			t.Fatal("应报错")
		}
	})
	t.Run("响应签名被伪造（用别的密钥签）：报错，绝不能信", func(t *testing.T) {
		p, cfg := newAliServer(t, func(w http.ResponseWriter, r *http.Request) {
			merchant, _ := aliKeys(t)
			biz := `{"code":"10000","msg":"Success","trade_no":"T1","out_trade_no":"VC2610160930124821330","trade_status":"TRADE_SUCCESS","total_amount":"100.00"}`
			_, _ = w.Write([]byte(`{"alipay_trade_query_response":` + biz + `,"sign":"` + rsaSign(t, merchant, biz) + `"}`))
		})
		if res, err := p.QueryOrder(context.Background(), cfg, ref); err == nil {
			t.Fatalf("伪造的已付响应不能被接受：%+v", res)
		}
	})
	t.Run("响应里的订单号对不上、金额无效：报错", func(t *testing.T) {
		for name, biz := range map[string]string{
			"订单号不符": `{"code":"10000","msg":"Success","trade_no":"T1","out_trade_no":"OTHER","trade_status":"TRADE_SUCCESS","total_amount":"100.00"}`,
			"金额无效":  `{"code":"10000","msg":"Success","trade_no":"T1","out_trade_no":"VC2610160930124821330","trade_status":"TRADE_SUCCESS","total_amount":"abc"}`,
			"状态未知":  `{"code":"10000","msg":"Success","trade_no":"T1","out_trade_no":"VC2610160930124821330","trade_status":"WHAT","total_amount":"100.00"}`,
		} {
			p, cfg := respond(t, biz)
			if res, err := p.QueryOrder(context.Background(), cfg, ref); err == nil {
				t.Errorf("%s：应报错，实际 %+v", name, res)
			}
		}
	})
}

func TestAlipay_CloseOrder(t *testing.T) {
	ref := payment.OrderRef{OrderNo: "VC2610160930124821330", Method: payment.MethodAlipay}
	closeWith := func(t *testing.T, biz string) error {
		p, cfg := newAliServer(t, func(w http.ResponseWriter, r *http.Request) {
			_ = r.ParseForm()
			if r.Form.Get("method") != "alipay.trade.close" {
				t.Errorf("method 不对：%s", r.Form.Get("method"))
			}
			_, _ = w.Write([]byte(signedResponse(t, "alipay_trade_close", biz)))
		})
		return p.CloseOrder(context.Background(), cfg, ref)
	}
	if err := closeWith(t, `{"code":"10000","msg":"Success","out_trade_no":"VC2610160930124821330"}`); err != nil {
		t.Fatalf("关单成功：%v", err)
	}
	if err := closeWith(t, `{"code":"40004","msg":"Business Failed","sub_code":"ACQ.TRADE_NOT_EXIST","sub_msg":"交易不存在"}`); err != nil {
		t.Fatalf("订单在支付宝不存在（用户从没打开付款页）视为关单成功：%v", err)
	}
	if err := closeWith(t, `{"code":"40004","msg":"Business Failed","sub_code":"ACQ.TRADE_HAS_SUCCESS","sub_msg":"交易已支付"}`); err == nil {
		t.Fatal("交易已支付无法关闭，必须报错，让 service 去查单入账")
	}
}

// aliNotifyBase 返回一份未签名的异步通知参数。
func aliNotifyBase() url.Values {
	return url.Values{
		"app_id": {"2021004123456789"}, "notify_id": {"2026101600abc"}, "notify_type": {"trade_status_sync"}, "trade_no": {"2026101622001400001"},
		"out_trade_no": {"VC2610160930124821330"}, "trade_status": {"TRADE_SUCCESS"}, "total_amount": {"100.00"}, "receipt_amount": {"100.00"},
		"seller_id": {"2088000000000001"}, "gmt_payment": {"2026-10-16 10:01:02"}, "charset": {"utf-8"}, "version": {"1.0"}, "sign_type": {"RSA2"},
	}
}

// aliSigned 用「支付宝」的私钥给参数签名（签完才能再做篡改类的变异）。
func aliSigned(t *testing.T, v url.Values) url.Values {
	_, gateway := aliKeys(t)
	v.Set("sign", rsaSign(t, gateway, aliCanonical(v, "sign", "sign_type")))
	return v
}

func TestAlipay_VerifyNotify(t *testing.T) {
	merchant, _ := aliKeys(t)
	p := alipay.New(http.DefaultClient)
	cfg := aliConfig(t, "")
	post := func(v url.Values) payment.NotifyRequest {
		return payment.NotifyRequest{Method: http.MethodPost, Body: []byte(v.Encode())}
	}
	t.Run("签名正确：解析订单号、交易号、金额和 app_id", func(t *testing.T) {
		res, err := p.VerifyNotify(context.Background(), cfg, post(aliSigned(t, aliNotifyBase())))
		if err != nil || res.State != payment.StatePaid || res.OrderNo != "VC2610160930124821330" || res.TradeNo != "2026101622001400001" || res.PaidFen != 10000 || res.Identity != "2021004123456789" || res.EventKey == "" {
			t.Fatalf("%+v %v", res, err)
		}
	})
	t.Run("TRADE_FINISHED 算已付；WAIT_BUYER_PAY / TRADE_CLOSED 不算", func(t *testing.T) {
		for status, wantPaid := range map[string]bool{"TRADE_FINISHED": true, "WAIT_BUYER_PAY": false, "TRADE_CLOSED": false} {
			v := aliNotifyBase()
			v.Set("trade_status", status)
			res, err := p.VerifyNotify(context.Background(), cfg, post(aliSigned(t, v)))
			if err != nil || (res.State == payment.StatePaid) != wantPaid {
				t.Fatalf("%s：%+v %v", status, res, err)
			}
		}
	})
	rejects := map[string]func() url.Values{
		"金额被篡改（签名不变）": func() url.Values {
			v := aliSigned(t, aliNotifyBase())
			v.Set("total_amount", "1.00")
			return v
		},
		"用别的密钥签名": func() url.Values {
			v := aliNotifyBase()
			v.Set("sign", rsaSign(t, merchant, aliCanonical(v, "sign", "sign_type")))
			return v
		},
		"缺少签名": aliNotifyBase,
		"app_id 不是我们的（签名正确）": func() url.Values {
			v := aliNotifyBase()
			v.Set("app_id", "2021009999999999")
			return aliSigned(t, v)
		},
		"带证书序列号（我们只支持公钥模式，避免被诱导去下载证书）": func() url.Values {
			v := aliNotifyBase()
			v.Set("alipay_cert_sn", "evil")
			return aliSigned(t, v)
		},
	}
	for name, build := range rejects {
		t.Run("验签失败："+name, func(t *testing.T) {
			if _, err := p.VerifyNotify(context.Background(), cfg, post(build())); !errors.Is(err, payment.ErrVerify) {
				t.Fatalf("期望 ErrVerify，实际 %v", err)
			}
		})
	}
}

func TestAlipay_AckAndProbe(t *testing.T) {
	p := alipay.New(http.DefaultClient)
	if a := p.NotifyAck(true); a.Status != 200 || a.Body != "success" {
		t.Fatalf("支付宝要求成功应答是纯文本 success：%+v", a)
	}
	if a := p.NotifyAck(false); a.Status == 200 {
		t.Fatalf("%+v", a)
	}
	t.Run("Probe：订单不存在说明凭证和签名都被接受", func(t *testing.T) {
		pp, cfg := newAliServer(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(signedResponse(t, "alipay_trade_query", `{"code":"40004","msg":"Business Failed","sub_code":"ACQ.TRADE_NOT_EXIST","sub_msg":"交易不存在"}`)))
		})
		if err := pp.Probe(context.Background(), cfg); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("Probe：支付宝说验签出错（应用私钥与应用不匹配）：报错并带原因", func(t *testing.T) {
		pp, cfg := newAliServer(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"error_response":{"code":"40002","msg":"Invalid Arguments","sub_code":"isv.invalid-signature","sub_msg":"验签出错，建议检查签名字符串或签名私钥与应用公钥是否匹配"}}`))
		})
		err := pp.Probe(context.Background(), cfg)
		if err == nil || !strings.Contains(err.Error(), "验签出错") {
			t.Fatalf("%v", err)
		}
	})
}
