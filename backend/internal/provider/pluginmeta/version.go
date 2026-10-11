// 本文件：插件版本号的辅助函数：合法性、semver 优先级比较、补丁位递增。
// 上传插件时版本号由服务层决定（见 docs/design/插件简化/插件简化.md §6.5），这里只提供纯函数。

package pluginmeta

import (
	"fmt"
	"strconv"
	"strings"
)

// IsValidVersion 判断是不是合法的插件版本号：semver（如 1.2.3、1.2.3-rc.1），且不超过 MaxVersionLen 个字符。
func IsValidVersion(v string) bool {
	return len(v) <= MaxVersionLen && semverRe.MatchString(v)
}

// semver 是拆开的版本号。数字部分放得进 uint64 才算解析成功。
type semver struct {
	major, minor, patch uint64
	pre                 []string // 预发布标识，按 . 拆开；没有预发布为 nil
}

func parseSemver(v string) (semver, bool) {
	m := semverRe.FindStringSubmatch(v)
	if m == nil {
		return semver{}, false
	}
	var out semver
	for i, dst := range []*uint64{&out.major, &out.minor, &out.patch} {
		n, err := strconv.ParseUint(m[i+1], 10, 64)
		if err != nil {
			return semver{}, false
		}
		*dst = n
	}
	if pre := strings.TrimPrefix(m[4], "-"); pre != "" {
		out.pre = strings.Split(pre, ".")
	}
	return out, true
}

// CompareVersions 按 semver 2.0 的优先级规则比较两个版本号，返回 -1、0、1。
// 主、次、补丁按数字比；同一个版本的预发布版低于正式版；预发布标识逐段比：都是数字按数字比，
// 数字低于字母，字母按字典序，前面都相同时段数少的低。不合法的版本号一律视为低于合法的，两个都不合法按字符串比。
func CompareVersions(a, b string) int {
	x, okA := parseSemver(a)
	y, okB := parseSemver(b)
	switch {
	case !okA && !okB:
		return strings.Compare(a, b)
	case !okA:
		return -1
	case !okB:
		return 1
	}
	for _, p := range [][2]uint64{{x.major, y.major}, {x.minor, y.minor}, {x.patch, y.patch}} {
		if p[0] != p[1] {
			if p[0] < p[1] {
				return -1
			}
			return 1
		}
	}
	return comparePre(x.pre, y.pre)
}

func comparePre(a, b []string) int {
	switch {
	case len(a) == 0 && len(b) == 0:
		return 0
	case len(a) == 0:
		return 1 // 正式版高于预发布版
	case len(b) == 0:
		return -1
	}
	for i := 0; i < len(a) && i < len(b); i++ {
		if c := compareIdentifier(a[i], b[i]); c != 0 {
			return c
		}
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	}
	return 0
}

func compareIdentifier(a, b string) int {
	na, errA := strconv.ParseUint(a, 10, 64)
	nb, errB := strconv.ParseUint(b, 10, 64)
	switch {
	case errA == nil && errB == nil:
		switch {
		case na < nb:
			return -1
		case na > nb:
			return 1
		}
		return 0
	case errA == nil:
		return -1 // 数字标识低于字母标识
	case errB == nil:
		return 1
	}
	return strings.Compare(a, b)
}

// HighestVersion 返回版本号里优先级最高的一个；没有合法的版本号返回空串。不合法的版本号被忽略。
func HighestVersion(versions []string) string {
	best := ""
	for _, v := range versions {
		if _, ok := parseSemver(v); !ok {
			continue
		}
		if best == "" || CompareVersions(v, best) > 0 {
			best = v
		}
	}
	return best
}

// NextPatchVersion 返回补丁位加 1 的版本号，并去掉预发布后缀：1.3.1 → 1.3.2，2.0.0-rc.1 → 2.0.1。
// 版本号不合法，或补丁位已经是 uint64 的最大值时返回错误。
func NextPatchVersion(v string) (string, error) {
	p, ok := parseSemver(v)
	if !ok {
		return "", fmt.Errorf("版本号 %q 不是合法的 semver", v)
	}
	if p.patch == ^uint64(0) {
		return "", fmt.Errorf("版本号 %q 的补丁位已经到上限，无法递增", v)
	}
	return fmt.Sprintf("%d.%d.%d", p.major, p.minor, p.patch+1), nil
}
