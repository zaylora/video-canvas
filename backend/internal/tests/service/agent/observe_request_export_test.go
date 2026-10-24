package agent_test

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	. "video-canvas/internal/service/agent"
)

func gzipJSON(t *testing.T, s string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte(s)); err != nil || zw.Close() != nil {
		t.Fatal("压缩失败")
	}
	return buf.Bytes()
}

// requestFacts：两次模型调用，只有第一次留了请求记录（第二次过了保留期或超过上限）。
func requestFacts() *model.AgentExportFacts {
	f := exportFacts()
	f.ModelCalls = []model.AgentModelCall{
		{ID: 11, Purpose: "main", ModelKey: "claude", UpstreamModel: "claude-up", InputTokens: 76936, OutputTokens: 1080, StopReason: "tool_calls", CreatedAt: obsT0},
		{ID: 12, Purpose: "summary", ModelKey: "claude", UpstreamModel: "claude-up", InputTokens: 5000, OutputTokens: 300, StopReason: "stop", CreatedAt: obsT0},
	}
	f.Requests = []model.AgentModelCallBody{
		{CallID: 11, RunID: 5, MessageCount: 23, ToolCount: 14, RawBytes: 48 << 10, Params: rawJSON(`{"model":"claude-up","max_tokens":4096,"stream":true}`), CreatedAt: obsT0},
	}
	return f
}

func TestExportRun_ModelRequestSummary(t *testing.T) {
	e := newObserveEnv(t)
	e.repo.exports[5] = requestFacts()
	out, err := e.svc.ExportRun(t.Context(), ObserveActor{UserID: 9}, 5, ExportBoth)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("JSON：每次模型调用带请求摘要，留了的有摘要，没留的是 null；摘要里没有请求体", func(t *testing.T) {
		raw, _ := json.Marshal(out.Data.ModelCalls)
		var calls []struct {
			ID      string `json:"id"`
			Request *struct {
				Messages int             `json:"messages"`
				Tools    int             `json:"tools"`
				Bytes    int             `json:"bytes"`
				Kept     bool            `json:"kept"`
				Params   json.RawMessage `json:"params"`
				Body     json.RawMessage `json:"body"`
			} `json:"request"`
		}
		if err := json.Unmarshal(raw, &calls); err != nil {
			t.Fatal(err)
		}
		if len(calls) != 2 || calls[0].ID != idcodec.Encode(11) {
			t.Fatalf("calls=%s", raw)
		}
		r := calls[0].Request
		if r == nil || r.Messages != 23 || r.Tools != 14 || r.Bytes != 48<<10 || !r.Kept || !strings.Contains(string(r.Params), `"max_tokens":4096`) {
			t.Errorf("请求摘要不对: %+v", r)
		}
		if len(r.Body) != 0 {
			t.Errorf("导出默认不带请求体: %s", r.Body)
		}
		if calls[1].Request != nil {
			t.Errorf("没留存的调用请求摘要应为 null: %+v", calls[1].Request)
		}
	})

	t.Run("Markdown：列出每次模型调用和请求摘要，没留存的说明原因", func(t *testing.T) {
		for _, want := range []string{"## 模型调用", "消息 23 条", "工具 14 个", "48KB", "max_tokens", "请求体未留存"} {
			if !strings.Contains(out.Content, want) {
				t.Errorf("md 缺少 %q:\n%s", want, out.Content)
			}
		}
	})
}

func TestRunRequests(t *testing.T) {
	e := newObserveEnv(t)
	e.repo.exports[5] = requestFacts()
	e.repo.requests[5] = []model.AgentModelCallBody{
		{CallID: 11, RunID: 5, MessageCount: 2, ToolCount: 1, RawBytes: 60, Params: rawJSON(`{"model":"claude-up"}`), Body: gzipJSON(t, `{"model":"claude-up","messages":[{"role":"system","content":"你是画布助手"}]}`)},
		{CallID: 12, RunID: 5, MessageCount: 40, RawBytes: 9 << 20}, // 超过上限：只有摘要
	}

	t.Run("返回每次调用的完整请求体（解压成 JSON），超限的 body 为空；令牌访问写审计", func(t *testing.T) {
		out, err := e.svc.RunRequests(t.Context(), ObserveActor{UserID: 9, TokenID: 3}, 5)
		if err != nil {
			t.Fatal(err)
		}
		if len(out.Items) != 2 {
			t.Fatalf("items=%+v", out.Items)
		}
		first, second := out.Items[0], out.Items[1]
		if first.ID != idcodec.ID(11) || first.Messages != 2 || !strings.Contains(string(first.Body), "你是画布助手") {
			t.Errorf("第一条: %+v body=%s", first, first.Body)
		}
		if second.ID != idcodec.ID(12) || len(second.Body) != 0 || second.Kept {
			t.Errorf("超限的只有摘要: %+v", second)
		}
		var decoded map[string]any
		if err := json.Unmarshal(first.Body, &decoded); err != nil {
			t.Errorf("body 应是合法 JSON: %v", err)
		}
		if got := e.audit.actions(); len(got) == 0 || got[len(got)-1] != model.AdminAuditAgentRunsRead {
			t.Errorf("令牌访问要写审计: %v", got)
		}
	})

	t.Run("运行不存在返回 60012", func(t *testing.T) {
		if _, err := e.svc.RunRequests(t.Context(), ObserveActor{UserID: 9}, 404); !errors.Is(err, errcode.ErrAgentRunMissing) {
			t.Errorf("err=%v", err)
		}
	})
}

func TestPurgeExpiredRequestBodies(t *testing.T) {
	e := newObserveEnv(t)
	n, err := e.svc.PurgeExpiredRequestBodies(t.Context())
	if err != nil || n != 5 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if want := obsT0.Add(-7 * 24 * time.Hour); !e.repo.bodiesAt.Equal(want) {
		t.Errorf("请求体保留 7 天：cutoff=%v 期望 %v", e.repo.bodiesAt, want)
	}
}
