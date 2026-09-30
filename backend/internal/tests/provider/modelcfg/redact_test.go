// 本文件：redact.go 的单元测试：密钥脱敏（含 URL 转义形式、长 secret 优先、不修改入参）。

package modelcfg_test

import (
	"encoding/json"
	"strings"
	"testing"

	"video-canvas/internal/provider/modelcfg"
)

func TestRedact(t *testing.T) {
	const secret = "sk-abc123"
	in := map[string]any{
		"Authorization": "Bearer " + secret,
		"list":          []any{"x", "k=" + secret, 5.0, nil, true},
		"nested":        map[string]any{"a": []any{map[string]any{"b": secret + secret}}},
		"typed":         []map[string]any{{"c": secret}},
		"headers":       map[string]string{"X": secret},
		"header2":       map[string][]string{"X": {secret, "ok"}},
		"strs":          []string{secret},
		"num":           42,
		"query":         "key=sk-abc123&other=1",
	}
	out := modelcfg.Redact(in, secret, "", "unused-secret").(map[string]any)

	b, _ := json.Marshal(out)
	if strings.Contains(string(b), secret) {
		t.Fatalf("脱敏后仍含明文：%s", b)
	}
	if out["Authorization"] != "Bearer ***" {
		t.Fatalf("Authorization = %v", out["Authorization"])
	}
	if out["num"] != 42 {
		t.Fatalf("非字符串应原样：%v", out["num"])
	}
	if out["query"] != "key=***&other=1" {
		t.Fatalf("query = %v", out["query"])
	}
	// 原对象不能被修改
	if in["Authorization"] != "Bearer "+secret {
		t.Fatal("入参被修改")
	}
}

func TestRedact_边界(t *testing.T) {
	t.Run("没有 secret 原样返回", func(t *testing.T) {
		if got := modelcfg.Redact("abc"); got != "abc" {
			t.Fatal(got)
		}
		if got := modelcfg.Redact("abc", ""); got != "abc" {
			t.Fatal("空 secret 不应把内容替换掉")
		}
	})
	t.Run("URL 转义形式也脱敏", func(t *testing.T) {
		got := modelcfg.Redact("https://x.test/?k=a%2Bb%26c", "a+b&c")
		if got != "https://x.test/?k=***" {
			t.Fatal(got)
		}
	})
	t.Run("长 secret 优先，不留残片", func(t *testing.T) {
		got := modelcfg.Redact("token=abcdef", "abc", "abcdef")
		if got != "token=***" {
			t.Fatal(got)
		}
	})
	t.Run("nil 与字节", func(t *testing.T) {
		if modelcfg.Redact(nil, "x") != nil {
			t.Fatal("nil 应原样")
		}
		if got := modelcfg.Redact([]byte("a x b"), "x").([]byte); string(got) != "a *** b" {
			t.Fatal(string(got))
		}
	})
}
