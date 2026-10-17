package payment_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"video-canvas/internal/payment"
	"video-canvas/internal/payment/payutil"
)

func TestYuanFromFen(t *testing.T) {
	tests := []struct {
		fen  int
		want string
	}{
		{0, "0.00"}, {5, "0.05"}, {10, "0.10"}, {100, "1.00"}, {10000, "100.00"}, {123456, "1234.56"}, {99, "0.99"},
	}
	for _, tt := range tests {
		if got := payutil.YuanFromFen(tt.fen); got != tt.want {
			t.Errorf("YuanFromFen(%d) = %q，期望 %q", tt.fen, got, tt.want)
		}
	}
}

func TestFenFromYuan(t *testing.T) {
	ok := []struct {
		in   string
		want int
	}{
		{"10.00", 1000}, {"10", 1000}, {"0.1", 10}, {"0.01", 1}, {" 100.00 ", 10000}, {"1234.56", 123456}, {"0", 0}, {"10.0", 1000},
	}
	for _, tt := range ok {
		got, err := payutil.FenFromYuan(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("FenFromYuan(%q) = %d, %v，期望 %d", tt.in, got, err, tt.want)
		}
	}
	// 钱的换算不能宽松：多余小数位、符号、千分位、科学计数法、空串都必须拒绝，而不是悄悄四舍五入
	for _, in := range []string{"", "abc", "-1.00", "+1.00", "1,000.00", "1e3", "10.001", "1.2.3", ".5", "10.", "NaN", "99999999999999999999"} {
		if got, err := payutil.FenFromYuan(in); err == nil {
			t.Errorf("FenFromYuan(%q) 应报错，实际返回 %d", in, got)
		}
	}
}

func TestMD5Hex(t *testing.T) {
	if got := payutil.MD5Hex("abc"); got != "900150983cd24fb0d6963f7d28e17f72" {
		t.Fatalf("%s", got)
	}
}

func TestCanonical(t *testing.T) {
	vals := map[string]string{"b": "2", "a": "1", "sign": "x", "sign_type": "MD5", "empty": "", "c": "3"}
	if got := payutil.Canonical(vals, "sign", "sign_type"); got != "a=1&b=2&c=3" {
		t.Fatalf("应按键升序、跳过空值和被排除的键：%q", got)
	}
	if got := payutil.Canonical(map[string]string{}); got != "" {
		t.Fatalf("%q", got)
	}
}

func TestSafeEqual(t *testing.T) {
	if !payutil.SafeEqual("abc", "abc") || payutil.SafeEqual("abc", "abd") || payutil.SafeEqual("abc", "ab") {
		t.Fatal("比较结果不对")
	}
	if payutil.SafeEqual("", "") {
		t.Fatal("空签名不能算相等：否则缺少 sign 参数的请求会被放过")
	}
}

func TestReadLimited(t *testing.T) {
	if b, err := payutil.ReadLimited(strings.NewReader("hello"), 10); err != nil || string(b) != "hello" {
		t.Fatalf("%q %v", b, err)
	}
	if _, err := payutil.ReadLimited(strings.NewReader(strings.Repeat("x", 11)), 10); err == nil {
		t.Fatal("超过上限应报错，防止渠道返回超大响应撑爆内存")
	}
}

func TestNotifyValues(t *testing.T) {
	t.Run("GET：取 query", func(t *testing.T) {
		got, err := payutil.NotifyValues(payment.NotifyRequest{Method: http.MethodGet, Query: url.Values{"a": {"1"}, "b": {"2"}}})
		if err != nil || got["a"] != "1" || got["b"] != "2" {
			t.Fatalf("%v %v", got, err)
		}
	})
	t.Run("POST 表单：取 body", func(t *testing.T) {
		got, err := payutil.NotifyValues(payment.NotifyRequest{Method: http.MethodPost, Body: []byte("a=1&b=%E4%B8%AD")})
		if err != nil || got["a"] != "1" || got["b"] != "中" {
			t.Fatalf("%v %v", got, err)
		}
	})
	t.Run("POST JSON：数字和字符串都转成字符串", func(t *testing.T) {
		got, err := payutil.NotifyValues(payment.NotifyRequest{Method: http.MethodPost, Body: []byte(`{"a":"1","n":12,"f":1.5,"t":true}`)})
		if err != nil || got["a"] != "1" || got["n"] != "12" || got["f"] != "1.5" {
			t.Fatalf("%v %v", got, err)
		}
	})
	t.Run("同名参数重复出现：拒绝，防止参数污染绕过验签", func(t *testing.T) {
		if _, err := payutil.NotifyValues(payment.NotifyRequest{Method: http.MethodGet, Query: url.Values{"a": {"1", "2"}}}); err == nil {
			t.Fatal("重复参数应报错")
		}
		if _, err := payutil.NotifyValues(payment.NotifyRequest{Method: http.MethodPost, Body: []byte("a=1&a=2")}); err == nil {
			t.Fatal("重复参数应报错")
		}
	})
	t.Run("空请求报错", func(t *testing.T) {
		if _, err := payutil.NotifyValues(payment.NotifyRequest{Method: http.MethodPost}); err == nil {
			t.Fatal("应报错")
		}
	})
}
