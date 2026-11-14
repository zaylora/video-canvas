package llmgateway_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"video-canvas/internal/llmgateway"
)

// recorder 是测试用观察者：记下每次尝试的记录。
type recorder struct {
	mu   sync.Mutex
	recs []llmgateway.CallRecord
}

func (r *recorder) OnCall(_ context.Context, rec llmgateway.CallRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recs = append(r.recs, rec)
}

func (r *recorder) all() []llmgateway.CallRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]llmgateway.CallRecord(nil), r.recs...)
}

func observed(rec *recorder, opts ...func(*llmgateway.Options)) *llmgateway.Gateway {
	return gateway(append([]func(*llmgateway.Options){func(o *llmgateway.Options) { o.Observer = rec }}, opts...)...)
}

func TestObserver_SuccessRecordsRequestAndResult(t *testing.T) {
	up := newUpstream(t, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		time.Sleep(20 * time.Millisecond)
		sse(w, chunk(`{"content":"你好"}`, ""), chunk(`{"content":"呀"}`, "stop"), usageChunk(12, 3, 2), "[DONE]")
	})
	rec := &recorder{}
	if _, err := streamOnce(t, observed(rec), target(up)); err != nil {
		t.Fatal(err)
	}
	recs := rec.all()
	if len(recs) != 1 {
		t.Fatalf("一次调用应回调一次，实际 %d", len(recs))
	}
	r := recs[0]
	if r.Attempt != 1 || r.Status != http.StatusOK || r.Err != nil || r.Result == nil || r.Result.Text != "你好呀" {
		t.Errorf("成功记录不对：%+v", r)
	}
	if r.Result.Usage.InputTokens != 12 || r.Result.Usage.CachedTokens != 2 {
		t.Errorf("应带用量：%+v", r.Result.Usage)
	}
	if !strings.HasSuffix(r.Endpoint, "/chat/completions") || r.ChannelKey != "ch1" || r.UpstreamModel != "gpt-up" {
		t.Errorf("应带地址、渠道和上游模型：%+v", r)
	}
	if !strings.Contains(string(r.Body), `"gpt-up"`) || strings.Contains(string(r.Body), "sk-secret-key") {
		t.Errorf("应带实际请求体，且不含 Key：%s", r.Body)
	}
	if r.TTFT <= 0 || r.Latency < r.TTFT {
		t.Errorf("应带首字耗时和总耗时：ttft=%v latency=%v", r.TTFT, r.Latency)
	}
}

func TestObserver_UpstreamErrorKeepsRedactedBody(t *testing.T) {
	up := newUpstream(t, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad key sk-secret-key","type":"invalid_request_error","param":"max_tokens"}}`))
	})
	rec := &recorder{}
	_, err := streamOnce(t, observed(rec), target(up))
	var ue *llmgateway.UpstreamError
	if !errors.As(err, &ue) {
		t.Fatalf("应返回 UpstreamError：%v", err)
	}
	if !strings.Contains(ue.Body, `"param":"max_tokens"`) || strings.Contains(ue.Body, "sk-secret-key") || !strings.Contains(ue.Body, "***") {
		t.Errorf("UpstreamError.Body 应保留错误体原文并遮掉 Key：%q", ue.Body)
	}
	if strings.Contains(ue.Error(), `"param"`) {
		t.Errorf("Error() 不应带错误体原文，免得经 err.Error() 漏到别处：%s", ue.Error())
	}
	r := rec.all()[0]
	if r.Status != http.StatusBadRequest || r.ErrorBody != ue.Body || r.Err == nil {
		t.Errorf("观察者应拿到状态码和错误体原文：%+v", r)
	}
}

func TestObserver_StreamErrorKeepsBody(t *testing.T) {
	up := newUpstream(t, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
		sse(w, `{"error":{"message":"content filtered","code":"sensitive"}}`)
	})
	_, err := streamOnce(t, gateway(), target(up))
	var ue *llmgateway.UpstreamError
	if !errors.As(err, &ue) || !strings.Contains(ue.Body, `"code":"sensitive"`) {
		t.Errorf("流里的 error 也要保留原文：%v %+v", err, ue)
	}
}

func TestObserver_IdleRetryRecordsEachAttempt(t *testing.T) {
	var hits atomic.Int32
	up := newUpstream(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if hits.Add(1) == 1 {
			hang(r)
			return
		}
		sse(w, chunk(`{"content":"好了"}`, "stop"), "[DONE]")
	})
	rec := &recorder{}
	if _, err := streamOnce(t, observed(rec, func(o *llmgateway.Options) { o.IdleTimeout = 150 * time.Millisecond }), target(up)); err != nil {
		t.Fatal(err)
	}
	recs := rec.all()
	if len(recs) != 2 || recs[0].Attempt != 1 || !errors.Is(recs[0].Err, llmgateway.ErrIdleTimeout) || recs[1].Attempt != 2 || recs[1].Err != nil {
		t.Errorf("空闲超时重试应各回调一次：%+v", recs)
	}
}

func TestObserver_NotCalledForInvalidTarget(t *testing.T) {
	rec := &recorder{}
	_, err := streamOnce(t, observed(rec), llmgateway.Target{BaseURL: "ftp://x", APIKey: "k", UpstreamModel: "m"})
	if err == nil || len(rec.all()) != 0 {
		t.Errorf("没发出请求时不回调：err=%v recs=%d", err, len(rec.all()))
	}
}
