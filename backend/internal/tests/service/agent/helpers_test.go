package agent_test

import (
	"context"
	"sync"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/ws"
)

// RecordCanvasReads 与真实仓储一样，读取失败不记录、重复记录幂等。
func (f *fakeAgentCanvasRepo) RecordCanvasReads(_ context.Context, rows []model.AgentCanvasRead) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.recordReadErr != nil {
		return f.recordReadErr
	}
	f.addReadRows(rows)
	return nil
}

// addReadRows 在锁内写入详情和自建节点的已读记录。
func (f *fakeAgentCanvasRepo) addReadRows(rows []model.AgentCanvasRead) {
	if f.reads == nil {
		f.reads = map[model.AgentCanvasRead]bool{}
	}
	for _, row := range rows {
		row.CreatedAt = time.Time{}
		f.reads[row] = true
	}
}

func (f *fakeAgentCanvasRepo) ListCanvasReads(_ context.Context, run *model.AgentRun) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.readErr != nil {
		return nil, f.readErr
	}
	var ids []string
	for row := range f.reads {
		if row.UserID == run.UserID && row.CanvasID == run.CanvasID && row.SessionID == run.SessionID && row.RunID == run.ID {
			ids = append(ids, row.NodeID)
		}
	}
	return ids, nil
}

// fakeTaskBroadcaster 记录发布过的频道和消息。
type fakeTaskBroadcaster struct {
	mu       sync.Mutex
	msgs     []ws.Message
	channels []string
}

func (b *fakeTaskBroadcaster) Publish(ctx context.Context, channel string, msg ws.Message) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.channels = append(b.channels, channel)
	b.msgs = append(b.msgs, msg)
}
