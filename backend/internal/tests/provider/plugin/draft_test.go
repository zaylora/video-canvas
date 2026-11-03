package plugin_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/plugin"
	"video-canvas/internal/provider/pluginrunner"
)

func TestDraftChecker_MediaReferences(t *testing.T) {
	const code = `module.exports={
meta:{apiVersion:1,key:"draft-media",name:"媒体样例",auth:{type:"bearer"},endpoints:{video:{mode:"sync"}}},
buildSubmitRequest:function(ctx){return {method:"POST",path:"/generate",json:{images:ctx.input.images.map(function(ref){return {__fileRef:ref,as:"url"};}),audio:{__fileRef:"input:audios.0",as:"url"},video:{__fileRef:"input:videos.0",as:"url"}}};},
parseSubmitResponse:function(ctx,res){return {immediate:[{url:res.body.url,mediaType:"video"}]};}
};`
	for _, tc := range []struct {
		name   string
		images []any
		change string
		wantOK bool
	}{
		{"三张参考图与音视频占位引用", []any{"input:images.0", "input:images.1", "input:images.2"}, "", true},
		{"文档URL不被下载而转成占位引用", []any{"https://example.invalid/a.jpg", "https://example.invalid/b.jpg", "https://example.invalid/c.jpg"}, "", true},
		{"真实素材ID同样规范化", []any{"101", "102", "103"}, "", true},
		{"越界引用必须失败", []any{"101", "102", "103"}, "input:audios.9", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checker := plugin.NewDraftChecker(plugin.NewInProcessRunnerClient(pluginrunner.NewServer(pluginrunner.Options{}).Handler()))
			source := code
			if tc.change != "" {
				source = strings.Replace(source, "input:audios.0", tc.change, 1)
			}
			pre, err := checker.Precheck(context.Background(), source)
			if err != nil || !pre.OK {
				t.Fatalf("预检失败：%+v %v", pre, err)
			}
			out, err := checker.CheckDraft(context.Background(), provider.PluginDraftInput{Code: source, CtxSample: map[string]any{
				"channel": map[string]any{"baseUrl": "https://example.invalid"},
				"model":   map[string]any{"kind": "video", "upstreamModel": "sample"},
				"input":   map[string]any{"images": tc.images, "audios": []any{"sample-audio"}, "videos": []any{"sample-video"}},
			}}, pre)
			if err != nil || out.OK != tc.wantOK {
				t.Fatalf("样例执行：%+v %v", out, err)
			}
			if tc.wantOK {
				if out.Coverage == nil || out.Coverage.InputMedia["images"] != 3 || out.Coverage.InputMedia["audios"] != 1 || out.Coverage.InputMedia["videos"] != 1 {
					t.Fatalf("未报告实际媒体覆盖：%+v", out.Coverage)
				}
				raw, _ := json.Marshal(out.Request)
				if !strings.Contains(string(raw), "input:images.2") || !strings.Contains(string(raw), "input:videos.0") {
					t.Fatalf("媒体引用丢失：%s", raw)
				}
			}
		})
	}
}
