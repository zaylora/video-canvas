package initialize

import (
	"context"
	"time"

	"go.uber.org/zap"

	"video-canvas/internal/pkg/logger"
)

// startStorageHybrid 运行可恢复的存储检查和复制，应用关停后放回未完成任务。
func (a *App) startStorageHybrid(ctx context.Context) func() {
	if a.storageHybrid == nil {
		return func() {}
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := a.storageHybrid.RunPending(ctx); err != nil && ctx.Err() == nil {
					logger.Warn("混合存储后台任务暂时失败", zap.Error(err))
				}
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}
