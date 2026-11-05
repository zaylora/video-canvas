package drama

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Unit 是原文单元；预检目录不序列化 Text，草稿正文另存。
type Unit struct {
	No    int    `json:"no"`
	Kind  string `json:"kind"`
	Title string `json:"title"`
	Text  string `json:"text,omitempty"`
	Chars int    `json:"chars"`
}

// IndexedUnit 是草稿目录，偏移和长度以 UTF-8 字节计，不重复保存完整原文。
type IndexedUnit struct {
	No     int    `json:"no"`
	Kind   string `json:"kind"`
	Title  string `json:"title"`
	Offset int    `json:"offset"`
	Length int    `json:"length"`
	Chars  int    `json:"chars"`
}

// IndexUnits 按顺序索引去噪后的原文，避免草稿 JSON 再存一份大正文。
func IndexUnits(text string, units []Unit) []IndexedUnit {
	out := make([]IndexedUnit, 0, len(units))
	offset := 0
	for _, u := range units {
		index := strings.Index(text[offset:], u.Text)
		if index < 0 {
			continue
		}
		offset += index
		out = append(out, IndexedUnit{No: u.No, Kind: u.Kind, Title: u.Title, Offset: offset, Length: len(u.Text), Chars: u.Chars})
		offset += len(u.Text)
	}
	return out
}

// Issue 是原文预处理的可展示记录。
type Issue struct {
	Kind    string `json:"kind"`
	Count   int    `json:"count"`
	Message string `json:"message"`
}

// PreflightResult 是无模型调用的来源识别和切分结果。
type PreflightResult struct {
	Text        string
	KindGuess   string
	KindSignals string
	UnitKind    string
	Units       []Unit
	Issues      []Issue
}

var episodeMark = regexp.MustCompile(`^第[零〇一二三四五六七八九十百千万两0-9]+集[^\n]*$`)
var chapterMark = regexp.MustCompile(`^第[零〇一二三四五六七八九十百千万两0-9]+[章节回卷][^\n]*$`)
var sceneHead = regexp.MustCompile(`^(?:[0-9]+[-－.][0-9]+(?:\s|[：:、]).*|场景[：:].*|[内外]景(?:\s|[：:、]).*|(?:日|夜)[\s·/／].*)$`)
var dialogueLine = regexp.MustCompile(`^[\p{Han}A-Za-z·]{1,16}(?:[（(][^）)\n]{0,30}[）)])?[：:].+`)
var noiseLine = regexp.MustCompile(`(?i)^(?:(?:https?://|www\.)\S+|(?:求推荐票|求月票|求订阅|求收藏|求打赏|笔趣阁|顶点小说|起点中文网).*|[（(]?(?:本章完|未完待续)[）)]?[。！!]*|作者有话说.*|PS[：:].*)$`)

// Preflight 去噪后根据用户类型或规则识别结果切单元，不删除作品正文。
func Preflight(source, kind string) PreflightResult {
	lines := strings.Split(source, "\n")
	clean := make([]string, 0, len(lines))
	removed := 0
	var signals sourceSignals
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if utf8.RuneCountInString(trimmed) < 80 && noiseLine.MatchString(trimmed) {
			removed++
			continue
		}
		clean = append(clean, line)
		signals.observe(trimmed)
	}
	episodes, chapters, scenes := signals.episodes, signals.chapters, signals.scenes
	dialogues, quotes, total := signals.dialogues, signals.quotes, signals.total
	guess := "unknown"
	switch {
	case episodes >= 2 || scenes >= 3 && dialogues*10 >= total*3:
		guess = "script"
	case chapters >= 2 || quotes*10 >= total && total > 0 && dialogues*10 < total:
		guess = "novel"
	}
	if kind == "" {
		kind = guess
	}
	p := PreflightResult{Text: strings.TrimSpace(strings.Join(clean, "\n")), KindGuess: guess,
		KindSignals: fmt.Sprintf("找到 %d 个分集标记、%d 个章节标题、%d 个场次头；对白行占 %d%%", episodes, chapters, scenes, dialogues*100/max(1, total)), Issues: []Issue{}}
	if removed > 0 {
		p.Issues = append(p.Issues, Issue{"noise_removed", removed, fmt.Sprintf("清掉 %d 行插入语", removed)})
	}
	var marker *regexp.Regexp
	switch {
	case episodes >= 2:
		p.UnitKind, marker = "episode", episodeMark
	case kind == "script" && scenes >= 3:
		p.UnitKind, marker = "scene", sceneHead
	case chapters >= 2:
		p.UnitKind, marker = "chapter", chapterMark
	default:
		p.UnitKind = "chunk"
	}
	p.Units = splitMarked(p.Text, p.UnitKind, marker)
	p.splitLongUnits()
	return p
}

// splitMarked 保留标记行及之前的序言，按原文顺序生成单元。
func splitMarked(text, kind string, marker *regexp.Regexp) []Unit {
	if marker == nil {
		return paragraphUnits(text, kind, "")
	}
	units := []Unit{}
	start, offset, title, currentKind := 0, 0, "序", "prologue"
	for _, line := range strings.SplitAfter(text, "\n") {
		s := strings.TrimSpace(line)
		if marker.MatchString(s) {
			if part := strings.TrimSpace(text[start:offset]); part != "" {
				units = append(units, Unit{Kind: currentKind, Title: title, Text: part})
			}
			start, title, currentKind = offset, s, kind
		}
		offset += len(line)
	}
	if part := strings.TrimSpace(text[start:]); part != "" {
		units = append(units, Unit{Kind: currentKind, Title: title, Text: part})
	}
	return units
}

// paragraphUnits 按约九千字聚合段落；单段超长时按字符切开，防止无换行作品无法处理。
func paragraphUnits(text, kind, title string) []Unit {
	parts := []string{}
	var pending strings.Builder
	used := 0
	flush := func() {
		if pending.Len() > 0 {
			parts = append(parts, strings.TrimSpace(pending.String()))
			pending.Reset()
			used = 0
		}
	}
	for _, line := range strings.SplitAfter(text, "\n") {
		runes := []rune(line)
		if len(runes) > 9000 && used > 0 {
			take := min(9000-used, len(runes))
			pending.WriteString(string(runes[:take]))
			runes = runes[take:]
			flush()
		}
		if used+len(runes) > 9000 {
			flush()
		}
		for len(runes) > 9000 {
			parts = append(parts, string(runes[:9000]))
			runes = runes[9000:]
		}
		pending.WriteString(string(runes))
		used += len(runes)
	}
	flush()
	units := make([]Unit, 0, len(parts))
	for i, part := range parts {
		if part == "" {
			continue
		}
		name := fmt.Sprintf("第 %d 段", i+1)
		if title != "" {
			name = fmt.Sprintf("%s（%d/%d）", title, i+1, len(parts))
		}
		units = append(units, Unit{Kind: kind, Title: name, Text: part})
	}
	return units
}

// splitLongUnits 拆长章，记录空单元、重复标题与长集警示。
func (p *PreflightResult) splitLongUnits() {
	out := []Unit{}
	previous := ""
	for _, u := range p.Units {
		u.Chars = utf8.RuneCountInString(u.Text)
		if u.Chars == 0 || (u.Kind == "episode" || u.Kind == "chapter" || u.Kind == "scene") && strings.TrimSpace(u.Text) == u.Title {
			p.Issues = append(p.Issues, Issue{"empty", 1, "丢弃空单元"})
			continue
		}
		if u.Title == previous {
			p.Issues = append(p.Issues, Issue{"duplicate", 1, "连续重复标题：" + u.Title})
		}
		previous = u.Title
		if (u.Kind == "chapter" || u.Kind == "chunk") && u.Chars > 12000 {
			out = append(out, paragraphUnits(u.Text, u.Kind, u.Title)...)
			p.Issues = append(p.Issues, Issue{"split_long", 1, "超长章节已按段落拆分：" + u.Title})
			continue
		}
		if u.Kind == "episode" && u.Chars > 40000 {
			p.Issues = append(p.Issues, Issue{"episode_too_long", 1, u.Title + "超过 4 万字"})
		}
		out = append(out, u)
	}
	for i := range out {
		out[i].No = i + 1
		out[i].Chars = utf8.RuneCountInString(out[i].Text)
		out[i].Title = Clip(out[i].Title, 255)
	}
	p.Units = out
}

// Clip 按 Unicode 字符截取前缀。
func Clip(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	r := []rune(text)
	if len(r) > limit {
		return string(r[:limit])
	}
	return text
}

// sourceSignals 汇集单次扫描的结构信号，不依赖模型。
type sourceSignals struct{ episodes, chapters, scenes, dialogues, quotes, total int }

// observe 只统计非空行，标题行也计入对白比例的分母。
func (s *sourceSignals) observe(line string) {
	if line == "" {
		return
	}
	s.total++
	if episodeMark.MatchString(line) {
		s.episodes++
	}
	if chapterMark.MatchString(line) {
		s.chapters++
	}
	if sceneHead.MatchString(line) {
		s.scenes++
	}
	if dialogueLine.MatchString(line) {
		s.dialogues++
	}
	if strings.Contains(line, "“") && strings.Contains(line, "”") {
		s.quotes++
	}
}
