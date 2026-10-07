package llmgateway

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"
)

// maxStreamBytes 是一次响应最多读多少字节，防止上游无限输出。
const maxStreamBytes = 32 << 20

// wireUsage 是上游报告的用量。
type wireUsage struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	PromptTokensDetails struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
}

func (u *wireUsage) toUsage() Usage {
	return Usage{InputTokens: u.PromptTokens, OutputTokens: u.CompletionTokens, CachedTokens: u.PromptTokensDetails.CachedTokens}
}

// wireToolCall 是上游的工具调用片段。
type wireToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// wireChunk 是一个流式块；同一个结构也能读整段（非流式）响应里的 message。
type wireChunk struct {
	Choices []struct {
		Delta struct {
			Content          *string        `json:"content"`
			ReasoningContent *string        `json:"reasoning_content"`
			Reasoning        *string        `json:"reasoning"`
			ToolCalls        []wireToolCall `json:"tool_calls"`
		} `json:"delta"`
		Message struct {
			Content   *string        `json:"content"`
			ToolCalls []wireToolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *wireUsage `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// accumulator 把流式块累积成完整结果，并随时产出事件。
type accumulator struct {
	emit     func(Event)
	text     strings.Builder
	thinking strings.Builder
	calls    map[int]*ToolCall
	finish   string
	usage    *Usage
}

func newAccumulator(emit func(Event)) *accumulator {
	return &accumulator{emit: emit, calls: map[int]*ToolCall{}}
}

// apply 处理一个块。上游在流里报错时返回 *UpstreamError。
func (a *accumulator) apply(c *wireChunk) error {
	if c.Error != nil {
		return &UpstreamError{Status: 502, Message: truncate(c.Error.Message, maxErrRunes)}
	}
	if c.Usage != nil {
		u := c.Usage.toUsage()
		a.usage = &u
		a.emit(Event{Type: EventUsage, Usage: &u})
	}
	for _, ch := range c.Choices {
		d := ch.Delta
		if r := firstNonNil(d.ReasoningContent, d.Reasoning); r != "" {
			a.thinking.WriteString(r)
			a.emit(Event{Type: EventThinking, Text: r})
		}
		if d.Content != nil && *d.Content != "" {
			a.text.WriteString(*d.Content)
			a.emit(Event{Type: EventText, Text: *d.Content})
		}
		for _, tc := range d.ToolCalls {
			a.addToolDelta(tc)
		}
		if ch.FinishReason != nil && *ch.FinishReason != "" {
			a.finish = *ch.FinishReason
		}
	}
	return nil
}

// addToolDelta 按序号把工具调用的片段拼起来：id 和名字只在首段出现，参数分多段到达。
func (a *accumulator) addToolDelta(tc wireToolCall) {
	call := a.calls[tc.Index]
	if call == nil {
		call = &ToolCall{}
		a.calls[tc.Index] = call
	}
	if tc.ID != "" {
		call.ID = tc.ID
	}
	if tc.Function.Name != "" {
		call.Name = tc.Function.Name
	}
	call.Arguments += tc.Function.Arguments
	a.emit(Event{Type: EventToolDelta, ToolIndex: tc.Index, ToolID: tc.ID, ToolName: tc.Function.Name, ArgsDelta: tc.Function.Arguments})
}

// result 生成最终结果。
func (a *accumulator) result() *Result {
	res := &Result{Text: a.text.String(), Thinking: a.thinking.String(), FinishReason: a.finish}
	idx := make([]int, 0, len(a.calls))
	for i := range a.calls {
		idx = append(idx, i)
	}
	sort.Ints(idx)
	for _, i := range idx {
		res.ToolCalls = append(res.ToolCalls, *a.calls[i])
	}
	if a.usage != nil {
		res.Usage = *a.usage
	} else {
		res.UsageMissing = true
	}
	return res
}

// readSSE 读一个 SSE 流：只认 data 行，注释行和 event 行忽略。
// 收到 [DONE] 或者「已有 finish_reason 后连接正常关闭」算成功（部分上游不发 [DONE]）；
// 其余提前关闭返回 ErrStreamTruncated，已收到的部分仍随结果返回。touch 在每读到一行时调用，用来重置空闲计时；
// raw 不为 nil 时，每读到一行就原样回调一次（透传）。
func readSSE(r io.Reader, emit func(Event), touch func(), raw func([]byte)) (*Result, error) {
	acc := newAccumulator(emit)
	br := bufio.NewReaderSize(io.LimitReader(r, maxStreamBytes), 64<<10)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			touch()
			if raw != nil {
				raw(line) // 原样转给调用方，包括空行分隔符和 [DONE]
			}
			done, perr := acc.handleLine(bytes.TrimSpace(line))
			if perr != nil {
				return acc.result(), perr
			}
			if done {
				return acc.result(), nil
			}
		}
		if err == nil {
			continue
		}
		if errors.Is(err, io.EOF) && acc.finish != "" {
			return acc.result(), nil
		}
		if errors.Is(err, io.EOF) {
			return acc.result(), ErrStreamTruncated
		}
		return acc.result(), err
	}
}

// handleLine 处理一行；done 表示收到了 [DONE]。解析不了的 data 行忽略（部分上游会夹杂非 JSON 的心跳）。
func (a *accumulator) handleLine(line []byte) (done bool, err error) {
	payload, ok := bytes.CutPrefix(line, []byte("data:"))
	if !ok {
		return false, nil
	}
	payload = bytes.TrimSpace(payload)
	if string(payload) == "[DONE]" {
		return true, nil
	}
	var c wireChunk
	if json.Unmarshal(payload, &c) != nil {
		return false, nil
	}
	return false, a.apply(&c)
}

// readWhole 读整段（非流式）JSON 响应，转成同样的事件和结果：上游忽略了 stream 参数时用。
func readWhole(r io.Reader, emit func(Event), raw func([]byte)) (*Result, error) {
	var c wireChunk
	if err := json.NewDecoder(io.LimitReader(r, maxStreamBytes)).Decode(&c); err != nil {
		return nil, ErrStreamTruncated
	}
	for i := range c.Choices { // 整段响应的内容在 message 里，挪到 delta 上走同一条路径
		m := c.Choices[i].Message
		c.Choices[i].Delta.Content = m.Content
		c.Choices[i].Delta.ToolCalls = m.ToolCalls
		for j := range c.Choices[i].Delta.ToolCalls {
			c.Choices[i].Delta.ToolCalls[j].Index = j
		}
	}
	acc := newAccumulator(emit)
	if err := acc.apply(&c); err != nil {
		return nil, err
	}
	res := acc.result()
	if raw != nil {
		for _, l := range synthesizeSSE(res) {
			raw(l)
		}
	}
	return res, nil
}

func firstNonNil(ps ...*string) string {
	for _, p := range ps {
		if p != nil && *p != "" {
			return *p
		}
	}
	return ""
}
