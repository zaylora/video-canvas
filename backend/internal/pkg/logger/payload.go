package logger

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"go.uber.org/zap"
)

// payload.log 记每次上游调用的请求和响应全文（成功的也记），和 app.log 里的调用行用 call_id 对应。
// 全文原样，只改三处：鉴权头遮成 ***、大段 base64 换成摘要占位、单字段超长截断。渠道 Key 由调用方（网关、插件宿主）在交进来之前遮掉。

// MaxPayloadField 是单个文本字段的上限（字节），超出截断并标 truncated=true。
const MaxPayloadField = 4 << 20

// base64MinLen 是换成摘要占位的最短 base64 长度：约等于 1KB 原始数据。
const base64MinLen = 1366

// PayloadRecord 是 payload.log 的一条记录：一次上游调用。
type PayloadRecord struct {
	CallID    string           // 对应 app.log 调用行的 call_id
	Kind      string           // llm / provider / http
	Purpose   string           // 调用用途，如 drama.scripts、agent.main、agent.web
	Request   *PayloadRequest  // 发出的请求
	Response  *PayloadResponse // 收到的响应
	Steps     any              // kind=provider：插件钩子和 HTTP 步骤（provider.Trace 的步骤）
	Error     string           // 调用失败的原因（已遮 Key）
	LatencyMs int64
	Extra     []zap.Field // 其他字段，如 net_attempt、phase
}

// PayloadRequest 是发出的请求。
type PayloadRequest struct {
	Method  string            `json:"method,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
}

// PayloadResponse 是收到的响应。文本模型记拼装后的正文、思考和工具调用；其他 HTTP 记响应体。
type PayloadResponse struct {
	Status    int               `json:"status,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	Body      string            `json:"body,omitempty"`
	Text      string            `json:"text,omitempty"`
	Thinking  string            `json:"thinking,omitempty"`
	ToolCalls any               `json:"tool_calls,omitempty"`
	ErrorBody string            `json:"error_body,omitempty"` // 上游非 2xx 时的错误体原文
}

// NewCallID 生成一次上游调用的 call_id：12 位十六进制随机值。
func NewCallID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// PayloadEnabled 表示 payload.log 已开启；没开启时调用方可以省掉拼装全文的开销。
func PayloadEnabled() bool { return payload != nil }

// Payload 往 payload.log 写一条记录，带上 ctx 的关联字段；未开启时是空操作。
func Payload(ctx context.Context, rec PayloadRecord) {
	if payload == nil {
		return
	}
	truncated := false
	clean := func(s string) string {
		out, cut := SanitizeText(s)
		truncated = truncated || cut
		return out
	}
	fields := append(Fields(ctx), zap.String("call_id", rec.CallID), zap.String("kind", rec.Kind))
	if rec.Purpose != "" {
		fields = append(fields, zap.String("purpose", rec.Purpose))
	}
	if req := rec.Request; req != nil {
		fields = append(fields, zap.Any("request", PayloadRequest{Method: req.Method, URL: req.URL, Headers: RedactHeaders(req.Headers), Body: clean(req.Body)}))
	}
	if resp := rec.Response; resp != nil {
		fields = append(fields, zap.Any("response", PayloadResponse{Status: resp.Status, Headers: RedactHeaders(resp.Headers), Body: clean(resp.Body),
			Text: clean(resp.Text), Thinking: clean(resp.Thinking), ToolCalls: resp.ToolCalls, ErrorBody: clean(resp.ErrorBody)}))
	}
	if rec.Steps != nil {
		fields = append(fields, zap.Any("steps", rec.Steps))
	}
	if rec.Error != "" {
		fields = append(fields, zap.String("error", clean(rec.Error)))
	}
	fields = append(fields, zap.Int64("latency_ms", rec.LatencyMs))
	fields = append(fields, rec.Extra...)
	if truncated {
		fields = append(fields, zap.Bool("truncated", true))
	}
	payload.Info("payload", fields...)
}

// sensitiveHeaders 是整个值都要遮掉的请求头（小写）。
var sensitiveHeaders = map[string]bool{
	"authorization": true, "proxy-authorization": true, "x-api-key": true, "api-key": true, "x-goog-api-key": true,
	"cookie": true, "set-cookie": true,
}

// RedactHeaders 返回遮掉鉴权头后的副本；其余请求头原样保留。
func RedactHeaders(h map[string]string) map[string]string {
	if len(h) == 0 {
		return nil
	}
	out := make(map[string]string, len(h))
	for k, v := range h {
		if sensitiveHeaders[strings.ToLower(k)] {
			v = "***"
		}
		out[k] = v
	}
	return out
}

var (
	// dataURIPattern 匹配 data: URI 里的 base64；第 1 组是 MIME，第 2 组是数据
	dataURIPattern = regexp.MustCompile(`data:([\w.+-]+/[\w.+-]+);base64,([A-Za-z0-9+/]+={0,2})`)
	// rawBase64Pattern 匹配裸的长 base64 串（RE2 的重复次数上限是 1000，够不够 base64MinLen 在替换时再判断）
	rawBase64Pattern = regexp.MustCompile(`[A-Za-z0-9+/]{1000,}={0,2}`)
)

// SanitizeText 把超过约 1KB 的 base64 换成摘要占位（图片原件在素材库里，只为控制体积），再按 MaxPayloadField 截断；
// 第二个返回值表示是否截断过。
func SanitizeText(s string) (string, bool) {
	if s == "" {
		return s, false
	}
	if len(s) >= base64MinLen {
		s = dataURIPattern.ReplaceAllStringFunc(s, func(m string) string {
			sub := dataURIPattern.FindStringSubmatch(m)
			if len(sub[2]) < base64MinLen {
				return m
			}
			return base64Placeholder(sub[1], sub[2])
		})
		s = rawBase64Pattern.ReplaceAllStringFunc(s, func(m string) string {
			if len(m) < base64MinLen {
				return m
			}
			return base64Placeholder("", m)
		})
	}
	if len(s) <= MaxPayloadField {
		return s, false
	}
	cut := MaxPayloadField
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut], true
}

// base64Placeholder 返回 <base64 image/png 1.2MB sha256:ab12…>：类型、原始大小和内容哈希前 12 位。
func base64Placeholder(mime, data string) string {
	sum := sha256.Sum256([]byte(data))
	kind := "base64"
	if mime != "" {
		kind += " " + mime
	}
	return fmt.Sprintf("<%s %s sha256:%s>", kind, humanSize(len(data)*3/4), hex.EncodeToString(sum[:6]))
}

// humanSize 把字节数写成 512B / 1.2KB / 3.4MB。
func humanSize(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%dB", n)
}
