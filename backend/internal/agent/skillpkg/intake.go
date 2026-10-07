package skillpkg

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// srcFile 是通过了结构检查、待规范化的文件。
type srcFile struct {
	Path string
	Data []byte
}

// collector 汇总 zip 与文件夹两种输入共用的结构检查：路径、重复、数量、大小。
type collector struct {
	issues  []Issue
	seen    map[string]bool // 小写路径 -> 已出现
	files   []srcFile
	ignored int
	count   int
	total   int64
	once    map[string]bool
	fatal   bool
}

func newCollector() *collector {
	return &collector{seen: map[string]bool{}, once: map[string]bool{}}
}

// add 记录一条错误，并让预检提前结束。
func (c *collector) fail(code, p, msg string) {
	c.fatal = true
	c.issues = append(c.issues, Issue{Level: LevelError, Code: code, Path: p, Message: msg})
}

// failOnce 同一个代码只记录一次，避免上千个文件刷屏。
func (c *collector) failOnce(code, p, msg string) {
	if c.once[code] {
		return
	}
	c.once[code] = true
	c.fail(code, p, msg)
}

var driveRe = regexp.MustCompile(`^[A-Za-z]:`)

// cleanPath 校验并规范化路径：拒绝 NUL、反斜杠、绝对路径、盘符和 ..；去掉空段与 .。
func cleanPath(raw string) (string, bool) {
	if raw == "" || strings.ContainsAny(raw, "\x00\\") || strings.HasPrefix(raw, "/") || driveRe.MatchString(raw) {
		return "", false
	}
	segs := strings.Split(raw, "/")
	out := segs[:0]
	for _, s := range segs {
		switch s {
		case "..":
			return "", false
		case "", ".":
		default:
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return "", false
	}
	return strings.Join(out, "/"), true
}

// isIgnored 判断是否自动忽略的垃圾文件：__MACOSX/、.git/、.DS_Store、Thumbs.db、AppleDouble 的 ._ 文件。
func isIgnored(p string) bool {
	segs := strings.Split(p, "/")
	for _, s := range segs[:len(segs)-1] {
		if s == "__MACOSX" || s == ".git" {
			return true
		}
	}
	base := segs[len(segs)-1]
	return base == ".DS_Store" || base == "Thumbs.db" || strings.HasPrefix(base, "._")
}

// checkPath 校验路径；目录条目只校验不收录。返回规范化路径。
func (c *collector) checkPath(raw string) (string, bool) {
	p, ok := cleanPath(strings.TrimSuffix(raw, "/"))
	if !ok {
		c.failOnce(CodePathEscape, raw, "路径含 ..、绝对路径、盘符、反斜杠或空字符，已拒绝："+printable(raw))
		return "", false
	}
	return p, true
}

// admit 对一个文件做路径、忽略、重复、数量和大小检查，通过则返回规范化路径。
// declared 是声明的解压后大小，用来在读取内容前就拒绝超限的包。
func (c *collector) admit(raw string, declared uint64) (string, bool) {
	p, ok := c.checkPath(raw)
	if !ok {
		return "", false
	}
	if isIgnored(p) {
		c.ignored++
		return "", false
	}
	key := strings.ToLower(p)
	if c.seen[key] {
		c.failOnce(CodeDupPath, p, "存在重复路径（含仅大小写不同）："+p)
		return "", false
	}
	c.seen[key] = true
	c.count++
	if c.count > MaxFiles {
		c.failOnce(CodeTooManyFiles, "", fmt.Sprintf("文件数超过 %d 个，请去掉不需要的文件后重新打包", MaxFiles))
		return "", false
	}
	if declared > MaxFileBytes {
		c.failOnce(CodeFileTooLarge, p, fmt.Sprintf("文件 %s 解压后超过 %d MB", p, MaxFileBytes>>20))
		return "", false
	}
	c.total += int64(declared)
	if c.total > MaxTotalBytes {
		c.failOnce(CodePackageTooLarge, "", fmt.Sprintf("解压后总大小超过 %d MB", MaxTotalBytes>>20))
		return "", false
	}
	return p, true
}

// printable 把路径里的控制字符转成可读形式，避免污染提示文字。
func printable(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return '?'
		}
		return r
	}, s)
}

// decodeName 取 zip 条目的文件名：有效 UTF-8 直接用，否则按 GBK 解码，解不开返回 false。
func decodeName(name string) (string, bool) {
	if utf8.ValidString(name) {
		return name, true
	}
	s, err := simplifiedchinese.GBK.NewDecoder().String(name)
	if err != nil || strings.ContainsRune(s, utf8.RuneError) {
		return "", false
	}
	return s, true
}

// FromZip 在内存里预检一个 zip 技能包。
// 预检不通过不返回 error，问题记在 Result.Issues；error 只用于不可能出现的内部失败。
func FromZip(data []byte) (*Result, error) {
	c := newCollector()
	if len(data) > MaxZipBytes {
		c.fail(CodePackageTooLarge, "", fmt.Sprintf("压缩包超过 %d MB，请去掉不需要的大文件后重新打包", MaxZipBytes>>20))
		return &Result{Issues: c.issues}, nil
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	// 标准库对 ..、绝对路径、反斜杠返回 ErrInsecurePath 但 reader 仍可用，由本包自己判定
	if err != nil && !errors.Is(err, zip.ErrInsecurePath) {
		c.fail(CodeBadZip, "", "不是有效的 zip 压缩包，或文件已损坏")
		return &Result{Issues: c.issues}, nil
	}
	if len(zr.File) > MaxFiles*20 {
		c.fail(CodeTooManyFiles, "", fmt.Sprintf("压缩包条目过多，文件数超过 %d 个", MaxFiles))
		return &Result{Issues: c.issues}, nil
	}

	todo := c.inspectHeaders(zr)
	if c.fatal {
		return &Result{Issues: c.issues}, nil
	}

	// 第二遍解压；文件头声明的大小可能撒谎，所以读取时按声明大小加 1 截断
	for _, t := range todo {
		rc, err := t.f.Open()
		if err != nil {
			c.fail(CodeBadZip, t.path, "无法读取压缩包条目："+t.path)
			continue
		}
		b, err := io.ReadAll(io.LimitReader(rc, int64(t.f.UncompressedSize64)+1))
		_ = rc.Close()
		if err != nil || uint64(len(b)) != t.f.UncompressedSize64 {
			c.fail(CodeBadZip, t.path, "压缩包条目损坏或声明大小与实际不符："+t.path)
			continue
		}
		c.files = append(c.files, srcFile{t.path, b})
	}
	if c.fatal {
		return &Result{Issues: c.issues}, nil
	}
	return finish(c)
}

// FromFiles 预检文件夹拖入或单个 SKILL.md 的输入，规则与 FromZip 相同（没有符号链接与压缩比问题）。
func FromFiles(in []Input) (*Result, error) {
	c := newCollector()
	if len(in) > MaxFiles*20 {
		c.fail(CodeTooManyFiles, "", fmt.Sprintf("文件数超过 %d 个", MaxFiles))
		return &Result{Issues: c.issues}, nil
	}
	for _, f := range in {
		p, ok := c.admit(f.Path, uint64(len(f.Data)))
		if !ok {
			continue
		}
		c.files = append(c.files, srcFile{p, f.Data})
	}
	if c.fatal {
		return &Result{Issues: c.issues}, nil
	}
	return finish(c)
}

// pendingEntry 是通过文件头检查、等待解压的 zip 条目。
type pendingEntry struct {
	f    *zip.File
	path string
}

// inspectHeaders 是第一遍：只看文件头，检查文件名编码、符号链接、路径、数量、声明大小和压缩比，通过后才解压。
func (c *collector) inspectHeaders(zr *zip.Reader) []pendingEntry {
	var todo []pendingEntry
	for _, f := range zr.File {
		name, ok := decodeName(f.Name)
		if !ok {
			c.failOnce(CodeBadFilenameEnc, "", "文件名既不是 UTF-8 也不是 GBK，请用 UTF-8 重新打包")
			continue
		}
		if f.Mode()&os.ModeSymlink != 0 {
			c.failOnce(CodeSymlink, name, "包内含符号链接，已拒绝："+printable(name))
			continue
		}
		if f.FileInfo().IsDir() || strings.HasSuffix(name, "/") {
			c.checkPath(name)
			continue
		}
		if f.Flags&0x1 != 0 {
			c.failOnce(CodeBadZip, name, "压缩包含加密条目，无法读取")
			continue
		}
		p, ok := c.admit(name, f.UncompressedSize64)
		if !ok {
			continue
		}
		if f.UncompressedSize64 >= MinRatioBytes && f.UncompressedSize64/max(f.CompressedSize64, 1) > MaxRatio {
			c.failOnce(CodeZipBomb, p, fmt.Sprintf("文件 %s 压缩比超过 %d:1，疑似压缩炸弹", p, MaxRatio))
			continue
		}
		todo = append(todo, pendingEntry{f, p})
	}
	return todo
}
