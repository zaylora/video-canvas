package modelcfg_test

import (
	"testing"

	"video-canvas/internal/provider/modelcfg"
)

func TestJoinFieldErrors(t *testing.T) {
	got := modelcfg.JoinFieldErrors([]modelcfg.FieldError{
		{Field: "prompt", Message: "不能为空"},
		{Message: "整体不合法"},
		{Field: "ratio", Message: "不支持"},
	})
	if want := "prompt：不能为空；整体不合法；ratio：不支持"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if modelcfg.JoinFieldErrors(nil) != "" {
		t.Fatal("空列表应返回空字符串")
	}
}
