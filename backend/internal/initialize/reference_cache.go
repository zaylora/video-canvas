package initialize

import (
	"context"
	"time"

	"go.uber.org/zap"

	"video-canvas/internal/pkg/logger"
)

// startReferenceCache 定期对账和清理，关闭数据库前取消并等待任务结束。
func (a *App) startReferenceCache(ctx context.Context) func() {
	if a.refCache == nil {
		return func() {}
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.runReferenceCache(ctx)
	}()
	return func() {
		cancel()
		<-done
	}
}

func (a *App) runReferenceCache(ctx context.Context) {
	timer := time.NewTimer(time.Minute)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			a.maintainReferenceCache(ctx)
			timer.Reset(time.Hour)
		}
	}
}

func (a *App) maintainReferenceCache(ctx context.Context) {
	round, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	stats, err := a.refCache.Maintain(round)
	if err != nil {
		if ctx.Err() == nil {
			logger.Warn("R2 临时参考素材对账或清理失败", zap.Error(err))
		}
		return
	}
	logger.Info("R2 临时参考素材对账完成",
		zap.Int64("hits", stats.Hits), zap.Int64("uploaded_bytes", stats.UploadedBytes),
		zap.Int64("failures", stats.Failures), zap.Int64("removed", stats.Removed),
		zap.Int64("managed_bytes", stats.ManagedBytes))
}
