package llmgateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
)

// RawLimits 是透传时对请求体的收紧。
type RawLimits struct {
	MaxTokens int // 单次输出 Token 上限（模型的 context.output）；请求里没写或写得更大都收紧到它，0 表示不限制
}

// rawAllowed 是透传时放行的请求体字段。请求体由我们自己起的 Node 进程里的 pi 拼出来，但这条路上只该有对话本身：
// 其余字段（logit_bias、user、n、response_format、store……）一律丢掉，渠道 Key 的权限不该被拿来试探上游的其他功能。
var rawAllowed = []string{"messages", "tools", "tool_choice", "temperature", "top_p", "stop"}

// sanitizeRaw 把调用方拼好的 OpenAI 请求体收紧成可以发往渠道的形状：
//   - 只保留白名单字段，messages 必须是非空数组（原样保留，含图片分段）；
//   - model 强制成渠道的上游模型名，stream 和 stream_options.include_usage 强制打开（计费靠用量）；
//   - max_tokens / max_completion_tokens 合并成 max_tokens，并按上限收紧。
func sanitizeRaw(body []byte, tg Target, lim RawLimits) ([]byte, error) {
	var in map[string]json.RawMessage
	if err := json.Unmarshal(body, &in); err != nil || in == nil {
		return nil, fmt.Errorf("%w: 请求体不是 JSON 对象", ErrInvalidRequest)
	}
	var msgs []json.RawMessage
	if err := json.Unmarshal(in["messages"], &msgs); err != nil || len(msgs) == 0 {
		return nil, fmt.Errorf("%w: messages 必须是非空数组", ErrInvalidRequest)
	}
	out := map[string]any{
		"model":          tg.UpstreamModel,
		"stream":         true,
		"stream_options": map[string]any{"include_usage": true},
	}
	for _, k := range rawAllowed {
		if v, ok := in[k]; ok {
			out[k] = v
		}
	}
	if n := capTokens(requestedTokens(in), lim.MaxTokens); n > 0 {
		out["max_tokens"] = n
	}
	return json.Marshal(out)
}

// requestedTokens 取请求里写的输出上限（两种字段名都认），没写返回 0。
func requestedTokens(in map[string]json.RawMessage) int {
	for _, k := range []string{"max_tokens", "max_completion_tokens"} {
		var f float64
		if raw, ok := in[k]; ok && json.Unmarshal(raw, &f) == nil && f > 0 {
			return int(math.Min(f, math.MaxInt32))
		}
	}
	return 0
}

// capTokens 按上限收紧：没写或写得更大取上限；没有上限就照请求。
func capTokens(requested, limit int) int {
	if limit <= 0 {
		return requested
	}
	if requested <= 0 || requested > limit {
		return limit
	}
	return requested
}

// synthesizeSSE 把一份整段（非流式）结果改写成 SSE 行：调用方按流式协议解析，上游忽略 stream 时也要能用。
func synthesizeSSE(res *Result) [][]byte {
	delta := map[string]any{"role": "assistant", "content": res.Text}
	if res.Thinking != "" {
		delta["reasoning_content"] = res.Thinking
	}
	if len(res.ToolCalls) > 0 {
		calls := make([]any, len(res.ToolCalls))
		for i, c := range res.ToolCalls {
			calls[i] = map[string]any{"index": i, "id": c.ID, "type": "function", "function": map[string]any{"name": c.Name, "arguments": c.Arguments}}
		}
		delta["tool_calls"] = calls
	}
	finish := res.FinishReason
	if finish == "" {
		finish = "stop"
	}
	chunks := []any{map[string]any{"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}}}
	if !res.UsageMissing {
		chunks = append(chunks, map[string]any{"choices": []any{}, "usage": map[string]any{
			"prompt_tokens": res.Usage.InputTokens, "completion_tokens": res.Usage.OutputTokens,
			"prompt_tokens_details": map[string]any{"cached_tokens": res.Usage.CachedTokens},
		}})
	}
	var lines [][]byte
	for _, c := range chunks {
		b, _ := json.Marshal(c) // 只含字符串和数字，序列化不会失败
		lines = append(lines, append(append([]byte("data: "), b...), '\n', '\n'))
	}
	return append(lines, bytes.Clone([]byte("data: [DONE]\n\n")))
}
