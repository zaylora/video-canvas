package storage

import (
	"errors"
	"fmt"
	"strings"
)

// maxKeyLen 对象 key 的最大长度，与 assets.storage_key 列宽（512）保持一致。
const maxKeyLen = 512

// ErrInvalidKey 对象 key 不合法（含路径穿越、绝对路径、反斜杠等危险写法）。
var ErrInvalidKey = errors.New("storage: invalid object key")

// ValidateKey 校验对象 key 只含安全字符，防止路径穿越。
//
// 规则：
//   - 只允许 a-z A-Z 0-9 . _ - 和路径分隔符 /
//   - 不允许以 / 开头（绝对路径）、不允许出现反斜杠或盘符冒号（字符白名单已排除）
//   - 不允许空段（连续 //、末尾 /）以及以 . 开头的段（排除 . / .. / 隐藏文件，
//     也保证不会和本地存储写入时使用的 .tmp- 临时文件重名）
func ValidateKey(key string) error {
	if key == "" || len(key) > maxKeyLen {
		return fmt.Errorf("%w: 长度必须在 1-%d 之间", ErrInvalidKey, maxKeyLen)
	}
	for _, seg := range strings.Split(key, "/") {
		if seg == "" {
			return fmt.Errorf("%w: 不能以 / 开头或包含空路径段", ErrInvalidKey)
		}
		if seg[0] == '.' {
			return fmt.Errorf("%w: 路径段不能以 . 开头", ErrInvalidKey)
		}
		for i := 0; i < len(seg); i++ {
			if !isSafeKeyByte(seg[i]) {
				return fmt.Errorf("%w: 含有不允许的字符", ErrInvalidKey)
			}
		}
	}
	return nil
}

func isSafeKeyByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	case b == '.', b == '_', b == '-':
		return true
	}
	return false
}
