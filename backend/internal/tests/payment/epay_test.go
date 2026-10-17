package payment_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"video-canvas/internal/payment"
	"video-canvas/internal/payment/epay"
)

// 用 Python hashlib 独立算出的标准答案：参数按键升序拼成 k=v&k=v（不含 sign / sign_type / 空值），末尾直接拼商户密钥 testkey。
const (
	epayVectorSign = "1db2bdbb1e7ce64349c4bc83226e6225"
	epayVectorKey  = "testkey"
)

func epayVectorQuery() url.Values {
	return url.Values{
		"pid": {"1001"}, "trade_no": {"2026101612345678"}, "out_trade_no": {"VC2610160930124821330"}, "type": {"wxpay"},
		"name": {"test"}, "money": {"100.00"}, "trade_status": {"TRADE_SUCCESS"}, "sign_type": {"MD5"}, "sign": {epayVectorSign},
	}
}

// newEpayServer 起一个 TLS 测试网关，返回装配好的适配器和配置。
func newEpayServer(t *testing.T, h http.HandlerFunc) (*epay.Provider, payment.Config, *httptest.Server) {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	return epay.New(srv.Client()), payment.Config{"api_base": srv.URL, "pid": "1001", "key": epayVectorKey}, srv
}

func TestEpay_ValidateConfig(t *testing.T) {
	p := epay.New(http.DefaultClient)
	good := payment.Config{"api_base": "https://pay.example.com", "pid": "1001", "key": "k"}
	if err := p.ValidateConfig(good); err != nil {
		t.Fatalf("合法配置不应报错：%v", err)
	}
	tests := []struct {
		name   string
		mutate func(c payment.Config)
	}{
		{"缺接口地址", func(c payment.Config) { delete(c, "api_base") }},
		{"缺商户 ID", func(c payment.Config) { c["pid"] = " " }},
		{"缺密钥", func(c payment.Config) { delete(c, "key") }},
		{"接口地址必须是 https（密钥会出现在查单的 URL 里）", func(c payment.Config) { c["api_base"] = "http://pay.example.com" }},
		{"接口地址不能带路径参数", func(c payment.Config) { c["api_base"] = "https://pay.example.com/?a=1" }},
		{"接口地址不能带用户名密码", func(c payment.Config) { c["api_base"] = "https://u:p@pay.example.com" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := payment.Config{}
			for k, v := range good {
				c[k] = v
			}
			tt.mutate(c)
			if err := p.ValidateConfig(c); err == nil {
				t.Fatal("应报错")
			}
		})
	}
}

func TestEpay_Meta(t *testing.T) {
	p := epay.New(http.DefaultClient)
	caps := p.Capabilities()
	if p.Key() != "epay" || !caps.CanQuery || caps.CanCloseUpstream || !caps.ConfirmNotify || len(caps.Methods) != 2 {
		t.Fatalf("能力声明不对：%+v", caps)
	}
	if got := p.Identity(payment.Config{"pid": " 1001 "}, payment.MethodWechat); got != "1001" {
		t.Fatalf("商户身份应是 pid：%q", got)
	}
	var secret, identity int
	for _, f := range p.Fields() {
		if f.Secret {
			secret++
		}
		if f.Identity {
			identity++
		}
	}
	if secret != 1 || identity != 1 {
		t.Fatalf("应有 1 个密钥字段（key）和 1 个身份字段（pid）：%d %d", secret, identity)
	}
}

func TestEpay_CreateOrder(t *testing.T) {
	req := payment.CreateReq{
		OrderNo: "VC2610160930124821330", Method: payment.MethodAlipay, Subject: "积分充值 10,000 积分", AmountFen: 10000,
		NotifyURL: "https://site.example.com/api/v1/payment/notify/epay", ReturnURL: "https://site.example.com/pay/return", ClientIP: "1.2.3.4",
	}
	t.Run("支付宝：返回 payurl 时走跳转；表单参数和 MD5 签名正确", func(t *testing.T) {
		var got url.Values
		var path string
		p, cfg, _ := newEpayServer(t, func(w http.ResponseWriter, r *http.Request) {
			path = r.URL.Path
			_ = r.ParseForm()
			got = r.PostForm
			_, _ = w.Write([]byte(`{"code":1,"msg":"","trade_no":"T1","payurl":"https://pay.example.com/pay/T1"}`))
		})
		co, err := p.CreateOrder(context.Background(), cfg, req)
		if err != nil || co.Mode != payment.CheckoutRedirect || co.Value != "https://pay.example.com/pay/T1" {
			t.Fatalf("%+v %v", co, err)
		}
		if path != "/mapi.php" || got.Get("pid") != "1001" || got.Get("type") != "alipay" || got.Get("out_trade_no") != req.OrderNo ||
			got.Get("money") != "100.00" || got.Get("notify_url") != req.NotifyURL || got.Get("return_url") != req.ReturnURL ||
			got.Get("clientip") != "1.2.3.4" || got.Get("device") != "pc" || got.Get("sign_type") != "MD5" || got.Get("name") == "" {
			t.Fatalf("请求参数不对：%s %v", path, got)
		}
		// 服务端用同样规则重算签名，必须与请求里的一致
		vals := map[string]string{}
		for k := range got {
			vals[k] = got.Get(k)
		}
		if want := expectedEpaySign(vals, cfg["key"]); got.Get("sign") != want {
			t.Fatalf("签名不对：%s != %s", got.Get("sign"), want)
		}
	})
	t.Run("微信：优先用 qrcode，前端画成二维码", func(t *testing.T) {
		p, cfg, _ := newEpayServer(t, func(w http.ResponseWriter, r *http.Request) {
			_ = r.ParseForm()
			if r.PostForm.Get("type") != "wxpay" {
				t.Errorf("微信应使用 wxpay 通道：%s", r.PostForm.Get("type"))
			}
			_, _ = w.Write([]byte(`{"code":1,"trade_no":"T2","payurl":"https://pay.example.com/h5/T2","qrcode":"weixin://wxpay/bizpayurl?pr=abc"}`))
		})
		wx := req
		wx.Method = payment.MethodWechat
		co, err := p.CreateOrder(context.Background(), cfg, wx)
		if err != nil || co.Mode != payment.CheckoutQRCode || co.Value != "weixin://wxpay/bizpayurl?pr=abc" {
			t.Fatalf("%+v %v", co, err)
		}
	})
	// 易支付的 payurl 有时其实是扫码链接（真实案例：支付宝返回 https://qr.alipay.com/xxx），桌面端应画成二维码让用户扫，
	// 而不是让用户去新标签页打开一个只会再显示二维码的页面。
	qrCases := map[string]struct {
		method, body, wantMode, wantValue string
	}{
		"支付宝：只返回扫码链接形式的 payurl → 画二维码": {
			payment.MethodAlipay, `{"code":1,"payurl":"https://qr.alipay.com/bax05659mnwtblmkbcjk258a"}`, payment.CheckoutQRCode, "https://qr.alipay.com/bax05659mnwtblmkbcjk258a",
		},
		"支付宝：同时返回 qrcode 和 payurl → 优先 qrcode": {
			payment.MethodAlipay, `{"code":1,"payurl":"https://pay.example.com/p","qrcode":"https://qr.alipay.com/abc"}`, payment.CheckoutQRCode, "https://qr.alipay.com/abc",
		},
		"支付宝：payurl 是普通收银台页面 → 跳转": {
			payment.MethodAlipay, `{"code":1,"payurl":"https://pay.example.com/cashier/T1"}`, payment.CheckoutRedirect, "https://pay.example.com/cashier/T1",
		},
		"微信：payurl 是 weixin:// 协议 → 画二维码": {
			payment.MethodWechat, `{"code":1,"payurl":"weixin://wxpay/bizpayurl?pr=abc"}`, payment.CheckoutQRCode, "weixin://wxpay/bizpayurl?pr=abc",
		},
		"微信：payurl 是 wxp:// 协议 → 画二维码": {
			payment.MethodWechat, `{"code":1,"payurl":"wxp://f2f0abc"}`, payment.CheckoutQRCode, "wxp://f2f0abc",
		},
	}
	for name, c := range qrCases {
		t.Run(name, func(t *testing.T) {
			p, cfg, _ := newEpayServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(c.body)) })
			r := req
			r.Method = c.method
			co, err := p.CreateOrder(context.Background(), cfg, r)
			if err != nil || co.Mode != c.wantMode || co.Value != c.wantValue {
				t.Fatalf("%+v %v", co, err)
			}
		})
	}
	t.Run("可以按支付方式覆盖易支付的 type（不同站点通道名不同）", func(t *testing.T) {
		p, cfg, _ := newEpayServer(t, func(w http.ResponseWriter, r *http.Request) {
			_ = r.ParseForm()
			if r.PostForm.Get("type") != "wechat" {
				t.Errorf("应使用配置的 wechat_type：%s", r.PostForm.Get("type"))
			}
			_, _ = w.Write([]byte(`{"code":1,"payurl":"https://pay.example.com/p"}`))
		})
		cfg["wechat_type"] = "wechat"
		wx := req
		wx.Method = payment.MethodWechat
		if _, err := p.CreateOrder(context.Background(), cfg, wx); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("网关返回失败：错误里带网关的提示", func(t *testing.T) {
		p, cfg, _ := newEpayServer(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"code":-1,"msg":"商户余额不足"}`))
		})
		_, err := p.CreateOrder(context.Background(), cfg, req)
		if err == nil || !strings.Contains(err.Error(), "商户余额不足") {
			t.Fatalf("%v", err)
		}
	})
	t.Run("成功但没有任何支付地址：报错", func(t *testing.T) {
		p, cfg, _ := newEpayServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"code":1}`)) })
		if _, err := p.CreateOrder(context.Background(), cfg, req); err == nil {
			t.Fatal("应报错")
		}
	})
	t.Run("响应不是 JSON、HTTP 5xx：报错", func(t *testing.T) {
		p, cfg, _ := newEpayServer(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) })
		if _, err := p.CreateOrder(context.Background(), cfg, req); err == nil {
			t.Fatal("应报错")
		}
	})
	t.Run("支付方式不支持：报错", func(t *testing.T) {
		p, cfg, _ := newEpayServer(t, func(w http.ResponseWriter, r *http.Request) {})
		bad := req
		bad.Method = "qq"
		if _, err := p.CreateOrder(context.Background(), cfg, bad); err == nil {
			t.Fatal("应报错")
		}
	})
}

// expectedEpaySign 在测试里独立写一遍签名规则，不复用被测代码。
func expectedEpaySign(vals map[string]string, key string) string {
	var keys []string
	for k, v := range vals {
		if k != "sign" && k != "sign_type" && v != "" {
			keys = append(keys, k)
		}
	}
	for i := range keys {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + vals[k]
	}
	return md5hex(strings.Join(parts, "&") + key)
}

func TestEpay_QueryOrder(t *testing.T) {
	ref := payment.OrderRef{OrderNo: "VC2610160930124821330", Method: payment.MethodWechat}
	serve := func(body string, check func(*http.Request)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if check != nil {
				check(r)
			}
			_, _ = w.Write([]byte(body))
		}
	}
	t.Run("已付：状态 paid，金额和交易号取自响应；请求带 act=order、pid、key、out_trade_no", func(t *testing.T) {
		p, cfg, _ := newEpayServer(t, serve(`{"code":1,"msg":"查询订单号成功！","trade_no":"T9","out_trade_no":"VC2610160930124821330","type":"wxpay","pid":"1001","name":"x","money":"100.00","status":1,"endtime":"2026-10-16 10:01:02"}`, func(r *http.Request) {
			q := r.URL.Query()
			if r.URL.Path != "/api.php" || q.Get("act") != "order" || q.Get("pid") != "1001" || q.Get("key") != epayVectorKey || q.Get("out_trade_no") != ref.OrderNo {
				t.Errorf("查单请求不对：%s", r.URL)
			}
		}))
		res, err := p.QueryOrder(context.Background(), cfg, ref)
		if err != nil || res.State != payment.StatePaid || res.PaidFen != 10000 || res.TradeNo != "T9" || res.OrderNo != ref.OrderNo || res.Identity != "1001" {
			t.Fatalf("%+v %v", res, err)
		}
		if res.PaidAt.IsZero() {
			t.Fatal("应解析出付款时间")
		}
	})
	t.Run("未付：pending", func(t *testing.T) {
		p, cfg, _ := newEpayServer(t, serve(`{"code":1,"trade_no":"T9","out_trade_no":"VC2610160930124821330","pid":"1001","money":"100.00","status":0}`, nil))
		res, err := p.QueryOrder(context.Background(), cfg, ref)
		if err != nil || res.State != payment.StatePending {
			t.Fatalf("%+v %v", res, err)
		}
	})
	t.Run("渠道没有这笔订单：not_exist，不是错误", func(t *testing.T) {
		p, cfg, _ := newEpayServer(t, serve(`{"code":-1,"msg":"订单号不存在"}`, nil))
		res, err := p.QueryOrder(context.Background(), cfg, ref)
		if err != nil || res.State != payment.StateNotExist {
			t.Fatalf("%+v %v", res, err)
		}
	})
	t.Run("商户号对不上、订单号对不上、金额无效：都当作错误，不能当作已付", func(t *testing.T) {
		for name, body := range map[string]string{
			"pid 不符":   `{"code":1,"trade_no":"T","out_trade_no":"VC2610160930124821330","pid":"9999","money":"100.00","status":1}`,
			"订单号不符":    `{"code":1,"trade_no":"T","out_trade_no":"OTHER","pid":"1001","money":"100.00","status":1}`,
			"金额无效":     `{"code":1,"trade_no":"T","out_trade_no":"VC2610160930124821330","pid":"1001","money":"abc","status":1}`,
			"已付但没有交易号": `{"code":1,"out_trade_no":"VC2610160930124821330","pid":"1001","money":"100.00","status":1}`,
		} {
			p, cfg, _ := newEpayServer(t, serve(body, nil))
			if res, err := p.QueryOrder(context.Background(), cfg, ref); err == nil {
				t.Errorf("%s：应报错，实际 %+v", name, res)
			}
		}
	})
	t.Run("网关故障：报错", func(t *testing.T) {
		p, cfg, _ := newEpayServer(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) })
		if _, err := p.QueryOrder(context.Background(), cfg, ref); err == nil {
			t.Fatal("应报错")
		}
	})
}

func TestEpay_VerifyNotify(t *testing.T) {
	p := epay.New(http.DefaultClient)
	cfg := payment.Config{"api_base": "https://pay.example.com", "pid": "1001", "key": epayVectorKey}
	get := func(q url.Values) payment.NotifyRequest {
		return payment.NotifyRequest{Method: http.MethodGet, Query: q}
	}

	t.Run("签名正确（标准答案）：解析出订单号、交易号、金额和商户身份", func(t *testing.T) {
		res, err := p.VerifyNotify(context.Background(), cfg, get(epayVectorQuery()))
		if err != nil {
			t.Fatal(err)
		}
		if res.State != payment.StatePaid || res.OrderNo != "VC2610160930124821330" || res.TradeNo != "2026101612345678" || res.PaidFen != 10000 || res.Identity != "1001" || res.EventKey == "" {
			t.Fatalf("%+v", res)
		}
	})
	t.Run("POST 表单同样支持", func(t *testing.T) {
		res, err := p.VerifyNotify(context.Background(), cfg, payment.NotifyRequest{Method: http.MethodPost, Body: []byte(epayVectorQuery().Encode())})
		if err != nil || res.State != payment.StatePaid {
			t.Fatalf("%+v %v", res, err)
		}
	})
	rejects := map[string]func(q url.Values){
		"金额被篡改":      func(q url.Values) { q.Set("money", "1.00") },
		"订单号被篡改":     func(q url.Values) { q.Set("out_trade_no", "VC-OTHER") },
		"签名被篡改":      func(q url.Values) { q.Set("sign", strings.Repeat("0", 32)) },
		"缺少签名":       func(q url.Values) { q.Del("sign") },
		"签名为空":       func(q url.Values) { q.Set("sign", "") },
		"商户号不是我们的":   func(q url.Values) { q.Set("pid", "9999") },
		"签名类型不是 MD5": func(q url.Values) { q.Set("sign_type", "RSA") },
	}
	for name, mutate := range rejects {
		t.Run("验签失败："+name, func(t *testing.T) {
			q := epayVectorQuery()
			mutate(q)
			if _, err := p.VerifyNotify(context.Background(), cfg, get(q)); !errors.Is(err, payment.ErrVerify) {
				t.Fatalf("期望 ErrVerify，实际 %v", err)
			}
		})
	}
	t.Run("密钥不同：同一份通知验签失败", func(t *testing.T) {
		other := payment.Config{"api_base": "https://pay.example.com", "pid": "1001", "key": "other-key"}
		if _, err := p.VerifyNotify(context.Background(), other, get(epayVectorQuery())); !errors.Is(err, payment.ErrVerify) {
			t.Fatalf("%v", err)
		}
	})
	t.Run("参数重复：拒绝", func(t *testing.T) {
		q := epayVectorQuery()
		q.Add("money", "0.01")
		if _, err := p.VerifyNotify(context.Background(), cfg, get(q)); !errors.Is(err, payment.ErrVerify) {
			t.Fatalf("%v", err)
		}
	})
	t.Run("交易状态不是 TRADE_SUCCESS：签名对，但不算已付", func(t *testing.T) {
		q := epayVectorQuery()
		q.Set("trade_status", "WAIT_BUYER_PAY")
		// 改了参数就必须重新签名，否则就是验签失败：用独立实现重算
		vals := map[string]string{}
		for k := range q {
			vals[k] = q.Get(k)
		}
		q.Set("sign", expectedEpaySign(vals, epayVectorKey))
		res, err := p.VerifyNotify(context.Background(), cfg, get(q))
		if err != nil || res.State == payment.StatePaid {
			t.Fatalf("%+v %v", res, err)
		}
	})
}

func TestEpay_AckAndClose(t *testing.T) {
	p := epay.New(http.DefaultClient)
	if a := p.NotifyAck(true); a.Status != 200 || a.Body != "success" {
		t.Fatalf("成功应答必须是纯文本 success：%+v", a)
	}
	if a := p.NotifyAck(false); a.Status == 200 || a.Body == "success" {
		t.Fatalf("失败应答不能让渠道以为成功：%+v", a)
	}
	if err := p.CloseOrder(context.Background(), payment.Config{}, payment.OrderRef{OrderNo: "x"}); err == nil {
		t.Fatal("易支付没有关单接口，调用应报错而不是假装成功")
	}
}

func TestEpay_Probe(t *testing.T) {
	t.Run("渠道回答「订单不存在」：凭证可用", func(t *testing.T) {
		p, cfg, _ := newEpayServer(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"code":-1,"msg":"订单号不存在"}`))
		})
		if err := p.Probe(context.Background(), cfg); err != nil {
			t.Fatalf("%v", err)
		}
	})
	t.Run("渠道回答商户或密钥错误：不可用，错误里带原因", func(t *testing.T) {
		p, cfg, _ := newEpayServer(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"code":-1,"msg":"商户ID或密钥错误"}`))
		})
		err := p.Probe(context.Background(), cfg)
		if err == nil || !strings.Contains(err.Error(), "密钥错误") {
			t.Fatalf("%v", err)
		}
	})
	t.Run("网关打不开：不可用", func(t *testing.T) {
		p, cfg, srv := newEpayServer(t, func(w http.ResponseWriter, r *http.Request) {})
		srv.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		err := p.Probe(ctx, cfg)
		if err == nil {
			t.Fatal("应报错")
		}
		// 查单的 URL 里带着商户密钥，Go 的网络错误默认会把整条 URL 写进错误文本，必须去掉，否则密钥会进日志和失败原因
		if strings.Contains(err.Error(), epayVectorKey) {
			t.Fatalf("错误文本泄露了商户密钥：%v", err)
		}
	})
}
