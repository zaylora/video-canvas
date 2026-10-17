package payment_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"

	"video-canvas/internal/payment"
	"video-canvas/internal/payment/xunhupay"
)

// 用 Python hashlib 独立算出的标准答案：虎皮椒 hash = md5(参数按键升序拼成 k=v&k=v，不含 hash 和空值，末尾直接拼 AppSecret)。
// 参数：appid=20211234 nonce_str=abc status=OD time=1760580000 total_fee=100.00 trade_order_id=VC2610160930124821330 transaction_id=4200001234，密钥 xhsecret。
const (
	xhVectorHash   = "c09112d94a8dc21c8e279f0a5c74902a"
	xhVectorSecret = "xhsecret"
)

func xhVectorValues() map[string]string {
	return map[string]string{
		"appid": "20211234", "trade_order_id": "VC2610160930124821330", "total_fee": "100.00", "transaction_id": "4200001234",
		"status": "OD", "time": "1760580000", "nonce_str": "abc", "hash": xhVectorHash,
	}
}

func xhConfig(base string) payment.Config {
	return payment.Config{
		"gateway": base, "wechat_app_id": "20211234", "wechat_app_secret": xhVectorSecret,
		"alipay_app_id": "20215678", "alipay_app_secret": "alisecret",
	}
}

func newXHServer(t *testing.T, h http.HandlerFunc) (*xunhupay.Provider, payment.Config) {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	return xunhupay.New(srv.Client()), xhConfig(srv.URL)
}

// xhHash 在测试里独立写一遍 hash 规则，不复用被测代码。
func xhHash(vals map[string]string, secret string) string {
	var keys []string
	for k, v := range vals {
		if k != "hash" && v != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + vals[k]
	}
	return md5hex(strings.Join(parts, "&") + secret)
}

func readJSONBody(t *testing.T, r *http.Request) map[string]string {
	t.Helper()
	var raw map[string]any
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		t.Errorf("请求体不是 JSON：%v", err)
		return nil
	}
	out := map[string]string{}
	for k, v := range raw {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}

func TestXunhupay_Meta(t *testing.T) {
	p := xunhupay.New(http.DefaultClient)
	caps := p.Capabilities()
	if p.Key() != "xunhupay" || !caps.CanQuery || caps.CanCloseUpstream || !caps.ConfirmNotify || len(caps.Methods) != 2 {
		t.Fatalf("%+v", caps)
	}
	cfg := xhConfig("https://api.xunhupay.com")
	if p.Identity(cfg, payment.MethodWechat) != "20211234" || p.Identity(cfg, payment.MethodAlipay) != "20215678" {
		t.Fatal("商户身份应按支付方式取各自的 AppID")
	}
	var secrets, ids int
	for _, f := range p.Fields() {
		if f.Secret {
			secrets++
			if !f.Optional {
				t.Errorf("两组凭证都是可选的（至少填一组）：%s", f.Key)
			}
		}
		if f.Identity {
			ids++
		}
	}
	if secrets != 2 || ids != 2 {
		t.Fatalf("应有 2 个密钥字段和 2 个身份字段：%d %d", secrets, ids)
	}
}

func TestXunhupay_ValidateConfig(t *testing.T) {
	p := xunhupay.New(http.DefaultClient)
	if err := p.ValidateConfig(payment.Config{"wechat_app_id": "a", "wechat_app_secret": "s"}); err != nil {
		t.Fatalf("只填一组凭证、网关用默认值是合法的：%v", err)
	}
	bad := map[string]payment.Config{
		"两组凭证都没填":         {},
		"只填了 AppID 没填 密钥": {"wechat_app_id": "a"},
		"网关不是 https":      {"wechat_app_id": "a", "wechat_app_secret": "s", "gateway": "http://api.xunhupay.com"},
		"网关带路径":           {"wechat_app_id": "a", "wechat_app_secret": "s", "gateway": "https://api.xunhupay.com/x"},
		"网关带用户名密码":        {"wechat_app_id": "a", "wechat_app_secret": "s", "gateway": "https://u:p@api.xunhupay.com"},
	}
	for name, cfg := range bad {
		if err := p.ValidateConfig(cfg); err == nil {
			t.Errorf("%s：应报错", name)
		}
	}
}

func TestXunhupay_CreateOrder(t *testing.T) {
	req := payment.CreateReq{
		OrderNo: "VC2610160930124821330", Method: payment.MethodWechat, Subject: "积分充值 10,000 积分", AmountFen: 10000,
		NotifyURL: "https://site.example.com/api/v1/payment/notify/xunhupay", ReturnURL: "https://site.example.com/pay/return",
	}
	t.Run("微信：用微信那组凭证下单，请求体和 hash 正确，返回二维码图片", func(t *testing.T) {
		var got map[string]string
		var path string
		p, cfg := newXHServer(t, func(w http.ResponseWriter, r *http.Request) {
			path = r.URL.Path
			got = readJSONBody(t, r)
			_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"success!","url_qrcode":"https://api.xunhupay.com/qr/1.png","url":"https://pay.xunhupay.com/p/1"}`))
		})
		co, err := p.CreateOrder(context.Background(), cfg, req)
		if err != nil || co.Mode != payment.CheckoutQRCodeImage || co.Value != "https://api.xunhupay.com/qr/1.png" {
			t.Fatalf("%+v %v", co, err)
		}
		if path != "/payment/do.html" || got["appid"] != "20211234" || got["trade_order_id"] != req.OrderNo || got["total_fee"] != "100.00" ||
			got["notify_url"] != req.NotifyURL || got["return_url"] != req.ReturnURL || got["title"] == "" || got["time"] == "" || got["nonce_str"] == "" {
			t.Fatalf("请求不对：%s %v", path, got)
		}
		if want := xhHash(got, xhVectorSecret); got["hash"] != want {
			t.Fatalf("hash 不对：%s != %s", got["hash"], want)
		}
	})
	t.Run("支付宝：用支付宝那组凭证", func(t *testing.T) {
		var got map[string]string
		p, cfg := newXHServer(t, func(w http.ResponseWriter, r *http.Request) {
			got = readJSONBody(t, r)
			_, _ = w.Write([]byte(`{"errcode":0,"url_qrcode":"https://api.xunhupay.com/qr/2.png"}`))
		})
		ali := req
		ali.Method = payment.MethodAlipay
		if _, err := p.CreateOrder(context.Background(), cfg, ali); err != nil {
			t.Fatal(err)
		}
		if got["appid"] != "20215678" || got["hash"] != xhHash(got, "alisecret") {
			t.Fatalf("应使用支付宝凭证：%v", got)
		}
	})
	t.Run("没有二维码图片时退到付款页跳转", func(t *testing.T) {
		p, cfg := newXHServer(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"errcode":0,"url":"https://pay.xunhupay.com/p/3"}`))
		})
		co, err := p.CreateOrder(context.Background(), cfg, req)
		if err != nil || co.Mode != payment.CheckoutRedirect || co.Value != "https://pay.xunhupay.com/p/3" {
			t.Fatalf("%+v %v", co, err)
		}
	})
	t.Run("该支付方式没配置凭证：报错，不发请求", func(t *testing.T) {
		called := false
		p, cfg := newXHServer(t, func(w http.ResponseWriter, r *http.Request) { called = true })
		delete(cfg, "alipay_app_id")
		delete(cfg, "alipay_app_secret")
		ali := req
		ali.Method = payment.MethodAlipay
		if _, err := p.CreateOrder(context.Background(), cfg, ali); err == nil || called {
			t.Fatalf("应报错且不发请求：%v called=%v", err, called)
		}
	})
	t.Run("errcode 非 0（数字或字符串）：错误里带 errmsg", func(t *testing.T) {
		for _, body := range []string{`{"errcode":40029,"errmsg":"appid 不存在"}`, `{"errcode":"40029","errmsg":"appid 不存在"}`} {
			p, cfg := newXHServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) })
			_, err := p.CreateOrder(context.Background(), cfg, req)
			if err == nil || !strings.Contains(err.Error(), "appid 不存在") {
				t.Fatalf("%s：%v", body, err)
			}
		}
	})
	t.Run("成功但二维码和付款页都没有：报错", func(t *testing.T) {
		p, cfg := newXHServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"errcode":0}`)) })
		if _, err := p.CreateOrder(context.Background(), cfg, req); err == nil {
			t.Fatal("应报错")
		}
	})
}

func TestXunhupay_QueryOrder(t *testing.T) {
	ref := payment.OrderRef{OrderNo: "VC2610160930124821330", Method: payment.MethodWechat}
	t.Run("已付（OD）：金额、交易号、付款时间取自 data；请求用对应方式的凭证", func(t *testing.T) {
		var got map[string]string
		var path string
		p, cfg := newXHServer(t, func(w http.ResponseWriter, r *http.Request) {
			path, got = r.URL.Path, readJSONBody(t, r)
			_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"success","data":{"status":"OD","total_fee":100,"transaction_id":"42001","out_trade_order":"VC2610160930124821330","paid_date":"2026-10-16 10:01:02"}}`))
		})
		res, err := p.QueryOrder(context.Background(), cfg, ref)
		if err != nil || res.State != payment.StatePaid || res.PaidFen != 10000 || res.TradeNo != "42001" || res.OrderNo != ref.OrderNo || res.Identity != "20211234" || res.PaidAt.IsZero() {
			t.Fatalf("%+v %v", res, err)
		}
		if path != "/payment/query.html" || got["appid"] != "20211234" || got["out_trade_order"] != ref.OrderNo || got["hash"] != xhHash(got, xhVectorSecret) {
			t.Fatalf("查单请求不对：%s %v", path, got)
		}
	})
	t.Run("待付（WP）：pending；已关闭（CD）：closed", func(t *testing.T) {
		for status, want := range map[string]string{"WP": payment.StatePending, "CD": payment.StateClosed} {
			p, cfg := newXHServer(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{"errcode":0,"data":{"status":"` + status + `","total_fee":"100.00","out_trade_order":"VC2610160930124821330"}}`))
			})
			res, err := p.QueryOrder(context.Background(), cfg, ref)
			if err != nil || res.State != want {
				t.Fatalf("%s：%+v %v", status, res, err)
			}
		}
	})
	t.Run("渠道没有这笔订单：not_exist", func(t *testing.T) {
		p, cfg := newXHServer(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"errcode":404,"errmsg":"订单不存在"}`))
		})
		res, err := p.QueryOrder(context.Background(), cfg, ref)
		if err != nil || res.State != payment.StateNotExist {
			t.Fatalf("%+v %v", res, err)
		}
	})
	t.Run("订单号对不上、状态缺失、已付但没有交易号：都是错误，不能当作已付", func(t *testing.T) {
		for name, body := range map[string]string{
			"订单号不符":  `{"errcode":0,"data":{"status":"OD","total_fee":"100.00","transaction_id":"1","out_trade_order":"OTHER"}}`,
			"没有状态":   `{"errcode":0,"data":{"total_fee":"100.00","out_trade_order":"VC2610160930124821330"}}`,
			"已付无交易号": `{"errcode":0,"data":{"status":"OD","total_fee":"100.00","out_trade_order":"VC2610160930124821330"}}`,
			"金额无效":   `{"errcode":0,"data":{"status":"OD","total_fee":"x","transaction_id":"1","out_trade_order":"VC2610160930124821330"}}`,
		} {
			p, cfg := newXHServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) })
			if res, err := p.QueryOrder(context.Background(), cfg, ref); err == nil {
				t.Errorf("%s：应报错，实际 %+v", name, res)
			}
		}
	})
}

func TestXunhupay_VerifyNotify(t *testing.T) {
	p := xunhupay.New(http.DefaultClient)
	cfg := xhConfig("https://api.xunhupay.com")
	form := func(m map[string]string) payment.NotifyRequest {
		v := url.Values{}
		for k, val := range m {
			v.Set(k, val)
		}
		return payment.NotifyRequest{Method: http.MethodPost, Body: []byte(v.Encode())}
	}
	t.Run("hash 正确（标准答案）：解析出订单号、交易号、金额和 AppID", func(t *testing.T) {
		res, err := p.VerifyNotify(context.Background(), cfg, form(xhVectorValues()))
		if err != nil {
			t.Fatal(err)
		}
		if res.State != payment.StatePaid || res.OrderNo != "VC2610160930124821330" || res.TradeNo != "4200001234" || res.PaidFen != 10000 || res.Identity != "20211234" || res.EventKey == "" {
			t.Fatalf("%+v", res)
		}
	})
	t.Run("JSON 请求体同样支持（金额可能是数字）", func(t *testing.T) {
		vals := xhVectorValues()
		body, _ := json.Marshal(vals)
		res, err := p.VerifyNotify(context.Background(), cfg, payment.NotifyRequest{Method: http.MethodPost, Body: body})
		if err != nil || res.State != payment.StatePaid {
			t.Fatalf("%+v %v", res, err)
		}
	})
	rejects := map[string]func(m map[string]string){
		"金额被篡改":       func(m map[string]string) { m["total_fee"] = "1.00" },
		"订单号被篡改":      func(m map[string]string) { m["trade_order_id"] = "VC-OTHER" },
		"hash 被篡改":    func(m map[string]string) { m["hash"] = strings.Repeat("0", 32) },
		"缺少 hash":     func(m map[string]string) { delete(m, "hash") },
		"AppID 不是我们的": func(m map[string]string) { m["appid"] = "99999999" },
	}
	for name, mutate := range rejects {
		t.Run("验签失败："+name, func(t *testing.T) {
			m := xhVectorValues()
			mutate(m)
			if _, err := p.VerifyNotify(context.Background(), cfg, form(m)); !errors.Is(err, payment.ErrVerify) {
				t.Fatalf("期望 ErrVerify，实际 %v", err)
			}
		})
	}
	t.Run("用另一组凭证的密钥签名：验签失败（微信 AppID 不能用支付宝的密钥）", func(t *testing.T) {
		m := xhVectorValues()
		m["hash"] = xhHash(m, "alisecret")
		if _, err := p.VerifyNotify(context.Background(), cfg, form(m)); !errors.Is(err, payment.ErrVerify) {
			t.Fatalf("%v", err)
		}
	})
	t.Run("支付宝那组的通知：用支付宝的密钥验签，身份是支付宝的 AppID", func(t *testing.T) {
		m := xhVectorValues()
		m["appid"] = "20215678"
		m["hash"] = xhHash(m, "alisecret")
		res, err := p.VerifyNotify(context.Background(), cfg, form(m))
		if err != nil || res.Identity != "20215678" || res.State != payment.StatePaid {
			t.Fatalf("%+v %v", res, err)
		}
	})
	t.Run("状态不是 OD：验签通过但不算已付", func(t *testing.T) {
		m := xhVectorValues()
		m["status"] = "CD"
		m["hash"] = xhHash(m, xhVectorSecret)
		res, err := p.VerifyNotify(context.Background(), cfg, form(m))
		if err != nil || res.State == payment.StatePaid {
			t.Fatalf("%+v %v", res, err)
		}
	})
}

func TestXunhupay_AckCloseProbe(t *testing.T) {
	p := xunhupay.New(http.DefaultClient)
	if a := p.NotifyAck(true); a.Status != 200 || a.Body != "success" {
		t.Fatalf("%+v", a)
	}
	if a := p.NotifyAck(false); a.Status == 200 || a.Body == "success" {
		t.Fatalf("%+v", a)
	}
	if err := p.CloseOrder(context.Background(), payment.Config{}, payment.OrderRef{OrderNo: "x"}); err == nil {
		t.Fatal("虎皮椒没有关单接口，调用应报错而不是假装成功")
	}

	t.Run("Probe：每组已配置的凭证都探测一遍，订单不存在视为可用", func(t *testing.T) {
		var appids []string
		pp, cfg := newXHServer(t, func(w http.ResponseWriter, r *http.Request) {
			appids = append(appids, readJSONBody(t, r)["appid"])
			_, _ = w.Write([]byte(`{"errcode":404,"errmsg":"订单不存在"}`))
		})
		if err := pp.Probe(context.Background(), cfg); err != nil {
			t.Fatal(err)
		}
		if len(appids) != 2 {
			t.Fatalf("两组凭证都应探测：%v", appids)
		}
	})
	t.Run("Probe：渠道说 appid 或签名有问题，报错", func(t *testing.T) {
		pp, cfg := newXHServer(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"errcode":40001,"errmsg":"hash 校验失败"}`))
		})
		if err := pp.Probe(context.Background(), cfg); err == nil {
			t.Fatal("应报错")
		}
	})
	t.Run("网关不可达时错误里不含密钥", func(t *testing.T) {
		pp, cfg := newXHServer(t, func(w http.ResponseWriter, r *http.Request) {})
		srv := cfg["gateway"]
		_ = srv
		cfg["gateway"] = "https://127.0.0.1:1"
		err := pp.Probe(context.Background(), cfg)
		if err == nil || strings.Contains(err.Error(), xhVectorSecret) {
			t.Fatalf("%v", err)
		}
	})
}
