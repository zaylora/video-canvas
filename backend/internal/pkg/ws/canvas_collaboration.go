package ws

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"video-canvas/internal/pkg/logger"
)

const (
	// TypeCanvasCollaboration signals that clients should reconcile against the durable event log.
	TypeCanvasCollaboration = "canvas.collaboration"
	// TypeCanvasProgress signals ephemeral progress for a canvas node.
	TypeCanvasProgress = "canvas.generation_progress"
	// TypeCanvasCommentChanged 提示编辑成员回放评论。
	TypeCanvasCommentChanged  = "canvas.comment_changed"
	canvasCollaborationPrefix = "canvas-collaboration:"
	canvasCollaborationTopic  = "video-canvas:collaboration:notice"
	canvasPresenceTopic       = "video-canvas:collaboration:presence"
)

// CanvasCollaborationChannel scopes subscriptions to one encoded canvas ID.
func CanvasCollaborationChannel(canvasID string) string {
	return canvasCollaborationPrefix + canvasID
}

// CanvasCollaborationID extracts the encoded ID from a room channel.
func CanvasCollaborationID(channel string) (string, bool) {
	return strings.CutPrefix(channel, canvasCollaborationPrefix)
}

// CanvasCollaborationNotice is a wake-up signal. Confirmed content requires an authorized replay request.
type CanvasCollaborationNotice struct {
	CanvasID        string                    `json:"canvasId"`
	EmittedAt       int64                     `json:"emittedAt,omitempty"`
	Epoch           uint64                    `json:"epoch,omitempty"`
	Revision        uint64                    `json:"revision,omitempty"`
	TransferPending bool                      `json:"transferPending,omitempty"`
	CommentChanged  bool                      `json:"commentChanged,omitempty"`
	Progress        *CanvasGenerationProgress `json:"progress,omitempty"`
}

// CanvasGenerationProgress 包含画布节点上可共享的临时进度，不包含任务标识或账户用量。
type CanvasGenerationProgress struct {
	NodeID      string `json:"nodeId"`
	RunRevision uint64 `json:"runRevision"`
	Percent     int    `json:"percent"`
}

// CanvasCollaborationBus distributes room signals across server instances.
type CanvasCollaborationBus struct {
	hub              *Hub
	redis            *redis.Client
	editAccess       Authorizer
	presenceUsername func(context.Context, uint64) string
	acknowledge      func(context.Context, CanvasCollaborationNotice)
	joins            atomic.Uint64
	resumed          atomic.Uint64
}

// CanvasRoomStats 统计当前实例的活跃画布房间和连接发送队列。
type CanvasRoomStats struct {
	Rooms          int
	Connections    int
	MaxQueued      int
	MaxQueuedBytes int
	Joins          uint64
	Resumed        uint64
}

// RoomStats 返回当前实例的实时房间统计。
func (bus *CanvasCollaborationBus) RoomStats() CanvasRoomStats {
	bus.hub.mu.RLock()
	defer bus.hub.mu.RUnlock()
	connections := make(map[*client]struct{})
	var stats CanvasRoomStats
	for channel, members := range bus.hub.channels {
		if _, canvas := CanvasCollaborationID(channel); !canvas {
			continue
		}
		active := false
		for connection := range members {
			select {
			case <-connection.done:
				continue
			default:
			}
			active = true
			connections[connection] = struct{}{}
			count, bytes := connection.send.stats()
			stats.MaxQueued = max(stats.MaxQueued, count)
			stats.MaxQueuedBytes = max(stats.MaxQueuedBytes, bytes)
		}
		if active {
			stats.Rooms++
		}
	}
	stats.Connections = len(connections)
	stats.Joins, stats.Resumed = bus.joins.Load(), bus.resumed.Load()
	return stats
}

// NewCanvasCollaborationBus uses the local hub when Redis is disabled.
func NewCanvasCollaborationBus(hub *Hub, upstream *redis.Client, editors ...Authorizer) *CanvasCollaborationBus {
	bus := &CanvasCollaborationBus{hub: hub, redis: upstream}
	hub.canvasBus = bus
	if len(editors) > 0 {
		bus.editAccess = editors[0]
	}
	return bus
}

// WithNoticeAcknowledgement 设置成功派发的确认回调，应在启动房间之前调用。
func (bus *CanvasCollaborationBus) WithNoticeAcknowledgement(acknowledge func(context.Context, CanvasCollaborationNotice)) *CanvasCollaborationBus {
	bus.acknowledge = acknowledge
	return bus
}

// WithPresenceUsername 注入账号用户名查询，应在启动房间之前调用。
func (bus *CanvasCollaborationBus) WithPresenceUsername(lookup func(context.Context, uint64) string) *CanvasCollaborationBus {
	bus.presenceUsername = lookup
	return bus
}

// Notice publishes a signal after a successful commit. Replay also handles any lost signals.
func (bus *CanvasCollaborationBus) Notice(ctx context.Context, notice CanvasCollaborationNotice) bool {
	if notice.Progress == nil && !notice.CommentChanged {
		notice.EmittedAt = time.Now().UnixMilli()
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 150*time.Millisecond)
	defer cancel()
	if bus.redis != nil {
		encoded, err := json.Marshal(notice)
		if err == nil && bus.redis.Publish(ctx, canvasCollaborationTopic, encoded).Err() == nil {
			bus.acknowledgeNotice(ctx, notice)
			return true
		}
		if notice.Progress == nil && !notice.CommentChanged {
			logger.Warn("协作通知推送失败，将重试已提交版本", zap.String("canvas_id", notice.CanvasID))
		}
		bus.deliver(ctx, notice)
		return false
	}
	bus.deliver(ctx, notice)
	bus.acknowledgeNotice(ctx, notice)
	return true
}

func (bus *CanvasCollaborationBus) acknowledgeNotice(ctx context.Context, notice CanvasCollaborationNotice) {
	if bus.acknowledge != nil && notice.Revision > 0 {
		bus.acknowledge(ctx, notice)
	}
}

// Run consumes cross-instance room signals until the server stops.
func (bus *CanvasCollaborationBus) Run(ctx context.Context) {
	if bus.redis == nil {
		return
	}
	subscription := bus.redis.Subscribe(ctx, canvasCollaborationTopic, canvasPresenceTopic)
	defer subscription.Close()
	stream := subscription.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case message, open := <-stream:
			if !open {
				return
			}
			if message.Channel == canvasPresenceTopic {
				bus.deliverRemotePresence(ctx, message.Payload)
				continue
			}
			var notice CanvasCollaborationNotice
			if len(message.Payload) <= 1024 && json.Unmarshal([]byte(message.Payload), &notice) == nil && notice.CanvasID != "" {
				bus.deliver(ctx, notice)
			}
		}
	}
}

func (bus *CanvasCollaborationBus) deliver(ctx context.Context, notice CanvasCollaborationNotice) {
	channel := CanvasCollaborationChannel(notice.CanvasID)
	if notice.CommentChanged {
		bus.hub.Publish(ctx, channel, Message{Type: TypeCanvasCommentChanged})
		return
	}
	if notice.Progress != nil {
		bus.hub.Publish(ctx, channel, Message{Type: TypeCanvasProgress, Data: notice})
		return
	}
	if notice.Revision == 0 && !notice.TransferPending {
		bus.hub.mu.Lock()
		for connection := range bus.hub.channels[channel] {
			connection.subs[channel].presenceAccessVersion++
			delete(connection.presenceAuth, channel)
		}
		bus.hub.mu.Unlock()
	}
	bus.hub.Publish(ctx, channel, Message{Type: TypeCanvasCollaboration, Data: notice})
	if notice.EmittedAt > 0 {
		logger.Ctx(ctx).Debug("画布协作通知已处理", zap.String("canvas_id", notice.CanvasID),
			zap.Uint64("revision", notice.Revision), zap.Bool("permission_check", notice.Revision == 0 && !notice.TransferPending),
			zap.Int64("dispatch_ms", max(0, time.Now().UnixMilli()-notice.EmittedAt)))
	}
}
