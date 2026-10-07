package skillpkg

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// fixedTime 是规范化 zip 里所有条目的修改时间，保证同内容得到同一哈希。
var fixedTime = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)

// knownFields 是本系统已支持的 frontmatter 字段；其余（含 allowed-tools）进入 Unsupported。
var knownFields = map[string]bool{"name": true, "description": true, "license": true, "compatibility": true, "metadata": true}

// finish 在结构检查通过后做规范化、frontmatter 校验、文件清单和提示类检查。
func finish(c *collector) (*Result, error) {
	r := &Result{Issues: c.issues, Frontmatter: map[string]any{}}
	files := c.files

	if c.ignored > 0 {
		r.Issues = append(r.Issues, Issue{Level: LevelInfo, Code: CodeIgnoredFiles,
			Message: fmt.Sprintf("已自动忽略 %d 个垃圾文件（__MACOSX、.DS_Store、Thumbs.db、.git 等）", c.ignored)})
	}

	outerDir := stripOuterDir(files)
	if root := r.locateSkillMD(files); root >= 0 {
		files[root].Path = "SKILL.md"
		r.checkSkillMD(files[root].Data, files, outerDir)
	}

	r.inventory(files)

	sortIssues(r.Issues)

	pkg, err := canonicalZip(files)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(pkg)
	r.Package = pkg
	r.SHA256 = hex.EncodeToString(sum[:])
	return r, nil
}

// stripOuterDir 去掉唯一的外层目录并返回它的名字；没有则返回空串。
func stripOuterDir(files []srcFile) string {
	if len(files) == 0 {
		return ""
	}
	first := strings.SplitN(files[0].Path, "/", 2)
	if len(first) < 2 {
		return ""
	}
	for _, f := range files {
		if !strings.HasPrefix(f.Path, first[0]+"/") {
			return ""
		}
	}
	for i := range files {
		files[i].Path = strings.TrimPrefix(files[i].Path, first[0]+"/")
	}
	return first[0]
}

// locateSkillMD 在根目录找 SKILL.md（不区分大小写），返回下标，找不到返回 -1，并记录缺失或多个的错误。
func (r *Result) locateSkillMD(files []srcFile) int {
	root, nested := -1, 0
	for i, f := range files {
		if !strings.EqualFold(path.Base(f.Path), "SKILL.md") {
			continue
		}
		if f.Path == path.Base(f.Path) {
			root = i
		} else {
			nested++
		}
	}
	switch {
	case root >= 0 && nested == 0:
	case root >= 0 || nested > 1:
		r.addIssue(LevelError, CodeMultiSkillMD, "", "包里有多个 SKILL.md，一次只能导入一个技能")
	case nested == 1:
		r.addIssue(LevelError, CodeNoSkillMD, "", "SKILL.md 不在包的根目录，请把它放到最外层")
	default:
		r.addIssue(LevelError, CodeNoSkillMD, "", "包里没有 SKILL.md")
	}
	return root
}

// inventory 按路径排序文件，生成清单、分类，并提示脚本与可执行文件。
func (r *Result) inventory(files []srcFile) {
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	for _, f := range files {
		kind, lang := classify(f.Path)
		sum := sha256.Sum256(f.Data)
		r.Files = append(r.Files, File{Path: f.Path, Size: int64(len(f.Data)), SHA256: hex.EncodeToString(sum[:]),
			Kind: kind, Lang: lang, Text: isText(f.Data)})
		r.TotalBytes += int64(len(f.Data))
		r.HasScripts = r.HasScripts || kind == KindScript
		if isExecutable(f.Data) {
			r.addIssue(LevelWarn, CodeBinaryExecutable, f.Path, "包内有可执行文件，导入后不会被执行："+f.Path)
		}
	}
	if r.HasScripts {
		r.addIssue(LevelWarn, CodeHasScripts, "", "包内有脚本文件；当前版本只保存，不会执行")
	}
}

func (r *Result) addIssue(level, code, p, msg string) {
	r.Issues = append(r.Issues, Issue{Level: level, Code: code, Path: p, Message: msg})
}

// checkSkillMD 解析并校验 SKILL.md 头部，填充 Name、Description 等，并检查行数与相对引用。
func (r *Result) checkSkillMD(data []byte, files []srcFile, outerDir string) {
	if !utf8.Valid(data) {
		r.addIssue(LevelError, CodeBadYAML, "SKILL.md", "SKILL.md 不是有效的 UTF-8 文本")
		return
	}
	if n := strings.Count(string(data), "\n") + 1; n > MaxSkillMDLines {
		r.addIssue(LevelWarn, CodeLongSkillMD, "SKILL.md", fmt.Sprintf("SKILL.md 有 %d 行，超过建议的 %d 行，建议拆到 references/", n, MaxSkillMDLines))
	}
	head, body, err := ParseFrontmatter(data)
	if err != nil {
		r.addIssue(LevelError, CodeBadYAML, "SKILL.md", "SKILL.md 头部无法解析："+err.Error())
		return
	}
	r.Frontmatter, r.Body = head, body

	name, _ := head["name"].(string)
	name = strings.TrimSpace(name)
	r.Name = name
	switch {
	case name == "":
		r.addIssue(LevelError, CodeBadName, "SKILL.md", "name 缺失或不是字符串")
	case utf8.RuneCountInString(name) > MaxNameLen:
		r.addIssue(LevelError, CodeBadName, "SKILL.md", fmt.Sprintf("name 超过 %d 个字符", MaxNameLen))
	case !nameRe.MatchString(name):
		r.addIssue(LevelError, CodeBadName, "SKILL.md", "name 只能用小写字母、数字和连字符，且不能以连字符开头、结尾或连续出现")
	}

	desc, _ := head["description"].(string)
	desc = strings.TrimSpace(desc)
	r.Description = desc
	switch {
	case desc == "":
		r.addIssue(LevelError, CodeBadDescription, "SKILL.md", "description 缺失或为空")
	case utf8.RuneCountInString(desc) > MaxDescriptionLen:
		r.addIssue(LevelError, CodeBadDescription, "SKILL.md", fmt.Sprintf("description 超过 %d 个字符", MaxDescriptionLen))
	}

	if v, ok := head["compatibility"]; ok {
		if s, isStr := v.(string); !isStr || utf8.RuneCountInString(s) > MaxCompatibilityLen {
			r.addIssue(LevelWarn, CodeBadCompatibility, "SKILL.md", fmt.Sprintf("compatibility 应是不超过 %d 个字符的字符串", MaxCompatibilityLen))
		}
	}

	r.Title = name
	if md, ok := head["metadata"].(map[string]any); ok {
		if t, ok := md["title"].(string); ok && strings.TrimSpace(t) != "" {
			r.Title = strings.TrimSpace(t)
		}
	}

	for k := range head {
		if !knownFields[k] {
			r.Unsupported = append(r.Unsupported, k)
		}
	}
	sort.Strings(r.Unsupported)
	for _, k := range r.Unsupported {
		r.addIssue(LevelWarn, CodeUnsupportedField, "SKILL.md", "字段 "+k+" 本系统暂未支持，会原样保存")
	}

	if outerDir != "" && name != "" && outerDir != name {
		r.addIssue(LevelWarn, CodeNameDirMismatch, "", fmt.Sprintf("name「%s」与外层目录名「%s」不一致，以 name 为准", name, outerDir))
	}
	r.checkRefs(body, files)
}

// refRe 匹配正文里的 scripts/、references/、assets/ 相对路径；前面不能是路径或 URL 字符。
var refRe = regexp.MustCompile(`(?:^|[^\w/.:-])((?:scripts|references|assets)/[\p{L}\p{N}_./-]+)`)

// linkRe 匹配 Markdown 链接目标。
var linkRe = regexp.MustCompile(`\]\(([^)\s]+)`)

// checkRefs 检查正文引用的相对路径是否都在包里，缺失的逐个提示。
func (r *Result) checkRefs(body string, files []srcFile) {
	have := make(map[string]bool, len(files))
	for _, f := range files {
		have[f.Path] = true
	}
	exists := func(p string) bool {
		if strings.HasSuffix(p, "/") {
			for k := range have {
				if strings.HasPrefix(k, p) {
					return true
				}
			}
			return false
		}
		return have[p]
	}
	refs := refTargets(body)
	reported := map[string]bool{}
	for _, ref := range refs {
		trailing := strings.HasSuffix(ref, "/")
		p := strings.TrimPrefix(path.Clean(ref), "./")
		if trailing {
			p += "/"
		}
		if p == "" || p == "." || reported[p] || exists(p) {
			continue
		}
		reported[p] = true
		r.addIssue(LevelWarn, CodeMissingRef, p, "SKILL.md 引用了包里不存在的文件："+p)
	}
}

// canonicalZip 把文件按路径排序，用固定时间戳和权限重新打包，保证同内容得到同一份字节。
func canonicalZip(files []srcFile) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range files {
		h := &zip.FileHeader{Name: f.Path, Method: zip.Deflate, Modified: fixedTime}
		h.SetMode(0o644)
		w, err := zw.CreateHeader(h)
		if err != nil {
			return nil, fmt.Errorf("生成规范化 zip：%w", err)
		}
		if _, err := w.Write(f.Data); err != nil {
			return nil, fmt.Errorf("生成规范化 zip：%w", err)
		}
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("生成规范化 zip：%w", err)
	}
	return buf.Bytes(), nil
}

// refTargets 取出正文里引用的相对路径（含 Markdown 链接目标）。
func refTargets(body string) []string {
	var refs []string
	for _, m := range refRe.FindAllStringSubmatch(body, -1) {
		refs = append(refs, strings.TrimRight(m[1], ".:!?"))
	}
	for _, m := range linkRe.FindAllStringSubmatch(body, -1) {
		t := m[1]
		if i := strings.IndexAny(t, "#?"); i >= 0 {
			t = t[:i]
		}
		if t == "" || strings.Contains(t, ":") || strings.HasPrefix(t, "/") || strings.HasPrefix(t, "..") {
			continue
		}
		refs = append(refs, t)
	}
	return refs
}
