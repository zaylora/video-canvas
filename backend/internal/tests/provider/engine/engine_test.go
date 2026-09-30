//go:build legacy

package engine_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
	. "video-canvas/internal/provider/engine"

	"video-canvas/internal/provider"
	"video-canvas/internal/provider/dsl"
)

func TestEngine_完整流程(t *testing.T) {
	rh := engNewRH(t)
	snap := engSnapshot(t, rh.srv.URL)
	assets := &engAssets{content: []byte("PNGDATA"), url: "https://signed.example/asset/12"}
	ex := engNew(assets, engDefaultSecrets(), func(o *Options) {
		o.Now = func() time.Time { return time.Date(2026, 9, 29, 8, 0, 0, 0, time.UTC) }
	})
	ctx := context.Background()

	// 1. 提交：先上传首帧，再提交
	id, err := ex.Submit(ctx, snap, engSubmitInput())
	if err != nil {
		t.Fatalf("提交失败：%v", err)
	}
	if id != "T-1001" {
		t.Fatalf("平台任务 id = %q", id)
	}

	// 上传请求：鉴权头、文件名、MIME、内容
	if rh.uploadAuth != "Bearer "+engSecret {
		t.Errorf("上传鉴权头 = %q", rh.uploadAuth)
	}
	if rh.uploadName != "first-frame.png" || rh.uploadMime != "image/png" || string(rh.uploadBody) != "PNGDATA" {
		t.Errorf("上传内容不符：%q %q %q", rh.uploadName, rh.uploadMime, rh.uploadBody)
	}
	if !reflect.DeepEqual(assets.opened, []uint64{12}) {
		t.Errorf("应只读取素材 12，实际 %v", assets.opened)
	}

	// 提交请求：路径、鉴权、渲染后的 mapping
	if rh.submitAuth != "Bearer "+engSecret {
		t.Errorf("提交鉴权头 = %q", rh.submitAuth)
	}
	if rh.submitURI != "/openapi/v2/run/ai-app/2093984571330498561" {
		t.Errorf("提交路径 = %q", rh.submitURI)
	}
	wantNodes := []any{
		map[string]any{"nodeId": "6", "fieldName": "text", "fieldValue": "一只在奔跑的猫"},
		map[string]any{"nodeId": "12", "fieldName": "image", "fieldValue": "uploaded/abc.png"},
		map[string]any{"nodeId": "30", "fieldName": "length", "fieldValue": 161.0},
	}
	if !reflect.DeepEqual(rh.submitBody["nodeInfoList"], wantNodes) {
		t.Errorf("nodeInfoList = %#v", rh.submitBody["nodeInfoList"])
	}
	if rh.submitBody["instanceType"] != "default" || rh.submitBody["webhookUrl"] != "https://app.example/hook/abc" {
		t.Errorf("submit body = %#v", rh.submitBody)
	}
	if _, leaked := rh.submitBody["apiKey"]; leaked {
		t.Error("bearer 鉴权不应往 body 里塞凭证")
	}

	// 2. 查询：平台 QUEUED → 映射 queued；SUCCESS → 筛选出 mp4
	task := provider.TaskRef{ID: 55, UserID: 7, ProviderTaskID: id}
	rh.onQuery = func(w http.ResponseWriter, r *http.Request) {
		engJSON(w, 200, map[string]any{"status": "QUEUED"})
	}
	res, err := ex.Query(ctx, snap, task)
	if err != nil || res.Status != provider.StatusQueued || len(res.Outputs) != 0 {
		t.Fatalf("queued 查询结果不符：%+v %v", res, err)
	}
	if rh.queryBody["taskId"] != "T-1001" {
		t.Errorf("查询体 = %#v", rh.queryBody)
	}
	rh.onQuery = nil
	res, err = ex.Query(ctx, snap, task)
	if err != nil {
		t.Fatalf("查询失败：%v", err)
	}
	wantOut := []provider.Output{{URL: rh.srv.URL + "/files/out.mp4", Type: "mp4", Node: "9"}}
	if res.Status != provider.StatusSucceeded || !reflect.DeepEqual(res.Outputs, wantOut) {
		t.Fatalf("succeeded 查询结果不符：%+v", res)
	}
	if res.ProviderCost == nil || *res.ProviderCost != 0.25 {
		t.Errorf("provider_cost = %v", res.ProviderCost)
	}

	// 3. 下载
	rh.setExtra("/files/out.mp4", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("下载不应携带平台凭证")
		}
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write([]byte("MP4DATA"))
	})
	dl, err := ex.Download(ctx, snap, res.Outputs[0].URL)
	if err != nil {
		t.Fatalf("下载失败：%v", err)
	}
	defer dl.Body.Close()
	data, _ := io.ReadAll(dl.Body)
	if string(data) != "MP4DATA" || dl.ContentType != "video/mp4" || dl.FileName != "out.mp4" || dl.Size != 7 {
		t.Fatalf("下载结果不符：%q %+v", data, dl)
	}
}

func TestEngine_没有upload操作时使用签名URL(t *testing.T) {
	rh := engNewRH(t)
	snap := engSnapshot(t, rh.srv.URL)
	snap.Provider.Operations.Upload = nil
	assets := &engAssets{content: []byte("x"), url: "https://signed.example/asset/12?sig=abc"}
	ex := engNew(assets, engDefaultSecrets(), nil)

	if _, err := ex.Submit(context.Background(), snap, engSubmitInput()); err != nil {
		t.Fatalf("提交失败：%v", err)
	}
	nodes := rh.submitBody["nodeInfoList"].([]any)
	if got := nodes[1].(map[string]any)["fieldValue"]; got != "https://signed.example/asset/12?sig=abc" {
		t.Fatalf("图片字段应为签名 URL，实际 %v", got)
	}
	if rh.uploadName != "" {
		t.Fatal("没有 upload 操作时不应上传")
	}
}

func TestEngine_素材不属于任务用户(t *testing.T) {
	rh := engNewRH(t)
	ex := engNew(&engAssets{}, engDefaultSecrets(), nil)
	in := engSubmitInput()
	in.Task.UserID = 8 // engAssets 只认 7
	_, err := ex.Submit(context.Background(), engSnapshot(t, rh.srv.URL), in)
	if engClass(t, err) != provider.ClassTerminal {
		t.Fatalf("应为 terminal：%v", err)
	}
}

func TestEngine_输入不合法(t *testing.T) {
	rh := engNewRH(t)
	ex := engNew(&engAssets{}, engDefaultSecrets(), nil)
	in := engSubmitInput()
	in.Input = map[string]any{"prompt": "", "image": uint64(12)}
	_, err := ex.Submit(context.Background(), engSnapshot(t, rh.srv.URL), in)
	if engClass(t, err) != provider.ClassTerminal || !strings.Contains(err.Error(), "提示词") {
		t.Fatalf("应为 terminal 且提示字段：%v", err)
	}
}

func TestEngine_鉴权方式(t *testing.T) {
	tests := []struct {
		name  string
		auth  dsl.AuthConfig
		check func(t *testing.T, rh *engRH, r *http.Request)
	}{
		{"header", dsl.AuthConfig{Type: dsl.AuthHeader, Secret: "runninghub_api_key", Name: "X-Api-Key"},
			func(t *testing.T, rh *engRH, r *http.Request) {
				if got := r.Header.Get("X-Api-Key"); got != engSecret {
					t.Errorf("X-Api-Key = %q", got)
				}
				if r.Header.Get("Authorization") != "" {
					t.Error("不应有 Authorization")
				}
			}},
		{"query", dsl.AuthConfig{Type: dsl.AuthQuery, Secret: "runninghub_api_key", Name: "api_key"},
			func(t *testing.T, rh *engRH, r *http.Request) {
				if got := r.URL.Query().Get("api_key"); got != engSecret {
					t.Errorf("api_key = %q", got)
				}
			}},
		{"body_field", dsl.AuthConfig{Type: dsl.AuthBodyField, Secret: "runninghub_api_key", Name: "apiKey"},
			func(t *testing.T, rh *engRH, r *http.Request) {
				if got := rh.submitBody["apiKey"]; got != engSecret {
					t.Errorf("body.apiKey = %v", got)
				}
				if _, ok := rh.submitBody["nodeInfoList"]; !ok {
					t.Error("body_field 不应覆盖原有字段")
				}
			}},
		{"none", dsl.AuthConfig{Type: dsl.AuthNone},
			func(t *testing.T, rh *engRH, r *http.Request) {
				if r.Header.Get("Authorization") != "" || strings.Contains(r.URL.RawQuery, "key") {
					t.Error("none 不应带任何凭证")
				}
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rh := engNewRH(t)
			snap := engSnapshot(t, rh.srv.URL)
			snap.Provider.Auth = tt.auth
			snap.Provider.Operations.Upload = nil
			var seen *http.Request
			rh.onSubmit = func(w http.ResponseWriter, r *http.Request) {
				seen = r.Clone(context.Background())
				engJSON(w, 200, map[string]any{"taskId": "T-9"})
			}
			ex := engNew(&engAssets{url: "https://s.example/a"}, engDefaultSecrets(), nil)
			if _, err := ex.Submit(context.Background(), snap, engSubmitInput()); err != nil {
				t.Fatalf("提交失败：%v", err)
			}
			tt.check(t, rh, seen)
		})
	}
}

func TestEngine_Submit错误分类(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      any
		wantClass provider.ErrorClass
		wantInMsg string
	}{
		{"429 可重试", 429, map[string]any{"errorMessage": "too many"}, provider.ClassRetryable, "HTTP 429"},
		{"500 可重试", 500, "oops", provider.ClassRetryable, "HTTP 500"},
		{"503 可重试", 503, nil, provider.ClassRetryable, "HTTP 503"},
		{"平台余额不足", 200, map[string]any{"errorCode": "1001", "errorMessage": "Insufficient balance"}, provider.ClassProviderBalance, "Insufficient balance"},
		{"中文余额不足", 400, map[string]any{"errorMessage": "账户余额不足"}, provider.ClassProviderBalance, "余额"},
		{"内容审核", 200, map[string]any{"errorMessage": "包含违规内容"}, provider.ClassModeration, "违规"},
		{"英文审核", 400, map[string]any{"errorMessage": "NSFW detected"}, provider.ClassModeration, "NSFW"},
		{"其他 4xx 为 terminal", 400, map[string]any{"errorMessage": "bad param"}, provider.ClassTerminal, "bad param"},
		{"200 但没有 taskId 为 terminal", 200, map[string]any{"status": "FAILED"}, provider.ClassTerminal, ""},
		{"节点被作者改掉（node_errors）", 200, map[string]any{"taskId": "T1", "promptTips": `{"node_errors":{"6":{"message":"节点缺失"}}}`}, provider.ClassTerminal, "节点缺失"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rh := engNewRH(t)
			rh.onSubmit = func(w http.ResponseWriter, r *http.Request) {
				if s, ok := tt.body.(string); ok {
					w.WriteHeader(tt.status)
					_, _ = w.Write([]byte(s))
					return
				}
				if tt.body == nil {
					w.WriteHeader(tt.status)
					return
				}
				engJSON(w, tt.status, tt.body)
			}
			snap := engSnapshot(t, rh.srv.URL)
			snap.Provider.Operations.Upload = nil
			ex := engNew(&engAssets{url: "https://s.example/a"}, engDefaultSecrets(), nil)
			_, err := ex.Submit(context.Background(), snap, engSubmitInput())
			if got := engClass(t, err); got != tt.wantClass {
				t.Fatalf("分类 = %s，期望 %s（%v）", got, tt.wantClass, err)
			}
			if tt.wantInMsg != "" && !strings.Contains(err.Error(), tt.wantInMsg) {
				t.Fatalf("错误信息 %q 应包含 %q", err.Error(), tt.wantInMsg)
			}
		})
	}
}

func TestEngine_Submit读超时为submit_unknown(t *testing.T) {
	rh := engNewRH(t)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	rh.onSubmit = func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}
	snap := engSnapshot(t, rh.srv.URL)
	snap.Provider.Operations.Upload = nil
	snap.Provider.Operations.Submit.Timeout = dsl.Duration(200 * time.Millisecond)
	ex := engNew(&engAssets{url: "https://s.example/a"}, engDefaultSecrets(), nil)

	start := time.Now()
	_, err := ex.Submit(context.Background(), snap, engSubmitInput())
	if got := engClass(t, err); got != provider.ClassSubmitUnknown {
		t.Fatalf("分类 = %s，期望 submit_unknown（%v）", got, err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("超时没有生效")
	}
}

func TestEngine_Submit响应头之后连接中断为submit_unknown(t *testing.T) {
	rh := engNewRH(t)
	rh.onSubmit = func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Error("不支持 Hijack")
			return
		}
		conn, _, _ := hj.Hijack()
		_ = conn.Close() // 请求已收到，但没有任何响应
	}
	snap := engSnapshot(t, rh.srv.URL)
	snap.Provider.Operations.Upload = nil
	ex := engNew(&engAssets{url: "https://s.example/a"}, engDefaultSecrets(), nil)
	_, err := ex.Submit(context.Background(), snap, engSubmitInput())
	if got := engClass(t, err); got != provider.ClassSubmitUnknown {
		t.Fatalf("分类 = %s，期望 submit_unknown（%v）", got, err)
	}
}

func TestEngine_连接失败为retryable(t *testing.T) {
	rh := engNewRH(t)
	snap := engSnapshot(t, rh.srv.URL)
	snap.Provider.Operations.Upload = nil
	rh.srv.Close() // 端口不再监听
	ex := engNew(&engAssets{url: "https://s.example/a"}, engDefaultSecrets(), nil)

	_, err := ex.Submit(context.Background(), snap, engSubmitInput())
	if got := engClass(t, err); got != provider.ClassRetryable {
		t.Fatalf("提交连接失败应为 retryable，实际 %s（%v）", got, err)
	}
	_, err = ex.Query(context.Background(), snap, provider.TaskRef{ProviderTaskID: "T"})
	if got := engClass(t, err); got != provider.ClassRetryable {
		t.Fatalf("查询连接失败应为 retryable，实际 %s（%v）", got, err)
	}
}

func TestEngine_Query读超时为retryable(t *testing.T) {
	rh := engNewRH(t)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	rh.onQuery = func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}
	snap := engSnapshot(t, rh.srv.URL)
	snap.Provider.Operations.Query.Timeout = dsl.Duration(200 * time.Millisecond)
	ex := engNew(nil, engDefaultSecrets(), nil)
	_, err := ex.Query(context.Background(), snap, provider.TaskRef{ProviderTaskID: "T"})
	if got := engClass(t, err); got != provider.ClassRetryable {
		t.Fatalf("查询是幂等的，超时应为 retryable，实际 %s", got)
	}
}

func TestEngine_Query状态映射(t *testing.T) {
	tests := []struct {
		name       string
		resp       map[string]any
		wantStatus string
		wantClass  provider.ErrorClass
		wantMsg    string
		wantOutN   int
	}{
		{"运行中", map[string]any{"status": "RUNNING"}, provider.StatusRunning, "", "", 0},
		{"未知状态走 _default（running）", map[string]any{"status": "AUDITING"}, provider.StatusRunning, "", "", 0},
		{"缺少状态走 _default", map[string]any{}, provider.StatusRunning, "", "", 0},
		{"失败：审核", map[string]any{"status": "FAILED", "errorCode": "805", "errorMessage": "内容违规"}, provider.StatusFailed, provider.ClassModeration, "内容违规", 0},
		{"失败：余额", map[string]any{"status": "FAILED", "errorMessage": "insufficient balance"}, provider.StatusFailed, provider.ClassProviderBalance, "insufficient balance", 0},
		{"失败：其他为 terminal", map[string]any{"status": "FAILED", "errorMessage": "oom"}, provider.StatusFailed, provider.ClassTerminal, "oom", 0},
		{"失败没有文案", map[string]any{"status": "FAILED"}, provider.StatusFailed, provider.ClassTerminal, "平台任务失败", 0},
		{"成功但没有匹配的产物按 terminal 失败", map[string]any{"status": "SUCCESS", "results": []any{
			map[string]any{"url": "https://x/a.png", "outputType": "png"}}}, provider.StatusFailed, provider.ClassTerminal, "没有匹配", 0},
		{"成功但没有任何产物", map[string]any{"status": "SUCCESS"}, provider.StatusFailed, provider.ClassTerminal, "没有匹配", 0},
		{"成功：多个视频产物", map[string]any{"status": "SUCCESS", "results": []any{
			map[string]any{"url": "https://x/a.mp4", "outputType": "mp4"},
			map[string]any{"url": "https://x/b.webm", "outputType": "webm"},
			map[string]any{"url": "https://x/c.gif", "outputType": "gif"}}}, provider.StatusSucceeded, "", "", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rh := engNewRH(t)
			rh.onQuery = func(w http.ResponseWriter, r *http.Request) { engJSON(w, 200, tt.resp) }
			snap := engSnapshot(t, rh.srv.URL)
			ex := engNew(nil, engDefaultSecrets(), nil)
			res, err := ex.Query(context.Background(), snap, provider.TaskRef{ProviderTaskID: "T"})
			if err != nil {
				t.Fatalf("查询失败：%v", err)
			}
			if res.Status != tt.wantStatus || res.ErrorClass != tt.wantClass || len(res.Outputs) != tt.wantOutN {
				t.Fatalf("结果不符：%+v", res)
			}
			if tt.wantMsg != "" && !strings.Contains(res.ErrorMessage, tt.wantMsg) {
				t.Fatalf("ErrorMessage = %q，应包含 %q", res.ErrorMessage, tt.wantMsg)
			}
		})
	}
}

func TestEngine_Query进度与失败码(t *testing.T) {
	rh := engNewRH(t)
	rh.onQuery = func(w http.ResponseWriter, r *http.Request) {
		engJSON(w, 200, map[string]any{"status": "RUNNING", "pct": "42.9", "errorCode": 12})
	}
	snap := engSnapshot(t, rh.srv.URL)
	snap.Provider.Operations.Query.Extract["progress"] = "resp.pct"
	ex := engNew(nil, engDefaultSecrets(), nil)
	res, err := ex.Query(context.Background(), snap, provider.TaskRef{ProviderTaskID: "T"})
	if err != nil || res.Progress == nil || *res.Progress != 42 || res.ErrorCode != "12" {
		t.Fatalf("结果不符：%+v %v", res, err)
	}
	// 越界进度被限制在 0–100
	rh.onQuery = func(w http.ResponseWriter, r *http.Request) {
		engJSON(w, 200, map[string]any{"status": "RUNNING", "pct": 250})
	}
	res, _ = ex.Query(context.Background(), snap, provider.TaskRef{ProviderTaskID: "T"})
	if res.Progress == nil || *res.Progress != 100 {
		t.Fatalf("进度应被限制为 100：%v", res.Progress)
	}
}

func TestEngine_Query需要平台任务id(t *testing.T) {
	rh := engNewRH(t)
	ex := engNew(nil, engDefaultSecrets(), nil)
	_, err := ex.Query(context.Background(), engSnapshot(t, rh.srv.URL), provider.TaskRef{ID: 1})
	if engClass(t, err) != provider.ClassTerminal {
		t.Fatal("应为 terminal")
	}
}

func TestEngine_Query的HTTP错误由规则分类(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   provider.ErrorClass
	}{
		{"429 retryable", 429, provider.ClassRetryable},
		{"502 retryable", 502, provider.ClassRetryable},
		{"404 terminal", 404, provider.ClassTerminal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rh := engNewRH(t)
			rh.onQuery = func(w http.ResponseWriter, r *http.Request) {
				engJSON(w, tt.status, map[string]any{"errorMessage": "x"})
			}
			ex := engNew(nil, engDefaultSecrets(), nil)
			_, err := ex.Query(context.Background(), engSnapshot(t, rh.srv.URL), provider.TaskRef{ProviderTaskID: "T"})
			if got := engClass(t, err); got != tt.want {
				t.Fatalf("分类 = %s，期望 %s", got, tt.want)
			}
		})
	}
}

func TestEngine_没有匹配规则时的默认分类(t *testing.T) {
	rh := engNewRH(t)
	snap := engSnapshot(t, rh.srv.URL)
	snap.Provider.ErrorRules = nil
	snap.Provider.Operations.Upload = nil
	ex := engNew(&engAssets{url: "https://s.example/a"}, engDefaultSecrets(), nil)
	for status, want := range map[int]provider.ErrorClass{429: provider.ClassRetryable, 500: provider.ClassRetryable, 400: provider.ClassTerminal} {
		status := status
		rh.onSubmit = func(w http.ResponseWriter, r *http.Request) { engJSON(w, status, map[string]any{}) }
		_, err := ex.Submit(context.Background(), snap, engSubmitInput())
		if got := engClass(t, err); got != want {
			t.Errorf("HTTP %d 默认分类 = %s，期望 %s", status, got, want)
		}
	}
}

func TestEngine_错误规则写错时按不匹配处理(t *testing.T) {
	rh := engNewRH(t)
	rh.onSubmit = func(w http.ResponseWriter, r *http.Request) { engJSON(w, 400, map[string]any{}) }
	snap := engSnapshot(t, rh.srv.URL)
	snap.Provider.Operations.Upload = nil
	snap.Provider.ErrorRules = []dsl.ErrorRule{{When: "resp.a.b.c.d(", Class: "moderation"}, {When: "true", Class: "terminal"}}
	ex := engNew(&engAssets{url: "https://s.example/a"}, engDefaultSecrets(), nil)
	_, err := ex.Submit(context.Background(), snap, engSubmitInput())
	if engClass(t, err) != provider.ClassTerminal {
		t.Fatalf("应跳过坏规则落到 terminal：%v", err)
	}
}

func TestEngine_Cancel(t *testing.T) {
	rh := engNewRH(t)
	ex := engNew(nil, engDefaultSecrets(), nil)
	task := provider.TaskRef{ID: 1, ProviderTaskID: "T-1"}

	t.Run("平台不支持取消", func(t *testing.T) {
		err := ex.Cancel(context.Background(), engSnapshot(t, rh.srv.URL), task)
		if !errors.Is(err, provider.ErrCancelUnsupported) {
			t.Fatalf("期望 ErrCancelUnsupported，实际 %v", err)
		}
		if rh.cancelCalled {
			t.Fatal("不支持取消时不应发请求")
		}
	})

	t.Run("平台支持取消", func(t *testing.T) {
		snap := engSnapshot(t, rh.srv.URL)
		snap.Provider.Operations.Cancel = &dsl.Operation{
			Method: "POST", Path: "/openapi/v2/cancel", Encoding: dsl.EncodingConfig{Type: dsl.EncodingJSON},
			Body: map[string]any{"taskId": "${ task.provider_task_id }"}, Success: "status == 200 && resp.code == 0",
		}
		if err := ex.Cancel(context.Background(), snap, task); err != nil || !rh.cancelCalled {
			t.Fatalf("取消失败：%v called=%v", err, rh.cancelCalled)
		}
	})

	t.Run("取消失败按规则分类", func(t *testing.T) {
		snap := engSnapshot(t, rh.srv.URL)
		snap.Provider.Operations.Cancel = &dsl.Operation{Method: "POST", Path: "/openapi/v2/cancel", Encoding: dsl.EncodingConfig{Type: dsl.EncodingJSON}}
		rh.onCancel = func(w http.ResponseWriter, r *http.Request) { engJSON(w, 503, map[string]any{}) }
		err := ex.Cancel(context.Background(), snap, task)
		if engClass(t, err) != provider.ClassRetryable {
			t.Fatalf("应为 retryable：%v", err)
		}
	})
}

func TestEngine_凭证读取失败为terminal(t *testing.T) {
	rh := engNewRH(t)
	snap := engSnapshot(t, rh.srv.URL)
	snap.Provider.Operations.Upload = nil
	ex := engNew(&engAssets{url: "https://s.example/a"}, &engSecrets{m: map[string]string{}}, nil)
	_, err := ex.Submit(context.Background(), snap, engSubmitInput())
	if engClass(t, err) != provider.ClassTerminal || !strings.Contains(err.Error(), "runninghub_api_key") {
		t.Fatalf("应为 terminal 并提示凭证名：%v", err)
	}
}

func TestEngine_path参数会被转义(t *testing.T) {
	tests := []struct {
		name    string
		webapp  string
		wantURI string // 空表示期望被拒绝
	}{
		{"斜杠与问号被转义成单个路径段", "a/b?x=1", "/openapi/v2/run/ai-app/a%2Fb%3Fx=1"},
		{"路径穿越被拒绝", "..", ""},
		{"正常值", "2093984571330498561", "/openapi/v2/run/ai-app/2093984571330498561"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rh := engNewRH(t)
			snap := engSnapshot(t, rh.srv.URL)
			snap.Provider.Operations.Upload = nil
			snap.Model.Params = map[string]any{"webappId": tt.webapp}
			ex := engNew(&engAssets{url: "https://s.example/a"}, engDefaultSecrets(), nil)
			_, err := ex.Submit(context.Background(), snap, engSubmitInput())
			if tt.wantURI == "" {
				if engClass(t, err) != provider.ClassTerminal {
					t.Fatalf("应被拒绝：%v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("提交失败：%v", err)
			}
			if rh.submitURI != tt.wantURI {
				t.Fatalf("请求路径 = %q，期望 %q", rh.submitURI, tt.wantURI)
			}
		})
	}
}

func TestEngine_大整数任务id不丢精度(t *testing.T) {
	rh := engNewRH(t)
	rh.onSubmit = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"taskId": 2041791506008539138}`))
	}
	snap := engSnapshot(t, rh.srv.URL)
	snap.Provider.Operations.Upload = nil
	ex := engNew(&engAssets{url: "https://s.example/a"}, engDefaultSecrets(), nil)
	id, err := ex.Submit(context.Background(), snap, engSubmitInput())
	if err != nil || id != "2041791506008539138" {
		t.Fatalf("id=%q err=%v", id, err)
	}
}

func TestEngine_快照缺失(t *testing.T) {
	ex := engNew(nil, engDefaultSecrets(), nil)
	ctx := context.Background()
	if _, err := ex.Submit(ctx, nil, provider.SubmitInput{}); engClass(t, err) != provider.ClassTerminal {
		t.Fatal("Submit(nil) 应为 terminal")
	}
	if _, err := ex.Query(ctx, nil, provider.TaskRef{}); engClass(t, err) != provider.ClassTerminal {
		t.Fatal("Query(nil) 应为 terminal")
	}
	if err := ex.Cancel(ctx, nil, provider.TaskRef{}); engClass(t, err) != provider.ClassTerminal {
		t.Fatal("Cancel(nil) 应为 terminal")
	}
	if _, err := ex.Download(ctx, nil, "http://x"); engClass(t, err) != provider.ClassTerminal {
		t.Fatal("Download(nil) 应为 terminal")
	}
}
