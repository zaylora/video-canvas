package llmgateway_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"video-canvas/internal/llmgateway"
)

func TestGemini_VideoParts(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		video                  llmgateway.Video
		field, valueKey, value string
	}{
		{"对象存储", llmgateway.Video{MimeType: "video/mp4", URL: "https://cdn.example.com/video.mp4?sig=secret"}, "fileData", "fileUri", "https://cdn.example.com/video.mp4?sig=secret"},
		{"本地存储", llmgateway.Video{MimeType: "video/webm", Data: "VklERU8="}, "inlineData", "data", "VklERU8="},
	} {
		t.Run(tc.name, func(t *testing.T) {
			up := newUpstream(t, func(w http.ResponseWriter, _ *http.Request, _ []byte) {
				sse(w, `{"candidates":[{"content":{"parts":[{"text":"对白：出发。"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3000,"candidatesTokenCount":20}}`)
			})
			req := llmgateway.Request{Messages: []llmgateway.Message{{Role: llmgateway.RoleUser, Parts: []llmgateway.Part{{Text: "分析声音"}, {Video: &tc.video}}}}}
			res, err := gateway().Stream(context.Background(), geminiTarget(up), req, func(llmgateway.Event) {})
			if err != nil || res.Text != "对白：出发。" || res.Usage.InputTokens != 3000 {
				t.Fatalf("res=%+v err=%v", res, err)
			}
			part := contentParts(t, up.requests()[0].Body, 0)[1].(map[string]any)[tc.field].(map[string]any)
			if part["mimeType"] != tc.video.MimeType || part[tc.valueKey] != tc.value {
				t.Fatalf("视频未正确序列化: %v", part)
			}
			_, err = gateway().Stream(context.Background(), target(up), req, func(llmgateway.Event) {})
			if !errors.Is(err, llmgateway.ErrInvalidRequest) || len(up.requests()) != 1 {
				t.Fatalf("OpenAI 应在出站前拒绝视频: %v", err)
			}
		})
	}
}

func TestGemini_InvalidVideoPart(t *testing.T) {
	for _, video := range []string{
		`{"mime_type":"image/png","data":"VklERU8="}`,
		`{"mime_type":"video/mp4"}`,
		`{"mime_type":"video/mp4","url":"https://cdn.example/a","data":"VklERU8="}`,
		`{"mime_type":"video/mp4","url":"file:///private/video.mp4"}`,
		`{"mime_type":"video/mp4","data":"invalid base64"}`,
	} {
		_, err := llmgateway.ToGemini([]byte(`{"messages":[{"role":"user","content":[{"type":"video","video":` + video + `}]}]}`))
		if !errors.Is(err, llmgateway.ErrInvalidRequest) {
			t.Fatalf("非法视频应被拒绝: %s err=%v", video, err)
		}
	}
}
