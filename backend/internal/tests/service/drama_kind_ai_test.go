package service_test

import (
	"context"
	"strings"
	"testing"

	"video-canvas/internal/service"
)

// 规则认不出类型的原文：没有分集、章节、场次标记，也没有对白。
var ambiguousSource = strings.Repeat("他走进房间，看见桌上放着一封信。\n", 40)

func preflightWith(t *testing.T, outputs []string, input service.DramaPreflightInput) (*service.DramaPreflightView, *dramaTextStreamer) {
	t.Helper()
	r := newDramaMemory()
	e := &dramaTextStreamer{outputs: outputs}
	s, _ := dramaTestService(r, e, nil)
	if input.Text == "" {
		input.Text = ambiguousSource
	}
	view, err := s.Preflight(context.Background(), 7, &input)
	if err != nil {
		t.Fatal(err)
	}
	return view, e
}

func TestPreflightAIClassifiesWhenRulesUnsure(t *testing.T) {
	view, e := preflightWith(t, []string{`{"kind":"novel","reason":"叙述为主，没有场次与对白"}`}, service.DramaPreflightInput{})
	if view.KindGuess != "novel" {
		t.Fatalf("应采用 AI 判断，got %q", view.KindGuess)
	}
	if !strings.Contains(view.KindSignals, "AI") || !strings.Contains(view.KindSignals, "叙述为主") {
		t.Fatalf("依据里应写明 AI 判断与理由: %q", view.KindSignals)
	}
	if e.calls != 1 || !strings.Contains(e.prompts[0], "他走进房间") {
		t.Fatalf("calls=%d，提示词应带原文采样", e.calls)
	}
}

func TestPreflightAIFailureFallsBackToUnknown(t *testing.T) {
	for name, outputs := range map[string][]string{
		"上游失败":     nil,
		"输出不是JSON": {"我觉得是小说"},
		"类型不认识":    {`{"kind":"poem"}`},
	} {
		t.Run(name, func(t *testing.T) {
			view, _ := preflightWith(t, outputs, service.DramaPreflightInput{})
			if view.KindGuess != "unknown" {
				t.Fatalf("AI 不可用时应保持 unknown 让用户手选，got %q", view.KindGuess)
			}
		})
	}
}

func TestPreflightSkipsAIWhenRulesOrUserDecided(t *testing.T) {
	script := "第1集 开始\n甲：走。\n第2集 结束\n乙：好。"
	view, e := preflightWith(t, []string{`{"kind":"novel"}`}, service.DramaPreflightInput{Text: script})
	if view.KindGuess != "script" || e.calls != 0 {
		t.Fatalf("规则已识别出剧本不应再问 AI: guess=%q calls=%d", view.KindGuess, e.calls)
	}
	view, e = preflightWith(t, []string{`{"kind":"script"}`}, service.DramaPreflightInput{SourceKind: "novel"})
	if e.calls != 0 || view.KindGuess == "script" {
		t.Fatalf("用户已指定类型不应再问 AI: guess=%q calls=%d", view.KindGuess, e.calls)
	}
}
