package llmgateway

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

// encodeCallID 用 pi 会原样保留的字符生成 id，签名只属于对应的调用。
func encodeCallID(signature string) string {
	id := "gc_" + strings.ToLower(rand.Text()[:8])
	if signature != "" {
		id += "_" + base64.RawURLEncoding.EncodeToString([]byte(signature))
	}
	return id
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []geminiResponsePart `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	Usage *struct {
		Prompt     int `json:"promptTokenCount"`
		Candidates int `json:"candidatesTokenCount"`
		Thoughts   int `json:"thoughtsTokenCount"`
		Cached     int `json:"cachedContentTokenCount"`
	} `json:"usageMetadata"`
	PromptFeedback struct {
		BlockReason string `json:"blockReason"`
	} `json:"promptFeedback"`
	Error json.RawMessage `json:"error"`
}

type geminiResponsePart struct {
	Text         *string `json:"text"`
	Thought      bool    `json:"thought"`
	FunctionCall *struct {
		Name string          `json:"name"`
		Args json.RawMessage `json:"args"`
	} `json:"functionCall"`
	ThoughtSignature string `json:"thoughtSignature"`
}

type geminiDelta struct {
	Content          *string          `json:"content,omitempty"`
	ReasoningContent *string          `json:"reasoning_content,omitempty"`
	ToolCalls        []map[string]any `json:"tool_calls,omitempty"`
}

// geminiResponseState 只保存结束原因、最后用量和调用序号，不累积流式正文。
type geminiResponseState struct {
	seen   bool
	calls  int
	finish string
	usage  *wireUsage
}

func (s *geminiResponseState) translate(c *geminiResponse) ([]geminiDelta, error) {
	if len(c.Error) > 0 && string(c.Error) != "null" {
		// 上游可能在错误里回显 Key 或媒体地址；流内错误不能原样交给 raw。
		return nil, &UpstreamError{Status: 502, Message: "上游无法处理本次请求，请稍后重试或联系管理员"}
	}
	if len(c.Candidates) > 0 || c.Usage != nil || c.PromptFeedback.BlockReason != "" {
		s.seen = true
	}
	if c.Usage != nil {
		s.usage = &wireUsage{PromptTokens: c.Usage.Prompt, CompletionTokens: c.Usage.Candidates + c.Usage.Thoughts}
		s.usage.PromptTokensDetails.CachedTokens = c.Usage.Cached
	}
	if len(c.Candidates) == 0 {
		if c.PromptFeedback.BlockReason != "" {
			s.finish = "content_filter"
			text := "请求被上游安全策略拦截（" + geminiBlockReason(c.PromptFeedback.BlockReason) + "）"
			return []geminiDelta{{Content: &text}}, nil
		}
		return nil, nil
	}
	// 网关只请求一个候选；不能把多个候选的工具调用混入同一轮。
	candidate := c.Candidates[0]
	if candidate.FinishReason != "" {
		s.finish = geminiFinishReason(candidate.FinishReason)
	}
	deltas := make([]geminiDelta, 0, len(candidate.Content.Parts))
	for _, part := range candidate.Content.Parts {
		deltas = append(deltas, s.translatePart(part)...)
	}
	return deltas, nil
}

func (s *geminiResponseState) translatePart(part geminiResponsePart) []geminiDelta {
	var deltas []geminiDelta
	if part.Text != nil {
		delta := geminiDelta{Content: part.Text}
		if part.Thought {
			delta = geminiDelta{ReasoningContent: part.Text}
		}
		deltas = append(deltas, delta)
	}
	if part.FunctionCall != nil {
		call := part.FunctionCall
		args := "{}"
		if len(call.Args) > 0 && !bytes.Equal(bytes.TrimSpace(call.Args), []byte("null")) {
			var compact bytes.Buffer
			if json.Compact(&compact, call.Args) == nil {
				args = compact.String()
			}
		}
		tc := map[string]any{"index": s.calls, "id": encodeCallID(part.ThoughtSignature), "type": "function", "function": map[string]any{"name": call.Name, "arguments": args}}
		s.calls++
		deltas = append(deltas, geminiDelta{ToolCalls: []map[string]any{tc}})
	}
	return deltas
}

func geminiFinishReason(reason string) string {
	switch reason {
	case "STOP":
		return "stop"
	case "MAX_TOKENS":
		return "length"
	default:
		return "content_filter"
	}
}

func geminiBlockReason(reason string) string {
	switch reason {
	case "SAFETY", "OTHER", "BLOCKLIST", "PROHIBITED_CONTENT", "IMAGE_SAFETY", "MODEL_ARMOR":
		return reason
	default:
		return "未知原因"
	}
}

func (s *geminiResponseState) finishReason() string {
	if s.calls > 0 {
		return "tool_calls"
	}
	if s.finish != "" {
		return s.finish
	}
	return "stop"
}

// geminiSSEReader 同步逐块翻译，不启动后台 goroutine；取消和读错误由原读取链路处理。
type geminiSSEReader struct {
	source  *bufio.Reader
	limited *io.LimitedReader
	pending bytes.Buffer
	state   geminiResponseState
	err     error
}

func newGeminiSSEReader(r io.Reader) io.Reader {
	limited := &io.LimitedReader{R: r, N: maxStreamBytes}
	return &geminiSSEReader{source: bufio.NewReaderSize(limited, 64<<10), limited: limited}
}

func (r *geminiSSEReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for r.pending.Len() == 0 && r.err == nil {
		line, err := r.source.ReadBytes('\n')
		r.translateLine(bytes.TrimSpace(line))
		if r.err != nil {
			break
		}
		if err != nil {
			if errors.Is(err, io.EOF) && r.limited.N > 0 {
				r.complete()
			} else {
				r.err = err
				if r.limited.N == 0 {
					r.err = ErrStreamTruncated
				}
			}
		}
	}
	if r.pending.Len() > 0 {
		return r.pending.Read(p)
	}
	return 0, r.err
}

func (r *geminiSSEReader) translateLine(line []byte) {
	payload, ok := bytes.CutPrefix(line, []byte("data:"))
	if !ok {
		return
	}
	payload = bytes.TrimSpace(payload)
	if string(payload) == "[DONE]" {
		r.complete()
		return
	}
	var c geminiResponse
	if json.Unmarshal(payload, &c) != nil {
		return
	}
	deltas, err := r.state.translate(&c)
	if err != nil {
		r.err = err
		return
	}
	for _, delta := range deltas {
		r.writeSSE(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": delta}}})
	}
	if r.state.finish != "" {
		r.complete()
	}
}

func (r *geminiSSEReader) writeSSE(value any) {
	b, _ := json.Marshal(value) // 翻译后的字段只含已验证的 JSON、字符串和数字。
	r.pending.WriteString("data: ")
	r.pending.Write(b)
	r.pending.WriteString("\n\n")
}

func (r *geminiSSEReader) complete() {
	if r.state.seen {
		r.writeSSE(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": r.state.finishReason()}}})
		if r.state.usage != nil {
			r.writeSSE(map[string]any{"choices": []any{}, "usage": r.state.usage})
		}
		r.pending.WriteString("data: [DONE]\n\n")
	}
	r.err = io.EOF
}

// geminiWholeToOpenAI 转成 readWhole 能读取的整段响应，raw 仍由现有 synthesizeSSE 生成。
func geminiWholeToOpenAI(body []byte) ([]byte, error) {
	var c geminiResponse
	if json.Unmarshal(body, &c) != nil {
		return nil, ErrStreamTruncated
	}
	var state geminiResponseState
	deltas, err := state.translate(&c)
	if err != nil {
		return nil, err
	}
	if !state.seen {
		return nil, ErrStreamTruncated
	}
	var text, thinking strings.Builder
	var calls []map[string]any
	for _, delta := range deltas {
		if delta.Content != nil {
			text.WriteString(*delta.Content)
		}
		if delta.ReasoningContent != nil {
			thinking.WriteString(*delta.ReasoningContent)
		}
		calls = append(calls, delta.ToolCalls...)
	}
	choice := map[string]any{"message": map[string]any{"content": text.String(), "tool_calls": calls}, "finish_reason": state.finishReason()}
	// readWhole 会保留 delta 的思考字段；借此复用现有累积和整段 SSE 合成，不改 OpenAI 行为。
	if thinking.Len() > 0 {
		choice["delta"] = map[string]any{"reasoning_content": thinking.String()}
	}
	out := map[string]any{"choices": []any{choice}}
	if state.usage != nil {
		out["usage"] = state.usage
	}
	return json.Marshal(out)
}

// geminiTouchReader 按底层字节刷新计时，心跳和仅含用量的块也能维持连接活跃。
type geminiTouchReader struct {
	io.Reader
	touch func()
}

func (r geminiTouchReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if n > 0 {
		r.touch()
	}
	return n, err
}

func readGeminiResponse(resp *http.Response, emit func(Event), touch func(), raw func([]byte)) (*Result, error) {
	source := geminiTouchReader{Reader: resp.Body, touch: touch}
	if strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
		body, err := io.ReadAll(io.LimitReader(source, maxStreamBytes+1))
		if err != nil {
			return nil, err
		}
		if len(body) > maxStreamBytes {
			return nil, ErrStreamTruncated
		}
		translated, err := geminiWholeToOpenAI(body)
		if err != nil {
			return nil, err
		}
		return readWhole(bytes.NewReader(translated), emit, raw)
	}
	return readSSE(newGeminiSSEReader(source), emit, touch, raw)
}

// geminiHTTPError 在通用分类、截断前移除媒体地址；OpenAI 的错误处理路径保持原样。
func geminiHTTPError(resp *http.Response, key string, request []byte) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrBodyBytes))
	var wire struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &wire) == nil {
		var req any
		if json.Unmarshal(request, &req) == nil {
			wire.Error.Message = redactGeminiMedia(wire.Error.Message, req)
		}
		body, _ = json.Marshal(wire)
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	return classifyHTTPError(resp, key)
}

func redactGeminiMedia(message string, value any) string {
	switch node := value.(type) {
	case []any:
		for _, child := range node {
			message = redactGeminiMedia(message, child)
		}
	case map[string]any:
		for key, child := range node {
			if key == "fileUri" || key == "data" {
				if address, ok := child.(string); ok && address != "" {
					message = strings.ReplaceAll(message, address, "***")
				}
			} else {
				message = redactGeminiMedia(message, child)
			}
		}
	}
	return message
}
