package initialize

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"video-canvas/internal/config"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/queue"
	"video-canvas/internal/provider/worker"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
	agentsvc "video-canvas/internal/service/agent"
	"video-canvas/pkg/version"
)

// riverUIPrefix 是嵌入的 River UI 的挂载路径：它要和路由里 /admin/river 的位置一致。
const riverUIPrefix = "/api/v1/admin/river"

// newTaskQueue 创建任务队列运行时。ai.worker.enabled=false 时不创建 worker（本实例不执行任务，只入队）。
// 队列用的数据库连接池与 GORM 共用（入队因此能进业务事务），另开一个很小的 pgx 连接池只用来 LISTEN 新任务通知，
// 拿不到时（例如经过不支持 LISTEN 的连接池）退化成轮询，只是取任务慢一点。
func newTaskQueue(cfg *config.Config, db *gorm.DB, taskSvc *service.GenerationTaskService, executor provider.Executor,
	assetSvc *service.AssetService, settingsSvc *service.SettingsService, dramaSvc queue.DramaHandler) (*queue.Runtime, *pgxpool.Pool, error) {
	sqlDB, err := db.DB()
	if err != nil {
		return nil, nil, fmt.Errorf("取数据库连接池失败: %w", err)
	}
	var handler queue.Handler
	if cfg.AI.Worker.Enabled {
		handler = worker.New(taskSvc, executor, provider.AssetSaver(assetSvc), worker.Options{MaxDownloadBytes: cfg.Storage.MaxResult})
	}
	// Agent 启用时：任务终态的事务里入队“通知 Agent”的 job（API 实例上用户取消任务也要入队）；执行这些 job 的只有 worker 实例
	var notifier provider.AgentNotifier
	if cfg.Agent.Enabled && cfg.AI.Worker.Enabled {
		notifier = &agentsvc.TaskNotifier{BaseURL: cfg.Agent.URL, ServiceToken: cfg.Agent.ServiceToken}
	}
	listener := newQueueListener(cfg.Database.DSN)
	host, _ := os.Hostname()
	rt, err := queue.New(queue.Config{
		DB: sqlDB, ListenerPool: listener, Store: taskSvc, Handler: handler, Settings: settingsSvc,
		Repo: repository.NewQueueRepository(db), Enabled: cfg.AI.Worker.Enabled,
		Drama:    dramaSvc,
		HardCap:  cfg.AI.Queue.MaxConcurrency,
		Notifier: notifier, AgentNotify: cfg.Agent.Enabled,
		Version: version.Version, Hostname: host, StopTimeout: cfg.Server.ShutdownTimeout,
	})
	if err != nil {
		if listener != nil {
			listener.Close()
		}
		return nil, nil, err
	}
	return rt, listener, nil
}

// newQueueListener 创建 LISTEN 专用的 pgx 连接池；失败只记日志并返回 nil（队列退化成轮询）。
func newQueueListener(dsn string) *pgxpool.Pool {
	pcfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		logger.Warn("解析数据库连接串失败，任务队列将用轮询取任务", zap.Error(err))
		return nil
	}
	pcfg.MaxConns = 2
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		logger.Warn("创建任务队列的监听连接池失败，将用轮询取任务", zap.Error(err))
		return nil
	}
	return pool
}
