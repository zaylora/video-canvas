package plugin

import (
	"encoding/json"
	"strings"
	"time"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
)

// redactor 按凭证明文脱敏：明文、URL 转义形式（modelcfg.Redact 负责）以及 JSON 转义形式都会被替换成 ***。
type redactor struct {
	secrets []string
}

// add 登记一个需要脱敏的凭证。
func (r *redactor) add(secret string) {
	if secret == "" {
		return
	}
	r.secrets = append(r.secrets, secret)
	// JSON 编码会把 < > & 与控制字符转义成 < 这类形式，钩子入参与响应里出现的是转义后的文本
	if b, err := json.Marshal(secret); err == nil {
		if esc := strings.Trim(string(b), `"`); esc != secret {
			r.secrets = append(r.secrets, esc)
		}
	}
}

// str 脱敏一段文本。
func (r *redactor) str(s string) string {
	if len(r.secrets) == 0 {
		return s
	}
	out, _ := modelcfg.Redact(s, r.secrets...).(string) // 字符串入参一定得到字符串
	return out
}

// traceJSON 把一段 JSON 脱敏、截断成可放进追踪的 JSON：超过 TraceBodyLimit 或脱敏后不再合法时，改为 JSON 字符串。
func (r *redactor) traceJSON(raw []byte) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	s := r.str(string(raw))
	if len(s) <= provider.TraceBodyLimit && json.Valid([]byte(s)) {
		return json.RawMessage(s)
	}
	b, err := json.Marshal(truncate(s, provider.TraceBodyLimit))
	if err != nil {
		return nil
	}
	return b
}

// truncate 按字节截断，并避免截在 UTF-8 字符中间。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && (s[cut]&0xC0) == 0x80 {
		cut--
	}
	return s[:cut] + "…（已截断）"
}

// recordHook 记录一次钩子调用：输入（单个参数时就是它本身，多个时是参数数组）、输出与 utils.log。
func recordHook(trace *provider.Trace, red *redactor, hook string, args []json.RawMessage, out *hookOutcome) {
	if trace == nil {
		return
	}
	var input []byte
	if len(args) == 1 {
		input = args[0]
	} else if b, err := json.Marshal(args); err == nil {
		input = b
	}
	h := &provider.TraceHook{Name: hook, Input: red.traceJSON(input), Output: red.traceJSON(out.result)}
	for _, l := range out.logs {
		h.Logs = append(h.Logs, red.str(l))
	}
	step := provider.TraceStep{Name: "hook:" + hook, Kind: "hook", Hook: h, DurationMs: out.duration.Milliseconds()}
	if out.err != nil {
		step.Error = red.str(out.err.Error())
	}
	trace.Add(step)
}

// hookOutcome 是一次钩子调用在追踪里要记录的结果。
type hookOutcome struct {
	result   json.RawMessage
	logs     []string
	err      error
	duration time.Duration
}
