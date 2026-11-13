package provider

import (
	"regexp"
	"strings"
)

// 上游错误原文里的请求 / 追踪编号，如 "(request id: 2026...)"、"request_id=abc"、"[trace_id: t-1]"、"请求ID：xx"。
// 键名后必须跟冒号或等号，值只取字母数字和 . _ -，避免误删正常文字。
const traceIDPattern = `(?:(?:x-)?(?:request|req|trace|log)[ _-]?id|请求\s*id|请求编号)\s*[:：=]\s*[A-Za-z0-9._\-]+`

var (
	// tracedBracket 是整段都是编号的括号，连括号一起去掉
	tracedBracket = regexp.MustCompile(`(?i)\s*[(\[（【]\s*` + traceIDPattern + `\s*[)\]）】]`)
	// tracedBare 是没有单独括号的编号（可能在括号里与别的内容并列），连同前面的逗号 / 分号一起去掉
	tracedBare = regexp.MustCompile(`(?i)\s*[,，;；]?\s*` + traceIDPattern)
	// traceSpaces 是连续空白
	traceSpaces = regexp.MustCompile(`\s{2,}`)
)

// StripTraceIDs 去掉上游错误原文里上游自己的请求 / 追踪编号：对用户没有意义，排查用的是平台自己的任务编号。
// 只剩编号时返回空串，调用方回落到固定文案。
func StripTraceIDs(s string) string {
	s = tracedBracket.ReplaceAllString(s, "")
	s = tracedBare.ReplaceAllString(s, "")
	s = strings.NewReplacer("( ", "(", "（ ", "（", "[ ", "[").Replace(s)
	for _, empty := range []string{"()", "（）", "[]", "【】"} {
		s = strings.ReplaceAll(s, empty, "")
	}
	s = traceSpaces.ReplaceAllString(s, " ")
	return strings.Trim(s, " \t\r\n,，;；")
}
