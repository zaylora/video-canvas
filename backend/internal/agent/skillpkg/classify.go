package skillpkg

import (
	"bytes"
	"path"
	"strings"
	"unicode/utf8"
)

// scriptLangs 是脚本扩展名到语言名的映射。
var scriptLangs = map[string]string{
	".py": "python", ".js": "javascript", ".mjs": "javascript", ".cjs": "javascript",
	".ts": "typescript", ".sh": "bash", ".bash": "bash", ".rb": "ruby", ".pl": "perl",
}

// assetExts 是图片和数据文件的扩展名，归为 asset。
var assetExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".svg": true, ".ico": true, ".bmp": true,
	".json": true, ".yaml": true, ".yml": true, ".csv": true, ".tsv": true, ".xml": true, ".toml": true, ".pdf": true,
}

// classify 按设计文档 §6.7 的「文件类型」规则返回 kind 和 lang。
func classify(p string) (kind, lang string) {
	if p == "SKILL.md" {
		return KindSkill, ""
	}
	ext := strings.ToLower(path.Ext(p))
	lang = scriptLangs[ext]
	if strings.HasPrefix(p, "scripts/") || lang != "" {
		return KindScript, lang
	}
	if strings.HasPrefix(p, "assets/") {
		return KindAsset, ""
	}
	if ext == ".md" || ext == ".txt" {
		return KindDoc, ""
	}
	if assetExts[ext] {
		return KindAsset, ""
	}
	return KindOther, ""
}

// isText 判断内容是否可预览：有效 UTF-8，且前 8 KB 不含 NUL。
func isText(b []byte) bool {
	head := b
	if len(head) > 8<<10 {
		head = head[:8<<10]
	}
	return utf8.Valid(b) && !bytes.Contains(head, []byte{0})
}

// isExecutable 判断是否 ELF、Mach-O 或 PE 可执行文件（看文件头魔数）。
func isExecutable(b []byte) bool {
	if len(b) < 4 {
		return false
	}
	switch {
	case bytes.HasPrefix(b, []byte("\x7fELF")):
		return true
	case bytes.HasPrefix(b, []byte{0xfe, 0xed, 0xfa, 0xce}), bytes.HasPrefix(b, []byte{0xfe, 0xed, 0xfa, 0xcf}),
		bytes.HasPrefix(b, []byte{0xce, 0xfa, 0xed, 0xfe}), bytes.HasPrefix(b, []byte{0xcf, 0xfa, 0xed, 0xfe}),
		bytes.HasPrefix(b, []byte{0xca, 0xfe, 0xba, 0xbe}):
		return true
	case bytes.HasPrefix(b, []byte("MZ")):
		// PE：两字节魔数太短，要求带 NUL 的二进制内容，避免误伤以 MZ 开头的文本
		return len(b) >= 8 && !isText(b)
	}
	return false
}
