package payment_test

import (
	"context"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"video-canvas/internal/payment"
	"video-canvas/internal/payment/wechat"
)

// 测试里扮演两方：merchant 是我们的商户私钥（SDK 用它给请求签名），wxGateway 是「微信支付」（测试用它给响应和通知签名）。
// 响应签名、通知加密都按微信支付 APIv3 文档在测试里独立实现，不借助 SDK。
const (
	wxAppID    = "wx8a1b2c3d4e5f6789"
	wxMchID    = "1690012345"
	wxSerial   = "5A1EC93F00000000000000000000000000000001"
	wxPubKeyID = "PUB_KEY_ID_0116900123452024"
	wxAPIv3Key = "0123456789abcdef0123456789abcdef" // 32 字节
)

var (
	wxKeysOnce sync.Once
	wxMerchant *rsa.PrivateKey
	wxGateway  *rsa.PrivateKey
)

func wxKeys(t *testing.T) (merchant, gateway *rsa.PrivateKey) {
	t.Helper()
	wxKeysOnce.Do(func() {
		var err error
		if wxMerchant, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
			panic(err)
		}
		if wxGateway, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
			panic(err)
		}
	})
	return wxMerchant, wxGateway
}

func wxConfig(t *testing.T) payment.Config {
	merchant, gateway := wxKeys(t)
	return payment.Config{
		"app_id": wxAppID, "mch_id": wxMchID, "merchant_serial_no": wxSerial, "wechatpay_public_key_id": wxPubKeyID,
		"merchant_private_key": pemPKCS8(t, merchant), "api_v3_key": wxAPIv3Key, "wechatpay_public_key": pemPKIX(t, &gateway.PublicKey),
	}
}

type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// wxServer 起一个 TLS 测试网关，并返回一个把所有请求改写到它上面的客户端（SDK 里的域名是写死的 api.mch.weixin.qq.com）。
func wxServer(t *testing.T, h http.HandlerFunc) *wechat.Provider {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "https://")
	client := &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		r = r.Clone(r.Context())
		r.URL.Scheme, r.URL.Host = "https", host
		return srv.Client().Transport.RoundTrip(r)
	})}
	return wechat.New(client)
}

// wxReply 按 APIv3 的规则给响应签名：待签名串是「时间戳\n随机串\n响应体\n」，用微信的私钥做 SHA256withRSA。
func wxReply(t *testing.T, w http.ResponseWriter, status int, body string) {
	_, gateway := wxKeys(t)
	ts, nonce := strconv.FormatInt(time.Now().Unix(), 10), "reply-nonce-1234"
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Wechatpay-Timestamp", ts)
	h.Set("Wechatpay-Nonce", nonce)
	h.Set("Wechatpay-Serial", wxPubKeyID)
	h.Set("Wechatpay-Signature", rsaSign(t, gateway, ts+"\n"+nonce+"\n"+body+"\n"))
	h.Set("Request-ID", "test-request-id")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

var wxAuthRe = regexp.MustCompile(`(\w+)="([^"]*)"`)

// wxCheckAuth 独立验证 SDK 发出的请求签名：待签名串是「方法\n路径(含查询)\n时间戳\n随机串\n请求体\n」，用商户公钥验。
func wxCheckAuth(t *testing.T, r *http.Request, body string) {
	t.Helper()
	merchant, _ := wxKeys(t)
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "WECHATPAY2-SHA256-RSA2048 ") {
		t.Errorf("Authorization 格式不对：%s", auth)
		return
	}
	f := map[string]string{}
	for _, m := range wxAuthRe.FindAllStringSubmatch(auth, -1) {
		f[m[1]] = m[2]
	}
	if f["mchid"] != wxMchID || f["serial_no"] != wxSerial {
		t.Errorf("商户号或证书序列号不对：%v", f)
	}
	sig, err := base64.StdEncoding.DecodeString(f["signature"])
	if err != nil {
		t.Errorf("签名不是 base64：%v", err)
		return
	}
	sum := sha256.Sum256([]byte(r.Method + "\n" + r.URL.RequestURI() + "\n" + f["timestamp"] + "\n" + f["nonce_str"] + "\n" + body + "\n"))
	if err := rsa.VerifyPKCS1v15(&merchant.PublicKey, crypto.SHA256, sum[:], sig); err != nil {
		t.Errorf("请求签名不符合微信支付规则：%v", err)
	}
}

func TestWechat_MetaAndValidate(t *testing.T) {
	p := wechat.New(http.DefaultClient)
	caps := p.Capabilities()
	if p.Key() != "wechat_native" || !caps.CanQuery || !caps.CanCloseUpstream || caps.ConfirmNotify || len(caps.Methods) != 1 || caps.Methods[0] != payment.MethodWechat {
		t.Fatalf("%+v", caps)
	}
	cfg := wxConfig(t)
	if p.Identity(cfg, payment.MethodWechat) != wxMchID {
		t.Fatal("商户身份应是商户号")
	}
	var secrets, ids int
	for _, f := range p.Fields() {
		if f.Secret {
			secrets++
		}
		if f.Identity {
			ids++
		}
	}
	if secrets != 3 || ids != 2 {
		t.Fatalf("应有 3 个密钥字段（商户私钥、APIv3 密钥、微信支付公钥）和 2 个身份字段（AppID、商户号）：%d %d", secrets, ids)
	}
	if err := p.ValidateConfig(cfg); err != nil {
		t.Fatalf("合法配置不应报错：%v", err)
	}
	bad := map[string]func(c payment.Config){
		"缺 AppID":         func(c payment.Config) { delete(c, "app_id") },
		"缺商户号":            func(c payment.Config) { delete(c, "mch_id") },
		"缺证书序列号":          func(c payment.Config) { delete(c, "merchant_serial_no") },
		"缺公钥 ID":          func(c payment.Config) { delete(c, "wechatpay_public_key_id") },
		"商户私钥无效":          func(c payment.Config) { c["merchant_private_key"] = "bad" },
		"微信支付公钥无效":        func(c payment.Config) { c["wechatpay_public_key"] = "bad" },
		"APIv3 密钥不是 32 位": func(c payment.Config) { c["api_v3_key"] = "short" },
		"缺 APIv3 密钥":      func(c payment.Config) { delete(c, "api_v3_key") },
	}
	for name, mutate := range bad {
		c := wxConfig(t)
		mutate(c)
		if err := p.ValidateConfig(c); err == nil {
			t.Errorf("%s：应报错", name)
		}
	}
}

func TestWechat_CreateOrder(t *testing.T) {
	req := payment.CreateReq{
		OrderNo: "VC2610160930124821330", Method: payment.MethodWechat, Subject: "积分充值 10,000 积分", AmountFen: 10000,
		NotifyURL: "https://site.example.com/api/v1/payment/notify/wechat_native", ExpiresAt: time.Date(2026, 10, 16, 10, 15, 0, 0, time.FixedZone("CST", 8*3600)),
	}
	t.Run("成功：请求体、请求签名正确，返回二维码内容", func(t *testing.T) {
		p := wxServer(t, func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			wxCheckAuth(t, r, string(b))
			var got map[string]any
			_ = json.Unmarshal(b, &got)
			amount, _ := got["amount"].(map[string]any)
			if r.Method != http.MethodPost || r.URL.Path != "/v3/pay/transactions/native" || got["appid"] != wxAppID || got["mchid"] != wxMchID ||
				got["out_trade_no"] != req.OrderNo || got["notify_url"] != req.NotifyURL || amount["total"] != float64(10000) ||
				got["description"] != "积分充值 10,000 积分" || got["time_expire"] != "2026-10-16T10:15:00+08:00" {
				t.Errorf("请求不对：%s %s", r.URL.Path, b)
			}
			wxReply(t, w, http.StatusOK, `{"code_url":"weixin://wxpay/bizpayurl?pr=abcdef"}`)
		})
		co, err := p.CreateOrder(context.Background(), wxConfig(t), req)
		if err != nil || co.Mode != payment.CheckoutQRCode || co.Value != "weixin://wxpay/bizpayurl?pr=abcdef" {
			t.Fatalf("%+v %v", co, err)
		}
	})
	t.Run("商品描述过长时截断，避免被微信拒绝", func(t *testing.T) {
		var desc string
		p := wxServer(t, func(w http.ResponseWriter, r *http.Request) {
			var got map[string]any
			_ = json.NewDecoder(r.Body).Decode(&got)
			desc, _ = got["description"].(string)
			wxReply(t, w, http.StatusOK, `{"code_url":"weixin://x"}`)
		})
		long := req
		long.Subject = strings.Repeat("积分", 100)
		if _, err := p.CreateOrder(context.Background(), wxConfig(t), long); err != nil {
			t.Fatal(err)
		}
		if n := len([]rune(desc)); n == 0 || n > 40 {
			t.Fatalf("描述应截断到 40 字以内：%d", n)
		}
	})
	t.Run("微信返回业务错误：错误里带微信的提示", func(t *testing.T) {
		p := wxServer(t, func(w http.ResponseWriter, r *http.Request) {
			wxReply(t, w, http.StatusBadRequest, `{"code":"PARAM_ERROR","message":"商品描述不合法"}`)
		})
		_, err := p.CreateOrder(context.Background(), wxConfig(t), req)
		if err == nil || !strings.Contains(err.Error(), "商品描述不合法") {
			t.Fatalf("%v", err)
		}
	})
	t.Run("响应签名不对（不是微信发的）：拒绝", func(t *testing.T) {
		p := wxServer(t, func(w http.ResponseWriter, r *http.Request) {
			merchant, _ := wxKeys(t)
			body := `{"code_url":"weixin://evil"}`
			ts, nonce := strconv.FormatInt(time.Now().Unix(), 10), "n"
			w.Header().Set("Wechatpay-Timestamp", ts)
			w.Header().Set("Wechatpay-Nonce", nonce)
			w.Header().Set("Wechatpay-Serial", wxPubKeyID)
			w.Header().Set("Wechatpay-Signature", rsaSign(t, merchant, ts+"\n"+nonce+"\n"+body+"\n"))
			_, _ = w.Write([]byte(body))
		})
		if co, err := p.CreateOrder(context.Background(), wxConfig(t), req); err == nil {
			t.Fatalf("伪造的响应不能被接受：%+v", co)
		}
	})
	t.Run("成功但没有 code_url：报错", func(t *testing.T) {
		p := wxServer(t, func(w http.ResponseWriter, r *http.Request) { wxReply(t, w, http.StatusOK, `{}`) })
		if _, err := p.CreateOrder(context.Background(), wxConfig(t), req); err == nil {
			t.Fatal("应报错")
		}
	})
}

func TestWechat_QueryOrder(t *testing.T) {
	ref := payment.OrderRef{OrderNo: "VC2610160930124821330", Method: payment.MethodWechat}
	tx := func(state, extra string) string {
		return `{"appid":"` + wxAppID + `","mchid":"` + wxMchID + `","out_trade_no":"VC2610160930124821330","transaction_id":"4200001234202610160000000001","trade_state":"` + state + `","amount":{"total":10000,"payer_total":10000,"currency":"CNY"},"success_time":"2026-10-16T10:01:02+08:00"` + extra + `}`
	}
	serve := func(t *testing.T, status int, body string) *wechat.Provider {
		return wxServer(t, func(w http.ResponseWriter, r *http.Request) {
			wxCheckAuth(t, r, "")
			if r.Method != http.MethodGet || r.URL.Path != "/v3/pay/transactions/out-trade-no/VC2610160930124821330" || r.URL.Query().Get("mchid") != wxMchID {
				t.Errorf("查单请求不对：%s", r.URL)
			}
			wxReply(t, w, status, body)
		})
	}
	t.Run("SUCCESS：paid，金额和交易号取自响应", func(t *testing.T) {
		res, err := serve(t, 200, tx("SUCCESS", "")).QueryOrder(context.Background(), wxConfig(t), ref)
		if err != nil || res.State != payment.StatePaid || res.PaidFen != 10000 || res.TradeNo != "4200001234202610160000000001" || res.OrderNo != ref.OrderNo || res.Identity != wxMchID || res.PaidAt.IsZero() {
			t.Fatalf("%+v %v", res, err)
		}
	})
	t.Run("NOTPAY / USERPAYING：pending；CLOSED / REVOKED / PAYERROR：closed", func(t *testing.T) {
		for state, want := range map[string]string{"NOTPAY": payment.StatePending, "USERPAYING": payment.StatePending, "CLOSED": payment.StateClosed, "REVOKED": payment.StateClosed, "PAYERROR": payment.StateClosed} {
			res, err := serve(t, 200, tx(state, "")).QueryOrder(context.Background(), wxConfig(t), ref)
			if err != nil || res.State != want {
				t.Fatalf("%s：%+v %v", state, res, err)
			}
		}
	})
	t.Run("REFUND（已付又退款）：不自动处理，报错让人工核实，绝不当作已付入账", func(t *testing.T) {
		if res, err := serve(t, 200, tx("REFUND", "")).QueryOrder(context.Background(), wxConfig(t), ref); err == nil {
			t.Fatalf("%+v", res)
		}
	})
	t.Run("订单不存在（用户没扫过码）：not_exist，不是错误", func(t *testing.T) {
		res, err := serve(t, 404, `{"code":"ORDER_NOT_EXIST","message":"订单不存在"}`).QueryOrder(context.Background(), wxConfig(t), ref)
		if err != nil || res.State != payment.StateNotExist {
			t.Fatalf("%+v %v", res, err)
		}
	})
	t.Run("商户号、订单号对不上、已付但没有交易号：都是错误", func(t *testing.T) {
		for name, body := range map[string]string{
			"商户号不符": strings.Replace(tx("SUCCESS", ""), wxMchID, "9999999999", 1),
			"订单号不符": strings.Replace(tx("SUCCESS", ""), `"out_trade_no":"VC2610160930124821330"`, `"out_trade_no":"OTHER"`, 1),
			"无交易号":  strings.Replace(tx("SUCCESS", ""), `"transaction_id":"4200001234202610160000000001",`, "", 1),
		} {
			if res, err := serve(t, 200, body).QueryOrder(context.Background(), wxConfig(t), ref); err == nil {
				t.Errorf("%s：应报错，实际 %+v", name, res)
			}
		}
	})
	t.Run("其它业务错误（如签名错误）：报错", func(t *testing.T) {
		if _, err := serve(t, 401, `{"code":"SIGN_ERROR","message":"签名错误"}`).QueryOrder(context.Background(), wxConfig(t), ref); err == nil {
			t.Fatal("应报错")
		}
	})
}

func TestWechat_CloseOrder(t *testing.T) {
	ref := payment.OrderRef{OrderNo: "VC2610160930124821330", Method: payment.MethodWechat}
	closeWith := func(t *testing.T, status int, body string) error {
		p := wxServer(t, func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			wxCheckAuth(t, r, string(b))
			if r.Method != http.MethodPost || r.URL.Path != "/v3/pay/transactions/out-trade-no/VC2610160930124821330/close" || !strings.Contains(string(b), wxMchID) {
				t.Errorf("关单请求不对：%s %s", r.URL.Path, b)
			}
			wxReply(t, w, status, body)
		})
		return p.CloseOrder(context.Background(), wxConfig(t), ref)
	}
	if err := closeWith(t, http.StatusNoContent, ""); err != nil {
		t.Fatalf("关单成功（204）：%v", err)
	}
	if err := closeWith(t, 404, `{"code":"ORDER_NOT_EXIST","message":"订单不存在"}`); err != nil {
		t.Fatalf("订单在微信不存在（用户没扫过码）视为关单成功：%v", err)
	}
	if err := closeWith(t, 400, `{"code":"ORDERCLOSED","message":"订单已关闭"}`); err != nil {
		t.Fatalf("订单已关闭视为关单成功：%v", err)
	}
	if err := closeWith(t, 400, `{"code":"ORDERPAID","message":"订单已支付"}`); err == nil {
		t.Fatal("订单已支付无法关闭，必须报错，让 service 去查单入账")
	}
}

// wxNotify 构造一份微信支付的异步通知：resource 用 APIv3 密钥做 AES-256-GCM 加密，整个请求体用微信的私钥签名。
func wxNotify(t *testing.T, txJSON, apiV3Key string, signer *rsa.PrivateKey) (http.Header, []byte) {
	t.Helper()
	block, err := aes.NewCipher([]byte(apiV3Key))
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce, aad := "abcdef123456", "transaction"
	ct := gcm.Seal(nil, []byte(nonce), []byte(txJSON), []byte(aad))
	body, _ := json.Marshal(map[string]any{
		"id": "EV-20261016100102-0001", "create_time": "2026-10-16T10:01:02+08:00", "resource_type": "encrypt-resource",
		"event_type": "TRANSACTION.SUCCESS", "summary": "支付成功",
		"resource": map[string]string{
			"original_type": "transaction", "algorithm": "AEAD_AES_256_GCM", "ciphertext": base64.StdEncoding.EncodeToString(ct), "associated_data": aad, "nonce": nonce,
		},
	})
	ts, hn := strconv.FormatInt(time.Now().Unix(), 10), "notify-nonce-5678"
	h := http.Header{}
	h.Set("Wechatpay-Timestamp", ts)
	h.Set("Wechatpay-Nonce", hn)
	h.Set("Wechatpay-Serial", wxPubKeyID)
	h.Set("Wechatpay-Signature", rsaSign(t, signer, ts+"\n"+hn+"\n"+string(body)+"\n"))
	h.Set("Wechatpay-Signature-Type", "WECHATPAY2-SHA256-RSA2048")
	return h, body
}

func TestWechat_VerifyNotify(t *testing.T) {
	merchant, gateway := wxKeys(t)
	p := wechat.New(http.DefaultClient)
	cfg := wxConfig(t)
	txJSON := `{"appid":"` + wxAppID + `","mchid":"` + wxMchID + `","out_trade_no":"VC2610160930124821330","transaction_id":"4200001234202610160000000001","trade_state":"SUCCESS","amount":{"total":10000,"payer_total":10000,"currency":"CNY"},"success_time":"2026-10-16T10:01:02+08:00"}`
	req := func(h http.Header, body []byte) payment.NotifyRequest {
		return payment.NotifyRequest{Method: http.MethodPost, Header: h, Body: body}
	}
	t.Run("签名和加密都正确：解密出订单号、交易号、金额和商户号", func(t *testing.T) {
		h, body := wxNotify(t, txJSON, wxAPIv3Key, gateway)
		res, err := p.VerifyNotify(context.Background(), cfg, req(h, body))
		if err != nil || res.State != payment.StatePaid || res.OrderNo != "VC2610160930124821330" || res.TradeNo != "4200001234202610160000000001" || res.PaidFen != 10000 || res.Identity != wxMchID || res.EventKey != "wxpay:EV-20261016100102-0001" {
			t.Fatalf("%+v %v", res, err)
		}
	})
	t.Run("trade_state 不是 SUCCESS：验签通过但不算已付", func(t *testing.T) {
		h, body := wxNotify(t, strings.Replace(txJSON, `"SUCCESS"`, `"CLOSED"`, 1), wxAPIv3Key, gateway)
		res, err := p.VerifyNotify(context.Background(), cfg, req(h, body))
		if err != nil || res.State == payment.StatePaid {
			t.Fatalf("%+v %v", res, err)
		}
	})
	rejects := map[string]func() (http.Header, []byte){
		"请求体被篡改": func() (http.Header, []byte) {
			h, body := wxNotify(t, txJSON, wxAPIv3Key, gateway)
			return h, []byte(strings.Replace(string(body), "EV-2026", "EV-2027", 1))
		},
		"用别的密钥签名": func() (http.Header, []byte) { return wxNotify(t, txJSON, wxAPIv3Key, merchant) },
		"缺少签名头": func() (http.Header, []byte) {
			h, body := wxNotify(t, txJSON, wxAPIv3Key, gateway)
			h.Del("Wechatpay-Signature")
			return h, body
		},
		"公钥 ID 不是我们配置的": func() (http.Header, []byte) {
			h, body := wxNotify(t, txJSON, wxAPIv3Key, gateway)
			h.Set("Wechatpay-Serial", "PUB_KEY_ID_OTHER")
			return h, body
		},
		"APIv3 密钥不对（解密失败）": func() (http.Header, []byte) {
			return wxNotify(t, txJSON, "ffffffffffffffffffffffffffffffff", gateway)
		},
		"商户号不是我们的（签名和加密都正确）": func() (http.Header, []byte) {
			return wxNotify(t, strings.Replace(txJSON, wxMchID, "9999999999", 1), wxAPIv3Key, gateway)
		},
		"AppID 不是我们的（签名和加密都正确）": func() (http.Header, []byte) {
			return wxNotify(t, strings.Replace(txJSON, wxAppID, "wxevil", 1), wxAPIv3Key, gateway)
		},
		"请求体不是 JSON": func() (http.Header, []byte) {
			h, _ := wxNotify(t, txJSON, wxAPIv3Key, gateway)
			return h, []byte("not json")
		},
	}
	for name, build := range rejects {
		t.Run("验签失败："+name, func(t *testing.T) {
			h, body := build()
			if _, err := p.VerifyNotify(context.Background(), cfg, req(h, body)); !errors.Is(err, payment.ErrVerify) {
				t.Fatalf("期望 ErrVerify，实际 %v", err)
			}
		})
	}
}

func TestWechat_AckAndProbe(t *testing.T) {
	p := wechat.New(http.DefaultClient)
	if a := p.NotifyAck(true); a.Status != 200 || !strings.Contains(a.Body, `"SUCCESS"`) || !strings.Contains(a.ContentType, "json") {
		t.Fatalf("微信要求成功应答是 HTTP 200 + JSON {\"code\":\"SUCCESS\"}：%+v", a)
	}
	if a := p.NotifyAck(false); a.Status < 400 || !strings.Contains(a.Body, `"FAIL"`) {
		t.Fatalf("失败应答必须是 4xx/5xx，微信才会重试：%+v", a)
	}
	t.Run("Probe：订单不存在说明商户号、证书和公钥都被接受", func(t *testing.T) {
		pp := wxServer(t, func(w http.ResponseWriter, r *http.Request) {
			wxReply(t, w, 404, `{"code":"ORDER_NOT_EXIST","message":"订单不存在"}`)
		})
		if err := pp.Probe(context.Background(), wxConfig(t)); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("Probe：微信说签名错误（证书序列号或私钥不对）：报错并带原因", func(t *testing.T) {
		pp := wxServer(t, func(w http.ResponseWriter, r *http.Request) {
			wxReply(t, w, 401, `{"code":"SIGN_ERROR","message":"商户证书序列号与私钥不匹配"}`)
		})
		err := pp.Probe(context.Background(), wxConfig(t))
		if err == nil || !strings.Contains(err.Error(), "商户证书序列号") {
			t.Fatalf("%v", err)
		}
	})
}
