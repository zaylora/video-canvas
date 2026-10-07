package agent_test

import (
	"context"
	"sync"

	"video-canvas/internal/pkg/ws"
)

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
