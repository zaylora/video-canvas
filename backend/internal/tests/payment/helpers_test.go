package payment_test

import (
	"crypto/md5"
	"encoding/hex"
)

// md5hex 是测试里独立的 MD5 实现入口，不复用被测包的 payutil。
func md5hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}
