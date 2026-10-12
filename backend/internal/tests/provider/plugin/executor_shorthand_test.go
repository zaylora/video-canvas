package plugin_test

// 本文件：统一结果的简写（设计文档 docs/design/插件简化/插件简化.md §6.3）。
// 只放宽“漏写”：产物可以写成字符串、对象缺 type 时按 url / text 补、失败可以直接写字符串、error.class 缺省为 terminal；
// 写了就要写对：非法的 class、非法的 status、图片模型返回非地址的字符串，仍然是 terminal 插件级失败。

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/modelcfg"
	"video-canvas/internal/provider/pluginmeta"
)

// shorthandSubmit 用一个同步插件跑一次提交，插件的 parseSubmitResponse 直接返回 ret 这段 JS 表达式。
func shorthandSubmit(t *testing.T, kind, ret string) (*provider.SubmitResult, error) {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) }))
	t.Cleanup(upstream.Close)
	code := `module.exports = {
  buildSubmitRequest: function() { return {method: "GET", path: "/x"}; },
  parseSubmitResponse: function() { return ` + ret + `; }
};`
	snap := testSnapshot(upstream.URL, code, kind, pluginmeta.Auth{Type: pluginmeta.AuthNone})
	snap.Plugin.Meta.Endpoints = map[string]pluginmeta.Endpoint{kind: {Mode: pluginmeta.ModeSync}}
	return submit(execWith(code, nil), snap)
}

func wantPluginFault(t *testing.T, err error) {
	t.Helper()
	var pe *provider.Error
	if !errors.As(err, &pe) || pe.Class != provider.ClassTerminal || !pe.PluginFault {
		t.Fatalf("应报 terminal 插件级失败：%v", err)
	}
}

func TestShorthandOutputs(t *testing.T) {
	t.Run("图片模型：outputs 写 URL 字符串", func(t *testing.T) {
		res, err := shorthandSubmit(t, modelcfg.KindImage, `{immediate: {status: "succeeded", outputs: ["https://cdn.example.com/a.png"]}}`)
		if err != nil {
			t.Fatal(err)
		}
		out := res.Immediate.Outputs[0]
		if out.Type != provider.OutputURL || out.URL != "https://cdn.example.com/a.png" || out.MediaType != modelcfg.KindImage {
			t.Fatalf("应补成 url 产物并取模型种类作为 media_type：%+v", out)
		}
	})

	t.Run("视频模型：多个 URL 字符串", func(t *testing.T) {
		res, err := shorthandSubmit(t, modelcfg.KindVideo, `{immediate: {status: "succeeded", outputs: ["https://a.com/1.mp4", "https://b.com/2.mp4"]}}`)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Immediate.Outputs) != 2 || res.Immediate.Outputs[1].URL != "https://b.com/2.mp4" || res.Immediate.Outputs[1].MediaType != modelcfg.KindVideo {
			t.Fatalf("产物不符合预期：%+v", res.Immediate.Outputs)
		}
	})

	t.Run("文本模型：outputs 写正文字符串", func(t *testing.T) {
		res, err := shorthandSubmit(t, modelcfg.KindText, `{immediate: {status: "succeeded", outputs: ["你好"]}}`)
		if err != nil {
			t.Fatal(err)
		}
		out := res.Immediate.Outputs[0]
		if out.Type != provider.OutputText || out.Text != "你好" || out.MediaType != modelcfg.KindText {
			t.Fatalf("应补成 text 产物：%+v", out)
		}
	})

	t.Run("对象缺 type：有 url 补 url，有 text 补 text", func(t *testing.T) {
		res, err := shorthandSubmit(t, modelcfg.KindImage, `{immediate: {status: "succeeded", outputs: [{url: "https://cdn.example.com/b.png", mime: "image/png"}]}}`)
		if err != nil {
			t.Fatal(err)
		}
		if o := res.Immediate.Outputs[0]; o.Type != provider.OutputURL || o.Mime != "image/png" {
			t.Fatalf("应补 type=url 并保留其他字段：%+v", o)
		}
		res, err = shorthandSubmit(t, modelcfg.KindText, `{immediate: {status: "succeeded", outputs: [{text: "hi", usage: {input_tokens: 3, output_tokens: 5}}]}}`)
		if err != nil {
			t.Fatal(err)
		}
		if o := res.Immediate.Outputs[0]; o.Type != provider.OutputText || o.Usage == nil || o.Usage.OutputTokens != 5 {
			t.Fatalf("应补 type=text 并保留 usage：%+v", o)
		}
	})

	t.Run("完整写法照旧可用，字符串与对象可以混写", func(t *testing.T) {
		res, err := shorthandSubmit(t, modelcfg.KindImage, `{immediate: {status: "succeeded", outputs: [{type: "url", url: "https://a.com/1.png", media_type: "image"}, "https://a.com/2.png"]}}`)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Immediate.Outputs) != 2 || res.Immediate.Outputs[1].Type != provider.OutputURL {
			t.Fatalf("产物不符合预期：%+v", res.Immediate.Outputs)
		}
	})

	t.Run("图片模型的字符串不是 http(s) 地址：仍然是插件故障", func(t *testing.T) {
		for _, bad := range []string{"data:image/png;base64,iVBOR", "ftp://a.com/x.png", "just text", ""} {
			_, err := shorthandSubmit(t, modelcfg.KindImage, `{immediate: {status: "succeeded", outputs: ["`+bad+`"]}}`)
			wantPluginFault(t, err)
		}
	})

	t.Run("对象既没有 type 也没有 url / text：仍然是插件故障", func(t *testing.T) {
		_, err := shorthandSubmit(t, modelcfg.KindImage, `{immediate: {status: "succeeded", outputs: [{mime: "image/png"}]}}`)
		wantPluginFault(t, err)
	})

	t.Run("文本模型返回 url 对象：仍然是插件故障", func(t *testing.T) {
		_, err := shorthandSubmit(t, modelcfg.KindText, `{immediate: {status: "succeeded", outputs: [{url: "https://a.com/x"}]}}`)
		wantPluginFault(t, err)
	})
}

func TestShorthandErrors(t *testing.T) {
	t.Run("error 直接写字符串：class 缺省为 terminal", func(t *testing.T) {
		res, err := shorthandSubmit(t, modelcfg.KindImage, `{immediate: {status: "failed", error: "上游提示：内容审核未通过"}}`)
		if err != nil {
			t.Fatal(err)
		}
		q := res.Immediate
		if q.Status != provider.StatusFailed || q.ErrorClass != provider.ClassTerminal || q.ErrorMessage != "上游提示：内容审核未通过" {
			t.Fatalf("应补成 terminal 失败：%+v", q)
		}
	})

	t.Run("error 对象缺 class：补 terminal，其余字段保留", func(t *testing.T) {
		res, err := shorthandSubmit(t, modelcfg.KindImage, `{immediate: {status: "failed", error: {code: "E1002", message: "账号余额不足"}}}`)
		if err != nil {
			t.Fatal(err)
		}
		q := res.Immediate
		if q.ErrorClass != provider.ClassTerminal || q.ErrorCode != "E1002" || q.ErrorMessage != "账号余额不足" {
			t.Fatalf("不符合预期：%+v", q)
		}
	})

	t.Run("完整写法：显式 class 原样保留", func(t *testing.T) {
		res, err := shorthandSubmit(t, modelcfg.KindImage, `{immediate: {status: "failed", error: {class: "moderation", message: "x"}}}`)
		if err != nil {
			t.Fatal(err)
		}
		if res.Immediate.ErrorClass != provider.ClassModeration {
			t.Fatalf("显式 class 不应被改：%+v", res.Immediate)
		}
	})

	t.Run("class 写了但不合法：仍然是插件故障（写了就要写对）", func(t *testing.T) {
		_, err := shorthandSubmit(t, modelcfg.KindImage, `{immediate: {status: "failed", error: {class: "fatal", message: "x"}}}`)
		wantPluginFault(t, err)
	})

	t.Run("failed 没有 error：仍然是插件故障", func(t *testing.T) {
		_, err := shorthandSubmit(t, modelcfg.KindImage, `{immediate: {status: "failed"}}`)
		wantPluginFault(t, err)
	})

	t.Run("error 是空字符串或无信息的对象：仍然是插件故障", func(t *testing.T) {
		for _, e := range []string{`""`, `{}`, `42`, `[]`} {
			_, err := shorthandSubmit(t, modelcfg.KindImage, `{immediate: {status: "failed", error: `+e+`}}`)
			wantPluginFault(t, err)
		}
	})
}

// status 不放宽：宿主不替插件猜“上游的哪个状态算成功”。
func TestShorthandStatusNotRelaxed(t *testing.T) {
	for _, st := range []string{"SUCCESS", "success", "completed", "Succeeded", "done", ""} {
		_, err := shorthandSubmit(t, modelcfg.KindImage, `{immediate: {status: "`+st+`", outputs: ["https://a.com/1.png"]}}`)
		wantPluginFault(t, err)
	}
}

// 异步查询返回的结果走同一套规范化。
func TestShorthandQueryResult(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) }))
	defer upstream.Close()
	run := func(ret string) (*provider.QueryResult, error) {
		code := `module.exports = {
  buildSubmitRequest: function() { return {method: "GET", path: "/x"}; },
  parseSubmitResponse: function() { return {providerTaskId: "t1"}; },
  buildQueryRequest: function() { return {method: "GET", path: "/q"}; },
  parseQueryResponse: function() { return ` + ret + `; }
};`
		snap := testSnapshot(upstream.URL, code, modelcfg.KindVideo, pluginmeta.Auth{Type: pluginmeta.AuthNone})
		snap.Plugin.Meta.Endpoints = map[string]pluginmeta.Endpoint{modelcfg.KindVideo: {Mode: pluginmeta.ModeAsync}}
		return execWith(code, nil).Query(t.Context(), snap, provider.TaskRef{ID: 1, ProviderTaskID: "t1"})
	}

	res, err := run(`{status: "succeeded", outputs: ["https://cdn.example.com/v.mp4"]}`)
	if err != nil || res.Outputs[0].Type != provider.OutputURL || res.Outputs[0].MediaType != modelcfg.KindVideo {
		t.Fatalf("查询结果也应支持产物简写：%+v %v", res, err)
	}
	res, err = run(`{status: "failed", error: "任务超时"}`)
	if err != nil || res.ErrorClass != provider.ClassTerminal || res.ErrorMessage != "任务超时" {
		t.Fatalf("查询结果也应支持失败简写：%+v %v", res, err)
	}
	if _, err = run(`{status: "failed", error: {class: "fatal"}}`); err == nil {
		t.Fatal("非法 class 应报错")
	}
}
