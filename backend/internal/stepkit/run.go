package stepkit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"video-canvas/internal/llmgateway"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
)

// maxAttempts 是一次运行最多调用模型的次数：首次加一次带原因的重写。
const maxAttempts = 2

// 运行错误的类别。调用失败和校验失败要区分：前者是上游问题（可以稍后重试），后者是方法或模型的输出不合格。
var (
	// ErrCallFailed 表示模型调用本身失败（上游错误、超时、渠道不可用）。用 errors.As 取 *CallError 看底层原因。
	ErrCallFailed = errors.New("模型调用失败")
	// ErrOutputInvalid 表示重写后输出仍然不合格。用 errors.As 取 *ValidationError 看原因列表。
	ErrOutputInvalid = errors.New("输出不合格")
	// ErrModelCapability 表示模型能力或输入素材不满足环节的媒体需求（不会调用模型）。
	ErrModelCapability = errors.New("模型能力不满足环节需求")
)

// CallError 是模型调用失败，errors.Is(err, ErrCallFailed) 为真，同时保留底层错误供 errors.Is / As 判断。
type CallError struct{ Err error }

// Error 实现 error。
func (e *CallError) Error() string { return "模型调用失败：" + e.Err.Error() }

// Unwrap 返回底层错误。
func (e *CallError) Unwrap() error { return e.Err }

// Is 让 errors.Is(err, ErrCallFailed) 成立。
func (e *CallError) Is(target error) bool { return target == ErrCallFailed }

// ValidationError 是重写后仍不合格的输出：Errors 是最后一次的硬错误原因。
type ValidationError struct{ Errors []string }

// Error 实现 error。
func (e *ValidationError) Error() string { return "输出不合格：" + strings.Join(e.Errors, "；") }

// Is 让 errors.Is(err, ErrOutputInvalid) 成立。
func (e *ValidationError) Is(target error) bool { return target == ErrOutputInvalid }

// Caller 是一次模型调用：发一条用户消息（可带图片、视频 Part），返回完整结果。
// 生产实现是 GatewayCaller，测试用假实现。
type Caller interface {
	// Complete 向快照里的模型发一次对话请求。
	Complete(ctx context.Context, snap *provider.Snapshot, msgs []llmgateway.Message) (*llmgateway.Result, error)
}

// Result 是一次运行的结果。出错时 Run 也会返回已有的部分结果（用量、尝试次数、最后一次输出），便于计费和排查。
type Result struct {
	Output       json.RawMessage // 最后一次的输出 JSON；无法解析时为 nil
	Flags        Flags           // 软问题，只在成功时有值
	Usage        llmgateway.Usage
	UsageMissing bool // 有一次调用上游没给用量：计费按估算
	Attempts     int  // 实际调用模型的次数，1 或 2
	PromptChars  int  // 各次提示词的字符数之和，计费估算用
	Method       MethodSnapshot
	Duration     time.Duration
}

// Runner 执行环节：拼提示词、调模型、解码、校验、必要时重写一次。
type Runner struct{ caller Caller }

// NewRunner 创建执行器。
func NewRunner(c Caller) *Runner { return &Runner{caller: c} }

// Run 用 method 和 model 运行 step：
//  1. 按媒体需求准备多模态 Parts（视频按 Capabilities.Video 判断，不支持时用 in.Images 里调用方准备好的截帧）；
//  2. 拼提示词并调用模型；
//  3. 解码 JSON，先过 Schema 检查，再调环节的 Validate；
//  4. 有硬错误就带原因重写一次，仍不合格返回 *ValidationError；调用本身失败返回 *CallError（不重试）。
func (r *Runner) Run(ctx context.Context, step Step, method Method, model *provider.Snapshot, in Input) (*Result, error) {
	parts, err := mediaParts(step, model, in)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	res := &Result{Method: method.Snapshot()}
	defer func() { res.Duration = time.Since(start) }()
	var feedback []string
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		prompt, err := BuildPrompt(step, method, in, feedback)
		if err != nil {
			return res, err
		}
		res.Attempts = attempt
		res.PromptChars += utf8.RuneCountInString(prompt)
		reply, err := r.caller.Complete(ctx, model, []llmgateway.Message{buildMessage(prompt, parts)})
		if err != nil {
			return res, &CallError{Err: err}
		}
		res.Usage.InputTokens += reply.Usage.InputTokens
		res.Usage.OutputTokens += reply.Usage.OutputTokens
		res.Usage.CachedTokens += reply.Usage.CachedTokens
		res.UsageMissing = res.UsageMissing || reply.UsageMissing
		out, flags, errs := checkOutput(step, in, reply.Text)
		res.Output = out
		if len(errs) == 0 {
			res.Flags = flags
			return res, nil
		}
		feedback = errs
	}
	return res, &ValidationError{Errors: feedback}
}

// checkOutput 解码并校验一次输出：返回输出 JSON（能解析时）、软问题和硬错误原因。
func checkOutput(step Step, in Input, text string) (json.RawMessage, Flags, []string) {
	out, err := ExtractJSON(text)
	if err != nil {
		return nil, nil, []string{err.Error()}
	}
	if errs := CheckSchema(step.Schema, out); len(errs) > 0 {
		return out, nil, errs
	}
	flags, err := step.Validate(out, in)
	if err != nil {
		return out, nil, ErrorList(err)
	}
	return out, flags, nil
}

// ExtractJSON 从模型回复里取出 JSON：去掉首尾空白和代码围栏，要求整段是合法 JSON，不做宽松截取
// （夹带解释文字属于不合格输出，交给重写去纠正）。
func ExtractJSON(text string) (json.RawMessage, error) {
	s := strings.TrimSpace(text)
	if strings.HasPrefix(s, "```") {
		if nl := strings.IndexByte(s, '\n'); nl >= 0 {
			s = s[nl+1:]
		}
		s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "```"))
	}
	if !json.Valid([]byte(s)) {
		return nil, errors.New("输出不是合法 JSON，只能输出一个 JSON 对象，不要夹带解释文字")
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(s)); err != nil {
		return nil, errors.New("输出不是合法 JSON")
	}
	return buf.Bytes(), nil
}

// mediaParts 按环节的媒体需求准备多模态 Part：
//   - none：不附；
//   - images：需要 Vision 且有图片；
//   - video：有视频且模型支持 Capabilities.Video 时直接附视频（此时忽略 Images）；
//     否则退回 Vision + in.Images（调用方准备好的截帧）。
//
// 能力或素材不满足时返回 ErrModelCapability，不调用模型。
func mediaParts(step Step, snap *provider.Snapshot, in Input) ([]llmgateway.Part, error) {
	caps := snap.Model.Capabilities
	switch step.Media {
	case MediaImages:
		if !caps.Vision {
			return nil, fmt.Errorf("%w：环节 %s 需要看图，所选模型不支持", ErrModelCapability, step.Key)
		}
		if len(in.Images) == 0 {
			return nil, fmt.Errorf("%w：环节 %s 需要图片输入", ErrModelCapability, step.Key)
		}
		return imageParts(in.Images), nil
	case MediaVideo:
		if in.Video != nil && caps.Video {
			return []llmgateway.Part{{Video: in.Video}}, nil
		}
		if caps.Vision && len(in.Images) > 0 {
			return imageParts(in.Images), nil
		}
		return nil, fmt.Errorf("%w：环节 %s 需要视频，所选模型不能看视频，也没有可用的截帧", ErrModelCapability, step.Key)
	}
	return nil, nil
}

// imageParts 把图片地址变成多模态 Part。
func imageParts(urls []string) []llmgateway.Part {
	out := make([]llmgateway.Part, 0, len(urls))
	for _, u := range urls {
		out = append(out, llmgateway.Part{ImageURL: u})
	}
	return out
}

// buildMessage 组装用户消息：没有媒体时只发文字，有媒体时文字块在最前，素材附在后面。
func buildMessage(prompt string, media []llmgateway.Part) llmgateway.Message {
	if len(media) == 0 {
		return llmgateway.Message{Role: llmgateway.RoleUser, Text: prompt}
	}
	parts := append([]llmgateway.Part{{Text: prompt}}, media...)
	return llmgateway.Message{Role: llmgateway.RoleUser, Parts: parts}
}

// Cost 按模型定价和实际用量算一次运行应收的积分（各次调用累加）；上游没给用量时按预估收。
func Cost(snap *provider.Snapshot, res *Result) int {
	var usage *modelcfg.Usage
	if !res.UsageMissing {
		usage = &modelcfg.Usage{InputTokens: max(0, res.Usage.InputTokens), OutputTokens: max(0, res.Usage.OutputTokens)}
	}
	quoted := modelcfg.Quote(snap.Model.Pricing, snap.Model.Capabilities, modelcfg.Spec{PromptChars: res.PromptChars, Params: map[string]any{}})
	return modelcfg.Settle(snap.Model.Pricing, quoted, usage)
}

// Estimate 按提示词字数预估一次运行的最高费用（假设用满重写次数），用于冻结积分。
func Estimate(snap *provider.Snapshot, step Step, method Method, in Input) (int, error) {
	prompt, err := BuildPrompt(step, method, in, nil)
	if err != nil {
		return 0, err
	}
	chars := utf8.RuneCountInString(prompt)
	one := modelcfg.Quote(snap.Model.Pricing, snap.Model.Capabilities, modelcfg.Spec{PromptChars: chars, Params: map[string]any{}})
	return one * maxAttempts, nil
}
