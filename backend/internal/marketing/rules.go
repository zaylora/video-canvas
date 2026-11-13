package marketing

import (
	_ "embed" // 内置禁用说法清单
	"regexp"
	"strings"
	"sync"
	"unicode"
)

// zhForbiddenRaw 是中文平台的广告法极限词与禁用说法清单，格式见文件开头的注释。
//
//go:embed rules/zh_forbidden.txt
var zhForbiddenRaw string

// pattern 是一条禁用说法：逐字包含，或正则。输入文本先转小写，所以字面量也是小写。
type pattern struct {
	literal string
	re      *regexp.Regexp
}

// find 在已转小写的文本里找第一处命中，返回命中的原文片段；没有命中返回空串。
func (p pattern) find(lower string) string {
	if p.re != nil {
		return p.re.FindString(lower)
	}
	if strings.Contains(lower, p.literal) {
		return p.literal
	}
	return ""
}

// reLinePrefix 标记清单里的正则行。
const reLinePrefix = "re:"

// parsePatterns 解析清单：忽略空行与 # 注释，re: 开头按正则（忽略大小写），其余逐字匹配。
// 清单是随代码发布的静态文件，正则写错属于编码错误，MustCompile 在测试里即暴露。
func parsePatterns(raw string) []pattern {
	var out []pattern
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, reLinePrefix) {
			out = append(out, pattern{re: regexp.MustCompile("(?i)" + strings.TrimPrefix(line, reLinePrefix))})
			continue
		}
		out = append(out, pattern{literal: strings.ToLower(line)})
	}
	return out
}

// regexPatterns 把一组英文正则编译成 pattern。
func regexPatterns(exprs ...string) []pattern {
	out := make([]pattern, len(exprs))
	for i, e := range exprs {
		out[i] = pattern{re: regexp.MustCompile("(?i)" + e)}
	}
	return out
}

// enMedicalPatterns 是英文医疗功效用语，所有平台的英文文本都扫。
var enMedicalPatterns = sync.OnceValue(func() []pattern {
	return regexPatterns(
		`\bcure[sd]?\b`, `\bcuring\b`, `\bheal(s|ed|ing)?\b`,
		`\bclinically (proven|tested)\b`,
		`\bdermatologist[- ](recommended|approved|tested)\b`, `\bdoctor[- ](recommended|approved)\b`,
		`\bfda[- ]approved\b`, `\bmedical[- ]grade\b`,
		`\bprevents? (disease|cancer|acne|aging|infection)\b`,
		`\b(relieves|eliminates|removes) (pain|wrinkles|acne|scars)\b`,
		`\bguaranteed results?\b`,
	)
})

// enAmazonPatterns 是亚马逊额外禁止的价格、促销、竞品与排名用语。
var enAmazonPatterns = sync.OnceValue(func() []pattern {
	return regexPatterns(
		`\bbest[- ]?sellers?\b`, `\btop[- ](rated|selling)\b`, `#\s?1\b`, `\bno\.?\s?1\b`, `\bnumber (one|1)\b`,
		`\b(5|five)[- ]stars?\b`,
		`\bcheapest\b`, `\bcheaper than\b`, `\blowest price\b`, `\bbest price\b`, `\bbetter than\b`,
		`\bon sale\b`, `\bsale\b`, `\bdiscounts?\b`, `\bcoupons?\b`, `\bfree shipping\b`, `\blimited time\b`,
		`\bbuy one get one\b`, `\bbogo\b`, `\d+\s?%\s?off\b`,
		`\$\s?\d+(?:,\d{3})*(?:\.\d+)?`, `[€£]\s?\d+(?:,\d{3})*(?:\.\d+)?`,
	)
})

// zhPatterns 是解析后的中文清单，只解析一次。
var zhPatterns = sync.OnceValue(func() []pattern { return parsePatterns(zhForbiddenRaw) })

// flagKey 用来保证同一镜头、同一类型、同一文本只报一次。
type flagKey struct {
	shot int
	typ  string
	text string
}

// scanner 是一次扫描的上下文：平台适用的禁用清单、品牌与背书依据。
type scanner struct {
	patterns []pattern
	rev      *Reverse
	corpus   []string // 事实卡里可作为背书依据的文本，已规整
}

// newScanner 按平台选清单：国内平台扫中文极限词，亚马逊另扫价格促销用语，所有平台扫英文医疗功效。
func newScanner(platform string, facts ProductFacts, rev *Reverse) *scanner {
	p := PlatformOf(platform)
	var pats []pattern
	if p.Domestic {
		pats = append(pats, zhPatterns()...)
	}
	pats = append(pats, enMedicalPatterns()...)
	if p.Key == "amazon" {
		pats = append(pats, enAmazonPatterns()...)
	}
	return &scanner{patterns: pats, rev: rev, corpus: factsCorpus(facts)}
}

// forbidden 返回文本命中的全部禁用说法（小写原文片段）。
func (s *scanner) forbidden(text string) []string {
	lower := strings.ToLower(text)
	var out []string
	for _, p := range s.patterns {
		if m := p.find(lower); m != "" {
			out = append(out, m)
		}
	}
	return out
}

// ScanScript 逐镜头扫描旁白与画面描述，返回软问题（flag.shot 从 0 起）。
//
//   - forbidden：命中极限词或禁用说法，旁白和画面描述都扫；国内平台扫中文清单，
//     亚马逊另扫价格促销与排名用语，所有平台扫英文医疗功效用语；
//   - brand：旁白或画面里出现 rev.Brands 里的第三方品牌（不分大小写），rev 为 nil 不扫；
//   - claim：旁白里出现 rev.Claims 中的背书话术，且事实卡（名称与卖点的 text / proof）里找不到依据。
//     判定规则见 claimFragments：背书拆成关键片段，旁白含任一片段算「出现」，事实卡含任一片段算「有依据」。
//
// 同一镜头同一类型同一文本只报一次。
func ScanScript(shots []ScriptShot, platform string, facts ProductFacts, rev *Reverse) []Flag {
	sc := newScanner(platform, facts, rev)
	var out []Flag
	seen := map[flagKey]bool{}
	add := func(shot int, typ, text string) {
		k := flagKey{shot, typ, text}
		if !seen[k] {
			seen[k] = true
			out = append(out, Flag{Shot: shot, Type: typ, Text: text})
		}
	}
	for i, sh := range shots {
		for _, text := range []string{sh.Narration, sh.Scene} {
			for _, m := range sc.forbidden(text) {
				add(i, FlagForbidden, m)
			}
		}
		if rev == nil {
			continue
		}
		for _, b := range sc.brandsIn(sh) {
			add(i, FlagBrand, b)
		}
		for _, c := range sc.unsupportedClaims(sh.Narration) {
			add(i, FlagClaim, c)
		}
	}
	return out
}

// ScanNarrations 给 PUT 改旁白后重扫用：scenes 与 narrations 对应下标，可更短（缺的按空串）。
func ScanNarrations(narrations []string, scenes []string, platform string, facts ProductFacts, rev *Reverse) []Flag {
	shots := make([]ScriptShot, len(narrations))
	for i, n := range narrations {
		shots[i].Narration = n
		if i < len(scenes) {
			shots[i].Scene = scenes[i]
		}
	}
	return ScanScript(shots, platform, facts, rev)
}

// brandsIn 返回镜头旁白或画面里出现的第三方品牌（按 rev.Brands 里的写法返回）。
func (s *scanner) brandsIn(sh ScriptShot) []string {
	text := strings.ToLower(sh.Narration + "\n" + sh.Scene)
	var out []string
	for _, b := range s.rev.Brands {
		b = strings.TrimSpace(b)
		if b != "" && strings.Contains(text, strings.ToLower(b)) {
			out = append(out, b)
		}
	}
	return out
}

// unsupportedClaims 返回旁白里出现、但事实卡没有依据的背书话术。
func (s *scanner) unsupportedClaims(narration string) []string {
	narr := normalize(narration)
	if narr == "" {
		return nil
	}
	var out []string
	for _, c := range s.rev.Claims {
		frags := claimFragments(c)
		if len(frags) == 0 || !containsAny([]string{narr}, frags) {
			continue
		}
		if !containsAny(s.corpus, frags) {
			out = append(out, c)
		}
	}
	return out
}

// 背书匹配的关键片段长度：含中文取 4 个字符，纯拉丁文字取 8 个字符（去掉空格后）。
const (
	zhFragmentLen = 4
	enFragmentLen = 8
	minWholeClaim = 2 // 整句背书短于片段长度时，至少这么长才拿整句去比
)

// claimFragments 把一条背书话术拆成用来比对的关键片段。
//
// 规则（简单、可解释）：先按标点切成小句；每个小句去掉空白后取所有长度为 N 的连续片段
// （含中文 N=4，纯拉丁 N=8）；所有小句都短于 N 时，整句（≥2 个字符）作为唯一片段。
// 例：「全网销量第一」得到「全网销量」「网销量第」「销量第一」；旁白里写「销量第一的香水」就算出现了该背书。
func claimFragments(claim string) []string {
	n := enFragmentLen
	if hasHan(claim) {
		n = zhFragmentLen
	}
	var out []string
	var whole []string
	for _, seg := range splitClauses(claim) {
		r := []rune(seg)
		whole = append(whole, seg)
		for i := 0; i+n <= len(r); i++ {
			out = append(out, string(r[i:i+n]))
		}
	}
	if len(out) > 0 {
		return out
	}
	joined := strings.Join(whole, "")
	if len([]rune(joined)) >= minWholeClaim {
		return []string{joined}
	}
	return nil
}

// splitClauses 按标点把文本切成小句，每个小句转小写并去掉空白。
func splitClauses(s string) []string {
	parts := strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !unicode.IsSpace(r) })
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if n := normalize(p); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// normalize 转小写并只保留字母与数字，用来做不受标点、空白影响的包含比较。
func normalize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// hasHan 判断文本里是否有汉字。
func hasHan(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

// containsAny 判断 texts 里是否有任一段包含 frags 中的任一片段。
func containsAny(texts, frags []string) bool {
	for _, t := range texts {
		for _, f := range frags {
			if strings.Contains(t, f) {
				return true
			}
		}
	}
	return false
}

// factsCorpus 取事实卡里可作为背书依据的文本：商品名与各卖点的 text、proof，每段分别规整。
func factsCorpus(f ProductFacts) []string {
	var out []string
	add := func(s string) {
		if n := normalize(s); n != "" {
			out = append(out, n)
		}
	}
	add(f.Name)
	for _, sp := range f.SellingPoints {
		add(sp.Text)
		add(sp.Proof)
	}
	return out
}
