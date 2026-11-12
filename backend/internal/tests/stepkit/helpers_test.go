package stepkit_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"

	"video-canvas/internal/llmgateway"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/stepkit"
)

// testSchema 是测试环节的输出 Schema：必填 title（字符串）和 shots（1 到 3 条）。
const testSchema = `{
  "type": "object",
  "required": ["title", "shots"],
  "properties": {
    "title": {"type": "string", "description": "标题"},
    "shots": {"type": "array", "minItems": 1, "maxItems": 3, "description": "镜头列表",
      "items": {"type": "object", "required": ["text"], "properties": {"text": {"type": "string", "description": "镜头描述"}}}}
  }
}`

// newTestStep 返回一个带校验函数的测试环节：标题含「禁用」是硬错误，含「夸张」是软问题。
func newTestStep() stepkit.Step {
	return stepkit.Step{
		Key:    "test.step",
		Title:  "测试环节",
		Studio: "test",
		Duty:   "把商品信息写成镜头列表。",
		Inputs: []stepkit.Var{{Name: "product", Type: "string", Desc: "商品名"}},
		Media:  stepkit.MediaNone,
		Schema: json.RawMessage(testSchema),
		Rules:  stepkit.Rules{Hard: []string{"标题不能含禁用词"}, Soft: []string{"标题不宜夸张"}},
		Validate: func(out json.RawMessage, _ stepkit.Input) (stepkit.Flags, error) {
			var v struct {
				Title string `json:"title"`
			}
			if err := json.Unmarshal(out, &v); err != nil {
				return nil, err
			}
			if strings.Contains(v.Title, "禁用") {
				return nil, stepkit.Hard("标题含禁用词")
			}
			var flags stepkit.Flags
			if strings.Contains(v.Title, "夸张") {
				flags = append(flags, stepkit.Flag{Type: "claim", Text: "标题夸张"})
			}
			return flags, nil
		},
		Builtin: "内置方法：先想卖点，再写镜头。",
	}
}

// fakeCaller 按顺序返回预设的回复，并记录每次收到的消息。
type fakeCaller struct {
	mu      sync.Mutex
	replies []fakeReply
	calls   [][]llmgateway.Message
}

type fakeReply struct {
	text  string
	usage llmgateway.Usage
	err   error
}

func (f *fakeCaller) Complete(_ context.Context, _ *provider.Snapshot, msgs []llmgateway.Message) (*llmgateway.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, msgs)
	if len(f.calls) > len(f.replies) {
		return nil, errors.New("没有更多预设回复")
	}
	r := f.replies[len(f.calls)-1]
	if r.err != nil {
		return nil, r.err
	}
	return &llmgateway.Result{Text: r.text, Usage: r.usage}, nil
}

// promptOf 取第 n 次调用里的文字内容（单文本消息取 Text，多模态取所有文字 Part 拼接）。
func (f *fakeCaller) promptOf(n int) string {
	msg := f.calls[n][0]
	if len(msg.Parts) == 0 {
		return msg.Text
	}
	var b strings.Builder
	for _, p := range msg.Parts {
		b.WriteString(p.Text)
	}
	return b.String()
}

// snapshot 构造一个文本模型快照，vision / video 控制多模态能力。
func snapshot(vision, video bool) *provider.Snapshot {
	return &provider.Snapshot{Model: provider.ModelSnapshot{
		Key: "text-a", Kind: modelcfg.KindAgent,
		Capabilities: modelcfg.Capabilities{Vision: vision, Video: video},
	}}
}

const goodOutput = `{"title":"晨雾","shots":[{"text":"开场"}]}`
