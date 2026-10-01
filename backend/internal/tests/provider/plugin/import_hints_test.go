// 本文件：导入模型的参数预填建议（paramHints）：宿主按契约解码、透传给管理端，格式不合规时整次导入按插件故障失败。

package plugin_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"video-canvas/internal/provider"
)

func importCode(hints string) string {
	return `
module.exports = {
  buildImportRequest: function() { return {method: "GET", path: "/models"}; },
  parseImportResponse: function() {
    return [{upstreamModel: "m-1", kind: "image", label: "一号", params: {groups: {"2K": "4"}}, paramHints: ` + hints + `}];
  }
};`
}

func TestExecutorImportParamHints(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) }))
	defer srv.Close()

	t.Run("合法的建议按参数名透传", func(t *testing.T) {
		code := importCode(`{resolution: {options: ["2K", "4K"], default: "2K"}, duration: {min: 4, max: 15, step: 1, default: 5},
			generate_audio: {remove: true}, aspect_ratio: {open: false}}`)
		drafts, err := execWith(code, nil).Import(context.Background(), textSnap(srv.URL, code).Runtime(), map[string]any{})
		if err != nil || len(drafts) != 1 {
			t.Fatalf("%v %+v", err, drafts)
		}
		h := drafts[0].ParamHints
		if r := h["resolution"]; len(r.Options) != 2 || r.Default != "2K" {
			t.Fatalf("resolution 建议不符：%+v", r)
		}
		if d := h["duration"]; d.Min == nil || *d.Min != 4 || *d.Max != 15 || d.Default != 5.0 {
			t.Fatalf("duration 建议不符：%+v", d)
		}
		if !h["generate_audio"].Remove || h["aspect_ratio"].Open == nil || *h["aspect_ratio"].Open {
			t.Fatalf("remove / open 不符：%+v", h)
		}
		if drafts[0].Params["groups"] == nil {
			t.Fatal("params 应原样保留")
		}
	})

	t.Run("没有建议时为空", func(t *testing.T) {
		code := importCode(`undefined`)
		drafts, err := execWith(code, nil).Import(context.Background(), textSnap(srv.URL, code).Runtime(), map[string]any{})
		if err != nil || len(drafts) != 1 || drafts[0].ParamHints != nil {
			t.Fatalf("%v %+v", err, drafts)
		}
	})

	for name, hints := range map[string]string{
		"参数名不合法":    `{"Resolution": {options: ["2K"]}}`,
		"可选值是对象":    `{resolution: {options: [{value: "2K"}]}}`,
		"可选值是空字符串":  `{resolution: {options: [""]}}`,
		"默认值是对象":    `{resolution: {default: {}}}`,
		"最大值超出范围":   `{duration: {max: 99999}}`,
		"建议不是对象的映射": `[1, 2]`,
		"最小值不是整数":   `{duration: {min: 1.5}}`,
	} {
		t.Run("不合规："+name, func(t *testing.T) {
			code := importCode(hints)
			_, err := execWith(code, nil).Import(context.Background(), textSnap(srv.URL, code).Runtime(), map[string]any{})
			var pe *provider.Error
			if !errors.As(err, &pe) || pe.Class != provider.ClassTerminal {
				t.Fatalf("应按插件故障失败：%v", err)
			}
		})
	}
}
