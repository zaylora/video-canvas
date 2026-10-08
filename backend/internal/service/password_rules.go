package service

import (
	_ "embed" // 弱密码表随二进制分发
	"strings"

	"video-canvas/internal/pkg/errcode"
)

// 统一密码规则（docs/design/个人中心/个人中心.md §6.8）：注册、个人中心改密码、后台手填重置三处共用 validateNewPassword。
// 登录不走这里：登录仍接受 6..128 位，保证存量的 6–7 位密码可以登录。

//nolint:gosec // G101 误报：这些是长度常量与提示文案，不是凭证
const (
	minPasswordBytes = 8  // 新密码最短字节数
	maxPasswordBytes = 72 // bcrypt 只取前 72 字节，超过会被 x/crypto 拒绝，所以这也是上限

	msgPasswordTooShort = "密码至少 8 位"
	msgPasswordTooLong  = "密码最多 72 字节（约 24 个汉字或 72 个英文字符）"
	msgPasswordIsName   = "密码不能与用户名相同"
)

// weakPasswordsRaw 是内置的常见弱密码表（约 1000 条，每行一条，全小写，长度 8..72 字节）。
//
//go:embed weak_passwords.txt
var weakPasswordsRaw string

// weakPasswords 是弱密码表的集合，包初始化时从 weakPasswordsRaw 构建，之后只读。
var weakPasswords = buildWeakPasswords(weakPasswordsRaw)

// buildWeakPasswords 把每行一条的文本解析成小写集合，忽略空行。
func buildWeakPasswords(raw string) map[string]struct{} {
	set := map[string]struct{}{}
	for _, line := range strings.Split(raw, "\n") {
		if w := strings.ToLower(strings.TrimSpace(line)); w != "" {
			set[w] = struct{}{}
		}
	}
	return set
}

// validateNewPassword 校验新密码是否满足统一规则：
//   - 长度按 UTF-8 字节计 8..72，否则 10001（文案区分太短 / 太长）；
//   - 不能等于用户名（不区分大小写），否则 55003，文案“密码不能与用户名相同”；
//   - 不能命中弱密码表（不区分大小写），否则 55003。
//
// 不要求字符组合：按 NIST 800-63B，组合规则只会让用户写出更好猜的密码。
func validateNewPassword(username, pw string) error {
	// 1. 长度：按字节算，因为 bcrypt 的上限是字节而不是字符
	if len(pw) < minPasswordBytes {
		return errcode.ErrInvalidParams.WithMsg(msgPasswordTooShort)
	}
	if len(pw) > maxPasswordBytes {
		return errcode.ErrInvalidParams.WithMsg(msgPasswordTooLong)
	}
	// 2. 等于用户名：用户名是公开信息，等于它就等于没有密码
	lower := strings.ToLower(pw)
	if username != "" && lower == strings.ToLower(username) {
		return errcode.ErrPasswordWeak.WithMsg(msgPasswordIsName)
	}
	// 3. 弱密码表：撞库字典里排在最前面的那批，拦住它们收益最大
	if _, hit := weakPasswords[lower]; hit {
		return errcode.ErrPasswordWeak
	}
	return nil
}
