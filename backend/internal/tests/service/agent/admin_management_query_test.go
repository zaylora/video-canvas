package agent_test

import (
	"context"
	"encoding/json"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	. "video-canvas/internal/service/agent"
)

type managementQueue struct {
	minutes int
	err     error
}

func (q *managementQueue) Overview(context.Context) (*model.QueueOverview, error) {
	return &model.QueueOverview{Queue: model.QueueCard{Paused: true, Concurrency: 12, Counts: model.QueueCounts{Waiting: 7}}}, q.err
}
func (q *managementQueue) Throughput(_ context.Context, n int) ([]model.QueueThroughputPoint, error) {
	q.minutes = n
	return []model.QueueThroughputPoint{}, q.err
}

type managementRuns struct {
	filter RunListFilter
	actor  ObserveActor
}

func (r *managementRuns) ListRuns(_ context.Context, actor ObserveActor, f *RunListFilter) (*RunListPage, error) {
	r.actor = actor
	r.filter = *f
	return &RunListPage{Items: []*AgentRunDigestView{}}, nil
}

func TestAdminManagementQueueAndRuns(t *testing.T) {
	e := newManagementEnv()
	queue, runs := &managementQueue{}, &managementRuns{}
	e.d.Queue, e.d.Runs = queue, runs
	call := func(args string) *ToolResult {
		t.Helper()
		out, err := e.b.ExecutePlatformTool(context.Background(), e.rc, "query", "admin_query", json.RawMessage(args))
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	out := call(`{"resource":"queue"}`)
	var view model.QueueOverview
	if out.IsError || json.Unmarshal([]byte(out.Content), &view) != nil || view.Queue.Concurrency != 12 || view.Queue.Counts.Waiting != 7 || !view.Queue.Paused {
		t.Fatalf("队列状态：%+v", out)
	}
	out = call(`{"resource":"queue_throughput","minutes":30}`)
	if out.IsError || queue.minutes != 30 {
		t.Fatalf("吞吐参数：%+v %+v", out, queue)
	}
	out = call(`{"resource":"runs","scene":"admin","status":"failed","user_id":42,"model":"test","feedback":"down","from":"2026-10-01T00:00:00+08:00","to":"2026-11-01T00:00:00+08:00","page":2,"page_size":5}`)
	f := runs.filter
	if out.IsError || runs.actor.UserID != e.rc.UserID || f.UserID != 42 || f.Scene != "admin" || f.ModelKey != "test" || f.Page != 2 || f.Limit != 5 || len(f.Statuses) != 1 || f.Statuses[0] != "failed" || f.Since.IsZero() || f.Until.IsZero() {
		t.Fatalf("运行筛选：%+v %+v", out, f)
	}
	for _, args := range []string{`{"resource":"runs","from":"bad"}`, `{"resource":"runs","to":"bad"}`, `{"resource":"runs","from":"2026-11-02T00:00:00Z","to":"2026-11-01T00:00:00Z"}`, `{"resource":"users","role":"root"}`, `{"resource":"usage","status":"unknown"}`, `{"resource":"image_processors","status":"unknown"}`} {
		if out := call(args); !out.IsError {
			t.Fatalf("接受非法筛选：%s %+v", args, out)
		}
	}
	queue.err = errcode.ErrForbidden
	if out := call(`{"resource":"queue"}`); !out.IsError {
		t.Fatal("丢失业务错误")
	}
}
