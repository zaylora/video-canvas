// Package payutil 是手写支付渠道（易支付、虎皮椒）共用的小工具：金额换算、签名串拼接、通知参数解析。
// 金额全部用整数分和字符串换算，不经过浮点数。
package payutil

import (
	"bytes"
	"crypto/md5" //nolint:gosec // G501：易支付、虎皮椒的协议规定用 MD5 签名，不是我们选的算法
	"crypto/rsa"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"video-canvas/internal/payment"
)

// YuanFromFen 把分换算成「元」字符串，固定两位小数：10000 → "100.00"。
func YuanFromFen(fen int) string {
	return fmt.Sprintf("%d.%02d", fen/100, fen%100)
}

// FenFromYuan 把「元」字符串精确换算成分。只接受形如 "10"、"10.5"、"10.50" 的非负十进制数，
// 小数位超过两位、带符号、千分位、科学计数法、空串一律报错，绝不四舍五入：金额不对宁可拒绝。
func FenFromYuan(s string) (int, error) {
	s = strings.TrimSpace(s)
	whole, frac, hasDot := strings.Cut(s, ".")
	if whole == "" || !allDigits(whole) || (hasDot && (frac == "" || len(frac) > 2 || !allDigits(frac))) {
		return 0, fmt.Errorf("金额格式无效：%q", s)
	}
	yuan, err := strconv.Atoi(whole)
	if err != nil {
		return 0, fmt.Errorf("金额格式无效：%q", s)
	}
	cents := 0
	if hasDot {
		cents, _ = strconv.Atoi((frac + "0")[:2]) // frac 已校验是 1–2 位数字，右补 0 到两位
	}
	if yuan > (1<<31-1)/100 {
		return 0, fmt.Errorf("金额过大：%q", s)
	}
	return yuan*100 + cents, nil
}

// allDigits 判断 s 非空且全是 ASCII 数字。
func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// MD5Hex 返回 s 的 MD5 十六进制小写串。
func MD5Hex(s string) string {
	sum := md5.Sum([]byte(s)) //nolint:gosec // G401：协议规定
	return hex.EncodeToString(sum[:])
}

// Canonical 把参数按键的 ASCII 升序拼成 k=v&k=v，跳过空值和 exclude 里的键，这是易支付和虎皮椒共同的待签名串规则。
func Canonical(values map[string]string, exclude ...string) string {
	skip := make(map[string]bool, len(exclude))
	for _, k := range exclude {
		skip[k] = true
	}
	keys := make([]string, 0, len(values))
	for k, v := range values {
		if v != "" && !skip[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + values[k]
	}
	return strings.Join(parts, "&")
}

// SafeEqual 常量时间比较两个签名；空串永远不相等，否则缺少 sign 参数的请求会被放过。
func SafeEqual(actual, expected string) bool {
	return actual != "" && len(actual) == len(expected) && subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) == 1
}

// ErrBodyTooLarge 表示渠道响应超过了读取上限。
var ErrBodyTooLarge = errors.New("渠道响应过大")

// ReadLimited 读取至多 limit 字节；超过就报错，防止异常响应撑爆内存。
func ReadLimited(r io.Reader, limit int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, ErrBodyTooLarge
	}
	return b, nil
}

// NotifyValues 把异步通知拍平成 键 → 值。GET 取 query；POST 先取 query 再合并 body（表单或 JSON 对象，JSON 里的
// 数字、布尔都转成字符串）。同一个键出现多次一律报错：重复参数是常见的验签绕过手法。
func NotifyValues(req payment.NotifyRequest) (map[string]string, error) {
	out := map[string]string{}
	add := func(k, v string) error {
		if _, dup := out[k]; dup {
			return fmt.Errorf("通知参数重复：%s", k)
		}
		out[k] = v
		return nil
	}
	for k, vs := range req.Query {
		if len(vs) != 1 {
			return nil, fmt.Errorf("通知参数重复：%s", k)
		}
		if err := add(k, vs[0]); err != nil {
			return nil, err
		}
	}
	body := bytes.TrimSpace(req.Body)
	if req.Method == http.MethodPost && len(body) > 0 {
		if err := mergeBody(body, add); err != nil {
			return nil, err
		}
	}
	if len(out) == 0 {
		return nil, errors.New("通知内容为空")
	}
	return out, nil
}

// mergeBody 解析 body：以 { 开头按 JSON 对象，否则按 urlencoded 表单。
func mergeBody(body []byte, add func(k, v string) error) error {
	if body[0] == '{' {
		var obj map[string]any
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.UseNumber() // 保留数字原文，避免 1.50 变成 1.5、大整数变科学计数法
		if err := dec.Decode(&obj); err != nil {
			return fmt.Errorf("通知不是合法的 JSON：%w", err)
		}
		for k, v := range obj {
			if err := add(k, scalarString(v)); err != nil {
				return err
			}
		}
		return nil
	}
	form, err := url.ParseQuery(string(body))
	if err != nil {
		return fmt.Errorf("通知不是合法的表单：%w", err)
	}
	for k, vs := range form {
		if len(vs) != 1 {
			return fmt.Errorf("通知参数重复：%s", k)
		}
		if err := add(k, vs[0]); err != nil {
			return err
		}
	}
	return nil
}

// scalarString 把 JSON 标量转成字符串；null 和嵌套结构按空串处理（它们不会参与金额和订单号判断）。
func scalarString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	case bool:
		return strconv.FormatBool(t)
	default:
		return ""
	}
}

// JSONStrings 把 JSON 对象的顶层字段拍平成 键 → 字符串，数字保留原文（1.50 不会变成 1.5）。渠道响应的字段类型不统一
// （同一个字段有的站点返回数字、有的返回字符串），统一成字符串再判断最稳。
func JSONStrings(body []byte) (map[string]string, error) {
	var obj map[string]any
	dec := json.NewDecoder(bytes.NewReader(bytes.TrimSpace(body)))
	dec.UseNumber()
	if err := dec.Decode(&obj); err != nil {
		return nil, fmt.Errorf("渠道响应不是合法的 JSON 对象：%w", err)
	}
	out := make(map[string]string, len(obj))
	for k, v := range obj {
		out[k] = scalarString(v)
	}
	return out, nil
}

// cnLocation 是国内支付渠道时间字段所用的时区（东八区）。
var cnLocation = time.FixedZone("CST", 8*3600)

// ParseCNTime 解析渠道返回的 "2006-01-02 15:04:05" 本地时间（东八区）；格式不对返回零值，调用方自行兜底。
func ParseCNTime(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04:05", strings.TrimSpace(s), cnLocation)
	if err != nil {
		return time.Time{}
	}
	return t
}

// StripURLError 去掉 *url.Error 里的请求 URL：有的渠道把密钥放在查询参数里，Go 的网络错误会把整条 URL 带进错误文本，
// 日志和失败原因里就会泄露密钥。只保留底层原因（连接被拒、超时……）。
func StripURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("%s：%w", ue.Op, ue.Err)
	}
	return err
}

// FormatCNTime 把时间格式化成渠道常用的 "2006-01-02 15:04:05"（东八区）。
func FormatCNTime(t time.Time) string {
	return t.In(cnLocation).Format("2006-01-02 15:04:05")
}

// RSAPrivateKey 解析 RSA 私钥：接受 PEM（PKCS#8 或 PKCS#1）和支付平台后台常见的单行 base64（可带空白换行）。
func RSAPrivateKey(raw string) (*rsa.PrivateKey, error) {
	der, err := keyDER(raw)
	if err != nil {
		return nil, err
	}
	if k8, err8 := x509.ParsePKCS8PrivateKey(der); err8 == nil {
		rk, ok := k8.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("不是 RSA 私钥")
		}
		return rk, nil
	}
	k1, err := x509.ParsePKCS1PrivateKey(der)
	if err != nil {
		return nil, errors.New("无法解析，需要 RSA 私钥（PKCS#8 或 PKCS#1）")
	}
	return k1, nil
}

// RSAPublicKey 解析 RSA 公钥：接受 PEM（PKIX 或 PKCS#1）和单行 base64。
func RSAPublicKey(raw string) (*rsa.PublicKey, error) {
	der, err := keyDER(raw)
	if err != nil {
		return nil, err
	}
	if pk, errPKIX := x509.ParsePKIXPublicKey(der); errPKIX == nil {
		rk, ok := pk.(*rsa.PublicKey)
		if !ok {
			return nil, errors.New("不是 RSA 公钥")
		}
		return rk, nil
	}
	k1, err := x509.ParsePKCS1PublicKey(der)
	if err != nil {
		return nil, errors.New("无法解析，需要 RSA 公钥")
	}
	return k1, nil
}

// PrivateKeyPEM 把私钥规范成 PKCS#8 PEM，给只认 PEM 的 SDK 用。
func PrivateKeyPEM(raw string) (string, error) {
	k, err := RSAPrivateKey(raw)
	if err != nil {
		return "", err
	}
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), nil
}

// PublicKeyPEM 把公钥规范成 PKIX PEM。
func PublicKeyPEM(raw string) (string, error) {
	k, err := RSAPublicKey(raw)
	if err != nil {
		return "", err
	}
	der, err := x509.MarshalPKIXPublicKey(k)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})), nil
}

// keyDER 取出密钥的 DER 字节：有 PEM 头就按 PEM 解，否则当作（可能带空白和换行的）单行 base64。
func keyDER(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("未填写")
	}
	if block, _ := pem.Decode([]byte(raw)); block != nil {
		return block.Bytes, nil
	}
	der, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(raw), ""))
	if err != nil {
		return nil, errors.New("既不是 PEM 也不是 base64")
	}
	return der, nil
}

// InCN 把时间换算到东八区：微信支付的 time_expire 要求带 +08:00 时区的 RFC3339，不接受 Z。
func InCN(t time.Time) time.Time { return t.In(cnLocation) }
