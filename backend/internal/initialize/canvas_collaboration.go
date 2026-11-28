package initialize

import (
	"context"
	"time"

	"go.uber.org/zap"

	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/pkg/ws"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

func canvasRoomAuthorizer(check func(context.Context, uint64, uint64) bool) ws.Authorizer {
	return func(ctx context.Context, user uint64, channel string) bool {
		raw, isCanvas := ws.CanvasCollaborationID(channel)
		if !isCanvas {
			return false
		}
		id, valid := idcodec.Decode(raw)
		return valid && idcodec.Encode(id) == raw && check(ctx, user, id)
	}
}

func canvasRoomReaders(repo *repository.CanvasCollaborationRepository) ws.RoomReaders {
	return func(ctx context.Context, users []uint64, channel string) map[uint64]bool {
		raw, isCanvas := ws.CanvasCollaborationID(channel)
		id, valid := idcodec.Decode(raw)
		if !isCanvas || !valid || idcodec.Encode(id) != raw {
			return nil
		}
		return repo.RoomReaders(ctx, users, id)
	}
}

func canvasPresenceUsername(users *service.UserService) func(context.Context, uint64) string {
	return func(ctx context.Context, userID uint64) string {
		user, err := users.State(ctx, userID)
		if err != nil || user == nil {
			return ""
		}
		return user.Username
	}
}

func canvasNoticeAcknowledgement(repo *repository.CanvasCollaborationRepository) func(context.Context, ws.CanvasCollaborationNotice) {
	return func(ctx context.Context, notice ws.CanvasCollaborationNotice) {
		canvas, ok := idcodec.Decode(notice.CanvasID)
		if !ok {
			return
		}
		if err := repo.AcknowledgeNotice(ctx, canvas, notice.Epoch, notice.Revision); err != nil {
			logger.Warn("协作通知确认失败", zap.String("canvas_id", notice.CanvasID), zap.Uint64("revision", notice.Revision))
		}
	}
}

func runCanvasBus(bus *ws.CanvasCollaborationBus, repo *repository.CanvasCollaborationRepository) func() {
	if bus == nil {
		return func() {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	dispatched := make(chan struct{})
	go func() {
		defer close(done)
		bus.Run(ctx)
	}()
	go func() {
		defer close(dispatched)
		if repo != nil {
			runCanvasNoticeDispatch(ctx, bus, repo)
		}
	}()
	return func() {
		cancel()
		<-done
		<-dispatched
	}
}

func runCanvasNoticeDispatch(ctx context.Context, bus *ws.CanvasCollaborationBus, repo *repository.CanvasCollaborationRepository) {
	ticker := time.NewTicker(750 * time.Millisecond)
	defer ticker.Stop()
	statsTicker := time.NewTicker(time.Minute)
	defer statsTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			dispatchCanvasNotices(ctx, bus, repo)
		case <-statsTicker.C:
			stats := bus.RoomStats()
			logger.Info("画布协作房间统计", zap.Int("rooms", stats.Rooms),
				zap.Int("connections", stats.Connections), zap.Int("max_queued_messages", stats.MaxQueued),
				zap.Int("max_queued_bytes", stats.MaxQueuedBytes),
				zap.Uint64("room_joins_total", stats.Joins), zap.Uint64("room_resumes_total", stats.Resumed))
		}
	}
}

func dispatchCanvasNotices(ctx context.Context, bus *ws.CanvasCollaborationBus, repo *repository.CanvasCollaborationRepository) {
	events, err := repo.PendingNotices(ctx, 100)
	if err != nil {
		logger.Warn("读取待派发的协作通知失败", zap.Error(err))
		return
	}
	if len(events) > 0 {
		logger.Debug("重试派发画布协作通知", zap.Int("pending_batch", len(events)),
			zap.Duration("oldest_pending_age", time.Since(events[0].CreatedAt)))
	}
	for _, event := range events {
		if ctx.Err() != nil || !bus.Notice(ctx, ws.CanvasCollaborationNotice{
			CanvasID: idcodec.Encode(event.CanvasID), Epoch: event.Epoch, Revision: event.Revision,
		}) {
			return
		}
	}
}

func (a *App) cleanupCollaborationHistory(ctx context.Context) {
	if a.canvasCollaboration == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	cutoff := time.Now().AddDate(0, 0, -30)
	if err := a.canvasCollaboration.PruneHistory(ctx, cutoff, 200); err != nil {
		logger.Warn("清理过期画布协作历史失败", zap.Error(err))
	}
}
