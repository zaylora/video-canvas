package agent_test

import (
	"context"
	"net/http"
	"testing"

	"video-canvas/internal/pkg/errcode"
)

type dramaModePlatform struct {
	fakePlatform
	mode string
}

func (f *dramaModePlatform) CheckCanvasMode(_ context.Context, userID, canvasID uint64, mode string) error {
	f.checkedUser, f.checkedCanvas, f.mode = userID, canvasID, mode
	return f.err
}

func TestPlatformHandler_CheckCanvasDramaMode(t *testing.T) {
	api := &dramaModePlatform{}
	w := platformCall(newPlatformRouter(api), http.MethodPost, "/internal/platform/v1/canvases/check", `{"user_id":"1","canvas_id":"7","mode":"drama"}`, nil)
	if w.Code != 200 || api.mode != "drama" || api.checkedUser != 1 || api.checkedCanvas != 7 {
		t.Fatalf("mode=%s status=%d %s", api.mode, w.Code, w.Body)
	}
	api.err = errcode.ErrInvalidParams
	w = platformCall(newPlatformRouter(api), http.MethodPost, "/internal/platform/v1/canvases/check", `{"user_id":"1","canvas_id":"7","mode":"drama"}`, nil)
	if w.Code != 400 || respCode(t, w) != errcode.ErrInvalidParams.Code {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	api.err = errcode.ErrCanvasNotFound
	w = platformCall(newPlatformRouter(api), http.MethodPost, "/internal/platform/v1/canvases/check", `{"user_id":"2","canvas_id":"7","mode":"drama"}`, nil)
	if w.Code != 404 || respCode(t, w) != errcode.ErrCanvasNotFound.Code {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	w = platformCall(newPlatformRouter(api), http.MethodPost, "/internal/platform/v1/canvases/check", `{"user_id":"1","canvas_id":"7","mode":"unknown"}`, nil)
	if w.Code != 400 || respCode(t, w) != errcode.ErrInvalidParams.Code {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}
