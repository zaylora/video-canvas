package pluginrunner_test

import (
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"strings"
	"testing"

	"video-canvas/internal/provider/pluginrunner"
)

// hmacSHA256Hex 的密钥是十六进制文本（解码成字节再做 HMAC），这样上一步 hmacSHA256 的十六进制输出
// 可以直接当下一步的密钥，实现火山引擎这类“逐级派生密钥”的签名。
func TestUtilsHmacSHA256HexChainsDerivedKeys(t *testing.T) {
	c := newClient(pluginrunner.Options{})
	code := `module.exports = {buildSubmitRequest: function() {
		var k1 = utils.hmacSHA256("secret", "20260101");
		return utils.hmacSHA256Hex(k1, "cn-north-1");
	}}`
	resp := callHook(t, c, code, "buildSubmitRequest", `{}`)
	if resp.Error != nil {
		t.Fatalf("调用失败：%+v", resp.Error)
	}
	m1 := hmac.New(sha256.New, []byte("secret"))
	m1.Write([]byte("20260101"))
	m2 := hmac.New(sha256.New, m1.Sum(nil))
	m2.Write([]byte("cn-north-1"))
	want, _ := json.Marshal(hex.EncodeToString(m2.Sum(nil)))
	if string(resp.Result) != string(want) {
		t.Fatalf("派生密钥签名不符：%s，应为 %s", resp.Result, want)
	}
}

func TestUtilsHmacSHA256HexRejectsBadKey(t *testing.T) {
	c := newClient(pluginrunner.Options{})
	resp := callHook(t, c, `module.exports = {buildSubmitRequest: function() { return utils.hmacSHA256Hex("zz", "m"); }}`, "buildSubmitRequest", `{}`)
	if resp.Error == nil {
		t.Fatalf("非十六进制密钥应抛错：%s", resp.Result)
	}
}

func TestUtilsJwtSignRS256(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	pkcs8 := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	pkcs1 := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))

	for name, pemText := range map[string]string{"pkcs8": pkcs8, "pkcs1": pkcs1} {
		t.Run(name, func(t *testing.T) {
			c := newClient(pluginrunner.Options{})
			code := `module.exports = {buildSubmitRequest: function(ctx) {
				return utils.jwtSignRS256({iss: "sa@x.iam", aud: "https://oauth2.googleapis.com/token"}, ctx.pem);
			}}`
			arg, _ := json.Marshal(map[string]string{"pem": pemText})
			resp := callHook(t, c, code, "buildSubmitRequest", string(arg))
			if resp.Error != nil {
				t.Fatalf("调用失败：%+v", resp.Error)
			}
			var token string
			if err := json.Unmarshal(resp.Result, &token); err != nil {
				t.Fatal(err)
			}
			parts := strings.Split(token, ".")
			if len(parts) != 3 {
				t.Fatalf("JWT 应有三段：%s", token)
			}
			header, _ := base64.RawURLEncoding.DecodeString(parts[0])
			if string(header) != `{"alg":"RS256","typ":"JWT"}` {
				t.Fatalf("header 不符：%s", header)
			}
			sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
			sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
			if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, sum[:], sig); err != nil {
				t.Fatalf("签名验证失败：%v", err)
			}
		})
	}
}

func TestUtilsJwtSignRS256RejectsBadKey(t *testing.T) {
	c := newClient(pluginrunner.Options{})
	resp := callHook(t, c, `module.exports = {buildSubmitRequest: function() { return utils.jwtSignRS256({a: 1}, "not a pem"); }}`, "buildSubmitRequest", `{}`)
	if resp.Error == nil {
		t.Fatalf("无效私钥应抛错：%s", resp.Result)
	}
}
