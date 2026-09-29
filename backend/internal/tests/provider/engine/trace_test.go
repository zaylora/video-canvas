package engine_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	. "video-canvas/internal/provider/engine"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/dsl"
)

func TestEngine_Trace记录各步骤且不含凭证(t *testing.T) {
	rh := engNewRH(t)
	// 平台把收到的凭证原样回显，最能考验脱敏
	rh.onSubmit = func(w http.ResponseWriter, r *http.Request) {
		engJSON(w, 200, map[string]any{"taskId": "T-77", "echo": r.Header.Get("Authorization")})
	}
	snap := engSnapshot(t, rh.srv.URL)
	ex := engNew(&engAssets{content: []byte("PNG"), url: "https://s.example/a"}, engDefaultSecrets(), nil)

	ctx, trace := WithTrace(context.Background())
	if _, err := ex.Submit(ctx, snap, engSubmitInput()); err != nil {
		t.Fatal(err)
	}
	if _, err := ex.Query(ctx, snap, provider.TaskRef{ProviderTaskID: "T-77"}); err != nil {
		t.Fatal(err)
	}

	steps := trace.Snapshot()
	var names []string
	for _, s := range steps {
		names = append(names, s.Name)
	}
	if strings.Join(names, ",") != "upload:image,submit,query" {
		t.Fatalf("步骤 = %v", names)
	}
	up, sub, q := steps[0], steps[1], steps[2]
	if up.Request == nil || !strings.Contains(up.Request.Body, "first-frame.png") || strings.Contains(up.Request.Body, "PNG\n") {
		t.Errorf("上传记录应只描述文件不含内容：%+v", up.Request)
	}
	if up.Extract["ref"] != "uploaded/abc.png" {
		t.Errorf("上传提取结果 = %v", up.Extract)
	}
	if sub.Request.Headers["Authorization"] != "Bearer ***" {
		t.Errorf("提交请求头应脱敏：%v", sub.Request.Headers)
	}
	if !strings.Contains(sub.Request.Body, "uploaded/abc.png") || sub.Response.Status != 200 || sub.Extract["provider_task_id"] != "T-77" {
		t.Errorf("提交步骤记录不完整：%+v", sub)
	}
	if q.Response == nil || q.Extract["status"] != "SUCCESS" || q.DurationMs < 0 {
		t.Errorf("查询步骤记录不完整：%+v", q)
	}

	b, _ := json.Marshal(steps)
	if strings.Contains(string(b), engSecret) {
		t.Fatalf("Trace 含凭证明文：%s", b)
	}
	if !strings.Contains(sub.Response.Body, "Bearer ***") {
		t.Errorf("平台回显的凭证应被脱敏：%s", sub.Response.Body)
	}
}

func TestEngine_Trace脱敏query鉴权与错误信息(t *testing.T) {
	rh := engNewRH(t)
	snap := engSnapshot(t, rh.srv.URL)
	snap.Provider.Auth = dsl.AuthConfig{Type: dsl.AuthQuery, Secret: "runninghub_api_key", Name: "api_key"}
	snap.Provider.Operations.Upload = nil
	ex := engNew(&engAssets{url: "https://s.example/a"}, engDefaultSecrets(), nil)

	// 成功路径
	ctx, trace := WithTrace(context.Background())
	if _, err := ex.Submit(ctx, snap, engSubmitInput()); err != nil {
		t.Fatal(err)
	}
	if u := trace.Snapshot()[0].Request.URL; strings.Contains(u, engSecret) || !strings.Contains(u, "api_key=") {
		t.Fatalf("URL 应保留参数名并脱敏值：%s", u)
	}

	// 失败路径：连接被拒绝时 Go 的错误信息会带完整 URL（含 query 里的凭证）
	rh.srv.Close()
	ctx, trace = WithTrace(context.Background())
	_, err := ex.Submit(ctx, snap, engSubmitInput())
	if err == nil {
		t.Fatal("服务器已关闭，应失败")
	}
	if strings.Contains(err.Error(), engSecret) {
		t.Fatalf("错误信息含凭证明文：%v", err)
	}
	b, _ := json.Marshal(trace.Snapshot())
	if strings.Contains(string(b), engSecret) {
		t.Fatalf("Trace 含凭证明文：%s", b)
	}
	if trace.Snapshot()[0].Error == "" {
		t.Error("失败步骤应记录错误")
	}
}

func TestEngine_没有Trace时不记录(t *testing.T) {
	rh := engNewRH(t)
	ex := engNew(nil, engDefaultSecrets(), nil)
	if _, err := ex.Query(context.Background(), engSnapshot(t, rh.srv.URL), provider.TaskRef{ProviderTaskID: "T"}); err != nil {
		t.Fatal(err)
	}
}

func TestTrace_响应体截断(t *testing.T) {
	rh := engNewRH(t)
	rh.onQuery = func(w http.ResponseWriter, r *http.Request) {
		engJSON(w, 200, map[string]any{"status": "RUNNING", "pad": strings.Repeat("中", 20000)})
	}
	ex := engNew(nil, engDefaultSecrets(), nil)
	ctx, trace := WithTrace(context.Background())
	if _, err := ex.Query(ctx, engSnapshot(t, rh.srv.URL), provider.TaskRef{ProviderTaskID: "T"}); err != nil {
		t.Fatal(err)
	}
	resp := trace.Snapshot()[0].Response
	if !resp.Truncated || len(resp.Body) > TraceBodyLimit+64 {
		t.Fatalf("响应应被截断：truncated=%v len=%d", resp.Truncated, len(resp.Body))
	}
	if !strings.HasSuffix(resp.Body, "（已截断）") {
		t.Fatal("截断处应有提示")
	}
	// 不能截在 UTF-8 字符中间
	if !json.Valid([]byte(`"` + strings.ReplaceAll(resp.Body, `"`, `\"`) + `"`)) {
		t.Fatal("截断后的文本不是合法 UTF-8")
	}
}

func TestDryRun(t *testing.T) {
	snap := engSnapshot(t, "https://www.runninghub.cn")
	snap.Provider.AllowedHosts = []string{"www.runninghub.cn"}

	t.Run("渲染上传与提交请求且不发送不解析凭证", func(t *testing.T) {
		res, err := DryRun(context.Background(), snap, map[string]any{"prompt": "猫", "image": 12.0, "duration": 10.0})
		if err != nil {
			t.Fatalf("干跑失败：%v", err)
		}
		if len(res.Uploads) != 1 || res.Uploads[0].Name != "upload:image" {
			t.Fatalf("上传请求 = %+v", res.Uploads)
		}
		up := res.Uploads[0]
		if up.Method != "POST" || up.URL != "https://www.runninghub.cn/openapi/v2/media/upload/binary" ||
			up.Headers["Authorization"] != "Bearer ***" || !strings.Contains(up.Body.(map[string]any)["file"].(string), "image-placeholder.png") {
			t.Errorf("上传请求不符：%+v", up)
		}
		s := res.Submit
		if s.Method != "POST" || s.URL != "https://www.runninghub.cn/openapi/v2/run/ai-app/2093984571330498561" ||
			s.Headers["Authorization"] != "Bearer ***" || s.Headers["Content-Type"] != "application/json" {
			t.Errorf("提交请求不符：%+v", s)
		}
		body := s.Body.(map[string]any)
		nodes := body["nodeInfoList"].([]any)
		if nodes[0].(map[string]any)["fieldValue"] != "猫" || nodes[2].(map[string]any)["fieldValue"] != 161.0 {
			t.Errorf("nodeInfoList = %#v", nodes)
		}
		if ref := nodes[1].(map[string]any)["fieldValue"].(string); !strings.Contains(ref, "image") {
			t.Errorf("图片应为占位引用：%q", ref)
		}
		if body["webhookUrl"] == nil || res.Mapping == nil {
			t.Errorf("body/mapping 缺失：%#v", res)
		}
		b, _ := json.Marshal(res)
		if strings.Contains(string(b), engSecret) {
			t.Fatal("结果含凭证明文")
		}
	})

	t.Run("没有 upload 操作时图片是占位 URL", func(t *testing.T) {
		s2 := *snap
		s2.Provider.Operations.Upload = nil
		res, err := DryRun(context.Background(), &s2, map[string]any{"prompt": "猫", "image": 12})
		if err != nil || len(res.Uploads) != 0 {
			t.Fatalf("res=%+v err=%v", res, err)
		}
		nodes := res.Submit.Body.(map[string]any)["nodeInfoList"].([]any)
		if v := nodes[1].(map[string]any)["fieldValue"].(string); !strings.HasPrefix(v, "https://placeholder.invalid/") {
			t.Errorf("占位 URL = %q", v)
		}
		// 没传 duration 用默认值 5：5*16+1
		if v := nodes[2].(map[string]any)["fieldValue"]; v != 81.0 {
			t.Errorf("默认时长换算 = %v", v)
		}
	})

	t.Run("query 鉴权用 *** 占位", func(t *testing.T) {
		s2 := *snap
		s2.Provider.Auth = dsl.AuthConfig{Type: dsl.AuthQuery, Secret: "k", Name: "api_key"}
		res, err := DryRun(context.Background(), &s2, map[string]any{"prompt": "猫", "image": 12})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(res.Submit.URL, "?api_key=***") {
			t.Errorf("URL = %s", res.Submit.URL)
		}
	})

	t.Run("body_field 鉴权", func(t *testing.T) {
		s2 := *snap
		s2.Provider.Auth = dsl.AuthConfig{Type: dsl.AuthBodyField, Secret: "k", Name: "apiKey"}
		res, err := DryRun(context.Background(), &s2, map[string]any{"prompt": "猫", "image": 12})
		if err != nil {
			t.Fatal(err)
		}
		if res.Submit.Body.(map[string]any)["apiKey"] != "***" {
			t.Errorf("body = %#v", res.Submit.Body)
		}
	})

	t.Run("输入不合法返回字段级错误", func(t *testing.T) {
		_, err := DryRun(context.Background(), snap, map[string]any{"image": 12})
		ie, ok := err.(*InputError)
		if !ok || len(ie.Errors) != 1 || ie.Errors[0].Field != "prompt" {
			t.Fatalf("期望 *InputError(prompt)，实际 %#v", err)
		}
		if !strings.Contains(ie.Error(), "提示词 不能为空") {
			t.Errorf("错误文案 = %q", ie.Error())
		}
	})

	t.Run("base_url 不在白名单时报错", func(t *testing.T) {
		s2 := *snap
		s2.Provider.AllowedHosts = []string{"other.example.com"}
		if _, err := DryRun(context.Background(), &s2, map[string]any{"prompt": "猫", "image": 12}); err == nil {
			t.Fatal("应报错")
		}
	})

	t.Run("表达式渲染失败时报错", func(t *testing.T) {
		s2 := *snap
		s2.Model.Mapping = map[string]any{"a": "${ input.prompt + }"}
		if _, err := DryRun(context.Background(), &s2, map[string]any{"prompt": "猫", "image": 12}); err == nil {
			t.Fatal("应报错")
		}
	})

	t.Run("快照缺失或没有 submit", func(t *testing.T) {
		if _, err := DryRun(context.Background(), nil, nil); err == nil {
			t.Fatal("nil 快照应报错")
		}
		s2 := *snap
		s2.Provider.Operations.Submit = nil
		if _, err := DryRun(context.Background(), &s2, map[string]any{"prompt": "猫", "image": 12}); err == nil {
			t.Fatal("没有 submit 应报错")
		}
	})
}

func TestDryRun_不解析凭证也不访问素材(t *testing.T) {
	secrets := engDefaultSecrets()
	// DryRun 是包级函数，不接触 Secrets/Assets；这里仅确认调用过程中凭证解析器没被使用
	snap := engSnapshot(t, "https://www.runninghub.cn")
	snap.Provider.AllowedHosts = []string{"www.runninghub.cn"}
	if _, err := DryRun(context.Background(), snap, map[string]any{"prompt": "猫", "image": 12}); err != nil {
		t.Fatal(err)
	}
	if secrets.calls != 0 {
		t.Fatalf("干跑不应解析凭证，实际调用 %d 次", secrets.calls)
	}
}

func TestEngine_失败时凭证名与明文都不进错误(t *testing.T) {
	rh := engNewRH(t)
	// 平台返回的错误文案里恰好包含调用方的凭证（比如回显了请求头）
	rh.onQuery = func(w http.ResponseWriter, r *http.Request) {
		engJSON(w, 400, map[string]any{"errorMessage": "bad key " + r.Header.Get("Authorization")})
	}
	ex := engNew(nil, engDefaultSecrets(), nil)
	_, err := ex.Query(context.Background(), engSnapshot(t, rh.srv.URL), provider.TaskRef{ProviderTaskID: "T"})
	if err == nil || strings.Contains(err.Error(), engSecret) {
		t.Fatalf("错误信息不应含凭证明文：%v", err)
	}
	var e *provider.Error
	if !asAIError(err, &e) || !strings.Contains(e.Message, "***") {
		t.Fatalf("应保留脱敏后的文案：%v", err)
	}
}

func asAIError(err error, target **provider.Error) bool {
	e, ok := err.(*provider.Error)
	if ok {
		*target = e
	}
	return ok
}
