package plugin_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/plugin"
)

const dryRunImagesPlugin = `
module.exports = {
  buildSubmitRequest: function(ctx) {
    var content = [];
    for (var i = 0; i < ctx.input.images.length; i++) {
      content.push({type: "image_url", url: {__fileRef: ctx.input.images[i], as: "url"}});
    }
    return {method: "POST", path: "/run", json: {content: content}};
  },
  parseSubmitResponse: function(ctx) { return {immediate: {status: "succeeded", outputs: [{type: "url", url: ctx.channel.baseUrl + "/o.png"}]}}; }
};`

// dry-run 校验参考素材归属时要用发起人的 ID：素材属于发起人就能渲染，不能因为 user_id 为 0 而全部“素材不可用”。
func TestExecutorDryRunChecksAssetsAgainstCaller(t *testing.T) {
	assets := &fakeAssets{userID: 9, files: map[uint64][]byte{5: []byte("a"), 6: []byte("b")}}
	exec := execWith(dryRunImagesPlugin, func(o *plugin.Options) { o.Assets = assets })
	snap := imageSnap("http://127.0.0.1:1", dryRunImagesPlugin)
	input := withImageInput(map[string]any{"images": []any{uint64(5), uint64(6)}})

	t.Run("素材属于发起人，渲染成功", func(t *testing.T) {
		if _, err := exec.DryRun(context.Background(), 9, snap, input); err != nil {
			t.Fatalf("发起人自己的素材不应报错：%v", err)
		}
	})

	t.Run("素材不属于发起人，仍按素材不可用拒绝", func(t *testing.T) {
		_, err := exec.DryRun(context.Background(), 77, snap, input)
		var pe *provider.Error
		if !errors.As(err, &pe) || pe.Class != provider.ClassTerminal {
			t.Fatalf("别人的素材应报 terminal 错误：%v", err)
		}
	})
}

// dry-run 返回的请求描述里，每个素材引用都要能看出来是哪一个（input:images.N），不能序列化成空对象。
func TestExecutorDryRunDescribesEveryFileRef(t *testing.T) {
	assets := &fakeAssets{userID: 9, files: map[uint64][]byte{5: []byte("a"), 6: []byte("b")}}
	exec := execWith(dryRunImagesPlugin, func(o *plugin.Options) { o.Assets = assets })
	snap := imageSnap("http://127.0.0.1:1", dryRunImagesPlugin)

	res, err := exec.DryRun(context.Background(), 9, snap, withImageInput(map[string]any{"images": []any{uint64(5), uint64(6)}}))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		JSON struct {
			Content []struct {
				URL map[string]any `json:"url"`
			} `json:"content"`
		} `json:"json"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%v：%s", err, raw)
	}
	if len(out.JSON.Content) != 2 {
		t.Fatalf("应有 2 个素材：%s", raw)
	}
	for i, want := range []string{"input:images.0", "input:images.1"} {
		got := out.JSON.Content[i].URL
		if got["__fileRef"] != want || got["as"] != "url" {
			t.Fatalf("第 %d 个素材引用应是 %s（as=url）：%v", i, want, got)
		}
	}
}

// multipart / form 的请求描述同样要带出普通字段的值和文件引用：之前 fields、parts 都序列化成空对象，
// form 里的文件字段还会被写成空字符串，运营看不出到底发了什么。
func TestExecutorDryRunDescribesFormAndMultipart(t *testing.T) {
	assets := &fakeAssets{userID: 9, files: map[uint64][]byte{5: []byte("a")}}
	run := func(t *testing.T, code string) map[string]any {
		t.Helper()
		exec := execWith(code, func(o *plugin.Options) { o.Assets = assets })
		res, err := exec.DryRun(context.Background(), 9, imageSnap("http://127.0.0.1:1", code), withImageInput(map[string]any{"images": []any{uint64(5)}}))
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(res)
		var out map[string]any
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	const tail = `parseSubmitResponse: function(ctx) { return {immediate: {status: "succeeded", outputs: [{type: "url", url: ctx.channel.baseUrl + "/o.png"}]}}; } };`

	t.Run("multipart", func(t *testing.T) {
		out := run(t, `module.exports = { buildSubmitRequest: function(ctx) {
  return {method: "POST", path: "/run", multipart: {fields: {prompt: "hi"}, parts: [{name: "input_reference", fileRef: ctx.input.images[0], filename: "a.png"}]}};
}, `+tail)
		mp, _ := out["multipart"].(map[string]any)
		fields, _ := mp["fields"].(map[string]any)
		parts, _ := mp["parts"].([]any)
		if fields["prompt"] != "hi" {
			t.Fatalf("普通字段应带出值：%v", mp)
		}
		part, _ := parts[0].(map[string]any)
		if len(parts) != 1 || part["name"] != "input_reference" || part["fileRef"] != "input:images.0" || part["filename"] != "a.png" {
			t.Fatalf("文件部分应带出名称与引用：%v", parts)
		}
	})

	t.Run("form 里的文件字段保留引用", func(t *testing.T) {
		out := run(t, `module.exports = { buildSubmitRequest: function(ctx) {
  return {method: "POST", path: "/run", form: {prompt: "hi", image: {__fileRef: ctx.input.images[0], as: "url"}}};
}, `+tail)
		form, _ := out["form"].(map[string]any)
		ref, _ := form["image"].(map[string]any)
		if form["prompt"] != "hi" || ref["__fileRef"] != "input:images.0" || ref["as"] != "url" {
			t.Fatalf("form 应带出字段值与文件引用：%v", form)
		}
	})
}
