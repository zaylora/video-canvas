package webaccess

import (
	"io"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// 这些元素的内容不是正文。
var skipAlways = map[atom.Atom]bool{
	atom.Script: true, atom.Style: true, atom.Noscript: true, atom.Template: true, atom.Svg: true, atom.Iframe: true,
	atom.Form: true, atom.Nav: true, atom.Aside: true, atom.Button: true, atom.Select: true, atom.Input: true,
	atom.Textarea: true, atom.Dialog: true, atom.Canvas: true, atom.Head: true, atom.Img: true,
}

// 页眉页脚在整页兜底时跳过；已经找到 main 或 article 时，里面的页眉往往是文章标题，要留着。
var skipOnFullPage = map[atom.Atom]bool{atom.Header: true, atom.Footer: true}

var (
	blankLines  = regexp.MustCompile(`\n{3,}`)
	spaceRuns   = regexp.MustCompile(`[ \t\r\n\f]+`)
	trailingSpc = regexp.MustCompile(`[ \t]+\n`)
)

// ExtractPage 从 HTML 里抽出标题和正文，正文是 Markdown：标题、段落、列表、代码块、表格和 http(s) 链接。
// 导航、页眉页脚、侧栏、脚本和样式都不要。优先取 main，其次是唯一的 article，都没有就取整个 body。
func ExtractPage(r io.Reader, base *url.URL) (title, text string, err error) {
	doc, err := html.Parse(r)
	if err != nil {
		return "", "", err
	}
	if t := findFirst(doc, func(n *html.Node) bool { return n.DataAtom == atom.Title }); t != nil {
		title = collapse(nodeText(t))
	}
	root, fullPage := pickRoot(doc)
	w := &mdWriter{base: base, skipHeaderFooter: fullPage}
	w.block(root)
	return title, tidy(w.sb.String()), nil
}

func pickRoot(doc *html.Node) (*html.Node, bool) {
	if m := findFirst(doc, func(n *html.Node) bool { return n.DataAtom == atom.Main }); m != nil {
		return m, false
	}
	if arts := findAll(doc, func(n *html.Node) bool { return n.DataAtom == atom.Article }); len(arts) == 1 {
		return arts[0], false
	}
	if b := findFirst(doc, func(n *html.Node) bool { return n.DataAtom == atom.Body }); b != nil {
		return b, true
	}
	return doc, true
}

type mdWriter struct {
	sb               strings.Builder
	base             *url.URL
	skipHeaderFooter bool
	listDepth        int
}

func (w *mdWriter) skip(n *html.Node) bool {
	return skipAlways[n.DataAtom] || (w.skipHeaderFooter && skipOnFullPage[n.DataAtom])
}

// newline 保证当前行已结束；blank 保证和上一段之间隔一个空行。
func (w *mdWriter) newline() {
	if s := w.sb.String(); s != "" && !strings.HasSuffix(s, "\n") {
		w.sb.WriteByte('\n')
	}
}
func (w *mdWriter) blank() {
	w.newline()
	if s := w.sb.String(); s != "" && !strings.HasSuffix(s, "\n\n") {
		w.sb.WriteByte('\n')
	}
}

func (w *mdWriter) block(n *html.Node) {
	switch n.Type {
	case html.TextNode:
		if t := collapse(n.Data); t != "" {
			w.newline()
			w.sb.WriteString(t)
			w.sb.WriteByte('\n')
		}
		return
	case html.ElementNode:
		if w.skip(n) {
			return
		}
	default:
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			w.block(c)
		}
		return
	}
	switch n.DataAtom {
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		w.heading(n)
	case atom.P:
		if t := w.inline(n); t != "" {
			w.blank()
			w.sb.WriteString(t + "\n\n")
		}
	case atom.Pre:
		w.blank()
		w.sb.WriteString("```\n" + strings.Trim(nodeText(n), "\n") + "\n```\n\n")
	case atom.Ul, atom.Ol:
		w.blank()
		w.list(n)
		w.blank()
	case atom.Table:
		w.table(n)
	case atom.Blockquote:
		w.quote(n)
	case atom.Br, atom.Hr:
		w.newline()
	default:
		// 普通容器：先结束当前行，里面的块和文字各自成段，容器之间不会粘在一起
		w.newline()
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			w.block(c)
		}
		w.newline()
	}
}

func (w *mdWriter) heading(n *html.Node) {
	level := int(n.Data[1] - '0')
	if t := w.inline(n); t != "" {
		w.blank()
		w.sb.WriteString(strings.Repeat("#", level) + " " + t + "\n\n")
	}
}

// table 每行一个 tr，单元格用 " | " 隔开。
func (w *mdWriter) table(n *html.Node) {
	w.blank()
	for _, tr := range findAll(n, func(x *html.Node) bool { return x.DataAtom == atom.Tr }) {
		var cells []string
		for c := tr.FirstChild; c != nil; c = c.NextSibling {
			if c.DataAtom == atom.Td || c.DataAtom == atom.Th {
				cells = append(cells, w.inline(c))
			}
		}
		if len(cells) > 0 {
			w.sb.WriteString(strings.Join(cells, " | ") + "\n")
		}
	}
	w.blank()
}

func (w *mdWriter) quote(n *html.Node) {
	w.blank()
	sub := &mdWriter{base: w.base, skipHeaderFooter: w.skipHeaderFooter}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		sub.block(c)
	}
	for _, line := range strings.Split(strings.TrimSpace(tidy(sub.sb.String())), "\n") {
		w.sb.WriteString("> " + line + "\n")
	}
	w.blank()
}

func (w *mdWriter) list(n *html.Node) {
	ordered := n.DataAtom == atom.Ol
	w.listDepth++
	defer func() { w.listDepth-- }()
	i := 0
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.DataAtom != atom.Li {
			continue
		}
		i++
		marker := "- "
		if ordered {
			marker = strconv.Itoa(i) + ". "
		}
		var own strings.Builder
		var nested []*html.Node
		for x := c.FirstChild; x != nil; x = x.NextSibling {
			if x.DataAtom == atom.Ul || x.DataAtom == atom.Ol {
				nested = append(nested, x)
			} else {
				own.WriteString(w.inlineNode(x))
			}
		}
		if t := collapse(own.String()); t != "" {
			w.newline()
			w.sb.WriteString(strings.Repeat("  ", w.listDepth-1) + marker + t + "\n")
		}
		for _, sub := range nested {
			w.list(sub)
		}
	}
}

// inline 把一个元素的内容当作一行文字：链接保留成 [文字](地址)，块之间用空格隔开。
func (w *mdWriter) inline(n *html.Node) string {
	var sb strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		sb.WriteString(w.inlineNode(c))
	}
	return collapse(sb.String())
}

func (w *mdWriter) inlineNode(n *html.Node) string {
	switch n.Type {
	case html.TextNode:
		return n.Data
	case html.ElementNode:
		if w.skip(n) {
			return ""
		}
		if n.DataAtom == atom.A {
			text := w.inline(n)
			if href := w.resolve(attr(n, "href")); href != "" && text != "" {
				return "[" + text + "](" + href + ")"
			}
			return text
		}
		var sb strings.Builder
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			sb.WriteString(w.inlineNode(c))
		}
		switch n.DataAtom {
		case atom.P, atom.Div, atom.Br, atom.Li, atom.Tr, atom.Td, atom.Th, atom.Section:
			return sb.String() + " "
		}
		return sb.String()
	}
	return ""
}

// resolve 把链接转成绝对的 http(s) 地址；javascript:、mailto: 等其他协议一律丢掉。
func (w *mdWriter) resolve(href string) string {
	href = strings.TrimSpace(href)
	if href == "" || strings.HasPrefix(href, "#") {
		return ""
	}
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if w.base != nil {
		u = w.base.ResolveReference(u)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	return u.String()
}

func collapse(s string) string { return strings.TrimSpace(spaceRuns.ReplaceAllString(s, " ")) }

// tidy 去掉行尾空白、压缩连续空行。
func tidy(s string) string {
	s = trailingSpc.ReplaceAllString(s, "\n")
	return strings.TrimSpace(blankLines.ReplaceAllString(s, "\n\n"))
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func nodeText(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(x *html.Node) {
		if x.Type == html.TextNode {
			sb.WriteString(x.Data)
		}
		for c := x.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return sb.String()
}

func findFirst(n *html.Node, pred func(*html.Node) bool) *html.Node {
	if n.Type == html.ElementNode && pred(n) {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if f := findFirst(c, pred); f != nil {
			return f
		}
	}
	return nil
}

func findAll(n *html.Node, pred func(*html.Node) bool) []*html.Node {
	var out []*html.Node
	if n.Type == html.ElementNode && pred(n) {
		out = append(out, n)
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		out = append(out, findAll(c, pred)...)
	}
	return out
}

func hasClass(n *html.Node, token string) bool {
	for _, c := range strings.Fields(attr(n, "class")) {
		if c == token {
			return true
		}
	}
	return false
}
