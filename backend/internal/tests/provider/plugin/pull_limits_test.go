package plugin_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"video-canvas/internal/provider"
)

// 上游的 /v1/models 有的会随模型带上下文规格：Anthropic 是 max_input_tokens / max_tokens，
// OpenRouter 是 context_length / top_provider.max_completion_tokens。没带的模型不进结果。
func TestListUpstreamLimits_识别各家字段(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[
			{"id":"claude-x","max_input_tokens":200000,"max_tokens":64000},
			{"id":"or/m","context_length":131072,"top_provider":{"max_completion_tokens":16384}},
			{"id":"only-window","context_window":32000},
			{"id":"misc","context_window":100000,"max_output_tokens":8000},
			{"id":"plain"},
			{"id":"zero","max_input_tokens":0,"max_tokens":0},
			{"id":"neg","context_length":-5}
		]}`))
	}))
	defer srv.Close()

	got, err := pullExecutor(func() bool { return true }).ListUpstreamLimits(context.Background(), provider.ChannelSnapshot{Key: "c1", BaseURL: srv.URL}, "k")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]provider.UpstreamLimits{
		"claude-x":    {Window: 200000, Output: 64000},
		"or/m":        {Window: 131072, Output: 16384},
		"only-window": {Window: 32000},
		"misc":        {Window: 100000, Output: 8000},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("规格 = %v，期望 %v（没带规格、规格为 0 或负数的模型不进结果）", got, want)
	}
}

func TestListUpstreamLimits_失败原样返回拉取错误(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) }))
	defer srv.Close()
	_, err := pullExecutor(func() bool { return true }).ListUpstreamLimits(context.Background(), provider.ChannelSnapshot{Key: "c1", BaseURL: srv.URL}, "k")
	var pe *provider.PullError
	if !errors.As(err, &pe) || pe.Reason != provider.PullUnauthorized {
		t.Fatalf("应是 PullUnauthorized：%v", err)
	}
}
