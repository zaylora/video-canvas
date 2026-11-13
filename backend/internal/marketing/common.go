package marketing

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"video-canvas/internal/stepkit"
)

// 时间线校验的容差（秒）。
const (
	timelineTol = 0.3 // 首镜起点与相邻镜头首尾相接的容差
	durationTol = 0.5 // 末镜结束与总时长的容差
)

// span 是一个镜头的起止秒，timelineProblems 对拆解镜头和脚本镜头共用。
type span struct{ start, end float64 }

// timelineProblems 检查镜头时间线：起点接近 0、每镜结束晚于开始、相邻首尾相接、末镜结束与 duration 相差不超过容差。
// duration ≤ 0 表示不知道总时长，跳过最后一项。
func timelineProblems(spans []span, duration float64) []string {
	var out []string
	if len(spans) == 0 {
		return out
	}
	if first := spans[0].start; first < -timelineTol || first > timelineTol {
		out = append(out, fmt.Sprintf("第一个镜头必须从 0 秒开始（容差 %.1f 秒），实际 %.1f 秒", timelineTol, first))
	}
	for i, s := range spans {
		if s.end <= s.start {
			out = append(out, fmt.Sprintf("镜头 %d 的结束时间必须大于开始时间（%.1f–%.1f）", i+1, s.start, s.end))
		}
		if i == 0 {
			continue
		}
		if gap := s.start - spans[i-1].end; gap < -timelineTol || gap > timelineTol {
			out = append(out, fmt.Sprintf("镜头 %d 与前一个镜头没有首尾相接（前一个结束于 %.1f 秒，本镜头开始于 %.1f 秒，容差 %.1f 秒）", i+1, spans[i-1].end, s.start, timelineTol))
		}
	}
	if duration > 0 {
		last := spans[len(spans)-1].end
		if diff := last - duration; diff < -durationTol || diff > durationTol {
			out = append(out, fmt.Sprintf("镜头总长与时长不符：最后一个镜头结束于 %.1f 秒，时长应为 %.1f 秒（容差 %.1f 秒）", last, duration, durationTol))
		}
	}
	return out
}

// scriptSpans 取脚本镜头的起止。
func scriptSpans(shots []ScriptShot) []span {
	out := make([]span, len(shots))
	for i, s := range shots {
		out[i] = span{s.Start, s.End}
	}
	return out
}

// resolveLanguage 取轮次设置的旁白语言；没填或不认识时用平台默认语言。
func resolveLanguage(b Brief) string {
	if b.Language == LangZH || b.Language == LangEN {
		return b.Language
	}
	return PlatformOf(b.Platform).Language
}

// resolveAspect 取画幅；没填时用平台默认。
func resolveAspect(b Brief) string {
	if strings.TrimSpace(b.AspectRatio) != "" {
		return b.AspectRatio
	}
	return PlatformOf(b.Platform).AspectRatio
}

// platformVars 是放进 Vars["platform"] 的平台信息。
func platformVars(key string) map[string]any {
	p := PlatformOf(key)
	return map[string]any{"key": p.Key, "name": p.Name, "style": p.Style, "bans": p.Bans}
}

// baseVars 是各环节共用的输入变量：平台、语言、时长、画幅。
func baseVars(b Brief) map[string]any {
	return map[string]any{
		"platform":     platformVars(b.Platform),
		"language":     resolveLanguage(b),
		"duration":     b.Duration,
		"aspect_ratio": resolveAspect(b),
	}
}

// varAs 把 Vars 里的值经 JSON 往返转成目标类型，结构体、map 都能读；键不存在或转换失败返回 false。
func varAs[T any](in stepkit.Input, key string) (T, bool) {
	var zero T
	v, ok := in.Vars[key]
	if !ok || v == nil {
		return zero, false
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return zero, false
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		return zero, false
	}
	return out, true
}

// varFloat 读数值型变量（int、float64 等都行）。
func varFloat(in stepkit.Input, key string) float64 {
	f, _ := varAs[float64](in, key)
	return f
}

// varString 读字符串变量。
func varString(in stepkit.Input, key string) string {
	s, _ := varAs[string](in, key)
	return s
}

// varPlatformKey 读 Vars["platform"].key。
func varPlatformKey(in stepkit.Input) string {
	m, _ := varAs[map[string]any](in, "platform")
	k, _ := m["key"].(string)
	return k
}

// blank 判断是否空白串。
func blank(s string) bool { return strings.TrimSpace(s) == "" }

// decode 把 JSON 解码到目标，失败时带上环节名。
func decode[T any](raw json.RawMessage, what string) (T, error) {
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("解码%s失败：%w", what, err)
	}
	return out, nil
}

// schemaCheck 先按 Schema 检查，没问题再返回 nil；有问题返回硬错误。
func schemaCheck(schema json.RawMessage, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("编码失败：%w", err)
	}
	return stepkit.Hard(stepkit.CheckSchema(schema, raw)...)
}

// clip 按字符数截断并加省略号。
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// fmtSec 把秒数写成最多一位小数、去掉多余的 0（5 → "5"，2.5 → "2.5"）。
func fmtSec(f float64) string {
	s := fmt.Sprintf("%.1f", f)
	s = strings.TrimSuffix(s, ".0")
	return s
}

// isPunctOrSpace 判断是否标点、符号或空白。
func isPunctOrSpace(r rune) bool {
	return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
}

// FmtSec 供格式化器子包复用：秒数写成最多一位小数。
func FmtSec(f float64) string { return fmtSec(f) }

// stringsOrEmpty 把 nil 切片换成空切片，避免编码成 JSON null 而过不了 Schema。
func stringsOrEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
