package stepkit_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"video-canvas/internal/llmgateway"
	"video-canvas/internal/stepkit"
)

func runStep(t *testing.T, caller stepkit.Caller, step stepkit.Step, in stepkit.Input) (*stepkit.Result, error) {
	t.Helper()
	return stepkit.NewRunner(caller).Run(context.Background(), step, stepkit.BuiltinMethod(step), snapshot(true, true), in)
}

func TestRunSucceedsFirstTry(t *testing.T) {
	caller := &fakeCaller{replies: []fakeReply{{text: "```json\n" + goodOutput + "\n```", usage: llmgateway.Usage{InputTokens: 100, OutputTokens: 20}}}}
	res, err := runStep(t, caller, newTestStep(), stepkit.Input{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Attempts != 1 || len(caller.calls) != 1 {
		t.Fatalf("应只调用一次：%d", res.Attempts)
	}
	var out map[string]any
	if json.Unmarshal(res.Output, &out) != nil || out["title"] != "晨雾" {
		t.Fatalf("输出应去掉代码围栏后解码：%s", res.Output)
	}
	if res.Usage.InputTokens != 100 || res.Usage.OutputTokens != 20 || res.PromptChars == 0 {
		t.Fatalf("用量不对：%+v", res)
	}
	if res.Method.Source != "builtin" || res.Method.SHA256 == "" {
		t.Fatalf("应带方法快照：%+v", res.Method)
	}
}

// 软问题不触发重写，原样返回 flags。
func TestRunSoftFlagsDoNotRetry(t *testing.T) {
	caller := &fakeCaller{replies: []fakeReply{{text: `{"title":"夸张标题","shots":[{"text":"a"}]}`}}}
	res, err := runStep(t, caller, newTestStep(), stepkit.Input{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Attempts != 1 || len(res.Flags) != 1 || res.Flags[0].Type != "claim" {
		t.Fatalf("软问题应返回 flags 且不重写：%+v", res)
	}
}

// 硬错误：带原因重写一次，第二次通过；两次用量累加。
func TestRunRewritesOnceAfterHardError(t *testing.T) {
	caller := &fakeCaller{replies: []fakeReply{
		{text: `{"title":"禁用词标题","shots":[{"text":"a"}]}`, usage: llmgateway.Usage{InputTokens: 100, OutputTokens: 10}},
		{text: goodOutput, usage: llmgateway.Usage{InputTokens: 120, OutputTokens: 15}},
	}}
	res, err := runStep(t, caller, newTestStep(), stepkit.Input{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Attempts != 2 {
		t.Fatalf("应重写一次：%d", res.Attempts)
	}
	if !strings.Contains(caller.promptOf(1), "标题含禁用词") {
		t.Fatal("重写提示词应带上上次的硬错误原因")
	}
	if strings.Contains(caller.promptOf(0), "上次输出") {
		t.Fatal("首次提示词不应带反馈")
	}
	if res.Usage.InputTokens != 220 || res.Usage.OutputTokens != 25 {
		t.Fatalf("用量应累加：%+v", res.Usage)
	}
}

// 重写后仍不合格：返回 ErrOutputInvalid（不是调用失败），带原因列表和最后一次输出。
func TestRunStillInvalidAfterRewrite(t *testing.T) {
	caller := &fakeCaller{replies: []fakeReply{
		{text: `{"title":"禁用1","shots":[{"text":"a"}]}`},
		{text: `{"title":"禁用2","shots":[{"text":"a"}]}`},
		{text: goodOutput},
	}}
	res, err := runStep(t, caller, newTestStep(), stepkit.Input{})
	if !errors.Is(err, stepkit.ErrOutputInvalid) || errors.Is(err, stepkit.ErrCallFailed) {
		t.Fatalf("应是输出不合格：%v", err)
	}
	if len(caller.calls) != 2 {
		t.Fatalf("最多调用两次：%d", len(caller.calls))
	}
	var verr *stepkit.ValidationError
	if !errors.As(err, &verr) || len(verr.Errors) != 1 || verr.Errors[0] != "标题含禁用词" {
		t.Fatalf("原因列表不对：%+v", err)
	}
	if res == nil || res.Attempts != 2 || !strings.Contains(string(res.Output), "禁用2") {
		t.Fatalf("失败时也应返回最后一次输出和尝试次数：%+v", res)
	}
}

// 调用本身失败（上游错误、超时）与校验失败要区分，且不重写。
func TestRunCallFailureIsDistinct(t *testing.T) {
	boom := errors.New("上游 502")
	caller := &fakeCaller{replies: []fakeReply{{err: boom}}}
	_, err := runStep(t, caller, newTestStep(), stepkit.Input{})
	if !errors.Is(err, stepkit.ErrCallFailed) || errors.Is(err, stepkit.ErrOutputInvalid) {
		t.Fatalf("应是调用失败：%v", err)
	}
	if !errors.Is(err, boom) {
		t.Fatal("应保留底层错误")
	}
	if len(caller.calls) != 1 {
		t.Fatal("调用失败不应重试")
	}
}

// 重写那次调用失败：同样按调用失败返回，并保留首次的用量。
func TestRunCallFailureOnRewrite(t *testing.T) {
	caller := &fakeCaller{replies: []fakeReply{
		{text: `{"title":"禁用","shots":[{"text":"a"}]}`, usage: llmgateway.Usage{InputTokens: 50, OutputTokens: 5}},
		{err: errors.New("超时")},
	}}
	res, err := runStep(t, caller, newTestStep(), stepkit.Input{})
	if !errors.Is(err, stepkit.ErrCallFailed) {
		t.Fatalf("%v", err)
	}
	if res == nil || res.Usage.InputTokens != 50 {
		t.Fatalf("应保留已发生的用量：%+v", res)
	}
}

// 不是 JSON、缺必填字段、数量越界都是硬错误，由协议层的 Schema 检查抓住，不依赖环节自己的 Validate。
func TestRunSchemaViolationsAreHardErrors(t *testing.T) {
	cases := map[string]string{
		"不是 JSON":      "我觉得可以这样写",
		"缺必填":          `{"title":"x"}`,
		"类型不对":         `{"title":1,"shots":[{"text":"a"}]}`,
		"数组为空":         `{"title":"x","shots":[]}`,
		"数组超长":         `{"title":"x","shots":[{"text":"a"},{"text":"a"},{"text":"a"},{"text":"a"}]}`,
		"元素缺必填":        `{"title":"x","shots":[{}]}`,
		"顶层不是对象":       `[1,2]`,
		"尾部多余内容":       goodOutput + " 以上",
		"代码围栏里不是 JSON": "```json\nnot json\n```",
	}
	for name, text := range cases {
		caller := &fakeCaller{replies: []fakeReply{{text: text}, {text: text}}}
		_, err := runStep(t, caller, newTestStep(), stepkit.Input{})
		if !errors.Is(err, stepkit.ErrOutputInvalid) {
			t.Fatalf("%s：应是输出不合格：%v", name, err)
		}
		if len(caller.calls) != 2 {
			t.Fatalf("%s：应重写一次，调用 %d 次", name, len(caller.calls))
		}
	}
}

// 多模态：图片按 Vision 附在同一条消息里；视频按 Capabilities.Video 附；都不满足返回能力错误，不调用模型。
func TestRunMediaParts(t *testing.T) {
	step := newTestStep()
	step.Media = stepkit.MediaVideo
	video := &llmgateway.Video{MimeType: "video/mp4", URL: "https://x/v.mp4"}
	images := []string{"https://x/1.jpg", "https://x/2.jpg"}

	t.Run("模型支持视频", func(t *testing.T) {
		caller := &fakeCaller{replies: []fakeReply{{text: goodOutput}}}
		_, err := stepkit.NewRunner(caller).Run(context.Background(), step, stepkit.BuiltinMethod(step), snapshot(true, true), stepkit.Input{Video: video, Images: images})
		if err != nil {
			t.Fatal(err)
		}
		parts := caller.calls[0][0].Parts
		if len(parts) < 2 || parts[0].Text == "" {
			t.Fatalf("文字块应在最前：%+v", parts)
		}
		var gotVideo bool
		for _, p := range parts {
			if p.Video != nil {
				gotVideo = true
			}
		}
		if !gotVideo {
			t.Fatal("应附视频")
		}
	})
	t.Run("不支持视频退回图片", func(t *testing.T) {
		caller := &fakeCaller{replies: []fakeReply{{text: goodOutput}}}
		_, err := stepkit.NewRunner(caller).Run(context.Background(), step, stepkit.BuiltinMethod(step), snapshot(true, false), stepkit.Input{Video: video, Images: images})
		if err != nil {
			t.Fatal(err)
		}
		var n int
		for _, p := range caller.calls[0][0].Parts {
			if p.Video != nil {
				t.Fatal("模型不支持视频，不应附视频")
			}
			if p.ImageURL != "" {
				n++
			}
		}
		if n != 2 {
			t.Fatalf("应附 2 张截帧图：%d", n)
		}
	})
	t.Run("都不支持", func(t *testing.T) {
		caller := &fakeCaller{}
		_, err := stepkit.NewRunner(caller).Run(context.Background(), step, stepkit.BuiltinMethod(step), snapshot(false, false), stepkit.Input{Video: video, Images: images})
		if !errors.Is(err, stepkit.ErrModelCapability) {
			t.Fatalf("应返回能力错误：%v", err)
		}
		if len(caller.calls) != 0 {
			t.Fatal("能力不足不应调用模型")
		}
	})
	t.Run("看图环节没有图", func(t *testing.T) {
		s := newTestStep()
		s.Media = stepkit.MediaImages
		_, err := stepkit.NewRunner(&fakeCaller{}).Run(context.Background(), s, stepkit.BuiltinMethod(s), snapshot(true, false), stepkit.Input{})
		if !errors.Is(err, stepkit.ErrModelCapability) {
			t.Fatalf("没有输入图片应报错：%v", err)
		}
	})
	t.Run("无媒体环节不附 Parts", func(t *testing.T) {
		caller := &fakeCaller{replies: []fakeReply{{text: goodOutput}}}
		_, err := stepkit.NewRunner(caller).Run(context.Background(), newTestStep(), stepkit.BuiltinMethod(newTestStep()), snapshot(false, false), stepkit.Input{Images: images})
		if err != nil {
			t.Fatal(err)
		}
		if len(caller.calls[0][0].Parts) != 0 || caller.calls[0][0].Text == "" {
			t.Fatal("无媒体环节应只发文字")
		}
	})
}
