package agent_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/provider"
	. "video-canvas/internal/service/agent"
)

func notice() provider.TaskSettledNotice {
	return provider.TaskSettledNotice{TaskID: 1234567890123456789, AgentRunID: 987654321098765432, Status: model.TaskSucceeded}
}

// 门铃：POST 到 Agent 的内部接口，带服务令牌；id 用字符串发，避免超过 2^53 的雪花 id 丢精度。
func TestTaskNotifier_PostsNoticeWithServiceToken(t *testing.T) {
	var gotPath, gotToken, gotMethod string
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotToken, gotMethod = r.URL.Path, r.Header.Get("X-Agent-Service-Token"), r.Method
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = w.Write([]byte(`{"code":0,"msg":"success","data":{"accepted":true}}`))
	}))
	defer srv.Close()

	n := &TaskNotifier{BaseURL: srv.URL + "/", ServiceToken: "secret-token"}
	if err := n.TaskSettled(context.Background(), notice()); err != nil {
		t.Fatalf("送达应成功：%v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/internal/v1/watches/task-settled" || gotToken != "secret-token" {
		t.Fatalf("请求不对：%s %s token=%q", gotMethod, gotPath, gotToken)
	}
	want := map[string]string{"task_id": "1234567890123456789", "run_id": "987654321098765432", "status": "succeeded"}
	for k, v := range want {
		if gotBody[k] != v {
			t.Fatalf("body[%s]=%q，期望 %q（%v）", k, gotBody[k], v, gotBody)
		}
	}
}

// Agent 说没有对应的等待者（404：已取消等待、不用等的任务）是正常的，不能让队列一直重试。
func TestTaskNotifier_NotFoundIsDelivered(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":60012,"msg":"没有对应的等待"}`))
	}))
	defer srv.Close()
	n := &TaskNotifier{BaseURL: srv.URL, ServiceToken: "x"}
	if err := n.TaskSettled(context.Background(), notice()); err != nil {
		t.Fatalf("404 应视为已处理：%v", err)
	}
}

// 连不上、Agent 报错都要返回错误，让队列退避重试。
func TestTaskNotifier_FailuresAreRetriable(t *testing.T) {
	for name, status := range map[string]int{"服务端出错": 500, "令牌不对": 401, "网关超时": 504} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
			defer srv.Close()
			n := &TaskNotifier{BaseURL: srv.URL, ServiceToken: "x"}
			if err := n.TaskSettled(context.Background(), notice()); err == nil {
				t.Fatalf("HTTP %d 应返回错误", status)
			}
		})
	}
	t.Run("连不上", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		url := srv.URL
		srv.Close()
		n := &TaskNotifier{BaseURL: url, ServiceToken: "x"}
		if err := n.TaskSettled(context.Background(), notice()); err == nil {
			t.Fatal("连不上应返回错误")
		}
	})
}
