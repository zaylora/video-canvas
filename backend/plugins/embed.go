// Package plugins 随二进制发布内置协议插件（JS 源码）。启动时由 service 按 key + version 自动登记为 builtin 来源，
// 内置插件不能被删除或覆盖；新版本随发版登记，渠道仍需显式切换（见 docs/design/协议插件设计.md 6.7）。
package plugins

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
)

//go:embed *.js
var files embed.FS

// Builtin 返回全部内置插件的源码，按文件名排序（顺序稳定，启动日志可读）。
func Builtin() ([][]byte, error) {
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		return nil, fmt.Errorf("读取内置插件目录失败：%w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	out := make([][]byte, 0, len(names))
	for _, n := range names {
		b, err := files.ReadFile(n)
		if err != nil {
			return nil, fmt.Errorf("读取内置插件 %s 失败：%w", n, err)
		}
		out = append(out, b)
	}
	return out, nil
}
